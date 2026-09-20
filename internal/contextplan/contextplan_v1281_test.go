// contextplan_v1281_test.go — v1.2.8.1 regressions for the adaptive
// history floor and honest section accounting.
//
// Pins the repair of the two v1.2.8 defects that turned recoverable
// context pressure into hard turn refusals:
//
//  1. the hard `MinHistoryTokens = 2048` floor promised history tokens
//     that do not exist on small effective contexts (2K/4K/8K), so the
//     windower over-filled and the final fit gate refused the turn;
//  2. TotalTokens() counted DROPPED (Included=false) sections, so a
//     designed graceful drop (history-refs under pressure) poisoned
//     Overflow() and refused a prompt that actually fit.
package contextplan

import (
	"strings"
	"testing"
)

// smallContextCases are the v1.2.8.1 spec scenarios: 2K / 4K / 8K
// effective contexts with a large history, a large summary, cross-mode
// refs and attachments — everything the degradation ladder must survive.
func smallContextCases() []struct {
	name             string
	numCtx           int
	systemTokens     int
	toolTokens       int
	summaryTokens    int
	recallTokens     int
	histRefTokens    int
	attachmentTokens int
} {
	return []struct {
		name             string
		numCtx           int
		systemTokens     int
		toolTokens       int
		summaryTokens    int
		recallTokens     int
		histRefTokens    int
		attachmentTokens int
	}{
		{
			name:             "2k window with large history and refs",
			numCtx:           2048,
			systemTokens:     420,
			toolTokens:       300,
			summaryTokens:    220,
			recallTokens:     180,
			histRefTokens:    3600, // 4 refs × DefaultBlockTokens
			attachmentTokens: 150,
		},
		{
			name:             "4k window with large history and refs",
			numCtx:           4096,
			systemTokens:     600,
			toolTokens:       500,
			summaryTokens:    250,
			recallTokens:     300,
			histRefTokens:    3600,
			attachmentTokens: 400,
		},
		{
			name:             "8k deep-tier window",
			numCtx:           8192,
			systemTokens:     3000,
			toolTokens:       1200,
			summaryTokens:    300,
			recallTokens:     600,
			histRefTokens:    3600,
			attachmentTokens: 800,
		},
	}
}

// TestAssembleSmallContextsDegradeGracefully runs the spec scenarios and
// verifies the three invariants that make degradation graceful:
//
//	history budget ≤ the tokens that actually remain,
//	dropped sections never count toward Overflow(),
//	the plan does not refuse while anything degradable remains.
func TestAssembleSmallContextsDegradeGracefully(t *testing.T) {
	for _, tc := range smallContextCases() {
		t.Run(tc.name, func(t *testing.T) {
			plan := Assemble(Input{
				SystemTokens:       tc.systemTokens,
				ToolTokens:         tc.toolTokens,
				SummaryTokens:      tc.summaryTokens,
				RecallTokens:       tc.recallTokens,
				HistoryRefTokens:   tc.histRefTokens,
				AttachmentTokens:   tc.attachmentTokens,
				NumCtx:             tc.numCtx,
				MaxOutputTokens:    512,
				SafetyMarginTokens: 64,
				MinHistoryTokens:   2048, // the v1.2.8 production default
			})

			// Invariant 1: the history budget never exceeds what actually
			// remains after the INCLUDED fixed sections. (The v1.2.8 floor
			// raised it to 2048 regardless.)
			remaining := plan.Budget.Usable - 64 - tc.toolTokens
			if sectionIncluded(plan, SectionSystem) {
				remaining -= tc.systemTokens
			}
			if sectionIncluded(plan, SectionSummary) {
				remaining -= tc.summaryTokens
			}
			if plan.HistoryBudget > remaining {
				t.Fatalf("history budget %d exceeds the real remainder %d — the floor promised tokens that do not exist",
					plan.HistoryBudget, remaining)
			}

			// Invariant 2: overflow is measured over INCLUDED sections
			// only. With history not yet windowed (0 tokens), a degradable
			// plan NEVER refuses here.
			if over := plan.Overflow(); over > 0 {
				t.Fatalf("plan overflows by %d tokens while optional sections remain droppable — graceful degradation broken", over)
			}

			// Invariant 3: a dropped section keeps its measured tokens for
			// the UI, but is excluded from TotalTokens().
			if !sectionIncluded(plan, SectionHistoryRefs) && tc.histRefTokens > 0 {
				if got := plan.TotalTokens(); got > plan.PromptCeiling() {
					t.Fatalf("TotalTokens %d still exceeds the ceiling %d despite dropped sections",
						got, plan.PromptCeiling())
				}
			}
		})
	}
}

// TestOverflowZeroWhenHistoryRefsDropped is the concrete v1.2.8 failure
// repro: 4 attached references (4 × 900 = 3600 tok) on a 4K window — the
// planner drops the refs section by design and the turn MUST proceed
// without them, not refuse.
func TestOverflowZeroWhenHistoryRefsDropped(t *testing.T) {
	plan := Assemble(Input{
		SystemTokens:       600,
		ToolTokens:         500,
		SummaryTokens:      250,
		HistoryRefTokens:   3600,
		NumCtx:             4096,
		MaxOutputTokens:    512,
		SafetyMarginTokens: 64,
	})

	if sectionIncluded(plan, SectionHistoryRefs) {
		t.Fatal("precondition: the refs section must be dropped under this budget")
	}

	if over := plan.Overflow(); over != 0 {
		t.Fatalf("dropped history-refs poisoned Overflow(): %d tokens", over)
	}

	if plan.TotalTokens() != tc3IncludedTokens(plan) {
		t.Fatalf("TotalTokens = %d, want only included sections %d",
			plan.TotalTokens(), tc3IncludedTokens(plan))
	}
}

// tc3IncludedTokens sums the sections the contract counts: everything
// included plus the always-traveling system briefing.
func tc3IncludedTokens(p Plan) int {
	total := 0
	for _, s := range p.Sections {
		if s.Name == SectionReserve {
			continue
		}
		if !s.Included && s.Name != SectionSystem {
			continue
		}
		total += s.Tokens
	}
	return total
}

func sectionIncluded(p Plan, name string) bool {
	for _, s := range p.Sections {
		if s.Name == name {
			return s.Included
		}
	}
	return false
}

// TestTotalTokensExcludesDroppedSections pins the accounting rule
// directly: dropped retrieval sections are excluded, the system briefing
// is always counted (splitSystemPrefix always carries it), the output
// reserve is never counted.
func TestTotalTokensExcludesDroppedSections(t *testing.T) {
	plan := Plan{
		Sections: []Section{
			{Name: SectionReserve, Tokens: 512, Included: true},
			{Name: SectionSystem, Tokens: 400, Included: false}, // refused but still travels
			{Name: SectionTools, Tokens: 300, Included: true},
			{Name: SectionSummary, Tokens: 250, Included: false}, // refused AND not injected
			{Name: SectionRecall, Tokens: 180, Included: false},  // dropped
			{Name: SectionHistoryRefs, Tokens: 3600, Included: false},
			{Name: SectionHistory, Tokens: 0, Included: true},
		},
		Budget: Budget{Total: 2048, ReserveOutput: 512, Usable: 1536},
	}

	// 400 (system, always travels) + 300 (tools) + 0 (history).
	if got := plan.TotalTokens(); got != 700 {
		t.Fatalf("TotalTokens = %d, want 700 (system + tools only)", got)
	}

	if strings.Count(plan.Summary(), "recall") != 0 {
		t.Fatal("Summary renders a dropped recall section")
	}
}

// TestAdaptiveFloorMetWhenRoomExists guards the other direction: the
// floor is a PREFERENCE — when the window DOES have room, history still
// receives at least the requested floor.
func TestAdaptiveFloorMetWhenRoomExists(t *testing.T) {
	plan := Assemble(Input{
		SystemTokens:     400,
		ToolTokens:       300,
		SummaryTokens:    150,
		NumCtx:           8192,
		MaxOutputTokens:  512,
		MinHistoryTokens: 2048,
	})

	if plan.HistoryBudget < 2048 {
		t.Fatalf("history floor violated when room exists: %d", plan.HistoryBudget)
	}
}
