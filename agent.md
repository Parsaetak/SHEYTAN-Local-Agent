# SHEYTAN-Local-Agent — Agent Context

> **v1.7.0 handoff note (2026-09-26):** this release hardens the
> engine transaction and ships the automation layer. (1) The
> provisioning transaction's rollback paths now stop and reap the
> candidate engine BEFORE any restore
> (`internal/llm/llama.go` `stopCandidateForRollback` — lifecycle-owned
> stop, watchdog cancelled, deterministic reap; the Windows
> "Access is denied" rollback failure class is structurally closed;
> `internal/llm/variant_rollback_v170_test.go` proves the contract at
> the exact rollback instant, including byte-identical restore,
> authoritative manifest, healthy LKG restart and zero orphans).
> (2) The ONE scheduler grew the v1.7.0 Automation/Tasks system
> (`internal/scheduler/automation.go`): pause gate, once / interval /
> daily / weekly at LOCAL time, linked skills, task-scoped tools,
> RunNow / Pause / Resume / Cancel / UpdateTask / Runs / NotifyEvent /
> ShutdownSettle, durable claims extended to every schedule kind
> (no crash replay, missed deadlines run exactly once) and the
> `/api/automation/*` surface. (3) Markdown SKILL.md packages with
> progressive disclosure (`internal/skills/markdown.go` — scopes
> global/workspace/task, metadata-first, agent-facing `skill_create`,
> VERIFIED-rule-gated promotion; JSON path untouched). (4) Task-scoped
> custom tools (`internal/customtools/tasktools.go` — disabled and
> unapproved by default, ONE registry, real loop E2E). (5) First-class
> task/run artifacts (`internal/artifacts/taskmeta.go` +
> `internal/tools/artifact_create.go` — atomic, path-safe, bounded,
> versioned, sandboxed viewer). (6) The systems are wired at the
> runtime (`internal/runtime/automation.go`) with genuine emitters
> (startup / file_change / git_change / test_failure) — ci_failure and
> build_failure remain declared kinds with no emitters yet (ROADMAP
> NEXT 2). Read `ROADMAP.md` FIRST (its §0 is the authoritative
> forward handoff), then `UPDATE.md` (top section) and the v1.7.0
> section of `ARCHITECTURE.md`.

> **v1.6.2 handoff note (2026-09-26):** this release is a correctness
> pass over v1.6.1, reproduced-first: strict engine-variant parsing
> (`internal/updater/variant.go` — `ParseAssetVariant`; invalid
> variants are a deterministic 400 at `POST /api/engine/provision`,
> never CPU; the v1.6.1 silent-CPU defect was reproduced then fixed),
> variant-aware release resolution (the newest release that ACTUALLY
> contains the exact variant asset — a CPU-only release is skipped for
> Vulkan requests), an architecture-aware support matrix (Windows x64
> cpu+vulkan, Windows ARM64 cpu-only per the verified b11191 asset
> list), the GGUF import race fix (`internal/llm/importlock.go` —
> per-models-directory exclusive lock, in-process + cross-process,
> -race-verified; the overwrite defect was reproduced then fixed),
> runtime backend verification inside the provisioning transaction
> (`verifyRuntimeBackendForVariant` — the engine's own device
> enumeration gates the commit; a failed verification rolls back), the
> extra-args sampling-gate closure (`ValidateExtraArgs` — malformed
> numeric arguments never spawn the engine) with atomically persisted
> repairs, the real Settings engine-backend card, and corrected
> upstream/Linux facts (b11191 publishes Linux cpu+vulkan as .tar.gz —
> the zip installer does not consume them yet; ROADMAP NEXT item 2).
> CI no longer hard-codes the engine tag — the Windows job runs the
> authoritative resolver gate. Read `ROADMAP.md` FIRST (its §0 is the
> authoritative forward handoff), then `UPDATE.md` (top section) and
> the v1.6.2 section of `ARCHITECTURE.md`.

> **v1.6.1 handoff note (2026-09-26):** this release shipped the
> deterministic sampling gate (`internal/config/sampling.go` — the
> authoritative validation module; `internal/llm/llama.go`
> `startLocked` refuses invalid sampling values BEFORE any engine
> process, the compatibility ladder, or the download path — classified
> `InvalidSamplingConfigError`, never a compat/accelerator verdict; the
> Settings PATCH API rejects invalid values with an actionable 400),
> the first-class local GGUF import (`internal/llm/importmodel.go` +
> `POST /api/models/import` + `POST /api/models/import/pick` + the
> picker's "Import GGUF…" flow — header validation, streaming copy,
> atomic placement, duplicate-safe, source untouched; migrated
> model-path re-anchoring in `internal/config/modelpaths.go`), the
> v1.6.1 log discipline (`internal/api/perf.go` — accelerator
> resolutions log at INFO only when CHANGED; `internal/updater/
> install.go` — stale-file reporting aggregated to one line per swap),
> REAL Windows Vulkan engine provisioning (`internal/updater/variant.go`
> — variant-aware assets + manifest identity;
> `LlamaServer.UpdateEngineVariantNow` + `POST /api/engine/provision`;
> explicit VULKAN never silently falls back to CPU; AUTO stays
> evidence-gated), and the conservative mixed licensing model
> (`LICENSE`, `LICENSE-APACHE`, `LICENSE-PROPRIETARY`, `LICENSE-MAP.md`
> — the classification authority, `NOTICE.md`, `CONTRIBUTING.md`,
> `SECURITY.md`). Regression evidence: the full Go suite (53 packages,
> headless), `go test -race` on the concurrency-heavy packages, the
> native C++ engine suite (13/13), the frontend suite (115 unit tests,
> typecheck, lint, production build + static-asset contract) and the
> browser E2E suite (24/24 with the real C++ engine and real GGUF
> generation). Read `ROADMAP.md` FIRST for what comes next, then
> `UPDATE.md` (top section) and the v1.6.1 section of
> `ARCHITECTURE.md`.

> **v1.6.0 handoff note (2026-09-26, updated by the repair pass):** this
> release added the startup
> maintenance gate (`internal/api/maintenance.go` — engine maintenance
> completes BEFORE any engine start; prewarm and the scheduled updater's
> first pass are gated), top-level Chat/Agent views (`src/workspace.ts`,
> ModeSwitch removed), automatic long context (user-facing context editor
> removed; backend resolution unchanged), the custom tool system
> (`internal/customtools/` + `/api/custom-tools` + `MyToolsCard`), and
> truthful engine maintenance UX (`/api/maintenance` +
> `src/MaintenanceBanner.tsx`). The repair pass (same date) fixed the
> failed v1.6.0 submission: Windows custom-command process-tree
> ownership (`internal/customtools/proctree_{windows,other}.go`, Job
> Objects), the argv-based output-cap fixture (now file-based), real
> tab semantics on the top-level navigation, Chat-first deterministic
> landing (`resolveInitialView`: hash > remembered view > Chat, driving
> both the App's first render and the store's boot mode), and the run
> gate's maintenance-gate wait + native convergence
> (`internal/api/server.go`, `internal/runtime/runtime.go`
> `EnsureLLMContext`). Read `UPDATE.md` (top section) and the
> v1.6.0 section of `ARCHITECTURE.md` first; regression tests live in
> `internal/api/maintenance_test.go`,
> `internal/llm/maintenance_stop_test.go`,
> `internal/customtools/customtools_test.go`,
> `internal/api/customtools_api_test.go`,
> `internal/agent/customtools_e2e_test.go` and
> `src/workspace-v136.test.ts`.


> Persistent engineering handoff for the next agent working on this repository.

Repository: https://github.com/Parsaetak/SHEYTAN-local-agent

Branch: `main`

<!-- sheytan:handoff:begin -->
# Latest Agent Handoff

## Task
SHEYTAN-LA v1.3.6 (engine lifecycle ownership, system discovery, Net
Search, canonical data root): root-fix the Start/Update race, the blind
port-8080 adoption, the unclassified 0xC0000139 loader failures, the
competing engine download paths, the AppData second data root, the
Research-tab discoverability problem, and the stale v1.2.2 UI version
fallback — while preserving the proven architecture (Chat/Agent shared
sessions, context planning, repository intelligence, Coding Lab,
native engine, evidence/verification semantics, local-first operation)
and the green CI baseline.

## Current state
All v1.3.6 work is IMPLEMENTED on `main` (base `57f0c1b` = 1.3.5;
v1.3.6 landed as commit `9036e1f` plus `0207611`, which removed the
stale `src/ResearchPanel.tsx` and `web/static/assets/ResearchPanel.js`
files that had failed Actions run `35702334699`). Verified on Linux x86-64 (Go 1.27.1,
Node 24): gofmt + go vet (-tags headless) clean; go test
./internal/... -tags headless 51/51 packages including the new suites
(engine lifecycle/adoption/classification/transactional update/race in
internal/llm; installer transaction in internal/updater; process
identity in internal/proc; static analysis in internal/engcheck;
discovery in internal/engdiscovery; migration + path invariants in
internal/config); frontend typecheck, lint (0 warnings), test:units
96/96 (workspace/net-search/version contracts included),
test:release 28/28, build, verify:web; release-version.mjs --check
green at 1.3.6; ROADMAP.md blob SHA unchanged
(c7e2c1720eb5e97bd932c0d76100b8719193e650).

NOT verified in this environment — remains owned by the Windows
development machine and CI: Windows builds (exe/ZIP/NSIS installer),
Windows native C++ CTest (12/12 gate), Windows Go verification, the
real-Windows runtime acceptance (§45), GitHub Actions runs, the
v1.3.6 tag, release publication and published-asset hashes. A real
llama.cpp boot on user hardware is likewise unverified here.

## Surfaces touched
- internal/proc: identity.go + identity_windows.go + identity_unix.go
  (NEW) — ListeningProcess/ProcessExePath/SameExecutable via real OS
  APIs; never shell parsing.
- internal/engcheck (NEW): single static-analysis authority — SHA-256
  identity, PE/ELF architecture sniff, Windows import closure.
- internal/llm: loaderclass.go (NEW), preflight.go (NEW),
  diagnostics.go (NEW), adoption.go (NEW); llama.go — ensureBinary now
  tiers through discovery import and updater.InstallStaged (the old
  downloadEngineArchive/llamaDownloadURL/extractEngineArchive
  duplicates are DELETED), preflight gate before any model launch,
  loader-class failures break the compatibility ladder, identity-proven
  adoption, UpdateEngineNow engine-owned transaction,
  EngineDiagnostics(); exitFailure carries the raw exit code.
- internal/updater: install.go (NEW) — InstallStaged/
  InstallStagedFromArchive/ImportCandidate (stage → validate → atomic
  swap → verify → commit → cleanup, rollback on failure, dedupe by
  SHA-256); TransactionalEngine delegation in UpdateEngineWithProgress;
  extractZip honors stored Unix modes.
- internal/engdiscovery (NEW): tiered discovery (0/1/2), persisted
  cache, non-destructive validation, bounded probe via the production
  subprocess runner.
- internal/config: migrate_appdata.go (NEW) — legacy AppData root
  migration (idempotent, hash-verified, engine bundle as a unit);
  migrate_appdata_test.go (NEW) incl. hard path invariants.
- cmd/root.go: legacy AppData migration wired after
  MigrateMalformedRoots.
- packaging/nsis/installer.nsi: install-local data tree + Users modify
  ACL (icacls), DataDir registry note, legacy machine SHEYTAN_DATA_DIR
  env var DELETED.
- internal/api: /api/net-search registered (same handler — one
  implementation); /api/run body gains netSearch; /api/engine snapshot
  gains the diagnostics block.
- internal/agent: WithNetSearch + ToolPolicy.NetSearch (server-side
  enforcement, research tool guaranteed offered and executable for that
  request only); ctxtelemetry TurnRecord.NetSearch.
- Frontend: ResearchPanel.tsx deleted; research layer removed from
  workspace.ts/shortcuts.ts/App.tsx/store/api client; Net Search
  control added to ComposerControls (states off/enabled/searching/
  results/failed from REAL wire events; result count only when stated);
  vite.config.ts injects __APP_VERSION__ from package.json;
  vite-env.d.ts declares it; App.tsx displayVersion() replaces the
  v1.2.2 fallback; settings labels → Net Search (JSON keys unchanged);
  layers.css/motion.css research blocks removed; new tests
  workspace-v136 / net-search / version-contract wired into
  test:units; web/static rebuilt (ResearchPanel.js gone, no 1.2.2
  literal in the bundle).
- Version: package.json 1.3.6 → release-version.mjs synced
  internal/config/config.go, build/config.yml, SIGNATURE; package-lock
  root version synced.

## Verification notes
Everything in "Current state" corresponds to an executed check in this
environment. The Windows/CI-owned items above were NOT executed and
must not be claimed as passing. The UPDATE ZIP manifest records the
honest verification summary.

<!-- sheytan:handoff:end -->

Current release: `v1.3.4` — codename-gate root-fix + Repository
Intelligence slice 1 (persistent bounded repository index, symbol/
dependency graph, test/source relationships, Git-aware metadata, hybrid
evidence-ranked search, repo_search agent tool, tier-gated repo-evidence
context block, Workspace index card). Prior lines:
`v1.3.0` (runtime path correctness, clean logging, universal scrolling,
professional Settings, GitHub cloning), `v1.2.9` (stabilization & security),
`v1.2.8`/`v1.2.8.1` (professional Chat + Agent workspaces, cross-mode
references, handoff), `v1.2.7` (run-transport terminal-state repair),
`v1.2.6` (authoritative run transport, measured timing), `v1.2.5` (adaptive
context tiers), `v1.2.3` (download manager), `v1.2.2` (generation
visibility), `v1.2.1` (package-root contract), `v1.2.0` (unified product
upgrade), `v1.1.5`–`v1.1.9` (native engine phases, Chat/Agent separation,
options/diagnostics). Full engineering evidence: `worklog.md`.

# Durable engineering invariants (from past repairs — still binding)

- **Run lifecycle gate** — in `handleActivityWS` (internal/api/server.go) map
  membership is NOT "run active"; `runLive` is the ONE lifecycle authority.
  A terminal-but-registered entry serves the idle sentinel with the recorded
  `lastRun` outcome. Do not add a second authority.
- **Outcome-record ordering** — `settle()` records the run outcome in the
  bounded registry BEFORE the authoritative state flips terminal.
- **Transport attach contract** — first frame after WS upgrade is the
  `attached` ack; attachments receive a seq/runId-filtered `run_snapshot`;
  idle sentinels carry the authoritative `lastRun` outcome.
- **One active run per session** — a newer run cancels-and-replaces by
  design; durable queuing is documented design intent only.
- **Download Manager** — every remote asset flows through
  `internal/downloader` (HTTPS-only, `.part` → verify → atomic rename,
  Range resume, ordered sources, explicit fallback opt-in). No second
  download path.
- **Package-root contract** — one canonical root variable per platform
  (`WIN_PKG_ROOT` / `LINUX_PKG_ROOT`); `internal/releasecontract` mirrors
  them and the stress gate fails on drift.
- **Stable asset contract** — generated frontend filenames are
  deterministic (`assets/[name].js`), verified in CI; `web/static` exactly
  mirrors the clean build.
- **Version identity** — package.json is the single source; no hardcoded
  version or codename constants in the workflow.
- **`gpus` wire guarantee** — hardware/sysinfo probes serialize `[]`, never
  JSON `null` (a null slice crashes defensive frontend readers).
- **Streaming snapshot semantics** — progress captions are cumulative
  snapshots (replace, not append); replayed frames are idempotent.
- **Engine guard coherence** — the load/unload guard covers the WHOLE
  in-flight generation window; observable state can never contradict it.
- **Windows hardware probe** — ONE batched CIM invocation; the deep probe
  warms in the background (`deepReady` on the wire).
- **Accelerator evidence** — GPU requires ENGINE evidence, NPU requires
  presence + measured OpenVINO load + arch/quant gates; presence alone
  never wins.

# 1. Mission

SHEYTAN-Local-Agent is a local-first AI software-engineering environment.

Core principle:

> **The model proposes. The tools execute. The laboratory verifies.**

The current mission is maintaining and deepening **verified runtime behavior** — not adding visual surface area. Every change must keep the full path working:

```text
desktop launch
  → automatic llama.cpp startup
  → real model readiness
  → inference (streaming, tools, budget)
  → attachments / chunking / cache / recall
  → continuum rollover on pressure
  → objective verification (Lab)
  → visible, honest result
```

# 2. Development rules

Before modifying anything:

```text
inspect live repository
verify exact main commit
inspect relevant source
verify actual runtime behavior
```

Never:

```text
assume a commit works
assume a successful build means functionality
assume a UI control is wired
assume engine state is true
claim success without evidence
claim a performance win without a before/after measurement
```

When the user says "done, check verify and continue":

```text
inspect → verify → diagnose → fix → retest → continue
```

# 3. Architecture

Backend: Go 1.26, Wails v3 (desktop shell), Go HTTP API + WebSocket on `127.0.0.1:8765`.

Engine stack (the native engine performs REAL transformer inference for the llama architecture and serves REAL generation with streaming + cancellation + measured metrics; llama.cpp remains the default/fallback and serves every model the native engine cannot execute):

```text
React/TypeScript → Wails → Go Core → llm.Backend contract
                                    ├─ llama.cpp (default + fallback: tools, images,
                                    │             non-llama archs, unsupported tensors)
                                    └─ native backend (selected + GenerationCapable)
                                        → shtn-engine-host (supervised subprocess)
                                        → C++ engine: GGUF tokenizer → transformer
                                          forward pass (RMSNorm/RoPE/GQA/SwiGLU)
                                          → real logits → sampler → fp16 KV cache
                                          → decode loop → streamed chunks → metrics
```

Frontend: React 19, TypeScript, Vite, Zustand; embedded via `web/static` (go:embed) — **`npm run build` must be re-run after any frontend change** so the embedded assets stay in sync.

Primary packages:

```text
internal/agent       orchestrator (per-run config snapshot, tool registry)
                     + Phase 6 reliability core: failure classification
                     (14 categories with repair hints), LoopGuard (repeat
                     detection, tool/wall-clock budgets), EvidenceCollector
                     (run-level verified/partial/failed/not_verified)
internal/llm         LlamaServer (engine lifecycle) + OpenAI-compatible client
                     + Backend contract + LlamaBackend + selection (v1.1.5)
internal/native/engine  SHEYTAN native engine: protocol (v4), supervised runtime,
                     Backend adapter, hardware profile, metrics, model lifecycle,
                     tokenizer, KV cache, scheduler, sampler AND REAL llama-
                     architecture generation (Phase 5: transformer forward
                     pass, streamed chunks, cooperative cancellation,
                     measured generation metrics; unsupported models fall
                     back to llama.cpp with an inspectable reason)
internal/api         REST/WS surface, run registry, engine event bus
internal/runtime     Stack wiring (single source for every subsystem)
internal/config      Config + Source (copy-on-write live config)  ← READ THIS
internal/attachments streaming staged uploads, shared chunk engine,
                    bounded retrieval (measured stats)
internal/contextplan context budget authority (+ measured PromptBytes)
internal/contextcache content-keyed LRU cache, single-flight coalescing,
                    oversized-entry guard
internal/continuum   chapter rollover (wired post-run since v1.1.4)
internal/lab         Coding Lab (policy, runner, verifier, repair,
                     safe anchored edits: read_file/edit_file)
internal/sandbox     Job-Object code-exec governor
internal/proc        process spawn/kill-tree + environment sanitization
internal/tools       17 agent tools
internal/projectintel persistent per-project intelligence (measured
                     languages/commands/layout + Lab-verified build/test
                     commands + lessons; card injected per run)
internal/memory      M1–M7 trust-classed store (append-aware cache)
internal/recall      BM25 recall + feedback steering (cached corpus stats)
internal/research    multi-provider search
internal/multiagent  planner→executor→critic pipeline (CLI `ask --multi` ONLY — sequential, single model, no HTTP/UI surface)
internal/updater     engine download/update (zip-slip hardened)
internal/logging     log catcher + redaction
internal/sysinfo     hardware probe (CIM-first on Windows)
internal/netcheck    parallel connectivity probes
native/engine/       C++ native engine (CMake + Makefile): C ABI core,
                     shtn-engine-host subprocess, protocol + host tests,
                     llama forward pass + generation + tensor dequant
                     (F32/F16/Q4_0/Q4_1/Q5_0/Q5_1/Q8_0), Python reference
                     + fixtures for the numerical-correctness tests
```

# 4. Configuration: the Source contract

`config.Source` (internal/config/source.go) is the **only** sanctioned way to mutate live configuration:

- Values obtained from `Load()` are **immutable by contract** — never mutate them.
- Writers go through `Update`/`UpdateErr`/`Store` (copy → mutate → publish).
- Runs, requests and engine starts take ONE snapshot per operation.
- `updater.RunScheduled` runs `CheckAndApply` on a private copy and publishes back.

Violating this contract reintroduces the v1.1.3 data race (`*s.cfg = updated` in the patch handler). `TestSourceConcurrentReadWrite` and `TestConfigPatchIsRaceFree` guard it under `-race` — keep them passing.

# 4b. Data-pipeline rules

The local data path has one owner per stage — Source/Input → Loader →
Normalizer → Chunker (chunking.ChunkText) → Cache (contextcache) →
Retriever → Context Builder — and every stage keeps derived data
separate from sources and bounded:

- Chunk IDs/metadata are deterministic from content + processing
  parameters; bump `chunking.ProcessingVersion` AND
  `contextcache.Version` together when derivation changes shape.
- The cache is never a source of truth. Concurrent same-key work is
  coalesced by `contextcache.GetOrCompute`; a value above the per-entry
  bound is rejected, not stored.
- Memory-store search, recall search and attachment retrieval are
  measured (Store.ParseStats, cache Stats, ResourceUsage,
  RetrievalStats, plan PromptBytes). Log measured values only.
- Never trade a trust/security bound for speed: quarantine,
  authoritative-user-fact rules and path jails apply to the fast paths
  exactly as before.

# 5. Engine rules

- Engine state is backend-authoritative: `idle/downloading/starting/ready/running/busy/stopping/stopped/failed`. `ready`/`busy` are the reachable alive states; the UI must never invent any state. The native engine uses the SAME vocabulary and event shape (`llm.State*`, `llm.EngineEvent`) — there is no second state system; each engine owns its authoritative state and the API exposes one snapshot per engine.
- The engine start captures one config snapshot (a Settings PATCH mid-boot can no longer produce half-old/half-new launch flags).
- `MarkBusy` performs the whole transition under one lock — do not split it again (see `setStateLocked`).
- Streaming has NO overall client timeout by design; the stall watchdog (5 min zero-byte) provides the hang bound. Do not reintroduce a blanket `http.Client.Timeout` on the stream client.
- Engine downloads are context-bounded (10 min) and size-capped (2 GiB).
- v1.1.5 Phase 5 backend rules: generation is routed by `llm.SelectGenerationBackend` (wired through the orchestrator's generation router — `Stack.streamGeneration`) — the native engine only when selected (`engineBackend: "native"`) AND `GenerationCapable()` (alive + a loaded model whose llama graph validated at load time); otherwise llama.cpp. Request shapes the native path cannot serve (tools, images) and pre-first-token native failures fall back to llama.cpp with the reason logged and inspectable. Native engine failures never fail the llama path (best-effort, logged, visible in `native.state`).
- Native MODEL loading (Phase 2) is real: `LoadModel` validates the file and loads it natively (GGUF validate → memory-map → metadata → memory plan). Model states use their own dedicated vocabulary — `unloaded/loading/loaded/failed` — separate from the engine states above; a host restart resets the model state (a fresh host maps nothing). Loading a model does NOT enable generation.
- v1.1.5 Phase 5 native generation (REAL): the native engine performs the actual transformer computation for the llama architecture — token embeddings → per-layer RMSNorm → Q/K/V projection → RoPE → causal GQA attention over a REAL fp16 KV cache → output projection + residual → RMSNorm → SwiGLU FFN + residual → final norm → logits. The sampler consumes the REAL logits (temperature, top-k, top-p, repetition penalty, seed — the Phase 4 primitives). Generation streams coarse-grained chunks over IPC (one generate request → event frames → final frame; NEVER one frame per token), supports REAL cooperative cancellation (observed every token), stops honestly (EOS without emitting it / max_tokens / context bound / error) and reports MEASURED metrics only (prompt tokens, generated tokens, prefill time, TTFT, decode tok/s, KV positions — monotonic clock). The context bound is a REJECT policy: prompt+max_tokens beyond the context window fails explicitly (no silent truncation).
- Native generation support is NARROW and honest: architecture llama only; tensor types F32/F16/Q4_0/Q4_1/Q5_0/Q5_1/Q8_0 only; RoPE freq_scale 1.0 only; no chat-template interpretation (plain role-labeled prompt format — llama.cpp keeps template fidelity); tools/images requests return ErrNotImplemented (the router sends them to llama.cpp). A model outside the supported set loads fine but reports `generationCapable=false` with an inspectable reason; generation selection (`llm.SelectGenerationBackend` → native only when selected AND capable) then routes to llama.cpp. A native failure BEFORE the first streamed token falls back to llama.cpp with the reason logged; after the first token it surfaces like any engine error.
- The native KV cache now stores REAL fp16 bits (uint16_t, Phase 5 corrected the Phase 4 float[]-but-reported-f16 defect: capacity_bytes == the actual allocation, layer offsets exact, used_bytes consistent with written positions — pinned by regression tests). The cache allocates at the first generate (sized from model dims) and resets per request; `kv_cache_info` reports the measured population.
- The native scheduler runs a REAL single-slot worker thread (max concurrent = 1): queued/active/completed/cancelled/failed counts are real; the generation lane in the host keeps the dispatch loop responsive (cancel/metrics/shutdown while generating).
- Model loads now compute the llama-graph verdict (metadata-level: every required tensor present with the right shape and a supported type) and report `generationCapable` + `generationReason` in model_info. Pre-warm loads the selected model natively and logs the verdict; an incapable model keeps llama.cpp serving generation.
- The native engine host (`shtn-engine-host`) runs with a sanitized environment, bounded op timeouts (10 s; model loads 30 s), a 1 MiB frame cap and a protocol/ABI handshake that fails closed (protocol/ABI v4 — both sides bumped together; v4 adds the generate/cancel ops with streamed event frames + real cancellation to v3; generation runs on bounded lanes so the host stays responsive mid-generation; malformed generate payloads and host-side errors are bounded error frames, never crashes). Build it from `native/engine/` (CMake or Make); this phase does not ship or auto-download it.
- v1.1.5 Phase 4 frontend perf contract: streaming model output is COALESCED through `flushStreaming` (rAF-boundary batching in `store.ts`). The UI updates at most once per frame regardless of token rate — a model emitting 200 tokens/sec no longer triggers 200 React renders/sec. Lifecycle events (done/error/session) bypass the coalescer and reset state immediately. A frame-budget diagnostic HUD (`src/perf-hud.ts`) is OFF by default — toggle with Ctrl+Shift+P or `window.__shtnTogglePerfHUD()`. The HUD measures real frame time, dropped frames, longtask count and coalesced stream-update frequency. The target budget is auto-detected from the display refresh rate (8.33 ms for 120 Hz, 16.67 ms for 60 Hz) — the HUD does NOT claim guaranteed 120 FPS; it reports `optimized for high-refresh displays / frame-budget aware / 120 Hz-capable presentation where hardware permits`.

# 6. Bounded-resource invariants

Every long-running operation must have: context cancellation, timeout, bounded output, cleanup. Current bounds to preserve:

```text
run time budget       runTimeoutMinutes (default 60, clamp 1..1440, 0=off)
engine watchdog       3 restarts/episode, 1/2/4s backoff
LLM retries           4 attempts (no retry after first emitted token)
stream stall          5 min zero-byte abort
lab output            2 MiB shared stdout+stderr
lab command timeout   ≤ 3600s     repair iterations ≤ 100
shell output          tool-level caps (64 KB simulator, 2 MB file reads)
attachments           manager-enforced size/count/chunk/processing caps;
                      staging is streaming (RAM ≈ 16 KiB head + 128 KiB
                      buffer, not the file size)
attachment chunks     ≤ 512 per file (bounded derived data)
retrieval objects     ≤ 32 MiB retained per retrieve call; larger objects
                      fall back to exact byte-range reads
context cache         entries + bytes + per-entry bound; single-flight;
                      oversized entries rejected, never retained
screenshots           50 kept     crash reports: 20 kept
WS hubs               128-event buffers, drop-on-slow (never block runs)
```

# 7. Security invariants (do not regress)

```text
loopback-only API + origin allow-list
path jail (traversal + symlink resolution) on every file tool
zip-slip-safe extraction with caps (llama.go AND updater)
fetch: URL validation + DNS pre-resolution + pinned dial IP
sandbox + lab: sanitized environment, HOME pinned to workspace
lab policy: dangerous/network/interactive/escape denylists
   (incl. $VAR/ ~/ %VAR% expansion tokens — see isExpandedPathToken)
secrets redacted: config GET, diagnostics zip, logs
```

Fail closed. Never weaken a control to unblock a feature.

# 8. Wired-surface contract

Before this release, several subsystems were fully implemented but had **zero production callers** (GGUF cards, continuum rollover, recall feedback, RunScheduled, sandbox settings, parts of sampling). The rule going forward:

> **A capability is either wired end-to-end (backend + API + UI + tests) or deleted. A stored-but-ignored setting is a defect.**

When adding a config field, grep for a consumer in the same change. When adding an endpoint, verify the frontend calls it (and vice versa).

# 9. Frontend contract

- The activity WebSocket reconnects automatically (exponential backoff); `done`/`error` always release the composer. Session create/delete rebind the socket and reset conversation state. If you touch session lifecycle, keep those invariants.
- `ActivityEvent.data.caption` is the display text (the backend `agent.Activity` contract) — formatters read caption first.
- The engine toggle AND the badge both read `engine.state` (never `models.llamaRunning`).
- The engine poll is stopped on view unmount.
- Recall feedback buttons send the exchange query (the user message preceding the reply) — the backend derives the same capsule id as `IndexTurn`.

# 10. Testing requirements

```bash
go test -tags headless ./internal/... -count=1
go test -race  -tags headless ./internal/agent/ ./internal/llm/ ./internal/api/ ./internal/native/engine/ ./internal/runtime/
go vet -tags headless ./...
npm run typecheck && npm run lint && npm run build
go run ./scripts/stress-main stress          # release gate (0 fail required)
node scripts/release-version.mjs --check     # version surfaces consistent
# C++ native engine (when toolchain available):
cmake -S native/engine -B native/engine/build && cmake --build native/engine/build
ctest --test-dir native/engine/build         # 12 suites: engine, protocol, host, gguf, model,
                                              #  tokenizer, kv_cache, scheduler, sampler
                                              #  + Phase 5: tensor, forward (vs the independent
                                              #    Python reference), generate
# Go↔C++ integration (skips when the host binary is not built):
go test -tags headless ./internal/native/engine/ -run 'TestRealCppHostEndToEnd|TestRealCppHostModelLifecycle|TestRealCppHostPhase4|TestRealCppHostPhase5'
#   Phase 6 regressions inside that set: PromptOwnership (the run-34703102794
#   dangling-prompt defect — 6 pipelined distinct prompts must report their own
#   token counts) and LaneRecycling (20 sequential generations on one host)
# Fixtures regenerate (deterministic; the reference comparison depends on them):
python3 native/engine/tests/reference/make_fixture.py
# Measured performance evidence (prints the measured table):
go test -tags headless ./internal/native/engine/ -run TestPhase5Benchmark -count=1 -v
```

New runtime features need a regression test at the level where a real user would notice the failure (HTTP-level for API changes, request-shape tests for wire fields, behavioral tests for loop mechanics).

# 11. CI / release discipline

`.github/workflows/build-desktop.yml` is version-agnostic: the release job runs for any `v*` tag and verifies `GITHUB_REF_NAME == v{APP_VERSION}Z`. Version bumps flow from `package.json` via `node scripts/release-version.mjs` (syncs `config.go`, `build/config.yml`, `SIGNATURE`, workflow `APP_VERSION`). Hand-edit nothing else for a bump — then re-run the `--check`.

Do not reintroduce hardcoded version literals in the workflow (grep literals derive from `APP_VERSION`; the release gate is `startsWith(github.ref, 'refs/tags/v')`).

# 12. Definition of done

```text
frontend action → API → runtime → real operation → state update
→ visible result → error path → cancellation → tests → verification
```

A button is not a feature. An endpoint is not a feature. A compile is not a feature. A commit is not proof.

# 13. Immediate next tasks (priority order)

```text
1. Native generation PERF: the Phase 5 forward pass is portable scalar
   C++ (dequant-then-dot) — measured SLOWER than llama.cpp (see
   worklog.md Phase 5 performance table). SIMD/block-fused matvec,
   thread-parallel prefill and quantized-compute paths are the next
   lever. Correctness is pinned by the Python reference; optimize under
   it.
2. Native generation SCOPE: more architectures (the derive/validate/
   forward separation keeps arch-specific behaviour explicit), K-quants,
   chat-template interpretation (currently a plain role-labeled prompt),
   tool-call formatting/parsing on the native path.
3. Native engine packaging: build + ship shtn-engine-host in the
   portable layout (bin/) with an update path (updater pattern).
4. Vision pipeline verification with a real mmproj projector
5. Phase 6 follow-ups: extend the failure classifier from measured
   end-to-end runs (tune categories/hints against real small-model
   transcripts); grow EvidenceCollector coverage (lint commands, more
   tool kinds); project-intelligence lessons from orchestrator
   failure→repair pairs (currently: Lab-verified commands only)
6. Continuum rollover exercise under real long sessions (it is wired +
   unit-tested; it has not yet been observed in a real multi-hour thread)
7. Context Engine foundations (PLANNED work — see ARCHITECTURE.md
   Part II; Phase 3 shipped the bounded chunk/cache/metrics layer it
   will build on, NOT the semantic index itself)
8. Model tier discovery + capability-based routing (PLANNED — see
   ARCHITECTURE.md §II.2)
```

# 14. Architectural direction (PLANNED — read `ARCHITECTURE.md` Part II)

The validated direction for SHEYTAN's future AI runtime, in one paragraph:
small fast local models are the foundation (`many efficient agents +
orchestration + tools + external memory + verification`, never one
giant model); a tier ladder (Tier 0 smallest → Tier 4 optional
high-end multimodal) routes work by capability with model-agnostic,
hardware-adaptive rules; the Context Engine treats model context as
working memory backed by external project memory (structural index →
semantic index → hierarchical retrieval → budgeted context builder);
context budgeting enforces the smallest sufficient working set;
specialized agents (planner, coder, researcher, tester, debugger,
documentation, reviewer, verifier) communicate through structured
artifacts (`analysis.json`, `patch.diff`, `findings.md`,
`test-results.json`); document editing flows through section-aware
retrieval, structured patches and objective validation gates where the
model is never the authority on correctness.

Every clause above is **future architecture**. The current runtime is a
sequential single-agent loop with one model per session (plus the
CLI-only sequential multiagent pipeline). Full details, current-state
notes and the candidate model examples (verified 2026-09, labeled
non-integrated) are in `ARCHITECTURE.md`.

# 15. Final rule

Prefer real behavior + verification + reliability over more panels, more settings, more visual features. The next agent must work from evidence, not assumptions.
