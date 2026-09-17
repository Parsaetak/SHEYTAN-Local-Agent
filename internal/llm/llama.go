package llm

import (
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
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
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/downloader"
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
//	idle        fresh manager, nothing attempted yet
//	downloading engine binary is being fetched
//	starting    subprocess spawned, model loading (health pending)
//	ready       healthy: /health 200, model loaded, no inference in flight
//	running     subprocess alive and serving (superset of ready/busy)
//	busy        inference request in flight
//	stopping    deliberate shutdown in progress
//	stopped     terminated deliberately (or died without auto-restart armed)
//	failed      terminal failure after bounded retries
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
	// Download carries live asset-download progress (phase, bytes,
	// speed, ETA, source, verification) while the state is
	// "downloading" — the v1.2.3 Download Manager surfaces REAL
	// measured progress instead of a bare state label. Nil outside
	// downloads.
	Download *downloader.Progress `json:"download,omitempty"`
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

	// gen is the lifecycle episode counter (v1.2.5). Incremented on every
	// successful boot; a watchdog armed for episode N may only act while
	// gen == N. This is what makes a stale restart from an OLDER engine
	// episode unable to resurrect an engine after a newer lifecycle has
	// started or stopped.
	gen uint64

	// watchdog ownership (v1.2.5). Exactly one pending restart can be
	// armed at a time; Stop() cancels it and waits for the goroutine to
	// exit, so after Stop() completes no delayed watchdog can call Start().
	// Guarded by mu. watchStop is closed to cancel; watchDone is closed by
	// the watchdog goroutine on exit.
	watchArmed bool
	watchStop  chan struct{}
	watchDone  chan struct{}

	// startedAt is the time of the last successful boot (v1.1.5Z: the
	// measured uptime base for backend Metrics).
	startedAt time.Time

	// stopping arms the deliberate-shutdown flag so the exit watcher's
	// auto-restart is suppressed while Stop() runs.
	stopping bool

	// exitDone is the CURRENT process's watcher-done channel: closed by
	// the watcher goroutine after cmd.Wait() has returned. Stop() waits
	// on it (bounded) so a killed child is fully reaped — and on Windows
	// its executable unlocked — before Stop reports completion.
	// Guarded by mu. Nil when no process is owned.
	exitDone chan struct{}

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

	// dlJob / dlProgress expose the v1.2.3 Download Manager job while
	// an engine asset (llama.cpp archive, future model packages) is
	// being fetched: measured progress for the UI and a cancellation
	// handle. dlProgress is nil outside downloads.
	dlJob      atomic.Pointer[downloader.Job]
	dlProgress atomic.Pointer[downloader.Progress]
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

	// v1.2.3: the fetch runs through the reusable Download Manager —
	// streamed to disk, resume-capable, verified, retried with
	// bounded backoff, observable (progress in engine events) and
	// cancellable. Extraction happens only from the VERIFIED archive.
	if err := s.downloadEngineArchive(url, dir); err != nil {
		if errors.Is(err, downloader.ErrCancelled) {
			return "", fmt.Errorf("engine download cancelled")
		}
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
					episodeOK := s.beginEpisodeLocked()
					s.mu.Unlock()

					if !episodeOK {
						// A deliberate Stop() raced the boot: the shutdown
						// owns the lifecycle — kill the fresh child.
						s.abortFreshBoot()

						return fmt.Errorf(
							"engine start aborted: shutdown in progress",
						)
					}

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
					episodeOK := s.beginEpisodeLocked()
					s.mu.Unlock()

					if !episodeOK {
						// A deliberate Stop() raced the boot: the shutdown
						// owns the lifecycle — kill the fresh child.
						s.abortFreshBoot()

						return fmt.Errorf(
							"engine start aborted: shutdown in progress",
						)
					}

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
//	0 — everything: Speed Pack flags, GPU offload, user extra args
//	1 — everything + --jinja
//	2 — --jinja + GPU, but no speed flags
//	3 — bare: model, host/port, context, threads
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

	exit := &procExit{
		done: make(chan struct{}),
	}

	s.mu.Lock()
	s.cmd = cmd
	s.exitDone = exit.done
	s.mu.Unlock()

	go func() {
		exit.err = cmd.Wait()

		// v1.2.5 exit classification: the watcher snapshots whether
		// the shutdown was DELIBERATE (Stop() armed s.stopping before
		// this classification ran) in the SAME critical section that
		// computes liveness. Intent is never inferred after the fact:
		// a deliberate stop must NEVER be logged as a crash, counted
		// as an unexpected restart, or reported as an error.
		s.mu.Lock()

		latest := s.cmd == cmd

		if latest {
			s.cmd = nil
			s.exitDone = nil
		}

		deliberate := s.stopping
		wasAlive :=
			latest &&
				s.loaded == modelPath &&
				aliveStates[s.state]

		if wasAlive {
			s.loaded = ""
		}

		// v1.2.5 diagnostics snapshot: everything needed to diagnose
		// a REAL death, captured atomically with the classification.
		diag := exitDiagnostics{
			model:       filepath.Base(modelPath),
			state:       s.state,
			stopping:    deliberate,
			visionState: string(s.visionState),
			visionMsg:   s.visionReason,
		}

		if cmd.Process != nil {
			diag.pid = cmd.Process.Pid
		}

		if s.caps != nil {
			diag.tag = s.caps.Tag
		}

		diag.stderrTail = s.errRing.lines()

		s.mu.Unlock()

		diag.exit = exitDescription(exit.err)

		switch {
		case wasAlive && deliberate:
			// Deliberate shutdown: the normal stopped transition.
			// No ERROR, no restart, no restart-count increment.
			// Stop() drives the state machine to stopped; this
			// only makes the transition honest in the log.
			logging.Default().Info(
				"engine",
				"engine stopped (deliberate shutdown, %s)",
				diag.exit,
			)
		case wasAlive:
			// Unexpected process death: ERROR + bounded recovery.
			s.setState(StateStopped)

			// v1.2.5 honest detail for /api/engine and the UI.
			s.SetDetail("Unexpected exit: " + diag.exit)

			logging.Default().Error(
				"engine",
				"engine exited unexpectedly: %s\n%s",
				diag.exit,
				diag.render(s.logBuf.lines()),
			)

			// v1.1.3Z bounded auto-recovery, v1.2.5 lifecycle-
			// owned: the watchdog restarts the engine a bounded
			// number of times with backoff so a crashed engine
			// recovers transparently, while a fundamentally
			// broken setup surfaces as `failed` instead of
			// looping forever.
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
			s.exitDone = nil
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

	if _, err := updater.UpdateEngineWithProgress(
		ctx,
		cfg,
		nil,
		latest,
		s.publishDownloadProgress,
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
// period, then kill — followed by a BOUNDED wait until the exit watcher
// reports cmd.Wait() has returned.
//
// v1.2.0 (Windows): Kill() only REQUESTS termination; the process object
// (and the lock the dying process holds on its executable) survives until
// Wait() reaps it. Stop() previously returned right after Kill, so the
// fake-engine test binary could still be alive when `go test` tried to
// remove or rewrite it — the observed "unlinkat llm.test.exe: Access is
// denied" plus orphan llm.test/conhost processes. Deterministic ownership
// means Stop does not return until the child is reaped or the bounded
// wait is exhausted.
func (s *LlamaServer) Stop() error {
	s.mu.Lock()

	cmd := s.cmd
	done := s.exitDone
	s.stopping = true

	// v1.2.5: a deliberate stop cancels any pending watchdog restart and
	// waits (bounded) for that goroutine to exit. After Stop returns, no
	// delayed watchdog can call Start().
	s.watchArmed = false
	watchStop, watchDone := s.watchStop, s.watchDone
	s.watchStop = nil
	s.watchDone = nil

	if cmd == nil || cmd.Process == nil {
		s.cmd = nil
		s.exitDone = nil
		s.loaded = ""
		s.mu.Unlock()

		// v1.2.5: even with no live process, a watchdog armed by an
		// earlier unexpected death must be canceled — this stop is
		// deliberate and owns the lifecycle now.
		if watchStop != nil {
			close(watchStop)
		}

		if watchDone != nil {
			select {
			case <-watchDone:
			case <-time.After(2 * time.Second):
			}
		}

		s.setState(StateStopped)
		return nil
	}

	s.mu.Unlock()

	s.setState(StateStopping)

	_ = cmd.Process.Signal(syscall.SIGTERM)

	// Bounded grace: wait for the watcher's close(exit.done) — a real
	// signal of Wait() completion — instead of blind-polling process
	// identity. The 50ms fallback poll covers a watcher that has not
	// observed the exit yet.
	deadline := time.Now().Add(4 * time.Second)

	for time.Now().Before(deadline) {
		select {
		case <-done:
			deadline = time.Time{} // already reaped
		default:
		}

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

	// Reap deterministically: after Kill, the child must be observed
	// exiting before this call returns (bounded — a wedged child must
	// not hang the caller forever, but the OS gets a fair window to
	// release the executable).
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}

	s.mu.Lock()

	if s.cmd == cmd {
		s.cmd = nil
		s.exitDone = nil
	}

	s.loaded = ""
	s.mu.Unlock()

	// v1.2.5: cancel the pending watchdog (if any) and wait for its
	// goroutine to exit — a deliberate shutdown never leaves a delayed
	// restart behind. After the child is reaped, the exit watcher has
	// already observed stopping=true, so it classified this exit as
	// deliberate (no ERROR, no restart) — the cancel below only covers
	// a watchdog armed BEFORE this Stop began.
	if watchStop != nil {
		close(watchStop)
	}

	if watchDone != nil {
		select {
		case <-watchDone:
		case <-time.After(2 * time.Second):
		}
	}

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
	// Live download progress rides along with "downloading"
	// transitions; leaving the state clears it so the UI never shows
	// stale progress.
	if st == StateDownloading {
		ev.Download = s.dlProgress.Load()
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

// exitDiagnostics is the v1.2.5 evidence bundle captured when the engine
// dies unexpectedly: enough to diagnose the death without a second run.
type exitDiagnostics struct {
	exit        string
	pid         int
	tag         string
	model       string
	state       string
	stopping    bool
	visionState string
	visionMsg   string
	stderrTail  []string
}

// render formats the diagnostics block appended to the unexpected-exit log.
func (d exitDiagnostics) render(stdoutTail []string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "  pid=%d tag=%s model=%s state-before=%s busy=%v stopRequested=%v",
		d.pid, orUnknown(d.tag), d.model, d.state, d.state == StateBusy, d.stopping)

	fmt.Fprintf(&b, "\n  vision=%s", orUnknown(d.visionState))

	if d.visionMsg != "" {
		fmt.Fprintf(&b, " (%s)", d.visionMsg)
	}

	if len(d.stderrTail) > 0 {
		fmt.Fprintf(&b, "\n  stderr tail:\n    %s",
			strings.Join(d.stderrTail, "\n    "))
	}

	if len(stdoutTail) > 0 {
		tail := stdoutTail

		if len(tail) > 8 {
			tail = tail[len(tail)-8:]
		}

		fmt.Fprintf(&b, "\n  stdout tail:\n    %s",
			strings.Join(tail, "\n    "))
	}

	return b.String()
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}

	return s
}

// exitDescription renders an exec error as human evidence: the exit code
// or the terminating signal (unix), never a bare guess. Windows reports
// exit codes only (see exitdetail_windows.go).
func exitDescription(err error) string {
	if err == nil {
		return "clean exit (code 0)"
	}

	var exitErr *exec.ExitError

	if errors.As(err, &exitErr) {
		if code := exitErr.ExitCode(); code >= 0 {
			return fmt.Sprintf("code %d", code)
		}

		if sig := exitSignalPart(err); sig != "" {
			return sig
		}
	}

	return err.Error()
}

// restartBudgetResetAfter is the stability window a boot must reach before
// it counts as a genuinely healthy episode for the watchdog budget. A
// crash-loop (boot → ready → die within seconds) NEVER resets the counter,
// so repeated crashes exhaust the bounded budget and surface as `failed`
// instead of recovering forever. Variable, not const: tests shrink it.
var restartBudgetResetAfter = 60 * time.Second

// beginEpisodeLocked starts a fresh alive episode: a new generation token,
// an armed-stop reset and a cleared detail. The restart budget resets ONLY
// when the previous episode was genuinely stable — a short-lived boot that
// immediately died again keeps its place on the bounded ladder. Callers
// hold s.mu and invoke this ONLY after a verified successful boot.
//
// Returns false when a deliberate Stop() raced the boot: the caller must
// NOT open a new episode and must kill the fresh child instead (the
// shutdown owns the lifecycle).
func (s *LlamaServer) beginEpisodeLocked() bool {
	if s.stopping {
		return false
	}

	s.gen++

	if s.startedAt.IsZero() || time.Since(s.startedAt) >= restartBudgetResetAfter {
		s.restarts = 0
	}

	s.stopping = false
	s.detail = ""
	s.startedAt = time.Now()

	return true
}

// abortFreshBoot kills a child that became healthy while a deliberate Stop()
// raced the boot. Deterministic: the child is reaped (bounded) and the state
// machine lands on stopped — a shutdown can never be overwritten by a boot
// that lost the race.
func (s *LlamaServer) abortFreshBoot() {
	s.mu.Lock()
	c := s.cmd
	d := s.exitDone
	s.cmd = nil
	s.exitDone = nil
	s.loaded = ""
	s.mu.Unlock()

	if c != nil && c.Process != nil {
		_ = c.Process.Kill()
	}

	if d != nil {
		select {
		case <-d:
		case <-time.After(3 * time.Second):
		}
	}

	s.setState(StateStopped)
}

// cancelWatchdog cancels any pending watchdog restart and waits (bounded)
// for its goroutine to exit. Called from Stop(): after Stop returns, no
// delayed watchdog can later call Start(). Safe to call any time.
func (s *LlamaServer) cancelWatchdog() {
	s.mu.Lock()
	stopCh, doneCh := s.watchStop, s.watchDone
	s.watchArmed = false
	s.watchStop = nil
	s.watchDone = nil
	s.mu.Unlock()

	if stopCh != nil {
		close(stopCh)
	}

	if doneCh != nil {
		select {
		case <-doneCh:
		case <-time.After(2 * time.Second):
			// The watchdog is mid-Start(); the boot itself honors
			// the stopping flag at its ladder checkpoints, so no
			// engine can survive a completed shutdown because of
			// this.
		}
	}
}

// scheduleAutoRestart implements the watchdog's bounded recovery: restart
// the engine with exponential backoff (1s, 2s, 4s) up to maxAutoRestarts
// times per alive episode. When the budget is spent the engine reports
// failed and only an explicit user action may retry. Deliberate stops are
// honored: the stopping flag suppresses recovery entirely.
//
// v1.2.5 lifecycle ownership: the pending restart is an OWNED, CANCELABLE
// timer (no naked time.Sleep), armed for one specific generation; only one
// restart can be armed at a time; Stop() cancels and reaps it; a stale
// watchdog from an older episode re-validates its generation before acting
// and can never resurrect a newer lifecycle.
func (s *LlamaServer) scheduleAutoRestart(modelPath string) {
	s.mu.Lock()

	if s.stopping {
		s.mu.Unlock()
		return
	}

	if s.watchArmed {
		// Only one restart attempt can be scheduled at a time.
		s.mu.Unlock()
		return
	}

	if s.restarts >= maxAutoRestarts {
		s.detail = fmt.Sprintf(
			"Unexpected exit: engine failed %d times — automatic recovery exhausted: %s",
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
	gen := s.gen

	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	s.watchArmed = true
	s.watchStop = stopCh
	s.watchDone = doneCh

	s.mu.Unlock()

	logging.Default().Warn(
		"engine",
		"engine died — automatic restart %d/%d in %v",
		attempt,
		maxAutoRestarts,
		delay,
	)

	s.SetDetail(fmt.Sprintf(
		"Unexpected exit — restarting %d/%d in %v",
		attempt, maxAutoRestarts, delay,
	))

	go func() {
		defer close(doneCh)

		timer := time.NewTimer(delay)
		defer timer.Stop()

		select {
		case <-timer.C:
		case <-stopCh:
			// Canceled: deliberate shutdown (or an explicit
			// teardown) while the restart was pending. No restart.
			return
		}

		s.mu.Lock()
		stale := s.stopping || s.gen != gen
		s.watchArmed = false
		s.watchStop = nil
		s.watchDone = nil
		s.mu.Unlock()

		if stale {
			// A deliberate stop happened during the backoff, or a
			// newer engine episode started: this watchdog is
			// history and must not resurrect anything.
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

// safeArchivePath validates an archive member name and returns the
// absolute extraction target.
//
// v1.2.0: this is a DELEGATION to the single authoritative validator
// (updater.safeZipPath, exported as updater.SafeArchivePath). Keeping two
// independent implementations invited exactly the kind of drift the
// zip-slip hardening was meant to prevent; the contract now lives in one
// place and covers POSIX-absolute, Windows-rooted, drive-letter, UNC and
// mixed-separator members identically on every host.
func safeArchivePath(dir, name string) (string, error) {
	return updater.SafeArchivePath(dir, name)
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
// downloadEngineArchive fetches the llama.cpp server archive through the
// reusable Download Manager (v1.2.3) and extracts it into dir.
//
// The archive is streamed to "<dir>/.engine-download/<name>.part",
// verified, atomically renamed, and only then extracted — a partially
// downloaded or truncated archive can never reach extraction. Interrupted
// transfers resume via HTTP Range when the release server supports it,
// and the last working source is remembered so a retry does not re-probe
// every endpoint.
func (s *LlamaServer) downloadEngineArchive(url, dir string) error {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		engineDownloadTimeout,
	)
	defer cancel()

	stage := filepath.Join(dir, ".engine-download")
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return err
	}

	dest := filepath.Join(stage, "llama-server"+engineArchiveSuffix(url))

	opts := downloader.Options{
		Dest: dest,
		Sources: []downloader.Source{{
			URL:   url,
			Label: "llama.cpp release (github.com/ggml-org/llama.cpp)",
			Trust: downloader.TrustPrimary,
		}},
		MaxBytes:   engineDownloadCapBytes,
		FileMode:   0o644,
		Resume:     true,
		CacheKey:   "llama-engine-" + runtime.GOOS + "-" + runtime.GOARCH,
		CacheDir:   stage,
		OnProgress: s.publishDownloadProgress,
	}

	job, err := downloader.New(opts)
	if err != nil {
		return err
	}
	s.dlJob.Store(job)
	defer func() {
		s.dlJob.Store(nil)
		s.dlProgress.Store(nil)
	}()

	res, err := job.Run(ctx)
	if err != nil {
		return err
	}

	s.logf(
		"llama.cpp archive verified (%d bytes, source %s, resumed %d)",
		res.Bytes, res.Source.Label, res.ResumedFrom,
	)

	if err := extractEngineArchive(res.Path, url, dir); err != nil {
		return err
	}

	// Extraction succeeded: the staged archive has served its purpose.
	// The source cache under the stage dir survives for the next retry.
	_ = os.Remove(dest)
	_ = os.Remove(dest + ".part")
	return nil
}

// publishDownloadProgress stores the latest measured progress and fans it
// out to engine-event subscribers (throttled upstream to ~4 Hz). It runs
// on the download goroutine; fan-out is non-blocking by contract.
func (s *LlamaServer) publishDownloadProgress(p downloader.Progress) {
	snapshot := p
	s.dlProgress.Store(&snapshot)

	ev := EngineEvent{
		State:     StateDownloading,
		Timestamp: time.Now(),
		Download:  &snapshot,
	}

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

// DownloadProgress returns the live engine-asset download progress, or
// nil when nothing is being downloaded.
func (s *LlamaServer) DownloadProgress() *downloader.Progress {
	return s.dlProgress.Load()
}

// CancelDownload aborts an in-flight engine asset download. Network and
// file activity stop immediately; the .part file is kept so a retry can
// resume instead of restarting from zero.
func (s *LlamaServer) CancelDownload() bool {
	job := s.dlJob.Load()
	if job == nil {
		return false
	}
	job.Cancel()
	return true
}

// engineArchiveSuffix maps a release URL to its archive suffix so the
// extraction format can be detected even when the staged file uses a
// neutral local name.
func engineArchiveSuffix(url string) string {
	switch {
	case strings.HasSuffix(url, ".zip"):
		return ".zip"
	case strings.HasSuffix(url, ".tar.gz"):
		return ".tar.gz"
	case strings.HasSuffix(url, ".tgz"):
		return ".tgz"
	default:
		return filepath.Ext(url)
	}
}

// extractEngineArchive unpacks a VERIFIED archive into dir. The format
// is chosen from the source URL suffix (the staged name is neutral).
// Zip members go through the shared zip-slip guard; tar.gz extraction is
// delegated to the system tar the same way the previous implementation
// did.
func extractEngineArchive(archivePath, url, dir string) error {
	switch engineArchiveSuffix(url) {
	case ".zip":
		f, err := os.Open(archivePath)
		if err != nil {
			return err
		}
		defer f.Close()

		stat, err := f.Stat()
		if err != nil {
			return err
		}

		zr, err := zip.NewReader(f, stat.Size())
		if err != nil {
			return err
		}

		for _, fm := range zr.File {
			out, err := safeArchivePath(
				dir,
				fm.Name,
			)
			if err != nil {
				return err
			}

			if fm.FileInfo().IsDir() {
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

			rc, err := fm.Open()
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

	case ".tar.gz", ".tgz":
		f, err := os.Open(archivePath)
		if err != nil {
			return err
		}
		defer f.Close()

		gz, err := gzip.NewReader(f)
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
		out, err := os.Create(
			filepath.Join(
				dir,
				filepath.Base(url),
			),
		)
		if err != nil {
			return err
		}

		f, err := os.Open(archivePath)
		if err != nil {
			out.Close()
			return err
		}
		defer f.Close()

		if _, err := io.Copy(out, f); err != nil {
			out.Close()
			return err
		}
		out.Close()
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

	tag, tagErr := updater.LatestTag(scanCtx)
	scanCancel()

	if tagErr == nil && tag != "" {
		if url := updater.AssetURL(tag); url != "" {
			return url, tag, nil
		}

		return "", "",
			fmt.Errorf(
				"no prebuilt llama.cpp server asset exists for %s/%s — this platform/architecture is not served by upstream releases; build llama-server from source and set llamaBinPath, or select the native engine (engineBackend \"native\")",
				runtime.GOOS,
				runtime.GOARCH,
			)
	}

	// v1.2.2: classify the failure HONESTLY. The old text always claimed
	// "no prebuilt asset … (upstream no longer publishes Linux binaries)"
	// — on a Windows machine whose api.github.com/Atom requests are
	// blocked, engine startup reported a wrong, misleading cause. Network
	// failure and asset absence are different problems with different
	// remedies, and neither may leave startup looking mysteriously broken.
	if updater.IsNoAssetError(tagErr) {
		return "", "",
			fmt.Errorf(
				"no recent llama.cpp release ships a prebuilt server asset for %s/%s — build llama-server from source and set llamaBinPath, or select the native engine (engineBackend \"native\")",
				runtime.GOOS,
				runtime.GOARCH,
			)
	}

	logging.Default().Warn(
		"engine",
		"engine binary missing and the llama.cpp release server could not be reached: %v",
		tagErr,
	)

	return "", "",
		fmt.Errorf(
			"the llama.cpp release server could not be reached (%v) — the engine binary is not installed on this machine and could not be downloaded. Reconnect and start the engine again, or place a prebuilt llama-server(.exe) into the bin folder, or select the native engine (engineBackend \"native\")",
			tagErr,
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
