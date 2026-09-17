package agent

// v125_measurement_test.go — the honest v1.2.4 → v1.2.5 before/after
// measurement for FIRST-REQUEST prompt cost on a trivial chat turn.
//
// v1.2.4 baseline (measured on this same code path before the change, see
// worklog): EVERY turn composed the FULL AI-context briefing
// (29,691 chars ≈ 7,368 tokens) + every enabled tool schema + recall +
// project card + skills BEFORE the first engine call — a one-line "hi"
// paid a ~10K-token prefill.
//
// This test measures what the engine ACTUALLY receives under v1.2.5 for
// the same turn and asserts the measured reduction.
import (
	"context"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/chunking"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

func TestV125FirstPromptCostMeasurement(t *testing.T) {
	seen := &engineSeen{}

	server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
		seen.record(turn, body)
		return sseChunk("hello!") + sseDone
	})

	cfg := remoteConfig(t, server.URL)
	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	// Register a realistic tool surface so the tool-schema cost is real.
	for _, tool := range []Tool{
		&fakeTool{name: "shell"},
		&fakeTool{name: "files"},
		&fakeTool{name: "codeExec"},
		&fakeTool{name: "webSearch"},
		&fakeTool{name: "git"},
		&fakeTool{name: "browser"},
		&fakeTool{name: "dataAnalysis"},
		&fakeTool{name: "json"},
		&fakeTool{name: "archive"},
		&fakeTool{name: "fetch"},
		&fakeTool{name: "diff"},
		&fakeTool{name: "screenshot"},
		&fakeTool{name: "linux"},
		&fakeTool{name: "research"},
		&fakeTool{name: "memory"},
		&fakeTool{name: "pipeline"},
	} {
		orch.Register(tool)
	}

	orch.SetProjectCard(func() string {
		return strings.Repeat("## PROJECT INTELLIGENCE\n- facts\n", 20)
	})

	orch.SetRecaller(recallerFunc(func(query string, k, maxTokens int) string {
		return "user asked: baseline\nassistant answered: baseline"
	}))

	res, err := orch.RunDetailed(context.Background(),
		[]llm.Message{{Role: "user", Content: "hi"}},
		func(_ Activity) {})

	if err != nil {
		t.Fatalf("RunDetailed: %v", err)
	}

	// MEASURED: the prompt the engine received (all message content).
	promptTokens := chunking.EstimateTokens(seen.allContent())

	// MEASURED: the tool schemas the engine received.
	toolSchemas := len(seen.tools)

	t.Logf("v1.2.5 measured first-request prompt: ~%d tokens (tier=%s, tools offered=%d, firstPromptTokens=%d)",
		promptTokens, res.Tier, toolSchemas, res.Timing.FirstPromptTokens)

	if res.Tier != "FAST" {
		t.Fatalf("tier = %s; measurement only meaningful on FAST", res.Tier)
	}

	// The v1.2.4 full briefing ALONE was ~7,368 tokens; the complete
	// v1.2.4 first prompt (briefing + 17 tool schemas + recall + card)
	// measured ~10K tokens. The v1.2.5 FAST prompt must be a small
	// fraction of that: compact briefing (~353) + bounded tools + the
	// one-line turn.
	if promptTokens > 4000 {
		t.Fatalf("FAST first prompt = ~%d tokens — expected a small fraction of the ~10K v1.2.4 baseline", promptTokens)
	}

	// The offered tool surface is tier-bounded (FAST caps at 6).
	if toolSchemas > 6 {
		t.Fatalf("FAST offered %d tool schemas; the tier caps at 6", toolSchemas)
	}

	// The measured timeline reported the same initial cost.
	if res.Timing.FirstPromptTokens <= 0 || res.Timing.FirstPromptTokens > 4000 {
		t.Fatalf("FirstPromptTokens = %d (out of the expected FAST range)", res.Timing.FirstPromptTokens)
	}

	// Honest context accounting on the result.
	if res.Timing.ClassifyMs < 0 || res.Timing.ContextMs < 0 || res.Timing.PromptMs < 0 {
		t.Fatalf("negative timing values: %+v", res.Timing)
	}
}
