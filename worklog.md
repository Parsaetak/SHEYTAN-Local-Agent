# SHEYTAN-Local-Agent — Worklog

## Current State

Date: 2026-09-13 (Phase 6: reliability, coding effectiveness, context intelligence, multi-agent efficiency; phase logs below)

Repository:

```text
https://github.com/Parsaetak/SHEYTAN-local-agent
```

Branch: `main`

Current release:

```text
v1.1.5Z
```

v1.1.5Z is the **SHEYTAN Native AI Engine** release line. Phase 1
established the backend abstraction, the supervised native engine path
and the C++ engine skeleton. Phase 2 added **native GGUF model
loading**: a hardened C++ GGUF reader, memory-mapped model access, real
metadata extraction, load-time memory planning, the model lifecycle and
the `ModelInfo` surface through Go. Phase 3 rebuilt the **local data
pipeline for memory efficiency**: a shared chunk engine with full
provenance metadata, single-flight content caching, streaming attachment
staging, append-aware memory-store caching and allocation-free recall
scoring — all measured with before/after benchmarks. Phase 4 added the
**native engine foundation primitives**: a real GGUF-backed tokenizer
(BPE/Unigram/WPM), a real KV-cache data structure sized from model dims
(GQA-aware, bounded), a real bounded scheduler, real sampling
primitives, and a frame-budget-aware streaming UI with a diagnostic
perf HUD. Phase 5 (this log, first below) implemented the **REAL native
transformer inference path**: the llama-architecture forward pass
(RMSNorm / RoPE / GQA causal attention over a true fp16 KV cache /
SwiGLU / logits), the sampler consuming REAL logits, REAL token-by-token
generation with coarse-grained streamed chunks over the IPC protocol,
REAL cooperative cancellation, measured generation metrics, the
load-time llama-graph capability verdict, and the backend selection
router wired through the orchestrator (native when selected AND capable
AND plain-text; llama.cpp otherwise with a logged, inspectable reason).
Native generation support is deliberately narrow and honest: llama
architecture only, F32/F16/Q4_0/Q4_1/Q5_0/Q5_1/Q8_0 tensor types only,
and the current forward pass is portable scalar C++ measured SLOWER
than llama.cpp (the numbers are below — no native-speed claim is
made). llama.cpp remains fully functional as the fallback (and the
default generation engine for everything the native path does not
support). Full phase logs below.

---

# v1.1.5Z Phase 6 Implementation Log (2026-09-13)

Phase 6 is the RELIABILITY / EFFECTIVENESS pass: the strategic product
target is that a small local model plus SHEYTAN accomplishes work that
normally needs a substantially larger model or a cloud agent. Four P0
systems were implemented, each with tests through the real boundaries.

## 0. CI defect fix (run 34703102794, Linux job)

The two failing tests (`TestRealCppHostPhase5EndToEndGeneration` —
prompt tokens 7 vs want 5; `TestRealCppHostPhase5ContextOverflow` —
7+59 > 64) were root-caused to a **dangling prompt pointer** in
`native/engine/src/host_main.cpp op_generate`: `lane->opts.prompt`
pointed into a stack-local `std::string` that died when `op_generate`
returned; the generation thread then read freed SSO bytes — a
scheduling race (locally the lane thread won; on the 2-core CI runner
the dispatch loop reused the stack slot first, the corrupted prompt
lost its BPE merges, and "hello" tokenized as 7 instead of 5).

Fixes (ownership, not timing):
- `GenLane` now owns `std::string prompt` (copied before `c_str()`).
- Lane lifecycle: `finished` atomic + `sweep_finished_lanes()` in the
  host loop — completed lanes are joined and retired, so `kMaxLanes=16`
  bounds CONCURRENT work, not lifetime totals (previously the host
  rejected ALL generations after 16 total).
- `test_host.cpp` silent-failure bug: `main()` checked `failures > 0`
  BEFORE the phase5 tests ran (CI was green while they failed); the
  gate moved to the end. The streaming harness broke on the first
  non-chunk frame (the load_model response); now it breaks on the
  generate terminal frame.
- Regression tests: C++ burst prompt-ownership (6 pipelined distinct
  prompts) + lane recycling (20 sequential generations); Go
  `TestRealCppHostPhase5PromptOwnership` +
  `TestRealCppHostPhase5LaneRecycling` through the real host binary.
  Proof: reverting only the host_main.cpp fix reproduces the exact CI
  signature deterministically.

## 1. P0 #1 — Agent reliability (internal/agent/reliability.go, verification.go)

- **FailureClassification**: 14 categories (knowledge, reasoning,
  planning, tool_selection, tool_arguments, execution, filesystem,
  process, network, model_capability, context, verification,
  environment, state, unknown). Every tool error is classified from its
  evidence, and a category-specific `RepairHint` is appended to the
  tool result — the model re-plans against the DIAGNOSIS, never retries
  blind. Classification is surfaced as a "failure" activity and
  tallied on RunResult.FailureTally.
- **LoopGuard**: repeat detection by call signature (tool + normalized
  args — whitespace-insensitive so reformatting cannot reset the
  counter). First repeat: strategy-change warning. Third: the exact
  call is refused with an instruction to produce the final answer.
  Run-level budgets: 200 tool calls, 30 min wall clock. Blocked calls
  are not counted — counters track EXECUTIONS (CallStats match what
  actually ran).
- **Verification as a first-class system**: `RunResult.Verification`
  reports `verified / partially_verified / failed / not_verified` from
  an `EvidenceCollector` watching tool traffic (lab verify actions,
  explicit build/test outcomes). Unknown tools and ambiguous output
  contribute nothing; a completion claim with no evidence reports
  `not_verified` and the timeline says so. Never again does model
  prose serve as proof of success.

## 2. P0 #2 — Coding effectiveness: safe anchored edits (internal/lab/safeedit.go)

The Coding Lab gained `read_file` (bounded line-numbered views with
offset/limit, binary sniffing, 2000-line/256KB caps) and `edit_file`
(the safe-edit primitive): the anchor (oldText) must occur EXACTLY
ONCE in the file — zero matches is STALE (the file changed since the
last read; the model must re-read), two or more is AMBIGUOUS (more
surrounding lines required). Writes are atomic (same-dir temp + fsync
+ rename), re-read and byte-verified before success, CRLF-normalized,
and every edit invalidates the task's verification state. The edit
success message explicitly states the edit is NOT verified — verify
must run. Small local models no longer corrupt files through shell
quoting accidents; the coding loop is read_file → edit_file → verify.

## 3. P0 #3 — Context intelligence: persistent project intelligence (internal/projectintel)

A per-project measured-facts store (`<DataDir>/projectintel/`, one JSON
per project root): languages (extension census), build system +
inferred commands (go.mod/package.json/Cargo.toml/CMakeLists/Makefile),
layout, entry hints, conventions, and lessons (bounded dedup FIFO,
24 x 200 chars). The Lab records VERIFIED build/test commands against
the task's source root — a verified command is never clobbered by
re-observation. The compact `Card()` (<= 4KB) is injected before the
last user message (cache-friendly position, measured into the context
plan like recall blocks): the model starts the task already knowing
the project instead of re-discovering it every session.

## 4. P0 #4 — Multi-agent efficiency + honest model status

- The multi-agent critic loop previously read the raw `m.maxIter`
  field instead of the normalized local — with `maxIter < 1` the critic
  NEVER ran (fixed; regression test added). The executor now runs via
  `RunDetailed`, so the critic receives the run's measured
  verification verdict ("Objective verification: ...") and the critic
  prompt forbids `"satisfied": true` without objective evidence for
  verifiable tasks. All inter-agent hand-offs are bounded excerpts
  (6KB, rune-safe, marked) — low-end hardware: one small model,
  sequential phases, compact artifacts.
- `/api/models` now reports `backend` (which engine serves
  generation), `servingPath`, and per-model `serving: true` for the
  model the ACTIVE backend is serving; the settings model list marks
  it "currently serving (native engine | llama.cpp)". Discovered /
  loaded / serving are now honestly distinguishable in the UI.

## Verification performed (Phase 6)

- `node scripts/release-version.mjs --check`: consistent.
- `npm ci && npm run typecheck && npm run lint && npm run build`: pass
  (0 warnings, 0 errors).
- `go test ./internal/... -tags headless -count=1`: 25 packages ok.
- `go test -race` on agent/llm/api/native/engine + lab/multiagent/
  projectintel: pass.
- `go vet -tags headless ./...`: clean.
- cmake + ctest (native engine): 12/12 PASS with the fixed host.
- Real Go<->C++ integration tests: ALL PASS, including both original
  CI failures and both new regression tests.
- `go run ./scripts/stress-main stress`: 30 pass / 0 fail.
- Non-headless `go test ./...`: gtk4/webkitgtk system libs absent in
  the dev sandbox — verified PRE-EXISTING at HEAD (same failures on
  the untouched tree); CI runners have the libraries.

---

# v1.1.5Z Validation & Repair Log (2026-09-12, full functional verification pass)

Scope: continue past the Actions-run-34546418321 repair (ad34c74) through
every previously-skipped validation stage, fixing only real defects
discovered on the way. The live tree re-verified every claim of the
2026-09-11 repair log independently — all reproduced clean (see evidence).

## Real defects found and fixed (this pass)

1. **The engine toggle never loaded a model natively** (`internal/api`
   handleLlama "start"): the toggle started the native host (async,
   30 s) but nothing ever called the native `LoadModel` — the ONLY
   production caller was the launch prewarm (`prewarmNative`). A
   native-selected user who disabled auto-start and pressed
   "engine start" got an alive-but-generation-incapable engine whose
   runs still gated on llama.cpp — and on a native-only deployment (no
   llama.cpp binary) the toggle returned 500 while the engine it had
   just started could have served. This is the exact "a button is not a
   feature" defect class the repository forbids.
   Fix: extracted the bounded start+load+verdict sequence into
   `Stack.EnsureNativeReady(ctx)` (single implementation, still used by
   the launch prewarm); the toggle calls it synchronously (bounded 60 s,
   matching the toggle's existing synchronous `llama.Start` contract),
   then keeps llama.cpp's result authoritative ONLY when native is not
   actually serving — a failed llama fallback no longer fails the toggle
   when native serves (the same ordering the Phase 5 run-gate repair
   applies to `EnsureLLM`/`EnsureLLMContext`).
   Pinned by the new HTTP-level regression test
   `TestEngineToggleNativeServesGeneration` (toggle → honest native badge
   → plain-text run → REAL native reply persisted, on a native-only
   deployment; it failed before the fix and passes after).

2. **The llama.cpp auto-download could never succeed on Linux any more**
   (`internal/llm/llama.go`): `llamaDownloadURL` hardcoded the pinned tag
   (b10642), but llama.cpp REMOVED its prebuilt Linux binaries upstream —
   `llama-b10642-bin-ubuntu-x64.zip` now 404s permanently while the
   Windows asset still exists (verified against the upstream release
   asset list). Every Linux engine start with a missing binary produced
   a confusing 404 download failure forever. Fix: the engine download
   path now follows the updater's existing design — probe the pinned tag
   (Windows unaffected), scan the newest releases for the first tag that
   actually carries this platform's asset (bounded), and when none
   exists fail with an honest, actionable error ("build llama-server
   from source and set llamaBinPath, or select the native engine")
   instead of a bare 404. The recorded engine tag is now the ACTUAL
   source tag (`llamaDownloadURL` returns it; `RecordEngineTag` stores
   it). Linux self-heals automatically if upstream ever ships Linux
   binaries again.

3. **Supporting fixture** (`native/engine/tests/fixtures/
   tiny-llama-app.gguf`, +85280 bytes, generated by the repository's own
   deterministic `make_fixture.py`): the F32 fixture dims (greedy decode
   deterministically runs to max_tokens, pinned by `test_generate`) with
   a 2048-token context so a REAL application prompt (system briefing +
   user turn) fits the native engine's honest context-reject bound. The
   generator was extended (app-fixture block in `main()`); regeneration
   is byte-identical for all pre-existing fixtures.

## Documentation truth fixes

- README `engineBackend` row still carried Phase-1-era text ("generation
  still served by llama.cpp until later phases") contradicting the same
  file's Phase 5 truth table — updated.
- README engine-download section now documents the Linux reality
  (upstream ships no Linux prebuilt binaries; options: build from source,
  set `llamaBinPath`, or select the native engine).

## Runtime verification performed (this pass)

- `node scripts/release-version.mjs --check` — all four surfaces
  consistent at 1.1.5-zeta. `npm ci`, typecheck, lint (0 warnings),
  build + sync; `web/static/index.html` contains the root div and
  `diff -r dist web/static` is empty.
- `go build -tags headless ./...` PASS; `go vet -tags headless ./...` PASS.
- `go test -tags headless ./internal/... -count=1` — 27 packages PASS.
- `go test -race -tags headless` on agent/llm/api/native-engine — PASS.
- `go test ./... -run Test -count=1` (default tags) — 23 packages PASS;
  the root desktop package + internal/desktop cannot COMPILE in this
  environment (no GTK4/WebKitGTK dev libs, no sudo) — recorded as the
  documented environment limitation; CI's Linux job installs them and
  runs the full suite, and `GOOS=windows CGO_ENABLED=0` cross-compile
  of the desktop package PASS.
- C++: cmake configure + build (42 targets) + `ctest` — 12/12 suites
  PASS. Real-host Go↔C++ integration: 10/10 `TestRealCppHost*` PASS.
- Stress suite: 30 pass / 0 fail.
- **Real-model native generation (REAL weights, not fixtures)**:
  TinyLlama-1.1B-Chat Q8_0 (llama arch, untied output) driven through
  the Go↔C++ boundary — load verdict generationCapable=true, 20 real
  prompt tokens, 8 tokens generated greedily (finish=length),
  coarse-grained streamed chunks, measured prefill 155.9 s, decode
  54.5 s (0.15 tok/s), kv_positions 27. This is ~1000× larger than the
  fixtures and confirms the full tokenize → embed → forward → logits →
  sample → KV path on real quantized weights. The same model at Q4_0
  (output.weight Q6_K) correctly reported generationCapable=false with
  reason "tensor output.weight has unsupported type" — the honest
  capability verdict working against a real unsupported model.
- **Real application smoke (headless serve, real C++ host, real GGUF)**:
  launch → HTTP surface up → native selected+available → engine toggle
  start → 200 on a native-only deployment (no llama.cpp) → effective
  backend native with the honest badge (state from the native engine) →
  model visible with real GGUF metadata → session created → prompt →
  REAL native generation streamed over the activity WebSocket
  (coalesced response frames; perf activity "≈485 tok/s, first token
  1.2 s") → assistant reply persisted → busy during generation → abort
  mid-run → engine recovered to ready (not stuck busy) → next prompt
  completed → session usable → SIGTERM shutdown clean. 16/16 checks.

## Known limitations (unchanged, recorded honestly)

- Wails/GTK desktop compilation and Windows GUI execution remain
  CI-verified paths only (this environment has no GTK4/WebKitGTK and no
  sudo; Windows runtime is exercised by the CI build + console smoke).
- llama.cpp fallback with a REAL server binary was not exercisable here
  (no Linux prebuilt exists; see repair #2). The fallback seam is
  pinned by the router/contract tests and by the native engine's
  pre-first-token fallback behavior observed in the smoke.
- Native forward pass remains portable scalar C++ (slow on real models:
  measured above); SIMD/thread-parallel optimization is agent.md §13
  task 1 and untouched by this pass.

---

# v1.1.5Z Phase 5 Repair Log (2026-09-11, post-phase5 CI failure)

## Root failure (GitHub Actions run 34546418321)

`Source & frontend audit → Verify repository shape and release identity`
failed with `[sheytan-release] ERROR: unable to read build/config.yml:
ENOENT`. Root cause: the v1.1.5Z-phase5 commit (eedb766) DELETED
`build/config.yml` (52 lines removed) although the phase 5 replacement
manifest itself declares "deleted: none in Phase 5" and lists the file
as committed Wails source. The deletion was accidental, not intentional.

## Additional accidental deletions discovered in the same commit

The same commit also deleted four INTERNAL PACKAGES that the surviving
code still imports — the tree at eedb766 did not compile at all (CI
never reached a Go step to expose it):

- `internal/sessions`  (imported by runtime, api, recall, continuum, cmd)
- `internal/sandbox`   (imported by runtime, cmd/stress)
- `internal/attachments` (imported by runtime, api)
- `internal/memory`    (imported by runtime, multiagent, cmd)

All four were restored byte-identical from the Phase 4 baseline
(84324b8) — exactly what the phase 5 manifest's "unchanged" status
promised. After restoration the tree builds and all packages test green
again.

## Real defects found and fixed during continued validation

1. **Misleading engine state when the native backend serves**: the
   `/api/engine` top-level `state` (the single UI badge source) always
   reported the llama.cpp state machine, so a user with
   `engineBackend: native` whose llama.cpp fallback could not start
   (offline / no binary) saw "failed" while native generation actually
   worked. Fix: when the effective serving backend is native (same
   selection policy that routes generation), state/detail/pid/
   loadedPath/logs come from the native engine. Purely local reads; the
   llama path is byte-unchanged.
2. **Run gate required llama.cpp even when native serves**:
   `EnsureLLM`/`EnsureLLMContext` always gated runs on
   `Llama.Start()`, so native-only deployments could never execute a
   run. Fix: the same native-serving early exit (selected + actually
   generation-capable). The fallback gate still applies whenever native
   is not serving.
3. **CI never executed the Go↔C++ integration tests**: the C++ engine
   was built only in the audit job (no Go steps); build-linux/build-windows
   re-checkout fresh trees without `native/engine/build`, so every
   `TestRealCppHost*` test SKIPPED in all Go jobs. Fix: build-linux now
   builds + ctests the C++ engine before `go test`, so the real-host
   e2e suites actually run in CI.
4. **Flaky unload-guard test** (`native/engine/tests/test_generate.cpp`,
   "model unload during active generation → rejected"): the sequencer
   thread polls `active_requests > 0` then asserts unload is rejected
   — but the generation used temperature 1.1 sampling, with which the
   toy model can emit EOS within the first few tokens, completing the
   generation before the sequencer's unload lands (observed ~2/8 runs
   failing under load, zero engine defect behind it — the test's own
   "deterministic order — no race" comment was wrong). Fixed by
   switching the sub-test to greedy decode, which on this fixture
   deterministically runs to max_tokens (verified: finish reason
   "length", 200/200 tokens). Post-fix: 10/10 ctest runs green.
5. **Stale Phase-1/Phase-4 documentation contradicting the live
   implementation** (the §25 audit): README claimed native inference
   "NOT implemented — every generation request therefore runs on
   llama.cpp" and "NO forward pass yet"; config.go / engine.go /
   runtime.go carried Phase-1 "generation is NOT implemented yet"
   comments. All updated to the verified Phase 5 truth (implemented +
   narrow + honest limits).

## Runtime verification performed (this repair)

- `node scripts/release-version.mjs --check` — all four surfaces
  consistent at 1.1.5-zeta (build/config.yml restored).
- `go build -tags headless ./...` PASS; `go vet -tags headless ./...` PASS.
- `go test -tags headless ./internal/... -count=1` — 27 packages PASS
  (including the restored sessions/attachments/memory and the
  releasegate critical-package gate).
- `go test -race -tags headless` on agent/llm/api/native-engine — PASS.
- C++: cmake configure + build + `ctest` — 12/12 suites PASS
  (engine, protocol, host, gguf, model, tokenizer, kv_cache,
  scheduler, sampler, tensor, forward, generate).
- Real-host Go integration tests re-run WITH the built host present:
  10/10 `TestRealCppHost*` PASS (lifecycle, model, tokenizer, KV,
  scheduler, phase5 generation/cancel/context-overflow/unsupported/
  backend contract).
- Stress suite: 30 pass / 0 fail.
- Frontend: npm ci, typecheck, lint (0 warnings), build, sync into
  web/static; `web/static/index.html` contains the root div and
  `diff -r dist web/static` is empty.
- **Real application smoke test (headless build, real C++ host, real
  GGUF fixture)**: launch → native engine ready (honest state after the
  fix) → model visible with real GGUF metadata → native load with
  capability verdict → prompt submitted → REAL native generation
  streamed over the WebSocket (3 coalesced response frames; perf
  activity reports measured 493.7 tok/s, 1.2 s TTFT) → assistant
  message persisted → abort mid-run ("Aborted by user", engine
  recovered to ready) → next request succeeds → SIGTERM shutdown clean
  (host subprocess reaped). The context-overflow REJECT policy was also
  exercised for real (over-long prompt → explicit engine-side rejection
  → logged fallback attempt).

## Environment-verified vs unverified (honest list)

- Verified here (Linux, Go 1.26.0, Node 24, CMake 4.4.3, g++ 14): all
  of the above.
- NOT verifiable in this environment: the Wails/GTK GUI desktop build
  (no GTK4/WebKitGTK-6.0 dev libraries, no root) — the Windows CI job's
  CGO-free cross-compile remains the verification path, unchanged.
- Windows runtime execution: unverified here (Linux-only environment);
  CI covers build + console probe + package integrity.

---

# v1.1.5Z Phase 5 Implementation Log (2026-09-11)

## What was implemented (REAL native transformer inference + generation)

1. **KV cache correction first** (the Phase 4 defect): the cache was a
   `unique_ptr<float[]>` while documenting/reporting fp16 — the actual
   allocation was 2x the reported `capacity_bytes`, and per-layer
   pointer math used `sizeof(float)` against f16-byte counts so
   adjacent layers overlapped. Rewritten as TRUE `uint16_t` fp16-bit
   storage (software round-to-nearest-even conversion in `fp16.h`,
   bit-exact vs numpy across 65k random values + boundary cases):
   `capacity_bytes` IS the allocation size, layer offsets are
   element-exact, `used_bytes` equals positions × per-position
   K+V footprint, `reset()` clears counters only (no memset),
   allocation bounds-checked (context cap 1M, total 16 GiB, RAM check).
   Regression tests pin expected == allocation == reported.
2. **Tensor access layer** (`src/tensor.*`): lookup, type/shape/
   byte-range validation, row dequantization for EXACTLY
   F32/F16/Q4_0/Q4_1/Q5_0/Q5_1/Q8_0 (per-row block padding per the GGML
   nbytes formula; block tail discarded). Everything else fails with an
   explicit unsupported error — never a reinterpretation.
3. **Llama graph derivation + validation** (`src/llama.*`): hyper
   parameters from real GGUF metadata — required keys must exist (both
   historical rms_eps spellings accepted; disagreement fails closed as
   ambiguous; rope.freq_base required, no default invented);
   head_dim from attention.key_length or the documented emb/heads
   derivation; GQA divisibility checked; the full tensor set
   (token_embd, output [tied or untied], output_norm, per-layer
   attn_norm/ffn_norm/attn_q/k/v/output/ffn_gate/up/down) validated for
   presence/shape/type at LOAD time → `generationCapable` + reason in
   model_info (metadata-level; nothing allocated).
4. **Transformer forward pass** (`src/forward.*`): the actual llama
   computation — embeddings → per-layer [RMSNorm → Q/K/V matvec →
   RoPE (NORM pairing, freq base from metadata) → causal GQA attention
   reading the fp16 KV cache → output projection + residual → RMSNorm
   → SwiGLU FFN + residual] → final norm → logits. Double
   accumulators; scratch reused (no per-token heap allocations); norm
   weights dequantized once per model binding; per-token (not
   per-layer) KV position advance.
5. **Generation runner** (`src/generate.*`): prompt encoded by the
   engine's own GGUF tokenizer (BOS-primed, llama-style ▁ handling);
   context bound is a REJECT policy (prompt + max_tokens > context →
   explicit error, no silent truncation); per-request KV reset (no
   state leaks between requests); prefill (cancellation observed every
   16 tokens) + decode loop (cancellation observed EVERY token);
   sampler consumes the REAL logits (temperature/top-k/top-p/
   repetition-penalty/seed — Phase 4 primitives); stop conditions: EOS
   (sampled EOS is never emitted into the text), max_tokens, context
   exhaustion, cancellation, error; UTF-8-complete chunk emission
   (first token immediate for honest TTFT, then ≥8-token/24-byte
   windows — never one IPC frame per token); metrics from the
   monotonic clock only (prompt/decode/TTFT/tok/s/KV positions; zero
   = not measured).
6. **Scheduler worker thread** (`src/scheduler.*`): single slot (max
   concurrent 1), FIFO queue with cap, real active/completed/cancelled/
   failed counters; queued cancels remove, active cancels set the
   cooperative flag; graceful shutdown cancels + joins. Without a
   worker the Phase 4 semantics are preserved (tests unchanged).
7. **Engine ABI v4** (`engine.cpp`): `shtn_engine_generate` (blocking
   submit to the scheduler; work registry by request id; the emit
   callback fires per streamed chunk), `shtn_engine_cancel_generation`,
   `shtn_engine_generation_stats`; unload/load rejected while a
   generation holds the mapping (SHTN_ERR_MODEL_STATE); metrics report
   the real scheduler active count; new error codes -9 CANCELLED,
   -10 CONTEXT_OVERFLOW, -11 GENERATION.
8. **Host generation lanes** (`host_main.cpp`): the `generate` op
   spawns a bounded lane (≤16; the engine's queue bounds acceptance
   anyway) that calls the ABI and writes event frames
   (`{"id":N,"ok":true,"event":"chunk","result":{requestId,text,token,
   final}}`) followed by one final frame (finish reason + measured
   metrics, or a bounded error frame). All frame writes share one mutex
   so event frames never interleave mid-frame with dispatch responses.
   `cancel` is real. EOF/shutdown cancels in-flight lanes and joins
   them BEFORE exit (no writes to dead streams). The dispatch loop
   stays fully responsive during generation.
9. **Go streaming IPC** (`internal/native/engine`): protocol v4;
   `ipcConn.streamCall` — event channel per pending request (never
   closed; late stragglers absorb into the bounded buffer), consumer
   goroutine forwarding to the callback with backpressure, a
   deliver-then-drain abort protocol, and a final-frame guarantee
   (every buffered event is delivered before the call returns).
   `Engine.StreamGeneration` (busy-state cycling in the existing
   vocabulary, 5-minute zero-progress stall watchdog mirroring the
   llama stream contract, ContextExhaustedError mapping).
10. **Backend + routing** (`backend.go`, `internal/runtime`, `internal/
    agent`): `Generate`/`StreamGenerate` real (llm.StreamEvent with
    finish reason + measured usage; PerfStats from measured metrics);
    `GenerationCapable` = alive + loaded + validated; tools/images
    requests return `llm.ErrNotImplemented` (the explicit fallback
    signal); the orchestrator gained a `GenerationStream` seam wired to
    `Stack.streamGeneration` — native when selected + capable +
    plain-text, llama.cpp otherwise, pre-first-token native failures
    fall back with the reason logged; pre-warm loads the selected model
    natively and logs the capability verdict.
11. **Fixtures + independent reference** (`tests/reference/
    make_fixture.py`): deterministic tiny llama GGUFs (F32; llama.cpp-
    loadable — verified with llama-bench; both rms_eps key spellings
    now the canonical one) + a numpy reference implementation computing
    the staged values and final logits WITHOUT the C++ code, including
    the exact fp16 K/V round-trip semantics.
12. **Prompt format note (honest limitation)**: the native path renders
    messages with a plain role-labeled format ("System:/User:/Assistant:")
    — it does NOT interpret the model's chat template. llama.cpp keeps
    full template fidelity; the router only sends plain-text requests
    natively.

## Runtime verification performed (Phase 5)

- ctest: 12/12 suites green — engine, protocol, host (streaming
  generate + cancel through real OS pipes), gguf, model, tokenizer,
  kv_cache (byte-accounting regressions), scheduler, sampler, tensor
  (hand-computed dequant values), forward (independent Python
  reference: staged values within 1e-5, logits within 1e-3, greedy
  argmax matches), generate (greedy determinism, seed reproducibility,
  EOS/max-tokens/context/cancel, metrics sanity, KV reset between
  requests, non-llama fallback, unload guard, engine reusability after
  every failure).
- Go: 23 packages `go test -tags headless` green; `-race` green on
  agent/llm/api/native-engine/runtime; `go vet` clean.
- Real Go↔C++ boundary: 5 e2e tests (generation + streamed chunks +
  measured metrics + KV/scheduler accounting; mid-decode cancellation;
  context-overflow rejection + reuse; unsupported-model fallback
  signal; backend contract incl. llm.StreamEvent/PerfStats).
- Frontend gates: typecheck, lint, build green (frontend untouched —
  the Phase 4 coalescing contract is intact by construction).
- Stress gate 30/0; release-version check consistent (NO version bump —
  §34 discipline: the release workflow decides version changes).

## Measured performance (same fixtures, single thread, no claims beyond these numbers)

| fixture | engine | prompt (pp5) | decode (tg) |
|---|---|---|---|
| tiny-llama-f32 (85KB) | llama.cpp (llama-bench @df03399) | 56810 tok/s | 28643 tok/s |
| tiny-llama-f32 | NATIVE (this phase) | 17695 tok/s | 20708 tok/s |
| tiny-llama-slow (5.7MB) | llama.cpp | 5888 tok/s | 2656 tok/s |
| tiny-llama-slow | NATIVE | 285 tok/s | 611 tok/s |

Native model load: 301–437µs (fixtures); TTFT 0.3ms (tiny) / 17.6ms
(slow); KV accounting measured exactly (8192B cap / 4352B used / 34
positions on the tiny fixture). Host RSS: idle 2044KB, +144KB after
load+generate (tiny fixture).

**Honest conclusion: the native engine is SLOWER than llama.cpp on
these fixtures** (pp ~3.2x, tg ~1.4x on the tiny; pp ~20x, tg ~4.3x on
the slow). Expected — llama.cpp has heavily optimized kernels (SIMD,
fused quantized compute, layout-aware access) while Phase 5 is portable
scalar dequant-then-dot C++ with correctness as the priority. The
numerical correctness is pinned by the Python reference; optimization
is future work (agent.md §13 task 1).

## Known limitations (Phase 5, read literally)

- Native generation: llama architecture ONLY; F32/F16/Q4_0/Q4_1/Q5_0/
  Q5_1/Q8_0 tensors ONLY; rope.freq_scale 1.0 ONLY; plain prompt
  format (no chat-template interpretation); tools/images stay on
  llama.cpp.
- Native forward pass is scalar C++ — slower than llama.cpp (numbers
  above); performance optimization is future work.
- shtn-engine-host is not shipped/auto-downloaded yet (build from
  native/engine; packaging is agent.md §13 task 3).
- Windows: the numerical engine is platform-neutral C++ (no
  platform-specific inference code added); Windows RUNTIME execution
  was not exercised in this environment (Linux-only here; Windows CI
  cross-build remains the verification path).
- The Wails/GTK desktop build still cannot compile in this environment
  (no GTK4/WebKit dev libs); Windows CI job builds it.
- Vision (mmproj) path not exercised with a real projector model.
- Small instruct models may not emit formal tool calls; loop mechanics
  covered by deterministic tests.
- The API is loopback-only without an auth token — the OS user account
  is the trust boundary.
- The Lab command policy is lexical (denylists + env pinning), not a
  kernel sandbox.
- Agent tool calls execute sequentially by design.

---

# v1.1.5Z Phase 4 Implementation Log (2026-09-10)

## Goal

Phase 4 turns the native engine from "GGUF loader + metadata + memory plan"
into a real foundation for native inference: a working tokenizer, a real
KV-cache data structure, a bounded scheduler, real sampling primitives, and
a smooth frame-budget-aware application UI. The transformer forward pass
remains a later phase — Phase 4 implements the foundation primitives that
the future inference loop will call, NOT the forward pass itself.

```text
Phase 2:   GGUF loader + metadata + memory plan
Phase 4:   + REAL tokenizer (BPE/Unigram/WPM)
           + REAL KV cache (sized from model dims, GQA-aware, bounded)
           + REAL bounded scheduler (single-slot, cancel, drain)
           + REAL sampling primitives (greedy/temperature/top-k/top-p/
             repetition penalty, seedable RNG)
           + streaming UI coalescing (rAF-boundary batched updates)
           + frame-budget diagnostic HUD
Phase 5+:  transformer forward pass → real native inference (NOT YET)
```

## What was implemented (REAL — verified by tests)

### C++ native engine (new files)

- `native/engine/src/tokenizer.h` + `tokenizer.cpp` — real GGUF tokenizer:
  reads `tokenizer.ggml.tokens` / `token_type` / `scores` / `merges` arrays
  by re-walking the memory-mapped file (the first-pass parser skips array
  element bytes for hostile-input safety; this materializes them on demand
  only when the host asks). Supports BPE (Llama-style with U+2581 space
  marker, and gpt2-style), Unigram (greedy longest-match — a faithful
  simplification of SentencePiece Viterbi, documented as such, NOT claimed
  as a full lattice), and WPM (BERT-style WordPiece with `##` continuation
  marker). Special token resolution (BOS/EOS/UNK/PAD/SEP/EOT) from GGUF
  scalars. Encode is deterministic, bounded by `max_tokens`; decode is
  bounded by `max_bytes`. Unknown tokenizer model kinds return
  `SHTN_ERR_UNSUPPORTED` honestly — the host surfaces that and the
  llama.cpp fallback remains the generation backend.
- `native/engine/src/kv_cache.h` + `kv_cache.cpp` — real KV-cache data
  structure sized from real model dimensions (layer_count, embedding_length,
  head_count, head_dim, kv_head_count for GQA, context_length). One
  contiguous allocation holding per-layer K then per-layer V; layer-major,
  position-contiguous. Capacity bytes is the REAL allocation:
  `2 (K+V) * layers * ctx * kv_dim * 2 (f16 bytes)`. Used bytes is
  proportional to `used_positions` — but `used_positions` is honestly 0
  until a forward pass exists (no fabricated utilization). Bounded by
  `kMaxContextLength` (1M positions), `kMaxKVBytes` (16 GiB) and the
  available-RAM check (the host passes its measured free RAM; the cache
  refuses an allocation that would not fit). Move-only, RAII, idempotent
  release. `reset()` marks positions unused without zeroing buffers (the
  future forward pass will overwrite them; zeroing would waste work).
- `native/engine/src/scheduler.h` + `scheduler.cpp` — real bounded
  scheduler. Single-slot execution (`kMaxConcurrent = 1`) — no speculative
  continuous batching (the execution path cannot support it correctly yet).
  Bounded queue (default 8, hard cap 64). FIFO ordering. Real cancellation
  (queued requests removed and marked cancelled; the future active-request
  cancel hook is wired but no worker thread runs in Phase 4). Graceful
  shutdown drains the queue with "shutting down" cancellations. Counts are
  measured (queued, totals since create); `active` is honestly 0 in Phase
  4. No busy polling (waits on a `condition_variable`).
- `native/engine/src/sampler.h` + `sampler.cpp` — real sampling primitives.
  Greedy (argmax) for `temperature == 0`. Temperature scaling. Top-k
  filtering (`nth_element` partial sort, O(n) average). Top-p (nucleus)
  filtering with cumulative softmax walk. Repetition penalty (CTRL
  formulation: `if logit > 0: divide; else: multiply`). Seedable
  deterministic RNG (xorshift64* with SplitMix64 seed scrambling — not
  `std::mt19937` to keep the binary lean and the sequence reproducible).
  Deterministic: same `(logits, recent_tokens, config, rng)` → same token.
  NULL/empty logits return `SHTN_ERR_INVALID_ARG` (never crash).

### C++ ABI / protocol extension (additive — v2 → v3)

- `include/shtn/engine.h` — Phase 4 surface declarations: `shtn_engine_tokenizer_init`,
  `shtn_engine_tokenizer_info`, `shtn_engine_tokenizer_encode`,
  `shtn_engine_tokenizer_decode`, `shtn_engine_kv_cache_info`,
  `shtn_engine_scheduler_info`.
- `include/shtn/types.h` — Phase 4 ABI structs: `shtn_tokenizer_info`,
  `shtn_kv_cache_info`, `shtn_scheduler_info`, `shtn_encode_options`,
  `shtn_encode_result`, `shtn_decode_options`, `shtn_decode_result`.
- `include/shtn/version.h` — `SHTN_ABI_VERSION` and `SHTN_PROTOCOL_VERSION`
  bumped 2 → 3 (additive — a v2-era host can still build against this
  header by ignoring the new functions; the wire protocol adds new ops but
  does not change existing op shapes).
- `src/engine.cpp` — owns a `shtn::kv::Cache` and a `shtn::sched::Scheduler`
  per engine instance; implements the new ABI functions; the KV cache is
  NOT auto-allocated on model load (it reports the honest zero-state until
  a future op explicitly allocates it).
- `src/host_main.cpp` — dispatches the new ops (`tokenizer_init`,
  `tokenizer_info`, `tokenizer_encode`, `tokenizer_decode`,
  `kv_cache_info`, `scheduler_info`); every op round-trips one frame in,
  one frame out (coarse-grained — no per-token IPC chatter).
- `src/json.h` + `json.cpp` — added `extract_encode_payload` and
  `extract_decode_payload` helpers (mirroring `extract_load_payload`'s
  bounds-checked scanner pattern).

### Go side (new + updated files)

- `internal/native/engine/protocol.go` — `ProtocolVersion` bumped 2 → 3;
  added op constants (`OpTokenizerInit`, `OpTokenizerInfo`,
  `OpTokenizerEncode`, `OpTokenizerDecode`, `OpKVCacheInfo`,
  `OpSchedulerInfo`) and `ValidOps` entries.
- `internal/native/engine/backend.go` — `ABIVersionExpected` bumped 2 → 3.
- `internal/native/engine/tokenizer.go` (NEW) — Go-side surface:
  `Engine.InitTokenizer`, `Engine.TokenizerInfo`, `Engine.TokenizerEncode`,
  `Engine.TokenizerDecode`, `Engine.KVCacheInfo`, `Engine.SchedulerInfo`.
  Honest error reporting: `ErrUnsupportedTokenizer` for an unsupported
  tokenizer model kind; `Generate`/`StreamGenerate` STILL return
  `llm.ErrNotImplemented` (Phase 4 does NOT fake inference).
- `internal/native/engine/phase4_integration_test.go` (NEW) — end-to-end
  tests against the real C++ host: `TestRealCppHostPhase4Tokenizer` (loads
  a real BPE GGUF, inits the tokenizer, encodes "hello" with BOS+EOS →
  [1, 6, 7, 2], decodes [6, 7] → " hello"), `TestRealCppHostPhase4KVCache`
  (verifies the honest zero-state), `TestRealCppHostPhase4Scheduler`
  (verifies real measured counts, single-slot).

### Frontend (new + updated files)

- `src/store.ts` — added a streaming coalescer: `queueStreamingContent` /
  `queueStreamingReasoning` accumulate token chunks; `flushStreaming` runs
  on a `requestAnimationFrame` boundary and writes ONE `setState` per
  frame. `handleConversationEvent` now uses the coalescer instead of
  calling `setState` per token. Lifecycle events (done/error/session)
  bypass the coalescer (`flushStreaming()` runs first, then resets
  `streaming`). `createSession` / `selectSession` / `disconnectActivity`
  call `resetPendingStreaming()` to drop pending chunks for the old
  session. The coalescer is the Phase 4 §17 contract:
  `native token stream → Go stream → WS events → UI accumulation buffer
  → frame-aligned render/update`.
- `src/perf-hud.ts` (NEW) — frame-budget diagnostic HUD. OFF by default;
  toggle with Ctrl+Shift+P or `window.__shtnTogglePerfHUD()`. Auto-detects
  the display refresh rate (sample 10 rAF intervals, derive Hz, compute
  the target budget: 8.33 ms for 120 Hz, 16.67 ms for 60 Hz). Measures
  real frame time (avg/min/max), dropped frames (> 1.5× budget),
  longtask count (PerformanceObserver), coalesced stream-update frequency
  (counts `flushStreaming` calls per second, NOT per token). The HUD
  reports `optimized for high-refresh displays / frame-budget aware /
  120 Hz-capable presentation where hardware permits` — it does NOT claim
  guaranteed 120 FPS.
- `src/main.tsx` — initializes the perf HUD and wires the stream-update
  recorder.

## Validation performed (this phase)

```text
go build -tags headless ./...                                              PASS
go vet  -tags headless ./...                                               PASS
go test -tags headless ./internal/... -count=1                             27 packages PASS
go test -tags headless ./internal/native/engine/                           PASS (incl. Phase 4 integration tests)
cmake -S native/engine -B native/engine/build                              PASS
cmake --build native/engine/build -j 4                                     PASS (0 warnings)
ctest --test-dir native/engine/build                                      9/9 PASS:
  engine, protocol, host, gguf, model (Phase 1+2)
  + tokenizer, kv_cache, scheduler, sampler (Phase 4)
go test -tags headless ./internal/native/engine/ -run TestRealCppHostPhase4 3/3 PASS:
  TestRealCppHostPhase4Tokenizer   — BPE round-trip through real C++ host
  TestRealCppHostPhase4KVCache     — honest zero-state verified
  TestRealCppHostPhase4Scheduler   — measured counts verified
npm run typecheck                                                          PASS
npm run lint                                                               0 warnings, 0 errors
npm run build                                                              PASS (web/static synced)
go run ./scripts/stress-main stress                                        30 pass / 0 fail
node scripts/release-version.mjs --check                                   PASS
```

## Honest scope statement

Phase 4 implements REAL foundation primitives. It does NOT implement:

- the transformer forward pass (no RMSNorm, no GQA attention, no RoPE,
  no SwiGLU MLP, no logits projection);
- native generation (Generate/StreamGenerate still return
  `llm.ErrNotImplemented`);
- real measured TTFT / tokens-per-second (the GenerationStats struct
  carries the shape, but every value is honestly 0 — no fabricated
  inference metrics);
- Windows/Linux verification on real hardware (the build is cross-compile-
  clean; CI runs the Windows job; this sandbox is Linux-only and the
  Windows-specific behavior is verified by the existing `main_windows.go`
  + `sysinfo` CIM path that was already shipped in v1.1.4Z and unchanged
  here);
- continuous batching / speculative decoding (the scheduler is single-slot
  by design — the execution path cannot support either correctly yet);
- a real GPU/NPU forward pass (the hardware profile can REPRESENT
  accelerators; no kernel exists).

The llama.cpp fallback remains the production generation backend. Native
generation will be enabled ONLY after a real transformer forward pass
exists and is verified end-to-end against a real GGUF model — never
before.

---

# v1.1.5Z Phase 3 Implementation Log (2026-09-10)

## Goal

Make SHEYTAN significantly more efficient for real local workloads by
improving the complete data path — file/attachment/conversation →
loading → normalization → chunking → cache → context planning →
recall/memory → LLM request — without breaking verified behaviour,
wire contracts or any security bound. Generation stays on llama.cpp;
the Phase 2 native GGUF loading path is untouched and still passes its
real-host integration tests.

## Baseline verification (before any change)

The Phase 2 tree was verified FIRST at commit `dc4172c`
(v1.1.5Z-phase2): `go build -tags headless ./...`, `go vet -tags headless
./...`, the full `go test -tags headless ./internal/... -count=1` suite
(all packages PASS), race tests on agent/llm/api/native-engine (PASS),
npm typecheck/lint/build (PASS), stress 30/30, release-version --check
PASS, CMake configure+build, ctest 5/5 and the two real-host Go↔C++
integration tests PASS. Only then did Phase 3 work begin.

## Audit findings (measured, from source)

1. `attachments.Retrieve → chunkText`: the ENTIRE stored object was
   re-read per selected chunk (N selected chunks = N full-object reads
   and N full-size transient allocations per retrieval).
2. `attachments.Add`: the whole upload (up to 64 MiB) was buffered in
   RAM just to hash and classify it, including binaries and images —
   only a 16 KiB head is needed for classification.
3. `memory.Store`: every append invalidated the parsed-cache key, so the
   next search re-parsed the WHOLE JSONL file; search additionally
   copied every entry per call.
4. `recall.Engine.Search`: rebuilt a term-frequency map for EVERY
   capsule on EVERY query (≈15k map allocations per turn at the 5000
   capsule index cap).
5. `contextcache`: concurrent callers of the same expensive key both
   computed; a single entry larger than the byte bound could pin the
   cache above its bound forever (eviction always keeps ≥1 entry).
6. `attachments.buildChunks`: recomputed chunk offsets by re-scanning
   the text with `strings.Index` per chunk; hard splits could cut a
   multi-byte UTF-8 rune in half.
7. `llm.ReadModelCard`: unbuffered reads issued one syscall per skipped
   tokenizer-array string; the model-card cache key ignored file size.
8. Context construction had no measured instrumentation (bytes,
   chunks considered/selected, cache behaviour, pressure).

## 1. Unified loading pipeline

Ownership is now explicit and single-copy per stage:

```text
Source/Input        staged upload, object file, session JSON, memory JSONL
→ Loader            attachments.Manager.spool (streaming), readObject/
                    readObjectRange (counted), memory/recall scanners
→ Normalizer        attachments.NormalizeText (zero-copy fast path)
→ Chunker           chunking.ChunkText (single interval pass)
→ Cache             contextcache.Cache (single-flight, bounded)
→ Retriever         attachments.RetrieveWithStats, recall.Engine.Search
→ Context Builder   contextplan.Assemble + orchestrator (measured plan)
```

No duplicate loaders were introduced; the legacy `writeObjectAtomic` /
`buildChunks` paths were removed after their last callers moved to the
shared flow.

## 2. Chunking engine (internal/chunking/chunker.go)

New `ChunkText` / `ChunkerConfig` / `Chunk` (v2 processing version):

- one interval pass, no rescanning; chunk strings share the source
  backing array (no repeated copying, no O(n²));
- exact byte offsets recorded during the pass (the old path re-scanned);
- configurable `Overlap` (rune-aligned, progress-guaranteed) and
  `MaxChunks` without unbounded memory;
- UTF-8-correct hard splits (a hard cut backs up to a rune boundary);
- deterministic chunk IDs from source content hash + processing
  parameters + chunk content hash;
- full metadata per chunk: source identity, byte range, estimated
  tokens, sequence index, total chunks, processing version, preview;
- raw source data is never retained by derived chunks.

`SplitParagraphs` was re-expressed on the same pass; with ASCII input
its cuts are byte-identical to the pre-Phase-3 algorithm (a fuzz-style
equivalence test pins this). Lossless reconstruction is preserved for
Overlap=0. `attachments.buildChunks` now maps `chunking.Chunk` onto the
unchanged wire format (`<attID>:<index>:<hash8>`, same JSON fields), so
stored meta files stay valid.

## 3. Cache as a data-layer primitive (internal/contextcache)

- `GetOrCompute[T]`: single-flight coalescing — the first caller
  computes OUTSIDE the cache mutex, joiners share the result; panics
  are propagated to joiners and never wedge the in-flight slot;
- oversized-entry guard (`WithMaxEntryBytes`): `Put` (now returns
  bool) rejects a single entry above the per-entry bound instead of
  letting it pin the cache above its byte bound;
- exact accounting: `inserts` counted on new keys only, oversized
  rejections and coalesced joins measured;
- `Stats` gained `inserts`, `coalesced`, `oversizedRejected`; content
  keys, version invalidation, config fingerprints, LRU and both bounds
  are unchanged. Processing version bumped 3 → 4.

## 4. Attachment loading (internal/attachments)

- STREAMING STAGING: `Add` spools to a temp object while hashing
  (SHA-256 on the fly) and keeps only a 16 KiB sniff head plus one
  pooled 128 KiB copy buffer in RAM — binaries and images are never
  fully buffered anymore; oversize/empty rejects happen during the
  copy and never touch the object store;
- content addressing, symlink refusal, dedupe, naming, caps and
  provenance unchanged (same error strings, same IDs);
- text processing reads the stored object back once;
- `RetrieveWithStats`: each attachment's object is read AT MOST ONCE
  per retrieval call and reused for every selected chunk (verified:
  objectReads == 1 for a multi-chunk call); objects larger than the
  32 MiB per-call retention cap degrade to exact byte-range reads —
  correctness identical, memory bounded;
- `NormalizeText` zero-copy fast path: valid UTF-8 without CR/BOM
  returns the original bytes (previously up to three full-content
  copies);
- measured resource counters on the Manager (`ResourceUsage()`):
  files staged, staged bytes, object reads, bytes read, chunks built,
  cache sheds.

## 5. Context construction + instrumentation

- `contextplan.Plan.PromptBytes`: the MEASURED byte size of the final
  prompt (set by the orchestrator after assembly; never an estimate);
- the orchestrator logs one context-metrics line per turn (prompt
  bytes, estimated tokens, pressure, elided, recalled, attachments);
- the API server logs one attachment-retrieval line per turn with the
  measured retrieval stats (attachments considered, chunks
  considered/selected, bytes, object reads, cache hit);
- wire/UI contracts unchanged — the plan JSON gained additive fields
  only.

## 6. Persistent memory efficiency (internal/memory)

- APPEND-AWARE cache: `AppendEntry` folds the normalized entry into the
  parsed cache and refreshes the stat key; `DeleteByID` rewrites from
  the warm cache (it previously re-parsed the file on every delete);
  `Clear` caches the empty state; an empty store parse warms the cache;
- copy-free search: `SearchWithOptions` scores the cached entries
  read-only (scores in a parallel slice; cached entries never mutated)
  and copies only matches — the whole-store copy per search is gone;
- a non-existent file correctly stays cold (nothing to cache);
- trust rules untouched: M1–M7, quarantine, external downgrade,
  authoritative-user-fact logic and search semantics are byte-for-byte
  the same paths as before, re-verified by the full trust test suite;
- measured counters: `Store.ParseStats()` (full parses vs incremental
  appends) — tests assert appends cause ZERO full re-parses.

## 7. Resource policy

Bounds are unchanged in law and now observable in practice: staging
caps (size/count/total/timeout), the 512-chunk-per-file cap, the
32 MiB per-call retrieval retention cap with graceful degradation to
byte-range reads, the cache bounds (entries, bytes, per-entry), the
5000-capsule recall cap, and the memory-store stat-keyed cache. The
degradation ladder is: shed retained derived objects → re-read from
disk on demand → never truncate authoritative data (nothing
authoritative is retained in the shedded structures). All counters are
real measurements exposed via `attachments.ResourceUsage()`,
`contextcache.Stats` and `memory.ParseStats()`.

## 8. Other loading paths

- `llm.ReadModelCard` now reads through a 64 KiB buffered window —
  skipping a tokenizer vocabulary array used to issue one unbuffered
  syscall per string (up to ~150k per header); parsing semantics
  unchanged;
- the API model-card cache is keyed by path+size+mtime (a same-size
  rewrite used to serve stale metadata) and bounded (512 entries).

## 9. Concurrency

No new goroutines were introduced. The only new concurrency primitive
is the cache's in-flight map (mutex-protected, WaitGroup-synchronised;
compute happens without the lock). The full race suite passes:
`-race` on agent, llm, api, native/engine, contextcache, memory,
attachments and recall, plus the new concurrent cache/memory/retrieval
tests.

## 10. Benchmarks (measured, before vs after)

Environment: 2-vCPU Linux container, Go 1.27.1. Same-binary comparison
for the chunker (legacy algorithm compiled next to the new one);
baseline worktree at commit dc4172c for memory/recall. Median of 5:

| Benchmark | Before | After | Δ |
|---|---|---|---|
| chunk derivation — 512×4 KiB chunks of a 5.5 MB source (includes sha256 + metadata + preview) | 5.77 ms/op · 10.91 MB/op · 4880 allocs | 3.64 ms/op · 2.46 MB/op · 4880 allocs | 1.6× faster · 4.4× fewer bytes |
| memory.Search — 5000-entry store | 7.40 ms/op · 7.67 MB/op · 172 allocs | 3.24 ms/op · 1.53 MB/op · 90 allocs | 2.3× faster · 5× fewer bytes |
| recall.Search — 5000-capsule corpus | 4.78 ms/op · 5.46 MB/op · 15160 allocs | 2.37 ms/op · 3.17 MB/op · 124 allocs | 2.0× faster · 122× fewer allocs |
| contextcache.GetOrCompute hit | — | 20.9 ns/op · 0 allocs | hit path is allocation-free |

Structural wins not visible in micro-benchmarks: per-retrieval object
reads drop from N(selected chunks) to ≤1 per attachment (verified by
test); binary/image staging peak RAM drops from the full file size
(≤64 MiB) to ≤16 KiB + one copy buffer; remember→recall cycles no
longer re-parse the memory file.

## 11. Tests added

- `internal/chunking/chunker_test.go`: legacy-equivalence (ASCII
  byte-identical), lossless reconstruction, determinism, metadata
  integrity (hash/offset/total/version round-trip), UTF-8 hard splits,
  max-chunks cap, overlap re-inclusion + termination, benchmarks;
- `internal/contextcache/contextcache_phase3_test.go`: compute-once,
  16-goroutine coalescing, panic propagation + recovery, oversized
  rejection (Put and GetOrCompute), cross-key parallelism, exact
  insert accounting, hit-path benchmark;
- `internal/attachments/attachments_phase3_test.go`: 6 MB staging +
  retrieval, identical-content cache reuse (chunksBuilt unchanged),
  changed-content invalidation, single object read per call, degraded
  range-read path, measured retrieval stats, binary never fully
  buffered, concurrent retrieval;
- `internal/memory/memory_phase3_test.go`: appends cause zero full
  re-parses, search correctness after append/clear/re-append,
  incremental delete, external-writer visibility, cache non-mutation,
  concurrent append+search race, 5000-entry benchmark;
- `internal/recall/recall_phase3_test.go`: BM25 scoring identical to a
  tf-map reference implementation, distinct-count integrity, clear
  resets caches, 5000-capsule benchmark.

## 12. Validation performed (this release)

```text
go build -tags headless ./...                                  PASS
go vet -tags headless ./...                                    PASS
go test -tags headless ./internal/... -count=1                 ALL PACKAGES PASS
go test -race -tags headless ./internal/{agent,llm,api,
        native/engine,contextcache,memory,attachments,recall}/ PASS
npm run typecheck / lint / build                               PASS
go run ./scripts/stress-main stress                            30 pass / 0 fail
node scripts/release-version.mjs --check                       PASS
cmake -S native/engine -B native/engine/build                  PASS
cmake --build native/engine/build                              PASS
ctest --test-dir native/engine/build                           5/5 PASS
go test -tags headless ./internal/native/engine/
  -run 'TestRealCppHostEndToEnd|TestRealCppHostModelLifecycle' PASS (real host binary)
```

## Known limitations (Phase 3, by design)

- Retrieval scoring still ranks on chunk previews (cheap first pass);
  this was a deliberate v1.1.3Z design, not changed here.
- The native engine still does NOT generate tokens — Phase 2 model
  loading is untouched; generation remains llama.cpp.
- No semantic/structural repository retrieval exists (nothing in this
  phase implements embeddings or an index beyond the existing BM25 and
  lexical scorers).
- No new config surface: the phase reuses existing limits; nothing was
  added to the settings UI or the wire API.

# v1.1.5Z Phase 2 Implementation Log (2026-09-10)

## Goal

Make the SHEYTAN Native Engine load a real supported GGUF model in C++:
read its metadata, validate it, plan its memory requirements and expose
real model information to Go — WITHOUT redesigning the Phase 1 Go↔C++
boundary. Generation stays on llama.cpp in this phase (explicitly).

```text
Go Core → SHEYTAN Native API → shtn-engine-host → C++ Native Engine → GGUF model
```

## Phase 1 verification (before any change)

The Phase 1 implementation was verified FIRST on the base commit
(`f4488d5`, v1.1.5Z-phase1): C++ build + 3/3 ctest, `go build`/`go vet`,
22 Go packages pass, race tests pass, `TestRealCppHostEndToEnd` passes,
npm typecheck/lint/build pass, stress 30/30, release-version --check
PASS. Only then did Phase 2 work begin.

## 1. Versioned protocol/ABI extension (no boundary redesign)

Protocol v1 → **v2** and ABI v1 → **v2**, bumped together on both sides
(`include/shtn/version.h` + `internal/native/engine/protocol.go` /
`backend.go ABIVersionExpected`). The handshake still fails closed on
mismatch. Pre-existing ops (ping/health/hwinfo/metrics/cancel/shutdown)
kept their exact wire shapes — the change is purely additive:

- wire ops `load_model` (payload `{path, contextLength?}`),
  `unload_model`, `model_info`;
- C ABI functions `shtn_engine_load_model` / `shtn_engine_unload_model` /
  `shtn_engine_model_info` / `shtn_engine_memory_plan` with new fixed-size
  structs (`shtn_model_load_options`, `shtn_model_info`,
  `shtn_memory_plan`) and new error codes appended (never renumbered):
  `SHTN_ERR_MODEL_FORMAT` / `SHTN_ERR_MODEL_STATE` / `SHTN_ERR_NO_MODEL`.

## 2. Native GGUF reader (native/engine/src/gguf.*)

Dependency-free, mmap-backed, hostile-input-hardened:

- magic + version validation (v2/v3 — the llama.cpp-supported set; v1
  and >3 rejected with a readable error);
- metadata parsing with bounds on every count/length (KV count ≤ 16384,
  string ≤ 16 MiB, array elements ≤ 100M, tensor count ≤ 1M, dims ≤ 8);
  unknown value types rejected; `general.alignment` validated (power of
  two, ≤ 4096) and honored for the data-section start;
- array VALUES are never materialized (the tokenizer vocab array is
  walked length-prefix-only; its element count feeds the vocab-size
  fallback);
- tensor table: per-tensor dims bounds, overflow-checked element
  products (a dims product that overflows uint64 is a hard reject),
  offsets validated against the data section, exact byte-size checks for
  known GGML types (ggml_nbytes formula: last dim padded to block size),
  lenient in-range checks only for types this build does not know;
- every arithmetic that could overflow goes through checked_add/mul —
  wrap-around is impossible by construction;
- the reader touches ONLY header pages (no full-file reads, no copying).

## 3. Model lifecycle + memory planning (native/engine/src/model.*)

- state machine: `unloaded → loading → loaded | failed → unloaded`, one
  mutex-protected model slot per engine; loads serialize, inspections
  run concurrently against stable snapshots;
- load = validate path → whole-file read-only mmap (POSIX mmap / Windows
  CreateFileMapping; lazy, no eager copy) → parse + validate → metadata
  extraction → memory plan → `loaded`. Replace semantics (load while
  loaded unloads first), all-or-nothing (a failed load leaves NOTHING
  loaded, mapping and handle released on every path);
- caller-argument errors (empty path, reserved field) leave the state
  unchanged (nothing was attempted); file-level failures (missing file,
  parse errors, zero-tensor files) walk to `failed` with the reason
  recorded;
- unload is idempotent; engine destroy releases the mapping;
- memory plan (computed, NOTHING allocated): model file / mapped bytes /
  weights (tensor-data span = file − data start) / workspace estimate
  (context·vocab·4 logits row) / KV-cache estimate
  (2·K/V·layers·context·embedding·2 bytes f16) / fixed 64 MiB runtime
  allowance / overflow-checked total / fit-vs-detected-RAM verdict
  (1/0/-1). Inputs missing from the file → that component is an honest 0.

## 4. Go side (internal/native/engine)

- `model.go` is now REAL: `Engine.LoadModel` / `Engine.UnloadModel` /
  `Engine.ModelInfo` drive the IPC ops with a dedicated model-state
  machine (`unloaded/loading/loaded/failed`) — deliberately separate
  from the engine states (`llm.State*`), no second engine-state system;
- the model snapshot RESETS on every host lifecycle boundary (start,
  restart, deliberate stop, death): a fresh host maps nothing, so stale
  state cannot survive;
- load ops use a bounded 30 s window (metadata-only parse, but cold
  slow disks deserve headroom) plus the caller ctx;
- `backend.go` maps the native card onto the shared `llm.ModelInfo`
  contract — additive fields (`State`, `FileSizeBytes`, `TensorCount`,
  `GGUFVersion`, `VocabSize`, `EmbeddingLength`, `LayerCount`,
  KV/workspace/total memory estimates) that the llama path leaves zero;
  `llm.FormatParameterCount` shared with the llama card formatter;
- metrics op additionally reports `modelState` (observability only);
- `GenerationCapable()` stays FALSE: loading is not generating.
  Generate/StreamGenerate still return `llm.ErrNotImplemented` and
  `llm.SelectGenerationBackend` still resolves to llama.cpp — pinned by
  tests.

## 5. Tests added

C++ (`ctest`, 5 suites now):

- `test_gguf` — valid v2/v3 files, custom alignment, malformed magic,
  unsupported versions (1 and 4), truncated files (empty / magic-only /
  counts cut / metadata cut / tensor-table cut / missing data section),
  hostile counts/lengths/types/alignment, invalid tensor entries
  (out-of-range offset, exact-size overflow, zero dim, dim-count bounds,
  dims-product overflow), checked arithmetic, metadata helpers;
- `test_model` — fresh-engine unloaded state, NULL/invalid-argument
  rejection, load→info→plan correctness (exact KV/workspace/total
  values), context-override planning, load A → load A again → load B
  replace semantics, unload/reload/idempotent unload, failed-load
  recovery (garbage file, zero-tensor file), destroy-with-loaded-model,
  concurrent safe inspection (4 inspectors + load/unload churn thread);
- `test_engine` / `test_host` extended: ABI v2 pins, model-surface NULL
  checks, fresh-engine `model_info` op, load/unload round trips through
  the dispatch loop, malformed-payload bounded errors, host-survives
  checks.

The whole C++ suite also passes under AddressSanitizer (no leaks, no
out-of-bounds access in the reader or the lifecycle).

Go:

- `model_test.go` — fake-host lifecycle matrix (load A / load A again /
  load B / unload / reload / failed load), validation ordering, bounded
  caller-ctx load, state resets across stop/start, concurrent inspection
  (race-detector target);
- `cpp_integration_test.go` — `TestRealCppHostModelLifecycle`: a real
  GGUF v3 file is built byte-by-byte in the test, loaded through the
  REAL C++ reader via the real host, metadata + derived parameter count
  + memory plan verified exactly, unload/reload/failed-load/recovery
  exercised, and the `llm.ModelInfo` mapping checked end-to-end;
- `engine_test.go` — fake host grew model ops (including loadfail /
  loadslow modes) mirroring the v2 wire shapes.

## 6. Validation performed (this release)

```text
cmake -S native/engine -B native/engine/build + build   PASS
cmake --build + ctest                                   5/5 PASS
                                                        (engine, protocol, host,
                                                         gguf, model)
plain make + make test                                  PASS
C++ suite under AddressSanitizer                        5/5 PASS (no leaks)
go build -tags headless ./...                          PASS
go vet  -tags headless ./...                           PASS
go test -tags headless ./internal/... -count=1         22 packages PASS
go test -race -tags headless (agent, llm, api,
                              native/engine)            PASS
TestRealCppHostEndToEnd + TestRealCppHostModelLifecycle PASS (real C++ host)
frontend: typecheck / lint (0 warnings) / build         PASS (assets unchanged:
                                                         no frontend edits)
stress suite (release gate)                             30 pass / 0 fail
node scripts/release-version.mjs --check                PASS (all surfaces 1.1.5)
version smoke: release stays v1.1.5Z                    PASS (no bump)
```

## Known limitations (Phase 2, by design)

- **No native inference.** Loading a model does NOT enable generation:
  Generate/StreamGenerate return `ErrNotImplemented`,
  GenerationCapable() is false, and every generation request runs on
  llama.cpp. Do not "fix" that by faking inference.
- The memory plan is arithmetic on parsed metadata (weights span, f16 KV
  estimate, logits-row workspace, fixed overhead allowance) — real
  allocation behavior arrives with the inference phase.
- GGUF v1 containers and GGML tensor types unknown to this build's size
  table are rejected or validated leniently (offset-in-range only),
  never guessed.
- The context override plans memory only; no context buffers exist yet.
- The native host binary is still not shipped or auto-downloaded; build
  it from `native/engine/` and place it in `{DataDir}/bin/` (or
  `nativeEnginePath`).
- SIGBUS risk if a mapped file shrinks mid-flight is inherent to mmap
  consumers (llama.cpp has the same property); files are opened
  read-only and nothing in SHEYTAN writes to model files.

---

# v1.1.5Z Phase 1 Implementation Log (2026-09-10)

## Goal

Establish the SHEYTAN Native AI Engine architecture:

```text
React/TypeScript → Wails → Go Core → SHEYTAN Native API → C++ Native Engine
```

Go remains the main application/runtime engine; the C++ engine becomes
the future heavy-compute/AI execution engine. llama.cpp stays functional
as the fallback throughout.

## 1. LLM backend abstraction (internal/llm)

`llm.Backend` formalizes the engine surface the rest of SHEYTAN depends
on: `Start / Stop / Health / LoadModel / UnloadModel / Generate /
StreamGenerate / Cancel / ModelInfo / HardwareInfo / Metrics`, plus
shared contract types (`HealthReport`, `ModelSpec`, `ModelInfo`,
`HardwareInfo` — the platform-neutral hardware profile —, `Metrics`) and
the error vocabulary (`ErrNotImplemented`, `ErrCancelContextBased`).

`llm.LlamaBackend` adapts the EXISTING pieces (LlamaServer + Client) to
the contract — a delegation layer, not a second engine: streaming,
retries, the stall watchdog, cancellation, busy reporting, engine events
and the config snapshot contract are untouched. Additive surface on
LlamaServer: `StartedAt()`, `Restarts()` (measured values for metrics)
and `ProbeHealth()` (a real bounded `/health` GET — the same endpoint the
startup ladder polls).

`llm.SelectGenerationBackend` is the single routing point: the native
engine serves generation only when selected AND
`GenerationCapable()`; otherwise llama.cpp. In Phase 1 the native backend
reports generation-incapable, so generation always resolves to llama.cpp
— pinned by tests so Phase 2 flips routing by implementing generation,
not by editing call sites.

## 2. Native engine package (internal/native/engine)

Concern layout (files): `protocol.go` (framing + ops + validation),
`runtime.go` (supervision), `backend.go` (`llm.Backend` adapter),
`platform.go` (hardware profile assembly), `metrics.go`,
`model.go`/`memory.go`/`kv.go`/`generation.go`/`scheduler.go` (Phase 1
concern types — the future data model, no fake inference).

- **Protocol**: 4-byte LE length-prefixed JSON over the host's
  stdin/stdout; 1 MiB frame cap; ops `ping/health/hwinfo/metrics/cancel/
  shutdown`; malformed frames rejected as protocol violations; request/
  response multiplexer with per-op timeouts (10 s) and serialized writers.
- **Supervision**: spawn (sanitized environment — no secrets cross the
  boundary) → protocol+ABI handshake (fails closed on mismatch) → health
  → ready; event-driven exit watcher (no polling, no busy loops); bounded
  auto-restart (3 recoveries per supervision episode, 1/2/4 s backoff,
  deliberate stops suppressed, terminal failure visible). Native engine
  state uses the SAME vocabulary and event shape as llama.cpp
  (`llm.State*`, `llm.EngineEvent`) — no second state system.
- **Hardware profile**: native probe (C++ detected values) merged with
  the sysinfo probe (GPU/VRAM); `DetectedBy` records each source; NPU/
  accelerator and shared-memory GPU fields are representable but stay
  empty until a detector exists.
- **Metrics**: only measured values — engine state, pid, uptime, restarts
  (Go side) + the C++ engine's own process RSS and uptime; generation
  metrics (TTFT, prompt/decode speed) are OMITTED until a backend
  actually serves generation.

## 3. C++ native engine skeleton (native/engine/)

Buildable independently (CMake ≥ 3.16 or plain make; C++17; ZERO
third-party dependencies):

- `include/shtn/{engine,types,version}.h` — the narrow C ABI: engine
  create/destroy, health, hardware info, metrics, ABI version. Opaque
  handle, fixed-size C structs, error codes instead of exceptions.
- `src/engine.cpp` — the engine core (state, uptime, hardware cache,
  measured metrics).
- `src/hardware.cpp` — real platform detection (Linux /proc + sysconf,
  Windows cpuid + GLPI + GlobalMemoryStatusEx + psapi, macOS sysctl +
  mach) with per-platform #ifdef paths.
- `src/{protocol,json}.cpp` — framing + a careful minimal JSON
  parser/serializer (escape-safe, nesting-aware, hostile-input-safe).
- `src/host_main.cpp` — `shtn-engine-host`: the supervised subprocess;
  one request frame in → one response frame out; malformed input gets a
  bounded error response (never a crash, never an exit); `shutdown`
  acknowledges and exits cleanly; stdin EOF exits cleanly.
- `tests/` — `test_engine` (ABI contract: create/destroy, NULL
  rejection, ABI mismatch, health, hwinfo, metrics monotonicity),
  `test_protocol` (framing, caps, truncation, JSON edge cases),
  `test_host` (dispatch: valid ops, unknown ops, garbage inputs, oversized
  frames, shutdown, EOF).

## 4. Go↔C++ boundary decision

**B) supervised native subprocess + IPC** (documented in
`internal/native/engine/doc.go` and ARCHITECTURE.md §I.9): crash
isolation (a native crash cannot kill the app; the bounded watchdog
restarts it), preserved `CGO_ENABLED=0` Windows cross-build, future
Android maps to a service process, explicit versioned protocol, and
coarse-grained ops only (the boundary never carries tiny high-frequency
calls). Verified end-to-end by `TestRealCppHostEndToEnd` (Go Engine ↔
real C++ host: handshake, health, hardware, metrics, cancel, clean stop).

## 5. llama.cpp preserved (fallback contract)

- llama.cpp remains the DEFAULT engine (`engineBackend: "llama"`) — the
  entire v1.1.4Z behavior is unchanged unless the user explicitly opts in.
- With `engineBackend: "native"`: the native engine is supervised
  (lifecycle real), generation still runs on llama.cpp (native reports
  generation-incapable), and native failures NEVER fail the llama path
  (best-effort, logged, visible in the engine snapshot).
- The engine toggle (POST /api/llama start/stop) drives both engines when
  native is enabled; llama's state stays the authoritative action result.
- `/api/engine` snapshot gains `backend` (effective generation backend)
  and a `native` status block (local reads only — the poll path performs
  NO engine IPC); native transitions broadcast into the existing WS
  pipeline with "Native engine …" captions; the UI badge keeps reading
  the llama snapshot state.
- Config: `engineBackend` (exact-match opt-in, fail-closed to llama on
  malformed values; normalized on load), `nativeEnginePath` (optional
  override). Consumed by runtime wiring + selection; no Settings UI
  control in Phase 1 (config.json / `SHEYTAN_ENGINE_BACKEND` env are the
  opt-in paths — a toggle whose only visible effect would be a process in
  Task Manager is feature theater).

## 6. Validation performed (this release)

```text
go build -tags headless ./...                          PASS
go vet  -tags headless ./...                           PASS
go test -tags headless ./internal/... -count=1         22 packages PASS
go test -race -tags headless (agent, llm, api,
                              native/engine, config)   PASS
frontend: npm ci / typecheck / lint (0 warnings) / build
         + sync into web/static                       PASS (assets unchanged:
                                                         types-only api.ts edit)
stress suite (release gate)                            30 pass / 0 fail
node scripts/release-version.mjs (sync + --check)      PASS (all surfaces 1.1.5)
C++ (cmake 3.30 + g++ 14, C++17): build                PASS
C++ ctest: engine + protocol + host                    3/3 PASS
Go↔C++ integration (TestRealCppHostEndToEnd)           PASS
version smoke: "SHEYTAN-Local-Agent v1.1.5"            PASS
```

New tests added:

- `internal/llm/backend_test.go` — selection matrix (default llama /
  native-incapable fallback / native-capable (Phase 2 shape) / native
  absent / malformed values fail closed / no-probe backends), llama
  backend delegation, cancel semantics, metrics (measured values only),
  hardware from sysinfo, model validation, contract constants.
- `internal/native/engine/engine_test.go` — protocol framing/validation;
  lifecycle (start→ready, stop walk, events, missing binary → failed);
  failure (crash → bounded restart ladder → terminal failed, garbage
  host → connection loss detection, slow host → bounded teardown);
  cancellation (cancel round-trip miss with reason, op timeouts);
  concurrent lifecycle (race-clean); hardware merge; measured metrics;
  backend contract (ErrNotImplemented for generation, GenerationCapable
  false, LoadModel validates first); model spec path jail.
- `internal/native/engine/cpp_integration_test.go` — end-to-end against
  the real C++ host binary (skips when not built).
- `internal/api/server_native_test.go` — engine snapshot (default: no
  native block; native selected: honest unavailable status, backend stays
  llama), engine toggle behavior unchanged (500 on missing binary exactly
  as v1.1.4Z).

## Known limitations (Phase 1, by design)

- **No native inference.** Generate/StreamGenerate/LoadModel/ModelInfo
  return `ErrNotImplemented` on the native backend; every generation
  request runs on llama.cpp. This is the honest fallback signal — do not
  "fix" it by faking inference.
- The native host binary is not shipped or auto-downloaded; it must be
  built from `native/engine/` (CMake or Make) and placed in
  `{DataDir}/bin/` (or `nativeEnginePath`).
- The C++ hardware probe does not enumerate GPUs/accelerators (the Go
  side merges sysinfo for real GPU facts; the profile fields exist and
  stay empty until a detector does).
- The Wails/GTK desktop build still cannot compile in the Linux CI-less
  environment (pre-existing; Windows CI builds it).
- llama.cpp watchdog semantics (budget resets after a successful
  restart) are intentionally UNTOUCHED; the native engine uses a stricter
  episode policy (auto-restarts never reset the budget — a persistently
  crashing native host gives up after 3 recoveries).

---

# v1.1.4Z Windows CI repair (2026-09-07, post-release)

The `Windows x64` job of the `Build Desktop` workflow failed at the
`Verify release metadata` step with:

```text
A positional parameter cannot be found that accepts argument '1.1.4\'.
```

## Root cause

The step reused **Bash-style `\"` escaping inside PowerShell strings**:

```powershell
-Pattern "AppVersion  = \"$env:APP_VERSION\""   # invalid in PowerShell
```

PowerShell terminates the string at the quote after the backslash, the
`-Pattern` argument splits into extra positional tokens, and parameter
binding fails on the fragment `1.1.4\`. The release metadata itself was
never wrong — `node scripts/release-version.mjs` succeeded and all four
surfaces were in sync. Reproduced locally with pwsh 7.4.6 executing the
verbatim step script: identical error, exit 1.

## Second (latent) defect fixed in the same step

`Select-String ... | Out-Null` **exits 0 when nothing matches**. Even with
correct quoting, a mismatched release surface would have passed the gate
silently (verified by test: wrong version, exit 0). The repaired step uses
`-Quiet` and throws on a false result, so a metadata mismatch now fails
the job loudly.

## Fix

`Windows x64` job, `Verify release metadata` step: rewritten with
PowerShell-native single-quoted literals plus explicit concatenation
(no escaping needed), `-SimpleMatch` retained, all four release surfaces
checked in a data-driven loop, expectations derived from
`$env:APP_VERSION` (version-agnostic, no hard-coded version). The bash
`grep -F` equivalents in the audit and Linux jobs were already correct
and untouched.

## Verification performed (this repair)

- The exact step script (extracted verbatim from the workflow YAML)
  executed with pwsh 7.4.6 against the real repository files:
  10/10 checks — correct metadata passes; wrong `APP_VERSION` fails;
  corrupted `SIGNATURE` fails; version bump to 1.1.5 through the release
  machinery passes and restores cleanly; no `\"` artifacts remain
  anywhere in the workflow.
- `node scripts/release-version.mjs` and `--check`: all four surfaces
  consistent, no drift, byte-preserving sync intact.
- Full suite re-run after the change: `go build`/`go vet` headless,
  `go test -tags headless ./internal/...` (21 packages),
  `-race` on agent/llm/api, full headless tree, `npm ci`/typecheck/lint
  (0 warnings)/build, stress suite 30/0, releasegate — all green.
- `zeta_release_surface` stress contract still pins the workflow shape;
  no contract fragment was touched.

## Repository consistency cleanups shipped with the repair

- `web/static`: the committed tree carried a stale duplicate generation of
  hashed assets and a `manifest.json` pointing at the old generation while
  `index.html` referenced the new one. A clean `npm run build` +
  `scripts/sync-web.mjs` removes the 8 stale files and corrects the
  manifest; the runtime entry (`index.html` + referenced assets) is
  byte-identical.
- Documentation audit (new `ARCHITECTURE.md`, updates to `README.md`,
  `agent.md`, this file, `FIX-README.md`): every capability statement is
  now labeled with the truth standard (IMPLEMENTED / TESTED / PARTIALLY
  IMPLEMENTED / PLANNED), the small-models/multi-agent/Context-Engine
  direction is recorded as future architecture, `internal/multiagent` is
  documented (it was implemented and CLI-wired but absent from the
  package map), and two overstated claims were corrected
  ("semantic chunking" → paragraph-boundary chunking; "checksum-verified
  publication" → integrity-checked publication via ZIP CRC + entry
  contracts — the SHA256 steps left the workflow during earlier
  refactors).

---

# v1.1.4Z Remediation Log (2026-09-07)

## Audit scope and method

Full-repository inspection before any modification: every Go package (52k+ LOC), the React/TS frontend, CI workflow, build/release scripts, docs and tests. Six subsystem maps were produced (llm, agent+api, tools+lab+sandbox, research+memory+updater, config+cmd+CI, frontend) and every suspected dead/defective path was verified by reference tracing before removal. Baseline validation before changes: build, vet and all test packages green on `main` @ `79c710c` (v1.1.3).

## 1. Concurrency and state integrity

| Fix | Defect | Resolution |
|---|---|---|
| **Config data race** | `mergeConfigPatch` wrote `*s.cfg = updated` in place while run goroutines, the engine manager and the client read the same struct — a genuine `-race` class defect | `internal/config.Source`: copy-on-write holder (RWMutex, `Load/Store/Update`). Patch handler, engine compat writes, engine-tag writes and the update loop all publish through it; runs and requests snapshot once and stay consistent for their duration |
| **Orchestrator races** | tool map read/written without a lock; `SetSessionID` clobbered between concurrent runs; per-iteration config reads could flip policy mid-turn | RWMutex-guarded registry with `tool()` lookup; per-run config snapshot |
| **MarkBusy window** | aliveness checked under `mu`, transition applied after unlock — a death in between could be overwritten by a stale `busy`/`ready` | `setStateLocked` — the whole transition is atomic |
| **busyHook write race** | `Client.SetBusyHook` wrote a plain field that streaming reads raced | RWMutex-guarded |
| **MultiAgent struct mutation** | `Run` defaulted `m.maxIter` on the shared struct | local variable |
| **Termshell engine** | shared instance with unlocked `cwd`/`history`/`env` | mutex on `Exec` |
| **Stack.Browser lazy cache** | unlocked field write | `browserMu` |
| **Config persistence** | plain `WriteFile` (a crash mid-write truncated config.json) | atomic tmp+rename |

Regression tests: `TestSourceConcurrentReadWrite`, `TestConfigPatchIsRaceFree` (both run under `-race`).

## 2. Engine / LLM layer

| Fix | Defect | Resolution |
|---|---|---|
| **Engine download hang** | plain `http.Get` (no timeout, no cap) could hold `switchMu` forever | 10-minute context + 2 GiB cap on `downloadAndExtract` |
| **`ListLoadedModels` hang** | plain `http.Get` from an HTTP handler | 5-second client |
| **Streaming truncation** | a single 10-minute `http.Client.Timeout` covered the whole SSE body — long generations were silently cut, while stalled streams hung until it fired | dedicated stream client (no overall timeout, 2-minute header timeout) + **stall watchdog**: request-context cancel after 5 minutes with zero bytes (adaptive tick); `TestStreamStallWatchdogAbortsQuietStream` pins it |
| **Silent retry exhaustion** | the final give-up after 4 attempts was never logged | logged to `llm.jsonl` on both Chat and Stream paths |
| **Vestigial stateCh** | legacy channel written non-blocking, never read | removed (with `publishEvent`, folded into `setStateLocked`) |

## 3. Functional completion (documented-but-unwired subsystems)

Every item below was fully implemented and unit-tested in the repository but had **no production caller** — the exact "feature theater" class this release eliminates:

| Subsystem | Previous state | v1.1.4Z |
|---|---|---|
| **GGUF model cards** (`llm/gguf.go`) | complete parser, zero callers; `/api/models` shipped stat-only entries while the docs promised architecture/quantization/context | wired into `/api/models` with an mtime-keyed cache; parser now tested |
| **Sampling settings** | `minP`, `repeatLastN`, `presencePenalty`, `frequencyPenalty`, `mirostatTau/Eta` editable in Settings, never sent anywhere | sent per-request (OpenAI-standard fields to all providers; llama.cpp-only fields local-gated) and as engine launch flags |
| **Sandbox settings** | `sandboxEnabled/Memory/CPU` stored but ignored (runtime hardcoded 512 MB / 25 %; the toggle lied — the sandbox was ALWAYS on while the config said off) | wired: the toggle gates registration, memory/CPU feed the governor with parsed/clamped effective getters; default ON (fail closed) |
| **ShowPerfHUD** | collected telemetry never reached any UI | emits a `perf` activity event with the HUD line |
| **Continuum rollover** | distillation/chapters/framework sidecars implemented + tested since v1.0.7; only the pressure meter was live | wired after each run: threshold check → deterministic distill → new chapter session → `session` activity event; the frontend follows the thread automatically |
| **Recall feedback** | `SetFeedback`/`FeedbackFor` and the liked×1.25/disliked×0.6 steering existed since v1.0.6 with no write path | `/api/feedback` endpoint + 👍/👎 on assistant messages in the conversation view (optimistic, with revert on failure) |
| **Scheduled engine updates** | `updater.RunScheduled` (immediate pass when due + 6 h re-check) had zero callers — "update (scheduled: daily/…)" only ever meant manual runs | started in `Server.EnsureSetup` (Source-aware; `off`/`never` disables) |
| **BMP images** | `.bmp` accepted by discovery, rejected by the encoder | BMP decode via `x/image/bmp` |
| **Sampling presets** | served, loaded into the store, never rendered or applied | model metadata now surfaced in the model picker; preset data remains available for the sampling card |

## 4. Security

| Fix | Defect class | Resolution |
|---|---|---|
| **DNS rebinding (fetch)** | URL validation + DNS pre-resolution, but the dialer re-resolved the name — a rebinding answer slipped past | `pinnedPublicDialContext`: resolve → validate public → **dial the verified IP** (TLS keeps the original hostname) |
| **Zip-slip (updater)** | `extractZip` joined member names unvalidated | `safeZipPath` (absolute/volume/`..` rejected) + per-entry and total size caps; tested at both the path and archive layers |
| **Sandbox secret exposure** | `codeExec` ran with the FULL `os.Environ()` — model-generated python could read host API keys | `proc.SanitizedEnvironment` (shared with the Lab runner) |
| **Lab expansion escape** | lexical policy passed `$HOME/secret`, `~/x`, `${VAR}/x`, `%USERPROFILE%\x` while the shell resolved them outside the jail | expansion-token rejection + `HOME`/`USERPROFILE`/`TMPDIR` pinned to the workspace in the sanitized env |
| **WebSearch redirects** | used `http.DefaultClient` (any scheme/host on redirect) | dedicated client: ≤5 hops, http(s) only, no credentials |
| **Diagnostics leakage** | `tools.jsonl`/`llm.jsonl` shipped unredacted in the diagnostics zip (tool args can contain secrets) | redaction applied; `redact` extended to JSON members and env-style `key=value` |
| **SSRF test seam** | production-file bypass setter on an unsynchronized global | atomic `Bool` |
| **Unbounded bodies** | most JSON endpoints accepted arbitrary request sizes | `MaxBytesReader` on config (1 MB), sessions PUT (1 MB), research (64 KB), lab (256 KB), llama/abort (4 KB) |
| **Abort no-op** | malformed abort body returned `{ok:true}` while aborting nothing | 400 with the decode error |

## 5. Reliability, errors, resources

- **Per-run time budget** (`runTimeoutMinutes`, default 60, clamp 1–1440, 0 = off): a run was previously bounded only by `maxIterations` and per-call timeouts — a pathological turn could hold the run slot for hours. Timeout is distinguished from user abort in the end caption.
- **Swallowed persistence failures surfaced**: pre-run `Save` (user message could silently vanish), `AppendMessage` (a generated reply could be lost with no visible error), `AppendActivity`, `IndexTurn` — all now log, and reply loss emits a visible error activity.
- **netcheck worst case** 17.5 s → ~2.5 s (parallel probes; the sequential version stalled offline users on the engine-start gate).
- **Unbounded growth bounded**: recall index (5000-capsule retention, atomic rewrite), browser screenshots (keep 50), crash reports (keep 20), memory reads cached by (size, mtime) instead of re-parsing the file per tool call.
- **sysinfo on Windows 11 24H2+**: CIM via PowerShell first, `wmic` fallback; free-memory probe added.

## 6. Frontend

| Fix | Defect | Resolution |
|---|---|---|
| **createSession dead composer** | only prepended the session + set the id: the socket stayed bound to the OLD session (the stale guard then discarded every event for the new one) and state was never reset — the first message on a fresh session never streamed and `running` stuck true | full reset + socket rebind (mirrors `selectSession`) |
| **deleteSession stale view** | the deleted session's conversation stayed on screen | state cleared + `loadSession` for the newly active session |
| **No WS reconnect** | any mid-run disconnect permanently killed event delivery (`running` could never clear) | exponential-backoff auto-reconnect (1.5 s → 15 s, 20 attempts, reset on success) |
| **Activity captions** | the activity feed read `data.message/content/text` — the backend populates `caption`; every event rendered as a bare type label | `caption`-first formatter |
| **Engine poll leak** | the 2.5 s `/api/engine` poll ran for the app lifetime once started | `stopEnginePolling` + unmount cleanup |
| **Unhandled rejections** | Lab/Research store actions rethrew to fire-and-forget callers | state carries the error; no rethrow |
| **Two engine-state sources** | the Start/Stop toggle read `models.llamaRunning` while the badge read `engine.state` — they could disagree | both follow the authoritative `engine.state` |
| **Model picker** | filename-only labels | GGUF metadata (quant, params) in the option labels |
| Dead exports | 12 legacy wrappers + a legacy WS client + dead types removed; unused deps (`clsx`, `lucide-react`) removed | — |

## 7. Dead code removed (verified no references before deletion)

`llm`: `stateCh`, `publishEvent`, `ApplyPreset`, `SwitchModel`, `LoadOrStartWithModel`, `EnsureRunning`, `ListRemoteModels` (+ `models.go`), 10 unused `ForTest` seams. `api`: `uploadTimeout`, `marshalAttachmentsSafe`, `parseIntDefault`. `config`: `ProMode`, `VerboseAgent` (meaningless settings). `browser`: `Session.Info`. `research`: `searxngMaxResults`. `logging`: `Recent`. `netcheck`: `Force`. `installer`: `lookPathImpl` indirection. `termshell`: `quote`. `tools`: import-keeper stubs. `cmd/stress`: two tautological scenarios + the hand-copied `simulateExtractJSON` (now uses the exported real parser). `humanBytes` ×3 → `internal/humanize`. `cmd` files reorganized to match their contents (`setup.go`, `logs.go`, `diagnostics.go`, `license.go`).

## 8. Release engineering

- **The release-job trap** (the single most dangerous defect in the pipeline): the release job was gated on hardcoded `refs/tags/v1.1.3Z` with hardcoded asset names — every version bump silently skipped release publication until someone hand-edited the workflow. Now: version-agnostic `v*` tag gate + a first step verifying `GITHUB_REF_NAME == v{APP_VERSION}Z`; all asset/artifact names derive from `APP_VERSION`.
- Version grep literals in the three build jobs derive from `APP_VERSION`/`$env:APP_VERSION` instead of hardcodes.
- `config.Save` atomic; `scripts/build-and-zip.sh` and launcher `.bat` versioned.

## Validation performed (this release)

```text
go build -tags headless ./...                 PASS
go vet  -tags headless ./...                  PASS
go test -tags headless ./internal/... -count=1          21 packages PASS
go test -race (config, agent, llm, api, sessions,
               attachments, contextcache)               PASS
frontend: npm ci / typecheck / lint (0 warnings) / build
          + sync into web/static                    PASS
stress suite (release gate)                    30 pass / 0 fail
node scripts/release-version.mjs --check        PASS (all surfaces 1.1.4)
cross-compile GOOS=windows CGO_ENABLED=0       PASS
ZIP package extracted to a clean directory and
re-validated (build + vet + tests + stress)    PASS
```

Known limitations (unchanged or newly documented):

- The Wails/GTK desktop build cannot compile in this environment (no GTK4/WebKit dev libs); the Windows CI job builds it (CGO-free cross-compile verified here).
- Vision (mmproj) path not exercised with a real projector model.
- Small instruct models may not emit formal tool calls; loop mechanics covered by deterministic tests.
- The API is loopback-only without an auth token — the OS user account is the trust boundary.
- The Lab command policy is lexical (denylists + env pinning), not a kernel sandbox.
- Agent tool calls execute sequentially by design.

---

# Historical: v1.1.3Z Implementation Log (AAA upgrade, 2026-09-04)

## What was implemented

1. Automatic engine lifecycle — `api.Server.EnsureSetup` calls `Stack.PrewarmLLM`; `handleRun` gates every run on `Stack.EnsureLLMContext` (bounded 3 min); cold starts stream engine transitions to the UI.
2. Engine state machine — spec states `idle/downloading/starting/ready/running/busy/stopping/stopped/failed`; `SubscribeEvents` fans transitions to the API layer; `MarkBusy` wired to inference windows; bounded watchdog auto-restart (3 attempts, 1/2/4 s backoff, stop-suppressed).
3. Attachments (`internal/attachments`) — content-addressed staging (sha256, symlink-safe, 0600), size/count/timeout/chunk caps, text normalization + stable chunk identity, lexical retrieval with provenance headers, image classification for the vision pipeline.
4. Context cache (`internal/contextcache`) — content-keyed LRU (entries + bytes), version + config-fingerprint keys, TTL, prefix invalidation, stats.
5. Long-context plan (`internal/contextplan`) — explicit budget (`numCtx` minus output reserve), sections system/tools/recall/attachments/history with priorities; tool specs measured exactly before windowing; overflow emits a visible error.
6. API surface — `GET /api/engine`, engine events on every activity WS, `/api/attachments` upload/list/inspect/delete, `/api/run` with attachment retrieval injection and regenerate.
7. Frontend — dead-composer fix (`running` resets on done/error), real conversation view, engine card on authoritative states, composer attachment tray.
8. Headless build tag — `desktop.go` gated `!headless`; `desktop/headless.go` serves the same stack; `go build/test -tags headless` works without GTK/WebKit.
9. Configuration correctness — local `EffectiveBaseURL` derives from `LlamaHost:LlamaPort` (legacy value migrates); default `numCtx` 8192 → 16384 (measured: briefing + full tool schemas ≈ 9.8k tokens).
10. Tests — contextcache/attachments/contextplan/llm (real spawn + fake engine re-exec)/agent (fake SSE)/sessions/api suites.

## Runtime verification performed (v1.1.3Z)

- Stub-engine e2e: 16/16 PASS — launch, auto-start, ready, session, upload, run with attachment, engine envelope, regenerate, cache stats, shutdown.
- REAL engine e2e: llama.cpp b10642 (linux x64, CPU) + Qwen2.5-0.5B/1.5B Instruct GGUF: automatic startup to ready (~5 s), real inference streamed and persisted, busy → ready, regenerate.
- The oversized-request failure (9854 tok vs 8192 ctx) was reproduced against the real engine, root-caused, fixed and re-verified.

---

## v1.1.6Z Phase 7 Implementation Log (2026-09-13)

### P0 #1 — llama.cpp launch contract repaired at the source (flash-attn / cache-reuse)

Root cause confirmed against the real Windows log: newer llama.cpp releases
changed `--flash-attn` from a bare boolean flag to the tri-state
`--flash-attn on|off|auto` option. The Phase 6 launcher emitted the bare
flag immediately before `--cache-reuse N`, so the new engine parsed the pair
as `--flash-attn '--cache-reuse'` and aborted, which silently dropped every
startup into compatibility mode 2 (no speed flags).

Implemented (`internal/llm/capability.go`, `speed.go`, `llama.go`):
- `EngineCaps` capability profile; detection via `llama-server --help`
  parsing (preferred) with a conservative release-tag fallback.
- Version-aware `SpeedArgsWithCaps` — bare flag for legacy engines,
  explicit `on`/`off` for value-form engines; `--cache-reuse N` remains an
  independent option; per-option capability strips.
- Pre-launch validation (`argProblems`) — the malformed pair can never be
  spawned; validation failure = loud error, no engine call.
- `ClassifyStartupFailure` maps engine stderr to typed failures
  (option-layout / unknown-option / model-arch / model-load / environment).
- Surgical repair (`launchWithRepair`): repair ONLY the offending option
  (layout flip / single-option drop), bounded to 2 attempts per compat
  level, retry at the SAME level; the generic "no speed flags" ladder is
  now only the last resort.
- Verified-profile persistence: `DataDir/engine-caps.json`, stamped with
  the verified-at time, keyed by release tag; downgrades recorded with
  reason.

Evidence: `TestEngineStartRepairsHistoricalRegression` reproduces the exact
historical error against a strict CLI-contract engine, then reaches READY
with `--flash-attn on` + `--cache-reuse 32` in the REAL argv and the
verified profile persisted. `TestSpeedArgsNeverProducesFlashAttnCacheReuse`
locks the regression shut.

### P0 #2 — context preflight budget pipeline (fit-guaranteed requests)

The "context overflow: fixed sections exceed usable window" path is now a
preflight repair pipeline, not a warning (`internal/agent/orchestrator.go`,
`internal/contextplan`):
- Model-aware effective context (`internal/llm/modelcaps.go`):
  min(configured numCtx, GGUF training limit, engine-reported limit);
  the launcher passes the effective value as --ctx-size.
- Safety margin (~1.5%, clamp 64..256 tok) held back on top of the output
  reserve; `Plan.PromptCeiling()` / `Plan.Overflow()` are the authority.
- Optional blocks (recall, project card, skills, staged attachments) are
  COMPOSED first, INJECTED only when the plan keeps them.
- Degradation ladder on fixed overflow: dynamic toolset reduction →
  compact system briefing → drop all optional blocks → honest refusal with
  NO engine call. Every step recorded on the plan.
- In-loop fit guard (`compactToolResults`) elides oldest tool results
  mid-turn; the final request is still guaranteed to fit.
- v1.1.3 double-count fixed: history section now measures history only.

### Phase 7 foundations wired

- 7A Dynamic toolsets (`internal/toolsets`): 10 capability groups,
  task-signal selection, budget-driven reduction inside the agent loop.
- 7B Skills (`internal/skills`): identity/trigger/procedure/tools/
  prerequisites/verification/failure-modes/evidence; load-on-demand,
  token-bounded injection; promotion ONLY on verified objective evidence.
- 7C Specialists (`internal/multiagent/specialists.go`): complexity-gated
  bounded consultations (researcher/architect/coder/debugger/tester/
  security) between planner and executor; ≤2 per run; critic unchanged.
- 7D Pipelines (`internal/pipeline` + `agent/pipeline_tool.go`): model
  declares a bounded stage plan once (≤12 stages, per-stage timeout,
  output caps); runtime executes deterministically; observable results.
- 7E Computer use (`internal/computer`): observe→inspect→act→observe→
  verify with deny-by-default risk policy, per-action timeout, action
  boundaries, result observation.
- 7F MCP (`internal/mcp`): optional stdio JSON-RPC bridge; guarded
  registration pipeline (discovery → classification → deny-by-default
  permission → schema validation → bounded execution); off by default.
- 8 Scheduler (`internal/scheduler`): event taxonomy (manual/startup/
  timer/file/git/test/CI/build/maintenance), bounded runs, persisted
  reports, memory summaries; manual/startup/timer implemented.
- 10 Context telemetry (`internal/ctxtelemetry`): per-turn tokens
  added/removed, retrieval latency + hits, compression ratio, pressure,
  tool-success rate, verification verdict; bounded JSONL + summary.
- 11 Self-improvement (`internal/improve`): prediction→outcome→verified
  tactic lifecycle; candidate → active ONLY with two independently
  verified predictions; unverified attempts change nothing.

Runtime wiring (`internal/runtime/runtime.go`): context-limit provider,
skills store, telemetry store, pipeline tool registration, scheduler +
timer loop. Version surfaces bumped to 1.1.6-zeta.


### Validation evidence (Phase 7, full re-run at the final tree)

| Stage | Command | Result |
|---|---|---|
| Build | `go build -tags headless ./...` | PASS |
| Full Go suite | `go test -tags headless ./internal/... ./cmd/... -count=1` | PASS (all packages) |
| Race detector | `go test -race -tags headless ./internal/agent/ ./internal/llm/ ./internal/api/ ./internal/native/engine/ -count=1` | PASS |
| Vet | `go vet -tags headless ./...` | PASS |
| Typecheck | `npm run typecheck` | PASS (0 errors) |
| Lint | `npm run lint` | PASS (0 warnings / 0 errors, 20 files) |
| Frontend build | `npm run build && npm run sync:web` | PASS (embedded bundle refreshed byte-identical) |
| Native build | `cmake -S native/engine -B native/engine/build && cmake --build` | PASS |
| Native tests | `ctest --test-dir native/engine/build --output-on-failure` | 12/12 PASS |
| Release gate | `node scripts/release-version.mjs --check` | PASS (all surfaces 1.1.6 / 1.1.6-zeta) |

### Real-model acceptance (native C++ boundary, REAL inference — not mocked)

`go test ./internal/native/engine/ -run TestPhase7SmallModelAcceptance -v`:

```text
acceptance: model card F32 · 256 ctx · 6 MB (arch=llama quant=F32 size=5959200)
acceptance: effective context 256 (configured 4096, model 256, engine 256)
acceptance: tokens=8 chunks=2 prompt=0.041s ttft=0.041s decode=0.011s
            (699.50 tok/s) finish=length
```

The acceptance proves the full small-model chain on REAL weights: the real
GGUF header's 256-token training limit OVERRODE the configured 4096 window
(the Phase 7 model-aware clamp working exactly as designed), the prompt fit
preflight, and real tokens streamed back through the production IPC
boundary with measured metrics. No compatibility-mode fallback, no
malformed llama arguments (the native path carries no llama.cpp CLI at
all), no false overflow.

The llama.cpp path itself is validated against a strict CLI-contract fake
engine (`TestEngineStartRepairsHistoricalRegression`): the exact historical
`--flash-attn` / `--cache-reuse` error is reproduced, classified, repaired
surgically, and the engine reaches READY with `--flash-attn on` +
`--cache-reuse 32` in the REAL argv and the verified profile persisted.
A genuinely small real llama.cpp GGUF run was not possible in this
environment (no prebuilt llama-server for this host); the fixture-based
contract validation is reported as such, distinctly from the REAL native
inference run above.

---

## v1.1.6-zeta Stabilisation Log (2026-09-13)

Task: 1.1.6 stabilisation / next update — CI deadlock fix, architectural
context-overflow protection, per-session + per-agent context, startup UX,
Windows icon/branding/theme, Settings scrolling, docs, validation,
replacement ZIP.

### P0 #1 — `internal/improve` Linux CI deadlock (FIXED)

- Root cause confirmed in `internal/improve/improve.go`: `Propose()` and
  `RecordAttempt()` locked `s.mu` and then called the locking `Load()` —
  `sync.Mutex` is not reentrant → the 10-minute test panic in
  `TestProposeRequiresCompleteDefinition` (Actions run 34758505188).
- Fix: `Load()` = lock + `loadLocked()`; `Propose()`/`RecordAttempt()`/
  `Active()` use `loadLocked()` under the already-held mutex. Save errors
  are now propagated instead of swallowed.
- Regression tests added: `TestProposeCompletesImmediately`,
  `TestRecordAttemptCompletesImmediately` (watchdog-guarded),
  `TestConcurrentStoreAccess` (8 writers × 12 verified attempts +
  concurrent readers; asserts no lost updates and correct promotion),
  `TestPersistenceRoundTrip`, `TestEvidenceBounded`,
  `TestStoreBoundedToMaxTactics`. All pass with `-race`.

### P0 #2 — Context overflow architecture (CLOSED)

- Per-session context policy: `sessions.Context.ContextTokens`, persisted
  with the session, editable per chat via `GET/PUT
  /api/sessions/{id}/context`; the run path passes
  `agent.WithSessionContext(...)` into `RunDetailed`.
- Resolution chain implemented exactly once, in `llm.ResolveSessionContext`:
  effective = min(session policy, global configured, model GGUF max,
  engine-verified window), floored at 1024, every clamp recorded.
- The engine's verified window (llama.cpp `/props` while alive; native
  loaded-model limit) feeds back into planning through the context-limit
  provider (`EngineContextLimit` returns 0 for a dead engine — a stale
  window can never clamp the next plan).
- Wire truthfulness: `BuildChatRequestWithOptions` sends the validated
  effective `n_ctx`, not the raw global value.
- Resource protection: `internal/llm/resources.go` classifies candidate
  windows (weights + KV-cache from GGUF facts + runtime overhead vs
  RAM/VRAM) into safe/caution/unsupported; the API rejects unsupported
  values with the reason; the selector ladder (4K…128K) exposes per-rung
  verdicts and model/engine clamp flags.
- Per-agent policies: `internal/multiagent/context.go` — per-role
  requested/minimum/maximum/outputReserve, resolved against the same
  limits, logged per consult, applied to planner/critic/summarizer/
  specialist requests; specialist activity captions show the resolved
  context.
- Telemetry: `TurnRecord` now carries the full decision trail
  (requested/effective/modelMax/engineMax/session policy, per-section
  token split, elided/compressed, rolloverTriggered, overflowPrevented);
  the orchestrator logs a structured `context plan:` block BEFORE the
  engine call; refusal gates set `overflowPrevented`.

### Startup / readiness / branding / Settings

- Engine snapshot gains `phase` (waiting → downloading-engine →
  loading-model → checking-capabilities → ready), `verified`,
  `verifiedModel`, `verifiedContext`, `degraded` — "ready" means verified
  serving; degraded startup is visible.
- UI: header CONTEXT selector + USED meter + status from the backend;
  first-use gate (No model selected / Choose model / Open models folder);
  model card facts; startup phase labels.
- Windows: `scripts/gen-syso` emits the full 16/24/32/48/64/128/256 ladder
  and `build/sheytan.ico`; the `.syso` stays build-time-generated (CI runs
  gen-syso). Branding: `document.title` per layer, "Go + Wails" footer
  removed. Theme: central token block in `styles.css` (surface/border/
  text/accent/…), settings.css undefined variables now resolve to real
  tokens. Settings scrolling: `.settings-page` is the app-level scroll
  container with a sticky solid toolbar and keyboard focus support.

### Validation (Linux, CI-equivalent)

- `go test -tags headless ./internal/... -count=1` PASS
- `go test -race -tags headless ./internal/improve/` PASS
- `go vet -tags headless ./internal/...` PASS
- `npm run typecheck`, `npm run lint`, `npm run build` PASS (embedded
  frontend regenerated)
- `node scripts/release-version.mjs --check` PASS
- native engine C++ build + ctest PASS
- Real Windows/model smoke: not executable in this environment (headless
  Linux); everything reproducible headlessly is green. Recorded in
  UPDATE.md § KNOWN LIMITATIONS.

---

## v1.1.7 — Options Clarity, Capability Truth, Live Telemetry, In-App Diagnostics

Date: 2026-09-13 · Base: main @ dd49eed (1.1.6Z)

### Compatibility-mode finding (the technical clue from the Windows log)

The user log showed the same line on every boot:

    started in compatibility mode 2 (no speed flags)

Root cause: `startLocked` persisted `engineCompat` after the first
successful boot at any level > 0. Every later boot resumed AT that level
and logged the line again — even when the Phase 7 capability adapter had
already detected/validated/persisted a profile under which the full-speed
(level 0) launch was valid. A one-time option rejection became a
permanent, silent speed downgrade with no recorded cause.

Fix (all in the existing engine system — no engine rewrite):
- `engineCompatReason` / `engineCompatAt` recorded in config when a boot
  settles above level 0; cleared at level 0 (backend-managed fields).
- `shouldRetryFullSpeed` grants ONE bounded full-speed retry when the
  recorded reason is option-class, a VERIFIED capability profile postdates
  the downgrade (or the upgrade case: no stamp), and the level-0 profile
  passes pre-launch validation. A failed non-option retry stamps a fresh
  time, re-blocking the gate until the profile changes — no rediscovery
  loops, no extra restarts in the common path.
- Boot logs now state the recorded reason; `/api/perf` exposes
  `compat: {level, name, optimised, reason, recordedAt, changes[]}`.
- Regression-locked by `internal/llm/capability_compat_test.go`.

### Shipped alongside (P0/P1, all reusing existing infrastructure)

- Settings → six scannable sections (General/Performance/Generation/
  Tools/Network/Logs), option tooltips (1–2 sentences, 400 ms delay),
  capability chips; restart-after-save now covers EVERY engine-affecting
  key (speed settings were previously saved but inert until manual
  restart).
- Concise one-line tool labels via `ShortDescription()`; the full
  model-facing `Description()` specs are untouched (they carry the JSON
  action syntax — shortening them would break orchestration).
- `GET /api/perf`: CPU/RAM/GPU-VRAM where measurable + prompt tok/s,
  generation tok/s, TTFT, context usage, backend, model, engine profile,
  recommended settings (explicit Apply only). N/A for anything
  unmeasurable; the 16-sample ring is fed by the existing tokenTimer.
- `GET /api/netcheck`: one bounded check → Excellent/Good/Unstable/Slow/
  Offline + first failure reason (cold-start TLS asymmetry handled).
- `GET /api/logs` + Settings → Logs viewer: live tail/pause/autoscroll/
  search/severity+subsystem filters/copy/non-destructive clear view over
  the existing ring (bounded); entries redacted before display (inline
  secret scrubbing added to `redact()`).
- Simple before/after observation (in-memory) when a performance option
  changes.

### Verification executed (automated only, per release policy)

- `go build ./...` (GOOS=windows, full tree) PASS
- `go vet ./internal/... ./cmd/... .` (GOOS=windows) PASS
- `go test ./internal/...`: 34 packages OK (incl. new tests); only
  `internal/desktop` fails to BUILD on the Linux host (Wails
  GTK4/webkitgtk dev headers absent) — pristine base fails identically;
  Windows cross-build of the same tree passes.
- `npm run typecheck` PASS · `npm run lint` PASS (0/0) · `npm run build`
  PASS (embedded web/static regenerated).
- Native C++ tests not re-run: no file under `native/engine/**` changed.

Package: SHEYTAN-Local-Agent-v1.1.7-UPDATE.zip (UPDATE.md §6 lists the
same results; §1–§5 carry the exact file map incl. DO NOT TOUCH).

---
Task ID: v1.1.8
Agent: Super Z (upgrade agent)
Task: v1.1.7 → v1.1.8 — CI release-identity fix, Chat/Agent separation, model picker, settings tabs, backend optimisation, minimalist pass

Work Log:
- Root-caused Actions run 34788709977: workflow env APP_VERSION="1.1.6" pinned at parse time while release-version.mjs repaired the tree to 1.1.7; greps also assumed a "-zeta" suffix package.json no longer carries.
- release-version.mjs: added --env mode (APP_VERSION / APP_VERSION_FULL / APP_CODENAME); replaced the workflow APP_VERSION target with a shape check for the runtime derivation marker.
- build-desktop.yml: removed hardcoded APP_VERSION/APP_CODENAME; audit job step "identity" derives them from package.json into $GITHUB_OUTPUT; build-windows/build-linux/release consume needs.audit.outputs.*; all version greps use runtime-derived values (no "-zeta" assumption); audit also runs release-version.mjs --check after sync.
- Version bumped 1.1.7 → 1.1.8 (package.json, package-lock, config.go, build/config.yml, SIGNATURE via the script).
- internal/api/server.go: modelCardFor now caches ResolveModelCapabilities alongside the GGUF card (bounded path+size+mtime cache) — removes per-poll per-model GGUF re-parsing; handleModels exposes multimodal/nativeBackend/chatTemplate/nativeReason/estimatedVRAMBytes; host memory probe hoisted out of the per-model loop. sessioncontext.go reuses the cached read.
- Frontend: WorkspaceMode (chat|agent) in store.ts (localStorage sheytan.mode, default chat); ModeSwitch segmented control in AgentHeader; AgentBody mode-aware (Chat = model rail + stream + composer; Agent = runtime panel + PerfStrip + activity); ModelPicker.tsx (measured facts only, honest RAM-based classification, no Remove); PerfStrip.tsx (4 s /api/perf polling, N/A contract); settings Models/Advanced tabs split; minimalist CSS pass (mode-switch, chat-rail, model-picker, perf-strip, calmer background grid/topbar).
- switchModel returns success; first-use DOM-query hack replaced by the picker.
- Docs updated: README v1.1.8 section, agent.md v1.1.8 notes, ARCHITECTURE I.8c, UPDATE.md rewritten.
- Checks executed: node scripts/release-version.mjs --check; go build/vet/test -tags headless ./... (33 packages ok); npm run typecheck/lint/build.

Stage Summary:
- v1.1.8 complete per the release brief; automated checks green; no architecture changes; native engine and all subsystems untouched.

---
Task ID: v1.1.9
Agent: release-implementation
Task: v1.1.9 — CI output fix, Chat/Agent separation completion, minimalist UI, model states, settings refinement, targeted optimisation, docs, ZIP delivery.

Work Log:
- CI run 34791882219 root cause: audit job outputs mapped steps.identity.outputs.version/version_full/codename while the identity step writes APP_VERSION/APP_VERSION_FULL/APP_CODENAME — needs.audit.outputs.* resolved empty. Fixed the mapping to the APP_* names and added a fail-fast ::error:: assertion when any identity value is empty (build-desktop.yml).
- Version bump 1.1.8 → 1.1.9: package.json edited once; internal/config/config.go, build/config.yml, SIGNATURE repaired by scripts/release-version.mjs (sync mode), then --check verified.
- Mode-aware navigation: workspace.ts WorkspaceLayer.modes + visibleWorkspaceLayers(); App.tsx filters nav (Chat hides Coding Lab) and falls back to the workspace when the restored hash points at hidden machinery; agent layer label "Workspace"; AgentSidebar eyebrow CHAT/AGENT.
- Model chooser redesign (ModelPicker.tsx): explicit ModelState ready/loading/incompatible/available; aligned Context/RAM/Tools/Vision/Native fact grid; Tools derived only from chatTemplate; "—" for unknown; "Choose a model" empty state; still no Remove (no deletion API).
- Settings: llama.cpp EngineCard moved Performance → Advanced (touchesEngine logic untouched).
- Minimalist pass (styles.css): --border-soft token defined (was referenced but undeclared); body gradients + grid overlay removed; shadows softened; .model-card-facts collision fixed via .runtime-model-facts rename; topbar status text → dot+tooltip; session-ID row and engine detail line removed from runtime panel; sidebar footer trimmed.
- Optimisation: PerfStrip skips /api/perf while document.hidden (+visibilitychange refresh); duplicated model <select> option builder unified in AgentBody.
- Docs: README v1.1.9 section, agent.md v1.1.9 notes, ARCHITECTURE I.8d, UPDATE.md rewritten for v1.1.9.
- Checks: release-version --check OK; typecheck OK; oxlint 0/0; npm run build OK (web/static regenerated); go test -tags headless ./... exit 0 (34 pkgs); go vet clean on first-party pkgs (Wails shell pkgs need GTK libs absent in sandbox); native C++: 11/12 test binaries pass (test_host hangs on slow fixture in sandbox; native/engine byte-identical to v1.1.8).
- Packaged SHEYTAN-Local-Agent-v1.1.9-UPDATE.zip (UPDATE.md + REPLACEMENT-MANIFEST.txt + REPLACEMENT-SHA256.txt + all changed files) and copied a byte-identical copy to the repository root.

Stage Summary:
- v1.1.9 complete per the release brief; automated checks green with two documented environment exceptions (GTK-dependent vet of third-party wails packages; sandbox hang of the untouched test_host); no architecture changes; Chat/Agent separation completed at the navigation layer; model states made explicit; engine tuning relocated to Advanced.

---
Task ID: v1.2.0
Agent: release-implementation
Task: v1.2.0 — SHEYTAN-LA unified product upgrade (vision readiness pipeline, hardware intelligence, recommendation engine, chat/composer polish, agent progressive disclosure, settings levels, Environment Centre, verified health, SHEYTAN-LA identity, installer, app updater, release factory, docs, ZIP delivery).

Work Log:
- Live audit: HEAD ac63b88 verified; two full audits (frontend + backend) mapped existing vision pairing (internal/vision), capability adapter, rAF streaming, updater (engine-only), workflow job graph, identity gaps (no AUMID, no NSIS, no checksums, no SHEYTAN-LA short name, gen-syso lowercase mismatch).
- Vision state machine: internal/vision/states.go (9 states, conservative mtmd arch allow-list, EvaluateModel evidence rules); llama.go records loading/ready/degraded/failed at pairing, verified boot, text-only retry and terminal failure; --no-mmproj-offload posture via config.VisionMMProjOffload (adapter-verified).
- /api/models gains per-model visionFields (state/reason/mmproj path/name/size/verified); /api/engine gains the runtime vision block (visionState/reason/projector/name/bytes/active).
- internal/hardware: measured-only profile over sysinfo + Vulkan/native/engine-tag evidence; PoolBytes mirrors llm.AssessContextResource.
- internal/recommendation: 7 task profiles; threads/ubatch/flash-attn/KV-quant/GPU posture from measured inputs; context clamped by model limit + verified engine window + resource ladder; explicit Reasons + Notes; predictions labelled predicted.
- internal/platform: AUMID Parsaetak.SHEYTAN-LA (windows syscall + honest ErrUnsupportedPlatform elsewhere); firewall manager with unit-locked netsh arg vectors, idempotent apply/remove/status, default = no rule (loopback-first).
- internal/updater/appupdate: release-manifest.json schema, version ordering incl. prerelease, SHA-256(+size)-verified staging under updates/staging; never executes artifacts.
- /api/environment (System Centre), /api/health (11 evidence-backed checks incl. honest unsigned-identity report), /api/recommendation, /api/update/status|check|download.
- Frontend: vision.ts badge helper; ModelPicker Text/Vision/Tools/Native/Context/RAM/VRAM grid + "Recommended for your device" + projector details + "Use recommended setup" onboarding; MessageStream markdown (react-markdown+remark-gfm+rehype-highlight, hand-rolled token theme), code copy, message copy, inline images, stick-to-bottom autoscroll + jump affordance; composer Enter/Shift+Enter, auto-grow, paste + drag/drop through the existing upload path, image previews (object URLs), honest vision gating hint; ActivityStream phase grouping (Plan/Execution/Repair/Verification/Run) with Raw toggle; Settings Performance three levels (Simple postures / measure-recommend-verify / Advanced pointer), new Vision and Updates tabs; System workspace view (SystemPanel) wired through workspace.ts + App.tsx.
- Identity: gen-syso ProductName SHEYTAN-LA / FileDescription "SHEYTAN Local Agent" / CompanyName Parsaetak / SHEYTAN-LA.exe; desktop title + AUMID; SHEYTAN-LA.bat launcher (legacy fallback); build/config.yml identity; CI builds SHEYTAN-LA.exe, portable zip renamed SHEYTAN-LA-v<ver>-windows-x64.zip.
- Release factory: NSIS installer job (packaging/nsis/installer.nsi — per-machine, AUMID, SHEYTAN_DATA_DIR, clean uninstall preserving data), installer verification, SHA256SUMS.txt, release-manifest.json generation + validation, publish-after-verification, release asset re-verification (zip + installer + checksums + manifest).
- Version 1.1.9 → 1.2.0 via package.json + release-version.mjs (config.go, build/config.yml, SIGNATURE repaired; --check green); build-and-zip.sh now derives VERSION from package.json (was hardcoded 1.1.4).
- Docs: README v1.2.0 section + SHEYTAN-LA header; ARCHITECTURE Part II.9; agent.md v1.2.0 next-agent notes; worklog (this entry); UPDATE.md rewritten for v1.2.0.
- Checks executed: release-version --check OK; go test -tags headless ./... 36 pkgs ok; go vet clean; npm typecheck OK; oxlint 0/0; npm run build OK (web/static regenerated, SystemPanel/vision chunks present).
- Known limitations (documented, not faked): no Authenticode signing (certificates unavailable; pipeline reports status honestly); MSIX/AppInstaller not produced; firewall apply/verify and AUMID syscall are Windows-only paths exercised by construction + unit-locked arg vectors on Linux; CI not triggered from this environment (no push access).

Stage Summary:
- v1.2.0 complete per the release brief: strongest real subset implemented, all existing subsystems preserved (rAF streaming, capability adapter, session/attachment/lab/research/continuum machinery untouched), no second runtime/telemetry/frontend/database introduced.

---
Task ID: v1.2.1
Agent: release-implementation (Super Z, 2-hour implementation session)
Task: v1.2.1 — Linux ZIP verification fix at the root (canonical package-root contract), installer desktop-shortcut option + clean upgrade/uninstall, bounded stress expansion, workflow optimisation, UPDATE ZIP delivery.

Work Log:
- Live audit of main @ e260d53 (v1.2.0): confirmed the failing Linux job (run 34871838054) is a ZIP root naming mismatch — the ZIP is created under SHEYTAN-Local-Agent/ while "Verify Linux ZIP" and the release job's payload verification expect SHEYTAN-LA/ entries; deeper: internal/releasecontract.RequiredLinuxZipEntries() itself mixed roots (WindowsAppRoot entries in the Linux list), which is why the stress gate stayed green while CI failed (two drifting copies of the truth). Confirmed the installer already had MUI_PAGE_DIRECTORY + WriteUninstaller and that the desktop shortcut was unconditional.
- Canonical package-root contract: workflow env gains WIN_PKG_ROOT="SHEYTAN-LA" + LINUX_PKG_ROOT="SHEYTAN-Local-Agent"; every staging dir, ZIP name, ZIP entry check, artifact name and release-metadata reference now derives from them (no duplicated root literals left — verified by grep). internal/releasecontract rewritten: WorkflowWinRootEnvLine/WorkflowLinuxRootEnvLine, ${env:WIN_PKG_ROOT}/${LINUX_PKG_ROOT} slot spellings, WindowsAppDirWorkflowSlot/LinuxAppDirWorkflowSlot, RequiredWindowsZipWorkflowEntries/RequiredLinuxZipWorkflowEntries; RequiredLinuxZipEntries fixed to the Linux root. cmd/stress_zeta gate strengthened (3e2 requires the env lines; 3h/3i require the slots + parameterized spellings; staging check moved to slots).
- Installer (packaging/nsis/installer.nsi): desktop shortcut became a checkbox on the directory page (nsDialogs in MUI_PAGE_CUSTOMFUNCTION_SHOW, default CHECKED via .onInit, state persists across Back/Next, silent installs keep the default); upgrades close a running instance (graceful taskkill → bounded 3×500ms retry on the locked exe → explicit Retry/Cancel), SetShellVarContext all for install+uninstall symmetry; uninstall preserves user data by construction (plain RMDir only; models/workspace/sessions/config + SHEYTAN_DATA_DIR untouched); fixed the latent developer-path default BUILDDIR pointing one level short of the repo root (packaging/nsis/../../dist now).
- Stress expansion (cmd/stress_release.go, wired into runStressSuite): 14 bounded scenarios — update_offline_manifest, update_tampered_sha, update_size_mismatch, update_staged_drift, update_zip_slip_members (loopback-only httptest servers), vision_missing_projector, vision_incompatible_arch, memory_parallel_appends_unique, memory_delete_missing_id, sessions_restart_persistence, sessions_garbage_ids, contextcache_bounded_entries, contextplan_huge_history_bounded, recommendation_extreme_inputs, loopguard_retry_ceiling (uses exported agent.LoopGuard.WallClockExhausted). Suite now ends with a machine-readable STRESS-RESULT line.
- Workflow optimisation: job-level timeout-minutes (audit 40 / windows 60 / linux 50 / release 20), native engine build cached on hashFiles('native/engine/**') in audit + linux, in-package BUILD-INFO version verification inside both portable ZIPs, NSIS SOURCE contract verification step (directory page, checkbox, WriteUninstaller, UninstallString, SHEYTAN_DATA_DIR, preserve user data; forbidden RMDir /r) before makensis runs.
- Version 1.2.0 → 1.2.1 via package.json + release-version.mjs (config.go, build/config.yml, SIGNATURE repaired; --check green). The app updater needs a strictly higher version to offer the improved installer.
- Docs: README v1.2.1 section + header, agent.md v1.2.1 next-agent notes, ARCHITECTURE Part II.10, worklog (this entry), UPDATE.md rewritten for v1.2.1.
- Checks executed: node scripts/release-version.mjs --check OK; npm ci + typecheck + lint (0/0) + build OK (web/static regenerated byte-identical); go test ./internal/... -tags headless 36 pkgs OK + ./cmd OK (internal/desktop build skipped: sandbox lacks GTK4/WebKitGTK headers — pristine base fails identically, documented); GOOS=windows CGO_ENABLED=0 go build ./... PASS (CI-parity shipped binary); GOOS=windows go vet ./... full tree clean; cmake+ctest native engine 12/12 PASS; stress suite 45/45 (0 hangs, 0 crashes); NSIS installer compiled with CI-parity flags (VERSION=1.2.1, absolute BUILDDIR/OUTFILE) and structurally validated (MZ/PE headers, UTF-16 version resources ProductName=SHEYTAN-LA ProductVersion=1.2.1, NullsoftInst signature) — the Linux makensis port runs under an LD_PRELOAD path shim (compiled-in /usr/share/nsis redirected into a user prefix), error text prints only on Windows hosts.
- Packaged SHEYTAN-Local-Agent-v1.2.1-UPDATE.zip (UPDATE.md + REPLACEMENT-MANIFEST.txt + REPLACEMENT-SHA256.txt + all changed files) and copied a byte-identical copy to the repository root.

Stage Summary:
- v1.2.1 complete per the 2-hour brief: CI contract fixed at the source with a stronger two-sided gate, installer options and uninstall boundaries contractual, bounded stress suite expanded and green, packaging gates hardened, no feature work, no architecture changes, user data preservation verified by construction.

---
Task ID: v1.2.2
Agent: repair-implementation (Super Z, 2-hour focused repair sprint)
Task: v1.2.2 — black-screen/System-tab crash root fix, live generation timeline, tab-switch mid-run stability, runtime/log/engine reliability.

Work Log:
- Reproduced the failure set by code-auditing the v1.2.1 tree (no runnable Windows host in the sandbox — every root cause below is code-evidence-based, each pinned to the exact lines): (A) ZERO React error boundaries — any render exception unmounted the whole tree (black screen); (B) SystemPanel DeviceCard read `device.gpus.length` while hardware.Collect/sysinfo.Probe marshal a nil Go GPU slice as JSON `null` (WMI/PowerShell GPU probes fail under heavy inference load → the System tab crashed DURING generation); (C) App.tsx keys the workspace by view → AgentBody unmount called disconnectActivity() mid-run; the backend activityHub has NO event replay, so `done` was lost → `running` stuck true forever → frozen composer; the backend's `idle` sentinel was never handled; (D) the orchestrator's emitProgress publishes CUMULATIVE response/reasoning captions while the store APPENDED every caption → massively duplicated streamed text; (E) no lifecycle surface (dead gap between Send and first token), streaming reasoning not collapsible; (F) done/error/abort discarded partial output; (G) llamaDownloadURL hard-coded "upstream no longer publishes Linux binaries" into every failure — misleading on GitHub-blocked Windows machines; (H) app.log mixes sessions/versions with no separator.
- Error boundaries: new src/ErrorBoundary.tsx (AppErrorBoundary around <App/> in main.tsx; PanelErrorBoundary per lazy panel in App.tsx with resetKey clearing stale failures on view switch; recover/reload actions).
- Wire contract fix: hardware.Collect initializes GPUs to []GPU{}; sysinfo.Probe nil-guards info.GPU (tab-formatted, minimal diff). Frontend defends in depth (Array.isArray guards across DeviceCard/RuntimeCard/RecommendationCard/HealthCard).
- store.ts rework: cumulative-snapshot streaming via new pure src/stream-accumulator.ts (replace semantics — replayed frames after reconnect are idempotent by construction; the old append path also caused quadratic string growth); run lifecycle machine in pure src/run-phase.ts; acquireActivity/releaseActivity + acquireEnginePolling/releaseEnginePolling refcounted ownership (a live run holds the socket+poll across tab switches; release exactly when the run settles and the last consumer is gone — no duplicate sockets, no leaks); `idle` sentinel handling with resync from authoritative history (recoverRunFromIdle, 2.5s grace guard against the standby attach race, per-run event-evidence tracking); finalisation confirms the history actually gained the assistant reply and otherwise promotes the streamed partial locally (errors/aborts never lose output); abort unlocks the composer immediately with a bounded fallback finalisation; abort captions ("Aborted by user"/"Run stopped…") settle the timeline as Stopped, not Complete.
- MessageStream.tsx: GenerationBubble mounts the instant a run starts (running || live phase — no dead gap), phase badge + elapsed clock + collapsible reasoning (ReasoningPanel: auto-open while thinking, auto-fold when the answer streams, user intent wins via onToggle) + streamed text + progressive tool/context/engine activity strip; completed messages keep their reasoning panel; RunOutcomeNote shows the settled outcome with the backend's own caption; autoscroll follows reasoning and phase too.
- SystemPanel.tsx hardening: defensive reads everywhere, AbortController-cancellable environment/health fetches (api.environment/api.health accept a signal), unmount-safe recommendation apply.
- Engine honesty: updater.IsNoAssetError classifies no-asset vs network; llamaDownloadURL reports platform-aware messages with real remedies + WARN log; GitHub/Atom failures remain non-fatal.
- Logging: Manager.SessionBanner (wired in cmd/root.go before the boot line) — every process start writes `==== SHEYTAN-LA v1.2.2 session start (pid N) ====` through the ring/rotation pipeline (visible in the in-app Log Viewer); old v0.8.0 entries above a banner are verifiably historical.
- Regression tests: src/run-phase.test.ts + src/stream-accumulator.test.ts (20 assertions, node --test native TS, `npm run test:units`); Go: internal/hardware/hardware_test.go (gpus never marshals null), internal/logging/logging_sessionbanner_test.go (banner markers + noop safety), internal/updater/updater_noasset_test.go (no-asset vs network classification). tsconfig.app.json excludes *.test.ts from the app build; no new dependencies.
- Version 1.2.1 → 1.2.2 via the identity chain (package.json → release-version.mjs --check green: config.go, build/config.yml, SIGNATURE repaired).
- Checks executed: release-version --check OK; npm ci; typecheck OK; oxlint 0/0 (33 files); build:web OK; sync:web regenerated web/static; npm run test:units 20/20 PASS. NOT executed (no Go toolchain in this sandbox — documented honestly): go build/vet/test could not run; the three new Go test files are provided for CI to execute; frontend E2E against a live backend could not run.
- Diff hygiene: whitespace-only reformatting reverted (workspace.ts, api.ts and AgentBody.tsx re-patched minimally; tab-indented Go files reconstructed from HEAD with surgical re-application — final Go diff is 23 insertions/1 deletion across 3 tab-style files + focused edits in the space-style files).
- Packaged SHEYTAN-Local-Agent-v1.2.2-UPDATE.zip (UPDATE.md + REPLACEMENT-MANIFEST.txt + REPLACEMENT-SHA256.txt + all changed/new files including regenerated web/static) and copied a byte-identical copy to the repository root.

Stage Summary:
- v1.2.2 complete per the 2-hour repair brief: every reported symptom traced to a code-level root cause and fixed at the source (boundaries + wire contract + ownership lease + replace-semantics + honest errors + session banners); stability and generation visibility first; no unrelated features; no architecture rewrite. Go-side verification remains pending CI (no toolchain in this environment).

---
Task ID: v1.2.3
Agent: maintenance-implementation (Super Z)
Task: v1.2.3 — Linux CI unload-guard race fixed at the root, reusable Download Manager, async verified app-update staging, downloader progress UI, UX coherence pass.

Work Log:
- REPRODUCED the CI failure locally before touching anything: a targeted
  stress repro (scripts/repro_unload_race.cpp pattern) hit the unload-guard
  race 56/200 times — the sequencer observed the scheduler's active slot
  while the engine's guard counted `active_generations` (set later, inside
  the executor), so unload succeeded and tore the model from under an
  in-flight generation (generation then failed with SHTN_ERR_NO_MODEL).
- ENGINE FIX — native/engine/src/engine.cpp: one authority,
  `generation_in_flight()`, covers the whole in-flight window (registered
  work map non-empty + scheduler active slot + executor count) for BOTH
  shtn_engine_load_model and shtn_engine_unload_model. Observable state can
  no longer contradict the guard. Post-fix repro: 0/1200.
- TEST FIX — test_generate.cpp unload-guard block rewritten: explicit
  first-streamed-token signal (mutex+condvar, 60 s bounded), scheduler
  coherence assert (active_requests==1), unload AND reload must both be
  rejected, the blocked generate must return SHTN_ERR_CANCELLED with
  finish_reason "cancelled", scheduler totals consistent, engine reused
  afterwards. Deterministic: 10/10 idle, 5/5 niced, 3/3 under CPU
  saturation; full ctest 12/12.
- WARNINGS — dead `utf8_encode` (tokenizer.cpp) removed; unused `nbytes`
  local (tensor.cpp) removed; clean rebuild emits zero warnings, no
  suppression pragmas.
- DOWNLOAD MANAGER — new internal/downloader package (downloader.go /
  transfer.go / meta.go + downloader_test.go): HTTPS-only (enforced across
  redirects; loopback exemption for test doubles), persistent pooled
  transport, single-stream by design, stream-to-`.part` → SHA-256/size
  verify → atomic rename + dir fsync, `.bak` rollback preservation,
  HTTP-Range resume (200-fallback restarts honestly), ordered sources
  primary/mirror/fallback with explicit AllowFallback gate, TTL'd source
  cache (sheytan-source-cache), bounded backoff+jitter, permanent-4xx
  no-retry, cancel (unblocks in-flight Read via attempt context) and
  pause/resume, 4 Hz throttled progress with speed EWMA + ETA. 14 tests,
  race-clean ×3.
- INTEGRATION — llm.ensureBinary + engine self-update
  (updater.UpdateEngineWithProgress) + scheduled updater downloadEngine +
  app staging (updater.StageAppUpdate(WithProgress) via
  AppUpdateStagingJob) all run on the manager; verified-archive-only
  extraction; llamaDownloadURL unchanged (its honest classification from
  v1.2.2 remains).
- API — /api/engine + WS engine frames carry `download` progress;
  POST /api/llama {action:"cancel-download"}; /api/update/download is
  ASYNC (immediate return, staging goroutine, single-slot claim,
  15-min cap), /api/update/cancel added, Close() cancels in-flight
  staging; AppUpdateStatus carries `download`.
- FRONTEND — src/DownloadProgress.tsx (phase chain, determinate bar, %,
  speed, ETA, source+trust, verification badge, retry reason,
  Cancel/Retry; role=status aria-live=polite) + `.dl-*` styles on the
  canonical tokens (reduced-motion collapse applies); wired into
  UpdatesCard (1.2 s status poll while downloading, async start, cancel,
  retry-after-failure), AgentBody runtime panel (compact + Stop download)
  and SystemPanel RuntimeCard; AgentHeader pill shows live %; store gained
  modelsLoading so the model picker renders skeletons instead of a fake
  empty state.
- STRESS — two new scenarios: downloader_integrity_resume (Range resume →
  verified activation → no residue) and downloader_untrusted_fallback
  (fallback never contacted without opt-in; checksum refused).
  47/47 scenarios green.
- VERSION — 1.2.2 → 1.2.3 via the identity chain (package.json →
  release-version.mjs repaired config.go / build/config.yml / SIGNATURE).
- CHECKS EXECUTED — ctest 12/12 (0 warnings); go build/vet + full
  internal suite + root tests green (38 ok packages); stress 47/47;
  npm run typecheck / lint (0/0) / build + sync:web green; downloader
  tests race-clean ×3; unload race repro 0/1200 post-fix.
- DIFF HYGIENE — workspace-style gofmt reformatting reverted; every Go
  file re-patched in the repo's existing indentation (space-style files
  space-indented, tab-style files tab-indented); final Go diff is surgical
  (llama.go +221/-? focused, appupdate.go ~111, updater.go ~77,
  stress_release.go +137, api files ~+63/-7).
- Packaged SHEYTAN-Local-Agent-v1.2.3-UPDATE.zip (UPDATE.md +
  REPLACEMENT-MANIFEST.txt + REPLACEMENT-SHA256.txt + changed/new sources +
  regenerated web/static) and copied a byte-identical copy to the
  repository root; both ZIPs' identity verified by SHA-256.

Stage Summary:
- v1.2.3 complete per the brief: the failing CI condition reproduced and
  fixed at the source (engine guard contract + deterministic test — not by
  weakening CI), one reusable verified download mechanism for every remote
  asset with honest measured progress in the UI, async cancellable
  app-update staging, a coherent downloader UX across panels, warnings
  cleaned, all suites green, update package built and verified.
