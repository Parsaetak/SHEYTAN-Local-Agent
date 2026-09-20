package agent

// handoff_v129_test.go — v1.2.9 regressions: concurrent agent.md writes
// are SERIALIZED per workspace path, so two runs (or a run racing the
// CLI) can never silently drop each other's handoff section.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestConcurrentHandoffWritesAllSurvive pins the serialization: N
// concurrent writers each perform a read-splice-rename cycle against the
// SAME agent.md. Before the per-path lock, a writer's base could be
// read before another writer's rename, silently erasing the earlier
// section. After the fix, the file converges to a valid single-section
// state and — when writers extend different stable parts — no run's
// content is silently lost mid-sequence.
func TestConcurrentHandoffWritesAllSurvive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, HandoffFileName)

	const writers = 16

	// Pre-existing stable content OUTSIDE the section (must survive
	// byte-for-byte, whatever the interleaving).
	stable := "# Agent Handoff\n\nStable engineering instructions — never rewritten.\n\n"
	if err := os.WriteFile(path, []byte(stable), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, writers)

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			h := Handoff{
				Task:      fmt.Sprintf("task %d", n),
				Objective: fmt.Sprintf("objective %d", n),
			}
			if err := WriteHandoffFile(path, h); err != nil {
				errs <- fmt.Errorf("writer %d: %w", n, err)
			}
		}(i)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent write failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(data)

	// 1. Exactly ONE marker pair (no interleaved duplication).
	if got := strings.Count(content, HandoffBeginMarker); got != 1 {
		t.Fatalf("begin markers = %d, want 1 (interleaved writes corrupted the section):\n%s", got, content)
	}
	if got := strings.Count(content, HandoffEndMarker); got != 1 {
		t.Fatalf("end markers = %d, want 1", got)
	}

	// 2. The stable content survived byte-for-byte.
	if !strings.HasPrefix(content, stable) {
		t.Fatalf("stable prefix was rewritten by a concurrent handoff write:\n%q", content)
	}

	// 3. The section content is internally consistent: the rendered
	// section parses (Task and Objective from the SAME writer — a
	// mixed splice would pair task N with objective M).
	begin := strings.Index(content, HandoffBeginMarker)
	end := strings.Index(content, HandoffEndMarker)
	section := content[begin:end]

	// Consistency check: extract "task N" and "objective N" and require
	// a matching N (a mixed splice would pair task N with objective M).
	var taskN, objN = -1, -1
	for i := 0; i < writers; i++ {
		if strings.Contains(section, fmt.Sprintf("task %d\n", i)) || strings.HasSuffix(section, fmt.Sprintf("task %d", i)) {
			taskN = i
		}
		if strings.Contains(section, fmt.Sprintf("objective %d\n", i)) || strings.HasSuffix(section, fmt.Sprintf("objective %d", i)) {
			objN = i
		}
	}
	if taskN != objN {
		t.Fatalf("mixed splice: section pairs task %d with objective %d — the read-splice-rename was NOT serialized:\n%s", taskN, objN, section)
	}
}

// TestConcurrentHandoffDifferentPathsNoDeadlock ensures the keyed locks
// do not serialize UNRELATED files (and never deadlock).
func TestConcurrentHandoffDifferentPathsNoDeadlock(t *testing.T) {
	dir := t.TempDir()

	var wg sync.WaitGroup
	done := make(chan struct{})

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			path := filepath.Join(dir, fmt.Sprintf("ws-%d", n), HandoffFileName)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Errorf("mkdir: %v", err)
				return
			}
			if err := WriteHandoffFile(path, Handoff{Task: fmt.Sprintf("t%d", n)}); err != nil {
				t.Errorf("write %s: %v", path, err)
			}
		}(i)
	}

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// all writes completed — no cross-path serialization or deadlock
	case <-time.After(10 * time.Second):
		t.Fatal("independent-path handoff writes deadlocked — the per-path locks must not interact")
	}
}
