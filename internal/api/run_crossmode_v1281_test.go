// run_crossmode_v1281_test.go — v1.2.8.1 API-level regressions for the
// cross-mode history contract, run/REGENERATE reference parity, server-
// side same-mode enforcement, read-modify-write serialization, and the
// run_snapshot wire frame carrying the task state.
package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/sessions"
)

// seedAgentHistory creates an agent session with one distinct exchange and
// a summary, returning its id + the summary version.
func seedAgentHistory(t *testing.T, srv *Server, server *httptest.Server) (string, int) {
	t.Helper()

	agentSess := createSessionWithMode(t, server, "agent")
	agentID := agentSess["id"].(string)

	if _, err := srv.store.AppendMessage(agentID, llm.Message{Role: "user", Content: "We chose the composite index on orders for the checkout path."}); err != nil {
		t.Fatalf("seed agent turn: %v", err)
	}
	if _, err := srv.store.AppendMessage(agentID, llm.Message{Role: "assistant", Content: "The orders index is live in the checkout path."}); err != nil {
		t.Fatalf("seed agent reply: %v", err)
	}

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

	return agentID, sum.Version
}

// TestRegenerateKeepsHistoryReferences pins requirement 9: a regenerated
// response uses the SAME persisted cross-mode history references as the
// original run — the regenerate request carries no refs and must still
// retrieve them (persisted-context fallback), and the prompt proves it.
func TestRegenerateKeepsHistoryReferences(t *testing.T) {
	engine, cap := newCapturingEngine(t)
	srv, server := newRemoteServerWithHandle(t, engine.URL)

	agentID, sumVer := seedAgentHistory(t, srv, server)

	chat := createSessionWithMode(t, server, "chat")
	chatID := chat["id"].(string)

	body := fmt.Sprintf(
		`{"sessionId":%q,"message":"Remind me what we decided about the orders index?","historyRefs":[{"sessionId":%q,"mode":"agent","summaryVersion":%d}]}`,
		chatID, agentID, sumVer,
	)

	runResp, err := http.Post(server.URL+"/api/run", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/run: %v", err)
	}
	runResp.Body.Close()

	if !waitForSummarySettled(t, server, chatID) {
		t.Fatal("the chat run never settled its summary")
	}

	if reqs := cap.snapshot(); len(reqs) == 0 || !strings.Contains(flattenRequestMessages(reqs[0]), "[AUTOMATED REFERENCE-DATA ATTACHMENT") {
		t.Fatal("precondition: the original run must carry the history block")
	}

	// REGENERATE: no historyRefs in the body — the persisted context is
	// the authority now.
	regResp, err := http.Post(
		server.URL+"/api/run",
		"application/json",
		strings.NewReader(fmt.Sprintf(`{"sessionId":%q,"message":"","regenerate":true}`, chatID)),
	)
	if err != nil {
		t.Fatalf("POST /api/run (regenerate): %v", err)
	}
	if regResp.StatusCode != http.StatusOK {
		buf := make([]byte, 512)
		n, _ := regResp.Body.Read(buf)
		t.Fatalf("regenerate POST status = %d: %s", regResp.StatusCode, string(buf[:n]))
	}
	regResp.Body.Close()

	// The regenerate run replaces the transcript's assistant tail; wait
	// for ITS engine request specifically — the summary already exists
	// from run 1, so a summary poll would return instantly.
	var regenReq map[string]any

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		reqs := cap.snapshot()
		if len(reqs) >= 2 {
			regenReq = reqs[len(reqs)-1]
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if regenReq == nil {
		t.Fatalf("engine requests = %d, want >= 2 — the regenerate run never reached the engine", len(cap.snapshot()))
	}

	regenPrompt := flattenRequestMessages(regenReq)

	if !strings.Contains(regenPrompt, "[AUTOMATED REFERENCE-DATA ATTACHMENT") ||
		!strings.Contains(regenPrompt, "source-mode: agent") {
		t.Fatalf("the regenerated turn LOST the cross-mode history context:\n---\n%s\n---", regenPrompt)
	}

	// The refs survive on the context after regeneration.
	chatFull, err := srv.store.Get(chatID)
	if err != nil {
		t.Fatalf("Get chat: %v", err)
	}
	if len(chatFull.Context.HistoryRefs) == 0 {
		t.Fatal("historyRefs vanished from the session context after regenerate")
	}

	// Deterministic settle-tail sync (mirrors the sibling tests): wait for
	// run 2's summary roll (version bumps 1 → 2) so the goroutine tail
	// cannot race t.TempDir cleanup with a late sidecar write.
	tailDeadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(tailDeadline) {
		if s, err := srv.store.Summary(chatID); err == nil && s != nil && s.Version >= 2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal("the regenerate run's settle tail never completed (summary version did not bump)")
}

// TestSameModeReferenceDroppedServerSide pins server-side cross-mode
// enforcement: an AGENT session referencing ANOTHER AGENT session is
// dropped by the backend (the client payload is never the authority), no
// history block is retrieved, and nothing is persisted.
func TestSameModeReferenceDroppedServerSide(t *testing.T) {
	engine, cap := newCapturingEngine(t)
	srv, server := newRemoteServerWithHandle(t, engine.URL)

	otherAgent, _ := seedAgentHistory(t, srv, server)

	agent := createSessionWithMode(t, server, "agent")
	agentID := agent["id"].(string)

	body := fmt.Sprintf(
		`{"sessionId":%q,"message":"What did the other session decide?","historyRefs":[{"sessionId":%q,"mode":"agent"}]}`,
		agentID, otherAgent,
	)

	runResp, err := http.Post(server.URL+"/api/run", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/run: %v", err)
	}
	runResp.Body.Close()

	if !waitForSummarySettled(t, server, agentID) {
		t.Fatal("run never settled")
	}

	reqs := cap.snapshot()
	if len(reqs) == 0 {
		t.Fatal("no engine request")
	}

	if strings.Contains(flattenRequestMessages(reqs[0]), "[AUTOMATED REFERENCE-DATA ATTACHMENT") {
		t.Fatal("a same-mode reference was retrieved — cross-mode enforcement broken")
	}

	full, err := srv.store.Get(agentID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(full.Context.HistoryRefs) != 0 {
		t.Fatalf("a same-mode reference persisted on the context: %+v", full.Context.HistoryRefs)
	}
}

// TestSessionContextConcurrentMutationsSurvive is the requirement-20 race
// regression: concurrent context mutations (history refs + context token
// policy) applied through the serialized store path never overwrite each
// other. Run with -race for the memory-model check on top.
func TestSessionContextConcurrentMutationsSurvive(t *testing.T) {
	engine := remoteFakeEngine(t, "unused.")
	srv, server := newRemoteServerWithHandle(t, engine.URL)

	created := createSessionWithMode(t, server, "chat")
	chatID := created["id"].(string)

	// v1.2.9: the writer attaches REAL agent sessions — the server now
	// prunes dead references (source session not in the store), so
	// hand-invented ids would be correctly dropped.
	srcIDs := make([]string, 3)
	for i := range srcIDs {
		src := srv.store.CreateInMode("agent")
		src.Title = fmt.Sprintf("agent source %d", i)
		if err := srv.store.Save(src); err != nil {
			t.Fatalf("seed source session %d: %v", i, err)
		}
		srcIDs[i] = src.ID
	}

	const rounds = 24

	var wg sync.WaitGroup
	errCh := make(chan error, rounds*2)

	// Writer A: atomic historyRefs deltas (attach → attach → attach…).
	wg.Add(1)
	go func() {
		defer wg.Done()

		for i := 0; i < rounds; i++ {
			payload := fmt.Sprintf(
				`{"historyRefs":[{"sessionId":%q,"mode":"agent"}]}`,
				srcIDs[i%3], // three distinct sources, re-attached repeatedly
			)

			req, err := http.NewRequest(http.MethodPut,
				server.URL+"/api/sessions/"+chatID, strings.NewReader(payload))
			if err != nil {
				errCh <- err
				return
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				errCh <- err
				return
			}
			resp.Body.Close()
		}
	}()

	// Writer B: token-policy PUTs (the /context subresource), which under
	// the old Get-copy/Update-whole-context shape reverted writer A.
	wg.Add(1)
	go func() {
		defer wg.Done()

		for i := 0; i < rounds; i++ {
			payload := fmt.Sprintf(`{"contextTokens":%d}`, 2048+i%3*1024)

			req, err := http.NewRequest(http.MethodPut,
				server.URL+"/api/sessions/"+chatID+"/context", strings.NewReader(payload))
			if err != nil {
				errCh <- err
				return
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				errCh <- err
				return
			}
			resp.Body.Close()
		}
	}()

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent mutation failed: %v", err)
	}

	final, err := srv.store.Get(chatID)
	if err != nil {
		t.Fatalf("final Get: %v", err)
	}

	// BOTH writers' last state survives — neither silently reverted the
	// other (the read-modify-write hazard this repair removes).
	if final.Context.ContextTokens == 0 {
		t.Fatal("the token-policy updates were lost — concurrent context writes are not serialized")
	}

	if len(final.Context.HistoryRefs) == 0 {
		t.Fatal("the historyRefs updates were lost — concurrent context writes are not serialized")
	}

	found := false
	for _, ref := range final.Context.HistoryRefs {
		for _, id := range srcIDs {
			if ref.SessionID == id {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("unexpected refs on the context: %+v", final.Context.HistoryRefs)
	}
}

// TestRunSnapshotWireFrameCarriesTask closes the v1.2.8 wire gap: the
// task state folds into the authoritative snapshot, and the run_snapshot
// frame a RECONNECTING socket receives actually carries it.
func TestRunSnapshotWireFrameCarriesTask(t *testing.T) {
	engine := slowStreamEngine(t, []string{"working", " still"}, 120*time.Millisecond)
	srv, server := newRemoteServerWithHandle(t, engine.URL)
	sessionID := createSessionForRun(t, server)

	runResp, err := http.Post(
		server.URL+"/api/run",
		"application/json",
		strings.NewReader(fmt.Sprintf(`{"sessionId":%q,"message":"task wire"}`, sessionID)),
	)
	if err != nil {
		t.Fatalf("POST /api/run: %v", err)
	}
	runResp.Body.Close()

	srv.runsMu.Lock()
	rs := srv.runs[sessionID]
	srv.runsMu.Unlock()

	if rs == nil || rs.live == nil {
		t.Fatal("no active run right after POST — precondition broken")
	}

	rs.live.observe(newTestTaskActivity())

	// A RECONNECTING client: a fresh WS attach mid-run.
	conn := dialActivityWS(t, server, sessionID)
	defer conn.Close()

	var frame map[string]any

	// Skip the attach ack, then find the run_snapshot.
	deadlineFrames := 0
	for {
		frame = readFrameWithDeadline(t, conn)
		deadlineFrames++
		if frame["type"] == "run_snapshot" || deadlineFrames > 5 {
			break
		}
	}

	if frame["type"] != "run_snapshot" {
		t.Fatalf("expected run_snapshot, got %v", frame["type"])
	}

	task, present := frame["task"].(map[string]any)
	if !present {
		t.Fatalf("the run_snapshot wire frame does not carry the task block: %v", frame)
	}

	if task["currentStep"] != "ACT: go build ./..." {
		t.Fatalf("wire task step = %v", task["currentStep"])
	}

	files := asStringSlice(t, task["filesChanged"])
	if len(files) != 1 || files[0] != "main.go" {
		t.Fatalf("wire task filesChanged = %v", files)
	}

	// Settle tail hygiene (mirrors sibling tests).
	if !waitForReplyPersisted(t, server, sessionID) {
		t.Fatal("the run never persisted its reply")
	}
	if !waitForSummarySettled(t, server, sessionID) {
		t.Fatal("the settle tail (summary sidecar) never completed")
	}
}
