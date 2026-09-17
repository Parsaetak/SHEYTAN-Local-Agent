package api

// Engine state surface (v1.1.3Z): the backend engine/process state is the
// single source of truth for the UI. This file exposes it two ways:
//
//   - GET /api/engine — the authoritative snapshot (state, model, detail…)
//   - engine transitions broadcast into every activity WebSocket (both
//     live run hubs and idle standby connections)
//
// The frontend must never invent a state it has not received from here.
//
// v1.1.5Z Phase 1: the snapshot additionally reports WHICH backend serves
// generation (backend) and the supervised native engine's own status
// (native) when the native path is enabled. Since Phase 5 the native
// backend can genuinely serve generation: when it is the SELECTED backend
// and it is actually generation-capable, its state is the authoritative
// `state` for the UI badge (the llama.cpp fallback stays prewarmed in the
// background but must not own the badge while native serves). When the
// native engine is not serving (not selected, or not capable), the
// llama.cpp engine state remains authoritative exactly as before, and
// native transitions reach the activity feed as "Native engine …" captions
// without touching the badge.

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/downloader"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// engineSnapshot is the /api/engine payload.
type engineSnapshot struct {
	State      string   `json:"state"`
	Detail     string   `json:"detail,omitempty"`
	Model      string   `json:"model,omitempty"`
	LoadedPath string   `json:"loadedPath,omitempty"`
	Pid        int      `json:"pid,omitempty"`
	Vision     bool     `json:"vision"`
	Provider   string   `json:"provider"`
	Logs       []string `json:"logs,omitempty"`
	CacheStats any      `json:"cacheStats,omitempty"`
	Timestamp  string   `json:"timestamp"`

	// Backend (v1.1.5Z) names the backend that serves generation
	// ("llama" in Phase 1 — the native engine reports
	// generation-incapable and the selection falls back).
	Backend string `json:"backend"`

	// Native (v1.1.5Z) carries the supervised native engine's status
	// when the native path is enabled (nil otherwise). Purely local
	// reads — the poll path never performs IPC.
	Native *nativeEngineSnapshot `json:"native,omitempty"`

	// 1.1.6 §9/§11 startup experience: a user-facing PHASE for the
	// progress flow (waiting | downloading-engine | loading-model |
	// checking-capabilities | preparing-context | ready) and the
	// verified-readiness proof. Ready requires the process to be
	// alive AND healthy AND the serving model verified — a spawned
	// subprocess alone never reports verified.
	Phase           string `json:"phase"`
	Verified        bool   `json:"verified"`
	VerifiedModel   string `json:"verifiedModel,omitempty"`
	VerifiedContext int    `json:"verifiedContext,omitempty"`
	Degraded        bool   `json:"degraded,omitempty"`

	// v1.2.0: the runtime vision readiness block from the engine's own
	// state machine — loading/ready/degraded/failed with the evidence
	// reason and the paired projector's measured size. Ready is only
	// ever reported because the engine is actually serving with the
	// projector; filenames alone never produce it.
	VisionState          string `json:"visionState,omitempty"`
	VisionReason         string `json:"visionReason,omitempty"`
	VisionProjector      string `json:"visionProjector,omitempty"`
	VisionProjectorName  string `json:"visionProjectorName,omitempty"`
	VisionProjectorBytes int64  `json:"visionProjectorBytes,omitempty"`
	VisionActive         bool   `json:"visionActive,omitempty"`

	// v1.2.3: live asset-download progress (phase, bytes, speed, ETA,
	// source, verification state) from the Download Manager while the
	// engine downloads its llama.cpp archive or a model package. Nil
	// outside downloads.
	Download *downloader.Progress `json:"download,omitempty"`

	// v1.2.5: watchdog auto-restart attempts for the current alive
	// episode (bounded by maxAutoRestarts; reset after a stable healthy
	// episode). Lets the UI report recovery honestly.
	Restarts int `json:"restarts,omitempty"`
}

// nativeEngineSnapshot is the native engine status block (local reads
// only; no IPC on the poll path).
type nativeEngineSnapshot struct {
	Selected      bool    `json:"selected"`
	Available     bool    `json:"available"`
	Path          string  `json:"path,omitempty"`
	State         string  `json:"state"`
	Detail        string  `json:"detail,omitempty"`
	Pid           int     `json:"pid,omitempty"`
	UptimeSeconds float64 `json:"uptimeSeconds,omitempty"`
	Restarts      int     `json:"restarts,omitempty"`
}

// handleEngine serves the authoritative engine snapshot.
func (s *Server) handleEngine(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	writeJSON(w, s.engineSnapshot())
}

func (s *Server) engineSnapshot() engineSnapshot {
	snap := engineSnapshot{
		State:     s.llama.State(),
		Detail:    s.llama.Detail(),
		Pid:       s.llama.Pid(),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		// v1.2.5: the CURRENT episode's watchdog attempts — the UI
		// can show an honest recovery count next to the detail
		// ("Unexpected exit — restarting 1/3").
		Restarts: s.llama.Restarts(),
	}

	// v1.1.5Z: effective generation backend per the selection policy.
	snap.Backend = "llama"

	if s.stack != nil && s.stack.Engine() != nil {
		snap.Backend = s.stack.Engine().Name()
	}

	// Phase 5 repair: when the native engine is the backend actually
	// serving generation (selected AND generation-capable — the same
	// policy that routed generation), the badge state, detail, pid,
	// loaded model and logs must come from THAT engine. Otherwise a
	// user with engineBackend=native whose llama.cpp fallback cannot
	// start (offline, no binary) sees "failed" while generation
	// actually works — the exact misleading-state defect this fixes.
	if snap.Backend == "native" && s.native != nil {
		snap.State = s.native.State()
		snap.Detail = s.native.Detail()
		snap.Pid = s.native.Pid()
		snap.LoadedPath = s.native.NativeModelPath()
		snap.Logs = tailStrings(s.native.Logs(), 24)
	}

	if s.src.Load().IsRemote() {
		snap.Provider = "remote"
		snap.Model = s.src.Load().EffectiveModel()

		if snap.State == llm.StateIdle {
			// A remote provider needs no local engine: report a distinct
			// state so the UI does not show a misleading "stopped" badge
			// for remote mode.
			snap.State = "remote"
		}

		return snap
	}

	snap.Provider = "local"
	snap.Model = s.src.Load().DisplayModel()
	snap.Vision = s.llama.VisionActive()

	// v1.2.3: live download progress for the UI (nil outside downloads).
	if dp := s.llama.DownloadProgress(); dp != nil {
		p := *dp
		snap.Download = &p
	}

	// v1.2.0: full runtime vision readiness block.
	if vs := s.llama.VisionStatus(); vs.State != "" {
		snap.VisionState = string(vs.State)
		snap.VisionReason = vs.Reason
		snap.VisionActive = vs.Active
		if vs.ProjectorName != "" {
			snap.VisionProjector = vs.Projector
			snap.VisionProjectorName = vs.ProjectorName
			snap.VisionProjectorBytes = vs.ProjectorBytes
		}
	}

	// 1.1.6 §11: readiness must mean READY — verified model serving,
	// not merely a spawned subprocess. The phase mapping drives the
	// startup progress states in the UI.
	if snap.Backend != "native" {
		snap.VerifiedModel = s.llama.VerifiedModel()
		snap.VerifiedContext = s.llama.VerifiedContext()
		snap.Verified = s.llama.VerifiedReady() && snap.VerifiedContext > 0
	}
	snap.Phase = enginePhase(snap.State, snap.Verified, s.src.Load().IsRemote())
	snap.Degraded = snap.Backend == "llama" && snap.State == llm.StateReady && !snap.Verified

	// When native serves, LoadedPath/Logs were already sourced from
	// the native engine above; only the llama fallback path (and the
	// pre-native v1.1.4Z contract) populates them from llama.cpp.
	if snap.Backend != "native" {
		snap.LoadedPath = s.llama.LoadedModel()
		snap.Logs = tailStrings(s.llama.Logs(), 24)
	}

	// v1.1.5Z: native engine status block (local reads only).
	if s.native != nil {
		native := &nativeEngineSnapshot{
			Selected:  s.src.Load().NativeBackendEnabled(),
			Available: s.native.Available(),
			Path:      s.native.Path(),
			State:     s.native.State(),
			Detail:    s.native.Detail(),
			Pid:       s.native.Pid(),
			Restarts:  s.native.Restarts(),
		}

		if started := s.native.StartedAt(); !started.IsZero() {
			native.UptimeSeconds = time.Since(started).Seconds()
		}

		snap.Native = native

		// 1.1.6 §11: when the native engine SERVES generation, its
		// loaded state is the readiness proof (its ModelInfo is the
		// verified model + context source).
		if snap.Backend == "native" {
			if res, err := s.native.ModelInfo(context.Background()); err == nil &&
				res.Loaded && res.Model != nil {
				snap.Verified = true
				snap.VerifiedContext = int(res.Model.ContextLength)
			}
			snap.Phase = enginePhase(snap.State, snap.Verified, false)
		}
	}

	if s.stack != nil && s.stack.Cache != nil {
		snap.CacheStats = s.stack.Cache.Stats()
	}

	return snap
}

// enginePhase maps the raw engine state onto the 1.1.6 §9 startup flow
// states the UI renders as real progress:
//
//	Starting → Downloading engine → Loading model →
//	Checking capabilities → Preparing context → Ready
func enginePhase(state string, verified bool, remote bool) string {
	if remote {
		return "ready"
	}

	switch state {
	case llm.StateIdle:
		return "waiting"
	case llm.StateDownloading:
		return "downloading-engine"
	case llm.StateStarting:
		return "loading-model"
	case llm.StateReady, llm.StateRunning, llm.StateBusy:
		if verified {
			return "ready"
		}
		return "checking-capabilities"
	case llm.StateStopping:
		return "stopping"
	case llm.StateStopped, llm.StateFailed:
		return state
	default:
		return state
	}
}

// engineActivity converts one engine transition into an agent.Activity so
// it flows through the normal hub machinery. v1.2.3: live Download Manager
// progress (when present) rides along so WS consumers can render real
// bytes/speed/ETA instead of a bare "downloading" label.
func engineActivity(ev llm.EngineEvent) agent.Activity {
	detail := map[string]any{
		"state":    ev.State,
		"previous": ev.Previous,
		"model":    ev.Model,
		"detail":   ev.Detail,
	}
	if ev.Download != nil {
		detail["download"] = *ev.Download
	}
	return agent.Activity{
		Type:      "engine",
		Caption:   engineCaption(ev.State),
		Timestamp: ev.Timestamp,
		Detail:    detail,
	}
}

// nativeEngineActivity converts one native engine transition; the caption
// is explicitly "Native engine …" so the activity feed distinguishes the
// two engines while the UI badge keeps reading the llama snapshot.
func nativeEngineActivity(ev llm.EngineEvent) agent.Activity {
	act := engineActivity(ev)
	act.Caption = nativeEngineCaption(ev.State)
	return act
}

// watchEngineEvents subscribes to the engine state machines once per
// server and fans transitions out to every live run hub and standby
// connection. v1.1.5Z: it ALSO watches the native engine's transitions
// when the native path is enabled. It returns when stop closes (server
// shutdown).
func (s *Server) watchEngineEvents(stop <-chan struct{}) {
	events, unsubscribe := s.llama.SubscribeEvents()
	defer unsubscribe()

	var nativeEvents <-chan llm.EngineEvent
	var nativeUnsubscribe func()

	if s.native != nil {
		nativeEvents, nativeUnsubscribe = s.native.SubscribeEvents()
		defer nativeUnsubscribe()
	}

	for {
		select {
		case <-stop:
			return

		case ev, ok := <-events:
			if !ok {
				return
			}

			s.broadcastEngineEvent(ev)

		case ev, ok := <-nativeEvents:
			if !ok {
				// Native subscription closed: keep serving
				// llama events.
				nativeEvents = nil
				continue
			}

			act := nativeEngineActivity(ev)

			s.broadcastActivity(act, ev)
		}
	}
}

// broadcastEngineEvent pushes one llama.cpp transition to run hubs and
// standby connections. Standby connections receive the pre-encoded frame
// on their dedicated channel; the standby loop writes it without
// re-parking.
func (s *Server) broadcastEngineEvent(ev llm.EngineEvent) {
	act := engineActivity(ev)
	s.broadcastActivity(act, ev)
}

// broadcastActivity pushes one engine transition activity (shared by both
// engines) to every run hub and standby connection.
func (s *Server) broadcastActivity(act agent.Activity, ev llm.EngineEvent) {
	s.runsMu.Lock()

	for _, rs := range s.runs {
		if rs != nil && rs.hub != nil {
			rs.hub.publish(act)
		}
	}

	s.runsMu.Unlock()

	frame, err := json.Marshal(map[string]any{
		"type":      act.Type,
		"caption":   act.Caption,
		"state":     ev.State,
		"previous":  ev.Previous,
		"model":     ev.Model,
		"detail":    ev.Detail,
		"timestamp": ev.Timestamp,
	})

	if err != nil {
		return
	}

	s.standbyMu.Lock()

	for _, conns := range s.standby {
		for _, sc := range conns {
			select {
			case sc.engineCh <- frame:
			default:
			}
		}
	}

	s.standbyMu.Unlock()
}

// engineCaption renders the human sentence for one engine state.
func engineCaption(state string) string {
	switch state {
	case llm.StateIdle:
		return "Engine idle"
	case llm.StateDownloading:
		return "Downloading llama.cpp engine…"
	case llm.StateStarting:
		return "Starting llama.cpp and loading the model…"
	case llm.StateReady:
		return "Engine ready — model loaded"
	case llm.StateRunning:
		return "Engine running"
	case llm.StateBusy:
		return "Engine busy — inference in flight"
	case llm.StateStopping:
		return "Engine stopping…"
	case llm.StateStopped:
		return "Engine stopped"
	case llm.StateFailed:
		return "Engine failed — see engine logs"
	default:
		return "Engine: " + state
	}
}

// nativeEngineCaption renders the human sentence for one native engine
// state (same state vocabulary; distinct captions for the activity feed).
func nativeEngineCaption(state string) string {
	switch state {
	case llm.StateIdle:
		return "Native engine idle"
	case llm.StateStarting:
		return "Starting native engine…"
	case llm.StateReady:
		return "Native engine ready (supervised)"
	case llm.StateRunning:
		return "Native engine running"
	case llm.StateBusy:
		return "Native engine busy"
	case llm.StateStopping:
		return "Native engine stopping…"
	case llm.StateStopped:
		return "Native engine stopped"
	case llm.StateFailed:
		return "Native engine failed — llama.cpp fallback active"
	default:
		return "Native engine: " + state
	}
}

// tailStrings returns the last n strings, preserving order.
func tailStrings(in []string, n int) []string {
	if len(in) <= n {
		return in
	}

	return in[len(in)-n:]
}
