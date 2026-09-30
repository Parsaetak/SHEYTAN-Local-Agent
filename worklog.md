# SHEYTAN-Local-Agent — Engineering Worklog

Current release:  v1.8.2

This worklog is a session log, not a second architecture document. The
architecture truth lives in `ARCHITECTURE.md`, the release evidence in
`UPDATE.md`, the future in `ROADMAP.md`.

---
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
