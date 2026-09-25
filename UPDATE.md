# UPDATE.md — v1.6.0 Startup Maintenance Gate, Top-Level Views, Custom Tools

**Release:** `v1.6.0` (canonical application version; single version
hierarchy: package.json → release-version.mjs → config.go /
build/config.yml / SIGNATURE)
**Base:** `main @ 8f7a2b3` (`v1.5.1`) · **Date:** 2026-09-26
**Package:** `SHEYTAN-Local-Agent-v1.6.0-FINAL.zip` (complete repository
tree)

v1.6.0 repairs the v1.5.1 startup ordering defect, makes Chat and Agent
real top-level views, removes the user-facing context-window control,
ships a working custom tool system, and gives engine maintenance a
truthful lifecycle surface. All changes preserve the v1.5.1 foundation:
the same session architecture, the same downloader, the same engine
updater, the same memory/context authorities.

## IMPLEMENTED and TESTED

1. **P0 — Startup maintenance gate (the ordering repair).**
   `Server.EnsureSetup` previously prewarmed the engine and THEN started
   the scheduled updater, whose immediate pass could stop the
   freshly-booted engine mid-startup (the observed "engine ready →
   updater stops engine → downloads" sequence). v1.6.0 introduces ONE
   authoritative gate (`internal/api/maintenance.go`): the maintenance
   decision — and any transactional update it requires — completes
   BEFORE any engine process is started or prewarmed. The prewarm and
   the scheduled updater's first pass (`updater.RunScheduledAfter`) are
   released together, after, through explicit channel synchronization.
   No sleeps, no timing assumptions. Regression tests in
   `internal/api/maintenance_test.go` and
   `internal/llm/maintenance_stop_test.go` prove the ordering (they fail
   against the v1.5.1 ordering).

   Cases covered: no-update startup (prewarm after check) · required
   update (transaction completes before prewarm) · failed update with a
   valid installed engine (last-known-good survives, startup continues,
   failure surfaced) · failed update with an unusable engine (prewarm
   BLOCKED with an explicit diagnostic) · no selected model (model-first
   preserved: install-only, never a start) · repeated startup
   (single-flight gate, exactly one prewarm) · deliberate maintenance
   stop never classified as a crash · deterministic channel
   synchronization only.

   The gate exposes its explicit state at `GET /api/maintenance`
   (CHECKING → MAINTENANCE_REQUIRED → DOWNLOADING → VERIFYING →
   INSTALLING → READY_FOR_PREWARM, terminal DEFERRED / FAILED / BLOCKED)
   and the UI renders the honest phase sequence
   (`src/MaintenanceBanner.tsx`) instead of a generic "Updating…".

2. **Honest compatibility diagnostics.** "reason unrecorded" is gone:
   an unknown compatibility-fallback reason now reports the explicit
   value `unknown` (log line, `CompatInfo` API, and the pre-v1.1.7
   downgrade label). No fake specificity; accelerator honesty rules
   unchanged.

3. **P1 — Chat and Agent are top-level views.** The navigation is now
   `CHAT | AGENT | WORKSPACE/LAB | SYSTEM | SETTINGS`. The internal
   Chat|Agent segmented selector inside the Agent workspace is removed;
   the active view IS the conversation space (`#chat` / `#agent`). The
   store's per-mode session machinery is untouched: independent
   histories, per-mode active session, cross-mode history references,
   shared engine/memory/tool/research infrastructure — all preserved.

4. **P2 — Automatic long context.** The user-facing context-window
   controls are gone (the per-session context selector in the header,
   the editable context-size input and history-window % in Settings, and
   the context recommendation row). A read-only
   `LONG CONTEXT · AUTOMATIC` indicator states the real concept. The
   physical context handling is unchanged (contextplan, continuum,
   chunking, summaries, memory, histref, per-session policies resolve
   exactly as before), and historical saved values (config `numCtx`,
   session `contextTokens`) still load — they are internal legacy
   values, never user controls. Logical continuity remains unbounded
   through history, summaries, retrieval, chunking, rollover, memory and
   cross-mode references.

5. **P3 — Custom tools (end-to-end, tested).** Settings → Agent & Tools
   → My Tools → + Create Tool. Definitions persist atomically under
   `<DataDir>/custom-tools` (the existing data root), are validated
   (object-root input schema; string/number/integer/boolean/array/object
   param types; HTTPS-only URLs; bounded timeout/output), are DISABLED
   by default, and register as FIRST-CLASS tools in the ONE orchestrator
   registry — `/api/tools` carries `source: builtin|custom`, toolset
   selection picks them up when named, the agent loop executes them, and
   the result returns to the model (E2E test:
   `internal/agent/customtools_e2e_test.go`). Execution types: HTTP
   (HTTPS-only, bounded, secret values never model-visible) and local
   command (explicit permission, controlled environment, cancellable,
   bounded). Tests: persistence, validation, registry, execution,
   timeout, cancellation, output limits, disabled/invalid rejection,
   API lifecycle, real agent-loop execution.

6. **Version identity.** `v1.6.0` everywhere (package.json,
   config.AppVersion, build/config.yml, SIGNATURE regenerated). No
   codename.

## PARTIALLY IMPLEMENTED (honest scope notes)

* **AI-assisted tool builder (spec §10):** deferred — the manual tool
  builder is complete and mandatory; the AI-assisted draft generation is
  the first custom-tool UX surface to defer under time pressure, per the
  spec's own priority rule. No fake button exists for it.
* **Download Center (spec §11/§13):** the shared download experience
  rides the EXISTING Download Manager surfaces (DownloadProgressPanel
  with the truthful phase chain, measured bytes/speed/ETA, source/trust,
  retries, verification) plus the new maintenance banner. A single
  unified "Downloads Center" page aggregating every asset kind is not
  built in this release; the per-surface truthful progress model is.

## Backward compatibility

Sessions, histories, memory, settings, model selections, engine state
and context-related saved config from v1.5.1 all load without crashing
or destructive migration. Legacy sessions without a mode follow the
existing deterministic rule (agent space). The mode migration is
deterministic: the persisted workspace mode resolves against the same
per-mode memory; the view binding is additive (`#chat` is new, invalid
hashes still resolve to Agent).

---

# UPDATE.md — v1.3.7 Run Settlement, Provisioning Order & Installer Integrity

**Release:** `v1.3.7` (canonical application version; single version
hierarchy: package.json → release-version.mjs → config.go /
build/config.yml / SIGNATURE)
**Base:** `main @ 838e244` (`1.3.6`) · **Date:** 2026-09-24
**Package:** `SHEYTAN-Local-Agent-v1.3.7-FINAL.zip` (complete repository
tree)
**ROADMAP.md:** untouched by this release.

This release completes the v1.3.6 work: it root-fixes the one CI failure
of the v1.3.6 push (Actions run 35749698189 —
`TestStaleRunEventsFilteredByServer`: "idle sentinel carries no lastRun
outcome") and closes the gaps the v1.3.6 engine/provisioning work left
open.

1. **Run settlement edge (the 35749698189 fix).** A run's terminal
   VISIBILITY now implies a RECOVERABLE terminal outcome: `runLive`
   owns a settlement channel closed exactly once by
   `settleTerminal` (which `settle()` calls strictly AFTER recording
   the outcome in the bounded registry), and the WebSocket idle
   sentinel waits on that edge whenever the session's registered run is
   terminal but not yet settled. A socket that attaches between the
   orchestrator's final event and the durable settle-tail (reply
   persist → summary roll → agent.md handoff → recall index →
   continuum rollover) can never again receive a lastRun-less idle
   sentinel it cannot finalise from. No timing hacks: the edge IS the
   happens-before relationship, bounded by a 10-second safety budget so
   a wedged settle-tail degrades instead of hanging the socket.

2. **Engine provisioning order.** `ensureBinary` now runs
   managed engine → locally discovered/importable engine → offline
   gate → network download. A valid local engine on an offline machine
   is imported and used (the v1.3.6 order refused it before ever
   looking).

3. **Rediscover/Repair honesty.** A present managed binary is trusted
   only after the real preflight gate (format/arch, dependency closure,
   bounded `--version` probe) — `os.Stat` alone is no longer proof of a
   usable installation. A previously-running engine is restored after
   the operation (the v1.3.6 restart condition was inverted and left it
   stopped while reporting ok); a restart failure is an ERROR, never a
   swallowed warning.

4. **Model-architecture auto-update is transactional.** The compat
   ladder's auto-update now uses the same deferred-commit install as
   every other engine update: the ladder's verification pass commits on
   ready; ANY other exit restores the previous package byte-for-byte.
   The legacy path committed immediately and deleted last-known-good
   before the new binary had ever been launched.

5. **Installer integrity.** Companion files (the native engine host,
   package metadata) are COPIED into the new package instead of moved —
   a rolled-back update no longer destroys them. The transactional
   directory swap is refused for a `llamaBinPath` outside the SHEYTAN
   data root, so an automatic update can never delete user files that
   happen to live beside a custom engine location.

6. **Data-root residuals.** The installer broadcasts
   `WM_SETTINGCHANGE` after retiring the machine
   `SHEYTAN_DATA_DIR` (stale-environment children no longer resurrect
   the AppData root for one launch), and the historical
   `<AppRoot>\logs` directory folds into `<AppRoot>\data\logs` even
   though the log catcher has already created the canonical directory
   (verified merge, live `app.log` never overwritten, restart-safe).

7. **Net Search server-side contract.** The explicit `netSearch`
   request intent is now pinned by Go tests end-to-end (policy
   authorization under every tool mode, offered-surface guarantee, and
   the exactly-one-tool boundary). Stale Research remnants were removed
   (LogViewer filter label, shortcuts help text, dead client exports,
   dead `Config.ResearchDir`); the `research*` config keys keep their
   documented backward compatibility.

Every fix carries a regression test that fails on the corresponding
v1.3.6 defect. The ownership/adoption machinery (englease, proc
identity, adoption proof) was audited and verified already enforced
everywhere — it is unchanged.

---

# UPDATE.md — v1.3.6 Engine Lifecycle, System Discovery, Net Search & Canonical Data Root

**Release:** `v1.3.6` (canonical application version; single version
hierarchy: package.json → release-version.mjs → config.go /
build/config.yml / SIGNATURE)
**Base:** `main @ 57f0c1b` (`1.3.5`) · **Date:** 2026-09-22
**Package:** `SHEYTAN-Local-Agent-v1.3.6-UPDATE.zip`
**ROADMAP.md:** untouched by this release (blob SHA verified unchanged:
`c7e2c1720eb5e97bd932c0d76100b8719193e650`).

This release root-fixes the engine ownership defect class reported in
the 2026-09-22 runtime evidence: engine startup raced the engine
updater, `0xC0000139` (STATUS_ENTRYPOINT_NOT_FOUND) was retried through
the compatibility ladder as if it were an argument problem, a process
answering `/health` on port 8080 could be adopted without any identity
proof, and the installer's hidden `SHEYTAN_DATA_DIR` environment
variable kept a second data root alive in AppData.

1. **One provisioning authority.** `internal/updater.InstallStaged` is
   now the only path that puts a llama.cpp engine into the managed bin
   directory: staged download → extraction → static candidate
   validation → atomic directory swap (old dir renamed aside, staging
   renamed in, non-engine files merged back) → installed-binary
   identity verification (SHA-256) → commit (tag + `engine-install.json`
   manifest) → cleanup. Any failure before the commit restores the
   previous engine directory; last-known-good is never destroyed and
   obsolete staging archives are removed. The in-place `copyAll`
   overwrite of a live bin directory is gone.

2. **Engine-owned transactional updates.** `LlamaServer.UpdateEngineNow`
   wraps the whole update in an EXCLUSIVE lifecycle operation: it holds
   `switchMu` start-to-finish (Start/Restart/prewarm can never
   interleave), stops the engine itself, installs through the single
   authority, restarts the exact installed binary and verifies
   readiness. `updater.UpdateEngineWithProgress` delegates to it
   (`TransactionalEngine`), so the scheduled updater and the manual
   updater both take the same engine-owned path.

3. **Identity-proven adoption (no blind port adoption).**
   `internal/proc.ListeningProcess` attributes a listening TCP port to
   a PID through real OS APIs (GetExtendedTcpTable +
   QueryFullProcessImageName on Windows; /proc/net/tcp +
   /proc/<pid>/exe on Linux — never cmd.exe/PowerShell/tasklist
   parsing). Adoption of a port-serving process now REQUIRES that its
   executable equals the managed engine path; a foreign process is
   refused with recorded evidence (PID + executable), never killed
   automatically.

4. **`0xC0000139` is a first-class diagnostic.** Loader-class Windows
   exit codes (0xC0000135/0xC0000139/0xC000007B/0xC0000142/… and Win32
   5/126/127/193) decode into actionable reports (class, summary,
   advice, binary identity, dependency evidence); when the OS exit code
   is truncated (Unix keeps 8 bits), the loader's own NTSTATUS text in
   the captured stderr is classified. A new ENGINE PREFLIGHT
   (`internal/llm/preflight.go`: exists → architecture → dependency
   closure via debug/pe → bounded `--version` probe, sharing the
   production subprocess runner) catches these failures BEFORE any
   model launch.

5. **No retry storm on deterministic failures.** Loader-class startup
   failures break the compatibility ladder immediately — the ladder
   remains only for genuine llama.cpp argument-layout problems (the
   bounded repair behavior is unchanged and still tested).

6. **System Engine Discovery.** `internal/engdiscovery` searches for an
   existing compatible llama.cpp / shtn-engine-host package before any
   download: Tier 0 (managed dir + persisted discovery cache),
   Tier 1 (PATH, exe dir, sibling bins, common user locations),
   Tier 2 (bounded parallel full-system scan — background only, never
   on the startup path). Validation is non-destructive: static checks
   first (format + architecture + PE import closure), a bounded
   `--version` probe last. Validated candidates are imported into the
   managed directory with byte-identical dedupe (SHA-256) and
   provenance recorded.

7. **Canonical data root + AppData migration.** The NSIS installer no
   longer writes the machine `SHEYTAN_DATA_DIR` env var (it DELETES it,
   retiring the 1.3.5 AppData contract), creates the install-local
   `<AppRoot>\data` tree and grants the built-in Users group modify
   rights (icacls, inheritable) so mutable data stays writable under a
   Program Files install. The runtime migrates a 1.3.5-era
   `%LOCALAPPDATA%\SHEYTAN-LA` root into the canonical root once —
   hash-verified, idempotent, restart-safe, engine bundle as a unit
   (two llama.cpp builds are never mixed), legacy root removed only
   after full verification, and an explicit user override is never
   touched.

8. **Net Search (Research tab removed, backend reused).** The Research
   workspace layer/tab/panel is removed with no dead route (an old
   `#research` hash resolves to Agent). The SAME research service now
   backs a per-request Net Search control in BOTH Chat and Agent
   composers (`Thinking · Tools · Net Search`), enforced server-side
   via `agent.WithNetSearch` (the research tool is authorized for that
   request only, recorded in telemetry) and reachable directly at
   `/api/net-search` (same handler as the `/api/research` shim — one
   implementation). Settings labels read "Net Search"; persisted JSON
   keys are unchanged.

9. **Visible version fixed.** The `appVersion ?? "v1.2.2"` fallback in
   `src/App.tsx` is replaced by a build-time canonical constant
   (`__APP_VERSION__` injected by Vite from package.json — the single
   release source); the backend's runtime version stays authoritative
   once loaded. A regression suite (`version-contract.test.ts`) blocks
   any stale version literal from returning to shipped source.

10. **Engine diagnostics surface.** `/api/engine` now carries a
    `diagnostics` block (binary path, recorded tag + probed version,
    decoded failure class, restart count, recent stdout/stderr, the
    full failure report and the last preflight evidence) so the UI can
    render the REAL engine state (Ready/Starting/Updating/Failed with
    the exact reason) instead of a generic spinner.

11. **Release identity.** Canonical `1.3.6` synced through
    package.json → `internal/config/config.go` (`AppVersion`),
    `build/config.yml` (`productVersion`), `SIGNATURE`. Version-only
    identity: tag `v1.3.6`, release title `1.3.6`, no codename, no
    suffix, no `APP_VERSION_FULL`.

## Verification status (truthful)

Verified locally on Linux x86-64 (Go 1.27.1, Node 24):

- `gofmt` clean (one pre-existing drift in `internal/research/service.go`
  also corrected); `go vet -tags headless ./internal/... ./cmd/...` clean.
- `go test ./internal/... -tags headless -count=1` — 51 packages ok,
  including the new suites: engine lifecycle/adoption/classification/
  transactional-update/race (`internal/llm`), installer transaction and
  rollback (`internal/updater`), process identity (`internal/proc`),
  static analysis (`internal/engcheck`), discovery
  (`internal/engdiscovery`), migration + path invariants
  (`internal/config`).
- Frontend: `npm ci`, `typecheck`, `lint` (0 warnings), `test:units`
  (96/96, including the new workspace/net-search/version contracts),
  `test:release` (28/28), `build`, `verify:web`; the stale `v1.2.2`
  literal is gone from the rebuilt `web/static` bundle.

NOT verified in this environment (remains CI / Windows-machine owned):

- Windows builds, Windows NSIS installer execution, Windows native C++
  CTest, Windows executable smoke tests, and the real-Windows runtime
  acceptance of §45.
- GitHub Actions runs observed from THIS environment, the
  v1.3.6 tag, release publication, and published asset hashes.
- A real llama.cpp engine boot on user hardware.

---

# UPDATE.md — v1.3.5 Windows CTest Completion / Native Test Hardening (historical)


**Release:** `v1.3.5` (canonical application version; single version
hierarchy: package.json → release-version.mjs → config.go /
build/config.yml / SIGNATURE)
**Base:** `main @ a339b31` (`v1.3.4`) · **Date:** 2026-09-22
**Package:** `SHEYTAN-Local-Agent-v1.3.5-UPDATE.zip`
**ROADMAP.md:** untouched by this release (no roadmap feature is
shipped; the v1.3.4 Repository Intelligence slice is stabilized).

This release root-fixes the Windows CI completion path (Actions run
`35583009466`): the Windows runner configured CMake, built the
complete native engine, built all 12 test executables, and started
CTest — `10 PASS / 2 FAIL` (`tokenizer`, `generate`). Both failures
shared one root cause (POSIX-only temporary-path assumptions in the
native tests), and the deep audit that followed found and fixed two
further genuine portability defects plus one build-system defect.
No assertion was weakened; no test was skipped, disabled, or made
non-fatal.

1. **Root cause of the two Windows failures — POSIX temp-path
   assumptions.** `test_tokenizer.cpp` resolved its synthetic GGUF
   location with `getenv("TMPDIR")` falling back to the literal
   `/tmp`; `TMPDIR` does not exist on Windows, `/tmp` resolves to
   `<current-drive>:\tmp` (absent on the runner), the synthetic model
   never loaded, and the remaining 37 assertions cascaded.
   `test_generate.cpp` hard-coded `/tmp/shtn-not-llama.gguf`, so
   `fopen()` returned `nullptr` and the malformed-model block was
   skipped. Both tests now build their fixtures under a shared
   cross-platform helper (below).

2. **One reusable native-test helper — `tests/temp_dir.h` (new).**
   A single RAII `shtn_test::TempDir` for the whole native suite:
   resolves the platform temp root through
   `std::filesystem::temp_directory_path()` (GetTempPath on Windows;
   TMPDIR/TMP/TEMP then the platform default on POSIX — never a
   hard-coded path in callers), generates collision-free directories
   (process id + in-process counter + monotonic clock + random
   suffix), returns native paths that round-trip through stdio
   (spaces fully supported), and removes its whole tree on
   destruction. No `tmpnam`, no `TMPDIR`-must-exist assumption, no
   per-test `#ifdef` copies. The three previously-duplicated private
   copies (`test_gguf`, `test_model`, `test_host`) are consolidated
   onto it; `test_engine`'s `/definitely/not/here.gguf`
   nonexistent-path literal is now derived from the helper too.

3. **Cross-platform regression contracts (new test sections, same
   12 binaries).** `test_gguf` gains a portable temp-directory
   contract (create / write / read / close / remove, a path with
   spaces, uniqueness under repeated creation, RAII cleanup) and a
   mapped-file release contract (a mapped+closed file must be
   deletable). `test_model` gains an engine-level contract: after
   load + unload the model file must be deletable, and the engine
   must remain reusable.

4. **Windows engine defect fixed — `MappedFile` handle inversion
   (`native/engine/src/gguf.cpp`).** The Windows `open()` stored the
   file handle in `handle_` and the section handle in `map_`; the
   Windows `close()` then called `UnmapViewOfFile(section)` — which
   requires the VIEW BASE address — so the unmap silently failed,
   the section object leaked, and the still-mapped view pinned the
   `.gguf` file for the life of the process. On Windows this meant:
   every model unload leaked a section + view, and a model file
   could not be deleted or replaced after unload while the host
   process lived (a user-facing file-lock defect), and test temp
   cleanup could never remove a loaded model's file. `open()` now
   closes the file handle once the view exists (the section holds
   its own reference) and stores `handle_` = section, `map_` = view
   base, matching the class contract and the POSIX branch; `close()`
   unmaps the view and closes the section.

5. **`test_host` cwd-dependent fixture path fixed.** The Phase 5
   streaming tests resolved fixtures via
   `getenv("SHTN_FIXTURES_DIR")` with a `"../tests/fixtures"`
   fallback — a cwd-relative path that only worked under CTest
   (whose default working directory is the build dir). The test now
   uses the configure-time `SHTN_FIXTURES_DIR` macro (which CMake
   already defined for this target — it was dead configuration),
   exactly like `test_forward` and `test_generate`.

6. **`native/engine/Makefile` — `test_host` rule repaired.** The
   plain-make path never defined `SHTN_FIXTURES_DIR` (relying on the
   broken cwd fallback, so `make test` could never pass `test_host`
   from any working directory). The rule now bakes
   `-DSHTN_FIXTURES_DIR='"$(CURDIR)/tests/fixtures"'` like the
   forward/generate rules; `make -C native/engine test` passes 12/12.

7. **CI regression gate (new).** The audit job gains a "Native
   temp-path portability gate": `git grep` fails the build if any
   native source or test outside `tests/temp_dir.h` reintroduces a
   `"/tmp` literal or a direct `getenv("TMPDIR")` call.

8. **Release identity.** Canonical `1.3.5` synced through
   package.json → `internal/config/config.go` (`AppVersion`),
   `build/config.yml` (`productVersion`), `SIGNATURE`. Version-only
   identity: tag `v1.3.5`, release title `1.3.5`, no codename, no
   `-Z`, no suffix, no `APP_VERSION_FULL`. The codename gate returns
   zero matches.

## Verification (this release, local matrix)

- Native Linux: fresh `cmake --fresh` configure + build +
  `ctest --output-on-failure` → **12/12**; parallel `ctest -j4` →
  12/12; zero temp leftovers; `make -C native/engine test` → 12/12;
  every test binary also passes when executed from a foreign cwd.
- Native Windows: full cross-compile of the engine, host and all 12
  test binaries (clang 23 / llvm-mingw, x86_64-w64-windows-gnu,
  fully static) executed under Wine 11.18 (WoW64) → **12/12**,
  zero temp leftovers (the pre-fix v1.3.4 binaries reproduce both
  original failures plus the mapped-file lock in the same harness).
- Go: `gofmt` clean; `go vet -tags headless ./internal/... ./cmd/...`
  clean; race gate
  (`api, agent, sessions, contextplan, histref, runtime`) PASS;
  `go test ./internal/... -tags headless -count=1` → 47/47 packages
  ok, including `internal/repoindex` (v1.3.4 Repository Intelligence)
  and `internal/native/engine` (real-host Go↔C++ integration);
  stress suite 47/47.
- Frontend: `npm ci`, `typecheck`, `lint`, `test:units` (84/84),
  `test:release` (28/28), `build`, `verify:web --dist` all pass;
  `web/static` is byte-identical to the committed v1.3.4 build (no
  frontend source changed, so no regenerated assets ship).
- Release metadata: `release-version.mjs --check` consistent;
  codename gate zero matches; `APP_VERSION=1.3.5`.

No file was deleted by this release. The update ZIP contains only
the files listed in `REPLACEMENT-MANIFEST.txt`.
