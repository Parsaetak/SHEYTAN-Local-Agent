// run_settlement_test.go — v1.2.8 integration: an AGENT run with real tool
// evidence settles → the rolling summary updates AND the workspace agent.md
// handoff is written (the §8 mandatory handoff contract, end to end).
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
)

// sseChunk builds one OpenAI-compatible SSE data frame (api-local copy of
// the internal/agent test helper).
func sseChunk(content string) string {
	delta := map[string]any{"role": "assistant"}

	if content != "" {
		delta["content"] = content
	}

	body, _ := json.Marshal(map[string]any{
		"id":      "chatcmpl-test",
		"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": nil}},
	})

	return "data: " + string(body) + "\n\n"
}

func sseToolCall(id, name, args string) string {
	body, _ := json.Marshal(map[string]any{
		"id": "chatcmpl-test",
		"choices": []map[string]any{{
			"index": 0,
			"delta": map[string]any{
				"tool_calls": []map[string]any{{
					"index": 0,
					"id":    id,
					"type":  "function",
					"function": map[string]any{
						"name":      name,
						"arguments": args,
					},
				}},
			},
		}},
	})

	return "data: " + string(body) + "\n\n"
}

const sseDone = "data: [DONE]\n\n"

// toolStreamEngine streams a tool call on turn 1 and a plain answer on
// turn 2 — an OpenAI-compatible fake the API server runs against.
func toolStreamEngine(t *testing.T) *httptest.Server {
	t.Helper()

	turns := 0

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		turns++

		var frames []string

		if turns == 1 {
			frames = []string{
				sseChunk(""),
				sseToolCall("call-1", "files", `{"action":"write","path":"evidence.txt"}`),
			}
		} else {
			frames = []string{sseChunk("The file is written and verified.")}
		}

		for _, frame := range frames {
			_, _ = w.Write([]byte(frame))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}

		_, _ = w.Write([]byte(sseDone))
	}))
}

// TestAgentRunSettlesWithSummaryAndHandoff pins the settlement contract:
// files tool evidence + done outcome → agent.md handoff in the workspace
// and a version-bumped session summary.
func TestAgentRunSettlesWithSummaryAndHandoff(t *testing.T) {
	engine := toolStreamEngine(t)
	t.Cleanup(engine.Close)

	srv, server := newRemoteServerWithHandle(t, engine.URL)

	cfg := srv.src.Load()
	wsRoot := cfg.EffectiveWorkspaceRoot()

	created := createSessionWithMode(t, server, "agent")
	id := created["id"].(string)

	runResp, err := http.Post(
		server.URL+"/api/run",
		"application/json",
		strings.NewReader(fmt.Sprintf(`{"sessionId":%q,"message":"Write the evidence file. You must not touch anything else."}`, id)),
	)
	if err != nil {
		t.Fatalf("POST /api/run: %v", err)
	}
	runResp.Body.Close()

	if !waitForReplyPersisted(t, server, id) {
		t.Fatal("the agent run never persisted its reply")
	}

	if !waitForSummarySettled(t, server, id) {
		t.Fatal("the rolling summary never settled")
	}

	// 1. The rolling summary carries the objective + constraints.
	sumResp, err := http.Get(server.URL + "/api/sessions/" + id + "/summary")
	if err != nil {
		t.Fatalf("GET summary: %v", err)
	}
	defer sumResp.Body.Close()

	var summary map[string]any
	if err := json.NewDecoder(sumResp.Body).Decode(&summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}

	if v, _ := summary["version"].(float64); v < 1 {
		t.Fatalf("summary version = %v, want >= 1", summary["version"])
	}

	if obj, _ := summary["objective"].(string); !strings.Contains(obj, "evidence") {
		t.Fatalf("objective = %q (should carry the user request)", obj)
	}

	// 2. The workspace agent.md handoff was written with REAL evidence.
	path := filepath.Join(wsRoot, agent.HandoffFileName)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("agent.md handoff missing at %s: %v", path, err)
	}

	handoff := string(data)

	for _, want := range []string{
		HandoffBeginMarkerWire,
		"# Latest Agent Handoff",
		"## Files changed",
		"evidence.txt",
		"run outcome: done",
	} {
		if !strings.Contains(handoff, want) {
			t.Fatalf("handoff missing %q:\n%s", want, handoff)
		}
	}

	// 3. The task state rode the run result: the activities sidecar has a
	// task milestone (persisted non-delta event).
	sess, err := srv.store.Get(id)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}

	sawTask := false
	for _, a := range sess.Activities {
		if a.Type == "task" {
			sawTask = true
			break
		}
	}
	if !sawTask {
		t.Fatalf("no persisted task milestone among %d activities", len(sess.Activities))
	}
}

// HandoffBeginMarkerWire re-exports the marker for the wire test without
// importing the agent package's constant twice.
const HandoffBeginMarkerWire = agent.HandoffBeginMarker

var _ = time.Now
