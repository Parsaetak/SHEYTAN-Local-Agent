# SHEYTAN-Local-Agent — Agent Context

> Persistent engineering handoff for the next agent working on this repository.

Repository: https://github.com/Parsaetak/SHEYTAN-local-agent

Branch: `main`

Current release: `v1.2.3` (CI engine race fixed at the ROOT — the load/unload guard now covers the WHOLE in-flight generation window and the unload-guard test synchronizes on the first streamed token instead of polling; a reusable native-Go Download Manager (`internal/downloader`: HTTPS-only, Range resume, .part→verify→atomic rename, SHA-256/size pinning, ordered sources with explicit fallback trust, source cache, bounded backoff, cancel/pause, measured progress) now powers engine bootstrap, engine self-update and app-update staging; app update staging is ASYNC with live progress + `/api/update/cancel`; downloader progress UI (phase chain, %, speed, ETA, source, verification) across Updates/Runtime/System with a header percent pill; model-picker skeletons; two known native warnings cleaned; see the v1.2.3 notes below — all prior notes remain authoritative). Prior line: `v1.2.2` (generation visibility & black-screen repair: error boundaries everywhere, `gpus: null` wire defect fixed, live-run socket lease across tab switches + `idle`-sentinel resync, live generation timeline, cumulative-snapshot streaming fix, partial-output preservation, honest engine-asset errors, log session banners; see the v1.2.2 notes below — all prior notes remain authoritative). Prior line: `v1.2.1` (CI package-root contract fix + installer options + packaging hardening; see the v1.2.1 notes below — all prior notes remain authoritative). (SHEYTAN-LA unified product upgrade: vision readiness state machine, hardware intelligence, evidence-based recommendation engine, chat markdown/composer polish, Environment Centre, verified health, SHEYTAN-LA Windows identity + AUMID, NSIS installer, manifest-verified app updater, release checksums; see the v1.2.0 notes below — all prior notes remain authoritative). Prior line: `v1.1.9` (CI output-name fix + mode-aware navigation + explicit model states on top of v1.1.8; see the v1.1.9 notes below. The v1.1.8 Chat/Agent separation + model picker + CI release-identity fix, the v1.1.7 options/telemetry/diagnostics work and the v1.1.6-zeta stabilisation before it remain authoritative: context is per-session and per-agent — `sessions.Context.ContextTokens` + `llm.ResolveSessionContext` (min of session policy / global / GGUF max / engine-verified window), resource-aware classification (`internal/llm/resources.go`), full per-turn context telemetry, wire-level `n_ctx` truthfulness; startup shows real phases and `ready` means VERIFIED serving; Windows icon (16–256 ladder + `build/sheytan.ico` via `scripts/gen-syso`), per-layer `SHEYTAN — X` branding, a central theme-token system, and a real Settings scroll container. Phase 7: runtime stability + context intelligence + Agent OS foundation. The llama.cpp launch contract is now detected, validated and surgically repaired per option — the historical `--flash-attn`/`--cache-reuse` malformed-argument failure is fixed at the source and regression-locked. Context is a preflight budget pipeline with a guaranteed fit: model-aware effective window, safety margin, dynamic toolsets, compact-briefing fallback, in-loop tool-result bounding, and an honest refusal (no engine call) when the budget is impossible. Foundations wired: dynamic toolsets, verified-learning skills, specialist consultations, programmatic pipelines, computer-use abstraction, MCP bridge (off by default), event scheduler, context telemetry, self-improvement tactics. Phase 5 (real native inference) and Phase 6 (reliability + verification + safe edits + project intelligence) remain authoritative — see `worklog.md` for the full phase logs).

**v1.2.3 notes for the next agent:**

- ENGINE GUARD — `generation_in_flight` (native/engine/src/engine.cpp) is
  the SINGLE authority for load/unload rejection: registered work + the
  scheduler's active slot + the executor's `active_generations`. Do not
  reintroduce partial guards: any observable "active request" MUST imply
  SHTN_ERR_MODEL_STATE for load/unload. The unload-guard test waits on the
  FIRST STREAMED TOKEN (condvar, 60 s bound) — an explicit data signal
  emitted from inside the runner — never on sleeps or poll budgets.
- DOWNLOAD MANAGER — ALL remote asset fetches go through
  `internal/downloader` (engine bootstrap, engine self-update, app-update
  staging). Never add a second HTTP-download path: the manager owns the
  HTTPS-only rule (loopback exemption for tests only), .part → verify →
  atomic rename, resume, mirrors-with-trust, backoff and progress. Trust
  boundary: `TrustFallback` sources are SKIPPED unless the caller sets
  `AllowFallback` explicitly; untrusted mirrors are never silently
  substituted.
- ASYNC STAGING — `/api/update/download` returns immediately; progress is
  polled from `/api/update/status` (`download` block); `/api/update/cancel`
  stops network+file activity and KEEPS the .part. Close() cancels in-flight
  staging. Engine downloads publish progress via EngineEvent.Download
  (WS `engine` frames + `/api/engine` snapshot) and are cancelled with
  `POST /api/llama {action:"cancel-download"}`.
- FRONTEND — the shared downloader visual lives in `src/DownloadProgress.tsx`
  + the `.dl-*` styles (styles.css). The model picker distinguishes
  "loading" (`modelsLoading` in the store) from "no models". The header pill
  shows a live percentage during engine bootstrap.
- VERSION — 1.2.3 via the identity chain (package.json →
  release-version.mjs).
- VERIFIED — native ctest 12/12 (incl. 3 runs under CPU saturation);
  `go test ./internal/... -tags headless` + `go test ./...` + `go vet` green;
  downloader suite race-clean ×3; stress suite 47/47 (incl. the two new
  downloader hardening scenarios); typecheck/lint/build green.

**v1.2.2 notes for the next agent:**

- ERROR BOUNDARIES — `src/ErrorBoundary.tsx` ships `AppErrorBoundary`
  (root, wraps `<App/>` in `main.tsx`) and `PanelErrorBoundary` (one per
  lazy workspace panel in `App.tsx`, `resetKey` = view id). A panel render
  failure now shows that panel's recover/reload card. NEVER delete these
  — the pre-v1.2.2 app had ZERO boundaries, so one `gpus.length`-on-null
  TypeError blanked the entire window (the reported black screen).
- GPU WIRE CONTRACT — `hardware.Collect` and `sysinfo.Probe` now
  guarantee `"gpus": []` on the wire (nil Go slices marshal as `null`).
  `internal/hardware/hardware_test.go` locks it. The frontend ALSO
  defends (`Array.isArray(device.gpus)` in SystemPanel) — keep both
  layers; the WMI/PowerShell probe genuinely fails under full inference
  load, which is why the System tab crashed DURING generation.
- STREAMING WIRE CONTRACT — the orchestrator's `emitProgress` publishes
  CUMULATIVE captions: every `response`/`reasoning` activity event
  carries the FULL text so far. `src/stream-accumulator.ts` (+ unit
  tests via `npm run test:units`, Node's native TS runner) implements
  replace-semantics; the OLD store appended every caption and duplicated
  the streamed text. If you ever touch the streaming path again, do NOT
  go back to append semantics.
- SOCKET OWNERSHIP — `store.ts` exposes `acquireActivity`/
  `releaseActivity` (+ engine-poll equivalents, refcounted). AgentBody
  acquires on mount, releases on unmount; a LIVE run keeps the socket
  across workspace tab switches (the backend `activityHub` has NO event
  replay — unmount-disconnecting lost `done` and froze the composer
  forever). The backend `idle` sentinel is now handled: a run the UI
  believes live but the backend no longer registers is re-synced from
  authoritative history (`recoverRunFromIdle`, with a 2.5 s grace guard
  against the standby attach race right after Send).
- RUN PHASES — `src/run-phase.ts` is the pure lifecycle machine
  (`idle/preparing/thinking/generating/finalising/complete/error/
  aborted`), unit-tested; the store feeds it real wire events only.
  `MessageStream.tsx` renders the GenerationBubble whenever a run is
  live: phase badge, elapsed clock, collapsible reasoning (auto-open
  while thinking, auto-fold on first answer token, user intent wins
  through `onToggle`), streamed text, progressive activity strip.
  Finalisation (`finaliseRun`) confirms the history actually gained the
  assistant reply and otherwise promotes the partial locally — errors
  and aborts never discard the streamed output.
- ENGINE ASSET ERRORS — `updater.IsNoAssetError` classifies "upstream
  ships no prebuilt asset" vs network failure; `llamaDownloadURL`
  reports each with its real remedy (the old text claimed "no prebuilt
  asset (upstream no longer publishes Linux binaries)" on
  GitHub-blocked WINDOWS machines). GitHub/Atom socket failures stay
  non-fatal WARNs by design.
- LOG SESSIONS — `logging.Manager.SessionBanner` writes one
  `==== SHEYTAN-LA v<ver> session start (pid N) ====` line at every boot
  (wired in `cmd/root.go`); everything above a banner is verifiably
  historical in app.log and the in-app Log Viewer.
- VERSION — 1.2.2 via the identity chain (package.json →
  release-version.mjs). Frontend regression tests run with
  `npm run test:units` (no new dependencies).

**v1.2.1 notes for the next agent:**

- PACKAGE-ROOT CONTRACT — the workflow defines `WIN_PKG_ROOT: "SHEYTAN-LA"`
  and `LINUX_PKG_ROOT: "SHEYTAN-Local-Agent"` ONCE (workflow env block) and
  derives every packaging path from them. `internal/releasecontract.go`
  mirrors them: `WorkflowWinRootEnvLine` / `WorkflowLinuxRootEnvLine`, the
  `${env:WIN_PKG_ROOT}` / `${LINUX_PKG_ROOT}` slot spellings, staging-dir
  slots and `Required*ZipWorkflowEntries()`. The stress gate (3e2/3h/3i in
  `cmd/stress_zeta.go`) fails the build when the workflow loses any of
  these — do NOT reintroduce a root literal anywhere in the packaging
  path; that is the exact run-34871838054 failure class (the Linux ZIP was
  verified against `SHEYTAN-LA/` while created under
  `SHEYTAN-Local-Agent/`, and the Go contract's own
  `RequiredLinuxZipEntries` mixed roots so the gate stayed green).
- NSIS INSTALLER — desktop shortcut is a checkbox on the directory page
  (`DirectoryPageShow`, default CHECKED via `.onInit` →
  `CreateDesktopShortcut = BST_CHECKED`); silent installs keep the
  default. Upgrades close a running instance (graceful taskkill, bounded
  retry, Retry/Cancel on a locked exe). The uninstaller must keep plain
  `RMDir` only — a CI contract check FORBIDS `RMDir /r` in the script and
  requires the `preserve user data` marker; user data
  (%LOCALAPPDATA%\SHEYTAN-LA + SHEYTAN_DATA_DIR) is never touched. CI
  greps the SOURCE for these fragments before building — keep them intact.
- PACKAGING GATES — in-package `BUILD-INFO.txt` must carry the resolved
  `Version:` (verified inside both ZIPs); job-level `timeout-minutes`
  bound all four jobs; `native/engine/build` is cached on
  `hashFiles('native/engine/**')` (exact source hash — reproducible).
- STRESS — new scenarios live in `cmd/stress_release.go`
  (`stressReleaseScenarios`): updater integrity, vision honesty,
  memory/session robustness, bounded context machinery, LoopGuard
  ceiling. The suite ends with a machine-readable
  `STRESS-RESULT pass=N fail=M hangs=0 crashes=0` line.
- VERSION — 1.2.1 via the identity chain (package.json →
  release-version.mjs). The updater needs a strictly higher version to
  offer the improved installer.

**v1.2.0 notes for the next agent:**

- VISION STATE MACHINE — `internal/vision/states.go` is the single
  authority: `unsupported/supported/projector-missing/projector-found/
  projector-verified/loading/ready/degraded/failed`. Pre-boot state comes
  from `vision.EvaluateModel` (override wins if it exists; a paired mmproj
  beats the architecture allow-list; the allow-list is CONSERVATIVE —
  extend `visionArchs` only for arches llama.cpp mtmd actually supports).
  `ready` is ONLY reachable through a verified boot WITH the projector
  (llama.go setVision call sites at the setState(StateReady) paths).
  `/api/models` carries per-model `visionFields`; `/api/engine` carries
  the runtime block. The UI renders via `src/vision.ts visionBadge()` —
  do not invent states client-side.
- RECOMMENDATIONS — `internal/recommendation.Recommend` owns
  Detected→Calculated→Recommended. It never mutates config: the UI applies
  through the existing `PUT /api/config` and measures through `/api/perf`.
  New task profiles must be added to `Tasks()` (single source) and stay
  explainable — every branch must append a reason or a note.
- SETTINGS — the Performance tab has three LEVELS (Simple/Performance/
  Advanced) in `PerformanceLevel` (SettingsPanel.tsx); Simple postures are
  task profiles (low-power/chat/maximum). New performance surfaces belong
  in the right LEVEL, not all three.
- IDENTITY — `internal/platform` owns OS integration: AUMID
  `Parsaetak.SHEYTAN-LA` is set in `desktop.Run` BEFORE the first window.
  Windows version resources come from `config.AppShortName /
  AppDescription / AppPublisher / ExecutableName` via gen-syso. The
  firewall manager creates NOTHING by default (loopback needs none) and
  its netsh argument vectors are unit-locked — changing scope is a
  reviewed security decision.
- UPDATES — `internal/updater/appupdate.go`: the release manifest
  (`release-manifest.json`, produced by CI) is authoritative; downloads
  are SHA-256 verified BEFORE staging under `<DataDir>/updates/staging`;
  nothing is ever auto-executed. `/api/update/status|check|download` keep
  the last check in `Server.lastAppUpdate` (atomic.Value).
- RELEASE ARTIFACTS — CI now produces `SHEYTAN-LA-v<ver>-windows-x64.zip`,
  `SHEYTAN-LA-v<ver>-windows-x64-installer.exe` (NSIS,
  packaging/nsis/installer.nsi), `SHA256SUMS.txt` and
  `release-manifest.json`. The Linux artifact keeps its existing name.
  Signing is still NOT configured — the installer verification step
  REPORTS Authenticode status honestly instead of failing or faking.
- The vision retry contract is unchanged: one text-only fallback when the
  projector fails with every profile — now surfaced as `degraded` with a
  reason instead of a silent downgrade.

**v1.1.9 notes for the next agent:**

- CI run `34791882219` root cause: the audit job's `outputs:` mapped
  `steps.identity.outputs.version` / `version_full` / `codename`, but
  the identity step writes `APP_VERSION` / `APP_VERSION_FULL` /
  `APP_CODENAME` to `$GITHUB_OUTPUT` (the exact key names from
  `release-version.mjs --env`). Every `needs.audit.outputs.*` therefore
  resolved to "" and Linux/Windows failed metadata verification. The
  fix is the mapping (audit outputs now use the `APP_*` names) plus a
  fail-fast empty-identity assertion inside the identity step. If you
  ever rename the emitted keys, you MUST rename the audit output
  mappings in the same commit — they are two spellings of one contract.
- Navigation is mode-aware: `WorkspaceLayer.modes` in
  `src/workspace.ts` + `visibleWorkspaceLayers(mode)`; `App.tsx`
  filters the nav and falls back to the workspace view when the
  restored hash points at machinery the current mode hides (e.g. Lab
  while Chat). The agent view label is "Workspace" — do not confuse it
  with the Agent MODE (the `[ Chat │ Agent ]` switch in the header).
- Model cards (`src/ModelPicker.tsx`) carry an explicit `ModelState`:
  `ready` (serving), `loading` (busy switch targeting THIS model),
  `incompatible` (estimated footprint > total host RAM), `available`.
  The fact grid shows Context / RAM / Tools / Vision / Native; Tools is
  derived ONLY from `chatTemplate` (the honest prerequisite signal) and
  renders "—" when unknown. Do not add fields the backend does not
  report; there is still NO Remove action (no deletion API).
- Settings: the llama.cpp EngineCard moved from the performance tab to
  Advanced in `src/SettingsPanel.tsx`. `touchesEngine` restart logic is
  unchanged and tab-independent.
- CSS: `--border-soft` is now a defined token (it was referenced by
  five blocks but never declared). The runtime panel's model facts use
  `.runtime-model-facts` (renamed from `.model-card-facts`, which the
  picker uses for its inline facts line). Background gradients and the
  body grid overlay are gone — keep the background flat.
- `PerfStrip` skips `/api/perf` fetches while `document.hidden` and
  refreshes on visibilitychange. The 2.5 s `/api/engine` poll in the
  store is unchanged.

**v1.1.8 notes for the next agent:**

- Release identity has ONE source: `package.json`. The workflow NO
  LONGER carries `APP_VERSION`/`APP_CODENAME` env constants — the audit
  job resolves them at runtime (`node scripts/release-version.mjs
  --env`, step id `identity`) and every other job consumes them via
  `needs.audit.outputs.*`. To bump the version: edit `package.json`, run
  `node scripts/release-version.mjs`, commit. Do NOT reintroduce a
  hardcoded version constant anywhere — that was the root cause of run
  `34788709977`. The `--env` flag prints `APP_VERSION`,
  `APP_VERSION_FULL`, `APP_CODENAME`; the workflow shape check lives in
  `release-version.mjs`'s target list.
- Chat/Agent separation is a UI-only concern: `WorkspaceMode` in
  `src/store.ts` (`mode` + `setMode`, persisted under `localStorage`
  key `sheytan.mode`, default `chat`). Both modes share the SAME
  session/model/engine wiring in `AgentBody.tsx`; do not fork runtime
  state per mode. Mode decides VISIBILITY: Chat hides the runtime
  panel, context pills (`AgentHeader`), telemetry and activity; Agent
  shows everything.
- The model picker (`src/ModelPicker.tsx`) renders ONLY backend facts
  from `/api/models`. The new per-model fields (`multimodal`,
  `nativeBackend`, `chatTemplate`, `nativeReason`, `estimatedVRAMBytes`)
  come from `llm.ResolveModelCapabilities` via the SAME bounded
  path+size+mtime cache as the header card (`modelCardFor` in
  `internal/api/server.go` — now also caches caps). Do not compute
  capabilities client-side; do not remove the Remove-less action set
  (there is no model-deletion API by design).
- `switchModel` in `AgentBody.tsx` returns success and owns the engine
  restart dance; the picker closes only when it returns true. Keep ONE
  implementation — the chat rail select, the agent panel select, and
  the picker all route through it.
- Settings tabs: Models and Advanced were split out of General
  (`TABS` + `SettingsTab` in `src/SettingsPanel.tsx`). The
  restart-after-save condition (`touchesEngine`) must still list every
  engine-affecting config key regardless of which tab hosts it.
- Performance strip (`src/PerfStrip.tsx`) polls the EXISTING `/api/perf`
  every 4 s while mounted and renders N/A for anything unmeasured —
  same honesty contract as the perf HUD.

**v1.1.7 notes for the next agent:**

- `shouldRetryFullSpeed` (`internal/llm/capability.go`) is the ONLY path
  that may re-attempt compatibility level 0 after a persisted downgrade.
  Its four gate conditions are regression-locked in
  `capability_compat_test.go` — extend the gate and the tests together.
  `engineCompatReason`/`engineCompatAt` in config are backend-managed:
  never let a UI patch write them.
- `/api/perf`, `/api/logs`, `/api/netcheck` (`internal/api/perf.go`,
  `logs.go`, `netcheck.Diagnose`) are read-only surfaces over EXISTING
  infrastructure (sysinfo probe, logging ring, netcheck). If you add a
  metric, it must come from a real measurement — the UI renders N/A for
  anything absent, and fabricated values are a defect, not a placeholder.
- Tool descriptions: `ShortDescription()` (one line, UI) vs
  `Description()` (full spec, model). Never shorten the model-facing one;
  the JSON action syntax lives there.
- The Settings restart-after-save condition (`touchesEngine` in
  `src/SettingsPanel.tsx`) must list every engine-affecting config key —
  add new engine options there AND to `EngineCaps`/`argProblems`/
  `repairCapsFor` together.
- The log viewer displays entries redacted by `logging.RecentParsed`;
  keep `redact()` ahead of any new secret-shaped string the app logs.

**v1.1.6-zeta context (previous release):** stabilisation on top of Phase 7: the `internal/improve` recursive-mutex CI deadlock is fixed and regression-locked; context is per-session and per-agent — `sessions.Context.ContextTokens` + `llm.ResolveSessionContext` (min of session policy / global / GGUF max / engine-verified window), resource-aware classification (`internal/llm/resources.go`), full per-turn context telemetry, wire-level `n_ctx` truthfulness; startup shows real phases and `ready` means VERIFIED serving; Windows icon (16–256 ladder + `build/sheytan.ico` via `scripts/gen-syso`), per-layer `SHEYTAN — X` branding, a central theme-token system, and a real Settings scroll container. Phase 7: runtime stability + context intelligence + Agent OS foundation. The llama.cpp launch contract is now detected, validated and surgically repaired per option — the historical `--flash-attn`/`--cache-reuse` malformed-argument failure is fixed at the source and regression-locked. Context is a preflight budget pipeline with a guaranteed fit: model-aware effective window, safety margin, dynamic toolsets, compact-briefing fallback, in-loop tool-result bounding, and an honest refusal (no engine call) when the budget is impossible. Foundations wired: dynamic toolsets, verified-learning skills, specialist consultations, programmatic pipelines, computer-use abstraction, MCP bridge (off by default), event scheduler, context telemetry, self-improvement tactics. Phase 5 (real native inference) and Phase 6 (reliability + verification + safe edits + project intelligence) remain authoritative — see `worklog.md` for the full phase logs.

**Phase 5 repair (2026-09-11)**: the phase5 commit had accidentally deleted `build/config.yml` and four internal packages (`sessions`, `sandbox`, `attachments`, `memory`) that live code still imports — the tree did not compile and CI failed at the release gate. All were restored byte-identical from the Phase 4 baseline; two real native-path defects (misleading engine badge state; run gate requiring llama.cpp when native serves) and one CI gap (Go↔C++ integration tests never executed in any job) were fixed. Evidence in `worklog.md` — "v1.1.5Z Phase 5 Repair Log".

**Phase 5 validation pass (2026-09-12)**: every validation stage re-run independently and clean; two more real defects fixed — the engine toggle never loaded a model natively (alive-but-incapable engine; runs still gated on llama.cpp; fixed via the `Stack.EnsureNativeReady` seam + honest toggle result), and the llama.cpp auto-download could never succeed on Linux again (upstream removed Linux prebuilt binaries; fixed with a bounded probe → release scan → honest actionable error). llama.cpp no longer publishes Linux binaries — Linux users build from source (`llamaBinPath`) or select the native engine. Evidence in `worklog.md` — "v1.1.5Z Validation & Repair Log".

**Read `worklog.md` before working.** It records the audit findings and the fixes this release shipped, including which subsystems were previously unwired and why.

**Read `ARCHITECTURE.md` for the implementation truth table and the validated future direction.** Its Part I now carries the Phase 7 rows (engine capability adapter, model capabilities, preflight budget pipeline, dynamic toolsets, skills, specialists, pipelines, computer use, MCP, scheduler, telemetry, self-improvement) with their honest IMPLEMENTED / PARTIALLY IMPLEMENTED / foundation status. Its Part II records what is still planned (tiered model routing, hierarchical retrieval, parallel multi-agent, artifact communication, document editing) — never present those as current capability. Its Part III defines the documentation truth standard every change must follow.

**Phase 7 notes for the next agent:**

- `internal/llm/capability.go` is the single authority for the llama.cpp
  CLI contract. Never hard-code a flag layout again: extend `EngineCaps`
  and the `--help` parser instead. The verified profile lives in
  `DataDir/engine-caps.json`, keyed by release tag.
- `internal/llm/modelcaps.go` is the single authority for model
  capabilities. The effective context is `min(configured, GGUF limit,
  engine limit)` — never raise a window beyond what the model declares.
- The orchestrator preflight (`RunDetailed`) composes optional blocks and
  injects them only when the plan keeps them. If you add a new prompt
  section, wire it through `contextplan.Assemble` — do not bypass the
  plan.
- The compat ladder (levels 1–3) now fires only after per-option surgical
  repair fails. When adding engine options, add them to `EngineCaps`,
  `argProblems` and `repairCapsFor` together.
- Every new subsystem (toolsets, skills, pipelines, computer, mcp,
  scheduler, ctxtelemetry, improve) inherits the security invariants —
  loopback-only, path jails, sanitized env, bounded resources,
  deny-by-default risk policy.

---

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

# 3. Architecture (v1.1.5Z)

Backend: Go 1.26, Wails v3 (desktop shell), Go HTTP API + WebSocket on `127.0.0.1:8765`.

Engine stack (v1.1.5Z Phase 5 — the native engine performs REAL transformer inference for the llama architecture and serves REAL generation with streaming + cancellation + measured metrics; llama.cpp remains the default/fallback and serves every model the native engine cannot execute):

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
                     + Backend contract + LlamaBackend + selection (v1.1.5Z)
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
internal/continuum   chapter rollover (wired post-run since v1.1.4Z)
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

Violating this contract reintroduces the v1.1.3Z data race (`*s.cfg = updated` in the patch handler). `TestSourceConcurrentReadWrite` and `TestConfigPatchIsRaceFree` guard it under `-race` — keep them passing.

# 4b. Data-pipeline rules (v1.1.5Z Phase 3)

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
- v1.1.5Z Phase 5 backend rules: generation is routed by `llm.SelectGenerationBackend` (wired through the orchestrator's generation router — `Stack.streamGeneration`) — the native engine only when selected (`engineBackend: "native"`) AND `GenerationCapable()` (alive + a loaded model whose llama graph validated at load time); otherwise llama.cpp. Request shapes the native path cannot serve (tools, images) and pre-first-token native failures fall back to llama.cpp with the reason logged and inspectable. Native engine failures never fail the llama path (best-effort, logged, visible in `native.state`).
- Native MODEL loading (Phase 2) is real: `LoadModel` validates the file and loads it natively (GGUF validate → memory-map → metadata → memory plan). Model states use their own dedicated vocabulary — `unloaded/loading/loaded/failed` — separate from the engine states above; a host restart resets the model state (a fresh host maps nothing). Loading a model does NOT enable generation.
- v1.1.5Z Phase 5 native generation (REAL): the native engine performs the actual transformer computation for the llama architecture — token embeddings → per-layer RMSNorm → Q/K/V projection → RoPE → causal GQA attention over a REAL fp16 KV cache → output projection + residual → RMSNorm → SwiGLU FFN + residual → final norm → logits. The sampler consumes the REAL logits (temperature, top-k, top-p, repetition penalty, seed — the Phase 4 primitives). Generation streams coarse-grained chunks over IPC (one generate request → event frames → final frame; NEVER one frame per token), supports REAL cooperative cancellation (observed every token), stops honestly (EOS without emitting it / max_tokens / context bound / error) and reports MEASURED metrics only (prompt tokens, generated tokens, prefill time, TTFT, decode tok/s, KV positions — monotonic clock). The context bound is a REJECT policy: prompt+max_tokens beyond the context window fails explicitly (no silent truncation).
- Native generation support is NARROW and honest: architecture llama only; tensor types F32/F16/Q4_0/Q4_1/Q5_0/Q5_1/Q8_0 only; RoPE freq_scale 1.0 only; no chat-template interpretation (plain role-labeled prompt format — llama.cpp keeps template fidelity); tools/images requests return ErrNotImplemented (the router sends them to llama.cpp). A model outside the supported set loads fine but reports `generationCapable=false` with an inspectable reason; generation selection (`llm.SelectGenerationBackend` → native only when selected AND capable) then routes to llama.cpp. A native failure BEFORE the first streamed token falls back to llama.cpp with the reason logged; after the first token it surfaces like any engine error.
- The native KV cache now stores REAL fp16 bits (uint16_t, Phase 5 corrected the Phase 4 float[]-but-reported-f16 defect: capacity_bytes == the actual allocation, layer offsets exact, used_bytes consistent with written positions — pinned by regression tests). The cache allocates at the first generate (sized from model dims) and resets per request; `kv_cache_info` reports the measured population.
- The native scheduler runs a REAL single-slot worker thread (max concurrent = 1): queued/active/completed/cancelled/failed counts are real; the generation lane in the host keeps the dispatch loop responsive (cancel/metrics/shutdown while generating).
- Model loads now compute the llama-graph verdict (metadata-level: every required tensor present with the right shape and a supported type) and report `generationCapable` + `generationReason` in model_info. Pre-warm loads the selected model natively and logs the verdict; an incapable model keeps llama.cpp serving generation.
- The native engine host (`shtn-engine-host`) runs with a sanitized environment, bounded op timeouts (10 s; model loads 30 s), a 1 MiB frame cap and a protocol/ABI handshake that fails closed (protocol/ABI v4 — both sides bumped together; v4 adds the generate/cancel ops with streamed event frames + real cancellation to v3; generation runs on bounded lanes so the host stays responsive mid-generation; malformed generate payloads and host-side errors are bounded error frames, never crashes). Build it from `native/engine/` (CMake or Make); this phase does not ship or auto-download it.
- v1.1.5Z Phase 4 frontend perf contract: streaming model output is COALESCED through `flushStreaming` (rAF-boundary batching in `store.ts`). The UI updates at most once per frame regardless of token rate — a model emitting 200 tokens/sec no longer triggers 200 React renders/sec. Lifecycle events (done/error/session) bypass the coalescer and reset state immediately. A frame-budget diagnostic HUD (`src/perf-hud.ts`) is OFF by default — toggle with Ctrl+Shift+P or `window.__shtnTogglePerfHUD()`. The HUD measures real frame time, dropped frames, longtask count and coalesced stream-update frequency. The target budget is auto-detected from the display refresh rate (8.33 ms for 120 Hz, 16.67 ms for 60 Hz) — the HUD does NOT claim guaranteed 120 FPS; it reports `optimized for high-refresh displays / frame-budget aware / 120 Hz-capable presentation where hardware permits`.

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

# 8. Wired-surface contract (the v1.1.4Z lesson)

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
