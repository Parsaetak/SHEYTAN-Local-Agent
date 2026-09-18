package api

// runtransport_v127_test.go — v1.2.7 regression tests for the RUN LIFECYCLE
// GATE and the terminal-recovery fallthrough. These pin the exact CI failure
// of GitHub Actions run 35367243405 (TestStaleRunEventsFilteredByServer:
// "expected idle, got run_snapshot") and the sibling hazard the audit of the
// same boundary uncovered:
//
//   1. a run entry that is still registered in s.runs but is ALREADY
//      authoritatively terminal (live.snapshot().Running == false — the
//      window between terminal settlement and deferred registry cleanup)
//      must serve the idle sentinel with the recorded lastRun outcome,
//      NEVER a stale terminal run_snapshot;
//   2. a hub that closed before any post-snapshot event existed must fall
//      through to the idle sentinel instead of parking the socket on
//      clientGone forever (the abort path settles without publishing a
//      terminal activity — previously the socket hung with no terminal
//      marker at all);
//   3. a real aborted run, observed through a fresh attach, ends in the
//      idle sentinel carrying the recorded outcome (no hang, no
//      run_snapshot of a dead run).

import (
        "encoding/json"
        "fmt"
        "net/http"
        "net/http/httptest"
        "strings"
        "testing"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// newRemoteServerWithHandle builds the same server as newRemoteServer but
// ALSO returns the *Server handle so white-box tests can inject synthetic
// run states into the registry — the only way to deterministically observe
// the terminal-but-registered window that production reaches whenever a
// socket attaches between run settlement and cleanup.
func newRemoteServerWithHandle(t *testing.T, engineURL string) (*Server, *httptest.Server) {
        t.Helper()

        cfg := config.Default()
        cfg.DataDir = t.TempDir()
        cfg.ModelsDir = cfg.DataDir + "/models"
        cfg.SessionsDir = cfg.DataDir + "/sessions"
        cfg.Host = "127.0.0.1"
        cfg.Port = 0
        cfg.Provider = "remote"
        cfg.RemoteBaseURL = engineURL + "/v1"
        cfg.RemoteAPIKey = "test-key"
        cfg.RemoteModel = "fake-remote-model"
        cfg.LlamaAutoStart = false
        cfg.UpdateSchedule = "off"

        srv, err := New(cfg)
        if err != nil {
                t.Fatalf("api.New: %v", err)
        }

        if err := srv.EnsureSetup(); err != nil {
                t.Fatalf("EnsureSetup: %v", err)
        }

        t.Cleanup(srv.Close)

        server := httptest.NewServer(srv.Handler())
        t.Cleanup(server.Close)

        return srv, server
}

// TestTerminalRegistryEntryServesIdleNotSnapshot pins the v1.2.7 lifecycle
// gate: map membership is NOT "run active". A registered run whose
// authoritative state is already terminal falls through to the idle path.
func TestTerminalRegistryEntryServesIdleNotSnapshot(t *testing.T) {
        engine := remoteFakeEngine(t, "unused.")
        srv, server := newRemoteServerWithHandle(t, engine.URL)
        sessionID := createSessionForRun(t, server)

        // Synthetic terminal-but-registered run: exactly the state the run
        // goroutine leaves behind between settle() and its deferred delete.
        live := newRunLive("run-terminal-gate", sessionID, time.Now())
        live.settleTerminal("done", "Completed", true, "the final answer", "")

        // The outcome is recorded BEFORE the state flips terminal (v1.2.7
        // ordering) — mirror that here, as settle() does.
        srv.outcomes.record(sessionID, runOutcome{
                RunID:     "run-terminal-gate",
                StartedAt: time.Now().Add(-time.Second),
                EndedAt:   time.Now(),
                Outcome:   "done",
                Persisted: true,
        })

        srv.runsMu.Lock()
        srv.runs[sessionID] = &runState{
                cancel: func() {},
                hub:    newActivityHub(),
                live:   live,
        }
        srv.runsMu.Unlock()

        conn := dialActivityWS(t, server, sessionID)

        if frame := readFrameWithDeadline(t, conn); frame["type"] != "attached" {
                t.Fatalf("first frame = %v, want attached", frame["type"])
        }

        // THE regression: the very next frame is the idle sentinel — never a
        // stale terminal run_snapshot.
        idle := readFrameWithDeadline(t, conn)
        if idle["type"] != "idle" {
                t.Fatalf("expected idle, got %v — the terminal-entry gate regressed", idle["type"])
        }

        lastRun, ok := idle["lastRun"].(map[string]any)
        if !ok {
                t.Fatal("idle sentinel carries no lastRun outcome for the terminal run")
        }

        if outcome, _ := lastRun["outcome"].(string); outcome != "done" {
                t.Fatalf("lastRun.outcome = %q, want done", outcome)
        }

        if rid, _ := lastRun["runId"].(string); rid != "run-terminal-gate" {
                t.Fatalf("lastRun.runId = %q, want run-terminal-gate", rid)
        }
}

// TestClosedHubWithNoEventsFallsThroughToIdle pins the v1.2.7 fallthrough:
// a run that closes its hub before any post-snapshot event existed leaves
// the socket with the terminal-relevant snapshot, the idle sentinel — not a
// silent park on clientGone.
func TestClosedHubWithNoEventsFallsThroughToIdle(t *testing.T) {
        engine := remoteFakeEngine(t, "unused.")
        srv, server := newRemoteServerWithHandle(t, engine.URL)
        sessionID := createSessionForRun(t, server)

        // A STILL-RUNNING live state whose hub is already closed: the state
        // a socket faces when the run published everything, settled, and the
        // cleanup landed between the gate and the subscription — or when an
        // abort settled without publishing a terminal activity.
        live := newRunLive("run-closed-hub", sessionID, time.Now())

        hub := newActivityHub()
        hub.close()

        srv.runsMu.Lock()
        srv.runs[sessionID] = &runState{
                cancel: func() {},
                hub:    hub,
                live:   live,
        }
        srv.runsMu.Unlock()

        conn := dialActivityWS(t, server, sessionID)

        if frame := readFrameWithDeadline(t, conn); frame["type"] != "attached" {
                t.Fatalf("first frame = %v, want attached", frame["type"])
        }

        snap := readFrameWithDeadline(t, conn)
        if snap["type"] != "run_snapshot" {
                t.Fatalf("second frame = %v, want run_snapshot (the run still reports running)", snap["type"])
        }

        // The fallthrough: the idle sentinel must arrive on its own — the
        // previous behavior blocked here until the client disconnected.
        idle := readFrameWithDeadline(t, conn)
        if idle["type"] != "idle" {
                t.Fatalf("expected the closed-hub fallthrough to send idle, got %v", idle["type"])
        }
}

// TestAbortThenAttachReceivesIdleWithOutcome pins the end-to-end abort
// recovery contract: after a run is aborted and has settled, a fresh socket
// attaches and receives the idle sentinel with the recorded outcome — no
// stale run_snapshot, no hang, no silent request loss.
func TestAbortThenAttachReceivesIdleWithOutcome(t *testing.T) {
        engine := slowStreamEngine(t, []string{"a", "b", "c", "d", "e", "f", "g", "h"}, 120*time.Millisecond)
        srv, server := newRemoteServerWithHandle(t, engine.URL)
        sessionID := createSessionForRun(t, server)

        runResp, err := http.Post(
                server.URL+"/api/run",
                "application/json",
                strings.NewReader(fmt.Sprintf(`{"sessionId":%q,"message":"abort me"}`, sessionID)),
        )
        if err != nil {
                t.Fatalf("POST /api/run: %v", err)
        }
        runResp.Body.Close()

        abortResp, err := http.Post(
                server.URL+"/api/abort",
                "application/json",
                strings.NewReader(fmt.Sprintf(`{"sessionId":%q}`, sessionID)),
        )
        if err != nil {
                t.Fatalf("POST /api/abort: %v", err)
        }
        abortResp.Body.Close()

        // Wait until the run goroutine recorded its terminal outcome — the
        // authoritative settlement boundary (bounded registry).
        settled := false

        deadline := time.Now().Add(30 * time.Second)

        for time.Now().Before(deadline) {
                if _, ok := srv.outcomes.latest(sessionID); ok {
                        settled = true
                        break
                }

                time.Sleep(20 * time.Millisecond)
        }

        if !settled {
                t.Fatal("the aborted run never recorded its outcome — the settle path is broken")
        }

        // Attach AFTER settlement: the contract under test.
        conn := dialActivityWS(t, server, sessionID)

        if frame := readFrameWithDeadline(t, conn); frame["type"] != "attached" {
                t.Fatalf("first frame = %v, want attached", frame["type"])
        }

        idle := readFrameWithDeadline(t, conn)
        if idle["type"] != "idle" {
                t.Fatalf("expected idle after abort, got %v — a dead run leaked into the attach path", idle["type"])
        }

        lastRun, ok := idle["lastRun"].(map[string]any)
        if !ok {
                t.Fatal("idle sentinel carries no lastRun outcome after abort")
        }

        // The aborted run settles from REAL signals: the orchestrator may
        // surface the cancellation as an error result or as the aborted
        // outcome — both are terminal, neither may ever re-enter the
        // active path.
        outcome, _ := lastRun["outcome"].(string)
        if outcome != "aborted" && outcome != "error" {
                t.Fatalf("lastRun.outcome = %q, want aborted or error", outcome)
        }

        encoded, _ := json.Marshal(lastRun)
        if !strings.Contains(string(encoded), `"runId"`) {
                t.Fatalf("lastRun block lost the runId: %s", encoded)
        }
}
