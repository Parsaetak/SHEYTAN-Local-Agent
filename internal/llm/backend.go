package llm

// engine backend contract (v1.1.5 Phase 1 — SHEYTAN Native AI Engine
// architecture).
//
// Backend is the engine abstraction the rest of SHEYTAN programs against.
// Before v1.1.5 every consumer reached directly for the two concrete
// pieces: LlamaServer (subprocess lifecycle) and Client (OpenAI-compatible
// generation). The backend contract formalizes that surface so the managed
// llama.cpp engine and the future SHEYTAN native engine are
// interchangeable behind one interface:
//
//      React/TypeScript → Wails → Go Core → SHEYTAN Native API → C++ Native Engine
//                                          ↘ Backend contract (this file)
//
// Phase 1 status (do not overstate):
//
//      llama   — full implementation: lifecycle + generation, the default
//                engine and the fallback for every generation request.
//      native  — lifecycle, health, hardware and metrics only. Generate /
//                StreamGenerate / LoadModel / ModelInfo return ErrNotImplemented
//                until later phases; callers fall back to the llama backend.
//
// The wire types reused by this contract (Message, ChatRequest,
// ChatResponse, StreamEvent, PerfStats, ToolSpec) are the same ones the
// llama client already speaks; no duplicate request model exists.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// ErrNotImplemented reports a backend operation that exists in the contract
// but is not implemented by the selected backend yet (native engine Phase 1
// generation surface). Callers must treat this as a signal to fall back to
// the llama backend, never as a crash.
var ErrNotImplemented = errors.New("backend operation not implemented in this phase")

// ErrCancelContextBased reports a backend whose generation cancellation is
// context-based only: abort the request context instead of calling Cancel.
var ErrCancelContextBased = errors.New("backend cancellation is context-based; abort the request context")

// Backend is the engine abstraction SHEYTAN depends on (v1.1.5).
//
// Implementations:
//   - *LlamaBackend   (this package) — managed llama.cpp engine
//   - *engine.Backend (internal/native/engine) — SHEYTAN native engine
//
// Contract rules:
//
//   - Start/Stop are serialized by the implementation; concurrent callers
//     must be safe.
//   - Health performs a REAL probe (a round-trip to the engine), unlike
//     State()/IsAlive() which are cached-state reads.
//   - Generation methods must honor ctx cancellation — streaming must stop
//     promptly when the context dies.
//   - Metrics only reports values the backend actually measures; absent
//     fields are omitted, never faked.
//   - Methods are safe for concurrent use.
type Backend interface {
	// Name returns the backend kind (config.BackendLlama / config.BackendNative).
	Name() string

	// Start boots the engine if it is not already alive. Idempotent: a
	// second Start while alive (or starting) returns nil.
	Start(ctx context.Context) error

	// Stop shuts the engine down gracefully with a bounded grace period.
	Stop(ctx context.Context) error

	// Health actively probes the engine and reports liveness.
	Health(ctx context.Context) (HealthReport, error)

	// LoadModel makes the engine serve the given model. The llama backend
	// persists the model and relaunches the engine (llama.cpp loads the
	// model at process start); the native engine returns ErrNotImplemented
	// until its model loader exists.
	LoadModel(ctx context.Context, spec ModelSpec) error

	// UnloadModel releases the loaded model. llama.cpp can only unload by
	// stopping, so the llama backend stops the engine.
	UnloadModel(ctx context.Context) error

	// Generate performs a non-streaming completion.
	Generate(ctx context.Context, req *ChatRequest) (*ChatResponse, error)

	// StreamGenerate performs a streaming completion, invoking onEvent for
	// every decoded chunk until the stream finishes or ctx is canceled.
	StreamGenerate(ctx context.Context, req *ChatRequest, onEvent func(StreamEvent) error) (PerfStats, error)

	// Cancel requests cooperative cancellation of one in-flight generation
	// request. Backends whose cancellation is purely context-based return
	// ErrCancelContextBased.
	Cancel(ctx context.Context, requestID string) error

	// ModelInfo reports the model currently known to the backend.
	ModelInfo(ctx context.Context) (ModelInfo, error)

	// HardwareInfo reports the backend's view of the execution hardware.
	HardwareInfo(ctx context.Context) (HardwareInfo, error)

	// Metrics reports engine metrics for values that are actually measured.
	Metrics(ctx context.Context) (Metrics, error)
}

// GenerationCapable is the optional capability probe used by backend
// selection. A backend that does not implement it is treated as
// generation-capable; the native backend implements it and reports false
// until its generation path exists.
type GenerationCapable interface {
	GenerationCapable() bool
}

// SelectGenerationBackend returns the backend that must serve a generation
// request: the native engine when the user selected it AND it can actually
// generate, otherwise the llama.cpp fallback.
//
// This is the single selection point for the engine routing — the same
// policy backs runtime.Stack.Engine() and the /api/engine snapshot. Phase 1
// always resolves to the llama backend because the native engine reports
// GenerationCapable() == false; the function exists and is pinned by tests
// so later phases flip the routing by implementing generation, not by
// editing call sites.
func SelectGenerationBackend(cfg *config.Config, native, fallback Backend) Backend {
	if cfg != nil && cfg.NativeBackendEnabled() && native != nil {
		if gc, ok := native.(GenerationCapable); ok && !gc.GenerationCapable() {
			return fallback
		}
		return native
	}
	return fallback
}

// HealthReport is the result of an active health probe.
type HealthReport struct {
	State     string    `json:"state"`
	Alive     bool      `json:"alive"`
	Detail    string    `json:"detail,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// ModelSpec names the model a backend should load.
type ModelSpec struct {
	// Path is the model file path or model id understood by the backend.
	Path string `json:"path,omitempty"`
}

// ModelInfo describes the model state of one backend. Only fields the
// backend can actually determine are populated.
//
// Phase 2 additions (native engine GGUF loading): State, FileSizeBytes,
// TensorCount, VocabSize, EmbeddingLength, LayerCount, GGUFVersion and
// the memory-plan estimates. They are additive optional fields — the
// llama backend leaves them zero, so the shared contract stays
// backward-compatible.
type ModelInfo struct {
	Backend string `json:"backend"`

	// Loaded reports whether a model is currently loaded/served.
	Loaded bool `json:"loaded"`

	// ModelPath is the resolved model path (when known).
	ModelPath string `json:"modelPath,omitempty"`

	// LoadedIDs lists model ids the engine reports as loaded.
	LoadedIDs []string `json:"loadedIds,omitempty"`

	// GGUF card facts, when a local file can be parsed.
	Architecture  string `json:"architecture,omitempty"`
	Quantization  string `json:"quantization,omitempty"`
	Parameters    string `json:"parameters,omitempty"`
	ContextLength int    `json:"contextLength,omitempty"`

	// Native-engine lifecycle state of the model (unloaded / loading /
	// loaded / failed). Empty on backends without a model state.
	State string `json:"state,omitempty"`

	// File facts (native engine, read from the GGUF header).
	FileSizeBytes uint64 `json:"fileSizeBytes,omitempty"`
	TensorCount   int    `json:"tensorCount,omitempty"`
	GGUFVersion   int    `json:"ggufVersion,omitempty"`

	// Architecture facts (native engine; zero when not present in the
	// file — never guessed).
	VocabSize       int `json:"vocabSize,omitempty"`
	EmbeddingLength int `json:"embeddingLength,omitempty"`
	LayerCount      int `json:"layerCount,omitempty"`

	// Memory-plan estimates from the native loader (planned, not
	// allocated).
	KVCacheEstimateBytes     uint64 `json:"kvCacheEstimateBytes,omitempty"`
	WorkspaceEstimateBytes   uint64 `json:"workspaceEstimateBytes,omitempty"`
	TotalMemoryEstimateBytes uint64 `json:"totalMemoryEstimateBytes,omitempty"`
}

// HardwareInfo is the platform-neutral hardware profile (v1.1.5).
//
// It is capable of representing CPU, RAM, GPUs (discrete VRAM or shared /
// unified memory), NPU-class AI accelerators, the machine architecture and
// the producing backend. Values are filled ONLY from real probes
// (sysinfo on the Go side, the native engine's own detection on the C++
// side); nothing is hardcoded per vendor. Fields with no detector today
// (accelerators, shared-memory GPUs) stay empty until a probe exists.
type HardwareInfo struct {
	Backend string `json:"backend,omitempty"`

	// Architecture is the CPU architecture (amd64, arm64, ...).
	Architecture string `json:"architecture,omitempty"`
	OS           string `json:"os,omitempty"`

	CPU CPUHardware `json:"cpu"`
	RAM RAMHardware `json:"ram"`

	// GPUs lists detected graphics devices. SharedMemory is true for
	// unified/shared-memory devices (e.g. console-style APUs) — set only
	// when the probe can tell.
	GPUs []GPUHardware `json:"gpus,omitempty"`

	// Accelerators lists NPU/AI-accelerator devices; empty until a real
	// detector exists.
	Accelerators []AcceleratorHardware `json:"accelerators,omitempty"`

	// DetectedBy names the probes that produced this profile (e.g.
	// "sysinfo", "shtn-engine-host").
	DetectedBy []string `json:"detectedBy,omitempty"`
}

// CPUHardware is the CPU portion of the hardware profile.
type CPUHardware struct {
	Name          string `json:"name,omitempty"`
	PhysicalCores int    `json:"physicalCores,omitempty"`
	LogicalCores  int    `json:"logicalCores,omitempty"`
	FrequencyMHz  int    `json:"frequencyMHz,omitempty"`
}

// RAMHardware is the memory portion of the hardware profile.
type RAMHardware struct {
	TotalBytes     uint64 `json:"totalBytes,omitempty"`
	AvailableBytes uint64 `json:"availableBytes,omitempty"`
}

// GPUHardware is one graphics device.
type GPUHardware struct {
	Vendor        string `json:"vendor,omitempty"`
	Name          string `json:"name,omitempty"`
	VRAMBytes     uint64 `json:"vramBytes,omitempty"`
	SharedMemory  bool   `json:"sharedMemory,omitempty"`
	DriverVersion string `json:"driverVersion,omitempty"`
}

// AcceleratorHardware is one NPU/AI accelerator device.
type AcceleratorHardware struct {
	Vendor      string `json:"vendor,omitempty"`
	Name        string `json:"name,omitempty"`
	Kind        string `json:"kind,omitempty"` // npu, tpu, dsp, other
	MemoryBytes uint64 `json:"memoryBytes,omitempty"`
}

// Metrics is the engine metrics snapshot (v1.1.5).
//
// The struct is capable of tracking engine state, model, TTFT, prompt
// processing, decode speed, active requests and engine process memory.
// RULE: only measured values are populated — absent fields are omitted,
// never zero-filled to look like a measurement. The llama backend reports
// lifecycle facts it measures (state, model, pid, uptime, restarts); the
// native engine additionally reports its process RSS from the C++ side.
// Generation-time metrics become measurable when a backend actually serves
// generation.
type Metrics struct {
	Backend     string `json:"backend"`
	EngineState string `json:"engineState"`
	Model       string `json:"model,omitempty"`
	Pid         int    `json:"pid,omitempty"`

	// UptimeSeconds is measured from the engine's last successful start.
	UptimeSeconds float64 `json:"uptimeSeconds,omitempty"`

	// Restarts counts auto-restarts in the current alive episode.
	Restarts int `json:"restarts,omitempty"`

	// ActiveRequests is the number of in-flight generation requests.
	ActiveRequests int `json:"activeRequests,omitempty"`

	// Generation metrics — only set when the backend measures a request.
	TTFTSeconds           float64 `json:"ttftSeconds,omitempty"`
	PromptTokensPerSecond float64 `json:"promptTokensPerSecond,omitempty"`
	DecodeTokensPerSecond float64 `json:"decodeTokensPerSecond,omitempty"`

	// ProcessRSSBytes is the engine process resident set, when measured.
	ProcessRSSBytes uint64 `json:"processRssBytes,omitempty"`
}

// String renders a compact one-line summary for logs and diagnostics.
func (m Metrics) String() string {
	if m == (Metrics{}) {
		return "metrics: (empty)"
	}

	s := fmt.Sprintf(
		"backend=%s state=%s",
		m.Backend,
		m.EngineState,
	)

	if m.Model != "" {
		s += " model=" + m.Model
	}

	if m.Pid != 0 {
		s += fmt.Sprintf(" pid=%d", m.Pid)
	}

	if m.UptimeSeconds > 0 {
		s += fmt.Sprintf(" up=%.0fs", m.UptimeSeconds)
	}

	if m.Restarts > 0 {
		s += fmt.Sprintf(" restarts=%d", m.Restarts)
	}

	if m.ActiveRequests > 0 {
		s += fmt.Sprintf(" active=%d", m.ActiveRequests)
	}

	if m.DecodeTokensPerSecond > 0 {
		s += fmt.Sprintf(" decode=%.1ft/s", m.DecodeTokensPerSecond)
	}

	if m.ProcessRSSBytes > 0 {
		s += fmt.Sprintf(" rss=%dB", m.ProcessRSSBytes)
	}

	return s
}
