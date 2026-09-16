# UPDATE.md — v1.2.3 SHEYTAN-LA Engine-Race Fix, Download Manager & Honest Download Progress Package

**Release:** `v1.2.3` (codename Zeta) · **Base:** `main @ f0ef0d2` (`v1.2.2`)
**Date:** 2026-09-15
**Package:** `SHEYTAN-Local-Agent-v1.2.3-UPDATE.zip`

This package repairs the failing Linux CI job at its ROOT (the native
engine's unload-guard race — a real engine contract gap, not merely a flaky
test), replaces every ad-hoc remote-asset download with ONE reusable,
verifiable, resumable, observable Download Manager, makes app-update staging
asynchronous and cancellable with live progress, and applies a coherent
downloader/UX pass across the whole application. It is a COMPLETE
REPLACEMENT of the files it lists; every path is relative to the repository
root. No second runtime is introduced and no unrelated feature work was
made.

The version moves `1.2.2` → `1.2.3` through the established identity chain
(package.json → release-version.mjs → internal/config/config.go,
build/config.yml, SIGNATURE — `--check` is green).

## 0. The CI failure and its root cause (what this package fixes first)

Failed run: **34921857962** (`Build Desktop` #67), job `Linux x64`, step
"Native engine (C++) build for Go integration tests" (ctest exit 8).
`native/engine/tests/test_generate.cpp` failed at the unload-guard check.

Root cause chain, code-pinned:

1. The test's sequencer polled `shtn_engine_scheduler_info()` until
   `active_requests > 0`. That observable comes from the scheduler's active
   slot, set in `worker_loop` BEFORE the executor runs.
2. The load/unload guard counted a DIFFERENT variable —
   `active_generations`, incremented inside `detail_runner_execute`, i.e.
   strictly later.
3. An unload landing in the window between the two observations found
   `active_generations == 0`, SUCCEEDED, and tore the model out from under
   the in-flight generation (the generation then failed with
   `SHTN_ERR_NO_MODEL`); the test's unconditional
   `CHECK(unload == SHTN_ERR_MODEL_STATE)` failed. ~25% of loaded CI runs
   (the test's own comment); reproduced locally at **56/200** iterations
   before the fix and **0/1200** after it.

The fix has two halves — neither weakens CI:

* **Engine contract:** one authority, `generation_in_flight()`
  (native/engine/src/engine.cpp), covers the WHOLE in-flight window —
  registered work (the generate call is in flight), the scheduler's active
  slot (popped, executor not yet entered) and the executor count — for BOTH
  `shtn_engine_load_model` and `shtn_engine_unload_model`. Observable state
  can never contradict the guard again: if any observer can see an active
  request, unload/load return `SHTN_ERR_MODEL_STATE`.
* **Test determinism:** the sequencer now waits on an EXPLICIT signal — the
  first streamed non-final token, which is emitted only from inside the
  runner's decode loop, strictly after the guard state is established — via
  a bounded condvar wait (60 s; a broken engine FAILS the test instead of
  hanging CI). It then asserts scheduler coherence (`active_requests == 1`),
  the unload rejection, the reload rejection (same guard), cancels the
  request, verifies the blocked `generate` returned `SHTN_ERR_CANCELLED`
  with finish reason `cancelled`, checks scheduler totals and reuses the
  engine. No sleeps, no poll budgets, no timing luck. Verified: 12/12
  ctest; 10/10 idle runs; 5/5 niced runs; 3/3 runs under CPU saturation
  (2 busy-loops on 2 cores).

## 1. Download Manager — architecture, verification/security model

New package `internal/downloader` (native Go, no external tools, no second
runtime). Every remote asset the app pulls now flows through it:

* **Transport** — shared `http.Transport` with keep-alive pooling and
  HTTP/2 where negotiated (persistent connections across jobs). Dial 15 s,
  TLS 10 s, response-header 60 s caps.
* **HTTPS only by default** — every source URL and EVERY redirect hop is
  scheme-checked; a https→http redirect is refused. Plain `http://` is
  accepted only for loopback hosts (`IsLoopbackURL`: 127.0.0.0/8, ::1,
  localhost) so the Go test doubles work — remote sources are always HTTPS.
* **Stream to disk, never RAM** — the body is copied to `<dest>.part` in
  64 KiB chunks; hashing streams the file; nothing is buffered whole.
* **.part → verify → atomic rename** — the artifact is activated ONLY after
  verification passes: SHA-256 when pinned (mismatch deletes the .part and
  refuses), declared size when known, hard `MaxBytes` cap (default 2 GiB)
  against hostile sources. Activation is a same-directory `os.Rename` plus
  a directory fsync; a partially downloaded or corrupt asset is NEVER
  activated.
* **Resume** — an existing `.part` continues via `Range: bytes=<n>-`;
  a 206 response continues, a 200 restarts honestly from zero. A `.part`
  oversized vs the pinned size is discarded. Cancellation (context or
  `Job.Cancel`) stops network + file activity immediately (the in-flight
  request context is cancelled so a blocked `Read` unblocks at once) and
  KEEPS the .part for a resumable retry.
* **Pause/Resume** — supported during the downloading phase where
  technically safe (flush + suspend + Range continue on resume).
* **Ordered sources & trust boundary** — candidates are
  authoritative release asset → configured/verified mirror → fallback.
  `TrustFallback` sources are SKIPPED unless the caller explicitly sets
  `AllowFallback`; the chosen source (URL, label, trust) travels with every
  progress report and the result — an untrusted mirror is never silently
  substituted. No user-provided URL is downloaded without such an explicit
  trust boundary.
* **Retries** — transient failures (network, 5xx, 429, 408) retry with
  bounded exponential backoff (500 ms → 8 s) + jitter; permanent 4xx move
  straight to the next source; a verification failure poisons THAT source's
  bytes (no retries on it).
* **Source cache** — `sheytan-source-cache/<key>.json` (TTL 24 h) remembers
  the last VERIFIED source per key so routine startup tries the known-good
  endpoint first instead of probing every mirror; failures fall through to
  the normal ordered list.
* **Rollback** — with `KeepBackup`, the previous working file is preserved
  as `<dest>.bak` before the new asset takes its place; if the rename
  fails, the backup is restored.
* **Progress** — phase (resolving / connecting / downloading / verifying /
  installing / ready), bytes done/total, speed (EWMA ~2 s window), ETA,
  attempt/retry count, source + trust label, verification state, pause
  state and retry/error reasons; `Job.Progress()` snapshot + throttled
  `OnProgress` callback (~4 Hz).

Callers: engine bootstrap (`llm.ensureBinary` → `downloadEngineArchive`),
engine self-update (`updater.UpdateEngineWithProgress`), scheduled engine
updater (`updater.downloadEngine`), app-update staging
(`updater.AppUpdateStagingJob` / `StageAppUpdateWithProgress`).

## 2. Runtime/update flow changes

* **Engine bootstrap** — the llama.cpp archive is fetched by the manager
  (resume-capable, verified, source-cached per GOOS/GOARCH) and extraction
  happens ONLY from the verified archive; the staged archive is removed
  after successful extraction. Live progress rides `EngineEvent.Download`
  → WS `engine` frames + `/api/engine` snapshot (`download` block);
  `POST /api/llama {"action":"cancel-download"}` stops it immediately
  (the .part resumes on retry).
* **Engine self-update / scheduled update** — same manager, same progress
  surface; the binary swap path (stop → stage → extract → swap → restart)
  is unchanged.
* **App updater** — `POST /api/update/download` now RETURNS IMMEDIATELY:
  it re-fetches the manifest (bounded), builds the pinned staging job
  (SHA-256 REQUIRED from the manifest, size verified when declared) and
  runs it on a background goroutine (single slot; second request → 409;
  15-minute cap). `GET /api/update/status` carries live `download`
  progress; `POST /api/update/cancel` stops network + file activity at
  once. `Server.Close()` cancels in-flight staging — the updater can never
  outlive the server. Staged installers are still NEVER executed by the
  app.
* **UI/runtime lifecycle** — the Updates card polls status only while a
  download runs (interval cleaned up on state change/unmount); the Agent
  runtime panel and the System Centre render compact download progress;
  the header phase pill shows a live percentage during engine bootstrap.

## 3. Change map (authoritative)

Status vocabulary: `ADD` (new file) · `REPLACE` (file fully replaced by
this package) · `MODIFY` (edited in place) · `DELETE` (removed by this
package) · `DO NOT TOUCH`.

```text
internal/downloader/downloader.go       ADD       Download Manager core (Options/Sources/Job/Run, retry + mirror orchestration, source cache reorder)
internal/downloader/transfer.go         ADD       HTTPS/scheme enforcement, persistent transport, Range resume, streaming + speed/ETA, verify + atomic activate + rollback
internal/downloader/meta.go             ADD       sheytan-source-cache (TTL'd last-good source memory)
internal/downloader/downloader_test.go  ADD       14 behavioural tests (happy path, skip-existing, checksum/size refusal, failover, resume, cancel, pause, trust boundary, retry classes, cache, backup, oversize)
src/DownloadProgress.tsx                ADD       shared downloader UI (phase chain, determinate bar, speed/ETA/source/verification, actions)

native/engine/src/engine.cpp            MODIFY    generation_in_flight() — whole-window load/unload guard (CI root cause)
native/engine/tests/test_generate.cpp   MODIFY    deterministic unload-guard block (first-token condvar signal, coherence + reload asserts, cancelled-rc asserts, reuse check)
native/engine/src/tokenizer.cpp         MODIFY    dead utf8_encode removed (warning cleanup)
native/engine/src/tensor.cpp            MODIFY    unused nbytes local removed (warning cleanup)

internal/llm/llama.go                   MODIFY    engine downloads via Download Manager (downloadEngineArchive/publishDownloadProgress/DownloadProgress/CancelDownload/engineArchiveSuffix/extractEngineArchive); EngineEvent.Download; dlJob/dlProgress
internal/updater/updater.go             MODIFY    downloadEngine on the manager (+onProgress); UpdateEngineWithProgress
internal/updater/appupdate.go           MODIFY    StageAppUpdate(WithProgress)/AppUpdateStagingJob on the manager; AppUpdateStatus.Download
internal/updater/appupdate_test.go      MODIFY    tamper case clears the staging dir first (skip-existing semantics documented)
internal/api/update.go                  REPLACE   async staging + /api/update/cancel + live progress in status
internal/api/engine.go                  MODIFY    engineSnapshot.Download + WS engine frames carry progress
internal/api/server.go                  MODIFY    appUpdateJob/appUpdateStaging/appUpdateCancel fields; "cancel-download" action; route
internal/api/lifecycle.go               MODIFY    Close() cancels in-flight staging
internal/config/config.go               MODIFY    AppVersion "1.2.3"
build/config.yml                        MODIFY    productVersion "1.2.3"
SIGNATURE                               MODIFY    first line v1.2.3 (release-version.mjs repair)
package.json / package-lock.json        MODIFY    version "1.2.3"

cmd/stress_release.go                   MODIFY    +downloader_integrity_resume, +downloader_untrusted_fallback

src/api.ts                              MODIFY    DownloadProgress type; engine/update download fields; updateCancel(); "cancel-download" action
src/store.ts                            MODIFY    modelsLoading flag (skeleton, not fake empty)
src/AgentBody.tsx                       MODIFY    compact engine-download panel + cancel in the runtime panel
src/AgentHeader.tsx                     MODIFY    live download percentage in the phase pill
src/ModelPicker.tsx                     MODIFY    loading skeleton branch (aria-busy)
src/SettingsVisionUpdates.tsx           MODIFY    async UpdatesCard: poll-while-downloading, progress panel, cancel, retry
src/SystemPanel.tsx                     MODIFY    compact download slot in RuntimeCard
src/styles.css                          MODIFY    .dl-* downloader styles on canonical tokens (+ skeleton card)
README.md                               MODIFY    v1.2.3 section + release header
agent.md                                MODIFY    v1.2.3 notes for the next agent
worklog.md                              MODIFY    v1.2.3 entry appended

web/static/**                           MODIFY    regenerated embedded frontend (vite build + sync:web; new content hashes)
UPDATE.md                               REPLACE   this document
REPLACEMENT-MANIFEST.txt                REPLACE   this package's manifest
REPLACEMENT-SHA256.txt                  REPLACE   SHA-256 of every file in this package
```

## 4. DELETE list

The regenerated embedded frontend replaces its own hashed artifacts
(apply over the base, then delete these v1.2.2 build outputs if the
archiver did not already exclude them):

```text
web/static/assets/AgentBody-CyMQd0oP.js
web/static/assets/AgentBody-EocjZLgJ.js
web/static/assets/AgentHeader-C5gpAJAl.js
web/static/assets/AgentHeader-Dezw4syJ.js
web/static/assets/AgentSidebar-D2AIsqsa.js
web/static/assets/AgentSidebar-D_scJRE6.js
web/static/assets/LabPanel-DNdaVwnC.js
web/static/assets/LabPanel-Ys_1jxhi.js
web/static/assets/ResearchPanel-DVSjuNFM.js
web/static/assets/ResearchPanel-cRC5-t9l.js
web/static/assets/SettingsPanel-C5otSLaN.js
web/static/assets/SettingsPanel-DrGWWN7P.js
web/static/assets/SystemPanel-BOrHcKSv.js
web/static/assets/SystemPanel-CzupHX9T.js
web/static/assets/index-Bto0wA8H.css
web/static/assets/index-CTp-kOgC.js
web/static/assets/index-DHBcE2Kp.js
web/static/assets/index-DJQM74Q8.css
```

No source file is deleted. Nothing under `web/static/assets` outside the
hash lists above is referenced by `web/static/index.html` after apply.

## 5. DO NOT TOUCH list (this package deliberately leaves alone)

```text
native/engine/** (except the four files above)   engine core, tokenizer,
                                                 KV cache, sampler,
                                                 scheduler, host — the fix
                                                 is scoped to the guard +
                                                 one test + warning sites
internal/agent/**                                orchestrator loop, tool
                                                 pipeline, verification —
                                                 untouched
internal/native/**                               Go-side native host
                                                 supervision — untouched
internal/sessions/**                             session persistence —
                                                 untouched
internal/memory/**, internal/recall/**           memory/recall — untouched
internal/hardware/**, internal/sysinfo/**        telemetry — untouched
packaging/nsis/**                                installer — untouched
.github/workflows/**                             CI — untouched (the fix is
                                                 in the code CI runs)
cmd/stress.go, cmd/stress_zeta.go                stress core — untouched
scripts/**                                       tooling — untouched
web/static/index.html structure                  only asset hashes change
user data: models/, sessions/, config.json,      NEVER touched by update
installed.json, updates/ staged artifacts        machinery
.git/, native/engine/build/, node_modules/,      excluded from the package
dist/, caches, temp files
```

## 6. Safe-apply procedure

1. Apply over a clean checkout of the base commit (`f0ef0d2`, v1.2.2).
2. Copy every file from this package over the tree (REPLACE/MODIFY/ADD).
3. Delete the v1.2.2 hashed assets listed in §4 if present.
4. Verify identity: `node scripts/release-version.mjs --check` must report
   all surfaces at 1.2.3.
5. Rebuild: `cmake -S native/engine -B native/engine/build && cmake --build
   native/engine/build && ctest --test-dir native/engine/build`,
   `npm ci && npm run build`, `go test ./internal/... -tags headless`.
6. Launch; the engine bootstrap/update and the Settings → Updates card now
   report live, verified download progress.

## 7. Executed automated checks (this package, before packaging)

* `ctest --test-dir native/engine/build` — **12/12 pass**, zero compiler
  warnings on a clean rebuild (gcc 14, -Wall -Wextra -Wpedantic);
  test_generate repeated 10× idle, 5× niced, 3× under CPU saturation —
  all pass.
* Race repro of the original defect: 0/1200 post-fix (was 56/200).
* `go build ./...` (headless), `go vet ./...` — clean.
* `go test ./internal/... -tags headless` + `go test ./... -run Test` —
  all 38 packages pass (incl. the real shtn-engine-host integration
  tests against the rebuilt binary and the new downloader suite,
  race-clean ×3 via `-race`).
* Stress suite — **47/47 pass, 0 hangs, 0 crashes** (45 existing + the 2
  new downloader hardening scenarios).
* Frontend — `npm run typecheck` clean, `npm run lint` 0 warnings/0
  errors, `npm run build` + `sync:web` green (embedded `web/static`
  regenerated).
* Release identity — `node scripts/release-version.mjs` reports
  package.json 1.2.3 with config.go / build/config.yml / SIGNATURE
  consistent.

## 8. Known platform limitations

* Resume requires the server to honour HTTP Range; GitHub release assets
  do. If a source answers 200 to a Range request, the manager restarts
  from zero rather than producing a corrupt hybrid file.
* Concurrency is deliberately single-stream; parallel chunked download is
  not attempted against hosts that have not proven Range support.
* The source cache is advisory (reorder-only) and expires after 24 h; it
  never ADDS sources and never overrides the trust gate.
* Engine download progress is surfaced for the llama.cpp archive; the
  native engine host (`shtn-engine-host`) is built from source, never
  downloaded.
* The Windows job of the CI workflow is unaffected by the guard change and
  was already green; the fix is verified on Linux locally (the sandbox has
  no Windows host) — the guard is platform-independent C++.
* Plain-HTTP staging against a user-hosted mirror would be refused by the
  HTTPS-only rule by design; mirrors must be HTTPS.
