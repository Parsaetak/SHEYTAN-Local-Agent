# UPDATE.md — v1.8.2 Release Notes & Maintenance Behavior

**Release:** `v1.8.2` (canonical application version; single version
hierarchy: package.json → release-version.mjs → config.go /
build/config.yml / SIGNATURE)
**Base:** `main @ 9d481be3` (`v1.8.1`) · **Date:** 2026-09-30
**Package:** `SHEYTAN-Local-Agent-v1.8.2-FINAL.zip` (complete repository
tree)

The authoritative per-release history lives in `changelog.md`; this file
carries the CURRENT release notes and the operational maintenance
behavior.

## v1.8.2 changes

1. **P0 — live streamed text no longer waits for Stop.** v1.8.1's fast
   path folded stream-critical events into the accumulator at
   socket-receive time but flushed exclusively on `requestAnimationFrame`;
   in the affected WebView2 runtimes frame callbacks can be throttled or
   suspended while the event loop, the socket and React keep running —
   phase labels and the elapsed clock updated, streamed text never
   appeared, and Stop (whose path flushes synchronously) revealed
   everything at once. The flush is now armed on BOTH an event-loop task
   (MessageChannel — the same primitive React's scheduler uses) AND an
   animation frame; first to run flushes, the other no-ops; exactly one
   coalescing latch remains. Visibility never requires frame callbacks.
   Evidence: `src/stream-flush-scheduler.test.ts` (8 deterministic tests);
   the real-stack browser harness `e2e/repro-stream.mjs` traces
   socket → fast path → accumulator → flush → DOM.

2. **P0 — live surface shows the truth about reasoning and memory.** While
   a model emits no reasoning stream, the bubble states it factually
   ("Thinking · this model is not exposing a reasoning stream") — never
   fabricated reasoning, never labels presented as model thoughts. A
   backend-truth memory line renders the measured injection evidence from
   the turn's `context` report — `Memory: session summary · 2 recalled
   exchanges`, `… · no recall matches` — and shows NOTHING before that
   report arrives. A block the history windower elided is never claimed.

3. **P0 — memory evidence is composed at the injection site.**
   `contextplan.MemoryEvidence` (summary injected + measured tokens;
   recalled exchanges actually carried; attached history references;
   recall attempted) rides the existing `context` activity on the plan —
   the same plan the UI and reconnect replay already consume. The
   selection policy is unchanged: FAST chat stays cheap; targeted recall
   fires on memory-relevant intent; the rolling session summary stays
   automatic. Evidence: `TestContextActivityCarriesMemoryEvidence`,
   `TestMemoryEvidenceOnContextPlan`, `src/memory-evidence.test.ts`.

4. **P0 — the model describes its actual tools and capabilities.** A
   deterministic capability-intent signal (`taskclassify.SelfDescribe`)
   detects "what tools do you have?" / "what can you do?" / "what model
   are you running?"; the orchestrator injects ONE bounded runtime
   self-model block from the EXISTING authorities — the registry snapshot
   (`ShortDescription()` first), the already-resolved model card,
   config-backed backend facts, the sysinfo fast snapshot, the turn's
   memory plan — with the honest distinctions: registered vs enabled vs
   offered-this-request vs disabled-and-NOT-callable. Capability
   questions stay cheap (no research, no recall, no repo indexing, one
   engine turn). Evidence: `TestClassifySelfDescribeIntent`,
   `TestCapabilityIntentInjectsSelfModel`,
   `TestOrdinaryChatDoesNotInjectSelfModel`, `TestBuildSelfModelCatalog`.

5. **P1 — human-facing logs lose the opaque identity tokens.** `runId=`,
   `runID=`, `session=`, `sessionId=`, `sessionID=` are redacted at the
   ONE central log sink (and in crash-report text); prose, URLs/paths,
   durations, causes and every other diagnostic field survive; internal
   identity (API objects, run state, journals, storage keys) is untouched.
   Evidence: `internal/logging/redact_test.go` (18 cases + idempotence).

6. **P1 — repeated per-turn model-card parsing eliminated.** The model
   capability cache dropped its arbitrary 10-second TTL for identity-based
   caching: the immutable parsed GGUF card is cached under
   (path, size, mtime) indefinitely; config-sensitive fields re-derive
   from the cached card when a configuration fingerprint changes; a
   replaced model file re-parses exactly once. Evidence:
   `internal/llm/modelcaps_cache_v182_test.go`.

7. **P1 — README current-only; history in `changelog.md`.** The README no
   longer carries release-history sections; `changelog.md` is the single
   canonical history artifact.

8. **P1 — engine identity log stages explicit.** Boot-time capability
   probes read `engine boot probe: binary build <tag> …`; the deferred
   commit names the build that became active and says the next boot probes
   it — the `staged → boot probe → committed` sequence is unambiguous.

## Maintenance / update / rollback behavior (current)

* The maintenance gate (identity manifest + committed state) still owns
  engine updates; two-boot idempotency and the corruption matrix are
  unchanged (deterministic, fault-injected).
* A rolled-back engine transaction leaves the RECORDED tag agreeing with
  the restored package (re-recorded from the restored manifest; no
  invented identity). The commit remains the authoritative identity
  record — state, manifest, binary hash all agree after every successful
  update, and the log sequence names which build is serving and which is
  staged.
* The update path still resolves engine variants through the ONE
  authoritative resolver (pinned tag first, else newest release containing
  the asset); unsupported variants are REFUSED, never silently CPU-fallen.
* Update rollback safety, zipsafe installation and effective-tag rules are
  unchanged from v1.7.4–v1.7.5 (plus the v1.8.1 rollback identity repair).
* The model capability cache is in-memory only (bounded, identity-keyed);
  restarting the app re-reads each model's card once. Nothing to migrate.
* The Runtime Governor adds NO persistence, NO new scheduler and NO new
  updater: it is a policy read-model over existing telemetry. Restarting
  the app re-measures; nothing to migrate.

## Evidence-truth statements (standing)

* Deterministic unit / race / integration / E2E / CI / real-engine probe /
  real-host runtime are DISTINCT evidence classes and are never conflated.
* v1.8.2 live-visibility evidence: the browser harness runs the REAL stack
  (server + embedded frontend + native engine) and traces every WebSocket
  frame with timestamps against sampled DOM state; the flush-scheduler
  proofs are deterministic (no sleeps). The WebView2 frame-callback
  failure mode was eliminated by construction (the task boundary does not
  depend on the compositor); a physical Windows host run remains the
  user's own acceptance step — no physical-PC claim is made by CI.
* Build/typecheck ≠ runtime proof; CI ≠ physical-host runtime proof;
  detection ≠ execution.

---

# Historical records (compressed — authoritative detail lives in the tags, their tests, and `changelog.md`)

## v1.8.1 record

The real-Windows local-generation crash repaired at the root (Gemma-class
GGUF metadata exceeded the card parser's 8 MiB bound → nil capability card
deref between `task classified` and `tier selected`; repaired by the
documented nil-card fallback plus the 32 MiB read bound; the first
LOCAL-provider run-level E2E tests shipped with it). Streamed answers
crossed ONE render frame instead of two (`src/stream-fast-path.ts` — the
v1.8.2 dual-boundary flush removed its residual rAF dependency). Engine
rollback re-records the identity from the restored manifest.

## v1.8.0 record

Pause→resume synchronization repaired at the root
(`resumedGenerationEvidence`: strictly newer run sequence AND changed
cumulative snapshot). Honest abort marker end-to-end (the orchestrator
publishes `aborted`, the live state and the registry agree, the
abort-after-resume flake eliminated 15/15). Runtime Governor vertical
slice (resource state, pressure model, envelope, admission, self-model) +
`GET /api/governor` + System Centre card. Documentation consolidated;
version truth through the canonical gate.

## v1.7.6 record

Edit-transaction journals (atomic, integrity-hashed) + startup recovery
converging interrupted edits from durable evidence across ten
crash/failure windows (deterministic fault-injection matrix); corrupt
journals fail closed (quarantined); restart-recovered runs claim the
checkpoint revision; `/api/run/paused` excludes journals/temp files;
token-aware codename gate (every spelling fails, legitimate compound
identifiers pass); generation-aware spec cache (registry generation
carried in cache entries).

## v1.7.5 record

Transactional pause/edit/resume: CAS validation without mutation,
transcript replace, checkpoint commit, transcript rollback on commit
failure, publication strictly last; one control mutex serializing
edit/resume/stop; resume handler owns the fresh generation context (abort
reaches the resuming window); stale session-list responses cannot
resurrect deleted sessions (monotonic generation guard); Windows migration
"restart" test realism; strict numeric engine-variant gates.

## v1.7.4 record

The pause/edit/resume state machine (RUNNING → PAUSING → PAUSED → RESUMING
→ terminal), durable checkpoints, WS snapshot/replay continuity across
pause/resume, double-pause idempotency, stale-revision 409s, stop-after-
pause semantics, one-run-per-session replacement consuming checkpoints.

## v1.7.0–v1.7.3 record

Scheduler RunNow settlement contract (close = persistence + bookkeeping
complete); recovery/handoff durability; engine-lifecycle and stress
hardening.

## v1.7.1 record

Preflight gate: one report over existing authorities, hard
incompatibilities refuse runs BEFORE engine start; LiveMonitor with
hysteresis and synchronous cooperative critical protection (active run
contexts canceled — nothing killed); context-exhaustion recovery (typed
conditions only, exactly one bounded restart); native engine first-class
backend; license/doc cleanup.

## v1.6.x record

Engine-variant provisioning through the authoritative resolver with
Windows-runner asset validation (HEAD-checked reachability ≠ runtime
execution — labeled then and now); stable embedded-frontend asset
contract; custom tools surface; workflow output-name repair.

## v1.5 and earlier (compressed)

Multi-agent context, memory/recall tiers, durable summaries, cross-mode
history, task state, agent.md handoff, continuum rollover, repository
index, Lab workspaces, downloader/installer foundations, environment
centre, streaming replay contracts — each with the deterministic and
integration evidence that shipped in its release.
