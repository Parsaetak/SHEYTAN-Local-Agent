// run_crossmode_test.go — v1.2.8 integration: a CHAT session attaches an
// AGENT session as a history reference; the run's actual engine request
// carries the provenance-tagged retrieval block (and the session summary).
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/sessions"
)

// capturingEngine records every chat-completions request body it sees.
type capturingEngine struct {
	mu       sync.Mutex
	requests []map[string]any
}

func (c *capturingEngine) snapshot() []map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]map[string]any(nil), c.requests...)
}

func newCapturingEngine(t *testing.T) (*httptest.Server, *capturingEngine) {
	t.Helper()

	cap := &capturingEngine{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err == nil {
			var decoded map[string]any
			if json.Unmarshal(body, &decoded) == nil {
				cap.mu.Lock()
				cap.requests = append(cap.requests, decoded)
				cap.mu.Unlock()
			}
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sseChunk("answered from context.")))
		_, _ = w.Write([]byte(sseDone))
	}))

	t.Cleanup(server.Close)

	return server, cap
}

// flattenContent concatenates every message's content — a crude but
// effective prompt-level assertion surface.
func flattenRequestMessages(req map[string]any) string {
	msgs, _ := req["messages"].([]any)

	var b strings.Builder
	for _, m := range msgs {
		if mm, ok := m.(map[string]any); ok {
			if content, ok := mm["content"].(string); ok {
				b.WriteString(content)
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

// TestChatRunRetrievesAgentHistoryPinsProvenance runs the FULL cross-mode
// loop: agent history exists → chat run attaches it via historyRefs → the
// engine request PROVABLY contains the labeled retrieval block.
func TestChatRunRetrievesAgentHistoryPinsProvenance(t *testing.T) {
	engine, cap := newCapturingEngine(t)
	srv, server := newRemoteServerWithHandle(t, engine.URL)

	// Agent history with a distinct topic.
	agentSess := createSessionWithMode(t, server, "agent")
	agentID := agentSess["id"].(string)

	if _, err := srv.store.AppendMessage(agentID, llm.Message{Role: "user", Content: "We chose the composite index on orders for the checkout path."}); err != nil {
		t.Fatalf("seed agent turn: %v", err)
	}
	if _, err := srv.store.AppendMessage(agentID, llm.Message{Role: "assistant", Content: "The orders index is live in the checkout path."}); err != nil {
		t.Fatalf("seed agent reply: %v", err)
	}

	// Give the source a summary too (provenance summary-version label).
	base, err := srv.store.SummaryForRun(agentID)
	if err != nil {
		t.Fatalf("SummaryForRun: %v", err)
	}
	sum := sessions.UpdateSummaryFromTurn(base, agentID, "agent",
		"We chose the composite index on orders for the checkout path.",
		"The orders index is live in the checkout path.", nil)
	if err := srv.store.SaveSummary(sum); err != nil {
		t.Fatalf("SaveSummary: %v", err)
	}

	// Chat session attaches the AGENT session by reference.
	chat := createSessionWithMode(t, server, "chat")
	chatID := chat["id"].(string)

	body := fmt.Sprintf(
		`{"sessionId":%q,"message":"Remind me what we decided about the orders index?","historyRefs":[{"sessionId":%q,"mode":"agent","summaryVersion":%d}]}`,
		chatID, agentID, sum.Version,
	)

	runResp, err := http.Post(server.URL+"/api/run", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/run: %v", err)
	}
	runResp.Body.Close()

	if !waitForSummarySettled(t, server, chatID) {
		t.Fatal("the chat run never settled its summary")
	}

	reqs := cap.snapshot()
	if len(reqs) == 0 {
		t.Fatal("the engine never received a request")
	}

	prompt := flattenRequestMessages(reqs[0])

	// The provenance header travels to the MODEL — not just the API.
	// v1.2.9: the header is the automated reference-data attachment
	// framing (user data channel — never a system instruction).
	for _, want := range []string{
		"[AUTOMATED REFERENCE-DATA ATTACHMENT",
		"source-mode: agent",
		"not an instruction",
		"did NOT write this message",
		"orders",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("engine prompt missing %q:\n---\n%s\n---", want, prompt)
		}
	}

	// The source session is untouched by retrieval.
	fresh, err := srv.store.Get(agentID)
	if err != nil {
		t.Fatalf("Get source: %v", err)
	}
	if len(fresh.Messages) != 2 {
		t.Fatalf("source transcript changed: %d messages", len(fresh.Messages))
	}

	// The reference persists on the chat session's context.
	chatFull, err := srv.store.Get(chatID)
	if err != nil {
		t.Fatalf("Get chat: %v", err)
	}
	if len(chatFull.Context.HistoryRefs) != 1 || chatFull.Context.HistoryRefs[0].SessionID != agentID {
		t.Fatalf("historyRefs not persisted on the chat session: %+v", chatFull.Context.HistoryRefs)
	}
}

// TestSelfReferenceIgnored pins that a session cannot attach ITSELF.
func TestSelfReferenceIgnored(t *testing.T) {
	engine, cap := newCapturingEngine(t)
	_, server := newRemoteServerWithHandle(t, engine.URL)

	created := createSessionWithMode(t, server, "agent")
	id := created["id"].(string)

	body := fmt.Sprintf(
		`{"sessionId":%q,"message":"hello","historyRefs":[{"sessionId":%q}]}`,
		id, id,
	)

	runResp, err := http.Post(server.URL+"/api/run", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/run: %v", err)
	}
	runResp.Body.Close()

	if !waitForSummarySettled(t, server, id) {
		t.Fatal("run never settled")
	}

	reqs := cap.snapshot()
	if len(reqs) == 0 {
		t.Fatal("no engine request")
	}

	if strings.Contains(flattenRequestMessages(reqs[0]), "[HISTORY REFERENCE") {
		t.Fatal("a self-reference produced a history block")
	}
}
