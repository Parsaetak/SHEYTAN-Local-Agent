// calibration_test.go — the CONTROL-CONTRACT tests for the bounded
// automatic performance check.
//
// The scripted runner exists to prove retain/rollback/budget/dedup
// behavior — the product NEVER fabricates performance numbers, and these
// tests assert exactly that: an unmeasured candidate can never win, and
// "not benchmarked" is the honest verdict when nothing measured.
package calibration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/hardware"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/recommendation"
)

// realisticRunner is a full fake with per-candidate outcomes, keyed by
// the candidate label the Calibrate loop passes via ApplyProfile.
type realisticRunner struct {
	// initial is the label of the profile ALREADY serving when
	// Calibrate starts (the safe baseline is measured in place — the
	// runner never applies it).
	initial    string
	bootErr    map[string]error
	measure    map[string]llm.PerfStats
	measureErr map[string]error
	stable     map[string]bool

	applied  []string
	measured []string
}

// currentLabel is what the fake engine is serving right now.
func (r *realisticRunner) currentLabel() string {
	if len(r.applied) > 0 {
		return r.applied[len(r.applied)-1]
	}
	return r.initial
}

func (r *realisticRunner) ApplyProfile(_ context.Context, _ string, rec recommendation.Recommendation) error {
	// Identify the candidate by its distinctive field values.
	label := candidateLabel(rec)
	r.applied = append(r.applied, label)
	if err, ok := r.bootErr[label]; ok {
		return err
	}
	return nil
}

func (r *realisticRunner) MeasureGeneration(_ context.Context, _ string, _ int) (llm.PerfStats, error) {
	label := r.currentLabel()
	r.measured = append(r.measured, label)

	if err, ok := r.measureErr[label]; ok {
		return llm.PerfStats{}, err
	}
	if p, ok := r.measure[label]; ok {
		return p, nil
	}
	return llm.PerfStats{}, nil
}

func (r *realisticRunner) EngineStable() bool {
	if v, ok := r.stable[r.currentLabel()]; ok {
		return v
	}
	return true
}

// candidateLabel derives a stable identity from the fields the candidate
// variants actually change (GPU posture + context).
func candidateLabel(rec recommendation.Recommendation) string {
	gpu := "cpu"
	if rec.GPUAutoOffload || rec.GPULayers > 0 {
		gpu = "gpu"
	}
	return gpu + "/" + itoa(rec.Context)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

func baseRec() recommendation.Recommendation {
	return recommendation.Recommendation{
		Task:           recommendation.TaskChat,
		Context:        8192,
		Threads:        8,
		GPULayers:      0,
		UBatchSize:     512,
		FlashAttention: true,
		KVCacheQuant:   "f16",
	}
}

func baseWithGPU() recommendation.Recommendation {
	r := baseRec()
	r.GPUAutoOffload = true
	r.GPULayers = 99
	return r
}

// TestCandidatesBoundedAndEvidenceDerived proves the candidate set is
// small, deduplicated, and only derived from measured evidence.
func TestCandidatesBoundedAndEvidenceDerived(t *testing.T) {
	// CPU-only hardware: a CPU-baseline recommendation yields just the
	// context variant (context 8192 is NOT > 8192 → none) → 1 candidate.
	hwCPU := hardware.Profile{}
	cands := Candidates(baseRec(), hwCPU, nil)
	if len(cands) != 1 || cands[0].Label != "safe baseline" {
		t.Fatalf("CPU-only baseline should yield exactly the baseline, got %+v", cands)
	}

	// GPU measured + CPU baseline → GPU offload variant joins.
	hwGPU := hardware.Profile{}
	hwGPU.GPUs = []hardware.GPU{{Vendor: "Intel", Name: "Arc A770", VRAMBytes: 16 << 30}}
	cands = Candidates(baseRec(), hwGPU, nil)
	if len(cands) != 2 {
		t.Fatalf("GPU evidence should add the GPU-offload candidate, got %+v", cands)
	}
	if cands[1].Label != "GPU offload" || !cands[1].Profile.GPUAutoOffload {
		t.Fatalf("second candidate must be the GPU-offload variant, got %+v", cands[1])
	}

	// GPU baseline → CPU-safe variant joins (no GPU evidence needed —
	// the baseline itself offloads, bounding the risk is the reason).
	cands = Candidates(baseWithGPU(), hardware.Profile{}, nil)
	if len(cands) != 2 || cands[1].Label != "CPU safe" {
		t.Fatalf("GPU baseline should add the CPU-safe candidate, got %+v", cands)
	}

	// Big context → the fast-context variant; total capped at 3.
	big := baseWithGPU()
	big.Context = 32768
	cands = Candidates(big, hwGPU, nil)
	if len(cands) > MaxCandidates {
		t.Fatalf("candidate set must stay bounded at %d, got %d", MaxCandidates, len(cands))
	}
	labels := map[string]bool{}
	for _, c := range cands {
		labels[c.Label] = true
	}
	if len(labels) != len(cands) {
		t.Fatalf("duplicate candidates: %+v", cands)
	}
}

// TestCalibrateWinnerRetainedFromMeasuredRuns proves: only measured
// stable candidates can win, the winner is re-applied, and the summary
// carries the real measured values.
func TestCalibrateWinnerRetainedFromMeasuredRuns(t *testing.T) {
	base := baseRec()

	// Two candidates: baseline (slow) and GPU offload (fast).
	gpu := baseWithGPU()

	runner := &realisticRunner{
		initial: "cpu/8192",
		measure: map[string]llm.PerfStats{
			"cpu/8192": {TTFTMs: 900, Tokens: 24, TokensPerSec: 12, PromptTokens: 9, WallMs: 2000},
			"gpu/8192": {TTFTMs: 300, Tokens: 24, TokensPerSec: 40, PromptTokens: 9, WallMs: 900},
		},
	}

	sum := Calibrate(context.Background(), runner, "/models/m.gguf", base, []Candidate{
		{Label: "safe baseline", Profile: base},
		{Label: "GPU offload", Profile: gpu},
	}, time.Minute)

	if !sum.Ran {
		t.Fatalf("calibration must report ran with two stable measured candidates: %+v", sum)
	}
	if sum.Winner != "GPU offload" {
		t.Fatalf("winner = %q, want GPU offload (40 tok/s beats 12)", sum.Winner)
	}
	if !sum.Changed {
		t.Fatal("a non-baseline winner must mark Changed")
	}

	// The winner's real numbers are carried verbatim.
	found := false
	for _, m := range sum.Measurements {
		if m.Candidate == "GPU offload" {
			found = true
			if m.GenTokensPerSec != 40 || m.TTFTSeconds != 0.3 {
				t.Fatalf("winner measurement must carry the measured values, got %+v", m)
			}
			if m.PromptTokensPerSec != 30 { // 9 prompt tokens / 0.3s
				t.Fatalf("prompt throughput must be derived from measured values, got %+v", m)
			}
		}
	}
	if !found {
		t.Fatal("winner measurement missing")
	}

	// Final applied profile is the WINNER (last ApplyProfile call).
	if got := runner.applied[len(runner.applied)-1]; got != "gpu/8192" {
		t.Fatalf("final applied profile = %q, want the GPU winner", got)
	}
}

// TestCalibrateUnmeasuredCandidateNeverWins proves the honesty rule: a
// candidate whose generation fails or is unstable can NEVER be the
// winner, even if it booted.
func TestCalibrateUnmeasuredCandidateNeverWins(t *testing.T) {
	base := baseRec()
	gpu := baseWithGPU()

	runner := &realisticRunner{
		initial: "cpu/8192",
		measure: map[string]llm.PerfStats{
			"cpu/8192": {TTFTMs: 900, Tokens: 24, TokensPerSec: 12, PromptTokens: 9, WallMs: 2000},
		},
		measureErr: map[string]error{
			"gpu/8192": errors.New("engine died mid-generation"),
		},
		stable: map[string]bool{"cpu/8192": true},
	}

	sum := Calibrate(context.Background(), runner, "/models/m.gguf", base, []Candidate{
		{Label: "safe baseline", Profile: base},
		{Label: "GPU offload", Profile: gpu},
	}, time.Minute)

	if !sum.Ran {
		t.Fatalf("the stable measured baseline still counts as a run: %+v", sum)
	}
	if sum.Winner != "safe baseline" {
		t.Fatalf("an unmeasured candidate must never win; winner = %q", sum.Winner)
	}
	if sum.Changed {
		t.Fatal("baseline winner must not mark Changed (no extra restart)")
	}
}

// TestCalibrateFailedWinnerRollsBack proves the rollback contract: when
// the winner cannot be re-applied, the verified baseline is restored.
func TestCalibrateFailedWinnerRollsBack(t *testing.T) {
	base := baseRec()
	gpu := baseWithGPU()

	runner := &realisticRunner{
		initial: "cpu/8192",
		measure: map[string]llm.PerfStats{
			"cpu/8192": {TTFTMs: 900, Tokens: 24, TokensPerSec: 12, PromptTokens: 9, WallMs: 2000},
			"gpu/8192": {TTFTMs: 300, Tokens: 24, TokensPerSec: 40, PromptTokens: 9, WallMs: 900},
		},
	}

	// Apply call sequence: (1) GPU candidate boot, (2) winner re-apply.
	// The second must fail so the rollback to the verified baseline runs.
	wrapped := &failingOnThird{runner: runner}
	wrapped.failOn = 2

	sum := Calibrate(context.Background(), wrapped, "/models/m.gguf", base, []Candidate{
		{Label: "safe baseline", Profile: base},
		{Label: "GPU offload", Profile: gpu},
	}, time.Minute)

	if sum.Winner != "safe baseline" || sum.Changed {
		t.Fatalf("failed winner re-apply must roll back to the baseline, got %+v", sum)
	}
	if sum.Reason == "" {
		t.Fatal("rollback must be documented in the summary reason")
	}
	// The last applied profile is the rollback (cpu baseline).
	if got := runner.applied[len(runner.applied)-1]; got != "cpu/8192" {
		t.Fatalf("final applied = %q, want the rolled-back baseline", got)
	}
}

// failingOnThird fails the Nth ApplyProfile (proving the rollback path).
type failingOnThird struct {
	runner  *realisticRunner
	failOn  int
	applied int
}

func (f *failingOnThird) ApplyProfile(ctx context.Context, path string, rec recommendation.Recommendation) error {
	f.applied++
	if f.applied == f.failOn {
		return errors.New("winner re-apply exploded")
	}
	return f.runner.ApplyProfile(ctx, path, rec)
}

func (f *failingOnThird) MeasureGeneration(ctx context.Context, prompt string, maxTokens int) (llm.PerfStats, error) {
	return f.runner.MeasureGeneration(ctx, prompt, maxTokens)
}

func (f *failingOnThird) EngineStable() bool { return f.runner.EngineStable() }

// TestCalibrateNothingMeasuredIsNotBenchmarked proves the honest
// "not benchmarked" verdict when no candidate produces a measured run.
func TestCalibrateNothingMeasuredIsNotBenchmarked(t *testing.T) {
	base := baseRec()

	runner := &realisticRunner{
		initial: "cpu/8192",
		measureErr: map[string]error{
			"cpu/8192": errors.New("offline"),
		},
	}

	sum := Calibrate(context.Background(), runner, "/models/m.gguf", base, nil, time.Minute)

	if sum.Ran {
		t.Fatalf("nothing measured must never report Ran, got %+v", sum)
	}
	if sum.Winner != "" || sum.WinnerProfile != nil {
		t.Fatalf("no winner without measurement, got %+v", sum)
	}
	if sum.Reason == "" {
		t.Fatal("the not-benchmarked reason must be present")
	}
}

// TestCalibrateBudgetBounded proves candidates beyond the budget are
// skipped (never retried, never measured).
func TestCalibrateBudgetBounded(t *testing.T) {
	base := baseRec()
	gpu := baseWithGPU()

	runner := &realisticRunner{
		measure: map[string]llm.PerfStats{
			"cpu/8192": {TTFTMs: 100, Tokens: 24, TokensPerSec: 12, WallMs: 300},
			"gpu/8192": {TTFTMs: 100, Tokens: 24, TokensPerSec: 40, WallMs: 300},
		},
	}

	// A budget so small EVERY candidate (including the in-place
	// baseline) is skipped — the honest verdict is "not benchmarked",
	// and the already-applied baseline stays.
	sum := Calibrate(context.Background(), runner, "/models/m.gguf", base, []Candidate{
		{Label: "safe baseline", Profile: base},
		{Label: "GPU offload", Profile: gpu},
	}, time.Nanosecond)

	skipped := 0
	for _, m := range sum.Measurements {
		if m.Note == "skipped: calibration budget exhausted" {
			skipped++
		}
	}
	if skipped != 2 {
		t.Fatalf("the exhausted budget must skip every candidate, measurements: %+v", sum.Measurements)
	}
	if sum.Ran || sum.Winner != "" {
		t.Fatalf("an exhausted budget must never claim a measured winner, got %+v", sum)
	}
	if sum.Reason == "" {
		t.Fatal("the honest not-benchmarked reason must be present")
	}
}

// TestStoreRetainAndReuse proves the retain/reuse fingerprinting: a
// retained record is reused only for the SAME model fingerprint on the
// SAME machine.
func TestStoreRetainAndReuse(t *testing.T) {
	dir := t.TempDir()
	model := filepath.Join(dir, "m.gguf")
	if err := os.WriteFile(model, []byte("gguf-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	mk, err := ModelKey(model)
	if err != nil {
		t.Fatal(err)
	}
	hwKey := "cpu=8/16;ram=17179869184;gpu="

	if _, ok := Retained(dir, mk, hwKey); ok {
		t.Fatal("empty store must not retain anything")
	}

	rec := Record{
		ModelPath:       model,
		Winner:          "GPU offload",
		Profile:         baseWithGPU(),
		GenTokensPerSec: 40,
		TTFTSeconds:     0.3,
		MeasuredAt:      time.Now().UTC(),
	}
	if err := Retain(dir, mk, hwKey, rec); err != nil {
		t.Fatalf("retain: %v", err)
	}

	got, ok := Retained(dir, mk, hwKey)
	if !ok || got.Winner != "GPU offload" || got.GenTokensPerSec != 40 {
		t.Fatalf("retained record must round-trip, got %+v (ok=%v)", got, ok)
	}

	// A DIFFERENT machine fingerprint must NOT reuse the record.
	if _, ok := Retained(dir, mk, "cpu=4/8;ram=8589934592;gpu="); ok {
		t.Fatal("a different machine must not reuse the retained measurement")
	}

	// A DIFFERENT model fingerprint must NOT reuse the record.
	other := filepath.Join(dir, "other.gguf")
	if err := os.WriteFile(other, []byte("other-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	omk, err := ModelKey(other)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := Retained(dir, omk, hwKey); ok {
		t.Fatal("a different model must not reuse the retained measurement")
	}
}
