# SHEYTAN-Local-Agent v1.3.6 — FINAL FACTUAL REPORT

This report follows the mandated section-50 structure. Every claim in
the "Verification" section corresponds to an actually executed check in
the producing environment. Anything that was NOT executed there is
listed as such — the words "fixed", "complete", "release-ready" and
"verified" are used only where evidence exists.

## Repository state

* Base commit: `57f0c1bc106dae121e0c333892dfd3c9306b7ea7` (`1.3.5`)
* Final commit: `b98b33653599d7f78eeb6f575cf42b3454c4364f` (branch
  `v1.3.6-engine-lifecycle`, produced locally; NOT pushed)
* Version: `1.3.6` (package.json → release-version.mjs →
  `internal/config/config.go` AppVersion, `build/config.yml`
  productVersion, `SIGNATURE`; `--check` green)
* Branch: `v1.3.6-engine-lifecycle` (local only)
* Actions run(s): NONE from this environment. The green baseline run
  `35669251105` was inspected via the spec; no new run was triggered
  because no push was performed.

## Root causes

Only actually discovered causes (confirmed by code inspection and, where
possible, deterministic tests):

1. **Two competing engine provisioning paths.**
   `internal/llm/llama.go` owned `ensureBinary` →
   `downloadEngineArchive` while `internal/updater/updater.go` owned
   `UpdateEngineWithProgress` → `downloadEngine`, which overwrote the
   live bin directory in place (`copyAll`) after a 700 ms sleep. No
   lock existed across the two paths; the scheduled updater ran with a
   live `LlamaServer` while boot-path auto-update ran inside
   `startLocked`. This is the structural race behind the observed
   startup/update overlap.
2. **Blind port adoption.** `adoptExisting` adopted ANY process
   answering `/health` with HTTP 200 on port 8080 — no PID, no
   executable-path check — so a foreign llama.cpp or a stale
   installation could be connected to and labeled as the managed
   engine (consistent with the observed b10642 → b11090 → b10642
   version flap).
3. **Loader failures treated as argument failures.** `explainExit`
   decoded only `0xC0000135`; `0xC0000139` (STATUS_ENTRYPOINT_NOT_FOUND
   — the reported runtime failure) fell through to the generic
   "exited during startup" error and the compatibility ladder retried
   the SAME broken binary up to four times.
4. **Installer-imposed second data root.** `packaging/nsis/installer.nsi`
   wrote the machine environment variable
   `SHEYTAN_DATA_DIR=%LOCALAPPDATA%\SHEYTAN-LA`, forcing installed
   copies to keep models/logs/sessions in AppData while the runtime
   contract resolves the canonical root to `<AppRoot>\data`.
5. **Stale visible version.** `src/App.tsx` rendered
   `appVersion ?? "v1.2.2"` — a hard-coded stale literal shown before
   `/api/state` resolved (or on failure).

## Engine

* Lifecycle architecture: ONE lifecycle owner (`LlamaServer`) with the
  existing `switchMu`/watchdog/generation-token architecture preserved;
  a NEW explicit `updating` state (`StateUpdating`) covers maintenance;
  `UpdateEngineNow` holds `switchMu` for the whole update transaction
  (stop → install → restart → verify), so Start/Start, Start/Update,
  Update/Start, prewarm/first-request and watchdog/updater overlaps are
  structurally serialized.
* Ownership/adoption: adoption now requires the OS-attributed listening
  PID's executable to equal the managed engine path
  (`proc.ListeningProcess` + `proc.SameExecutable`; Windows
  GetExtendedTcpTable/QueryFullProcessImageName, Linux
  /proc/net/tcp + /proc/<pid>/exe — no shell parsing). Foreign or
  unprovable processes are refused with recorded evidence and are never
  killed automatically.
* Provisioning/discovery: ONE authority (`updater.InstallStaged`) —
  staged download → extraction → static candidate validation
  (`engcheck.StaticValidate` + PE import closure) → atomic directory
  swap (previous dir renamed aside, staging renamed in, non-engine
  files merged back) → installed-binary SHA-256 verification → commit
  (tag + `engine-install.json`) → cleanup. `ensureBinary` tiers:
  managed binary → discovery import → download via the same authority.
* Failure classification: loader-class exit codes
  (0xC0000135/0xC0000139/0xC000007B/0xC0000142/0xC0000005/0xC000001D/
  0xC0000409, Win32 5/126/127/0xC1) decode into actionable diagnostics;
  a textual NTSTATUS fallback covers Unix's 8-bit exit truncation. The
  preflight (exists → arch → dependency closure via `debug/pe` →
  bounded `--version` through the production subprocess runner) runs
  BEFORE any model launch; preflight loader failures stop the boot with
  zero model launches.
* Logging: per-attempt stdout (500-line ring) and stderr (64-line ring)
  preserved; failure reports (`EngineFailureReport`) capture phase,
  attempt ID, generation, executable identity (path/SHA-256/size/mtime),
  expected tag, probed version, OS/arch, PID, exit code (hex included),
  decoded class, dependency evidence, stdout/stderr tails, model path,
  compat level and attempt context. Surfaced via `/api/engine`
  `diagnostics`.
* Update synchronization: scheduled updater and manual updater both
  route through `TransactionalEngine.UpdateEngineNow` (engine-owned)
  and a process-wide install lock guards every provisioning operation.
* Rollback: any failure before commit restores the previous engine
  directory; last-known-good is preserved and restarted on failure
  (tested: `TestUpdateEngineFailurePreservesLastKnownGood`).

## System discovery

* Search mechanism: `internal/engdiscovery` — Tier 0 (managed dir +
  persisted cache), Tier 1 (PATH, executable dir, sibling bins, common
  user/package locations), Tier 2 (bounded parallel scan: worker pool,
  depth bound, time bound, candidate cap, noise-dir skip list;
  background-only, never on the startup path).
* Cache: `<DataDir>/discovery-cache.json` (path/size/mtime/SHA-256/
  arch/tag/tier/validation); entries re-inspected when size/mtime
  change; corrupt binaries are not offered (tested).
* Candidates found on THIS machine: none expected (sandbox); the
  fixture-based tests prove discovery/import behavior. No claim is
  made about coverage on the user's machine.
* Imported package: validated candidates import through
  `ImportCandidate` with byte-identical dedupe (SHA-256) and
  provenance recorded in `engine-install.json` (source = original
  path); the recorded release tag is NOT overwritten by a discovered
  import (bookkeeping never relabels the engine silently).
* Validation evidence: static-first (format + architecture + import
  closure), bounded `--version` probe last, through the same
  `internal/proc` runner as production launches; probe output is
  recorded, never trusted as configuration.

## Data root

* Old root: `%LOCALAPPDATA%\SHEYTAN-LA` (forced by the 1.3.5
  installer's machine env var).
* New canonical root: `<AppRoot>\data` (portable contract, already the
  runtime default; the installer no longer overrides it and DELETES the
  legacy machine env var, creating the data tree with Users-modify
  ACLs).
* Migration result: `config.MigrateLegacyAppDataRoot` folds the legacy
  root into the canonical root once — idempotent, restart-safe,
  hash-verified copies, newer-wins collisions, config recovery, engine
  bundle as a unit (never mixed), legacy root removed only after full
  verification, explicit `SHEYTAN_DATA_DIR` overrides never touched.
  VERIFIED by the new test suite on Linux; NOT executed against a real
  Windows installation from here.
* Duplicate cleanup result: identical files are dropped during merge
  (SHA-256), engine staging directories (`.update-stage`,
  `.engine-download`, `.update-old`) are removed after committed
  installs, and discovered imports skip byte-identical copies.

## Net Search

* Removed Research surface: workspace layer, nav item, lazy route,
  `#research` hash handling (invalid hashes resolve to Agent — no dead
  route), `ResearchPanel.tsx` (deleted), store research slice, API
  client methods, and the research CSS blocks. Shortcut numbering
  updated (Ctrl+4 = System).
* Chat integration: the shared composer control (`Thinking · Tools ·
  Net Search`) serves Chat and Agent (one component, one store).
* Agent integration: identical control and payload; the per-request
  `netSearch` intent rides `/api/run`.
* Backend reuse: the existing `internal/research` service, providers
  and cache are REUSED with zero duplication; `/api/net-search` is the
  same handler as the `/api/research` shim; `agent.WithNetSearch`
  authorizes exactly the research tool for that request (offered
  surface + execution gate), recorded in turn telemetry
  (`ctxtelemetry.TurnRecord.NetSearch`).
* Tests: `workspace-v136.test.ts` (layer removed, hash fallback),
  `net-search.test.ts` (evidence classification, honest result-count
  extraction), plus existing suites kept green; Go-side enforcement is
  covered by `ToolPolicy.allows` semantics and the orchestrator tool
  selection; no duplicate search request path exists.

## UI version

* Source of version: `package.json` (single release source) → Vite
  `define` injects `__APP_VERSION__` at build time; the backend's
  runtime `appVersion` (`/api/state` → `config.AppVersion`) stays
  authoritative once loaded; `displayVersion()` normalizes the `v`
  prefix.
* Visible value: `v1.3.6` for a v1.3.6 build (first paint included).
* Stale-version regression test: `src/version-contract.test.ts` — bans
  version-shaped display literals in shipped source (comments excepted),
  requires the `__APP_VERSION__` path, requires the vite injection from
  package.json, and the ambient declaration. The rebuilt
  `web/static/assets/index.js` contains `1.3.6` and zero `1.2.2`/`1.3.5`
  literals.

## Verification

Only actual results, separated by environment:

### Local Linux (this environment; Go 1.27.1, Node 24, Linux x86-64)

* `gofmt` clean across `internal/` and `cmd/` (one PRE-EXISTING drift in
  `internal/research/service.go` was also corrected — whitespace-only).
* `go vet -tags headless ./internal/... ./cmd/...` clean.
* `go test ./internal/... -tags headless -count=1` — 51/51 packages ok,
  including NEW suites: `internal/llm` (loader classification,
  preflight-phase catch with zero model launches, foreign-process
  adoption refusal, managed-process adoption acceptance, transactional
  commit/rollback, deterministic Start+Update race), `internal/updater`
  (zip mode honoring, transactional seam), `internal/proc` (real
  port-owner attribution), `internal/engcheck`, `internal/engdiscovery`
  (discovery, cache invalidation, bounded scan, validation),
  `internal/config` (AppData migration + hard path invariants).
* Frontend: `npm ci`, `npm run typecheck` clean, `npm run lint`
  (0 warnings, 0 errors), `npm run test:units` 96/96 (was 84 tests + 12
  new), `npm run test:release` 28/28, `npm run build`, `npm run
  verify:web` — all pass; `web/static` rebuilt and embedded-tree
  contract satisfied.
* `node scripts/release-version.mjs --check` — consistent at 1.3.6.
* Clean-room: a fresh `git worktree` at the final commit builds
  (`go build -tags headless ./...`) and re-passes the focused suites
  and the release check.
* Deterministic note: full (`-tags headless`-less) Go builds/tests of
  the Wails desktop target require GTK/webkitgtk dev packages, absent
  in this sandbox — the headless variant (what CI's Go verification
  exercises) is the verified configuration. This is a pre-existing
  environment property, not a regression.

### Local Windows

* NOT verified — no Windows machine or Windows CI in this environment.
  Windows-specific code paths (`identity_windows.go`, NSIS installer
  execution, MSVC/CTest native suites) compile-checked via
  `GOOS=windows` only as far as cross-compilation of the Go packages
  (see below) and are otherwise unexecuted.

### GitHub Actions

* NOT verified — nothing was pushed. No run exists for this work. CI
  must not be weakened when the branch lands; the new tests execute
  cross-platform (the engine/discovery fixtures run on Linux and
  Windows runners; no filesystem-wide unbounded scans in CI).

### Tag/release verification

* NOT performed — no `v1.3.6` tag exists, no release exists, no asset
  hashes exist. Publication state: NOT PUBLISHED.

## Remaining issues

Only issues actually unresolved:

1. Windows runtime acceptance (§45) — installer execution, data-root
   migration on a real installation, engine boot, `0xC0000139`
   reproduction in the wild, Net Search against live providers: must be
   performed on the Windows machine after applying this update.
2. GitHub Actions verification of the pushed branch, the `v1.3.6` tag
   run, and release publication.
3. The native C++ suite was not executed here (no Windows toolchain, no
   Wine); it is untouched by this change set and its 12/12 gate remains
   CI-owned.
4. The `0xC0000139` incident root cause on the user's machine (a
   mismatched/partial DLL set beside the binary, per the evidence) is
   now DIAGNOSED and PREVENTED structurally (identity proof + package
   validation + no mixing), but the specific defective binary on the
   user's disk was never inspected from here — the diagnostic surface
   will name the evidence on the machine itself.

## ZIP

* Exact path: `/home/z/my-project/download/SHEYTAN-Local-Agent-v1.3.6-UPDATE.zip`
* Contents count: 62 changed/new repository files + `MANIFEST.txt` +
  `REPORT.md` (this file)
* SHA-256: printed by the build step (see MANIFEST header output in the
  delivery message; the value is also verifiable against the file)
* Manifest verification: every entry re-hashed after writing and
  compared; entries match the git diff file list exactly; no `.git`,
  `node_modules`, build caches, binaries or installers are included;
  deleted files are listed in the manifest (not in the ZIP).
