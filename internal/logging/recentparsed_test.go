package logging

// logging_recentparsed_test.go — v1.1.7: the in-app Log Viewer surface must
// parse entries, redact secrets BEFORE display, and stay bounded.

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRecentParsedRedactsSecrets(t *testing.T) {
	m, err := New(filepath.Join(t.TempDir(), "logs"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	m.Info("tools", `running fetch with apiKey "sk-super-secret-123"`)
	m.Warn("engine", "startup slow (%d ms)", 1200)
	m.Error("network", "dial failed")

	entries := m.RecentParsed(10)
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(entries))
	}

	tool := entries[0]
	if tool.Category != "tools" {
		t.Fatalf("category = %q, want tools", tool.Category)
	}
	if strings.Contains(tool.Message, "sk-super-secret-123") {
		t.Fatalf("secret leaked into the viewer surface: %q", tool.Message)
	}
	if !strings.Contains(tool.Message, "[REDACTED]") {
		t.Fatalf("expected redaction marker in %q", tool.Message)
	}

	if entries[1].Level != "WARN" || entries[1].Category != "engine" {
		t.Fatalf("parsed warn entry wrong: %+v", entries[1])
	}
	if entries[1].Message != "startup slow (1200 ms)" {
		t.Fatalf("message = %q", entries[1].Message)
	}
	if entries[2].Level != "ERROR" {
		t.Fatalf("level = %q, want ERROR", entries[2].Level)
	}
}

func TestRecentParsedBounded(t *testing.T) {
	m, err := New(filepath.Join(t.TempDir(), "logs"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for i := 0; i < recentLinesCap+50; i++ {
		m.Debug("test", "line %d", i)
	}

	entries := m.RecentParsed(1000)
	if len(entries) > recentLinesCap {
		t.Fatalf("entries = %d, ring must stay bounded at %d", len(entries), recentLinesCap)
	}
}

func TestRecentParsedTolerantOfOddLines(t *testing.T) {
	m, err := New(filepath.Join(t.TempDir(), "logs"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// A line that does not match the canonical layout must still surface.
	m.mu.Lock()
	m.recent = append(m.recent, "<<<garbage multi word line>>>")
	m.mu.Unlock()

	entries := m.RecentParsed(10)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if !strings.Contains(entries[0].Message, "garbage") {
		t.Fatalf("unparseable line dropped: %+v", entries[0])
	}
}
