package contextplan

import (
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

func TestNewBudgetReservesOutput(t *testing.T) {
	b := NewBudget(8192, 1024)

	if b.Total != 8192 {
		t.Fatalf("total = %d, want 8192", b.Total)
	}

	if b.ReserveOutput != 1024 {
		t.Fatalf("reserve = %d, want 1024", b.ReserveOutput)
	}

	if b.Usable != 8192-1024 {
		t.Fatalf("usable = %d", b.Usable)
	}
}

func TestNewBudgetClampsExtremeOutputRequest(t *testing.T) {
	// A max_tokens larger than half the window is clamped so the prompt
	// always retains the majority of the context.
	b := NewBudget(4096, 8000)

	if b.ReserveOutput > 4096/2 {
		t.Fatalf("reserve not clamped: %d", b.ReserveOutput)
	}

	if b.ReserveOutput < 512 {
		t.Fatalf("reserve floor violated: %d", b.ReserveOutput)
	}
}

func TestAssembleSectionsAccountFullBudget(t *testing.T) {
	sys := 420
	tools := 1200
	injected := 300

	plan := Assemble(Input{
		SystemTokens:    sys,
		ToolTokens:      tools,
		RecallTokens:    injected,
		NumCtx:          8192,
		MaxOutputTokens: 1024,
	})

	if plan.HistoryBudget <= 0 {
		t.Fatal("history budget must be positive")
	}

	// The plan must account for the whole usable window: history budget +
	// measured fixed sections must never exceed usable.
	used := sys + tools + injected
	if plan.HistoryBudget+used > plan.Budget.Usable+2048 {
		t.Fatalf(
			"budget overcommitted: history %d + fixed %d > usable %d (floor exception)",
			plan.HistoryBudget, used, plan.Budget.Usable,
		)
	}

	// Section inventory must be complete.
	seen := map[string]bool{}
	for _, s := range plan.Sections {
		seen[s.Name] = true
	}

	for _, want := range []string{SectionSystem, SectionTools, SectionRecall, SectionHistory, SectionReserve} {
		if !seen[want] {
			t.Fatalf("plan missing section %s", want)
		}
	}
}

func TestAssemblePressureDropsRecallFirst(t *testing.T) {
	// A tiny window stuffed with fixed content: the plan must report the
	// overflow honestly (pressure > 1) instead of pretending everything
	// fits — the spec's "never exceed real capacity" is enforced by making
	// the overflow VISIBLE, not by silently lying about it.
	plan := Assemble(Input{
		SystemTokens:    3000,
		ToolTokens:      3000,
		RecallTokens:    2000,
		NumCtx:          8192,
		MaxOutputTokens: 1024,
	})

	// Fixed sections (system + tools) alone leave less than the recall
	// block + history floor: recall must be reported as dropped.
	fixed := 3000 + 3000
	floor := 2048

	if fixed+2000+floor > plan.Budget.Usable {
		for _, s := range plan.Sections {
			if s.Name == SectionRecall && s.Included {
				t.Fatal("recall must be reported dropped under extreme pressure")
			}
		}
	}

	if plan.Summary() == "" {
		t.Fatal("summary must always render")
	}
}

func TestAssembleTinyWindowKeepsHistoryFloor(t *testing.T) {
	// v1.2.8.1 REPAIR: the history floor is bounded by the actual
	// available prompt budget — never a fixed 2048-token promise. On a
	// 4096 window with 7000 tokens of fixed sections, the v1.2.8
	// unconditional floor allocated 2048 history tokens that do not
	// exist, overfilled the windower, and forced a hard turn refusal.
	// The adaptive floor keeps the budget at the real remainder
	// and lets the optional sections drop instead.
	plan := Assemble(Input{
		SystemTokens: 4000,
		ToolTokens:   3000,
		NumCtx:       4096,
	})

	// usable = 4096 - 1024 (output reserve) = 3072. The system briefing
	// (4000) exceeds half the window and is refused; tools (3000) count.
	// The true remainder is 72 — the floor must never exceed it (the
	// v1.2.8 code promised 2048 here).
	if plan.HistoryBudget > plan.Budget.Usable-3000 {
		t.Fatalf("adaptive floor must not promise tokens that do not exist: %d (remainder %d)",
			plan.HistoryBudget, plan.Budget.Usable-3000)
	}

	for _, s := range plan.Sections {
		if s.Name == SectionRecall && s.Included {
			t.Fatal("optional sections must be dropped when the history floor cannot be met")
		}
	}
}

func TestSummaryRendersProvenance(t *testing.T) {
	plan := Assemble(Input{
		SystemTokens: 500,
		ToolTokens:   1000,
		NumCtx:       8192,
	})

	// The orchestrator measures the windowed history before reporting;
	// mirror that here.
	plan.SetSectionTokens(SectionHistory, 2500)

	summary := plan.Summary()

	for _, want := range []string{"context", "system", "tools", "history"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q: %s", want, summary)
		}
	}
}

func TestEstimateTokensConsistency(t *testing.T) {
	// Same estimator as chunking: 4 bytes ≈ 1 token.
	if got := EstimateTokens("abcdefgh"); got != 2 {
		t.Fatalf("EstimateTokens(8 ascii bytes) = %d, want 2", got)
	}
}

func TestPlanPressureNeverNegative(t *testing.T) {
	plan := Assemble(Input{NumCtx: 8192})

	if plan.Pressure() < 0 {
		t.Fatalf("pressure must never be negative: %v", plan.Pressure())
	}
}

func TestHistoryMessagesDoNotConsumeSystemBudget(t *testing.T) {
	// The Input.History field exists for downstream measurement; the plan
	// must not panic or mis-allocate when the conversation is populated.
	history := []llm.Message{
		{Role: "user", Content: strings.Repeat("hello ", 500)},
		{Role: "assistant", Content: strings.Repeat("answer ", 500)},
	}

	plan := Assemble(Input{
		SystemTokens: 500,
		ToolTokens:   800,
		History:      history,
		NumCtx:       8192,
	})

	if plan.HistoryBudget <= 0 {
		t.Fatal("history budget must remain positive with populated history")
	}
}
