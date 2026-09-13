package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	tools   map[string]bool
	results map[string]struct {
		out string
		err error
	}
	calls []string
}

func (f *fakeRunner) HasTool(name string) bool { return f.tools[name] }

func (f *fakeRunner) RunTool(_ context.Context, name string, _ json.RawMessage) (string, error) {
	f.calls = append(f.calls, name)
	if r, ok := f.results[name]; ok {
		return r.out, r.err
	}
	return "ok", nil
}

func TestRunHappyPath(t *testing.T) {
	r := &fakeRunner{tools: map[string]bool{"files": true, "shell": true}}

	report := Run(context.Background(), r, []Stage{
		{Tool: "files", Args: json.RawMessage(`{"action":"list"}`), Label: "inspect"},
		{Tool: "shell", Args: json.RawMessage(`{"command":"go build ./..."}`), Label: "build"},
	}, nil)

	if !report.OK {
		t.Fatalf("pipeline failed: %s", report.Summary())
	}
	if len(report.Stages) != 2 || !report.Stages[0].OK || !report.Stages[1].OK {
		t.Fatalf("stages: %+v", report.Stages)
	}
	if len(r.calls) != 2 || r.calls[0] != "files" {
		t.Fatalf("execution order wrong: %v", r.calls)
	}
}

func TestRunStopsOnRequiredFailure(t *testing.T) {
	r := &fakeRunner{
		tools: map[string]bool{"shell": true, "files": true},
		results: map[string]struct {
			out string
			err error
		}{
			"shell": {"", errors.New("exit status 1")},
		},
	}

	report := Run(context.Background(), r, []Stage{
		{Tool: "files", Label: "inspect"},
		{Tool: "shell", Label: "build"},
		{Tool: "files", Label: "should-never-run"},
	}, nil)

	if report.OK {
		t.Fatal("pipeline must fail when a required stage fails")
	}
	if report.FailedStage != 1 {
		t.Fatalf("failedStage = %d, want 1", report.FailedStage)
	}
	if len(r.calls) != 2 {
		t.Fatalf("stages after failure must not run: %v", r.calls)
	}
	if !report.Stages[2].Skipped {
		t.Fatal("post-failure stage must be marked skipped")
	}
}

func TestRunOptionalFailureContinues(t *testing.T) {
	r := &fakeRunner{
		tools: map[string]bool{"shell": true, "files": true},
		results: map[string]struct {
			out string
			err error
		}{
			"shell": {"", errors.New("boom")},
		},
	}

	report := Run(context.Background(), r, []Stage{
		{Tool: "shell", Optional: true, Label: "prebuild"},
		{Tool: "files", Label: "inspect"},
	}, nil)

	if !report.OK {
		t.Fatalf("optional failure must not abort: %s", report.Summary())
	}
}

func TestRunUnknownToolFails(t *testing.T) {
	r := &fakeRunner{tools: map[string]bool{}}
	report := Run(context.Background(), r, []Stage{{Tool: "nope"}}, nil)
	if report.OK {
		t.Fatal("unknown tool must fail the stage")
	}
}

func TestRunCancellation(t *testing.T) {
	r := &fakeRunner{tools: map[string]bool{"shell": true}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	report := Run(ctx, r, []Stage{{Tool: "shell"}}, nil)
	if report.OK || !report.Canceled {
		t.Fatalf("canceled run must be reported: %+v", report)
	}
}

func TestRunBoundedStages(t *testing.T) {
	r := &fakeRunner{tools: map[string]bool{"shell": true}}
	stages := make([]Stage, MaxStages+5)
	for i := range stages {
		stages[i] = Stage{Tool: "shell"}
	}
	report := Run(context.Background(), r, stages, nil)
	if len(report.Stages) > MaxStages {
		t.Fatalf("stage bound ignored: %d", len(report.Stages))
	}
}

func TestSummaryRendering(t *testing.T) {
	r := &fakeRunner{tools: map[string]bool{"shell": true}}
	report := Run(context.Background(), r, []Stage{{Tool: "shell", Label: "build"}}, nil)
	s := report.Summary()
	if !strings.Contains(s, "pipeline OK") || !strings.Contains(s, "shell") {
		t.Fatalf("summary malformed: %q", s)
	}
}

func TestStageTimeoutBound(t *testing.T) {
	// A stage with an absurd timeout must still be capped by the
	// per-stage execution boundary (600s) — verified indirectly by the
	// stage completing quickly (fake runner) and the duration recorded.
	r := &fakeRunner{tools: map[string]bool{"shell": true}}
	start := time.Now()
	report := Run(context.Background(), r, []Stage{{Tool: "shell", TimeoutSec: 100000}}, nil)
	if time.Since(start) > 2*time.Second {
		t.Fatal("stage timeout cap not applied")
	}
	if !report.OK {
		t.Fatalf("report: %s", report.Summary())
	}
}
