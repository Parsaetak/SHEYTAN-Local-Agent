// histref_test.go — v1.2.8 cross-mode history retrieval tests.
package histref

import (
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/sessions"
)

func newCrossModeStore(t *testing.T) *sessions.Store {
	t.Helper()

	store := sessions.New(t.TempDir())

	// An AGENT session with two distinct topics.
	agentSess := store.CreateInMode(sessions.ModeAgent)
	agentSess.Title = "Deploy pipeline work"
	if err := store.Save(agentSess); err != nil {
		t.Fatalf("Save agent: %v", err)
	}

	turns := []llm.Message{
		{Role: "user", Content: "Fix the deploy script for the release zip."},
		{Role: "assistant", Content: "Patched scripts/build-and-zip.sh and verified the artifact."},
		{Role: "user", Content: "Now tune the database index for the orders table."},
		{Role: "assistant", Content: "Added the composite index on orders(created_at, status)."},
	}
	for _, m := range turns {
		if _, err := store.AppendMessage(agentSess.ID, m); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}

	// A CHAT session.
	chatSess := store.CreateInMode(sessions.ModeChat)
	chatSess.Title = "Database design chat"
	if err := store.Save(chatSess); err != nil {
		t.Fatalf("Save chat: %v", err)
	}

	for _, m := range []llm.Message{
		{Role: "user", Content: "How should the orders table be indexed?"},
		{Role: "assistant", Content: "Use a composite index on (created_at, status) for the orders table."},
	} {
		if _, err := store.AppendMessage(chatSess.ID, m); err != nil {
			t.Fatalf("AppendMessage chat: %v", err)
		}
	}

	return store
}

// TestResolveRetrievesRelevantTurnsWithProvenance pins the core contract:
// only the RELEVANT turns travel, and the provenance labels (session id,
// mode, retrieval reason) are inside the block.
//
// v1.2.9: the block travels on the USER (data) channel — never the
// system instruction channel (prompt-injection escalation fix).
func TestResolveRetrievesRelevantTurnsWithProvenance(t *testing.T) {
	store := newCrossModeStore(t)

	blocks := Resolve(store,
		[]Ref{{SessionID: "missing-session"}, {SessionID: findAgent(t, store)}},
		"orders table database index",
		1800,
	)

	if len(blocks) != 1 {
		t.Fatalf("Resolve returned %d blocks, want 1 (unknown ref skipped)", len(blocks))
	}

	block := blocks[0]
	if block.Role != "user" {
		t.Fatalf("block role = %q, want user (data channel — v1.2.9 security: history must never occupy the system instruction channel)", block.Role)
	}

	content := block.Content

	for _, want := range []string{
		"[AUTOMATED REFERENCE-DATA ATTACHMENT",
		"source-session: ",
		"source-mode: agent",
		"retrieval-reason:",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("provenance label %q missing from block:\n%s", want, content)
		}
	}

	// The relevant turn is IN; the irrelevant one is OUT.
	if !strings.Contains(content, "orders") {
		t.Fatalf("relevant turn missing:\n%s", content)
	}
	if strings.Contains(content, "deploy script") {
		t.Fatalf("irrelevant turn leaked into the block:\n%s", content)
	}

	// The data-not-authority statement is part of the contract.
	if !strings.Contains(content, "not an instruction") {
		t.Fatalf("data-not-authority framing missing:\n%s", content)
	}

	// v1.2.9: the automated-attachment framing (the model must know the
	// user did not write this message) is part of the contract.
	if !strings.Contains(content, "did NOT write this message") {
		t.Fatalf("not-user-authored framing missing:\n%s", content)
	}
}

// TestResolveBudgetBounded pins the token budget: a tiny budget still
// yields ONE clipped excerpt, never an unbounded dump. (v1.2.8.1: the
// budget was raised from 120 to 200 — the honest fence header grew, but
// the contract is unchanged: header + one clipped excerpt must fit and
// the block must never exceed the caller's bound.)
func TestResolveBudgetBounded(t *testing.T) {
	store := newCrossModeStore(t)

	blocks := Resolve(store, []Ref{{SessionID: findAgent(t, store)}}, "orders index", 200)

	if len(blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(blocks))
	}

	tokens := estimateTokensForTest(blocks[0].Content)
	if tokens > 200 {
		t.Fatalf("block exceeded the budget: %d > 200 tokens", tokens)
	}
}

// TestResolveRangesPinsRetrievalScope covers the explicit "read more"
// control: a range restricted to the first turn excludes the second.
// v1.2.9: the query must MATCH the in-range turn — when nothing in the
// pinned range is relevant, the reference resolves to NO block (the
// newest-turn fallback is gone: unrelated history is not a substitute
// for relevance).
func TestResolveRangesPinsRetrievalScope(t *testing.T) {
	store := newCrossModeStore(t)

	// Message indices: 0 user(deploy), 1 user(db) — user turns only.
	// Query matches the DEPLOY turn (turn 0) so the block travels; the
	// range then proves the orders turn (turn 1) stays out.
	blocks := Resolve(store,
		[]Ref{{SessionID: findAgent(t, store), Ranges: []Range{{From: 0, To: 1}}}},
		"deploy script release zip",
		1800,
	)

	if len(blocks) != 1 {
		t.Fatalf("blocks = %d, want 1 (in-range turn is relevant)", len(blocks))
	}

	if strings.Contains(blocks[0].Content, "orders") {
		t.Fatalf("out-of-range turn leaked in:\n%s", blocks[0].Content)
	}

	if !strings.Contains(blocks[0].Content, "deploy") {
		t.Fatalf("in-range turn missing:\n%s", blocks[0].Content)
	}
}

// TestResolveNeverMutatesSource pins that retrieval is read-only.
func TestResolveNeverMutatesSource(t *testing.T) {
	store := newCrossModeStore(t)

	id := findAgent(t, store)

	before, err := store.Get(id)
	if err != nil {
		t.Fatalf("Get before: %v", err)
	}

	_ = Resolve(store, []Ref{{SessionID: id}}, "deploy orders database", 1800)

	after, err := store.Get(id)
	if err != nil {
		t.Fatalf("Get after: %v", err)
	}

	if len(before.Messages) != len(after.Messages) {
		t.Fatal("source transcript changed during retrieval")
	}
}

// TestSearchFiltersByModeAndRanks pins the picker search: mode scoping,
// title relevance, and the summary fields surfacing.
func TestSearchFiltersByModeAndRanks(t *testing.T) {
	store := newCrossModeStore(t)

	// The picker index covers TITLES + SUMMARIES (never transcripts —
	// turn contents are retrieval territory, not picker territory). A
	// query with no relevance returns no noise hits.
	all := Search(store, "nonexistentwidget", "", 10)
	if len(all) != 0 {
		t.Fatalf("zero-relevance query must not leak sessions: %+v", all)
	}

	// Title-relevant hits surface with their mode attached.
	agentHit := Search(store, "deploy", "", 10)
	if len(agentHit) != 1 || agentHit[0].SessionID != findAgent(t, store) || agentHit[0].Mode != "agent" {
		t.Fatalf("agent title search wrong: %+v", agentHit)
	}

	chatHit := Search(store, "database", "", 10)
	if len(chatHit) != 1 || chatHit[0].SessionID != findChat(t, store) || chatHit[0].Mode != "chat" {
		t.Fatalf("chat title search wrong: %+v", chatHit)
	}

	// Empty query = browse mode: everything surfaces, newest first.
	browse := Search(store, "", "", 10)
	if len(browse) != 2 {
		t.Fatalf("browse mode returned %d hits, want 2", len(browse))
	}

	chatOnly := Search(store, "database", sessions.ModeChat, 10)
	if len(chatOnly) != 1 || chatOnly[0].SessionID != findChat(t, store) {
		t.Fatalf("chat-scoped search wrong: %+v", chatOnly)
	}

	agentOnly := Search(store, "deploy", sessions.ModeAgent, 10)
	if len(agentOnly) != 1 || agentOnly[0].SessionID != findAgent(t, store) {
		t.Fatalf("agent-scoped search wrong: %+v", agentOnly)
	}

	// Mode scoping is exclusive: a chat-only query in the agent scope
	// returns nothing.
	leaked := Search(store, "database", sessions.ModeAgent, 10)
	if len(leaked) != 0 {
		t.Fatalf("mode scope leaked: %+v", leaked)
	}
}

// TestNormalizeRefsDedupsAndCaps pins the ref hygiene.
func TestNormalizeRefsDedupsAndCaps(t *testing.T) {
	refs := NormalizeRefs([]Ref{
		{SessionID: "a"},
		{SessionID: ""},
		{SessionID: "a"},
		{SessionID: "b"},
		{SessionID: "c"},
		{SessionID: "d"},
		{SessionID: "e"},
	})

	if len(refs) != 4 {
		t.Fatalf("NormalizeRefs returned %d refs, want 4 (dedup + cap)", len(refs))
	}
	if refs[0].SessionID != "a" || refs[1].SessionID != "b" {
		t.Fatalf("order wrong: %+v", refs)
	}
}

func findAgent(t *testing.T, store *sessions.Store) string {
	t.Helper()
	list, _ := store.List()
	for _, s := range list {
		if s.Mode == sessions.ModeAgent {
			return s.ID
		}
	}
	t.Fatal("agent session missing")
	return ""
}

func findChat(t *testing.T, store *sessions.Store) string {
	t.Helper()
	list, _ := store.List()
	for _, s := range list {
		if s.Mode == sessions.ModeChat {
			return s.ID
		}
	}
	t.Fatal("chat session missing")
	return ""
}

// estimateTokensForTest mirrors chunking.EstimateTokens (rune/4) locally
// so the budget assertion does not depend on the estimator's package.
func estimateTokensForTest(s string) int {
	n := len([]rune(s))
	if n == 0 {
		return 0
	}
	return (n + 3) / 4
}
