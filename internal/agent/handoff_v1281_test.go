// handoff_v1281_test.go — v1.2.8.1 handoff regressions:
//
//   - EVERY completed agent run leaves a handoff, including no-change
//     runs (the honest "No engineering changes were made." handoff);
//   - content OUTSIDE the marker pair is preserved BYTE-FOR-BYTE;
//   - the write is verified by read-back;
//   - an UNREADABLE existing agent.md aborts instead of being clobbered.
package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoChangeAgentRunWritesHonestHandoff pins the v1.2.8.1 product
// contract: an evidence-free completed run (no files, no commands, no
// tests, no repairs, no artifacts) still produces a usable, honest
// handoff — never an empty one, never a skipped write.
func TestNoChangeAgentRunWritesHonestHandoff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, HandoffFileName)

	// A run that only answered prose: no engineering evidence at all.
	ts := NewTaskState("Explain the deployment layout")

	h := HandoffFromTaskState(ts.snapshot(), "", "done")

	if err := WriteHandoffFile(path, h); err != nil {
		t.Fatalf("no-change handoff write: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	content := string(data)

	for _, want := range []string{
		"No engineering changes were made.",
		"## Task",
		"## Current state",
		"## Changes made",
		"## Remaining work",
		"None recorded.",
		"## Recommended next action",
		HandoffBeginMarker,
		HandoffEndMarker,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("honest no-change handoff missing %q:\n%s", want, content)
		}
	}

	if strings.Contains(content, "## Files changed") {
		t.Fatal("no-change handoff must not invent a Files changed section")
	}
}

// TestHandoffBytePreservationOutsideMarkers pins byte-exact preservation:
// odd whitespace, CRLF-ish content and trailing text after the markers
// survive a handoff replacement untouched.
func TestHandoffBytePreservationOutsideMarkers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, HandoffFileName)

	before := "# Rules\n\n  indented   line \n\n\nkeep   this   spacing\n"
	after := "\n\n\nTrailing notes: kept exactly.\n"

	seed := before + HandoffBeginMarker + "\n# Latest Agent Handoff\n\nold content\n" + HandoffEndMarker + after
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	h := Handoff{Task: "second run", Objective: "second run"}
	if err := WriteHandoffFile(path, h); err != nil {
		t.Fatalf("write: %v", err)
	}

	data, _ := os.ReadFile(path)
	out := string(data)

	if !strings.HasPrefix(out, before) {
		t.Fatalf("bytes before the markers were rewritten:\n%q", out[:min(len(out), len(before)+20)])
	}

	if !strings.HasSuffix(out, after) {
		t.Fatalf("bytes after the markers were rewritten:\n%q", out)
	}

	if strings.Count(out, HandoffBeginMarker) != 1 {
		t.Fatalf("expected exactly one marker pair:\n%s", out)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestHandoffReadErrorAborts pins the durability repair: an existing but
// unreadable agent.md must NEVER be silently replaced (the v1.2.8 code
// treated any read error as "absent" and clobbered the file).
func TestHandoffReadErrorAborts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, HandoffFileName)

	original := "# Precious content\n\nDo not lose this.\n"
	if err := os.WriteFile(path, []byte(original), 0o000); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := WriteHandoffFile(path, Handoff{Task: "clobber attempt"}); err == nil {
		// Restore permissions so TempDir cleanup works, then fail.
		_ = os.Chmod(path, 0o644)
		t.Fatal("an unreadable agent.md was replaced instead of aborting")
	}

	// The failure must be honest AND the file untouched. Restore read
	// permission first (the test itself needs it to verify the bytes).
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("restore permissions: %v", err)
	}

	data, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatalf("original file unreadable after failed handoff: %v", rerr)
	}

	if string(data) != original {
		t.Fatalf("original content changed:\n%s", string(data))
	}

	if strings.Contains(string(data), "clobber attempt") {
		t.Fatal("the handoff was written despite the read error")
	}
}

// TestHandoffReadBackVerification errors when the write cannot be
// verified — a handoff is only "completed" after the file on disk is
// proven to carry the section.
func TestHandoffReadBackVerification(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, HandoffFileName)

	h := Handoff{Task: "verified write", Objective: "verified write"}

	if err := WriteHandoffFile(path, h); err != nil {
		t.Fatalf("write: %v", err)
	}

	// verifyHandoffContent accepts the real file…
	data, _ := os.ReadFile(path)
	if !verifyHandoffContent(string(data), RenderHandoff(h)) {
		t.Fatal("read-back verification rejected a correct file")
	}

	// …and rejects a file whose section was tampered with.
	tampered := strings.Replace(string(data), "verified write", "tampered!", 1)
	if verifyHandoffContent(tampered, RenderHandoff(h)) {
		t.Fatal("read-back verification accepted tampered content")
	}
}
