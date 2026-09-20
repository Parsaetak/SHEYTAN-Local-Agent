package pipeline

// pipeline_timing_v129_test.go — v1.2.9 regressions for stage timing
// semantics: each stage's duration must measure THAT stage's runtime
// only (anchored at the stage's own start), while the pipeline total
// stays a separate figure.

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// slowRunner sleeps per tool to make timing measurable.
type slowRunner struct {
	tools  map[string]bool
	delays map[string]time.Duration
}

func (s *slowRunner) HasTool(name string) bool { return s.tools[name] }

func (s *slowRunner) RunTool(_ context.Context, name string, _ json.RawMessage) (string, error) {
	if d, ok := s.delays[name]; ok {
		time.Sleep(d)
	}
	return "ok", nil
}

// TestStageDurationsAnchorAtStageStart pins the fix: stage 2's
// DurationMs must NOT include stage 1's sleep (the old cumulative
// accounting made every stage report the elapsed pipeline time).
func TestStageDurationsAnchorAtStageStart(t *testing.T) {
	r := &slowRunner{
		tools: map[string]bool{"a": true, "b": true},
		delays: map[string]time.Duration{
			"a": 60 * time.Millisecond,
			"b": 20 * time.Millisecond,
		},
	}

	report := Run(context.Background(), r, []Stage{
		{Tool: "a", Label: "slow", TimeoutSec: 30},
		{Tool: "b", Label: "fast", TimeoutSec: 30},
	}, nil)

	if !report.OK || len(report.Stages) != 2 {
		t.Fatalf("pipeline did not complete: %+v", report)
	}

	fast := report.Stages[1].DurationMs
	// The fast stage sleeps ~20ms. With the old cumulative bug it
	// reported >= 80ms (stage 1's 60ms included). Allow generous slack
	// for scheduler noise, but never the full stage-1 cost.
	if fast >= 60 {
		t.Fatalf("stage 2 duration = %dms — looks CUMULATIVE (stage 1's 60ms included); per-stage timing must anchor at the stage's own start", fast)
	}
	if fast < 0 {
		t.Fatalf("negative stage duration: %d", fast)
	}

	// The pipeline TOTAL is a separate figure and DOES cover both
	// stages: it must be at least the sum of the sleeps.
	if report.DurationMs < 70 {
		t.Fatalf("pipeline total = %dms — must cover both stages (~80ms), it is separate from per-stage durations", report.DurationMs)
	}
}
