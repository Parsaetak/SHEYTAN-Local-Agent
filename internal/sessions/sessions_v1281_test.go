// sessions_v1281_test.go — v1.2.8.1 store-level regressions: the atomic
// context mutation primitive (requirement 20: read-modify-write races on
// session context must serialize through the authoritative store) and the
// objective-refresh rule for trivial openers.
package sessions

import (
	"fmt"
	"sync"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

// TestUpdateContextFuncAtomicMerge pins the serialization contract: N
// concurrent context mutations (refs merges + a token-policy write) all
// survive — no lost update. Run with -race for the memory-model check.
func TestUpdateContextFuncAtomicMerge(t *testing.T) {
	store := New(t.TempDir())
	sess := store.CreateInMode(ModeChat)
	if err := store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	const writers = 24

	var wg sync.WaitGroup

	// Half the writers merge one history ref each (distinct sources).
	for i := 0; i < writers/2; i++ {
		wg.Add(1)

		go func(n int) {
			defer wg.Done()

			err := store.UpdateContextFunc(sess.ID, func(c *Context) (bool, error) {
				id := fmt.Sprintf("agent-src-%d", n)

				for _, r := range c.HistoryRefs {
					if r.SessionID == id {
						return false, nil
					}
				}

				c.HistoryRefs = append(c.HistoryRefs, HistoryRef{SessionID: id})
				return true, nil
			})
			if err != nil {
				t.Errorf("ref merge %d: %v", n, err)
			}
		}(i)
	}

	// Half the writers apply a distinct context-token policy.
	for i := 0; i < writers/2; i++ {
		wg.Add(1)

		go func(n int) {
			defer wg.Done()

			want := 1024 * (n + 1)

			err := store.UpdateContextFunc(sess.ID, func(c *Context) (bool, error) {
				if c.ContextTokens == want {
					return false, nil
				}
				c.ContextTokens = want
				return true, nil
			})
			if err != nil {
				t.Errorf("token policy %d: %v", n, err)
			}
		}(i)
	}

	wg.Wait()

	final, err := store.Get(sess.ID)
	if err != nil {
		t.Fatalf("final Get: %v", err)
	}

	// EVERY ref-merge survived (no whole-object Save clobbered them).
	if len(final.Context.HistoryRefs) != writers/2 {
		t.Fatalf("history refs = %d, want %d — a concurrent update was lost",
			len(final.Context.HistoryRefs), writers/2)
	}

	// The token policy survives too (last writer wins for THAT field).
	if final.Context.ContextTokens == 0 {
		t.Fatal("the context-token write was lost — concurrent context writes are not serialized")
	}
}

// TestSaveMessagesKeepContextPreservesContext pins the transcript-save
// primitive: SaveMessagesKeepContext persists messages while a concurrent
// context mutation applied between the caller's snapshot and the save
// survives (the store's context is authoritative, not the caller's copy).
func TestSaveMessagesKeepContextPreservesContext(t *testing.T) {
	store := New(t.TempDir())
	sess := store.CreateInMode(ModeAgent)
	if err := store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// The caller holds a STALE copy (no refs).
	stale, err := store.Get(sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// A concurrent writer attaches a ref.
	err = store.UpdateContextFunc(sess.ID, func(c *Context) (bool, error) {
		c.HistoryRefs = append(c.HistoryRefs, HistoryRef{SessionID: "agent-src"})
		return true, nil
	})
	if err != nil {
		t.Fatalf("concurrent ref merge: %v", err)
	}

	// The stale copy saves a new user message.
	stale.Messages = append(stale.Messages, llm.Message{Role: "user", Content: "persisted message"})

	if err := store.SaveMessagesKeepContext(stale); err != nil {
		t.Fatalf("SaveMessagesKeepContext: %v", err)
	}

	final, err := store.Get(sess.ID)
	if err != nil {
		t.Fatalf("final Get: %v", err)
	}

	if len(final.Messages) != 1 || final.Messages[0].Content != "persisted message" {
		t.Fatalf("message not persisted: %+v", final.Messages)
	}

	if len(final.Context.HistoryRefs) != 1 || final.Context.HistoryRefs[0].SessionID != "agent-src" {
		t.Fatalf("the concurrent context mutation was reverted by the save: %+v", final.Context.HistoryRefs)
	}
}

// TestObjectiveRefreshesFromTrivialOpener pins the v1.2.8.1 summary fix:
// a session that opens with "hi" must not carry that as its objective
// forever — a substantially longer later user message supersedes it.
func TestObjectiveRefreshesFromTrivialOpener(t *testing.T) {
	sum := UpdateSummaryFromTurn(nil, "s1", ModeChat, "hi", "Hello! How can I help?", nil)
	if sum.Objective != "hi" {
		t.Fatalf("precondition: objective = %q, want the trivial opener", sum.Objective)
	}

	sum = UpdateSummaryFromTurn(sum, "s1", ModeChat,
		"Refactor the storage layer to use the new session store API",
		"Refactored the storage layer.", nil)

	if sum.Objective != "Refactor the storage layer to use the new session store API" {
		t.Fatalf("objective not refreshed from the trivial opener: %q", sum.Objective)
	}

	// Once non-trivial, the objective is stable.
	sum = UpdateSummaryFromTurn(sum, "s1", ModeChat, "another completely different long task", "Done.", nil)
	if sum.Objective != "Refactor the storage layer to use the new session store API" {
		t.Fatalf("a non-trivial objective must stay stable, got %q", sum.Objective)
	}
}
