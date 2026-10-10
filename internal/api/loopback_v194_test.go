package api

// loopback_v194_test.go — v1.9.4 (P0): the desktop loopback activity
// transport, pinned:
//
//   1. StartLoopback binds 127.0.0.1 on an EPHEMERAL port, /api/ws-origin
//      reports that exact origin over HTTP, and the same handler serves
//      the real API surface (one authority — no second implementation);
//   2. a server without a loopback listener reports "" (headless/browser
//      same-origin behavior is unchanged);
//   3. Close() drains the listener and the origin resets to "";
//   4. the WebSocket upgrade origin check accepts the Wails desktop page
//      origin (http/wails + wails.localhost) against LOOPBACK request
//      hosts only — and never a foreign origin, never a non-loopback
//      request host, preserving the no-wildcard contract;
//   5. the loopback transport is a REAL WebSocket path: the activity
//      endpoint upgrades through it (the desktop live-answer defect is
//      the socket never REACHING this handler — here it does).

import (
        "encoding/json"
        "net/http"
        "net/http/httptest"
        "path/filepath"
        "strings"
        "testing"

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

func TestLoopbackTransportServesOriginAndAPI(t *testing.T) {
        srv := newLoopbackTestServer(t)

        if got := srv.LoopbackOrigin(); got != "" {
                t.Fatalf("origin before StartLoopback = %q, want empty", got)
        }

        origin, err := srv.StartLoopback()
        if err != nil {
                t.Fatalf("StartLoopback: %v", err)
        }

        if !strings.HasPrefix(origin, "http://127.0.0.1:") {
                t.Fatalf("loopback origin = %q, want http://127.0.1:<ephemeral>", origin)
        }

        // Idempotent start: one origin per server lifetime.
        again, err := srv.StartLoopback()
        if err != nil || again != origin {
                t.Fatalf("second StartLoopback = (%q, %v), want (%q, nil)", again, err, origin)
        }

        // The discovery endpoint answers over the loopback transport itself.
        res, err := http.Get(origin + "/api/ws-origin")
        if err != nil {
                t.Fatalf("GET /api/ws-origin: %v", err)
        }
        defer res.Body.Close()

        if res.StatusCode != http.StatusOK {
                t.Fatalf("status = %d, want 200", res.StatusCode)
        }

        var body struct {
                Origin string `json:"origin"`
        }
        if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
                t.Fatalf("decode: %v", err)
        }

        if body.Origin != origin {
                t.Fatalf("reported origin = %q, want %q", body.Origin, origin)
        }

        // The SAME transport serves the real API surface (state endpoint).
        res2, err := http.Get(origin + "/api/state")
        if err != nil {
                t.Fatalf("GET /api/state: %v", err)
        }
        defer res2.Body.Close()

        if res2.StatusCode != http.StatusOK {
                t.Fatalf("api state status = %d, want 200", res2.StatusCode)
        }
}

func TestLoopbackTransportWebSocketUpgrade(t *testing.T) {
        srv := newLoopbackTestServer(t)

        origin, err := srv.StartLoopback()
        if err != nil {
                t.Fatalf("StartLoopback: %v", err)
        }

        wsURL := strings.Replace(origin, "http://", "ws://", 1) +
                "/ws/activity?sessionId=loopback-proof"

        header := http.Header{}
        // The desktop page origin (the exact production case: the page is
        // served from wails.localhost while the socket rides the loopback).
        header.Set("Origin", "http://wails.localhost")

        conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
        if err != nil {
                t.Fatalf("dial %s: %v (resp %v)", wsURL, err, resp)
        }
        defer conn.Close()

        // The FIRST frame is the deterministic attach acknowledgement — the
        // contract the frontend's attach wait depends on.
        var first map[string]any
        if err := conn.ReadJSON(&first); err != nil {
                t.Fatalf("read first frame: %v", err)
        }

        if first["type"] != "attached" {
                t.Fatalf("first frame type = %v, want attached", first["type"])
        }
}

func TestLoopbackCloseResetsOrigin(t *testing.T) {
        srv := newLoopbackTestServer(t)

        if _, err := srv.StartLoopback(); err != nil {
                t.Fatalf("StartLoopback: %v", err)
        }

        srv.Close()

        if got := srv.LoopbackOrigin(); got != "" {
                t.Fatalf("origin after Close = %q, want empty", got)
        }
}

func TestAllowedOriginWailsDesktopPage(t *testing.T) {
        cases := []struct {
                name   string
                origin string
                host   string
                want   bool
        }{
                {"wails http page on loopback transport", "http://wails.localhost", "127.0.0.1:45911", true},
                {"wails custom scheme page on loopback transport", "wails://wails.localhost", "127.0.0.1:45911", true},
                {"wails page on localhost transport", "http://wails.localhost", "localhost:45911", true},
                {"wails page against a NON-loopback host is refused", "http://wails.localhost", "example.com:443", false},
                {"foreign origin against loopback is refused", "https://evil.example", "127.0.0.1:45911", false},
                {"wildcard-ish suffix host is not enough alone", "http://notwails.localhost", "127.0.0.1:45911", false},
                {"same-origin still accepted", "http://127.0.0.1:45911", "127.0.0.1:45911", true},
                {"dev frontend still accepted", "http://localhost:5173", "127.0.0.1:45911", true},
        }

        for _, tc := range cases {
                t.Run(tc.name, func(t *testing.T) {
                        if got := allowedOrigin(tc.origin, tc.host); got != tc.want {
                                t.Fatalf("allowedOrigin(%q, %q) = %v, want %v", tc.origin, tc.host, got, tc.want)
                        }
                })
        }
}

func TestWSOriginEndpointWithoutLoopback(t *testing.T) {
        srv := newLoopbackTestServer(t)

        req := httptest.NewRequest(http.MethodGet, "/api/ws-origin", nil)
        rec := httptest.NewRecorder()

        srv.handleWSOrigin(rec, req)

        if rec.Code != http.StatusOK {
                t.Fatalf("status = %d, want 200", rec.Code)
        }

        var body struct {
                Origin string `json:"origin"`
        }
        if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
                t.Fatalf("decode: %v", err)
        }

        if body.Origin != "" {
                t.Fatalf("origin without loopback = %q, want empty (same-origin contract)", body.Origin)
        }
}
