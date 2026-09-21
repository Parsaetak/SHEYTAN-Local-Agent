# SHEYTAN-Local-Agent — Agent Context

> Persistent engineering handoff for the next agent working on this repository.

Repository: https://github.com/Parsaetak/SHEYTAN-local-agent

Branch: `main`

<!-- sheytan:handoff:begin -->
# Latest Agent Handoff

## Task
SHEYTAN-LA v1.3.3 (deep repair / native engine / release cleanup): root-fix the v1.3.2 CI failures of Actions run 35552680611 — the Linux settlement race ("agent.md handoff missing"), the Windows native-engine compilation failure in hardware.cpp, and the release-title identity drift — plus a native-engine deep audit and the permanent version-only release identity (tag v1.3.3, title 1.3.3, no prefix/codename/suffix, ever).

## Objective
v1.3.3 must make settlement deterministic (the barrier waits for the run's TERMINAL outcome, never summary completion), make a mandatory-handoff failure unable to masquerade as successful completion, make the Windows native engine compile cleanly, keep native execution real through the application path (no orphans, no wedged state, no silent fallback), and lock the version-only release identity structurally — while ROADMAP.md stays byte-identical (git blob SHA-1 c7e2c1720eb5e97bd932c0d76100b8719193e650, verified before and after).

## Current state
All v1.3.3 work is IMPLEMENTED and VERIFIED on this host: gofmt/vet clean; 46/46 headless Go packages pass (incl. the real C++ host integration suite); race gate passes; settlement tests 20x under -race pass; native Linux clean-room ctest 12/12; full-engine Windows cross-build (host + 12 tests) with zero project warnings; frontend gate fully green (typecheck/lint/78 units/28 release tests/build/verify:web); release metadata synced to 1.3.3 and --check passes; the workflow satisfies the enforced version-only contract.

## Changes made
- Settlement barrier (internal/api): waitForRunSettled / waitForRunSettledFor (runId-keyed, repeated-settlement-safe) / waitForRunOutcome poll the bounded outcome registry — the terminal record the v1.2.9 durable ordering writes only AFTER all durable artifacts; all settlement-waiting tests migrated; newRemoteServer returns the *Server handle.
- Mandatory-handoff honesty (internal/api/server.go): every completed Agent run attempts the handoff (nil task = real failure, not a silent skip); a write failure demotes resultOutcome to "error" with the concrete cause in the terminal caption; reply/summary/live error activity preserved; successful ordering unchanged.
- Windows native root-fix (native/engine/src/hardware.cpp): explicit <vector>/<cstdlib>/<intrin.h>; NOMINMAX + WIN32_LEAN_AND_MEAN guarded before windows.h; (std::min) parenthesized; the three windows.h test files carry the same guards.
- Engine hardening (internal/native/engine/runtime.go): the IPC read loop drops event frames for ABANDONED streams instead of blocking on a full buffer (a >256-frame straggler burst could wedge the connection ahead of the cancelled final frame); backpressure for live consumers unchanged.
- Native quality: 8 compiler warnings eliminated (sampler.cpp kept-counter; test_forward/test_model/test_host/test_generate dead vars/captures; test_generate rms-eps now a real GGUF F32 type-6 value).
- Version-only identity: GitHub Release title is the plain canonical version (name: ${{ env.APP_VERSION }}); publication re-verification asserts tag AND title; release-version.mjs contract gains REQUIRED fragments (version-only title, tag==v${APP_VERSION} gate, tag-from-ref) and BANNED fragments (product+tag title, Zeta, APP_VERSION_FULL); 7 new regression tests (28 total); canonical 1.3.3 synced across package.json/config.go/build-config.yml/SIGNATURE; stale APP_VERSION_FULL doc rows corrected (ARCHITECTURE.md, README.md).

## Files changed
- internal/api/{server.go, runregistry_test.go, run_settlement_test.go, run_settlement_v133_test.go (NEW), runtransport_test.go, run_crossmode_test.go, run_task_test.go, history_api_test.go}
- internal/native/engine/runtime.go
- native/engine/src/{hardware.cpp, sampler.cpp}, native/engine/tests/{test_gguf.cpp, test_model.cpp, test_host.cpp, test_forward.cpp, test_generate.cpp}
- scripts/{release-version.mjs, release-version.test.mjs}, package.json, internal/config/config.go, build/config.yml, SIGNATURE
- .github/workflows/build-desktop.yml
- README.md, UPDATE.md, agent.md, worklog.md

## Tests and verification
- Go: gofmt clean; go vet -tags headless clean; go test -tags headless -count=1 ./internal/... 46/46 PASS; race on api/agent/sessions/contextplan/histref/runtime PASS; settlement 20x -race PASS; native lifecycle 10x PASS.
- Native: Linux clean-room cmake --fresh + build + ctest 12/12; Windows full-engine MinGW-w64 cross-build (host + 12 CTest executables, PE32+, -Wall -Wextra -Wpedantic, ZERO project warnings); the pre-fix hardware.cpp REPRODUCES the CI failure class on the same toolchain and the fixed file compiles clean.
- Frontend: npm ci; typecheck; lint 0/0; test:units 78/78; test:release 28/28; build + sync:web; verify:web --dist satisfied.
- Release: release-version.mjs sync/check/env all consistent at 1.3.3; workflow satisfies the full v1.3.3 contract (live audit test).
- Repository scans: no codename/suffix reference in any active surface; ROADMAP.md blob SHA unchanged.

## Failures / blockers
- None open. Known environmental limits: Wails desktop shell needs GTK4/WebKitGTK (headless tag is the documented verification path); MSVC build/installer/publication execute on the GitHub Windows runner (the Windows native code path verified here via real-SDK cross-build + token-level macro-collision proof).

## Remaining work
- CI Actions rerun from a push of this tree (maintainer-side; tag v1.3.3 triggers the release job with the version-only title).
- Optional future (roadmap, do not start now): native engine packaging into the portable ZIPs.

## Recommended next action
Commit this tree, tag v1.3.3, push; CI runs the full matrix — settlement determinism and handoff honesty on Linux, the now-compilable native engine build + 12 CTest executables + real-host Go integration on Windows, the enforced version-only release identity, packaging, installer and release verification.

## Do not redo
- Do NOT use summary completion as a settlement proxy — the barrier is the run's TERMINAL outcome in the bounded registry (waitForRunSettled / waitForRunSettledFor for sequential runs).
- Do NOT let a mandatory-handoff failure settle "done" — the v1.3.3 honesty demotion (resultOutcome → "error") is pinned by TestMandatoryHandoffFailureCannotMasqueradeAsDone.
- Do NOT rely on transitive includes or unguarded windows.h in the native engine: explicit includes + NOMINMAX + parenthesized (std::min) are the permanent pattern (see hardware.cpp's header comment).
- Do NOT reintroduce a product-prefixed/tag-derived release title, a codename fragment (Zeta) or a second identity variable (APP_VERSION_FULL) — the release-version.mjs workflow contract fails the build on every one.
- Do NOT make the native→llama.cpp fallback silent; do NOT cache native/engine/build (--fresh always); do NOT hand-edit SIGNATURE's version line.
<!-- sheytan:handoff:end -->

Current release: `v1.3.2` — release repair + native engine execution
hardening (canonical cross-platform release-metadata gate, APP_VERSION_FULL
removal, observable engine selection, native execution test contract, Windows
native-execution pipeline stage, contract-driven asset verifier). Prior lines:
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
