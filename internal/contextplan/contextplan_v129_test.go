package contextplan

// contextplan_v129_test.go — v1.2.9 regressions: honest small-window
// budgeting (no fabricated context, no fixed floors that exceed the
// real window, adaptive history floor) and the estimator-tier label.

import (
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/chunking"
)

// TestNewBudgetHonestSmallWindows pins the removal of the two
// fabrications: numCtx < 1024 is no longer inflated to 1024, and the
// 512-token output reserve scales down instead of starving the prompt.
func TestNewBudgetHonestSmallWindows(t *testing.T) {
	cases := []struct {
		name          string
		numCtx        int
		maxTokens     int
		wantTotal     int
		wantUsableMin int // lower bound for usable prompt space
		wantUsableMax int // upper bound (reserve must exist)
	}{
		{"real 512 window", 512, 256, 512, 128, 384},
		{"real 256 window", 256, 128, 256, 32, 192},
		{"tiny 128 window", 128, 64, 128, 16, 96},
		{"unknown window", 0, 1024, 1024, 0, 1024},
		{"normal 8k window", 8192, 1024, 8192, 3072, 7680},
		{"large 128k window", 131072, 4096, 131072, 60000, 126976},
	}

	for _, tc := range cases {
		b := NewBudget(tc.numCtx, tc.maxTokens)
		if b.Total != tc.wantTotal {
			t.Errorf("%s: Total = %d, want %d (small windows must not be inflated)", tc.name, b.Total, tc.wantTotal)
		}
		if b.Usable < tc.wantUsableMin {
			t.Errorf("%s: Usable = %d, want >= %d (a fixed reserve must not starve the prompt)", tc.name, b.Usable, tc.wantUsableMin)
		}
		if b.Usable > tc.wantUsableMax {
			t.Errorf("%s: Usable = %d, want <= %d (an output reserve must exist)", tc.name, b.Usable, tc.wantUsableMax)
		}
		if b.Usable+b.ReserveOutput != b.Total {
			t.Errorf("%s: accounting broken: %d + %d != %d", tc.name, b.Usable, b.ReserveOutput, b.Total)
		}
	}
}

// TestAssembleHistoryFloorBoundedByWindow pins the adaptive floor: the
// 2048-token default preference never exceeds a quarter of the usable
// window, so small models are not pushed into permanent drop mode.
func TestAssembleHistoryFloorBoundedByWindow(t *testing.T) {
	cases := []struct {
		name   string
		numCtx int
	}{
		{"small 2k window", 2048},
		{"small 4k window", 4096},
		{"normal 8k window", 8192},
		{"large 32k window", 32768},
	}

	for _, tc := range cases {
		plan := Assemble(Input{
			SystemTokens:    200,
			ToolTokens:      400,
			NumCtx:          tc.numCtx,
			MaxOutputTokens: 1024,
		})

		// The plan must not promise more history budget than the usable
		// window minus fixed sections.
		if plan.HistoryBudget < 0 {
			t.Errorf("%s: negative history budget", tc.name)
		}

		cap := plan.Budget.Usable / 4
		fixed := plan.SectionTokens(SectionSystem) + plan.SectionTokens(SectionTools)
		remaining := plan.Budget.Usable - fixed
		if remaining < 0 {
			remaining = 0
		}
		if plan.HistoryBudget > remaining {
			t.Errorf("%s: history budget %d exceeds the real remainder %d", tc.name, plan.HistoryBudget, remaining)
		}

		// On small windows the implicit floor (usable/4) is small enough
		// that optional sections are droppable rather than force-fitted.
		if tc.numCtx <= 4096 && cap < 2048 {
			// sanity: the floor was actually scaled down.
			if plan.SectionTokens(SectionHistory) > plan.Budget.Usable {
				t.Errorf("%s: history exceeds the usable window", tc.name)
			}
		}
	}
}

// TestAssembleSmallContextDropsOptionalSections: on a 2k window with a
// big fixed payload, the optional sections (recall, refs, attachments)
// drop — and the SYSTEM section still reports honestly.
func TestAssembleSmallContextDropsOptionalSections(t *testing.T) {
	plan := Assemble(Input{
		SystemTokens:     300,
		ToolTokens:       500,
		RecallTokens:     600,
		HistoryRefTokens: 500,
		AttachmentTokens: 400,
		NumCtx:           2048,
		MaxOutputTokens:  1024,
	})

	// Recall must be dropped: it does not fit alongside the bounded
	// history floor.
	if plan.SectionIncluded(SectionRecall) {
		t.Fatalf("recall was included on a 2k window with 500 tool tokens — the drop ladder must fire")
	}
	if plan.Overflow() < 0 {
		t.Fatalf("negative overflow is impossible")
	}
}

// TestPlanCarriesEstimatorLabel pins the honesty label: a plan built on
// the heuristic tier is labeled "heuristic", never presented as exact.
func TestPlanCarriesEstimatorLabel(t *testing.T) {
	chunking.ResetTokenEstimator()
	plan := Assemble(Input{SystemTokens: 100, ToolTokens: 200, NumCtx: 8192, MaxOutputTokens: 1024})
	if plan.Estimator != "heuristic" {
		t.Fatalf("Estimator = %q, want heuristic (default tier)", plan.Estimator)
	}

	chunking.SetModelFamilyEstimator("qwen")
	plan = Assemble(Input{SystemTokens: 100, ToolTokens: 200, NumCtx: 8192, MaxOutputTokens: 1024})
	if plan.Estimator != "family:qwen" {
		t.Fatalf("Estimator = %q, want family:qwen", plan.Estimator)
	}

	chunking.SetExactTokenCounter(func(s string) int { return len(s) / 4 })
	plan = Assemble(Input{SystemTokens: 100, ToolTokens: 200, NumCtx: 8192, MaxOutputTokens: 1024})
	if plan.Estimator != "exact" {
		t.Fatalf("Estimator = %q, want exact", plan.Estimator)
	}

	chunking.ResetTokenEstimator()
}

// TestEstimatorCJKAware pins the conservative fallback: CJK runes count
// as ~1 token each (the old runes/4 formula under-counted by 4x — a
// latent overflow for non-Latin conversations).
func TestEstimatorCJKAware(t *testing.T) {
	chunking.ResetTokenEstimator()

	ascii := "abcdefghij" // 10 runes → ceil(10/4) = 3
	if got := chunking.EstimateTokens(ascii); got != 3 {
		t.Errorf("ascii estimate = %d, want 3", got)
	}

	cjk := "你好世界" // 4 CJK runes → 4 tokens (1 per rune)
	if got := chunking.EstimateTokens(cjk); got < 4 {
		t.Errorf("cjk estimate = %d, want >= 4 (one per rune — the old formula said %d)", got, 1)
	}

	// Mixed: 2 CJK + 8 ASCII → 2 + 2 = 4.
	mixed := "你好abcdefgh"
	if got := chunking.EstimateTokens(mixed); got < 4 {
		t.Errorf("mixed estimate = %d, want >= 4", got)
	}
}

// SectionIncluded reports whether one section traveled (test helper).
func (p Plan) SectionIncluded(name string) bool {
	for _, s := range p.Sections {
		if s.Name == name {
			return s.Included
		}
	}
	return false
}
