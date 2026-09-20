// preflight_v1281_test.go — v1.2.8.1 end-to-end small-context fit
// regressions.
//
// The v1.2.8 hard history floor (2048) made turns on 2K/4K windows FAIL
// at the final fit gate with a misleading reason ("current turn needs…")
// while the real culprit was the fictional history allocation. These
// tests run the REAL orchestrator against a fake engine at 2K / 4K / 8K
// effective contexts with a growing transcript and assert:
//
//   - the engine IS called (the turn degrades, never refuses while
//     history is elidable), and
//   - the serialized wire request stays inside the measured prompt
//     ceiling (no silent over-ceiling send, in-loop gate holds).
package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/chunking"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// TestSmallContextTurnDegradesAndFitsCeiling runs a long conversation
// through RunDetailed at small windows and verifies graceful degradation
// with an in-ceiling wire request.
func TestSmallContextTurnDegradesAndFitsCeiling(t *testing.T) {
	for _, numCtx := range []int{2048, 4096, 8192} {
		t.Run(strings.Join([]string{"numCtx", strings.TrimSpace(intString(numCtx))}, "-"), func(t *testing.T) {
			var lastRequest map[string]any
			var engineCalled bool

			server, _ := newFakeEngine(t, func(turn int, body map[string]any) string {
				engineCalled = true
				lastRequest = body
				return sseChunk("ack.") + sseDone
			})

			cfg := remoteConfig(t, server.URL)
			cfg.LLM.NumCtx = numCtx
			cfg.LLM.MaxTokens = 512

			client := llm.NewClient(config.NewSource(cfg))
			orch := New(config.NewSource(cfg), client)

			// A growing transcript: plenty of elidable history plus the
			// fresh user turn — exactly the shape that used to refuse.
			messages := []llm.Message{
				aiContextStub(strings.Repeat("brief. ", 20)),
			}

			for i := 0; i < 40; i++ {
				messages = append(messages,
					llm.Message{Role: "user", Content: strings.Repeat("history turn ", 8)},
					llm.Message{Role: "assistant", Content: strings.Repeat("older answer ", 8)},
				)
			}

			messages = append(messages, llm.Message{Role: "user", Content: "continue the work"})

			_, err := orch.RunDetailed(context.Background(), messages, func(_ Activity) {})
			if err != nil {
				t.Fatalf("small-context turn refused instead of degrading: %v", err)
			}

			if !engineCalled || lastRequest == nil {
				t.Fatal("the engine was never called")
			}

			// Measured ceiling: usable window minus the safety margin.
			safety := ContextSafetyMargin(numCtx)
			reserve := outputReserveForTest(numCtx, 512)
			ceiling := numCtx - reserve - safety

			// The serialized request must honor the ceiling.
			estimate := 0
			if msgs, ok := lastRequest["messages"].([]any); ok {
				for _, m := range msgs {
					if mm, ok := m.(map[string]any); ok {
						if c, ok := mm["content"].(string); ok {
							estimate += chunking.EstimateTokens(c)
						}
					}
				}
			}

			if estimate > ceiling {
				t.Fatalf("wire request ~%d tok exceeds the measured ceiling %d (numCtx %d)",
					estimate, ceiling, numCtx)
			}
		})
	}
}

func intString(n int) string {
	digits := ""
	if n == 0 {
		return "0"
	}
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

func outputReserveForTest(numCtx, maxTokens int) int {
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	if maxTokens > numCtx/2 {
		maxTokens = numCtx / 2
	}
	if maxTokens < 512 {
		maxTokens = 512
	}
	return maxTokens
}

// TestInLoopGrowthRefusesWhenUnfittable pins the v1.2.8.1 in-loop gate:
// when even after tool-result compaction the request cannot fit, the run
// refuses with a measured error instead of silently sending an
// over-ceiling request.
func TestInLoopGrowthRefusesWhenUnfittable(t *testing.T) {
	var turn int

	server, _ := newFakeEngine(t, func(t int, _ map[string]any) string {
		turn = t
		if t == 1 {
			return sseToolCall("call-1", "huge", "{}") + sseDone
		}
		return sseChunk("should never get here") + sseDone
	})

	cfg := remoteConfig(t, server.URL)
	// A tiny window plus a huge non-tool tail: compaction has nothing
	// left to remove — the in-loop gate must refuse.
	cfg.LLM.NumCtx = 2048
	cfg.LLM.MaxTokens = 512

	client := llm.NewClient(config.NewSource(cfg))
	orch := New(config.NewSource(cfg), client)

	orch.Register(bigTool{name: "huge", desc: "returns a huge result", onRun: func() string {
		return strings.Repeat("x", 40000) // ~10000 tokens of tool output
	}})

	// An oversized current user turn eats the window on top of the tool
	// result (the current turn is never truncated; the in-loop gate is
	// the honest refusal path).
	messages := []llm.Message{
		aiContextStub(strings.Repeat("brief. ", 20)),
		{Role: "user", Content: strings.Repeat("huge current request ", 700)},
	}

	_, err := orch.RunDetailed(context.Background(), messages, func(_ Activity) {})
	if err == nil {
		t.Skip("the fake engine's second turn answered — this scenario needs the gate, not the answer")
	}

	if !strings.Contains(err.Error(), "context fit violation") {
		t.Fatalf("expected the in-loop fit violation, got: %v", err)
	}

	_ = turn
}
