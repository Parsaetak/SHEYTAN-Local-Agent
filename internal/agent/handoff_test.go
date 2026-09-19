// handoff_test.go — v1.2.8 agent.md handoff tests.
package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteHandoffCreatesAndReplacesDynamicSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, HandoffFileName)

	h := Handoff{
		Task:         "Implement the deploy fix",
		Objective:    "Implement the deploy fix",
		CurrentState: "TEST: passed — go test ./...",
		ChangesMade:  []string{"run outcome: done", "patched scripts/build.sh"},
		FilesChanged: []string{"scripts/build.sh"},
		TestsAndVer:  []string{"tests run:", "  - go test ./...", "verification: verified — 1/1 checks passed"},
		NextAction:   "Push the tag",
		DoNotRedo:    []string{"scripts/build.sh"},
	}

	if err := WriteHandoffFile(path, h); err != nil {
		t.Fatalf("WriteHandoffFile (create): %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	first := string(data)

	// The section structure is present and machine-parseable.
	for _, want := range []string{
		HandoffBeginMarker,
		"# Latest Agent Handoff",
		"## Task",
		"## Objective",
		"## Changes made",
		"## Files changed",
		"## Tests and verification",
		"## Recommended next action",
		"## Do not redo",
		HandoffEndMarker,
		"- scripts/build.sh",
	} {
		if !strings.Contains(first, want) {
			t.Fatalf("handoff section missing %q:\n%s", want, first)
		}
	}

	// Stable content BEFORE the section survives a second write.
	stable := "# SHEYTAN-Local-Agent — Agent Context\n\nStable engineering rules stay here.\n"
	if err := os.WriteFile(path, []byte(stable), 0o644); err != nil {
		t.Fatalf("seed stable content: %v", err)
	}

	h2 := h
	h2.Task = "Second run"
	h2.Objective = "Second run"
	if err := WriteHandoffFile(path, h2); err != nil {
		t.Fatalf("WriteHandoffFile (update): %v", err)
	}

	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after update: %v", err)
	}
	updated := string(data)

	if !strings.HasPrefix(updated, "# SHEYTAN-Local-Agent — Agent Context") {
		t.Fatalf("stable content lost:\n%s", updated)
	}

	if !strings.Contains(updated, "Stable engineering rules stay here.") {
		t.Fatal("stable engineering instructions were not preserved")
	}

	if strings.Count(updated, HandoffBeginMarker) != 1 {
		t.Fatalf("expected exactly ONE dynamic section, found %d", strings.Count(updated, HandoffBeginMarker))
	}

	if strings.Contains(updated, "Implement the deploy fix\n") {
		t.Fatal("the previous handoff content should be fully replaced")
	}

	if !strings.Contains(updated, "Second run") {
		t.Fatal("the new handoff content is missing")
	}
}

func TestWriteHandoffReplacesTornSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, HandoffFileName)

	// A dangling begin marker (crash mid-write) is treated as absent.
	torn := "# Header\n\n" + HandoffBeginMarker + "\n## Task\npartial\n"
	if err := os.WriteFile(path, []byte(torn), 0o644); err != nil {
		t.Fatalf("seed torn file: %v", err)
	}

	if err := WriteHandoffFile(path, Handoff{Task: "recovery run"}); err != nil {
		t.Fatalf("WriteHandoffFile: %v", err)
	}

	data, _ := os.ReadFile(path)
	out := string(data)

	if strings.Count(out, HandoffBeginMarker) != 1 {
		t.Fatalf("torn marker not superseded:\n%s", out)
	}
	if !strings.Contains(out, "recovery run") {
		t.Fatal("new section missing")
	}
}

func TestHandoffFileNameIsLowercase(t *testing.T) {
	// Windows filesystems may treat agent.md and Agent.md as the SAME
	// path — the constant is lowercase by contract.
	if HandoffFileName != "agent.md" {
		t.Fatalf("HandoffFileName = %q, want exactly agent.md", HandoffFileName)
	}
}

func TestHandoffFromTaskStateFactualMapping(t *testing.T) {
	ts := NewTaskState("Ship the fix")
	ts.FilesChanged = append(ts.FilesChanged, "main.go")
	ts.CommandsRun = append(ts.CommandsRun, "go build ./...")
	ts.TestsRun = append(ts.TestsRun, "go test ./internal/...")
	ts.Failures = append(ts.Failures, "shell: compile error")
	ts.SetVerification("verified", "1/1 checks passed")

	h := HandoffFromTaskState(*ts, ts.Verification, "done")

	if h.Task != "Ship the fix" {
		t.Fatalf("task mapping wrong: %q", h.Task)
	}

	joined := strings.Join(h.TestsAndVer, "\n")
	if !strings.Contains(joined, "go test ./internal/...") ||
		!strings.Contains(joined, "verified — 1/1 checks passed") {
		t.Fatalf("tests/verification mapping wrong:\n%s", joined)
	}

	foundFile := false
	for _, item := range h.FilesChanged {
		if item == "main.go" {
			foundFile = true
		}
	}
	if !foundFile {
		t.Fatal("files changed not carried into the handoff")
	}
}
