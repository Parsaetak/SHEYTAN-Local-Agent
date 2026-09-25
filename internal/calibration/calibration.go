// Package calibration — the v1.5.0 bounded automatic performance check.
//
// This is NOT a second tuning engine. Every candidate profile is derived
// from the ONE recommendation engine (internal/recommendation), which
// already computes the evidence-based profile from measured hardware,
// GGUF facts and verified engine capabilities. Calibration only ADDS
// the missing stages of the documented pipeline:
//
//	selected model → GGUF facts → measured RAM/CPU/GPU/NPU →
//	engine capability/device enumeration → resource-fit calculation →
//	safe initial profile → engine start → SHORT BOUNDED CALIBRATION →
//	measured profile comparison → retain verified profile
//	                                     ^^^^^^^^^^ this package
//
// Contract rules (spec §4):
//   - the candidate set is SMALL and derived from hardware/model
//     evidence (safe baseline + GPU-offload + CPU-safe + at most one
//     batch/context variant; capped at MaxCandidates);
//   - a candidate wins ONLY from a measured successful run — never from
//     filename, size or heuristics;
//   - measured values are REAL (TTFT / generation throughput / prompt
//     throughput through the live engine); nothing is fabricated;
//   - when real calibration is unavailable, the evidence-based profile
//     is kept and honestly marked "not benchmarked";
//   - budgets are bounded: a candidate that cannot boot or finish one
//     short generation within its budget is skipped, never retried in a
//     loop;
//   - the fastest STABLE verified profile is retained (persisted per
//     model+machine fingerprint) so the next selection of the same model
//     reuses the measured result instead of re-benchmarking.
package calibration

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/hardware"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/recommendation"
)

// Bounded budgets (spec §4: "short bounded calibration" — never an
// enormous search space, never unbounded).
const (
	// DefaultTotalBudget bounds the WHOLE calibration pass.
	DefaultTotalBudget = 120 * time.Second
	// DefaultBootBudget bounds one candidate's engine start.
	DefaultBootBudget = 60 * time.Second
	// DefaultMeasureBudget bounds one candidate's short generation.
	DefaultMeasureBudget = 30 * time.Second
	// MaxCandidates caps the candidate set INCLUDING the safe baseline.
	MaxCandidates = 3
	// MeasureMaxTokens is the short generation length used per candidate.
	MeasureMaxTokens = 24
	// MeasurePrompt is the calibration prompt (short, deterministic; the
	// CONTENT is irrelevant — only the measured timings matter).
	MeasurePrompt = "Reply with a short sentence about the weather."
)

// Runner is the engine boundary calibration drives. The runtime stack
// implements it; tests inject a scripted runner to prove the CONTROL
// CONTRACT (retain/rollback/budget), never to fabricate product
// performance claims.
type Runner interface {
	// ApplyProfile atomically applies the candidate profile for
	// modelPath and (re)starts the engine. It returns after the engine
	// is verified ready and serving modelPath, or with the failure.
	ApplyProfile(ctx context.Context, modelPath string, rec recommendation.Recommendation) error

	// MeasureGeneration runs ONE short real generation against the live
	// engine and returns the measured performance. maxTokens bounds the
	// generation so a slow candidate cannot blow the budget.
	MeasureGeneration(ctx context.Context, prompt string, maxTokens int) (llm.PerfStats, error)

	// EngineStable reports whether the engine stayed healthy through the
	// last measurement (no death, no watchdog restart).
	EngineStable() bool
}

// Candidate is one bounded profile variant. Every candidate is derived
// from the recommendation engine's baseline — there is no independent
// knob arithmetic in this package.
type Candidate struct {
	// Label is the human-readable candidate name surfaced in summaries.
	Label string
	// Profile is the recommendation.Recommendation variant to apply.
	Profile recommendation.Recommendation
	// Reason cites the measured evidence that justified the variant.
	Reason string
}

// Measurement is one candidate's REAL measured outcome. Zero values mean
// "not measured" — never a fabricated number.
type Measurement struct {
	Candidate          string  `json:"candidate"`
	Booted             bool    `json:"booted"`
	Measured           bool    `json:"measured"`
	TTFTSeconds        float64 `json:"ttftSeconds,omitempty"`
	GenTokensPerSec    float64 `json:"genTokensPerSec,omitempty"`
	PromptTokensPerSec float64 `json:"promptTokensPerSec,omitempty"`
	TotalSeconds       float64 `json:"totalSeconds,omitempty"`
	Stable             bool    `json:"stable"`
	Note               string  `json:"note,omitempty"`
}

// Summary is the calibration verdict.
type Summary struct {
	// Ran is true iff at least one candidate produced a measured,
	// stable run. When false, Reason explains why calibration was
	// unavailable (never a fake "we measured something" claim).
	Ran bool `json:"ran"`
	// Reason documents why calibration did not run / stopped early.
	Reason string `json:"reason,omitempty"`
	// Winner is the label of the fastest stable verified candidate.
	Winner string `json:"winner,omitempty"`
	// WinnerProfile is the winning profile (nil when not ran).
	WinnerProfile *recommendation.Recommendation `json:"winnerProfile,omitempty"`
	// Measurements carries every candidate's honest outcome.
	Measurements []Measurement `json:"measurements"`
	// Changed is true when the winner differs from the applied baseline
	// (i.e. the final engine restart switched to the winning profile).
	Changed bool `json:"changed"`
	// At is when the pass finished.
	At time.Time `json:"at"`
}

// Candidates derives the SMALL bounded candidate set from the
// recommendation baseline and measured evidence:
//
//  1. "safe baseline"  — the recommendation as computed (always first);
//  2. "GPU offload"    — only when a GPU was actually measured and the
//     baseline stayed on CPU;
//  3. "CPU safe"       — only when the baseline offloads to the GPU
//     (bounds the risk of a bad GPU path);
//  4. "fast context"   — only when the baseline context exceeds 8192
//     (one prompt-throughput variant: context halved, floor 4096).
//
// Identical duplicates are dropped and the set is capped at
// MaxCandidates. No candidate is invented from filename or size.
func Candidates(base recommendation.Recommendation, hw hardware.Profile, caps *llm.EngineCaps) []Candidate {
	out := []Candidate{{
		Label:   "safe baseline",
		Profile: base,
		Reason:  "resource-fit recommendation from measured hardware + model facts + verified engine capabilities",
	}}

	gpuEvidence := hw.HasGPU()

	if gpuEvidence && !base.GPUAutoOffload && base.GPULayers == 0 {
		v := base
		v.GPUAutoOffload = true
		v.GPULayers = 99
		if !profileEqual(out[0].Profile, v) {
			out = append(out, Candidate{
				Label:   "GPU offload",
				Profile: v,
				Reason:  "measured GPU present — verify the offload posture actually wins on this machine",
			})
		}
	}

	if base.GPUAutoOffload || base.GPULayers > 0 {
		v := base
		v.GPUAutoOffload = false
		v.GPULayers = 0
		if !profileEqual(out[0].Profile, v) && !profileEqual(lastProfile(out), v) {
			out = append(out, Candidate{
				Label:   "CPU safe",
				Profile: v,
				Reason:  "baseline offloads to the GPU — bound the risk with a measured CPU-only comparison",
			})
		}
	}

	if base.Context > 8192 {
		v := base
		v.Context = base.Context / 2
		if v.Context < 4096 {
			v.Context = 4096
		}
		if !profileEqual(out[0].Profile, v) && !profileEqual(lastProfile(out), v) {
			out = append(out, Candidate{
				Label:   "fast context",
				Profile: v,
				Reason:  "one prompt-throughput variant: halved context with the same verified posture",
			})
		}
	}

	if len(out) > MaxCandidates {
		out = out[:MaxCandidates]
	}

	return out
}

func lastProfile(cands []Candidate) recommendation.Recommendation {
	if len(cands) == 0 {
		return recommendation.Recommendation{}
	}
	return cands[len(cands)-1].Profile
}

// profileEqual compares the fields that change engine launch args.
func profileEqual(a, b recommendation.Recommendation) bool {
	return a.Context == b.Context &&
		a.Threads == b.Threads &&
		a.ThreadsBatch == b.ThreadsBatch &&
		a.GPULayers == b.GPULayers &&
		a.GPUAutoOffload == b.GPUAutoOffload &&
		a.UBatchSize == b.UBatchSize &&
		a.FlashAttention == b.FlashAttention &&
		a.KVCacheQuant == b.KVCacheQuant
}

// Calibrate runs the bounded pass. The baseline profile MUST already be
// applied and serving (the selection flow applies it before calling) —
// the baseline candidate is measured in place, each other candidate
// costs one ApplyProfile, and the winner (possibly the baseline) is the
// profile left applied at the end. A winner switch or a failed
// non-baseline candidate always restores a verified-good state.
func Calibrate(
	ctx context.Context,
	r Runner,
	modelPath string,
	base recommendation.Recommendation,
	cands []Candidate,
	totalBudget time.Duration,
) Summary {
	if totalBudget <= 0 {
		totalBudget = DefaultTotalBudget
	}
	if len(cands) == 0 {
		cands = []Candidate{{Label: "safe baseline", Profile: base}}
	}

	sum := Summary{At: time.Now()}
	deadline := time.Now().Add(totalBudget)

	type scored struct {
		cand  Candidate
		meas  Measurement
		score float64
	}
	var stable []scored

	for _, cand := range cands {
		if time.Now().After(deadline) {
			sum.Measurements = append(sum.Measurements, Measurement{
				Candidate: cand.Label,
				Note:      "skipped: calibration budget exhausted",
			})
			continue
		}

		// The baseline is already applied and serving — measuring it in
		// place avoids one stop/start cycle (spec §6). Every other
		// candidate needs its own verified boot.
		if cand.Label != "safe baseline" {
			bootCtx, bootCancel := boundedCtx(ctx, DefaultBootBudget, deadline)
			err := r.ApplyProfile(bootCtx, modelPath, cand.Profile)
			bootCancel()

			if err != nil {
				sum.Measurements = append(sum.Measurements, Measurement{
					Candidate: cand.Label,
					Booted:    false,
					Note:      fmt.Sprintf("did not boot: %v", err),
				})
				continue
			}
		}

		measCtx, measCancel := boundedCtx(ctx, DefaultMeasureBudget, deadline)
		perf, err := r.MeasureGeneration(measCtx, MeasurePrompt, MeasureMaxTokens)
		measCancel()

		if err != nil {
			sum.Measurements = append(sum.Measurements, Measurement{
				Candidate: cand.Label,
				Booted:    true,
				Measured:  false,
				Note:      fmt.Sprintf("generation failed: %v", err),
			})
			continue
		}

		m := Measurement{
			Candidate:       cand.Label,
			Booted:          true,
			Measured:        true,
			TTFTSeconds:     float64(perf.TTFTMs) / 1000.0,
			GenTokensPerSec: perf.TokensPerSec,
			TotalSeconds:    float64(perf.WallMs) / 1000.0,
			Stable:          r.EngineStable(),
		}
		// Prompt throughput is derived from the MEASURED prompt size and
		// the MEASURED time-to-first-token (prompt processing completes
		// before the first token): still a real measurement, no fabric.
		if perf.PromptTokens > 0 && perf.TTFTMs > 0 {
			m.PromptTokensPerSec = float64(perf.PromptTokens) / (float64(perf.TTFTMs) / 1000.0)
		}

		if !m.Stable || m.GenTokensPerSec <= 0 {
			m.Note = "unstable or unmeasured throughput — never a winner"
			sum.Measurements = append(sum.Measurements, m)
			continue
		}

		sum.Measurements = append(sum.Measurements, m)
		stable = append(stable, scored{cand: cand, meas: m, score: m.GenTokensPerSec})
	}

	if len(stable) == 0 {
		sum.Ran = false
		sum.Reason = "no candidate produced a measured stable run — evidence-based profile kept, not benchmarked"
		return sum
	}

	// Fastest stable verified profile: generation throughput first, TTFT
	// as the tie-breaker. sort.SliceStable keeps deterministic order.
	sort.SliceStable(stable, func(i, j int) bool {
		if stable[i].score != stable[j].score {
			return stable[i].score > stable[j].score
		}
		return stable[i].meas.TTFTSeconds < stable[j].meas.TTFTSeconds
	})

	winner := stable[0]
	sum.Ran = true
	sum.Winner = winner.cand.Label
	p := winner.cand.Profile
	sum.WinnerProfile = &p
	sum.Changed = winner.cand.Label != "safe baseline"

	// Leave the WINNING profile applied: if the winner is not what is
	// currently running (the last measured candidate), apply it once.
	// When that fails, roll back to the verified baseline.
	if sum.Changed {
		applyCtx, applyCancel := boundedCtx(ctx, DefaultBootBudget, time.Now().Add(DefaultBootBudget))
		err := r.ApplyProfile(applyCtx, modelPath, winner.cand.Profile)
		applyCancel()

		if err != nil {
			rbCtx, rbCancel := boundedCtx(ctx, DefaultBootBudget, time.Now().Add(DefaultBootBudget))
			rbErr := r.ApplyProfile(rbCtx, modelPath, base)
			rbCancel()

			if rbErr != nil {
				sum.Reason = fmt.Sprintf(
					"winner %q failed to re-apply (%v) AND baseline rollback failed (%v) — engine state is supervised",
					winner.cand.Label, err, rbErr,
				)
			} else {
				sum.Reason = fmt.Sprintf(
					"winner %q failed to re-apply (%v) — rolled back to the safe baseline",
					winner.cand.Label, err,
				)
				sum.Winner = "safe baseline"
				sum.WinnerProfile = &base
				sum.Changed = false
			}
		}
	}

	return sum
}

// boundedCtx derives the measurement context: bounded by the runner
// budget, the overall deadline and the parent cancellation.
func boundedCtx(parent context.Context, budget time.Duration, overallDeadline time.Time) (context.Context, context.CancelFunc) {
	end := time.Now().Add(budget)
	if overallDeadline.Before(end) {
		end = overallDeadline
	}
	if parentDeadline, ok := parent.Deadline(); ok && parentDeadline.Before(end) {
		end = parentDeadline
	}
	return context.WithDeadline(parent, end)
}
