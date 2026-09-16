package api

// perf.go — v1.1.7 compact live performance surface.
//
// GET /api/perf — ONE endpoint that reports only REAL, measured values for
// the Settings → Performance view:
//
//   - CPU / RAM sampled live from the OS (cheap, bounded probes);
//   - GPU / VRAM via nvidia-smi when that tool exists (N/A otherwise);
//   - prompt tok/s, generation tok/s and TTFT from the streaming client's
//     own per-request measurements (llm.SnapshotEnginePerf — no sample yet
//     means the UI renders N/A, never a fabricated number);
//   - context usage from the last measured generation vs the engine's
//     verified window;
//   - the ACTIVE engine profile (compat level, recorded reason, removed
//     options) — the "why is my engine in compatibility mode" answer;
//   - recommended settings derived from detected hardware + model +
//     engine capability (never silently applied — the UI asks first).
//
// This is deliberately NOT a new telemetry architecture: every value comes
// from infrastructure that already existed (sysinfo probe, streaming
// tokenTimer, capability profile, model card reader).

import (
	"context"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/contextcache"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/ctxtelemetry"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/sysinfo"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/tools"
)

// gpuSampleInterval bounds how often nvidia-smi may run.
const gpuSampleInterval = 5 * time.Second

// perfHandler caches slightly-expensive samples so concurrent UI polls stay
// lightweight (CPU needs two delta samples; GPU shells out to nvidia-smi).
type perfHandler struct {
	mu          sync.Mutex
	cpuKnown    bool
	lastIdle    float64
	lastBusy    float64
	gpuMu       sync.Mutex
	lastGPUAt   time.Time
	gpuKnown    bool
	gpuPercent  float64
	gpuMemUsed  float64
	gpuMemTotal float64
}

var perfState = &perfHandler{}

// sampleCPU returns the average CPU busy percentage since the previous
// call (a real delta over the poll interval, not an invented snapshot).
// The first call only primes the baseline and reports not-known.
func (p *perfHandler) sampleCPU() (float64, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	idle, busy, ok := cpuTimesSnapshot()
	if !ok {
		return 0, false
	}

	if !p.cpuKnown {
		p.lastIdle, p.lastBusy, p.cpuKnown = idle, busy, true
		return 0, false
	}

	dIdle := idle - p.lastIdle
	dBusy := busy - p.lastBusy
	p.lastIdle, p.lastBusy = idle, busy

	total := dIdle + dBusy
	if total <= 0 {
		return 0, false
	}

	return 100 * dBusy / total, true
}

// sampleGPU runs nvidia-smi (when present) for GPU utilisation and memory.
// Any failure leaves the cache empty and the UI shows N/A — never a guess.
func (p *perfHandler) sampleGPU() (pct, memUsed, memTotal float64, ok bool) {
	p.gpuMu.Lock()
	defer p.gpuMu.Unlock()

	if p.gpuKnown && time.Since(p.lastGPUAt) < gpuSampleInterval {
		return p.gpuPercent, p.gpuMemUsed, p.gpuMemTotal, true
	}

	bin, err := exec.LookPath("nvidia-smi")
	if err != nil {
		return 0, 0, 0, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), gpuSampleInterval)
	defer cancel()

	out, err := exec.CommandContext(ctx, bin,
		"--query-gpu=utilization.gpu,memory.used,memory.total",
		"--format=csv,noheader,nounits",
	).Output()
	if err != nil || len(out) == 0 {
		return 0, 0, 0, false
	}

	// First GPU line: "34, 5120, 8192"
	fields := strings.Split(strings.SplitN(string(out), "\n", 2)[0], ",")
	if len(fields) < 3 {
		return 0, 0, 0, false
	}

	pct, err1 := strconv.ParseFloat(strings.TrimSpace(fields[0]), 64)
	used, err2 := strconv.ParseFloat(strings.TrimSpace(fields[1]), 64)
	total, err3 := strconv.ParseFloat(strings.TrimSpace(fields[2]), 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, 0, false
	}

	p.gpuPercent, p.gpuMemUsed, p.gpuMemTotal = pct, used, total
	p.gpuKnown, p.lastGPUAt = true, time.Now()
	return pct, used, total, true
}

// perfPayload is the /api/perf response. Pointer fields are nil when the
// value is genuinely not measurable — the UI renders "N/A" for those.
type perfPayload struct {
	GeneratedAt string `json:"generatedAt"`

	// Hardware (live where measurable).
	CPUPercent  *float64       `json:"cpuPercent"`
	GPUPercent  *float64       `json:"gpuPercent"`
	RAM         *ramSample     `json:"ram"`
	VRAM        *vramSample    `json:"vram"`
	Backend     string         `json:"backend"`
	EngineState string         `json:"engineState"`
	Model       string         `json:"model,omitempty"`
	Compat      llm.CompatInfo `json:"compat"`

	// Generation telemetry (real measurements from the streaming client).
	Gen EnginePerfAPI `json:"generation"`

	// Context usage of the last measured generation.
	Context *contextUsage `json:"context,omitempty"`

	// v1.2.4: runtime memory-policy telemetry — allocations/reclaims made
	// measurable (heap before/after last cleanup, bytes freed, cache trims,
	// bounded-capture counters, cache hit ratios).
	Memory *memoryStatsAPI `json:"memory,omitempty"`

	// Recommended settings (apply is always explicit, never automatic).
	Recommended *recommendedSettings `json:"recommended,omitempty"`
}

// memoryStatsAPI is the wire form of the coordinated memory telemetry.
type memoryStatsAPI struct {
	HeapAllocBytes    uint64 `json:"heapAllocBytes"`
	HeapInuseBytes    uint64 `json:"heapInuseBytes"`
	SysBytes          uint64 `json:"sysBytes"`
	ActiveRuns        int64  `json:"activeRuns"`
	CleanupsRun       uint64 `json:"cleanupsRun"`
	PressureCleanups  uint64 `json:"pressureCleanups"`
	LastCleanupAt     string `json:"lastCleanupAt,omitempty"`
	LastCleanupReason string `json:"lastCleanupReason,omitempty"`
	LastCleanupDurMs  int64  `json:"lastCleanupDurMs"`
	LastFreedBytes    int64  `json:"lastFreedBytes"`
	FreedBytesTotal   int64  `json:"freedBytesTotal"`
	HeapBeforeLast    uint64 `json:"heapBeforeLast,omitempty"`
	HeapAfterLast     uint64 `json:"heapAfterLast,omitempty"`

	// Data-path telemetry (v1.2.4): bytes moved vs bytes avoided.
	ContextCache *contextcache.Stats `json:"contextCache,omitempty"`
	ToolCapture  *tools.CaptureStats `json:"toolCapture,omitempty"`
	CtxTelemetry *ctxtelemetry.Stats `json:"ctxTelemetry,omitempty"`
	MemoryDedup  uint64              `json:"memoryDuplicatesSkipped,omitempty"`
}

// EnginePerfAPI mirrors llm.EnginePerfSnapshot for the wire (keeps the llm
// package free of API naming concerns).
type EnginePerfAPI struct {
	HasSample        bool    `json:"hasSample"`
	GenTokPerSec     float64 `json:"genTokPerSec,omitempty"`
	PromptTokPerSec  float64 `json:"promptTokPerSec,omitempty"`
	TTFTSeconds      float64 `json:"ttftSeconds,omitempty"`
	PromptTokens     int     `json:"promptTokens,omitempty"`
	Tokens           int     `json:"tokens,omitempty"`
	SamplesAvailable int     `json:"samplesAvailable"`
}

type ramSample struct {
	TotalBytes     uint64  `json:"totalBytes"`
	AvailableBytes uint64  `json:"availableBytes"`
	UsedBytes      uint64  `json:"usedBytes"`
	UsedPercent    float64 `json:"usedPercent"`
}

type vramSample struct {
	TotalMB float64 `json:"totalMb"`
	UsedMB  float64 `json:"usedMb"`
	Percent float64 `json:"percent,omitempty"`
	Source  string  `json:"source"`
}

type contextUsage struct {
	UsedTokens int     `json:"usedTokens"`
	Total      int     `json:"total"`
	Percent    float64 `json:"percent,omitempty"`
	Source     string  `json:"source,omitempty"`
}

type recommendedSettings struct {
	Threads        int      `json:"threads"`
	ThreadsBatch   int      `json:"threadsBatch"`
	GPULayers      int      `json:"gpuLayers"`
	GPUAutoOffload bool     `json:"gpuAutoOffload"`
	FlashAttention bool     `json:"flashAttention"`
	FlashAttnState string   `json:"flashAttnState"` // Supported | Unsupported
	UBatchSize     int      `json:"ubatchSize"`
	CacheReuse     int      `json:"cacheReuse"`
	Context        int      `json:"context"`
	Notes          []string `json:"notes,omitempty"`
}

// handlePerf serves GET /api/perf.
func (s *Server) handlePerf(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, errMethodNotAllowed())
		return
	}

	writeJSON(w, s.buildPerfPayload(llm.SnapshotEnginePerf()))
}

// buildPerfPayload assembles the measured-only performance payload.
func (s *Server) buildPerfPayload(perf llm.EnginePerfSnapshot) perfPayload {
	payload := perfPayload{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Backend + state + model (honest: from the authoritative snapshot).
	engineSnap := s.engineSnapshot()
	payload.Backend = engineSnap.Backend
	payload.EngineState = engineSnap.State
	payload.Model = engineSnap.Model
	payload.Compat = s.llama.CompatSnapshot()

	// CPU: live delta sample (nil until two samples exist).
	if pct, ok := perfState.sampleCPU(); ok {
		v := pct
		payload.CPUPercent = &v
	}

	// RAM: cheap OS-native read (no CIM round-trip).
	if ram, ok := sampleRAM(); ok {
		payload.RAM = &ram
	}

	// GPU / VRAM: nvidia-smi when present.
	if pct, used, total, ok := perfState.sampleGPU(); ok {
		v := pct
		payload.GPUPercent = &v
		payload.VRAM = &vramSample{
			TotalMB: total, UsedMB: used, Source: "nvidia-smi",
		}
		if total > 0 {
			payload.VRAM.Percent = 100 * used / total
		}
	}

	// Generation telemetry — pass through the real measurements.
	payload.Gen = EnginePerfAPI{
		HasSample:        perf.HasSample,
		GenTokPerSec:     perf.GenTokPerSec,
		PromptTokPerSec:  perf.PromptTokPerSec,
		TTFTSeconds:      perf.TTFTSeconds,
		PromptTokens:     perf.PromptTokens,
		Tokens:           perf.Tokens,
		SamplesAvailable: perf.SamplesAvailable,
	}

	// Context usage: last measured prompt vs the engine's verified window.
	if perf.HasSample && perf.PromptTokens > 0 {
		total := s.llama.VerifiedContext()
		if total <= 0 {
			total = s.src.Load().LLM.NumCtx
		}
		if total > 0 {
			payload.Context = &contextUsage{
				UsedTokens: perf.PromptTokens,
				Total:      total,
				Source:     "last generation",
			}
			payload.Context.Percent = 100 * float64(perf.PromptTokens) / float64(total)
		}
	}

	// Recommended settings from detected hardware + model + capability.
	payload.Recommended = s.buildRecommended(engineSnap.LoadedPath)

	// v1.2.4: coordinated memory telemetry (nil-safe for tests).
	payload.Memory = s.buildMemoryStats()

	return payload
}

// buildMemoryStats assembles the runtime memory-policy snapshot plus the
// data-path counters (bytes avoided by cache hits and deduplication) from
// the components that own them.
func (s *Server) buildMemoryStats() *memoryStatsAPI {
	if s.stack == nil || s.stack.MemMgr == nil {
		return nil
	}
	ms := s.stack.MemMgr.Stats()
	out := &memoryStatsAPI{
		HeapAllocBytes:    ms.HeapAllocBytes,
		HeapInuseBytes:    ms.HeapInuseBytes,
		SysBytes:          ms.SysBytes,
		ActiveRuns:        ms.ActiveRuns,
		CleanupsRun:       ms.CleanupsRun,
		PressureCleanups:  ms.PressureCleanups,
		LastCleanupAt:     ms.LastCleanupAt,
		LastCleanupReason: ms.LastCleanupReason,
		LastCleanupDurMs:  ms.LastCleanupDurMs,
		LastFreedBytes:    ms.LastFreedBytes,
		FreedBytesTotal:   ms.FreedBytesTotal,
		HeapBeforeLast:    ms.HeapBeforeLast,
		HeapAfterLast:     ms.HeapAfterLast,
	}
	if s.stack.Cache != nil {
		cs := s.stack.Cache.Stats()
		out.ContextCache = &cs
	}
	tc := tools.GetCaptureStats()
	out.ToolCapture = &tc
	if s.stack.Telemetry != nil {
		ts := s.stack.Telemetry.Stats()
		out.CtxTelemetry = &ts
	}
	if s.stack.Mem != nil {
		out.MemoryDedup = s.stack.Mem.DuplicatesSkipped()
	}
	return out
}

// buildRecommended derives settings advice from measurable inputs only:
// the sysinfo hardware probe, the loaded model's GGUF card and the
// verified engine capability profile. Nothing here mutates configuration —
// the UI applies values explicitly.
func (s *Server) buildRecommended(loadedModelPath string) *recommendedSettings {
	si := sysinfo.Probe()
	if si == nil {
		return nil
	}

	cfg := s.src.Load()

	rec := &recommendedSettings{
		Threads:        si.Recommended.NumThread,
		GPUAutoOffload: cfg.GPUAutoOffload,
		Context:        si.Recommended.NumCtx,
	}

	if si.Recommended.NumBatch > 0 {
		rec.UBatchSize = si.Recommended.NumBatch
	}

	if gen, batch := sysinfo.RecommendThreads(); gen > 0 {
		if rec.Threads == 0 {
			rec.Threads = gen
		}
		if rec.ThreadsBatch == 0 {
			rec.ThreadsBatch = batch
		}
	}

	// GPU advice: auto-offload is the right posture when a GPU with
	// measurable VRAM exists; CPU-only machines should keep it off.
	if len(si.GPU) > 0 && si.GPU[0].VRAMBytes > 0 {
		rec.GPUAutoOffload = true
		if si.Recommended.NumGPU > 0 {
			rec.GPULayers = si.Recommended.NumGPU
		}
	} else if len(si.GPU) == 0 {
		rec.GPUAutoOffload = false
		rec.GPULayers = 0
		rec.Notes = append(rec.Notes,
			"No dedicated GPU detected — run on CPU with GPU offload disabled.")
	}

	// Flash attention: capability-aware, never assumed.
	caps := s.llama.CapsSnapshot()
	if caps != nil && caps.FlashAttnEnabled {
		rec.FlashAttention = true
		rec.FlashAttnState = "Supported"
	} else {
		rec.FlashAttention = false
		rec.FlashAttnState = "Unsupported"
		rec.Notes = append(rec.Notes,
			"This engine build does not support --flash-attn — keep it off.")
	}

	// Cache reuse: keep the effective engine default when supported.
	if caps != nil && caps.CacheReuse {
		rec.CacheReuse = cfg.EffectiveCacheReuse()
	}

	// Context: never exceed the loaded model's own training limit.
	if loadedModelPath != "" {
		if card, err := llm.ReadModelCard(loadedModelPath); err == nil &&
			card != nil && card.ContextLength > 0 && rec.Context > card.ContextLength {
			rec.Context = card.ContextLength
			rec.Notes = append(rec.Notes,
				"Context clamped to the loaded model's training limit.")
		}
	}

	return rec
}
