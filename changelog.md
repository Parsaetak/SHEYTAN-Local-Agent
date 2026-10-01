# changelog.md — SHEYTAN-Local-Agent Release History

**This file is the ONLY canonical release-history artifact.** The README
describes the current product only; per-release evidence lives in the
release tags, the tests that shipped with them, and `UPDATE.md` (current
maintenance behavior).

Every release was shipped with the same invariants: one authority per
concern, evidence over confidence, no sleep-based correctness, honest
hardware claims, the codename gate enabled.

---

## v1.8.3 — 2026-10-01

Focus: root-cause and repair the exact Linux CI session-delete failure
(Actions run 36713108772, job 109880449134: Browser E2E 30/31,
`e2e/sessions.spec.ts:88` "delete session removes it and activates a
remaining one" — `Expected: < 4, Received: 4` after 15 s), and close the
real defects the investigation exposed. No new authority was created; the
v1.8.2 streaming, memory-evidence and self-model behavior is untouched
(the full browser suite, including every live-stream test, passes).

1. **P0 — the run-107 failure root-caused with instrumented evidence.**
   An instrumented diagnostic (request/response journaling + server-side
   authoritative list snapshots + DOM identity capture, with the browser's
   create response delivery delayed to reproduce CI latency) proved the
   interleaving: the test sampled its count baseline BEFORE the
   asynchronous "New session" create landed in the sidebar (its
   `expect(first()).toBeVisible()` gate was satisfied by the PRE-EXISTING
   items, not the new one); the delete then correctly removed exactly one
   session — verified absent from the server's authoritative post-delete
   list — while the just-created session's late response kept the sidebar
   count flat. The product deleted the correct session and never
   resurrected it; the count arithmetic compared equal numbers for 15 s.
   A passing run of the same test was the same interleaving with the poll
   catching the transient count window — a coin flip on runner latency.

2. **P0 — the browser test repaired and strengthened (measurement
   synchronizes on state, never on timing).** The repaired test waits —
   state-based, no sleeps — for the created session to actually appear
   (the count grows by one) before sampling its baseline, then asserts
   the SPECIFIC deleted id (new `data-session-id` rows; the rendered
   8-char id slice is ambiguous within the same bucket), the survivor
   identity set, the count, replacement activation, composer usability,
   and — after a full reload — that the deleted id is still absent from
   both the sidebar and a fresh authoritative list fetch. No timeout was
   increased; nothing was weakened.

3. **P0 — the startup session-list write now goes through the ONE
   generation guard (the v1.7.5 authority).** `initializeAgentOnce`
   previously applied its `GET /api/sessions` response WITHOUT a ticket —
   the v1.7.4 stale-response defect class survived in the init consumer.
   The sidebar's "New session" button is actionable while the startup GET
   is on the wire, so a user create landing in that window was clobbered
   by the stale startup list: the created session VANISHED from the
   sidebar. The repair: the init takes a `sessionListGuard` ticket before
   the GET; a response may only land while its ticket is current and the
   mode unchanged; a superseded response applies only the non-session
   state and delegates the list + selection re-resolution to
   `refreshSessions()` (its own ticket); the eager first-install create
   invalidates the guard exactly like `store.createSession` does. Verified
   by mutation at two layers: the store-level suite and a real-browser
   E2E both FAIL against the unguarded v1.8.2 init and PASS with the
   repair.

4. **P0 — deterministic session-delete coverage at three layers.**
   `src/session-delete-regression.test.ts` (11 scenarios against the REAL
   store over a scripted HTTP transport where response ordering is forced
   by causality — pending/persisted delete, replacement selection,
   delete-vs-held-GET, stale-GET non-resurrection, mode separation,
   honest duplicate-delete, post-delete fresh fetch, init-race survival,
   clean-init apply, eager first-install session preserved — plus a
   Node resolve hook making the store testable outside the bundler);
   `internal/sessions/delete_pending_test.go` (pending delete removes
   every authority; persisted delete cleans index/file/sidecars with a
   fresh-store no-resurrection check; unknown-id honest failure); and the
   new browser E2E for the init race.

5. **P0 — a failed DELETE is never silently swallowed.** The sidebar
   invoked `void deleteSession(id)`; a server-side DELETE failure was an
   invisible unhandled rejection and the session stayed in the list with
   no explanation. `deleteSession` now surfaces the failure through the
   same error state `renameSession` uses (state untouched on failure,
   never a fake success).

6. **P0 — the created-session prepend is idempotent.** A list refresh
   whose response lands between the create POST's dispatch and its
   arrival can already contain the created session; the unconditional
   prepend then produced a duplicate row. The prepend now dedupes by id —
   the new session is always the first row and appears exactly once.

7. **P1 — roadmap repaired.** `ROADMAP.md` now states the artifact truth
   (changelog = sole release history; README current-only; UPDATE.md =
   current release/maintenance evidence) and carries the evidence-ranked
   **Performance, Reliability & Scale Backlog** (D/I/H labels). External
   report claims — percentages, TPS/TTFT/cache/VRAM figures, "3x/5x/80%"
   numbers, Tokio/asyncio rewrites, mandatory FAISS/USearch/SQLite/
   bbolt/tree-sitter, NPU embedding services, dual-model VRAM swapping —
   are explicitly treated as hypotheses or rejected; the one-authority
   architecture and the Go runtime are preserved.

Verification: frontend typecheck/lint/unit 191/191; `go test ./internal/...
-tags headless` 58 packages; race suite (api/agent/sessions/contextplan/
histref/runtime); native C++ build + ctest 12/12; full browser E2E
32/32 (the repaired delete test and the new init-race test included);
stress suite 47/47 with zero hangs/crashes; release metadata and codename
gates in check mode. Windows acceptance on a physical machine remains
outstanding until observed (CI is not physical-host evidence).

---

## v1.8.2 — 2026-09-30

Focus: make live generation genuinely visible and self-describing; remove
avoidable per-turn overhead; clean human-facing logs. No new memory or
capability authorities were created — every feature consumes the existing
ones.

1. **P0 — the "text visible only after Stop" live-rendering defect,
   root-caused and repaired.** v1.8.1's single-frame fast path folded
   stream-critical events into the accumulator at socket-receive time, but
   the FLUSH was armed exclusively on `requestAnimationFrame`. In the
   affected WebView2 runtimes the compositor's frame callbacks can be
   throttled or suspended while the JS event loop, the WebSocket and
   React's own scheduler keep running — phase labels updated, elapsed
   clock ticked, streamed text never appeared, and pressing Stop (whose
   path flushes synchronously) revealed everything at once. The flush is
   now scheduled on TWO deterministic boundaries — an event-loop task
   (MessageChannel, the primitive React's scheduler itself uses) AND an
   animation frame; whichever runs first flushes, the other no-ops.
   Exactly one coalescing latch remains (never one render per token), and
   visibility no longer requires frame callbacks at all.
   Evidence: `src/stream-flush-scheduler.test.ts` (8 deterministic tests:
   one-flush latch, first-boundary-wins, cancel, task-only sufficiency),
   plus the real-stack browser harness (`e2e/repro-stream.mjs`) tracing
   socket→fast-path→accumulator→flush→DOM.

2. **P0 — the live surface shows more of what is happening.** The live
   generation bubble now carries: (a) a concise FACTUAL state while the
   model emits no reasoning stream ("Thinking · this model is not
   exposing a reasoning stream") — never fabricated reasoning, never
   generated labels presented as model thoughts; (b) a backend-truth
   MEMORY indicator (see 3); (c) the existing phase, cursor, reasoning
   panel and runtime activity, unchanged in authority.

3. **P0 — memory boosting made real, visible and evidence-backed.** The
   orchestrator composes a `MemoryEvidence` record at the injection site
   from the survival-reconciled injection facts (rolling session summary
   injected + measured tokens; recalled exchanges actually carried; the
   attached cross-mode history references; whether recall was attempted
   at all) and publishes it on the existing `context` activity. The live
   UI renders exactly this record — e.g. `Memory: session summary · 2
   recalled exchanges` or `Memory: session summary · no recall matches` —
   and nothing when the report has not arrived. A block the history
   windower elided is never claimed. No second memory engine: the
   selection policy remains the existing tiers (FAST stays cheap; targeted
   recall fires on memory-relevant intent; the rolling summary stays
   automatic). Evidence: `TestContextActivityCarriesMemoryEvidence`,
   `TestMemoryEvidenceOnContextPlan` (Go),
   `src/memory-evidence.test.ts` (9 unit tests).

4. **P0 — the model can accurately describe its own tools and
   capabilities.** A deterministic capability-intent signal
   (`taskclassify.Signals.SelfDescribe`) fires on vocabulary like "what
   tools do you have?" / "what can you do?" / "what model are you
   running?"; on that intent the orchestrator injects ONE bounded runtime
   self-model block composed from the EXISTING authorities — the tool
   registry snapshot (`ShortDescription()` first, bounded first-sentence
   fallback), the already-resolved model card (architecture, quantization,
   parameters, context limit, tokenizer family, chat template, vision only
   when real, native-executability verdict), config-backed backend facts
   (provider, installed engine build, streaming/cancellation support,
   research availability, Lab and memory availability), the sysinfo fast
   snapshot (OS/CPU/RAM measured; GPU/NPU labeled detection-only) and the
   turn's memory plan. Truthful distinctions: registered vs enabled vs
   offered-this-request vs disabled-and-NOT-callable. Capability questions
   stay cheap: no research, no recall, no repo indexing, no extra engine
   turns. Evidence: `TestClassifySelfDescribeIntent`,
   `TestSelfDescribeAddsNoComplexity`, `TestCapabilityIntentInjectsSelfModel`,
   `TestOrdinaryChatDoesNotInjectSelfModel`, `TestBuildSelfModelCatalog`.

5. **P1 — human-facing logs lose the opaque identity tokens.** `runId=`,
   `runID=`, `session=`, `sessionId=`, `sessionID=` tokens are redacted at
   the ONE central sink (the log record formatter), so app.log, the UI
   LogViewer ring, stderr and crash-report text all inherit the rule.
   Timestamps, severity, subsystems, durations, outcomes, error causes and
   ordinary prose containing the word "session" survive; internal identity
   (API objects, run state, journals, storage keys) is untouched.
   Evidence: `internal/logging/redact_test.go` (18 cases + idempotence +
   diagnostic-field preservation).

6. **P1 — repeated per-turn model-card parsing eliminated.** The model
   capability cache replaced its arbitrary 10-second TTL with
   identity-based caching: the immutable parsed GGUF card is cached under
   (path, size, mtime) with no expiry, and the config-sensitive derived
   fields re-derive from the cached card whenever a configuration
   fingerprint changes — a turn separated by minutes no longer re-reads a
   32 MiB metadata block. The map is bounded; a replaced model file
   re-parses exactly once. No second capability authority.
   Evidence: `internal/llm/modelcaps_cache_v182_test.go`.

7. **P1 — README is current-only; release history moved to this file.**
   The README no longer carries release-history sections or duplicated
   chronology; `changelog.md` is the canonical history. `ARCHITECTURE.md`
   (architecture), `agent.md` (current handoff) and `UPDATE.md` (current
   operational notes) remain the other documentation authorities.

8. **P1 — engine identity log stages made explicit.** The boot-time
   capability probe now reads `engine boot probe: binary build <tag> …`
   and the deferred-commit line names the build that became active
   (`… build <tag> committed as the active engine (next boot probes it)`),
   so a log sequence `staged → boot probe → committed` is unambiguous
   about which build is serving and which is staged.

---

## v1.8.1 — 2026-09-30

* **P0 — real-Windows local-generation crash repaired at the root.** On a
  machine running a small local Gemma-class model, an ordinary `hi` turn
  panicked with a nil pointer dereference between `task classified` and
  `tier selected`, settling as an error in ~57 ms. Root cause (reproduced
  deterministically): Gemma-class GGUFs carry ~262K-entry tokenizer blocks
  whose metadata exceeded the model-card parser's 8 MiB read bound, so the
  capability card resolved to nil and the orchestrator's estimator block
  dereferenced it. Repairs: the documented nil-card fallback (configured
  context + conservative estimator) in `resolveEffectiveContext`, and a
  32 MiB read bound so real Gemma-class cards parse again.
  Evidence: `TestResolveEffectiveContextSurvivesUnreadableCard`,
  `TestGemmaClassCardParsesUnderRaisedBound`, `TestLocalGemmaHiChatCompletes`
  (the api package's first local-provider run-level E2E),
  `TestLocalChatSurvivesUnreadableModelCard`.
* **P0 — streamed answers became visible through ONE render frame instead
  of two.** `src/stream-fast-path.ts` folds stream-critical events into the
  streaming accumulator the moment the WebSocket delivers them; the
  timeline batch, sequence/stale-run protection, cumulative-snapshot
  semantics, replay idempotence and synchronous done/error/abort flushing
  are preserved (12 deterministic frame-controller tests). (v1.8.2 note:
  the flush-boundary dependency this left on `requestAnimationFrame` is
  repaired above.)
* **P1 — engine rollback restores the recorded identity** — a failed
  startup verification after a package swap could leave the transient
  bundled-default tag stamped in `installed.json`; `Rollback()` now
  re-records from the restored manifest.
  Evidence: `TestRollbackRestoresRecordedIdentityFromManifest`.
* Audited, no change: the AppData root migration contract; GPU/AUTO CPU
  with `numGPU=0` as the honest evidence-ladder outcome.

## v1.8.0 — 2026-09-30

* **P0 — pause→resume→pause synchronization repaired at the root** —
  after a resume, proof of live generation requires a strictly newer run
  sequence AND a changed cumulative snapshot (`resumedGenerationEvidence`).
* **P0 — honest abort marker end-to-end** — the orchestrator publishes
  `aborted` (never `done`) for canceled generations; the live state, the
  outcome registry and the frontend agree; the abort-after-resume flake
  is gone (15/15 stressed runs).
* **Runtime Governor vertical slice (Adaptive Runtime Intelligence)** —
  resource state with explicit unknowns, pressure model with rolling
  signals, execution envelope with honest adjustment classes, resident-
  budget admission, provenance-carrying self-model. Policy only.
* **`GET /api/governor` + System Centre Governor card** with real values
  and named unknowns.
* Documentation consolidated; version truth through the canonical gate.

## v1.7.0 → v1.7.6 (compact)

* **v1.7.6** — edit-transaction journals with ten-window crash/fault
  recovery matrix; restart-recovered runs claim the checkpoint revision;
  paused surface excludes journals; token-aware codename gate;
  generation-aware spec cache.
* **v1.7.5** — transactional pause/edit/resume (CAS validation, transcript
  replace, checkpoint commit, publication last); stale session-list
  generation guard; numeric-variant gates.
* **v1.7.4** — pause/edit/resume state machine, durable checkpoints, WS
  replay continuity, double-pause idempotency, stop-after-pause semantics.
* **v1.7.3** — recovery/handoff durability and lifecycle hardening.
* **v1.7.2** — scheduler settlement contract, live-protection hardening.
* **v1.7.1** — preflight gate (PREFLIGHT → REFUSE → NO ENGINE START), live
  monitor with hysteresis + cooperative critical protection, native engine
  first-class backend, context-exhaustion recovery.
* **v1.7.0** — reliability pass: engine lifecycle, hub replay, stress
  surfaces.

## v1.5 → v1.6 (compact)

Multi-agent context, memory/recall tiers, engine-variant provisioning
through the authoritative resolver, Vulkan asset validation, clone +
repository indexing, Lab workspaces, stable embedded-frontend asset
contract.

## v1.3 → v1.4 (compact)

Durable summaries, cross-mode history, task state, handoff, continuum
rollover, repository index, custom tools, update rollback safety.

## ≤ v1.2 (compact)

Foundations: sessions, streaming with sequence-stamped replay, tool
registry, downloader, installer, health checks, environment centre,
bounded authoritative run state, settlement edge.
