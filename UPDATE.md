# UPDATE.md — v1.2.2 SHEYTAN-LA Generation Visibility & Black-Screen Repair Package

**Release:** `v1.2.2` (codename Zeta) · **Base:** `main @ 3bc4008` (`v1.2.1`)
**Date:** 2026-09-15
**Package:** `SHEYTAN-Local-Agent-v1.2.2-UPDATE.zip`

This package is a focused stability and generation-visibility repair of the
v1.2.1 tree. It fixes the black-screen/System-tab crash at its ROOT, makes
mid-generation tab switching safe, makes every run visible and alive from
Send to completion, and makes engine/log reporting honest. It is a COMPLETE
REPLACEMENT of the files it lists; every path is relative to the repository
root. No architecture changes and no unrelated feature work were made.

The version moves `1.2.1` → `1.2.2` through the established identity chain
(package.json → release-version.mjs → internal/config/config.go,
build/config.yml, SIGNATURE — `--check` is green).

What this package fixes, honestly and at the root:

1. **The black screen (root cause, not a mask).** The app had ZERO React
   error boundaries: a render exception anywhere unmounted the entire
   React tree and left a dead window. The crash itself was real and
   code-pinned: `internal/hardware.Collect` and `internal/sysinfo.Probe`
   leave the GPU slice nil when the platform probe finds nothing — and a
   nil Go slice marshals as JSON `null` — so `/api/environment` answered
   `"gpus": null` on machines where the Windows WMI/PowerShell probe
   fails (which happens precisely under heavy inference load: full CPU
   starves `Get-CimInstance Win32_VideoController`). `DeviceCard` then
   read `device.gpus.length` → `TypeError` → black screen. This is why
   the System tab crashed DURING generation. The fix is three-layered:
   the wire contract (`GPUs: []GPU{}` / nil-guarded `info.GPU`, locked by
   `internal/hardware/hardware_test.go`), defensive frontend reads
   (`Array.isArray` guards across every System card), and — so this
   CLASS of failure can never blank the app again — error boundaries at
   two levels: `AppErrorBoundary` around the whole app (`main.tsx`) and
   one `PanelErrorBoundary` per lazy workspace panel (`App.tsx`), each
   with a scoped recover/reload card and a view-switch reset.
2. **The tab-switch mid-generation freeze (root cause).** `App.tsx` keys
   the workspace `<main>` by view, so every tab switch unmounts
   `AgentBody` — whose cleanup called `disconnectActivity()`. The backend
   `activityHub` broadcasts live events with NO replay: every event
   published while the user was on the System tab was lost — including
   `done`, which left `running` stuck `true` forever and the composer
   permanently disabled. The reconnect-time backend `idle` sentinel
   (`{type:"idle", caption:"No active run"}`) was never handled by the
   frontend at all. The fix: refcounted socket/poll OWNERSHIP
   (`acquireActivity`/`releaseActivity` + engine-poll equivalents).
   AgentBody acquires on mount and releases on unmount; a LIVE run holds
   the lease across tab switches; resources release exactly when the run
   settles and the last consumer is gone — no lost events, no duplicate
   sockets or polls on return, no leaks. The `idle` sentinel is now a
   first-class recovery signal: a run the UI believes live but the
   backend no longer registers is re-synced from the authoritative
   session history (`recoverRunFromIdle`), with a 2.5 s grace guard
   against the standby attach race right after Send.
3. **The streaming duplication bug (wire-contract mismatch).** The
   orchestrator's `emitProgress` publishes CUMULATIVE captions — every
   `response`/`reasoning` activity event carries the FULL text emitted
   so far. The store APPENDED every caption, so a stream of snapshots
   `["A", "AB", "ABC"]` rendered as `AABABABC…` (and the appended string
   grew quadratically). The new pure accumulator
   (`src/stream-accumulator.ts`, unit-tested) implements replace
   semantics per stream: the displayed text is always exactly what the
   backend last emitted — nothing invented, nothing duplicated — and
   replayed/duplicate frames after reconnects are idempotent BY
   CONSTRUCTION (replace-with-same-value).
4. **The dead visual gap at generation start.** Nothing rendered between
   Send and the first streamed token (the engine gate can hold for
   minutes). The new GenerationBubble mounts the INSTANT the run starts
   and walks the real backend-driven lifecycle
   `Preparing → Thinking → Generating → Finalising → Complete` (pure
   state machine in `src/run-phase.ts`, unit-tested — unknown/stale
   events are no-ops, abort is terminal, late `done` cannot resurrect a
   stopped run). It shows a phase badge, an elapsed clock, the streamed
   answer, progressive tool/context/engine activity alongside, and a
   collapsible reasoning panel (auto-open while the model thinks,
   auto-fold when the answer starts streaming, the user's explicit choice
   always wins) that completed messages retain. Autoscroll follows the
   live output unless the user deliberately scrolled up (existing
   behaviour, now covering reasoning and phase changes too).
5. **Partial output preservation.** `done` keeps the streamed bubble
   visible through `Finalising` until the authoritative history replaces
   it — no blink, and if the reload races or fails, a bounded fallback
   re-checks. `error` and abort promote the streamed partial into the
   conversation locally (clearly marked, never presented as a persisted
   reply) exactly when the history did NOT gain the assistant message.
   Abort unlocks the composer immediately and settles the timeline as
   Stopped (the backend's own abort captions are honoured).
6. **System-tab crash hardening beyond the root fix.** Environment and
   health probes are `AbortController`-cancellable and ignore results
   after unmount (no unmounted-state mutation, no navigation races);
   recommendation application guards its setters against unmounted
   continuation; every optional payload field renders `—` instead of
   throwing on malformed/null backend payloads.
7. **Honest engine-asset reporting (the Windows log evidence).**
   `llamaDownloadURL` previously returned one hard-coded message for
   every failure — "no prebuilt llama.cpp server asset … (upstream no
   longer publishes Linux binaries)" — so a Windows machine whose
   `api.github.com`/Atom requests are blocked (the supplied log's socket
   failures) was told a wrong story at engine startup. Failures are now
   classified (`updater.IsNoAssetError`, unit-tested): genuine asset
   absence, unsupported platform, and network unreachability each report
   their real remedy (reconnect and retry / place a prebuilt
   `llama-server(.exe)` in the bin folder / select the native backend).
   GitHub/Atom socket failures were already non-fatal WARNs and remain
   so — they can never destabilise generation or the UI.
8. **Log session clarity (the v0.8.0 evidence).** `app.log` persists
   across versions and boots, so stale historical startup entries sat
   indistinguishably above current runtime lines. Every process start
   now writes one unambiguous session banner —
   `==== SHEYTAN-LA v1.2.2 session start (pid N) ====` — through the
   normal ring/rotation pipeline (`logging.Manager.SessionBanner`, wired
   in `cmd/root.go`), so everything ABOVE a banner is verifiably
   historical in both the file and the in-app Log Viewer.
9. **Regression tests for every fixed state-machine/crash bug.** The run
   lifecycle machine and the streaming accumulator are pure modules with
   20 unit assertions (`npm run test:units` — Node's native TypeScript
   runner, no new dependencies). The GPU wire contract, the session
   banner and the no-asset/network classification are locked by three
   new Go test files (`internal/hardware/hardware_test.go`,
   `internal/logging/logging_sessionbanner_test.go`,
   `internal/updater/updater_noasset_test.go`).

## 1. Exact change map

```text
src/ErrorBoundary.tsx                NEW       AppErrorBoundary (root) +
                                                PanelErrorBoundary (per
                                                lazy panel, resetKey =
                                                view id) with
                                                recover/reload cards —
                                                the black-screen class
                                                fix
src/run-phase.ts                     NEW       pure generation lifecycle
                                                state machine (idle/
                                                preparing/thinking/
                                                generating/finalising/
                                                complete/error/aborted)
                                                driven only by real wire
                                                events
src/run-phase.test.ts                NEW       10 unit tests (transitions,
                                                terminality, abort-done
                                                ordering, labels)
src/stream-accumulator.ts            NEW       pure cumulative-snapshot
                                                accumulator — replace
                                                semantics per stream,
                                                replay-idempotent merges
src/stream-accumulator.test.ts       NEW       10 unit tests (replace-not-
                                                append, iteration reset,
                                                reconnect replay,
                                                malformed frames, merge)
src/store.ts                         modified  cumulative-snapshot
                                                streaming; run phase +
                                                startedAt + note state;
                                                acquire/release ownership
                                                (live-run lease across
                                                tab switches); idle-
                                                sentinel resync with
                                                attach-race grace;
                                                finalisation that
                                                confirms the history and
                                                preserves partial output;
                                                abort fallback; abort-
                                                caption honouring; WS
                                                reconnect dedup by
                                                construction
src/MessageStream.tsx                modified  GenerationBubble (immediate
                                                mount, phase badge,
                                                elapsed clock, progressive
                                                activity strip, finalising
                                                note); ReasoningPanel
                                                (collapsible during AND
                                                after generation, user
                                                intent wins); RunOutcomeNote;
                                                autoscroll covers reasoning
                                                + phase; empty-state guard
src/SystemPanel.tsx                  modified  defensive payload reads
                                                (Array.isArray/?? guards
                                                on every card);
                                                AbortController-cancellable
                                                environment/health probes;
                                                unmount-safe
                                                recommendation apply
src/AgentBody.tsx                    modified  acquire/release ownership
                                                instead of hard
                                                connect/disconnect (the
                                                tab-switch mid-run fix)
src/App.tsx                          modified  PanelErrorBoundary around
                                                every lazy panel; version
                                                fallback 1.2.2
src/main.tsx                         modified  AppErrorBoundary around
                                                <App/>
src/api.ts                           modified  environment()/health()
                                                accept an AbortSignal
src/styles.css                       modified  generation timeline, run
                                                outcome note, error
                                                boundary styles (existing
                                                design tokens only)
tsconfig.app.json                    modified  exclude *.test.ts from the
                                                app build (Node-run unit
                                                tests are not app code)
package.json                         modified  version 1.2.2; test:units
                                                script
package-lock.json                    modified  version 1.2.2
internal/hardware/hardware.go        modified  Collect guarantees
                                                GPUs = []GPU{} — "gpus":
                                                [] on the wire, never
                                                null (the crash source)
internal/hardware/hardware_test.go   NEW       locks the JSON contract
                                                (gpus never marshals as
                                                null)
internal/sysinfo/sysinfo.go          modified  Probe nil-guards
                                                info.GPU (same wire
                                                contract for /api/sysinfo)
internal/logging/logging.go          modified  Manager.SessionBanner
                                                (one unambiguous session
                                                separator per boot)
internal/logging/                    NEW       banner markers in the
  logging_sessionbanner_test.go                parsed viewer surface +
                                                noop-manager safety
cmd/root.go                          modified  SessionBanner call before
                                                the boot line
internal/updater/updater.go          modified  IsNoAssetError — no-asset
                                                vs network classification
internal/updater/                    NEW       classification unit tests
  updater_noasset_test.go                     (errNoAsset, atom wording,
                                                DNS/HTTP/timeout are NOT
                                                no-asset)
internal/llm/llama.go               modified   llamaDownloadURL honest
                                               error tails (network vs
                                               no-asset vs unsupported
                                               platform) + WARN log
internal/config/config.go           modified   AppVersion "1.2.2"
build/config.yml                    modified   productVersion "1.2.2"
SIGNATURE                           modified   first line v1.2.2
                                               (release-version.mjs
                                               repair)
web/static/**                       modified   regenerated embedded
                                               frontend (vite build +
                                               sync:web — new content
                                               hashes; the v1.2.1 hashed
                                               assets are DELETED)
README.md                           modified   v1.2.2 section + release
                                               header
agent.md                            modified   v1.2.2 notes for the next
                                               agent
UPDATE.md                           modified   this document
worklog.md                          modified   v1.2.2 entry appended
```

## 2. DELETE list (files removed by this package)

The regenerated embedded frontend replaces its own hashed artifacts
(apply over the base, then delete these v1.2.1 build outputs if the
archiver did not already exclude them):

```text
web/static/assets/AgentBody-CyMQd0oP.js
web/static/assets/AgentHeader-Dezw4syJ.js
web/static/assets/AgentSidebar-D2AIsqsa.js
web/static/assets/LabPanel-DNdaVwnC.js
web/static/assets/ResearchPanel-DVSjuNFM.js
web/static/assets/SettingsPanel-C5otSLaN.js
web/static/assets/SystemPanel-CzupHX9T.js
web/static/assets/index-CTp-kOgC.js
web/static/assets/index-DJQM74Q8.css
```

No source file is deleted.

## 3. DO NOT TOUCH list (this package deliberately leaves alone)

```text
internal/agent/**               orchestrator loop, tool pipeline,
                                verification — untouched (the streaming
                                wire contract is unchanged; only the
                                FRONTEND's interpretation of it is fixed)
internal/api/server.go          run/abort/WS hub machinery — untouched
                                (the idle sentinel already existed; the
                                frontend now honours it)
internal/llm/** (except        engine pairing, launch contract, vision
 llama.go's download-error      recording — untouched
 tails)
internal/sessions/**            session persistence, continuum —
                                untouched
internal/native/**              native engine — untouched
packaging/nsis/**               installer — untouched (v1.2.1 work
                                remains authoritative)
.github/workflows/**            CI — untouched (no packaging-path
                                changes)
cmd/stress*.go                  stress suite — untouched (v1.2.1's 45
                                scenarios remain authoritative)
src/Settings*.tsx               Settings surfaces — untouched
src/LabPanel.tsx, Research-     unaffected panels — untouched
  Panel.tsx, ModelPicker.tsx,
  ActivityStream.tsx, PerfStrip,
  LogViewer.tsx
```

## 4. Safe-apply procedure

1. Check out the base commit (`main @ 3bc4008`, v1.2.1) cleanly.
2. Replace every file listed in `REPLACEMENT-MANIFEST.txt` with the copy
   from this ZIP (all paths are repository-root relative) and remove the
   §2 DELETE-list build artifacts.
3. Verify: `shasum -a 256 -c REPLACEMENT-SHA256.txt` (from the repository
   root, with the package's files in place) — every entry must read OK.
4. Run the verification battery (§5) — every command must pass.

## 5. Verification executed (automated, per release policy)

| Check | Result |
|---|---|
| `node scripts/release-version.mjs --check` | PASS (4/4 surfaces consistent at 1.2.2) |
| `npm ci` | PASS (0 vulnerabilities) |
| `npm run typecheck` (`tsc --noEmit`) | PASS |
| `npm run build:web` (`tsc -b && vite build`) | PASS (strict app config incl. `noImplicitOverride`, `noUncheckedIndexedAccess`) |
| `npm run lint` (oxlint) | PASS (0 warnings, 0 errors, 33 files) |
| `npm run sync:web` | PASS (`web/static` regenerated) |
| `npm run test:units` (node --test, native TS) | PASS — **20/20**: run-phase machine (fresh-run start, no backwards transitions from Generating, done→finalising, history-confirm→complete, error/abort terminal from every live phase, done-after-abort cannot resurrect, live/terminal classification, labels) and stream-accumulator (cumulative replace-not-append, iteration reset replaces, reconnect replay idempotent, reasoning/content independence, malformed frames ignored, flush drains, merge keeps unchanged streams) |

**Environment exceptions (documented, not faked):** this sandbox has NO
Go toolchain, so `go build`, `go vet` and `go test` could NOT be executed
here — the three new Go test files
(`internal/hardware/hardware_test.go`,
`internal/logging/logging_sessionbanner_test.go`,
`internal/updater/updater_noasset_test.go`) are provided for CI/local
execution and were written against the packages' existing exported and
package-private surfaces with gofmt-style formatting. Every Go change was
kept minimal and syntax-reviewed line-by-line (the tab-indented files were
reconstructed from HEAD with surgical re-application to avoid whitespace
drift); the shipped-binary CI path remains the authoritative executor.
End-to-end runs against a live backend (real generation, real WebSocket
disconnects, real engine failure) equally require the compiled binary and
were not executed in this environment — the state-machine and
accumulator behaviours they exercise are what the 20 Node unit tests pin.

## 6. What "verified" means here

- **Frontend verified end-to-end at the toolchain level**: strict
  typecheck, lint, production build, embedded-asset sync, and 20 unit
  assertions over every fixed state-machine transition and the streaming
  wire contract.
- **Root causes, not masks**: each reported symptom is pinned to exact
  code (missing boundary; `gpus: null` on the wire; unmount-disconnect
  vs a no-replay hub + unhandled `idle`; append-vs-cumulative mismatch;
  hard-coded misleading error text; no session separator) and fixed at
  that source.
- **No stuck states by construction**: `running` clears on `done`,
  `error`, `idle`-resync and abort-fallback; the timeline settles in
  every path; partial output survives.
- **No resource duplication by construction**: refcounted ownership with
  a live-run lease; the store is the single socket owner; release
  happens exactly once per run end when unconsumed.
- **Final ZIP created and byte-identical**: see §7.

## 7. Package contents and integrity

The ZIP contains exactly:

```text
UPDATE.md
REPLACEMENT-MANIFEST.txt
REPLACEMENT-SHA256.txt
agent.md
build/config.yml
cmd/root.go
internal/config/config.go
internal/hardware/hardware.go
internal/hardware/hardware_test.go
internal/llm/llama.go
internal/logging/logging.go
internal/logging/logging_sessionbanner_test.go
internal/sysinfo/sysinfo.go
internal/updater/updater.go
internal/updater/updater_noasset_test.go
package.json
package-lock.json
README.md
SIGNATURE
src/AgentBody.tsx
src/App.tsx
src/ErrorBoundary.tsx
src/MessageStream.tsx
src/SystemPanel.tsx
src/api.ts
src/main.tsx
src/run-phase.test.ts
src/run-phase.ts
src/store.ts
src/stream-accumulator.test.ts
src/stream-accumulator.ts
src/styles.css
tsconfig.app.json
web/static/.vite/manifest.json
web/static/assets/AgentBody-EocjZLgJ.js
web/static/assets/AgentHeader-C5gpAJAl.js
web/static/assets/AgentSidebar-D_scJRE6.js
web/static/assets/LabPanel-Ys_1jxhi.js
web/static/assets/ResearchPanel-cRC5-t9l.js
web/static/assets/SettingsPanel-DrGWWN7P.js
web/static/assets/SystemPanel-BOrHcKSv.js
web/static/assets/index-Bto0wA8H.css
web/static/assets/index-DHBcE2Kp.js
web/static/index.html
worklog.md
```

Excluded by design: `.git`, caches, `dist/` staging trees, native build
directories, model files, binaries, temporary files, the v1.2.1 hashed
build artifacts (superseded — see §2), and every unrelated generated
artifact. A byte-identical copy of the ZIP is placed at the repository
root and verified with SHA-256 before delivery.
