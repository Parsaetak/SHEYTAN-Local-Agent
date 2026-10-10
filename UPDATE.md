# UPDATE.md — v1.9.2 Release Notes & Maintenance Behavior

**Release:** `v1.9.2` (canonical application version; single version
hierarchy: package.json → release-version.mjs → config.go /
build/config.yml / SIGNATURE)
**Base:** `main @ f687489` (`v1.9.1`) · **Date:** 2026-10-10
**Package:** `SHEYTAN-Local-Agent-v1.9.2-FINAL.zip` (complete repository tree)

The authoritative per-release history lives in `changelog.md`; this file
carries the CURRENT release notes and the operational maintenance
behavior. The v1.9.0 feature surface (AI Systems, Goals, Approvals,
bounded delegation, `repo_nav`) is unchanged in v1.9.2 and is documented
in the changelog; its maintenance behavior below carries forward.

## What v1.9.2 changes

1. **P0 — the Linux CI live-stream failures are root-caused with a
   complete measured chain and repaired in the engine, where the
   evidence pointed.** The authoritative failure (Actions run
   `37869632400`, Linux job `113625251074`) expired all three
   `e2e/live-stream.spec.ts` tests at the 60 s wait for real
   non-placeholder streamed text — the failure moved EARLIER than
   v1.9.0's, which invalidated the v1.9.1 "placeholder-only" diagnosis
   as a complete explanation. The reproduced stack measured the full
   chain: the E2E prompt's ~1785 briefing bytes encode to **~1681
   engine-side tokens** (the fixture vocab carries 5 merges — ~1 token
   per byte; the Go-side log estimate of ~445 is 3.8× lower), and the
   native engine prefilled SERIALLY at a measured **19.0 ms per token**
   — dominated by per-row tensor-name resolution (~8064 std::map +
   ostringstream resolutions per token), not by the arithmetic (a
   standalone benchmark: -O0 and -O2 builds time identically). First
   token ≈ 32 s standalone, beyond the 60 s budget under suite
   contention: the budget expired before the first real snapshot
   existed. The repair is the real optimization at the proven boundary:
   the **resolved weight table** (each layer's weight matrices and the
   output projection resolved ONCE per model binding; rows walked by
   pointer arithmetic — measured **5.2× per-token speedup**, 19.08 →
   3.66 ms/token) plus **`Forward::prefill_span`** (bounded 16-token
   chunks, last-token logits only, cancellation cadence preserved) plus
   a Release-default native CMake build (a bare `cmake -S -B` previously
   produced an unflagged, unoptimized engine). Numerical identity is
   pinned by the new native `prefill_parity` gate: last-token logits and
   full KV bytes must match the serial `token()` path **bit-for-bit**
   across chunk sizes 1/3/7/16.

2. **P0 — the desktop window-close lifecycle defect is fixed on both
   platforms.** Verified against the `wails v3.0.0-beta.16` sources: a
   webview window's WM_CLOSE (Windows) / GTK close-request (Linux) only
   EMITS the platform WindowClosing event and destroys the window — the
   application event loop keeps running with zero windows (on Windows
   only the hidden `__wails_hidden_mainthread` window's WM_CLOSE triggers
   `Quit`, and no quit-on-last-window logic exists for webview windows).
   Closing SHEYTAN-LA's main window therefore left a zombie process with
   the backend running and the deferred `srv.Close()` un-executed —
   measured as the authoritative Windows smoke failure in run
   `37869632400` (the process survived `CloseMainWindow()` + 15 s). The
   single-window application now binds `events.Common.WindowClosing` to
   `app.Quit()`, so the close path terminates the lifecycle normally and
   the deferred backend cleanup runs.

3. **P0 — the desktop runtime smoke gates now prove what they claim.**
   The Windows smoke checks `MainWindowHandle != 0` (a real top-level
   window), checks `CloseMainWindow()`'s BOOL (a DELIVERED close
   request), requires exit code 0 through the normal path, and reports
   the exact unmet evidence level when no window exists. The Linux smoke
   owns a real Xvfb display (no more `xvfb-run` wrapper-pid confusion —
   the wrapper's PID was never the app's), locates the app's REAL
   top-level window via xdotool (`--pid` + `getwindowpid` ownership
   check), closes it with the WM's own `WM_DELETE_WINDOW` client
   message, and requires normal-path exit with status 0. SIGTERM remains
   only a failing-run backstop, never the passing path.

4. **P1 — the zero-session live-surface clobber is root-caused and
   fixed.** With the faster engine, every zero-session Send lost its
   live generation surface ~90 ms in (measured 5/5: bubble + Stop
   visible at +0 ms, gone at +90 ms, "aborted" at +160 ms — while the
   backend run completed normally 14 s later). A frame + state-transition
   trace named the mechanism: the server's attach sequence is
   `attached → idle → run_snapshot`; the attach-time idle sentinel (no
   session, no lastRun — composed before the run existed) is processed
   just after `run()` sets its startup state, and
   `recoverRunFromIdle`'s final fallback declared the live run "lost".
   The grace re-check would have re-clobbered at +2.7 s (the real run's
   first gate evidence arrives at ~4.6 s). The repair is the
   **attach-handshake exclusion**: an idle sentinel without a lastRun
   block that lands within 250 ms of the attach acknowledgement is
   handshake, not run evidence, and never triggers recovery. The v1.2.6
   fast-run recovery (idle + lastRun) and late-idle semantics are
   unchanged.

5. **P1 — E2E observation contracts corrected without weakening.**
   `e2e/zero-session.spec.ts` now excludes `data-stream-placeholder`
   arms from its visible-before-completion proof (placeholder text is
   presentation, never model output — the same real-content contract as
   the live-stream spec), and its observation bound is 60 s (the engine
   gate + prefill phase legitimately precedes the first snapshot; the
   v1.9.1 30 s bound expired inside that phase under load). The
   live-stream spec attaches compact structured failure diagnostics
   (live-surface snapshot + server log tail) to a poll timeout, without
   dumping prompt/answer content.

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
  execution receipts; one accelerator authority; one Governor.
* **Native engine numerics** — the resolved weight table and
  `prefill_span` are bit-for-bit identical to the serial path (pinned by
  `test_prefill_parity`); the public `Forward::token()` semantics are
  unchanged and every existing native test still passes.
* **Window close semantics** — closing the main window quits the
  application (single-window product, no tray). `srv.Close()` and the
  owned engine/server/background cleanup run through the existing
  deferred path.
* **Frontend verification stack** — typecheck, oxlint, the node unit
  suite (`npm run test:units`), and the release gates
  (`npm run test:release`) all run green on this release.
* **Browser E2E** — the full Playwright suite (40 tests) runs against
  the real headless server + real native engine + fixture GGUF; all
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
  vocab makes it negligible there).

## Known boundaries (honest)

* The desktop runtime smoke stages were repaired in v1.9.2 and are wired
  for the CI environment; they were NOT executed in the v1.9.2 local
  verification sandbox (no root, no GTK4/WebKitGTK headers — the Wails
  desktop binary cannot build there — and no Windows). The worklog
  records this evidence boundary explicitly; the authoritative CI verdict
  for v1.9.2 is the Actions run that carries this revision.
* The goal runner does not yet decompose steps into subtasks
  automatically (the delegation engine is implemented and tested; the
  integration is the first v1.9.x work item).
* The protected-evaluation anti-hack guard is designed but not
  implemented; until it lands, protected evaluation hygiene relies on
  the existing Lab policy and workspace boundaries.
* The MCP client remains implemented and tested but is not yet
  registered into the runtime tool registry.
