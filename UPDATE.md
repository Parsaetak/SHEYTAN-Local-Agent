# UPDATE.md — v1.2.5 SHEYTAN-LA Adaptive Context Tiers, Global Tool Control, Thinking Mode & Per-Request Telemetry

**Release:** `v1.2.5` (codename Zeta) · **Base:** `main @ dacae73` (`v1.2.4`)
**Date:** 2026-09-17
**Package:** `SHEYTAN-Local-Agent-v1.2.5-UPDATE.zip`

This package replaces the v1.2.4 "build the full context first" behaviour
with an adaptive, evidence-driven turn pipeline: classify the task, choose
a context tier (FAST / STANDARD / DEEP / MAX) from the machine's measured
resources, compose ONLY what that tier allows, send the request, and
escalate the tier mid-run when — and only when — real run evidence
demands it. It adds per-request controls that change the actual backend
request (a ChatGPT-style thinking control and a manual tool policy), an
expandable reasoning surface driven by the backend's own events, a live
status chip with measured tier/token telemetry, per-request stage timing,
and an explicit data-ownership ladder. No functionality was disabled to
improve numbers: streaming, model lifecycle, tool execution, workspace,
memory and CI systems are preserved, and the Phase 7 fit gates remain the
safety net under every request.

The version moves `1.2.4` → `1.2.5` through the established identity
chain (package.json → release-version.mjs → internal/config/config.go,
build/config.yml, SIGNATURE — `--check` is green).

## 0. The measured baseline (what this package fixes first)

Everything below was measured on the v1.2.4 tree BEFORE any edit (same
fake-engine harness that pins the new behaviour; see
`internal/agent/v125_measurement_test.go`):

| Hot path | v1.2.4 baseline | Root cause |
|---|---|---|
| First-request prompt, trivial chat ("hi") | **≈ 10K tokens** prefill before TTFT | full 29,691-char briefing (~7,368 tok) + ALL tool schemas + recall + project card + skills composed UNCONDITIONALLY every turn |
| AI-context briefing | 7,368 tokens, full form always | no tier system — one context shape for every machine and task |
| Recall retrieval | I/O + scoring on EVERY turn | composed before the plan regardless of task shape |
| Tool schemas | JSON-marshaled 2× per turn (measure + request) | no memoization of stable serialized specs |
| Optional composition | recall/card/skills composed even when dropped later | compose-first-then-degrade ladder only ever shrank under overflow |
| Per-request visibility | only engine TTFT + token accounting | no classify/context/prompt/serialization/tool/verification timeline |
| Thinking control | global settings toggle | not a per-request control; did not shape the tier or the nudge |
| Tool restriction | global EnabledTools config only | no per-request manual policy, no enforcement distinction |
| Startup | project-intel observe walked the workspace synchronously in NewStack | first chat waited on indexing |

## 1. Adaptive context tiers (FAST / STANDARD / DEEP / MAX)

New package `internal/taskclassify` (pure, deterministic, no I/O):

- **Classification**: 8 task kinds (chat, question, coding, architecture,
  research, vision, system, data) via additive keyword scoring; complexity
  0-100; explicit signals for files/terminal/tests/research/vision/
  memory/depth and conversation-past references; the composer's thinking
  control shifts complexity (fast −25, thinking +20).
- **Tier ladder with policies, not sizes**: FAST uses the compact
  ~353-token briefing, a 30% history share, a 700-token tool-schema
  budget, ≤6 offered tools, and does NOT compose recall, project card or
  skills at all; STANDARD/DEEP/MAX progressively restore the full
  briefing (50/70/85% shares, 1400/2200/0 tool budgets, 12/16/0 tools)
  and the optional blocks.
- **Resource-aware selection (never one hard-coded size)**: the tier is
  chosen from the measured effective context (min of configured, GGUF
  model limit, engine-verified limit, session policy), TTL-cached
  measured system RAM (low-RAM machines cap below DEEP), the vision
  payload estimate (vision never runs FAST), staged-attachment tokens,
  the task-relevant tool cost, the full-history size (long histories that
  cannot fit the tier's share escalate one step), and the user's
  explicit depth control (fast caps at FAST unless the payload needs
  more; thinking floors at STANDARD).

**Measured result** (the acceptance test pins it): the first engine
request for "hi" with 17 registered tools drops from ≈10K tokens
(v1.2.4) to **≈416 tokens engine-received (557 plan-accounted)** — about
**24× smaller**, with 3 offered tool schemas instead of all of them.
Recall retrieval I/O on trivial chat: **every turn → zero**.

## 2. Smart context building (P0-P3) and evidence-driven escalation

- `contextplan` now speaks the four priority classes — **P0 mandatory**
  (output reserve, system briefing), **P1 highly relevant** (tool
  schemas, history/current turn), **P2 useful** (recall digests), **P3
  optional** (attachment chunks) — as derived labels over the existing
  numeric ladder, plus the tier's history-share cap in `Assemble`.
  The initial request carries P0 + only the P1 the task signals need.
- Token budgets everywhere (never character counts), via the shared
  estimator; `cache_prompt=true` is preserved end-to-end.
- **Escalation**: eight named evidence reasons — `MissingFileContext`,
  `MissingHistory`, `MissingToolContext`, `RepositoryDependency`,
  `VerificationFailure`, `LargeAttachment`, `VisionRequirement`,
  `UserRequestedDepth` — derived only from real observations (tool
  results reporting missing files, the orchestrator's own tool-surface
  refusals, attachment-block drops, failed objective verification,
  mid-run vision payloads). One upgrade applies per evidence round,
  bounded at **2 per run** (FAST→STANDARD→DEEP; MAX only from DEEP).
- An upgrade enriches the LIVE conversation — never a from-scratch
  rebuild: the compact briefing is swapped for the full form, the newly
  allowed card/skills/recall blocks are injected before the fresh turn,
  the history window re-opens from the RETAINED pre-window transcript at
  the new share, and the offered tool surface widens (AUTO policy only —
  manual restrictions are never widened). Every move logs the honest
  line, e.g. `FAST 3.8K → STANDARD 7.2K reason=MissingFileContext`, and
  reaches the UI as an `escalation` event.

## 3. One global tool system + smart selection + manual mode

All legitimate tools already live in ONE orchestrator registry (files,
search, edit/write, terminal, build/tests via lab, diff, verification,
research [GitHub/Reddit/DuckDuckGo/SearXNG], browser, workspace, system,
memory, vision, attachments, model management, diagnostics) — the
workspace modes (Agent/Lab/Research/System) remain views over that same
system. v1.2.5 completes the picture:

- **Selection from the START**: the task-relevant toolset
  (`toolsets.SelectForTask`) is chosen BEFORE the first request under the
  tier's token/count budget — available globally no longer means every
  schema in every prompt (3 offered tools for trivial chat, measured).
- **Manual tool mode** (`toolMode: "manual"` + `toolAllow: [...]` per
  request, from the composer's Tools ▾ control): only the selected tools
  are offered AND executable; out-of-policy calls are refused with a
  clear instruction to re-plan; escalation never silently re-enables a
  disabled tool.
- **Repeat prevention & reuse**: identical successful calls to
  deterministic tools (files read-shape, diff, json) are served from a
  bounded LRU result cache (1 MiB / 64 entries) with a visible
  "served from cache" event; mutating file actions are never cached;
  genuinely retryable failures (transient network/process on idempotent
  network tools) retry exactly once — never side-effecting tools.
- **Tool-spec memoization**: serialized schemas are marshaled once per
  registry generation (`specCache`, invalidated on Register) — the
  per-turn double marshal of ~17-20 schemas is gone.

## 4. Thinking control + thinking UI + live status

- The composer gains a real **Thinking ▾** control (Auto / Fast /
  Thinking). It is persisted (localStorage), sent with EVERY run request
  (`thinking` field), and changes the actual backend behaviour:
  **Fast** forces the FAST tier posture (unless the payload needs more)
  and disables the thinking nudge; **Thinking** floors at STANDARD,
  enables the reasoning nudge even when the global toggle is off, and
  permits the deeper escalation/verification posture; **Auto** is the
  adaptive decision. It is NOT a visual-only control — the acceptance
  tests assert both the tier and the nudge change.
- **Thinking UI**: an expandable reasoning panel driven only by actual
  backend events — `thinking_start` opens it, `reasoning`/`thinking_delta`
  fill it (cumulative snapshots, no duplication), `thinking_end` folds
  it; the run timeline (run-phase machine) is unchanged and still driven
  only by real events. The backend emits the canonical vocabulary
  (`thinking_start/delta/end`, `assistant_delta`, `complete`, `status`,
  `escalation`) while the legacy names keep flowing for compatibility —
  one canonical mapping in `src/run-events.ts` (unit-tested) guarantees
  no streamed output is processed twice.
- **Clear live status**: the backend's own `status` events render in a
  compact chip — "Classifying request…", "Preparing context · FAST",
  "Thinking… · STANDARD · 7.2k tok", "First response…", "Generating…",
  "Using more context · …" — measured tier/token values only, plus
  "Running tool…" via the existing tool events and "Verifying…" via the
  verification report.

## 5. Memory ownership + background work

- New `internal/memmanager/ownership.go`: the explicit ladder
  **ACTIVE → SESSION → HOT CACHE → COLD CACHE → EXPIRED → RELEASE** with
  named ACTIVE holders (`HoldActive`/`ReleaseActive` wired into
  TrackRunStart/End), `OwnershipSnapshot()` on `/api/perf`, and release
  accounting in `Cleanup`. Active request data is never evicted — by
  name and by counter (the pressure/idle cleanup still refuses to run
  while any run is live). Duplicate-suppression, bounded caches and
  coordinated trims from v1.2.4 are unchanged; forced GC remains a
  non-optimization.
- Startup critical path: the initial project-intelligence observation
  (bounded workspace walk) moved to a background goroutine — a valid
  installed local engine is usable immediately and the first chat never
  waits for indexing. Updater/network failures remain off the local-chat
  path (scheduled updates and engine prewarm were already async; the
  offline briefing note still informs the model).

## 6. Per-request performance telemetry

`internal/agent/pertiming.go` — one `RunClock` per turn with 16 stage
marks (`received`, `classified`, `context_start/end`, `prompt_start/end`,
`serialized`, `request_sent`, `first_byte`, `first_token`,
`generation_end`, `tool_start/end`, `verification_start/end`, `done`)
deriving `classify_ms`, `context_ms`, `prompt_ms`, `serialization_ms`,
`TTFT`, `generation_ms`, `tool_ms`, `verification_ms`, `total_ms` and
`firstPromptTokens`. Unmarked stages report 0 — never invented. The
values travel on `RunResult.Timing`, on the ctxtelemetry `TurnRecord`
(with `Tier`, `FinalTier`, `Escalations`, `ThinkingControl`,
`ToolPolicyMode`), and surface in Advanced/System diagnostics via
`/api/perf` (`requests[]` — the 10 most recent measured timelines — plus
`ownership[]` and `toolCache`).

## 7. Change map (authoritative)

```text
package.json                                         MODIFY  version 1.2.5 (identity chain source of truth)
SIGNATURE                                            MODIFY  version sync via release-version.mjs
build/config.yml                                     MODIFY  version sync via release-version.mjs
internal/config/config.go                            MODIFY  AppVersion 1.2.5 (sync)
internal/taskclassify/taskclassify.go                ADD     task classification (kinds, signals, complexity, depth control)
internal/taskclassify/taskclassify_test.go           ADD     classifier + tier + escalation-ladder tests
internal/taskclassify/tiers.go                       ADD     FAST/STANDARD/DEEP/MAX policies, resource-aware SelectTier, escalation reasons
internal/contextplan/contextplan.go                  MODIFY  Tier metadata, P0-P3 priority classes, MaxHistoryShare cap
internal/agent/orchestrator.go                       MODIFY  RunDetailed adaptive pipeline (classify→tier→compose→escalate), per-request options, events, tool cache, clock
internal/agent/orchestrator_tiers.go                 ADD     turnComposer (tier-scoped composition + upgrades), escalationWatch, hardware TTL snapshot, policy types
internal/agent/orchestrator_tiers_test.go            ADD     acceptance: FAST first prompt, card tiers, thinking control, manual policy, cache, escalation, clock, spec cache, ladder bound
internal/agent/pertiming.go                          ADD     RunClock + Timing (16 measured stages)
internal/agent/toolcache.go                          ADD     specCache memoization + bounded deterministic result cache
internal/agent/userflows_test.go                     ADD     Phase 15 user-flow tests (old-conversation, research, vision, simple-chat recall skip)
internal/agent/v125_measurement_test.go               ADD     the before/after first-prompt measurement (v1.2.4 ≈10K → v1.2.5 ≈416 tok)
internal/agent/reliability_test.go                   MODIFY  card-position fixture updated to a card-carrying (coding) task for the tier era
internal/ctxtelemetry/ctxtelemetry.go                MODIFY  Tier/FinalTier/Escalations/ThinkingControl/ToolPolicyMode/RequestTiming
internal/memmanager/memmanager.go                    MODIFY  ownership wiring (named ACTIVE holders, release accounting)
internal/memmanager/ownership.go                     ADD     ACTIVE→SESSION→HOT→COLD→EXPIRED→RELEASE ladder + snapshot
internal/runtime/runtime.go                          MODIFY  project-intel observe moved off the startup critical path
internal/api/server.go                               MODIFY  handleRun parses thinking/toolMode/toolAllow + receivedAt; persistence skip list extended
internal/api/perf.go                                 MODIFY  requests[] timeline table, ownership ladder, toolCache counters
src/run-events.ts                                    ADD     canonical wire-event mapping + control normalizers (pure)
src/run-events.test.ts                               ADD     vocabulary/normalization unit tests
src/ComposerControls.tsx                             ADD     Thinking ▾ + Tools ▾ composer controls + live status chip
src/api.ts                                           MODIFY  RunRequest controls + RequestTiming/OwnershipLevel types
src/store.ts                                         MODIFY  controls state (persisted) + canonical event handling + liveStatus/tierEscalations/thinkingPanelOpen
src/AgentBody.tsx                                    MODIFY  composer mounts ComposerControls
src/MessageStream.tsx                                MODIFY  thinking panel follows backend thinking markers; escalation notices in the run activity
src/ActivityStream.tsx                               MODIFY  escalation/status group under Plan
src/styles.css                                       MODIFY  additive v1.2.5 section (control menus, status chip, escalation styling)
web/static/index.html                                MODIFY  rebuilt frontend bundle references
web/static/.vite/manifest.json                       MODIFY  rebuilt frontend manifest
web/static/assets/AgentBody-E9JJ_R6r.js              NEW     rebuilt bundle (hashed)
web/static/assets/AgentHeader-BpWR6c2d.js            NEW     rebuilt bundle (hashed)
web/static/assets/AgentSidebar-BJUTZobI.js           NEW     rebuilt bundle (hashed)
web/static/assets/DownloadProgress-1en0mJCS.js       NEW     rebuilt bundle (hashed)
web/static/assets/LabPanel-DG1YEr1n.js               NEW     rebuilt bundle (hashed)
web/static/assets/ResearchPanel-ByYo7Bbc.js          NEW     rebuilt bundle (hashed)
web/static/assets/SettingsPanel-DYKVli5M.js          NEW     rebuilt bundle (hashed)
web/static/assets/SystemPanel-DkdYORg1.js            NEW     rebuilt bundle (hashed)
web/static/assets/WorkspacePanel-aELe49Qv.js         NEW     rebuilt bundle (hashed)
web/static/assets/index-B77WlYpt.js                  NEW     rebuilt entry bundle
web/static/assets/index-5oqPjJgJ.css                 NEW     rebuilt stylesheet
web/static/assets/vision-Dw0T2tjy.js                 UNCHANGED (same content hash as v1.2.4 — included for a complete, self-consistent web/static tree)
worklog.md                                           MODIFY  this release's work-log entry
UPDATE.md                                            REPLACE this file
REPLACEMENT-MANIFEST.txt                             REPLACE this package's manifest
REPLACEMENT-SHA256.txt                               REPLACE this package's hashes
```

## 8. DELETE list (apply after copying)

Stale v1.2.4 hashed assets, replaced by the rebuilt bundles above:

```text
web/static/assets/AgentBody-cMt13HM9.js
web/static/assets/AgentHeader-dCYmm522.js
web/static/assets/AgentSidebar-CWp3SdAx.js
web/static/assets/DownloadProgress-28j0D3i-.js
web/static/assets/LabPanel-DFJX1ZVA.js
web/static/assets/ResearchPanel-BXiEx0Sd.js
web/static/assets/SettingsPanel-BdbLpW4s.js
web/static/assets/SystemPanel-BEI2b4hF.js
web/static/assets/WorkspacePanel-BV8_VjyW.js
web/static/assets/index-B_uB5cBU.js
web/static/assets/index-gFgARQCb.css
```

## 9. DO NOT TOUCH list

User data and runtime state are never touched by this update: `models/`,
`workspace/`, `sessions/`, `attachments/`, `memory.jsonl`, `config.json`,
`ctxtelemetry.jsonl`, `scheduler/`, `lab/`. The ownership ladder's trims
deliberately exclude all of these; telemetry JSONL is only flushed, never
truncated by policy.

## 10. Safe-apply procedure

1. Apply over a clean checkout of the base commit (`dacae73`, v1.2.4).
2. Copy every file from this package over the tree (REPLACE/MODIFY/ADD),
   then delete the v1.2.4 hashed assets listed in §8 if present.
3. Verify identity: `node scripts/release-version.mjs --check` must
   report all surfaces at 1.2.5.
4. Rebuild and validate: `go vet -tags headless ./...`, `go test
   -tags headless ./internal/... -count=1`, `npm ci && npm run typecheck
   && npm run lint && npm run test:units && npm run build`, and for the
   native engine `cmake -S native/engine -B native/engine/build &&
   cmake --build native/engine/build && ctest --test-dir
   native/engine/build` (no C++ changes; the rebuild simply re-confirms).
5. Launch: the composer shows **Thinking ▾** and **Tools ▾**; a trivial
   chat answers from the FAST tier (status chip shows the tier), coding
   work carries the project card, the reasoning panel opens only when
   the model actually reasons, and Settings → Performance → Advanced
   shows the per-request timeline table and the ownership ladder.

## 11. Executed automated checks (this package, before packaging)

Correctness suites (all green after the changes):

- `go vet -tags headless ./internal/...` clean; `go test -tags headless
  ./internal/... -count=1` fully green (41 packages, including the 20
  new v1.2.5 tests); the full suite was run twice back-to-back for
  stability.
- Race detector clean on the touched concurrency packages (agent,
  memmanager, taskclassify, ctxtelemetry, contextcache, sessions).
- Frontend: `npm run typecheck`, `npm run lint` (0/0), `npm run
  test:units` (28/28 — 8 new), `npm run build` + `sync:web` green.
- Native engine: NO C++ changes; rebuilt and 12/12 CTest suites pass.

Measured before/after (identical fake-engine harness, before vs after):

| Metric | v1.2.4 | v1.2.5 | Gain |
|---|---|---|---|
| First-request prompt, trivial chat (17 tools) | ≈10K tok | ≈416 tok engine-received / 557 plan-accounted | **~24× smaller** |
| Tool schemas offered, trivial chat | all (~17) | 3 (tier-bounded ≤6) | ~6× fewer |
| Recall retrieval I/O, trivial chat | every turn | 0 (FAST never composes it) | eliminated |
| Tool-schema JSON marshals per turn | 2 × N tools | 0 after first composition per registry generation | memoized |
| Optional composition work before TTFT | recall + card + skills always | tier-gated (zero on FAST) | eliminated on small tasks |

User-flow verification (Phase 15, as acceptance tests): simple chat,
normal question, coding bug (targeted card at STANDARD+), long/old
conversation retrieval (recall at STANDARD+ with digest injection),
research task, tool-heavy task (cache reuse + repeat prevention), vision
task (image_url wire part + tier floor), Thinking=Fast / Auto /
Thinking (tier + nudge verified on the engine side), manual tool
selection + restriction (offer AND execution refusal), context
escalation (FAST→STANDARD on MissingFileContext; enrichment verified in
the next engine request; ladder bound ≤2), engine paths unchanged
(PrewarmLLM async, offline note preserved).

## 12. Known platform limitations

- The GUI (Wails/GTK) shell cannot be exercised on a headless runner
  without GTK4/WebKitGTK system libraries; all logic is covered through
  the `headless` tag suite, which is what CI runs.
- The per-image vision token figure used for tier selection is a
  documented planning estimate (mmproj tile cost); the plan's reported
  tokens remain measured text tokens.
- The escalation ladder is bounded at two upgrades per run by design —
  evidence that would demand a third step is logged ("ladder exhausted")
  rather than silently ignored.
