package api

// run_settlement_v129_test.go — v1.2.9 regressions: DURABLE COMPLETION
// ORDERING. The invariant is
//
//      persist required durable state → settle → publish terminal completion
//
// so at the INSTANT the run becomes terminal (the outcome registry
// records "done"), the required durable artifacts — the rolled summary
// and the agent.md handoff — must ALREADY be on disk. A crash right
// after the terminal publication can no longer leave a "complete" run
// with silently missing artifacts.

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/agent"
)

// TestTerminalCompletionFollowsDurableArtifacts polls the outcome
// registry while the run is in flight; the first observation of the
// terminal outcome is immediately checked against the artifact files.
func TestTerminalCompletionFollowsDurableArtifacts(t *testing.T) {
	engine := toolStreamEngine(t)
	t.Cleanup(engine.Close)

	srv, server := newRemoteServerWithHandle(t, engine.URL)

	cfg := srv.src.Load()
	wsRoot := cfg.EffectiveWorkspaceRoot()
	handoffPath := filepath.Join(wsRoot, agent.HandoffFileName)

	created := createSessionWithMode(t, server, "agent")
	id := created["id"].(string)

	runResp, err := http.Post(
		server.URL+"/api/run",
		"application/json",
		strings.NewReader(fmt.Sprintf(`{"sessionId":%q,"message":"Write the evidence file for the ordering check."}`, id)),
	)
	if err != nil {
		t.Fatalf("POST /api/run: %v", err)
	}
	runResp.Body.Close()

	// Poll the OUTCOME REGISTRY: the moment it records the terminal
	// outcome, every required durable artifact must already exist.
	deadline := time.Now().Add(10 * time.Second)
	observed := false

	for time.Now().Before(deadline) {
		if rec, ok := srv.outcomes.latest(id); ok && rec.Outcome == "done" {
			observed = true

			// The summary sidecar exists (rolled BEFORE terminal settle).
			if _, err := srv.store.Summary(id); err != nil {
				t.Fatalf("terminal outcome recorded but the summary sidecar is missing: %v (durable ordering broken)", err)
			}

			// The agent.md handoff exists (written BEFORE terminal settle).
			if _, err := fileExists(handoffPath); err != nil {
				t.Fatalf("terminal outcome recorded but the agent.md handoff is missing at %s: %v (durable ordering broken)", handoffPath, err)
			}

			data := readFileOrFatal(t, handoffPath)
			if !strings.Contains(string(data), agent.HandoffBeginMarker) {
				t.Fatalf("handoff file exists but carries no handoff section:\n%s", string(data))
			}

			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if !observed {
		t.Fatal("the run never recorded a terminal outcome")
	}
}

func fileExists(path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		return false, err
	}
	return true, nil
}

func readFileOrFatal(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}
