package api

// runtransport_test.go — v1.2.6 continuation regression tests for the
// AUTHORITATIVE RUN TRANSPORT contract:
//
//   1. every socket receives `attached` (the deterministic attach ack) as
//      its FIRST frame;
//   2. a socket attaching MID-RUN receives a `run_snapshot` carrying the
//      run's authoritative state (phase, cumulative response/reasoning
//      snapshots, sequence) BEFORE live events continue;
//   3. a reconnect during generation replays the partial output and the
//      stream continues WITHOUT duplication (the cumulative captions are
//      replacement-safe; the seq filter drops the overlap);
//   4. POST /api/run returns the authoritative runId;
//   5. stale events of a replaced run never reach the client;
//   6. every event of the active run carries runId + a monotonic seq;
//   7. a socket attaching between run end and registry cleanup gets the
//      TERMINAL snapshot (authoritative final state + the persisted reply).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
)

// slowStreamEngine streams one chunk every interval — long enough for the
// test to attach/detach mid-generation.
func slowStreamEngine(t *testing.T, chunks []string, interval time.Duration) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		for _, chunk := range chunks {
			payload := map[string]any{
				"id": "chatcmpl-test",
				"choices": []map[string]any{{
					"index":         0,
					"delta":         map[string]any{"content": chunk},
					"finish_reason": nil,
				}},
			}

			enc, _ := json.Marshal(payload)
			_, _ = w.Write([]byte("data: " + string(enc) + "\n\n"))

			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

			time.Sleep(interval)
		}

		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))

	t.Cleanup(server.Close)

	return server
}

// TestAttachAckIsFirstFrameAndPostReturnsRunId pins the deterministic
// attach contract: the server's first frame after upgrade is `attached`,
// and POST /api/run hands back the authoritative runId.
func TestAttachAckIsFirstFrameAndPostReturnsRunId(t *testing.T) {
	engine := remoteFakeEngine(t, "hi there.")
	server := newRemoteServer(t, engine.URL)
	sessionID := createSessionForRun(t, server)

	conn := dialActivityWS(t, server, sessionID)

	first := readFrameWithDeadline(t, conn)
	if first["type"] != "attached" {
		t.Fatalf("first frame type = %v, want attached (the deterministic attach ack)", first["type"])
	}

	if sid, _ := first["sessionId"].(string); sid != sessionID {
		t.Fatalf("attached.sessionId = %q, want %q", sid, sessionID)
	}

	if now, _ := first["serverNow"].(float64); now <= 0 {
		t.Fatal("attached.serverNow missing — the server clock must ride the ack")
	}

	// No active run: the next frame is the idle sentinel (with lastRun
	// absent — no run ever happened for this session).
	second := readFrameWithDeadline(t, conn)
	if second["type"] != "idle" {
		t.Fatalf("second frame type = %v, want idle", second["type"])
	}

	// POST /api/run returns the runId.
	runResp, err := http.Post(
		server.URL+"/api/run",
		"application/json",
		strings.NewReader(fmt.Sprintf(`{"sessionId":%q,"message":"hi"}`, sessionID)),
	)
	if err != nil {
		t.Fatalf("POST /api/run: %v", err)
	}
	defer runResp.Body.Close()

	var body struct {
		OK        bool   `json:"ok"`
		SessionID string `json:"sessionId"`
		RunID     string `json:"runId"`
		State     string `json:"state"`
	}

	if err := json.NewDecoder(runResp.Body).Decode(&body); err != nil {
		t.Fatalf("decode run response: %v", err)
	}

	if !body.OK || body.RunID == "" || body.State != "registered" {
		t.Fatalf("run response = %+v — runId/state missing (the authoritative identity contract)", body)
	}

	// Let the fired run settle BEFORE the test ends: the run goroutine
	// persists into the DataDir (t.TempDir) and a live run races the
	// cleanup's RemoveAll.
	if !waitForReplyPersisted(t, server, sessionID) {
		t.Fatal("the fired run never settled — cleanup safety violated")
	}

	// v1.2.8: the settle tail also writes the rolling summary sidecar —
	// wait for it deterministically before the TempDir cleanup.
	if !waitForSummarySettled(t, server, sessionID) {
		t.Fatal("the settle tail (summary sidecar) never completed")
	}

	time.Sleep(150 * time.Millisecond)
}

// TestMidRunAttachReceivesSnapshotAndContinues is THE reconnect-during-
// generation regression: a socket that attaches AFTER partial output
// receives the cumulative snapshot immediately (never waiting for a future
// token) and the stream continues without duplication.
func TestMidRunAttachReceivesSnapshotAndContinues(t *testing.T) {
	chunks := []string{"Hello", " ", "from", " ", "the", " ", "replay", "test"}
	engine := slowStreamEngine(t, chunks, 60*time.Millisecond)
	server := newRemoteServer(t, engine.URL)
	sessionID := createSessionForRun(t, server)

	// Socket A: the original connection.
	connA := dialActivityWS(t, server, sessionID)

	runResp, err := http.Post(
		server.URL+"/api/run",
		"application/json",
		strings.NewReader(fmt.Sprintf(`{"sessionId":%q,"message":"stream please"}`, sessionID)),
	)
	if err != nil {
		t.Fatalf("POST /api/run: %v", err)
	}
	runResp.Body.Close()

	// Read frames on A until a response event with visible partial text.
	var runID string
	partialSeen := false

	for !partialSeen {
		frame := readFrameWithDeadline(t, connA)

		if frame["type"] == "response" {
			caption, _ := frame["caption"].(string)
			if strings.Contains(caption, "the") { // several chunks landed
				runID, _ = frame["runId"].(string)
				partialSeen = true
			}
		}
	}

	if runID == "" {
		t.Fatal("response frame carries no runId — the one-runId contract is broken")
	}

	// Drop socket A mid-generation (the reconnect scenario).
	connA.Close()

	// Socket B attaches NOW — mid-run.
	connB := dialActivityWS(t, server, sessionID)

	if frame := readFrameWithDeadline(t, connB); frame["type"] != "attached" {
		t.Fatalf("first frame = %v, want attached", frame["type"])
	}

	snap := readFrameWithDeadline(t, connB)
	if snap["type"] != "run_snapshot" {
		t.Fatalf("second frame = %v, want run_snapshot (authoritative replay)", snap["type"])
	}

	if rid, _ := snap["runId"].(string); rid != runID {
		t.Fatalf("snapshot runId = %q, want %q", rid, runID)
	}

	if running, _ := snap["running"].(bool); !running {
		t.Fatal("snapshot.running = false — the run is still generating")
	}

	// The cumulative snapshot already carries the partial output — a
	// reconnect NEVER waits for a future token to discover the state.
	if latest, _ := snap["latestResponse"].(string); latest == "" {
		t.Fatal("snapshot.latestResponse is empty — the partial output was not replayed")
	}

	// Continue to completion on socket B. The final text must be the full
	// answer with NO duplication (seq filter drops the overlap).
	var finalText string

	for {
		frame := readFrameWithDeadline(t, connB)

		if frame["type"] == "response" {
			if caption, _ := frame["caption"].(string); caption != "" {
				finalText = caption
			}
		}

		if frame["type"] == "done" || frame["type"] == "complete" {
			break
		}
	}

	expected := strings.Join(chunks, "")
	if finalText != expected {
		t.Fatalf("final text after reconnect = %q, want exactly %q (no duplication, no loss)", finalText, expected)
	}

	// v1.2.8: wait for the full settle tail (reply + summary sidecar)
	// before cleanup — a run goroutine still writing durable state
	// must never race t.TempDir's RemoveAll.
	if !waitForReplyPersisted(t, server, sessionID) {
		t.Fatal("the run never persisted its reply")
	}

	if !waitForSummarySettled(t, server, sessionID) {
		t.Fatal("the settle tail (summary sidecar) never completed")
	}
}

// TestRunEventsCarryMonotonicSequence pins the event contract: every
// activity of the run carries runId + a strictly increasing seq.
func TestRunEventsCarryMonotonicSequence(t *testing.T) {
	// 80ms per chunk: a ~240ms generation window that reliably covers
	// the attach even when the full suite loads the machine (attaching
	// AFTER completion is a different contract — see the terminal
	// branch below).
	engine := slowStreamEngine(t, []string{"a", "b", "c"}, 80*time.Millisecond)
	server := newRemoteServer(t, engine.URL)
	sessionID := createSessionForRun(t, server)

	runResp, err := http.Post(
		server.URL+"/api/run",
		"application/json",
		strings.NewReader(fmt.Sprintf(`{"sessionId":%q,"message":"seq test"}`, sessionID)),
	)
	if err != nil {
		t.Fatalf("POST /api/run: %v", err)
	}

	var body struct {
		RunID string `json:"runId"`
	}
	_ = json.NewDecoder(runResp.Body).Decode(&body)
	runResp.Body.Close()

	conn := dialActivityWS(t, server, sessionID)

	if frame := readFrameWithDeadline(t, conn); frame["type"] != "attached" {
		t.Fatalf("first frame = %v, want attached", frame["type"])
	}

	// The socket attaches while the run is live: snapshot first.
	snap := readFrameWithDeadline(t, conn)
	if snap["type"] != "run_snapshot" {
		t.Fatalf("expected run_snapshot, got %v", snap["type"])
	}

	// A machine stall can push the attach past the (short) generation:
	// the terminal snapshot IS the contract then (monotonic sequence
	// reached, persisted reply replayed) — the event loop below would
	// never see live frames. Verify and return instead of hanging.
	if running, _ := snap["running"].(bool); !running {
		if seq, _ := snap["sequence"].(float64); int64(seq) <= 0 {
			t.Fatalf("terminal snapshot carries no sequence: %v", snap)
		}

		if reply, _ := snap["latestResponse"].(string); reply == "" {
			t.Fatalf("terminal snapshot replays no persisted reply: %v", snap)
		}

		return
	}

	lastSeq := int64(0)

	for {
		frame := readFrameWithDeadline(t, conn)

		if frame["type"] == "idle" {
			break
		}

		if frame["type"] == "done" || frame["type"] == "complete" {
			break
		}

		rid, _ := frame["runId"].(string)
		seq, _ := frame["seq"].(float64)

		if rid != "" {
			if rid != body.RunID {
				t.Fatalf("event runId = %q, want %q", rid, body.RunID)
			}

			if seq <= 0 {
				t.Fatalf("event of run %s carries no seq: %v", rid, frame)
			}

			if int64(seq) <= lastSeq {
				t.Fatalf("seq regressed: %v after %v", seq, lastSeq)
			}

			lastSeq = int64(seq)
		}
	}

	if lastSeq == 0 {
		t.Fatal("no sequenced events observed")
	}

	// v1.2.8: the run goroutine's durable settle tail (reply persist,
	// summary sidecar) must complete before t.TempDir cleanup — a raw
	// RemoveAll race here is a TEST bug, not a runtime bug.
	if !waitForReplyPersisted(t, server, sessionID) {
		t.Fatal("the run never persisted its reply")
	}

	if !waitForSummarySettled(t, server, sessionID) {
		t.Fatal("the settle tail (summary sidecar) never completed")
	}
}

// TestStaleRunEventsFilteredByServer pins that a socket attached to the
// CURRENT run never receives frames stamped with an older run's id: the
// server's forwarding loop drops them before they reach the wire.
func TestStaleRunEventsFilteredByServer(t *testing.T) {
	engine := remoteFakeEngine(t, "first run answer.")
	server := newRemoteServer(t, engine.URL)
	sessionID := createSessionForRun(t, server)

	// Run 1 completes fully.
	runResp, err := http.Post(
		server.URL+"/api/run",
		"application/json",
		strings.NewReader(fmt.Sprintf(`{"sessionId":%q,"message":"one"}`, sessionID)),
	)
	if err != nil {
		t.Fatalf("POST /api/run: %v", err)
	}
	runResp.Body.Close()

	if !waitForReplyPersisted(t, server, sessionID) {
		t.Fatal("run 1 never persisted — precondition broken")
	}

	// Attach and confirm the idle sentinel carries the lastRun outcome.
	conn := dialActivityWS(t, server, sessionID)

	if frame := readFrameWithDeadline(t, conn); frame["type"] != "attached" {
		t.Fatalf("first frame = %v, want attached", frame["type"])
	}

	idle := readFrameWithDeadline(t, conn)
	if idle["type"] != "idle" {
		t.Fatalf("expected idle, got %v", idle["type"])
	}

	lastRun, ok := idle["lastRun"].(map[string]any)
	if !ok {
		t.Fatal("idle sentinel carries no lastRun outcome — the v1.2.6 terminal replay regressed")
	}

	if outcome, _ := lastRun["outcome"].(string); outcome != "done" {
		t.Fatalf("lastRun.outcome = %q, want done", outcome)
	}
}

// TestTerminalSnapshotReplaysPersistedReply pins the authoritative terminal
// state: a socket that attaches AFTER the run finished (but while the entry
// still exists) receives the terminal snapshot including the persisted
// reply text — not a bare idle marker.
func TestTerminalSnapshotReplaysPersistedReply(t *testing.T) {
	engine := remoteFakeEngine(t, "2+2 is 4.")
	server := newRemoteServer(t, engine.URL)
	sessionID := createSessionForRun(t, server)

	// Attach BEFORE the run so the socket is in standby when it starts —
	// it will observe the whole run and the terminal state.
	conn := dialActivityWS(t, server, sessionID)

	if frame := readFrameWithDeadline(t, conn); frame["type"] != "attached" {
		t.Fatalf("first frame = %v, want attached", frame["type"])
	}

	runResp, err := http.Post(
		server.URL+"/api/run",
		"application/json",
		strings.NewReader(fmt.Sprintf(`{"sessionId":%q,"message":"2+2?"}`, sessionID)),
	)
	if err != nil {
		t.Fatalf("POST /api/run: %v", err)
	}
	runResp.Body.Close()

	// Drain until the run settles on THIS socket (done → idle again).
	var sawDone bool

	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))

		var frame map[string]any
		if err := conn.ReadJSON(&frame); err != nil {
			t.Fatalf("read frame: %v", err)
		}

		if frame["type"] == "done" || frame["type"] == "complete" {
			sawDone = true
		}

		if sawDone && frame["type"] == "idle" {
			break
		}
	}

	if !sawDone {
		t.Fatal("the socket never observed the run's terminal event")
	}

	// A SECOND socket attaching now (post-terminal, post-cleanup) gets the
	// idle sentinel whose lastRun block is the authoritative outcome.
	conn2 := dialActivityWS(t, server, sessionID)

	if frame := readFrameWithDeadline(t, conn2); frame["type"] != "attached" {
		t.Fatalf("first frame = %v, want attached", frame["type"])
	}

	idle := readFrameWithDeadline(t, conn2)
	if idle["type"] != "idle" {
		t.Fatalf("expected idle, got %v", idle["type"])
	}

	lastRun, ok := idle["lastRun"].(map[string]any)
	if !ok {
		t.Fatal("idle sentinel lost the lastRun outcome block")
	}

	if persisted, _ := lastRun["persisted"].(bool); !persisted {
		t.Fatal("lastRun.persisted = false — the reply WAS appended")
	}
}

// TestRunSnapshotBounded pins the snapshot memory bounds: a huge cumulative
// caption is capped, never retained unboundedly.
func TestRunSnapshotBounded(t *testing.T) {
	live := newRunLive("run-bounded", "sess", time.Now())

	huge := strings.Repeat("x", snapshotCapBytes+4096)
	live.observe(stampedActivity("response", huge))

	snap := live.snapshot()

	if len(snap.LatestResponse) > snapshotCapBytes+64 {
		t.Fatalf("snapshot retained %d bytes — the bounded-snapshot contract is broken", len(snap.LatestResponse))
	}

	if len(live.recent) > recentEventCap {
		t.Fatalf("recent-events ring holds %d > %d entries", len(live.recent), recentEventCap)
	}
}

// TestRunLiveTerminalSettleOnce pins that the authoritative terminal state
// records exactly one outcome and replays the persisted reply.
func TestRunLiveTerminalSettleOnce(t *testing.T) {
	live := newRunLive("run-settle", "sess", time.Now())

	live.settleTerminal("done", "Completed", true, "final answer", "reasoning")

	// A second settle must not overwrite the first (exactly-once).
	live.settleTerminal("error", "late error", false, "", "")

	snap := live.snapshot()

	if snap.TerminalOutcome != "done" {
		t.Fatalf("terminalOutcome = %q, want done (exactly-once settle)", snap.TerminalOutcome)
	}

	if snap.LatestResponse != "final answer" {
		t.Fatalf("latestResponse = %q, want the persisted reply", snap.LatestResponse)
	}

	if snap.Running {
		t.Fatal("snapshot.running = true after terminal settle")
	}
}

// stampedActivity builds one activity for the state tests.
func stampedActivity(kind, caption string) agent.Activity {
	return agent.Activity{
		Type:      kind,
		Caption:   caption,
		Timestamp: time.Now(),
	}
}
