package api

// loopback.go — v1.9.5 (P0): the DESKTOP LIVE-ANSWER TRANSPORT, hardened.
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
// THE FIX — same endpoint, same authority, reachable transport, LEAST
// PRIVILEGE: the desktop process binds a loopback-only ephemeral listener
// (127.0.0.1:0) that serves EXACTLY ONE route — the activity WebSocket
// upgrade under /ws/activity. The v1.9.4 handler (the full s.Handler())
// exposed session, run, engine and updater REST endpoints to every local
// process that could discover or scan the port; v1.9.5 narrows the
// listener to the single route the desktop page strictly requires. REST
// and discovery keep flowing through the Wails-intercepted handler — no
// CORS surface, no second handler semantics, no parallel streaming
// subsystem (the SAME handleActivityWS authority is mounted).
//
// AUTHENTICATION (v1.9.5): a loopback listener is visible to every local
// process, and Origin checks do not authenticate arbitrary native
// processes. The listener therefore requires a cryptographically random
// per-server CAPABILITY TOKEN on every upgrade:
//   - minted from crypto/rand (256-bit) at listener start;
//   - delivered ONLY through the trusted Wails-intercepted discovery
//     surface (GET /api/ws-origin, same-origin HTTP inside the webview);
//   - required as the `token` query parameter on the upgrade request,
//     compared in constant time;
//   - never logged (the no-ID/no-secret log contract).
// The upgrade ALSO requires a non-empty Origin the existing origin
// authority accepts (the wails page origin against a loopback request
// host) — an absent Origin cannot bypass loopback authentication, and
// non-loopback request hosts are refused outright.

import (
        "context"
        "crypto/rand"
        "crypto/subtle"
        "encoding/hex"
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

// newCapabilityToken mints the per-server loopback capability token
// (256 bits of crypto/rand, hex-encoded).
func newCapabilityToken() (string, error) {
        b := make([]byte, 32)

        if _, err := rand.Read(b); err != nil {
                return "", fmt.Errorf("loopback capability token: %w", err)
        }

        return hex.EncodeToString(b), nil
}

// StartLoopback binds the least-privilege desktop activity listener on
// 127.0.0.1:0 and serves it until Close. The resolved origin
// ("http://127.0.0.1:PORT") is what GET /api/ws-origin reports to the
// frontend together with the capability token. Starting twice is a no-op
// (single origin per server lifetime); a bind failure is returned to the
// caller — the desktop shell treats a broken activity transport as a
// hard, visible fault and exits (v1.9.5: a desktop app that cannot live
// stream must not launch silently degraded).
func (s *Server) StartLoopback() (string, error) {
        s.loopbackMu.Lock()
        defer s.loopbackMu.Unlock()

        if s.loopbackOrigin != "" {
                return s.loopbackOrigin, nil
        }

        token, err := newCapabilityToken()
        if err != nil {
                return "", err
        }

        listener, err := net.Listen("tcp", "127.0.0.1:0")
        if err != nil {
                return "", fmt.Errorf("loopback activity transport bind: %w", err)
        }

        // LEAST PRIVILEGE: exactly one route. The activity upgrade rides
        // the ONE handleActivityWS authority (no parallel handler, no
        // second streaming subsystem); every other path — session, run,
        // engine, updater REST — is refused on this listener.
        mux := http.NewServeMux()
        mux.Handle("/ws/activity", s.loopbackActivityGuard(listener.Addr().String(), token))

        server := &http.Server{
                Handler:     mux,
                ReadTimeout: 0, // websockets are long-lived; the write deadline per frame owns stall detection
        }

        srv := server

        go func() {
                _ = srv.Serve(listener) // Close() drains; Serve always returns an error
        }()

        s.loopbackSrv = server
        s.loopbackToken = token
        s.loopbackOrigin = fmt.Sprintf("http://%s", listener.Addr().String())

        return s.loopbackOrigin, nil
}

// loopbackActivityGuard wraps the ONE activity upgrade handler with the
// listener-local trust boundary: loopback host only, a valid capability
// token, and a non-empty accepted Origin. It performs NO work itself —
// every validated request is handed to s.handleActivityWS unchanged.
func (s *Server) loopbackActivityGuard(listenAddr, token string) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                // The listener serves exactly one path and one method.
                if r.URL.Path != "/ws/activity" || r.Method != http.MethodGet {
                        http.NotFound(w, r)
                        return
                }

                // Loopback bind is enforced at the socket; the explicit
                // host check additionally refuses any request whose Host
                // header was rewritten (DNS-rebinding style).
                if r.Host != listenAddr {
                        http.NotFound(w, r)
                        return
                }

                // Non-loopback peers cannot exist on this listener, but a
                // forwarded/proxied-looking request is still refused.
                if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
                        http.NotFound(w, r)
                        return
                }

                // Capability token — constant-time compare, always required.
                got := r.URL.Query().Get("token")
                if got == "" ||
                        subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
                        http.Error(w, "forbidden", http.StatusForbidden)
                        return
                }

                // Non-empty accepted Origin: the desktop page is the only
                // legitimate client and cross-origin WS upgrades carry an
                // Origin header. An absent Origin must never bypass the
                // loopback authentication.
                origin := strings.TrimSpace(r.Header.Get("Origin"))
                if origin == "" || !allowedOrigin(origin, r.Host) {
                        http.Error(w, "forbidden", http.StatusForbidden)
                        return
                }

                s.handleActivityWS(w, r)
        })
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

// LoopbackToken reports the desktop loopback capability token. It is
// delivered exclusively through the trusted Wails-intercepted discovery
// surface (handleWSOrigin) and never logged.
func (s *Server) LoopbackToken() string {
        if s == nil {
                return ""
        }

        s.loopbackMu.Lock()
        defer s.loopbackMu.Unlock()

        return s.loopbackToken
}

// closeLoopback drains the loopback listener (bounded). Safe to call on
// servers that never started one, and idempotent/concurrency-safe: the
// first caller takes ownership of the server handle, later callers (and
// Close re-entries) observe nil and return.
func (s *Server) closeLoopback() {
        s.loopbackMu.Lock()
        server := s.loopbackSrv
        s.loopbackSrv = nil
        s.loopbackOrigin = ""
        s.loopbackToken = ""
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
// deliberately tiny: {"origin": "...", "token": "..."} where the empty
// origin means "use the page's own origin" (browser, headless E2E and
// every non-desktop serving mode; no token is published in that case).
// The call rides the intercepted same-origin HTTP surface, so it works
// on every platform and needs no CORS. The token is a capability
// credential: it travels only here (inside the webview) and is never
// logged anywhere.
func (s *Server) handleWSOrigin(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
                writeErr(w, http.StatusMethodNotAllowed, fmt.Errorf("GET required"))
                return
        }

        origin := s.LoopbackOrigin()
        token := ""

        // Defense in depth: the loopback origin is only ever meaningful for
        // the local desktop page. A wss/https page (remote serving) must never
        // be pointed at an http:// loopback socket.
        if origin != "" && strings.HasPrefix(pageOrigin(r), "https") {
                origin = ""
        }

        if origin != "" {
                token = s.LoopbackToken()
        }

        writeJSONStatus(w, http.StatusOK, map[string]string{"origin": origin, "token": token})
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
