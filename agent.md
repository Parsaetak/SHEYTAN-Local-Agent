# SHEYTAN-Local-Agent — Agent Context (CURRENT v1.8.1 handoff)

This is the concise, current handoff for an engineering agent continuing
work on SHEYTAN-LA. It states what IS (verified), what is NOT (future), the
durable invariants, and the surfaces most sensitive to regression. Full
truth: `ARCHITECTURE.md` (architecture), `ROADMAP.md` (future),
`UPDATE.md` (release evidence), `worklog.md` (session log).

**Current release: v1.8.1.** Version identity is exactly `1.8.1`
everywhere (canonical gate: `node scripts/release-version.mjs --check`).

---

## What SHEYTAN is

A Windows-first, local-first AI engineering laboratory — NOT an Ollama/LM
Studio wrapper. One Go runtime owns every authority exactly once; a
React/TypeScript frontend is embedded; managed llama.cpp and a native C++
engine path serve inference behind one backend contract. Motto: "The model
proposes. The tools execute. The laboratory verifies."

## Verified architecture (current)

* **One run lifecycle** — one registry, one hub, one `runLive` per run;
  sequence-stamped activity replay; settlement ordering (outcome recorded
  before the terminal flip; the settlement edge closes AFTER the registry
  record).
* **Pause / edit / resume** — durable atomic checkpoints; transactional
  edits with journals + startup recovery; resume = semantic continuation
  rebuilt from the transcript authority; ONE settlement path
  (`finishSuccessfulRun`) for fresh and resumed runs.
* **v1.8.0 synchronization contract** — after a resume, proof of a live
  resumed generation = strictly newer run sequence AND changed cumulative
  response/reasoning snapshot (`resumedGenerationEvidence`); stale
  cumulative state is never evidence; `waitForRunResponse` is
  initial-generation only (hazard note in source).
* **v1.8.0 abort honesty** — the orchestrator's context-cancelation exits
  publish `aborted` (never `done`); `observe()` folds it; live state and
  outcome registry agree; the frontend consumes the typed marker.
* **v1.8.1 local-generation crash repair** — a GGUF card that cannot be
  read (Gemma-class tokenizer blocks beyond the old 8 MiB bound, or any
  unreadable file) resolves to a nil capability object; every consumer
  follows the documented fallback (configured context + conservative
  estimator) — the orchestrator never dereferences it. The read bound is
  now 32 MiB, so real Gemma-class cards parse and drive the model-aware
  context clamp. FIRST local-provider run-level E2E tests exist in
  `internal/api` (fake llama-server subprocess, real engine contract).
* **v1.8.1 single-frame streaming** — `src/stream-fast-path.ts` folds
  stream-critical WS events into the streaming accumulator at receive
  time (ONE render-frame boundary); the timeline batch skips them via a
  self-draining ledger; done/error/abort still flush synchronously.
* **Runtime Governor** (`internal/governor`) — the ONE runtime POLICY
  authority: resource state (measured + explicit unknowns), sustained +
  rolling pressure signals over the shipped four-level vocabulary,
  envelopes with honest adjustment classes (`live`/`next-run`/`reload`),
  resident-budget admission (unknown evidence → conservative refusal),
  self-model with provenance. It executes NOTHING: the monitor keeps
  critical protection (cooperative cancellation), the engine lifecycle
  owner keeps engines, the scheduler keeps automations, the memory manager
  keeps trimming.
* **`GET /api/governor`** — one composed read model (governor + sysinfo
  hardware + engine metrics + construction-time capabilities); the System
  Centre renders the Runtime Governor card.

## Current evidence (what backs the claims)

* Deterministic unit: governor policy matrix; synchronization contract;
  abort-marker folding; pause/resume family; edit-transaction fault
  windows; llama.cpp CLI contract against real `--help` fixtures.
* Race: governor suite; the CI race gate (api, agent, sessions,
  contextplan, histref, runtime). Abort-after-resume now passes 15/15
  stressed (was ~1-in-6 flaky at v1.7.6 baseline).
* Integration: API contract tests including `/api/governor`; restart
  recovery; cross-mode surfaces.
* CI: Linux headless gates green in the producing environment. The
  Windows pause/resume defect was repaired against the exact CI scenario
  deterministically — **no physical-PC Windows runtime claim is made.**
* Known limitations: CPU policy inert on Windows (live load authority
  doesn't exist there yet — reported as unknown); GPU/NPU surfaces are
  detection-level; CI ≠ physical-host runtime proof.

## Durable engineering invariants (never weaken)

1. One authority per concern (run lifecycle, session store, tool registry,
   scheduler, downloader, installer/update path, engine lifecycle, memory,
   runtime policy).
2. No sleep-based correctness; deterministic tests synchronize on
   authoritative state/events; no "wait longer" polling.
3. Never weaken production behavior to satisfy a test; never delete or
   skip tests to hide failures; fault-injection panics are intentional.
4. Never let an erased/discarded draft enter final history; semantic
   continuation is the honest pause/resume model.
5. Hardware detection ≠ execution evidence; GPU/NPU claims stay separated;
   numeric/unsupported engine arguments fail before spawn; the modern
   llama.cpp CLI contract (b10642/b11205 split cache types, load-mode) is
   verified against fixtures — inspect the exact binary `--help` before
   changing compatibility logic.
6. Never kill processes as resource policy; protection is cooperative.
7. `LICENSE.md` is the ONE license artifact. Keep the codename gate
   enabled and exact. Keep release identity version-only (no codenames).

## Regression-sensitive surfaces (re-check after ANY change)

* Startup/maintenance gate ordering (gate before engine/model prewarm);
  two-boot idempotency; identity fallback/corruption matrix.
* Pause/Edit/Resume: durable checkpoint; revision consistency; edit
  journaling; restart recovery; empty-draft semantics; pause-after-resume;
  stop/abort-after-resume; one authoritative run.
* WebSocket run snapshot/replay and sequence continuity.
* Sessions: stale refresh responses cannot resurrect deleted state.
* Tools: deep-copied metadata under race; generation-aware spec cache.
* Engine lifecycle: no orphaned processes; error normalization.
* Governor: admission stays conservative on unknowns; envelope and
  monitor's protection never disagree about what a level means.

## Next strategic direction

`ROADMAP.md` owns it: v1.8 is the CURRENT release (adaptive runtime
intelligence — this handoff's Governor slice); v1.9 AI System Builder,
v1.10 Universal Agent, v1.11 Learning & Evaluation, v1.12 Advanced Model
Intelligence, v2.0 platform end state are FUTURE — NOT IMPLEMENTED, and
must never be marked complete merely because they are documented.
