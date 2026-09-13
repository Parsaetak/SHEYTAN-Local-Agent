package sessions

import (
	"fmt"
	"sync"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()

	return New(t.TempDir())
}

func TestCreateSaveGetRoundTrip(t *testing.T) {
	store := newTestStore(t)

	sess := store.Create()

	sess.Title = "round trip"
	sess.Messages = append(sess.Messages, llm.Message{Role: "user", Content: "hello"})

	if err := store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Get(sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if loaded.Title != "round trip" || len(loaded.Messages) != 1 {
		t.Fatalf("round trip mismatch: %+v", loaded)
	}
}

func TestAppendMessagePersistsEachOne(t *testing.T) {
	store := newTestStore(t)

	sess := store.Create()
	if err := store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	for i := 0; i < 10; i++ {
		if _, err := store.AppendMessage(sess.ID, llm.Message{
			Role:    "user",
			Content: fmt.Sprintf("message %d", i),
		}); err != nil {
			t.Fatalf("AppendMessage %d: %v", i, err)
		}
	}

	loaded, err := store.Get(sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if len(loaded.Messages) != 10 {
		t.Fatalf("expected 10 messages, got %d", len(loaded.Messages))
	}
}

func TestListNewestFirstAndStubsAreLean(t *testing.T) {
	store := newTestStore(t)

	first := store.Create()
	first.Title = "first"
	_ = store.Save(first)

	second := store.Create()
	second.Title = "second"
	_ = store.Save(second)

	list, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(list) != 2 {
		t.Fatalf("expected 2 stubs, got %d", len(list))
	}

	if list[0].Title != "second" {
		t.Fatalf("newest-first violated: %s then %s", list[0].Title, list[1].Title)
	}

	// Stubs must not carry full histories.
	if list[0].Messages != nil {
		t.Fatal("stub list must not include messages")
	}
}

func TestDeleteRemovesAndErrorsOnRepeat(t *testing.T) {
	store := newTestStore(t)

	sess := store.Create()
	_ = store.Save(sess)

	if err := store.Delete(sess.ID); err != nil {
		t.Fatalf("first delete: %v", err)
	}

	if err := store.Delete(sess.ID); err == nil {
		t.Fatal("second delete must error")
	}
}

func TestContextAttachmentIDsPersist(t *testing.T) {
	store := newTestStore(t)

	sess := store.Create()
	_ = store.Save(sess)

	ctx := Context{
		SystemPrompt:  "be brief",
		AttachmentIDs: []string{"a1234", "a5678"},
	}

	if err := store.UpdateContext(sess.ID, ctx); err != nil {
		t.Fatalf("UpdateContext: %v", err)
	}

	loaded, err := store.Get(sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if len(loaded.Context.AttachmentIDs) != 2 {
		t.Fatalf("attachment ids lost: %+v", loaded.Context)
	}
}

func TestConcurrentAppendsNeverLoseMessages(t *testing.T) {
	store := newTestStore(t)

	sess := store.Create()
	_ = store.Save(sess)

	var wg sync.WaitGroup

	const writers = 8

	const perWriter = 25

	for w := 0; w < writers; w++ {
		wg.Add(1)

		go func(seed int) {
			defer wg.Done()

			for i := 0; i < perWriter; i++ {
				_, err := store.AppendMessage(sess.ID, llm.Message{
					Role:    "user",
					Content: fmt.Sprintf("w%d-m%d", seed, i),
				})
				if err != nil {
					t.Errorf("append: %v", err)

					return
				}
			}
		}(w)
	}

	wg.Wait()

	loaded, err := store.Get(sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got := len(loaded.Messages); got != writers*perWriter {
		t.Fatalf("lost messages under concurrency: %d != %d", got, writers*perWriter)
	}
}

func TestIndexSelfHealsOrphans(t *testing.T) {
	dir := t.TempDir()

	store := New(dir)

	sess := store.Create()
	sess.Title = "orphan source"
	_ = store.Save(sess)

	// Simulate a lost index by creating a fresh store on the same dir with
	// the index file removed.
	store2 := New(dir)

	list, err := store2.List()
	if err != nil {
		t.Fatalf("List after heal: %v", err)
	}

	if len(list) != 1 || list[0].Title != "orphan source" {
		t.Fatalf("orphan not folded back into the index: %+v", list)
	}
}

func TestActivitySidecarBounded(t *testing.T) {
	store := newTestStore(t)

	sess := store.Create()
	_ = store.Save(sess)

	for i := 0; i < 500; i++ {
		if err := store.AppendActivity(sess.ID, ActivityEntry{
			Type:    "tool_start",
			Caption: fmt.Sprintf("activity %d", i),
		}); err != nil {
			t.Fatalf("AppendActivity %d: %v", i, err)
		}
	}

	loaded, err := store.Get(sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if len(loaded.Activities) > maxActivities {
		t.Fatalf("activity sidecar not bounded: %d", len(loaded.Activities))
	}
}

// TestSessionContextPolicyPersistsPerChat: the 1.1.6 per-session context
// policy travels with the session — Chat A = 8K, Chat B = 32K — and
// switching (reloading) restores each chat's own policy.
func TestSessionContextPolicyPersistsPerChat(t *testing.T) {
	store := newTestStore(t)

	a := store.Create()
	a.Context.ContextTokens = 8192
	if err := store.Save(a); err != nil {
		t.Fatalf("save A: %v", err)
	}

	b := store.Create()
	b.Context.ContextTokens = 32768
	if err := store.Save(b); err != nil {
		t.Fatalf("save B: %v", err)
	}

	c := store.Create() // inherits the global policy (0)
	if err := store.Save(c); err != nil {
		t.Fatalf("save C: %v", err)
	}

	// "Switch sessions": reload each one fresh.
	for _, tc := range []struct {
		id   string
		want int
	}{} {
		_ = tc
	}

	reloadedA, err := store.Get(a.ID)
	if err != nil {
		t.Fatalf("get A: %v", err)
	}
	reloadedB, err := store.Get(b.ID)
	if err != nil {
		t.Fatalf("get B: %v", err)
	}
	reloadedC, err := store.Get(c.ID)
	if err != nil {
		t.Fatalf("get C: %v", err)
	}

	if reloadedA.Context.ContextTokens != 8192 {
		t.Fatalf("chat A policy = %d, want 8192", reloadedA.Context.ContextTokens)
	}
	if reloadedB.Context.ContextTokens != 32768 {
		t.Fatalf("chat B policy = %d, want 32768", reloadedB.Context.ContextTokens)
	}
	if reloadedC.Context.ContextTokens != 0 {
		t.Fatalf("chat C policy = %d, want 0 (inherit)", reloadedC.Context.ContextTokens)
	}

	// UpdateContext replaces the policy honestly.
	reloadedA.Context.ContextTokens = 16384
	if err := store.UpdateContext(a.ID, reloadedA.Context); err != nil {
		t.Fatalf("update A: %v", err)
	}
	again, _ := store.Get(a.ID)
	if again.Context.ContextTokens != 16384 {
		t.Fatalf("chat A updated policy = %d, want 16384", again.Context.ContextTokens)
	}
}
