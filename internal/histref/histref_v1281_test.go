// histref_v1281_test.go — v1.2.8.1 cross-mode reference regressions:
// prompt-injection fencing, staleness honesty, Mode hint preservation.
package histref

import (
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/sessions"
)

// fenceID extracts the 8-hex fence id from one marker occurrence.
func fenceID(content, marker string) string {
	i := strings.Index(content, marker)
	if i < 0 {
		return ""
	}
	rest := content[i+len(marker):]
	if len(rest) < 8 {
		return ""
	}
	return rest[:8]
}

// TestBlocksAreFencedAsUntrustedData pins the v1.2.8.1 injection defense:
// every block wraps its excerpts in a per-run random fence, and the header
// states that fence-bounded content is quoted data, never instructions.
func TestBlocksAreFencedAsUntrustedData(t *testing.T) {
	store := newCrossModeStore(t)

	blocks := Resolve(store, []Ref{{SessionID: findAgent(t, store)}}, "orders index", 900)

	if len(blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(blocks))
	}

	content := blocks[0].Content

	begin := strings.Index(content, "<<<BEGIN-HISTREF:")
	end := strings.Index(content, "<<<END-HISTREF:")

	if begin < 0 || end <= begin {
		t.Fatalf("fence markers missing or inverted:\n%s", content)
	}

	if !strings.Contains(content, "untrusted data") {
		t.Fatal("the fence framing (untrusted data) is missing from the header")
	}

	// Both markers carry the SAME random run id.
	idBegin := fenceID(content, "<<<BEGIN-HISTREF:")
	idEnd := fenceID(content, "<<<END-HISTREF:")
	if idBegin == "" || idBegin != idEnd {
		t.Fatalf("fence ids mismatch: begin %q end %q", idBegin, idEnd)
	}

	// Two Resolve calls produce DIFFERENT fences (per-run randomness —
	// a source conversation cannot pre-forge a closing fence).
	other := Resolve(store, []Ref{{SessionID: findAgent(t, store)}}, "orders index", 900)
	if len(other) != 1 {
		t.Fatalf("second resolve blocks = %d", len(other))
	}
	secondID := fenceID(other[0].Content, "<<<BEGIN-HISTREF:")
	if idBegin == secondID {
		t.Fatalf("fence id repeated across runs: %s", idBegin)
	}
}

// TestStaleSummaryVersionIsMarked pins staleness honesty: when the source
// session was re-summarized after the reference was attached, the block
// says so (attached version vs current version) instead of silently
// shipping a different revision than the picker previewed.
func TestStaleSummaryVersionIsMarked(t *testing.T) {
	store := newCrossModeStore(t)
	agentID := findAgent(t, store)

	// Attached at summary version 1: create the summary the fixture did
	// not ship (version 0 shell → one settled turn → version 1).
	base, err := store.SummaryForRun(agentID)
	if err != nil {
		t.Fatalf("SummaryForRun: %v", err)
	}
	sum := sessions.UpdateSummaryFromTurn(base, agentID, sessions.ModeAgent,
		"Now tune the database index for the orders table.",
		"Added the composite index on orders.", nil)
	if err := store.SaveSummary(sum); err != nil {
		t.Fatalf("SaveSummary: %v", err)
	}

	// The source is re-summarized (version bumps).
	rolled := *sum
	rolled.Version++
	if err := store.SaveSummary(&rolled); err != nil {
		t.Fatalf("re-summarize source: %v", err)
	}

	blocks := Resolve(store, []Ref{{SessionID: agentID, SummaryVersion: 1}}, "orders index", 900)
	if len(blocks) != 1 {
		t.Fatalf("blocks = %d", len(blocks))
	}

	if !strings.Contains(blocks[0].Content, "re-summarized since this reference was attached") {
		t.Fatalf("staleness note missing:\n%s", blocks[0].Content)
	}
}

// TestNormalizeRefsPreservesModeHint pins that the Mode label survives
// normalization (it labels the picker chip; the authoritative mode is
// re-resolved server-side from the store).
func TestNormalizeRefsPreservesModeHint(t *testing.T) {
	refs := []Ref{
		{SessionID: "a", Mode: "chat", SummaryVersion: 3},
		{SessionID: "a", Mode: "agent"}, // duplicate — dropped
		{SessionID: "", Mode: "chat"},   // empty id — dropped
		{SessionID: "b", Mode: "agent"}, // extra — dropped (cap 4 not reached, but dedup order kept)
	}

	out := NormalizeRefs(refs)

	if len(out) != 2 {
		t.Fatalf("normalized length = %d, want 2", len(out))
	}
	if out[0].Mode != "chat" || out[0].SummaryVersion != 3 {
		t.Fatalf("first ref lost its mode/version: %+v", out[0])
	}
	if out[1].Mode != "agent" {
		t.Fatalf("second ref lost its mode: %+v", out[1])
	}
}
