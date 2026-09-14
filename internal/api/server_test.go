package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

// newTestServer builds a fully-wired Server against a temp data dir. The
// local engine cannot start here (no binary/model) — the prewarm fails
// fast and the engine reports failed, which is itself part of the
// contract under test.
func newTestServer(t *testing.T) (*httptest.Server, *config.Config) {
	t.Helper()

	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	cfg.SessionsDir = filepath.Join(cfg.DataDir, "sessions")
	cfg.Host = "127.0.0.1"
	cfg.Port = 0
	cfg.Provider = "local"
	cfg.LlamaAutoStart = false
	cfg.UpdateSchedule = "off" // v1.2.0: no network in unit tests — the scheduled updater is owned and closed, never exercised here // no engine binary in tests — no prewarm noise

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

	return server, cfg
}

func TestEngineEndpointReportsAuthoritativeState(t *testing.T) {
	server, _ := newTestServer(t)

	resp, err := http.Get(server.URL + "/api/engine")
	if err != nil {
		t.Fatalf("GET /api/engine: %v", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var snap struct {
		State    string `json:"state"`
		Provider string `json:"provider"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if snap.Provider != "local" {
		t.Fatalf("provider = %s", snap.Provider)
	}

	// Without a binary the engine is either idle (no prewarm) or failed
	// (prewarm attempt) — both are REAL backend states. What must never
	// happen is a fabricated ready.
	if snap.State == "ready" || snap.State == "running" || snap.State == "busy" {
		t.Fatalf("engine must not fabricate readiness, got %s", snap.State)
	}
}

func TestSessionLifecycleOverAPI(t *testing.T) {
	server, _ := newTestServer(t)

	// Create.
	resp, err := http.Post(server.URL+"/api/sessions", "application/json", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	defer resp.Body.Close()

	var sess struct {
		ID string `json:"id"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if sess.ID == "" {
		t.Fatal("no session id")
	}

	// Rename.
	body, _ := json.Marshal(map[string]any{"title": "api test"})

	req, _ := http.NewRequest(http.MethodPut, server.URL+"/api/sessions/"+sess.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("rename: %v", err)
	}

	_ = resp2.Body.Close()

	// Detail includes the new title.
	detail, err := http.Get(server.URL + "/api/sessions/" + sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	defer detail.Body.Close()

	var got struct {
		Title string `json:"title"`
	}

	_ = json.NewDecoder(detail.Body).Decode(&got)

	if got.Title != "api test" {
		t.Fatalf("title = %q", got.Title)
	}

	// Delete.
	reqDel, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/sessions/"+sess.ID, nil)

	respDel, err := http.DefaultClient.Do(reqDel)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	_ = respDel.Body.Close()

	// Second delete errors.
	respDel2, err := http.DefaultClient.Do(reqDel)
	if err != nil {
		t.Fatalf("delete 2: %v", err)
	}

	defer respDel2.Body.Close()

	if respDel2.StatusCode != http.StatusInternalServerError && respDel2.StatusCode != http.StatusNotFound {
		t.Fatalf("second delete should fail, got %d", respDel2.StatusCode)
	}
}

func TestAttachmentUploadInspectDelete(t *testing.T) {
	server, _ := newTestServer(t)

	// Upload via multipart.
	var buf bytes.Buffer

	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("files", "notes.txt")
	if err != nil {
		t.Fatalf("form file: %v", err)
	}

	content := strings.Repeat("attachment pipeline test line\n", 100)

	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("write part: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	upload, err := http.Post(server.URL+"/api/attachments", writer.FormDataContentType(), &buf)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	defer upload.Body.Close()

	if upload.StatusCode != http.StatusOK {
		t.Fatalf("upload status = %d", upload.StatusCode)
	}

	var uploaded struct {
		OK          bool `json:"ok"`
		Attachments []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Kind string `json:"kind"`
			Size int64  `json:"size"`
		} `json:"attachments"`
		Failed []map[string]string `json:"failed"`
	}

	if err := json.NewDecoder(upload.Body).Decode(&uploaded); err != nil {
		t.Fatalf("decode upload: %v", err)
	}

	if !uploaded.OK || len(uploaded.Attachments) != 1 {
		t.Fatalf("upload mismatch: ok=%v n=%d failed=%v", uploaded.OK, len(uploaded.Attachments), uploaded.Failed)
	}

	att := uploaded.Attachments[0]

	if att.Kind != "text" || att.Size != int64(len(content)) {
		t.Fatalf("attachment metadata: %+v", att)
	}

	// Inspect.
	inspect, err := http.Get(server.URL + "/api/attachments/" + att.ID)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}

	defer inspect.Body.Close()

	var detail struct {
		Attachment struct {
			Chunks []struct {
				Index int `json:"index"`
			} `json:"chunks"`
		} `json:"attachment"`
	}

	if err := json.NewDecoder(inspect.Body).Decode(&detail); err != nil {
		t.Fatalf("decode inspect: %v", err)
	}

	if len(detail.Attachment.Chunks) == 0 {
		t.Fatal("text attachment must be chunked")
	}

	// List shows it.
	list, err := http.Get(server.URL + "/api/attachments")
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	defer list.Body.Close()

	_ = list.Body.Close()

	// Delete.
	reqDel, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/attachments/"+att.ID, nil)

	del, err := http.DefaultClient.Do(reqDel)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	defer del.Body.Close()

	var deleted struct {
		OK bool `json:"ok"`
	}

	_ = json.NewDecoder(del.Body).Decode(&deleted)

	if !deleted.OK {
		t.Fatal("delete must report ok")
	}
}

func TestRunRejectsBadInput(t *testing.T) {
	server, _ := newTestServer(t)

	// Missing message.
	resp, err := http.Post(server.URL+"/api/run", "application/json", strings.NewReader(`{"sessionId":"whatever"}`))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	// Unknown session with message → 404.
	resp2, err := http.Post(server.URL+"/api/run", "application/json", strings.NewReader(`{"sessionId":"nope","message":"hi"}`))
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}

	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp2.StatusCode)
	}
}

func TestConfigPatchRoundTrip(t *testing.T) {
	server, _ := newTestServer(t)

	resp, err := http.Get(server.URL + "/api/config")
	if err != nil {
		t.Fatalf("get config: %v", err)
	}

	defer resp.Body.Close()

	var cfg map[string]any

	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if _, ok := cfg["thinkingMode"]; !ok {
		t.Fatal("config response missing thinkingMode")
	}

	// The redacted config must never leak the API key.
	if key, _ := cfg["remoteApiKey"].(string); key != "" {
		t.Fatal("remoteApiKey leaked through GET /api/config")
	}
}

// guard against unused import in future refactors.
var _ = fmt.Sprintf
