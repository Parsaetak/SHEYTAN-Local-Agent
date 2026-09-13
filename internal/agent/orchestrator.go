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
        "strings"
        "sync"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/aicontext"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/chunking"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/contextplan"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/continuum"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/logging"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/netcheck"
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

        // Verification (v1.1.5Z Phase 6): the run-level verification verdict
        // — verified / partially_verified / failed / not_verified — computed
        // from the objective evidence the run's tool traffic actually
        // produced (lab verify actions, build/test outcomes). "The model
        // said so" is not proof; this field is the honest record.
        Verification VerificationReport

        // LoopStats (v1.1.5Z Phase 6): per-tool call counts (total and
        // distinct arguments) for the run report — makes repetitive
        // behaviour visible after the fact.
        LoopStats []StatsSnapshot

        // FailureTally (v1.1.5Z Phase 6): how many tool failures were
        // classified, by category — the reliability HUD for the timeline.
        FailureTally map[FailureCategory]int
}

// abortCaption renders the correct end caption for a canceled context:
// a user abort and the v1.1.4Z per-run time budget are different events
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
// (v1.1.5Z Phase 6). When set and non-empty, its block is injected as a
// system message immediately before the last user message — the same
// cache-friendly position as the recall block — so the model starts the
// task already knowing the measured facts of the current project
// (languages, verified build/test commands, past lessons). Nil disables.
func (o *Orchestrator) SetProjectCard(provider func() string) {
        o.mu.Lock()
        o.projectCard = provider
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

        // genMu guards the router swap (set once at wiring; also readable
        // under race detector in tests).
        genMu sync.RWMutex
        gen   GenerationStream

        mu        sync.Mutex
        sessionID string
        recaller  Recaller
        projectCard func() string
}

func New(src *config.Source, client *llm.Client) *Orchestrator {
        return &Orchestrator{
                src:    src,
                client: client,
                tools:  make(map[string]Tool),
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
        o.toolsMu.Lock()
        o.tools[t.Name()] = t
        o.toolsMu.Unlock()
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
) (RunResult, error) {
        o.mu.Lock()
        recaller := o.recaller
        cardProvider := o.projectCard
        o.mu.Unlock()

        // v1.1.4Z: one consistent config snapshot per run. The previous code
        // read the shared mutable Config throughout the loop — a Settings PATCH
        // mid-run produced a data race and could flip sampling/tool policy
        // between two iterations of the SAME turn. Changes now apply cleanly
        // from the next run on.
        cfg := o.src.Load()

        result := RunResult{}

        // v1.1.5Z Phase 6 reliability core: one loop guard (repeat detection,
        // tool-call/wall-clock budgets) and one evidence collector (objective
        // verification record) per run. Both are local to the run — nothing
        // accumulates across runs.
        guard := NewLoopGuard()
        evidence := NewEvidenceCollector()
        failTally := map[FailureCategory]int{}

        // v1.0.1: every conversation now starts with the SHEYTAN AI-context
        // briefing (AI-CONTEXT.md + live environment) as system message #1, so
        // ANY plugged-in model knows where it runs, what tools exist and how to
        // call them. Skipped when the caller already included it.
        if !hasAIContext(messages) {
                registeredToolNames := make(
                        []string,
                        0,
                        len(o.tools),
                )

                for name := range o.Tools() {
                        if cfg.ToolEnabled(name) {
                                registeredToolNames = append(
                                        registeredToolNames,
                                        name,
                                )
                        }
                }

                ctxContent := aicontext.SystemMessageWithTools(
                        cfg,
                        registeredToolNames,
                )

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
        }

        // v1.0.2 thinking mode: append the <think> nudge to the AI-context
        // message (stable position — the prefix only changes when the user
        // toggles the mode, which is rare).
        if cfg.ThinkingMode {
                messages = ensureThinkingNudge(messages)
        }

        // v1.1.5Z Phase 6 persistent project intelligence: inject the
        // measured project card (languages, verified build/test commands,
        // lessons) before the fresh turn — the model starts the task with
        // the project's facts instead of re-discovering them. Bounded by
        // the card renderer itself (≤ 4KB), so no plan change is needed.
        if cardProvider != nil {
                if card := cardProvider(); card != "" {
                        messages = insertBeforeLastUser(
                                messages,
                                llm.Message{
                                        Role:    "system",
                                        Content: card,
                                },
                        )
                }
        }

        // v1.0.2 persistent recall: pull the most relevant past-exchange digests
        // for the incoming question and inject them as a bounded block right
        // BEFORE the last user message — after all earlier history (so the
        // llama.cpp KV cache still covers the stable prefix), before the fresh
        // turn.
        if recaller != nil && cfg.RecallEnabled {
                if q := lastUserQuery(messages); q != "" {
                        block := recaller.RelevantBlock(
                                q,
                                cfg.EffectiveRecallTopK(),
                                recallBlockTokenBudget,
                        )

                        if block != "" {
                                messages = insertBeforeLastUser(
                                        messages,
                                        llm.Message{
                                                Role:    "system",
                                                Content: block,
                                        },
                                )

                                result.Recalled = strings.Count(
                                        block,
                                        "user asked:",
                                )

                                onActivity(Activity{
                                        Type: "thinking",
                                        Caption: fmt.Sprintf(
                                                "Recalled %d relevant past exchange(s) from memory",
                                                result.Recalled,
                                        ),
                                        Timestamp: time.Now(),
                                })
                        }
                }
        }

        // v1.0.2 history window: compact older messages so the prompt stays
        // inside a bounded share of num_ctx (leading system messages survive).
        //
        // v1.1.3Z long-context engine: the window budget is no longer a raw
        // config share — it is the EXPLICIT plan allocation computed from the
        // real model context: system briefing + MEASURED tool schemas +
        // recall/attachment blocks + reserved output tokens.
        //
        // Tool specs are built BEFORE windowing so their exact serialized
        // cost is part of the plan (the old per-tool guess under-counted by
        // kilotokens and let real llama.cpp reject oversized requests).
        names := make([]string, 0, len(o.tools))

        for name := range o.tools {
                if cfg.ToolEnabled(name) {
                        names = append(names, name)
                }
        }

        sort.Strings(names)

        toolSpecs := make(
                []llm.ToolSpec,
                0,
                len(names),
        )

        for _, name := range names {
                t, ok := o.tool(name)
                if !ok {
                        continue
                }

                spec := llm.ToolSpec{}
                spec.Type = "function"
                spec.Function.Name = t.Name()
                spec.Function.Description = t.Description()
                spec.Function.Parameters = t.Parameters()

                toolSpecs = append(
                        toolSpecs,
                        spec,
                )
        }

        toolTokens := estimateToolSpecsTokens(toolSpecs)

        sysTokens, injectedTokens, injectedBlocks := classifyMessages(messages)

        plan := contextplan.Assemble(contextplan.Input{
                SystemTokens:    sysTokens,
                ToolTokens:      toolTokens,
                RecallTokens:    injectedTokens,
                NumCtx:          cfg.LLM.NumCtx,
                MaxOutputTokens: cfg.LLM.MaxTokens,
        })

        plan.Recalled = result.Recalled
        plan.Attachments = injectedBlocks

        prefix, body := splitSystemPrefix(messages)

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

        // Measure the actual windowed history so the report reflects the
        // real prompt, not the allocation.
        plan.SetSectionTokens(
                contextplan.SectionHistory,
                chunking.EstimateMessagesTokens(messages),
        )

        // v1.1.5Z Phase 3: record the MEASURED prompt size (bytes actually
        // carried by the assembled messages) on the plan — the context report
        // now carries a real byte figure alongside the token estimates.
        plan.SetPromptBytes(int64(measureMessagesBytes(messages)))

        logging.Default().Info(
                "agent",
                "context metrics: promptBytes=%d estTokens=%d pressure=%.2f elided=%d recalled=%d attachments=%d",
                plan.PromptBytes,
                plan.TotalTokens(),
                plan.Pressure(),
                plan.Elided,
                plan.Recalled,
                plan.Attachments,
        )

        // v1.1.3Z: publish the context provenance report once per turn so
        // the UI can show the real budget split without exposing prompts.
        onActivity(Activity{
                Type:      "context",
                Caption:   plan.Summary(),
                Detail:    plan,
                Timestamp: time.Now(),
        })

        // Fixed sections alone overflowing the usable window must be VISIBLE:
        // the engine will reject the request, so say why up front.
        if overflow := plan.TotalTokens() - plan.Budget.Usable; overflow > 0 {
                logging.Default().Warn(
                        "agent",
                        "context overflow: fixed sections exceed usable window by ~%d tok — raise numCtx",
                        overflow,
                )

                onActivity(Activity{
                        Type: "error",
                        Caption: fmt.Sprintf(
                                "Context overflow: system + tool definitions need ~%d tokens but only %d fit (numCtx %d). Raise the context size in Settings → Model.",
                                plan.TotalTokens(),
                                plan.Budget.Usable,
                                plan.Budget.Total,
                        ),
                        Timestamp: time.Now(),
                })
        }

        maxIter := cfg.MaxIterations

        if maxIter < 1 {
                maxIter = 25
        }

        toolsUsed := map[string]bool{}

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

                req := o.client.BuildChatRequest(
                        cfg.EffectiveModel(),
                        messages,
                        toolSpecs,
                )

                if u := continuum.EstimateUsage(
                        messages,
                        budgetTokens,
                ); u.EstTokens > peakUsage.EstTokens {
                        peakUsage = u
                }

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
                                onActivity(Activity{
                                        Type:      "reasoning",
                                        Caption:   r,
                                        Timestamp: time.Now(),
                                })
                        }

                        if content != "" {
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
                                if ev.Content != "" {
                                        raw.WriteString(ev.Content)
                                        emitProgress(false)
                                }

                                if ev.Reasoning != "" {
                                        native.WriteString(ev.Reasoning)
                                        emitProgress(false)
                                }

                                if len(ev.ToolCalls) > 0 {
                                        lastToolCalls = ev.ToolCalls
                                }

                                return nil
                        },
                )

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

                        // v1.1.4Z: ShowPerfHUD was a stored setting no
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

                        // v1.1.5Z Phase 6: the run's verification verdict —
                        // computed ONLY from objective evidence the tool
                        // traffic produced. A completion claim with no
                        // evidence reports not_verified, so the UI (and the
                        // multi-agent critic) can demand verification instead
                        // of celebrating an unproven "done".
                        result.Verification = evidence.Report()
                        result.LoopStats = guard.CallStats()
                        result.FailureTally = failTally

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

                        onActivity(Activity{
                                Type:      "done",
                                Caption:   doneCaption,
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

                        tool, ok := o.tool(tc.Function.Name)

                        var result2 string

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
                                                names,
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
                                                names,
                                                ", ",
                                        ),
                                )
                        }

                        if result2 != "" {
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

                        // v1.1.5Z Phase 6 loop guard: observe the call BEFORE
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
                                onActivity(Activity{
                                        Type: "tool_end",
                                        Caption: fmt.Sprintf(
                                                "Tool %s refused by loop guard",
                                                tc.Function.Name,
                                        ),
                                        Detail:    obs.Block,
                                        Timestamp: time.Now(),
                                })

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

                        result2, err := tool.Run(
                                ctx,
                                json.RawMessage(
                                        tc.Function.Arguments,
                                ),
                        )

                        dur := time.Since(start)

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

                        // v1.1.5Z Phase 6: record the outcome for repeat
                        // detection and collect verification evidence (lab
                        // verify actions, build/test outcomes).
                        rawOutput := result2

                        guard.Record(
                                tc.Function.Name,
                                tc.Function.Arguments,
                                result2,
                        )

                        evidence.ObserveToolResult(
                                tc.Function.Name,
                                tc.Function.Arguments,
                                result2,
                                err != nil,
                        )

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

                                // v1.1.5Z Phase 6 failure classification:
                                // diagnose WHY the call failed and append the
                                // category's repair hint so the model re-plans
                                // against the diagnosis instead of retrying
                                // blind. The category is also surfaced in the
                                // activity timeline and tallied on the result.
                                cat := ClassifyFailure(ToolFailure{
                                        Tool:    tc.Function.Name,
                                        Args:    tc.Function.Arguments,
                                        Err:     err.Error(),
                                        Output:  rawOutput,
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
        }

        result.ToolsUsed = toolList(toolsUsed)
        result.ContextUsage = peakUsage
        result.Verification = evidence.Report()
        result.LoopStats = guard.CallStats()
        result.FailureTally = failTally

        onActivity(Activity{
                Type:      "verification",
                Caption:   result.Verification.Summary(),
                Detail:    result.Verification,
                Timestamp: time.Now(),
        })

        onActivity(Activity{
                Type:      "done",
                Caption:   "Max iterations reached",
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

// historyBudgetFor was removed in v1.1.3Z: the context plan (contextplan
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
