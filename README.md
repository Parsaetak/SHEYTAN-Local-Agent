# SHEYTAN-LA™ (SHEYTAN Local Agent)

> **A local-first AI software-engineering laboratory.**
>
> The model proposes. The tools execute. The laboratory verifies.
> SHEYTAN calculates the best runtime for the machine, the model and the task.

SHEYTAN™ Local-Agent is a local-first desktop AI engineering environment built around Go, React/TypeScript, Wails v3, managed llama.cpp inference, controlled tools, isolated coding workspaces, Net Search, memory, recall, and objective verification.

**SHEYTAN™ is a trademark of Parsaetak · © 2024–2026 Parsaetak. All rights reserved.**

Licensed under a **conservative mixed model** — Apache-2.0 for explicitly designated open components, the **Parsaetak Proprietary License v1.1** for SHEYTAN-specific material (see `LICENSE` + `LICENSE-MAP.md`).

```text
Application:      SHEYTAN-LA (SHEYTAN Local Agent)
Current release:  v1.6.1
Executable:       SHEYTAN-LA.exe
AppUserModelID:   Parsaetak.SHEYTAN-LA
Branch:           main
```

---

# What SHEYTAN is

## v1.6.1 highlights

* **P0 — deterministic sampling gate** — the in-the-wild `--repeat-penalty 0` failure ("repeat-penalty must be finite and greater than 0") is now refused BEFORE any engine process starts: one authoritative validation module (`internal/config/sampling.go`) runs at config load (safe repair to the documented default, reported), at the Settings PATCH API (rejected with an actionable 400), on environment overrides, at the engine boot gate (classified `InvalidSamplingConfigError` — a DETERMINISTIC configuration failure, never an accelerator/compatibility verdict) and on the launch arguments themselves. The compatibility ladder and repair loop are never entered for invalid values: the pre-fix behavior spawned the engine 4+ times for one bad value. Proven by tests that count real spawns (0 after the fix).
* **First-class local GGUF import** — "place a file in the folder yourself" is replaced by an import workflow with the same engineering standard as every model operation: GGUF header validation before any bytes move (the same `ReadModelCard` authority the picker uses), streaming 1 MiB-chunk file copy (never whole-model RAM), atomic final placement (hidden `.import-*.tmp` staging + rename, size-verified), safe duplicate handling (same size + SHA-256 = duplicate report; different content = fresh `-1` name, never an overwrite), and the source is copied, never moved — arbitrary external model paths stay the user's. The picker exposes "Import GGUF…" (native Windows file dialog; typed-path fallback elsewhere) and immediately selects the imported model through the existing selection state machine. Migrated `model`/`draftModel`/`visionMmproj` paths pointing inside a retired runtime root are re-anchored onto the canonical root.
* **Readable normal logs** — unchanged per-poll accelerator resolutions log at DEBUG (Advanced diagnostics keeps the evidence; only CHANGED resolutions log at INFO), the device-enumeration warning deduplicates the same way, and the updater's stale-file reporting aggregates dozens of per-filename lines into one line per package swap (per-file detail stays at DEBUG + in diagnostics). Actionable startup/update/engine errors stay visible; no secrets, tokens, headers or model contents are ever logged.
* **Real Windows Vulkan provisioning** — the engine package carries a recorded backend VARIANT (manifest `variant: cpu|vulkan`); the transactional installer provisions the real upstream Vulkan asset (`llama-<tag>-bin-win-vulkan-x64.zip`) through the same staged/leased/rollback-safe authority as every engine update (`POST /api/engine/provision`). An explicit VULKAN request that cannot be provisioned fails loudly — never a silent CPU install; AUTO keeps selecting Vulkan only on runtime evidence (engine enumeration or a measured offload line), `executionVerified` stays evidence-gated, and a CI gate HEAD-checks the pinned Vulkan asset so the promise cannot drift into a naming fiction.
* **Conservative mixed licensing + governance** — `LICENSE` (routing), `LICENSE-APACHE`, `LICENSE-PROPRIETARY`, `LICENSE-MAP.md` (the component classification authority — open only by explicit designation), `NOTICE.md` (third-party attribution), `CONTRIBUTING.md`, `SECURITY.md`; SPDX headers on the Apache-designated component.

## v1.6.0 highlights

* **Startup maintenance gate** — engine maintenance (update/repair) completes BEFORE any engine process starts; a due update can no longer stop the freshly-booted engine mid-startup. Failed updates keep the last-known-good engine; a genuinely unusable engine blocks startup with an explicit diagnostic. The real lifecycle phase is visible (`/api/maintenance` + the maintenance banner), and compatibility fallbacks report truthful reasons (`unknown` when genuinely unknown — never "unrecorded").
* **Chat and Agent are top-level views** — `CHAT | AGENT | WORKSPACE/LAB | SYSTEM | SETTINGS`. The internal segmented selector is gone; the navigation exposes real tab semantics (`tablist`/`tab`/`aria-selected`). A fresh install lands on CHAT through one deterministic resolution (explicit URL hash > remembered view > Chat) with the conversation-space mode bound at first paint; every view owns an explicit `#hash`. Each view is its own conversation space with independent histories, per-mode active sessions and bounded cross-mode history references, over one shared runtime (engine, memory, tools, research).
* **Automatic long context** — the user-facing context-window editor is removed. The physical window is managed automatically from the model and engine limits; logical conversation continuity is unbounded through history, summaries, retrieval, chunking, rollover and memory. A read-only `LONG CONTEXT · AUTOMATIC` indicator states the concept honestly.
* **Custom tools** — Settings → Agent & Tools → My Tools → + Create Tool. Local custom tools become first-class citizens of the same registry as built-ins (`/api/tools` carries `source: builtin|custom`), with explicit permissions (network / local execution), HTTPS-only HTTP tools, bounded timeouts and output, cancellation, and disabled-by-default creation. Local commands run under per-invocation process-tree ownership (Windows Job Objects — timeout/cancellation terminates the complete descendant tree, not just the direct child). Tested end-to-end: create → validate → register → model call → execution → result.
* **Truthful downloads** — the existing Download Manager remains the single authority; every download surface shows the real phase chain (resolving → connecting → downloading → verifying → installing → ready), measured bytes/speed/ETA only when measurable, source/trust, retries and verification state.

A single Windows-first desktop application that:

1. **Manages its own inference engine** — a llama.cpp server is downloaded, launched, health-checked, supervised (bounded auto-restart) and updated automatically. No manual engine babysitting.
2. **Runs a real agent loop** — plan → tool calls → observations → verification → final answer, with streaming, cancellation, retries, per-run time budgets and loop prevention.
3. **Executes engineering work in an isolated Coding Lab** — workspace copies, shell/network/dangerous-command policy, objective verification gates, repair loops, snapshot-before-promote.
4. **Treats long context as an engineering problem** — measured context budgets, structure-aware (paragraph-boundary) chunking, a content-keyed cache, provenance-tagged retrieval, recall of past exchanges, and automatic chapter rollover for sessions that outgrow their window.

The model is never the authority on whether an engineering task succeeded — objective verification is.

# Architecture

```text
┌─────────────────────────────────────────────┐
│              React / TypeScript             │
│                  Vite UI                    │
└──────────────────────┬──────────────────────┘
                       │ REST + WebSocket (127.0.0.1)
                       ▼
┌─────────────────────────────────────────────┐
│                   Go API                    │
└──────────────────────┬──────────────────────┘
                       ▼
┌─────────────────────────────────────────────┐
│                 Go Runtime                  │
│  agent orchestrator · tool registry (18)   │
│  engine backend contract · llama.cpp      │
│  lifecycle · sandbox governor ·           │
│  attachments · chunking · context cache    │
│  context plan · memory · recall · continuum│
│  net search · sessions · browser · vision  │
└──────────┬──────────────────────┬─────────┘
            │                      │
            ▼                      ▼
┌───────────────────┐   ┌───────────────────────────────┐
│  llama.cpp server │   │  SHEYTAN Native Engine (new)  │
│  local inference  │   │  C++ core + supervised host   │
│  (fallback and    │   │  (Phase 5: lifecycle, health,│
│   default engine  │   │   hardware, metrics, GGUF     │
│   for unsupported │   │   model loading, tokenizer, KV│
│   cases)          │   │   cache, scheduler, REAL      │
│                   │   │   llama-arch generation with  │
│                   │   │   streaming + cancellation)   │
└───────────────────┘   └───────────────────────────────┘
```

Critical execution logic belongs to Go. Presentation and interaction logic belong to React. The production desktop app embeds the built frontend (`web/static/`) via `go:embed` — no separate frontend server is needed.

## v1.3.5 — Windows CTest Completion / Native Test Hardening

**v1.3.5 root-fixes the two Windows CTest failures of Actions run 35583009466 (FAIL `tokenizer`, FAIL `generate` — the Windows runner had successfully configured CMake, built the complete native engine, built all 12 test executables and started CTest, 10 PASS / 2 FAIL): both failures share ONE root cause — POSIX-only temporary-path assumptions in the native tests (`getenv("TMPDIR")` with a literal `/tmp` fallback, and a hard-coded `/tmp/shtn-not-llama.gguf`), which do not exist on Windows. The fix is ONE reusable cross-platform test helper (`native/engine/tests/temp_dir.h`, new): `std::filesystem::temp_directory_path()` (GetTempPath on Windows), collision-free unique directories (process id + counter + clock + random suffix), native paths that round-trip through stdio (spaces supported), and RAII recursive cleanup — no `tmpnam`, no TMPDIR-must-exist assumption, no per-test platform `#ifdef` copies (the three previously duplicated implementations in test_gguf/test_model/test_host are consolidated). The deep audit that followed found and fixed two further GENUINE defects: a Windows engine bug in `MappedFile` (`native/engine/src/gguf.cpp`) where `close()` passed the SECTION handle to `UnmapViewOfFile` — which requires the VIEW base address — so the unmap silently failed, every model unload leaked a section + view, and the still-mapped view pinned the `.gguf` file until process exit (a model could not be deleted or replaced after unload on Windows); and `test_host`'s Phase 5 fixtures resolving through `getenv` with a cwd-relative `../tests/fixtures` fallback that only worked under CTest's default working directory (now the configure-time `SHTN_FIXTURES_DIR` macro, plus the Makefile rule define that was missing — `make -C native/engine test` now passes 12/12 too). New regression contracts prove the portable temp directory (create/write/read/close/remove, spaces, uniqueness, RAII cleanup) and the mapped-file release (after load + unload the model file must be deletable); a CI gate fails the build if any native source reintroduces a `/tmp` literal or a direct `getenv("TMPDIR")` outside the one helper. No assertion was weakened; no test skipped, disabled or excluded; CTest remains a hard release gate expecting 12/12 on BOTH platforms. The v1.3.4 Repository Intelligence slice is untouched and fully re-verified. Windows verification on this host: the engine, host and all 12 test binaries cross-compile fully-static (llvm-mingw/clang) and pass 12/12 executed under Wine with zero temp leftovers; MSVC CTest, packaging, installer and publication remain CI-owned.**

| Area | Change | Status |
|---|---|---|
| **test_tokenizer / test_generate root-fix** | Synthetic fixtures now live under `shtn_test::TempDir` (portable temp root, unique per run, removed on exit); all original assertions kept 1:1 | ROOT-FIXED, VERIFIED (Linux 12/12; Windows 12/12 under Wine; the pre-fix v1.3.4 binaries reproduce both failures in the same harness) |
| **Shared helper — `tests/temp_dir.h` (NEW)** | One RAII `TempDir` for the whole native suite: `std::filesystem::temp_directory_path()` + collision-free naming + native paths + spaces support + recursive no-throw cleanup; the duplicated private `tmp_dir()` copies in test_gguf/test_model/test_host are gone | IMPLEMENTED, TESTED (portable temp contract in test_gguf: create/write/read/close/remove, spaces, uniqueness, RAII) |
| **Windows engine defect — `MappedFile` handle inversion** | `open()` closes the file handle once the view exists (the section holds its own reference) and stores `handle_` = section, `map_` = view base; `close()` unmaps the VIEW and closes the SECTION — matching the class contract and the POSIX branch | ROOT-FIXED, TESTED (unload → model file deletable contract in test_model + release contract in test_gguf; Windows run leaves zero locked files) |
| **`test_host` cwd-dependence** | Phase 5 fixtures use the configure-time `SHTN_FIXTURES_DIR` macro (previously dead configuration — the getenv + `../tests/fixtures` fallback only worked under CTest's cwd); the plain-make rule now defines it too | ROOT-FIXED, VERIFIED (passes from foreign cwds; `make -C native/engine test` 12/12) |
| **test_engine portability** | The `/definitely/not/here.gguf` nonexistent-path literal is now derived from the portable temp dir | IMPLEMENTED, VERIFIED |
| **CI regression gate** | Audit job: "Native temp-path portability gate" — `git grep` fails on any `"/tmp` literal or direct `getenv("TMPDIR")` in native sources outside `tests/temp_dir.h` | IMPLEMENTED, VERIFIED (gate returns zero hits on this tree) |
| **Repository Intelligence (v1.3.4)** | No source change; full re-verification (`internal/repoindex` tests, API contract, agent context, Workspace card) | INTACT, VERIFIED |
| **Release integrity** | Canonical version 1.3.5 synced across package.json → config.go / build/config.yml / SIGNATURE via the canonical script; `--check` green; codename gate zero matches; ROADMAP.md byte-identical | VERIFIED |

## v1.3.4 — Codename-Gate Root-Fix + Repository Intelligence (ROADMAP v1.4 Slice 1)

**v1.3.4 root-fixes the self-triggering codename-removal gate (Actions run 35571331850): the repository-wide gate scans EVERY tracked file, and the retired product codename had survived as literals inside the release contract source itself, its regression fixtures and four documentation files — the gate kept tripping on its own enforcement surface. The fix removes every literal (docs included), keeps the regression coverage by CONSTRUCTING the forbidden test strings dynamically at runtime, weakens nothing (no file exclusions, no pattern loosening), and leaves the permanent release rule untouched: GitHub Release title = the plain canonical version, tag `v${APP_VERSION}`, one identity variable. On top of the repair, v1.3.4 ships the first production vertical slice of ROADMAP v1.4 — Repository Intelligence: a bounded, persistent, INCREMENTALLY updated repository index (`internal/repoindex`) with language-aware symbol extraction, resolved in-repo dependency edges, deterministic test/source relationships, optional Git-aware relevance metadata, a hybrid evidence-ranked search API, the agent-facing `repo_search` tool, a tier-gated repository-evidence context block wired through the existing budget pipeline, and a Workspace card exposing the index state and a search entry point.**

| Area | Change | Status |
|---|---|---|
| **Codename gate root-fix** | The retired codename literal is gone from every tracked file (README, UPDATE, agent.md, worklog, release-contract source, regression fixtures); `scripts/release-version.mjs` and its test suite now assemble the banned token at runtime (`["Ze", "ta"].join("")`) so the scanned source text never contains it; the gate command itself is unchanged — zero matches repository-wide, 28/28 release tests | ROOT-FIXED, VERIFIED (exact CI gate command re-run locally: zero hits) |
| **Persistent repository index** | `internal/repoindex`: one JSON file per workspace root under `<DataDir>/repoindex/` (atomic tmp+rename, the house persistence pattern); records carry path, language, role, size, mtime, bounded content digest, package, symbols, raw imports, resolved dependency edges, test links and Git/recent metadata | IMPLEMENTED, TESTED (persistence/reload round-trip) |
| **Incremental updates** | Unchanged files are detected via (mtime, size) and never re-read; a bounded digest (size + first 64 KB) catches mtime-only touches; only changed/new files are parsed; deletions drop records; every update is bounded (3 s work budget with resumable partial passes, 8 000 files, 512 KB parse cap, 400 symbols/file, 120 imports/file, 100 edges/file); nothing pathological can freeze the app | IMPLEMENTED, TESTED (unchanged pass = 0 re-parses; mtime-touch = digest-stable; deletion = clean edge removal) |
| **Language-aware parsing** | Go (package/func/method/type/struct/interface/const + import paths), TypeScript/JavaScript (function/class/interface/type/enum/const + import/export-from/require/dynamic), C/C++ (struct/class/enum/namespace/define/typedef/functions/qualified methods + quoted/system includes), JSON (top-level keys as bounded symbols), go.mod (module path for dependency resolution); conservative binary sniffing and fallback otherwise — no heavyweight parser dependency | IMPLEMENTED, TESTED (per-language extraction suites) |
| **Dependency graph** | Go module imports resolve to the imported package directory's files; TS/JS relative imports resolve with extension + `index.*` probing; C/C++ quoted includes resolve against the importer dir and conventional include roots; every edge is a statement-derived VERIFIED structural fact; reverse edges (imported-by) derived in-memory | IMPLEMENTED, TESTED (forward + reverse edges, all three language families) |
| **Test/source relationships** | Deterministic, evidence-based: `foo.go ↔ foo_test.go` (same directory), `foo.ts → foo.test.ts / foo.spec.ts` (same dir + `__tests__/` sibling); symmetric links, bounded per file | IMPLEMENTED, TESTED (all three conventions) |
| **Git-aware foundation** | `git ls-files` + `git status --porcelain` (both output-bounded) stamp tracked/untracked/modified per record; mtime recency (7-day window) works without Git; signals are relevance metadata only, never correctness authority; Git absence degrades honestly | IMPLEMENTED, TESTED (git + non-git fixtures) |
| **Hybrid search** | `repoindex.Store.Search`: symbol (exact/prefix/substring), path (basename/path), language/role filters, keyword and task dimensions over indexed metadata, dependency expansion (depsOf/usedBy), test lookup (testsOf); weighted evidence scoring, deterministic order (score desc, path asc), bounded result pages (default 12, hard cap 50); every hit carries an Evidence string distinguishing structural facts from inferred relevance | IMPLEMENTED, TESTED (deterministic ranking pinned across repeated runs) |
| **Agent tool: `repo_search`** | Registered in the orchestrator + `toolsets` (coding/filesystem groups); bounded, deterministic, path-safe (workspace jail — absolute refs relativized, escapes rejected), evidence-based output with symbols/lines/evidence | IMPLEMENTED, TESTED (end-to-end tool run, escape rejection) |
| **Context-pipeline integration** | `Orchestrator.SetRepoEvidence` mirrors the project-card seam: a tier-gated (STANDARD+) repository-evidence block composed per task, budgeted through the existing optional-block plan (degradation ladder + escalation ladder honored), injected in the same cache-friendly position — no second context system | IMPLEMENTED (lives inside the existing contextplan budget path) |
| **Workspace surface** | Repository Index card on the Workspace panel: indexed files, languages, symbols, dependency edges, test links, Git availability, index state, last update, bounded refresh action and a repository search entry point (POST /api/repo/search); status endpoint is walk-free and poll-safe | IMPLEMENTED, TESTED (API contract test: switch→reindex→status→search) |
| **Release integrity** | Canonical version 1.3.4 synced across package.json → config.go / build/config.yml / SIGNATURE via the canonical script; `--check` green; ROADMAP.md byte-identical (blob `c7e2c17` verified — the roadmap is documented, never edited, for completion) | VERIFIED |

## v1.3.3 — Deep Repair: Settlement Determinism, Native Windows Build, Version-Only Release Identity

**v1.3.3 root-fixes the v1.3.2 CI failures (Actions run 35552680611): the Linux settlement race (`TestAgentRunSettlesWithSummaryAndHandoff` — "agent.md handoff missing") is eliminated by replacing the summary-completion proxy with a deterministic TERMINAL-OUTCOME settlement barrier (production records the terminal outcome only after every required durable artifact — rolled summary, agent.md handoff, recall, continuum — is on disk, so observing settlement IS observing complete settlement, and every test that waits for settlement now uses it); a completed Agent run whose MANDATORY agent.md handoff fails to write can no longer settle "done" — the terminal outcome is honestly demoted to "error" with the concrete cause in the caption while the reply, summary and live error activity are preserved; the Windows native-engine compilation failure in `hardware.cpp` is root-fixed (explicit `<vector>` / `<cstdlib>` / `<intrin.h>`, `NOMINMAX` before `windows.h`, parenthesized `(std::min)` — no transitive-include or macro-collision dependence), the engine's read loop can no longer wedge behind an abandoned stream's straggler burst, and the release identity is permanently VERSION-ONLY: the GitHub Release title is the plain canonical version (tag `v1.3.3`, title `1.3.3`, no prefix, no codename, no suffix), structurally enforced by the release contract (required + banned fragments, 28-test regression suite). No working architecture was changed.**

| Area | Change | Status |
|---|---|---|
| **Deterministic settlement barrier** | `waitForRunSettled` / `waitForRunSettledFor` / `waitForRunOutcome` poll the bounded outcome registry — the terminal record the v1.2.9 durable-completion ordering guarantees is written only AFTER all durable artifacts; summary completion is never again a settlement proxy; repeated settlement keys the barrier on the exact runId | IMPLEMENTED, TESTED (20x race stress PASS) |
| **Mandatory-handoff honesty** | A completed Agent run with a failed `agent.md` write settles `"error"` (caption carries the concrete cause), never `"done"`; nil task state is itself reported as a handoff failure instead of silently skipping the mandatory artifact; reply/summary/live-error behavior preserved | IMPLEMENTED, TESTED (deterministic ENOTDIR failure injection) |
| **Windows native build** | `hardware.cpp`: `<vector>`, `<cstdlib>`, `<intrin.h>` explicit; `NOMINMAX` + `WIN32_LEAN_AND_MEAN` guarded; `(std::min)` parenthesized; the three `windows.h` test files carry the same guards; full-engine Windows cross-build verified (host + all 12 CTest executables, zero project warnings) | IMPLEMENTED, VERIFIED (cross-toolchain; MSVC run owned by CI) |
| **Engine robustness** | IPC read loop drops event frames for ABANDONED streams instead of blocking on a full buffer (a >256-frame straggler burst can no longer wedge the connection ahead of the cancelled final); 8 compiler warnings eliminated (1 production, 7 tests) | IMPLEMENTED, TESTED (real-host lifecycle suites, 10x) |
| **Version-only release identity** | GitHub Release title = plain `APP_VERSION` (`1.3.3`); the release job verifies tag == `v${APP_VERSION}` AND title == version after publication; the workflow contract structurally bans the product/tag-derived title, retired-codename fragments and `APP_VERSION_FULL`; canonical version 1.3.3 synced across package.json → config.go / build/config.yml / SIGNATURE | IMPLEMENTED, TESTED (28/28 release suite) |

## v1.3.2 — Release Repair + Native Engine Execution Hardening

**v1.3.2 root-fixes the Windows CI release-metadata failure (GitHub Actions run 35542000811): release metadata is now validated through ONE canonical cross-platform implementation (`scripts/release-version.mjs --check` — every build job orchestrates it; the per-shell bash/PowerShell reimplementations, one of which treated regex escapes as literal text under `-SimpleMatch`, are gone and structurally banned from returning), the redundant `APP_VERSION_FULL` alias is removed, and a release-metadata regression suite pins semver shapes, drift, whitespace/CRLF handling, identity emission and the workflow contract. Native engine execution becomes a first-class release objective: engine selection is observable (a native selection that falls back to llama.cpp now carries an inspectable reason in logs and `/api/engine` — never silent), a dedicated execution test contract covers executable discovery, deterministic startup, bounded boots, model-path failures, shutdown-during-generation and orphan-process prevention against the real C++ host, the Windows pipeline builds/ctests the native engine and runs the real-host Go integration suites before packaging, and the stable-asset verifier is rewritten contract-driven (vite output-pattern assertion + asset reachability) so legitimate deterministic names carrying version digits are never false-rejected. No working architecture was changed; the v1.3.1 frontend contract (deterministic filenames, `web/static` mirrors clean `dist`) is preserved.**

| Area | Change | Status |
|---|---|---|
| **Canonical release gate** | `release-version.mjs` is the single release-metadata authority (sync / `--check` / `--env`); the audit, Windows and Linux jobs all run the canonical check; the validator's workflow contract requires `--check` in every build job and bans per-shell reimplementations (the run-35542000811 pattern class included) | IMPLEMENTED, TESTED (21-test regression suite, `npm run test:release`, run in every CI job) |
| **Identity simplification** | `APP_VERSION_FULL` removed everywhere (workflow outputs/envs, `--env` emission) — an audit found no consumer distinguishing it from `APP_VERSION`; `--env` emits exactly one variable | IMPLEMENTED, TESTED |
| **Observable engine selection** | `llm.SelectGenerationBackendDetailed` + `GenerationFallbackReporter`; the native backend reports the three infrastructure classes (not running / no model / not natively executable); the runtime seam logs and records the reason; `/api/engine` exposes `fallbackReason` + `fallbackCount` | IMPLEMENTED, TESTED |
| **Native execution contract** | New `execution_contract_test.go`: executable discovery, protocol-mismatch rejection + retry, bounded-boot teardown, missing model path (engine stays healthy, load recovers), stop-during-generation (no wedge, no orphan, restartable), orphan prevention across repeated cycles (signal-0 probe) | IMPLEMENTED, ALL TESTS PASS (real C++ host) |
| **Windows native execution** | The Windows job builds + ctests the native engine and stages `shtn-engine-host.exe` so the Go↔C++ `TestRealCppHost*` suites execute for real on Windows before packaging (subprocess IPC — no cgo change) | IMPLEMENTED (pipeline; validated structurally + via local execution of the same suites) |
| **Contract-driven asset verifier** | `verify-static-assets.mjs` asserts the vite stable output patterns (no hash tokens) and proves every `web/static` file reachable from `index.html` (html refs, JS imports incl. `__vite__mapDeps`, css `url()`); the hyphen+digit name heuristic is gone | IMPLEMENTED, TESTED (7-case negative/positive suite) |
| **Stale comment cleanup** | Phase-1-era comments corrected to Phase-5 reality (`llm/backend.go`, `api/engine.go`, integration tests); `TestBackendGenerationNotImplemented` renamed to match its semantics | IMPLEMENTED |

## v1.3.1 — Product Polish, Stable File Structure & Repository Cleanup

**v1.3.1 is a professionalization release: the product identity becomes version-only (the retired codename is removed from every surface — runtime constants, CI, packaging, user-agents, docs and tests), the embedded frontend adopts STABLE deterministic filenames (no content hashes — `index.js`, `AgentBody.js`, `SettingsPanel.js`, …), `web/static` is regenerated from one clean authoritative build, the release/version architecture collapses to a single canonical source (`package.json` → `APP_VERSION`), CI gains explicit codename/stable-asset/stale-file gates, and the version-suffixed test-file pile is consolidated into behavior-oriented test files. No architecture changes, no feature loss.**

| Area | Change | Status |
|---|---|---|
| **Version-only identity** | `package.json` is the single source of truth; `release-version.mjs` validates plain semver, synchronizes `config.go` / `build/config.yml` / `SIGNATURE` and emits exactly one identity variable, `APP_VERSION` (the retired `APP_VERSION_FULL` alias stays dead). No codename dimension exists anywhere | IMPLEMENTED, TESTED |
| **Stable frontend filenames** | The Vite/Rollup build emits `assets/[name].js` + `assets/[name][extname]` — deterministic names, code splitting preserved. The same physical file keeps the same name across builds | IMPLEMENTED, TESTED (`scripts/verify-static-assets.mjs`) |
| **Authoritative embedded tree** | `clean dist → npm run build → sync-web → web/static`; the old hashed bundles are gone; `web/static` exactly mirrors the clean build | IMPLEMENTED, VERIFIED in CI |
| **CI hardening** | New gates: codename-removal scan (zero matches), stable-asset contract, stale-generated-file check (`--dist` mirror), version consistency; all prior gates (Windows/Linux builds, native C++, race, packaging, installer, release manifest, checksums) unchanged | IMPLEMENTED |
| **Research identity** | GitHub/Reddit/DuckDuckGo/SearXNG requests carry `SHEYTAN-Local-Agent/<version>` derived from the runtime constants — no obsolete hardcoded identity | IMPLEMENTED, TESTED |
| **Test consolidation** | `*_v129_test.go` / `*_v1281_test.go` / `v125/v126_measurement` / phase-suffixed files merged into canonical behavior-named files (`handoff_test.go`, `runtransport_test.go`, `measurement_test.go`, …) with identical coverage | IMPLEMENTED, ALL TESTS PASS |
| **Naming cleanup** | the codename-branded stress file renamed to `cmd/stress_release_surface.go` (scenarios renamed `release_surface` / `memory_unique_ids` / `trimlogs_rotate`); historical `v1.x.yZ` labels rewritten to plain versions across code and docs | IMPLEMENTED |

## v1.3.0 — Runtime Path Correctness, Clean Logging, Universal Scrolling, Professional Settings, GitHub Cloning

**v1.3.0 is a runtime-correctness and workflow release: the malformed `%LOCALAPPDATA%` runtime paths are eliminated at the source (NSIS installer fixed, one authoritative path resolver, automatic migration of malformed v1.2.9 trees), startup logging is clean (no repeated fast-snapshot spam, no blank warnings), every workspace tab scrolls through one explicit layout contract, Settings is restructured into eight user-outcome sections with engine internals automated or moved to Advanced, and a first-class Clone GitHub Repository workflow lands end-to-end with automatic workspace switching.**

| Area | Change | Status |
|---|---|---|
| **Runtime path correctness** | ONE authoritative path resolver (`internal/config/paths.go`): environment references expand once, unresolved `%TOKEN%` values are rejected to the canonical root, every derived directory (models/sessions/logs/workspace) anchors there; the NSIS installer writes an expanded absolute data path instead of a REG_EXPAND_SZ token | IMPLEMENTED, TESTED (unit + real-binary acceptance) |
| **Malformed-root migration** | v1.2.9 trees (`<root>\%LOCALAPPDATA%\SHEYTAN-LA`, doubled SHEYTAN-LA nesting) fold into the canonical root: models/sessions move first (renamed or hash-verified), collisions keep the newer file, a recovered config.json is re-loaded, the source tree is removed only after every entry is accounted for; idempotent and restart-safe | IMPLEMENTED, TESTED (unit + real-binary acceptance) |
| **Clean logging** | `fastSnapshot()` is a silent read/cache event; ONE concise "fast environment ready" summary per process; the deep probe logs its one measurement; WARN/ERROR records are never blank (call-site context injected by the log manager); the scheduled updater's blank `WARN [updater]` root cause fixed | IMPLEMENTED, TESTED |
| **Universal scrolling** | One layout contract: `.workspace > .view-transition` is a shrinkable flex column; System/Workspace/Research/Settings own a single vertical scroll viewport; Agent and Coding Lab keep their internal IDE-style scroll regions; short windows and 100–150% scaling resolve through the same chain | IMPLEMENTED, TESTED (browser-validated per tab, short-window validated) |
| **Settings restructure** | Eight sections — General, Models, Performance (Quiet/Balanced/Maximum + measured facts), Agent & Tools, Network, Updates, Diagnostics (read-only), Advanced; the MM projector is "Automatically managed" (no raw path fields in normal UI); engine host/port/paths/flags, cache tuning, batch/thread controls and storage limits are Advanced-only; legacy config fields remain loadable/savable | IMPLEMENTED, TESTED (structure + persistence regressions, browser-validated) |
| **Settings save safety** | `llm` patches deep-merge field-by-field (partial posture/preset patches no longer zero sampling fields); posture applies write the real top-level `ubatchSize`; hardware facts load on Diagnostics too | IMPLEMENTED, TESTED |
| **GitHub cloning** | `POST/GET /api/workspace/clone[/status|/cancel]`: structured git execution via the validated proc seam (no shell, explicit argv, tree-kill cancellation, bounded output, exit-code validation), classified actionable errors, verified checkout, automatic workspace switch through the exact `/api/workspace/switch` sequence; Workspace tab gains the full Clone workflow UI (URL, destination, branch, progress, cancel, result) | IMPLEMENTED, TESTED (unit + real end-to-end clone + auto-switch) |
| **Automation-first controls** | GPU offload, flash attention, KV quant, projector pairing and context recommendations are automatic/measured; the UI labels Automatic / Recommended / Measured explicitly and never claims "optimal" without measurement | IMPLEMENTED |

## Release history (summary)

Full engineering evidence for every release lives in `worklog.md`; the
notes below are one-line headlines.

| Version | Headline |
|---|---|
| v1.6.1 | P0 sampling gate (deterministic config failures never spawn the engine), first-class GGUF import, readable normal logs, real Windows Vulkan engine provisioning (variant manifests, no silent CPU fallback), conservative mixed licensing + governance |
| v1.6.0 | Startup maintenance gate, top-level Chat/Agent views, automatic long context, custom tools, truthful downloads |
| v1.5.1 | Honest model-card compatibility verdicts, selection-flow hardening |
| v1.5.0 | MODEL-FIRST startup: backend-authoritative selection state machine, recommendation engine, retained calibration |
| v1.4.0 | CI-integrity repair, release contract, testfakes portability gate |
| v1.3.7 | Run settlement edge (idle/lastRun always recoverable), engine provisioning order offline-first, Repair honesty, transactional model-architecture auto-update, installer rollback + unmanaged-dir protections, Net Search server-side contract |
| v1.3.0 | Runtime path correctness (one authoritative path resolver + malformed-tree migration), clean startup logging, universal scrolling, user-outcome Settings, first-class GitHub clone workflow |
| v1.2.9 | Stabilization & security: cross-mode history authority boundaries, durable completion ordering, concurrent handoff safety, honest context budgeting, CI race gate |
| v1.2.8 | Professional Chat + Agent workspaces on one runtime: mode-separated histories, cross-mode references, durable rolling summaries, context compaction, agent.md handoff |
| v1.2.8.1 | Repair label: CI CMake cache fix, adaptive context floor, regenerate reference parity, handoff read-back verification |
| v1.2.7 | Run-transport terminal-state repair, run-lifecycle audit closure, CI/test stabilization |
| v1.2.6 | Authoritative run transport, deterministic attach, measured run timing, evidence-based accelerator resolution |
| v1.2.5 | Adaptive context tiers (FAST/STANDARD/THINKING), thinking mode, per-request telemetry |
| v1.2.4 | Faster data processing, memory reclamation, workspace panel, usability pass |
| v1.2.3 | CI engine-race root fix, reusable verifiable Download Manager, honest download progress |
| v1.2.2 | Generation visibility & black-screen repair (error boundaries, socket lease, live timeline) |
| v1.2.1 | CI package-root contract (one canonical root per platform), installer options, packaging hardening |
| v1.2.0 | SHEYTAN-LA unified product upgrade: vision readiness, hardware intelligence, recommendations, NSIS installer, verified updater |
| v1.1.9 | CI output-name fix, mode-aware navigation, explicit model states |
| v1.1.8 | Chat/Agent separation, model picker, release-metadata single source of truth |
| v1.1.7 | Options clarity, capability truth, live telemetry, in-app diagnostics |
| v1.1.6 | Stabilisation: context control (per-session policy, guaranteed fit), startup UX, branding |
| v1.1.5 | Native engine phases 4–6: real llama-architecture C++ inference, reliability, verification, safe edits, project intelligence |

## SHEYTAN Native AI Engine (real native inference)

v1.1.5 establishes the **SHEYTAN Native AI Engine architecture**: Go
remains the main application/runtime engine, and a C++ native engine
(`native/engine/`) performs **real transformer inference** for the llama
architecture behind a narrow C ABI, supervised by Go as a subprocess
(`shtn-engine-host`) over a length-prefixed JSON IPC protocol.

**Status — read this literally:**

- **IMPLEMENTED**: the backend abstraction (`internal/llm` `Backend`
  contract with Start/Stop/Health/LoadModel/UnloadModel/Generate/
  StreamGenerate/Cancel/ModelInfo/HardwareInfo/Metrics), backend
  selection with automatic fallback to llama.cpp, the supervised native
  engine lifecycle (start / health-check / mark ready / stop / detect
  failure / bounded restart), the platform-neutral hardware profile, the
  native metrics snapshot, and — since Phase 2 — **native GGUF model
  loading**: a bounds-checked, overflow-safe C++ GGUF reader
  (header/version/metadata/tensor-table validation), memory-mapped model
  access (lazy — tensor data is never copied into RAM), real metadata
  extraction (architecture, parameter count, context length, vocabulary
  size, embedding length, layer count, quantization, tensor count, file
  size), a load-time memory plan (file/mapped/weights/workspace/KV-cache
  estimates/runtime overhead — computed, never allocated), the model
  lifecycle (`unloaded` / `loading` / `loaded` / `failed`) with
  replace-semantics and clean unload, and the `ModelInfo` surface wired
  through the shared `llm.Backend` contract. Phase 4 added **real
  foundation primitives**: a real GGUF-backed tokenizer (BPE/Unigram/WPM
  with merges, special tokens, BOS/EOS/UNK — materialized by re-walking
  the memory map on demand), a real KV-cache data structure (sized from
  real model dims, GQA-aware, contiguous K+V allocation, capacity/usage/
  used-positions measured honestly — since Phase 5 populated by the real
  forward pass), a real bounded scheduler (single-slot, FIFO,
  queue cap, cancel, drain, no busy poll), real sampling primitives
  (greedy/temperature/top-k/top-p/repetition penalty/seedable RNG —
  deterministic), streaming UI coalescing (rAF-boundary batching of token
  chunks — one setState per frame regardless of token rate), and a
  frame-budget diagnostic perf HUD (auto-detects the display refresh
  rate; reports `optimized for high-refresh displays / frame-budget
  aware / 120 Hz-capable presentation where hardware permits` — never a
  guaranteed-120-FPS claim).
- **IMPLEMENTED (Phase 5 — REAL native inference)**: in addition to the
  foundation above, the real llama-architecture forward pass
  (embeddings → per-layer RMSNorm → Q/K/V matvec → RoPE → causal GQA
  attention over a true fp16 KV cache → SwiGLU FFN → final norm →
  logits), a tensor access layer with row dequantization for
  F32/F16/Q4_0/Q4_1/Q5_0/Q5_1/Q8_0 (anything else fails explicitly),
  load-time llama-graph validation (`GenerationCapable()` = alive +
  loaded + validated — never blindly true), REAL token-by-token
  generation with streamed chunks, cooperative cancellation
  (per-token observation), measured generation metrics (TTFT / tok/s /
  KV positions from the monotonic clock), and the generation router:
  native when selected AND capable AND plain-text, llama.cpp otherwise
  with a logged, inspectable reason.
- **Narrow, honest native support**: llama architecture ONLY;
  F32/F16/Q4_0/Q4_1/Q5_0/Q5_1/Q8_0 tensors ONLY; rope.freq_scale 1.0
  ONLY; a plain role-labeled prompt format (no chat-template
  interpretation); tools/images requests stay on llama.cpp. Measured on
  the test fixtures the portable scalar C++ forward pass is SLOWER than
  llama.cpp (see `worklog.md` Phase 5 numbers) — no native-speed claim is
  made; numerical correctness is pinned against an independent Python
  reference.
- **llama.cpp remains fully functional as the fallback** (and the
  default engine). Nothing about v1.1.4 runtime behavior changes unless
  you explicitly opt in via `engineBackend: "native"` in `config.json`
  (or `SHEYTAN_ENGINE_BACKEND=native`), which additionally requires
  building the host binary from `native/engine/` with CMake/Make (the
  host binary is not shipped/auto-downloaded yet).

The Go↔C++ boundary decision (supervised subprocess + IPC instead of
cgo) and the full rationale are documented in
`internal/native/engine/doc.go` and `ARCHITECTURE.md`.

# Major capabilities (all implemented and tested)

| Area | What works |
|---|---|
| **Engine lifecycle** | Automatic download → launch → `/health` readiness → `ready`; bounded auto-restart (3 attempts, exponential backoff); deliberate stop suppression; port-conflict adoption; compat ladder (4 launch profiles, now the LAST resort behind per-option surgical repair); engine capability profile (--help detection, pre-launch validation, verified persistence); one-shot self-update when a model needs a newer engine; scheduled update loop (daily/weekly/monthly) |
| **Engine states** | `idle / downloading / starting / ready / running / busy / stopping / stopped / failed` — backend-authoritative, fanned out over every activity WebSocket; the UI never invents them |
| **Agent loop** | Streaming responses with think-tag splitting and native `reasoning_content`; tool calls with argument validation; tool-result follow-up turns; iteration cap (default 25); per-run time budget (default 60 min); abort with partial-result preservation; regenerate; timeout-vs-abort distinguished in the UI |
| **Reliability (Phase 6)** | Every tool failure diagnosed into 14 categories with a category-specific repair hint fed back to the model (re-plan, never blind retry); loop guard detects repeated identical calls (warning on first repeat, refusal past the bound) with run-level tool-call (200) and wall-clock (30 min) budgets; failure tally and per-tool call stats on every run |
| **Verification (Phase 6)** | Run-level verification verdict computed from objective evidence only: `verified / partially_verified / failed / not_verified`. Lab verify actions and explicit build/test outcomes count; everything else contributes nothing. A completion claim with no evidence reports `not_verified` — model prose is never proof |
| **Tools** | 18 registered tools (adds `pipeline`): `shell`, `files`, `codeExec` (Job-Object sandboxed), `webSearch`, `git`, `browser`, `dataAnalysis`, `json`, `archive`, `fetch`, `diff`, `screenshot`, `linux`, `coding_lab`, `research`, `memory` (+ sandbox override). Tool schemas are measured exactly before windowing |
| **Coding Lab** | Isolated workspace copies (symlinks skipped, `.git` excluded), lexical command policy (dangerous/network/interactive/escape denylists + expansion-token hardening), 2 MiB bounded output, sanitized environment (secrets scrubbed, `HOME` pinned to the workspace), objective verification (trivial `echo`-style checks rejected), bounded repair loop with repeat-command detection, patch export, snapshot-before-promote; safe anchored edits (`read_file` bounded line-numbered views, `edit_file` requires the anchor to occur exactly once — stale or ambiguous anchors are refused, writes are atomic and re-verified, every edit invalidates verification) |
| **Project intelligence (Phase 6)** | Persistent per-project facts: measured languages, build system, layout, Lab-verified build/test commands (verified beats inferred, never clobbered), bounded lessons. The compact card is injected into every run so the model starts knowing the project instead of re-discovering it |
| **Attachments** | Content-addressed staging (sha256, symlink-safe, no exec bits), size/count/processing/chunk caps, text normalization + paragraph-boundary chunking, cached retrieval with provenance headers, image classification into the vision pipeline |
| **Context** | Preflight budget pipeline with a guaranteed fit: model-aware effective window (configured ∩ GGUF limit ∩ engine limit), output reserve + safety margin, measured tool schemas, automatic degradation ladder (dynamic toolsets → compact briefing → dropped optional blocks), refusal without an engine call when the budget is impossible; history windowing to the budget; content-keyed LRU cache; context-effectiveness telemetry per turn |
| **Long context** | Continuum chapter rollover: when a session crosses the pressure threshold, facts/decisions/threads are distilled into a framework and the conversation continues in a fresh chapter session (the UI follows automatically) |
| **Memory & recall** | Trust-classed memory (M1–M7, external material quarantined), BM25 recall with recency boost and 👍/👎 feedback steering (persistent sidecar) |
| **Net Search** (v1.3.6) | The former Research backend (Auto/GitHub/Reddit/DuckDuckGo/SearXNG providers, TTL-cached, authority-ranked, provenance-tagged) exposed as a per-request composer control in Chat and Agent — `/api/net-search` (the `/api/research` route remains as a shim); user-facing naming is Net Search everywhere |
| **Observability** | `app.log` + `tools.jsonl` + `llm.jsonl` with rotation and bounds, crash reports (pruned), diagnostics zip with secret redaction, perf HUD (TTFT / tok/s), engine logs ring |
| **Concurrency** | Copy-on-write live configuration (no data races between Settings patches and active runs), mutex-guarded registries, per-run config snapshots, race-detector-clean core |

# Supported environment

- **Windows 10/11 x64** — the primary target (GUI subsystem exe, console-less, DPI-aware, process-tree kills via `taskkill /T`).
- **Linux x64** — desktop build requires GTK4 / WebKitGTK 6.0 dev libraries; the `headless` build tag runs the same stack without them.
- Hardware probing (CPU/RAM/GPU) uses CIM via PowerShell with a `wmic` fallback (wmic is removed on Windows 11 24H2+).

# Model / runtime integration

- **Local engine**: managed llama.cpp server (bundled CPU build by default; Vulkan offload detected automatically). Models are discovered from the `models/` folder, with GGUF header metadata (architecture, quantization, context length, parameter count) surfaced in the UI.
- **Remote providers**: any OpenAI-compatible endpoint (`SHEYTAN_PROVIDER=remote`). llama.cpp-only request fields (`top_k`, `min_p`, `n_ctx`, `repeat_last_n`) are automatically omitted for remotes; OpenAI-standard sampling fields are sent to both.

# Installation

Windows and Linux x64 portable ZIPs are produced by CI. The Windows package layout (package root `SHEYTAN-LA`):

```text
SHEYTAN-LA-v1.3.2-windows-x64.zip
└── SHEYTAN-LA/
    ├── SHEYTAN-LA.exe           (GUI app + embedded UI + HTTP/WS API)
    ├── SHEYTAN-LA.bat           (portable launcher)
    ├── AI-CONTEXT.md            (the model's operating manual; generated from agent.md)
    ├── BUILD-INFO.txt           (version, platform, commit)
    ├── README.md / LICENSE / SIGNATURE
    ├── models/                  (empty; drop .gguf files here)
    └── workspace/               (empty; portable Coding Lab workspaces)
```

The Linux package (`SHEYTAN-Local-Agent-Linux-x64-v1.3.2.zip`) mirrors this layout under a `SHEYTAN-Local-Agent/` root with a Linux executable. A Windows NSIS installer (`SHEYTAN-LA-v1.3.2-windows-x64-installer.exe`) is produced alongside the portable ZIP.

Unzip anywhere and run the executable. On first launch the app creates its portable data layout next to it:

```text
SHEYTAN-Local-Agent/
├── models/         (drop .gguf files here; mmproj-*.gguf pairs as vision projectors)
├── sessions/       (one JSON per session + activity sidecars)
├── logs/           (app.log, tools.jsonl, llm.jsonl, crashes/, screenshots/)
├── bin/            (auto-downloaded llama-server)
├── attachments/    (content-addressed staged uploads)
├── lab/workspaces/ (isolated coding-lab copies)
├── sandbox/        (governed code execution)
├── recall/         (index + feedback sidecars)
└── config.json
```

The engine binary downloads automatically when the machine is online; drop a prebuilt `llama-server(.exe)` into `bin/` for offline installs. NOTE: llama.cpp stopped publishing prebuilt LINUX binaries — on Linux the automatic download therefore cannot succeed any more; build `llama-server` from source and set `llamaBinPath`, or select the native engine (`engineBackend: "native"`). The engine still scans recent upstream releases and self-heals automatically if prebuilt Linux binaries return.

**Models** are imported first-class: `Import GGUF…` in the Model Picker (or `POST /api/models/import`) validates the GGUF header, streams the file into `models/` atomically (duplicate-safe, source untouched) and selects it through the normal selection flow. Placing a `.gguf` file into `models/` by hand still works. On Windows the engine package carries a recorded backend variant (`cpu` or `vulkan`) — `POST /api/engine/provision {"variant":"vulkan"}` swaps in the real Vulkan engine package transactionally; an explicit Vulkan request that cannot be provisioned fails loudly and never silently installs CPU.

# Configuration

Settings are edited in the UI (`Settings` view) or by patching `config.json` (the API accepts partial JSON objects). Selected keys:

| Key | Default | Meaning |
|---|---|---|
| `provider` | `local` | `local` (managed llama.cpp) or `remote` (OpenAI-compatible endpoint) |
| `engineBackend` | `llama` | `llama` (default, full engine) or `native` (opt-in — since Phase 5 the native engine performs REAL llama-architecture generation and serves plain-text runs when selected AND capable; llama.cpp serves everything else: tools, images, non-llama architectures, unsupported tensors, and the fallback) |
| `nativeEnginePath` | (auto) | override for the `shtn-engine-host` binary location (default `{dataDir}/bin/`) |
| `model` | first `.gguf` | active local model |
| `llamaPort` | 8080 | managed engine port |
| `llamaAutoStart` | true | prewarm engine at launch |
| `llm.numCtx` | 16384 | configured context window — the EFFECTIVE window is min(configured, GGUF model limit, engine limit), so a small model is never over-windowed |
| `llm.*` | — | sampling: temperature, top-p, top-k, min-p, penalties, mirostat, seed, stop |
| `maxIterations` | 25 | agent-loop iteration cap |
| `runTimeoutMinutes` | 60 | per-turn budget (0 = unbounded) |
| `sandboxEnabled` | true | Job-Object resource governor for `codeExec` |
| `thinkingMode` | false | externalized reasoning blocks |
| `recallEnabled` | true | inject relevant past exchanges |
| `continuumEnabled` | true | automatic chapter rollover |
| `labEnabled` | true | Coding Lab tool |
| `researchEnabled` | true | research tool + providers |
| `updateSchedule` | daily | engine update cadence (`off` disables) |

Environment overrides (`SHEYTAN_*`) are documented in `sheytan help`.

# Usage

```bash
# desktop app (default on Windows/Linux)
SHEYTAN-Local-Agent.exe

# headless server + UI in a browser
sheytan-local-agent serve --port 8765

# one-shot headless agent turn
sheytan-local-agent ask "summarize ./notes" --session work

# multi-agent planner/executor/critic pipeline (CLI; the critic judges
# against the run's objective verification verdict, not executor prose,
# and all inter-agent hand-offs are bounded excerpts)
sheytan-local-agent ask "..." --multi

# health, diagnostics, engine update
sheytan-local-agent doctor
sheytan-local-agent diagnostics
sheytan-local-agent update --status
```

REST/WS surface (loopback only): `/api/state`, `/api/engine`, `/api/models`, `/api/sessions`, `/api/config`, `/api/llama`, `/api/run`, `/api/abort`, `/api/attachments`, `/api/tools`, `/api/lab`, `/api/net-search` (shim: `/api/research`), `/api/feedback`, `/ws/activity?sessionId=`.

# Agent / tool capabilities and limits

- The agent loop executes tools **sequentially** (parallel execution is a deliberate non-goal for now).
- Small instruct models may not emit formal tool calls even when tools are advertised — the loop mechanics are covered by deterministic tests.
- The `linux` tool is an honest in-process shell simulator (its description tells the model so); real shell work goes through `shell`/`codeExec`/the Lab.
- Vision requires an `mmproj-*.gguf` projector paired with the active model; screenshots capture the primary display (Windows).
- The Coding Lab's command policy is lexical — it blocks known-dangerous, network, interactive and escape tokens, and the runtime additionally pins `HOME` and scrubs secrets, but it is not a kernel-level sandbox.

# Security model

- The LLM is an **untrusted proposal source**. Runtime policy is authoritative.
- The HTTP/WS API binds `127.0.0.1` and rejects non-approved origins; it has no auth token by design (loopback-only desktop app), so any local process can reach it — treat the machine's user account as the trust boundary.
- Filesystem tools are jailed to the portable data root with traversal + symlink resolution checks; archive extraction is zip-slip-safe with entry/total caps.
- `fetch` enforces public-destination SSRF controls end-to-end: URL validation, DNS pre-resolution, **and** dialed-IP pinning (DNS-rebinding window closed).
- Lab and sandbox processes run with a sanitized environment (API keys/tokens/credentials scrubbed) and a workspace-pinned `HOME`.
- Secrets never appear in API responses (redacted on read), and diagnostics zips redact config, crash logs and structured logs.

# Development

```bash
# backend (no GTK needed)
go build -tags headless ./...
go test -tags headless ./internal/... -count=1
go test -race -tags headless ./internal/agent/ ./internal/llm/ ./internal/api/ ...
go vet -tags headless ./...

# frontend
npm install
npm run typecheck
npm run lint
npm run build        # tsc + vite + sync into web/static

# C++ native engine (optional in Phase 2; requires cmake or plain make)
cmake -S native/engine -B native/engine/build
cmake --build native/engine/build
ctest --test-dir native/engine/build   # engine + protocol + host + gguf + model tests

# release stress suite (gate used by CI and build-and-zip.sh)
go run ./scripts/stress-main stress

# release consistency (package.json -> config.go / config.yml / SIGNATURE / workflow)
node scripts/release-version.mjs --check
```

The desktop (Wails) build for Linux needs `libgtk-4-dev`, `libwebkitgtk-6.0-dev`, `libsoup-3.0-dev`, `pkg-config`; the Windows build is CGO-free and cross-compilable.

After any frontend change, `npm run build` must be run so `web/static` (the embedded assets) stays in sync.

# Testing

- **27 Go test packages** — engine lifecycle (real process spawn/kill via a fake llama.cpp re-exec), agent loop (fake SSE engine: streaming, tool calls, abort, error propagation; generation-router seam), API surface (HTTP-level session/attachment/config/feedback contracts), attachments, chunking, context cache, context plan, continuum, lab (policy, repair loop, verification), memory, recall, research (SSRF/alias contracts), sessions (concurrency, sidecar bounds), termshell, tools, vision, releasegate, runtime, native engine (real C++ host e2e: generation, cancellation, KV/scheduler accounting, fallback signals), plus v1.1.4 regression tests for the config source race, sampling wire format, GGUF parser, stream stall watchdog, zip-slip and escape tokens.
- **Stress suite** — 30 scenarios (hostile prompts, garbage tool args, shell injection, memory/session contracts, release-surface pinning) run in CI and as a release gate.
- **CI** (`.github/workflows/build-desktop.yml`) — audit job (version sync + frontend verify + C++ engine build/ctest), Windows job (tests + GUI exe + console probe + package + zip verification), Linux job (C++ engine build so the Go↔C++ integration tests run for real + tests + stress suite + package), release job (version-agnostic tag gate, integrity-checked publication: ZIP CRC test plus entry-contract verification on both platform ZIPs).

# Development direction (planned — NOT implemented today)

SHEYTAN's validated future direction is documented in full in
`ARCHITECTURE.md` (Part II). Nothing in that direction is implemented
yet; the list below is design intent, not shipped capability:

- **Small, fast, local models as the foundation** — the system is
  designed for `many efficient agents + orchestration + tools + external
  memory + verification`, not one enormous model. Model size is not the
  sole source of system intelligence.
- **Model tiers and hardware-adaptive routing** — a tier ladder
  (Tier 0 smallest → Tier 4 optional high-end multimodal) with
  model-agnostic, capability-based routing by task complexity, modality,
  latency, hardware and budget. Candidate model families (e.g.
  Gemma-class, GLM-class) are examples only, not integrations.
- **The Context Engine** — repository structural index → semantic index
  → hierarchical retrieval → budgeted context builder, so model context
  is treated as *working memory* while external structured storage
  provides *project memory*. The implemented seeds today are the context
  plan, context cache, attachment chunking, recall and continuum.
- **Context budgeting** — "use the smallest sufficient working set",
  extending the existing measured budget (`internal/contextplan`) to
  finer-grained sections.
- **Multi-agent architecture** — specialized low-cost agents (planner,
  coder, researcher, tester, debugger, documentation, reviewer,
  verifier) with per-agent model assignment. Today the runtime is a
  sequential single-agent loop (plus a CLI-only sequential
  planner→executor→critic pipeline); parallelism remains a deliberate
  non-goal until implemented.
- **Artifact-based agent communication** — agents exchange
  `analysis.json` / `patch.diff` / `findings.md` / `test-results.json`
  style artifacts instead of forwarding whole conversation histories.
- **Document editing architecture** — section-aware retrieval →
  structured patch → validation gates (structure, references, forbidden
  deletions, version metadata) with the model never the authority on
  correctness.
- **Durable request queuing** — a persistent job queue between "request
  accepted" and "run started". THIS IS EXPLICITLY NOT SHIPPED TODAY: the
  current contract is one active run per session (a newer `POST /api/run`
  cancels and replaces the active run; an in-flight request can be
  rejected when inputs are invalid; a request submitted while another
  run of the SAME session is active is deliberately aborted-and-replaced
  by design, and a request lost to a process crash is gone — sessions
  persist, queued intents do not). The planned queue adds a persistent
  `jobId`, durable state (ACCEPTED → QUEUED → WAITING_FOR_RESOURCES →
  RUNNING → COMPLETED / FAILED / CANCELED, incl. retry states), FIFO/fair
  per-session scheduling with bounded concurrency, retry/backoff, crash
  recovery, idempotent execution, engine/resource gating and full
  WebSocket/API replay of queue state. See `ARCHITECTURE.md` (Part II)
  for the design direction.

# Version

`v1.2.7` — see `worklog.md` for the complete implementation/remediation history and `agent.md` for the engineering handoff context.

# License

SHEYTAN™ Local-Agent is proprietary software. See `LICENSE` for the governing terms.
