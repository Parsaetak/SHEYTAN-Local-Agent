# SHEYTAN-Local-Agent — Engineering Worklog

Current release:  v1.9.1

This worklog is a session log, not a second architecture document. The
architecture truth lives in `ARCHITECTURE.md`, the release evidence in
`UPDATE.md`, the future in `ROADMAP.md`.

---
Task ID: 1 (v1.8.3 session)
Agent: v1.8.3 engineering session
Task: v1.8.3 — P0 root-cause and repair of the Linux CI session-delete
failure (Actions run 36713108772 / job 109880449134,
e2e/sessions.spec.ts:88 "Expected: < 4, Received: 4"), P0 deterministic
session-delete coverage, P1 roadmap evidence-ranking of the supplied
bottleneck reports

Work Log:
- Baseline: HEAD `c8867886` (v1.8.2). CI run 36713108772: Browser E2E
  30/31, only e2e/sessions.spec.ts:88 failing; stress/package skipped
  after the E2E failure. All other gates (source/frontend audit,
  Windows x64, Linux build deps, release metadata, native C++, Go
  verification) PASS.
- v1.8.2 diff audit: no session create/delete/list path changed (streaming,
  memory-evidence, self-model only) — the failure is a latent timing race,
  not a v1.8.2 regression.
- Reproduced with an instrumented diagnostic (request/response journal +
  server-side authoritative list snapshots + DOM identity capture; the
  browser's create-response delivery delayed to model CI latency): the
  test sampled its count baseline BEFORE the asynchronous "New session"
  create landed (its visibility gate was satisfied by pre-existing
  items); the delete then removed the correct session (verified absent
  server-side); the late create kept the count flat; final DOM == final
  server state. A pass of the same test = the poll catching the transient
  count window. Product behavior correct; measurement raced the app.
- Second, real defect found by the investigation: initializeAgentOnce
  applied its startup GET /api/sessions response WITHOUT a sessionListGuard
  ticket (the v1.7.5 repair covered refreshSessions only). The sidebar's
  New-session button is actionable during init, so a create landing in
  that window was clobbered by the stale startup list — the created
  session vanished from the sidebar. Also: AgentSidebar's
  `void deleteSession(id)` turned server DELETE failures into invisible
  unhandled rejections; and createSession's unconditional prepend could
  duplicate a row when a refresh landed between POST dispatch and
  response.
- P0 repairs: (1) agent-init routes the startup list write through the
  ONE sessionListGuard (ticket before the GET; mode-change and
  user-action detection around the eager first-install create; superseded
  responses apply only app/loading and delegate list+selection
  re-resolution to refreshSessions); (2) deleteSession surfaces failures
  through the store error state (state untouched on failure, never a fake
  success); (3) createSession prepend is idempotent by id; (4) sidebar
  rows carry data-session-id (the rendered 8-char slice collides within a
  bucket).
- Test repair (never weakened): the browser delete test now waits
  STATE-BASED for the created session (count grows by one), then asserts
  the SPECIFIC deleted id, survivor identity-set equality, the count,
  replacement activation, composer usability, and post-reload absence
  from the sidebar AND a fresh authoritative fetch. New browser E2E for
  the init race: the startup GET response is held until the create POST's
  response has REACHED the page (causality, no timers), then delivered;
  asserts the created session survives, appears exactly once, and is
  active.
- Deterministic coverage: src/session-delete-regression.test.ts (11
  scenarios over the REAL store via a scripted HTTP transport where
  response ordering is forced by causality — pending/persisted delete,
  replacement selection, delete-vs-held-GET, stale-GET non-resurrection,
  mode separation, honest duplicate-delete, post-delete fresh fetch,
  init-race survival, clean-init apply, eager first-install session) +
  src/extensionless-ts-resolver.mjs (Node resolve hook making the store
  testable outside the bundler); internal/sessions/delete_pending_test.go
  (pending delete removes every authority; persisted delete cleans
  index/file/sidecars with fresh-store no-resurrection; unknown-id honest
  failure). Registered in package.json test:units.
- Mutation verification: with the init guard neutralized (v1.8.2
  semantics), BOTH the store-level test and the browser E2E fail; with
  the repair, both pass. The coverage detects the defect class it was
  written for.
- ROADMAP.md repaired: artifact truth (changelog = sole release history;
  README current-only; UPDATE.md current release/maintenance evidence);
  Performance, Reliability & Scale Backlog with D/I/H evidence labels;
  v1.9/v1.10/v1.11/v1.12/v2.0 expanded per the mission; external report
  figures (percentages, TPS/TTFT/cache/VRAM, 3x/5x/80%) explicitly
  treated as hypotheses; incompatible prescriptions (Tokio/asyncio
  rewrites, mandatory FAISS/USearch/SQLite/bbolt/tree-sitter, assumed
  PagedAttention, NPU embedding service, dual-model VRAM swapping)
  rejected; performance measurement framework documented with no invented
  targets.
- Version truth 1.8.3 via the canonical gate (release-version.mjs
  --check green: package.json, package-lock.json, config.go,
  build/config.yml, SIGNATURE); codename gate green; README/ARCHITECTURE/
  agent.md/UPDATE.md/changelog.md synchronized to v1.8.3.

Verification (evidence classes; Linux x86-64, Go 1.26, Node 24):
- Frontend: typecheck green; oxlint 0 warnings; npm run test:units 191/191
  (incl. the 11 new store-level scenarios); production build + embedded
  sync green.
- Go: go test ./internal/... -tags headless -count=1 green (58 packages);
  go vet -tags headless ./internal/... green; race gate green
  (api/agent/sessions/contextplan/histref/runtime).
- Native: cmake configure/build + ctest 12/12 green.
- Browser E2E: FULL suite 32/32 green (real stack, real native engine,
  no skipped failures) — the repaired delete test and the new init-race
  test included; every v1.8.2 streaming/memory/self-model test unchanged
  and passing.
- Stress suite: 47 pass / 0 fail, hangs=0, crashes=0.
- NOT executed here (stated honestly): physical-Windows runtime
  acceptance. The repairs are pinned by deterministic tests and
  mutation-verified coverage, not by a physical-host observation.

Stage Summary:
- v1.8.3 complete on this revision: the run-107 session-delete failure is
  root-caused with instrumented evidence and repaired without weakening
  any test; the startup list write goes through the one generation
  authority; DELETE failures are honest; the prepend is idempotent;
  session-delete behavior is pinned deterministically at store, Go and
  browser layers with mutation-verified coverage; the roadmap carries
  the evidence-ranked backlog; version identity is exactly 1.8.3.
Task ID: 1 (v1.8.1 session)
Agent: v1.8.1 engineering session
Task: v1.8.1 — P0 real-Windows local-generation crash repair (small Gemma
model + `hi`), P0 single-frame streamed-answer visibility, P1 engine
rollback identity repair, audits, version truth

Work Log:
- Baseline: HEAD `e5a12c0` (`v1.8.0`), CI green (`36570586149`). The
  primary evidence is the user's Windows runtime log: local CPU backend,
  `gemma-4-E2B-it-Q4_K_M.gguf`, llama.cpp b11261, preflight
  `compatible=true`, then `recovered panic: runtime error: invalid memory
  address or nil pointer dereference` after `task classified: kind=chat
  complexity=8`, run settled `error` in ~57 ms, no request-sent /
  response-header / first-byte evidence.
- Reproduced the panic deterministically BEFORE any fix (synthetic
  Gemma-class GGUF: 262,144-entry tokenizer block > the parser's 8 MiB
  read bound): `ReadModelCard` fails at the LimitReader →
  `ResolveModelCapabilities` returns nil → the orchestrator's
  `resolveEffectiveContext` dereferences `caps.TokenizerFamily` between
  `task classified` and `tier selected` — the exact window and signature.
- P0 crash repairs: (1) nil-card fallback in `resolveEffectiveContext`
  (documented caps contract: configured context + conservative heuristic
  estimator); (2) GGUF metadata read bound 8 → 32 MiB
  (`ggufMetadataReadLimit`) so real Gemma-class cards parse again
  (model-aware context clamp + family estimator restored for that model
  class); (3) `ResolveModelCapabilities` made uniformly nil-config-safe
  (the vision block was guarded, the recommendation block was not — same
  deref class, found during verification); (4) gemma3n/gemma4 added to
  the tokenizer-family and chat-template lineage lists.
- Regressions (each verified to FAIL on the pre-fix code):
  `TestResolveEffectiveContextSurvivesUnreadableCard` (unit, panics
  pre-fix), `TestGemmaClassCardParsesUnderRaisedBound`,
  `TestResolveEffectiveContextGemmaClassEndToEnd`,
  `TestLocalGemmaHiChatCompletes` and
  `TestLocalChatSurvivesUnreadableModelCard` (the api package's FIRST
  local-provider run-level E2E tests — the test binary re-executes as a
  fake llama-server subprocess serving the real engine contract; the
  unreadable-card E2E settles `error` pre-fix, `done` post-fix).
- P0 streaming: the store routed streamed text through TWO render frames
  (activity batch frame → streaming flush frame). New
  `src/stream-fast-path.ts`: stream-critical events
  (response/reasoning) fold into the streaming accumulator at
  socket-receive time — ONE frame boundary; the events still join the
  timeline batch, a self-draining ledger prevents double processing;
  cumulative-replace, replay idempotence, sequence/stale-run protection
  and the synchronous done/error/abort flush are unchanged. 12
  deterministic frame-controller tests (`stream-fast-path.test.ts`), no
  sleeps. Registered in `npm run test:units`.
- Backend `emitProgress` SplitThink O(n²) assessed and NOT optimized:
  bounded by the ~8 ms emit throttle; at realistic local-model token
  rates the scan volume is negligible (worst case ~6 MB/s on a 100 KB
  response). Correctness preserved (measure, don't guess).
- P1 engine identity: the user's b10642 transient is the documented
  last-resort stamp during the verification window; the commit is
  authoritative (existing tests pin two-boot no-redownload, manifest
  fallback, corrupt-state fallback, genuinely-newer-still-updates). ONE
  real defect found and fixed: after a FAILED verification + Rollback,
  the transient stamp survived in `installed.json` while the restored
  manifest described the serving engine (state-first
  `EffectiveInstalledEngineTag` would then read the wrong build).
  `Rollback()` now re-records from the restored manifest;
  `TestRollbackRestoresRecordedIdentityFromManifest` fails pre-fix at
  exactly `b10642`, plus `TestCommitRemainsTheAuthoritativeIdentity` and
  `TestRollbackLeavesStateUntouchedWhenManifestHasNoTag`.
- P1 runtime-root migration audit: the user's
  `detected=1 merged=1 recovered=0 collisions=4 removed=1
  reloadConfig=false` IS the intended contract (deterministic newer-wins
  collisions, one authoritative root, idempotent next boot) — pinned by
  the existing migration family; no change. GPU/AUTO CPU with `numGPU=0`
  remains the honest evidence-ladder outcome; Runtime Governor untouched.
- Version truth 1.8.1 through the canonical gate
  (`node scripts/release-version.mjs --check` green); README/UPDATE/
  ARCHITECTURE/agent.md updated to v1.8.1 current with v1.8.0 compressed
  to history.

Stage Summary:
- v1.8.1 repairs the real-Windows `hi` crash at the root (both layers:
  the deref guard and the card-read bound), makes streamed answers
  visible through ONE render frame, and restores engine rollback
  identity truth. All regressions were proven against the pre-fix code.

---
Task ID: 1 (v1.8.0 session)
Agent: v1.8.0 engineering session
Task: v1.8.0 — P0 pause/resume synchronization repair, honest abort
marker, Runtime Governor vertical slice (Adaptive Runtime Intelligence),
documentation consolidation, version truth

Work Log:
- Inspected the live repository at HEAD `eab6e1b` (`v1.7.6`) and the
  failing Actions run `36553559366` (Windows x64 Go verification:
  `TestPauseResumePauseAgainThenResumeCompletes` — `run settled "done"
  (error "") while waiting for phase "paused"`).
- Root-caused the CI failure: the test reused `waitForRunResponse` after
  a resume; its `LatestResponse != ""` condition is satisfied by the
  STALE pre-resume cumulative snapshot, so the second pause was issued
  without proof the resumed generation was live. Reproduced the defect
  class locally; on Linux the failure is scheduling-dependent (the same
  structural hazard), on the Windows runner it deterministically failed.
- P0 repair (no production weakening): added `resumedGenerationEvidence`
  (strictly newer run sequence AND changed cumulative response/reasoning
  snapshot) + `waitForResumedGeneration` (fail-fast on settlement);
  `TestPauseResumePauseAgainThenResumeCompletes` now captures the
  pre-resume snapshot and requires new authoritative activity before the
  second pause. Hazard note added to `waitForRunResponse` (initial
  generation only). Regression coverage:
  `TestResumedGenerationEvidenceContract` (deterministic unit — stale
  cumulative state, seq-only bumps, shorter continuation captions),
  `TestPauseResumeRequiresNewGenerationEvidence` (integration replay of
  the CI scenario).
- P0 (latent, found during the audit): abort-after-resume was flaky at
  BASELINE (~1-in-6 on Linux). Root cause: the orchestrator's four
  context-cancelation exits published `Activity{Type: "done"}`;
  `observe()` folds any `done` as the terminal outcome, so the live
  state flipped `done` while the caller settled `aborted` — a registry/
  state divergence the one-settlement rule can never repair. Repair:
  honest `aborted` activity type end-to-end (orchestrator exits,
  `observe` folding, `captureTerminal` + resumed-path caption capture
  replacing the hardcoded "Completed", frontend typed `aborted` kind +
  dedicated store case replacing caption-string sniffing). Evidence:
  15/15 stressed abort-after-resume runs; deterministic regressions
  `TestAbortedRunStateAgreesWithOutcome`, `TestRunLiveObserveAbortedMarker`.
- Feature: Runtime Governor vertical slice (`internal/governor`) —
  ONE runtime policy authority fed by the EXISTING live monitor (same
  sampler, same cadence, same protection path; the monitor's cooperative
  critical protection unchanged). `ResourceState` (measured facts +
  explicit unknowns), sustained-duration + bounded rolling signals over
  the shipped four-level pressure vocabulary, `Envelope` with adjustment
  classes (`live`/`next-run`/`reload`), `AdmitHeavyweight`/`AdmitMemory`
  resident-budget admission (mapped size ≠ resident; unknown →
  conservative refusal), `SelfModel` with provenance. Policy only — no
  second sampler/scheduler/authority; executes nothing.
- Wiring: `Stack.StartGovernor()` on the existing poll path
  (`observeGovernor`); engine facts through the existing `llm.Backend
  Metrics` (read-only); active runs through the existing counter; CPU
  through an injected platform seam (Linux measured load average;
  Windows honestly unknown with CPU policy inert).
- Surface: `GET /api/governor` composing governor + sysinfo hardware
  (GPU/NPU labeled detection-only) + engine metrics + capabilities;
  System Centre `GovernorCard` (real values, named unknowns). API
  contract tests + frontend typecheck/lint/units/build green.
- Docs: README rewritten current + compact history; ARCHITECTURE.md
  rewritten around the v1.8 truth + compressed historical layers;
  ROADMAP.md rewritten capability-driven (v1.9+ explicitly FUTURE —
  NOT IMPLEMENTED); UPDATE.md top section = v1.8.0 with compressed
  records; agent.md = current handoff; REPORT.md marked unmistakably
  historical; worklog updated (this entry). Version identity 1.8.0 via
  the canonical gate (`release-version.mjs --check` green: package.json,
  package-lock, config.go, build/config.yml, SIGNATURE).

Verification (evidence classes; Linux x86-64, Go 1.26.0, Node 24):
- Deterministic unit: `go test ./internal/governor/ -count=3` green;
  new api regression suites green (`-count=5` targeted, family `-count=3`).
- Race: `go test ./internal/governor/ -race -count=3` green; api+agent
  full suites `-tags headless -count=2` green (176s + 40s).
- Integration: `/api/governor` contract; pause/resume/abort/edit family.
- Frontend: `npm run typecheck` (CI script), `oxlint` 0 warnings,
  `npm run test:units` green, production build + embedded sync green.
- Release: `node scripts/release-version.mjs --check` consistent at
  1.8.0; codename gate green on the tracked tree (and re-run against the
  clean-room drop at packaging).
- NOT executed here (stated honestly): physical-PC Windows runtime
  verification; native C++ build/ctest on this host (GTK/WebKit-free
  environment builds headless targets only); Browser E2E (Playwright)
  in this session.

Stage Summary:
- v1.8.0 complete on this revision: the actual CI failure is repaired at
  the root with regression coverage that makes stale cumulative-state
  reuse impossible; the abort path is honest end-to-end (flake
  eliminated); the Runtime Governor vertical slice is real, wired and
  exercised; docs are consolidated (current + history, no duplicated
  essays); version identity is exactly 1.8.0. Pre-v1.8 session records
  are summarized once below.

---
Task ID: 1 (pre-v1.8.0 record — summarized once)
Agent: v1.3.0 → v1.7.6 engineering sessions
Task: everything shipped before v1.8.0

Summary (compressed; the authoritative per-release detail lives in the
release tags, their tests, and the historical sections of UPDATE.md):
- Foundations (≤v1.2): sessions, streaming with sequence-stamped replay,
  tool registry, downloader, installer, health checks, environment
  centre, bounded authoritative run state, settlement edge.
- v1.3–v1.4: durable completion ordering (persist → summary → handoff →
  recall → rollover → terminal), idle-sentinel barrier, task state,
  cross-mode history, continuum rollover, repository index, custom
  tools, update rollback safety.
- v1.5–v1.6: multi-agent context, memory/recall tiers, Lab workspaces,
  engine-variant provisioning through the authoritative resolver,
  Vulkan asset validation on the Windows runner, clone + workspace
  surfaces, stable embedded-frontend asset contract.
- v1.7.0–v1.7.2: reliability passes (engine lifecycle, hub replay,
  stress surfaces), scheduler settlement contract, tool-registry
  snapshot isolation (the one-word-chat crash repair).
- v1.7.1: preflight gate (refuse before engine start), LiveMonitor with
  hysteresis + cooperative critical protection, context-exhaustion
  recovery (typed conditions, one bounded restart), native engine
  first-class backend, LICENSE.md consolidation.
- v1.7.4–v1.7.6: pause/edit/resume with durable checkpoints; transactional
  edits + journals + ten-window fault-injection recovery; restart
  recovery claiming the checkpoint revision; token-aware codename gate;
  generation-aware spec cache; the llama.cpp b10642/b11205 CLI contract
  verified against real `--help` fixtures.

---
Task ID: 1 (v1.8.2 session)
Agent: v1.8.2 engineering session
Task: v1.8.2 — P0 live-visibility repair (Stop-to-see bug), live
reasoning/memory/capability surfaces, P1 log redaction, identity-based
model-caps cache, README→changelog split, engine-identity log clarity,
version truth

Work Log:
- Baseline: upstream `main @ 9d481be3` (`v1.8.1`). Provisioned the
  sandbox toolchain (Go 1.27.1, Node 24, native build via plain make);
  native engine + frontend + headless server all built from THIS tree.
- REPRODUCTION (real stack): `e2e/repro-stream.mjs` drives Chromium
  against the real server + embedded frontend + native-engine fixture,
  tracing every WebSocket frame with timestamps and sampling the live
  generation bubble's DOM at 100 ms. Measured: `response` frames reach
  the browser; the fast path folds them; the rAF flush paints live text.
  The v1.8.1 pipeline is intact in a healthy renderer — every client
  failure class except the flush boundary was eliminated with evidence
  (frame trace, store-site audit, component/CSS audit, Go local-chat
  E2E for the llama path).
- ROOT CAUSE (P0): the streaming flush was armed EXCLUSIVELY on
  `requestAnimationFrame`. In the user's WebView2 runtime compositor
  frame callbacks can be throttled/suspended while the JS event loop,
  the WebSocket and React keep running — phase label + elapsed clock
  update, streamed text never appears, and Stop (synchronous flush)
  reveals everything at once. Repair: `src/stream-flush-scheduler.ts` —
  the flush is armed on an event-loop task (MessageChannel, React's own
  scheduling primitive) AND an animation frame; first to run flushes,
  the other no-ops; ONE coalescing latch; visibility never requires
  frame callbacks. Evidence: 8 deterministic scheduler tests + the
  browser harness.
- P0 live surface: factual no-reasoning state ("Thinking · this model is
  not exposing a reasoning stream" — only while thinking/preparing and
  no reasoning arrived; never fabricated); backend-truth memory line.
- P0 memory evidence: `contextplan.MemoryEvidence` composed at the
  injection site from survival-reconciled facts (summary injected +
  measured tokens; recalled exchanges actually carried; history refs;
  recall attempted) riding the EXISTING `context` activity; rendered
  verbatim (`Memory: session summary · 2 recalled exchanges` / `… · no
  recall matches`; NOTHING before the report arrives). Selection policy
  unchanged (FAST stays cheap; recall on intent; rolling summary
  automatic). Evidence: 2 Go tests + 9 frontend unit tests.
- P0 capability self-model: `taskclassify.SelfDescribe` deterministic
  intent signal (28 phrases, negative-space tested) +
  `internal/agent/selfmodel.go` — ONE bounded formatter over the
  EXISTING authorities: registry snapshot (ShortDescription-first,
  first-sentence fallback), the already-resolved model card (one read
  per run — `resolveEffectiveContextCaps`), config-backed backend
  facts, sysinfo fast snapshot, memory plan. Registered/enabled/
  offered/disabled distinctions; capability questions stay cheap (no
  research/recall/repo; one engine turn). Evidence: intent tests,
  inject/no-inject orchestrator tests, builder unit test.
- P1 log redaction: `internal/logging/redact.go` removes `runId=` /
  `runID=` / `session=` / `sessionId=` / `sessionID=` tokens at the ONE
  central sink (log formatter + crash-report panic text); token-start
  boundaries, empty-paren cleanup, idempotence; prose/URLs/durations/
  causes survive; internal identity untouched. Evidence: 18-case unit
  family + diagnostic-preservation test.
- P1 caps cache: `internal/llm/modelcaps.go` dropped the 10 s TTL for
  identity-based caching — immutable card under (path, size, mtime)
  with no expiry; config-sensitive fields re-derive from the cached
  card on a config-fingerprint change; bounded map, oldest-evicted.
  Evidence: 4-test family (no-reparse beyond TTL window, re-parse on
  identity change, re-derive on config drift, fingerprint coverage).
- P1 README/changelog: README rewritten current-only (no release
  history); `changelog.md` created as the ONE canonical history
  artifact (v1.8.2 detail + v1.8.1/v1.8.0 + compressed pre-history);
  UPDATE.md top section = v1.8.2 notes with v1.8.1 compressed to
  history; ARCHITECTURE.md + agent.md updated to v1.8.2 truth.
- P1 engine identity logs: boot probe reads `engine boot probe: binary
  build <tag> …`; deferred commit names the active build + next-boot
  probe (`staged → boot probe → committed` unambiguous).
- P1 turn latency: measured the class during the caps-cache work — the
  identity-based cache removes the per-turn GGUF metadata re-parse
  (32 MiB reads on Gemma-class cards) that the 10 s TTL always
  expired into. No benchmark numbers published (not measured on
  physical Windows here).
- E2E: `e2e/live-stream.spec.ts` (2 tests) + fixture support
  (`e2e/make-e2e-live-model.py` — a fixture-level unreachable-EOS
  model so the live window is deterministic; `startSheytan` gained
  model/generator/maxTokens options). Both tests PROVE live visibility
  while the run is live (Stop button up), text growth without user
  action, one final message, no duplicates, and an honest mid-run
  Stop.
- Version truth 1.8.2 via the canonical gate (`release-version.mjs
  --check` green: package.json, package-lock, config.go,
  build/config.yml, SIGNATURE); codename gate green.

Verification (evidence classes; Linux x86-64, Go 1.27.1, Node 24):
- go vet -tags headless ./internal/... green; `go test ./internal/...
  -tags headless -count=1` green (zero FAIL).
- Race: api/agent/llm/governor `-race` green (api re-run in isolation
  green; one earlier FAIL was CPU contention with the concurrent
  browser suite, not a race).
- Frontend: typecheck, oxlint 0 warnings, `npm run test:units` 180
  pass, production build + embedded sync green.
- Native: `make -C native/engine test` — all engine test binaries
  pass.
- Browser E2E: FULL suite 31/31 green (29 existing + 2 new
  live-visibility), real stack, no skipped-failure.
- NOT executed here (stated honestly): physical-Windows runtime
  acceptance; the WebView2 frame-throttling behavior was eliminated
  by construction (task-boundary flush) and pinned by deterministic
  tests, not by a physical-host observation.

Stage Summary:
- v1.8.2 complete on this revision: the Stop-to-see bug is repaired at
  the scheduling boundary, the live surface shows truthful reasoning
  and memory state, the model can describe its real tools and
  capabilities, human-facing logs are free of opaque identity tokens,
  repeated model-card work is gone, README is current-only with the
  history in `changelog.md`, and version identity is exactly 1.8.2.

---
Task ID: v1.8.4 session
Agent: v1.8.4 engineering session
Task: v1.8.4 — P0 root-cause and repair of four defect families from the
real Windows runtime evidence: (A) live response/thinking invisible
during generation until Stop; (B) the AUTO Vulkan candidate blocked by a
stale derived GPU posture + the b11273/b11310 boot-probe identity
contradiction; (C) zero-session Send dead end; (D) stale context-usage
races. Documentation to current state; full verification funnel; the
v1.8.4 final source ZIP.

Work Log:
- Baseline verified first: HEAD `ca44b08008f26df467d4bd9c7b70cc49729e1ffb`
  (v1.8.3) on `main` — matches the stated baseline exactly.
- P0-A root cause (from the code, deterministic): the v1.8.2
  `MessageChannelTaskController` was ONE-SHOT (port handler nulled the
  channel after the first delivery; a post while a message was in flight
  chained onto the armed callback WITHOUT reposting). One lost/delayed
  MessageChannel message wedged the controller and the flush latch
  forever — streamed text accumulated until Stop's synchronous flush,
  while the fast path's synchronous `transitionPhase` renders kept the
  phase label moving. Exactly the reported split. Also found the
  ACTIVITY flush was rAF-only (statuses/tools/done/error) — the same
  boundary-loss class for run settlement.
  Repair: `src/stream-flush-scheduler.ts` — THREE independent boundaries
  (reusable MessageChannel macrotask + 0ms timer task + animation frame)
  behind ONE coalescing latch; recoverable latest-wins task controller
  with an in-flight handshake and a deterministic microtask fallback; the
  activity flush moved onto the same scheduler class.
- P0-B root causes: (i) `internal/recommendation` wrote
  `gpuAutoOffload=false + numGpu=0` as a derived posture ("CPU-only
  until a Vulkan engine build is provisioned"); the AUTO probe gate then
  read it as an explicit OFF — the exact circularity in the runtime log.
  Repair: the recommendation never writes a derived OFF; a new
  `gpuAutoOffloadUserSet` field marks explicit user actions (toggle +
  config patch); `config.Load` repairs the legacy derived state ONCE,
  honestly noted and persisted; explicit OFF / manual layers / CPU
  profile are never touched. (ii) the deferred-commit verification
  window keyed boot identity on the RECORDED tag (still the previous
  build until Commit) — the "b11310 staged → probe reports b11273"
  contradiction. Repair: the installer writes a window-scoped
  `engine-stage-pending.json` marker at swap time; `detectCapsForBoot`
  probes/caches/reports the ACTUAL staged binary while it exists;
  Commit/Rollback clear it.
- P0-C root cause: `run()`'s lazy session creation was unreachable — the
  composer textarea and Send button were hard-disabled on
  `!activeSessionId`. Repair: composer enabled on zero-session spaces
  (model + live-run gates unchanged), truthful placeholder/footer.
- P0-D root cause: `refreshSessionContext` guarded only the session id —
  same-session out-of-order responses overwrote newer state; deletion
  never cleared/refreshed the context. Repair: monotonic request
  generation; invalidation on session switch/deletion/creation/mode
  switch/fresh runs; deletion now clears + refreshes a replacement.
- Funnel-surfaced defect (P1): `config.Save` used the FIXED
  `config.json.tmp` name — two concurrent writers rename each other's
  temp away (observed in this repo's own E2E run as a hard engine-start
  failure: "persist default engine path: rename …: no such file"). Every
  config write now uses a unique same-directory temp file.
- Tests added/updated: `src/stream-flush-scheduler.test.ts` (13 —
  including the deterministic lost-message wedge the v1.8.2 design
  fails), `src/context-refresh-race.test.ts` (7), 
  `src/zero-session-send.test.ts` (5), `e2e/zero-session.spec.ts` (4),
  the suspended-rAF live-stream E2E (1),
  `internal/updater/staged_identity_v184_test.go` (3),
  `internal/config/gpu_posture_v184_test.go` (5), recommendation
  contract updates. New store-level suites run the REAL store via the
  extensionless resolver (same harness discipline as v1.8.3).
- Docs: README current-only to v1.8.4 (triple-boundary flush, new
  verified capabilities); UPDATE.md rewritten as v1.8.4 current notes
  with compressed history; changelog.md v1.8.4 entry (factual,
  evidence-cited); ARCHITECTURE.md current for v1.8.4 (flush, zero-
  session, context generation, staged identity, GPU posture contract);
  agent.md handoff updated; ROADMAP.md only records what actually
  shipped with its evidence classes.

Verification (what actually ran here, Linux x64):
- `go build ./...` and the headless build pass; `go vet`-equivalent via
  test compilation clean.
- Go: full `go test ./internal/... -tags headless -count=1` PASS;
  race gate PASS (api, agent, sessions, contextplan, histref, runtime,
  config, updater, recommendation; one load-sensitive governor flake
  observed once under heavy parallel load, not reproducible in 3
  targeted race runs — unrelated to this change set).
- Native C++: `make -C native/engine test` all checks passed.
- Frontend: `tsc --noEmit` clean; `oxlint` clean; `npm run test:units`
  209/209; `npm run test:release` 38/38; release-version gate consistent
  at 1.8.4.
- Browser E2E (real stack: headless server + embedded frontend + native
  engine + Chromium): FULL suite 37/37 green (32 existing + 5 new).
- NOT executed here (stated honestly): GitHub Actions Windows x64
  packaging, physical-Windows/WebView2 runtime acceptance, real-GPU
  Vulkan execution evidence — the repairs are proven deterministically
  and on healthy Chromium; the physical claims remain the user's own
  acceptance step.

Stage Summary:
- v1.8.4 complete on this revision: the streaming flush cannot wedge
  (three boundaries, recoverable controller), the activity lifecycle is
  not rAF-hostage, zero-session Send works end-to-end, stale context
  responses can never land, the AUTO GPU posture is honest and
  provenance-marked, the engine-update window reports the binary that is
  actually on disk, and config writes are race-free. Version identity is
  exactly 1.8.4; no new authority was introduced anywhere.

---
Task ID: 1 (v1.8.5 PHASE 1 session)
Agent: v1.8.5 Phase 1 engineering session
Task: SHEYTAN-LA v1.8.5 Phase 1 — P0 live-streaming server-side repair, P0 reasoning-depth ladder (Low/Mid/High/Ultra) with real backend budgets, P0 Show/Hide Thinking, P1 engine Start/Stop removal, Phase 2 execution/evidence foundation, docs reconciliation, repository ZIP

Work Log:
- Baseline: HEAD `ef2e15f` (v1.8.4), clean tree. Full inspection of the streaming path end-to-end: orchestrator emitProgress (~8ms SmoothStream cadence) → publish wrapper (seq stamp + runLive.observe) → activityHub → WS writer → frontend fast-path fold → triple-boundary flush → render. Frontend v1.8.4 scheduler verified robust (self-healing MessageChannel controller, three boundaries).
- ROOT CAUSE FOUND (the remaining P0 bottleneck): the activityHub's subscriber delivery was a plain 128-deep channel whose overflow policy DROPPED THE NEWEST event ("a slow WebSocket must never block the agent run"). For cumulative response/reasoning snapshots that discards the frame carrying the FULL text while the buffer keeps stale prefixes — under transport backpressure (the same runtime class that lost MessageChannel deliveries) visible text freezes at a stale prefix until the run ends (Stop included), then the reconnect run_snapshot replay reveals everything at once. The server-side twin of the v1.8.4 wedge.
- P0 FIX: bounded, conflation-aware subscriber queues (activitySub). Overflow evicts the OLDEST conflatable event (response/assistant_delta, reasoning/thinking_delta, status — newest subsumes older); order + seq replay contract preserved; terminal events never preferentially evicted; publisher never blocks; memory bounded; per-event write deadline (30s) tears a wedged client down deterministically (reconnect + snapshot replay recover). 9 deterministic regression suites (drop-newest reproduction, terminal survival, publish-never-blocks with 20k offers to a wedged reader, close-drains-first, concurrent publishers, eviction policy); race gate green; full API suite green.
- P0 REASONING LADDER: replaced Auto/Fast/Thinking with Low/Mid/High/Ultra. Verified the llama.cpp request-level `reasoning_budget_tokens` parameter exists in BOTH managed builds by fetching and reading the ACTUAL b10642 + b11205 server sources (server-common.cpp: the OAI chat endpoint maps it into the reasoning budget sampler, applied when the chat template exposes a thinking section; the CLI fixtures independently document --reasoning-budget). Budgets: low=0 (thinking off), mid=1024 (default), high=4096, ultra=not sent (engine default — honest encoding). Implementation: ThinkingControl ladder + legacy migration at BOTH boundaries (Go NormalizeThinkingControl, TS normalizeThinkingControl); ChatRequest.ReasoningBudget (*int — explicit 0 stays on the wire; found AND fixed that the custom MarshalJSON needed the field added — caught by the wire-field-name test); orchestrator applyReasoningBudget gated !IsRemote; tier posture (low=latency floor, mid=neutral, high/ultra=nudge without context inflation); the level rides EVERY run request. 9 Go suites + tier-posture pin + 4 store-level scripted-transport suites.
- P0 SHOW/HIDE THINKING: persisted visibility-only preference (showThinking) gating the rendering of backend-reported reasoning on the live bubble, safety-net bubble and history messages; never in the payload (pinned), never touches level/budget; the store keeps folding reasoning snapshots while hidden; Chat/Agent share the semantics.
- P1 ENGINE CONTROLS: removed the user-facing Start/Stop engine toggle from AgentBody (the supervised-machinery decision); internal lifecycle (run gate EnsureLLMContext, settings restart flow, recovery, shutdown) untouched; download cancellation kept.
- PHASE 2 FOUNDATION: internal/llm/execution.go — the ONE shared execution/evidence ladder (detected → backend-available → device-selected → model-loaded → generation-executed → execution-evidence → verified) as a PURE composer over existing authorities (device presence, backend health, accelerator selection memo, verified model, measured perf ring, runtime offload line); surfaced as /api/engine's execution block; a read-through memo of the accelerator resolution (written by the ONE selection authority on /api/perf) — no second sampler/policy engine. 5 ladder suites pin detected ≠ verified, monotonicity, unknowns, wire contract.
- Docs: README (current product, v1.8.5 surfaces + honest limitations), ROADMAP (explicit Phase 1 COMPLETE / Phase 2 NEXT / Phase 3+ sections), ARCHITECTURE (subscriber delivery, execution ladder, reasoning budgets), UPDATE (v1.8.5 release notes), changelog (v1.8.5 entry), agent.md (v1.8.5 handoff + Phase 2 entry notes).
- Verification: go build headless OK; go vet changed packages OK; full go test for api/agent/llm/taskclassify OK; -race for the same four OK; frontend typecheck OK; 213/213 unit tests (incl. the 4 new reasoning suites); npm run build + sync:web + verify:web OK; release-version gate OK at 1.8.5; browser E2E on the REAL stack: live-stream 3/3 (visible-before-completion, Stop-settles-honestly, suspended-rAF), chat 5/5, composer 4/4 (incl. the thinking-control persistence/travel test). KNOWN FLAKE observed in THIS environment: zero-session.spec Chat-mode visible-before-completion fails ~50% of runs at the ROUTER level (POST /api/run answered 404 before any run/hub involvement — orthogonal to this diff; the Agent-mode zero-session test and all other suites pass deterministically; the same flake appeared across runs in different test positions). No test was weakened or skipped.
- Packaging: download/SHEYTAN-Local-Agent-v1.8.5-PHASE1-FINAL.zip (complete repository state, excluding .git/node_modules/caches/build artifacts).

Stage Summary:
- v1.8.5 Phase 1 COMPLETE: the live-streaming failure class is now closed on BOTH sides of the wire (v1.8.4 render side + v1.8.5 server side); the reasoning surface has real, verified backend meaning; thinking visibility is a first-class control; the engine left the user's critical path; Phase 2 starts from an explicit execution/evidence contract and a precise roadmap.
- Phase 2 entry: ROADMAP.md §Phase 2 + agent.md "Phase 2 entry notes".

---
Task ID: 1 (v1.8.8 session)
Agent: v1.8.8 engineering session
Task: v1.8.8 — audit-job repair, release-metadata synchronization, dependency
security, dataAnalysis hardening, E2E zero-session reload-persistence repair;
clean-room build and package of SHEYTAN-Local-Agent-v1.8.8-FINAL.zip

Work Log:
- Baseline: HEAD `e9a8448` (v1.8.7). Reproduced the CI run 37446279646
  audit-job failure exactly: `codename-gate.mjs` flagged 4 tracked lines in
  `internal/tools/data_tool_test.go` (650/658/666/672) — the v1.8.7
  TestJSONColumnOrderIsDeterministic fixtures used the retired codename as a
  JSON key. Renamed the fixture key to a codename-free alternative that
  preserves the ordering semantics (document order ≠ alphabetical order);
  the gate stays strict and the regression stays real (no exemption added).
- Release metadata: package.json 1.8.8; release-version.mjs repaired
  config.go / build/config.yml / SIGNATURE; package-lock.json root metadata
  synchronized (stale at 1.8.5 since v1.8.6); README current-release marker,
  ROADMAP CURRENT RELEASE section, UPDATE.md release notes, changelog §v1.8.8.
- npm audit: the high-severity advisory (GHSA-68fv-2mgg-jv7q,
  source-map-js <1.2.2 via vite→postcss) repaired with the semver-compatible
  lockfile-only upgrade to 1.2.2; direct dependency contract untouched;
  `npm audit` now reports 0 vulnerabilities.
- dataAnalysis hardening (the ONE data authority — no new subsystem):
  (a) export with `limit` mutated the LRU-cached dataset (cache poisoning)
  — the limited view is now a separate dataset; (b) delimiter sniff counted
  delimiters inside quoted fields — now quote-aware (countUnquoted);
  (c) analyze correlations ordering relied on sort internals for equal |r| —
  explicit (a,b) tie-breaker with SliceStable; (d) a requested correlations
  section with <2 numeric columns silently vanished — now answers honestly;
  (e) the 256 MB error text implied chunked processing — message corrected
  to the honest in-process bound; (f) join's right-index build honors
  cancellation; (g) the sample-mode PRNG global state is mutex-guarded.
  New `data_tool_hardening_test.go` pins RFC-4180 edges, splitLinesAny
  parity, delimiter sniff, numeric semantics, stats/outlier edges, aggregate
  degenerate groups, join many-to-many/empty, quality ±Inf, export cache
  containment/determinism, JSON later-keys/duplicate-keys, correlation
  tie-break.
- E2E (real stack, real engine): the zero-session reload-persistence spec
  failed deterministically. Trace + network HAR + WS frame logs root-caused
  it: run() kept `running:false` across session creation and the bounded
  attach wait, so the composer read idle while a Send was mid-startup; a
  reload in that window killed the pipeline before POST /api/run and the
  message never persisted (HAR: POST /api/sessions ok, no POST /api/run,
  messages total:0). Repair: the run-startup state (running/preparing) is
  set BEFORE the transport work in run(). Spec green in isolation (2.2s) and
  in the full suite.
- Verification evidence: frontend typecheck/lint/213 units/38 release green;
  codename gate green; release-version --check green; go test
  ./internal/... -tags headless -count=1 green; -race audit green on
  api/agent/sessions/contextplan/histref/runtime/tools/engdiscovery; native
  engine cmake clean configure + 12/12 ctest; 15 TestRealCppHost* Go
  integration tests RUN (not skipped) and pass; Playwright full suite
  36-37/38 per run, remaining failure class = live-window sampling
  sensitivity (proven pre-existing via stash experiment: live-stream:87
  fails identically on unmodified v1.8.7 code in this sandbox; the
  deterministic streaming siblings — Stop + rAF-suspended growth — pass).
- Sandbox gaps recorded honestly (no root): `go test ./...` and `go vet`
  fail only on the Wails cgo desktop packages (gtk4/webkitgtk-6.0 headers
  absent; CI installs them); Playwright --with-deps unusable (sudo) but
  chromium runs; all other gates executed for real.

Stage Summary:
- Every v1.8.8 surface reads 1.8.8 through the ONE release authority.
- The audit job's blocker is repaired at the root; platform jobs unblocked.
- The one dataAnalysis authority is hardened with regression coverage.
- The zero-session reload-persistence defect is fixed in the product, not
  the test.
- Deliverable: SHEYTAN-Local-Agent-v1.8.8-FINAL.zip (728 files, ZIP audit:
  opens, root present, required sources present, no junk, version 1.8.8,
  no stale identity, no codename leakage).

---

Task ID: v1.9.0-release
Agent: release engineering session (2026-10-08)
Task: v1.8.8 -> v1.9.0 — the AI System Builder + long-horizon agentic
engineering layer; root-cause repair of the v1.8.8 Linux zero-session
reload-persistence E2E blocker; version/doc restructure; packaging.

Work Log:
- P0 root cause (Actions 37654274420, e2e/zero-session.spec.ts reload
  test): the zero-session lazy `createSession()` POST ran while the store
  still read `running:false` — the v1.8.8 repair moved the startup state
  before the attach wait but left this ONE async window; the E2E
  "composer enabled" probe legitimately sampled INSIDE it and reloaded
  before POST /api/run (session exists server-side pending; transcript
  total:0 forever). Product fix in `src/store.ts`: startup state set
  BEFORE all transport work; lazy create carries `keepRunState` (it
  previously reset running/runPhase mid-startup); failure paths clean up
  deterministically; attachment snapshot before transport. E2E waits for
  the deterministic run-dispatch marker (optimistic user bubble) + the
  assistant row, then reloads and asserts the DURABLE transcript.
  Deterministic store regressions: zero-session-send.test.ts #6/#7 (+#4
  cleanup assertions).
- AI System authority: internal/aisystem (durable per-document store,
  atomic writes, bounded, deterministic, corruption-tolerant, reserved
  default system, non-destructive migration, revision monotonicity,
  clone/export/import, active pointer with repair). HTTP /api/systems.
  Execution binding frozen per run (systemId + systemRevision):
  instructions block, model override, reasoning preference (existing
  v1.8.5 ladder; explicit request wins), tool-surface constrain
  (ToolPolicy.AISystemConstrain — offer AND execution, remove-only),
  skills filtering, approval policy. ai_system activity event + run
  response identity.
- Goal engine: internal/goal (phases, bounded plan, per-step +
  goal-level evidence journal, checkpoints after planning/each step/
  approval/terminal, bounded replanning that keeps failure evidence,
  turn-budget parking, boot recovery marking found-live goals paused —
  idempotent, terminal stays terminal). Drive executor = ONE orchestrator
  with the frozen AI System snapshot. HTTP /api/goals (+start/pause/
  resume/cancel/approve/reject). Goals UI card.
- Approval authority: internal/approval (5 deterministic risk classes,
  policy vocabulary, exact normalized call identity + bounded ledger),
  per-run agent.WithApprovalGate seam installed ONLY by goal runs
  (deny-by-default; chat/agent behavior byte-identical), durable
  goal-level approval parking with exact-call resume + evidence on
  rejection + stale-id rejection.
- Bounded delegation: internal/multiagent/subtasks (read-only
  parallelism <=2, mutating serialized, no nested spawning, fan-out <=8,
  honest deadline blocks, deterministic subtaskId-order merge, failures
  stay failed). Race-tested.
- Repository navigation: internal/repoindex/navigation.go (`repo_nav`:
  open/navigate/read/grep; bounded ranges, provenance, honest truncation;
  same path-safety authority as repo_search). Registered in the runtime.
- Frontend: System Centre hosts the AI System selector/editor (create/
  edit/activate/clone/export/import/delete) and the Goals card (create+
  start, honest state chips, plan progress from actual plan state,
  pause/resume/cancel/approve/reject). Pure view-model unit-tested
  (ai-systems-view.test.ts). New E2E: e2e/ai-systems.spec.ts.
- Live-visibility fixture: e2e/make-e2e-live-model.py dims re-tuned
  (emb=144, layers=6, heads=6, kv=3, head_dim=24, ffn=384) after
  measured evidence: the previous dims generated the full 160-token
  reply in ~6 ms on a fast host — first response frame and done 6 ms
  apart — so the live surface legitimately never painted between two
  renderer frames (pre-existing sensitivity recorded for v1.8.7/v1.8.8
  sandboxes above); the first re-tune (emb=256/layers=10) overshot into
  a >30 s generation (observation bound expired before the first token).
  The shipped dims stream 22 response frames over a ~2 s window on this
  host: real generation, deterministic visibility on any host speed.
- Verification: go test ./internal/... -tags headless = ALL PASS; go vet
  (headless) clean; race detector green on goal/multiagent/aisystem/
  approval; npm typecheck/lint/220 unit/38 release tests green; full
  Playwright suite 40/40 (including zero-session reload and the new
  ai-systems flows) against the real headless server + real native
  engine + fixture GGUF. Sandbox gaps (unchanged, honest): no root —
  the Wails cgo desktop packages cannot compile here (gtk4/webkitgtk
  headers absent; CI installs them); no codename literals in this log.

Stage Summary:
- v1.8.8 -> v1.9.0: every surface reads 1.9.0 through the ONE release
  authority; changelog.md gains the v1.9.0 entry; ROADMAP restructured
  (compact baseline, v1.9 current/future boundary, v1.10-2.0 future);
  UPDATE.md = v1.9.0 notes; README current-only; ARCHITECTURE documents
  the v1.9 authorities; agent.md is the repaired v1.9.0 handoff.
- The Linux E2E blocker is fixed in the product; the full browser suite
  is green on the real stack; Windows-side sources carry no regression
  (platform-independent Go/TS only).
- Deliverable: download/SHEYTAN-Local-Agent-v1.9.0-FINAL.zip
  (repository tree; ZIP audit: opens, required sources present, version
  1.9.0 everywhere, no stale current identity, no codename leakage,
  exactly one LICENSE.md).

---
Task ID: v1.9.1-release
Agent: v1.9.1 engineering session (2026-10-08)
Task: v1.9.0 -> v1.9.1 — P0 root-cause + repair of the authoritative
Linux CI live-stream failure (Actions run 37704905404, Linux Browser-E2E
job 113077751580, e2e/live-stream.spec.ts "streamed text is visible
WHILE the run is live and grows without Stop"; CI 39/40), evidence-
discipline repair for the v1.9.0 "40/40" claim, Windows/Linux desktop
RUNTIME SMOKE gates in CI, packaging.

Work Log:
- EVIDENCE CLASSIFICATION FIRST: the v1.9.0 entry below records "full
  Playwright suite 40/40" — that was a LOCAL single-run claim. The
  authoritative GitHub Actions run 37704905404 shows 39/40 with the
  live-stream growth assertion failing. Correction recorded here: local
  runs are local evidence only; GitHub Actions is the only CI authority;
  the v1.9.1 verdict below is likewise local until the CI run carrying
  this revision reports.
- INSPECT: HEAD 4cdd392 (v1.9.0); the whole streaming chain was read
  (orchestrator emitProgress -> runLive.observe -> activityHub (bounded,
  conflation-aware) -> WS writer -> store fast path -> accumulator ->
  triple-boundary scheduler -> GenerationBubble DOM).
- MEASURED FIXTURE FACTS (this 2-core sandbox, real native engine):
  standalone engine-host probe — decode ~130+ tok/s (160 tokens over 21
  event frames in ~0.26 s warm), and PREFILL ~25-40 ms per prompt token
  (2-token prompt TTFT 0.02 s; ~300-token prompt TTFT 8.15 s; ~450-token
  prompt TTFT 15.9 s). Full-stack run telemetry: system briefing 440
  tokens, context 446/3.6k, TTFT 7.6 s, classifyMs 4091 (the classify
  stage includes the engine-gate wait), run total ~12.8 s, response
  frames 22 over a ~1.2 s window (p50 inter-frame 62 ms), final persisted
  reply 203 chars, exactly one assistant message.
- REPRODUCE: the failing test passed in isolation UNLOADED (20.3 s), so
  the CI failure was reproduced under MEASURED CPU CONTENTION (2 busy
  workers saturating both cores): the test FAILED with the exact
  authoritative signature — growth poll "Expected: > 0, Received: 0"
  (predicate stuck at 0: Stop visible the whole time, text never past
  the baseline) and the failure screenshot shows the run STILL LIVE at
  30 s with the bubble rendering the "…" PLACEHOLDER arm.
- ROOT CAUSE (test observation contract, NOT the product stream): the
  v1.8.2 contract sampled the live bubble's textContent and accepted ANY
  non-empty text as "streamed text". GenerationBubble renders
  PRESENTATION PLACEHOLDERS before the first content snapshot
  ("Connecting to the engine and preparing the turn…" — 48 chars — while
  preparing, "…" afterwards), so waitForLiveText passed within ~100 ms
  of Send on placeholder text; the growth poll's 30 s budget then ran
  during the engine gate + prefill phase (measured above: >30 s under
  contention), expiring before the real streaming window ever opened.
  The product chain was verified healthy end to end (WS frame + DOM
  timeline: browser received every cumulative snapshot; DOM followed at
  50-70 ms cadence; one persisted reply).
- REPAIR (observation only, never weaker):
  1) src/MessageStream.tsx: both placeholder arms now carry
     data-stream-placeholder in the DOM — presentation text is
     distinguishable from streamed content.
  2) e2e/live-stream.spec.ts: waitForLiveText + both growth observations
     require NON-placeholder snapshots; the growth baseline is the FIRST
     REAL streamed snapshot; the rAF-suspended proof uses the same
     real-content contract. All original assertions preserved.
  3) Fixture budget 160 -> 320 tokens: the engine emits one event chunk
     per 8 tokens (kEmitTokenWindow), so the budget IS the deterministic
     snapshot count of the live window (40 chunks); window duration
     still scales with host speed, snapshot count does not.
- MUTATION VERIFICATION: with the repaired test under the same 2-worker
  contention — (a) repaired contract: PASS; (b) placeholder detection
  disabled (the v1.9.0 contract restored by mutation): FAIL with the CI
  signature; (c) restored: PASS unloaded and under load. The coverage
  detects the defect class it was written for.
- CI RUNTIME SMOKE GATES (wired for the CI environment; NOT executed in
  this sandbox): build-linux gains "Desktop runtime smoke (native Wails
  binary under Xvfb)" — the REAL CGO desktop executable launched under
  Xvfb, session banner + boot markers asserted STATE-BASED in the
  isolated data dir's logs/app.log, liveness + no-crash-report, bounded
  SIGTERM shutdown — and "Headless surface runtime smoke" — the headless
  build serving the REAL embedded UI + API: event-driven /api/health
  readiness, GET / proves the embedded shell, /api/models discovery,
  clean shutdown. build-windows gains "Desktop runtime smoke (real
  executable, process + backend init)" — SHEYTAN-LA.exe launched with an
  isolated SHEYTAN_DATA_DIR, banner/liveness/no-crash state-based,
  CloseMainWindow + bounded exit (Kill only as a failing-run backstop).
  Packaging remains a DISTINCT gate: packaging success never implies GUI
  runtime success. Evidence levels are labelled explicitly in the steps.
- LOCAL VERIFICATION OF THE SMOKE DESIGN: the headless-surface smoke was
  executed HERE against the real binary and passed (embedded UI +
  health + model discovery + clean shutdown). The native Wails desktop
  binary CANNOT be built in this sandbox (no gtk4/webkitgtk pkg-config
  headers, no root) — same documented boundary as v1.9.0; its smoke step
  is CI-wired and its execution is recorded nowhere until CI runs it.
- Version identity 1.9.1 through the canonical gate (release-version
  --check green); changelog.md gains the factual v1.9.1 entry; UPDATE.md
  rewritten as v1.9.1 current notes; agent.md is the v1.9.1 handoff;
  ROADMAP gains the measured native-prefill batching work item; README
  current-only.

Verification (evidence classes; LOCAL Linux x86-64 sandbox, Go 1.27,
Node 24, 2 cores — see the classification note above):
- Frontend: typecheck green; oxlint 0 warnings/0 errors (108 files);
  npm run test:units 220/220; npm run test:release 38/38 (includes the
  codename gate); production build + embedded sync green;
  verify-static-assets green.
- Go: go test ./internal/... -tags headless -count=1 = 62 packages ok
  (2 load-sensitive flake observations in internal/api on this 2-core
  box while OTHER Go suites ran concurrently: TestGovernorEndpoint-
  ServesComposedSelfModel and TestLocalGemmaHiChatCompletes (30 s
  chat deadline) — both pass in isolation, both also flaked at the
  v1.9.0 baseline the same way, and the api package passes cleanly
  when run sequentially; not v1.9.1 regressions — CI runs suites
  sequentially). go test ./... -run Test = 61 packages ok + the two
  sandbox-blocked desktop packages (root + internal/desktop) failing
  to BUILD for the documented no-GTK-headers boundary. go vet ./cmd/...
  ./internal/... clean (the same two packages are the sandbox build
  boundary; CI vets the full module with the deps installed). Race
  gate (-race, headless): api/agent/sessions/contextplan/histref/
  runtime ALL ok.
- Native: clean cmake configure + build + ctest 12/12 green.
- Browser E2E: FULL suite 40/40 green (5.6 min, real headless server +
  real native engine + fixture GGUF, no skipped failures) — the
  repaired live-stream spec (3 tests) included; additionally the
  live-stream growth test was verified under 2-worker CPU contention
  (PASS) and with the placeholder detection mutated away (FAIL with
  the CI signature — the mutation evidence above).
- Stress: 47 pass / 0 fail, hangs=0, crashes=0.
- Headless-surface runtime smoke: executed HERE against the real
  binary — PASS (embedded UI + /api/health overall=ok + model
  discovery + clean SIGTERM shutdown).
- NOT executed here (stated honestly): GitHub Actions (no runner
  access from this sandbox), the native Wails desktop smoke (GTK4/
  WebKitGTK headers absent — the binary cannot even build here),
  Windows (no Windows). The authoritative v1.9.1 CI verdict is the
  Actions run that carries this revision; until it reports, no CI
  claim is made for v1.9.1.

Stage Summary:
- v1.9.0 -> v1.9.1: the authoritative live-stream CI failure is
  root-caused (placeholder-baseline observation contract racing the
  measured prefill phase — NOT a product streaming defect; the chain was
  verified healthy end to end), repaired with strengthened (never
  weakened) assertions, mutation-verified coverage, and a deterministic
  fixture snapshot budget; the v1.9.0 "40/40" claim is re-classified as
  local-only evidence with the CI 39/40 recorded as authoritative;
  desktop runtime smoke gates (Linux Xvfb + Windows process/backend-init)
  are wired into CI with explicit evidence-level labels; version
  identity is exactly 1.9.1 through the canonical gate.
- Full local regression green on this revision (see Verification above);
  the CI verdict for this revision is pending by construction (no local
  run may claim it).
- Deliverable: download/SHEYTAN-Local-Agent-v1.9.1-FINAL.zip (complete
  reproducible source tree; exclusions per the release contract; ZIP
  audit recorded in this entry).
