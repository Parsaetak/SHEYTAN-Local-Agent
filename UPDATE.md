# UPDATE.md — v1.1.6-zeta Phase 7 Replacement Package

**Release:** `v1.1.6-zeta` (codename Zeta, Phase 7) · **Base:** `main @ f3fe78a6ab7423fd35c55ba86c9a9c0cea5e9f38` (`v1.1.5-zeta`, Phase 6)
**Date:** 2026-09-13

This package is a COMPLETE REPLACEMENT of the repository state. Apply it by
replacing the whole tree (or, file-by-file, by following the exact map
below). Every path is relative to the repository root.

Phase 7 fixes the malformed llama.cpp `--flash-attn` / `--cache-reuse`
argument construction at its source, replaces the post-build context
overflow warning with a preflight budget pipeline that guarantees the final
request fits, and lays the Hermes-inspired foundations (dynamic toolsets,
verified-learning skills, specialist consultations, programmatic pipelines,
computer-use abstraction, MCP bridge, event scheduler, context telemetry,
self-improvement tactics) — without regressing any Phase 6 behavior.

---

## 1. DELETE

Files/directories removed from the repository. Reason stated per entry.

| Path | Why |
|---|---|
| `web/static/assets/AgentBody-2-br3YvV.js` | stale pre-Phase-6 Vite bundle; regenerated build produces the hashed set actually referenced by `web/static/index.html` |
| `web/static/assets/AgentHeader-B8b76u9o.js` | stale pre-Phase-6 Vite bundle (same reason) |
| `web/static/assets/AgentSidebar-DJaBwMC5.js` | stale pre-Phase-6 Vite bundle (same reason) |
| `web/static/assets/LabPanel-CW_A6EcB.js` | stale pre-Phase-6 Vite bundle (same reason) |
| `web/static/assets/ResearchPanel-Qeb2hrAX.js` | stale pre-Phase-6 Vite bundle (same reason) |
| `web/static/assets/SettingsPanel-C5Hpkvys.js` | stale pre-Phase-6 Vite bundle (same reason) |
| `web/static/assets/index-C1xyllm1.js` | stale pre-Phase-6 Vite bundle (same reason) |

## 2. REPLACE

Files whose entire contents were rewritten (new file replaces the old one
in full).

| Path | Why |
|---|---|
| `UPDATE.md` | this change map (Phase 6 map superseded) |
| `REPLACEMENT-MANIFEST.txt` | regenerated for the Phase 7 tree |
| `REPLACEMENT-SHA256.txt` | regenerated for the Phase 7 tree |

## 3. MODIFY

Files edited in place. Reason stated per entry.

| Path | Why |
|---|---|
| `internal/llm/speed.go` | **ROOT-CAUSE FIX (1):** version-aware Speed Pack emission. `SpeedArgs` now resolves the engine capability profile; `SpeedArgsWithCaps` emits `--flash-attn on`/`off` for value-form engines (new llama.cpp) or the bare flag for legacy engines, keeps `--cache-reuse N` an independent option, and drops per-option when the profile says the build lacks it |
| `internal/llm/llama.go` | **ROOT-CAUSE FIX (1+3):** capability-aware launch path — `detectCapsForBoot` before any launch; `buildArgsWithCaps` (also passes the model-aware `--ctx-size`); `launchWithRepair` validates the profile pre-spawn and performs surgical per-option repair (classify → repair ONLY the offending option → retry at the same compat level, bounded); verified startup state machine (`verifyStartupState`: `/v1/models` + `/props` probes, `VerifiedModel`/`VerifiedContext`/`Caps` accessors); verified profile persisted only after a successful start |
| `internal/contextplan/contextplan.go` | Phase 7 budget primitives: `SafetyMarginTokens` input, `Plan.SafetyMargin`/`PromptCeiling()`/`Overflow()`/`AddAdjustment()`/`SectionTokens()`, `ModelSummary`; `TotalTokens()` no longer double-counts the output reserve against the prompt ceiling |
| `internal/agent/orchestrator.go` | **ROOT-CAUSE FIX (2):** preflight budget pipeline in `RunDetailed` — model-aware effective context (`resolveEffectiveContext`), safety margin, optional blocks (recall / project card / skills / staged attachments) composed first and injected only when the plan keeps them, degradation ladder (dynamic toolset reduction → compact briefing → drop optional blocks), guaranteed-fit gates that refuse WITHOUT an engine call, non-double-counting history measurement, in-loop fit guard (`compactToolResults`), skills + telemetry + toolset integration |
| `internal/aicontext/aicontext.go` | `CompactSystemMessage` + `CompactMarker` for the context-pressure degradation ladder |
| `internal/aicontext/AI-CONTEXT.md` | runtime briefing updated: capability adapter replaces the compat-ladder description; context budget guarantee documented; context version marker 10 → 11 |
| `internal/multiagent/multiagent.go` | Phase 7C hook: bounded specialist consultations attached to the execution brief between planner and executor |
| `internal/runtime/runtime.go` | Phase 7 wiring: context-limit provider (native engine), skills store, context telemetry store, `pipeline` tool registration, scheduler instance + `StartScheduler` loop + `scheduleRunner` seam |
| `internal/llm/llama_test.go` | fake engine gains strict CLI-contract modes (`strict-new-args` reproducing the EXACT historical error, `strict-legacy-args`), argv recording (`SHEYTAN_FAKE_ARGS_OUT`), and `/v1/models` + `/props` endpoints |
| `package.json` | version 1.1.5-zeta → **1.1.6-zeta** (single source of truth) |
| `internal/config/config.go` | `AppVersion` 1.1.5 → **1.1.6** (synced by `scripts/release-version.mjs`) |
| `build/config.yml` | `productVersion` 1.1.5-zeta → **1.1.6-zeta** (synced) |
| `SIGNATURE` | first line `SHEYTAN-Local-Agent v1.1.5` → **v1.1.6** (synced) |
| `.github/workflows/build-desktop.yml` | `APP_VERSION` 1.1.5 → **1.1.6** (synced) |
| `README.md` | release header v1.1.6Z; new "Phase 7" capability section; engine lifecycle / context / tools rows updated honestly (18 tools, preflight pipeline, capability adapter); `llm.numCtx` key now documents the effective-window rule |
| `ARCHITECTURE.md` | Part I truth table gains 14 Phase 7 rows with honest status labels (IMPLEMENTED + TESTED / PARTIALLY IMPLEMENTED / IMPLEMENTED foundation); II.5 context-budgeting section updated (core implemented, deep retrieval still planned); II.6 multi-agent current-state note updated (specialist consultations ≠ parallel multi-agent) |
| `agent.md` | engineering handoff updated to v1.1.6Z: Phase 7 summary, next-agent notes (capability.go is the single CLI-contract authority; modelcaps.go the single capability authority; preflight wiring rules) |
| `worklog.md` | Phase 7 implementation log appended (root causes, fixes, evidence) |

## 4. ADD

New files/directories. Reason stated per entry.

| Path | Why |
|---|---|
| `internal/llm/capability.go` | Phase 7 engine capability layer: `EngineCaps` profile, `--help` detection + tag fallback, profile persistence (`engine-caps.json`), `argProblems` pre-launch validation, `ClassifyStartupFailure`, `repairCapsFor` surgical repair |
| `internal/llm/capability_test.go` | regression lock: the historical `--flash-attn --cache-reuse` pair can never be emitted; valid combinations validated; classification + surgical repair unit-tested; end-to-end repair test reaching READY with the corrected argv and persisted profile |
| `internal/llm/modelcaps.go` | Phase 7 model capability card (`ModelCapabilities`), `ResolveModelCapabilities` (real GGUF parse, cached), `ResolveEffectiveContext` (min of configured / model / engine), `launchContextSize` for `--ctx-size`, native architecture verdict |
| `internal/llm/modelcaps_test.go` | capability card parsed from the REAL GGUF fixture; effective-context clamps (small model, engine limit, never-raise rule) |
| `internal/toolsets/toolsets.go` | Phase 7A dynamic toolsets: 10 capability groups, tool→group map, deterministic task-signal selection, bounded reduction |
| `internal/toolsets/toolsets_test.go` | group selection, max-tools bound, determinism |
| `internal/skills/skills.go` | Phase 7B skills subsystem: skill schema, JSONL store, trigger matching, bounded rendering, `PromoteCandidate` verified-learning rule |
| `internal/skills/skills_test.go` | promotion requires `verified` objective evidence (all other verdicts rejected); store persistence, matching, budget |
| `internal/pipeline/pipeline.go` | Phase 7D programmatic pipelines: bounded stage execution (≤12 stages, per-stage timeout ≤600 s, 64 KiB output cap), stop-on-required-failure with skipped stages, evidence-grade report |
| `internal/pipeline/pipeline_test.go` | happy path, required-failure abort, optional-failure continue, cancellation, bounds |
| `internal/agent/pipeline_tool.go` | the `pipeline` agent tool + orchestrator registry runner adapter |
| `internal/computer/computer.go` | Phase 7E computer-use abstraction: observe/inspect/act/verify loop, deny-by-default risk policy, per-action timeout, verification predicates |
| `internal/computer/computer_test.go` | risk denial, gate override, verify-failure stop, timeout, cancellation, step bound |
| `internal/mcp/mcp.go` | Phase 7F MCP stdio bridge: JSON-RPC client (initialize/tools/list/tools/call), guarded registration pipeline (deny-by-default), bounded execution; OFF by default |
| `internal/scheduler/scheduler.go` | Phase 8 event/scheduler foundation: event taxonomy, task store, 5-minute timer floor, bounded runs, persisted reports, memory summaries, `Tick` loop |
| `internal/scheduler/scheduler_test.go` | interval policy, persistence, bounded manual runs, failure honesty, concurrency refusal, timer firing |
| `internal/ctxtelemetry/ctxtelemetry.go` | Phase 10 context-effectiveness telemetry: per-turn record, bounded JSONL, aggregation summary |
| `internal/ctxtelemetry/ctxtelemetry_test.go` | recording, aggregation math, compaction bound |
| `internal/improve/improve.go` | Phase 11 self-improvement tactics: candidate → active (two verified predictions) → retired (verified contradiction); unverified attempts change nothing |
| `internal/improve/improve_test.go` | promotion/retirement rules, unverified-never-promotes |
| `internal/contextplan/contextplan_phase7_test.go` | prompt-ceiling/safety-margin, exact-fit, overflow-by-one, output-reserve rejection, adjustment trail |
| `internal/agent/preflight_test.go` | preflight pipeline regressions: impossible budget → zero engine calls, toolset reduction on overflow, compact briefing on overflow, in-loop tool-result compaction (end-to-end via fake engine), telemetry recorded, compaction structure preservation, safety-margin clamps |
| `internal/native/engine/phase7_acceptance_test.go` | REAL-MODEL acceptance run through the native C++ boundary: real GGUF capability card → model-aware effective context (256 for the 256-ctx fixture) → preflight fit → real streamed generation (measured tokens + metrics) |

## 5. DO NOT TOUCH

Unrelated files/directories that Phase 7 deliberately did not modify.
Do not regenerate, reformat or "modernize" any of them.

| Path | Why untouched |
|---|---|
| `internal/lab/**` | Phase 6 Coding Lab (runner, verifier, safeedit, policy, repair) is authoritative and unchanged |
| `internal/agent/reliability.go`, `internal/agent/verification.go` | Phase 6 failure classification, loop guard, evidence collector — untouched and still authoritative |
| `internal/memory/**`, `internal/recall/**` | trust model and BM25 recall unchanged |
| `internal/continuum/**` | chapter rollover unchanged (the preflight pipeline composes with it, does not replace it) |
| `internal/sandbox/**`, `internal/proc/**` | security governors unchanged |
| `internal/updater/**` | engine download/update machinery unchanged (capability profile is stored beside it, not inside it) |
| `internal/browser/**`, `internal/tools/**`, `internal/research/**` | existing tool implementations unchanged (the Phase 7E computer-use layer wraps, not rewrites) |
| `internal/api/**` | local API surface unchanged in this phase (loopback/origin security untouched) |
| `native/engine/src/**`, `native/engine/include/**` | native C++ engine unchanged (Phase 5 performance rule: no native optimization this phase) |
| `scripts/**`, `web/embed.go`, `index.html`, `vite.config.ts`, `tsconfig*.json` | build/dev tooling unchanged |
| `LICENSE`, `FIX-README.md`, `go.mod`, `go.sum` | unchanged |
| `src/**` (React/TS sources) | frontend unchanged in Phase 7 (bundles rebuilt byte-identical, hashed names unchanged) |

## 6. TESTS — exact validation performed

All commands run at the final tree state (`v1.1.6-zeta`), Go 1.26:

```text
go build -tags headless ./...                                → PASS
go test -tags headless ./internal/... ./cmd/... -count=1     → PASS (all packages)
go test -race -tags headless ./internal/agent/ ./internal/llm/
         ./internal/api/ ./internal/native/engine/ -count=1 → PASS
go vet -tags headless ./...                                  → PASS
npm ci && npm run typecheck                                  → PASS (0 errors)
npm run lint                                                 → PASS (0 warnings, 0 errors, 20 files)
npm run build && npm run sync:web                            → PASS (web/static embedded bundle refreshed)
cmake -S native/engine -B native/engine/build && cmake --build native/engine/build → PASS
ctest --test-dir native/engine/build --output-on-failure     → 12/12 PASS
node scripts/release-version.mjs --check                     → PASS (all version surfaces = 1.1.6/1.1.6-zeta)
```

Focused Phase 7 regression suites (all PASS):

```text
go test ./internal/llm/ -run 'TestSpeedArgs|TestArgProblems|TestClassifyStartup|TestRepairCaps|TestEngineStartRepairs|TestEngineStartLegacy|TestStartupVerification|TestDetectEngineCaps|TestResolveModel|TestResolveEffective|TestLaunchContext|TestNativeArchitecture'
go test ./internal/agent/ -run 'TestPreflight|TestInLoop|TestCompactTool|TestContextSafety'
go test ./internal/contextplan/ ./internal/toolsets/ ./internal/skills/ ./internal/pipeline/ ./internal/computer/ ./internal/scheduler/ ./internal/ctxtelemetry/ ./internal/improve/
go test ./internal/native/engine/ -run TestPhase7SmallModelAcceptance
```

## 7. DOCUMENTATION — updated in this package

| Document | Change |
|---|---|
| `README.md` | Phase 7 capability section; engine/context/tools rows; version header |
| `ARCHITECTURE.md` | Part I Phase 7 rows; II.5/II.6 status updates |
| `agent.md` | v1.1.6Z handoff + Phase 7 next-agent notes |
| `worklog.md` | Phase 7 implementation log with evidence |
| `internal/aicontext/AI-CONTEXT.md` | runtime briefing: capability adapter + context budget guarantee (version 11) |
| `UPDATE.md` | this file |

## 8. ZIP

| Item | Value |
|---|---|
| Archive | `SHEYTAN-local-agent-1.1.6-zeta-phase7-replacement.zip` |
| Contents | the complete final repository source tree (see §9 exclusions) |
| Integrity | `REPLACEMENT-SHA256.txt` covers every file in the archive; verify with `sha256sum -c REPLACEMENT-SHA256.txt` |

## 9. ZIP exclusions (by design)

```text
.git/
node_modules/
models/
sessions/
logs/
caches/
tmp/
secrets/
native/engine/build/
*.exe, *.syso, *.zip
machine-specific runtime data (engine-caps.json is a runtime artifact created under DataDir at first boot, not shipped)
```
