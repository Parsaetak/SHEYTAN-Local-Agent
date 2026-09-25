// Package agent is the agent runtime: orchestrator (plan → execute → critic)
// with streaming activity captions, tool registry, and abort support.
//
// v1.0.2 adds: thinking mode (native reasoning_content + <think> tag
// extraction), tool allow-listing, history windowing, and persistent recall
// injection.
package agent

import (
        "context"
        "encoding/json"
        "errors"
        "fmt"
        "sort"
        "strconv"
        "strings"
        "sync"
        "time"
        "unicode/utf8"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/aicontext"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/chunking"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/contextplan"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/continuum"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/ctxtelemetry"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/netcheck"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/skills"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/taskclassify"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/toolsets"
)

// Tool is the interface every agent tool implements.
type Tool interface {
        Name() string
        Description() string
        Parameters() any // JSON Schema (struct tag based)
        Run(ctx context.Context, args json.RawMessage) (string, error)
}

// Activity is one "what the agent is doing now" event sent to the UI.
type Activity struct {
        Type      string    `json:"type"` // "thinking" | "reasoning" | "tool_start" | "tool_end" | "plan" | "response" | "error" | "done"
        Caption   string    `json:"caption"`
        Timestamp time.Time `json:"timestamp"`
        Detail    any       `json:"detail,omitempty"`

        // RunID (v1.2.6) stamps every activity of one run with the run
        // identifier created by the API layer, so a client that attaches
        // mid-run — or replays after the fact — can attribute every frame to
        // exactly one run. Empty for orchestrator-internal callers that have
        // no run identity (tests, CLI).
        RunID string `json:"runId,omitempty"`

        // Seq (v1.2.6 continuation) is the monotonic per-run sequence
        // number the API layer stamps on every PUBLISHED event, assigned
        // BEFORE the event reaches any subscriber. The replay contract:
        // a run snapshot at sequence N plus every event with seq > N is
        // the complete, duplicate-free state. Zero on orchestrator-
        // internal events that never pass through the API publisher.
        Seq int64 `json:"seq,omitempty"`
}

// RunResult carries everything a completed turn produced (v1.0.2). Run()
// returns only Text for backward compatibility; surfaces that want the
// reasoning trace or tool usage call RunDetailed().
type RunResult struct {
        Text      string   // final answer (think tags stripped)
        Reasoning string   // thinking trace: native reasoning_content + <think> blocks
        ToolsUsed []string // distinct tools executed, in first-use order
        Elided    int      // older messages compacted out of the prompt
        Recalled  int      // past-exchange digests injected from recall

        // Perf is the last streaming call's speed HUD line (v1.0.4), e.g.
        // "41.2 tok/s · first token 0.8s". Empty when telemetry is off.
        Perf string

        // ContextUsage (v1.0.7): the PEAK prompt pressure observed during the
        // turn (largest message list actually sent to the engine), measured
        // against the history token budget. Drives the context meter and the
        // Continuum chapter-rollover decision in the UI.
        ContextUsage continuum.Usage

        // Verification (v1.1.5 Phase 6): the run-level verification verdict
        // — verified / partially_verified / failed / not_verified — computed
        // from the objective evidence the run's tool traffic actually
        // produced (lab verify actions, build/test outcomes). "The model
        // said so" is not proof; this field is the honest record.
        Verification VerificationReport

        // LoopStats (v1.1.5 Phase 6): per-tool call counts (total and
        // distinct arguments) for the run report — makes repetitive
        // behaviour visible after the fact.
        LoopStats []StatsSnapshot

        // FailureTally (v1.1.5 Phase 6): how many tool failures were
        // classified, by category — the reliability HUD for the timeline.
        FailureTally map[FailureCategory]int

        // Tier (v1.2.5): the context tier the turn STARTED at
        // (FAST/STANDARD/DEEP/MAX).
        Tier string `json:"tier,omitempty"`

        // FinalTier (v1.2.5): the tier the turn ENDED at (== Tier when no
        // escalation fired).
        FinalTier string `json:"finalTier,omitempty"`

        // Escalations (v1.2.5): the recorded tier moves with their evidence
        // reasons and token deltas.
        Escalations []taskclassify.Escalation `json:"escalations,omitempty"`

        // Timing (v1.2.5): the measured per-request timeline
        // (classify/context/prompt/serialization/TTFT/generation/tool/
        // verification/total).
        Timing Timing `json:"timing,omitempty"`

        // Task (v1.2.8): the bounded structured task state maintained from the
        // run's REAL tool traffic — goal, files inspected/changed, commands,
        // tests, failures/repairs, verification, next step. Published live via
        // `task` activities, folded into the run snapshot (reconnect recovery)
        // and consumed at settlement for the session summary + agent.md
        // handoff. Nil on runs that never touched tools.
        Task *TaskState `json:"task,omitempty"`
}

// abortCaption renders the correct end caption for a canceled context:
// a user abort and the v1.1.4 per-run time budget are different events
// and must not be reported identically.
func abortCaption(err error) string {
        if errors.Is(err, context.DeadlineExceeded) {
                return "Run stopped: exceeded the configured time budget"
        }

        return "Aborted by user"
}

// Recaller is the subset of the recall engine the orchestrator needs
// (keeps the package decoupled for tests).
type Recaller interface {
        RelevantBlock(query string, k, maxTokens int) string
}

// SetProjectCard installs the persistent project intelligence provider
// (v1.1.5 Phase 6). When set and non-empty, its block is injected as a
// system message immediately before the last user message — the same
// cache-friendly position as the recall block — so the model starts the
// task already knowing the measured facts of the current project
// (languages, verified build/test commands, past lessons). Nil disables.
func (o *Orchestrator) SetProjectCard(provider func() string) {
        o.mu.Lock()
        o.projectCard = provider
        o.mu.Unlock()
}

// SetRepoEvidence installs the repository-intelligence evidence
// provider (v1.3.4, ROADMAP v1.4 slice 1). When set and non-empty, its
// block is injected as a system message immediately after the project
// card — the same cache-friendly position — so the model starts the
// task with targeted, evidence-ranked file suggestions from the
// persistent repository index. Nil disables.
func (o *Orchestrator) SetRepoEvidence(provider func(task string) string) {
        o.mu.Lock()
        o.repoEvidence = provider
        o.mu.Unlock()
}

// responseEmitInterval is the LEGACY streaming coalesce cadence (~12
// updates/s). v1.0.9 (TURBINE) derives the live cadence from the config:
// cfg.EffectiveStreamEmitInterval() targets ONE emit per display frame
// (default 120 FPS → ~8ms), and the UI-side frame pacer coalesces those
// into at most one widget batch per frame — the stream now renders at the
// monitor's cadence instead of flooding the widget tree. When SmoothStream
// is disabled the legacy constant applies.
const responseEmitInterval = 80 * time.Millisecond

// recallBlockTokenBudget bounds the auto-recalled past-context block.
const recallBlockTokenBudget = 600

// thinkingNudge is appended to the AI-context system message when thinking
// mode is on — it makes ANY model (with or without native reasoning)
// externalize its reasoning inside <think> tags the orchestrator can split.
const thinkingNudge = `

---

## THINKING MODE (enabled by the user)

Before answering, reason step by step inside <think></think> tags: restate
the goal, plan the approach, check assumptions, and verify tool results.
Keep the thinking focused (a few short paragraphs at most). After the
closing </think> tag, write the final answer for the user — never reference
the thinking block in it. If the task is trivial, a one-line thought is
enough.`

// thinkingNudgeSentinel detects an already-present nudge.
const thinkingNudgeSentinel = "## THINKING MODE (enabled by the user)"

// GenerationStream is the pluggable streaming-generation seam: the same
// signature the llm client exposes, so the runtime can route through the
// backend selection (native when selected + capable, llama.cpp
// otherwise) WITHOUT the loop knowing which engine serves the request.
// A nil router falls back to the direct client path.
type GenerationStream func(ctx context.Context, req *llm.ChatRequest,
        onEvent func(llm.StreamEvent) error) (llm.PerfStats, error)

// Orchestrator runs the plan-execute-critic loop with streaming activity.
type Orchestrator struct {
        src     *config.Source
        client  *llm.Client
        toolsMu sync.RWMutex
        tools   map[string]Tool

        // specs (v1.2.5) memoizes serialized tool schemas per registry
        // generation — the per-turn double JSON marshal of every tool spec is
        // gone. Invalidated on Register.
        specs *specCache

        // genMu guards the router swap (set once at wiring; also readable
        // under race detector in tests).
        genMu sync.RWMutex
        gen   GenerationStream

        mu           sync.Mutex
        sessionID    string
        recaller     Recaller
        projectCard  func() string
        repoEvidence func(task string) string

        // Phase 7 additions -------------------------------------------------

        // ctxLimits optionally reports an ENGINE-side context limit (the
        // native backend reports the loaded model's limit). The model-aware
        // effective context is min(configured, GGUF model limit, engine
        // limit).
        ctxLimits func(cfg *config.Config) int

        // skillSource is the Phase 7B skills store: load-on-demand skill
        // injection driven by task-trigger matching.
        skillSource SkillSource

        // telemetry records per-turn context-effectiveness measurements.
        telemetry *ctxtelemetry.Store
}

// SkillSource is the skills subset the orchestrator consumes.
type SkillSource interface {
        MatchTask(task string, limit int) []skills.Skill
        RecordUse(id string)
}

// SetContextLimitProvider installs the engine-side context limit provider
// (native engine bridge). Nil disables the clamp.
func (o *Orchestrator) SetContextLimitProvider(fn func(cfg *config.Config) int) {
        o.mu.Lock()
        o.ctxLimits = fn
        o.mu.Unlock()
}

// SetSkillSource installs the skills store (load-on-demand injection).
func (o *Orchestrator) SetSkillSource(s SkillSource) {
        o.mu.Lock()
        o.skillSource = s
        o.mu.Unlock()
}

// SetTelemetry installs the context-effectiveness telemetry store.
func (o *Orchestrator) SetTelemetry(t *ctxtelemetry.Store) {
        o.mu.Lock()
        o.telemetry = t
        o.mu.Unlock()
}

func New(src *config.Source, client *llm.Client) *Orchestrator {
        return &Orchestrator{
                src:    src,
                client: client,
                tools:  make(map[string]Tool),
                specs:  newSpecCache(),
        }
}

// SetGenerationStream installs the backend-aware generation router (the
// runtime seam; Phase 5). Nil restores the direct client path.
func (o *Orchestrator) SetGenerationStream(router GenerationStream) {
        o.genMu.Lock()
        o.gen = router
        o.genMu.Unlock()
}

// generationRouter returns the installed router (nil = client path).
func (o *Orchestrator) generationRouter() GenerationStream {
        o.genMu.RLock()
        defer o.genMu.RUnlock()
        return o.gen
}

// streamChat routes ONE generation request through the installed
// backend-aware router, or the direct client path when no router is set
// (unit tests, the CLI multi-agent path).
func (o *Orchestrator) streamChat(ctx context.Context, req *llm.ChatRequest,
        onEvent func(llm.StreamEvent) error) (llm.PerfStats, error) {
        if router := o.generationRouter(); router != nil {
                return router(ctx, req, onEvent)
        }
        return o.client.StreamChatDetailed(ctx, req, onEvent)
}

// Register adds a tool to the registry.
func (o *Orchestrator) Register(t Tool) {
        if t == nil {
                return
        }

        o.toolsMu.Lock()
        o.tools[t.Name()] = t
        o.toolsMu.Unlock()

        // v1.2.5: a new/changed tool invalidates the memoized spec cache.
        if o.specs != nil {
                o.specs.Invalidate()
        }
}

// Unregister removes a tool from the registry (v1.6.0: custom tools are
// first-class registry citizens and may be disabled/deleted at runtime).
func (o *Orchestrator) Unregister(name string) {
        o.toolsMu.Lock()
        delete(o.tools, name)
        o.toolsMu.Unlock()

        if o.specs != nil {
                o.specs.Invalidate()
        }
}

// Tools returns the tool registry (for schema export to the UI). The map
// is read-only by contract.
func (o *Orchestrator) Tools() map[string]Tool {
        o.toolsMu.RLock()
        defer o.toolsMu.RUnlock()
        return o.tools
}

// tool looks one tool up under the registry read lock.
func (o *Orchestrator) tool(name string) (Tool, bool) {
        o.toolsMu.RLock()
        defer o.toolsMu.RUnlock()
        t, ok := o.tools[name]
        return t, ok
}

// SetRecaller wires the persistent recall engine (optional; nil disables
// injection even when cfg.RecallEnabled is true).
func (o *Orchestrator) SetRecaller(r Recaller) {
        o.mu.Lock()
        o.recaller = r
        o.mu.Unlock()
}

// SetSessionID tags subsequent tool-call log records with the session.
func (o *Orchestrator) SetSessionID(id string) {
        o.mu.Lock()
        o.sessionID = id
        o.mu.Unlock()
}

func (o *Orchestrator) currentSessionID() string {
        o.mu.Lock()
        defer o.mu.Unlock()
        return o.sessionID
}

// Run executes one agent turn and returns just the final text (compat
// wrapper around RunDetailed).
func (o *Orchestrator) Run(
        ctx context.Context,
        messages []llm.Message,
        onActivity func(Activity),
) (string, error) {
        res, err := o.RunDetailed(
                ctx,
                messages,
                onActivity,
        )

        return res.Text, err
}

// RunOption customizes one RunDetailed call without touching global state.
type RunOption func(*runOptions)

// runOptions is the resolved per-run option set.
type runOptions struct {
        sessionContext int

        // thinking (v1.2.5) is the composer's thinking control:
        // "auto" | "fast" | "thinking". It changes the ACTUAL backend
        // behaviour — tier selection, the thinking nudge, verification
        // posture — never just the UI.
        thinking string

        // toolPolicy (v1.2.5) is the per-request manual tool control.
        toolPolicy ToolPolicy

        // netSearch (v1.3.6, spec §23/§24) is the composer's explicit
        // per-request Net Search intent. When true, the EXISTING research
        // tool is authorized for THIS request server-side — never inferred
        // from message text, never silently extended to unrelated tools.
        netSearch bool

        // receivedAt (v1.2.5) carries the API layer's request-received
        // timestamp so the run clock's TTFT/total cover the full path.
        receivedAt time.Time

        // summaryBlock/historyBlocks (v1.2.8): the session summary and the
        // retrieved cross-mode history blocks for this turn (see the
        // With* builders above).
        summaryBlock  string
        historyBlocks []llm.Message
}

// WithSessionContext applies the per-session context policy (1.1.6) to
// this run only: the plan, the request's n_ctx and the fit gates all use
// the resolved effective window. The policy lives with the session — it
// never mutates the global config and never affects other sessions.
func WithSessionContext(tokens int) RunOption {
        return func(ro *runOptions) {
                if tokens > 0 {
                        ro.sessionContext = tokens
                }
        }
}

// WithThinkingMode sets the per-request thinking control (v1.2.5):
// "fast" (latency first, smaller initial context, no thinking nudge),
// "thinking" (deeper reasoning, context escalation permitted, stronger
// verification) or "auto" (adaptive decision). Invalid values resolve to
// auto. This setting affects the real backend request.
func WithThinkingMode(mode string) RunOption {
        return func(ro *runOptions) {
                ro.thinking = NormalizeThinkingControl(mode)
        }
}

// WithToolPolicy sets the per-request manual tool control (v1.2.5). In
// MANUAL mode only the allowed tools are offered and executable; the
// restriction overrides automatic selection and is never widened by
// context escalation.
func WithToolPolicy(mode string, allowed []string) RunOption {
        return func(ro *runOptions) {
                ro.toolPolicy = ToolPolicy{
                        Mode:    NormalizeToolPolicyMode(mode),
                        Allowed: allowed,
                }
        }
}

// WithNetSearch records the per-request Net Search intent (v1.3.6,
// spec §24). The backend authorizes the EXISTING research tool for THIS
// request: the tool is guaranteed in the offered surface and executable
// under any tool-policy mode, while every other tool's availability is
// untouched. The setting is recorded in run telemetry.
func WithNetSearch(enabled bool) RunOption {
        return func(ro *runOptions) {
                ro.netSearch = enabled
                ro.toolPolicy.NetSearch = enabled
        }
}

// WithReceivedAt anchors the run clock to the moment the API layer
// received the request (v1.2.5 telemetry).
func WithReceivedAt(t time.Time) RunOption {
        return func(ro *runOptions) {
                if !t.IsZero() {
                        ro.receivedAt = t
                }
        }
}

// WithSessionSummaryBlock (v1.2.8) installs the current session's rolling
// summary block (pre-rendered, pre-bounded by the sessions package). It is
// planned as the REQUIRED "summary" section and injected into the stable
// system prefix — elided history stays survivable. Empty string = no-op.
func WithSessionSummaryBlock(block string) RunOption {
        return func(ro *runOptions) {
                ro.summaryBlock = strings.TrimSpace(block)
        }
}

// WithHistoryBlocks (v1.2.8) installs the provenance-tagged cross-mode
// history reference blocks retrieved for THIS turn (pre-rendered, pre-
// bounded by the histref package). They are planned as the OPTIONAL
// "history-refs" section and injected before the fresh user turn. The
// blocks are DATA with explicit provenance — never instructions.
func WithHistoryBlocks(blocks []llm.Message) RunOption {
        return func(ro *runOptions) {
                for _, b := range blocks {
                        if strings.TrimSpace(b.Content) != "" {
                                ro.historyBlocks = append(ro.historyBlocks, b)
                        }
                }
        }
}

// RunDetailed executes one agent turn: prompt → tool calls → tool results →
// final answer, exposing the full result envelope. All activity is streamed
// via `onActivity`.
//
// Cancellation is controlled entirely by the caller-provided context. The API
// layer creates one context per active session, so canceling one session does
// not affect any other session.
func (o *Orchestrator) RunDetailed(
        ctx context.Context,
        messages []llm.Message,
        onActivity func(Activity),
        opts ...RunOption,
) (result RunResult, err error) {
        ro := runOptions{}
        for _, opt := range opts {
                if opt != nil {
                        opt(&ro)
                }
        }

        // v1.2.5: the per-request measured timeline.
        clock := NewRunClock()
        if !ro.receivedAt.IsZero() {
                clock.SetReceived(ro.receivedAt)
        }

        o.mu.Lock()
        recaller := o.recaller
        cardProvider := o.projectCard
        repoProvider := o.repoEvidence
        o.mu.Unlock()

        // v1.1.4: one consistent config snapshot per run. The previous code
        // read the shared mutable Config throughout the loop — a Settings PATCH
        // mid-run produced a data race and could flip sampling/tool policy
        // between two iterations of the SAME turn. Changes now apply cleanly
        // from the next run on.
        cfg := o.src.Load()

        // v1.1.5 Phase 6 reliability core: one loop guard (repeat detection,
        // tool-call/wall-clock budgets) and one evidence collector (objective
        // verification record) per run. Both are local to the run — nothing
        // accumulates across runs.
        guard := NewLoopGuard()
        evidence := NewEvidenceCollector()
        failTally := map[FailureCategory]int{}

        // v1.2.5 escalation evidence watcher — REAL observations only.
        watch := &escalationWatch{}

        // =====================================================================
        // v1.2.5 ADAPTIVE TURN PIPELINE.
        //
        //   request -> CLASSIFY -> tier (FAST/STANDARD/DEEP/MAX) ->
        //   tier-scoped composition (P0 + the P1 the task actually signals)
        //   -> plan -> engine -> first response -> evidence -> escalate ONLY
        //   when required -> continue.
        //
        // The Phase 7 preflight fit gates and the degradation ladder remain
        // the safety net BELOW the tier decision; what changes is the START:
        // small requests now start small instead of paying the full ~10K
        // token prefill for a one-line reply.
        // =====================================================================

        task := lastUserQuery(messages)

        // v1.3.6 (spec §24): NET SEARCH REQUEST CONTRACT. The composer's
        // explicit Net Search intent is carried on the tool policy itself
        // (ToolPolicy.NetSearch — see WithNetSearch): the research tool is
        // guaranteed offered and executable for THIS request, server-side,
        // never inferred from message text, never extended to other tools.
        if ro.netSearch {
                logging.Default().Info("agent",
                        "net search enabled for this request — the research tool is authorized server-side")
        }

        // v1.2.8: one bounded task state per run, maintained from REAL tool
        // traffic (never model claims). Published as a `task` activity per
        // tool round, folded into the run snapshot for reconnect recovery, and
        // consumed at settlement (session summary + agent.md handoff).
        runTask := NewTaskState(task)
        runTask.Constraints = extractConstraints(task)

        hasImages, _, attachmentBlockCount := requestFacts(messages)
        _, stagedTokensProbe := countStagedBlocks(messages)

        // --- CLASSIFY -------------------------------------------------------
        profile := taskclassify.Classify(task, taskclassify.ClassifyOptions{
                HasImages:       hasImages,
                AttachmentCount: attachmentBlockCount,
                HistoryMessages: len(messages),
                UserDepth:       ro.thinking,
        })

        clock.Mark(StageClassified)

        tier := "" // closure variable — status events read the LIVE tier

        emitStatus := func(phase string) {
                detail := map[string]any{
                        "phase":     phase,
                        "tier":      tier,
                        "elapsedMs": clock.SinceReceived(),
                }

                onActivity(Activity{
                        Type:      "status",
                        Caption:   phase,
                        Timestamp: time.Now(),
                        Detail:    detail,
                })
        }

        logging.Default().Info(
                "agent",
                "task classified: kind=%s complexity=%d reason=%s",
                profile.Kind, profile.Complexity, profile.Reason,
        )

        emitStatus("Classifying request…")

        // Model-aware effective context: min(session policy, configured,
        // GGUF model limit, engine limit) — resolved BEFORE tier selection
        // because the tier decision needs the real window.
        effCtx := o.resolveEffectiveContext(cfg, ro.sessionContext)
        safety := contextSafetyMargin(effCtx.Effective)

        // --- SELECT TIER (measured resources, no hard-coded sizes) ---------
        ramTotalMB, ramAvailMB := RAMInfo()
        historyTokens := chunking.EstimateMessagesTokens(messages)

        enabledNames := make([]string, 0, len(o.tools))
        for name := range o.Tools() {
                if cfg.ToolEnabled(name) {
                        enabledNames = append(enabledNames, name)
                }
        }
        sort.Strings(enabledNames)

        // Measured tool requirement: the task-relevant selection's cached
        // spec cost (single source of truth with the composer below).
        toolRequirementTokens := 0
        if relevant := toolsets.SelectForTask(enabledNames, task, 0); len(relevant) > 0 {
                relTools := make([]Tool, 0, len(relevant))
                for _, n := range relevant {
                        if t, ok := o.tool(n); ok {
                                relTools = append(relTools, t)
                        }
                }
                _, toolRequirementTokens = o.specs.BuildSpecs(relTools)
        }

        res := taskclassify.Resources{
                EffectiveContext:    effCtx.Effective,
                OutputReserve:       contextplan.OutputReserveFor(effCtx.Effective, cfg.LLM.MaxTokens),
                SystemRAMMB:         ramTotalMB,
                AvailableRAMMB:      ramAvailMB,
                VisionPayloadTokens: visionPayloadTokens(messages),
                AttachmentTokens:    stagedTokensProbe,
                ToolRequirement:     toolRequirementTokens,
                HistoryTokens:       historyTokens,
                UserDepth:           ro.thinking,
        }

        tier = taskclassify.SelectTier(profile, res)
        result.Tier = tier

        emitStatus("Preparing context · " + tier)

        logging.Default().Info(
                "agent",
                "tier selected: %s (%s)",
                tier, taskclassify.DescribeResources(res),
        )

        // --- TIER-SCOPED COMPOSITION ----------------------------------------
        clock.Mark(StageContextStart)

        composer := o.newTurnComposer(
                cfg, effCtx, safety, ro.toolPolicy, ro.thinking,
                tier, task, recaller, cardProvider, repoProvider,
        )

        // v1.0.1: every conversation still opens with the SHEYTAN briefing
        // as system message #1 — but v1.2.5 composes the briefing the TIER
        // chose: FAST uses the compact ~350-token form, deeper tiers the
        // full instructions. Skipped when the caller already included it.
        wePrependedBriefing := false
        if !hasAIContext(messages) {
                ctxContent, compact := composer.Briefing()
                composer.usedCompact = compact

                // When offline, fold the environment note into the briefing so the
                // LLM knows which tools cannot work and wastes no iterations on
                // web calls.
                if note := netcheck.Note(); note != "" {
                        ctxContent += "\n\n" + note

                        onActivity(Activity{
                                Type:      "thinking",
                                Caption:   "Offline mode — web tools disabled, local tools fully available",
                                Timestamp: time.Now(),
                        })
                }

                messages = append(
                        []llm.Message{
                                {
                                        Role:    "system",
                                        Content: ctxContent,
                                },
                        },
                        messages...,
                )

                wePrependedBriefing = true
        }

        // v1.0.2 thinking mode — v1.2.5: the per-request control resolves
        // against the global toggle ("fast" disables the nudge for THIS
        // request; "thinking" enables it even when the global toggle is off).
        if composer.ThinkingEnabled() {
                messages = ensureThinkingNudge(messages)
        }

        clock.Mark(StageContextEnd)

        // =====================================================================
        // Phase 7 PREFLIGHT BUDGET PIPELINE — single authoritative path.
        //
        // capability → effective window → output reserve → safety margin →
        // tool/schema budget → system/instructions → project intelligence →
        // memory/recall → attachments → history → compression/rollover →
        // FINAL request fits the budget AS MEASURED.
        //
        // v1.2.9 honesty note: "fits" means fits under the ACTIVE token
        // estimator tier (plan.Estimator: exact engine tokenizer when the
        // native path serves it, family-tuned or conservative heuristic
        // otherwise) plus the safety margin. On estimator tiers this is
        // conservative budgeting, NOT a mathematically exact context
        // guarantee — the plan label says which.
        //
        // Everything optional (recall, project card, skills, staged
        // attachments) is COMPOSED by the TIER first and INJECTED only when
        // the plan keeps it; when fixed sections still overflow, the
        // degradation ladder reduces lower-priority components automatically
        // instead of merely warning. The engine is never called with a
        // request that intentionally exceeds the measured budget.
        // =====================================================================

        // Measure the assembled messages: the briefing prefix (system) and
        // whatever optional blocks the caller already embedded.
        sysTokens, _, _ := classifyMessages(messages)

        // Detach already-injected optional blocks (staged attachments
        // inserted by the API layer, recall from retries) so the planner
        // — not message order — decides what travels this turn.
        keptMessages, stagedBlocksMsgs := splitInjectedSystemBlocks(messages)
        stagedTokens := chunking.EstimateMessagesTokens(stagedBlocksMsgs)
        messages = keptMessages

        // 1.1.6: set by the refusal gates below so telemetry can prove
        // overflow never reached the engine on this turn; counts the tokens
        // freed by in-loop tool-result compaction.
        overflowPrevented := false
        tokensCompressedNow := 0

        clock.Mark(StagePromptStart)

        composePlan := func(sysTok, toolTok, optionalTok, summaryTok, histRefTok int) contextplan.Plan {
                p := contextplan.Assemble(contextplan.Input{
                        SystemTokens:       sysTok,
                        ToolTokens:         toolTok,
                        RecallTokens:       optionalTok,
                        SummaryTokens:      summaryTok,
                        HistoryRefTokens:   histRefTok,
                        NumCtx:             effCtx.Effective,
                        MaxOutputTokens:    cfg.LLM.MaxTokens,
                        SafetyMarginTokens: safety,
                        MaxHistoryShare:    composer.HistoryShare(),
                })

                p.Tier = tier
                p.Recalled = result.Recalled
                p.ModelSummary = strings.Join(effCtx.Reasons, "; ")
                return p
        }

        // v1.2.8 measured costs of the summary + history-ref sections. The
        // blocks arrive pre-bounded from the API layer; under pressure the
        // planner (not the caller) decides what travels — history-refs are
        // dropped first among the retrieval sections, the summary is required.
        summaryBlock := ro.summaryBlock
        summaryTokens := 0
        if summaryBlock != "" {
                summaryTokens = chunking.EstimateTokens(summaryBlock)
        }
        histRefMsgs := ro.historyBlocks
        histRefTokens := 0
        for _, blk := range histRefMsgs {
                histRefTokens += chunking.EstimateTokens(blk.Content)
        }

        optionalTokens := composer.OptionalTokens(stagedTokens)

        plan := composePlan(sysTokens, composer.toolTok, optionalTokens, summaryTokens, histRefTokens)

        // Degradation ladder — compress/elide lower-priority components
        // BEFORE dropping what the task cannot live without. Each step is
        // recorded on the plan (the honest trail). v1.2.5: the tier already
        // front-loads this work (FAST starts compact + task-relevant tools),
        // so these steps now fire only under genuine pressure.
        if plan.Overflow() > 0 {
                // Step 1: dynamic toolsets — the schemas of task-irrelevant
                // tools are pure overhead under pressure.
                if selected := toolsets.SelectForTask(composer.allNames, task, 0); len(selected) > 0 && len(selected) < len(composer.allNames) {
                        before := len(composer.allNames)
                        composer.setTools(selected, task)
                        plan = composePlan(sysTokens, composer.toolTok, optionalTokens, summaryTokens, histRefTokens)
                        plan.AddAdjustment(fmt.Sprintf(
                                "dynamic toolset: reduced tool surface %d → %d for this task",
                                before, len(composer.allNames)))
                }
        }

        if plan.Overflow() > 0 {
                // Step 2: compact the system briefing (long-form guidance is
                // the largest fixed block; the compact form keeps the
                // binding rules).
                if wePrependedBriefing && !composer.usedCompact {
                        compactMsg := llm.Message{
                                Role:    "system",
                                Content: aicontext.CompactSystemMessage(cfg, composer.allNames),
                        }

                        messages = append(
                                []llm.Message{compactMsg},
                                messages[1:]...,
                        )

                        if composer.ThinkingEnabled() {
                                messages = ensureThinkingNudge(messages)
                        }

                        composer.usedCompact = true
                        sysTokens, _, _ = classifyMessages(messages)
                        plan = composePlan(sysTokens, composer.toolTok, optionalTokens, summaryTokens, histRefTokens)
                        plan.AddAdjustment(
                                "compact system briefing (context-pressure mode)")
                }
        }

        if plan.Overflow() > 0 {
                // Step 3: drop ALL optional blocks — recall, project card,
                // skills, staged attachment chunks. History and the current
                // task keep the budget.
                if optionalTokens > 0 {
                        optionalTokens = 0
                        composer.dropOptional()
                        stagedBlocksMsgs = nil
                        stagedTokens = 0
                        plan = composePlan(sysTokens, composer.toolTok, 0, summaryTokens, histRefTokens)
                        plan.AddAdjustment(
                                "dropped recall/project-intelligence/skills/attachment blocks under context pressure")
                        // Staged attachment content the tier could not carry
                        // is escalation evidence (LargeAttachment).
                        watch.observeAttachmentTruncation()
                }
        }

        // Guaranteed-fit gate: if even the minimal fixed set cannot fit,
        // refuse BEFORE any engine call — an intentionally overflowing
        // request is never sent.
        if plan.Overflow() > 0 {
                overflow := plan.Overflow()
                overflowPrevented = true

                // v1.2.8.1: report the MEASURED section breakdown — the
                // refusal names what actually does not fit, not a guess.
                breakdown := measuredSectionBreakdown(plan)

                logging.Default().Warn(
                        "agent",
                        "context budget impossible: fixed sections exceed usable window by ~%d tok (effective ctx %d; %s) — no engine call",
                        overflow, effCtx.Effective, breakdown,
                )

                onActivity(Activity{
                        Type: "error",
                        Caption: fmt.Sprintf(
                                "Context budget impossible: even after automatic reduction the required sections (%s) need ~%d tokens more than fit (effective context %d). Raise the context size in Settings → Model or use a model with a larger window.",
                                breakdown, overflow, effCtx.Effective,
                        ),
                        Timestamp: time.Now(),
                })

                result.ContextUsage = continuum.EstimateUsage(messages, plan.HistoryBudget)
                return result, fmt.Errorf(
                        "context budget impossible: prompt cannot fit effective context %d (overflow ~%d tokens)",
                        effCtx.Effective, overflow)
        }

        // The plan kept the optional blocks: inject them in priority order
        // (project card → skills → recall), all in the cache-friendly
        // position right before the fresh turn. v1.2.8.1: the injected
        // blocks are tracked as MESSAGES per plan section so the post-
        // windowing measurement can reconcile what the history windower
        // actually kept (it may elide injected blocks when the current
        // turn alone fills the budget) — section tokens then reflect the
        // real prompt instead of the injection intent.
        preWindowRecallBlocks := []llm.Message{} // card, skills, recall, staged attachment chunks
        preWindowRefBlocks := []llm.Message{}    // cross-mode history-reference blocks

        card, repoBlk, skillBlk, recallBlk, cardOn, repoOn, skillsOn, recallOn := composer.Injectables()

        if cardOn {
                cardMsg := llm.Message{Role: "system", Content: card}
                messages = insertBeforeLastUser(messages, cardMsg)
                preWindowRecallBlocks = append(preWindowRecallBlocks, cardMsg)
        }

        if repoOn {
                repoMsg := llm.Message{Role: "system", Content: repoBlk}
                messages = insertBeforeLastUser(messages, repoMsg)
                preWindowRecallBlocks = append(preWindowRecallBlocks, repoMsg)
        }

        if skillsOn {
                skillMsg := llm.Message{Role: "system", Content: skillBlk}
                messages = insertBeforeLastUser(messages, skillMsg)
                preWindowRecallBlocks = append(preWindowRecallBlocks, skillMsg)
        }

        if recallOn {
                recallMsg := llm.Message{Role: "system", Content: recallBlk}
                messages = insertBeforeLastUser(messages, recallMsg)
                preWindowRecallBlocks = append(preWindowRecallBlocks, recallMsg)

                result.Recalled = strings.Count(recallBlk, "user asked:")
                plan.Recalled = result.Recalled

                onActivity(Activity{
                        Type: "thinking",
                        Caption: fmt.Sprintf(
                                "Recalled %d relevant past exchange(s) from memory",
                                result.Recalled,
                        ),
                        Timestamp: time.Now(),
                })
        }

        if len(stagedBlocksMsgs) > 0 {
                for _, blk := range stagedBlocksMsgs {
                        messages = insertBeforeLastUser(messages, blk)
                        preWindowRecallBlocks = append(preWindowRecallBlocks, blk)
                }
        }

        plan.Attachments = len(stagedBlocksMsgs)

        // v1.2.9 SECURITY (summary authority boundary): the rolling session
        // summary is DERIVED conversation memory — extracted from past
        // user/assistant text, which may itself carry injected content. It
        // must NOT occupy the system (instruction-authority) channel:
        // unverified model/user-derived text injected as `system` would
        // turn one bad turn into persistent authoritative context. The
        // block therefore rides the DATA channel — a user-role message
        // inserted before the fresh turn, exactly like the other retrieval
        // blocks, with its own header declaring it unverified derived
        // memory (RenderSummaryBlock). Only genuinely authoritative
        // information (the application briefing, verified capability
        // facts, task state) stays in the system prefix.
        //
        // Both summary and history-refs were BUDGETED above — injection
        // only happens for what the plan actually kept (history-refs are
        // the first retrieval section dropped under pressure, by
        // priority).
        var preWindowSummaryBlocks []llm.Message

        if summaryBlock != "" && planSectionIncluded(plan, contextplan.SectionSummary) {
                sumMsg := llm.Message{Role: "user", Content: summaryBlock}
                messages = insertBeforeLastUser(messages, sumMsg)
                preWindowSummaryBlocks = append(preWindowSummaryBlocks, sumMsg)
        }

        if len(histRefMsgs) > 0 && planSectionIncluded(plan, contextplan.SectionHistoryRefs) {
                for _, blk := range histRefMsgs {
                        messages = insertBeforeLastUser(messages, blk)
                        preWindowRefBlocks = append(preWindowRefBlocks, blk)
                }
                plan.SetSectionTokens(contextplan.SectionHistoryRefs, histRefTokens)
        }

        // History window: compact older messages so the prompt stays inside
        // the plan allocation (leading system messages survive).
        prefix, body := splitSystemPrefix(messages)

        historyTokensBefore := chunking.EstimateMessagesTokens(body)
        tokensRemovedNow := 0

        windowed, elided := chunking.WindowMessages(
                body,
                plan.HistoryBudget,
        )

        plan.Elided = elided

        if elided > 0 {
                result.Elided = elided

                logging.Default().Info(
                        "agent",
                        "history window: %d messages compacted (budget %d tok)",
                        elided,
                        plan.HistoryBudget,
                )

                onActivity(Activity{
                        Type: "thinking",
                        Caption: fmt.Sprintf(
                                "Context window: %d older messages compacted — key facts stay recallable",
                                elided,
                        ),
                        Timestamp: time.Now(),
                })
        }

        messages = append(
                append([]llm.Message{}, prefix...),
                windowed...,
        )

        // Phase 7 telemetry: how much history the windower removed.
        tokensRemovedNow = historyTokensBefore - chunking.EstimateMessagesTokens(windowed)
        if tokensRemovedNow < 0 {
                tokensRemovedNow = 0
        }

        // Measure the ACTUAL windowed history so the report reflects the
        // real prompt, not the allocation — and count ONLY history here:
        // the system prefix and the injected blocks already have their own
        // sections (the v1.1.3 double-count made the total exceed the real
        // prompt and is fatal under the Phase 7 fit gate).
        //
        // v1.2.8.1: the injected blocks are reconciled by SURVIVAL. The
        // windower may elide injected blocks when the current turn alone
        // fills the budget; subtracting the injection INTENT (the previous
        // `injectedNow`) regardless of survival mis-counted the prompt in
        // both directions. Section tokens now carry exactly what travels.
        survivedRecall := survivedBlockTokens(windowed, preWindowRecallBlocks)
        survivedRefs := survivedBlockTokens(windowed, preWindowRefBlocks)
        survivedSummary := survivedBlockTokens(windowed, preWindowSummaryBlocks)

        historyTokens = chunking.EstimateMessagesTokens(windowed) - survivedRecall - survivedRefs - survivedSummary
        if historyTokens < 0 {
                historyTokens = 0
        }

        plan.SetSectionTokens(
                contextplan.SectionHistory,
                historyTokens,
        )
        plan.SetSectionTokens(contextplan.SectionRecall, survivedRecall)
        plan.SetSectionTokens(contextplan.SectionHistoryRefs, survivedRefs)
        // v1.2.9: the summary now rides the data channel and can be elided
        // by the windower under pressure — report what SURVIVED, exactly
        // like the other injected blocks.
        plan.SetSectionTokens(contextplan.SectionSummary, survivedSummary)

        // v1.1.5 Phase 3: record the MEASURED prompt size (bytes actually
        // carried by the assembled messages) on the plan — the context report
        // now carries a real byte figure alongside the token estimates.
        plan.SetPromptBytes(int64(measureMessagesBytes(messages)))

        // Phase 7 context telemetry: one record per turn, recorded on every
        // exit path via defer. Observability only — telemetry never
        // influences the run.
        toolCallCount := 0
        toolSuccessCount := 0

        turnRecord := ctxtelemetry.TurnRecord{
                SessionID:          o.sessionIDValue(),
                Model:              cfg.EffectiveModel(),
                BudgetTokens:       effCtx.Effective,
                TokensAdded:        optionalTokens,
                RetrievalLatencyMs: composer.recallRetrievalMs,
                RetrievalHits:      result.Recalled,

                // v1.2.5 adaptive-tier telemetry (measured values only).
                Tier:            tier,
                ThinkingControl: ro.thinking,
                ToolPolicyMode:  ro.toolPolicy.Mode,
                NetSearch:       ro.netSearch,

                // 1.1.6 context telemetry: the full decision trail per turn.
                ContextRequested:    effCtx.Requested,
                ContextEffective:    effCtx.Effective,
                ContextModelMax:     effCtx.ModelMax,
                ContextEngineMax:    effCtx.EngineLimit,
                SessionContext:      effCtx.SessionPolicy,
                TokensSystem:        plan.SectionTokens(contextplan.SectionSystem),
                TokensTools:         plan.SectionTokens(contextplan.SectionTools),
                TokensRecall:        plan.SectionTokens(contextplan.SectionRecall),
                TokensAttachments:   plan.SectionTokens(contextplan.SectionAttachments),
                TokensHistory:       plan.SectionTokens(contextplan.SectionHistory),
                TokensOutputReserve: plan.SectionTokens(contextplan.SectionReserve),
                TokensSafetyReserve: safety,
                TokensCurrentTask:   chunking.EstimateTokens(task),
                TokensElided:        plan.Elided,
        }

        defer func() {
                turnRecord.UsedTokens = plan.TotalTokens()
                turnRecord.Pressure = plan.Pressure()
                turnRecord.TokensRemoved = tokensRemovedNow
                turnRecord.TokensCompressed = tokensCompressedNow + tokensRemovedNow
                turnRecord.OverflowPrevented = overflowPrevented
                total := turnRecord.TokensAdded + turnRecord.TokensRemoved
                if total > 0 {
                        turnRecord.CompressionRatio =
                                float64(turnRecord.TokensRemoved) / float64(total)
                }
                turnRecord.ToolCalls = toolCallCount
                turnRecord.ToolSuccesses = toolSuccessCount
                if toolCallCount > 0 {
                        turnRecord.ToolSuccessRate =
                                float64(toolSuccessCount) / float64(toolCallCount)
                }
                turnRecord.TaskSuccess = result.Text != ""
                turnRecord.Verified = string(result.Verification.Outcome)
                turnRecord.Escalations = len(composer.Escalations())
                turnRecord.FinalTier = composer.Tier()
                snap := clock.Snapshot()
                turnRecord.Timing = ctxtelemetry.RequestTiming{
                        ClassifyMs:        snap.ClassifyMs,
                        ContextMs:         snap.ContextMs,
                        PromptMs:          snap.PromptMs,
                        SerializationMs:   snap.SerializationMs,
                        TTFTMs:            snap.TTFTMs,
                        GenerationMs:      snap.GenerationMs,
                        ToolMs:            snap.ToolMs,
                        VerificationMs:    snap.VerificationMs,
                        TotalMs:           snap.TotalMs,
                        FirstPromptTokens: plan.TotalTokens(),
                }
                o.recordTelemetry(turnRecord)

                // v1.2.5: the result carries the measured timeline + tier trail.
                snap.FirstPromptTokens = plan.TotalTokens()
                result.Timing = snap
                result.FinalTier = composer.Tier()
                result.Escalations = composer.Escalations()
        }()

        // Final fit verification: the measured prompt must honor the
        // ceiling. History windowing bounds history by construction, so a
        // violation here means the current turn alone is huge — reported
        // honestly, never silently sent.
        if plan.Overflow() > 0 {
                overflow := plan.Overflow()
                overflowPrevented = true

                // v1.2.8.1: the refusal carries the exact measured split —
                // history vs fixed sections — so the reason is never
                // misattributed to the current turn when the fixed set is
                // the problem (or vice versa).
                breakdown := measuredSectionBreakdown(plan)

                logging.Default().Warn(
                        "agent",
                        "context fit violation after assembly: ~%d tok over the ceiling (effective ctx %d; %s; history %d/%d tok) — refusing the engine call",
                        overflow, effCtx.Effective, breakdown,
                        plan.SectionTokens(contextplan.SectionHistory), plan.HistoryBudget,
                )

                onActivity(Activity{
                        Type: "error",
                        Caption: fmt.Sprintf(
                                "This turn needs ~%d tokens more than the context budget allows (effective context %d; %s; history %d tok after compaction). Start a new session or reduce attachments.",
                                overflow, effCtx.Effective, breakdown,
                                plan.SectionTokens(contextplan.SectionHistory),
                        ),
                        Timestamp: time.Now(),
                })

                result.ContextUsage = continuum.EstimateUsage(messages, plan.HistoryBudget)
                return result, fmt.Errorf(
                        "context fit violation: assembled prompt exceeds effective context %d by ~%d tokens",
                        effCtx.Effective, overflow)
        }

        logging.Default().Info(
                "agent",
                "context metrics: promptBytes=%d estTokens=%d pressure=%.2f elided=%d recalled=%d attachments=%d effectiveCtx=%d safety=%d",
                plan.PromptBytes,
                plan.TotalTokens(),
                plan.Pressure(),
                plan.Elided,
                plan.Recalled,
                plan.Attachments,
                effCtx.Effective,
                safety,
        )

        // 1.1.6 §7: one structured per-turn context plan block BEFORE the
        // engine call — the plan as decided, not an overflow warning after
        // the fact.
        //
        // v1.2.5 status contract: the status reflects what ACTUALLY
        // happened during assembly. A record can never claim "compressed"
        // when no compression happened (the v1.2.4 defect: any automatic
        // adjustment — e.g. a toolset reduction — flipped the status to
        // compressed even at compressed=0):
        //
        //      raw             nothing was reduced or injected
        //      compressed      history/tool results were actually compressed
        //      elided          history was windowed without compression
        //      recalled        recall digests injected, nothing reduced
        //      adjusted        automatic repairs (toolset/briefing) applied
        //                      without any compression
        //      session-policy  a per-session context policy shaped the window
        //
        // Tier escalation is reported separately ("context escalation" log
        // plus telemetry Escalations/FinalTier) because it happens AFTER
        // this plan, during the tool loop — never during assembly.
        status := contextPlanStatus(
                tokensRemovedNow,
                plan.Elided,
                plan.Recalled,
                len(plan.Adjustments),
                effCtx.SessionPolicy,
        )

        // v1.2.5: an absent model/engine limit is reported as "unknown" —
        // never as 0, which read like a measured zero-capacity claim.
        modelMaxTxt := strconv.Itoa(effCtx.ModelMax)
        if effCtx.ModelMax <= 0 {
                modelMaxTxt = "unknown"
        }

        engineMaxTxt := strconv.Itoa(effCtx.EngineLimit)
        if effCtx.EngineLimit <= 0 {
                engineMaxTxt = "unknown"
        }

        logging.Default().Info(
                "agent",
                "context plan:\n\trequested=%d\n\teffective=%d\n\tmodelMax=%s\n\tengineMax=%s\n\tinputBudget=%d\n\testimated=%d\n\tcompressed=%d\n\telided=%d\n\tfinal=%d\n\toutputReserve=%d\n\tsafetyReserve=%d\n\tstatus=%s",
                effCtx.Requested,
                effCtx.Effective,
                modelMaxTxt,
                engineMaxTxt,
                plan.PromptCeiling(),
                plan.TotalTokens(),
                tokensRemovedNow,
                plan.Elided,
                plan.TotalTokens(),
                plan.SectionTokens(contextplan.SectionReserve),
                safety,
                status,
        )

        // v1.1.3: publish the context provenance report once per turn so
        // the UI can show the real budget split without exposing prompts.
        onActivity(Activity{
                Type:      "context",
                Caption:   plan.Summary(),
                Detail:    plan,
                Timestamp: time.Now(),
        })

        if len(plan.Adjustments) > 0 {
                onActivity(Activity{
                        Type:      "thinking",
                        Caption:   "Context auto-fit: " + strings.Join(plan.Adjustments, "; "),
                        Timestamp: time.Now(),
                })
        }

        maxIter := cfg.MaxIterations

        if maxIter < 1 {
                maxIter = 25
        }

        toolsUsed := map[string]bool{}

        // v1.2.5 escalation bookkeeping: the PRE-window body is retained so
        // a MissingHistory upgrade can re-window from the full transcript;
        // windowedLen/prefixLen locate the run-appended tail inside the
        // live message list.
        preWindowBody := append([]llm.Message{}, body...)
        prefixLen := len(prefix)
        windowedLen := len(windowed)

        // v1.0.7: peak prompt pressure across the turn's iterations — the
        // number the context meter shows after the reply lands. The budget
        // is the plan's explicit history allocation.
        budgetTokens := plan.HistoryBudget

        peakUsage := continuum.EstimateUsage(
                messages,
                budgetTokens,
        )

        for iter := 0; iter < maxIter; iter++ {
                if err := ctx.Err(); err != nil {
                        onActivity(Activity{
                                Type:      "done",
                                Caption:   abortCaption(err),
                                Timestamp: time.Now(),
                        })

                        return result, nil
                }

                onActivity(Activity{
                        Type: "thinking",
                        Caption: fmt.Sprintf(
                                "Iteration %d: planning next step...",
                                iter+1,
                        ),
                        Timestamp: time.Now(),
                })

                // Phase 7 in-loop fit guard: tool results appended during
                // the turn grow the prompt beyond the planned window. Elide
                // the OLDEST tool results (structure preserved, bodies
                // replaced by markers) until the request fits the ceiling
                // again — the final request is still guaranteed to fit.
                if elidedTools, savedTokens := compactToolResults(messages, plan.PromptCeiling()); elidedTools > 0 {
                        tokensCompressedNow += savedTokens
                        plan.AddAdjustment(fmt.Sprintf(
                                "in-loop: elided %d older tool result(s) to keep the request inside the budget",
                                elidedTools))

                        onActivity(Activity{
                                Type: "thinking",
                                Caption: fmt.Sprintf(
                                        "Context auto-fit: compacted %d older tool result(s) mid-turn",
                                        elidedTools),
                                Timestamp: time.Now(),
                        })
                }

                // v1.2.8.1 in-loop fit verification: tool-result compaction
                // only removes tool bodies. Growth from long assistant
                // turns, tool-call arguments, tier-escalation enrichment or
                // an oversized current turn can still leave the request
                // over the ceiling — refuse with the measured reason instead
                // of silently sending an over-ceiling request (the
                // pre-windowing gate cannot see mid-turn growth).
                if over := chunking.EstimateMessagesTokens(messages) - plan.PromptCeiling(); over > 0 {
                        overflowPrevented = true

                        logging.Default().Warn(
                                "agent",
                                "in-loop context fit violation: ~%d tok over the ceiling after tool-result compaction (effective ctx %d) — refusing the engine call",
                                over, effCtx.Effective,
                        )

                        onActivity(Activity{
                                Type: "error",
                                Caption: fmt.Sprintf(
                                        "Context budget exhausted mid-turn: even after compacting tool results the request needs ~%d tokens more than fit (effective context %d). Start a new session.",
                                        over, effCtx.Effective,
                                ),
                                Timestamp: time.Now(),
                        })

                        result.ContextUsage = peakUsage
                        return result, fmt.Errorf(
                                "context fit violation: in-loop request exceeds effective context %d by ~%d tokens after tool-result compaction",
                                effCtx.Effective, over)
                }

                // 1.1.6: the wire request carries the SAME effective
                // context the plan validated — never a second, larger
                // number from the global config. v1.2.5: the offered tool
                // surface is the tier/policy selection (composer), and the
                // prompt/serialization stages are measured.
                clock.Mark(StagePromptEnd)

                req := o.client.BuildChatRequestWithOptions(
                        cfg.EffectiveModel(),
                        messages,
                        composer.toolSpecs,
                        effCtx.Effective,
                )

                clock.Mark(StageSerialized)
                // NOTE (v1.2.6 continuation): StageRequestSent is NO LONGER
                // marked here — the previous code marked it immediately
                // after building the request struct, BEFORE any HTTP call,
                // so "request_sent → first_token" carried serialization and
                // connect time it never measured. The REAL mark is emitted by
                // the HTTP streaming client the moment the request is handed
                // to the transport (llm.TimingMarkRequestSent), together with
                // response_headers and first_byte — folded into the clock in
                // the streamChat callback below.

                if u := continuum.EstimateUsage(
                        messages,
                        budgetTokens,
                ); u.EstTokens > peakUsage.EstTokens {
                        peakUsage = u
                }

                var firstResponseSeen bool // v1.2.5: first-byte evidence for status events
                var assistantStarted bool  // v1.2.5: one status flip when the answer begins

                var raw strings.Builder    // raw content (may still contain <think> tags)
                var native strings.Builder // native reasoning_content deltas
                var lastToolCalls []llm.ToolCall

                // v1.0.1 streaming coalescer: emit throttled progress while tokens
                // arrive, then one final flush with the complete text so the last
                // chunk is never dropped. v1.0.2 splits <think> reasoning from
                // content at emit time (re-parsing the accumulated raw text is
                // cheap next to the O(n²) it replaced).
                var lastEmit time.Time

                // v1.0.9: frame-targeted emit cadence (SmoothStream → ~8ms).
                emitEvery := responseEmitInterval

                if cfg.SmoothStream {
                        emitEvery = cfg.EffectiveStreamEmitInterval()
                }

                reasoningEmitted := false

                emitProgress := func(force bool) {
                        reasoning, content := SplitThink(raw.String())

                        if reasoning == "" &&
                                content == "" &&
                                native.Len() == 0 {
                                return
                        }

                        if !force &&
                                time.Since(lastEmit) < emitEvery {
                                return
                        }

                        lastEmit = time.Now()

                        if r := reasoning + native.String(); r != "" {
                                // v1.2.5: thinking_start marks the reasoning
                                // panel OPEN (no content duplication — the
                                // marker carries no text).
                                if !reasoningEmitted {
                                        reasoningEmitted = true
                                        clock.Mark(StageFirstToken)

                                        onActivity(Activity{
                                                Type:      "thinking_start",
                                                Caption:   "Thinking…",
                                                Timestamp: time.Now(),
                                        })

                                        emitStatus("Thinking… · " + plan.TierSummary())
                                }

                                onActivity(Activity{
                                        Type:      "reasoning",
                                        Caption:   r,
                                        Timestamp: time.Now(),
                                })
                        }

                        if content != "" {
                                if firstResponseSeen && !assistantStarted {
                                        assistantStarted = true
                                        clock.Mark(StageFirstToken)

                                        emitStatus("First response… · " + plan.TierSummary())
                                }

                                onActivity(Activity{
                                        Type:      "response",
                                        Caption:   content,
                                        Timestamp: time.Now(),
                                })
                        }
                }

                perf, err := o.streamChat(
                        ctx,
                        req,
                        func(ev llm.StreamEvent) error {
                                // v1.2.6 continuation: transport-level timing
                                // observations from the HTTP client — request
                                // handed to the transport, response headers,
                                // first body byte READ. first_byte is NO
                                // LONGER derived from the first content delta
                                // (that is first_token semantics and was
                                // mislabelled for every v1.2.5 consumer).
                                if ev.TimingMark != "" {
                                        switch ev.TimingMark {
                                        case llm.TimingMarkRequestSent:
                                                clock.Mark(StageRequestSent)
                                        case llm.TimingMarkResponseHeader:
                                                clock.Mark(StageResponseHeaders)
                                        case llm.TimingMarkFirstByte:
                                                clock.Mark(StageFirstByte)
                                        }
                                        return nil
                                }

                                if ev.Content != "" {
                                        firstResponseSeen = true
                                        raw.WriteString(ev.Content)
                                        emitProgress(false)
                                }

                                if ev.Reasoning != "" {
                                        firstResponseSeen = true
                                        native.WriteString(ev.Reasoning)
                                        emitProgress(false)
                                }

                                if len(ev.ToolCalls) > 0 {
                                        lastToolCalls = ev.ToolCalls
                                }

                                return nil
                        },
                )

                clock.Mark(StageGenerationEnd)

                if err != nil {
                        if cerr := ctx.Err(); cerr != nil {
                                onActivity(Activity{
                                        Type:      "done",
                                        Caption:   abortCaption(cerr),
                                        Timestamp: time.Now(),
                                })

                                return result, nil
                        }

                        onActivity(Activity{
                                Type:      "error",
                                Caption:   "LLM error: " + err.Error(),
                                Timestamp: time.Now(),
                        })

                        return result, err
                }

                // v1.0.4: keep the speed telemetry of the last successful call
                // for the UI HUD.
                if hud := perf.String(); hud != "" {
                        result.Perf = hud

                        // v1.1.4: ShowPerfHUD was a stored setting no
                        // runtime path ever read. When enabled (the default)
                        // the live HUD line now reaches the activity stream
                        // ("perf" events never touch the streamed text).
                        if cfg.ShowPerfHUD {
                                onActivity(Activity{
                                        Type:      "perf",
                                        Caption:   hud,
                                        Timestamp: time.Now(),
                                })
                        }
                }

                emitProgress(true) // final flush — consumers always see the full text

                // v1.2.5: thinking_end folds the reasoning panel (marker only).
                if reasoningEmitted {
                        onActivity(Activity{
                                Type:      "thinking_end",
                                Caption:   "Thinking complete",
                                Timestamp: time.Now(),
                        })
                }

                if firstResponseSeen {
                        emitStatus("Generating… · " + plan.TierSummary())
                }

                // Split the completed stream into reasoning + clean content.
                reasoning, content := SplitThink(raw.String())

                if native.Len() > 0 {
                        reasoning = strings.TrimSpace(
                                native.String() + "\n" + reasoning,
                        )
                }

                // No tool calls → done
                if len(lastToolCalls) == 0 {
                        result.Text = content
                        result.Reasoning = reasoning
                        result.ToolsUsed = toolList(toolsUsed)
                        result.ContextUsage = peakUsage

                        // v1.1.5 Phase 6: the run's verification verdict —
                        // computed ONLY from objective evidence the tool
                        // traffic produced. A completion claim with no
                        // evidence reports not_verified, so the UI (and the
                        // multi-agent critic) can demand verification instead
                        // of celebrating an unproven "done".
                        clock.Mark(StageVerificationStart)
                        result.Verification = evidence.Report()
                        clock.Mark(StageVerificationEnd)
                        clock.AddVerificationMs(clock.sinceStages(StageVerificationStart, StageVerificationEnd))
                        result.LoopStats = guard.CallStats()
                        result.FailureTally = failTally

                        // v1.2.8: the task state's verification verdict comes
                        // from the same objective evidence collector — never
                        // from the model's own completion claims.
                        runTask.SetVerification(string(result.Verification.Outcome), result.Verification.Summary())
                        taskSnap := runTask.snapshot()
                        result.Task = &taskSnap

                        onActivity(Activity{
                                Type:      "verification",
                                Caption:   result.Verification.Summary(),
                                Detail:    result.Verification,
                                Timestamp: time.Now(),
                        })

                        doneCaption := "Completed"
                        if result.Verification.Outcome == VerificationFailed {
                                doneCaption = "Completed — verification FAILED: objective checks recorded and none passed"
                        }

                        clock.Mark(StageDone)

                        onActivity(Activity{
                                Type:      "done",
                                Caption:   doneCaption,
                                Timestamp: time.Now(),
                        })

                        // v1.2.5: the canonical completion marker for the new
                        // event vocabulary (tiny, no content duplication).
                        onActivity(Activity{
                                Type:      "complete",
                                Caption:   result.Verification.Summary(),
                                Detail:    map[string]any{"tier": composer.Tier(), "escalations": len(composer.Escalations())},
                                Timestamp: time.Now(),
                        })

                        return result, nil
                }

                // Add the assistant message (with tool calls) to the conversation.
                // The wire copy keeps content clean of think tags (they are display/
                // persistence metadata, not prompt material).
                assistantMsg := llm.Message{
                        Role:      "assistant",
                        Content:   content,
                        Reasoning: reasoning,
                        ToolCalls: lastToolCalls,
                }

                messages = append(
                        messages,
                        assistantMsg,
                )

                // Execute every tool call sequentially (parallel execution could be added)
                for _, tc := range lastToolCalls {
                        if err := ctx.Err(); err != nil {
                                onActivity(Activity{
                                        Type:      "done",
                                        Caption:   abortCaption(err),
                                        Timestamp: time.Now(),
                                })

                                return result, nil
                        }

                        caption := fmt.Sprintf(
                                "Calling tool: %s(%s)",
                                tc.Function.Name,
                                truncate(
                                        tc.Function.Arguments,
                                        80,
                                ),
                        )

                        onActivity(Activity{
                                Type:      "tool_start",
                                Caption:   caption,
                                Detail:    tc,
                                Timestamp: time.Now(),
                        })

                        // v1.2.8: task memory observes the attempt (INSPECT /
                        // ACT / TEST classification of the current step).
                        runTask.ObserveToolStart(tc.Function.Name, tc.Function.Arguments)

                        // v1.2.6 continuation: toolStart = immediately before
                        // this tool call is processed (execution, cache lookup
                        // or refusal — every path gets BOTH marks, and the
                        // per-path duration stays honest). The previous code
                        // marked tool_start AFTER tool.Run returned and never
                        // marked tool_end at all.
                        clock.Mark(StageToolStart)

                        tool, ok := o.tool(tc.Function.Name)

                        var result2 string

                        // v1.2.5 MANUAL tool mode FIRST: the user's selection
                        // overrides every automatic choice (tier budget
                        // included) and is NEVER silently re-enabled — refuse
                        // plainly, before any budget reasoning.
                        if ok && !ro.toolPolicy.allows(tc.Function.Name) {
                                ok = false

                                result2 = fmt.Sprintf(
                                        "Error: tool %q is excluded by the manual tool selection for this request. Allowed tools: %s. Do not call excluded tools — adapt your plan.",
                                        tc.Function.Name,
                                        strings.Join(composer.allNames, ", "),
                                )
                        }

                        // Phase 7A: when the dynamic toolset reduced the
                        // offered surface, a call to a non-offered tool is a
                        // planning error — refuse it with the offered list so
                        // the model re-plans within the budgeted toolset.
                        if ok && !containsName(composer.allNames, tc.Function.Name) {
                                ok = false

                                // v1.2.5: the model reached for a tool the tier
                                // did not offer — escalation evidence.
                                // v1.2.6: the refused tool's name travels with the
                                // evidence so the escalation re-offers exactly it.
                                watch.observeRefusal(tc.Function.Name)

                                result2 = fmt.Sprintf(
                                        "Error: tool %q is not part of the toolset offered this turn (context budget). Offered tools: %s.",
                                        tc.Function.Name,
                                        strings.Join(composer.allNames, ", "),
                                )
                        }

                        if ok &&
                                !cfg.ToolEnabled(
                                        tc.Function.Name,
                                ) {
                                // v1.0.2 tool selection: the model reached for a tool the
                                // user switched off. Tell it plainly so it re-plans instead
                                // of retrying.
                                ok = false

                                result2 = fmt.Sprintf(
                                        "Error: tool %q is disabled by the user. Enabled tools: %s. Do not call disabled tools — adapt your plan.",
                                        tc.Function.Name,
                                        strings.Join(
                                                composer.allNames,
                                                ", ",
                                        ),
                                )
                        }

                        if !ok &&
                                result2 == "" {
                                result2 = fmt.Sprintf(
                                        "Error: unknown tool %q. Available: %s",
                                        tc.Function.Name,
                                        strings.Join(
                                                composer.allNames,
                                                ", ",
                                        ),
                                )
                        }

                        if result2 != "" {
                                // v1.2.8: a refusal IS a settled outcome — the
                                // task state records it as a failure of this
                                // call (the model must re-plan, and the UI
                                // should show why).
                                runTask.ObserveToolEnd(tc.Function.Name, tc.Function.Arguments, result2, true)

                                onActivity(Activity{
                                        Type: "tool_end",
                                        Caption: fmt.Sprintf(
                                                "Tool %s → %s",
                                                tc.Function.Name,
                                                truncate(
                                                        result2,
                                                        80,
                                                ),
                                        ),
                                        Detail:    result2,
                                        Timestamp: time.Now(),
                                })

                                // Refused/unknown-tool path settled: tool_end
                                // fires for EVERY path (v1.2.6 continuation).
                                clock.Mark(StageToolEnd)

                                messages = append(
                                        messages,
                                        llm.Message{
                                                Role:       "tool",
                                                Content:    result2,
                                                ToolCallID: tc.ID,
                                                Name:       tc.Function.Name,
                                        },
                                )

                                continue
                        }

                        // v1.1.5 Phase 6 loop guard: observe the call BEFORE
                        // executing it. A repeat gets a strategy-change
                        // warning appended to its result; a call beyond the
                        // repeat bound (or past the run's tool/wall-clock
                        // budget) is refused outright with an instruction to
                        // produce the final answer instead.
                        obs := guard.Observe(
                                tc.Function.Name,
                                tc.Function.Arguments,
                        )

                        if obs.Block != "" {
                                runTask.ObserveToolEnd(tc.Function.Name, tc.Function.Arguments, obs.Block, true)

                                onActivity(Activity{
                                        Type: "tool_end",
                                        Caption: fmt.Sprintf(
                                                "Tool %s refused by loop guard",
                                                tc.Function.Name,
                                        ),
                                        Detail:    obs.Block,
                                        Timestamp: time.Now(),
                                })

                                // Loop-guard refusal settled: both marks fired
                                // (v1.2.6 continuation).
                                clock.Mark(StageToolEnd)

                                messages = append(
                                        messages,
                                        llm.Message{
                                                Role:       "tool",
                                                Content:    obs.Block,
                                                ToolCallID: tc.ID,
                                                Name:       tc.Function.Name,
                                        },
                                )

                                continue
                        }

                        start := time.Now()

                        // v1.2.5: identical successful deterministic calls are
                        // served from the bounded result cache — a repeat is
                        // evidence-free reuse, never a re-execution.
                        cacheKey := tc.Function.Name + "\x00" + normalizeArgsForCache(tc.Function.Arguments)

                        if cacheableCall(tc.Function.Name, tc.Function.Arguments) {
                                if cached, hit := globalResultCache.Get(cacheKey); hit {
                                        result2, err = cached, error(nil)

                                        runTask.ObserveToolEnd(tc.Function.Name, tc.Function.Arguments, cached, false)

                                        onActivity(Activity{
                                                Type: "tool_end",
                                                Caption: fmt.Sprintf(
                                                        "Tool %s served from cache",
                                                        tc.Function.Name,
                                                ),
                                                Detail:    cached,
                                                Timestamp: time.Now(),
                                        })

                                        logging.Default().ToolCall(logging.ToolCallRecord{
                                                TS:      start,
                                                Tool:    tc.Function.Name,
                                                Args:    tc.Function.Arguments,
                                                Result:  cached,
                                                Session: o.currentSessionID(),
                                        })

                                        toolsUsed[tc.Function.Name] = true
                                        toolCallCount++
                                        toolSuccessCount++

                                        // v1.2.5: a cached result is the SAME
                                        // escalation evidence (the file is
                                        // still missing from the prompt).
                                        watch.observeToolResult(tc.Function.Name, cached, false)

                                        obs2 := guard.Observe(tc.Function.Name, tc.Function.Arguments)
                                        if obs2.Warn != "" {
                                                result2 += "\n\n" + obs2.Warn
                                        }

                                        // Cache-served path settled: tool_end
                                        // fires for the cache path too
                                        // (v1.2.6 continuation).
                                        clock.Mark(StageToolEnd)

                                        messages = append(
                                                messages,
                                                llm.Message{
                                                        Role:       "tool",
                                                        Content:    result2,
                                                        ToolCallID: tc.ID,
                                                        Name:       tc.Function.Name,
                                                },
                                        )

                                        continue
                                }
                        }

                        result2, err = tool.Run(
                                ctx,
                                json.RawMessage(
                                        tc.Function.Arguments,
                                ),
                        )

                        // v1.2.5: genuinely retryable failures (transient
                        // network/process) retry exactly once — never
                        // side-effecting tools, never more than once.
                        if err != nil && idempotentNetworkTool(tc.Function.Name) {
                                cat := ClassifyFailure(ToolFailure{
                                        Tool:   tc.Function.Name,
                                        Args:   tc.Function.Arguments,
                                        Err:    err.Error(),
                                        Output: result2,
                                        Timeout: errors.Is(err, context.DeadlineExceeded) ||
                                                strings.Contains(err.Error(), "timeout"),
                                })

                                if cat == CatNetwork || cat == CatProcess {
                                        logging.Default().Info(
                                                "agent",
                                                "tool %s transient failure (%s) — retrying once",
                                                tc.Function.Name, cat,
                                        )

                                        result2, err = tool.Run(
                                                ctx,
                                                json.RawMessage(
                                                        tc.Function.Arguments,
                                                ),
                                        )
                                }
                        }

                        dur := time.Since(start)

                        // Measured execution time accumulates; tool_end fires
                        // immediately after the REAL execution (including its
                        // single transient retry) — never reusing another
                        // stage's timestamp (v1.2.6 continuation).
                        clock.AddToolMs(dur.Milliseconds())
                        clock.Mark(StageToolEnd)

                        // v1.0.6 VISION: tools that produce images (screenshot,
                        // future chart renderers) tag them with [[IMG:path]]
                        // markers. The marker never reaches the model as text — the
                        // path rides the tool message's Images field and the client
                        // converts it into an image_url part the vision encoder can
                        // actually see.
                        result2, toolImages := ExtractImageMarkers(
                                result2,
                        )

                        toolsUsed[tc.Function.Name] = true

                        if len(toolImages) > 0 {
                                watch.observeVisionPayload()
                        }

                        // Log catcher: one structured record per tool call
                        rec := logging.ToolCallRecord{
                                TS:         start,
                                Tool:       tc.Function.Name,
                                Args:       tc.Function.Arguments,
                                Result:     result2,
                                DurationMs: dur.Milliseconds(),
                                Session:    o.currentSessionID(),
                        }

                        if err != nil {
                                rec.Error = err.Error()
                                rec.Result = ""
                        }

                        logging.Default().ToolCall(rec)

                        // v1.1.5 Phase 6: record the outcome for repeat
                        // detection and collect verification evidence (lab
                        // verify actions, build/test outcomes).
                        rawOutput := result2

                        guard.Record(
                                tc.Function.Name,
                                tc.Function.Arguments,
                                result2,
                        )

                        // v1.2.5: escalation evidence + result caching.
                        watch.observeToolResult(tc.Function.Name, result2, err != nil)

                        if err == nil && cacheableCall(tc.Function.Name, tc.Function.Arguments) {
                                globalResultCache.Put(cacheKey, result2)
                        }

                        evidence.ObserveToolResult(
                                tc.Function.Name,
                                tc.Function.Arguments,
                                result2,
                                err != nil,
                        )

                        // v1.2.8: task memory folds the settled outcome —
                        // files changed, commands/tests run, failures and
                        // repairs. rawOutput is the pre-decoration result so
                        // the state records what actually happened.
                        runTask.ObserveToolEnd(tc.Function.Name, tc.Function.Arguments, rawOutput, err != nil)

                        // Phase 7 telemetry counters.
                        toolCallCount++
                        if err == nil {
                                toolSuccessCount++
                        }

                        if err != nil {
                                // Preserve the tool's output alongside the error —
                                // tools like git/shell return diagnostic stderr
                                // that the LLM needs to self-correct.
                                if result2 != "" {
                                        result2 = fmt.Sprintf(
                                                "Error: %v\n\nTool output:\n%s",
                                                err,
                                                truncate(
                                                        result2,
                                                        4000,
                                                ),
                                        )
                                } else {
                                        result2 = fmt.Sprintf(
                                                "Error: %v",
                                                err,
                                        )
                                }

                                // v1.1.5 Phase 6 failure classification:
                                // diagnose WHY the call failed and append the
                                // category's repair hint so the model re-plans
                                // against the diagnosis instead of retrying
                                // blind. The category is also surfaced in the
                                // activity timeline and tallied on the result.
                                cat := ClassifyFailure(ToolFailure{
                                        Tool:   tc.Function.Name,
                                        Args:   tc.Function.Arguments,
                                        Err:    err.Error(),
                                        Output: rawOutput,
                                        Timeout: errors.Is(err, context.DeadlineExceeded) ||
                                                strings.Contains(err.Error(), "timeout"),
                                })
                                failTally[cat]++

                                result2 += "\n\n[" + string(cat) + "] " + RepairHint(cat)

                                onActivity(Activity{
                                        Type: "tool_end",
                                        Caption: fmt.Sprintf(
                                                "Tool %s FAILED (%v): %s",
                                                tc.Function.Name,
                                                dur.Round(time.Millisecond),
                                                err.Error(),
                                        ),
                                        Detail:    result2,
                                        Timestamp: time.Now(),
                                })

                                onActivity(Activity{
                                        Type:      "failure",
                                        Caption:   fmt.Sprintf("Diagnosed failure: %s", cat),
                                        Detail:    cat,
                                        Timestamp: time.Now(),
                                })
                        } else {
                                if obs.Warn != "" {
                                        result2 += "\n\n" + obs.Warn
                                }

                                onActivity(Activity{
                                        Type: "tool_end",
                                        Caption: fmt.Sprintf(
                                                "Tool %s done (%v): %s",
                                                tc.Function.Name,
                                                dur.Round(time.Millisecond),
                                                truncate(
                                                        result2,
                                                        80,
                                                ),
                                        ),
                                        Detail:    result2,
                                        Timestamp: time.Now(),
                                })
                        }

                        messages = append(
                                messages,
                                llm.Message{
                                        Role:       "tool",
                                        Content:    result2,
                                        ToolCallID: tc.ID,
                                        Name:       tc.Function.Name,
                                        Images:     toolImages,
                                },
                        )

                        if cerr := ctx.Err(); cerr != nil {
                                onActivity(Activity{
                                        Type:      "done",
                                        Caption:   abortCaption(cerr),
                                        Timestamp: time.Now(),
                                })

                                return result, nil
                        }
                }

                // v1.2.8: publish the bounded task state ONCE per tool round.
                // The UI's agent pipeline view renders the REAL runtime state
                // (not a decorative stage strip), and runLive folds this into
                // the authoritative snapshot so a reconnect mid-run restores
                // the task view — the run never restarts because the UI did.
                taskSnap := runTask.snapshot()
                onActivity(Activity{
                        Type:      "task",
                        Caption:   taskStepCaption(taskSnap),
                        Detail:    taskSnap,
                        Timestamp: time.Now(),
                })

                // --------------------------------------------------------------
                // v1.2.5 CONTEXT ESCALATION: after each tool round, apply the
                // recorded evidence. The upgrade enriches the LIVE
                // conversation (never a from-scratch rebuild): optional
                // blocks the new tier allows are injected, the history
                // window re-opens at the new share, and the offered tool
                // surface widens (AUTO policy only — manual selection is
                // never re-enabled).
                // --------------------------------------------------------------
                if reason, refusedTool, ok := watch.take(); ok {
                        if up, ok2 := composer.Escalate(reason, task, refusedTool); ok2 {
                                tier = up.tier

                                // Re-plan at the new tier's share and re-window
                                // FIRST — from the RETAINED pre-window body +
                                // the run-appended tail — so the newly injected
                                // blocks ride the FINAL list, never the tail
                                // boundary.
                                sysTokens, _, _ = classifyMessages(messages)
                                plan = composePlan(sysTokens, composer.toolTok, composer.OptionalTokens(stagedTokens), summaryTokens, histRefTokens)

                                tail := messages[min(prefixLen+windowedLen, len(messages)):]
                                newBody := append(append([]llm.Message{}, preWindowBody...), tail...)
                                windowed2, elided2 := chunking.WindowMessages(newBody, plan.HistoryBudget)

                                prefix = messages[:prefixLen]
                                messages = append(append([]llm.Message{}, prefix...), windowed2...)
                                preWindowBody = newBody
                                windowedLen = len(windowed2)

                                // THEN enrich the live conversation: briefing
                                // upgrade + the blocks the new tier allows.
                                if up.fullBrief != "" && len(messages) > 0 && messages[0].Role == "system" {
                                        messages[0] = llm.Message{Role: "system", Content: up.fullBrief}

                                        if composer.ThinkingEnabled() {
                                                messages = ensureThinkingNudge(messages)
                                        }
                                }

                                // v1.2.8.1: escalation-injected blocks are
                                // added AFTER re-windowing (outside the
                                // windower's reach), so they always travel —
                                // accumulate them separately and reconcile
                                // the re-windowed blocks by survival.
                                postWindowRecallTokens := 0

                                if up.card != "" {
                                        messages = insertBeforeLastUser(messages, llm.Message{Role: "system", Content: up.card})
                                        postWindowRecallTokens += chunking.EstimateTokens(up.card)
                                }

                                if up.repo != "" {
                                        messages = insertBeforeLastUser(messages, llm.Message{Role: "system", Content: up.repo})
                                        postWindowRecallTokens += chunking.EstimateTokens(up.repo)
                                }

                                if up.skills != "" {
                                        messages = insertBeforeLastUser(messages, llm.Message{Role: "system", Content: up.skills})
                                        postWindowRecallTokens += chunking.EstimateTokens(up.skills)
                                }

                                if up.recall != "" {
                                        messages = insertBeforeLastUser(messages, llm.Message{Role: "system", Content: up.recall})
                                        postWindowRecallTokens += chunking.EstimateTokens(up.recall)

                                        if n := strings.Count(up.recall, "user asked:"); n > 0 {
                                                result.Recalled += n
                                                plan.Recalled = result.Recalled
                                        }
                                }

                                if elided2 != plan.Elided {
                                        plan.Elided = elided2
                                        result.Elided = elided2
                                }

                                // v1.2.8.1 survival reconciliation (same
                                // rule as the initial windowing): measure
                                // what the re-window actually kept.
                                survivedRecall2 := survivedBlockTokens(windowed2, preWindowRecallBlocks)
                                survivedRefs2 := survivedBlockTokens(windowed2, preWindowRefBlocks)

                                historyTokens := chunking.EstimateMessagesTokens(windowed2) - survivedRecall2 - survivedRefs2
                                if historyTokens < 0 {
                                        historyTokens = 0
                                }
                                plan.SetSectionTokens(contextplan.SectionHistory, historyTokens)
                                plan.SetSectionTokens(contextplan.SectionRecall, survivedRecall2+postWindowRecallTokens)
                                plan.SetSectionTokens(contextplan.SectionHistoryRefs, survivedRefs2)
                                plan.SetPromptBytes(int64(measureMessagesBytes(messages)))

                                tokensRemovedNow = historyTokensBefore - chunking.EstimateMessagesTokens(windowed2)
                                if tokensRemovedNow < 0 {
                                        tokensRemovedNow = 0
                                }

                                logging.Default().Info(
                                        "agent",
                                        "context escalation: %s",
                                        up.escalation.Describe(),
                                )

                                onActivity(Activity{
                                        Type:      "escalation",
                                        Caption:   up.escalation.Describe(),
                                        Detail:    up.escalation,
                                        Timestamp: time.Now(),
                                })

                                emitStatus("Using more context · " + plan.TierSummary())
                        } else {
                                logging.Default().Info(
                                        "agent",
                                        "escalation evidence (%s) at tier %s — ladder exhausted or bound reached, continuing",
                                        reason, composer.Tier(),
                                )
                        }
                }
        }

        result.ToolsUsed = toolList(toolsUsed)
        result.ContextUsage = peakUsage
        clock.Mark(StageVerificationStart)
        result.Verification = evidence.Report()
        clock.Mark(StageVerificationEnd)
        clock.AddVerificationMs(clock.sinceStages(StageVerificationStart, StageVerificationEnd))
        result.LoopStats = guard.CallStats()
        result.FailureTally = failTally

        // v1.2.8: the max-iterations exit is a REAL outcome — the task state
        // records the verdict and travels with the result like every other
        // exit path.
        runTask.SetVerification(string(result.Verification.Outcome), result.Verification.Summary())
        taskSnap := runTask.snapshot()
        result.Task = &taskSnap

        onActivity(Activity{
                Type:      "verification",
                Caption:   result.Verification.Summary(),
                Detail:    result.Verification,
                Timestamp: time.Now(),
        })

        clock.Mark(StageDone)

        onActivity(Activity{
                Type:      "done",
                Caption:   "Max iterations reached",
                Timestamp: time.Now(),
        })

        onActivity(Activity{
                Type:      "complete",
                Caption:   "Max iterations reached",
                Detail:    map[string]any{"tier": composer.Tier(), "escalations": len(composer.Escalations())},
                Timestamp: time.Now(),
        })

        return result, fmt.Errorf(
                "max iterations (%d) reached",
                maxIter,
        )
}

// imageMarkerPrefix/ExtractImageMarkers implement the v1.0.6 vision bridge
// between tools and the multimodal client: a tool tags its output with
// [[IMG:path]] and the orchestrator moves those paths onto the tool message's
// Images field (the client turns them into image_url parts).
const imageMarkerPrefix = "[[IMG:"

// classifyMessages measures the composed prompt into three buckets:
//
//   - system tokens: the leading system block (AI-context briefing and any
//     caller-provided system prompt) — the stable prefix
//   - injected tokens: system blocks inside the body (recall digests,
//     staged-attachment blocks) that arrive between history
//   - injected block count (for provenance reporting)
//
// The measurement is cheap (one pass, token estimates only) and honest:
// the plan reflects what is actually in the message list, not estimates
// from config values.
func classifyMessages(messages []llm.Message) (sysTokens, injectedTokens, injectedBlocks int) {
        inPrefix := true

        for i := range messages {
                m := messages[i]

                if inPrefix && m.Role != "system" {
                        inPrefix = false
                }

                tokens := chunking.EstimateTokens(m.Content)

                for _, tc := range m.ToolCalls {
                        tokens += chunking.EstimateTokens(tc.Function.Arguments) + 4
                }

                if inPrefix {
                        sysTokens += tokens
                        continue
                }

                if m.Role == "system" {
                        injectedTokens += tokens
                        injectedBlocks++
                }
        }

        return sysTokens, injectedTokens, injectedBlocks
}

// measureMessagesBytes returns the measured byte size of the message
// contents (content + tool-call arguments). Phase 3 instrumentation:
// bytes are counted, never estimated.
func measureMessagesBytes(messages []llm.Message) int {
        total := 0

        for i := range messages {
                total += len(messages[i].Content)

                for _, tc := range messages[i].ToolCalls {
                        total += len(tc.Function.Arguments)
                }
        }

        return total
}

// survivedBlockTokens (v1.2.8.1) sums the token cost of the injected
// blocks that are still present in msgs (matched by role + exact
// content). The history windower can elide injected blocks when the
// current turn alone fills the budget; honest section accounting must
// measure what actually survived, not the injection intent.
func survivedBlockTokens(msgs []llm.Message, blocks []llm.Message) int {
        if len(blocks) == 0 {
                return 0
        }

        present := make(map[string]struct{}, len(msgs))
        for i := range msgs {
                present[msgs[i].Role+"\x00"+msgs[i].Content] = struct{}{}
        }

        total := 0
        for _, b := range blocks {
                if _, ok := present[b.Role+"\x00"+b.Content]; ok {
                        total += chunking.EstimateTokens(b.Content)
                }
        }
        return total
}

// measuredSectionBreakdown (v1.2.8.1) renders the measured token cost of
// the prompt sections that actually travel (system, tools, summary,
// recall, history-refs) as a compact "system 412 · tools 1180" style
// breakdown for refusal gates — the refusal names what does not fit.
func measuredSectionBreakdown(p contextplan.Plan) string {
        order := []string{
                contextplan.SectionSystem,
                contextplan.SectionTools,
                contextplan.SectionSummary,
                contextplan.SectionRecall,
                contextplan.SectionHistoryRefs,
                contextplan.SectionAttachments,
        }

        parts := make([]string, 0, len(order))
        for _, name := range order {
                for _, s := range p.Sections {
                        if s.Name != name || s.Tokens <= 0 {
                                continue
                        }
                        if !s.Included && s.Name != contextplan.SectionSystem {
                                continue
                        }
                        parts = append(parts, fmt.Sprintf("%s %d", s.Name, s.Tokens))
                        break
                }
        }

        if len(parts) == 0 {
                return "no fixed sections"
        }
        return strings.Join(parts, " · ")
}

// historyBudgetFor was removed in v1.1.3: the context plan (contextplan
// package) is the single budget authority and tool schemas are measured
// exactly before windowing.

// estimateToolSpecsTokens measures the real serialized tool schema cost.
func estimateToolSpecsTokens(specs []llm.ToolSpec) int {
        total := 0

        for _, spec := range specs {
                data, err := json.Marshal(spec)
                if err != nil {
                        continue
                }

                total += chunking.EstimateTokens(string(data))
        }

        return total
}

// ExtractImageMarkers pulls every [[IMG:path]] marker out of a tool result,
// returning the cleaned text (markers stripped, runs of blank lines
// collapsed) and the image paths in order. Paths are taken verbatim — tools
// are trusted to emit absolute paths they just wrote.
func ExtractImageMarkers(
        s string,
) (string, []string) {
        if !strings.Contains(
                s,
                imageMarkerPrefix,
        ) {
                return s, nil
        }

        var paths []string
        var b strings.Builder

        for {
                i := strings.Index(
                        s,
                        imageMarkerPrefix,
                )

                if i < 0 {
                        b.WriteString(s)
                        break
                }

                rest := s[i+len(imageMarkerPrefix):]

                end := strings.Index(
                        rest,
                        "]]",
                )

                if end < 0 {
                        b.WriteString(s)
                        break
                }

                b.WriteString(s[:i])

                p := strings.TrimSpace(
                        rest[:end],
                )

                if p != "" {
                        paths = append(
                                paths,
                                p,
                        )
                }

                s = rest[end+2:]
        }

        out := b.String()

        // collapse the blank runs the stripped markers leave behind
        for strings.Contains(
                out,
                "\n\n\n",
        ) {
                out = strings.ReplaceAll(
                        out,
                        "\n\n\n",
                        "\n\n",
                )
        }

        return strings.TrimSpace(out), paths
}

// SplitThink separates <think> reasoning blocks from regular content.
//
// Handles multiple blocks, an unclosed trailing block (everything after the
// opening tag is reasoning), and surrounding whitespace. Stray closing tags
// without an opener (models that fumble the protocol) are stripped as noise.
// Text without tags returns unchanged — so models that never think are
// unaffected, and models that spontaneously emit <think> (Qwen3-style) get
// their trace extracted even when thinking mode is off.
func SplitThink(
        raw string,
) (reasoning, content string) {
        if !strings.Contains(
                raw,
                "<think>",
        ) {
                // No opening tag — but a stray closer may still be noise.
                if strings.Contains(
                        raw,
                        "</think>",
                ) {
                        return "",
                                strings.TrimSpace(
                                        strings.ReplaceAll(
                                                raw,
                                                "</think>",
                                                "",
                                        ),
                                )
                }

                return "", raw
        }

        var think strings.Builder
        var body strings.Builder
        rest := raw

        for {
                open := strings.Index(
                        rest,
                        "<think>",
                )

                if open < 0 {
                        body.WriteString(rest)
                        break
                }

                body.WriteString(
                        rest[:open],
                )

                rest = rest[open+len("<think>"):]

                close := strings.Index(
                        rest,
                        "</think>",
                )

                if close < 0 {
                        // unclosed: the rest is reasoning (stream interrupted mid-think)
                        think.WriteString(
                                strings.TrimSpace(rest),
                        )
                        break
                }

                think.WriteString(
                        strings.TrimSpace(
                                rest[:close],
                        ),
                )

                think.WriteString("\n")

                rest = rest[close+len("</think>"):]
        }

        return strings.TrimSpace(
                        think.String(),
                ),
                strings.TrimSpace(
                        body.String(),
                )
}

// ensureThinkingNudge appends the thinking instructions to the AI-context
// system message (idempotent).
func ensureThinkingNudge(
        messages []llm.Message,
) []llm.Message {
        for i := range messages {
                if messages[i].Role == "system" &&
                        strings.Contains(
                                messages[i].Content,
                                aicontext.HeaderSentinel,
                        ) {
                        if !strings.Contains(
                                messages[i].Content,
                                thinkingNudgeSentinel,
                        ) {
                                cp := make(
                                        []llm.Message,
                                        len(messages),
                                )

                                copy(
                                        cp,
                                        messages,
                                )

                                cp[i].Content += thinkingNudge

                                return cp
                        }

                        return messages
                }

                // only system messages may precede the conversation
                if messages[i].Role != "system" {
                        break
                }
        }

        return messages
}

// lastUserQuery returns the content of the final user message (the incoming
// question), or "".
func lastUserQuery(
        messages []llm.Message,
) string {
        for i := len(messages) - 1; i >= 0; i-- {
                if messages[i].Role == "user" {
                        return messages[i].Content
                }
        }

        return ""
}

// insertBeforeLastUser splices msg into the list immediately before the
// final user message (cache-friendly recall position).
func insertBeforeLastUser(
        messages []llm.Message,
        msg llm.Message,
) []llm.Message {
        out := make(
                []llm.Message,
                0,
                len(messages)+1,
        )

        inserted := false
        lastUser := -1

        for i := len(messages) - 1; i >= 0; i-- {
                if messages[i].Role == "user" {
                        lastUser = i
                        break
                }
        }

        for i := range messages {
                if i == lastUser {
                        out = append(
                                out,
                                msg,
                        )

                        inserted = true
                }

                out = append(
                        out,
                        messages[i],
                )
        }

        if !inserted {
                out = append(
                        out,
                        msg,
                )
        }

        return out
}

// splitSystemPrefix separates leading system messages from the conversation
// body. Windowing only compacts the body — the AI-context briefing always
// travels with the prompt.
func splitSystemPrefix(
        messages []llm.Message,
) (prefix, body []llm.Message) {
        i := 0

        for i < len(messages) &&
                messages[i].Role == "system" {
                i++
        }

        return messages[:i],
                messages[i:]
}

// splitInjectedSystemBlocks detaches MID-conversation system messages
// (staged attachment blocks, recall digests, project cards) so the preflight
// planner — not message order — decides what travels this turn. Leading
// system messages (the briefing) and everything from the last user message
// onward are preserved verbatim.
func splitInjectedSystemBlocks(
        messages []llm.Message,
) (kept, blocks []llm.Message) {
        prefixLen := 0
        for prefixLen < len(messages) && messages[prefixLen].Role == "system" {
                prefixLen++
        }

        lastUser := -1
        for i := len(messages) - 1; i >= 0; i-- {
                if messages[i].Role == "user" {
                        lastUser = i
                        break
                }
        }

        kept = make([]llm.Message, 0, len(messages))

        for i, m := range messages {
                if m.Role == "system" && i >= prefixLen && (lastUser == -1 || i < lastUser) {
                        blocks = append(blocks, m)
                        continue
                }
                kept = append(kept, m)
        }

        return kept, blocks
}

// contextSafetyMargin computes the Phase 7 headroom held back from the
// prompt on top of the output reserve: ~1.5% of the window, clamped to
// [64, 256] tokens. This absorbs token-estimator error and chat-template
// expansion so "fits" really means fits.
func contextSafetyMargin(effectiveCtx int) int {
        m := effectiveCtx / 64
        if m < 64 {
                m = 64
        }
        if m > 256 {
                m = 256
        }
        return m
}

// ContextSafetyMargin is the exported form of the run's safety reserve
// (1.1.6): the API layer reports the same figure the planner holds back
// so the UI never computes a different truth.
func ContextSafetyMargin(effectiveCtx int) int {
        return contextSafetyMargin(effectiveCtx)
}

// resolveEffectiveContext computes the model-aware context decision for
// the configured model: min(configured, GGUF model limit, engine-reported
// limit). Remote providers keep their configured value (the remote engine
// owns its window). sessionContext > 0 applies the per-session context
// policy on top (1.1.6): the policy can narrow the window freely and may
// raise it for THIS chat only up to what the model/engine genuinely serve.
//
// v1.2.9: resolving the model ALSO tunes the token-estimator tier — when
// a local model is resolved, its tokenizer family (from the GGUF card)
// activates the family-tuned estimator tier (chunking tier 2) unless the
// native engine's EXACT tokenizer tier (tier 1) is already installed.
// Remote or unresolved models keep the conservative fallback heuristic.
func (o *Orchestrator) resolveEffectiveContext(cfg *config.Config, sessionContext int) llm.EffectiveContext {
        engineLimit := 0
        if o.ctxLimits != nil {
                engineLimit = o.ctxLimits(cfg)
        }

        if cfg.IsRemote() {
                chunking.ResetTokenEstimator()
                return llm.ResolveSessionContext(cfg, nil, engineLimit, sessionContext)
        }

        modelPath, err := llm.ResolveModelPath(cfg.ModelsDir, cfg.Model)
        if err != nil {
                chunking.ResetTokenEstimator()
                return llm.ResolveSessionContext(cfg, nil, engineLimit, sessionContext)
        }

        caps := llm.ResolveModelCapabilities(cfg, modelPath)

        // The exact tier (native engine tokenizer) outranks the family tier;
        // only install the family estimator when exact counting is absent.
        if chunking.EstimatorKind() < chunking.EstimatorExact {
                if fam := familyForTokenizer(caps.TokenizerFamily); fam != "" {
                        chunking.SetModelFamilyEstimator(fam)
                } else {
                        chunking.ResetTokenEstimator()
                }
        }

        return llm.ResolveSessionContext(cfg, caps, engineLimit, sessionContext)
}

// familyForTokenizer maps the GGUF card's coarse tokenizer-family label
// onto the estimator family key. Empty = unknown → conservative
// heuristic stays active.
func familyForTokenizer(label string) string {
        switch strings.ToLower(strings.TrimSpace(label)) {
        case "spm (sentencepiece bpe)", "spm":
                return "spm"
        case "bpe", "bpe/gpt-style":
                return "bpe"
        default:
                return ""
        }
}

// compactToolResults brings an over-ceiling prompt back inside the budget
// (in-loop fit guard):
//
//      pass 1 — elide every tool result except the freshest (explicit
//               marker replaces the body; message structure preserved);
//      pass 2 — if still over, BOUND the freshest result to the remaining
//               room (head kept, tail replaced by an explicit marker).
//
// The model keeps the newest evidence either way; older results carry a
// re-run hint. Returns the number of tool results touched and the
// approximate tokens freed (for telemetry).
func compactToolResults(messages []llm.Message, ceiling int) (int, int) {
        if ceiling <= 0 {
                return 0, 0
        }

        total := chunking.EstimateMessagesTokens(messages)
        if total <= ceiling {
                return 0, 0
        }

        lastTool := -1
        for i := len(messages) - 1; i >= 0; i-- {
                if messages[i].Role == "tool" {
                        lastTool = i
                        break
                }
        }

        elided, savedTokens := 0, 0

        for i := 0; i < len(messages) && total > ceiling; i++ {
                if i == lastTool {
                        continue // pass 2 handles the freshest result
                }
                if messages[i].Role != "tool" {
                        continue
                }
                if strings.HasPrefix(messages[i].Content, "[tool result elided") {
                        continue
                }

                old := chunking.EstimateMessagesTokens([]llm.Message{messages[i]})
                messages[i].Content = fmt.Sprintf(
                        "[tool result elided under context pressure — original %d tokens; the full result remains in the session transcript; re-run the tool if the result is needed]",
                        old)
                freed := old - chunking.EstimateMessagesTokens([]llm.Message{messages[i]})
                total -= freed
                savedTokens += freed
                elided++
        }

        // Pass 2: bound the freshest result to whatever room remains.
        if total > ceiling && lastTool >= 0 &&
                !strings.HasPrefix(messages[lastTool].Content, "[tool result elided") {
                old := chunking.EstimateMessagesTokens([]llm.Message{messages[lastTool]})
                others := total - old
                // v1.2.8.1: the reserve must cover the bounded-marker text
                // itself (~20 tok) plus estimator noise — the previous 12
                // left the bounded prompt ~10 tokens OVER the ceiling and
                // the strict in-loop fit gate caught it.
                room := ceiling - others - 64
                if room < 100 {
                        room = 100
                }

                content := messages[lastTool].Content
                keep := room * 4 // tokens ≈ runes/4 → chars ≈ tokens*4
                if keep >= len(content) {
                        return elided, savedTokens
                }
                // Cut on a rune boundary.
                for keep > 0 && !utf8.RuneStart(content[keep]) {
                        keep--
                }

                messages[lastTool].Content = fmt.Sprintf(
                        "[tool result elided under context pressure — bounded to the first ~%d of %d tokens]\n%s",
                        room, old, content[:keep])

                freed := old - chunking.EstimateMessagesTokens([]llm.Message{messages[lastTool]})
                total -= freed
                savedTokens += freed
                elided++
        }

        return elided, savedTokens
}

// sessionIDValue returns the current session id (lock-safe read).
func (o *Orchestrator) sessionIDValue() string {
        return o.currentSessionID()
}

// containsName reports whether the sorted name list contains name.
func containsName(list []string, name string) bool {
        for _, v := range list {
                if v == name {
                        return true
                }
        }
        return false
}

// recordTelemetry stores one turn record when telemetry is installed.
func (o *Orchestrator) recordTelemetry(rec ctxtelemetry.TurnRecord) {
        o.mu.Lock()
        t := o.telemetry
        o.mu.Unlock()

        if t == nil {
                return
        }
        t.Record(rec)
}

func toolList(
        used map[string]bool,
) []string {
        out := make(
                []string,
                0,
                len(used),
        )

        for name := range used {
                out = append(
                        out,
                        name,
                )
        }

        sort.Strings(out)

        return out
}

// hasAIContext reports whether the message list already opens with the
// SHEYTAN AI-context briefing (guard against double-prepending when a
// caller pre-assembles it).
func hasAIContext(
        messages []llm.Message,
) bool {
        for _, m := range messages {
                if m.Role != "system" {
                        return false // only system messages precede the conversation
                }

                if strings.Contains(
                        m.Content,
                        aicontext.HeaderSentinel,
                ) {
                        return true
                }
        }

        return false
}

func truncate(
        s string,
        n int,
) string {
        s = strings.ReplaceAll(
                s,
                "\n",
                " ",
        )

        if len(s) <= n {
                return s
        }

        return s[:n] + "..."
}

// contextPlanStatus derives the v1.2.5 status label for the per-turn
// context plan log. The contract: the status reflects what ACTUALLY
// happened during assembly — a record can never claim "compressed" when
// no compression happened. Priority: real reduction beats injection beats
// metadata-only adjustments.
func contextPlanStatus(tokensCompressed, elided, recalled, adjustments, sessionPolicy int) string {
        switch {
        case tokensCompressed > 0:
                return "compressed"
        case elided > 0:
                return "elided"
        case recalled > 0:
                return "recalled"
        case adjustments > 0:
                return "adjusted"
        case sessionPolicy > 0:
                return "session-policy"
        default:
                return "raw"
        }
}

// insertAfterSystemPrefix splices msg into the list right after the last
// LEADING system message (v1.2.8 summary position): the block joins the
// stable system prefix, so it survives history windowing and keeps the
// engine's prompt cache warm.
func insertAfterSystemPrefix(
        messages []llm.Message,
        msg llm.Message,
) []llm.Message {
        prefixLen := 0
        for prefixLen < len(messages) && messages[prefixLen].Role == "system" {
                prefixLen++
        }

        out := make([]llm.Message, 0, len(messages)+1)
        out = append(out, messages[:prefixLen]...)
        out = append(out, msg)
        out = append(out, messages[prefixLen:]...)
        return out
}

// planSectionIncluded reports whether Assemble kept the named section
// (Included flag — Tokens alone are also set for refused sections).
func planSectionIncluded(p contextplan.Plan, name string) bool {
        for i := range p.Sections {
                if p.Sections[i].Name == name {
                        return p.Sections[i].Included
                }
        }
        return false
}

// taskStepCaption renders the one-line caption for a `task` activity.
func taskStepCaption(t TaskState) string {
        if t.CurrentStep != "" {
                return t.CurrentStep
        }
        if t.Verification != "" {
                return "Verified: " + t.Verification
        }
        return "Working — task state updated"
}
