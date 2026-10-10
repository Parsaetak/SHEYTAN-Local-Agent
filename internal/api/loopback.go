package api

// loopback.go — v1.9.4 (P0): the DESKTOP LIVE-ANSWER TRANSPORT.
//
// THE DEFECT THIS REPAIRS (the Windows "answer appears only after Stop"
// root cause, proven against the exact pinned wails v3.0.0-beta.16
// sources): packaged desktop builds serve the frontend and the HTTP API
// through the webview's resource-request interception — on Windows
// WebView2 forwards ONLY http://wails.localhost requests into the Go
// handler (webview_window_windows.go: a request whose scheme is not
// "http" is handed to the webview's default handler), and on Linux
// production the page is served through a custom wails:// URI scheme.
// WebSocket handshakes are NOT web-resource requests: the ws:// activity
// socket (ws://wails.localhost/ws/activity, derived from window.location
// in src/config.ts) therefore never reaches this server at all — the
// default network stack resolves *.localhost to the loopback address on
// port 80, where nothing listens. The browser E2E stack never saw this
// because it binds a real loopback HTTP listener, and the Windows runtime
// smoke never exercised a chat interaction.
//
// The consequence was the exact user-reported symptom: every live run
// frame (response/reasoning snapshots, status, done/error, run_snapshot
// replays, idle sentinels) is published by the activity hub but has NO
// reachable subscriber on the desktop, so streamed text could never
// appear while a run was live, `done` never arrived, and only the Stop
// button (an HTTP abort whose client path flushes synchronously and then
// recovers the persisted reply over plain HTTP) revealed the answer.
//
// THE FIX — same endpoint, same authority, reachable transport: the
// desktop process binds the EXISTING API handler (the one mux that owns
// /api/* and /ws/activity — no second handler implementation, no second
// streaming channel) on a loopback-only ephemeral listener
// (127.0.0.1:0). The frontend discovers the origin over the SAME
// intercepted HTTP surface (GET /api/ws-origin — empty origin means
// "stay same-origin", the browser/headless behavior) and points ONLY the
// activity WebSocket at it. HTTP fetches keep flowing through the
// intercepted same-origin path, so no CORS surface is introduced; the
// socket upgrade is origin-checked (ws.go allows the wails page origin
// against loopback request hosts only). Local-first/privacy posture is
// preserved: loopback bind, ephemeral port, in-process, origin-checked.

import (
        "context"
        "fmt"
        "net"
        "net/http"
        "strings"
        "time"
)

// loopbackShutdownGrace bounds one Close() drain of the loopback
// listener. Desktop sockets are short-lived (one activity socket per
// session surface); a graceful shutdown completes in milliseconds.
const loopbackShutdownGrace = 2 * time.Second

// StartLoopback binds the existing API handler on 127.0.0.1:0 and serves
// it until Close. The resolved origin ("http://127.0.0.1:PORT") is what
// GET /api/ws-origin reports to the frontend. Starting twice is a no-op
// (single origin per server lifetime); a bind failure is returned to the
// caller — the desktop shell logs it honestly (the webview-intercepted
// HTTP surface keeps working either way).
func (s *Server) StartLoopback() (string, error) {
        s.loopbackMu.Lock()
        defer s.loopbackMu.Unlock()

        if s.loopbackOrigin != "" {
                return s.loopbackOrigin, nil
        }

        listener, err := net.Listen("tcp", "127.0.0.1:0")
        if err != nil {
                return "", fmt.Errorf("loopback activity transport bind: %w", err)
        }

        server := &http.Server{
                // The SAME handler the desktop asset mux routes /api and /ws to —
                // one API authority, one WebSocket endpoint, no parallel surface.
                Handler:     s.Handler(),
                ReadTimeout: 0, // websockets are long-lived; the write deadline per frame owns stall detection
        }

        srv := server

        go func() {
                _ = srv.Serve(listener) // Close() drains; Serve always returns an error
        }()

        s.loopbackSrv = server
        s.loopbackOrigin = fmt.Sprintf("http://%s", listener.Addr().String())

        return s.loopbackOrigin, nil
}

// LoopbackOrigin reports the desktop loopback transport origin, or ""
// when no loopback listener is running (headless/browser serving — the
// frontend then stays same-origin exactly as before v1.9.4).
func (s *Server) LoopbackOrigin() string {
        if s == nil {
                return ""
        }

        s.loopbackMu.Lock()
        defer s.loopbackMu.Unlock()

        return s.loopbackOrigin
}

// closeLoopback drains the loopback listener (bounded). Safe to call on
// servers that never started one.
func (s *Server) closeLoopback() {
        s.loopbackMu.Lock()
        server := s.loopbackSrv
        s.loopbackSrv = nil
        s.loopbackOrigin = ""
        s.loopbackMu.Unlock()

        if server == nil {
                return
        }

        ctx, cancel := context.WithTimeout(context.Background(), loopbackShutdownGrace)
        defer cancel()

        _ = server.Shutdown(ctx)
}

// handleWSOrigin serves GET /api/ws-origin — the frontend's discovery
// call for the desktop loopback activity transport. The response is
// deliberately tiny and identifier-free: {"origin": "..."} with the empty
// string meaning "use the page's own origin" (browser, headless E2E and
// every non-desktop serving mode). The call rides the intercepted
// same-origin HTTP surface, so it works on every platform and needs no
// CORS.
func (s *Server) handleWSOrigin(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
                writeErr(w, http.StatusMethodNotAllowed, fmt.Errorf("GET required"))
                return
        }

        origin := s.LoopbackOrigin()

        // Defense in depth: the loopback origin is only ever meaningful for
        // the local desktop page. A wss/https page (remote serving) must never
        // be pointed at an http:// loopback socket.
        if origin != "" && strings.HasPrefix(pageOrigin(r), "https") {
                origin = ""
        }

        writeJSONStatus(w, http.StatusOK, map[string]string{"origin": origin})
}

// pageOrigin renders the request's own scheme://host for the https guard
// above (r.Host already carries the loopback host:port).
func pageOrigin(r *http.Request) string {
        scheme := "http"

        if r.TLS != nil {
                scheme = "https"
        }

        return scheme + "://" + r.Host
}
