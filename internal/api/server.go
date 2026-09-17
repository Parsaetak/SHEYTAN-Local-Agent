// Package api serves the embedded web UI and exposes the JSON REST+WS API
// that the UI talks to.
package api

import (
        "context"
        "encoding/json"
        "fmt"
        "io/fs"
        "net/http"
        "os"
        "path/filepath"
        "strconv"
        "strings"
        "sync"
        "sync/atomic"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/attachments"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/chunking"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/continuum"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/downloader"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/installer"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
        nativeengine "github.com/Parsaetak/SHEYTAN-local-agent/internal/native/engine"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/recall"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/runtime"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/sessions"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/sysinfo"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/updater"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/vision"
        "github.com/Parsaetak/SHEYTAN-local-agent/web"

        "github.com/gorilla/websocket"
)

// Server bundles everything the HTTP handlers need.
//
// The runtime stack is the single source of truth for the agent backend.
// This keeps the HTTP/API surface feature-identical with the CLI and desktop
// runtime instead of constructing a second independent orchestrator.
type Server struct {
        // src is the live configuration source shared with the runtime stack.
        // (v1.1.4Z: handlers snapshot it per request; the old shared mutable
        // *Config raced the patch handler against active runs.)
        src       *config.Source
        store     *sessions.Store
        stack     *runtime.Stack
        orch      *agent.Orchestrator
        llama     *llm.LlamaServer
        native    *nativeengine.Engine
        installer *installer.Manager
        sys       *sysinfo.SysInfo
        recall    *recall.Engine // v1.0.2 persistent memory over past chats

        // continuum evaluates chapter rollover after long sessions
        // (v1.1.4Z: the manager was fully implemented and tested but never
        // wired into any production path before).
        continuum *continuum.Manager

        // updateCancel stops the scheduled engine-update loop on Close
        // (v1.1.4Z: updater.RunScheduled existed with zero callers).
        // updateDone is RunScheduled's completion channel: closed once the
        // loop observed cancellation AND any in-flight pass finished —
        // Close waits on it (bounded) so the updater can never outlive the
        // server and write staging files behind its back.
        updateCancel context.CancelFunc
        updateDone   <-chan struct{}

        // lastAppUpdate caches the newest app-update check (v1.2.0) so
        // /api/update/status never blocks on the network. Written only by
        // /api/update/check and /api/update/download; read by status.
        lastAppUpdate atomic.Value

        // v1.2.3: the asynchronous app-update staging job (Download Manager).
        // appUpdateJob is the live handle for progress + cancel; appUpdateStaging
        // claims the single slot; appUpdateCancel stops the transfer goroutine
        // (also called from Close so staging never outlives the server).
        appUpdateJob     atomic.Pointer[downloader.Job]
        appUpdateStaging atomic.Bool
        appUpdateCancel  atomic.Pointer[context.CancelFunc]

        // active runs: sessionID → runState
        runsMu sync.Mutex
        runs   map[string]*runState

        // v1.1.2Z: idle WebSocket standby registry — sessionID → connections.
        // Before this, an activity WebSocket with no active run was closed
        // immediately after one "idle" sentinel, so the UI permanently showed
        // "Offline" between runs. Idle connections now stay open, and the
        // instant a run starts they are woken and attached to the run hub.
        // v1.1.3Z: each standby connection also owns an engineCh fed by the
        // engine event bus so idle UIs still observe engine transitions.
        standbyMu sync.Mutex
        standby   map[string][]*standbyConn

        // engineStop terminates the engine event fan-out on Close.
        engineStop chan struct{}
        engineDone chan struct{}
}

type runState struct {
        cancel context.CancelFunc
        hub    *activityHub
}

// activityHub broadcasts activity events to all WebSocket subscribers of a
// single active run. Each client receives its own buffered channel so clients
// do not consume events from one another.
type activityHub struct {
        mu      sync.RWMutex
        clients map[int]chan agent.Activity
        nextID  int
        closed  bool
}

func newActivityHub() *activityHub {
        return &activityHub{
                clients: make(map[int]chan agent.Activity),
        }
}

func (h *activityHub) subscribe() (int, <-chan agent.Activity, func()) {
        h.mu.Lock()
        defer h.mu.Unlock()

        if h.closed {
                ch := make(chan agent.Activity)
                close(ch)
                return 0, ch, func() {}
        }

        id := h.nextID
        h.nextID++

        ch := make(chan agent.Activity, 128)
        h.clients[id] = ch

        var once sync.Once

        unsubscribe := func() {
                once.Do(func() {
                        h.mu.Lock()
                        defer h.mu.Unlock()

                        if existing, ok := h.clients[id]; ok {
                                delete(h.clients, id)
                                close(existing)
                        }
                })
        }

        return id, ch, unsubscribe
}

func (h *activityHub) publish(ev agent.Activity) {
        h.mu.RLock()
        defer h.mu.RUnlock()

        for _, ch := range h.clients {
                select {
                case ch <- ev:
                default:
                        // A slow WebSocket must never block the agent run.
                        // Only that slow subscriber may lose an event.
                }
        }
}

func (h *activityHub) close() {
        h.mu.Lock()
        defer h.mu.Unlock()

        if h.closed {
                return
        }

        h.closed = true

        for id, ch := range h.clients {
                delete(h.clients, id)
                close(ch)
        }
}

// standbyConn carries the two channels one idle activity connection waits
// on: the run-start wake and pre-encoded engine event frames.
type standbyConn struct {
        wake     chan struct{}
        engineCh chan []byte
}

// enterStandby registers a standby connection for an idle activity
// connection. The wake channel is closed by wakeStandby the moment a run
// starts for the session; engineCh receives pre-encoded engine frames so
// idle UIs still observe engine transitions.
func (s *Server) enterStandby(sessionID string) *standbyConn {
        sc := &standbyConn{
                wake:     make(chan struct{}),
                engineCh: make(chan []byte, 8),
        }

        s.standbyMu.Lock()
        s.standby[sessionID] = append(s.standby[sessionID], sc)
        s.standbyMu.Unlock()

        return sc
}

// leaveStandby removes a standby connection that is no longer waiting (the
// client disconnected, or the connection was already woken).
func (s *Server) leaveStandby(sessionID string, sc *standbyConn) {
        s.standbyMu.Lock()
        defer s.standbyMu.Unlock()

        waiting := s.standby[sessionID]

        kept := waiting[:0]

        for _, w := range waiting {
                if w != sc {
                        kept = append(kept, w)
                }
        }

        if len(kept) == 0 {
                delete(s.standby, sessionID)
        } else {
                s.standby[sessionID] = kept
        }
}

// wakeStandby releases every idle connection waiting on a session so they
// can attach to the freshly registered run hub.
func (s *Server) wakeStandby(sessionID string) {
        s.standbyMu.Lock()
        waiting := s.standby[sessionID]
        delete(s.standby, sessionID)
        s.standbyMu.Unlock()

        for _, sc := range waiting {
                close(sc.wake)
        }
}

// New constructs a fully-wired server from the canonical runtime stack.
func New(cfg *config.Config) (*Server, error) {
        stack := runtime.NewStack(cfg)
        // v1.2.4: one shared session store (owned by the runtime stack) so the
        // memory manager can bound its hot cache and every consumer sees the
        // same warmed state.
        store := stack.Sessions

        s := &Server{
                src:        stack.Src,
                store:      store,
                stack:      stack,
                orch:       stack.Orch,
                llama:      stack.Llama,
                native:     stack.Native,
                installer:  installer.New(cfg),
                runs:       make(map[string]*runState),
                standby:    make(map[string][]*standbyConn),
                sys:        sysinfo.Probe(),
                recall:     stack.Recall,
                continuum:  continuum.NewManager(store, cfg.SessionsDir),
                engineStop: make(chan struct{}),
                engineDone: make(chan struct{}),
        }

        // v1.1.3Z: the engine event bus fans authoritative state transitions
        // to every WebSocket as they happen.
        go func() {
                defer close(s.engineDone)
                s.watchEngineEvents(s.engineStop)
        }()

        return s, nil
}

// EnsureSetup runs the installer, creates directories, prewarms the
// local engine (launch → llama.cpp starts automatically → healthy model)
// and starts the scheduled engine-update loop.
func (s *Server) EnsureSetup() error {
        cfg := s.src.Load()

        if err := cfg.EnsureDirs(); err != nil {
                return err
        }

        if _, _, err := s.installer.EnsureRun(false); err != nil {
                return err
        }

        // v1.1.3Z — THE acceptance requirement: the application owns the engine
        // lifecycle. A clean launch must reach a healthy model without any
        // manual llama.cpp intervention.
        if cfg.LlamaAutoStart && s.stack != nil {
                s.stack.PrewarmLLM()
        }

        // v1.1.4Z: the scheduled engine-update loop is live. RunScheduled was
        // fully implemented (immediate pass when due, re-check every 6 h) but
        // had zero callers — the "update (scheduled: daily/weekly/monthly)"
        // contract in the CLI help was only ever honored by manual runs. It
        // respects the UpdateSchedule setting ("off" disables it) and
        // short-circuits while offline.
        sched := strings.ToLower(strings.TrimSpace(cfg.UpdateSchedule))
        if sched != "off" && sched != "never" {
                ctx, cancel := context.WithCancel(context.Background())

                done := updater.RunScheduled(
                        ctx,
                        s.src,
                        s.llama,
                        nil,
                        func() {
                                // persist LastUpdateCheck mutations through the source
                                next := s.src.Load()
                                _ = config.Save(next.ConfigPath(), next)
                        },
                )

                s.updateCancel = cancel
                s.updateDone = done
        }

        // v1.2.4: idle maintenance loop of the runtime memory policy —
        // background cleanup runs ONLY while no agent run is active.
        if s.stack != nil && s.stack.MemMgr != nil {
                s.stack.StartMemoryManager(context.Background())
        }

        return nil
}

// Handler returns the http.Handler with all routes mounted.
func (s *Server) Handler() http.Handler {
        mux := http.NewServeMux()

        // Static UI
        staticRoot, _ := fs.Sub(web.StaticFS, "static")
        mux.Handle("/", http.FileServer(http.FS(staticRoot)))

        // REST API
        mux.HandleFunc("/api/state", s.handleState)
        mux.HandleFunc("/api/sysinfo", s.handleSysinfo)
        mux.HandleFunc("/api/presets", s.handlePresets)
        mux.HandleFunc("/api/models", s.handleModels)
        mux.HandleFunc("/api/sessions", s.handleSessions)
        mux.HandleFunc("/api/sessions/", s.handleSession)
        mux.HandleFunc("/api/config", s.handleConfig)
        mux.HandleFunc("/api/llama", s.handleLlama)
        mux.HandleFunc("/api/engine", s.handleEngine)
        mux.HandleFunc("/api/attachments", s.handleAttachments)
        mux.HandleFunc("/api/attachments/", s.handleAttachments)
        mux.HandleFunc("/api/run", s.handleRun)
        mux.HandleFunc("/api/abort", s.handleAbort)
        mux.HandleFunc("/api/feedback", s.handleFeedback)
        mux.HandleFunc("/api/tools", s.handleTools)
        mux.HandleFunc("/api/lab", s.handleLab)
        mux.HandleFunc("/api/lab/", s.handleLabTask)
        mux.HandleFunc("/api/research", s.handleResearch)
        mux.HandleFunc("/api/models/open-folder", s.handleModelsFolder)

        // v1.2.4: Workspace surface — one compact work-environment view plus
        // the reveal/switch actions.
        mux.HandleFunc("/api/workspace", s.handleWorkspace)
        mux.HandleFunc("/api/workspace/reveal", s.handleWorkspaceReveal)
        mux.HandleFunc("/api/workspace/switch", s.handleWorkspaceSwitch)

        // v1.1.7: compact live performance, in-app logs and connection
        // diagnostics — read-only surfaces over existing infrastructure.
        mux.HandleFunc("/api/perf", s.handlePerf)
        mux.HandleFunc("/api/logs", s.handleLogs)
        mux.HandleFunc("/api/netcheck", s.handleNetcheck)

        // v1.2.0: Environment Centre, verified health, the recommendation
        // engine and the application update surface.
        mux.HandleFunc("/api/environment", s.handleEnvironment)
        mux.HandleFunc("/api/health", s.handleHealth)
        mux.HandleFunc("/api/recommendation", s.handleRecommendation)
        mux.HandleFunc("/api/update/status", s.handleUpdateStatus)
        mux.HandleFunc("/api/update/check", s.handleUpdateCheck)
        mux.HandleFunc("/api/update/cancel", s.handleUpdateCancel)
        mux.HandleFunc("/api/update/download", s.handleUpdateDownload)

        // WebSocket: real-time agent activity for a session
        mux.HandleFunc("/ws/activity", s.handleActivityWS)

        return withCORS(mux)
}

// withCORS allows only approved local origins during development and
// same-origin requests in normal operation.
//
// Wildcard origins are intentionally prohibited because this API can expose
// local runtime state and perform privileged local operations.
func withCORS(h http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                origin := strings.TrimSpace(r.Header.Get("Origin"))

                if origin != "" {
                        if !allowedOrigin(origin, r.Host) {
                                http.Error(w, "forbidden origin", http.StatusForbidden)
                                return
                        }

                        w.Header().Set("Access-Control-Allow-Origin", origin)
                        w.Header().Set("Vary", "Origin")
                        w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
                        w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
                }

                if r.Method == http.MethodOptions {
                        w.WriteHeader(http.StatusNoContent)
                        return
                }

                h.ServeHTTP(w, r)
        })
}

// --- State ---

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
        st, _, _ := s.installer.EnsureRun(false)

        writeJSON(w, map[string]any{
                "appName":    config.AppName,
                "appVersion": config.AppVersion,
                "state":      st,
        })
}

func (s *Server) handleSysinfo(w http.ResponseWriter, r *http.Request) {
        // Re-probe on demand so the UI can refresh.
        info := sysinfo.Probe()
        writeJSON(w, info)
}

func (s *Server) handlePresets(w http.ResponseWriter, r *http.Request) {
        writeJSON(w, llm.Presets())
}

// modelInfo is the rich local-model descriptor consumed by the UI's model
// pickers. Before v1.1.2Z the API shipped bare filename strings while the
// frontend expected {id, name, path, sizeBytes} objects — every <option>
// rendered with an undefined value, so selecting a model silently wrote
// "undefined" into the config. The wire format now matches the contract.
//
// v1.1.4Z: architecture / quantization / context capacity arrive from the
// GGUF header (llm.ReadModelCard) — a full parser that existed in the
// repository with ZERO callers while the README documented exactly these
// fields. Headers are read from a bounded 8 MB prefix and cached by
// path+mtime so repeated polls never re-parse model files.
type modelInfo struct {
        ID        string `json:"id"`
        Name      string `json:"name"`
        Provider  string `json:"provider,omitempty"`
        Path      string `json:"path,omitempty"`
        SizeBytes int64  `json:"sizeBytes,omitempty"`

        Architecture  string `json:"architecture,omitempty"`
        Quantization  string `json:"quantization,omitempty"`
        ContextLength int    `json:"contextLength,omitempty"`
        ParameterInfo string `json:"parameterInfo,omitempty"`

        // 1.1.6 §10 first-use model card: estimated memory footprint
        // (weights + KV-cache at the recommended context + runtime
        // overhead) and the model-class-recommended context, so the UI
        // can present Architecture / Quantization / Context maximum /
        // Estimated memory / Backend / Status without guessing.
        EstimatedMemoryBytes int64 `json:"estimatedMemoryBytes,omitempty"`
        RecommendedContext   int   `json:"recommendedContext,omitempty"`

        // v1.1.8 model-picker capability facts, measured by
        // llm.ResolveModelCapabilities and served from the same bounded
        // cache as the header card (never guessed in the UI):
        //   - Multimodal:    a vision projector is paired with this model
        //   - NativeBackend: the native C++ engine can execute this arch
        //   - ChatTemplate:  the architecture ships a usable chat template
        //                    (prerequisite for tool calling)
        //   - EstimatedVRAMBytes: VRAM estimate at the recommended context
        Multimodal         bool   `json:"multimodal,omitempty"`
        NativeBackend      bool   `json:"nativeBackend,omitempty"`
        ChatTemplate       bool   `json:"chatTemplate,omitempty"`
        NativeReason       string `json:"nativeReason,omitempty"`
        EstimatedVRAMBytes int64  `json:"estimatedVRAMBytes,omitempty"`

        // Serving (v1.1.5Z Phase 6): true when the ACTIVE backend is
        // currently serving THIS model — the honest "currently serving"
        // marker for the picker. The backend itself is reported once at
        // the response level.
        Serving bool `json:"serving,omitempty"`

        // v1.2.0: the evidence-based vision state machine + projector
        // evidence (state, reason, paired mmproj path/size). Embedded so
        // the JSON stays flat for the UI.
        visionFields
}

// modelCardCache memoizes GGUF header reads for handleModels.
//
// v1.1.5Z Phase 3: entries are keyed by path+SIZE+mtime (a same-size
// rewrite used to serve stale metadata) and the map is bounded — past the
// cap it is reset wholesale (model files change rarely; the read is cheap
// and buffered).
var (
        modelCardMu    sync.Mutex
        modelCardCache = map[string]modelCardEntry{}
)

type modelCardEntry struct {
        modTime time.Time
        size    int64
        info    *llm.ModelCard

        // v1.1.8: ResolveModelCapabilities re-parses the same GGUF header the
        // card read already covers. It used to run UNCACHED for every local
        // model on every /api/models poll; caching it here removes that
        // duplicated capability calculation from the hot polling path.
        caps *llm.ModelCapabilities
}

// modelCardCacheCap bounds the card cache; a models directory with more
// entries than this re-parses on demand (correct, just slower).
const modelCardCacheCap = 512

// modelCardFor returns cached GGUF metadata and capability card for path
// (nil card when unreadable — a broken header must not hide the file from
// the picker; caps stay nil in that case).
func modelCardFor(path string, cfg *config.Config) (*llm.ModelCard, *llm.ModelCapabilities) {
        fi, err := os.Stat(path)
        if err != nil {
                return nil, nil
        }

        key := path + "\x00" + strconv.FormatInt(fi.Size(), 10)

        modelCardMu.Lock()
        cached, ok := modelCardCache[key]
        modelCardMu.Unlock()

        if ok && cached.size == fi.Size() && cached.modTime.Equal(fi.ModTime()) {
                return cached.info, cached.caps
        }

        card, _ := llm.ReadModelCard(path)
        caps := llm.ResolveModelCapabilities(cfg, path)

        modelCardMu.Lock()
        if len(modelCardCache) >= modelCardCacheCap {
                modelCardCache = map[string]modelCardEntry{}
        }
        modelCardCache[key] = modelCardEntry{modTime: fi.ModTime(), size: fi.Size(), info: card, caps: caps}
        modelCardMu.Unlock()

        return card, caps
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
        cfg := s.src.Load()
        local := llm.ListLocalModels(cfg.ModelsDir)

        loaded, err := s.llama.ListLoadedModels()
        if err != nil {
                loaded = nil
        }

        localInfos := make([]modelInfo, 0, len(local))

        // v1.1.8: one host memory probe per poll, shared by every model card.
        mem := s.systemMemory()

        for _, name := range local {
                info := modelInfo{
                        ID:       name,
                        Name:     strings.TrimSuffix(name, ".gguf"),
                        Provider: "local",
                        Path:     filepath.Join(cfg.ModelsDir, name),
                }

                if fi, statErr := os.Stat(info.Path); statErr == nil {
                        info.SizeBytes = fi.Size()
                }

                // v1.1.8: card AND caps come from the same bounded cache;
                // the per-poll ResolveModelCapabilities call is gone.
                card, caps := modelCardFor(info.Path, cfg)

                if card != nil && card.Arch != "" {
                        info.Architecture = card.Arch
                        info.Quantization = card.Quant
                        info.ContextLength = card.ContextLength
                        info.ParameterInfo = card.FormatParams()

                        // 1.1.6 §10: the estimated footprint uses the model
                        // class' recommended context, not the global setting,
                        // so the picker stays honest for any machine.
                        // v1.1.8: the host memory probe is hoisted out of the
                        // per-model loop — one read per poll, not N.
                        if caps != nil {
                                info.RecommendedContext = caps.RecommendedCtx
                                info.EstimatedMemoryBytes = llm.AssessContextResource(
                                        card, info.SizeBytes, caps.RecommendedCtx,
                                        cfg.EffectiveKVCacheQuant(), mem,
                                ).EstimatedTotalBytes

                                // v1.1.8 model-picker capability facts —
                                // measured, cached, never guessed client-side.
                                info.Multimodal = caps.Multimodal
                                info.NativeBackend = caps.NativeBackend
                                info.ChatTemplate = caps.ChatTemplate
                                info.NativeReason = caps.NativeReason
                                info.EstimatedVRAMBytes = caps.EstimatedVRAMBytes
                        }
                }

                // v1.2.0: per-model vision readiness (state machine +
                // projector evidence). Computed for EVERY local model so
                // the picker can render honest vision states before any
                // boot. When the engine is actively serving this model
                // WITH a projector, the state is upgraded to the runtime
                // truth (ready/verified beats pre-boot evidence).
                info.visionFields = visionForModel(
                        cfg.ModelsDir, info.Path, cfg.VisionMMProj, info.Architecture,
                )
                if info.Serving || filepath.Clean(info.Path) == filepath.Clean(s.llama.LoadedModel()) {
                        if vs := s.llama.VisionStatus(); vs.Active {
                                info.VisionState = vision.StateReady
                                info.VisionReason = vs.Reason
                                info.MMProjVerified = true
                        }
                }

                localInfos = append(localInfos, info)
        }

        loadedInfos := make([]modelInfo, 0, len(loaded))

        for _, id := range loaded {
                loadedInfos = append(loadedInfos, modelInfo{
                        ID:       id,
                        Name:     id,
                        Provider: "local",
                })
        }

        // v1.1.5Z Phase 6 (honest model status): report WHICH backend is
        // serving and WHICH model it serves, and mark that model in the
        // local list. The UI vocabulary stays honest: discovered (listed
        // here), loaded (llama.cpp loaded list), serving (the active
        // backend is generating with it right now). Capability wording
        // (native-capable / llama-capable) is owned by /api/engine, which
        // has the validated capability state.
        backend := "llama"

        if s.stack != nil && s.stack.Engine() != nil {
                backend = s.stack.Engine().Name()
        }

        servingPath := ""

        switch backend {
        case "native":
                if s.native != nil {
                        servingPath = s.native.NativeModelPath()
                }
        default:
                servingPath = s.llama.LoadedModel()
        }

        if servingPath != "" {
                for i := range localInfos {
                        if filepath.Clean(localInfos[i].Path) == filepath.Clean(servingPath) {
                                localInfos[i].Serving = true
                        }
                }
        }

        writeJSON(w, map[string]any{
                "local":        localInfos,
                "loaded":       loadedInfos,
                "llamaRunning": s.llama.IsRunning(),
                "backend":      backend,
                "servingPath":  servingPath,
        })
}

func (s *Server) handleLlama(w http.ResponseWriter, r *http.Request) {
        switch r.Method {
        case http.MethodGet:
                writeJSON(w, map[string]any{
                        "state": s.llama.State(),
                        "logs":  s.llama.Logs(),
                })

        case http.MethodPost:
                r.Body = http.MaxBytesReader(w, r.Body, 4<<10)

                var body struct {
                        Action string `json:"action"`
                }

                if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
                        writeErr(w, http.StatusBadRequest, err)
                        return
                }

                switch body.Action {
                case "start":
                        // v1.1.5Z: when the native path is enabled, the
                        // engine toggle brings BOTH engines up: the native
                        // engine AND the llama.cpp engine.
                        //
                        // v1.1.5Z repair (toggle usability): the toggle must
                        // produce a USABLE native engine — start the host AND
                        // load the selected model natively. Starting the host
                        // alone left it generation-incapable, so every run
                        // still gated on llama.cpp: on a native-only
                        // deployment (no llama.cpp binary) the "engine start"
                        // control could never lead to a working run — the
                        // exact "a button is not a feature" defect class.
                        // Synchronous + bounded (matches the toggle's
                        // existing synchronous llama.Start contract); the
                        // launch prewarm uses the same Stack seam.
                        nativeServes := false

                        if s.native != nil && s.src.Load().NativeBackendEnabled() {
                                nativeCtx, nativeCancel := context.WithTimeout(
                                        context.Background(),
                                        60*time.Second,
                                )

                                if err := s.stack.EnsureNativeReady(nativeCtx); err != nil {
                                        // Logged inside EnsureNativeReady; the
                                        // llama result below stays authoritative
                                        // when native did not come up.
                                        logging.Default().Warn(
                                                "native-engine",
                                                "engine toggle: native engine not ready: %v",
                                                err,
                                        )
                                }

                                nativeCancel()

                                if gc, ok := s.stack.NativeBackend().(llm.GenerationCapable); ok && gc.GenerationCapable() {
                                        nativeServes = true
                                }
                        }

                        if err := s.llama.Start(); err != nil {
                                if nativeServes {
                                        // Native serves generation; llama.cpp is
                                        // the FALLBACK — a failed fallback must
                                        // not fail the toggle (the same ordering
                                        // the Phase 5 run-gate repair applies to
                                        // EnsureLLM/EnsureLLMContext).
                                        logging.Default().Warn(
                                                "engine",
                                                "llama.cpp did not start (native engine serves generation): %v",
                                                err,
                                        )
                                } else {
                                        writeErr(w, http.StatusInternalServerError, err)
                                        return
                                }
                        }

                case "cancel-download":
                        // v1.2.3: stop an in-flight engine asset download
                        // (llama.cpp archive) immediately; the .part file stays
                        // so a retry resumes instead of restarting from zero.
                        cancelled := s.llama.CancelDownload()
                        writeJSON(w, map[string]any{
                                "cancelled": cancelled,
                                "state":     s.llama.State(),
                        })
                        return

                case "stop":
                        // v1.1.5Z: stop both engines (native bounded, errors
                        // logged only — the llama stop result is the
                        // authoritative one).
                        if s.native != nil {
                                stopCtx, cancel := context.WithTimeout(
                                        context.Background(),
                                        10*time.Second,
                                )

                                if err := s.native.Stop(stopCtx); err != nil {
                                        logging.Default().Warn(
                                                "native-engine",
                                                "native engine stop: %v",
                                                err,
                                        )
                                }

                                cancel()
                        }

                        _ = s.llama.Stop()

                default:
                        writeErr(w, http.StatusBadRequest, fmt.Errorf("unknown action %q", body.Action))
                        return
                }

                writeJSON(w, map[string]any{
                        "state": s.llama.State(),
                })

        default:
                writeErr(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
        }
}

// --- Sessions ---

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
        switch r.Method {
        case http.MethodGet:
                list, err := s.store.List()
                if err != nil {
                        writeErr(w, http.StatusInternalServerError, err)
                        return
                }

                writeJSON(w, list)

        case http.MethodPost:
                sess := s.store.Create()
                writeJSON(w, sess)

        default:
                writeErr(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
        }
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
        rest := strings.TrimPrefix(r.URL.Path, "/api/sessions/")

        if rest == "" {
                writeErr(w, http.StatusBadRequest, fmt.Errorf("missing session id"))
                return
        }

        // 1.1.6: per-session context policy subresource.
        if id, sub := splitSessionPath(rest); sub == "context" {
                s.handleSessionContext(w, r, id)
                return
        }

        id := rest

        switch r.Method {
        case http.MethodGet:
                sess, err := s.store.Get(id)
                if err != nil {
                        writeErr(w, http.StatusNotFound, err)
                        return
                }

                writeJSON(w, sess)

        case http.MethodDelete:
                if err := s.store.Delete(id); err != nil {
                        writeErr(w, http.StatusInternalServerError, err)
                        return
                }

                writeJSON(w, map[string]any{
                        "ok": true,
                })

        case http.MethodPut:
                r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

                var body struct {
                        Title   *string           `json:"title,omitempty"`
                        Context *sessions.Context `json:"context,omitempty"`
                        Model   *string           `json:"model,omitempty"`
                }

                if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
                        writeErr(w, http.StatusBadRequest, err)
                        return
                }

                if body.Title != nil {
                        if err := s.store.UpdateTitle(id, *body.Title); err != nil {
                                writeErr(w, http.StatusInternalServerError, err)
                                return
                        }
                }

                if body.Context != nil {
                        if err := s.store.UpdateContext(id, *body.Context); err != nil {
                                writeErr(w, http.StatusInternalServerError, err)
                                return
                        }
                }

                if body.Model != nil {
                        if err := s.store.SetModel(id, *body.Model); err != nil {
                                writeErr(w, http.StatusInternalServerError, err)
                                return
                        }
                }

                sess, err := s.store.Get(id)
                if err != nil {
                        writeErr(w, http.StatusNotFound, err)
                        return
                }

                writeJSON(w, sess)

        default:
                writeErr(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
        }
}

// redactedConfig returns a copy safe for API responses.
// Secrets are never returned to the browser.
func (s *Server) redactedConfig() config.Config {
        cfg := *s.src.Load()
        cfg.RemoteAPIKey = ""
        return cfg
}

// mergeConfigPatch applies a JSON object as a partial configuration update
// and publishes the result through the config source.
//
// Fields omitted from the request retain their current values.
//
// v1.1.4Z: the patch is applied to a PRIVATE COPY under the source's write
// lock and the new value is atomically published (copy-on-write). The old
// implementation wrote `*s.cfg = updated` in place on the shared pointer —
// a genuine data race against every run goroutine and the engine manager,
// which read the same struct concurrently. Handlers and runs now snapshot
// via s.src.Load() and observe either the old or the new value, never a
// half-written one.
func (s *Server) mergeConfigPatch(data []byte) (*config.Config, error) {
        var patch map[string]json.RawMessage
        if err := json.Unmarshal(data, &patch); err != nil {
                return nil, err
        }
        if patch == nil {
                return nil, fmt.Errorf("configuration patch must be a JSON object")
        }

        currentData, err := json.Marshal(s.src.Load())
        if err != nil {
                return nil, err
        }

        var current map[string]json.RawMessage
        if err := json.Unmarshal(currentData, &current); err != nil {
                return nil, err
        }

        for key, value := range patch {
                // A blank remoteApiKey coming from a redacted GET response must never
                // erase the stored secret. A non-empty value can intentionally replace
                // the configured key.
                if key == "remoteApiKey" {
                        var candidate string
                        if err := json.Unmarshal(value, &candidate); err == nil && candidate == "" {
                                continue
                        }
                }

                current[key] = value
        }

        merged, err := json.Marshal(current)
        if err != nil {
                return nil, err
        }

        var updated config.Config
        if err := json.Unmarshal(merged, &updated); err != nil {
                return nil, err
        }

        s.src.Store(&updated)
        return &updated, nil
}

// handleConfig patches the live configuration through the copy-on-write
// source.
//
// GET never exposes RemoteAPIKey.
//
// PUT/POST behave as patch operations: only fields supplied by the caller are
// changed; unspecified settings remain untouched.
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
        switch r.Method {
        case http.MethodGet:
                writeJSON(w, s.redactedConfig())

        case http.MethodPut, http.MethodPost:
                // v1.1.4Z: bounded body — the config endpoint previously
                // accepted unbounded request bodies.
                r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

                var raw json.RawMessage

                decoder := json.NewDecoder(r.Body)
                decoder.UseNumber()

                if err := decoder.Decode(&raw); err != nil {
                        writeErr(w, http.StatusBadRequest, err)
                        return
                }

                if len(raw) == 0 || string(raw) == "null" {
                        writeErr(w, http.StatusBadRequest, fmt.Errorf("configuration patch must be a JSON object"))
                        return
                }

                next, err := s.mergeConfigPatch(raw)
                if err != nil {
                        writeErr(w, http.StatusBadRequest, err)
                        return
                }

                if err := config.Save(next.ConfigPath(), next); err != nil {
                        writeErr(w, http.StatusInternalServerError, err)
                        return
                }

                if err := next.EnsureDirs(); err != nil {
                        writeErr(w, http.StatusInternalServerError, err)
                        return
                }

                writeJSON(w, s.redactedConfig())
        default:
                writeErr(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
        }
}

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
        out := make([]map[string]any, 0)

        for _, t := range s.orch.Tools() {
                // v1.1.7: the Options UI shows the SHORT one-line description;
                // the full operational spec (used by the model) stays in
                // `detail` — documented, not duplicated, never truncated.
                short, full := t.Description(), t.Description()
                if sd, ok := any(t).(interface {
                        ShortDescription() string
                }); ok {
                        short = sd.ShortDescription()
                }

                out = append(out, map[string]any{
                        "name":        t.Name(),
                        "description": short,
                        "detail":      full,
                })
        }

        writeJSON(w, out)
}

// --- Run / Abort ---

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
                return
        }

        // Bound request parsing; the run itself is async and bounded elsewhere.
        r.Body = http.MaxBytesReader(w, r.Body, 4<<20)

        var body struct {
                SessionID     string   `json:"sessionId"`
                Message       string   `json:"message"`
                AttachmentIDs []string `json:"attachmentIds,omitempty"`
                Regenerate    bool     `json:"regenerate,omitempty"`

                // v1.2.5 per-request controls — they shape the ACTUAL backend
                // request (tier posture, tool surface), never just the UI.
                Thinking  string   `json:"thinking,omitempty"`  // "auto" | "fast" | "thinking"
                ToolMode  string   `json:"toolMode,omitempty"`  // "auto" | "manual"
                ToolAllow []string `json:"toolAllow,omitempty"` // manual-mode allow-list
        }

        if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
                writeErr(w, http.StatusBadRequest, err)
                return
        }

        // v1.2.5: the run clock anchors to the moment the request landed.
        receivedAt := time.Now()

        if body.SessionID == "" {
                writeErr(w, http.StatusBadRequest, fmt.Errorf("sessionId required"))
                return
        }

        if body.Message == "" && !body.Regenerate {
                writeErr(w, http.StatusBadRequest, fmt.Errorf("message required (or regenerate=true)"))
                return
        }

        sess, err := s.store.Get(body.SessionID)
        if err != nil {
                writeErr(w, http.StatusNotFound, err)
                return
        }

        // Regenerate: drop trailing assistant output so the previous user
        // turn re-runs; the message body is optional in that case.
        if body.Regenerate {
                for len(sess.Messages) > 0 {
                        last := sess.Messages[len(sess.Messages)-1]

                        if last.Role == "assistant" {
                                sess.Messages = sess.Messages[:len(sess.Messages)-1]
                                continue
                        }

                        if last.Role == "tool" {
                                sess.Messages = sess.Messages[:len(sess.Messages)-1]
                                continue
                        }

                        break
                }
        }

        // Resolve staged attachments for this turn: explicit ids from the
        // request plus the session's persisted association.
        attIDs := s.attachmentIDsForSession(body.AttachmentIDs)

        for _, id := range s.attachmentIDsForSession(sess.Context.AttachmentIDs) {
                known := false

                for _, knownID := range attIDs {
                        if knownID == id {
                                known = true
                                break
                        }
                }

                if !known {
                        attIDs = append(attIDs, id)
                }
        }

        // Append the user message (display carries attachment names).
        if body.Message != "" {
                userMsg := llm.Message{
                        Role:    "user",
                        Content: body.Message,
                }

                if len(attIDs) > 0 && s.stack != nil && s.stack.Attachments != nil {
                        for _, id := range attIDs {
                                if att, ok := s.stack.Attachments.Get(id); ok {
                                        userMsg.Attachments = append(
                                                userMsg.Attachments,
                                                att.Name,
                                        )
                                }
                        }
                }

                sess.Messages = append(sess.Messages, userMsg)

                // If first user message, derive a title.
                if len(sess.Messages) == 1 {
                        title := body.Message
                        if len(title) > 60 {
                                title = title[:60] + "..."
                        }

                        _ = s.store.UpdateTitle(sess.ID, title)
                }
        }

        // Persist attachment association on the session context so later turns
        // keep the files reusable.
        if len(attIDs) > 0 {
                merged := sess.Context.AttachmentIDs

                for _, id := range attIDs {
                        known := false

                        for _, knownID := range merged {
                                if knownID == id {
                                        known = true
                                        break
                                }
                        }

                        if !known {
                                merged = append(merged, id)
                        }
                }

                sess.Context.AttachmentIDs = merged

                for _, id := range attIDs {
                        if s.stack != nil && s.stack.Attachments != nil {
                                s.stack.Attachments.Associate(id, sess.ID)
                        }
                }
        }

        // v1.1.4Z: a failed pre-run persistence was previously invisible —
        // the user message could silently vanish while the run continued.
        if err := s.store.Save(sess); err != nil {
                logging.Default().Warn(
                        "api",
                        "persist session before run: %v",
                        err,
                )
        }

        // Build the LLM message list (with optional system prompt).
        var messages []llm.Message

        if sess.Context.SystemPrompt != "" {
                messages = append(messages, llm.Message{
                        Role:    "system",
                        Content: sess.Context.SystemPrompt,
                })
        }

        // Legacy AttachedFiles (server-side paths) still flow through the
        // chunking engine; the staged attachments pipeline is layered on top.
        attachedImages, _ := chunking.SplitAttachments(sess.Context.AttachedFiles)

        if len(sess.Context.AttachedFiles) > 0 {
                note := chunking.ComposeUserMessage(
                        "",
                        sess.Context.AttachedFiles,
                        s.src.Load().AttachmentsBudgetBytes(),
                )

                messages = append(messages, llm.Message{
                        Role:    "system",
                        Content: note,
                })
        }

        messages = append(messages, sess.Messages...)

        // The freshest user message carries the current turn's images —
        // both legacy paths and newly staged image attachments.
        stagedImagePaths := make([]string, 0, 4)

        if len(attIDs) > 0 && s.stack != nil && s.stack.Attachments != nil {
                for _, id := range attIDs {
                        if att, ok := s.stack.Attachments.Get(id); ok &&
                                att.Kind == attachments.KindImage {
                                stagedImagePaths = append(
                                        stagedImagePaths,
                                        s.stack.Attachments.StagePath(id),
                                )
                        }
                }
        }

        for i := len(messages) - 1; i >= 0; i-- {
                if messages[i].Role == "user" {
                        messages[i].Images = append(messages[i].Images, attachedImages...)
                        messages[i].Images = append(messages[i].Images, stagedImagePaths...)
                        break
                }
        }

        // v1.1.3Z: staged attachments enter the real context pipeline here —
        // relevant chunks are retrieved for the current query and injected
        // as a bounded, provenance-tagged block BEFORE the fresh user turn
        // (stable prompt prefix preserves the engine KV cache). Raw binaries
        // never reach the prompt. The block is content-cached: identical
        // attachments + query skip re-chunking entirely.
        if len(attIDs) > 0 && s.stack != nil && s.stack.Attachments != nil {
                query := body.Message

                if query == "" {
                        // Regenerate: retrieve against the last user turn.
                        for i := len(sess.Messages) - 1; i >= 0; i-- {
                                if sess.Messages[i].Role == "user" {
                                        query = sess.Messages[i].Content
                                        break
                                }
                        }
                }

                budget := s.src.Load().AttachmentsBudgetBytes()

                // v1.1.5Z Phase 3: retrieval is instrumented with MEASURED
                // values only (chunks considered/selected, object reads,
                // bytes). Logged per turn for diagnosability; the wire/UI
                // contract is unchanged.
                block, retrievalStats := s.stack.Attachments.RetrieveWithStats(
                        r.Context(),
                        query,
                        attIDs,
                        budget,
                )

                logging.Default().Info(
                        "api",
                        "attachment retrieval: attachments=%d chunksConsidered=%d chunksSelected=%d bytes=%d objectReads=%d objectBytes=%d cacheHit=%t",
                        retrievalStats.AttachmentsConsidered,
                        retrievalStats.ChunksConsidered,
                        retrievalStats.ChunksSelected,
                        retrievalStats.BytesComposed,
                        retrievalStats.ObjectReads,
                        retrievalStats.ObjectBytesRead,
                        retrievalStats.CacheHit,
                )

                if block != "" {
                        blockMsg := llm.Message{
                                Role:    "system",
                                Content: "[staged attachments relevant to this turn]\n\n" + block,
                        }

                        // Insert before the last user message.
                        insertAt := len(messages)

                        for i := len(messages) - 1; i >= 0; i-- {
                                if messages[i].Role == "user" {
                                        insertAt = i
                                        break
                                }
                        }

                        messages = append(
                                messages[:insertAt],
                                append(
                                        []llm.Message{blockMsg},
                                        messages[insertAt:]...,
                                )...,
                        )
                }
        }

        // Spawn the run.
        //
        // v1.1.4Z: RunTimeoutMinutes (default 60) bounds the whole turn.
        // Previously a run was bounded ONLY by maxIterations and the LLM
        // client's per-call timeout — a pathological turn could legally run
        // for hours while holding the run slot.
        runCtx := context.Background()
        budgetCancel := func() {}

        if budget := s.src.Load().EffectiveRunTimeout(); budget > 0 {
                var budgetCtx context.Context
                budgetCtx, budgetCancel = context.WithTimeout(runCtx, budget)
                runCtx = budgetCtx
        }

        ctx, cancel := context.WithCancel(runCtx)
        hub := newActivityHub()

        s.runsMu.Lock()

        // There can be only one active run per session. Cancel the previous run
        // before replacing its state.
        if old, ok := s.runs[sess.ID]; ok {
                old.cancel()
                old.hub.close()
        }

        s.runs[sess.ID] = &runState{
                cancel: cancel,
                hub:    hub,
        }

        s.runsMu.Unlock()

        // v1.2.4: mark the run active for the memory policy — coordinated
        // cleanup (cache shedding, GC) is deferred while a generation is live,
        // and runs as soon as the run ends (success, error or abort; the defer
        // below is cancellation-aware).
        if s.stack.MemMgr != nil {
                s.stack.MemMgr.TrackRunStart()
        }

        // v1.1.2Z: release every activity connection parked in standby for
        // this session so they attach to the new run hub immediately.
        s.wakeStandby(sess.ID)

        go func() {
                defer func() {
                        // release the run-budget timer with the run itself
                        budgetCancel()

                        // v1.2.4: run ended — the memory manager releases one-shot
                        // state (image cache, cold session cache) and persists
                        // coalesced telemetry, with measured before/after stats.
                        if s.stack.MemMgr != nil {
                                s.stack.MemMgr.TrackRunEnd()
                        }

                        s.runsMu.Lock()

                        // Only remove the run if this goroutine still owns the current
                        // session entry. A newer run may already have replaced it.
                        if current, ok := s.runs[sess.ID]; ok && current.hub == hub {
                                delete(s.runs, sess.ID)
                        }

                        s.runsMu.Unlock()

                        hub.close()
                        cancel()
                }()

                // v1.1.3Z: gate every run on a real, healthy engine. A cold start
                // here streams the authoritative engine transitions to the UI
                // (starting → ready) while the request waits, bounded by the
                // engine gate timeout.
                gateCtx, gateCancel := context.WithTimeout(ctx, 3*time.Minute)

                if err := s.stack.EnsureLLMContext(gateCtx); err != nil {
                        gateCancel()

                        hub.publish(agent.Activity{
                                Type: "error",
                                Caption: fmt.Sprintf(
                                        "Engine unavailable: %v",
                                        err,
                                ),
                                Timestamp: time.Now(),
                        })

                        return
                }

                gateCancel()

                // 1.1.6 §4: the run carries the session's own context
                // policy — the orchestrator plans, gates and sends the
                // request under THIS chat's resolved window.
                res, err := s.orch.RunDetailed(
                        ctx,
                        messages,
                        func(a agent.Activity) {
                                hub.publish(a)

                                // Persist milestone events only. Streaming response/reasoning
                                // deltas are intentionally not persisted individually because
                                // each persistence operation rewrites the session JSON.
                                // v1.2.5: the status/thinking-marker deltas join the
                                // skip list (tiny, high-frequency, no persisted value).
                                switch a.Type {
                                case "response", "thinking", "reasoning",
                                        "status", "thinking_start", "thinking_end":
                                        return
                                }

                                // v1.1.4Z: persistence failures surface as a
                                // visible warning instead of vanishing.
                                if err := s.store.AppendActivity(
                                        sess.ID,
                                        sessions.ActivityEntry{
                                                Type:      a.Type,
                                                Caption:   a.Caption,
                                                Timestamp: a.Timestamp,
                                        },
                                ); err != nil {
                                        logging.Default().Warn(
                                                "api",
                                                "append activity: %v",
                                                err,
                                        )
                                }
                        },
                        agent.WithSessionContext(sess.Context.ContextTokens),
                        agent.WithThinkingMode(body.Thinking),
                        agent.WithToolPolicy(body.ToolMode, body.ToolAllow),
                        agent.WithReceivedAt(receivedAt),
                )

                if err != nil {
                        hub.publish(agent.Activity{
                                Type:      "error",
                                Caption:   err.Error(),
                                Timestamp: time.Now(),
                        })

                        return
                }

                // Append assistant reply.
                if res.Text != "" {
                        if _, err := s.store.AppendMessage(
                                sess.ID,
                                llm.Message{
                                        Role:      "assistant",
                                        Content:   res.Text,
                                        Reasoning: res.Reasoning,
                                },
                        ); err != nil {
                                // v1.1.4Z: a lost reply is a REAL failure the
                                // user must see, not a swallowed error.
                                hub.publish(agent.Activity{
                                        Type:      "error",
                                        Caption:   "The reply was generated but could not be saved to the session: " + err.Error(),
                                        Timestamp: time.Now(),
                                })

                                logging.Default().Error(
                                        "api",
                                        "append assistant reply: %v",
                                        err,
                                )
                        }
                }

                // Index completed exchange into persistent recall.
                if s.recall != nil && res.Text != "" {
                        if err := s.recall.IndexTurn(
                                sess.ID,
                                sess.Title,
                                body.Message,
                                res.Text,
                                res.ToolsUsed,
                        ); err != nil {
                                logging.Default().Warn(
                                        "recall",
                                        "index turn: %v",
                                        err,
                                )
                        }
                }

                // v1.1.4Z: Continuum chapter rollover. The whole subsystem
                // (distillation, chapter sessions, framework sidecars) was
                // implemented and unit-tested since v1.0.7 but NEVER wired
                // into a production path — only the pressure meter was
                // live. After a completed turn, a session at ≥ the
                // configured pressure threshold is distilled into a fresh
                // chapter that carries the framework + recent tail; the UI
                // is told via a `session` activity so it can follow the
                // thread into the new chapter.
                if s.continuum != nil {
                        if fresh, err := s.store.Get(sess.ID); err == nil {
                                if s.continuum.ShouldRollover(fresh, s.src.Load()) {
                                        if child, _, err := s.continuum.Rollover(fresh, s.src.Load()); err == nil {
                                                hub.publish(agent.Activity{
                                                        Type:    "session",
                                                        Caption: "Context threshold reached — conversation continued in chapter " + fmt.Sprint(child.Chapter),
                                                        Detail: map[string]any{
                                                                "sessionId": child.ID,
                                                                "threadId":  child.ThreadID,
                                                                "chapter":   child.Chapter,
                                                                "previous":  fresh.ID,
                                                        },
                                                        Timestamp: time.Now(),
                                                })

                                                logging.Default().Info(
                                                        "continuum",
                                                        "rolled over session %s → chapter %d (%s)",
                                                        fresh.ID,
                                                        child.Chapter,
                                                        child.ID,
                                                )
                                        } else {
                                                logging.Default().Warn(
                                                        "continuum",
                                                        "rollover failed (conversation continues in current session): %v",
                                                        err,
                                                )
                                        }
                                }
                        }
                }
        }()

        writeJSON(w, map[string]any{
                "ok":        true,
                "sessionId": sess.ID,
        })
}

func (s *Server) handleAbort(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                writeErr(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
                return
        }

        // v1.1.4Z: bounded body AND a decode failure that previously
        // returned {ok:true} while aborting nothing.
        r.Body = http.MaxBytesReader(w, r.Body, 4<<10)

        var body struct {
                SessionID string `json:"sessionId"`
        }

        if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
                writeErr(w, http.StatusBadRequest, fmt.Errorf("abort: %w", err))
                return
        }

        if body.SessionID == "" {
                writeErr(w, http.StatusBadRequest, fmt.Errorf("sessionId required"))
                return
        }

        s.runsMu.Lock()
        rs, ok := s.runs[body.SessionID]
        s.runsMu.Unlock()

        if ok {
                rs.cancel()
        }

        writeJSON(w, map[string]any{
                "ok": true,
        })
}

// --- Recall feedback ---

// handleFeedback records the user's 👍/👎 verdict for one past exchange
// (v1.1.4Z). The recall feedback sidecar (SetFeedback / FeedbackFor and
// the liked×1.25 / disliked×0.6 scoring boosts) existed since v1.0.6 with
// NO write path — the steering could never fire. The frontend sends the
// query text of the exchange it is rating; the deterministic capsule id
// is derived exactly like IndexTurn does.
func (s *Server) handleFeedback(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                writeErr(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
                return
        }

        r.Body = http.MaxBytesReader(w, r.Body, 64<<10)

        var body struct {
                SessionID string `json:"sessionId"`
                Query     string `json:"query"`
                Liked     bool   `json:"liked"`
                Clear     bool   `json:"clear,omitempty"`
        }

        if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
                writeErr(w, http.StatusBadRequest, err)
                return
        }

        if body.SessionID == "" || body.Query == "" {
                writeErr(w, http.StatusBadRequest, fmt.Errorf("sessionId and query required"))
                return
        }

        if s.recall == nil {
                writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("recall engine unavailable"))
                return
        }

        id := recall.CapsuleID(body.SessionID, body.Query)

        fb := 0
        if !body.Clear {
                if body.Liked {
                        fb = 1
                } else {
                        fb = -1
                }
        }

        if err := s.recall.SetFeedback(id, fb); err != nil {
                writeErr(w, http.StatusInternalServerError, err)
                return
        }

        writeJSON(w, map[string]any{
                "ok": true,
        })
}

// --- WebSocket: live activity ---

// wsPingInterval is how often an idle standby connection is pinged at the
// protocol level so half-open TCP sockets are detected and released.
const wsPingInterval = 25 * time.Second

func (s *Server) handleActivityWS(w http.ResponseWriter, r *http.Request) {
        sessionID := r.URL.Query().Get("sessionId")

        if sessionID == "" {
                writeErr(w, http.StatusBadRequest, fmt.Errorf("sessionId required"))
                return
        }

        conn, err := upgrader.Upgrade(w, r, nil)
        if err != nil {
                return
        }

        defer conn.Close()

        // v1.1.2Z: the read pump owns every incoming frame. It detects client
        // disconnects (read error closes clientGone) and routes abort actions
        // to whatever run is currently active for the session.
        clientGone := make(chan struct{})

        go func() {
                defer close(clientGone)

                conn.SetReadLimit(1024)

                for {
                        var msg map[string]any

                        if err := conn.ReadJSON(&msg); err != nil {
                                return
                        }

                        if action, _ := msg["action"].(string); action == "abort" {
                                s.runsMu.Lock()
                                rs, ok := s.runs[sessionID]
                                s.runsMu.Unlock()

                                if ok {
                                        rs.cancel()
                                }
                        }
                }
        }()

        idleSentinel := map[string]any{
                "type":    "idle",
                "caption": "No active run",
        }

        for {
                // Fast path: a run is already active — attach to its hub.
                s.runsMu.Lock()
                rs, ok := s.runs[sessionID]
                s.runsMu.Unlock()

                if ok {
                        _, updates, unsubscribe := rs.hub.subscribe()

                        served := false

                        for ev := range updates {
                                if err := conn.WriteJSON(ev); err != nil {
                                        served = true
                                        break
                                }

                                served = true
                        }

                        unsubscribe()

                        // Hub closed (run finished) or write failed. On write
                        // failure the client is gone; wait for clientGone so
                        // the read pump teardown wins the race deterministically.
                        if served {
                                select {
                                case <-clientGone:
                                        return
                                default:
                                }
                        } else {
                                <-clientGone
                                return
                        }

                        // Run finished: fall through to standby.
                        _ = conn.WriteJSON(idleSentinel)
                }

                // Standby: park the connection until a run starts, an engine
                // transition arrives, the client leaves, or a keepalive ping
                // is due.
                sc := s.enterStandby(sessionID)

                _ = conn.WriteJSON(idleSentinel)

                disconnected := false

                for !disconnected {
                        select {
                        case <-sc.wake:
                                disconnected = false
                                goto attached

                        case frame := <-sc.engineCh:
                                // Idle connections still observe engine state
                                // transitions (authoritative backend state).
                                if err := conn.WriteMessage(
                                        websocket.TextMessage,
                                        frame,
                                ); err != nil {
                                        disconnected = true
                                }

                        case <-clientGone:
                                disconnected = true

                        case <-time.After(wsPingInterval):
                                _ = conn.WriteControl(
                                        websocket.PingMessage,
                                        nil,
                                        time.Now().Add(5*time.Second),
                                )
                        }
                }

                s.leaveStandby(sessionID, sc)
                return

        attached:
                s.leaveStandby(sessionID, sc)
        }
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, v any) {
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
        w.WriteHeader(code)
        _ = json.NewEncoder(w).Encode(map[string]string{
                "error": err.Error(),
        })
}

func errMethodNotAllowed() error {
        return fmt.Errorf("method not allowed")
}
