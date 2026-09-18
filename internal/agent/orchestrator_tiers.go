// orchestrator_tiers.go — v1.2.5 adaptive-turn machinery.
//
// This file implements the tier-driven composition the orchestrator's
// RunDetailed drives:
//
//      turnComposer  — holds the tier-scoped composition state and applies
//                      tier upgrades (escalation) mid-run without rebuilding
//                      the conversation from scratch;
//      escalationWatch — turns REAL run evidence (tool results, refused
//                      calls, verification outcome) into tier-escalation
//                      decisions with named reasons;
//      hardware snapshot — a TTL-cached measured RAM read feeding the tier
//                      decision (never a guess).
//
// The v1.2.4 behaviour — compose EVERYTHING, then degrade under overflow —
// is preserved as the degradation ladder safety net. What changes is the
// STARTING POINT: the first request now carries only P0 + the P1 the task
// signals actually need, and everything else arrives when evidence
// demands it.
package agent

import (
        "strings"
        "sync"
        "sync/atomic"
        "time"

        "github.com/Parsaetak/SHEYTAN-local-agent/internal/aicontext"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/chunking"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/hardware"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/llm"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/skills"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/taskclassify"
        "github.com/Parsaetak/SHEYTAN-local-agent/internal/toolsets"
)

// ThinkingControl values (composer → backend request).
const (
        ThinkingAuto     = "auto"
        ThinkingFast     = "fast"
        ThinkingThinking = "thinking"
)

// NormalizeThinkingControl validates a thinking control string.
func NormalizeThinkingControl(s string) string {
        switch strings.ToLower(strings.TrimSpace(s)) {
        case "fast":
                return ThinkingFast
        case "thinking", "deep":
                return ThinkingThinking
        default:
                return ThinkingAuto
        }
}

// ToolPolicyMode values.
const (
        ToolPolicyAuto   = "auto"
        ToolPolicyManual = "manual"
)

// ToolPolicy is the per-request manual tool control. In MANUAL mode only
// the listed tools may be offered AND executed — the orchestrator never
// silently re-enables a disabled tool, even when escalating context tiers.
type ToolPolicy struct {
        Mode    string
        Allowed []string
}

// NormalizeToolPolicyMode validates a policy mode string.
func NormalizeToolPolicyMode(s string) string {
        if strings.EqualFold(strings.TrimSpace(s), ToolPolicyManual) {
                return ToolPolicyManual
        }

        return ToolPolicyAuto
}

// allows reports whether a tool may run under the policy.
func (p ToolPolicy) allows(name string) bool {
        if p.Mode != ToolPolicyManual {
                return true
        }

        for _, a := range p.Allowed {
                if strings.EqualFold(a, name) {
                        return true
                }
        }

        return false
}

// ---------------------------------------------------------------------------
// hardware snapshot (TTL-cached measured values)
// ---------------------------------------------------------------------------

// hwSnapshot is a TTL cache around hardware.Collect so tier selection
// gets MEASURED memory figures without re-probing the OS every turn.
type hwSnapshot struct {
        mu       sync.Mutex
        profile  atomic.Pointer[hardware.Profile]
        fetched  time.Time
        interval time.Duration
}

var hwCache = &hwSnapshot{interval: 60 * time.Second}

// RAMInfo returns measured total/available system RAM in MB (0/0 when the
// probe yields nothing). Cached for one minute; probes never run on the
// per-token path.
func RAMInfo() (totalMB, availMB int) {
        hwCache.mu.Lock()
        now := time.Now()
        prof := hwCache.profile.Load()

        if prof == nil || now.Sub(hwCache.fetched) > hwCache.interval {
                hwCache.mu.Unlock()

                p := hardware.Collect(&config.Config{})
                hwCache.profile.Store(&p)

                hwCache.mu.Lock()
                hwCache.fetched = now
                prof = &p
        }

        hwCache.mu.Unlock()

        if prof == nil {
                return 0, 0
        }

        totalMB = int(prof.RAM.TotalBytes >> 20)
        if prof.RAM.AvailableBytes > 0 {
                availMB = int(prof.RAM.AvailableBytes >> 20)
        } else if prof.RAM.FreeBytes > 0 {
                availMB = int(prof.RAM.FreeBytes >> 20)
        }

        return totalMB, availMB
}

// ---------------------------------------------------------------------------
// turnComposer — tier-scoped composition state + upgrades
// ---------------------------------------------------------------------------

// turnComposer owns the tier-scoped composition of ONE agent turn.
type turnComposer struct {
        orch      *Orchestrator
        cfg       *config.Config
        effCtx    llm.EffectiveContext
        safety    int
        policy    ToolPolicy
        thinkMode string

        tier string
        spec taskclassify.TierSpec

        // Enabled tool names (config-gated), sorted.
        enabled []string

        // allNames is the offered surface (tier + policy selected).
        allNames []string

        // toolSpecs/toolTokens mirror allNames.
        toolSpecs []llm.ToolSpec
        toolTok   int

        // composed optional blocks
        card      string
        skillBlk  string
        recallBlk string
        cardOn    bool
        skillsOn  bool
        recallOn  bool

        // recallRetrievalMs is the measured recall-retrieval latency of the
        // composition (0 when the tier skipped recall entirely).
        recallRetrievalMs int64

        // briefing state
        usedCompact bool

        // escalated counts applied upgrades (bounded by maxEscalations).
        escalated int

        escalations []taskclassify.Escalation
}

const maxEscalations = 2

// newTurnComposer builds the composer at the STARTING tier and performs
// the tier-scoped composition immediately — FAST never pays for recall
// retrieval, project cards or skill matching at all.
func (o *Orchestrator) newTurnComposer(
        cfg *config.Config,
        effCtx llm.EffectiveContext,
        safety int,
        policy ToolPolicy,
        thinkMode string,
        tier string,
        task string,
        recaller Recaller,
        cardProvider func() string,
) *turnComposer {
        spec := taskclassify.Spec(tier)

        c := &turnComposer{
                orch:      o,
                cfg:       cfg,
                effCtx:    effCtx,
                safety:    safety,
                policy:    policy,
                thinkMode: thinkMode,
                tier:      tier,
                spec:      spec,
        }

        // Enabled tools.
        for name := range o.Tools() {
                if cfg.ToolEnabled(name) {
                        c.enabled = append(c.enabled, name)
                }
        }
        sortStrings(c.enabled)

        // Tier- and policy-scoped tool selection — from the START, not only
        // under overflow. Manual policy intersects; auto uses task signals.
        // v1.2.6: a ZERO-tool selection is a VALID outcome (pure conversation:
        // no capability signal → no schemas). The old `len(picked) > 0` guard
        // fell back to EVERY enabled tool — the exact over-injection the
        // contract prohibits.
        selected := c.enabled
        if policy.Mode == ToolPolicyManual {
                manual := make([]string, 0, len(policy.Allowed))
                for _, a := range c.Allowed() {
                        for _, e := range c.enabled {
                                if strings.EqualFold(a, e) {
                                        manual = append(manual, e)
                                        break
                                }
                        }
                }

                if len(manual) > 0 {
                        selected = manual
                }
        } else {
                selected = toolsets.SelectForTask(c.enabled, task, spec.MaxTools)
        }

        c.setTools(selected, task)

        // Optional composition — gated by the tier BEFORE any I/O happens.
        if spec.IncludeProjectCard && cardProvider != nil {
                c.card = cardProvider()
                c.cardOn = c.card != ""
        }

        if spec.IncludeSkills && o.skillSource != nil && task != "" {
                if matched := o.skillSource.MatchTask(task, 2); len(matched) > 0 {
                        c.skillBlk = renderSkills(matched, 400)
                        c.skillsOn = c.skillBlk != ""

                        for i := range matched {
                                o.skillSource.RecordUse(matched[i].Identity.ID)
                        }
                }
        }

        if spec.IncludeRecall && recaller != nil && cfg.RecallEnabled && task != "" {
                started := time.Now()
                c.recallBlk = recaller.RelevantBlock(task, cfg.EffectiveRecallTopK(), recallBlockTokenBudget)
                c.recallOn = c.recallBlk != ""
                c.recallRetrievalMs = time.Since(started).Milliseconds()
        }

        return c
}

// dropOptional clears every composed optional block (degradation ladder
// step 3 — the plan decided none of them travel this turn).
func (c *turnComposer) dropOptional() {
        c.card = ""
        c.skillBlk = ""
        c.recallBlk = ""
        c.cardOn = false
        c.skillsOn = false
        c.recallOn = false
}

// Allowed returns the manual-allowed list (normalized, deduplicated).
func (c *turnComposer) Allowed() []string {
        seen := map[string]bool{}
        out := make([]string, 0, len(c.policy.Allowed))

        for _, a := range c.policy.Allowed {
                a = strings.TrimSpace(a)
                if a == "" || seen[strings.ToLower(a)] {
                        continue
                }

                seen[strings.ToLower(a)] = true
                out = append(out, a)
        }

        return out
}

// setTools stores the offered surface, bounded by the tier's tool-token
// budget (drop non-core tools last-in-first until it fits).
func (c *turnComposer) setTools(names []string, task string) {
        tools := make([]Tool, 0, len(names))

        for _, n := range names {
                if t, ok := c.orch.tool(n); ok {
                        tools = append(tools, t)
                }
        }

        specs, tokens := c.orch.specs.BuildSpecs(tools)

        // MAX (budget 0) never trims; a manual policy never trims below the
        // user's explicit selection — the budget only trims AUTO selections.
        if c.spec.ToolTokenBudget > 0 && c.policy.Mode != ToolPolicyManual && tokens > c.spec.ToolTokenBudget {
                for len(tools) > 1 && tokens > c.spec.ToolTokenBudget {
                        // drop the least-important tool: non-core first, reverse
                        // alphabetical within the same class (stable + explainable)
                        idx := leastImportantToolIndex(tools)

                        _, t := c.orch.specs.Spec(tools[idx])
                        tokens -= t

                        tools = append(tools[:idx], tools[idx+1:]...)
                }

                specs, tokens = c.orch.specs.BuildSpecs(tools)
        }

        c.toolSpecs = specs
        c.toolTok = tokens

        out := make([]string, 0, len(tools))
        for _, t := range tools {
                out = append(out, t.Name())
        }

        c.allNames = out
}

// leastImportantToolIndex picks the tool to drop under budget pressure:
// core tools (files/shell/memory) are kept longest; among the rest the
// reverse-alphabetical last is dropped first (deterministic).
func leastImportantToolIndex(tools []Tool) int {
        core := func(name string) bool {
                for _, c := range toolsets.CoreTools {
                        if strings.EqualFold(c, name) {
                                return true
                        }
                }

                return false
        }

        best := -1

        for i, t := range tools {
                if core(t.Name()) {
                        continue
                }

                if best == -1 {
                        best = i
                        continue
                }

                if t.Name() > tools[best].Name() {
                        best = i
                }
        }

        if best >= 0 {
                return best
        }

        return len(tools) - 1 // all core: drop the last
}

// Briefing renders the system briefing for the current tier.
func (c *turnComposer) Briefing() (string, bool) {
        if c.spec.CompactBriefing {
                return aicontext.CompactSystemMessage(c.cfg, c.allNames), true
        }

        return aicontext.SystemMessageWithTools(c.cfg, c.allNames), false
}

// OptionalTokens is the composed optional-block token estimate.
func (c *turnComposer) OptionalTokens(staged int) int {
        return staged +
                tokensOrZero(c.card) +
                tokensOrZero(c.skillBlk) +
                tokensOrZero(c.recallBlk)
}

func tokensOrZero(s string) int {
        if s == "" {
                return 0
        }

        return chunking.EstimateTokens(s)
}

// Injectables returns the composed optional blocks in priority order
// (card → skills → recall) with their injected flags.
func (c *turnComposer) Injectables() (card, skills, recall string, cardOn, skillsOn, recallOn bool) {
        return c.card, c.skillBlk, c.recallBlk, c.cardOn, c.skillsOn, c.recallOn
}

// Escalate applies ONE tier upgrade (evidence-driven). It returns the
// enrichment that must be injected into the live conversation.
type upgrade struct {
        tier       string
        card       string
        skills     string
        recall     string
        fullBrief  string // non-empty → swap the compact briefing for the full one
        newNames   []string
        newSpecs   []llm.ToolSpec
        newToolTok int
        escalation taskclassify.Escalation
}

// Escalate upgrades the composer's tier, composing the blocks the new
// tier newly allows. Manual tool policy is NEVER widened by escalation.
// refusedTool (v1.2.6) re-enters the offered surface when the escalation
// evidence was a tool-surface refusal (auto policy only).
func (c *turnComposer) Escalate(reason taskclassify.EscalationReason, task, refusedTool string) (*upgrade, bool) {
        if c.escalated >= maxEscalations {
                return nil, false
        }

        next, ok := taskclassify.EscalateFrom(c.tier)
        if !ok {
                return nil, false
        }

        from := c.tier
        before := c.totalComposedTokens()

        c.tier = next
        c.spec = taskclassify.Spec(next)
        c.escalated++

        up := &upgrade{tier: next}

        // Briefing upgrade: compact → full.
        if c.usedCompact && !c.spec.CompactBriefing {
                up.fullBrief = aicontext.SystemMessageWithTools(c.cfg, c.allNames)
                c.usedCompact = false
        }

        // Newly-allowed optional blocks (compose lazily — exactly now).
        if c.spec.IncludeProjectCard && !c.cardOn && c.orch.projectCardProvider() != nil {
                c.card = c.orch.projectCardProvider()()
                c.cardOn = c.card != ""
                up.card = c.card
        }

        if c.spec.IncludeSkills && !c.skillsOn && c.orch.skillSource != nil && task != "" {
                if matched := c.orch.skillSource.MatchTask(task, 2); len(matched) > 0 {
                        c.skillBlk = renderSkills(matched, 400)
                        c.skillsOn = c.skillBlk != ""
                        up.skills = c.skillBlk

                        for i := range matched {
                                c.orch.skillSource.RecordUse(matched[i].Identity.ID)
                        }
                }
        }

        if c.spec.IncludeRecall && !c.recallOn && c.orch.recallProvider() != nil && c.cfg.RecallEnabled && task != "" {
                c.recallBlk = c.orch.recallProvider().RelevantBlock(task, c.cfg.EffectiveRecallTopK(), recallBlockTokenBudget)
                c.recallOn = c.recallBlk != ""
                up.recall = c.recallBlk
        }

        // Tool surface widening (auto policy only — manual never re-enables).
        // v1.2.6: the refused tool re-enters the surface FIRST (it is the
        // exact capability the run proved it needs), then the new tier's
        // task selection applies on top — union, deduplicated, bounded by
        // the new tier's MaxTools/token budget inside setTools.
        if c.policy.Mode != ToolPolicyManual {
                names := c.allNames

                if refusedTool != "" {
                        found := false

                        for _, n := range names {
                                if strings.EqualFold(n, refusedTool) {
                                        found = true
                                        break
                                }
                        }

                        if !found {
                                for _, e := range c.enabled {
                                        if strings.EqualFold(e, refusedTool) {
                                                names = append([]string{e}, names...)
                                                break
                                        }
                                }
                        }
                }

                if picked := toolsets.SelectForTask(c.enabled, task, c.spec.MaxTools); len(picked) > 0 {
                        seen := map[string]bool{}
                        merged := make([]string, 0, len(picked)+len(names))

                        for _, n := range names {
                                if n == "" || seen[strings.ToLower(n)] {
                                        continue
                                }
                                seen[strings.ToLower(n)] = true
                                merged = append(merged, n)
                        }

                        for _, n := range picked {
                                if n == "" || seen[strings.ToLower(n)] {
                                        continue
                                }
                                seen[strings.ToLower(n)] = true
                                merged = append(merged, n)
                        }

                        names = merged
                }

                if len(names) > len(c.allNames) {
                        c.setTools(names, task)
                } else if c.spec.ToolTokenBudget > 0 && c.toolTok > c.spec.ToolTokenBudget {
                        c.setTools(c.allNames, task)
                }

                up.newNames = c.allNames
                up.newSpecs = c.toolSpecs
                up.newToolTok = c.toolTok
        }

        up.escalation = taskclassify.Escalation{
                From:         from,
                To:           next,
                Reason:       reason,
                TokensBefore: before,
                TokensAfter:  c.totalComposedTokens(),
        }

        c.escalations = append(c.escalations, up.escalation)

        return up, true
}

// totalComposedTokens is the rough composed-context size (for the
// escalation log line).
func (c *turnComposer) totalComposedTokens() int {
        total := c.toolTok

        if c.cardOn {
                total += tokensOrZero(c.card)
        }

        if c.skillsOn {
                total += tokensOrZero(c.skillBlk)
        }

        if c.recallOn {
                total += tokensOrZero(c.recallBlk)
        }

        return total
}

// Escalations returns the recorded tier moves.
func (c *turnComposer) Escalations() []taskclassify.Escalation {
        return c.escalations
}

// Tier reports the current tier.
func (c *turnComposer) Tier() string {
        return c.tier
}

// HistoryShare reports the current tier's history share for the plan.
func (c *turnComposer) HistoryShare() float64 {
        return c.spec.HistoryShare
}

// ThinkingEnabled resolves the per-request thinking control against the
// global config: "fast" disables the nudge for THIS request (latency
// first); "thinking" enables it even when the global toggle is off.
func (c *turnComposer) ThinkingEnabled() bool {
        switch c.thinkMode {
        case ThinkingFast:
                return false
        case ThinkingThinking:
                return true
        default:
                return c.cfg.ThinkingMode
        }
}

// ---------------------------------------------------------------------------
// escalation evidence — REAL observations only
// ---------------------------------------------------------------------------

// escalationWatch accumulates run evidence and decides tier upgrades.
type escalationWatch struct {
        mu      sync.Mutex
        pending taskclassify.EscalationReason
        has     bool

        // refusedTool (v1.2.6) is the tool the model tried to call while it
        // was NOT part of the offered surface. The escalation re-offers
        // exactly this tool — without it, a pure-conversation task
        // (zero-signal → zero tools) could never recover from its own
        // refusal.
        refusedTool string
}

// note records one escalation signal; the FIRST signal of a turn wins
// (the escalation applies the full ladder step, which usually resolves
// the later signals too).
func (w *escalationWatch) note(reason taskclassify.EscalationReason) {
        w.mu.Lock()
        defer w.mu.Unlock()

        if !w.has {
                w.pending = reason
                w.has = true
        }
}

// take returns and clears the pending reason (plus the refused tool,
// when the evidence was a tool-surface refusal).
func (w *escalationWatch) take() (taskclassify.EscalationReason, string, bool) {
        w.mu.Lock()
        defer w.mu.Unlock()

        r := w.pending
        tool := w.refusedTool
        ok := w.has
        w.has = false
        w.pending = ""
        w.refusedTool = ""

        return r, tool, ok
}

// observeToolResult derives escalation evidence from one real tool
// outcome. Patterns are deliberately conservative — each one is a signal
// the CONTEXT was insufficient, not a generic failure.
func (w *escalationWatch) observeToolResult(tool, result string, failed bool) {
        if failed {
                return
        }

        lower := strings.ToLower(result)

        switch tool {
        case "files", "diff", "codeExec":
                // The model reached for a file/context that was never in the
                // prompt — the project card (repo map + conventions) is the cure.
                if containsAnyOf(lower, "file not found", "no such file", "does not exist", "cannot find") {
                        w.note(taskclassify.ReasonMissingFileContext)
                }
        }

        // Project-wide asks: the answer references the repository itself.
        if containsAnyOf(lower, "the codebase", "the whole project", "the repository structure", "all files") {
                w.note(taskclassify.ReasonRepositoryDependency)
        }
}

// observeRefusal records the orchestrator's own tool-surface refusal —
// the model tried a tool that was not offered this turn. The refused
// tool's name is kept so the escalation can offer exactly what was
// missing (v1.2.6).
func (w *escalationWatch) observeRefusal(tool string) {
        w.mu.Lock()
        defer w.mu.Unlock()

        if !w.has {
                w.pending = taskclassify.ReasonMissingToolContext
                w.has = true
                w.refusedTool = tool
        } else if w.refusedTool == "" {
                w.refusedTool = tool
        }
}

// observeVerification records a failed objective verification with a
// recoverable shape (build/test evidence the deeper tiers can repair).
func (w *escalationWatch) observeVerification(outcome string) {
        if strings.Contains(outcome, "failed") {
                w.note(taskclassify.ReasonVerificationFailure)
        }
}

// observeAttachmentTruncation records that staged attachment content was
// dropped under the tier's budget.
func (w *escalationWatch) observeAttachmentTruncation() {
        w.note(taskclassify.ReasonLargeAttachment)
}

// observeVisionPayload records mid-run vision content (a tool produced
// images the tier did not plan for).
func (w *escalationWatch) observeVisionPayload() {
        w.note(taskclassify.ReasonVisionRequirement)
}

func containsAnyOf(s string, subs ...string) bool {
        for _, sub := range subs {
                if strings.Contains(s, sub) {
                        return true
                }
        }

        return false
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func sortStrings(s []string) {
        for i := 1; i < len(s); i++ {
                for j := i; j > 0 && s[j] < s[j-1]; j-- {
                        s[j], s[j-1] = s[j-1], s[j]
                }
        }
}

// visionPayloadTokens estimates the image payload riding the request.
// Per-image cost is the documented mmproj planning figure (an ESTIMATE
// used only for tier selection — the plan's reported tokens remain
// measured text tokens).
const perImageTokenEstimate = 800

func visionPayloadTokens(messages []llm.Message) int {
        n := 0

        for i := range messages {
                n += len(messages[i].Images)
        }

        return n * perImageTokenEstimate
}

// countStagedBlocks measures the attachment blocks already injected as
// system messages by the API layer (identified by their stable header).
const stagedAttachmentHeader = "[staged attachments relevant to this turn]"

func countStagedBlocks(messages []llm.Message) (blocks int, tokens int) {
        for i := range messages {
                if messages[i].Role == "system" && strings.Contains(messages[i].Content, stagedAttachmentHeader) {
                        blocks++
                        tokens += chunking.EstimateTokens(messages[i].Content)
                }
        }

        return blocks, tokens
}

// countImageMessages reports whether images ride the request and how
// many attachments the conversation carries.
func requestFacts(messages []llm.Message) (hasImages bool, imageCount, attachmentBlocks int) {
        for i := range messages {
                if len(messages[i].Images) > 0 {
                        hasImages = true
                        imageCount += len(messages[i].Images)
                }

                if messages[i].Attachments != nil {
                        attachmentBlocks += len(messages[i].Attachments)
                }
        }

        return hasImages, imageCount, attachmentBlocks
}

// renderSkills delegates to the skills package (kept local to avoid an
// import cycle in the composer file).
func renderSkills(matched []skills.Skill, budget int) string {
        return skills.RenderBlock(matched, budget)
}

// The orchestrator accessor shims below keep the composer decoupled from
// the orchestrator's private fields while reading under the right locks.

func (o *Orchestrator) projectCardProvider() func() string {
        o.mu.Lock()
        defer o.mu.Unlock()
        return o.projectCard
}

func (o *Orchestrator) recallProvider() Recaller {
        o.mu.Lock()
        defer o.mu.Unlock()
        return o.recaller
}

