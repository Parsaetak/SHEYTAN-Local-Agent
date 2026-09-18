# UPDATE.md — v1.2.6 SHEYTAN-LA Verified-Runtime Repair: Missing-Answer Root Cause, Honest Context Tiers, Progressive UI, Batched Windows Hardware Probe, Evidence-Based Accelerator Resolution

**Release:** `v1.2.6` (codename Zeta) · **Base:** `main @ fc5b500` (`v1.2.5`)
**Date:** 2026-09-18
**Package:** `SHEYTAN-Local-Agent-v1.2.6-UPDATE.zip`

This package fixes the four v1.2.5 field-report failures at their
roots: simple prompts that completed without a visible answer, the
THINKING control inflating a one-line question to ~13K prompt tokens,
Settings/System blocking on the measured-5,399-ms Windows hardware
probe, and GPU acceleration being assumed from a Vulkan DLL instead of
runtime evidence. The fixes add a bounded authoritative run-outcome
registry with WebSocket replay (a run can never become invisible
because the socket attached late), make the context tiers honest
(reasoning depth is no longer context size; zero-signal chat offers
zero tool schemas), split the hardware probe into an instant in-process
snapshot plus ONE batched background CIM invocation (7 process spawns →
1), make every Settings/System resource load independently through one
shared deduplicating cache, and introduce a single accelerator
abstraction (CPU / GPU_VULKAN / GPU_OPENVINO / NPU_OPENVINO) whose GPU
claims require the installed engine's own device enumeration or its
runtime offload log line, and whose NPU path requires measured OpenVINO
runtime load plus model-architecture/quantization gates plus a measured
benchmark that beats GPU — hardware presence alone never wins.

The version moves `1.2.5` → `1.2.6` through the established identity
chain (package.json → release-version.mjs → internal/config/config.go,
build/config.yml, SIGNATURE — `--check` is green).

---

## 0. Execution method (read this first)

INSPECT → REPRODUCE → ROOT CAUSE → PLAN → IMPLEMENT → VERIFY → STRESS →
CLEAN-ROOM BUILD → PACKAGE → FACTUAL REPORT. Baseline captured BEFORE any
edit (`main @ fc5b500`): 40 Go packages ok / 0 fail, typecheck 0 errors,
lint 0, frontend units 28/28. Every root cause below was derived from
source, and every fix is regression-locked. Where this environment could
not measure something, that is stated — nothing is claimed without
evidence.

## 1. ROOT CAUSE 1 — the missing answer (WebSocket late-attach race)

Reproduction path traced end-to-end: UI send → POST /api/run → run
registration → engine gate → classification → context composition →
serialization → HTTP request → streaming → generation end → assistant
persistence → done event → WebSocket delivery → UI finalisation.

The defect: the activity hub has **no replay**. A run that finishes
before the activity socket attaches (a FAST simple prompt completes in
well under the 2.5-second attach window on a warm engine) parks the
socket in standby; the only frame it ever receives is the bare `idle`
sentinel. The frontend's attach-race grace guard (`recoverRunFromIdle`,
RUN_IDLE_GRACE_MS = 2500) **drops** that sentinel when the run produced
no streamed evidence — it is indistinguishable from the harmless
standby marker emitted right after POST /api/run. The UI then stays
`running=true` with no visible answer, forever: no later event ever
arrives.

### The fix (root cause, not symptom)

- **Bounded run-outcome registry** (`internal/api/runregistry.go`,
  NEW): every run settles exactly once into a per-session ring (4
  entries, 256 sessions LRU) recording `runId`, `startedAt`, `endedAt`,
  `outcome` (done|error|aborted), the orchestrator's own terminal
  caption, `persisted` (the reply actually reached the session store)
  and the measured reply/reasoning sizes. The **idle sentinel now
  carries this outcome** (`lastRun` block) — a late-attaching socket
  receives authoritative terminal evidence, not a bare marker.
- **One runId across the whole path**: every activity frame of a run is
  stamped (`agent.Activity.RunID`), and the API-side stages —
  runAccepted, runRegistered, engineGateStart/Ready,
  assistantPersisted, donePublished — are measured and logged per run
  (`runId=… session=… outcome=… persistMs=… publishMs=… totalMs=…
  replyChars=…`), so a stalled run reveals WHERE it is stalled.
- **Frontend**: `recoverRunFromIdle` finalises immediately when the
  authoritative `lastRun.endedAt` proves the run is over (≥
  runStartedAt), grace window or not. A grace-ignored sentinel now
  arms a **bounded re-check** at the end of the grace window — if no
  run evidence arrived by then either, the run finalises from the
  authoritative history. `run()` and `regenerate()` ensure the activity
  socket is attaching BEFORE the POST fires.

Regression locks: `TestLateAttachSocketReceivesRunOutcome` (THE race —
run completes before the socket attaches; the first frame must carry the
outcome evidence), `TestActivitiesCarryRunIdAndDoneAttachesMidRun`,
`TestRunRegistryBounds`.

## 2. ROOT CAUSE 2 — context over-injection under THINKING

Measured baseline (the field report): a 1-word prompt cost ≈1.1K tokens
on FAST and **≈12,962 tokens on THINKING**. Source audit found a double
penalty: the classifier added **+20 complexity** for the thinking
control AND tier selection **forced the STANDARD floor** — so a one-line
question paid the full ~7.4K-token briefing + recall + project card + up
to 12 tool schemas. Two further defects: the core tools
(files/shell/memory) rode EVERY request including zero-signal chat, and
tier selection used the raw `max_tokens` as the output reserve while
context planning clamped it (min 512, max numCtx/2) — the same number
disagreed with itself between the tier decision and the plan report.

### The fix — THINKING is reasoning, not context

- The thinking control **no longer adds complexity and no longer floors
  the tier**. Reasoning depth comes from the thinking nudge plus the
  evidence-driven escalation ladder; a genuinely deep task carries its
  own signals and lands deeper on its own merits.
- **Zero-signal chat → ZERO tool schemas** and the compact briefing
  only (`toolsets.SelectForTask` returns nothing for a conversation
  with no capability signal). Naming a registered tool in the request
  IS a capability signal (custom tools included — "use the echo tool"
  offers echo). If the model reaches for a non-offered tool anyway, the
  refusal is recorded as escalation evidence and the refused tool
  re-enters the offered surface on the next iteration.
- **One output reserve**: `contextplan.OutputReserveFor` is the single
  clamped reserve both tier selection and context planning use.

Measured after (same fake-engine test rig as the v1.2.5 measurement —
`TestV126ThinkingSimplePromptStaysCompact`):

| Posture (1-line prompt) | v1.2.5 | v1.2.6 |
|---|---|---|
| FAST | ≈1,103 tokens (field report) | **≈443 tokens**, tier FAST, 0 tools |
| THINKING | ≈12,962 tokens (field report) | **≈548 tokens**, tier FAST, 0 tools |

## 3. ROOT CAUSE 3 — Windows hardware probe latency (measured 5,399 ms)

`sysinfo.probeUncached()` on Windows ran ~7 SEQUENTIAL PowerShell/CIM
process spawns (3× CPU properties, 2× RAM, 1× GPU list, 1× wmic disk)
at ~700–900 ms each. Worse, `/api/environment` and `/api/health` called
the blocking `hardware.Collect` per request, and Settings fetched
config+models+presets+tools+sysinfo in ONE global `Promise.all` — the
entire page waited for the slowest endpoint.

### The fix — one batched invocation + a fast/deep split

- **ONE batched CIM invocation** (`internal/sysinfo/cim_windows.go`):
  a single PowerShell script emits a stable KEY=VALUE block (CPU
  name/cores/frequency, RAM, GPU adapters + driver versions, the disk,
  and **NPU detection** via Win32_PnPEntity including PNPClass "ND"),
  parsed by the pure, fixture-tested `cim_parse.go`. Process-spawn
  count: 7 → 1.
- **Fast snapshot, zero spawns** (`sysinfo.ProbeFast`): OS/arch/CPU
  counts (runtime), RAM and disk through in-process Win32 APIs
  (GlobalMemoryStatusEx via kernel32, GetDiskFreeSpaceEx via
  golang.org/x/sys) — served instantly by /api/sysinfo,
  /api/environment and /api/health.
- **Deep probe in the background** (`hardware.WarmDeep` at startup;
  single-flight, bounded per source with per-source duration +
  success/failure logging). GPU/NPU/driver facts merge when they land;
  the wire carries `deepReady` so the UI can render the fast facts and
  fill in progressively.
- **Progressive frontend** (`src/resources.ts` + `src/useResource.ts`,
  NEW): one shared resource layer with per-resource states
  (unavailable/loading/ready/stale/error), request deduplication, TTL,
  stale-while-revalidate, last-known-good retention, AbortController.
  Settings loads config IMMEDIATELY (the save controls unlock the
  moment it lands), presets/tools in the background, models/sysinfo
  lazily when their tab is shown. System renders the fast environment
  instantly and polls until `deepReady`. One slow endpoint can never
  block another; Settings/System/Performance mounting together never
  issue duplicate requests.

Honest note: this package was built and tested on Linux (see §8), so
the Windows wall-clock improvement was not measured here — the spawn
count reduction (7 → 1) and the non-blocking split are verifiable from
source and covered by parser fixtures; the real-machine timing must be
confirmed in the Windows validation pass below.

## 4. ROOT CAUSE 4 — GPU assumed, never verified

The v1.2.5 rule: `autoGPUOffload = ggml-vulkan.dll exists AND any WMI
adapter present` → `--n-gpu-layers 99`. No device enumeration, no
runtime offload verification, no NPU evaluation anywhere in the
codebase — "Vulkan DLL exists = GPU inference".

### The fix — one accelerator abstraction, evidence-gated

New package `internal/accelerator`:

- **Kinds**: CPU, GPU_VULKAN, GPU_OPENVINO, NPU_OPENVINO.
- **Requested profiles** (config `accelerator`, default `auto`):
  AUTO / GPU / NPU / CPU.
- **AUTO runtime profiles** (stored on the resolution):
  INTERACTIVE_GPU, LOW_POWER_NPU, CPU_SAFE, VISION_GPU, MAXIMUM.

**GPU gates (strongest evidence first):**
1. the installed engine's own `--list-devices` enumeration
   (`llm.EnumerateEngineDevices`, cached per binary path+mtime, no
   per-launch spawn) enumerates a device → GPU_VULKAN;
2. the engine's runtime log measured real offload
   (`LlamaServer.OffloadEvidence` parses "offloaded N/M layers to GPU"
   from the live stdout/stderr stream) → GPU_VULKAN;
3. ONLY when the build supports no enumeration: the documented weaker
   fallback (Vulkan backend present) with the verification plan stated
   in the reason — the boot's offload line feeds gate 2 for every
   subsequent launch.

A Vulkan DLL with an engine that enumerates ZERO devices now resolves
to CPU. The auto-offload launcher uses this same ladder.

**NPU gates (every one required):** measured NPU presence
(Win32_PnPEntity identity) + **measured OpenVINO runtime load**
(`accelerator.DetectOpenVINO` — LoadLibrary on Windows / loader-cache
probe elsewhere, never a name) + model architecture in the
measured-supported set + quantization in the measured-supported set + a
**measured benchmark that beats GPU for the workload**. Any failed
gate falls back with the reason recorded in the resolution's
`fallbacks`. NPU presence alone NEVER selects NPU_OPENVINO.

`/api/perf` now carries the full resolution — requested, resolved
backend, device, auto-profile, GPU layers, and the WHY (`reason`) —
built from measured evidence only.

## 5. What is preserved

The architecture is unchanged: React 19 + TypeScript + Zustand, Go 1.26,
the C++ native engine, llama.cpp integration, Wails v3. The engine
state vocabulary, the deliberate-stop vs unexpected-exit lifecycle, the
Phase 7 preflight fit gates, the streaming coalescer, the stall
watchdog, the security invariants (loopback-only API, path jails,
zip-slip safety, sanitized environments) and the run timeout budget are
all untouched. No working subsystem was rewritten; every change is
additive or a root-cause repair inside the existing structure. The
native-engine ABI/protocol versions are unchanged (4/4) — no C++ files
are in this package.

## 6. Verification performed (all executed, all clean)

```text
go test -tags headless ./internal/... -count=1        # 42 ok, 0 FAIL
go test -race -tags headless ./internal/agent/ ./internal/llm/ \
  ./internal/api/ ./internal/accelerator/ ./internal/sysinfo/   # ok
go vet -tags headless ./...                            # clean
go run ./scripts/stress-main stress                     # 47 pass / 0 fail, 0 hangs, 0 crashes
npm run typecheck                                      # 0 errors
npm run lint                                           # 0 warnings
npm run test:units                                     # 34/34
npm run build                                          # clean, assets synced
node scripts/release-version.mjs --check               # consistent
GOOS=windows go build ./...                            # cross-compile gate OK
```

New regression tests shipped in this package:
`internal/api/runregistry_test.go` (the late-attach race + registry
bounds), `internal/agent/v126_measurement_test.go` (the
thinking-stays-compact measurement + zero-tools trivial chat),
`internal/accelerator/accelerator_test.go` (the full gate matrix —
including "DLL alone is not GPU evidence" and "NPU refused without
every gate"), `internal/llm/devices_test.go` (enumeration + offload
line parsing fixtures), `internal/sysinfo/cim_parse_test.go` (the
Intel Arc + AI Boost fixture machine, virtual-adapter skipping,
partial/garbage input), `internal/api/perf_accelerator_test.go` (the
/api/perf resolution block), `src/resources.test.ts` (dedup, TTL,
stale-while-revalidate, last-known-good, independence).

Clean-room: a fresh clone of the base commit with this package's patch
applied builds (frontend + Go, both platforms) and passes every gate
above; the rebuilt web assets are byte-identical to the ones in this
package (deterministic build verified).

## 7. Windows validation pass (§13 of the brief) — TO BE EXECUTED ON THE TARGET MACHINE

This package was implemented and verified on Linux. The Windows-only
paths (the batched CIM script, the in-process Win32 memory/disk reads,
the OpenVINO LoadLibrary probe, `--list-devices` against the real
llama-server.exe, Intel Arc + AI Boost enumeration) compile for
GOOS=windows and their parsers are fixture-tested — but were NOT
executed on real Windows hardware by this agent. Before release
sign-off, run on the target machine: cold launch → UI usable before
deep init → Settings opens immediately → every section → System →
`hi` / `2+2` / a normal question × {auto, fast, thinking} → verify
visible + persisted answers → inspect `/api/perf` (backend/device/
reason from real enumeration) → restart engine → repeat generation →
close/reopen. The honest expectations: the hardware probe log should
now report ONE batched source with its measured duration (the v1.2.5
baseline on this machine was 5,399 ms), and /api/perf's accelerator
block should name the enumerated device with its reason.

## 8. Known limitations (read literally)

- NPU_OPENVINO is an evidence-gated RESOLUTION only. No OpenVINO
  execution engine is wired — llama.cpp and the native C++ engine
  remain the only inference backends — so NPU can never be selected in
  practice until an OpenVINO backend with measured benchmarks exists.
  The resolution layer is ready for it, honestly.
- The GPU runtime verification observes llama.cpp's standard offload
  log line at boot; a build that offloads silently (no standard line)
  will fall back to CPU on machines whose engine build lacks
  enumeration support. The evidence ladder records which rung decided.
- The tier/output-reserve reconciliation changes
  `requestTimingAPI` semantics only additively; existing telemetry
  consumers see the same fields plus the reconciled reserve.

## 9. Apply procedure

1. `git clone https://github.com/Parsaetak/SHEYTAN-local-agent && cd SHEYTAN-local-agent && git checkout fc5b500858abe29aaba2c2ecd4c14c37f091e519`
2. Unzip this package OVER the checkout (it contains only the changed
   files at their repository-relative paths plus this bookkeeping).
3. Delete the ten stale hashed assets listed under `[DELETED FILES]` in
   `REPLACEMENT-MANIFEST.txt` — `web/static` is go:embedded, so stale
   files must be removed for a clean binary.
4. Verify: `node scripts/release-version.mjs --check`, `npm install`,
   `npm run build`, `go build -tags headless ./...`,
   `go test -tags headless ./internal/... -count=1`, `go vet -tags headless ./...`,
   `npm run typecheck && npm run lint && npm run test:units`.
