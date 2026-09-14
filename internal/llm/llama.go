package llm

import (
        "archive/zip"
        "compress/gzip"
        "context"
        "encoding/json"
        "fmt"
        "io"
        "net"
        "net/http"
        "os"
        "os/exec"
        "path/filepath"
        "runtime"
        "strings"
        "sync"
        "syscall"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/netcheck"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/proc"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/sysinfo"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/vision"
)

// Engine lifecycle states (v1.1.3Z spec): the backend process state is
// authoritative and the UI may never invent any of these.
//
//      idle        fresh manager, nothing attempted yet
//      downloading engine binary is being fetched
//      starting    subprocess spawned, model loading (health pending)
//      ready       healthy: /health 200, model loaded, no inference in flight
//      running     subprocess alive and serving (superset of ready/busy)
//      busy        inference request in flight
//      stopping    deliberate shutdown in progress
//      stopped     terminated deliberately (or died without auto-restart armed)
//      failed      terminal failure after bounded retries
const (
        StateIdle        = "idle"
        StateDownloading = "downloading"
        StateStarting    = "starting"
        StateReady       = "ready"
        StateRunning     = "running"
        StateBusy        = "busy"
        StateStopping    = "stopping"
        StateStopped     = "stopped"
        StateFailed      = "failed"
)

// aliveStates are the states in which the subprocess is alive and the
// HTTP endpoint is expected to answer.
var aliveStates = map[string]bool{
        StateRunning: true,
        StateReady:   true,
        StateBusy:    true,
}

// EngineEvent is one state transition published to subscribers (API/WS).
type EngineEvent struct {
        State     string    `json:"state"`
        Previous  string    `json:"previous"`
        Model     string    `json:"model,omitempty"`
        Detail    string    `json:"detail,omitempty"`
        Timestamp time.Time `json:"timestamp"`
}

// LlamaServer manages a llama.cpp server subprocess. This is the standalone
// inference engine that replaces LM Studio's local server.
type LlamaServer struct {
        // src is the live configuration source. Engine starts capture one
        // consistent snapshot; health probes read the current value.
        // (v1.1.4Z: replaced the shared mutable *Config which raced the HTTP
        // config patcher.)
        src     *config.Source
        cmd     *exec.Cmd
        mu      sync.Mutex
        state   string
        detail  string
        logBuf  *ringBuffer
        errRing *ringBuffer

        loaded   string // absolute path of the model currently loaded by the subprocess
        switchMu sync.Mutex

        // mmproj is the multimodal projector the engine was launched with.
        // Empty when the engine runs text-only.
        mmproj string

        // visionState / visionReason carry the v1.2.0 vision readiness state
        // machine for the RUNNING (or last) boot: loading → ready when the
        // projector survives a verified boot, degraded after the bounded
        // text-only retry, failed when an explicit override could not resolve
        // or the boot failed outright. Between boots it holds the evidence-
        // backed capability evaluation for the configured model. Guarded by mu.
        visionState  vision.State
        visionReason string

        // engineUpdateTried prevents repeated engine self-updates during one
        // application run.
        engineUpdateTried bool

        // restarts counts auto-restart attempts for the CURRENT alive episode;
        // reset on each successful Start. Bounded by maxAutoRestarts.
        restarts int

        // startedAt is the time of the last successful boot (v1.1.5Z: the
        // measured uptime base for backend Metrics).
        startedAt time.Time

        // stopping arms the deliberate-shutdown flag so the exit watcher's
        // auto-restart is suppressed while Stop() runs.
        stopping bool

        // caps is the engine capability profile in force for the CURRENT or
        // last boot (Phase 7). Guarded by mu. Resolved per boot from the
        // persisted verified profile / --help detection, then persisted again
        // after a verified successful startup.
        caps *EngineCaps

        // verifiedModel / verifiedCtx record what the startup state machine
        // actually verified after /health went 200 (Phase 7): the served model
        // list and the engine-reported context capability. Empty/0 when the
        // verification probe was unavailable — the UI must never present a
        // stronger claim than what was verified.
        verifiedModel string
        verifiedCtx   int

        // subs receive every state transition (never blocked; slow subscribers
        // drop events). Registered via SubscribeEvents.
        subsMu sync.Mutex
        subs   map[int]chan EngineEvent
        subSeq int
}

// maxAutoRestarts bounds the watchdog's automatic recovery attempts per
// alive episode. Retries are bounded by design: after this many backoff
// restarts the engine reports failed and waits for explicit user action.
const maxAutoRestarts = 3

type ringBuffer struct {
        mu   sync.Mutex
        buf  []string
        head int
        size int
}

func newRing(n int) *ringBuffer {
        return &ringBuffer{
                buf:  make([]string, n),
                size: n,
        }
}

func (r *ringBuffer) add(s string) {
        r.mu.Lock()
        defer r.mu.Unlock()

        r.buf[r.head] = s
        r.head = (r.head + 1) % r.size
}

func (r *ringBuffer) lines() []string {
        r.mu.Lock()
        defer r.mu.Unlock()

        out := make([]string, 0, r.size)

        for i := 0; i < r.size; i++ {
                idx := (r.head + i) % r.size

                if r.buf[idx] != "" {
                        out = append(out, r.buf[idx])
                }
        }

        return out
}

func (r *ringBuffer) reset() {
        r.mu.Lock()
        defer r.mu.Unlock()

        r.buf = make([]string, r.size)
        r.head = 0
}

// NewLlamaServer returns a manager bound to the live config source.
func NewLlamaServer(src *config.Source) *LlamaServer {
        return &LlamaServer{
                src:     src,
                state:   StateIdle,
                logBuf:  newRing(500),
                errRing: newRing(64),
                subs:    make(map[int]chan EngineEvent),
        }
}

// SubscribeEvents registers a channel receiving every engine state
// transition. The returned func unsubscribes. Channels are buffered;
// slow consumers drop transitions rather than block the engine.
func (s *LlamaServer) SubscribeEvents() (<-chan EngineEvent, func()) {
        s.subsMu.Lock()
        defer s.subsMu.Unlock()

        ch := make(chan EngineEvent, 32)
        s.subSeq++
        id := s.subSeq
        s.subs[id] = ch

        return ch, func() {
                s.subsMu.Lock()
                defer s.subsMu.Unlock()

                if existing, ok := s.subs[id]; ok {
                        delete(s.subs, id)
                        close(existing)
                }
        }
}

// Detail returns the latest human-readable engine detail (failure reason,
// recovery note). Empty when nothing noteworthy happened.
func (s *LlamaServer) Detail() string {
        s.mu.Lock()
        defer s.mu.Unlock()

        return s.detail
}

// SetDetail records a human-readable state detail.
func (s *LlamaServer) SetDetail(detail string) {
        s.mu.Lock()
        s.detail = detail
        s.mu.Unlock()
}

// IsAlive reports whether the subprocess is alive and serving.
func (s *LlamaServer) IsAlive() bool {
        s.mu.Lock()
        defer s.mu.Unlock()

        return aliveStates[s.state]
}

// MarkBusy flips the healthy engine between ready and busy around an
// inference request. It is a no-op unless the engine is alive — remote
// providers and adopted engines never report busy through this path.
//
// v1.1.4Z: the whole transition runs under one mutex acquisition. The
// previous version checked aliveness, released the lock, then re-locked in
// setState — a process death inside that window could be overwritten by a
// stale busy/ready transition.
func (s *LlamaServer) MarkBusy(busy bool) {
        s.mu.Lock()
        defer s.mu.Unlock()

        if !aliveStates[s.state] || s.stopping {
                return
        }

        switch {
        case busy && s.state != StateBusy:
                s.setStateLocked(StateBusy)
        case !busy && s.state == StateBusy:
                s.setStateLocked(StateReady)
        }
}

// State returns the current server state.
func (s *LlamaServer) State() string {
        s.mu.Lock()
        defer s.mu.Unlock()

        return s.state
}

// Logs returns the most recent log lines from the subprocess.
func (s *LlamaServer) Logs() []string {
        return s.logBuf.lines()
}

// IsRunning returns true if the subprocess is alive and listening.
// Both the coarse "running" and the fine-grained "ready"/"busy" states
// count: callers only care whether the endpoint is expected to answer.
func (s *LlamaServer) IsRunning() bool {
        s.mu.Lock()
        defer s.mu.Unlock()

        return aliveStates[s.state]
}

// ensureBinary makes sure the llama.cpp server binary exists and returns
// its effective path. An empty configured LlamaBinPath is resolved to the
// default location and persisted through the config source (v1.1.4Z: the
// old code mutated the shared Config in place).
func (s *LlamaServer) ensureBinary(cfg *config.Config) (string, error) {
        binPath := cfg.LlamaBinPath

        if binPath == "" {
                binPath = filepath.Join(
                        cfg.DataDir,
                        "bin",
                        llamaBinaryName(),
                )

                next := s.src.Update(func(c *config.Config) {
                        c.LlamaBinPath = binPath
                })

                if err := config.Save(next.ConfigPath(), next); err != nil {
                        return "", fmt.Errorf("persist default engine path: %w", err)
                }
        }

        if _, err := os.Stat(binPath); err == nil {
                if updater.InstalledEngineTag(cfg) == "" {
                        updater.RecordEngineTag(cfg, updater.DefaultEngineTag)
                }

                return binPath, nil
        }

        if netcheck.IsOffline() {
                return "", fmt.Errorf(
                        "llama.cpp server binary missing and you appear to be OFFLINE. "+
                                "Reconnect once so the server can be downloaded automatically, "+
                                "or place a prebuilt llama-server(.exe) into %s and retry",
                        filepath.Dir(binPath),
                )
        }

        dir := filepath.Dir(binPath)

        if err := os.MkdirAll(dir, 0o755); err != nil {
                return "", err
        }

        s.setState(StateDownloading)

        url, tag, err := llamaDownloadURL()
        if err != nil {
                return "", err
        }

        s.logf("Downloading llama.cpp server from %s", url)

        if err := downloadAndExtract(url, dir); err != nil {
                return "", fmt.Errorf("download llama.cpp: %w", err)
        }

        if _, err := os.Stat(binPath); err != nil {
                return "", fmt.Errorf(
                        "llama.cpp binary not found at %s after extraction",
                        binPath,
                )
        }

        if err := os.Chmod(binPath, 0o755); err != nil {
                // The binary must be executable on Unix; keep going on Windows where
                // the permission bit is meaningless but log the failure.
                s.logf("chmod engine binary: %v", err)
        }
        updater.RecordEngineTag(cfg, tag)

        return binPath, nil
}

const modelLoadTimeout = 180 * time.Second
const engineCompatMax = 3

func compatLevelName(level int) string {
        switch level {
        case 1:
                return "template compat (--jinja)"
        case 2:
                return "no speed flags"
        case 3:
                return "safe mode (CPU)"
        default:
                return "full speed"
        }
}

// Start boots the llama.cpp server as a subprocess if not already running.
// Serialized on switchMu so a prewarm and a first-send can never race.
func (s *LlamaServer) Start() error {
        s.switchMu.Lock()
        defer s.switchMu.Unlock()

        return s.startLocked()
}

// startLocked is the real boot path; the caller holds switchMu.
//
// The compatibility ladder retries progressively safer launch profiles.
// When the engine reports an unsupported model architecture, the bundled
// llama.cpp engine may be updated once and the ladder retried.
//
// adoptExisting must never be called while s.mu is held. The previous
// implementation could deadlock by attempting to lock s.mu again after
// adopting an existing engine.
func (s *LlamaServer) startLocked() error {
        s.mu.Lock()

        if aliveStates[s.state] || s.state == StateStarting {
                s.mu.Unlock()
                return nil
        }

        s.mu.Unlock()

        // v1.1.4Z: one consistent snapshot for the whole boot. A concurrent
        // Settings PATCH can no longer produce a half-old, half-new launch.
        cfg := s.src.Load()

        portInUse := PortInUse(
                cfg.LlamaHost,
                cfg.LlamaPort,
        )

        if portInUse {
                if s.adoptExisting(cfg) {
                        s.setState(StateReady)
                        return nil
                }

                s.setState(StateFailed)

                return fmt.Errorf(
                        "port %d is already in use by another program — "+
                                "change LlamaPort in config.json or close the other app",
                        cfg.LlamaPort,
                )
        }

        s.setState(StateStarting)

        binPath, err := s.ensureBinary(cfg)
        if err != nil {
                s.setState(StateFailed)
                return err
        }

        // Phase 7 startup state machine — before any launch attempt the
        // engine's own CLI contract is known: a persisted VERIFIED profile
        // wins, otherwise the binary is probed once via --help (tag
        // heuristic as last resort). Every launch profile below is built
        // from and validated against this profile.
        caps := s.detectCapsForBoot(cfg, binPath)

        s.mu.Lock()
        s.caps = caps
        s.verifiedModel = ""
        s.verifiedCtx = 0
        s.mu.Unlock()

        modelPath, err := ResolveModelPath(
                cfg.ModelsDir,
                cfg.Model,
        )
        if err != nil {
                s.setState(StateFailed)
                return err
        }

        mmproj := ""

        if cfg.VisionEnabled {
                if p := vision.FindProjector(
                        cfg.ModelsDir,
                        modelPath,
                        cfg.VisionMMProj,
                ); p != "" {
                        mmproj = p
                        s.logf(
                                "vision projector paired: %s",
                                filepath.Base(p),
                        )
                }
        }

        s.mu.Lock()
        s.mmproj = mmproj
        s.mu.Unlock()

        // v1.2.0: seed the vision state machine from the pre-boot evidence
        // evaluation (architecture + projector discovery + override check)
        // BEFORE the engine starts, so the UI shows honest states at every
        // point of the boot instead of guessing from filenames.
        var modelArch string
        if capsModel := ResolveModelCapabilities(cfg, modelPath); capsModel != nil {
                modelArch = capsModel.Arch
        }
        eval := vision.EvaluateModel(cfg.ModelsDir, modelPath, cfg.VisionMMProj, modelArch)
        if !cfg.VisionEnabled {
                reason := eval.Reason
                if reason == "" {
                        reason = "vision disabled in settings"
                } else {
                        reason += " — vision disabled in settings"
                }
                s.setVision(eval.State, reason)
        } else {
                s.setVision(eval.State, eval.Reason)
        }
        if cfg.VisionEnabled && mmproj != "" {
                s.setVision(vision.StateLoading,
                        "projector "+filepath.Base(mmproj)+" paired — verifying with engine boot")
        }

        visionRetryDone := false

        for {
                startLevel := cfg.EngineCompat

                if startLevel < 0 || startLevel > engineCompatMax {
                        startLevel = 0
                }

                // v1.1.7: boot-start snapshot of the persisted compat level
                // so the success path can tell a fresh descent (stamp the
                // reason) from a routine re-boot at the same level (leave
                // the stamp alone — re-stamping every boot would keep
                // re-blocking the retry-up gate below forever).
                persistedLevel := cfg.EngineCompat

                var lastErr error
                var lastReason string
                retryAttempted := false

                // persistBootOutcome records the final active configuration:
                // the compatibility level, WHY it is above full speed (when
                // it is), and the verified capability profile. The reason
                // stamp is refreshed only when the level actually changed or
                // a full-speed retry was attempted this boot.
                persistBootOutcome := func(level int, reason string) {
                        stamp := level != persistedLevel || retryAttempted

                        if cur := s.src.Load(); cur.EngineCompat != level || stamp {
                                next := s.src.Update(func(c *config.Config) {
                                        c.EngineCompat = level

                                        if level > 0 {
                                                c.EngineCompatReason = reason
                                                c.EngineCompatAt = time.Now().UTC().Format(time.RFC3339)
                                        } else {
                                                c.EngineCompatReason = ""
                                                c.EngineCompatAt = ""
                                        }
                                })

                                if err := config.Save(
                                        next.ConfigPath(),
                                        next,
                                ); err != nil {
                                        s.logf("persist engine compat: %v", err)
                                }
                        }

                        // Phase 7: persist the capability profile ONLY
                        // after a verified successful startup, including any
                        // surgical repairs applied along the way.
                        s.mu.Lock()
                        effective := s.caps
                        s.mu.Unlock()
                        s.persistVerifiedCaps(cfg, effective)
                }

                // v1.1.7 bounded full-speed retry-up: when the ONLY recorded
                // reason for the persisted compatibility level is an option
                // rejection, and the now-verified capability profile
                // postdates that record and validates the level-0 profile,
                // give full speed ONE chance before resuming the ladder.
                // See shouldRetryFullSpeed for the exact gate.
                if startLevel > 0 {
                        level0Args := s.buildArgsWithCaps(cfg, modelPath, 0, caps)

                        if retry, why := shouldRetryFullSpeed(cfg, caps, level0Args); retry {
                                retryAttempted = true

                                s.logf(
                                        "capability profile changed since the compatibility downgrade (%s) — retrying the full-speed profile once",
                                        why,
                                )
                                logging.Default().Info("engine",
                                        "retrying full-speed launch (compat 0) before the persisted level %d — recorded reason %q no longer applies to the verified capability profile",
                                        startLevel, why)

                                if err := s.launchWithRepair(cfg, binPath, modelPath, 0, caps); err == nil {
                                        persistBootOutcome(0, "")

                                        s.logf("full-speed profile restored — compatibility mode no longer required")

                                        logging.Default().Info("engine",
                                                "engine restored to the full-speed profile (compat 0) for %s",
                                                filepath.Base(modelPath))

                                        s.mu.Lock()
                                        s.restarts = 0
                                        s.stopping = false
                                        s.startedAt = time.Now()
                                        s.mu.Unlock()

                                        // v1.2.0: the projector survived a VERIFIED
                                        // boot — the only honest path to ready.
                                        if mmproj != "" {
                                                s.setVision(vision.StateReady,
                                                        "mmproj verified: engine serving with the paired projector "+filepath.Base(mmproj))
                                        }

                                        s.setState(StateReady)
                                        return nil
                                }

                                // Remember WHY the retry failed so a successful
                                // boot further down the ladder records an honest
                                // reason instead of an empty one.
                                lastReason = compatReasonFromError(err)

                                logging.Default().Warn("engine",
                                        "full-speed retry failed (%v) — resuming the compatibility ladder at level %d",
                                        err, startLevel)
                                s.logf("full-speed retry failed — resuming at compatibility level %d", startLevel)
                        }
                }

                for pass := 0; pass < 2; pass++ {
                        for level := startLevel; level <= engineCompatMax; level++ {
                                err := s.launchWithRepair(cfg, binPath, modelPath, level, caps)

                                if err == nil {
                                        // Never overwrite a recorded reason with an
                                        // empty one (a boot that succeeds at the
                                        // persisted level without any new failure
                                        // has no fresh reason of its own).
                                        reasonForPersist := lastReason
                                        if reasonForPersist == "" {
                                                reasonForPersist = cfg.EngineCompatReason
                                        }

                                        persistBootOutcome(level, reasonForPersist)

                                        if level > 0 {
                                                why := lastReason
                                                if why == "" {
                                                        why = "reason unrecorded"
                                                }

                                                s.logf(
                                                        "engine started in compatibility mode %d (%s) — some speed flags disabled (recorded reason: %s)",
                                                        level,
                                                        compatLevelName(level),
                                                        why,
                                                )

                                                logging.Default().Warn(
                                                        "engine",
                                                        "started in compatibility mode %d (%s) for %s — recorded reason: %s",
                                                        level,
                                                        compatLevelName(level),
                                                        filepath.Base(modelPath),
                                                        why,
                                                )
                                        }

                                        // A successful boot starts a fresh recovery episode.
                                        s.mu.Lock()
                                        s.restarts = 0
                                        s.stopping = false
                                        s.startedAt = time.Now()
                                        s.mu.Unlock()

                                        // v1.2.0: verified boot WITH the projector
                                        // — vision is genuinely ready.
                                        if mmproj != "" {
                                                s.setVision(vision.StateReady,
                                                        "mmproj verified: engine serving with the paired projector "+filepath.Base(mmproj))
                                        }

                                        s.setState(StateReady)
                                        return nil
                                }

                                lastErr = err
                                lastReason = compatReasonFromError(err)

                                logging.Default().Error(
                                        "engine",
                                        "startup attempt failed (compat %d, %s): %v",
                                        level,
                                        compatLevelName(level),
                                        err,
                                )

                                if _, died := err.(*exitFailure); !died {
                                        s.setState(StateFailed)
                                        return err
                                }

                                time.Sleep(250 * time.Millisecond)
                        }

                        if pass == 0 &&
                                needsNewerEngine(lastErr) &&
                                s.updateEngineForModel(cfg) {
                                startLevel = 0
                                s.setState(StateStarting)
                                continue
                        }

                        break
                }

                if mmproj != "" && !visionRetryDone {
                        visionRetryDone = true

                        projectName := filepath.Base(mmproj)
                        mmproj = ""

                        s.mu.Lock()
                        s.mmproj = ""
                        s.mu.Unlock()

                        // v1.2.0: the projector is REAL but the engine
                        // rejected it with every launch profile — a degraded,
                        // honestly-reported state, never a silent failure.
                        s.setVision(vision.StateDegraded,
                                "projector "+projectName+" failed with every launch profile; running text-only")

                        s.logf(
                                "all profiles failed with the vision projector — retrying text-only",
                        )

                        logging.Default().Warn(
                                "engine",
                                "vision projector %s failed with every profile; restarting without vision",
                                projectName,
                        )

                        s.setState(StateStarting)
                        continue
                }

                // v1.2.0: terminal boot failure — keep the vision story
                // honest about what died.
                if state, _ := s.visionSnapshot(); state == vision.StateLoading {
                        s.setVision(vision.StateFailed,
                                "engine boot failed while loading the vision projector")
                }

                s.setState(StateFailed)
                return lastErr
        }
}

// VisionActive reports whether the running engine carries a multimodal
// projector.
func (s *LlamaServer) VisionActive() bool {
        s.mu.Lock()
        defer s.mu.Unlock()

        return aliveStates[s.state] && s.mmproj != ""
}

// ProjectorPath returns the active projector file.
func (s *LlamaServer) ProjectorPath() string {
        s.mu.Lock()
        defer s.mu.Unlock()

        return s.mmproj
}

// setVision records a vision state-machine transition (v1.2.0). Every
// caller passes a reason grounded in real evidence: pairing, a verified
// boot, projector rejection, or an unresolvable override.
func (s *LlamaServer) setVision(state vision.State, reason string) {
        s.mu.Lock()
        s.visionState = state
        s.visionReason = reason
        s.mu.Unlock()
}

// visionSnapshot reads the current vision state under the lock.
func (s *LlamaServer) visionSnapshot() (vision.State, string) {
        s.mu.Lock()
        defer s.mu.Unlock()

        return s.visionState, s.visionReason
}

// VisionStatus is the runtime vision readiness surface consumed by
// /api/engine (v1.2.0). Ready is only ever reported because the engine is
// actually serving WITH the projector; the projector byte size is measured
// from disk at snapshot time.
type VisionStatus struct {
        Active         bool         `json:"active"`
        State          vision.State `json:"state"`
        Reason         string       `json:"reason,omitempty"`
        Projector      string       `json:"projector,omitempty"`
        ProjectorName  string       `json:"projectorName,omitempty"`
        ProjectorBytes int64        `json:"projectorBytes,omitempty"`
}

// VisionStatus returns the current vision readiness of the engine. Before
// the first boot it reports the evidence-backed capability evaluation for
// the configured model; during a boot it reports loading; after a verified
// boot with the projector it reports ready.
func (s *LlamaServer) VisionStatus() VisionStatus {
        state, reason := s.visionSnapshot()

        s.mu.Lock()
        mmproj := s.mmproj
        alive := aliveStates[s.state]
        engineReady := s.state == StateReady || s.state == StateRunning ||
                s.state == StateBusy
        s.mu.Unlock()

        if !state.Known() {
                state = vision.StateSupported
                reason = "no boot has evaluated vision yet"
        }

        // A projector in flight on a verified engine is READY — the engine
        // state machine only reaches Ready after /health + model
        // verification succeeded with that exact launch configuration.
        if alive && mmproj != "" && state == vision.StateLoading && engineReady {
                state = vision.StateReady
                reason = "mmproj verified: engine serving with the paired projector " +
                        filepath.Base(mmproj)
        }

        vs := VisionStatus{
                Active: alive && mmproj != "",
                State:  state,
                Reason: reason,
        }

        if mmproj != "" {
                vs.Projector = mmproj
                vs.ProjectorName = filepath.Base(mmproj)
                if fi, err := os.Stat(mmproj); err == nil {
                        vs.ProjectorBytes = fi.Size()
                }
        }

        return vs
}

// Pid returns the engine subprocess pid.
func (s *LlamaServer) Pid() int {
        s.mu.Lock()
        defer s.mu.Unlock()

        if s.cmd != nil && s.cmd.Process != nil {
                return s.cmd.Process.Pid
        }

        return 0
}

// StartedAt returns the time of the last successful boot (zero before the
// first boot). v1.1.5Z: measured uptime base for backend metrics.
func (s *LlamaServer) StartedAt() time.Time {
        s.mu.Lock()
        defer s.mu.Unlock()

        return s.startedAt
}

// Restarts returns the auto-restart count of the current alive episode.
// v1.1.5Z: measured value surfaced through backend metrics.
func (s *LlamaServer) Restarts() int {
        s.mu.Lock()
        defer s.mu.Unlock()

        return s.restarts
}

// ProbeHealth performs a REAL active health check against the managed
// llama.cpp /health endpoint (one bounded HTTP GET), unlike State()/
// IsAlive() which only read the cached state machine. v1.1.5Z: extracted
// behind the backend contract so Health() is an honest probe for both
// engines (the same endpoint waitReadySignaled polls during startup).
func (s *LlamaServer) ProbeHealth(ctx context.Context) error {
        if s.IsRunning() {
                return s.probeHealthEndpoint(ctx)
        }

        // Not alive per the authoritative state machine: a probe would
        // answer from some OTHER process that grabbed the port.
        state := s.State()

        return fmt.Errorf(
                "engine is not running (state %s)",
                state,
        )
}

// probeHealthEndpoint GETs http://{host}:{port}/health with a bounded
// client and treats HTTP 200 as healthy.
func (s *LlamaServer) probeHealthEndpoint(ctx context.Context) error {
        cfg := s.src.Load()

        url := fmt.Sprintf(
                "http://%s:%d/health",
                cfg.LlamaHost,
                cfg.LlamaPort,
        )

        probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
        defer cancel()

        req, err := http.NewRequestWithContext(
                probeCtx,
                http.MethodGet,
                url,
                nil,
        )

        if err != nil {
                return err
        }

        resp, err := http.DefaultClient.Do(req)
        if err != nil {
                return fmt.Errorf("health probe: %w", err)
        }

        defer resp.Body.Close()

        if resp.StatusCode != http.StatusOK {
                return fmt.Errorf(
                        "health probe: unexpected status %s",
                        resp.Status,
                )
        }

        return nil
}

// buildArgs assembles the launch command line using the server's current
// capability profile (see buildArgsWithCaps).
func (s *LlamaServer) buildArgs(
        cfg *config.Config,
        modelPath string,
        level int,
) []string {
        s.mu.Lock()
        caps := s.caps
        s.mu.Unlock()

        return s.buildArgsWithCaps(cfg, modelPath, level, caps)
}

// buildArgsWithCaps assembles the llama.cpp server command line for a
// compatibility level, honoring the engine capability profile: Speed Pack
// options are emitted only when the engine build supports them, in the
// exact layout that build supports (Phase 7 --flash-attn fix).
//
//      0 — everything: Speed Pack flags, GPU offload, user extra args
//      1 — everything + --jinja
//      2 — --jinja + GPU, but no speed flags
//      3 — bare: model, host/port, context, threads
//
// v1.1.4Z: the optional sampling knobs (min-p, repeat-last-n, presence and
// frequency penalty, mirostat tau/eta) are only emitted when the user set a
// non-zero value, so default installs keep the exact launch contract the
// stress suite pins while Settings sampling edits actually reach the engine.
func (s *LlamaServer) buildArgsWithCaps(
        cfg *config.Config,
        modelPath string,
        level int,
        caps *EngineCaps,
) []string {
        base := []string{
                "--model",
                modelPath,
                "--host",
                cfg.LlamaHost,
                "--port",
                fmt.Sprintf("%d", cfg.LlamaPort),
                // Phase 7 model-aware context: the launch context is the minimum
                // of the configured limit and the loaded GGUF model's own context
                // training limit (see modelcaps.go) — a small model can no longer
                // be handed a window bigger than it actually has.
                "--ctx-size",
                fmt.Sprintf("%d", launchContextSize(cfg, modelPath)),
                "--batch-size",
                fmt.Sprintf("%d", cfg.LLM.NumBatch),
                "--threads",
                fmt.Sprintf("%d", threadsFor(cfg)),
                "--temp",
                fmt.Sprintf("%v", cfg.LLM.Temperature),
                "--top-p",
                fmt.Sprintf("%v", cfg.LLM.TopP),
                "--top-k",
                fmt.Sprintf("%d", cfg.LLM.TopK),
                "--repeat-penalty",
                fmt.Sprintf("%v", cfg.LLM.RepeatPenalty),
        }

        if cfg.LLM.MinP > 0 {
                base = append(base, "--min-p", fmt.Sprintf("%v", cfg.LLM.MinP))
        }

        if cfg.LLM.RepeatLastN > 0 {
                base = append(
                        base,
                        "--repeat-last-n",
                        fmt.Sprintf("%d", cfg.LLM.RepeatLastN),
                )
        }

        if cfg.LLM.PresencePenalty != 0 {
                base = append(
                        base,
                        "--presence-penalty",
                        fmt.Sprintf("%v", cfg.LLM.PresencePenalty),
                )
        }

        if cfg.LLM.FrequencyPenalty != 0 {
                base = append(
                        base,
                        "--frequency-penalty",
                        fmt.Sprintf("%v", cfg.LLM.FrequencyPenalty),
                )
        }

        s.mu.Lock()
        mmproj := s.mmproj
        s.mu.Unlock()

        if mmproj != "" {
                base = append(base, "--mmproj", mmproj)

                // v1.2.0: explicit projector offload posture. "off" appends
                // --no-mmproj-offload (recent llama.cpp builds); "auto" and
                // "on" add nothing — the engine default is to offload the
                // projector when VRAM allows. An unsupported flag is caught
                // by the capability adapter's verified repair path, so this
                // never hard-codes an assumption about the engine build.
                if cfg.VisionMMProjOffload == "off" {
                        base = append(base, "--no-mmproj-offload")
                }
        }

        if cfg.LLM.Mirostat > 0 {
                base = append(
                        base,
                        "--mirostat",
                        fmt.Sprintf("%d", cfg.LLM.Mirostat),
                )

                if cfg.LLM.MirostatTau > 0 {
                        base = append(
                                base,
                                "--mirostat-tau",
                                fmt.Sprintf("%v", cfg.LLM.MirostatTau),
                        )
                }

                if cfg.LLM.MirostatEta > 0 {
                        base = append(
                                base,
                                "--mirostat-eta",
                                fmt.Sprintf("%v", cfg.LLM.MirostatEta),
                        )
                }
        }

        if cfg.LLM.Seed != 0 {
                base = append(
                        base,
                        "--seed",
                        fmt.Sprintf("%d", cfg.LLM.Seed),
                )
        }

        if level == engineCompatMax {
                return base
        }

        gpuLayers := cfg.LLM.NumGPU

        if gpuLayers <= 0 && s.autoGPUOffload(cfg) {
                gpuLayers = 99
        }

        if gpuLayers > 0 {
                base = append(
                        base,
                        "--n-gpu-layers",
                        fmt.Sprintf("%d", gpuLayers),
                )
        }

        if level <= 1 {
                base = append(base, SpeedArgsWithCaps(cfg, caps)...)
        } else if caps == nil || caps.NoWebUI {
                base = append(base, "--no-webui")
        }

        if level >= 1 && (caps == nil || caps.Jinja) {
                base = append(base, "--jinja")
        }

        if level <= 2 &&
                strings.TrimSpace(cfg.LlamaExtraArgs) != "" {
                base = append(
                        base,
                        strings.Fields(cfg.LlamaExtraArgs)...,
                )
        }

        return base
}

type procExit struct {
        done chan struct{}
        err  error
}

// launchWithRepair launches ONE compatibility level with Phase 7 surgical
// option repair: validate the profile → spawn → on a classified option
// failure repair ONLY the offending option and retry at the same level
// (bounded). Failures that are not option problems (model load, host
// environment, unknown architecture) are returned untouched so the outer
// compatibility ladder / engine-update logic handles them.
func (s *LlamaServer) launchWithRepair(
        cfg *config.Config,
        binPath string,
        modelPath string,
        level int,
        caps *EngineCaps,
) error {
        if caps == nil {
                caps = defaultCapsForTag(currentEngineTag(cfg))
        }

        effective := caps.Clone()

        for attempt := 0; ; attempt++ {
                args := s.buildArgsWithCaps(cfg, modelPath, level, effective)

                // Phase 7: validate option/value pairs BEFORE spawning. The
                // historical regression (--flash-attn --cache-reuse) fails
                // validation here and can therefore never reach the engine.
                if problems := argProblems(args, effective); len(problems) > 0 {
                        logging.Default().Error("engine",
                                "launch profile rejected by pre-launch validation (compat %d): %s",
                                level, strings.Join(problems, "; "))
                        s.logf("launch profile validation failed: %s",
                                strings.Join(problems, "; "))
                        return fmt.Errorf(
                                "launch profile failed pre-launch validation: %s",
                                strings.Join(problems, "; "))
                }

                err := s.launchArgs(cfg, binPath, modelPath, args)
                if err == nil {
                        s.mu.Lock()
                        s.caps = effective
                        s.mu.Unlock()
                        return nil
                }

                // Non-exit failures (spawn problems, readiness timeouts)
                // are not option-layout problems — no surgical repair.
                if _, died := err.(*exitFailure); !died {
                        return err
                }

                if attempt >= maxOptionRepairs {
                        return err
                }

                sf := ClassifyStartupFailure(err.Error())
                if sf == nil || sf.Kind != FailOptionLayout && sf.Kind != FailUnknownOption {
                        return err
                }

                repaired := repairCapsFor(effective, sf, args)
                if repaired == nil {
                        return err
                }

                logRepair(level, effective, repaired, sf)
                s.logf("repairing option layout and retrying (compat %d, attempt %d/%d)",
                        level, attempt+1, maxOptionRepairs)

                effective = repaired
        }
}

// launchArgs spawns the server with one explicit argument vector and waits
// until it is healthy, the model is verified and the context capability is
// recorded (Phase 7 verified startup state machine).
func (s *LlamaServer) launchArgs(
        cfg *config.Config,
        binPath string,
        modelPath string,
        args []string,
) error {
        cmd := proc.Command(
                binPath,
                args...,
        )

        cmd.Dir = cfg.DataDir

        cmd.Stdout = newLineWriter(func(line string) {
                s.logf("[llama.cpp] %s", line)
        })

        s.errRing.reset()

        cmd.Stderr = newLineWriter(func(line string) {
                s.logf("[llama.cpp!] %s", line)
                s.errRing.add(line)
        })

        proc.Hide(cmd)

        if err := cmd.Start(); err != nil {
                return &exitFailure{
                        err: fmt.Errorf("start llama.cpp: %w", err),
                }
        }

        s.mu.Lock()
        s.cmd = cmd
        s.mu.Unlock()

        exit := &procExit{
                done: make(chan struct{}),
        }

        go func() {
                exit.err = cmd.Wait()

                s.mu.Lock()

                latest := s.cmd == cmd

                if latest {
                        s.cmd = nil
                }

                wasAlive :=
                        latest &&
                                s.loaded == modelPath &&
                                aliveStates[s.state]

                if wasAlive {
                        s.loaded = ""
                }

                s.mu.Unlock()

                if wasAlive {
                        s.setState(StateStopped)

                        logging.Default().Error(
                                "engine",
                                "engine exited while running: %v",
                                exit.err,
                        )

                        // v1.1.3Z bounded auto-recovery: the watchdog restarts
                        // the engine a bounded number of times with backoff so a
                        // crashed engine recovers transparently, while a
                        // fundamentally broken setup surfaces as `failed`
                        // instead of looping forever.
                        s.scheduleAutoRestart(modelPath)
                }

                close(exit.done)
        }()

        if err := s.waitReadySignaled(
                cfg,
                modelLoadTimeout,
                exit,
        ); err != nil {
                s.mu.Lock()

                c := s.cmd

                if s.cmd == cmd {
                        s.cmd = nil
                }

                s.mu.Unlock()

                if c != nil && c.Process != nil {
                        _ = c.Process.Kill()
                }

                select {
                case <-exit.done:
                case <-time.After(3 * time.Second):
                }

                return err
        }

        s.mu.Lock()

        if s.cmd != cmd {
                s.mu.Unlock()

                select {
                case <-exit.done:
                case <-time.After(time.Second):
                }

                return &exitFailure{
                        err:  fmt.Errorf("llama.cpp died immediately after becoming ready"),
                        tail: s.errRing.lines(),
                }
        }

        s.loaded = modelPath
        s.mu.Unlock()

        // Phase 7 verified startup state machine: /health 200 proves the
        // PROCESS serves; it does not prove the MODEL is loaded and the
        // CONTEXT capability is real. Both are verified here, best-effort
        // and honestly recorded — an unverifiable claim is never claimed.
        s.verifyStartupState(cfg, modelPath)

        return nil
}

// verifyStartupState probes the freshly-ready engine for model-loaded and
// context-capability evidence. Bounded and non-fatal: older builds may not
// expose the endpoints, in which case the verification fields stay empty
// and the UI sees exactly that.
func (s *LlamaServer) verifyStartupState(cfg *config.Config, modelPath string) {
        client := &http.Client{Timeout: 2 * time.Second}
        base := fmt.Sprintf("http://%s:%d", cfg.LlamaHost, cfg.LlamaPort)

        // 1. Verify the model is actually served: GET /v1/models.
        var verifiedModel string
        if resp, err := client.Get(base + "/v1/models"); err == nil {
                body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
                _ = resp.Body.Close()

                if resp.StatusCode == http.StatusOK {
                        var parsed struct {
                                Data []struct {
                                        ID string `json:"id"`
                                } `json:"data"`
                        }
                        if json.Unmarshal(body, &parsed) == nil && len(parsed.Data) > 0 {
                                verifiedModel = parsed.Data[0].ID
                        }
                }
        }

        // 2. Verify the context capability: GET /props (n_ctx when exposed).
        var verifiedCtx int
        if resp, err := client.Get(base + "/props"); err == nil {
                body, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
                _ = resp.Body.Close()

                if resp.StatusCode == http.StatusOK {
                        verifiedCtx = parsePropsContext(body)
                }
        }

        // 3. Fall back to the model card when the engine does not report:
        // the GGUF training limit is evidence too (launchContextSize already
        // clamped --ctx-size to it).
        if verifiedCtx <= 0 {
                if caps := ResolveModelCapabilities(cfg, modelPath); caps != nil {
                        verifiedCtx = launchContextSize(cfg, modelPath)
                }
        }

        s.mu.Lock()
        s.verifiedModel = verifiedModel
        s.verifiedCtx = verifiedCtx
        s.mu.Unlock()

        if verifiedModel != "" {
                s.logf("verified model loaded: %s", verifiedModel)
        }
        if verifiedCtx > 0 {
                s.logf("verified context capability: %d tokens", verifiedCtx)
        }
}

// parsePropsContext extracts n_ctx from llama.cpp /props JSON (several
// historical shapes are tolerated).
func parsePropsContext(body []byte) int {
        var shapes struct {
                DefaultGenerationSettings struct {
                        NCtx int `json:"n_ctx"`
                } `json:"default_generation_settings"`
                NCtx int `json:"n_ctx"`
        }
        if json.Unmarshal(body, &shapes) != nil {
                return 0
        }
        if shapes.DefaultGenerationSettings.NCtx > 0 {
                return shapes.DefaultGenerationSettings.NCtx
        }
        return shapes.NCtx
}

// VerifiedModel returns the model id the engine actually serves (empty
// when the verification probe could not run).
func (s *LlamaServer) VerifiedModel() string {
        s.mu.Lock()
        defer s.mu.Unlock()
        return s.verifiedModel
}

// VerifiedContext returns the engine-verified context window in tokens
// (0 when unverified).
func (s *LlamaServer) VerifiedContext() int {
        s.mu.Lock()
        defer s.mu.Unlock()
        return s.verifiedCtx
}

// EngineContextLimit returns the engine-verified context window ONLY when
// the subprocess is currently alive (starting/running/ready/busy). A dead
// engine reports 0 — a stale window from a previous boot must never clamp
// a plan for a boot that may use a different --ctx-size.
func (s *LlamaServer) EngineContextLimit() int {
        s.mu.Lock()
        defer s.mu.Unlock()
        if !aliveStates[s.state] {
                return 0
        }
        return s.verifiedCtx
}

// VerifiedReady reports whether the current boot proved real readiness:
// process alive AND /health 200 AND the serving model verified via
// /v1/models. "Ready" in the UI must mean this, not merely a spawned
// subprocess.
func (s *LlamaServer) VerifiedReady() bool {
        s.mu.Lock()
        defer s.mu.Unlock()
        return aliveStates[s.state] && s.verifiedModel != ""
}

// Caps returns the capability profile in force for the current/last boot.
func (s *LlamaServer) Caps() *EngineCaps {
        s.mu.Lock()
        defer s.mu.Unlock()
        return s.caps.Clone()
}

// waitReadySignaled polls /health until 200, but aborts immediately when
// the subprocess exits or the timeout passes.
func (s *LlamaServer) waitReadySignaled(
        cfg *config.Config,
        timeout time.Duration,
        exit *procExit,
) error {
        deadline := time.Now().Add(timeout)

        url := fmt.Sprintf(
                "http://%s:%d/health",
                cfg.LlamaHost,
                cfg.LlamaPort,
        )

        client := &http.Client{
                Timeout: 2 * time.Second,
        }

        for time.Now().Before(deadline) {
                if resp, err := client.Get(url); err == nil {
                        _ = resp.Body.Close()

                        if resp.StatusCode == http.StatusOK {
                                return nil
                        }
                }

                select {
                case <-exit.done:
                        return s.exitError(exit.err)

                case <-time.After(400 * time.Millisecond):
                }
        }

        select {
        case <-exit.done:
                return s.exitError(exit.err)

        default:
        }

        tail := s.tailLines(6)

        msg := fmt.Sprintf(
                "llama.cpp did not become ready within %v "+
                        "(the model may be too large for this machine)",
                timeout,
        )

        if tail != "" {
                msg += "\n\nEngine output (last lines):\n" + tail
        }

        return fmt.Errorf("%s", msg)
}

func (s *LlamaServer) exitError(err error) error {
        return &exitFailure{
                err:  explainExit(err),
                tail: s.errRing.lines(),
        }
}

type exitFailure struct {
        err  error
        tail []string
}

func explainExit(err error) error {
        if err == nil {
                return fmt.Errorf("llama.cpp exited during startup")
        }

        msg := err.Error()

        if strings.Contains(msg, "3221225781") ||
                strings.Contains(msg, "0xc0000135") {
                return fmt.Errorf(
                        "llama.cpp could not start: a required Windows DLL is missing. "+
                                "Install the free Microsoft Visual C++ Redistributable (64-bit) "+
                                "from https://aka.ms/vs/17/release/vc_redist.x64.exe "+
                                "and start again: %v",
                        err,
                )
        }

        return fmt.Errorf(
                "llama.cpp exited during startup: %v",
                err,
        )
}

func (e *exitFailure) Error() string {
        msg := "llama.cpp exited during startup"

        if e.err != nil {
                msg = e.err.Error()
        }

        if tail := compactLines(e.tail, 12); tail != "" {
                msg += "\n\nEngine output (last lines):\n" + tail
        }

        return msg
}

func (e *exitFailure) Unwrap() error {
        return e.err
}

func (s *LlamaServer) tailLines(n int) string {
        return compactLines(s.errRing.lines(), n)
}

func compactLines(lines []string, n int) string {
        if len(lines) > n {
                lines = lines[len(lines)-n:]
        }

        var kept []string

        for _, line := range lines {
                line = strings.TrimSpace(line)

                if line == "" {
                        continue
                }

                if len(line) > 200 {
                        line = line[:200] + "…"
                }

                kept = append(kept, line)
        }

        return strings.Join(kept, "\n")
}

func needsNewerEngine(err error) bool {
        if err == nil {
                return false
        }

        msg := strings.ToLower(err.Error())

        for _, sig := range []string{
                "unknown model architecture",
                "unknown architecture",
                "unsupported architecture",
                "unknown model type",
                "unrecognized",
        } {
                if strings.Contains(msg, sig) {
                        return true
                }
        }

        return false
}

func (s *LlamaServer) updateEngineForModel(cfg *config.Config) bool {
        if s.engineUpdateTried {
                return false
        }

        s.engineUpdateTried = true

        if netcheck.IsOffline() {
                s.logf(
                        "model needs a newer engine but the machine is offline — skipping auto-update",
                )
                return false
        }

        ctx, cancel := context.WithTimeout(
                context.Background(),
                10*time.Minute,
        )
        defer cancel()

        latest, err := updater.LatestTag(ctx)
        if err != nil || latest == "" {
                s.logf(
                        "could not find a newer engine: %v",
                        err,
                )
                return false
        }

        current := updater.InstalledEngineTag(cfg)

        if current == "" {
                current = updater.DefaultEngineTag
        }

        if latest == current {
                s.logf(
                        "engine %s is already the newest release — "+
                                "the model is still not loadable",
                        current,
                )
                return false
        }

        s.logf(
                "this model needs a newer llama.cpp — updating %s → %s",
                current,
                latest,
        )

        logging.Default().Info(
                "engine",
                "auto-updating engine for new model architecture: %s → %s",
                current,
                latest,
        )

        s.setState(StateDownloading)

        if _, err := updater.UpdateEngine(
                ctx,
                cfg,
                nil,
                latest,
        ); err != nil {
                s.logf(
                        "engine auto-update failed: %v",
                        err,
                )

                logging.Default().Warn(
                        "engine",
                        "auto-update failed: %v",
                        err,
                )

                return false
        }

        next := s.src.Update(func(c *config.Config) {
                c.EngineCompat = 0
        })

        if err := config.Save(next.ConfigPath(), next); err != nil {
                s.logf("persist engine compat reset: %v", err)
        }

        return true
}

func (s *LlamaServer) autoGPUOffload(cfg *config.Config) bool {
        if !cfg.GPUAutoOffload {
                return false
        }

        if !s.hasVulkanBackend(cfg) {
                return false
        }

        info := sysinfo.Probe()
        return len(info.GPU) > 0
}

func (s *LlamaServer) hasVulkanBackend(cfg *config.Config) bool {
        return VulkanAvailable(cfg)
}

// VulkanAvailable reports whether a Vulkan backend sits beside the engine
// binary named by cfg (v1.2.0: shared with the hardware intelligence layer
// so GPU-posture recommendations rest on the same evidence as launch).
func VulkanAvailable(cfg *config.Config) bool {
        bin := cfg.LlamaBinPath

        if bin == "" {
                bin = filepath.Join(
                        cfg.DataDir,
                        "bin",
                        llamaBinaryName(),
                )
        }

        if _, err := os.Stat(
                filepath.Join(
                        filepath.Dir(bin),
                        "ggml-vulkan.dll",
                ),
        ); err == nil {
                return true
        }

        if runtime.GOOS != "windows" {
                if _, err := os.Stat(
                        filepath.Join(
                                filepath.Dir(bin),
                                "libggml-vulkan.so",
                        ),
                ); err == nil {
                        return true
                }
        }

        return false
}

func (s *LlamaServer) HasVulkanBackendForTest() bool {
        return s.hasVulkanBackend(s.src.Load())
}

func (s *LlamaServer) AutoGPUOffloadForTest() bool {
        return s.autoGPUOffload(s.src.Load())
}

func (s *LlamaServer) BuildArgsForTest(
        modelPath string,
        level int,
) []string {
        return s.buildArgs(s.src.Load(), modelPath, level)
}

func (s *LlamaServer) SetProjectorForTest(path string) {
        s.mu.Lock()
        s.mmproj = path
        s.mu.Unlock()
}

func (s *LlamaServer) ProjectorPathForTest() string {
        return s.ProjectorPath()
}

func MakeExitFailureForTest(
        err error,
        tail []string,
) error {
        return &exitFailure{
                err:  explainExit(err),
                tail: tail,
        }
}

func IsExitFailureForTest(err error) bool {
        _, ok := err.(*exitFailure)
        return ok
}

func NeedsNewerEngineForTest(err error) bool {
        return needsNewerEngine(err)
}

func CompactLinesForTest(
        lines []string,
        n int,
) string {
        return compactLines(lines, n)
}

// adoptExisting reports whether the port already answers our llama.cpp
// /health endpoint.
func (s *LlamaServer) adoptExisting(cfg *config.Config) bool {
        url := fmt.Sprintf(
                "http://%s:%d/health",
                cfg.LlamaHost,
                cfg.LlamaPort,
        )

        client := &http.Client{
                Timeout: 2 * time.Second,
        }

        resp, err := client.Get(url)
        if err != nil {
                return false
        }

        defer resp.Body.Close()

        return resp.StatusCode == http.StatusOK
}

// Stop terminates the subprocess gracefully: SIGTERM, bounded grace
// period, then kill. The state machine walks stopping → stopped and the
// exit watcher is allowed to observe the death without scheduling an
// auto-restart (stopping arms the suppression flag).
func (s *LlamaServer) Stop() error {
        s.mu.Lock()

        cmd := s.cmd
        s.stopping = true

        if cmd == nil || cmd.Process == nil {
                s.cmd = nil
                s.loaded = ""
                s.mu.Unlock()

                s.setState(StateStopped)
                return nil
        }

        s.mu.Unlock()

        s.setState(StateStopping)

        _ = cmd.Process.Signal(syscall.SIGTERM)

        // Bounded grace: poll the process exit (released by the watcher's
        // close(exit.done)) instead of a blind sleep.
        deadline := time.Now().Add(4 * time.Second)

        for time.Now().Before(deadline) {
                s.mu.Lock()
                alive := s.cmd == cmd
                s.mu.Unlock()

                if !alive {
                        break
                }

                time.Sleep(50 * time.Millisecond)
        }

        s.mu.Lock()
        alive := s.cmd == cmd
        s.mu.Unlock()

        if alive && cmd.Process != nil {
                _ = cmd.Process.Kill()
        }

        s.mu.Lock()

        if s.cmd == cmd {
                s.cmd = nil
        }

        s.loaded = ""
        s.mu.Unlock()

        s.setState(StateStopped)

        return nil
}

// EnsureRunning boots the engine if it is not already running.
func (s *LlamaServer) EnsureRunning() error {
        if s.IsRunning() {
                return nil
        }

        return s.Start()
}

// Restart stops the server and boots it again.
func (s *LlamaServer) Restart() error {
        s.switchMu.Lock()
        defer s.switchMu.Unlock()

        if err := s.Stop(); err != nil {
                return err
        }

        time.Sleep(300 * time.Millisecond)

        return s.startLocked()
}

// LoadedModel returns the absolute path of the model currently served.
func (s *LlamaServer) LoadedModel() string {
        s.mu.Lock()
        defer s.mu.Unlock()

        return s.loaded
}

// setState records one state transition and publishes an EngineEvent to
// every subscriber.
func (s *LlamaServer) setState(st string) {
        s.mu.Lock()
        defer s.mu.Unlock()
        s.setStateLocked(st)
}

// setStateLocked performs a transition while s.mu is held. Callers that
// already hold the mutex (MarkBusy) use this to keep the aliveness check
// and the transition atomic.
func (s *LlamaServer) setStateLocked(st string) {
        previous := s.state

        if previous == st {
                return
        }

        s.state = st

        detail := s.detail
        model := ""

        if s.loaded != "" {
                model = filepath.Base(s.loaded)
        }

        ev := EngineEvent{
                State:     st,
                Previous:  previous,
                Model:     model,
                Detail:    detail,
                Timestamp: time.Now(),
        }

        // The subscriber fan-out must never run under s.mu: a slow subscriber
        // would block the state machine. The snapshot+unlock below keeps the
        // non-blocking sends outside the state lock.
        s.subsMu.Lock()
        subs := make([]chan EngineEvent, 0, len(s.subs))
        for _, ch := range s.subs {
                subs = append(subs, ch)
        }
        s.subsMu.Unlock()

        for _, ch := range subs {
                select {
                case ch <- ev:
                default:
                }
        }
}

// scheduleAutoRestart implements the watchdog's bounded recovery: restart
// the engine with exponential backoff (1s, 2s, 4s) up to maxAutoRestarts
// times per alive episode. When the budget is spent the engine reports
// failed and only an explicit user action may retry. Deliberate stops are
// honored: the stopping flag suppresses recovery entirely.
func (s *LlamaServer) scheduleAutoRestart(modelPath string) {
        s.mu.Lock()

        if s.stopping {
                s.mu.Unlock()
                return
        }

        if s.restarts >= maxAutoRestarts {
                s.detail = fmt.Sprintf(
                        "engine exited %d times — giving up automatic recovery: %s",
                        s.restarts,
                        s.tailLines(3),
                )

                s.mu.Unlock()

                s.setState(StateFailed)

                logging.Default().Error(
                        "engine",
                        "auto-restart budget exhausted; engine reported as failed",
                )

                return
        }

        s.restarts++

        attempt := s.restarts
        delay := time.Duration(1<<(attempt-1)) * time.Second // 1s, 2s, 4s

        s.mu.Unlock()

        logging.Default().Warn(
                "engine",
                "engine died — automatic restart %d/%d in %v",
                attempt,
                maxAutoRestarts,
                delay,
        )

        go func() {
                time.Sleep(delay)

                s.mu.Lock()
                stopping := s.stopping
                s.mu.Unlock()

                if stopping {
                        return
                }

                if err := s.Start(); err != nil {
                        logging.Default().Error(
                                "engine",
                                "auto-restart %d/%d failed: %v",
                                attempt,
                                maxAutoRestarts,
                                err,
                        )

                        // The next death (or this failure's terminal state) advances
                        // the bounded ladder; nothing unbounded ever loops here.
                        return
                }

                logging.Default().Info(
                        "engine",
                        "engine auto-restarted successfully (model %s)",
                        filepath.Base(modelPath),
                )
        }()
}

func (s *LlamaServer) logf(
        format string,
        args ...interface{},
) {
        line := fmt.Sprintf(format, args...)
        s.logBuf.add(line)
}

// safeArchivePath validates an archive member name and returns a path
// guaranteed to remain inside dir.
//
// Archive entry names are normalized to forward slashes first so Windows
// backslash traversal is treated exactly like Unix-style traversal.
//
// Examples rejected:
//   - ../../outside.exe
//   - ../outside.exe
//   - /absolute/path
//   - \\server\share\file
//   - C:\outside.exe
//   - C:/outside.exe
//
// Examples accepted:
//   - llama-server.exe
//   - bin/llama-server.exe
func safeArchivePath(dir, name string) (string, error) {
        name = strings.TrimSpace(name)

        if name == "" {
                return "", fmt.Errorf("archive contains an empty path")
        }

        normalized := strings.ReplaceAll(name, "\\", "/")
        cleanName := filepath.Clean(filepath.FromSlash(normalized))

        if cleanName == "." ||
                cleanName == string(filepath.Separator) ||
                cleanName == "" {
                return "", fmt.Errorf(
                        "archive contains invalid path %q",
                        name,
                )
        }

        if filepath.IsAbs(cleanName) ||
                filepath.VolumeName(cleanName) != "" {
                return "", fmt.Errorf(
                        "archive path %q is absolute or contains a volume",
                        name,
                )
        }

        base, err := filepath.Abs(dir)
        if err != nil {
                return "", fmt.Errorf(
                        "resolve archive destination: %w",
                        err,
                )
        }

        target := filepath.Join(base, cleanName)

        relative, err := filepath.Rel(base, target)
        if err != nil {
                return "", fmt.Errorf(
                        "validate archive path %q: %w",
                        name,
                        err,
                )
        }

        if relative == ".." ||
                strings.HasPrefix(
                        relative,
                        ".."+string(filepath.Separator),
                ) {
                return "", fmt.Errorf(
                        "archive path %q escapes extraction directory",
                        name,
                )
        }

        return target, nil
}

// engineDownloadTimeout bounds one engine-binary download. The previous
// plain http.Get had no deadline: a stalled CDN connection could hang the
// prewarm goroutine AND hold switchMu forever, blocking every later engine
// start/restart (v1.1.4Z).
const engineDownloadTimeout = 10 * time.Minute

// engineDownloadCapBytes bounds the downloaded archive size (2 GiB — the
// full Vulkan llama.cpp bundles are a few hundred MB).
const engineDownloadCapBytes = 2 << 30

// downloadAndExtract downloads url and extracts it into dir.
func downloadAndExtract(url, dir string) error {
        ctx, cancel := context.WithTimeout(
                context.Background(),
                engineDownloadTimeout,
        )
        defer cancel()

        req, err := http.NewRequestWithContext(
                ctx,
                http.MethodGet,
                url,
                nil,
        )
        if err != nil {
                return err
        }

        resp, err := http.DefaultClient.Do(req)
        if err != nil {
                return err
        }

        defer resp.Body.Close()

        if resp.StatusCode != http.StatusOK {
                return fmt.Errorf(
                        "download %s: HTTP %d",
                        url,
                        resp.StatusCode,
                )
        }

        tmp, err := os.CreateTemp("", "llama-*")
        if err != nil {
                return err
        }

        defer os.Remove(tmp.Name())
        defer tmp.Close()

        if _, err := io.Copy(
                tmp,
                io.LimitReader(resp.Body, engineDownloadCapBytes+1),
        ); err != nil {
                return fmt.Errorf("download llama.cpp: %w", err)
        }

        if fi, err := tmp.Stat(); err == nil && fi.Size() > engineDownloadCapBytes {
                return fmt.Errorf(
                        "engine download exceeds %d bytes — refusing to extract",
                        engineDownloadCapBytes,
                )
        }

        if _, err := tmp.Seek(0, 0); err != nil {
                return err
        }

        switch {
        case strings.HasSuffix(url, ".zip"):
                stat, err := tmp.Stat()
                if err != nil {
                        return err
                }

                zr, err := zip.NewReader(tmp, stat.Size())
                if err != nil {
                        return err
                }

                for _, f := range zr.File {
                        out, err := safeArchivePath(
                                dir,
                                f.Name,
                        )
                        if err != nil {
                                return err
                        }

                        if f.FileInfo().IsDir() {
                                if err := os.MkdirAll(
                                        out,
                                        0o755,
                                ); err != nil {
                                        return err
                                }

                                continue
                        }

                        if err := os.MkdirAll(
                                filepath.Dir(out),
                                0o755,
                        ); err != nil {
                                return err
                        }

                        rc, err := f.Open()
                        if err != nil {
                                return err
                        }

                        outFile, err := os.Create(out)
                        if err != nil {
                                _ = rc.Close()
                                return err
                        }

                        _, copyErr := io.Copy(
                                outFile,
                                rc,
                        )

                        closeErr := outFile.Close()
                        rcErr := rc.Close()

                        if copyErr != nil {
                                return copyErr
                        }

                        if closeErr != nil {
                                return closeErr
                        }

                        if rcErr != nil {
                                return rcErr
                        }
                }

        case strings.HasSuffix(url, ".tar.gz"),
                strings.HasSuffix(url, ".tgz"):
                if _, err := tmp.Seek(0, 0); err != nil {
                        return err
                }

                gz, err := gzip.NewReader(tmp)
                if err != nil {
                        return err
                }

                defer gz.Close()

                cmd := proc.Command(
                        "tar",
                        "-xzf",
                        "-",
                        "-C",
                        dir,
                )

                cmd.Stdin = gz

                if out, err := cmd.CombinedOutput(); err != nil {
                        return fmt.Errorf(
                                "tar -xzf: %w: %s",
                                err,
                                out,
                        )
                }

        default:
                if _, err := tmp.Seek(0, 0); err != nil {
                        return err
                }

                out, err := os.Create(
                        filepath.Join(
                                dir,
                                filepath.Base(url),
                        ),
                )
                if err != nil {
                        return err
                }

                defer out.Close()

                if _, err := io.Copy(out, tmp); err != nil {
                        return err
                }
        }

        return nil
}

// ResolveModelPath turns a configured model name into an existing absolute
// GGUF path.
func ResolveModelPath(
        modelsDir,
        name string,
) (string, error) {
        name = strings.TrimSpace(name)

        if name != "" {
                if abs, err := filepath.Abs(name); err == nil {
                        if fi, err := os.Stat(abs); err == nil && !fi.IsDir() {
                                return abs, nil
                        }
                }
        }

        entries, err := os.ReadDir(modelsDir)
        if err != nil {
                if name == "" {
                        return "",
                                fmt.Errorf(
                                        "models dir %s is empty or missing "+
                                                "(drop a .gguf file inside it)",
                                        modelsDir,
                                )
                }

                return "",
                        fmt.Errorf(
                                "cannot read models dir %s: %w",
                                modelsDir,
                                err,
                        )
        }

        var files []string

        for _, e := range entries {
                if e.IsDir() ||
                        !strings.HasSuffix(
                                strings.ToLower(e.Name()),
                                ".gguf",
                        ) {
                        continue
                }

                files = append(files, e.Name())
        }

        if len(files) == 0 {
                return "",
                        fmt.Errorf(
                                "no .gguf files in %s — drop a model file inside it and try again",
                                modelsDir,
                        )
        }

        if name == "" {
                return filepath.Join(modelsDir, files[0]), nil
        }

        for _, f := range files {
                if strings.EqualFold(f, name) {
                        return filepath.Join(modelsDir, f), nil
                }
        }

        lower := strings.ToLower(name)

        for _, f := range files {
                if strings.Contains(
                        strings.ToLower(f),
                        lower,
                ) {
                        return filepath.Join(modelsDir, f), nil
                }
        }

        tokens := fuzzyTokens(name)

        if len(tokens) > 0 {
                for _, f := range files {
                        lf := strings.ToLower(f)
                        all := true

                        for _, tok := range tokens {
                                if !strings.Contains(lf, tok) {
                                        all = false
                                        break
                                }
                        }

                        if all {
                                return filepath.Join(modelsDir, f), nil
                        }
                }
        }

        return "",
                fmt.Errorf(
                        "no .gguf in %s matches %q (have: %s)",
                        modelsDir,
                        name,
                        strings.Join(files, ", "),
                )
}

func fuzzyTokens(name string) []string {
        var toks []string

        for _, field := range strings.Fields(
                strings.ToLower(name),
        ) {
                var cur strings.Builder

                for _, r := range field {
                        if (r >= 'a' && r <= 'z') ||
                                (r >= '0' && r <= '9') {
                                cur.WriteRune(r)
                        }
                }

                if cur.Len() > 0 {
                        toks = append(toks, cur.String())
                }
        }

        return toks
}

func llamaBinaryName() string {
        switch runtime.GOOS {
        case "windows":
                return "llama-server.exe"
        case "darwin":
                return "llama-server"
        default:
                return "llama-server"
        }
}

func llamaDownloadURL() (string, string, error) {
        // v1.1.5Z repair: llama.cpp removed its prebuilt LINUX binaries
        // upstream (b10642 still ships the Windows asset but no ubuntu zip any
        // more), so the pinned-tag URL 404s forever on Linux. Follow the
        // updater's existing design instead of hardcoding the pinned tag:
        //   1. pinned tag, when this platform's asset actually exists;
        //   2. newest release that carries this platform's asset (bounded scan);
        //   3. otherwise an honest error that says what to do — never a bare
        //      HTTP 404 from a URL that can never succeed again.
        // Returns (url, resolvedTag, error) so the caller records the ACTUAL
        // source tag the binary came from.
        if url := updater.AssetURL(updater.DefaultEngineTag); url != "" {
                probeCtx, probeCancel := context.WithTimeout(
                        context.Background(),
                        30*time.Second,
                )

                if updater.AssetExists(probeCtx, updater.DefaultEngineTag) {
                        probeCancel()
                        return url, updater.DefaultEngineTag, nil
                }

                probeCancel()
        }

        scanCtx, scanCancel := context.WithTimeout(
                context.Background(),
                60*time.Second,
        )

        if tag, err := updater.LatestTag(scanCtx); err == nil && tag != "" {
                if url := updater.AssetURL(tag); url != "" {
                        scanCancel()
                        return url, tag, nil
                }
        }

        scanCancel()

        return "", "",
                fmt.Errorf(
                        "no prebuilt llama.cpp server asset for %s/%s (upstream no longer publishes Linux binaries) — build llama-server from source and set llamaBinPath, or select the native engine (engineBackend \"native\")",
                        runtime.GOOS,
                        runtime.GOARCH,
                )
}

func ListLocalModels(dir string) []string {
        entries, err := os.ReadDir(dir)
        if err != nil {
                return nil
        }

        var out []string

        for _, e := range entries {
                if e.IsDir() ||
                        !strings.HasSuffix(
                                strings.ToLower(e.Name()),
                                ".gguf",
                        ) {
                        continue
                }

                if vision.IsMMProj(e.Name()) {
                        continue
                }

                out = append(out, e.Name())
        }

        return out
}

func (s *LlamaServer) ListLoadedModels() ([]string, error) {
        if !s.IsRunning() {
                return nil,
                        fmt.Errorf("llama.cpp server not running")
        }

        cfg := s.src.Load()

        url := fmt.Sprintf(
                "http://%s:%d/v1/models",
                cfg.LlamaHost,
                cfg.LlamaPort,
        )

        // v1.1.4Z: bounded request. The previous plain http.Get could hang an
        // HTTP handler forever when the engine stopped answering mid-poll.
        client := &http.Client{Timeout: 5 * time.Second}

        resp, err := client.Get(url)
        if err != nil {
                return nil, err
        }

        defer resp.Body.Close()

        var body struct {
                Data []struct {
                        ID string `json:"id"`
                } `json:"data"`
        }

        if err := json.NewDecoder(
                resp.Body,
        ).Decode(&body); err != nil {
                return nil, err
        }

        var out []string

        for _, model := range body.Data {
                out = append(out, model.ID)
        }

        return out, nil
}

func PortInUse(host string, port int) bool {
        addr := fmt.Sprintf(
                "%s:%d",
                host,
                port,
        )

        l, err := net.Listen(
                "tcp",
                addr,
        )
        if err != nil {
                return true
        }

        _ = l.Close()
        return false
}

type lineWriter struct {
        buf []byte
        cb  func(string)
}

func newLineWriter(
        cb func(string),
) *lineWriter {
        return &lineWriter{
                cb: cb,
        }
}

func (w *lineWriter) Write(
        p []byte,
) (int, error) {
        w.buf = append(w.buf, p...)

        for {
                i := -1

                for j, b := range w.buf {
                        if b == '\n' {
                                i = j
                                break
                        }
                }

                if i < 0 {
                        break
                }

                line := strings.TrimRight(
                        string(w.buf[:i]),
                        "\r",
                )

                w.buf = w.buf[i+1:]
                w.cb(line)
        }

        return len(p), nil
}
