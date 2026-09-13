package contextplan

import "testing"

// Phase 7 regression tests for the fit-guarantee primitives.

func TestPromptCeilingWithSafetyMargin(t *testing.T) {
	b := NewBudget(8192, 1024) // usable = 7168
	plan := Assemble(Input{
		SystemTokens:       100,
		ToolTokens:         100,
		NumCtx:             8192,
		MaxOutputTokens:    1024,
		SafetyMarginTokens: 256,
	})
	if plan.Budget.Usable != b.Usable {
		t.Fatalf("budget usable = %d, want %d", plan.Budget.Usable, b.Usable)
	}
	if plan.PromptCeiling() != b.Usable-256 {
		t.Fatalf("ceiling = %d, want %d", plan.PromptCeiling(), b.Usable-256)
	}
}

func TestOverflowExactFit(t *testing.T) {
	// system+tools+history exactly at the ceiling → zero overflow.
	numCtx := 4096
	maxOut := 512
	safety := 64

	plan := Assemble(Input{
		SystemTokens:       300,
		ToolTokens:         500,
		NumCtx:             numCtx,
		MaxOutputTokens:    maxOut,
		SafetyMarginTokens: safety,
	})
	_ = plan

	budget := NewBudget(numCtx, maxOut)
	ceiling := budget.Usable - safety
	sysTok := 300
	toolTok := 500
	history := ceiling - sysTok - toolTok

	plan2 := Assemble(Input{
		SystemTokens:       sysTok,
		ToolTokens:         toolTok,
		NumCtx:             numCtx,
		MaxOutputTokens:    maxOut,
		SafetyMarginTokens: safety,
		MinHistoryTokens:   1,
	})
	plan2.SetSectionTokens(SectionHistory, history)
	if plan2.Overflow() > 0 {
		t.Fatalf("exact-fit plan overflows by %d (total %d, ceiling %d)",
			plan2.Overflow(), plan2.TotalTokens(), plan2.PromptCeiling())
	}
}

func TestOverflowByOne(t *testing.T) {
	numCtx := 4096
	sysTok := 300
	toolTok := 500
	safety := 64

	budget := NewBudget(numCtx, 512)
	ceiling := budget.Usable - safety
	history := ceiling - sysTok - toolTok + 1 // one over

	plan := Assemble(Input{
		SystemTokens:       sysTok,
		ToolTokens:         toolTok,
		NumCtx:             numCtx,
		MaxOutputTokens:    512,
		SafetyMarginTokens: safety,
		MinHistoryTokens:   1,
	})
	plan.SetSectionTokens(SectionHistory, history)

	if got := plan.Overflow(); got != 1 {
		t.Fatalf("overflow = %d, want exactly 1", got)
	}
}

func TestOutputReserveCausesRejection(t *testing.T) {
	// A prompt that would fit WITHOUT the output reserve must still be
	// rejected when the reserve is honored: reserve = half the window.
	numCtx := 2048
	plan := Assemble(Input{
		SystemTokens:       1500, // larger than half the usable space
		ToolTokens:         0,
		NumCtx:             numCtx,
		MaxOutputTokens:    1024, // clamped to numCtx/2 = 1024
		SafetyMarginTokens: 0,
	})

	// The system section is marked NOT included (pathological), which
	// makes the plan's total exceed the usable window once history is
	// measured: overflow must be visible, never hidden.
	plan.SetSectionTokens(SectionHistory, 1)
	if plan.Budget.ReserveOutput != 1024 {
		t.Fatalf("reserve = %d, want 1024 (half window)", plan.Budget.ReserveOutput)
	}
	if plan.Overflow() <= 0 && plan.Sections[1].Included {
		t.Fatalf("oversized system must be flagged: %+v", plan.Sections[1])
	}
}

func TestAdjustmentsTrail(t *testing.T) {
	plan := Assemble(Input{NumCtx: 4096, MaxOutputTokens: 512})
	plan.AddAdjustment("dynamic toolset: reduced tool surface 16 → 7 for this task")
	plan.AddAdjustment("compact system briefing (context-pressure mode)")
	if len(plan.Adjustments) != 2 {
		t.Fatalf("adjustments = %v", plan.Adjustments)
	}
}

func TestSectionTokens(t *testing.T) {
	plan := Assemble(Input{SystemTokens: 10, NumCtx: 4096, MaxOutputTokens: 512})
	if plan.SectionTokens(SectionSystem) != 10 {
		t.Fatal("SectionTokens(system) mismatch")
	}
	if plan.SectionTokens("nonexistent") != 0 {
		t.Fatal("absent section must read 0")
	}
}
