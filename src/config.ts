export const API_BASE = "/api";

function browserOrigin(): string {
  return window.location.origin;
}

export const WS_BASE = `${window.location.protocol === "https:" ? "wss:" : "ws:"}//${window.location.host}`;

// ---------------------------------------------------------------------------
// v1.9.4 (P0 — the desktop live-answer transport):
//
// Packaged desktop builds serve the page through the webview's
// resource-request interception, which carries HTTP only (Windows
// WebView2 forwards just http://wails.localhost requests into the Go
// handler; Linux production serves the wails:// custom scheme). WebSocket
// handshakes are NOT web-resource requests, so the activity socket derived
// from window.location (ws://wails.localhost/ws/activity) could never
// reach the backend there: zero live frames, no streamed text, no done
// event — the answer became visible only after Stop, whose HTTP abort
// path recovers the persisted reply via the history reload.
//
// The desktop process therefore ALSO serves the same API handler on a
// loopback-only ephemeral listener (internal/api/loopback.go) and
// publishes its origin through this discovery call, which rides the
// intercepted same-origin HTTP surface and works on every platform.
//
// Resolution is once-per-page-load, best-effort and cached:
//   - an explicit http:// loopback origin is honoured for the socket;
//   - an empty origin (browser, headless E2E, every non-desktop serving
//     mode — and the scripted unit-test transports) keeps the historical
//     same-origin WS_BASE behavior unchanged;
//   - any failure keeps same-origin — never a broken connect.
// ---------------------------------------------------------------------------
let wsOriginOverride: string | null = null;
let wsOriginResolution: Promise<void> | null = null;

export function ensureActivityWsOrigin(): Promise<void> {
  if (wsOriginResolution === null) {
    wsOriginResolution = fetch(`${API_BASE}/ws-origin`)
      .then((res) => (res.ok ? res.json() : null))
      .then((body: unknown) => {
        const origin =
          body &&
          typeof body === "object" &&
          typeof (body as { origin?: unknown }).origin === "string"
            ? ((body as { origin: string }).origin as string)
            : "";

        // Only a loopback http origin is honoured — never a remote or
        // https value (the server applies the same guard).
        wsOriginOverride =
          origin.startsWith("http://127.0.0.1") ||
          origin.startsWith("http://localhost")
            ? origin
            : null;
      })
      .catch(() => {
        // Discovery is best-effort; same-origin remains the fallback.
        wsOriginOverride = null;
      });
  }

  return wsOriginResolution;
}

export function activityWebSocketURL(sessionId?: string | null): string {
  const query =
    sessionId && sessionId.trim()
      ? `?sessionId=${encodeURIComponent(sessionId)}`
      : "";

  return `${wsOriginOverride ?? WS_BASE}/ws/activity${query}`;
}

export function apiOrigin(): string {
  return browserOrigin();
}
