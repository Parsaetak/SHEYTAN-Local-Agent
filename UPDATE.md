# UPDATE.md — v1.9.1 Release Notes & Maintenance Behavior

**Release:** `v1.9.1` (canonical application version; single version
hierarchy: package.json → release-version.mjs → config.go /
build/config.yml / SIGNATURE)
**Base:** `main @ 4cdd392` (`v1.9.0`) · **Date:** 2026-10-08
**Package:** `SHEYTAN-Local-Agent-v1.9.1-FINAL.zip` (complete repository tree)

The authoritative per-release history lives in `changelog.md`; this file
carries the CURRENT release notes and the operational maintenance
behavior. The v1.9.0 feature surface (AI Systems, Goals, Approvals,
bounded delegation, `repo_nav`) is unchanged in v1.9.1 and is documented
in the changelog; its maintenance behavior below carries forward.

## What v1.9.1 changes

1. **P0 — the Linux CI live-stream failure is root-caused and repaired.**
   The authoritative failure (Actions run `37704905404`, Linux job
   `113077751580`) was the live-stream growth assertion in
   `e2e/live-stream.spec.ts`. The product streaming chain (engine →
   activity hub → WebSocket → streaming fast path → accumulator →
   triple-boundary scheduler → DOM) was verified healthy end to end with
   a frame + DOM timeline diagnostic; the defect was the TEST's
   observation contract: the "first visible streamed text" baseline
   accepted the live bubble's PRESENTATION PLACEHOLDER (the preparing
   text, 48 chars), so the strictly-longer growth observation raced the
   engine gate + prefill phase (measured at ~40 ms per prompt token —
   the poll's whole 30 s budget could be spent before the first real
   token under contention). The observation now requires real streamed
   content only: placeholder arms carry `data-stream-placeholder` in the
   DOM, the growth baseline is the first REAL snapshot, and the fixture
   budget (320 tokens → 40 real engine chunks) is the deterministic
   snapshot count of the live window. All original assertions are
   preserved and strengthened; the rAF-suspended proof uses the same
   real-content contract.

2. **P0 — evidence discipline restored.** The v1.9.0 worklog recorded a
   "40/40" full-suite claim from a single local run while the
   authoritative CI run showed 39/40. Local runs are local evidence;
   GitHub Actions is the only CI authority. The worklog now separates
   local, CI and physical-platform evidence explicitly.

3. **P1 — desktop runtime smoke gates (Linux + Windows) in CI.** The
   workflow gains runtime smoke stages that launch the REAL desktop
   binary and probe the embedded UI + backend health over loopback HTTP
   (Linux: GTK4/WebKitGTK 6.0 stack under Xvfb; Windows: the built
   executable with process-liveness and clean shutdown checks). These
   are runtime gates, kept strictly apart from build/package gates —
   packaging success never implies GUI runtime success.

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
  execution receipts; one accelerator authority; one Governor.
* **Frontend verification stack** — typecheck, oxlint, the node unit
  suite (`npm run test:units`), and the release gates
  (`npm run test:release`) all run in `--check` mode green on this
  release.
* **Browser E2E** — the full Playwright suite runs against the real
  headless server + real native engine + fixture GGUF; the live-stream
  proof now observes real streamed content only.

## Measured performance facts (native engine path, honest)

* Native-engine prefill is currently ~25–40 ms per prompt token on a
  mid/2-core host (measured with a standalone engine-host probe; a
  ~450-token system briefing costs ~8 s of TTFT unloaded, more under
  contention). Decode speed on the same fixture: ~130+ tok/s. Prefill
  batching is a recorded performance work item in `ROADMAP.md` — until
  it lands, first-token latency on the native path is dominated by
  prompt length.

## Known boundaries (honest)

* The desktop runtime smoke stages added in v1.9.1 are wired for the CI
  environment; they were NOT executed in the v1.9.1 local verification
  sandbox (no root, no GTK4/WebKitGTK headers, no Windows). The
  worklog records this evidence boundary explicitly.
* The goal runner does not yet decompose steps into subtasks
  automatically (the delegation engine is implemented and tested; the
  integration is the first v1.9.x work item).
* The protected-evaluation anti-hack guard is designed but not
  implemented; until it lands, protected evaluation hygiene relies on
  the existing Lab policy and workspace boundaries.
* The MCP client remains implemented and tested but is not yet
  registered into the runtime tool registry.
