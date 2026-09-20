# SHEYTAN-LA™ (SHEYTAN Local Agent)

> **A local-first AI software-engineering laboratory.**
>
> The model proposes. The tools execute. The laboratory verifies.
> SHEYTAN calculates the best runtime for the machine, the model and the task.

SHEYTAN™ Local-Agent is a local-first desktop AI engineering environment built around Go, React/TypeScript, Wails v3, managed llama.cpp inference, controlled tools, isolated coding workspaces, research, memory, recall, and objective verification.

**SHEYTAN™ is a trademark of Parsaetak · © 2024–2026 Parsaetak. All rights reserved.**

Licensed under the **Parsaetak Proprietary License v1.1** (see `LICENSE`).

```text
Application:      SHEYTAN-LA (SHEYTAN Local Agent)
Current release:  v1.2.9
Codename:         Zeta
Executable:       SHEYTAN-LA.exe
AppUserModelID:   Parsaetak.SHEYTAN-LA
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

## v1.2.9 — Stabilization, Security & Repository Cleanup

**v1.2.9 is a stabilization release: the verified architectural flaws of the v1.2.8 line are fixed at the root (prompt-injection authority boundaries, durable completion ordering, concurrent handoff safety, honest context budgeting), the runtime correctness repairs land (pipeline stage timing, scheduler crash-replay, artifact fingerprints, MCP size limits, updater trust wording), CI gains a race gate and frontend unit tests, and the repository is cleaned of obsolete release-package leftovers. No architectural rewrite, no new subsystem.**

| Area | Change | Status |
|---|---|---|
| **Cross-mode history authority** | Retrieved history rides the USER (data) channel with an automated-attachment header — never a `system` instruction channel; zero-relevance references resolve to NO block (no newest-turn fallback); history search scans ALL bounded summary projections (decisions, facts, files, tools, errors, unresolved, next step), not just title/objective/state | IMPLEMENTED, TESTED (adversarial injection regressions) |
| **Summary authority boundary** | The rolling session summary is labeled unverified derived conversation memory and injected on the data channel — never as system context; only genuinely authoritative information occupies authoritative channels | IMPLEMENTED, TESTED |
| **Durable completion ordering** | `persist required durable state → settle → publish terminal completion`: the summary roll, agent.md handoff, recall index and continuum rollover all land BEFORE the run flips terminal | IMPLEMENTED, TESTED |
| **Concurrent agent.md handoff** | Handoff writes serialize per workspace path (read-splice-rename under a keyed lock) — concurrent runs can no longer silently drop each other's handoff section | IMPLEMENTED, TESTED (race-clean) |
| **Context budget honesty** | Tiered token accounting (exact engine tokenizer when the native path serves it → model-family estimator → CJK-aware conservative fallback); no more fabricated context (the 1024 floor and fixed 512 reserve are gone); the history floor is bounded by the real window; plans carry an estimator label instead of claiming exactness | IMPLEMENTED, TESTED |
| **History reference hygiene** | Dead references (source session deleted) are actually pruned by `validateHistoryRefs` and the normalized result persists | IMPLEMENTED, TESTED |
| **Runtime correctness** | Pipeline stage durations anchor at each stage's own start; scheduler timer claims are durable (crash between claim/execute/persist cannot double-fire); artifact fingerprints carry size+modtime+bounded content digest (same-size rewrites detected); MCP results are size-capped BEFORE decoding with a bounded read loop; updater trust wording distinguishes transport/integrity/manifest/signature and version comparison follows semver | IMPLEMENTED, TESTED |
| **CI** | New focused race gate (`internal/api`, `internal/agent`, `internal/sessions`, `internal/contextplan`, `internal/histref`, `internal/runtime`) and `npm run test:units` added to every frontend gate; all existing gates unchanged | IMPLEMENTED |
| **Repository cleanup** | Obsolete v1.2.7/v1.2.8.1 update-package leftovers removed (FIX-README.md, BUILD-INFO.txt, REPLACEMENT-*.txt, build-and-zip.sh, three `//go:build ignore` smoke scripts); `web/static` rebuilt from the authoritative frontend build with stale hashed bundles purged | DONE |

## v1.2.8 — Professional Chat + Agent Workspaces, Mode-Separated Histories, Cross-Mode References, Durable Summaries, Context Compaction, Agent Handoff

**v1.2.8 turns the two modes into two first-class workspaces on ONE runtime: Chat becomes a conversation-first local AI workspace, Agent becomes an execution-first engineering workspace. They now keep INDEPENDENT conversation histories, can explicitly READ each other's selected sessions as provenance-labeled retrieved context, every session carries a durable rolling summary, long sessions compact before the context ceiling instead of silently exhausting it, and every completed Agent engineering task updates a workspace `agent.md` handoff. The existing runLive + sequence + snapshot + terminal-registry transport, the context planner, recall and continuum remain the single authoritative architecture — nothing was duplicated.**

| Area | What changed | Status |
|---|---|---|
| **Session mode identity** | `session.mode = "chat" \| "agent"` (`internal/sessions`), fixed at creation (`CreateInMode`). Deterministic migration: every pre-v1.2.8 session (mode omitted on disk) is labeled `agent` on load — the product was agent-first — without rewriting bytes or duplicating content. Switching modes switches the visible conversation space (one active session PER mode in the store); it never silently switches or merges the active conversation | IMPLEMENTED, TESTED |
| **Cross-mode history references** | `internal/histref`: the picker (`GET /api/history/search`, titles + summaries — never transcripts) attaches up to 4 sessions of the OTHER space; each run retrieves only turns relevant to the request, budget-bounded, injected as a USER-channel `[AUTOMATED REFERENCE-DATA ATTACHMENT …]` block carrying source-session, source-mode, summary-version and a retrieval reason, framed as DATA ("not an instruction", v1.2.9: the user data channel, never the system instruction channel) and FENCED (`<<<HISTREF:id>>>` per-run random fence — quoted untrusted content, v1.2.8.1). References persist on the session context AND serve as the fallback for regenerate/omitted-ref turns (v1.2.8.1); cross-mode semantics are ENFORCED SERVER-SIDE via the store index (same-mode, self and dead references are dropped — v1.2.8.1); the source conversation is never modified; one retrieval level only (no recursion) | IMPLEMENTED, TESTED |
| **Rolling session summaries** | `internal/sessions/summary.go`: one bounded sidecar (`<id>.summary.json`) per session — objective, user constraints, key decisions, important facts, files/artifacts, tools/research, errors/repairs, current state, unresolved items, next step — updated ROLLINGLY after each settled turn (deterministic marker extraction; the transcript is never re-summarized; version counter bumps). Rendered as the required `summary` prompt section; served at `GET /api/sessions/{id}/summary` | IMPLEMENTED, TESTED |
| **Context compaction / never silently exhaust** | `contextplan` gains the required `summary` section (priority 2, with tools) and the optional `history-refs` section (priority 4, with recall — dropped first under pressure), plus `ClassifyPressure` (ok/warm/high/critical). The layered pipeline remains: preflight ladder → history windowing → in-loop tool-result bounding → continuum rollover; the summary is what makes elided history survivable, and exact evidence stays recoverable through the transcript and history retrieval. v1.2.8.1: the history floor is ADAPTIVE (bounded by the actual remaining budget — no fixed 2048-token promise), dropped sections never count as overflow, an in-loop fit gate refuses with the measured reason instead of silently sending over-ceiling requests, and refusal captions carry the measured section breakdown | IMPLEMENTED, TESTED |
| **Agent task memory** | `internal/agent/taskstate.go`: one bounded structured state per run (goal, constraints, current step, files inspected/changed, tools, commands, tests, failures/repairs, verification, artifacts, next step) maintained ONLY from observed tool traffic. Published as a `task` activity per tool round, folded into `runLive`; the `run_snapshot` WIRE frame carries the `task` block (v1.2.8.1 — the v1.2.8 frame omitted it) so a reconnecting UI restores the task panel — the run never restarts because the UI did. Survives settlement via the summary + handoff | IMPLEMENTED, TESTED |
| **agent.md handoff** | `internal/agent/handoff.go`: after EVERY COMPLETED agent run, `<workspace>/agent.md` gains/updates a marker-bounded `# Latest Agent Handoff` section (task, objective, current state, changes, files, tests+verification, evidence, failures, remaining, next action, do-not-redo). Runs with no engineering evidence write the honest `No engineering changes were made.` handoff (v1.2.8.1 — the v1.2.8 evidence gate silently skipped them). Everything outside the markers is preserved byte-for-byte (no whitespace re-normalization); the write is durable (unique temp + fsync + rename + dir sync) and verified by READ-BACK before the handoff is reported; the filename is lowercase `agent.md` by contract; written only after the outcome is known; failures surface as an error activity | IMPLEMENTED, TESTED |
| **Chat UX** | Session rename (inline, double-click or ✎), per-space search, delete, copy-response, mode-filtered sidebar, cross-mode history picker with summary previews and multi-select, attached-reference chips in the composer, load-earlier paging | IMPLEMENTED, TESTED |
| **Agent UX** | Execution-first pipeline panel rendering the REAL task state (inspect/act/test/verification) from backend evidence — no decorative stage strip; concise activity summaries only; no chain-of-thought exposure | IMPLEMENTED, TESTED |
| **History paging** | `GET /api/sessions/{id}/messages?before=&limit=` serves bounded pages (newest first, absolute indices, hasMore/nextBefore); the frontend loads the newest page and fetches older history on demand; streaming and settlement are unaffected | IMPLEMENTED, TESTED |
| **Root-cause fix (recovery fast path)** | `runOutcome.endedAt` is marshalled as an RFC3339 STRING on the wire (pinned by the Go contract test) while the frontend parser accepted only epoch-ms numbers — the authoritative lastRun fast path in `recoverRunFromIdle` could never fire. The parser now accepts both shapes | FIXED, TESTED |
| **Security** | No weakening: history references are DATA (never authority, never auto-authorization), provenance is mandatory, retrieval is read-only, one level deep, and bounded; workspace/shell/network/snapshot/provenance surfaces are unchanged | VERIFIED |

## v1.2.7 — Run Transport Terminal-State Repair, CI/Test Stabilisation, Release Consistency

**v1.2.7 is a stabilization release: one real run-transport lifecycle bug fixed at the root, the run-lifecycle audit closed with three new regression tests, every test surface green, and the release identity + documentation re-synchronized. No architectural rewrite, no queue system, no new runtime.**

| Area | What changed |
|---|---|
| **Root cause fixed (run transport terminal state)** | The activity WebSocket attach path treated `s.runs[sessionID]` map membership as "a run is active". But between a run's terminal settlement (`live.settleTerminal`) and its deferred registry cleanup (which first releases the run budget and memory-manager tracking) the entry still exists while the run is ALREADY authoritatively terminal. A socket attaching inside that window received a stale terminal `run_snapshot` and then parked on `clientGone` without any terminal marker — the exact CI failure of run 35367243405 (`TestStaleRunEventsFilteredByServer`: "expected idle, got run_snapshot"; reproduced locally at ~1-in-5, 0/15+ after the fix). The attach path now consults the authoritative `runLive` state: a terminal entry falls through to the standby path whose idle sentinel carries the recorded `lastRun` outcome. `runLive` remains the ONE lifecycle authority — no second one was introduced |
| **Visibility gap closed (outcome registry ordering)** | `settle()` now records the outcome in the bounded registry BEFORE the authoritative state flips terminal. A socket that observes the run as terminal can therefore never miss the `lastRun` block of its idle sentinel (the happens-before edge runs through `settleTerminal`'s mutex) |
| **Terminal-recovery fallthrough** | A hub that closed before any post-snapshot event existed (e.g. an abort that settles without publishing a terminal activity) previously parked the socket on `clientGone` forever. It now falls through to the idle sentinel (with the recorded outcome) and the standby loop, which attaches to a replacement run the moment one starts |
| **Regression tests added** | `internal/api/runtransport_v127_test.go`: terminal-registry-entry gate (attached → idle, never a stale snapshot), closed-hub fallthrough (snapshot → idle, no hang), and end-to-end abort-then-attach recovery (idle + recorded outcome, no stale run_snapshot) |
| **Frontend transport verified** | Re-audited against the fixed server: `attached` ack, `idle` recovery via the authoritative `lastRun` block, terminal-`run_snapshot` finalisation, `runId`+`seq` duplicate filtering, reconnect replay — all compatible; no frontend changes required. The `idle` dispatch is guarded (only meaningful while the UI believes a run is live), so the extra sentinel after a fallthrough is a no-op |
| **Version** | `1.2.6` → `1.2.7` through the established identity chain (package.json → `release-version.mjs` → config.go / build/config.yml / SIGNATURE); README Installation section corrected to the actual v1.2.x package layout |
| **Verification** | Go: `go test ./internal/... -tags headless`, `go test ./... -run Test`, `go vet ./...`, race-detector runs on the transport/scheduler/runtime/continuum/sessions packages — all green; the previously flaky `internal/api` suite looped 15× clean. Frontend: typecheck, lint, 39 unit tests, production build. Native engine: cmake + ctest 12/12. Stress: 47/47 scenarios, 0 hangs, 0 crashes |

## v1.2.6 — Authoritative Run Transport, Deterministic Attach, Measured Timing

The missing-answer race was closed at the transport layer: every run owns a bounded authoritative state (`internal/api/runstate.go`: phase, cumulative response/reasoning snapshots, monotonic sequence, exactly-once terminal settle) folded from ONE publisher; the first frame after WebSocket upgrade is an explicit `attached` ack carrying the server clock; every attachment receives a `run_snapshot` (seq/runId-filtered, gapless + duplicate-free replay) before live events; idle sentinels carry the authoritative `lastRun` outcome (bounded per-session ring + LRU); API-side run stages are measured end-to-end; startup no longer blocks on the deep hardware probe; the accelerator resolution carries explicit evidence fields (available / selected / executionVerified / fallback) instead of assuming GPU capability from a DLL. See `worklog.md` (v1.2.6 entries) for the full log.

## v1.2.5 — Adaptive Context Tiers, Thinking Mode, Per-Request Telemetry

Context became evidence-driven instead of pay-per-turn: measured adaptive tiers (FAST/STANDARD/THINKING) start small and escalate on evidence with visible tier escalations, replacing the fixed ~10K-token prefill every trivial turn previously paid; global and per-request tool control; thinking mode with streamed reasoning panels; per-request timing telemetry with honest stage labels. See `worklog.md` (v1.2.5 entries) for measurements.

## v1.2.4 — Faster Data Processing, Memory Reclamation, Workspace, Usability

Faster data processing paths, coordinated memory reclamation (run-aware cache shedding with `TrackRunStart`/`TrackRunEnd`), a genuinely useful Workspace panel and a whole-app usability pass. Packaged as the first `UPDATE` ZIP with a replacement manifest + SHA-256 sheet. See `worklog.md` (v1.2.4 entry).

## v1.2.3 — CI Engine-Race Fix, Download Manager, Honest Download Progress

**The failing Linux CI job is fixed at its ROOT (a real engine guard gap, not a flaky test), every remote asset the app pulls now flows through one reusable, verifiable, resumable Download Manager with live UI progress, and the update/installer surface became asynchronous and cancellable. No new runtime, no second download path.**

| Area | What changed |
|---|---|
| **CI root cause (engine guard coherence)** | `test_generate.cpp`'s unload-guard raced: it polled the scheduler's observable `active_requests` while the load/unload guard counted a DIFFERENT variable (`active_generations`, set later inside the executor). An unload landing in the window SUCCEEDED, tore the model out from under the in-flight generation and failed the CHECK (~25% of loaded CI runs; repro'd locally at 28/200 before the fix, 0/1200 after). The engine guard now covers the WHOLE in-flight window (registered work → scheduler slot → executor count) in one authority (`generation_in_flight`), so observable state can never contradict the guard. The test itself now synchronizes on an EXPLICIT signal — the first streamed token, emitted only from inside the runner's decode loop — with a bounded condvar wait (no polling budget, no sleeps), asserts BOTH the unload AND reload rejection plus scheduler coherence, verifies the cancelled result and engine reusability. Deterministic under CPU saturation (verified: 3/3 loaded runs, 5/5 niced runs, 10/10 idle runs) |
| **Download Manager (`internal/downloader`)** | One native-Go mechanism for llama.cpp archives, future model packages, mmproj/projector assets and app updates: HTTPS-only by default (enforced across redirects; loopback exemption for tests only), persistent pooled connections, single-stream by design, streamed to `<dest>.part` → SHA-256/size verify → atomic rename (a partial or corrupt asset is NEVER activated), HTTP-Range resume of interrupted transfers, previous-version `.bak` preservation for rollback, ordered sources (authoritative release asset → configured mirror → fallback ONLY with an explicit `AllowFallback` opt-in — never a silent untrusted mirror), a TTL'd source cache so startup does not re-probe every endpoint, bounded exponential backoff + jitter, permanent 4xx never retried, immediate cancellation, pause/resume, and measured progress (phase, bytes, speed EWMA, ETA, source, verification state, retries) |
| **Engine bootstrap + self-update** | `ensureBinary`, the model-architecture self-update and the scheduled engine updater all run on the Download Manager now; progress rides engine events (WS + `/api/engine`) and the runtime UI; engine downloads are cancellable (`POST /api/llama {action:"cancel-download"}`) and resume instead of restarting |
| **App updater** | `POST /api/update/download` returns IMMEDIATELY (no more 5-minute blocking request); staging runs on the manager with SHA-256 + size pinned from a freshly re-fetched manifest; `GET /api/update/status` carries live progress; `POST /api/update/cancel` stops network + file activity at once (the .part resumes on retry); `Close()` cancels in-flight staging so the updater can never outlive the server |
| **Downloader UI** | One shared visual language (`DownloadProgressPanel`): the Resolving → Connecting → Downloading → Verifying → Installing → Ready phase chain, determinate bar with %, MB/s, downloaded/total, ETA, active source + trust label, verification badge, retry reasons, and Cancel/Retry actions — wired into the Updates card (Settings), the Agent runtime panel and the System Centre; the header phase pill shows the live percentage during engine bootstrap |
| **UX coherence pass** | Model picker renders loading skeletons instead of mistaking "not loaded" for "no models" (`modelsLoading` in the store, `aria-busy`); download panels are `role="status"`/`aria-live="polite"`; busy states on update actions; the unused `.m-progress-track`/`.m-skeleton` primitives are now real components' foundations; reduced-motion collapse still applies to every new animation |
| **Warnings** | The two known native C++ warnings are cleaned at the root (dead `utf8_encode` removed, unused `nbytes` local removed) — no suppression pragmas; a clean rebuild emits zero warnings |
| **Hardening suite** | Two new release stress scenarios: `downloader_integrity_resume` (Range resume → verified activation → no `.part` residue) and `downloader_untrusted_fallback` (fallback source NEVER contacted without explicit opt-in; checksum mismatch refused). 47/47 stress scenarios, 14 downloader tests (race-clean ×3), full native 12/12 |
| **Version** | `1.2.2` → `1.2.3` through the established identity chain (package.json → release-version.mjs → config.go / build/config.yml / SIGNATURE) |

## v1.2.2 — Generation Visibility & Black-Screen Repair

**A focused stability repair: the System-tab black screen is fixed at its root (error boundaries everywhere + the `gpus: null` wire defect), the mid-generation tab-switch composer freeze is eliminated (socket ownership with a live-run lease + `idle`-sentinel resync), the live generation timeline makes every run visible from Send to completion, and engine/log reporting becomes honest. No architecture changes, no feature work beyond the visibility brief.**

| Area | What changed |
|---|---|
| **Black screen (root fix)** | React error boundaries at three levels — app root (`AppErrorBoundary`), every lazy workspace panel (`PanelErrorBoundary` with a recover/reload card and view-switch reset) — so one panel's render failure can never blank the application again. The actual crash source is fixed on the wire: `hardware.Collect` and `sysinfo.Probe` now guarantee `"gpus": []` (a nil Go slice marshalled as JSON `null`, and `DeviceCard` read `gpus.length` off it — WMI/PowerShell GPU probes fail under heavy inference load, which is why the System tab crashed DURING generation) |
| **System-tab hardening** | Every optional payload field in DeviceCard / RuntimeCard / RecommendationCard / HealthCard is read defensively (`Array.isArray` guards, `??` fallbacks); the environment + health probes are `AbortController`-cancellable and ignore results after unmount; recommendation application guards its state setters against unmounted continuation |
| **Tab-switch mid-generation** | AgentBody now ACQUIRES/RELEASES the activity socket and engine poll instead of hard-connect/disconnect. A live run holds the lease across workspace switches — the backend hub has no event replay, and the old unmount-disconnect lost every mid-run event including `done`, leaving `running` stuck true and the composer frozen forever. The backend's `idle` sentinel is now handled: a run the UI believes live but the backend no longer registers is re-synced from the authoritative history (with a grace guard against the standby attach race). No duplicate sockets/pollers on return — release happens exactly when the run settles and the last consumer is gone |
| **Live generation timeline** | A generation bubble mounts the INSTANT Send is accepted and walks the real backend-driven lifecycle `Preparing → Thinking → Generating → Finalising → Complete` (pure state machine in `run-phase.ts`, unit-tested). Elapsed clock, collapsible reasoning panel (auto-open while thinking, auto-fold when the answer streams, user intent wins), streamed answer text, progressive tool/context/engine activity alongside, and a settled outcome note (Complete/Stopped/Failed with the backend's own caption) |
| **Streaming duplication fix** | The orchestrator's `emitProgress` publishes CUMULATIVE captions; the store previously APPENDED every caption, duplicating the streamed text massively. The new accumulator (`stream-accumulator.ts`, unit-tested) treats each event as an authoritative snapshot — replace semantics make replayed/duplicate frames after reconnects idempotent by construction |
| **Partial output preservation** | `done` keeps the streamed bubble visible through Finalising until the authoritative history replaces it (no blink); errors and aborts promote the partial reply into the conversation locally (marked, never presented as persisted) when — and only when — the history did not actually gain a reply. Abort unlocks the composer immediately with a bounded fallback finalisation |
| **Engine asset honesty** | `llamaDownloadURL` no longer reports "no prebuilt asset … (upstream no longer publishes Linux binaries)" on Windows machines whose `api.github.com`/Atom requests are blocked. Network failure and genuine asset absence are classified (`updater.IsNoAssetError`, unit-tested) and reported with their real remedy; the classification is logged as a WARN, never fatal to the UI |
| **Log session clarity** | Every process start writes one unambiguous session banner (`==== SHEYTAN-LA v1.2.2 session start (pid N) ====`) through the normal ring/rotation pipeline — stale v0.8.0 startup entries above a banner are verifiably historical in the file AND the in-app Log Viewer |
| **Version** | `1.2.1` → `1.2.2` through the established identity chain (package.json → release-version.mjs → config.go / build/config.yml / SIGNATURE) |

## v1.2.1 — CI Package-Root Contract, Installer Options, Packaging Hardening

**A targeted release-quality repair: the Linux ZIP verification failure (run 34871838054) is fixed at the ROOT — one canonical package-root variable per platform — the Windows installer gains a genuine desktop-shortcut option and a hardened upgrade/uninstall path, and the packaging gates are strictly stronger than before. No feature work, no architecture changes.**

| Area | What changed |
|---|---|
| **Canonical package-root contract** | The workflow now defines `WIN_PKG_ROOT` / `LINUX_PKG_ROOT` once and derives every staging directory, ZIP name, ZIP entry check, artifact name and release-metadata reference from them. The root cause of the failing Linux job: the ZIP was created under `SHEYTAN-Local-Agent/` while its verifier expected `SHEYTAN-LA/` entries — and `internal/releasecontract.RequiredLinuxZipEntries()` itself mixed roots, so the stress gate agreed with the workflow instead of with reality. The contract now exposes the workflow-slot spellings and the gate fails on any drift in EITHER direction |
| **Stress gate strengthened** | `zeta_release_surface` requires the canonical root variables and the parameterized verification spellings (`${env:WIN_PKG_ROOT}/…`, `${LINUX_PKG_ROOT}/…`) to be present in the workflow text; duplicated root literals can no longer pass. 14 new bounded scenarios cover the updater (offline / tampered SHA-256 / size mismatch / staged drift / zip-slip members), vision honesty, memory/session robustness, bounded context machinery and the LoopGuard retry ceiling — suite now prints a machine-readable `STRESS-RESULT` line |
| **Installer: desktop shortcut option** | `packaging/nsis/installer.nsi`: the previously unconditional desktop shortcut is now a checkbox on the directory page (`Create a &desktop shortcut`), DEFAULT CHECKED; unchecked means no desktop shortcut. State persists across Back/Next; silent installs keep the default |
| **Installer: clean upgrades** | A running instance is closed before the payload is replaced (graceful `taskkill`, then bounded retry of the locked executable, then an explicit Retry/Cancel message) — no silent partial upgrades, no uncontrolled loops. The developer-path default `BUILDDIR` was one level short of the repository root; the compile-time assertion now catches that class instead of a generic "no files found" |
| **Installer: uninstaller hardening** | Per-machine shell context (`SetShellVarContext all`) for shortcuts and their removal; uninstall removes the application, Start Menu + desktop shortcuts, ARP registration and the AppUserModelID — and preserve user data BY CONSTRUCTION: plain `RMDir` (the recursive variant is now FORBIDDEN by a CI contract check on the script) can only remove an empty install directory, so models/workspace/sessions/config survive untouched |
| **Packaging verification hardening** | In-package `BUILD-INFO.txt` must carry the resolved version (Windows + Linux); the NSIS SOURCE is contract-checked in CI (directory page, shortcut option, uninstaller registration, `SHEYTAN_DATA_DIR`, user-data preservation, no recursive deletes) BEFORE any installer is built from a weakened script; job-level `timeout-minutes` bound every job; the native engine build is cached on the full `native/engine/**` source hash (reproducibility: exact source hash key, ctest still runs) |
| **Version** | `1.2.0` → `1.2.1` through the established identity chain (package.json → release-version.mjs → config.go / build/config.yml / SIGNATURE) — the app updater needs a strictly higher version to offer the improved installer |

## v1.2.0 — SHEYTAN-LA Unified Product Upgrade

**The productisation release: vision readiness you can trust, hardware intelligence, evidence-based recommendations, a polished chat surface, SHEYTAN-LA Windows identity, an installer and a verified update path. Same architecture — no subsystems were rebuilt or replaced.**

| Area | What it does |
|---|---|
| **Vision readiness pipeline (P0)** | A real state machine — `unsupported → supported → projector-missing → projector-found → loading → ready` (plus `degraded` / `failed`) — derived from GGUF architecture detection, projector discovery/pairing and the engine's own verified boot. The UI can no longer claim "Vision: Yes" from a filename: `ready` appears only when the engine is actually serving WITH the paired mmproj, and `degraded` is reported when the projector failed with every launch profile and the engine fell back to text-only. Per-model state is exposed on `/api/models`; the runtime state lives on `/api/engine` |
| **mmproj as a first-class asset** | `/api/models` now carries `visionState`, `visionReason`, `mmprojPath`, `mmprojName`, `mmprojSizeBytes` and `mmprojVerified` per model — the ModelPackage view (language model + projector + capabilities + resource estimate) without breaking the existing model representation. A projector GPU-offload posture (`auto`/`on`/`off`) reaches the engine as `--no-mmproj-offload` through the existing capability adapter, which verifies flags against the actual engine build |
| **Model picker upgrade (P0)** | Fact grid now shows Text / Vision (state machine badge with evidence tooltip) / Tools / Native / Context / RAM / VRAM, plus the technical details block: architecture, file size, recommended context, projector name + size + verification state, and the "Why" evidence line. The Recommended chip says "Recommended for your device" — derived from the measured RAM ratio |
| **Chat polish (P0)** | Assistant messages render real Markdown (GFM tables, lists, links) with syntax-highlighted code blocks and one-click code copy; every completed message gains a hover Copy action; images sent through the vision wire format render inline; the composer sends on Enter, newlines on Shift+Enter, auto-grows, accepts pasted images and drag/dropped files (fed through the SAME attachment backend — no second upload path), shows image thumbnails and warns honestly when images are staged for a model without a verified projector. Streaming keeps the rAF-coalescing pipeline untouched — markdown parsing happens once per completed message, never per token |
| **Intelligent autoscroll** | The conversation follows the stream only while the user stays near the bottom; scrolling up suspends the follow and surfaces a "↓ Latest" affordance |
| **Agent progressive disclosure (P0)** | The activity feed groups into run phases — Plan / Execution / Repair / Verification / Run — derived from the event types the backend already emits (nothing new invented); failed phases are highlighted and auto-opened, and a Raw toggle shows the unfiltered feed |
| **Hardware intelligence (P0)** | New `internal/hardware` consolidates the EXISTING `internal/sysinfo` probes plus backend capability evidence (Vulkan presence beside the engine binary, native selection, installed engine tag) into one measured profile. Unavailable values stay unknown — never fabricated |
| **Recommendation engine (P0)** | New `internal/recommendation` — task-aware runtime profiles (chat, coding, research, vision, agent, low-power, maximum) resolve context/threads/GPU layers/ubatch/flash-attention/KV-quant/projector placement from measured inputs, clamped by the model's training limit and resource verdict ladder, with an explicit Reasons list citing every input. The pipeline contract is Detected → Calculated → Recommended → Applied → Measured → Compared → Verified: predictions are labelled predicted; only `/api/perf` speaks in measured numbers |
| **Explain-why UI (P0)** | Every recommendation carries expandable reasoning (Environment Centre "Why (N)", Performance "Why these values"), and predicted effects are separated from telemetry |
| **Three settings levels (P0)** | Settings → Performance gains Simple / Performance / Advanced. Simple shows Quiet / Balanced / Maximum postures (real task profiles resolved by the recommendation engine), recommended context, and automatic Vision/GPU. Performance keeps the existing measure → recommend → verify cards. Advanced points at the raw engine controls in the Advanced tab (single location, no duplication) |
| **Dedicated Vision & Updates settings (P0/P1)** | New **Vision** tab: enable toggle, projector override (Automatic + detected files), projector GPU offload posture, and the DETECTED PROJECTOR evidence card (file, size, family, status). New **Updates** tab: current/latest version, channel, check-now, and a download-&-verify stage action |
| **Environment Centre (P0)** | New **System** view + `/api/environment`: device (CPU/RAM/GPU/storage/identity), runtime (engine phase, verified model, context, vision readiness, engine tag) and the recommendation verdict with Apply — all values are real telemetry |
| **Verified health (P0)** | `/api/health` reports one evidence-backed check per subsystem — engine, model, context, vision, GPU, storage, network, verification, Coding Lab, firewall, application identity — with the underlying proof under each expandable row. Green never means assumed; the identity check honestly reports "code signature: none (unsigned developer build)" |
| **SHEYTAN-LA identity (P1)** | Windows version resources become ProductName `SHEYTAN-LA`, FileDescription `SHEYTAN Local Agent`, CompanyName `Parsaetak`, InternalName/OriginalFilename `SHEYTAN-LA(.exe)`; the process registers AppUserModelID `Parsaetak.SHEYTAN-LA` before the first window (taskbar, jump lists, notifications); the desktop window title is `SHEYTAN-LA — SHEYTAN Local Agent`; CI builds `SHEYTAN-LA.exe` and the portable zip is `SHEYTAN-LA-vX.Y.Z-windows-x64.zip`. No Authenticode signature is faked anywhere |
| **Firewall integration (P1)** | New `internal/platform` firewall manager: explicit, idempotent, removable `netsh advfirewall` rules scoped to the exact process image path + one TCP port, with unit-locked argument vectors and honest `ErrUnsupportedPlatform` behaviour off-Windows. Loopback-first architecture needs no rule by default — the default posture creates none |
| **Unified installer (P1)** | `packaging/nsis/installer.nsi` — a per-machine NSIS installer built by CI (`SHEYTAN-LA-vX.Y.Z-windows-x64-installer.exe`): shortcuts, AUMID registration, version-aware upgrades, clean uninstall that preserves models/sessions/config, and user data declared through the app's own `SHEYTAN_DATA_DIR` override. Models are NEVER bundled |
| **Verified update system (P1)** | New app updater (`internal/updater/appupdate`): the release pipeline publishes a `release-manifest.json`; the app checks installed vs latest with proper version ordering (including prerelease ordering), downloads ONLY a manifest-verified artifact (SHA-256 + size), stages it under `updates/staging/` and hands installation to the user — never executes, never touches user data. States: unknown / up-to-date / update-available / ready / failed |
| **GitHub release factory (P1)** | `build-desktop.yml` now also builds and verifies the NSIS installer, generates SHA256SUMS.txt, generates and validates release-manifest.json, publishes all artifacts only after payload verification, and re-verifies the published release (installer + checksums + manifest present) before declaring success |
| **First-run experience (P1)** | The model picker offers "Use recommended setup": detect device → apply the recommended configuration → pick the smallest fitting model → start the engine. The normal user should need zero configuration knowledge for the first successful chat |
| **Documentation** | README / agent.md / ARCHITECTURE / UPDATE / worklog updated for shipped items only; limitations (no code signing, MSIX not produced, firewall Windows-only) documented honestly |

## v1.1.9 — CI Output Fix, Mode-Aware Product Surface, Explicit Model States

**A focused maintenance release. Same Go + React/TypeScript + Wails + llama.cpp/native-engine architecture; nothing was rebuilt.**

| Area | What it does |
|---|---|
| **CI fix (run `34791882219`)** | Linux and Windows failed release-metadata verification because the audit job's outputs were declared as `steps.identity.outputs.version` / `version_full` / `codename` while the identity step actually writes `APP_VERSION` / `APP_VERSION_FULL` / `APP_CODENAME` — every `needs.audit.outputs.*` resolved to an empty string. The audit outputs now map the exact `APP_*` names the step emits, and the identity step fails fast (with a `::error::` annotation) if any release identity value is empty, so a broken derivation stops the run BEFORE any build job starts. The version flow is unchanged: `package.json` → `release-version.mjs --env` → identity step → audit outputs → `needs.audit.outputs.*` → Linux/Windows environment |
| **Chat / Agent separation completed** | The `[ Chat │ Agent ]` segmented switch stays in the workspace header. v1.1.9 finishes the separation: the sidebar navigation is mode-aware (Chat hides the Coding Lab — planning/verification/pipeline machinery stays Agent-side), the sessions sidebar speaks in the mode's voice, and viewing hidden machinery while in Chat falls back to the workspace. Chat surface: Model, Conversation, Input, Send/Stop, Attachments. Agent surface: Task, Model, Context, Tools, Activity, Verification, Result via the runtime panel, activity stream and Lab. Both modes reuse the SAME sessions, model runtime and engine |
| **Model chooser: explicit states + fact grid** | Each model card now shows an aligned fact grid (Context / RAM / Tools / Vision / Native) plus quantisation and parameters on the facts line, and an explicit state: **Ready** (backend is serving it), **Loading** (a switch to this model is in flight), **Incompatible** (measured footprint exceeds total host RAM), **Available**, plus the Recommended / Compatible / Limited sizing hint. Unknown values render as "—" — nothing is fabricated. The no-models empty state says "Choose a model" with an Open models folder action. There is still deliberately NO Remove action — no deletion API exists |
| **Settings: engine tuning moved to Advanced** | The llama.cpp engine card (threads, GPU offload, flash attention, mlock, KV quant, …) moved from Performance to **Advanced**. Performance now reads as measure → recommend → verify (Engine profile, Recommended, Context, Live metrics). Nothing was removed — all functionality remains |
| **Minimalist UI pass** | Removed the decorative background gradients and the fixed grid overlay; softened shadows; fixed an undefined `--border-soft` token (five component blocks referenced a token that was never declared); fixed a CSS collision where the runtime panel's model facts and the picker's facts line shared one class; dropped the persistent status text in the topbar (the dot remains, with a tooltip), the raw session-ID row and the engine detail line from the Agent panel; trimmed the sidebar footer. Animations remain transform/opacity-only |
| **Targeted optimisation** | `/api/perf` polling in the performance strip now pauses while the window is hidden and refreshes immediately on return; the duplicated model `<select>` option builder in `AgentBody` was unified into one shared function |
| **Documentation** | README / agent.md / ARCHITECTURE / UPDATE updated for shipped items only |

## v1.1.8 — Chat/Agent Separation, Model Picker, Release-Metadata Fix, Minimalist Pass

**A focused polish release. Same Go + React/TypeScript + Wails + llama.cpp/native-engine architecture; nothing was rebuilt.**

| Area | What it does |
|---|---|
| **CI release-identity fix (single source of truth)** | GitHub Actions run `34788709977` failed because `APP_VERSION: "1.1.6"` was pinned in the workflow `env:` block while `release-version.mjs` repaired the tree to 1.1.7 — the greps compared a runtime-repaired tree against a stale parse-time constant (and assumed a `-zeta` suffix package.json no longer carries). The workflow no longer hardcodes any version: the audit job derives `APP_VERSION` / `APP_VERSION_FULL` / `APP_CODENAME` from `package.json` via `node scripts/release-version.mjs --env` and every other job consumes it through `needs`-outputs. A 1.1.8 → 1.1.9 bump now touches `package.json` + the surfaces the script repairs — nothing else |
| **Chat / Agent mode separation** | A top-level segmented switch — `Chat` / `Agent` — lives in the workspace header. Chat is the calm conversational surface: model rail, message stream, input, send/stop, attachments — no tool/telemetry chrome. Agent is the engineering surface: runtime panel, context controls, activity, live performance strip. Both modes reuse the SAME sessions, model runtime, and engine infrastructure; the mode is a UI concern (persisted per device, defaults to Chat) |
| **Model picker redesign** | When no model is selected — or the user asks for it — a real model panel replaces the stream: one card per local GGUF with measured facts only (architecture, quantisation, parameters, context, RAM estimate, VRAM estimate, vision pairing, chat-template availability, native-engine support, In use / Selected / Recommended / Compatible / Limited classification derived from the host RAM vs the estimated footprint). Actions: Use model / Open models folder / Refresh / Details. There is deliberately NO Remove action — no deletion API exists |
| **Per-model capability facts (backend)** | `/api/models` now reports `multimodal`, `nativeBackend`, `chatTemplate`, `nativeReason` and `estimatedVRAMBytes` per model, all produced by the existing `llm.ResolveModelCapabilities` — never guessed in the UI |
| **Settings: Models and Advanced tabs** | Settings keeps its card system and gains two tabs: **Models** (provider + model runtime) and **Advanced** (system profile / hardware). General, Performance, Generation, Tools, Network, Logs unchanged |
| **Performance strip** | The Agent surface shows a compact live strip (CPU, GPU, RAM, VRAM, prompt tok/s, generation tok/s, TTFT, context, engine, backend, compatibility state) fed by the EXISTING `/api/perf` ring — no second telemetry system, N/A for anything unmeasured |
| **Backend optimisation** | `handleModels` used to call `ResolveModelCapabilities` (a full GGUF header parse) for every model on EVERY poll, uncached. Card and capability resolution now share the same bounded path+size+mtime cache, and the host memory probe is hoisted out of the per-model loop |
| **Minimalist UI pass** | Quieter background grid, denser topbar, navigation reduced to one label per item, the first-use "Choose model" DOM-query hack replaced by the real picker, mode-aware empty states and composer wording (`Send` vs `Forge →`). All animations remain transform/opacity-only |
| **Documentation** | README / agent.md / ARCHITECTURE / UPDATE updated for shipped items only |

## v1.1.7 — Options Clarity, Capability Truth, Live Telemetry, In-App Diagnostics

**Implemented in this release (all items shipped; nothing speculative):**

| Area | What it does |
|---|---|
| **Compatibility-mode fix + retry-up** | The v1.1.6Z defect is fixed at the source: once the launch ladder descended (e.g. "compatibility mode 2 (no speed flags)"), the level was persisted and EVERY later boot resumed there — even after the capability profile had been repaired and re-verified. The boot now records WHY it descended (`engineCompatReason` + timestamp in config), and `shouldRetryFullSpeed` grants ONE bounded full-speed retry when (and only when) the recorded reason is option-class, a verified capability profile postdates the downgrade, and the level-0 profile validates cleanly. A failed non-option retry re-blocks the gate — the same incompatibility is never re-discovered expensively, and no unnecessary restarts are added. The reason and the exact removed options are exposed in the UI and the engine log |
| **Settings redesign (same architecture)** | Six scannable sections — General / Performance / Generation / Tools / Network / Logs — on the existing card system, theme tokens and scrolling. No navigation-layer explosion |
| **Option metadata + tooltips** | Important options show name, current value, recommended value, plain-language description, support/status chips (`Supported`, `Unsupported`, `Recommended`, `Restart required`) and a delayed (400 ms) CSS tooltip limited to 1–2 sentences: what it does + the main trade-off |
| **Capability-aware engine options** | Engine/Speed-Pack controls (flash attention, cache reuse, KV-cache quant, prefill threads, u-batch, GPU layers, threads, mlock) are now visible, patchable, marked with real capability states, and — critically — the restart-after-save path now covers ALL of them, so saved speed options actually reach the engine |
| **Concise tool descriptions** | Every tool exposes a one-line `ShortDescription` for the Options UI ("Shell — Run bounded terminal commands." style). The full operational specs still drive the model (removing them would break orchestration); they remain available via the API `detail` field, outside the Options UI |
| **Live performance (real measurements only)** | `GET /api/perf` reports CPU % (kernel32 `GetSystemTimes` / `/proc/stat` delta), RAM (GlobalMemoryStatusEx / meminfo), GPU % + VRAM via `nvidia-smi` when present, and — from the streaming client's own per-request timer — prompt tok/s, generation tok/s and TTFT, plus context usage vs the verified window, backend, model and the ACTIVE engine profile. Anything unmeasurable renders N/A; nothing is fabricated. No new telemetry framework: one bounded ring (`perftracker.go`) fed by the existing `tokenTimer` |
| **Recommended settings** | Derived from detected hardware (sysinfo probe) + loaded model card + verified engine capability. Applied only through explicit Apply buttons — never silently overwritten |
| **Before/after comparison (P1)** | Changing a performance option snapshots the current measured metrics in memory; the Live Metrics card then shows Before → After with change % for generation tok/s, prompt tok/s, TTFT, CPU and GPU. No benchmark histories, no databases |
| **Network diagnostics** | `GET /api/netcheck` — ONE bounded check (DNS, two HTTPS latency samples for stability, second-endpoint reachability) mapped to `Excellent / Good / Unstable / Slow / Offline` with the first meaningful failure reason. Bounded, no retries, informational only — SHEYTAN stays local-first |
| **In-app Log Viewer** | Settings → Logs: live tail over the EXISTING logging ring (no second logging system), pause, auto-scroll, search, severity + subsystem filters, copy, and clear-view that hides displayed lines without touching the real log. Server-side ring cap (512) + replace-per-poll keep memory bounded; entries are REDACTED before they leave the process (inline secret scrubbing added) |
| **Documentation** | README / ARCHITECTURE / agent.md updated for shipped items only |

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

Windows and Linux x64 portable ZIPs are produced by CI. The Windows package layout (v1.2.x, package root `SHEYTAN-LA`):

```text
SHEYTAN-LA-v1.2.7-windows-x64.zip
└── SHEYTAN-LA/
    ├── SHEYTAN-LA.exe           (GUI app + embedded UI + HTTP/WS API)
    ├── SHEYTAN-LA.bat           (portable launcher)
    ├── AI-CONTEXT.md            (the model's operating manual; generated from agent.md)
    ├── BUILD-INFO.txt           (version, codename, platform, commit)
    ├── README.md / LICENSE / SIGNATURE
    ├── models/                  (empty; drop .gguf files here)
    └── workspace/               (empty; portable Coding Lab workspaces)
```

The Linux package (`SHEYTAN-Local-Agent-Linux-x64-v1.2.7Z.zip`) mirrors this layout under a `SHEYTAN-Local-Agent/` root with a Linux executable. A Windows NSIS installer (`SHEYTAN-LA-v1.2.7-windows-x64-installer.exe`) is produced alongside the portable ZIP.

Unzip anywhere and run the executable. On first launch the app creates its portable data layout next to it:

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
- **Durable request queuing** — a persistent job queue between "request
  accepted" and "run started". THIS IS EXPLICITLY NOT SHIPPED TODAY: the
  current contract is one active run per session (a newer `POST /api/run`
  cancels and replaces the active run; an in-flight request can be
  rejected when inputs are invalid; a request submitted while another
  run of the SAME session is active is deliberately aborted-and-replaced
  by design, and a request lost to a process crash is gone — sessions
  persist, queued intents do not). The planned queue adds a persistent
  `jobId`, durable state (ACCEPTED → QUEUED → WAITING_FOR_RESOURCES →
  RUNNING → COMPLETED / FAILED / CANCELED, incl. retry states), FIFO/fair
  per-session scheduling with bounded concurrency, retry/backoff, crash
  recovery, idempotent execution, engine/resource gating and full
  WebSocket/API replay of queue state. See `ARCHITECTURE.md` (Part II)
  for the design direction.

# Version

`v1.2.7` — see `worklog.md` for the complete implementation/remediation history and `agent.md` for the engineering handoff context.

# License

SHEYTAN™ Local-Agent is proprietary software. See `LICENSE` for the governing terms.
