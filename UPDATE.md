# UPDATE.md — v1.2.4 SHEYTAN-LA Data-Path Speed, Coordinated Memory Policy, Real Workspace & Usability Package

**Release:** `v1.2.4` (codename Zeta) · **Base:** `main @ 8d8858d` (`v1.2.3`)
**Date:** 2026-09-16
**Package:** `SHEYTAN-Local-Agent-v1.2.4-UPDATE.zip`

This package makes SHEYTAN process data faster, reclaim memory under an
explicit coordinated policy instead of ad-hoc self-bounding, turn the
Workspace into a genuine work environment, and apply a whole-app usability
pass — all without a major rewrite, and every optimization tied to a
measured baseline taken BEFORE the change. No second runtime is introduced;
the language split is unchanged (TypeScript for UI state, Go for
orchestration/filesystem/caches, C++ untouched — its mmap zero-copy hot
paths were already correct and no measured bottleneck justified moving
data across the Go/C++ boundary).

The version moves `1.2.3` → `1.2.4` through the established identity chain
(package.json → release-version.mjs → internal/config/config.go,
build/config.yml, SIGNATURE — `--check` is green).

## 0. The measured baseline (what this package fixes first)

Everything below was benchmarked on the identical machine before any edit
(`-benchmem`, steady-state fixtures; numbers are that sandbox, relative
gains are what matter):

| Hot path | v1.2.3 baseline | Root cause |
|---|---|---|
| `ctxtelemetry.Record` (2000 records) | **7.43 ms/op, 6.49 MB/op, 18685 allocs** | whole JSONL file re-read, re-marshalled and rewritten on EVERY turn |
| `ctxtelemetry.Recent` | 3.31 ms/op, 4.1 MB/op, 12032 allocs | same, on the UI read path |
| `sessions.AppendMessage` (400-msg session) | 2.54 ms/op, 1.21 MB/op, 3287 allocs | full session file re-read + unmarshal + marshal + rewrite per message |
| `sessions.Get` | 426 µs/op, 274 KB/op | same re-read on every load |
| `aicontext.SystemMessageWithTools` | 31.3 µs/op, 100 KB/op | AI-CONTEXT.md re-read from disk every turn |
| `projectintel.Card` | 8.9 µs/op | per-project facts re-read + re-parsed every turn |
| `chunking.FormatFileAttachment` (1 MB file) | 746 µs/op, **2.93 MB/op** | read + full string copy + windowed concat ≈ 3× file size live |
| `tools.Shell/CodeExec/Git` | **unbounded** | `CombinedOutput()` buffered an agent-triggered command's entire output in RAM — the only unbounded allocation in the app |
| `llm.Client` image cache | ≤ 8 × 6 MB base64 ≈ **48 MB** worst case | count-only bound, no byte bound |
| `memory` remember path | unbounded file growth | repeated identical facts appended forever |

Frontend profiling (existing perf-HUD + store inspection) additionally
showed: a fresh `engine` object written every 2.5 s re-rendering
AgentBody/AgentHeader/ModelPicker even when nothing changed, LabPanel
subscribing to the ENTIRE store, four non-memoized panels under AgentBody,
and index-shifting message keys causing memoized-bubble remounts.

## 1. Smarter internal data pipeline

All fixes are in the measured hot paths; behavior is preserved
byte-for-byte on the wire and on disk.

- **ctxtelemetry** (`internal/ctxtelemetry/ctxtelemetry.go`): the record
  set is cached in memory and read served from it; the file is rewritten
  only when a coalescing window closes (64 records or 10 s) or when
  `Flush()` is called at run boundaries/shutdown. On-disk format is
  unchanged; compaction-to-half semantics unchanged; a hard crash loses at
  most one coalescing window of OBSERVATIONAL telemetry (never user
  content). New `Stats()` exposes records/flushes/coalesced/dirty-max/
  last-flush duration.
- **sessions** (`internal/sessions/sessions.go`): a bounded hot cache
  (8 fully-loaded sessions, validated by (size, mtime) on every hit so
  external edits are still detected) removes the re-read from appends and
  repeat loads; `Get` returns a read-isolated copy so out-of-lock API
  mutations can never tear the cached state; the activity sidecar is
  cached the same way; session files are now STREAMED into the tmp file
  through a buffered `json.Encoder` (no full-JSON blob in memory per
  save); LRU eviction via `TrimHot(keep)`.
- **aicontext** (`internal/aicontext/aicontext.go`): (size, mtime)-
  validated text cache — one stat per turn, zero re-reads.
- **projectintel** (`internal/projectintel/projectintel.go`): validated
  parse cache per project root.
- **chunking** (`internal/chunking/chunking.go`): `WindowHeadTailBytes`
  windows the raw bytes directly (75/25 head/tail at line boundaries,
  explicit elision marker — identical semantics, pinned by a differential
  test against the string path). The composed attachment block now holds
  ~budget bytes, not ~3× file size.
- **tools** (`internal/tools/capture.go`, `tools.go`): bounded streaming
  capture for Shell/CodeExec/Git — stdout+stderr drain continuously (the
  child never blocks on a full pipe), the first 1 MiB is retained, and
  when output is cut an honest marker states the TRUE total plus how to
  get the rest. Aggregate counters (bytes produced / retained /
  truncations) make the saving measurable via `/api/perf`.
- **memory** (`internal/memory/memory.go`): the agent `remember` path
  suppresses EXACT duplicates (same class, same normalized content,
  case-insensitive) and tells the model "already known"; `AppendEntry`
  semantics are unchanged for every other caller; suppressed duplicates
  are counted. The FIRST copy of a fact stays authoritative.
- **llm** (`internal/llm/client.go`): image data-URL cache bounded by
  TOTAL BYTES (24 MiB) with oldest-first eviction — worst case drops from
  ~48 MB of retained base64 to the cap.
- **contextcache** (`internal/contextcache/contextcache.go`): new
  `TrimIdle(keep)` sheds the least-recently-used cold tail under
  coordinated cleanup; the existing LRU/byte/TTL bounds stay in force at
  all times (trimming is pressure relief, never correctness).

Context pipeline requirements are already structural in this codebase and
were preserved, not regressed: bounded chunks, priority-aware retention
(contextplan degradation ladder), evidence-first model, no silent drops —
the guaranteed-fit refusal gate and the untouchable current user turn are
exactly where they were.

## 2. Coordinated memory policy (new: internal/memmanager)

One owner for reclamation instead of scattered self-bounding:

- **Registered trims**: `sessions-hot` (TrimHot(1) — the most recent
  session stays warm), `image-cache` (client trim), `ctxtelemetry` (flush
  at boundaries). Components keep their own bounds; the manager only
  sheds the cold tail.
- **Run tracking**: `TrackRunStart`/`TrackRunEnd` around every agent run
  (the run's defer is cancellation-aware, so aborts release exactly like
  completions). Post-run cleanup releases one-shot state while it is safe.
- **Pressure**: above a 1 GiB heap watermark a cleanup (GC + trims) is
  triggered — but ONLY when no run is active. Active generation, current
  workspace, user-visible data and hot caches are never sacrificed for
  bytes. Nothing user-owned (sessions, memory entries, attachments) is
  ever a trim target.
- **Idle maintenance**: a 2-minute loop performs housekeeping only while
  the app is idle (started by `EnsureSetup`, stopped by `Stack.Close`).
- **Telemetry**: heap before/after last cleanup, bytes freed per trim and
  cumulative, cleanup duration, pressure count, plus the data-path
  counters (contextcache hit ratio, tool-capture bytes produced/retained/
  truncations, ctxtelemetry flush stats, memory dedup counter) — surfaced
  on `/api/perf` under `memory`.
- **session-aware isolation**: one shared session store now lives on the
  runtime Stack (the API layer previously constructed its own over the
  same directory); one hot cache to bound, one owner to trim.

## 3. Change map (authoritative)

```text
package.json                                         MODIFY  version 1.2.4 (identity chain source of truth)
SIGNATURE                                            MODIFY  version sync via release-version.mjs
build/config.yml                                     MODIFY  version sync via release-version.mjs
internal/config/config.go                            MODIFY  WorkspaceRoot + RecentWorkspaces + EffectiveWorkspaceRoot + PushRecentWorkspace
internal/ctxtelemetry/ctxtelemetry.go                MODIFY  in-memory cache + coalesced persistence + Stats/Flush
internal/sessions/sessions.go                        MODIFY  hot cache, read-isolated Get, streamed writes, TrimHot, sidecar cache
internal/aicontext/aicontext.go                      MODIFY  (size,mtime) text cache + ResetFileCache
internal/projectintel/projectintel.go                MODIFY  validated parse cache for facts
internal/chunking/chunking.go                        MODIFY  WindowHeadTailBytes + byte-windowed FormatFileAttachment
internal/tools/tools.go                              MODIFY  Shell/CodeExec/Git switched to boundedCombinedOutput
internal/tools/capture.go                            ADD     bounded streaming output capture + telemetry
internal/memory/memory.go                            MODIFY  AppendEntryUnique + remember-path dedup + DuplicatesSkipped
internal/llm/client.go                               MODIFY  byte-bounded LRU image cache + TrimImageCache
internal/contextcache/contextcache.go                MODIFY  TrimIdle cold-tail shedding
internal/memmanager/memmanager.go                    ADD     coordinated memory policy (trims, runs, pressure, idle, telemetry)
internal/memmanager/memmanager_test.go               ADD     policy + repeated-run bounded-memory tests (race-clean)
internal/runtime/runtime.go                          MODIFY  Stack.Sessions/MemMgr/Intel, trim registration, shared store, StartMemoryManager, Close flush
internal/api/server.go                               MODIFY  shared session store, run tracking in handleRun, workspace routes, idle-loop start
internal/api/perf.go                                 MODIFY  memory telemetry block on /api/perf
internal/api/workspace.go                            ADD     GET summary / reveal / switch endpoints
internal/api/workspace_test.go                       ADD     workspace endpoint contract tests
internal/aicontext/bench_test.go                     ADD     per-turn system-message benchmark
internal/chunking/format_bench_test.go               ADD     1 MB attachment compose benchmark
internal/chunking/window_bytes_test.go               ADD     byte-path differential + non-aliasing + large-file tests
internal/ctxtelemetry/bench_test.go                  ADD     steady-state record/read benchmarks
internal/projectintel/bench_test.go                  ADD     per-turn card benchmark
internal/sessions/bench_test.go                      ADD     append/get hot-path benchmarks
src/workspace.ts                                     MODIFY  workspace layer, Agent relabel, view persistence
src/api.ts                                           MODIFY  workspace types + endpoints
src/WorkspacePanel.tsx                               ADD     work-environment panel
src/shortcuts.ts                                     ADD     global shortcut registry + help data
src/App.tsx                                          MODIFY  workspace route, view restore, shortcuts, help overlay
src/store.ts                                         MODIFY  engine snapshot change-detection (re-render churn)
src/LabPanel.tsx                                     MODIFY  explicit store selectors
src/MessageStream.tsx                                MODIFY  memo export + stable message keys
src/ModelPicker.tsx                                  MODIFY  memo export
src/PerfStrip.tsx                                    MODIFY  memo export
src/ActivityStream.tsx                               MODIFY  memo export
src/styles.css                                       MODIFY  additive v1.2.4 section (workspace panel, overlay, skeletons)
web/static/index.html                                MODIFY  rebuilt frontend bundle references
web/static/.vite/manifest.json                       MODIFY  rebuilt frontend manifest
web/static/assets/AgentBody-cMt13HM9.js              NEW     rebuilt bundle (hashed)
web/static/assets/AgentHeader-dCYmm522.js            NEW     rebuilt bundle (hashed)
web/static/assets/AgentSidebar-CWp3SdAx.js           NEW     rebuilt bundle (hashed)
web/static/assets/DownloadProgress-28j0D3i-.js       NEW     rebuilt bundle (hashed)
web/static/assets/LabPanel-DFJX1ZVA.js               NEW     rebuilt bundle (hashed)
web/static/assets/ResearchPanel-BXiEx0Sd.js          NEW     rebuilt bundle (hashed)
web/static/assets/SettingsPanel-BdbLpW4s.js          NEW     rebuilt bundle (hashed)
web/static/assets/SystemPanel-BEI2b4hF.js            NEW     rebuilt bundle (hashed)
web/static/assets/WorkspacePanel-BV8_VjyW.js         NEW     new lazy panel bundle
web/static/assets/index-B_uB5cBU.js                  NEW     rebuilt entry bundle
web/static/assets/index-gFgARQCb.css                 NEW     rebuilt stylesheet
worklog.md                                           MODIFY  this release's work-log entry
UPDATE.md                                            REPLACE this file
REPLACEMENT-MANIFEST.txt                             REPLACE this package's manifest
REPLACEMENT-SHA256.txt                               REPLACE this package's hashes
```

## 4. DELETE list (apply after copying)

Stale v1.2.3 hashed assets, replaced by the rebuilt bundles above:

```text
web/static/assets/AgentBody-BjrGu1AP.js
web/static/assets/AgentHeader-Du6Ox9Mq.js
web/static/assets/AgentSidebar-DvyhjsPE.js
web/static/assets/DownloadProgress-CO-h6FaB.js
web/static/assets/LabPanel-BaaTabl2.js
web/static/assets/ResearchPanel-MRM_MVdu.js
web/static/assets/SettingsPanel-DALRLl1e.js
web/static/assets/SystemPanel-CAyGQpds.js
web/static/assets/index-CjGSRCFB.js
web/static/assets/index-DwewjhlI.css
```

## 5. DO NOT TOUCH list

User data and runtime state are never touched by the update machinery or
by the new memory policy: `models/`, `workspace/`, `sessions/`,
`attachments/`, `memory.jsonl`, `config.json` (except the additive
`workspaceRoot`/`recentWorkspaces` keys written by an explicit switch),
`ctxtelemetry.jsonl`, `scheduler/`, `lab/`. The memmanager's trims
deliberately exclude all of these; telemetry JSONL is only flushed, never
truncated by policy.

## 6. Safe-apply procedure

1. Apply over a clean checkout of the base commit (`8d8858d`, v1.2.3).
2. Copy every file from this package over the tree (REPLACE/MODIFY/ADD).
3. Delete the v1.2.3 hashed assets listed in §4 if present.
4. Verify identity: `node scripts/release-version.mjs --check` must report
   all surfaces at 1.2.4.
5. Rebuild: `cmake -S native/engine -B native/engine/build && cmake
   --build native/engine/build && ctest --test-dir native/engine/build`,
   `npm ci && npm run build`, `go test ./internal/... -tags headless`.
6. Launch; the new Workspace layer (sidebar "Workspace") shows the current
   project, recent files, active session, model/runtime state and quick
   actions; pressing `?` lists the keyboard shortcuts; Settings →
   Performance carries the new `memory` telemetry block.

## 7. Executed automated checks (this package, before packaging)

Measured improvements (identical machine, `-benchmem`, before vs after):

| Benchmark | v1.2.3 | v1.2.4 | Gain |
|---|---|---|---|
| ctxtelemetry.RecordSteady | 7430968 ns/op, 6492054 B/op, 18685 allocs | 64681 ns/op, 59462 B/op, 144 allocs | **115× faster · 109× less memory** |
| ctxtelemetry.RecentSteady | 3312198 ns/op, 4099584 B/op, 12032 allocs | 4253 ns/op, 18432 B/op, 1 alloc | **778× faster · 223× less memory** |
| sessions.AppendMessageGrowing | 2535120 ns/op, 1209299 B/op, 3287 allocs | 1255804 ns/op, 339100 B/op, 1475 allocs | **2.0× faster · 3.6× less memory** |
| sessions.GetLoaded | 425778 ns/op, 274176 B/op, 830 allocs | 26356 ns/op, 75200 B/op, 15 allocs | **16× faster · 3.6× less memory** |
| aicontext.SystemMessageSteady | 31320 ns/op, 100443 B/op | 9812 ns/op, 34796 B/op | 3.2× faster · 2.9× less memory |
| projectintel.CardSteady | 8894 ns/op, 2472 B/op | 2166 ns/op, 1272 B/op | 4.1× faster |
| chunking.FormatFileAttachment1MB | 746256 ns/op, 2926039 B/op | 678094 ns/op, 1869102 B/op | 1.56× less memory (read buffer is now the floor) |

Tool-output safety: the unbounded `CombinedOutput` path is gone; a
runaway command's result is capped at 1 MiB retained with an honest
total-count marker (covered by the capture counters on `/api/perf`).

Correctness suites (all green after the changes):

- `go vet ./...` clean; `go test ./internal/... -tags headless -count=1`
  fully green; `go test ./... -run Test` green (the Wails desktop shell
  package requires GTK/WebKit system libraries that CI provides).
- Race detector clean on the five touched-concurrency packages
  (memmanager, sessions, ctxtelemetry, memory, contextcache).
- Stress suite: `STRESS-RESULT pass=47 fail=0 hangs=0 crashes=0`
  (includes the contextcache bound proof and the 10k-message plan
  bound).
- New policy test: 200 simulated run cycles with a would-be-leaking cache
  keep steady-state heap bounded (`TestRepeatedRunMemoryStaysBounded`);
  cleanup deferral while a run is active is asserted directly.
- Frontend: `npm run typecheck`, `npm run lint` (0/0), `npm run
  test:units` (20/20), `npm run build` + `sync:web` green.
- Native engine: NO C++ changes; rebuilt and 12/12 CTest suites pass;
  Go↔native integration tests pass against the real host binary.

## 8. Known platform limitations

- The GUI (Wails/GTK) shell cannot be exercised on a headless CI runner
  without GTK4/WebKitGTK system libraries; all logic is covered through
  the `headless` tag suite, which is what CI runs.
- Opening a terminal at the workspace (`/api/workspace/reveal` with
  `target: "terminal"`) is best-effort on Linux (first of the common
  terminal emulators found) and returns an honest error when none exists.
- Telemetry flush coalescing (64 records / 10 s) means a hard crash can
  lose at most one window of observational context telemetry; user data
  is never coalesced.
