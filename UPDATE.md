# UPDATE.md — v1.9.6 Release Notes & Maintenance Behavior

**Release:** `v1.9.6` (canonical application version; single version
hierarchy: package.json → release-version.mjs → config.go /
build/config.yml / SIGNATURE)
**Base:** `main @ 070a059` (`v1.9.5`) · **Date:** 2026-10-11
**Package:** `SHEYTAN-Local-Agent-v1.9.6-FINAL.zip` (complete repository tree)

The authoritative per-release history lives in `changelog.md`; this file
carries the CURRENT release notes and the operational maintenance
behavior. The v1.9.0 feature surface (AI Systems, Goals, Approvals,
bounded delegation, `repo_nav`) is unchanged in v1.9.6 and is documented
in the changelog; its maintenance behavior below carries forward.

**v1.9.6 is the FINAL planned v1.9.x maintenance release.** The next
release is v1.10.0 — the first release of the R&D architecture cycle
documented below. No v1.9.7 is planned or implied.

## What v1.9.6 changes

1. **P0 — the Windows "engine maintenance in progress" 503 regression is
   repaired at its root (the admission claim placement).** In v1.9.5 the
   engine-mutation admission claim was taken at TRANSACTION START of the
   variant transaction. On windows/amd64 the background AUTO Vulkan
   bootstrap (an optimization spawned at every app start, after the
   maintenance gate) was fully eligible in ordinary postures — AUTO
   profile default, GPU offload default, a detected GPU, online, a
   selected model — and its transaction held the claim across the whole
   NETWORK resolve/download phase (minutes). Every chat typed in that
   window was refused with HTTP 503
   `engine maintenance in progress — new runs are deferred until the
   engine transaction settles; retry shortly`. This is exactly the
   authoritative run `38095637817` Windows failure
   (`TestLocalGemmaHiChatCompletes`,
   `TestLocalChatSurvivesUnreadableModelCard`), and it was a REAL
   product defect, not a test accident. v1.9.6 moves the claim to the
   DESTRUCTIVE BOUNDARY of the transaction (stop → swap → start →
   verify → rollback/commit): the network phases are claim-free (runs
   are admitted and served by the current engine while the archive
   downloads), the transaction refuses ITSELF at the boundary when a run
   is live (the AUTO probe records a deferral, never a bounded
   failure), and the claim still closes the check-then-act window
   atomically for the seconds the swap actually needs. Non-destructive
   exits (invalid variant, unsupported variant, already-installed no-op,
   early live-run refusal) never touch admission. A pure download
   failure no longer bounces a running engine. The two local-chat E2E
   fixtures pin an explicit CPU profile so the unrelated AUTO GPU
   bootstrap is never a hidden dependency of the pinned chat contract.

2. **P0 — the Linux desktop runtime smoke is re-configured for the
   bare-Xvfb environment it actually runs in.** Run `38095637817` was
   the gate's first real execution (earlier releases were skipped behind
   upstream failures) and failed with `NO visible top-level window` while
   the process stayed alive and wrote every boot marker — the documented
   signature of the WebKitGTK 6.0 DMABUF renderer / GTK4 GL renderer
   stalling under Xvfb (no GPU, no GLX/EGL device, no compositor). The
   smoke step now owns the documented software-renderer environment for
   that class of host (`WEBKIT_DISABLE_DMABUF_RENDERER=1`,
   `WEBKIT_DISABLE_COMPOSITING_MODE=1`, `GSK_RENDERER=cairo`); the gate
   itself is unchanged (a real visible, pid-owned top-level window,
   WM_DELETE_WINDOW delivery, normal-path exit 0) and now dumps the X11
   root window tree and environment keys on failure. Product behavior on
   real desktops is untouched.

3. **P1 — the R&D architecture direction is adopted and the first
   vertical slice ships.** The authoritative decision
   (`Parsaetak/Contents`, branch `Research-and-Development`,
   `SHEYTAN-LA-2026-10-11.md`): Go remains the single
   policy/orchestration authority (run lifecycle, sessions, tools,
   memory, Governor, evidence); C++ owns numerical inference (the
   existing `native/engine` becomes the promotion target under
   capability/parity gates, with managed llama.cpp as the automatic
   honest fallback); Qt (C++/QML) is the primary UI target for the next
   major cycle over a typed, coarse-grained IPC boundary; WinUI 3
   remains an optional future Windows-only shell. v1.9.6 implements the
   R&D Phase 0 slice: `internal/perf` + `cmd/shtn-perf`, the
   reproducible measurement baseline framework (machine-readable JSON +
   concise report, nearest-rank P50/P95, host/model/method/limitation
   metadata, hosted-CI labeling, NOT-MEASURED honesty for every metric
   without a real sample). No target is claimed as met.

4. **P1 — engine/run coordination keeps ONE authority; the same
   guarantees, a tighter window.** The API server remains the only run
   registry; `BeginEngineMutation`/`EndEngineMutation` remain the only
   claim primitives; the claim is mirrored by the same
   `EngineMutationActive` advisory signal the `/api/run` pre-checks
   read. Refused runs still leave no dangling transcript message and no
   live run/session lock (pinned by `internal/api/admission_v195_test.go`).

5. **Carried forward from v1.9.5 (unchanged in v1.9.6):** the
   least-privilege desktop activity transport — one Go API authority,
   one `/ws/activity` endpoint, loopback-only ephemeral listener,
   capability token delivered only through the same-origin discovery
   path, strict origin/host checks, degraded-streaming UI reporting on
   discovery failure; the opt-in sanitized stream trace (lengths only);
   the Windows live-answer acceptance harness as a REQUIRED CI gate
   (ordered live growth with Stop visible, deterministic multi-chunk
   fixture, settlement, one persisted reply, same-session reload, real
   window close); identifier-free ordinary logs.

## Current measured baseline (honest)

Recorded on the v1.9.6 authoring host (hosted Linux CI-class VM — NOT
physical consumer hardware; run `go run ./cmd/shtn-perf` on a real
target host for the physical baseline):

* `cold_start_to_interactive_ms`: measured (api.New + EnsureSetup
  boundary; the GUI shell adds its own window cost — that boundary is
  the Windows live-answer acceptance surface).
* `idle_memory_mb` / `peak_memory_mb`: measured (process RSS/VmHWM,
  server-only posture; excludes the WebView process).
* `engine_ready_ms`, `warm_ttft_ms`, `stream_duration_ms`:
  NOT MEASURED in this session — no serving llama-server binary and no
  real model on the authoring host; the runner labels them honestly
  instead of inventing numbers.
* `stream_chunk_to_visible_ms`: NOT MEASURED — the token→visible-DOM
  boundary requires the real desktop shell (Windows live-answer
  acceptance surface).
* `tool_round_trip_ms`, `file_output_overhead_ms`: NOT MEASURED — no
  agent tool round executed by the perf runner.

R&D §4 targets (<0.8 s cold start, <16 ms token→visible, <80 MB idle,
warm TTFT at or better than managed llama.cpp) are recorded as TARGETS
with measured gaps — never as achievements. Native C++ engine
promotion: NOT flipped in v1.9.6; the existing capability/parity gates
are the v1.10.0 Phase 1 promotion gate (see ROADMAP.md).

## Verification evidence (classes kept separate)

* **Local, v1.9.6 release session (Linux authoring host):** canonical
  version gate green (`node scripts/release-version.mjs --check` at
  1.9.6); Go `build -tags headless ./internal/... ./cmd/... .` clean;
  `go test ./internal/... -tags headless -count=1` all packages ok; the
  two authoritative Windows-regression tests pass with `-count=5`
  (determinism proof); the focused admission/rungate/variant/runtime
  transaction suites pass (including the revised v1.9.6 boundary pins
  and the full archive-seam runtime choreography); `go vet` clean on
  touched packages; `internal/perf` unit suite green; frontend
  typecheck/lint/unit/release results and the C++ native build/ctest
  results recorded in `worklog.md`.
* **UNVERIFIED in the v1.9.6 session (hosted CI executes them; no local
  evidence exists and none is claimed):** the Windows live-answer
  acceptance (no Windows host here), the Linux native window smoke
  (no GTK4/WebKitGTK headers and no root here), the packaged
  browser-E2E matrix, and the physical-hardware performance baseline.
* **CI:** the authoritative verdict for v1.9.6 is the Actions run that
  carries this revision. v1.9.5's run (38095637817) FAILED both
  platform jobs and is not a green baseline; no green-run claim is made
  for v1.9.6 until that run exists. Local green is not CI green; Linux
  green is not Windows green; hosted-CI numbers are not physical
  benchmarks.
