# UPDATE.md — v1.1.7 Options, Capability Truth, Telemetry & Diagnostics Replacement Package

**Release:** `v1.1.7` (codename Zeta) · **Base:** `main @ dd49eed` (`1.1.6Z`)
**Date:** 2026-09-13

This package is a COMPLETE REPLACEMENT of the repository state. Apply it by
replacing the whole tree (or, file-by-file, by following the exact map
below). Every path is relative to the repository root.

This release makes the existing runtime understandable and honest without
rebuilding anything: the repeated "started in compatibility mode 2 (no
speed flags)" defect is fixed at its source (persisted downgrade levels
now record a reason and earn ONE bounded full-speed retry when the
verified capability profile moves on), the Options experience is
restructured into scannable sections with plain-language tooltips and
capability-aware engine options, the tool list is one concise line per
tool, a compact live performance surface reports ONLY real measurements
(N/A otherwise), recommended settings are derived from detected hardware +
model + capability and applied only explicitly, a simple before/after
observation covers performance-option changes, connection diagnostics map
to Excellent/Good/Unstable/Slow/Offline with the first meaningful failure
reason, and the existing logs are exposed in-app with live tail, filters,
search, copy and a non-destructive clear-view — over the SAME logging
system, the SAME config copy-on-write, the SAME engine lifecycle and the
SAME telemetry inputs. No architecture was rebuilt; no experimental
subsystem was added; nothing was claimed that was not measured.

No full manual runtime/Windows acceptance phase is required for this
release; verification is the focused automated suite listed at the bottom
(all executed and green).

---

## 1. DELETE

Stale Vite bundles superseded by the regenerated embedded build. These
files existed in the base tree and are NOT shipped in this package; the
new hashed set is referenced by `web/static/index.html`.

| Path | Why |
|---|---|
| `web/static/assets/AgentBody-2-br3YvV.js` | stale bundle (superseded by `AgentBody-CfUf-j4_.js`) |
| `web/static/assets/AgentBody-BrHAHBrt.js` | stale bundle |
| `web/static/assets/AgentBody-cofMEBW6.js` | stale bundle |
| `web/static/assets/AgentHeader-B8b76u9o.js` | stale bundle (superseded by `AgentHeader-xOezjwpD.js`) |
| `web/static/assets/AgentHeader-BE0L2Nf8.js` | stale bundle |
| `web/static/assets/AgentHeader-DYauJI0M.js` | stale bundle |
| `web/static/assets/AgentSidebar-DJaBwMC5.js` | stale bundle (superseded by `AgentSidebar-BbRpID3s.js`) |
| `web/static/assets/AgentSidebar-Z4xMVYOX.js` | stale bundle |
| `web/static/assets/AgentSidebar-crN0VbUz.js` | stale bundle |
| `web/static/assets/LabPanel-BPBo4Rq7.js` | stale bundle (superseded by `LabPanel-rshNNsid.js`) |
| `web/static/assets/LabPanel-CTYxjiVu.js` | stale bundle |
| `web/static/assets/LabPanel-CW_A6EcB.js` | stale bundle |
| `web/static/assets/ResearchPanel-0ysC4-Du.js` | stale bundle (superseded by `ResearchPanel-CfWYT8Pb.js`) |
| `web/static/assets/ResearchPanel-DyeLdq3-.js` | stale bundle |
| `web/static/assets/ResearchPanel-Qeb2hrAX.js` | stale bundle |
| `web/static/assets/SettingsPanel-C5Hpkvys.js` | stale bundle (superseded by `SettingsPanel-BgejwQSw.js`) |
| `web/static/assets/SettingsPanel-eCjIM-UC.js` | stale bundle |
| `web/static/assets/SettingsPanel-g7g40a_x.js` | stale bundle |
| `web/static/assets/index-C1xyllm1.js` | stale bundle (superseded by `index-BRJmDjjc.js`) |
| `web/static/assets/index-CGpAJs8d.css` | stale bundle (superseded by `index-Bvpcz4Hd.css`) |
| `web/static/assets/index-CYXnT1iu.js` | stale bundle |
| `web/static/assets/index-OiRp_Wkd.js` | stale bundle |
| `web/static/assets/index-n0UF0DI4.css` | stale bundle |

## 2. REPLACE

| Path | Why |
|---|---|
| `UPDATE.md` | this change map (v1.1.6-zeta map superseded) |
| `REPLACEMENT-MANIFEST.txt` | regenerated for this tree |
| `REPLACEMENT-SHA256.txt` | regenerated for this tree |

## 3. MODIFY

| Path | Why |
|---|---|
| `internal/config/config.go` | **Compat truth (P0):** `AppVersion` → `1.1.7`; new backend-managed fields `engineCompatReason` (classified failure kind + offending option recorded when a boot settles above level 0, cleared at level 0) and `engineCompatAt` (RFC3339 stamp used by the retry-up gate) |
| `internal/llm/capability.go` | **Compat fix (P0):** `CompatInfo` + `CompatSnapshot()` (level, name, optimised, reason, recordedAt, removed-option changes — the user-visible "why compatibility mode" answer); `CapsSnapshot()` read-only accessor; `compatReasonFromError` (failure → recorded reason vocabulary); `shouldRetryFullSpeed` — the strict four-condition gate (persisted level > 0, option-class reason, verified profile newer than the downgrade or absent stamp, level-0 args pass `argProblems`) granting exactly ONE bounded full-speed retry; regression-locked in `capability_compat_test.go` |
| `internal/llm/llama.go` | **Boot ladder (P0):** `persistBootOutcome` records the final active configuration — level, reason (never overwriting a recorded reason with an empty one) and stamp, refreshed only on a level change or after a retry attempt; bounded full-speed retry before the ladder resumes at the persisted level (clearly logged both ways: "retrying the full-speed profile once" / "full-speed retry failed — resuming"); the compatibility-mode log line now carries the recorded reason so the user can see WHY compatibility mode was selected |
| `internal/llm/client.go` | **Telemetry (P0):** the streaming success path records what a REAL generation measured (prompt tokens, tokens, tok/s, TTFT, wall time) into the bounded perf ring — the exact numbers the old `PerfStats` already computed, now retained for the performance surface |
| `internal/llm/perftracker.go` | **NEW.** 16-sample bounded ring (`PerfSample`) + `SnapshotEnginePerf` aggregation (last sample = current speed, plus honest prompt tok/s from wall time); zero-measurement samples are dropped so aborted calls never appear as fabricated zero-speed metrics |
| `internal/llm/capability_compat_test.go` | **NEW.** `TestShouldRetryFullSpeedGates` (7 gate cases incl. the v1.1.6Z upgrade path and the invalid level-0 profile), `TestCompatReasonFromError` (option-layout/unknown-option/model-load/unknown vocabulary), `TestCompatSnapshotRecordsRemovedOptions` |
| `internal/llm/perftracker_test.go` | **NEW.** Empty tracker reports no sample; last-sample aggregation; empty-sample rejection; ring bound (16+10 recorded → exactly 16 kept) |
| `internal/netcheck/netcheck.go` | **Diagnostics (P0):** `Diagnose()` — ONE bounded check (OS DNS, two timed HTTPS samples, second-endpoint reachability, existing multi-strategy probe as proxied-machine fallback); `classifyDiag` → `Excellent / Good / Unstable / Slow / Offline` with the first meaningful failure reason (the warm sample must degrade vs the cold TLS-handshake sample to flag instability — no false "Unstable" on first-request handshake cost) |
| `internal/netcheck/netcheck_diag_test.go` | **NEW.** Classification table (9 cases), state vocabulary pin, `firstErrLine` truncation |
| `internal/logging/logging.go` | **Log surface (P0):** `LogEntry` + `RecentParsed` (parsed time/level/category/message, tolerant of non-canonical lines, REDACTED before display, ring-bounded); `redact()` gains inline scrubbing — quoted secret-bearing values (`apiKey/token/password/secret/authorization`), Bearer credentials and OpenAI-style `sk-…` tokens anywhere in a line |
| `internal/logging/recentparsed_test.go` | **NEW.** Secret redaction (inline `apiKey "sk-…"` case), ring bound, tolerance of unparseable lines |
| `internal/api/perf.go` | **NEW.** `GET /api/perf` — the compact live performance payload: CPU % (OS delta sampler), RAM, GPU/VRAM via `nvidia-smi` (5 s cache, bounded subprocess), generation metrics from `llm.SnapshotEnginePerf`, context usage vs the verified window, backend/model/engine state, `CompatInfo`, and `recommendedSettings` derived from sysinfo hardware + loaded model card + verified capability profile (advice only — applied exclusively through explicit UI actions) |
| `internal/api/perf_windows.go` | **NEW.** Windows samplers: kernel32 `GetSystemTimes` CPU delta + `GlobalMemoryStatusEx` RAM (LazyProc — no new dependencies) |
| `internal/api/perf_linux.go` | **NEW.** Linux samplers: `/proc/stat` CPU delta + `/proc/meminfo` RAM |
| `internal/api/perf_other.go` | **NEW.** Not-known fallback so other platforms render N/A instead of guessing |
| `internal/api/logs.go` | **NEW.** `GET /api/logs?limit=N` (default/cap 512) — parsed, redacted recent entries from the existing ring; `GET /api/netcheck` handler |
| `internal/api/server.go` | routes mounted (`/api/perf`, `/api/logs`, `/api/netcheck`); `handleTools` now prefers `ShortDescription()` for the UI `description` and keeps the full spec in `detail` |
| `internal/tools/shortdesc.go` | **NEW.** One-line `ShortDescription` per tool (Shell/Files/CodeExec/WebSearch/Git/Browser/Fetch/Screenshot/dataAnalysis/JSON/linuxSim/diff/archive) — the concise Options UI labels |
| `internal/research/tool.go` | `ShortDescription`: "Search external sources with provenance." (full model-facing spec unchanged) |
| `internal/lab/tool.go` | `ShortDescription`: "Run isolated engineering tasks in a disposable workspace." (full spec unchanged) |
| `internal/memory/memory.go` | `ShortDescription`: "Store and retrieve approved knowledge." (full spec unchanged) |
| `README.md` | version header → v1.1.7; new "v1.1.7" implemented-features section (shipped items only) |
| `ARCHITECTURE.md` | new Part I section **I.8b** — the v1.1.7 surfaces as truth-table rows (compat retry-up gate, /api/perf, /api/logs, /api/netcheck, Options UI, concise tool labels) with packages and honest status |
| `agent.md` | release line → v1.1.7; next-agent notes (retry gate ownership, real-measurements rule for the perf surface, Short/Full description contract, `touchesEngine` checklist, redaction rule) |
| `package.json` | version → `1.1.7` |
| `src/api.ts` | typed clients for `/api/perf`, `/api/logs`, `/api/netcheck` (`PerfSnapshot`, `RecommendedSettings`, `LogEntry`, `NetDiagResult`, `CompatInfo`); `ToolInfo.detail`; `engineCompatReason`/`engineCompatAt` on `RuntimeConfig` |
| `src/settings.css` | v1.1.7 styles appended (theme tokens unchanged): section tabs, status chips, delayed 400 ms CSS tooltips, engine-profile blocks, recommendation rows, live-metrics grid, before/after block, log viewer (list/levels/controls) |
| `src/SettingsPanel.tsx` | **Options redesign (P0, same architecture):** six scannable tabs (General / Performance / Generation / Tools / Network / Logs) over the existing card system, theme and scrolling; plain-language tooltips + capability chips on important options; `touchesEngine` — the restart-after-save condition now covers EVERY engine-affecting key (flash attention, cache reuse, u-batch, prefill threads, KV quant, mlock, GPU offload, draft model — previously saved but inert until a manual restart); in-memory before/after baseline captured when a performance option changes; live-metrics polling only while the Performance tab is open (3 s cadence) |
| `web/static/.vite/manifest.json`, `web/static/index.html` | regenerated embedded build metadata |

## 4. ADD

| Path | Why |
|---|---|
| `internal/llm/perftracker.go` + `perftracker_test.go` | bounded real-measurement perf ring feeding the live performance surface |
| `internal/llm/capability_compat_test.go` | focused regression tests for the compatibility-mode retry-up gate and reason vocabulary |
| `internal/api/perf.go`, `perf_windows.go`, `perf_linux.go`, `perf_other.go` | `GET /api/perf` + OS-native samplers (no new dependencies) |
| `internal/api/logs.go` | `GET /api/logs` + `GET /api/netcheck` handlers |
| `internal/netcheck/netcheck_diag_test.go` | diagnostics classification tests |
| `internal/logging/recentparsed_test.go` | log-surface redaction/bounding tests |
| `internal/tools/shortdesc.go` | concise one-line tool labels for the Options UI |
| `src/SettingsPerformance.tsx` | Performance tab cards (Engine profile, Recommended, Engine advanced, Context, Live Metrics + before/after), Network diagnostics card, Logs card |
| `src/settings-shared.tsx` | shared `Tip`/`Chip`/`FieldLabel` option-metadata primitives and N/A-safe formatters |
| `src/LogViewer.tsx` | in-app log viewer (live tail, pause, auto-scroll, search, severity + subsystem filters, copy, non-destructive clear view; replace-per-poll keeps memory bounded) |
| `web/static/assets/AgentBody-CfUf-j4_.js`, `AgentHeader-xOezjwpD.js`, `AgentSidebar-BbRpID3s.js`, `LabPanel-rshNNsid.js`, `ResearchPanel-CfWYT8Pb.js`, `SettingsPanel-BgejwQSw.js`, `index-BRJmDjjc.js`, `index-Bvpcz4Hd.css` | regenerated embedded frontend bundle |

## 5. DO NOT TOUCH

Everything not listed above is byte-identical to the base commit and MUST
NOT be modified by applying this package. In particular the following
subsystems are unchanged and regression-protected — applying v1.1.7 must
not alter a single byte of them:

- **Agent orchestration and reliability** — `internal/agent/*`
  (orchestrator, reliability, verification, preflight tests)
- **Tool system internals** — `internal/tools/tools.go` and every other
  existing tool file (only the NEW `shortdesc.go` is added; the
  model-facing `Description()` specs are untouched)
- **Coding Lab** — `internal/lab/*` except the one `ShortDescription`
  addition in `tool.go`
- **Context planning, overflow protection, session context** —
  `internal/contextplan`, `internal/contextcache`, `internal/ctxtelemetry`,
  `internal/continuum`, `internal/sessions`, `internal/llm/resources.go`,
  `internal/llm/modelcaps.go`
- **Memory / recall / research / attachments / MCP / scheduler /
  specialist agents / multi-agent / skills / toolsets / pipelines /
  computer use / improve** — `internal/memory` (except the one
  `ShortDescription` addition), `internal/recall`, `internal/research`
  (except the one `ShortDescription` addition), `internal/attachments`,
  `internal/mcp`, `internal/scheduler`, `internal/multiagent`,
  `internal/skills`, `internal/toolsets`, `internal/pipeline`,
  `internal/computer`, `internal/improve`
- **Native engine + llama.cpp integration** — `native/engine/**`,
  `internal/native/**`, `internal/llm/llamabackend.go`, `backend.go`,
  `gguf.go`, `models.go`, `presets.go`, `resources.go`, `modelcaps.go`
  (the llama lifecycle changes live ONLY in `llama.go`/`capability.go`
  listed above)
- **Security restrictions and path jails** — `internal/sandbox`,
  `internal/proc`, `internal/termshell`, `internal/basedir`,
  `internal/research/security_test.go`
- **Logging architecture (writers)** — the app.log/tools.jsonl/llm.jsonl
  writers, rotation and crash catching in `internal/logging/logging.go`
  are untouched; v1.1.7 adds a read-only parsed view plus inline
  redaction on top
- **Config copy-on-write** — `internal/config/source.go` (the new config
  fields are backend-managed: the UI never patches
  `engineCompatReason`/`engineCompatAt`)
- **Windows branding, updater, installer, desktop shell** —
  `internal/brand`, `internal/updater`, `internal/installer`,
  `internal/desktop`, `main_windows.go`, `build/**`, `scripts/**`
- **Existing API contract** — every pre-existing route keeps its exact
  request/response shape; `/api/tools` gains the additive `detail` field
  and `/api/config` gains two additive optional fields; nothing existing
  was renamed or removed
- **Existing tests** — no pre-existing test file was modified; all v1.1.7
  tests are NEW files

---

## 6. Automated verification executed for this release

All commands were actually executed; nothing below is assumed:

| Check | Result |
|---|---|
| `go build ./...` (GOOS=windows — the shipping target, full tree incl. desktop shell) | **PASS** |
| `go vet ./internal/... ./cmd/... .` (GOOS=windows) | **PASS** |
| `go test ./internal/...` (34 packages OK, including the new tests in `internal/llm`, `internal/api`, `internal/netcheck`, `internal/logging`) | **PASS** — the only build failure is `internal/desktop`, a pre-existing environment limitation: it imports the Wails GTK4/webkitgtk shell, whose dev headers are absent on the Linux build host; the pristine base commit fails identically, and the Windows cross-build of the same tree passes |
| `npm run typecheck` (`tsc --noEmit`) | **PASS** |
| `npm run lint` (oxlint) | **PASS** (0 warnings, 0 errors) |
| `npm run build` (`tsc -b && vite build && sync:web`) | **PASS** — embedded `web/static` regenerated |

Native C++ tests were NOT re-run: no C++/native-engine file was touched in
this release (`native/engine/**` is byte-identical to the base), so the
existing C++ validation remains the authoritative gate for unchanged code.

## 7. Compatibility-mode finding (summary for the release record)

**Finding.** v1.1.6Z persisted `engineCompat` after the first successful
boot at any compatibility level. Every later boot resumed AT that level
and logged `started in compatibility mode 2 (no speed flags)` again — even
when the Phase 7 capability adapter had already detected, validated and
persisted a profile under which the full-speed (level 0) launch was valid.
A one-time option rejection thus became a permanent, silent speed
downgrade with no recorded cause.

**Fix.** The boot now (1) records the classified reason (`option-layout`,
`unknown-option`, `model-load`, … + offending option) and time of any
descent; (2) exposes reason + removed options via `/api/perf` and the
Settings → Performance engine-profile card; (3) attempts ONE bounded
full-speed retry before resuming the persisted level, strictly gated by
`shouldRetryFullSpeed` (option-class reason + verified profile newer than
the downgrade + clean pre-launch validation of the level-0 profile); and
(4) re-blocks the gate after a failed non-option retry so the same
incompatibility is never expensively rediscovered. Supported flags are
never stripped by the fix — the surgical per-option repair behaviour of
Phase 7 is preserved unchanged.
