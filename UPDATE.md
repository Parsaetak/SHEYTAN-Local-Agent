# UPDATE.md — v1.9.3 Release Notes & Maintenance Behavior

**Release:** `v1.9.3` (canonical application version; single version
hierarchy: package.json → release-version.mjs → config.go /
build/config.yml / SIGNATURE)
**Base:** `main @ e5e1b79` (`v1.9.2`) · **Date:** 2026-10-10
**Package:** `SHEYTAN-Local-Agent-v1.9.3-FINAL.zip` (complete repository tree)

The authoritative per-release history lives in `changelog.md`; this file
carries the CURRENT release notes and the operational maintenance
behavior. The v1.9.0 feature surface (AI Systems, Goals, Approvals,
bounded delegation, `repo_nav`) is unchanged in v1.9.3 and is documented
in the changelog; its maintenance behavior below carries forward.

## What v1.9.3 changes

1. **P0 — the Go race-gate failure is root-caused by real reproduction
   and fixed with a pinned CPU policy contract plus deterministic test
   seams.** The authoritative failure (Actions run `38035650428`, job
   `114165397477`) failed `TestGovernorEndpointServesComposedSelfModel`
   (`internal/api/governor_test.go:93`) with
   `ok-pressure envelope class = live, want none`. The reproduced
   mechanism: `runtime.StartGovernor` wires the production platform CPU
   seam (`governor.CPULoadPlatform` — on Linux the 1-minute load average
   normalized by core count) into the governor the API test folds
   samples into; the test folded ONE OK-pressure RAM sample, and the
   platform seam folded the runner's REAL host load with it.
   `rollingSeries.observe` sets its first EWMA sample equal to the raw
   value, so one high first sample (a busy CI runner above 85% of core
   capacity) flipped the envelope to class `live` — contradicting the
   documented "one noisy sample never drives policy" promise. The
   reproduction was run on a real host under real generated load (4 busy
   spinners, loadavg 2.87 on 2 cores ≈ 143%): the ORIGINAL v1.9.2 code
   fails with the exact CI signature; the fixed code passes under
   96–218% measured load. The fix has two coordinated parts, with no
   production sampling disabled and no race coverage removed:

   - **The CPU policy contract is pinned in code and tested in BOTH
     directions** (`internal/governor`): the envelope's CPU-reduction
     branch now requires a SUSTAINED rolling signal — at least
     `cpuWarmupSamples` (3) folded samples AND a rolling average at or
     above `CPUReduceAbove`. A lone spike (including the very first
     sample, whose EWMA equals the raw value) never flips the envelope;
     a genuinely sustained high load (~45 s at the shipped 15 s cadence)
     still reduces background work and tool concurrency, matching the
     sustained-pressure philosophy of the RAM branches. The folded
     sample count is carried on `ResourceState.CPUSamples` and stated in
     the reduction reason. `TestSustainedCPUHigherThanThresholdReduces`
     now folds a genuinely sustained signal (its name means it), and the
     new `TestLoneCPUSpikeDoesNotFlipEnvelope` pins that one high sample
     leaves the OK-pressure envelope at class `none` while the same load
     sustained past the warm-up floor engages class `live`.
   - **The API integration test is deterministic regardless of host
     load** (`internal/api/governor_test.go`): the envelope contract
     under test is about PRESSURE, not the host's CPU, so the test pins
     the injected CPU seam to an honest unknown (the same posture as a
     platform that cannot measure CPU — every CPU policy branch stays
     OFF) via the new `Governor.SetCPUSampler` seam. Production wiring
     keeps `governor.CPULoadPlatform` as the ONE platform sampler.

2. **P0 — the native prefill parity gate's claims are corrected to
   exactly what is proven, and the proof is strengthened.** The v1.9.2
   `test_prefill_parity.cpp` header claimed per-position logits parity,
   but only the full prompt's FINAL logits were compared (the span path
   projects logits only for a span's last token by design — skipping the
   intermediate projections IS the optimization). The v1.9.3 gate proves
   and claims: per-position logits parity through the chunk=1 span
   cadence (every token is a span-final token there — bit-for-bit
   against the serial path at all 20 fixture positions); final-token
   logits for the production chunk sizes 3/7/16; full KV bytes (every
   layer's K/V fp16 words at every written position) identical across
   every chunking AND the serial run; chunk-boundary invariance of the
   final logits; edge/error bounds (empty span, null args, out-of-vocab,
   context overflow); and mixed prefill→decode continuity. Tied output
   is explicitly NOT claimed (every shipped fixture carries a real
   `output.weight`). Cancellation remains a between-chunks contract in
   the generate loop and is documented as such. A clean CMake
   configure/build/ctest of the native engine passes 13/13 locally
   including the strengthened parity gate.

3. **P1 — the zero-session E2E observation contract is closed against
   the placeholder loophole in every test.** v1.9.2 repaired the
   live-text test; the count-based assertions in the other three
   zero-session tests could still be satisfied by the placeholder-
   carrying generation bubble (it also carries `.message-row.from-agent`)
   or by an empty persisted reply. Every zero-session test now proves
   the settled assistant reply is REAL content — a `.message-content`
   with non-empty visible text and no `[data-stream-placeholder]`
   descendant — and the Agent-mode test asserts exactly one such reply.
   The rAF-suspended live-stream test additionally proves the persisted
   reply is non-empty. No assertion was weakened; the proof standard
   ("exactly one persisted non-empty assistant response") is now
   enforced on every path.

4. **P1 — sub-10px UI typography is raised to the documented floors.**
   The audited 62 `font-size` declarations at 6–9px are raised to the
   UI/UX contract floors: dense secondary metadata ≥ 10px, essential
   labels/states/errors ≥ 11px (topbar status, engine status, error
   banners, Lab failure states), and interactive control text ≥ 12px
   (Stop button, text buttons, secondary buttons, composer/textarea
   input text). Nothing above the floors was changed — density is
   preserved through hierarchy and layout, not microscopic text.

## Maintenance behavior (unchanged where not stated)

* **Update flow** — the existing updater/downloader authority checks,
  stages and installs releases; staging keeps a resumable `.part` file;
  cancel stops the download without deleting it.
* **Data locations** — user config remains `config.json` under the
  canonical data root; durable stores live beside the existing ones:
  `<data>/ai-systems/`, `<data>/goals/`. Deleting an AI System document
  or goal document by hand is tolerated (the stores are corruption-
  tolerant and repair the active pointer to Default).
* **Default AI System** — reserved id `default`; it cannot be deleted;
  deleting the ACTIVE system falls activation back to Default. Its
  behavior is the pre-1.9 runtime behavior (no instructions, no
  overrides).
* **Engine/execution evidence** — unchanged from v1.8: detection ≠
  availability ≠ selection ≠ execution ≠ verification; identity-bound
  execution receipts; one accelerator authority; one Governor. The
  Governor's CPU policy contract is documented in
  `internal/governor/governor.go` and pinned by tests in both
  directions.
* **Native engine numerics** — the resolved weight table and
  `prefill_span` are bit-for-bit identical to the serial path (pinned by
  the strengthened `test_prefill_parity`: per-position logits via the
  chunk=1 cadence, final-token logits per production chunk size, full KV
  bytes across every chunking); the public `Forward::token()` semantics
  are unchanged and every existing native test still passes.
* **Window close semantics** — closing the main window quits the
  application (single-window product, no tray). `srv.Close()` and the
  owned engine/server/background cleanup run through the existing
  deferred path.
* **Frontend verification stack** — typecheck, oxlint (0 warnings/0
  errors), the node unit suite (`npm run test:units`, 220/220), the
  release gates (`npm run test:release`) and the production build all
  run green on this release locally.
* **Browser E2E** — the full Playwright suite runs in CI against the
  real headless server + real native engine + fixture GGUF; all
  visibility proofs observe real streamed content only.

## Measured performance facts (native engine path, honest)

* Native-engine forward pass after the v1.9.2 resolved-weight-table
  repair: **3.66 ms per token** on the E2E live fixture (was 19.0 ms —
  5.2× faster), measured on a 2-core host with the standalone engine
  benchmark. The E2E prefill of ~1681 engine-side tokens now costs
  ~6.2 s standalone versus >60 s under contention before the repair.
  Decode on the same fixture scales identically (the same per-token
  path). The per-token cost is now dominated by the actual arithmetic
  (the double-accumulator dot products), not tensor-name resolution.
* Prefill uses `prefill_span` in 16-token chunks with last-token-only
  logits; on real-vocabulary models the skipped per-token logits
  projection is an additional large saving (the fixture's 50-token
  vocab makes it negligible there). No new TTFT numbers are claimed in
  v1.9.3 — the prefill path is unchanged from v1.9.2.

## Known boundaries (honest)

* The v1.9.3 local verification sandbox proved: the race gate (the
  authoritative failing command) green on all six packages; the full
  headless Go suite green; `go vet ./...` green; the frontend gates
  green; a clean native CMake configure/build/ctest green (13/13). A
  local pass does NOT establish a green Actions run — the authoritative
  CI verdict for v1.9.3 is the Actions run that carries this revision.
* The desktop runtime smokes are wired for the CI environment (real
  window handle + delivered WM_CLOSE on Windows; owned Xvfb + real app
  pid + WM_DELETE_WINDOW on Linux) and were NOT executed in the v1.9.3
  local sandbox (no Windows; no GTK4/WebKitGTK headers — the Wails
  desktop binary cannot build there). Desktop-runtime evidence for
  v1.9.2 and v1.9.3 is CI-bound until those jobs run green.
* The goal runner does not yet decompose steps into subtasks
  automatically (the delegation engine is implemented and tested; the
  integration is the first v1.9.x work item).
* The protected-evaluation anti-hack guard is designed but not
  implemented; until it lands, protected evaluation hygiene relies on
  the existing Lab policy and workspace boundaries.
* The MCP client remains implemented and tested but is not yet
  registered into the runtime tool registry.
