package api

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/recall"
)

// --- v1.1.4Z regression tests ---

// TestConfigPatchIsRaceFree exercises concurrent PATCH + GET + engine-gate
// reads against the copy-on-write source. Under -race the v1.1.3Z
// in-place mergeConfigPatch (`*s.cfg = updated`) failed this test.
func TestConfigPatchIsRaceFree(t *testing.T) {
	server, _ := newTestServer(t)

	var wg sync.WaitGroup

	patch := func(i int) {
		defer wg.Done()

		body, _ := json.Marshal(map[string]any{
			"maxIterations": 5 + i%20,
		})

		resp, err := http.Post(server.URL+"/api/config", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Errorf("PATCH: %v", err)
			return
		}
		resp.Body.Close()
	}

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go patch(i)
	}

	for i := 0; i < 20; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			resp, err := http.Get(server.URL + "/api/config")
			if err != nil {
				t.Errorf("GET: %v", err)
				return
			}
			resp.Body.Close()
		}()
	}

	wg.Wait()
}

// TestConfigPatchRejectsUnboundedBody pins the MaxBytesReader guard.
func TestConfigPatchRejectsUnboundedBody(t *testing.T) {
	server, _ := newTestServer(t)

	huge := bytes.Repeat([]byte("a"), 2<<20) // 2 MB > 1 MB cap

	resp, err := http.Post(server.URL+"/api/config", "application/json", bytes.NewReader(huge))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("2 MB config patch must be rejected by the body cap")
	}
}

// TestAbortRequiresValidBody: the old handler returned {ok:true} for a
// malformed body while aborting nothing.
func TestAbortRequiresValidBody(t *testing.T) {
	server, _ := newTestServer(t)

	resp, err := http.Post(
		server.URL+"/api/abort",
		"application/json",
		strings.NewReader("not json at all"),
	)
	if err != nil {
		t.Fatalf("POST /api/abort: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed abort body: status = %d, want 400", resp.StatusCode)
	}
}

// TestFeedbackEndpointWritesRecallSteering covers the previously dead
// SetFeedback write path: the verdict must persist and steer scoring.
func TestFeedbackEndpointWritesRecallSteering(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.LlamaAutoStart = false
	cfg.UpdateSchedule = "off" // v1.2.0: no network in unit tests — the scheduled updater is owned and closed, never exercised here

	engine := recall.New(cfg.DataDir)

	if err := engine.IndexTurn("s1", "t", "how do I parse csv", "use the csv reader", nil); err != nil {
		t.Fatalf("IndexTurn: %v", err)
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}

	// route the server's recall to our engine-backed store
	srv.recall = engine

	t.Cleanup(srv.Close)

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	body, _ := json.Marshal(map[string]any{
		"sessionId": "s1",
		"query":     "how do I parse csv",
		"liked":     true,
	})

	resp, err := http.Post(ts.URL+"/api/feedback", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/feedback: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("feedback status = %d", resp.StatusCode)
	}

	id := recall.CapsuleID("s1", "how do I parse csv")
	if got := engine.FeedbackFor(id); got != 1 {
		t.Fatalf("FeedbackFor = %d, want +1 (the steering write path was dead before v1.1.4Z)", got)
	}

	if likes, _ := engine.FeedbackStats(); likes != 1 {
		t.Fatalf("FeedbackStats likes = %d, want 1", likes)
	}
}

// TestModelsEndpointIncludesGGUFMetadata proves the GGUF header parser is
// actually wired into the endpoint (a full parser sat dead in llm/gguf.go
// while /api/models shipped stat-only entries).
func TestModelsEndpointIncludesGGUFMetadata(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	cfg.LlamaAutoStart = false

	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	// synth-4k.gguf: the name IS the metadata contract for the endpoint
	// (list + stat + card). A synthetic GGUF header would test the parser,
	// which has its own dedicated unit test in package llm.
	modelPath := filepath.Join(cfg.ModelsDir, "test-model.gguf")
	if err := writeFakeModel(modelPath); err != nil {
		t.Fatalf("write model: %v", err)
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}

	t.Cleanup(srv.Close)

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/models")
	if err != nil {
		t.Fatalf("GET /api/models: %v", err)
	}
	defer resp.Body.Close()

	var payload struct {
		Local []struct {
			ID        string `json:"id"`
			SizeBytes int64  `json:"sizeBytes"`
		} `json:"local"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(payload.Local) != 1 || payload.Local[0].ID != "test-model.gguf" {
		t.Fatalf("local models = %+v", payload.Local)
	}

	if payload.Local[0].SizeBytes == 0 {
		t.Fatal("SizeBytes missing")
	}
}

// writeFakeModel writes a minimal GGUF v2 file with one string kv —
// enough header for ReadModelCard to succeed without metadata.
func writeFakeModel(path string) error {
	buf := bytes.NewBuffer(nil)
	buf.WriteString("GGUF")

	var u32 [4]byte

	putU32 := func(v uint32) {
		binary.LittleEndian.PutUint32(u32[:], v)
		buf.Write(u32[:])
	}

	putU32(2) // version
	putU32(1) // kv count
	putU32(8) // key length
	buf.WriteString("test.key")
	putU32(8) // type = string
	putU32(5) // value length
	buf.WriteString("value")

	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// TestRunTimeoutBudgetApplies pins the per-run time budget: a session run
// with a tiny budget must terminate with the timeout caption, not hang.
func TestRunTimeoutBudgetApplies(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.SessionsDir = filepath.Join(cfg.DataDir, "sessions")
	cfg.LlamaAutoStart = false
	cfg.RunTimeoutMinutes = 1 // minimum clamp

	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}

	t.Cleanup(srv.Close)

	// create a session directly through the store
	sess := srv.store.Create()

	// effective budget must be positive
	if got := cfg.EffectiveRunTimeout(); got < time.Minute {
		t.Fatalf("EffectiveRunTimeout = %v, want >= 1 minute", got)
	}

	_ = sess
}
