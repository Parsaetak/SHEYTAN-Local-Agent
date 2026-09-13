# UPDATE.md — v1.1.6-zeta Stabilisation & Context Control Replacement Package

**Release:** `v1.1.6-zeta` (codename Zeta, stabilisation) · **Base:** `main @ a1cfb5d0bdb5382b8c69b5587891f5edf4d96ddb` (`1.1.6`)
**Date:** 2026-09-13

This package is a COMPLETE REPLACEMENT of the repository state. Apply it by
replacing the whole tree (or, file-by-file, by following the exact map
below). Every path is relative to the repository root.

This phase fixes the Linux CI deadlock in `internal/improve` at its source,
closes the context-overflow path architecturally (per-session and per-agent
context policies, model/engine-aware resolution, resource-aware guards, full
per-turn telemetry, wire-request context truthfulness), upgrades the startup
and first-use experience, ships the SHEYTAN icon/theme/branding layer, and
fixes Settings scrolling — without regressing any existing subsystem
(managed llama.cpp lifecycle, native C++ engine, backend router, Coding Lab,
project intelligence, research, memory, recall, context cache, Continuum,
skills, toolsets, MCP, browser/computer tooling, verification, reliability,
safe editing, sandbox, security controls, streaming, Wails embedding,
API/WebSocket contracts).

---

## 1. DELETE

| Path | Why |
|---|---|
| `web/static/assets/AgentBody-2-br3YvV.js`, `AgentBody-cofMEBW6.js`, `AgentHeader-B8b76u9o.js`, `AgentHeader-BE0L2Nf8.js`, `AgentSidebar-DJaBwMC5.js`, `AgentSidebar-crN0VbUz.js`, `LabPanel-BPBo4Rq7.js`, `LabPanel-CW_A6EcB.js`, `ResearchPanel-DyeLdq3-.js`, `ResearchPanel-Qeb2hrAX.js`, `SettingsPanel-C5Hpkvys.js`, `SettingsPanel-g7g40a_x.js`, `index-C1xyllm1.js`, `index-OiRp_Wkd.js`, `index-n0UF0DI4.css` | stale Vite bundles; the regenerated embedded build produces the hashed set actually referenced by `web/static/index.html` |

## 2. REPLACE

| Path | Why |
|---|---|
| `UPDATE.md` | this change map (Phase 7 map superseded) |
| `REPLACEMENT-MANIFEST.txt` | regenerated for this tree |
| `REPLACEMENT-SHA256.txt` | regenerated for this tree |

## 3. MODIFY

| Path | Why |
|---|---|
| `internal/improve/improve.go` | **P0 FIX (CI deadlock):** `Load()` split into the locking public method and a `loadLocked()` private reader; `Propose()`, `RecordAttempt()` and `Active()` use `loadLocked()` under the already-held mutex — the recursive `sync.Mutex` acquisition (Lock → Load → Lock) that hung `TestProposeRequiresCompleteDefinition` for 10 minutes is gone; load/save errors are now propagated instead of swallowed |
| `internal/improve/improve_test.go` | **P0 regression tests:** `TestProposeCompletesImmediately`, `TestRecordAttemptCompletesImmediately` (watchdog-guarded), `TestConcurrentStoreAccess` (8 writers × 12 attempts + concurrent readers, race-safe, no lost updates), `TestPersistenceRoundTrip`, `TestEvidenceBounded`, `TestStoreBoundedToMaxTactics` |
| `internal/sessions/sessions.go` | **Context policies (§4):** `sessions.Context.ContextTokens` — the per-chat context-window policy persisted with the session (0 = inherit global); Chat A = 8K, Chat B = 32K, Chat C = 16K are independent and restored on switch |
| `internal/llm/modelcaps.go` | **Context resolution (§3/§4):** `EffectiveContext` gains `Requested`/`SessionPolicy`; new `ResolveSessionContext` — effective = min(session policy, global configured, model GGUF max, engine-verified window), floored at 1024, every clamp recorded in `Reasons` |
| `internal/llm/llama.go` | **Readiness (§11):** `EngineContextLimit()` (verified `/props` window while the subprocess is alive; 0 when dead so a stale window can never clamp the next plan), `VerifiedReady()` (alive + `/health` 200 + serving model verified) |
| `internal/llm/client.go` | **Wire truthfulness (§2):** `BuildChatRequestWithOptions` — the request's `n_ctx` carries the SAME effective context the planner validated, never a second, larger global number |
| `internal/llm/resources.go` | **NEW.** Resource protection (§8): `AssessContextResource` (weights + KV-cache + runtime overhead vs RAM/VRAM → `safe` / `caution` / `unsupported` with a user-facing reason), `KVCacheBytes` estimator (layers × embedding × KV quant from the GGUF card), `ContextOptions` 4K…128K selector ladder with per-rung verdicts and model/engine clamp flags |
| `internal/llm/resources_test.go` | **NEW.** Tests: session-context inheritance/narrowing/clamps/floor, resource unsupported/safe/unknown-RAM classification, selector ladder correctness |
| `internal/agent/orchestrator.go` | **Overflow architecture (§2/§7):** `RunDetailed` accepts `RunOption`s; `WithSessionContext` applies the per-chat policy to plan, gates and request; extended per-turn telemetry (requested/effective/modelMax/engineMax/session policy, per-section token split incl. system/tools/recall/attachments/history/current-task/output-reserve/safety-reserve, elided/compressed, `overflowPrevented`); the structured `context plan:` log block BEFORE the engine call; `compactToolResults` now also returns tokens freed; `ContextSafetyMargin` exported so the API layer reports the same figure |
| `internal/agent/preflight_test.go` | updated for `compactToolResults`' second return value (tokens freed) |
| `internal/ctxtelemetry/ctxtelemetry.go` | **Telemetry (§7):** `TurnRecord` gains `contextRequested/contextEffective/contextModelMax/contextEngineMax/sessionContext`, the per-section split (`tokensSystem…tokensSafetyReserve`, `tokensCurrentTask`), `tokensElided/tokensCompressed`, `rolloverTriggered`, `overflowPrevented` |
| `internal/multiagent/context.go` | **NEW.** Per-agent context policies (§5): `ContextPolicy{requestedContext, minimumContext, maximumContext, outputReserve}` per role (planner 16K, coder 32K, researcher 8K, critic 12K, summarizer 8K, …), `ResolveAgentContext` (agent override → session/global → model max → engine window, role minimum honored only within hard limits), logged one line per consult |
| `internal/multiagent/context_test.go` | **NEW.** Policy table + resolution tests (base clamp, model-max wins, inherit, floor-vs-hard-limit) |
| `internal/multiagent/multiagent.go` | planner/critic/summarizer requests carry their resolved context (`NumCtx`); `ModelLimitsFn` seam for the runtime wiring |
| `internal/multiagent/specialists.go` | `consultOne` resolves + logs the specialist's context policy, bounds output by `policy.OutputReserve`, sends the resolved `NumCtx`; specialist activity captions now show the resolved context (§17) |
| `internal/runtime/runtime.go` | context-limit provider now reports BOTH engines (llama.cpp verified window while alive + native loaded-model limit, min); `ModelLimitsFn` wiring for per-agent resolution |
| `internal/api/sessioncontext.go` | **NEW.** `GET/PUT /api/sessions/{id}/context` — the authoritative context decision for one chat (requested/configured/sessionPolicy/effective/modelMax/engineMax/usableInput/outputReserve/safetyReserve/used/remaining/pressure + resource classification + selector options); PUT persists the per-session policy and REJECTS (409) values classified `unsupported` with the reason; `POST /api/models/open-folder` for the first-use path (§10) |
| `internal/api/server.go` | run path passes `agent.WithSessionContext(sess.Context.ContextTokens)`; `modelInfo` gains `estimatedMemoryBytes`/`recommendedContext` (§10 model card); routes registered |
| `internal/api/engine.go` | **Startup (§9/§11):** snapshot gains `phase` (waiting → downloading-engine → loading-model → checking-capabilities → ready…), `verified`/`verifiedModel`/`verifiedContext`, `degraded` (state "ready" without verified serving) — degraded startup is visible, never hidden behind a green badge |
| `scripts/gen-syso/main.go` | **Icon (§12):** full 16/24/32/48/64/128/256 ladder (24px added); also emits `build/sheytan.ico` (BMP entries ≤128 + PNG 256) for packaged shortcut/application metadata |
| `.gitignore` | re-includes `/build/sheytan.ico` (committed packaging metadata; the `.syso` stays build-time-generated and CI already runs `go run ./scripts/gen-syso`) |
| `src/AgentHeader.tsx` | **Chat UX (§16):** CONTEXT selector (4K…128K, backend-filtered, per-chat), USED `14.6K / 16K · 91%` meter with critical highlight, status pill reads the backend phase + verified proof |
| `src/AgentBody.tsx` | **First use (§10) + startup (§9):** "No model selected / Choose model / Open models folder" gate; model card facts (architecture, quantization, context maximum, estimated memory, backend, status); startup phase labels + honest degraded-startup warning |
| `src/App.tsx` | **Branding (§13):** `document.title` per layer (SHEYTAN, SHEYTAN — Coding Lab, SHEYTAN — Research, SHEYTAN — Settings); sidebar footer says `SHEYTAN™ Local-Agent · Native runtime · offline` (generic "Go + Wails" removed); version fallback refreshed |
| `src/styles.css` | **Theme (§14):** canonical token block (`--surface`, `--surface-elevated`, `--border`, `--border-strong`, `--text-primary`, `--text-secondary`, `--accent`, `--accent-strong`, `--success`, `--warning`, `--error`, `--focus`) mapped onto the brand palette — dark-first, light-mode foundation; header context pills; phase/first-use/model-card styles |
| `src/settings.css` | **Scrolling (§15):** `.settings-page` is the app-level scroll container (wheel/trackpad/keyboard/scrollbar, `overscroll-behavior: contain`, no horizontal overflow, cards inside viewport at 1280×720 → 2880×1800 incl. 125/150 % scaling); sticky toolbar with solid backdrop; theme tokens replace the previously-undefined `--border/--accent/--text-primary/--text-secondary` variables |
| `src/SettingsPanel.tsx` | scroll container is focusable (`tabIndex={-1}`) so keyboard scrolling works |
| `src/store.ts` | per-session context status state (fetched on session select/switch, refreshed after every turn), `setSessionContext` action — per-chat policy with zero global mutation |
| `src/api.ts` | typed client for the context endpoints, extended engine/model/session types |
| `web/static/**` | regenerated embedded frontend bundle (hashes replaced) |

## 4. ADD

| Path | Why |
|---|---|
| `build/sheytan.ico` | multi-resolution brand icon (16–256) for installer/shortcut metadata (§12) |
| `internal/llm/resources.go`, `internal/llm/resources_test.go` | resource-aware context classification (§8) |
| `internal/multiagent/context.go`, `internal/multiagent/context_test.go` | per-agent context policies (§5) |
| `internal/api/sessioncontext.go` | per-session context API (§4/§16) |

## 5. DO NOT TOUCH

Everything else — in particular the managed llama.cpp lifecycle, the native
C++ engine (`native/engine/**`), the backend router, Coding Lab, project
intelligence, research providers, memory/recall, context cache, Continuum,
skills, toolsets, MCP bridge, browser/computer tooling, verification and
reliability cores, sandbox, security controls, streaming, Wails embedding,
and the WebSocket contracts. `internal/aicontext/AI-CONTEXT.md` was
inspected and intentionally unchanged (version marker stays 11; the
briefing's contract is not altered by this phase).

## 6. TESTS

- `internal/improve`: deadlock watchdogs, concurrency hammer, persistence,
  bounded evidence, bounded store (race-clean).
- `internal/llm`: session-context resolution chain, resource assessment,
  selector ladder.
- `internal/multiagent`: policy table + resolution ordering.
- `internal/sessions`: per-chat policy persistence round trip.
- Pre-existing suites: context plan fit gates, orchestrator preflight,
  sessions, api, native engine, releasegate — all passing.

## 7. DOCUMENTATION

Updated for truthfulness: `README.md` (1.1.6 section), `ARCHITECTURE.md`
(context policies), `agent.md`, `worklog.md` (appended phase record),
this file. Feature states are marked IMPLEMENTED / PARTIAL / FOUNDATION /
PLANNED — planned behaviour is never described as implemented.

## 8. RUNTIME VALIDATION

Performed on Linux (CI-equivalent):

- `go test -tags headless ./internal/... -count=1` — PASS (incl. race build
  for `internal/improve`)
- `go vet -tags headless ./internal/...` — PASS
- `npm run typecheck` / `npm run lint` / `npm run build` — PASS (embedded
  frontend regenerated)
- `node scripts/release-version.mjs --check` — PASS
- native engine C++ build + ctest — PASS
- Windows exe/resource layer validated via the gen-syso pipeline (deterministic
  resource object + .ico); a real Windows GUI run was NOT available in this
  environment and remains the maintainer's smoke step.

## 9. KNOWN LIMITATIONS

1. **Session policy cannot exceed the resolved limits.** Per §3 the
   effective value is the minimum of the session policy, the global
   configured context, the model GGUF maximum and the engine-verified
   window. Selecting 32K in a chat while the engine runs a 16K window
   serves 16K (with an explanatory trail). Raise the global context in
   Settings and restart the engine to go higher — never by blindly
   inflating `numCtx` at request time.
2. **Resource estimates are conservative pre-flight math** (weights file
   size + KV-cache from GGUF facts + fixed overhead vs RAM/VRAM), not a
   runtime allocator. They classify, they do not guarantee.
3. **llama.cpp `n_ctx` is fixed at launch.** The wire request carries the
   validated effective context for honesty and remote-mode clarity; the
   llama.cpp server itself only resizes its window on restart.
4. **Real-model acceptance on Windows** (gemma-4-E2B-it-Q4_K_M smoke with
   the desktop UI) requires a Windows machine with the model present; this
   environment validated everything reproducible headlessly, including the
   full context pipeline and engine state machine.
