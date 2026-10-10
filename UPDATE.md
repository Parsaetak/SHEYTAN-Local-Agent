# UPDATE.md — v1.9.4 Release Notes & Maintenance Behavior

**Release:** `v1.9.4` (canonical application version; single version
hierarchy: package.json → release-version.mjs → config.go /
build/config.yml / SIGNATURE)
**Base:** `main @ 5bcd17b` (`v1.9.3`) · **Date:** 2026-10-10
**Package:** `SHEYTAN-Local-Agent-v1.9.4-FINAL.zip` (complete repository tree)

The authoritative per-release history lives in `changelog.md`; this file
carries the CURRENT release notes and the operational maintenance
behavior. The v1.9.0 feature surface (AI Systems, Goals, Approvals,
bounded delegation, `repo_nav`) is unchanged in v1.9.4 and is documented
in the changelog; its maintenance behavior below carries forward.

## What v1.9.4 changes

1. **P0 — the Windows "answer appears only after Stop" defect is
   root-caused and repaired at the TRANSPORT layer.** The proven root
   cause: in packaged desktop builds the activity WebSocket never
   reached the backend at all. Wails `v3.0.0-beta.16` serves the
   webview through resource-request interception, which carries HTTP
   only (Windows WebView2 forwards just `http://wails.localhost` into
   the Go handler; Linux production serves the `wails://` custom
   scheme). WebSocket handshakes are not web-resource requests and no
   loopback listener existed, so `ws://wails.localhost/ws/activity`
   resolved to loopback port 80 — nothing there — and the client's
   20 reconnect attempts failed silently. Live run frames never reached
   the UI: no streamed text, no `done`, a locked composer. Stop worked
   only because abort is HTTP (synchronous flush + history reload) —
   exactly the reported symptom, and exactly why four releases of
   client-side scheduling repairs never changed it. The repair keeps
   ONE authority: the desktop process additionally serves the EXISTING
   API handler on a loopback-only ephemeral listener
   (`internal/api/loopback.go`); `GET /api/ws-origin` publishes the
   origin over the intercepted HTTP surface (empty = same-origin, the
   unchanged browser/headless contract); the client resolves it once
   and points ONLY the activity socket there; the upgrade is
   origin-checked (the Wails page origin against loopback request hosts
   only — no wildcard). Pinned by `internal/api/loopback_v194_test.go`
   including a REAL WebSocket upgrade through the loopback with the
   Wails page origin receiving the `attached` acknowledgement.

2. **P0 — the two zero-session E2E failures (Actions run 38069168867)
   were a proof-selector defect.** The v1.9.3 helper
   `expectRealAssistantReply` waited on `.message-content`, which exists
   only on the LIVE surfaces; a settled assistant reply renders through
   `div.message-markdown` and never carries that class, so the helper
   could only match inside the finalising window (a timing race Agent
   mode's CI run happened to win; local reproduction failed 3/4). The
   failure artifacts show the settled reply rendered with real content
   throughout. The helper now asserts the settled Markdown surface with
   the strength contract preserved and tightened (placeholder rows
   excluded, non-whitespace text asserted on the markdown surface
   itself). No assertion weakened, no placeholder accepted.

3. **P1 — engine variant transactions can no longer stop a RUNNING
   engine under an active run.** The AUTO Vulkan eligibility checked
   "no run is active" once, before a transaction that can run for many
   minutes and stops a running engine with no re-check (the supplied
   Windows log's candidate/rollback messages overlapped a user
   generation). `LlamaServer.SetRunGate` (wired to the API layer's
   `anyRunActive`) is consulted before anything happens: running engine
   + active run fails CLOSED with `ErrEngineBusyWithRun`; the AUTO probe
   records a DEFERRAL, never a persisted bounded failure.

4. **P1 — the run summary names every boundary honestly.** The old
   `publishMs` (accepted→donePublished) was renamed `acceptToDoneMs`;
   the summary now also carries `gateMs`, the orchestrator's measured
   `ttftMs`, and aggregate published-event counters (`events`, `deltas`,
   `firstDeltaMs`). The supplied v1.9.3 log's `publishMs=82178` invited
   misreading it as socket latency; it never was one.

5. **P0-A — the opt-in sanitized stream trace + the real-Windows
   acceptance harness.** `src/stream-trace.ts` arms only on
   `localStorage SHEYTAN_STREAM_TRACE=1`: every client boundary
   (socket receive/attach/snapshot → fold → flush → DOM commit → run
   end) recorded with monotonic timestamps, ephemeral sequence numbers
   and LENGTHS ONLY (unit-pinned: content never enters the trace). The
   zero-session spec attaches the dump to any failure.
   `scripts/windows-live-answer-acceptance.mjs` runs the exact release
   contract on a real Windows host: launch → send → real non-placeholder
   text while Stop is visible → strictly longer DOM text while Stop
   stays up → one persisted answer → reload → clean close.

## Verification evidence (classes kept separate)

* **Local, this release (Linux authoring host):** `npm ci`;
  `npm run typecheck` green; `npm run lint` 0 warnings/0 errors;
  `npm run test:units` 226/226 (incl. the new stream-trace suite);
  `npm run test:release` 38/38; `node scripts/release-version.mjs
  --check` green; clean `npm run build` + `npm run verify:web` green;
  Go `build` clean; `go vet -tags headless ./internal/... ./cmd/...`
  clean; `go test ./internal/... -tags headless -count=1` — 62 packages
  ok, 0 FAIL; `go test -race -tags headless -count=1 ./internal/api
  ./internal/agent ./internal/sessions ./internal/contextplan
  ./internal/histref ./internal/runtime` — all six ok; fresh CMake
  configure/build + native CTest 13/13; the browser E2E suite run to
  completion locally: **40/40 passed** — agent 3, ai-systems 3, chat 5,
  composer 3, lab 2, live-stream 3 (incl. rAF-suspended), model-first 6,
  run-control 5, sessions 6, zero-session 4 (BOTH previously-failing
  tests green: "deleting the final session … Send creates + runs" and
  "reload after a zero-session Send", with the strengthened settled
  Markdown assertions).
* **Reproduction evidence:** the two zero-session failures reproduce
  locally against the v1.9.3 baseline (3/4 failing) and pass with the
  retargeted helper; the desktop root cause is proven from the pinned
  Wails beta.16 sources plus the loopback upgrade test.
* **CI:** the authoritative verdict for v1.9.4 is the Actions run that
  carries this revision. v1.9.3's latest run (38069168867) FAILED and
  is not a green baseline; no green-run claim is made for v1.9.4 until
  that run exists.
* **UNVERIFIED (honest):** the end-to-end Windows WebView2 interaction
  (real desktop app, real compositor). This host cannot run it; the
  shipped harness is the proof vehicle. The server-side transport
  (loopback bind, discovery, upgrade, attach ack) IS proven by tests.

## Maintenance behavior (unchanged where not stated)

* **Update flow** — the existing updater/downloader authority checks,
  stages and installs releases; staging keeps a resumable `.part` file;
  cancel stops the download without deleting it.
* **Data locations** — user config remains `config.json` under the
  canonical data root; durable stores live beside the existing ones:
  `<data>/ai-systems/`, `<data>/goals/`. Deleting an AI System document
  or goal document by hand is tolerated (the stores are corruption-
  tolerant and repair the active pointer to Default).
* **Default AI System** — reserved id `default`; it cannot be deleted;
  deleting the ACTIVE system falls activation back to Default. Its
  behavior is the pre-1.9 runtime behavior (no instructions, no
  overrides).
* **Engine/execution evidence** — unchanged from v1.8: detection ≠
  availability ≠ selection ≠ execution ≠ verification; identity-bound
  execution receipts; one accelerator authority; one Governor. The
  Governor's CPU policy contract is documented in
  `internal/governor/governor.go` and pinned by tests in both
  directions.
* **Native engine numerics** — the resolved weight table and
  `prefill_span` are bit-for-bit identical to the serial path (pinned by
  the strengthened `test_prefill_parity`: per-position logits via the
  chunk=1 cadence, final-token logits per production chunk size, full KV
  bytes across every chunking); the public `Forward::token()` semantics
  are unchanged and every existing native test still passes.
* **Window close semantics** — closing the main window quits the
  application (single-window product, no tray). `srv.Close()` and the
  owned engine/server/background cleanup run through the existing
  deferred path.
* **Frontend verification stack** — typecheck, oxlint (0 warnings/0
  errors), the node unit suite (`npm run test:units`, 226/226), the
  release gates (`npm run test:release`) and the production build all
  run green on this release locally.
* **Desktop activity transport (v1.9.4)** — the packaged desktop process
  binds the API on a loopback-only ephemeral listener
  (`internal/api/loopback.go`); the page discovers it via
  `GET /api/ws-origin` (empty origin = same-origin, the browser/headless
  contract). The WebSocket upgrade accepts the Wails page origin against
  LOOPBACK request hosts only; nothing external is exposed and the
  privacy posture is unchanged.
* **Browser E2E** — the full Playwright suite runs in CI against the
  real headless server + real native engine + fixture GGUF; all
  visibility proofs observe real streamed content only.

## Measured performance facts (native engine path, honest)

* Native-engine forward pass after the v1.9.2 resolved-weight-table
  repair: **3.66 ms per token** on the E2E live fixture (was 19.0 ms —
  5.2× faster), measured on a 2-core host with the standalone engine
  benchmark. The E2E prefill of ~1681 engine-side tokens now costs
  ~6.2 s standalone versus >60 s under contention before the repair.
  Decode on the same fixture scales identically (the same per-token
  path). The per-token cost is now dominated by the actual arithmetic
  (the double-accumulator dot products), not tensor-name resolution.
* Prefill uses `prefill_span` in 16-token chunks with last-token-only
  logits; on real-vocabulary models the skipped per-token logits
  projection is an additional large saving (the fixture's 50-token
  vocab makes it negligible there). No new TTFT numbers are claimed in
  v1.9.3 — the prefill path is unchanged from v1.9.2.

## Known boundaries (honest)

* The v1.9.4 local verification sandbox proved: the frontend gates
  (typecheck/lint/units/release/build), the Go build/vet, the targeted
  Go suites (loopback transport + origin matrix; run-gate + strict
  variant transactions), a clean native CMake configure/build/ctest,
  and the browser E2E suite (results below). A local pass does NOT
  establish a green Actions run — the authoritative CI verdict for
  v1.9.4 is the Actions run that carries this revision.
* The END-TO-END Windows WebView2 interaction was NOT executed in the
  v1.9.4 local sandbox (no Windows host, no WebView2 runtime). The
  desktop root cause is proven from the pinned Wails beta.16 sources
  and the loopback transport is proven by Go tests; the desktop
  user-journey proof for v1.9.4 is the shipped acceptance harness
  (`scripts/windows-live-answer-acceptance.mjs`) run on a real Windows
  host, and desktop-runtime evidence remains CI-bound until those jobs
  run green.
* The goal runner does not yet decompose steps into subtasks
  automatically (the delegation engine is implemented and tested; the
  integration is the first v1.9.x work item).
* The protected-evaluation anti-hack guard is designed but not
  implemented; until it lands, protected evaluation hygiene relies on
  the existing Lab policy and workspace boundaries.
* The MCP client remains implemented and tested but is not yet
  registered into the runtime tool registry.
