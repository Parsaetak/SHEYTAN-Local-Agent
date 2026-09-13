# SHEYTAN™ Local-Agent

> **A local-first AI software-engineering laboratory.**
>
> The model proposes. The tools execute. The laboratory verifies.

SHEYTAN™ Local-Agent is a local-first desktop AI engineering environment built around Go, React/TypeScript, Wails v3, managed llama.cpp inference, controlled tools, isolated coding workspaces, research, memory, recall, and objective verification.

**SHEYTAN™ is a trademark of Parsaetak · © 2024–2026 Parsaetak. All rights reserved.**

Licensed under the **Parsaetak Proprietary License v1.1** (see `LICENSE`).

```text
Application:      SHEYTAN-Local-Agent
Current release:  v1.1.6Z
Codename:         Zeta
Branch:           main
```

---

# What SHEYTAN is

A single Windows-first desktop application that:

1. **Manages its own inference engine** — a llama.cpp server is downloaded, launched, health-checked, supervised (bounded auto-restart) and updated automatically. No manual engine babysitting.
2. **Runs a real agent loop** — plan → tool calls → observations → verification → final answer, with streaming, cancellation, retries, per-run time budgets and loop prevention.
3. **Executes engineering work in an isolated Coding Lab** — workspace copies, shell/network/dangerous-command policy, objective verification gates, repair loops, snapshot-before-promote.
4. **Treats long context as an engineering problem** — measured context budgets, structure-aware (paragraph-boundary) chunking, a content-keyed cache, provenance-tagged retrieval, recall of past exchanges, and automatic chapter rollover for sessions that outgrow their window.

The model is never the authority on whether an engineering task succeeded — objective verification is.

# Architecture

```text
┌─────────────────────────────────────────────┐
│              React / TypeScript             │
│                  Vite UI                    │
└──────────────────────┬──────────────────────┘
                       │ REST + WebSocket (127.0.0.1)
                       ▼
┌─────────────────────────────────────────────┐
│                   Go API                    │
└──────────────────────┬──────────────────────┘
                       ▼
┌─────────────────────────────────────────────┐
│                 Go Runtime                  │
│  agent orchestrator · tool registry (18)   │
│  engine backend contract · llama.cpp      │
│  lifecycle · sandbox governor ·           │
│  attachments · chunking · context cache    │
│  context plan · memory · recall · continuum│
│  research · sessions · browser · vision    │
└──────────┬──────────────────────┬─────────┘
            │                      │
            ▼                      ▼
┌───────────────────┐   ┌───────────────────────────────┐
│  llama.cpp server │   │  SHEYTAN Native Engine (new)  │
│  local inference  │   │  C++ core + supervised host   │
│  (fallback and    │   │  (Phase 5: lifecycle, health,│
│   default engine  │   │   hardware, metrics, GGUF     │
│   for unsupported │   │   model loading, tokenizer, KV│
│   cases)          │   │   cache, scheduler, REAL      │
│                   │   │   llama-arch generation with  │
│                   │   │   streaming + cancellation)   │
└───────────────────┘   └───────────────────────────────┘
```

Critical execution logic belongs to Go. Presentation and interaction logic belong to React. The production desktop app embeds the built frontend (`web/static/`) via `go:embed` — no separate frontend server is needed.

## Phase 7 (v1.1.6Z) — Runtime Stability, Context Intelligence, Agent OS Foundation

**Implemented and tested in this release:**

| Area | What it does |
|---|---|
| **Engine capability adapter** | The launcher no longer guesses the llama.cpp CLI contract. It detects the installed engine's real surface (`--help` parsing, release-tag fallback), validates every option/value pair BEFORE spawning, and — when the engine still rejects an option — classifies the failure from the engine's own stderr and repairs ONLY that option (e.g. the historical `--flash-attn` flag→`on\|off\|auto` layout change), retries at the same profile, and persists the verified capability profile (`engine-caps.json`). A single option mismatch can no longer silently drop the engine into compatibility mode |
| **Model-aware context** | One `ModelCapabilities` object per loaded model (architecture, quantisation, parameters, GGUF context limit, tokenizer family, chat-template support, multimodal pairing, native-backend verdict, RAM/VRAM estimates, recommended context + generation budget). The effective context is the MINIMUM of configured `numCtx`, the GGUF training limit and any engine-reported limit — a small model never gets a window bigger than it has |
| **Preflight budget pipeline** | Every request is assembled through one authoritative budget: effective window → output reserve → safety margin → tool schemas → system briefing → project intelligence → skills → recall → attachments → history. Optional blocks are injected only when the plan keeps them; fixed-section overflow triggers an automatic degradation ladder (dynamic toolset reduction → compact briefing → drop optional blocks) and, if the budget is still impossible, the run refuses BEFORE any engine call. The prompt can no longer intentionally exceed the model context |
| **Verified startup state machine** | `/health` 200 proves the process serves — the engine then verifies the model is actually served (`/v1/models`) and the context capability is real (`/props`), recording both honestly |
| **Dynamic toolsets (7A)** | Tools are grouped into capability groups (coding, research, verification, filesystem, git, browser, computer, system, data). Under tool-schema pressure the agent receives only the task-relevant subset — the largest fixed context cost adapts to the task |
| **Skills (7B)** | Local skill store (identity / trigger / procedure / tools / prerequisites / verification / failure modes / evidence), load-on-demand with token-bounded injection. VERIFIED-LEARNING RULE: a successful execution never becomes a skill automatically — promotion requires an objective `verified` verdict |
| **Specialist agents (7C)** | Complexity-gated bounded consultations (researcher, architect, coder, debugger, tester, security) between planner and executor — ≤2 per run, every consultation time- and token-bounded; the critic still requires objective evidence |
| **Programmatic pipelines (7D)** | The `pipeline` tool executes a model-declared, bounded deterministic stage plan (inspect → build → test → verify) without regenerating every intermediate call; per-stage timeouts, output caps, stage bound, observable results |
| **Computer use (7E)** | Unified observe → inspect → act → observe → verify abstraction with a deny-by-default risk policy (read-only / interactive / destructive), per-action timeouts, action boundaries and result observation |
| **MCP bridge (7F)** | Optional stdio JSON-RPC adapter for external MCP servers with a guarded registration pipeline (discovery → classification → deny-by-default permission → schema validation → bounded execution). Off by default; never blindly exposes external tools |
| **Scheduler (8)** | Local event/task foundation: manual / startup / timer triggers implemented (file/git/CI triggers declared for future emitters), bounded runs, persisted reports, memory summaries |
| **Context telemetry (10)** | Per-turn measurement of tokens added/removed, retrieval latency and hits, compression ratio, pressure, tool-success rate and the verification verdict — answering "which context actually helped" |
| **Self-improvement (11)** | Prediction → outcome → verification tactic lifecycle: candidates never guide planning; two independently verified predictions activate a tactic; one verified contradiction retires it |

## 1.1.6-zeta Stabilisation — Context Control, Startup UX, Branding (IMPLEMENTED)

The 1.1.6 stabilisation phase fixes the `internal/improve` CI deadlock at
its source (non-reentrant locking: `loadLocked()` under an already-held
mutex, with concurrency/persistence/bounding regression tests) and closes
the context-overflow path architecturally. The core invariant: **SHEYTAN
may offer large context windows, but it never sends a request that exceeds
the model's verified usable context or the machine's safe resource
budget.**

| Feature | State | Where |
|---|---|---|
| Recursive-mutex fix + concurrency tests (improve store) | **IMPLEMENTED** | `internal/improve` |
| Per-chat (session) context policy, persisted with the session | **IMPLEMENTED** | `sessions.Context.ContextTokens`, `GET/PUT /api/sessions/{id}/context` |
| Context resolution chain: session policy → global → model GGUF max → engine-verified window | **IMPLEMENTED** | `llm.ResolveSessionContext`, runtime provider (llama `/props` window while alive + native limit) |
| Guaranteed-fit overflow architecture (plan → degrade → refuse before engine call) | **IMPLEMENTED** (Phase 7 core, extended with per-session policy + wire `n_ctx` truthfulness) | `internal/agent`, `internal/contextplan`, `llm.BuildChatRequestWithOptions` |
| Per-agent context policies (planner 16K, coder 32K, researcher 8K, …) with resolution + logging | **IMPLEMENTED** | `internal/multiagent/context.go` |
| Resource-aware large-context classification (safe/caution/unsupported, KV-cache estimate, explained rejections) | **IMPLEMENTED** (pre-flight estimates, not runtime guarantees) | `internal/llm/resources.go` |
| Full per-turn context telemetry + structured `context plan:` log before the engine call | **IMPLEMENTED** | `internal/ctxtelemetry`, orchestrator |
| Context selector + usage/pressure in the chat header (4K…128K, model-valid options only) | **IMPLEMENTED** | `src/AgentHeader.tsx`, store/api |
| Startup progress states + verified-ready semantics (`ready` requires verified serving; degraded visible) | **IMPLEMENTED** | `internal/api/engine.go`, `src/AgentBody.tsx` |
| First-use path (No model selected → Choose model / Open models folder; model card facts) | **IMPLEMENTED** | `src/AgentBody.tsx`, `/api/models/open-folder` |
| Windows icon (16/24/32/48/64/128/256) at the resource layer + `build/sheytan.ico` | **IMPLEMENTED** (syso regenerated by CI via `scripts/gen-syso`) | `scripts/gen-syso`, `.gitignore` |
| Window/layer branding (`SHEYTAN`, `SHEYTAN — Settings`, …; no generic Wails/localhost strings) | **IMPLEMENTED** | `src/App.tsx`, `internal/desktop` |
| Central theme-token system (surface/border/text/accent/…; dark-first, light-mode foundation) | **IMPLEMENTED** (light theme values are FOUNDATION — not designed yet) | `src/styles.css`, `src/settings.css` |
| Settings scrolling (dedicated scroll container, sticky toolbar, keyboard + wheel + scrollbar) | **IMPLEMENTED** | `src/settings.css`, `src/SettingsPanel.tsx` |
| Real-model Windows acceptance run (gemma-4-E2B-it-Q4_K_M desktop smoke) | **PLANNED** (requires a Windows box; headless validation covers the pipeline and engine state machine) | — |

## SHEYTAN Native AI Engine (v1.1.5Z, Phase 5 — REAL native inference)

v1.1.5Z establishes the **SHEYTAN Native AI Engine architecture**: Go
remains the main application/runtime engine, and a C++ native engine
(`native/engine/`) performs **real transformer inference** for the llama
architecture behind a narrow C ABI, supervised by Go as a subprocess
(`shtn-engine-host`) over a length-prefixed JSON IPC protocol.

**Status — read this literally:**

- **IMPLEMENTED**: the backend abstraction (`internal/llm` `Backend`
  contract with Start/Stop/Health/LoadModel/UnloadModel/Generate/
  StreamGenerate/Cancel/ModelInfo/HardwareInfo/Metrics), backend
  selection with automatic fallback to llama.cpp, the supervised native
  engine lifecycle (start / health-check / mark ready / stop / detect
  failure / bounded restart), the platform-neutral hardware profile, the
  native metrics snapshot, and — since Phase 2 — **native GGUF model
  loading**: a bounds-checked, overflow-safe C++ GGUF reader
  (header/version/metadata/tensor-table validation), memory-mapped model
  access (lazy — tensor data is never copied into RAM), real metadata
  extraction (architecture, parameter count, context length, vocabulary
  size, embedding length, layer count, quantization, tensor count, file
  size), a load-time memory plan (file/mapped/weights/workspace/KV-cache
  estimates/runtime overhead — computed, never allocated), the model
  lifecycle (`unloaded` / `loading` / `loaded` / `failed`) with
  replace-semantics and clean unload, and the `ModelInfo` surface wired
  through the shared `llm.Backend` contract. Phase 4 added **real
  foundation primitives**: a real GGUF-backed tokenizer (BPE/Unigram/WPM
  with merges, special tokens, BOS/EOS/UNK — materialized by re-walking
  the memory map on demand), a real KV-cache data structure (sized from
  real model dims, GQA-aware, contiguous K+V allocation, capacity/usage/
  used-positions measured honestly — since Phase 5 populated by the real
  forward pass), a real bounded scheduler (single-slot, FIFO,
  queue cap, cancel, drain, no busy poll), real sampling primitives
  (greedy/temperature/top-k/top-p/repetition penalty/seedable RNG —
  deterministic), streaming UI coalescing (rAF-boundary batching of token
  chunks — one setState per frame regardless of token rate), and a
  frame-budget diagnostic perf HUD (auto-detects the display refresh
  rate; reports `optimized for high-refresh displays / frame-budget
  aware / 120 Hz-capable presentation where hardware permits` — never a
  guaranteed-120-FPS claim).
- **IMPLEMENTED (Phase 5 — REAL native inference)**: in addition to the
  foundation above, the real llama-architecture forward pass
  (embeddings → per-layer RMSNorm → Q/K/V matvec → RoPE → causal GQA
  attention over a true fp16 KV cache → SwiGLU FFN → final norm →
  logits), a tensor access layer with row dequantization for
  F32/F16/Q4_0/Q4_1/Q5_0/Q5_1/Q8_0 (anything else fails explicitly),
  load-time llama-graph validation (`GenerationCapable()` = alive +
  loaded + validated — never blindly true), REAL token-by-token
  generation with streamed chunks, cooperative cancellation
  (per-token observation), measured generation metrics (TTFT / tok/s /
  KV positions from the monotonic clock), and the generation router:
  native when selected AND capable AND plain-text, llama.cpp otherwise
  with a logged, inspectable reason.
- **Narrow, honest native support**: llama architecture ONLY;
  F32/F16/Q4_0/Q4_1/Q5_0/Q5_1/Q8_0 tensors ONLY; rope.freq_scale 1.0
  ONLY; a plain role-labeled prompt format (no chat-template
  interpretation); tools/images requests stay on llama.cpp. Measured on
  the test fixtures the portable scalar C++ forward pass is SLOWER than
  llama.cpp (see `worklog.md` Phase 5 numbers) — no native-speed claim is
  made; numerical correctness is pinned against an independent Python
  reference.
- **llama.cpp remains fully functional as the fallback** (and the
  default engine). Nothing about v1.1.4Z runtime behavior changes unless
  you explicitly opt in via `engineBackend: "native"` in `config.json`
  (or `SHEYTAN_ENGINE_BACKEND=native`), which additionally requires
  building the host binary from `native/engine/` with CMake/Make (the
  host binary is not shipped/auto-downloaded yet).

The Go↔C++ boundary decision (supervised subprocess + IPC instead of
cgo) and the full rationale are documented in
`internal/native/engine/doc.go` and `ARCHITECTURE.md`.

# Major capabilities (all implemented and tested)

| Area | What works |
|---|---|
| **Engine lifecycle** | Automatic download → launch → `/health` readiness → `ready`; bounded auto-restart (3 attempts, exponential backoff); deliberate stop suppression; port-conflict adoption; compat ladder (4 launch profiles, now the LAST resort behind per-option surgical repair); engine capability profile (--help detection, pre-launch validation, verified persistence); one-shot self-update when a model needs a newer engine; scheduled update loop (daily/weekly/monthly) |
| **Engine states** | `idle / downloading / starting / ready / running / busy / stopping / stopped / failed` — backend-authoritative, fanned out over every activity WebSocket; the UI never invents them |
| **Agent loop** | Streaming responses with think-tag splitting and native `reasoning_content`; tool calls with argument validation; tool-result follow-up turns; iteration cap (default 25); per-run time budget (default 60 min); abort with partial-result preservation; regenerate; timeout-vs-abort distinguished in the UI |
| **Reliability (Phase 6)** | Every tool failure diagnosed into 14 categories with a category-specific repair hint fed back to the model (re-plan, never blind retry); loop guard detects repeated identical calls (warning on first repeat, refusal past the bound) with run-level tool-call (200) and wall-clock (30 min) budgets; failure tally and per-tool call stats on every run |
| **Verification (Phase 6)** | Run-level verification verdict computed from objective evidence only: `verified / partially_verified / failed / not_verified`. Lab verify actions and explicit build/test outcomes count; everything else contributes nothing. A completion claim with no evidence reports `not_verified` — model prose is never proof |
| **Tools** | 18 registered tools (adds `pipeline`): `shell`, `files`, `codeExec` (Job-Object sandboxed), `webSearch`, `git`, `browser`, `dataAnalysis`, `json`, `archive`, `fetch`, `diff`, `screenshot`, `linux`, `coding_lab`, `research`, `memory` (+ sandbox override). Tool schemas are measured exactly before windowing |
| **Coding Lab** | Isolated workspace copies (symlinks skipped, `.git` excluded), lexical command policy (dangerous/network/interactive/escape denylists + expansion-token hardening), 2 MiB bounded output, sanitized environment (secrets scrubbed, `HOME` pinned to the workspace), objective verification (trivial `echo`-style checks rejected), bounded repair loop with repeat-command detection, patch export, snapshot-before-promote; safe anchored edits (`read_file` bounded line-numbered views, `edit_file` requires the anchor to occur exactly once — stale or ambiguous anchors are refused, writes are atomic and re-verified, every edit invalidates verification) |
| **Project intelligence (Phase 6)** | Persistent per-project facts: measured languages, build system, layout, Lab-verified build/test commands (verified beats inferred, never clobbered), bounded lessons. The compact card is injected into every run so the model starts knowing the project instead of re-discovering it |
| **Attachments** | Content-addressed staging (sha256, symlink-safe, no exec bits), size/count/processing/chunk caps, text normalization + paragraph-boundary chunking, cached retrieval with provenance headers, image classification into the vision pipeline |
| **Context** | Preflight budget pipeline with a guaranteed fit: model-aware effective window (configured ∩ GGUF limit ∩ engine limit), output reserve + safety margin, measured tool schemas, automatic degradation ladder (dynamic toolsets → compact briefing → dropped optional blocks), refusal without an engine call when the budget is impossible; history windowing to the budget; content-keyed LRU cache; context-effectiveness telemetry per turn |
| **Long context** | Continuum chapter rollover: when a session crosses the pressure threshold, facts/decisions/threads are distilled into a framework and the conversation continues in a fresh chapter session (the UI follows automatically) |
| **Memory & recall** | Trust-classed memory (M1–M7, external material quarantined), BM25 recall with recency boost and 👍/👎 feedback steering (persistent sidecar) |
| **Research** | Auto/GitHub/Reddit/DuckDuckGo/SearXNG providers, TTL-cached, authority-ranked, provenance-tagged |
| **Observability** | `app.log` + `tools.jsonl` + `llm.jsonl` with rotation and bounds, crash reports (pruned), diagnostics zip with secret redaction, perf HUD (TTFT / tok/s), engine logs ring |
| **Concurrency** | Copy-on-write live configuration (no data races between Settings patches and active runs), mutex-guarded registries, per-run config snapshots, race-detector-clean core |

# Supported environment

- **Windows 10/11 x64** — the primary target (GUI subsystem exe, console-less, DPI-aware, process-tree kills via `taskkill /T`).
- **Linux x64** — desktop build requires GTK4 / WebKitGTK 6.0 dev libraries; the `headless` build tag runs the same stack without them.
- Hardware probing (CPU/RAM/GPU) uses CIM via PowerShell with a `wmic` fallback (wmic is removed on Windows 11 24H2+).

# Model / runtime integration

- **Local engine**: managed llama.cpp server (bundled CPU build by default; Vulkan offload detected automatically). Models are discovered from the `models/` folder, with GGUF header metadata (architecture, quantization, context length, parameter count) surfaced in the UI.
- **Remote providers**: any OpenAI-compatible endpoint (`SHEYTAN_PROVIDER=remote`). llama.cpp-only request fields (`top_k`, `min_p`, `n_ctx`, `repeat_last_n`) are automatically omitted for remotes; OpenAI-standard sampling fields are sent to both.

# Installation

```text
SHEYTAN-Local-Agent-Windows-x64-v1.1.5Z.zip
└── SHEYTAN-Local-Agent/
    ├── SHEYTAN-Local-Agent.exe   (GUI app + embedded UI + HTTP/WS API)
    ├── sheytan-local-agent.bat   (portable launcher)
    ├── AI-CONTEXT.md             (the model's operating manual)
    └── README / LICENSE / SIGNATURE / worklog
```

Unzip anywhere and run `SHEYTAN-Local-Agent.exe`. On first launch the app creates its portable data layout next to the executable:

```text
SHEYTAN-Local-Agent/
├── models/         (drop .gguf files here; mmproj-*.gguf pairs as vision projectors)
├── sessions/       (one JSON per session + activity sidecars)
├── logs/           (app.log, tools.jsonl, llm.jsonl, crashes/, screenshots/)
├── bin/            (auto-downloaded llama-server)
├── attachments/    (content-addressed staged uploads)
├── lab/workspaces/ (isolated coding-lab copies)
├── sandbox/        (governed code execution)
├── recall/         (index + feedback sidecars)
└── config.json
```

The engine binary downloads automatically when the machine is online; drop a prebuilt `llama-server(.exe)` into `bin/` for offline installs. NOTE (v1.1.5Z): llama.cpp stopped publishing prebuilt LINUX binaries — on Linux the automatic download therefore cannot succeed any more; build `llama-server` from source and set `llamaBinPath`, or select the native engine (`engineBackend: "native"`). The engine still scans recent upstream releases and self-heals automatically if prebuilt Linux binaries return.

# Configuration

Settings are edited in the UI (`Settings` view) or by patching `config.json` (the API accepts partial JSON objects). Selected keys:

| Key | Default | Meaning |
|---|---|---|
| `provider` | `local` | `local` (managed llama.cpp) or `remote` (OpenAI-compatible endpoint) |
| `engineBackend` | `llama` | v1.1.5Z: `llama` (default, full engine) or `native` (opt-in — since Phase 5 the native engine performs REAL llama-architecture generation and serves plain-text runs when selected AND capable; llama.cpp serves everything else: tools, images, non-llama architectures, unsupported tensors, and the fallback) |
| `nativeEnginePath` | (auto) | v1.1.5Z: override for the `shtn-engine-host` binary location (default `{dataDir}/bin/`) |
| `model` | first `.gguf` | active local model |
| `llamaPort` | 8080 | managed engine port |
| `llamaAutoStart` | true | prewarm engine at launch |
| `llm.numCtx` | 16384 | configured context window — the EFFECTIVE window is min(configured, GGUF model limit, engine limit), so a small model is never over-windowed |
| `llm.*` | — | sampling: temperature, top-p, top-k, min-p, penalties, mirostat, seed, stop |
| `maxIterations` | 25 | agent-loop iteration cap |
| `runTimeoutMinutes` | 60 | per-turn budget (0 = unbounded) |
| `sandboxEnabled` | true | Job-Object resource governor for `codeExec` |
| `thinkingMode` | false | externalized reasoning blocks |
| `recallEnabled` | true | inject relevant past exchanges |
| `continuumEnabled` | true | automatic chapter rollover |
| `labEnabled` | true | Coding Lab tool |
| `researchEnabled` | true | research tool + providers |
| `updateSchedule` | daily | engine update cadence (`off` disables) |

Environment overrides (`SHEYTAN_*`) are documented in `sheytan help`.

# Usage

```bash
# desktop app (default on Windows/Linux)
SHEYTAN-Local-Agent.exe

# headless server + UI in a browser
sheytan-local-agent serve --port 8765

# one-shot headless agent turn
sheytan-local-agent ask "summarize ./notes" --session work

# multi-agent planner/executor/critic pipeline (CLI; the critic judges
# against the run's objective verification verdict, not executor prose,
# and all inter-agent hand-offs are bounded excerpts)
sheytan-local-agent ask "..." --multi

# health, diagnostics, engine update
sheytan-local-agent doctor
sheytan-local-agent diagnostics
sheytan-local-agent update --status
```

REST/WS surface (loopback only): `/api/state`, `/api/engine`, `/api/models`, `/api/sessions`, `/api/config`, `/api/llama`, `/api/run`, `/api/abort`, `/api/attachments`, `/api/tools`, `/api/lab`, `/api/research`, `/api/feedback`, `/ws/activity?sessionId=`.

# Agent / tool capabilities and limits

- The agent loop executes tools **sequentially** (parallel execution is a deliberate non-goal for now).
- Small instruct models may not emit formal tool calls even when tools are advertised — the loop mechanics are covered by deterministic tests.
- The `linux` tool is an honest in-process shell simulator (its description tells the model so); real shell work goes through `shell`/`codeExec`/the Lab.
- Vision requires an `mmproj-*.gguf` projector paired with the active model; screenshots capture the primary display (Windows).
- The Coding Lab's command policy is lexical — it blocks known-dangerous, network, interactive and escape tokens, and the runtime additionally pins `HOME` and scrubs secrets, but it is not a kernel-level sandbox.

# Security model

- The LLM is an **untrusted proposal source**. Runtime policy is authoritative.
- The HTTP/WS API binds `127.0.0.1` and rejects non-approved origins; it has no auth token by design (loopback-only desktop app), so any local process can reach it — treat the machine's user account as the trust boundary.
- Filesystem tools are jailed to the portable data root with traversal + symlink resolution checks; archive extraction is zip-slip-safe with entry/total caps.
- `fetch` enforces public-destination SSRF controls end-to-end: URL validation, DNS pre-resolution, **and** dialed-IP pinning (DNS-rebinding window closed).
- Lab and sandbox processes run with a sanitized environment (API keys/tokens/credentials scrubbed) and a workspace-pinned `HOME`.
- Secrets never appear in API responses (redacted on read), and diagnostics zips redact config, crash logs and structured logs.

# Development

```bash
# backend (no GTK needed)
go build -tags headless ./...
go test -tags headless ./internal/... -count=1
go test -race -tags headless ./internal/agent/ ./internal/llm/ ./internal/api/ ...
go vet -tags headless ./...

# frontend
npm install
npm run typecheck
npm run lint
npm run build        # tsc + vite + sync into web/static

# C++ native engine (optional in Phase 2; requires cmake or plain make)
cmake -S native/engine -B native/engine/build
cmake --build native/engine/build
ctest --test-dir native/engine/build   # engine + protocol + host + gguf + model tests

# release stress suite (gate used by CI and build-and-zip.sh)
go run ./scripts/stress-main stress

# release consistency (package.json -> config.go / config.yml / SIGNATURE / workflow)
node scripts/release-version.mjs --check
```

The desktop (Wails) build for Linux needs `libgtk-4-dev`, `libwebkitgtk-6.0-dev`, `libsoup-3.0-dev`, `pkg-config`; the Windows build is CGO-free and cross-compilable.

After any frontend change, `npm run build` must be run so `web/static` (the embedded assets) stays in sync.

# Testing

- **27 Go test packages** — engine lifecycle (real process spawn/kill via a fake llama.cpp re-exec), agent loop (fake SSE engine: streaming, tool calls, abort, error propagation; generation-router seam), API surface (HTTP-level session/attachment/config/feedback contracts), attachments, chunking, context cache, context plan, continuum, lab (policy, repair loop, verification), memory, recall, research (SSRF/alias contracts), sessions (concurrency, sidecar bounds), termshell, tools, vision, releasegate, runtime, native engine (real C++ host e2e: generation, cancellation, KV/scheduler accounting, fallback signals), plus v1.1.4Z regression tests for the config source race, sampling wire format, GGUF parser, stream stall watchdog, zip-slip and escape tokens.
- **Stress suite** — 30 scenarios (hostile prompts, garbage tool args, shell injection, memory/session contracts, release-surface pinning) run in CI and as a release gate.
- **CI** (`.github/workflows/build-desktop.yml`) — audit job (version sync + frontend verify + C++ engine build/ctest), Windows job (tests + GUI exe + console probe + package + zip verification), Linux job (C++ engine build so the Go↔C++ integration tests run for real + tests + stress suite + package), release job (version-agnostic tag gate, integrity-checked publication: ZIP CRC test plus entry-contract verification on both platform ZIPs).

# Development direction (planned — NOT implemented today)

SHEYTAN's validated future direction is documented in full in
`ARCHITECTURE.md` (Part II). Nothing in that direction is implemented
yet; the list below is design intent, not shipped capability:

- **Small, fast, local models as the foundation** — the system is
  designed for `many efficient agents + orchestration + tools + external
  memory + verification`, not one enormous model. Model size is not the
  sole source of system intelligence.
- **Model tiers and hardware-adaptive routing** — a tier ladder
  (Tier 0 smallest → Tier 4 optional high-end multimodal) with
  model-agnostic, capability-based routing by task complexity, modality,
  latency, hardware and budget. Candidate model families (e.g.
  Gemma-class, GLM-class) are examples only, not integrations.
- **The Context Engine** — repository structural index → semantic index
  → hierarchical retrieval → budgeted context builder, so model context
  is treated as *working memory* while external structured storage
  provides *project memory*. The implemented seeds today are the context
  plan, context cache, attachment chunking, recall and continuum.
- **Context budgeting** — "use the smallest sufficient working set",
  extending the existing measured budget (`internal/contextplan`) to
  finer-grained sections.
- **Multi-agent architecture** — specialized low-cost agents (planner,
  coder, researcher, tester, debugger, documentation, reviewer,
  verifier) with per-agent model assignment. Today the runtime is a
  sequential single-agent loop (plus a CLI-only sequential
  planner→executor→critic pipeline); parallelism remains a deliberate
  non-goal until implemented.
- **Artifact-based agent communication** — agents exchange
  `analysis.json` / `patch.diff` / `findings.md` / `test-results.json`
  style artifacts instead of forwarding whole conversation histories.
- **Document editing architecture** — section-aware retrieval →
  structured patch → validation gates (structure, references, forbidden
  deletions, version metadata) with the model never the authority on
  correctness.

# Version

`v1.1.5Z` — see `worklog.md` for the complete remediation history and `agent.md` for the engineering handoff context.

# License

SHEYTAN™ Local-Agent is proprietary software. See `LICENSE` for the governing terms.
