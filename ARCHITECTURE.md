# SHEYTAN-Local-Agent — Architecture Truth Table & Future Direction

> **Status vocabulary used in this document** (see Part III for the rules):
>
> `IMPLEMENTED` · `TESTED` · `PARTIALLY IMPLEMENTED` · `EXPERIMENTAL` ·
> `PLANNED` · `OPTIONAL FUTURE`
>
> A capability is either wired end-to-end and verifiable, or it is clearly
> marked as future/planned. Nothing in Part II of this document is
> implemented today.

## v1.7.0 — Architecture changes shipped in this release

Everything below is `IMPLEMENTED` and `TESTED` (deterministic unit /
race / E2E evidence; test files cited):

1. **Windows transactional rollback hardening**
   (`internal/llm/llama.go`, `internal/llm/variant_rollback_v170_test.go`):
   the variant provisioning transaction stops and reaps the candidate
   engine BEFORE any rollback filesystem mutation
   (`stopCandidateForRollback`: lifecycle-owned SIGTERM → bounded grace
   → Kill → deterministic reap; watchdog cancelled; deliberate-stop
   marker reset). The Windows rollback failure class ("Access is
   denied" under a live candidate locking its own executable tree) is
   structurally eliminated; a package-level probe fires at the EXACT
   rollback instant and proves: candidate stopped, package
   byte-identical, manifest authoritative, LKG restart healthy, no
   orphans, commit path untouched. Rollback branches report explicit
   restart evidence; `StateFailed` only when nothing serves.
2. **Chronological automation layer** (`internal/scheduler/automation.go`,
   `automation_test.go`, `internal/api/automation.go`,
   `automation_api_test.go`): the ONE scheduler extends to the v1.7.0
   task model (pause gate; once / interval / daily / weekly at LOCAL
   time; linked skills; task tools; last run; run history) with
   RunNow / Pause / Resume / CancelRun / UpdateTask / Runs /
   NotifyEvent / ShutdownSettle. The durable-claim guarantee covers
   every schedule kind (claim persisted before execution; no crash
   replay; concurrent runs rejected; missed deadlines run exactly
   once). `/api/automation/*` serves tasks, runs, scoped tools and
   artifacts from persisted state.
3. **Markdown SKILL.md packages** (`internal/skills/markdown.go`,
   `markdown_test.go`): progressive disclosure — metadata-first
   discovery, bodies on match, references on demand; scopes
   global / workspace / task with nearest-scope precedence; validated
   agent-facing `skill_create` (task-scoped); promotion through the
   existing VERIFIED-learning rule; full JSON-skill compatibility and
   convergence into one Skill authority.
4. **Task-scoped custom tools** (`internal/customtools/tasktools.go`,
   `internal/agent/tasktool_e2e_test.go`): same definitions, executors
   and bounds as global custom tools, plus task/run ownership and an
   explicit approval gate; registered into the ONE orchestrator
   registry for the run and unregistered at teardown; real
   model→tool→executor→result loop E2E with honest refusal paths.
5. **First-class task/run artifacts** (`internal/artifacts/taskmeta.go`,
   `taskmeta_test.go`, `internal/tools/artifact_create.go`): durable
   TaskRegistry with full provenance (task, run, source tool, kind,
   path, size, version, hash); atomic path-safe bounded creation;
   per-path version history with archived superseded files (every
   version readable); restart-safe; API serves
   list/create/content/versions with a deny-by-default sandbox CSP
   (HTML inert source; SVG via <img>; no artifact JS in the app
   origin); Markdown rendered first-class in the UI.
6. **The four systems connected** (`internal/runtime/automation.go`,
   `automation_test.go`): the runtime task runner installs the task
   context, task-scoped tools and the bounded skill block around each
   run; the agent loop creates artifacts into the durable registry;
   run outputs carry the artifact provenance echo; genuine event
   emitters fire where subsystems already know the event (boot =
   startup; real file writes = file_change; succeeded clone =
   git_change; failed Lab verification = test_failure). UI:
   `src/AutomationPanel.tsx` + `src/AutomationArtifactViewer.tsx`
   render the chronological timeline from persisted scheduler state
   with full task actions and the artifact viewer.

## v1.6.2 — Architecture changes shipped in this release

Everything below is `IMPLEMENTED` and `TESTED` (deterministic unit /
race / CI-gate evidence; test files cited; the Windows-runtime hand
validation is tracked as ROADMAP NEXT item 1):

1. **Strict engine-variant parsing** (`internal/updater/variant.go`
   `ParseAssetVariant`/`ValidateAssetVariant`,
   `internal/updater/variant_strict_test.go`,
   `internal/api/engine_variant_strict_test.go`): explicit API/user
   variant requests accept exactly cpu/vulkan (+ documented aliases)
   after controlled normalization; empty/unknown values are a
   deterministic 400. The naming layer is fail-closed (an invalid
   variant never yields a CPU asset name). Legacy manifests still read
   back leniently as cpu.
2. **Variant-aware release resolution** (`ResolveDownloadURLForVariant`
   + `FirstWithVariantAsset` + `SetReleaseListForTest`/
   `SetVariantExistsForTest` seams): the newest release is selected
   because its payload CONTAINS the exact variant asset (verified),
   never because a CPU-oriented scan named the tag. Network failures
   are preserved verbatim and never reported as "no prebuilt asset".
3. **Architecture-aware support matrix** (`supportedVariantsFor`):
   windows/amd64 = cpu+vulkan; windows/arm64 = cpu only (upstream
   b11191 serves no win-vulkan-arm64 asset — verified); other
   platforms cpu-only (their upstream packages are .tar.gz, not
   consumable by the zip installer — documented limitation).
4. **GGUF import concurrency lock** (`internal/llm/importlock.go` +
   platform `importlock_*.go`, tests in
   `internal/llm/importmodel_concurrency_test.go` and
   `importlock_unix_test.go`/`importlock_windows_test.go`): exclusive
   per-models-directory lock (in-process mutex + cross-process
   flock/LockFileEx) held across destination choice AND copy. The
   Stat→choose→Rename race (reproduced with -race: two same-filename
   imports overwrote each other's completed file) is closed.
5. **Runtime backend verification in the provisioning transaction**
   (`internal/llm/llama.go` `verifyRuntimeBackendForVariant`, tests in
   `internal/llm/variant_runtime_v162_test.go`): engine-sourced
   evidence (--list-devices enumeration) gates the commit; a
   verification that cannot execute rolls back to last-known-good.
6. **Extra-args sampling gate + persisted repairs**
   (`internal/config/sampling.go` `ValidateExtraArgs` +
   `internal/config/config.go` `persistRepairedSamplingFields`, tests
   in `internal/config/sampling_test.go` and
   `internal/llm/extraargs_gate_v162_test.go`): malformed numeric
   extra arguments fail pre-spawn with zero engine spawns; repaired
   values persist atomically (file-sourced corruption only).
7. **Settings engine-backend surface** (`src/SettingsPerformance.tsx`
   `EngineBackendCard`, `src/engine-backend.ts` + its test,
   `src/api.ts` engineProvision/engineProvisionState): the REAL
   Settings → API → transaction → verification → UI chain.

## v1.6.1 — Architecture changes shipped in this release

Everything below is `IMPLEMENTED` and `TESTED` (test files cited; the
Go suites, the native-engine suite and the 24-test browser E2E suite
cover each claim):

1. **Deterministic sampling gate** (`internal/config/sampling.go`,
   `internal/config/sampling_test.go`,
   `internal/llm/llama.go` `startLocked` gate +
   `internal/llm/sampling_gate_test.go`,
   `internal/api/server.go` patch gate +
   `internal/api/config_sampling_patch_test.go`) — `TESTED` with
   real-spawn evidence. One authoritative validation module defines the
   engine-parser contract for every sampling field (repeat-penalty must
   be finite and > 0; temperature finite ≥ 0; top-p/min-p in [0,1];
   penalties finite; mirostat ∈ {0,1,2}; ctx/batch > 0). It runs at
   FIVE layers: config Load (safe repair to documented default +
   reported note), environment overrides (finiteness-checked), the
   Settings PATCH API (rejected with an actionable 400 before anything
   is stored), the engine boot gate (`validateSamplingForLaunch` —
   BEFORE the ownership lease, the engine download path, the capability
   probe and the compatibility ladder; returns the classified
   `InvalidSamplingConfigError`), and the argument vector itself
   (`argProblems` numeric-range rules, covering user extra args).
   Deterministic config failures never enter the compatibility ladder:
   the reproduction test counted 4 real engine spawns pre-fix for one
   invalid repeatPenalty; post-fix the count is 0.

2. **First-class local GGUF import** (`internal/llm/importmodel.go`,
   `internal/llm/importmodel_test.go`, `internal/api/models_import.go`
   + `internal/api/models_import_test.go`,
   `src/ModelPicker.tsx` + `src/api.ts`) — `TESTED` against the REAL
   in-repo GGUF fixture. `POST /api/models/import` validates the GGUF
   header (same `ReadModelCard` authority as the picker/recommendation
   engine), streams the file in 1 MiB chunks (never whole-model RAM),
   places it atomically (`.import-*.tmp` staging + rename + size
   verification), handles duplicates safely (size + SHA-256 identity →
   duplicate report; different content → fresh `-1` name, never an
   overwrite), and never modifies the source (external model paths are
   a supported posture). `POST /api/models/import/pick` opens the
   native comdlg32 multi-select picker on Windows. The picker chains
   import → refresh → the EXISTING selection state machine.
   `internal/config/modelpaths.go` re-anchors `model`/`draftModel`/
   `visionMmproj` fields that still point inside a retired runtime
   root (legacy AppData root, legacy home directory) onto the
   canonical root — external paths are never touched.

3. **Log discipline for the hot paths** (`internal/api/perf.go`,
   `internal/updater/install.go`) — `TESTED` by the existing suites.
   The per-poll accelerator resolution (recomputed on every
   `/api/perf` poll) logs at INFO only when the resolution CHANGES;
   unchanged recomputations log at DEBUG (Advanced diagnostics keeps
   the evidence). The device-enumeration warning deduplicates the same
   way. The updater's stale-file reporting emits ONE aggregated WARN
   per package swap (per-file detail at DEBUG + in the returned
   diagnostics). The secret policy is unchanged and audited: model
   contents, credentials, tokens, headers and secrets are never logged.

4. **Backend-variant engine provisioning (real Vulkan)**
   (`internal/updater/variant.go` + `variant_test.go` +
   `variant_install_test.go`, `internal/llm/llama.go`
   `UpdateEngineVariantNow`, `internal/api/engine_variant.go`,
   `internal/accelerator/v161_contract_test.go`) — `TESTED`
   deterministically on every platform + a Windows CI gate that
   HEAD-checks the pinned Vulkan asset. The install manifest records
   the package's backend variant (`variant: cpu|vulkan`; v1.6.0-era
   manifests read back as `cpu`). `InstallStagedDeferredWithVariant`
   provisions the REAL upstream Vulkan asset
   (`llama-<tag>-bin-win-vulkan-x64.zip`) through the same
   staged/leased/rollback-safe transaction as every engine update;
   `LlamaServer.UpdateEngineVariantNow` drives the full
   stop → install → start → verify → commit / rollback choreography;
   `POST /api/engine/provision` exposes it. An explicit VULKAN request
   that cannot be provisioned fails LOUDLY (before stopping anything) —
   never a silent CPU install. AUTO keeps the evidence gates: Vulkan
   is selected only with engine enumeration or a measured offload
   line; `executionVerified` stays false for DLL-presence-only
   evidence.

5. **Conservative mixed licensing + governance files** (`LICENSE`,
   `LICENSE-APACHE`, `LICENSE-PROPRIETARY`, `LICENSE-MAP.md`,
   `NOTICE.md`, `CONTRIBUTING.md`, `SECURITY.md`,
   `internal/brand/brand.go`) — the classification authority is
   `LICENSE-MAP.md`: open ONLY by explicit designation
   (`internal/humanize/`, Apache-2.0 + SPDX headers), proprietary by
   default for everything else. The model classifies actual material
   (code, docs, assets, implementations), never abstract ideas.

## v1.6.0 — Architecture changes shipped in this release

Everything below is `IMPLEMENTED` and `TESTED` (test files cited; the
Go suites and frontend unit suites cover each claim):

1. **Startup maintenance gate** (`internal/api/maintenance.go`,
   `internal/api/maintenance_test.go`,
   `internal/llm/maintenance_stop_test.go`) — `TESTED`. One
   authoritative coordinator runs the engine maintenance decision (and
   any transactional update, through the existing
   `LlamaServer.UpdateEngineNow` transaction and `updater`
   authorities) BEFORE any engine process is started or prewarmed.
   `EnsureSetup` arms the gate; the model prewarm
   (`prewarmAfterGate`) and the scheduled updater's first pass
   (`updater.RunScheduledAfter`) both synchronize on the gate's done
   channel. Model-first is preserved (no selection → install-only,
   never a start). Terminal states: `READY_FOR_PREWARM`, `DEFERRED`
   (off / not due / offline), `FAILED` (update failed, installed engine
   still valid → startup continues with it), `BLOCKED` (update failed
   AND installed engine fails static validation → prewarm refused).
   Exposed at `GET /api/maintenance`.

2. **Top-level Chat/Agent views** (`src/workspace.ts`,
   `src/App.tsx`, tests in `src/workspace-v136.test.ts`) — `TESTED`.
   `WorkspaceView` gains `chat`; `viewModeBinding` binds the
   conversation-space mode to the active view (shared layers never
   rebind it). The Chat|Agent segmented selector
   (`src/AgentHeader.tsx` ModeSwitch) is REMOVED. The store's
   per-mode session machinery is unchanged — independent histories,
   per-mode active sessions, cross-mode references, one shared runtime.
   v1.6.0 repair: the navigation exposes REAL tab semantics on the
   same buttons (`role="tablist"` + `role="tab"` + `aria-selected`,
   replacing `aria-pressed`); the initial view resolves through ONE
   deterministic path at first render — explicit URL hash >
   remembered workspace view > Chat (`resolveInitialView`) — and the
   store's boot mode resolves through the SAME function, so view and
   mode never disagree at first paint; every view owns an explicit
   hash (`#chat` … `#settings`, no agent→empty special case; root
   normalizes to the resolved view's hash; stale hashes resolve to
   Chat).

3. **Automatic long context** (`src/SettingsPerformance.tsx`
   ContextCard, `src/AgentHeader.tsx`) — `TESTED` (typecheck + unit
   suites; backend context resolution unchanged and covered by the
   existing sessioncontext tests). The editable context controls are
   removed; a read-only `LONG CONTEXT · AUTOMATIC` indicator replaces
   them. Historical `numCtx` / `contextTokens` values keep loading as
   internal legacy values.

4. **Custom tools** (`internal/customtools/`,
   `internal/api/customtools.go`,
   `internal/agent/customtools_e2e_test.go`,
   `internal/api/customtools_api_test.go`) — `TESTED`. Definitions
   persist atomically under `<DataDir>/custom-tools`; validation
   enforces the schema contract (object-root, basic types, HTTPS-only,
   permissions, bounds); the executor enforces disabled rejection,
   run-time re-validation, bounded timeout/output, cancellation,
   controlled child environment, and secret-never-model-visible.
   Local commands run under per-invocation process-tree ownership
   (v1.6.0 repair): on Windows each command is assigned to a
   `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` Job Object
   (`proctree_windows.go`, the established `internal/sandbox` pattern)
   so timeout/cancellation terminates the COMPLETE descendant tree —
   not merely the direct child — and the inherited pipes close
   promptly (verified: the timeout/cancellation/output-cap tests pass
   on Linux and the package cross-compiles and vets clean under
   `GOOS=windows`; the authoritative Windows run is CI-owned). The
   non-Windows tracker is an explicit no-op preserving the passing
   Linux/macOS semantics.
   Registration goes through the ONE orchestrator registry
   (`Register`/`Unregister`); `/api/tools` carries `source:
   builtin|custom`. The agent-loop E2E test proves the full path
   (model/tool protocol handling through a deterministic HTTP fake
   model — an honest scope statement, not a claim of a native local
   model deciding to call the tool).

5. **Honest engine diagnostics** (`internal/llm/llama.go`,
   `internal/llm/capability.go`) — `TESTED`. Compatibility fallbacks
   record concrete reasons; a genuinely unknown reason reports
   `unknown` (never "unrecorded").

6. **Maintenance-aware UI** (`src/MaintenanceBanner.tsx`,
   `src/api.ts`) — `TESTED` (typecheck/lint; renders only real phases
   from `/api/maintenance`).

`PARTIALLY IMPLEMENTED` (honest scope): AI-assisted tool builder
(deferred; the manual builder is complete) and a single unified
Downloads Center page (per-surface truthful progress shipped instead;
the downloader remains the one authority).

This document has two jobs:

1. State **what SHEYTAN actually implements today**, with evidence — so no
   reader (human or AI agent) mistakes a roadmap for a runtime.
2. Record the **validated architectural direction** for SHEYTAN's future AI
   runtime — so future development does not lose the design intent.

Related documents:

| File | Role |
|---|---|
| `README.md` | user-facing overview of the shipped product |
| `agent.md` | engineering handoff for the next AI agent working on this repo |
| `worklog.md` | dated remediation / release history (evidence log) |
| `internal/aicontext/AI-CONTEXT.md` | the runtime briefing prepended to the model's system prompt (a shipped product file, not developer docs) |
| this file | implementation truth table + future architecture |

The core principle that governs both current and future work:

> **The model proposes. The tools execute. The laboratory verifies.**

---

# Part I — What is implemented today

Every row below was verified against source code, tests and the stress
suite (Phase 7 rows at release `v1.1.6`; earlier rows audited at
`v1.1.4`/`v1.1.5` — see `worklog.md` for the audit method).
"TESTED" means covered by `go test` packages and/or the 30-scenario
stress suite; see the exact commands in `agent.md` §10.

| Subsystem | Package(s) | Status | Notes |
|---|---|---|---|
| Managed llama.cpp engine lifecycle (download → launch → health → supervise → bounded auto-restart → scheduled updates) | `internal/llm`, `internal/updater` | IMPLEMENTED + TESTED | 3 restarts/episode with 1/2/4 s backoff; downloads context-bounded (10 min) and size-capped (2 GiB) |
| **v1.3.6 Engine ownership + transactional provisioning** (ONE authority: staged download → static candidate validation → atomic directory swap → installed-binary SHA-256 verification → commit/rollback; engine-owned exclusive update operation holding the lifecycle lock; process-wide install lock) | `internal/updater` (`install.go`), `internal/llm` (`UpdateEngineNow`) | IMPLEMENTED + TESTED (Linux suite) | last-known-good engine preserved on every failure path; obsolete staging cleaned; `TestStartAndUpdateCannotInterleave`, `TestUpdateEngineTransactionCommitsVerifiedBinary`, `TestUpdateEngineFailurePreservesLastKnownGood` |
| **v1.3.6 Identity-proven adoption** (a port-serving process is adopted ONLY when the OS attributes it to the managed engine binary; foreign processes refused with recorded evidence, never killed) | `internal/proc` (`identity*.go`), `internal/llm` (`adoption.go`) | IMPLEMENTED + TESTED (Linux suite; Windows paths compile-gated) | GetExtendedTcpTable + QueryFullProcessImageName (Windows); /proc/net/tcp + /proc/<pid>/exe (Linux); no cmd.exe/PowerShell/tasklist parsing anywhere |
| **v1.3.6 Loader-failure classification + preflight** (first-class 0xC0000139-class diagnostics; deterministic loader failures break the compatibility ladder; preflight = exists → arch → PE import closure (debug/pe) → bounded --version, before any model launch) | `internal/llm` (`loaderclass.go`, `preflight.go`, `diagnostics.go`) | IMPLEMENTED + TESTED | classification is code-first with a textual NTSTATUS fallback (Unix truncates exit codes); evidence only — never names a DLL defective without proof; surfaced via `/api/engine` `diagnostics` |
| **v1.3.6 System Engine Discovery** (Tier 0 managed+cache, Tier 1 cheap locations, Tier 2 bounded background scan; non-destructive validation; SHA-256-dedup import into the managed directory with provenance) | `internal/engdiscovery` | IMPLEMENTED + TESTED | discovery cache persisted at `<DataDir>/discovery-cache.json`, invalidated by size/mtime; Tier 2 never runs on the startup path |
| **v1.3.6 Canonical data root** (install-local `<AppRoot>\data`; legacy AppData root migration reusing the verified merge machinery; engine bundle migrates as a unit — never mixed; explicit overrides never touched) | `internal/config` (`migrate_appdata.go`), `packaging/nsis/installer.nsi` | IMPLEMENTED + TESTED (Linux suite; installer behavior Windows-owned) | installer creates the data tree and grants Users modify (icacls); legacy machine `SHEYTAN_DATA_DIR` env var deleted on install |
| **v1.3.6 Net Search** (Research workspace layer removed; per-request composer control in Chat and Agent; server-side enforcement via the tool policy; same research service; `/api/net-search`) | `src/ComposerControls.tsx`, `src/workspace.ts`, `internal/agent`, `internal/api` | IMPLEMENTED + TESTED | explicit intent only — never inferred from message text; recorded in turn telemetry (`NetSearch`); stale-version UI fallback replaced by build-time `__APP_VERSION__` (spec contract `version-contract.test.ts`) |
| LLM client: streaming, retries, stall watchdog, sampling wire format | `internal/llm` | IMPLEMENTED + TESTED | no overall stream timeout by design; 5-min zero-byte stall watchdog pinned by `TestStreamStallWatchdogAbortsQuietStream` |
| Agent loop (plan → tool calls → observations → verify → answer) | `internal/agent` | IMPLEMENTED + TESTED | **sequential** tool execution; iteration cap (default 25) + per-run time budget (default 60 min) |
| Tool registry (17 tools when all features enabled) | `internal/tools`, `internal/lab`, `internal/research`, `internal/memory`, `internal/sandbox` | IMPLEMENTED + TESTED | `shell`, `files`, `codeExec`, `webSearch`, `git`, `browser`, `dataAnalysis`, `json`, `archive`, `fetch`, `diff`, `screenshot`, `linux`, `coding_lab`, `research`, `memory` + sandbox override |
| Coding Lab (isolated workspace, lexical command policy, verification gates, bounded repair loop, snapshot-before-promote) | `internal/lab` | IMPLEMENTED + TESTED | policy is lexical + env-pinned, not a kernel sandbox (documented limitation) |
| Code-exec sandbox governor (Windows Job Objects) | `internal/sandbox` | IMPLEMENTED + TESTED | memory/CPU configurable, fail-closed default ON |
| Custom-tool command process-tree ownership (Windows Job Objects, per invocation) | `internal/customtools` | IMPLEMENTED + TESTED | KILL_ON_JOB_CLOSE + cancellation terminates the job; non-Windows is an explicit no-op (v1.6.0 repair) |
| Attachments (content-addressed streaming staging, caps, normalization, bounded retrieval with provenance headers and measured stats) | `internal/attachments` | IMPLEMENTED + TESTED | sha256 staging streamed while hashed (RAM ≈ 16 KiB head + 128 KiB buffer, not the file size), symlink-safe; retrieval reads each object ≤1× per call with a 32 MiB retention cap and byte-range fallback (v1.1.5 Phase 3) |
| Chunking (shared provenance chunk engine: paragraph-boundary splitting with full metadata, byte budgets, head+tail windowing, history windowing) | `internal/chunking` | IMPLEMENTED + TESTED | `ChunkText` (processing version v2): deterministic IDs, byte ranges, token estimates, total counts, optional overlap, UTF-8-safe splits; **not** structural/semantic repository chunking — see Part II §4 |
| Context plan (explicit budget: system / tools / recall / attachments / history sections with priorities and pressure) | `internal/contextplan` | IMPLEMENTED + TESTED | the seed of the future budget taxonomy — see Part II §5 |
| Context cache (content-keyed LRU, TTL, bounds, single-flight coalescing, oversized-entry guard, measured counters) | `internal/contextcache` | IMPLEMENTED + TESTED | processing version v4; concurrent same-key computes are coalesced; a value above the per-entry bound is rejected, never retained |
| Memory (M1–M7 trust classes, persistent JSONL store, append-aware parsed cache, copy-free search) | `internal/memory` | IMPLEMENTED + TESTED | external material quarantined; appends/deletes fold into the cache incrementally (no full re-parse per write) without changing any trust rule |
| Recall (BM25 over past turns, cached corpus statistics, recency boost, 👍/👎 feedback steering) | `internal/recall` | IMPLEMENTED + TESTED | per-capsule terms + distinct counts cached; per-query scoring allocation-free; feedback wired via `/api/feedback` since v1.1.4 |
| Continuum chapter rollover | `internal/continuum` | IMPLEMENTED + TESTED (wired post-run) | deterministic `Distill` runs in production; the LLM `Enhance` pass is implemented and unit-tested but has **no production caller** (deliberate future option) |
| Research (auto/GitHub/Reddit/DuckDuckGo/SearXNG, TTL cache, provenance) | `internal/research` | IMPLEMENTED + TESTED | SSRF/alias contracts tested |
| Vision (mmproj projector pairing, image classification, screenshot capture) | `internal/vision`, `internal/screen` | PARTIALLY IMPLEMENTED | pairing logic implemented + unit-tested; not yet exercised with a real projector model (known limitation) |
| Multi-agent pipeline (planner → executor → critic → summarizer) | `internal/multiagent` | PARTIALLY IMPLEMENTED | wired via CLI `ask --multi` only; **not** exposed through the HTTP API or UI; runs **sequentially** on one model |
| Engine capability adapter (CLI-contract detection, pre-launch argument validation, classified startup failures, per-option surgical repair, verified profile persistence) | `internal/llm/capability.go` | IMPLEMENTED + TESTED (Phase 7) | `--help` parsing preferred, release-tag fallback; the historical `--flash-attn`/`--cache-reuse` argument regression is locked out by `TestSpeedArgsNeverProducesFlashAttnCacheReuse` and the end-to-end `TestEngineStartRepairsHistoricalRegression`; the 4-level compat ladder remains only as the last resort |
| Model capabilities (one card per loaded model: architecture, quant, params, GGUF context limit, tokenizer family, chat template, multimodal pairing, native verdict, RAM/VRAM estimates, recommended context + generation budget) | `internal/llm/modelcaps.go` | IMPLEMENTED + TESTED (Phase 7) | derived from the real GGUF header; the effective context is min(configured, model limit, engine limit) and the launcher passes it as `--ctx-size` |
| Preflight context budget pipeline (fit-guaranteed requests: effective window → output reserve → safety margin → tools → system → project intel → skills → recall → attachments → history; automatic degradation ladder; refusal without an engine call when impossible) | `internal/agent`, `internal/contextplan`, `internal/llm` | IMPLEMENTED + TESTED (Phase 7) | optional blocks composed-then-injected only when the plan keeps them; the v1.1.3 "fixed-section overflow" warning is now an automatic repair path (`TestPreflightImpossibleBudgetNeverCallsEngine`, `TestPreflightToolsetReductionOnOverflow`, `TestPreflightCompactBriefingOnOverflow`) |
| In-loop fit guard (mid-turn tool-result elision/bounding so the growing request stays inside the ceiling) | `internal/agent` | IMPLEMENTED + TESTED (Phase 7) | the freshest tool result is never fully elided (bounded truncation instead); structure preserved |
| Verified startup state machine (health → verify served model → verify context capability, honestly recorded) | `internal/llm` | IMPLEMENTED + TESTED (Phase 7) | `/v1/models` + `/props` probes; unverifiable claims are never claimed |
| Dynamic toolsets (capability groups + task-signal selection + pressure-driven reduction) | `internal/toolsets` | IMPLEMENTED + TESTED (Phase 7) | core tools (files/shell/memory) always offered; selection is deterministic and logged on the plan |
| Skills subsystem (identity/trigger/procedure/tools/prerequisites/verification/failure-modes/evidence; load-on-demand injection) | `internal/skills` | IMPLEMENTED + TESTED (Phase 7) | VERIFIED-LEARNING RULE enforced: promotion requires an objective `verified` verdict; unverified executions never become skills |
| Specialist agent consultations (researcher/architect/coder/debugger/tester/security) | `internal/multiagent/specialists.go` | PARTIALLY IMPLEMENTED (Phase 7) | complexity-gated (score ≥ 2), ≤ 2 specialists per run, each one bounded LLM call; advisory only — the critic still requires objective evidence; per-role separate models and persistent agent society remain Part II |
| Programmatic tool pipelines (model-declared bounded stage plans executed deterministically) | `internal/pipeline`, `internal/agent/pipeline_tool.go` | IMPLEMENTED + TESTED (Phase 7) | ≤ 12 stages, per-stage timeout (≤ 600 s), 64 KiB output cap per stage, observable results; the `pipeline` tool is registered like any other tool |
| Computer-use abstraction (observe → inspect → act → observe → verify, deny-by-default risk policy) | `internal/computer` | IMPLEMENTED foundation + TESTED (Phase 7) | read-only/interactive allowed, destructive denied without an explicit gate; wired over the existing browser/screen capabilities in a later phase |
| MCP extension bridge (stdio JSON-RPC adapter, guarded registration pipeline) | `internal/mcp` | IMPLEMENTED foundation + TESTED-ready (Phase 7) | deny-by-default tool permissions, schema validation, 120 s call timeout, 32 KiB result cap; OFF by default — no production wiring yet |
| Event/scheduler foundation (event taxonomy, bounded task runs, persisted reports, memory summaries) | `internal/scheduler` | PARTIALLY IMPLEMENTED (Phase 7) | manual/startup/timer triggers implemented (timer floor 5 min); file-change/git-change/test/CI/build-failure emitters are declared but not implemented |
| Context-effectiveness telemetry (tokens added/removed, retrieval latency/hits, compression ratio, pressure, tool success, verification verdict) | `internal/ctxtelemetry` | IMPLEMENTED + TESTED (Phase 7) | one bounded JSONL record per turn; observability only — never decides |
| Self-improvement loop (prediction → outcome → verified tactic lifecycle) | `internal/improve` | IMPLEMENTED foundation + TESTED (Phase 7) | candidates never guide planning; activation requires TWO independently verified predictions; one verified contradiction retires the tactic; the laboratory remains authoritative |
| Config source (copy-on-write live configuration) | `internal/config` | IMPLEMENTED + TESTED | race-detector-clean; `Source` is the only sanctioned mutation path |
| Observability (logs, rotation, crash reports, diagnostics zip with redaction, perf HUD) | `internal/logging`, `internal/resources` | IMPLEMENTED + TESTED | |
| Sessions (persistence, concurrency, sidecars) | `internal/sessions` | IMPLEMENTED + TESTED | |
| Release engineering (single-source version sync, CI gates, zip-slip-safe updater) | `scripts/release-version.mjs`, `.github/workflows/build-desktop.yml`, `internal/updater` | IMPLEMENTED + TESTED | `package.json` is the single source of truth for the version |
| Frontend (React 19 + TS + Vite, embedded via `go:embed`) | `src/`, `web/static` | IMPLEMENTED + TESTED | `npm run build` must be re-run after frontend changes |
| **LLM backend contract** (v1.1.5 Phase 1): engine-agnostic interface — Start/Stop/Health/LoadModel/UnloadModel/Generate/StreamGenerate/Cancel/ModelInfo/HardwareInfo/Metrics — plus generation-backend selection with automatic llama.cpp fallback | `internal/llm` (`backend.go`, `llamabackend.go`) | IMPLEMENTED + TESTED | `LlamaBackend` delegates to the existing LlamaServer+Client paths (no behavior change); since Phase 5 selection resolves to the NATIVE backend when it is selected and actually generation-capable (see the Phase 5 row below) |
| **SHEYTAN Native Engine foundation** (v1.1.5 Phase 1): supervised `shtn-engine-host` subprocess (spawn → protocol/ABI handshake → health → ready → bounded auto-restart), length-prefixed JSON IPC, platform-neutral hardware profile (native probe + sysinfo merge), native metrics (measured values only), C++ engine with a narrow C ABI (create/destroy/health/hwinfo/metrics) and its own test suite | `internal/native/engine`, `native/engine/` | IMPLEMENTED + TESTED | Superseded by Phase 5: the same subprocess now serves REAL native generation (see below); supervision/protocol/bounds unchanged |
| **Native GGUF model loading** (v1.1.5 Phase 2): bounds-checked, overflow-safe C++ GGUF reader (magic/version/metadata/tensor-table validation, hostile-input bounds, mmap-backed lazy access), `LoadModel`/`UnloadModel` with replace semantics and clean resource release, real metadata extraction (architecture, parameter count, context, vocab, embedding, layers, quantization, tensor count, file size), load-time memory plan, model states `unloaded/loading/loaded/failed` with host-restart resets, wire ops and the `llm.ModelInfo` mapping with additive fields; Phase 5 adds the llama-graph VALIDATION verdict (`generationCapable` + inspectable reason) at load time | `internal/native/engine` (`model.go`, `protocol.go`, `backend.go`), `native/engine/src/{gguf,model,llama}.*`, `native/engine/include/shtn/*` | IMPLEMENTED + TESTED | Loading alone still does not serve generation — but since Phase 5 a VALIDATED llama model DOES: `GenerationCapable()` is true for alive + validated models and generation routes natively (see the Phase 5 row) |
| **Native engine foundation primitives** (v1.1.5 Phase 4): real GGUF-backed tokenizer (BPE/Unigram/WPM with merges, special tokens, BOS/EOS/UNK, bounded encode/decode — materialized by re-walking the mmap on demand), real KV-cache, real bounded scheduler, real sampling primitives (greedy/temperature/top-k/top-p/repetition penalty/seedable RNG), streaming UI coalescing (rAF-boundary batching — one setState per frame regardless of token rate), frame-budget perf HUD | `internal/native/engine` (`tokenizer.go`), `native/engine/src/{tokenizer,kv_cache,scheduler,sampler}.*`, `src/store.ts` (coalescer), `src/perf-hud.ts`, `src/main.tsx` | IMPLEMENTED + TESTED | Superseded/completed by Phase 5: the KV cache now holds real fp16 data populated by the forward pass; the scheduler runs a real worker thread; the sampler consumes real logits. UI coalescing contract unchanged. Phase 5 measured the HUD claims honestly (no guaranteed-FPS claims) |
| **REAL native transformer inference + generation** (v1.1.5 Phase 5): llama-architecture forward pass in portable C++ (embeddings → per-layer RMSNorm → Q/K/V matvec → RoPE NORM → causal GQA attention over the fp16 KV cache → output projection + residual → RMSNorm → SwiGLU FFN + residual → final norm → logits; double accumulators; scratch reuse; norm weights dequantized once), tensor access layer with row dequant for F32/F16/Q4_0/Q4_1/Q5_0/Q5_1/Q8_0 (anything else fails explicitly), generation runner (engine-tokenized prompt, context-bound REJECT policy, per-request KV reset, prefill + decode loop with per-token cancellation observation, sampler over real logits, EOS/max/context/cancel stops with EOS never emitted into text, UTF-8-complete chunk emission, monotonic-clock metrics), REAL scheduler worker (single slot, real active/completed/cancelled/failed counts), host generation lanes with streamed event frames + real cancel (protocol/ABI v4), Go ipcConn streaming (event channel per request, backpressure, cooperative-cancel abandon path), Backend Generate/StreamGenerate over llm.StreamEvent/PerfStats, generation router wired through the orchestrator (native when selected+capable+plain-text; llama.cpp otherwise; pre-first-token fallback logged), numerical correctness pinned against an independent Python reference (staged values + final logits within tolerance), e2e acceptance through the real Go↔C++ boundary | `native/engine/src/{tensor,llama,forward,generate}.*`, `native/engine/src/{kv_cache,scheduler,engine}.*` (upgraded), `native/engine/tests/{test_tensor,test_forward,test_generate}.cpp` + `tests/reference/make_fixture.py`, `internal/native/engine/{generation,backend,runtime,protocol}.*` (upgraded), `internal/runtime/runtime.go` (router), `internal/agent/orchestrator.go` (seam) | IMPLEMENTED + TESTED (12 C++ suites + Go fake-host suite + 5 real-host e2e tests + router tests, all green) | Support is NARROW and honest: llama architecture only; F32/F16/Q4_0/Q4_1/Q5_0/Q5_1/Q8_0 tensors only; rope.freq_scale 1.0 only; plain role-labeled prompt (no chat-template interpretation); tools/images stay on llama.cpp; measured SLOWER than llama.cpp on the fixtures (pp 56810 vs 17695 tok/s, tg 28643 vs 20708 tok/s on the tiny fixture — see worklog Phase 5 table; no native-speed claim made) |

| **Agent reliability core** (v1.1.5 Phase 6): failure classification (14 evidence-driven categories, category-specific repair hints appended to tool results — the model re-plans against the diagnosis), LoopGuard repeat detection (signature = tool + whitespace-normalized args; first repeat warns with a strategy-change note, third is refused with an instruction to produce the final answer; blocked calls not counted so stats track executions), run-level budgets (200 tool calls, 30 min wall clock), failure tally + per-tool call stats on RunResult | `internal/agent` (`reliability.go`), `internal/agent/orchestrator.go` (wiring) | IMPLEMENTED + TESTED | classification surfaced as `failure` activities; every refusal/warning visible in the timeline |
| **Run-level verification** (v1.1.5 Phase 6): EvidenceCollector watches tool traffic; lab verify actions and explicit build/test outcomes record evidence; verdict `verified / partially_verified / failed / not_verified` on RunResult + a `verification` activity on completion; no evidence = not_verified (model prose is never proof) | `internal/agent` (`verification.go`) | IMPLEMENTED + TESTED | consumed by the multi-agent critic (see below) and the UI timeline |
| **Safe anchored edits** (v1.1.5 Phase 6): Coding Lab `read_file` (bounded line-numbered views, offset/limit, binary sniff, 2000-line/256KB caps) + `edit_file` (anchor must occur EXACTLY ONCE — stale or ambiguous anchors refused; atomic same-dir temp+fsync+rename writes; post-write byte-equality verification; every edit invalidates task verification) | `internal/lab` (`safeedit.go`, `tool.go`) | IMPLEMENTED + TESTED | the shell-quoting-free coding path: read_file → edit_file → verify |
| **Persistent project intelligence** (v1.1.5 Phase 6): per-project measured facts (languages, build system + commands, layout, entry hints), Lab-VERIFIED build/test commands recorded against the task's source root (verified beats inferred; re-observation never clobbers), bounded dedup lesson FIFO, compact Card (≤4KB) injected before the last user message | `internal/projectintel`, `internal/lab` (IntelRecorder), `internal/agent/orchestrator.go` (card seam), `internal/runtime/runtime.go` (wiring) | IMPLEMENTED + TESTED | bounded structural walk (4000 files, depth 12, vendor/build dirs skipped) |
| **Repository Intelligence slice 1** (v1.3.4, ROADMAP v1.4): persistent per-root repository index (files, languages, roles, symbols, raw imports, resolved dependency edges, deterministic test/source links, Git-aware + recency relevance metadata), incremental bounded updates (mtime+size fast path + bounded digest, resumable work budget), hybrid evidence-ranked search with verified/inferred evidence distinction | `internal/repoindex`, `internal/runtime/runtime.go` (wiring + trim), `internal/agent` (repo-evidence seam + tier gate), `internal/api/repoindex.go`, `internal/toolsets`, `src/RepositoryIndexCard.tsx` | IMPLEMENTED + TESTED | bounded structural scan (8 000 files, depth 24, 512 KB parse cap, 400 symbols / 100 edges per file, 3 s resumable budget) |
| **Evidence-based multi-agent critique** (v1.1.5 Phase 6): the multi-agent critic receives the run's measured verification verdict and a bounded excerpt of the executor result; the critic prompt forbids `satisfied: true` without objective evidence for verifiable tasks; maxIter<1 normalization bug fixed (the critic previously never ran) | `internal/multiagent` | PARTIALLY IMPLEMENTED (CLI `ask --multi` only; sequential, single model) | inter-agent hand-offs bounded (6KB, rune-safe, marked) |

Explicit non-goals of the **current** runtime (do not mistake these for
missing features):

- The agent loop executes tools **sequentially**. Parallel tool execution
  is a deliberate non-goal today.
- The HTTP/WS API is loopback-only with no auth token; the OS user account
  is the trust boundary.
- The Lab command policy is lexical (denylists + env pinning), not a
  kernel-level sandbox.

## I.8b — v1.1.7 surfaces: compatibility transparency, live telemetry, in-app diagnostics (IMPLEMENTED + TESTED)

v1.1.7 does not change any architecture — it makes the EXISTING runtime
visible and honest where it previously was silent. Verified rows:

| Surface | Package(s) | Status | Notes |
|---|---|---|---|
| Compatibility-mode reason + bounded retry-up | `internal/llm` (`capability.go` `shouldRetryFullSpeed`/`compatReasonFromError`, `llama.go` boot ladder), `internal/config` (`engineCompatReason`/`engineCompatAt`) | IMPLEMENTED + TESTED | the boot records WHY the ladder descended (classified kind + offending option); the retry-up gate fires only when the reason is option-class AND a verified capability profile postdates the recorded downgrade AND the level-0 profile passes `argProblems`; a failed non-option retry stamps a fresh downgrade time which re-blocks the gate until the profile changes again — no rediscovery loops, no extra restarts in the common path |
| Live performance snapshot `GET /api/perf` | `internal/api` (`perf.go`, `perf_windows.go`, `perf_linux.go`, `perf_other.go`), `internal/llm` (`perftracker.go`, client hook) | IMPLEMENTED + TESTED | real measurements only: CPU % via `GetSystemTimes`/`/proc/stat` deltas, RAM via `GlobalMemoryStatusEx`/`meminfo`, GPU/VRAM via `nvidia-smi` when installed, prompt tok/s + decode tok/s + TTFT from the streaming client's `tokenTimer` recorded into a bounded 16-sample ring, context usage from the last generation vs the verified window, backend/model/engine state, the active `CompatInfo` and hardware+model+capability-derived recommendations. Unmeasurable values are omitted (UI renders N/A) — never fabricated. No new telemetry pipeline |
| In-app log surface `GET /api/logs` | `internal/api` (`logs.go`), `internal/logging` (`RecentParsed`) | IMPLEMENTED + TESTED | reads ONLY the existing app-log ring (512-line cap — bounded memory by construction); entries are parsed (time/level/category/message) and REDACTED before leaving the process; `redact()` gained inline scrubbing (quoted `apiKey/token/…` values, Bearer tokens, `sk-…` prefixes) so secrets typed mid-sentence never render |
| Connection diagnostics `GET /api/netcheck` | `internal/netcheck` (`Diagnose`, `classifyDiag`), `internal/api` | IMPLEMENTED + TESTED | ONE bounded check per call: OS DNS resolution, two timed HTTPS samples (the first includes TLS handshake — only warm-path degradation flags "Unstable"), second-endpoint reachability, plus the existing multi-strategy probe as an honest fallback for proxied machines; maps to Excellent/Good/Unstable/Slow/Offline with the first meaningful failure reason; informational only, never a gate |
| Options UI sections, option metadata, tooltips | `src/SettingsPanel.tsx`, `src/SettingsPerformance.tsx`, `src/settings-shared.tsx`, `src/settings.css` | IMPLEMENTED | same card system/theme/scroll architecture; six tabs (General/Performance/Generation/Tools/Network/Logs); delayed 400 ms CSS tooltips (1–2 sentences); `Supported/Unsupported/Recommended/Restart required` chips on engine options; the restart-after-save condition now covers every engine-affecting key (speed settings previously saved but left inert until a manual restart) |
| Concise tool labels | `internal/tools/shortdesc.go`, `internal/research/tool.go`, `internal/lab/tool.go`, `internal/memory/memory.go`, `/api/tools` | IMPLEMENTED | one-line `ShortDescription` per tool for the Options UI; the FULL `Description()` remains the model-facing operational spec (removing it would strip the JSON action syntax the agent needs); `/api/tools` carries both (`description` short, `detail` full) |

## I.8c — v1.1.8 surfaces: Chat/Agent separation, model picker, release-identity gate (IMPLEMENTED + TESTED)

v1.1.8 does not change any architecture either — it separates the two
workload personas at the UI layer, upgrades model selection, and makes
the CI release identity derivation-only. Verified rows:

| Surface | Package(s)/File(s) | Status | Notes |
|---|---|---|---|
| Release identity single source of truth | `scripts/release-version.mjs` (`--env` mode), `.github/workflows/build-desktop.yml` | IMPLEMENTED + TESTED | `package.json` is canonical and the identity is VERSION-ONLY: the audit job resolves it at runtime (step `id: identity`) via `release-version.mjs --env` and exports `APP_VERSION` as its single job output consumed by `build-windows`, `build-linux` and `release` through `needs`-outputs. The script validates plain semver, synchronizes every derived surface and SHAPE-checks the workflow for the `--env` derivation marker. A hardcoded version or codename constant in the workflow is structurally impossible |
| Chat/Agent mode separation | `src/store.ts` (`WorkspaceMode`, `mode`, `setMode`, localStorage `sheytan.mode`), `src/AgentHeader.tsx` (`ModeSwitch`), `src/AgentBody.tsx` | IMPLEMENTED | UI-only concern: one segmented switch, exactly one active mode, same sessions/model/engine runtime for both. Chat: model rail + stream + composer (Send), no runtime panel/context pills/telemetry. Agent: runtime panel, context controls, activity, performance strip. No second runtime, no duplicated state |
| Model picker | `src/ModelPicker.tsx`, `src/AgentBody.tsx` | IMPLEMENTED | replaces the stream while open (automatic while no usable model exists); per-model cards show only backend-measured facts + an honest RAM-based classification (Recommended/Compatible/Limited) derived host-side from `sysinfo` vs `estimatedMemoryBytes`; actions limited to real APIs (Use/Open folder/Refresh/Details — no Remove, no deletion API) |
| Per-model capability facts on `/api/models` | `internal/api/server.go` (`modelInfo`, `modelCardFor`) | IMPLEMENTED | `multimodal`, `nativeBackend`, `chatTemplate`, `nativeReason`, `estimatedVRAMBytes` per model from `llm.ResolveModelCapabilities`; the capability resolution now shares the header card's bounded path+size+mtime cache (previously re-parsed per model per poll), and the host memory probe is hoisted out of the per-model loop |
| Performance strip | `src/PerfStrip.tsx` | IMPLEMENTED | compact 4 s polling of the EXISTING `/api/perf` snapshot into the Agent surface; N/A for anything unmeasured; no new telemetry pipeline |
| Settings tabs: Models / Advanced | `src/SettingsPanel.tsx` | IMPLEMENTED | `SettingsTab`/`TABS` gained `models` (provider + model runtime card) and `advanced` (system profile/hardware card); all other tabs and the card/tooltips system unchanged |

## I.8d — v1.1.9: CI output-name fix, mode-aware navigation, explicit model states (IMPLEMENTED)

v1.1.9 changes no architecture. It repairs a CI output-mapping defect,
completes the Chat/Agent separation at the navigation layer, and makes
model-card states explicit. Verified rows:

| Surface | Package(s)/File(s) | Status | Notes |
|---|---|---|---|
| CI identity outputs | `.github/workflows/build-desktop.yml` | IMPLEMENTED | the audit job's `outputs:` map the exact `APP_*` names the identity step writes to `GITHUB_OUTPUT`, and the identity step fails fast with a `::error::` annotation when any identity value is empty, BEFORE any build job starts. The derivation chain: `package.json` → `release-version.mjs --env` → identity step → audit outputs → `needs.audit.outputs.*` → Linux/Windows environment |
| Mode-aware navigation | `src/workspace.ts` (`WorkspaceLayer.modes`, `visibleWorkspaceLayers`), `src/App.tsx`, `src/AgentSidebar.tsx` | IMPLEMENTED | Chat hides the Coding Lab (Agent machinery); Research/Settings remain in both modes. `App.tsx` falls back to the workspace view when the restored hash points at a hidden layer. The agent layer label is "Workspace" (the Agent MODE is the header switch). Sidebar sessions heading speaks in the mode's voice. Runtime wiring untouched |
| Explicit model states + fact grid | `src/ModelPicker.tsx`, `src/styles.css` | IMPLEMENTED | per-card `ModelState`: ready (serving) / loading (busy switch targeting this model) / incompatible (estimated footprint > total host RAM) / available; the Recommended/Compatible/Limited sizing hint is unchanged. Cards render an aligned Context/RAM/Tools/Vision/Native grid; Tools derives only from `chatTemplate` and unknown values render "—" (no fabrication). No-model empty state: "Choose a model" + Open models folder. Still no Remove action (no deletion API) |
| Settings: engine tuning → Advanced | `src/SettingsPanel.tsx` | IMPLEMENTED | the llama.cpp EngineCard moved from the Performance tab to Advanced; `touchesEngine` restart logic unchanged and tab-independent. Performance reads measure → recommend → verify |
| Minimalist UI + targeted optimisation | `src/styles.css`, `src/AgentBody.tsx`, `src/PerfStrip.tsx` | IMPLEMENTED | background gradients and the body grid overlay removed; shadows softened; `--border-soft` defined (was referenced but never declared); `.runtime-model-facts` renamed (collided with the picker's `.model-card-facts`); duplicated model `<select>` option builder unified; PerfStrip skips `/api/perf` fetches while `document.hidden` and refreshes on visibilitychange |

## I.8e — v1.2.8: mode-separated histories, cross-mode references, durable summaries, context layers, agent handoff (IMPLEMENTED + TESTED)

v1.2.8 makes Chat and Agent two conversation spaces over ONE runtime and
adds the four durable layers every long session now maintains. The
authoritative run transport (runLive + sequence + snapshot + terminal
registry), the context planner, recall and continuum are unchanged — the
new layers compose with them.

### The four layers (and what must never be conflated)

| Layer | Where it lives | Role |
|---|---|---|
| **Transcript** | `sessions/<id>.json` | The AUTHORITATIVE exact history. Never rewritten by summaries, retrieval or compaction; exact wording/code/commands are recovered from here |
| **Session summary** | `sessions/<id>.summary.json` (sidecar) | A derived, bounded, ROLLING representation of the whole session (objective/constraints/decisions/facts/files/tools/errors/state/unresolved/next). Updated once per settled turn — never a full re-summarization. Injected as the REQUIRED `summary` prompt section |
| **agent.md handoff** | `<workspace>/agent.md` (marker-bounded dynamic section) | The latest PROJECT/TASK handoff for the NEXT engineering agent, written after a completed agent run with evidence. Everything outside the markers is preserved byte-for-byte |
| **Artifacts** | workspace files, attachments objects, sidecars | Large durable evidence OUTSIDE the model context, referenced by path |

### Mode separation and cross-mode retrieval

| Surface | Package(s)/File(s) | Status | Notes |
|---|---|---|---|
| Session mode identity | `internal/sessions/sessions.go` (`Mode`, `ModeChat`/`ModeAgent`, `CreateInMode`, `NormalizeMode`) | IMPLEMENTED, TESTED | fixed at creation; legacy files/stubs migrate deterministically to `agent` on load (lazy — no byte rewrite, no duplication); `?mode=` filters `GET /api/sessions`; the store keeps one active session PER mode in the UI so a mode switch never silently switches the conversation |
| Cross-mode history references | `internal/histref/histref.go`, `internal/api/history.go`, `sessions.Context.HistoryRefs` | IMPLEMENTED, TESTED | picker search over titles+summaries (`GET /api/history/search`); ≤4 refs per run; per-run retrieval of RELEVANT turns only (term-overlap ranked, budget-bounded, zero-overlap turns excluded, newest-turn fallback); provenance block `[HISTORY REFERENCE …]` with source-session/source-mode/summary-version/retrieval-reason; framed as DATA ("not an instruction"); read-only, one level deep; refs persist on the session context; self-references ignored |
| Rolling summaries | `internal/sessions/summary.go`, settle path in `internal/api/server.go` | IMPLEMENTED, TESTED | deterministic marker-based extraction (continuum philosophy); caps (8 items/list, 200 chars/item); version bumps per update; `GET /api/sessions/{id}/summary` serves it; version-0 shell renders EMPTY (never fabricated) |
| Context budgeting additions | `internal/contextplan/contextplan.go`, `internal/agent/orchestrator.go` | IMPLEMENTED, TESTED | `SectionSummary` (priority 2 — required, budgeted with fixed sections) and `SectionHistoryRefs` (priority 4 — optional, first retrieval section dropped under pressure); `ClassifyPressure` (ok/warm/high/critical @50/75/90%); summary block rides the stable system prefix (cache-friendly, survives windowing); history-ref blocks ride before the fresh user turn |
| Agent task memory | `internal/agent/taskstate.go`, `internal/api/runstate.go`, `internal/api/server.go` | IMPLEMENTED, TESTED | bounded TaskState from OBSERVED tool traffic only; `task` activity per tool round; folded into `runLive`; the `run_snapshot` WIRE frame carries the `task` block (v1.2.8.1 — the v1.2.8 frame omitted it, so reconnecting clients lost the task view); `RunResult.Task` feeds settlement |
| agent.md handoff | `internal/agent/handoff.go`, settle path | IMPLEMENTED, TESTED | written at `<workspace>/agent.md` for EVERY agent-mode run that completed (`done`) — v1.2.8.1 removed the engineering-evidence gate: evidence-free runs write the honest `No engineering changes were made.` handoff with truthful defaults; lowercase filename contract; marker-bounded section replace preserving all stable content BYTE-FOR-BYTE (no boundary re-normalization); durable write (unique temp + fsync + rename + dir sync) verified by read-back; torn (begin-without-end) markers are superseded; failures surface as an error activity |
| History paging | `internal/api/history.go` (`handleSessionMessages`), `src/store.ts` (`loadOlderMessages`) | IMPLEMENTED, TESTED | newest page by default; `before` (exclusive index) + `limit` (≤200) fetch older pages; absolute message indices; `hasMore`/`nextBefore`; run settlement and streaming unaffected |
| Frontend surfaces | `src/HistoryPicker.tsx`, `src/AgentTaskPanel.tsx`, `src/AgentSidebar.tsx`, `src/AgentBody.tsx`, `src/MessageStream.tsx`, `src/mode-sessions.ts`, `src/history-ref.ts` | IMPLEMENTED, TESTED | pure helper modules (`mode-sessions.ts`, `history-ref.ts`) are unit-tested with node:test; picker (search/mode filter/multi-select/summary previews), ref chips with detach, agent pipeline panel from real evidence, inline session rename, sidebar search, load-earlier pager |
| Recovery fast-path root-cause fix | `src/store.ts` (`parseEndedAt`) | FIXED, TESTED | the backend's RFC3339 `endedAt` string (pinned by `runregistry_test.go`) was rejected by a number-only parser, so the authoritative `lastRun` fast path in `recoverRunFromIdle` never fired; both wire shapes are accepted now |

## I.9 — The SHEYTAN Native AI Engine architecture (v1.1.5, IMPLEMENTED foundation + model loading)

The target engine stack is now wired at the foundation level:

```text
React/TypeScript
      ↓
    Wails
      ↓
   Go Core
      ↓
SHEYTAN Native API        ← llm.Backend contract (Go) + IPC protocol
      ↓
C++ Native Engine         ← native/engine/ (narrow C ABI + host process)
```

Go remains the main application/runtime engine. The C++ native engine is
the future heavy-compute/AI execution engine. **Phase 1 implemented the
architecture foundation and Phase 2 added native GGUF model loading —
still no inference.** The managed llama.cpp engine remains the only
generation engine (fallback and default).

### Go↔C++ boundary decision: supervised subprocess + IPC (IMPLEMENTED)

Two candidate boundaries were evaluated:

| Criterion | A) cgo / shared library | B) supervised subprocess + IPC (**chosen**) |
|---|---|---|
| Crash isolation | a native crash kills the whole Go process | the host dies; Go's bounded watchdog restarts it (verified by tests) |
| Windows-first cross-build | requires a Windows C++ toolchain per build host; breaks today's `CGO_ENABLED=0` cross-compile | plain binary spawn; cross-build preserved |
| Future Android | JNI/binder coupling | maps to an Android service process; protocol unchanged |
| Maintainability | build-coupled; errors cross ABI silently | explicit protocol with version handshake; both sides tested |
| Performance | in-process calls | coarse-grained ops only — lifecycle, health, hardware, metrics, whole generation requests; **never** tiny high-frequency calls, so IPC overhead is irrelevant at this granularity |

The boundary is: `shtn-engine-host`, a C++ subprocess speaking 4-byte
little-endian length-prefixed JSON frames over stdin/stdout (1 MiB frame
cap, protocol + ABI version handshake that fails closed on mismatch,
malformed requests answered with bounded errors — never a crash). The
underlying engine core is a narrow C ABI (`include/shtn/engine.h`):
create/destroy/health/hardware_info/metrics since Phase 1,
load_model/unload_model/model_info/memory_plan since Phase 2,
tokenizer_init/tokenizer_info/tokenizer_encode/tokenizer_decode/
kv_cache_info/scheduler_info since Phase 4, and
generate/cancel/generation_stats since Phase 5 (protocol v4 / ABI v4;
every bump lands on both sides together, mismatches fail closed; the
generate op streams event frames with the request id before its final
frame — one lane per request, bounded, so the dispatch loop stays
responsive mid-generation).

### Native model lifecycle (Phase 2, IMPLEMENTED)

`LoadModel` on the native backend drives a strict, all-or-nothing native
load: validate the path (regular file) → open + `fstat` + whole-file
read-only `mmap` (lazy — tensor data is faulted in page by page, never
copied) → parse the GGUF container (magic, version 2/3, bounded counts
and lengths, per-KV type validation) → parse the tensor table (dims
bounds, overflow-checked element products, per-tensor offsets validated
against the data section, exact byte-size checks for known GGML types)
→ extract metadata only from keys actually present → compute the memory
plan (weights = tensor-data span; KV = 2·K/V·layers·context·embedding·2
bytes f16; workspace = context·vocab·4 logits row; fixed 64 MiB runtime
allowance; totals overflow-checked; compared against detected RAM) →
state `loaded`. Any failure unmaps, releases everything, and walks the
model state to `failed` with the reason — a failed load never leaves a
stale model behind. `UnloadModel` is idempotent; `LoadModel` while
loaded replaces the resident model. The Go side resets its model
snapshot on every host lifecycle boundary (a fresh host maps nothing).

### Model state vocabulary (Phase 2)

The model concern uses its own small vocabulary — `unloaded`,
`loading`, `loaded`, `failed` — deliberately separate from the engine
states (`llm.State*`) that keep describing the host subprocess. There is
still only ONE engine-state system and now ONE model-state system; the
two never mix.

### State authority

The native engine's state lives in `internal/native/engine` using the
**same state vocabulary and event shape** (`llm.State*`, `llm.EngineEvent`)
as the llama.cpp engine. There is no second, conflicting engine-state
system: each engine owns its authoritative state, the API layer reads one
snapshot per engine, and native transitions reach the same WS activity
pipeline with "Native engine …" captions. Generation on the native path
cycles ready → busy → ready (the existing vocabulary — no
"inferencing"/"generating" states were invented in Phase 5).

### Honest capability reporting

Since Phase 5, `engine.Backend.GenerationCapable()` returns `true` ONLY
when the engine is alive AND the loaded model's llama graph validated at
load time (every required tensor present with the right shape and a
supported type) — the same boolean `llm.SelectGenerationBackend` reads to
route generation (native when additionally selected and the request is
plain text; llama.cpp otherwise, with tools/images and pre-first-token
native failures falling back with a logged reason). A host restart or
model unload flips it back to false — capability tracks reality. The
Phase 1-4 rules still hold underneath: generation metrics (TTFT,
prompt/decode speed) are reported only from MEASURED values (the C++
engine's monotonic clock since Phase 5), and native `ModelInfo` reports
only values the C++ reader actually read or derived; absent GGUF keys
stay zero. An unsupported model reports `generationCapable=false` with an
inspectable reason (architecture, missing tensor, unsupported type) —
never a silent llama.cpp switch without evidence.

---

# Part II — The validated architectural direction (PLANNED — NOT IMPLEMENTED)

> **Read this banner literally.** Nothing in Part II exists in the code
> today. These sections record design intent that has been reviewed and
> agreed as the direction for future development. Any statement here must
> never be copied into user-facing capability claims. When a Part II item
> becomes real, move its row into Part I with evidence, and note the
> release in `worklog.md`.

## II.1 — Small, fast, local models are the foundation

SHEYTAN should be designed so a user with modest hardware can run a
capable multi-agent system. The architecture must not assume one giant
model.

Preferred philosophy:

```text
many efficient agents
+ orchestration
+ tools
+ external memory
+ objective verification
```

rather than:

```text
one enormous model
```

Rationale:

- **Lower memory requirements.** Several small quantized models can share
  a machine that could not hold one very large model.
- **Higher concurrency.** Small models iterate quickly, so multiple agents
  can work in parallel within the same compute envelope.
- **Faster agent iteration.** Shorter time-per-token means planner/critic
  style loops stay interactive instead of minutes-per-pass.
- **Practical low-end hardware support.** This is the audience SHEYTAN is
  for: local-first, privacy-first, no datacenter GPU required.
- **Model specialization.** Different agents can use different models —
  a coding-tuned model for the coder role, a cheap fast model for routing
  and classification, a multimodal model only when vision is needed.
- **Better parallel workloads.** N small models × M concurrent agents
  scales on consumer hardware; one large model serializes everything.
- **Reserve strength for exceptional tasks.** A larger or multimodal
  model can be invoked selectively when a task genuinely requires it,
  instead of paying its cost on every turn.

The key architectural claim: **model size is not the sole source of
system intelligence.** Structure (tools, memory, retrieval, verification,
orchestration) contributes at least as much as raw model capacity.
SHEYTAN's intelligence should come from the *system*, not from a single
overwhelming model.

Current state (honest): the runtime loads **one** model per session
(`model` config key) and every run uses that model. There is no
small-models-first composition, no per-agent model assignment, and no
model pool today.

## II.2 — Model tiers and hardware-adaptive routing (PLANNED)

Intended ladder, expressed generically. Tiers describe *capability and
resource cost*, not specific products; the runtime should discover what
is available locally and map models to tiers by measured properties
(parameter count, context length, modality, quantization, tokens/sec).

| Tier | Conceptual class | Intended role |
|---|---|---|
| Tier 0 | smallest local model | ultra-low-resource tasks: routing, classification, extraction, formatting |
| Tier 1 | small local model | default agent turns, cheap tool loops, summarization |
| Tier 2 | medium local model | complex reasoning, planning, code editing |
| Tier 3 | larger local model | difficult local tasks: multi-file refactors, deep debugging |
| Tier 4 | optional high-end (possibly multimodal) model | exceptional reasoning, visual workloads, specialized tasks |

Routing factors the future orchestrator should weigh:

```text
task complexity
modality (text / image / structured)
latency requirement
available hardware (RAM, VRAM, tokens/sec)
context requirement
confidence / retry state
resource budget (battery, thermal, concurrent runs)
```

The architectural principle is **model-agnostic, capability-based
routing**: the system must not hard-depend on any specific model brand or
version. Model names change every few months; the tier abstraction and
routing rules are the durable part.

**Candidate model examples** (examples only — **none of these are
integrated with SHEYTAN**; specs verified against public sources in
2026-09 and will change):

- **Gemma-class** (Google open models): the E2B/E4B "edge" size class
  runs in roughly 2–4 GB of memory and suits Tier 0/1; the 12B–27B class
  suits Tier 2/3; the line offers up to 128K (edge) / 256K (large)
  context windows and multimodal variants, which is interesting for a
  Tier 4 vision agent.
- **GLM-class** (Z.ai open weights): the "Air" class couples a large
  total parameter count with a small *active* parameter count
  (MoE), giving high capability per active FLOP — a candidate for
  Tier 3/4 when hardware allows; recent generations offer 128K–200K
  context windows and strong agentic/tool-use behavior.

These families are named **only as concrete illustrations of the tier
ladder**. SHEYTAN must not make any of them mandatory, and any future
default model choice must be re-verified against authoritative specs at
the time of integration. Small instruct-tuned models from other families
can fill Tier 0–2 equally well.

Current state (honest): there is no tier system, no router, and no
automatic model selection. The user picks one model in the UI.

## II.3 — The Context Engine: external context and chunking (PLANNED)

SHEYTAN's future context architecture must not depend on repeatedly
placing an entire repository or a huge Markdown document into the model
context window. The intended pipeline:

```text
Repository
   ↓
Structural index
   ↓
Semantic index
   ↓
Hierarchical retrieval
   ↓
Context builder
   ↓
Small local model
   ↓
Action
   ↓
Verification
   ↓
Memory update
```

The core mental model:

- **Model context = working memory.** Small, expensive, rebuilt per turn,
  holds only what the current reasoning step needs.
- **External structured storage = project memory.** Large, cheap,
  persistent, queryable, versioned.

The system should be designed around:

- **structural chunking** — chunks derived from document/source structure
  (headings, symbols, blocks), not fixed-size token windows
- **semantic chunking** — chunk boundaries respecting meaning, with
  embedding-assisted grouping where available
- **hierarchical retrieval** — drill down from repo → file → section →
  chunk only as needed
- **metadata-aware retrieval** — filter/rank chunks by path, type, mtime,
  symbol, heading
- **provenance** — every retrieved fragment carries file path + span, so
  edits can be located and audited
- **neighboring context** — retrieval returns parents/siblings, because a
  chunk in isolation is often misleading
- **cross-reference retrieval** — follow links, imports, includes and
  definitions on demand
- **persistent memory** — distilled facts/decisions outlive individual
  sessions (today's `memory`/`recall`/`continuum` are the seed)
- **context budgeting** — a hard allowance per section (Part II §5)
- **artifact-based agent communication** — agents exchange compact
  structured artifacts instead of transcript dumps (Part II §7)

Current state (honest): SHEYTAN has the *seeds* — an explicit context
plan with measured prompt bytes (`internal/contextplan`), a content-keyed
single-flight cache (`internal/contextcache`), a provenance chunk engine
(`internal/chunking`, deterministic metadata + byte ranges since
v1.1.5 Phase 3), streaming attachment staging with bounded retrieval
(`internal/attachments`), an append-aware trust-classed memory store,
BM25 recall and continuum rollover. Measured Phase 3 results (same
inputs, 2-vCPU container, median of 5): chunk derivation 1.6× faster
with 4.4× fewer bytes allocated, memory search 2.3× faster, recall
search 2.0× faster with 122× fewer allocations. There is **no**
repository structural or semantic index, no embeddings, no hierarchical
retrieval, and no context builder that composes retrieved chunks for a
model. "Context Engine" is the name for the future system that unifies
these.

## II.4 — Hierarchical chunk model (PLANNED)

Intended hierarchy:

```text
Repository
  ↓
File
  ↓
Section / Symbol
  ↓
Chunk
  ↓
Exact edit region
```

For Markdown specifically, the chunker should understand: headings and
nested headings, lists, tables, code blocks, links and references, and
definitions — so that a "section" is a real structural section, not an
approximate byte window. For source code, the equivalent is symbols
(functions, types, methods) and their spans.

A chunk should conceptually retain metadata such as:

```text
file path
section path (e.g.  "README.md > Architecture > Context Engine")
chunk ID
parent section ID
previous chunk ID / next chunk ID
token estimate
content hash (for cache keys and change detection)
provenance (how the chunk was produced / last verified)
```

This must never be reduced to naive fixed-token splitting: fixed windows
cut semantic units in half, destroy provenance, and make retrieval
matching noisy.

Current state (honest): `internal/chunking` splits text at paragraph
boundaries under byte budgets, windows long content head+tail, and gives
attachment chunks a stable identity. Chunks carry **no** structural
metadata (no section path, no parent/sibling links, no content hash
index). The hierarchy above is the target Context Engine data model.

## II.5 — Context budgeting (implemented core; deeper retrieval is PLANNED)

**Phase 7 status:** the hard budget pipeline EXISTS and is authoritative:
model-aware effective window (min of configured / GGUF limit / engine
limit), output reserve, safety margin, measured tool schemas, and a
degradation ladder that reduces lower-priority content before anything
essential is touched. The request is guaranteed to fit before any engine
call (`contextplan.Assemble` + the orchestrator preflight). What remains
PLANNED is the deeper retrieval integration below — hierarchical
retrieval over hierarchical chunks feeding the same budget.

**Status (IMPLEMENTED):** the budget became a full context
CONTROL system. `llm.ResolveSessionContext` resolves every run's window as
`min(session policy, global configured, model GGUF max, engine-verified
window)` floored at 1024 — the per-session policy lives in
`sessions.Context.ContextTokens`, is edited per chat from the header
selector, and never mutates other sessions. The llama.cpp engine's
`/props`-verified window feeds back into planning while the process is
alive (`EngineContextLimit`), and the wire request's `n_ctx` carries the
same validated effective value (`BuildChatRequestWithOptions`) — no second
truth anywhere. Large windows are resource-classified BEFORE use
(`llm.AssessContextResource`: weights + KV-cache from GGUF facts + runtime
overhead vs RAM/VRAM → safe / caution / unsupported; unsupported values are
rejected by the API with the reason). Per-agent policies
(`internal/multiagent/context.go`) bound specialist/role windows the same
way. Every turn logs a structured `context plan:` block before the engine
call and records the full decision trail in `ctxtelemetry`.

Intended direction (unchanged):

- a context assembly pipeline that treats the window as a budget with
  hard per-section allowances,
- priority-ordered elision (recency-ranked summarization of history,
  progressive elision of large file sections, demoting low-value
  retrieved chunks under pressure),
- everything measured: token costs, retrieval relevance, and which
  context actually improved the task (Phase 7 telemetry records this per
  turn — `internal/ctxtelemetry`).

## II.6 — Multi-agent architecture (PLANNED)

Intended shape: a set of specialized, low-cost agents coordinated by an
orchestrator. Example conceptual roles:

```text
planner          decomposes the task
coder            produces edits
researcher       gathers external/internal context
tester           writes and runs checks
debugger         diagnoses failures
documentation    updates docs to match code
reviewer         audits diffs
verifier         runs objective verification gates
```

Agents should not all use the same model. The orchestrator should
eventually assign models per agent according to:

```text
task complexity
modality
latency requirement
hardware
context requirement
confidence in prior attempts
resource budget
failure / retry state
```

Current state (honest, updated Phase 7):

- The **primary runtime is a single-agent sequential loop**
  (`internal/agent`): one model, tools executed one at a time, iteration
  cap + time budget. Parallel execution is a **deliberate non-goal
  today**.
- A **sequential** planner → executor → critic → summarizer pipeline
  exists (`internal/multiagent`) and is reachable from the CLI
  (`sheytan ask --multi`). It uses a single model and is not exposed via
  the HTTP API or the UI.
- **Phase 7 added bounded specialist CONSULTATIONS** (researcher /
  architect / coder / debugger / tester / security) between planning and
  execution: complexity-gated, at most two per run, one bounded LLM call
  each, advisory only. This is a first step toward the role taxonomy
  below — NOT parallel multi-agent execution. Per-agent model assignment
  and a persistent agent society remain Part II futures.

Do not claim true parallel multi-agent execution, per-agent model
assignment, or a persistent agent society. Those are Part II futures.
The truth is: sequential single-model pipelines with bounded specialist
consultations today; specialized, model-diverse, eventually-parallel
agents as the target.

## II.7 — Agent-to-agent communication via structured artifacts (PLANNED)

Future agents should communicate primarily through **structured
artifacts** — files with schemas — rather than by forwarding entire
conversation histories:

```text
Agent A
  ↓ writes
analysis.json   patch.diff   findings.md   test-results.json
  ↓ read + verified by
Agent B
  ↓
verification result
```

Why:

- **Reproducibility** — artifacts are inspectable inputs/outputs; a run
  can be replayed and diffed.
- **Auditability** — the artifact chain is the evidence trail.
- **Token efficiency** — a consumer reads the distilled artifact, not the
  producer's whole transcript.
- **Long-running workflows** — artifacts persist across restarts; agents
  resume from files, not from live RAM.
- **Low-resource operation** — small models can consume compact,
  well-shaped artifacts far more reliably than huge raw histories.

Current state (honest): there is no production artifact protocol between
agents. The closest existing primitives are the Lab's patch export, the
`diff` tool, and JSONL sidecars (sessions, recall, memory). A future
implementation should define the artifact schemas first and treat every
artifact as untrusted input (validate before consume), consistent with
the security invariants in `agent.md` §7.

## II.8 — Document editing architecture (PLANNED)

Markdown and documentation editing are core SHEYTAN workloads. The
intended future workflow:

```text
request
 ↓
intent analysis
 ↓
document search / index
 ↓
relevant section retrieval
 ↓
dependency / cross-reference retrieval
 ↓
context construction (budgeted)
 ↓
model edit proposal
 ↓
structured patch
 ↓
diff inspection
 ↓
Markdown / document validation
 ↓
tests / semantic checks
 ↓
final verification
```

The model is **never the authority** on whether the document was
correctly modified. The repository/tooling must validate:

- syntax (well-formed Markdown / target format)
- structure (heading tree intact, no accidental section deletion)
- references (internal links resolve where testable)
- required sections still present
- forbidden deletions (protected content untouched)
- duplicate headings where they matter
- malformed tables
- unbalanced code fences
- expected architecture statements (when the doc is contract-bearing)
- version metadata consistency (release surfaces)
- generated assets in sync (e.g. `web/static` after frontend changes)

Current state (honest): the primitives exist — `files` tool (read/write
with jail), `diff` tool, Lab verification gates, `release-version.mjs`
for release-surface metadata, and CI checks for embedded frontend and
release identity. There is **no** section-aware retrieval, no structured
document patch format, and no Markdown-structure validator today.

---

# Part II.9 — v1.2.0 product surfaces (IMPLEMENTED)

**Vision readiness state machine.** `internal/vision/states.go` defines the
authoritative states (`unsupported → supported → projector-missing →
projector-found → projector-verified/loading → ready`, plus `degraded` and
`failed`). Pre-boot states are computed per model from GGUF architecture and
projector discovery; runtime states are recorded by the engine's launch
machine (`internal/llm`) at pairing, verified boot, projector rejection and
terminal failure. The UI (`src/vision.ts`) renders only these states — a
filename containing "mmproj" can never produce a green check.

**Hardware intelligence.** `internal/hardware` consolidates the existing
`internal/sysinfo` probe plus backend evidence (Vulkan beside the engine
binary, native selection, installed engine tag) into one measured profile.
It never re-probes and never guesses; unavailable values stay unknown.

**Recommendation engine.** `internal/recommendation` implements the
Detected → Calculated → Recommended stages with task-aware profiles (chat,
coding, research, vision, agent, low-power, maximum). Context is clamped by
the model training limit, the verified engine window and the resource
verdict ladder (`llm.AssessContextResource`); flash attention and cache
reuse follow the VERIFIED engine capability profile. Every output carries
explicit reasons; predictions are labelled predicted, measurements stay in
`/api/perf`.

**Product identity.** `internal/platform` owns OS integration: the
AppUserModelID (`Parsaetak.SHEYTAN-LA`) is registered before the first
window; Windows version resources (ProductName SHEYTAN-LA, FileDescription
"SHEYTAN Local Agent", CompanyName Parsaetak) are generated by
`scripts/gen-syso` from `internal/config` constants; the firewall manager
creates only explicit, idempotent, removable rules scoped to an exact
executable and port — and none by default (loopback-first architecture).

**Installer and updates.** CI builds `SHEYTAN-LA-v<ver>-windows-x64.zip`
(portable) and `SHEYTAN-LA-v<ver>-windows-x64-installer.exe` (NSIS;
per-machine install, AUMID registration, user data preserved through the
`SHEYTAN_DATA_DIR` override), publishes `SHA256SUMS.txt` and a
`release-manifest.json`, and verifies the published release before
declaring success. The in-app updater (`internal/updater/appupdate.go`)
checks the manifest with proper version ordering, verifies SHA-256 (and
declared size) BEFORE staging, stages under `<DataDir>/updates/staging/`
and never executes or deletes anything. Code signing is not configured in
this release; the pipeline reports Authenticode status honestly instead of
claiming it.

# Part II.10 — v1.2.1 release-contract machinery (IMPLEMENTED)

**Canonical package roots.** The release identity chain already resolved the
VERSION single-source problem (package.json → release-version.mjs → CI);
v1.2.1 resolves the same problem for the package ROOT — the directory name
the portable ZIP and the installer stage under. The workflow now carries two
canonical variables (`WIN_PKG_ROOT: "SHEYTAN-LA"`,
`LINUX_PKG_ROOT: "SHEYTAN-Local-Agent"`) and every staging directory, ZIP
creation path, ZIP entry check, artifact name and release-metadata reference
is derived from them; no root literal is repeated. The Go mirror
(`internal/releasecontract`) exposes the concrete identities (roots, exe
names, artifact names per version), the parameterized workflow-slot
spellings, and the required ZIP entry sets. Because the workflow cannot
import Go, `cmd/stress_release_surface.go` is the enforcement arm: it requires the
canonical env lines and the parameterized verification spellings to be
present in the workflow text and checks every `dist/` artifact the workflow
produces against the contract slots — naming drift now fails in either
direction instead of two copies of the truth agreeing with each other (the
exact mechanism that kept run 34871838054's failure invisible to the gate:
the contract's own Linux entry list mixed roots, so the workflow verifier
and the contract were wrong together).

**Installer option and uninstall boundaries.** The NSIS installer keeps the
graphical directory page; the desktop shortcut became a checkbox on that
same page (default checked, state persisted across Back/Next, silent
installs keep the default). Upgrades close a running instance through a
bounded retry with an explicit user decision on a locked executable — no
silent partial upgrade. The uninstaller's boundaries are contractual, not
conventional: it removes the application, shortcuts, ARP registration and
the AppUserModelID, while user data (models, workspace, sessions,
configuration, `SHEYTAN_DATA_DIR`) is preserved BY CONSTRUCTION — the
script may not contain a recursive delete, and CI verifies the source
contract (directory page, shortcut option, uninstaller registration, no
`RMDir /r`) before an installer is built from it.

**Bounded packaging.** Every workflow job carries `timeout-minutes`; both
portable ZIPs verify the in-package `BUILD-INFO.txt` version; the native
engine build is cached keyed on the exact `native/engine/**` source hash —
a cache hit reuses the build tree for identical sources, ctest still runs,
and reproducibility is unaffected.

# Part III — Documentation truth standard

Every significant capability statement in this repository must fit one
of these categories:

| Label | Meaning |
|---|---|
| `IMPLEMENTED` | wired end-to-end in production code paths |
| `TESTED` | covered by automated tests (unit / HTTP-level / stress) |
| `PARTIALLY IMPLEMENTED` | exists but not wired to every surface (e.g. CLI-only, no UI) |
| `EXPERIMENTAL` | behind a flag / unstable path, may change |
| `PLANNED` | agreed direction, **not** built — must say so explicitly |
| `OPTIONAL FUTURE` | idea kept on record, no commitment |

Rules:

1. Never let a planned capability read as an implemented one. Bad:
   "SHEYTAN runs hierarchical multi-agent memory." Good: "The current
   runtime provides measured context budgets and BM25 recall. The planned
   Context Engine (Part II §3) is intended to add hierarchical retrieval."
2. Resolve contradictions with **source code as the authority**, not by
   copying a claim into more documents.
3. When implementation status changes, update Part I of this file, the
   relevant section of `README.md` / `agent.md`, and add a dated entry in
   `worklog.md` — in the same change.
4. Model names and versions are examples, never foundations. Verify specs
   against authoritative sources at integration time, and label them as
   candidates otherwise.
