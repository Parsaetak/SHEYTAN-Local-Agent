// Package api serves the embedded web UI and exposes the JSON REST+WS API
// that the UI talks to.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/gitclone"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/hardware"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/histref"
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
	// (v1.1.4: handlers snapshot it per request; the old shared mutable
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
	// (v1.1.4: the manager was fully implemented and tested but never
	// wired into any production path before).
	continuum *continuum.Manager

	// updateCancel stops the scheduled engine-update loop on Close
	// (v1.1.4: updater.RunScheduled existed with zero callers).
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

	// v1.3.0 GitHub clone job state — the same single-slot pattern as the
	// app-update staging above: one clone at a time, live status through
	// the atomic envelope, cancellation through the stored job.
	cloneJob     atomic.Pointer[gitclone.Job]
	cloneRunning atomic.Bool
	cloneStatus  atomic.Pointer[cloneStatusEnvelope]

	// active runs: sessionID → runState
	runsMu sync.Mutex
	runs   map[string]*runState

	// outcomes (v1.2.6) is the bounded authoritative run-state registry:
	// the last few terminal outcomes per session, replayed on the idle
	// sentinel so a late-attaching socket can finalise deterministically
	// instead of staying stuck on a run it never observed.
	outcomes *runRegistry

	// v1.1.2: idle WebSocket standby registry — sessionID → connections.
	// Before this, an activity WebSocket with no active run was closed
	// immediately after one "idle" sentinel, so the UI permanently showed
	// "Offline" between runs. Idle connections now stay open, and the
	// instant a run starts they are woken and attached to the run hub.
	// v1.1.3: each standby connection also owns an engineCh fed by the
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

	// live (v1.2.6 continuation) is the AUTHORITATIVE per-run state —
	// phase, running, cumulative response/reasoning snapshots, latest
	// status, monotonic sequence, terminal outcome. A WebSocket that
	// attaches at ANY point receives a snapshot frame built from it
	// before live events continue: a reconnect never has to wait for a
	// future token to discover the run's state.
	live *runLive
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
		src:       stack.Src,
		store:     store,
		stack:     stack,
		orch:      stack.Orch,
		llama:     stack.Llama,
		native:    stack.Native,
		installer: installer.New(cfg),
		runs:      make(map[string]*runState),
		outcomes:  newRunRegistry(),
		standby:   make(map[string][]*standbyConn),
		// v1.2.6 continuation: startup must NEVER block on the deep
		// hardware probe. The previous code called sysinfo.Probe()
		// here — the DEEP probe (ONE batched PowerShell/CIM
		// invocation on Windows, seconds) ran synchronously inside
		// New(), before the listener even bound. The fast snapshot
		// (in-process, sub-millisecond) serves immediately; the deep
		// probe warms in the BACKGROUND (hardware.WarmDeep in
		// EnsureSetup, single-flight) and merges into every
		// ProbeFast() reader as it lands.
		sys:        sysinfo.ProbeFast(),
		recall:     stack.Recall,
		continuum:  continuum.NewManager(store, cfg.SessionsDir),
		engineStop: make(chan struct{}),
		engineDone: make(chan struct{}),
	}

	// v1.1.3: the engine event bus fans authoritative state transitions
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

	// v1.2.6: warm the deep hardware probe in the background (ONE batched
	// CIM invocation on Windows — replaced the ~7 sequential PowerShell
	// spawns that measured 5.4 s). The UI never waits for it: fast facts
	// render immediately, deep facts land when the probe finishes.
	hardware.WarmDeep()

	// v1.1.3 — THE acceptance requirement: the application owns the engine
	// lifecycle. A clean launch must reach a healthy model without any
	// manual llama.cpp intervention.
	if cfg.LlamaAutoStart && s.stack != nil {
		s.stack.PrewarmLLM()
	}

	// v1.1.4: the scheduled engine-update loop is live. RunScheduled was
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
	mux.HandleFunc("/api/engine/rediscover", s.handleEngineRediscover)
	mux.HandleFunc("/api/attachments", s.handleAttachments)
	mux.HandleFunc("/api/attachments/", s.handleAttachments)
	mux.HandleFunc("/api/run", s.handleRun)
	mux.HandleFunc("/api/abort", s.handleAbort)
	mux.HandleFunc("/api/feedback", s.handleFeedback)
	mux.HandleFunc("/api/tools", s.handleTools)
	mux.HandleFunc("/api/lab", s.handleLab)
	mux.HandleFunc("/api/lab/", s.handleLabTask)
	mux.HandleFunc("/api/research", s.handleResearch)
	// v1.3.6 (spec §26): the user-facing Net Search endpoint. It is
	// the SAME handler and the SAME research service — one coherent
	// surface, no duplicate implementation. /api/research stays as
	// the compatibility shim.
	mux.HandleFunc("/api/net-search", s.handleResearch)
	mux.HandleFunc("/api/models/open-folder", s.handleModelsFolder)

	// v1.2.8: history surfaces — picker search (cross-mode capable),
	// rolling session summaries and lazy history paging.
	mux.HandleFunc("/api/history/search", s.handleHistorySearch)

	// v1.2.4: Workspace surface — one compact work-environment view plus
	// the reveal/switch actions.
	mux.HandleFunc("/api/workspace", s.handleWorkspace)
	mux.HandleFunc("/api/workspace/reveal", s.handleWorkspaceReveal)
	mux.HandleFunc("/api/workspace/switch", s.handleWorkspaceSwitch)

	// v1.3.0: first-class GitHub clone workflow (start/status/cancel,
	// auto-switch on success — see clone.go).
	mux.HandleFunc("/api/workspace/clone", s.handleCloneStart)
	mux.HandleFunc("/api/workspace/clone/status", s.handleCloneStatus)
	mux.HandleFunc("/api/workspace/clone/cancel", s.handleCloneCancel)

	// v1.3.4 (ROADMAP v1.4 slice 1): repository intelligence — index
	// status, bounded refresh and the hybrid search entry point.
	mux.HandleFunc("/api/repo/index", s.handleRepoIndex)
	mux.HandleFunc("/api/repo/index/refresh", s.handleRepoIndexRefresh)
	mux.HandleFunc("/api/repo/search", s.handleRepoSearch)

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
	// v1.2.6: interactive surface — NEVER blocks on the deep probe.
	// Serves the fast snapshot (+ deep facts when the background probe
	// already landed). The deep probe warms at startup; a caller that
	// needs the full facts uses sysinfo.Probe() directly.
	writeJSON(w, sysinfo.ProbeFast())
}

func (s *Server) handlePresets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, llm.Presets())
}

// modelInfo is the rich local-model descriptor consumed by the UI's model
// pickers. Before v1.1.2 the API shipped bare filename strings while the
// frontend expected {id, name, path, sizeBytes} objects — every <option>
// rendered with an undefined value, so selecting a model silently wrote
// "undefined" into the config. The wire format now matches the contract.
//
// v1.1.4: architecture / quantization / context capacity arrive from the
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

	// Serving (v1.1.5 Phase 6): true when the ACTIVE backend is
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
// v1.1.5 Phase 3: entries are keyed by path+SIZE+mtime (a same-size
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

	// v1.1.5 Phase 6 (honest model status): report WHICH backend is
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
			// v1.1.5: when the native path is enabled, the
			// engine toggle brings BOTH engines up: the native
			// engine AND the llama.cpp engine.
			//
			// v1.1.5 repair (toggle usability): the toggle must
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
			// v1.1.5: stop both engines (native bounded, errors
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

		// v1.2.8: mode-separated histories. `?mode=chat|agent`
		// filters the list to ONE conversation space; without the
		// parameter the full list is served (compat: the CLI and
		// existing tests rely on it).
		if mode := strings.TrimSpace(r.URL.Query().Get("mode")); mode == sessions.ModeChat || mode == sessions.ModeAgent {
			filtered := make([]*sessions.Session, 0, len(list))
			for _, st := range list {
				if st == nil {
					continue
				}
				stMode := st.Mode
				if stMode == "" {
					stMode = sessions.DefaultMode
				}
				if stMode == mode {
					filtered = append(filtered, st)
				}
			}
			list = filtered
		}

		writeJSON(w, list)

	case http.MethodPost:
		// v1.2.8: the creation request MAY carry the conversation
		// space ({"mode":"chat"|"agent"}). An empty body keeps the
		// default (agent) — old clients behave identically.
		mode := ""
		var body struct {
			Mode string `json:"mode,omitempty"`
		}
		if r.Body != nil {
			if data, err := io.ReadAll(io.LimitReader(r.Body, 4<<10)); err == nil && len(strings.TrimSpace(string(data))) > 0 {
				_ = json.Unmarshal(data, &body)
			}
		}
		mode = body.Mode

		sess := s.store.CreateInMode(mode)
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

	// v1.2.8: history subresources — the durable summary and the lazy
	// message pager.
	if id, sub := splitSessionPath(rest); sub == "summary" {
		s.handleSessionSummary(w, r, id)
		return
	}

	if id, sub := splitSessionPath(rest); sub == "messages" {
		s.handleSessionMessages(w, r, id)
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
			Title       *string           `json:"title,omitempty"`
			Context     *sessions.Context `json:"context,omitempty"`
			Model       *string           `json:"model,omitempty"`
			HistoryRefs *[]histref.Ref    `json:"historyRefs,omitempty"`
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
			// v1.2.8.1: history references arriving through the
			// full-context PUT are VALIDATED server-side —
			// normalized (dedup + cap), self-references dropped,
			// same-mode references enforced against the store
			// index. The client picker is a convenience, never
			// the authority; this was previously a write-only
			// sink that accepted arbitrary reference lists.
			ctx := *body.Context
			ctx.HistoryRefs = s.validateHistoryRefs(id, s.store.ModeOf(id), ctx.HistoryRefs)

			if err := s.store.UpdateContext(id, ctx); err != nil {
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

		// v1.2.8.1: atomic history-reference delta. The picker's
		// attach/detach sends ONLY the new ref list (after server-
		// side validation) and the store applies it under its lock —
		// no GET-modify-PUT of the whole context, so a concurrent
		// token-policy or attachment update can never be silently
		// reverted by a stale full-context write.
		if body.HistoryRefs != nil {
			refs := s.validateHistoryRefs(id, s.store.ModeOf(id), *body.HistoryRefs)

			if err := s.store.UpdateContextFunc(id, func(c *sessions.Context) (bool, error) {
				changed := !equalHistoryRefs(c.HistoryRefs, refs)
				if changed {
					c.HistoryRefs = refs
				}
				return changed, nil
			}); err != nil {
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
// v1.1.4: the patch is applied to a PRIVATE COPY under the source's write
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

		// v1.3.0: "llm" is merged FIELD-BY-FIELD, not replaced wholesale.
		// The v1.2.9 behavior replaced the whole LLMOptions object, so a
		// partial llm patch (preset apply, posture apply, System apply —
		// all of which send only the fields they change) silently zeroed
		// every unspecified sampling field (temperature→0, topP→0…).
		// Deep-merging here keeps every partial patch additive.
		if key == "llm" {
			mergedLLM, err := mergeLLMPatch(current["llm"], value)
			if err != nil {
				return nil, fmt.Errorf("patch llm: %w", err)
			}
			current["llm"] = mergedLLM
			continue
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

// mergeLLMPatch merges a partial llm object into the current one:
// present keys from the patch win, absent keys keep their current
// values. Either side being absent degrades to the v1.2.9 wholesale
// replacement (safe: nothing to preserve).
func mergeLLMPatch(currentRaw, patchRaw json.RawMessage) (json.RawMessage, error) {
	if len(patchRaw) == 0 || string(patchRaw) == "null" {
		return currentRaw, nil
	}

	var patchObj map[string]json.RawMessage
	if err := json.Unmarshal(patchRaw, &patchObj); err != nil {
		return nil, err
	}
	if patchObj == nil {
		return currentRaw, nil
	}

	var currentObj map[string]json.RawMessage
	if len(currentRaw) > 0 && string(currentRaw) != "null" {
		if err := json.Unmarshal(currentRaw, &currentObj); err != nil {
			return nil, err
		}
	}
	if currentObj == nil {
		currentObj = map[string]json.RawMessage{}
	}

	for k, v := range patchObj {
		currentObj[k] = v
	}

	return json.Marshal(currentObj)
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
		// v1.1.4: bounded body — the config endpoint previously
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

		// v1.3.6 (spec §24): explicit per-request Net Search intent.
		// Server-side enforcement: the existing research tool is
		// authorized for THIS request only.
		NetSearch bool `json:"netSearch,omitempty"`

		// v1.2.8: cross-mode history references — sessions the user
		// EXPLICITLY attached as context. DATA, never authority: the
		// retrieved portions are provenance-tagged and never
		// authorize execution by themselves.
		HistoryRefs []histref.Ref `json:"historyRefs,omitempty"`
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

	// v1.2.8: resolve the attached history references. Self-references
	// are dropped (the active transcript is already here); unknown or
	// foreign ids are silently skipped by Resolve (never fabricated).
	//
	// v1.2.8.1: (a) The persisted session context is the FALLBACK
	// authority — a request that omits historyRefs (regenerate, CLI,
	// any client) runs with the SAME references the session already
	// carries, so a regenerated answer can never silently lose the
	// cross-mode context of the original run. Explicit request refs
	// win: the union is body-first, deduplicated and capped by
	// NormalizeRefs. (b) Cross-mode semantics are enforced SERVER-SIDE
	// (never trusted from the picker): a reference whose SOURCE session
	// — resolved from the store index, not from the client payload — is
	// not in the opposite conversation space is dropped here, and dead
	// references (source vanished) are pruned from the context.
	refsUnion := make([]histref.Ref, 0, len(body.HistoryRefs)+len(sess.Context.HistoryRefs))
	refsUnion = append(refsUnion, body.HistoryRefs...)
	refsUnion = append(refsUnion, sess.Context.HistoryRefs...)
	requestedRefs := s.validateHistoryRefs(sess.ID, sessions.NormalizeMode(sess.Mode), refsUnion)

	// Persist the surviving references (bounded, deduplicated) ATOMICALLY
	// so later turns keep retrieving them even when the client omits the
	// refs. The store lock serializes this against every other context
	// mutation (attachment association, token policy, picker PUT).
	if !equalHistoryRefs(sess.Context.HistoryRefs, requestedRefs) {
		if err := s.store.UpdateContextFunc(sess.ID, func(c *sessions.Context) (bool, error) {
			changed := !equalHistoryRefs(c.HistoryRefs, requestedRefs)
			if changed {
				c.HistoryRefs = requestedRefs
			}
			return changed, nil
		}); err != nil {
			logging.Default().Warn("api", "persist history refs: %v", err)
		}
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
	// keep the files reusable. v1.2.8.1: applied ATOMICALLY through the
	// store's context mutation (no read-modify-write race with a
	// concurrent picker attach or token-policy update).
	if len(attIDs) > 0 {
		if err := s.store.UpdateContextFunc(sess.ID, func(c *sessions.Context) (bool, error) {
			changed := false

			for _, id := range attIDs {
				known := false

				for _, knownID := range c.AttachmentIDs {
					if knownID == id {
						known = true
						break
					}
				}

				if !known {
					c.AttachmentIDs = append(c.AttachmentIDs, id)
					changed = true
				}
			}

			return changed, nil
		}); err != nil {
			logging.Default().Warn("api", "persist attachment association: %v", err)
		}

		for _, id := range attIDs {
			if s.stack != nil && s.stack.Attachments != nil {
				s.stack.Attachments.Associate(id, sess.ID)
			}
		}
	}

	// v1.1.4: a failed pre-run persistence was previously invisible —
	// the user message could silently vanish while the run continued.
	// v1.2.8.1: the save preserves the STORE's current context (fetched
	// under the same lock) — the run's context deltas were applied
	// atomically above; a whole-object Save from this (possibly stale)
	// copy would silently revert them.
	if err := s.store.SaveMessagesKeepContext(sess); err != nil {
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

	// v1.1.3: staged attachments enter the real context pipeline here —
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

		// v1.1.5 Phase 3: retrieval is instrumented with MEASURED
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

	// v1.2.8: retrieve the relevant portions of the attached history
	// references for THIS turn and load the session's rolling summary.
	// Both blocks are pre-bounded and provenance-tagged; the planner
	// still decides whether they travel (history-refs are the first
	// retrieval section dropped under pressure). The summary never
	// replaces the transcript — it is a derived aid.
	var histRefBlocks []llm.Message

	if len(requestedRefs) > 0 {
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

		// Budget: the per-reference default, scaled to the ref count
		// (bounded by design — the picker caps refs at MaxRefsPerRun).
		histRefBlocks = histref.Resolve(
			s.store,
			requestedRefs,
			query,
			histref.DefaultBlockTokens*len(requestedRefs),
		)

		if len(histRefBlocks) > 0 {
			logging.Default().Info(
				"history",
				"retrieved %d history block(s) for session %s (refs=%d queryBytes=%d)",
				len(histRefBlocks), sess.ID, len(requestedRefs), len(query),
			)
		}
	}

	var summaryBlock string

	if sum, err := s.store.SummaryForRun(sess.ID); err == nil {
		summaryBlock = sessions.RenderSummaryBlock(sum)
	}

	// Spawn the run.
	//
	// v1.1.4: RunTimeoutMinutes (default 60) bounds the whole turn.
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

	// v1.2.6: one runId across the WHOLE path (UI → POST → registry →
	// engine gate → orchestrator → persistence → WS delivery). Every
	// activity frame of this run carries it; the outcome registry and the
	// logs use it, so a stalled run can be attributed end-to-end.
	runID := newRunID()

	// v1.2.6 continuation: the AUTHORITATIVE live state. Every published
	// activity of this run is sequence-stamped and folded into it; every
	// WebSocket attachment replays from it.
	live := newRunLive(runID, sess.ID, time.Now())

	// v1.2.6: the API-side stage timeline — runAccepted → runRegistered →
	// engineGateStart/Ready → (orchestrator stages via RunClock) →
	// assistantPersisted → donePublished. Measured values only; the
	// summary line is logged once when the run settles.
	tl := runTimeline{
		runID:      runID,
		sessionID:  sess.ID,
		acceptedAt: receivedAt,
	}

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
		live:   live,
	}

	s.runsMu.Unlock()

	tl.registeredAt = time.Now()

	// v1.2.4: mark the run active for the memory policy — coordinated
	// cleanup (cache shedding, GC) is deferred while a generation is live,
	// and runs as soon as the run ends (success, error or abort; the defer
	// below is cancellation-aware).
	if s.stack.MemMgr != nil {
		s.stack.MemMgr.TrackRunStart()
	}

	// v1.1.2: release every activity connection parked in standby for
	// this session so they attach to the new run hub immediately.
	s.wakeStandby(sess.ID)

	// runSettled records the terminal outcome exactly once (the goroutine
	// below has several exit paths; every one of them settles the run).
	var settled sync.Once

	outcome := runOutcome{
		RunID:     runID,
		StartedAt: tl.registeredAt,
		Outcome:   "error",
	}

	settle := func(result string, caption string, persisted bool, replyText, reasonText string) {
		settled.Do(func() {
			outcome.EndedAt = time.Now()
			outcome.Outcome = result
			outcome.Caption = caption
			outcome.Persisted = persisted
			outcome.ReplyChars = len(replyText)
			outcome.ReasonChars = len(reasonText)

			// v1.2.7: record the outcome BEFORE the authoritative
			// state flips terminal. This closes the last visibility
			// gap of the v1.2.6 design: a socket that observes the
			// run as terminal (live.snapshot().Running == false,
			// set by settleTerminal below) can then never miss the
			// lastRun block of its idle sentinel — the outcome is
			// already in the bounded registry at that moment.
			// The happens-before edge runs through settleTerminal's
			// mutex, so every reader of the terminal state also
			// observes the recorded outcome.
			if s.outcomes != nil {
				s.outcomes.record(sess.ID, outcome)
			}

			// v1.2.6 continuation: the AUTHORITATIVE terminal
			// state (with the persisted reply snapshots a
			// late-attaching socket replays).
			live.settleTerminal(result, caption, persisted, replyText, reasonText)

			ms := func(a, b time.Time) int64 {
				if a.IsZero() || b.IsZero() || b.Before(a) {
					return 0
				}
				return b.Sub(a).Milliseconds()
			}

			logging.Default().Info(
				"run",
				"runId=%s session=%s outcome=%s persistMs=%d publishMs=%d totalMs=%d replyChars=%d",
				runID, sess.ID, result,
				ms(tl.gateReady, tl.persistedAt),
				ms(tl.acceptedAt, tl.donePublished),
				ms(tl.acceptedAt, outcome.EndedAt),
				len(replyText),
			)
		})
	}

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

		// v1.1.3: gate every run on a real, healthy engine. A cold start
		// here streams the authoritative engine transitions to the UI
		// (starting → ready) while the request waits, bounded by the
		// engine gate timeout.
		// v1.2.6: the gate stages are MEASURED (engineGateStart/Ready) so
		// a stalled run reveals WHERE it is stalled.
		tl.gateStart = time.Now()

		gateCtx, gateCancel := context.WithTimeout(ctx, 3*time.Minute)

		// v1.2.6 continuation: EVERY activity of this run flows through
		// ONE publisher — sequence-stamped (monotonic, per-run) and
		// folded into the authoritative live state BEFORE it reaches
		// any subscriber. The replay contract (snapshot at seq N +
		// events with seq > N) depends on this single path.
		publish := func(a agent.Activity) {
			a.RunID = runID
			a.Seq = live.nextSeq()
			live.observe(a)
			hub.publish(a)
		}

		if err := s.stack.EnsureLLMContext(gateCtx); err != nil {
			gateCancel()

			settle("error", fmt.Sprintf("Engine unavailable: %v", err), false, "", "")

			publish(agent.Activity{
				Type:  "error",
				RunID: runID,
				Caption: fmt.Sprintf(
					"Engine unavailable: %v",
					err,
				),
				Timestamp: time.Now(),
			})

			return
		}

		tl.gateReady = time.Now()

		gateCancel()

		// v1.2.6: every activity of THIS run carries the runId — one
		// identifier from POST /api/run to the last WS frame. The
		// publish wrapper (above) owns the stamping now; stampRun
		// remains for the few direct hub publishes below it.
		stampRun := func(a agent.Activity) agent.Activity {
			a.RunID = runID
			return a
		}

		// terminalCaption captures the orchestrator's own final
		// `done`/`complete` caption ("Completed", "Aborted by user",
		// "Run stopped…") — the honest terminal label, reused verbatim
		// by the outcome record instead of being re-derived.
		terminalCaption := ""

		captureTerminal := func(a agent.Activity) {
			if a.Type == "done" || a.Type == "complete" {
				tl.donePublished = time.Now()

				if a.Caption != "" {
					terminalCaption = a.Caption
				}
			}
		}

		// 1.1.6 §4: the run carries the session's own context
		// policy — the orchestrator plans, gates and sends the
		// request under THIS chat's resolved window.
		res, err := s.orch.RunDetailed(
			ctx,
			messages,
			func(a agent.Activity) {
				publish(a)

				captureTerminal(a)

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

				// v1.1.4: persistence failures surface as a
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
			agent.WithNetSearch(body.NetSearch),
			agent.WithReceivedAt(receivedAt),
			agent.WithSessionSummaryBlock(summaryBlock),
			agent.WithHistoryBlocks(histRefBlocks),
		)

		if err != nil {
			settle("error", err.Error(), false, res.Text, res.Reasoning)

			publish(stampRun(agent.Activity{
				Type:      "error",
				Caption:   err.Error(),
				Timestamp: time.Now(),
			}))

			return
		}

		// v1.2.6: settle the outcome from REAL signals — the run
		// context's cancellation state (abort) and the orchestrator's
		// own terminal caption, never a re-derived guess.
		resultOutcome := "done"
		if ctx.Err() != nil {
			resultOutcome = "aborted"
		}

		if terminalCaption == "" {
			terminalCaption = "Completed"
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
				// v1.1.4: a lost reply is a REAL failure the
				// user must see, not a swallowed error.
				publish(stampRun(agent.Activity{
					Type:      "error",
					Caption:   "The reply was generated but could not be saved to the session: " + err.Error(),
					Timestamp: time.Now(),
				}))

				logging.Default().Error(
					"api",
					"append assistant reply: %v",
					err,
				)

				settle("error", "reply persistence failed: "+err.Error(), false, res.Text, res.Reasoning)

				// v1.2.8.1: the outcome registry records "error"
				// — the run must NOT roll the summary or write
				// the agent.md handoff (which the gate below
				// would still reach because resultOutcome stays
				// "done"). Return here; the deferred cleanup
				// and the settle sync.Once make this safe.
				return
			}
		}

		tl.persistedAt = time.Now()

		// v1.2.9 DURABLE COMPLETION ORDERING. The required
		// invariant is: persist required durable state → finish
		// durable settlement → publish terminal completion.
		// Before this fix, settle() flipped the run terminal
		// (outcome registry + authoritative live state) BEFORE the
		// summary roll, the agent.md handoff, recall indexing and
		// the continuum rollover ran — a crash in that window left
		// a run marked complete while its required durable
		// artifacts were silently missing. Every DURABLE write now
		// happens before the terminal publication below; the
		// post-terminal publishes are UI events only (the WS hub
		// stays open until this goroutine returns).
		//
		// Durable step 1 — rolling session summary: ONE bounded
		// update per settled turn (never a full-transcript
		// re-summarization). The summary is the durable
		// whole-session memory the next turn, the cross-mode
		// picker and the compaction path all consume. Best-effort:
		// failures are logged, never fail the run (the transcript
		// itself stays authoritative) — a missing summary is a
		// visible degradation, not a silently broken invariant.
		if res.Text != "" {
			s.updateSessionSummaryRolling(sess, body.Message, res.Text, res.ToolsUsed)
		}

		// Durable step 2 — mandatory agent.md handoff: EVERY
		// completed agent run updates <workspace>/agent.md so the
		// NEXT agent can pick up the state without reconstructing
		// this transcript. Evidence-free runs write an honest
		// "No engineering changes were made." handoff (see
		// HandoffFromTaskState). Never written for speculative or
		// failed outcomes (the resultOutcome gate). A write
		// FAILURE is surfaced as an error activity — the UI must
		// never believe a handoff exists when it does not.
		// v1.3.3: the write is attempted for EVERY completed agent
		// run — a nil task state is reported by writeAgentHandoff as
		// a real failure instead of silently skipping the mandatory
		// artifact (the previous res.Task != nil gate could settle
		// "done" with no handoff attempt at all).
		handoffWriteErr := error(nil)
		handoffPath := ""

		if sess.Mode == sessions.ModeAgent && resultOutcome == "done" {
			handoffPath, handoffWriteErr = s.writeAgentHandoff(sess, res.Task, resultOutcome)
		}

		// v1.3.3 MANDATORY-HANDOFF HONESTY. For a completed Agent run
		// the agent.md handoff is REQUIRED durable state, not
		// best-effort decoration. A handoff that failed to write means
		// the run did NOT durably complete: settling "done" here would
		// publish a falsely successful terminal result while the next
		// agent starts without its handoff. Demote the terminal
		// outcome to "error" — the same honest class as a
		// reply-persistence failure — and carry the concrete cause in
		// the terminal caption; the post-terminal error activity below
		// still surfaces the write failure to the UI. The successful
		// ordering (summary → handoff → recall → continuum → terminal
		// publication) is unchanged.
		if handoffWriteErr != nil {
			resultOutcome = "error"
			terminalCaption = "agent.md handoff failed: " + handoffWriteErr.Error()
		}

		// Durable step 3 — index the completed exchange into
		// persistent recall.
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

		// Durable step 4 — Continuum chapter rollover: a session at
		// or beyond the pressure threshold is distilled into a
		// fresh chapter session (itself durable state) that
		// carries the framework + recent tail; the UI is told via
		// a `session` activity so it can follow the thread into
		// the new chapter.
		var continuumChild *sessions.Session
		var continuumPrev string

		if s.continuum != nil {
			if fresh, err := s.store.Get(sess.ID); err == nil {
				if s.continuum.ShouldRollover(fresh, s.src.Load()) {
					if child, _, err := s.continuum.Rollover(fresh, s.src.Load()); err == nil {
						continuumChild = child
						continuumPrev = fresh.ID

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

		// Terminal publication — every required durable artifact
		// above is already on disk. A crash AFTER this point can
		// no longer produce a "complete" run with silently
		// missing summary/handoff/recall/continuum state.
		settle(resultOutcome, terminalCaption, res.Text != "", res.Text, res.Reasoning)

		// Post-terminal UI events (non-durable): the handoff
		// outcome and the chapter transition are reported on the
		// activity stream, sequenced by the single publisher.
		if handoffWriteErr != nil {
			publish(stampRun(agent.Activity{
				Type:    "error",
				Caption: "agent.md handoff failed: " + handoffWriteErr.Error(),
				Detail: map[string]any{
					"sessionId": sess.ID,
				},
				Timestamp: time.Now(),
			}))
		} else if handoffPath != "" {
			publish(stampRun(agent.Activity{
				Type:    "handoff",
				Caption: "agent.md handoff updated for the next agent",
				Detail: map[string]any{
					"path":      handoffPath,
					"sessionId": sess.ID,
				},
				Timestamp: time.Now(),
			}))
		}

		if continuumChild != nil {
			publish(agent.Activity{
				Type:    "session",
				Caption: "Context threshold reached — conversation continued in chapter " + fmt.Sprint(continuumChild.Chapter),
				Detail: map[string]any{
					"sessionId": continuumChild.ID,
					"threadId":  continuumChild.ThreadID,
					"chapter":   continuumChild.Chapter,
					"previous":  continuumPrev,
				},
				Timestamp: time.Now(),
			})
		}
	}()

	// v1.2.6 continuation: POST completion must never imply the live
	// transport was attached — but it MUST hand back the authoritative
	// run identity so the client can attribute every subsequent frame
	// (and discard stale ones from an older run of the same session).
	writeJSON(w, map[string]any{
		"ok":        true,
		"sessionId": sess.ID,
		"runId":     runID,
		"state":     "registered",
	})
}

func (s *Server) handleAbort(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}

	// v1.1.4: bounded body AND a decode failure that previously
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
// (v1.1.4). The recall feedback sidecar (SetFeedback / FeedbackFor and
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

// standbyRecheckInterval (v1.2.8.1) bounds how long a standby connection
// can miss a wake signal — it re-checks the runs map itself. The wake
// channel fires for runs that start AFTER the connection entered standby;
// a run that replaced another one in the same instant (old hub closed,
// wakeStandby fired before the new sc existed) is caught by this re-check.
const standbyRecheckInterval = 2 * time.Second

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

	// v1.1.2: the read pump owns every incoming frame. It detects client
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

	idleSentinel := func() map[string]any {
		// v1.2.6: the idle sentinel now carries the AUTHORITATIVE last
		// run outcome (bounded registry). A socket that attaches after
		// a run finished — the exact case that previously left the UI
		// stuck with no visible answer — receives the evidence it needs
		// to finalise: runId, end time, outcome and whether a reply was
		// persisted. Legacy fields stay identical for old clients.
		frame := map[string]any{
			"type":    "idle",
			"caption": "No active run",
		}

		if s.outcomes != nil {
			if last, ok := s.outcomes.latest(sessionID); ok {
				frame["lastRun"] = last
			}
		}

		return frame
	}

	// v1.2.6 continuation: the DETERMINISTIC ATTACH ACKNOWLEDGEMENT.
	// The FIRST frame every socket receives after the protocol upgrade
	// is an explicit `attached` marker — the client waits for THIS frame
	// before POSTing /api/run, so "POST completed" can never silently
	// mean "the live transport was already attached" (the socket may
	// still have been CONNECTING when the POST fired — the v1.2.6 race).
	// The frame also carries the server clock so clients can interpret
	// timestamp deltas honestly.
	_ = conn.WriteJSON(map[string]any{
		"type":      "attached",
		"sessionId": sessionID,
		"serverNow": time.Now().UnixMilli(),
	})

	for {
		// Fast path: a run is already active — attach to its hub.
		s.runsMu.Lock()
		rs, ok := s.runs[sessionID]
		s.runsMu.Unlock()

		// v1.2.7: AUTHORITATIVE LIFECYCLE GATE. Map membership alone
		// is NOT "a run is active": between a run's terminal
		// settlement and its registry cleanup the entry still exists
		// while the run is already authoritatively terminal
		// (live.snapshot().Running == false — the deferred cleanup
		// releases the run budget and the memory-manager run tracking
		// BEFORE it deletes the entry). A socket attaching inside
		// that window previously received a stale terminal
		// run_snapshot and then parked on clientGone without any
		// terminal marker — the exact CI failure of run 35367243405
		// (TestStaleRunEventsFilteredByServer: "expected idle, got
		// run_snapshot"). The authoritative runLive state decides:
		// a terminal entry falls through to the standby path, whose
		// idle sentinel carries the recorded lastRun outcome.
		// runLive remains the ONE lifecycle authority — no second
		// one is introduced here.
		if ok && rs != nil && rs.live != nil && !rs.live.snapshot().Running {
			ok = false
		}

		if ok {
			// v1.2.6 continuation: AUTHORITATIVE REPLAY.
			//
			// Subscribe FIRST, then read the run's authoritative
			// snapshot. Events published between the subscribe and
			// the snapshot read carry seq ≤ snapshot.Sequence
			// (they are already folded into the cumulative
			// snapshots the frame carries) and are filtered below;
			// events published after carry seq > snapshot.Sequence
			// and are forwarded. (snapshot at N) + (events > N)
			// is therefore gapless AND duplicate-free — the
			// response/reasoning captions are cumulative by
			// contract, so the fold is idempotent.
			_, updates, unsubscribe := rs.hub.subscribe()

			snap := rs.live.snapshot()

			snapFrame := map[string]any{
				"type":            "run_snapshot",
				"runId":           snap.RunID,
				"sessionId":       snap.SessionID,
				"phase":           snap.Phase,
				"running":         snap.Running,
				"sequence":        snap.Sequence,
				"startedAt":       snap.StartedAt,
				"latestResponse":  snap.LatestResponse,
				"latestReasoning": snap.LatestReasoning,
				"latestStatus":    snap.LatestStatus,
				"terminalOutcome": snap.TerminalOutcome,
				"persisted":       snap.Persisted,
				"error":           snap.Error,
			}

			// v1.2.8.1 REPAIR: the task state was folded into the
			// authoritative snapshot (runLive.latestTask) but the
			// hand-built wire frame never carried it — a socket
			// reconnecting mid-run restored the streamed text yet
			// lost the whole task panel. The frame now carries the
			// same task block the in-process snapshot holds.
			if snap.Task != nil {
				snapFrame["task"] = snap.Task
			}

			if !snap.EndedAt.IsZero() {
				snapFrame["endedAt"] = snap.EndedAt
			}

			if err := conn.WriteJSON(snapFrame); err != nil {
				unsubscribe()
				return
			}

			served := false

			for ev := range updates {
				// Replay filter: skip events already folded
				// into the snapshot (seq ≤ N) and stale
				// events of an older run that drained after
				// this socket's run replaced it.
				if ev.RunID != "" && ev.RunID != snap.RunID {
					continue
				}

				if ev.Seq > 0 && ev.Seq <= snap.Sequence {
					continue
				}

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
			}

			// v1.2.7: a hub that closed before ANY post-snapshot
			// event existed means everything the run published is
			// already folded into the snapshot this socket holds.
			// The previous behavior parked on clientGone here,
			// stranding such a socket without a terminal marker
			// (the abort path, for one, settles without publishing
			// a terminal activity). Fall through to the idle
			// sentinel — it carries the recorded lastRun outcome —
			// and the standby loop below, which re-checks the runs
			// map and attaches to a replacement run the moment one
			// starts.

			// Run finished: fall through to standby.
			_ = conn.WriteJSON(idleSentinel())
		}

		// Standby: park the connection until a run starts, an engine
		// transition arrives, the client leaves, or a keepalive ping
		// is due.
		sc := s.enterStandby(sessionID)

		_ = conn.WriteJSON(idleSentinel())

		disconnected := false

		for !disconnected {
			select {
			case <-sc.wake:
				disconnected = false
				goto attached

			// v1.2.8.1: periodic runs-map re-check. Closes the
			// missed-wake race where a replacement run registered
			// BETWEEN the old hub's close and this connection
			// entering standby fired wakeStandby() before this sc
			// existed — the socket then parked through the ENTIRE
			// replacement run. The lifecycle gate (a terminal
			// entry falls through) is honored here too.
			case <-time.After(standbyRecheckInterval):
				s.runsMu.Lock()
				rsNow, active := s.runs[sessionID]
				s.runsMu.Unlock()

				if active && rsNow != nil && rsNow.live != nil && rsNow.live.snapshot().Running {
					disconnected = false
					goto attached
				}

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

// equalHistoryRefs (v1.2.8.1) compares two reference lists by value — the
// persisted-context merge is skipped when nothing actually changed, so a
// run never bumps the session's UpdatedAt (and never rewrites the file)
// just because the same refs were sent again.
func equalHistoryRefs(a, b []sessions.HistoryRef) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].SessionID != b[i].SessionID || a[i].Mode != b[i].Mode ||
			a[i].SummaryVersion != b[i].SummaryVersion {
			return false
		}
		if len(a[i].Ranges) != len(b[i].Ranges) {
			return false
		}
		for j := range a[i].Ranges {
			if a[i].Ranges[j] != b[i].Ranges[j] {
				return false
			}
		}
	}
	return true
}

// validateHistoryRefs (v1.2.8.1) applies the SERVER-SIDE cross-mode
// contract to a reference list for one session: normalize (dedup + cap),
// drop self-references, drop same-mode references (the source mode is
// resolved from the store index — the client payload is never trusted)
// and prune dead references (source session vanished). Shared by the run
// path (body refs merged with the persisted context, body-first) and the
// full-context PUT.
//
// v1.2.9: dead references are actually PRUNED. The store index resolves
// every session to a NON-EMPTY mode (NormalizeMode never returns ""), so
// ModeOf(ref) == "" uniquely means "source session no longer exists".
// The previous check only compared a non-empty mode against the active
// mode — a vanished source survived validation and the run path then
// re-persisted it forever (the pruning was claimed but never happened).
// The surviving normalized list is persisted by the callers (run path
// and PUT path both write it back), so the dead reference disappears
// durably after one validated turn.
func (s *Server) validateHistoryRefs(sessionID, activeMode string, refs []histref.Ref) []histref.Ref {
	normalized := histref.NormalizeRefs(refs)

	kept := make([]histref.Ref, 0, len(normalized))
	for _, ref := range normalized {
		if ref.SessionID == sessionID {
			continue
		}

		srcMode := s.store.ModeOf(ref.SessionID)
		if srcMode == "" {
			// v1.2.9: the source session does not exist (deleted, or a
			// foreign id the client invented). A reference to nothing is
			// noise that would otherwise be re-persisted forever — drop
			// it and say so.
			logging.Default().Info(
				"history",
				"pruned dead history reference %s from session %s (source session not found)",
				ref.SessionID, sessionID,
			)
			continue
		}

		if srcMode == activeMode {
			logging.Default().Info(
				"history",
				"dropped same-mode history reference %s from session %s (cross-mode only)",
				ref.SessionID, sessionID,
			)
			continue
		}

		kept = append(kept, ref)
	}

	return kept
}
