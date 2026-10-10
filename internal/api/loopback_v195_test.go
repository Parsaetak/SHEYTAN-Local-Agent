package api

// loopback_v195_test.go — v1.9.5 (P0): the desktop loopback activity
// transport, LEAST-PRIVILEGE contract:
//
//   1. StartLoopback binds 127.0.0.1 on an EPHEMERAL port; GET
//      /api/ws-origin — served by the MAIN intercepted handler, NOT the
//      loopback listener — reports the exact origin plus the per-server
//      capability token;
//   2. the listener serves EXACTLY ONE route (/ws/activity). Session,
//      run, engine and updater REST endpoints are NOT reachable through
//      it — a local process that discovers the port gets nothing but the
//      guarded activity upgrade;
//   3. every upgrade requires the capability token (constant-time
//      compare), a non-empty accepted Origin, a loopback Host matching
//      the listener address, and no forwarding headers; any absence is a
//      refusal — an absent Origin cannot bypass loopback authentication;
//   4. a server without a loopback listener reports "" and NO token
//      (headless/browser same-origin behavior is unchanged);
//   5. Close() drains the listener, resets the origin AND the token, and
//      is idempotent + concurrency-safe;
//   6. with the token + the Wails page origin the upgrade reaches the
//      ONE handleActivityWS authority (the real attach acknowledgement).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
)

func newLoopbackTestServer(t *testing.T) *Server {
	t.Helper()

	// The same minimal, offline construction the existing suite uses.
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.ModelsDir = filepath.Join(cfg.DataDir, "models")
	cfg.SessionsDir = filepath.Join(cfg.DataDir, "sessions")
	cfg.Host = "127.0.0.1"
	cfg.Port = 0
	cfg.Provider = "local"
	cfg.LlamaAutoStart = false
	cfg.UpdateSchedule = "off"

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(srv.Close)

	return srv
}

func TestLoopbackTransportLeastPrivilegeSurface(t *testing.T) {
	srv := newLoopbackTestServer(t)

	if got := srv.LoopbackOrigin(); got != "" {
		t.Fatalf("origin before StartLoopback = %q, want empty", got)
	}

	if got := srv.LoopbackToken(); got != "" {
		t.Fatalf("token before StartLoopback = %q, want empty", got)
	}

	origin, err := srv.StartLoopback()
	if err != nil {
		t.Fatalf("StartLoopback: %v", err)
	}

	if !strings.HasPrefix(origin, "http://127.0.0.1:") {
		t.Fatalf("loopback origin = %q, want http://127.0.0.1:<ephemeral>", origin)
	}

	token := srv.LoopbackToken()
	if len(token) < 32 {
		t.Fatalf("capability token too short (%d chars) — must be 256-bit random", len(token))
	}

	// Idempotent start: one origin and one token per server lifetime.
	again, err := srv.StartLoopback()
	if err != nil || again != origin {
		t.Fatalf("second StartLoopback = (%q, %v), want (%q, nil)", again, err, origin)
	}
	if srv.LoopbackToken() != token {
		t.Fatalf("token changed on idempotent start")
	}

	// Discovery rides the MAIN handler (the Wails-intercepted surface),
	// and reports origin + token together.
	rec := httptest.NewRecorder()
	srv.handleWSOrigin(rec, httptest.NewRequest(http.MethodGet, "/api/ws-origin", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("ws-origin status = %d, want 200", rec.Code)
	}

	var disc struct {
		Origin string `json:"origin"`
		Token  string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &disc); err != nil {
		t.Fatalf("decode discovery: %v", err)
	}
	if disc.Origin != origin || disc.Token != token {
		t.Fatalf("discovery = (%q, %q), want (%q, <token>)", disc.Origin, disc.Token, origin)
	}

	// LEAST PRIVILEGE: the loopback listener exposes ONLY /ws/activity.
	// Session/run/engine/updater REST surfaces are refused.
	for _, path := range []string{
		"/api/state",
		"/api/sessions",
		"/api/run",
		"/api/engine",
		"/api/ws-origin", // discovery itself is main-handler-only
		"/",
	} {
		res, err := http.Get(origin + path)
		if err != nil {
			t.Fatalf("GET %s on loopback listener: %v", path, err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("GET %s on loopback listener = %d, want 404 (least-privilege surface)", path, res.StatusCode)
		}
	}
}

func TestLoopbackUpgradeGuardMatrix(t *testing.T) {
	srv := newLoopbackTestServer(t)

	origin, err := srv.StartLoopback()
	if err != nil {
		t.Fatalf("StartLoopback: %v", err)
	}

	token := srv.LoopbackToken()
	wsBase := strings.Replace(origin, "http://", "ws://", 1) + "/ws/activity"

	dial := func(url string, header http.Header, rewriteHost string) (int, bool) {
		t.Helper()

		dialer := websocket.Dialer{}

		if rewriteHost != "" {
			// Reach a spoofed Host header: dial a loopback address but
			// declare a different Host via the request header.
			dialer.NetDialContext = nil
			url = strings.Replace(url, wsBase, "ws://"+rewriteHost+"/ws/activity", 1)
		}

		conn, resp, err := dialer.Dial(url, header)
		if err == nil {
			conn.Close()
			return resp.StatusCode, true
		}
		if resp == nil {
			// Connection-level refusal (host rewrite cannot reach a
			// listener that never bound it) counts as a refusal.
			return 0, false
		}
		return resp.StatusCode, false
	}

	wailsHeader := http.Header{}
	wailsHeader.Set("Origin", "http://wails.localhost")

	// (a) missing token — refused even with the valid page origin.
	if code, ok := dial(wsBase+"?sessionId=x", wailsHeader, ""); ok && code != http.StatusForbidden {
		t.Fatalf("upgrade without token = %d (ok=%v), want 403", code, ok)
	}

	// (b) bad token — refused.
	if code, ok := dial(wsBase+"?token=deadbeefdeadbeefdeadbeefdeadbeef", wailsHeader, ""); ok && code != http.StatusForbidden {
		t.Fatalf("upgrade with bad token = %d (ok=%v), want 403", code, ok)
	}

	// (c) token but ABSENT Origin — the v1.9.4 bypass is closed: no
	// Origin can never pass the loopback authentication.
	noOrigin := http.Header{}
	if code, ok := dial(wsBase+"?token="+token, noOrigin, ""); ok && code != http.StatusForbidden {
		t.Fatalf("upgrade without Origin = %d (ok=%v), want 403", code, ok)
	}

	// (d) token + foreign Origin — refused.
	foreign := http.Header{}
	foreign.Set("Origin", "https://evil.example")
	if code, ok := dial(wsBase+"?token="+token, foreign, ""); ok && code != http.StatusForbidden {
		t.Fatalf("upgrade with foreign origin = %d (ok=%v), want 403", code, ok)
	}

	// (e) valid token + valid page origin → the REAL upgrade: the first
	// frame is the attach acknowledgement from the ONE handleActivityWS.
	conn, resp, err := websocket.DefaultDialer.Dial(
		wsBase+"?sessionId=loopback-v195&token="+token, wailsHeader)
	if err != nil {
		t.Fatalf("upgrade with valid token+origin: %v (resp %v)", err, resp)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var first map[string]any
	if err := conn.ReadJSON(&first); err != nil {
		t.Fatalf("read first frame: %v", err)
	}
	if first["type"] != "attached" {
		t.Fatalf("first frame type = %v, want attached", first["type"])
	}
}

func TestLoopbackCloseIsIdempotentAndConcurrencySafe(t *testing.T) {
	srv := newLoopbackTestServer(t)

	if _, err := srv.StartLoopback(); err != nil {
		t.Fatalf("StartLoopback: %v", err)
	}

	// Concurrent Close calls (ownership handoff inside closeLoopback) —
	// no double-close panic, no data race, exactly one drain wins.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			srv.Close()
		}()
	}
	wg.Wait()

	if got := srv.LoopbackOrigin(); got != "" {
		t.Fatalf("origin after Close = %q, want empty", got)
	}
	if got := srv.LoopbackToken(); got != "" {
		t.Fatalf("token after Close = %q, want empty (credential released)", got)
	}

	// Re-Close after full teardown stays safe.
	srv.Close()
}

func TestWSOriginEndpointWithoutLoopbackHasNoToken(t *testing.T) {
	srv := newLoopbackTestServer(t)

	rec := httptest.NewRecorder()
	srv.handleWSOrigin(rec, httptest.NewRequest(http.MethodGet, "/api/ws-origin", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Origin string `json:"origin"`
		Token  string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if body.Origin != "" {
		t.Fatalf("origin without loopback = %q, want empty (same-origin contract)", body.Origin)
	}
	if body.Token != "" {
		t.Fatalf("token without loopback = %q, want empty (no credential for same-origin mode)", body.Token)
	}
}
