// handoff.go — v1.2.8 mandatory Agent.md handoff.
//
// The repository already carries agent.md as the canonical engineering
// handoff document (lowercase — Windows filesystems may treat agent.md and
// Agent.md as the SAME path, so the exact filename `agent.md` is a
// constant here and must never be "normalized" into another case).
//
// At the end of every completed Agent task that produced engineering
// evidence, the runtime updates the workspace's agent.md with a clearly
// separated DYNAMIC section:
//
//      <!-- sheytan:handoff:begin -->
//      # Latest Agent Handoff
//      ...
//      <!-- sheytan:handoff:end -->
//
// Everything OUTSIDE the marker block is preserved byte-for-byte: stable
// engineering instructions the file already carries are never rewritten by
// the runtime. The new section replaces any previous one (the latest
// handoff is the authoritative one) and is written only AFTER the actual
// task outcome is known — factual, bounded, no private chain-of-thought,
// no speculative success claims, no fabricated metrics.
package agent

import (
        "fmt"
        "os"
        "strings"
)

// HandoffFileName is the ONLY filename the handoff ever writes. Lowercase
// by contract (see package comment).
const HandoffFileName = "agent.md"

// Marker pair bounding the dynamic section inside agent.md.
const (
        HandoffBeginMarker = "<!-- sheytan:handoff:begin -->"
        HandoffEndMarker   = "<!-- sheytan:handoff:end -->"
)

// Handoff is the factual payload rendered into the dynamic section.
type Handoff struct {
        Task         string   // what was run
        Objective    string   // the goal
        CurrentState string   // where the work stands
        ChangesMade  []string // concrete changes
        FilesChanged []string // exact paths
        TestsAndVer  []string // tests run + verification verdict
        Evidence     []string // important evidence (outputs, ids, paths)
        Failures     []string // unresolved failures / blockers
        Remaining    []string // remaining work
        NextAction   string   // recommended next action
        DoNotRedo    []string // settled work the next agent must not repeat
}

// RenderHandoff renders the bounded markdown section between the markers.
// Machine-readable enough for another model to parse: stable `## Heading`
// blocks, `- key: value` bullets, exact values preserved verbatim.
func RenderHandoff(h Handoff) string {
        var b strings.Builder

        b.WriteString(HandoffBeginMarker + "\n")
        b.WriteString("# Latest Agent Handoff\n\n")

        writeSection := func(heading, body string) {
                if strings.TrimSpace(body) == "" {
                        return
                }
                fmt.Fprintf(&b, "## %s\n%s\n\n", heading, strings.TrimSpace(body))
        }
        writeList := func(heading string, items []string) {
                if len(items) == 0 {
                        return
                }
                fmt.Fprintf(&b, "## %s\n", heading)
                for _, item := range items {
                        if strings.TrimSpace(item) == "" {
                                continue
                        }
                        b.WriteString("- " + strings.TrimSpace(item) + "\n")
                }
                b.WriteString("\n")
        }

        writeSection("Task", h.Task)
        writeSection("Objective", h.Objective)
        writeSection("Current state", h.CurrentState)
        writeList("Changes made", h.ChangesMade)
        writeList("Files changed", h.FilesChanged)
        writeList("Tests and verification", h.TestsAndVer)
        writeList("Important evidence", h.Evidence)
        writeList("Failures / blockers", h.Failures)
        writeList("Remaining work", h.Remaining)
        writeSection("Recommended next action", h.NextAction)
        writeList("Do not redo", h.DoNotRedo)

        b.WriteString(HandoffEndMarker + "\n")

        return b.String()
}

// WriteHandoffFile updates path (an agent.md) with the rendered handoff:
//   - file absent      → created with a minimal stable header + the section;
//   - file without the section → section appended at the end;
//   - file with the section → ONLY the section is replaced; everything
//     outside the markers is preserved byte-for-byte.
//
// The write is atomic (tmp + rename) like every other durable artifact.
func WriteHandoffFile(path string, h Handoff) error {
        if strings.TrimSpace(path) == "" {
                return fmt.Errorf("handoff: empty path")
        }

        var existing string
        if data, err := os.ReadFile(path); err == nil {
                existing = string(data)
        }

        updated := spliceHandoff(existing, RenderHandoff(h))

        tmp := path + ".tmp"
        if err := os.WriteFile(tmp, []byte(updated), 0o644); err != nil {
                return err
        }
        return os.Rename(tmp, path)
}

// spliceHandoff replaces the marker-bounded section in existing (or
// appends it) while preserving everything outside the markers.
func spliceHandoff(existing, section string) string {
        begin := strings.Index(existing, HandoffBeginMarker)
        end := strings.Index(existing, HandoffEndMarker)

        if begin >= 0 && end > begin {
                before := existing[:begin]
                after := existing[end+len(HandoffEndMarker):]
                after = strings.TrimLeft(after, "\n")
                return strings.TrimRight(before, "\n") + "\n\n" + section + "\n" + after
        }

        // No COMPLETE section present. A dangling begin marker without an
        // end marker (a torn write) is STRIPPED — the fresh section
        // supersedes it, so exactly one marker pair ever remains.
        if begin >= 0 {
                existing = existing[:begin]
        }

        if strings.TrimSpace(existing) == "" {
                return handoffFileHeader() + "\n" + section
        }

        return strings.TrimRight(existing, "\n") + "\n\n" + section
}

func handoffFileHeader() string {
        return "# Agent Handoff\n\n> Dynamic next-agent handoff maintained by SHEYTAN agent runs.\n" +
                "> The bounded `Latest Agent Handoff` section below is replaced after\n" +
                "> every completed engineering task; content outside it is preserved.\n"
}

// HandoffFromTaskState builds the factual payload from the run's task
// state + verification verdict + the persistence facts the API layer
// observed. Values are passed through verbatim — no re-derivation, no
// fabrication.
func HandoffFromTaskState(t TaskState, verificationSummary, outcome string) Handoff {
        h := Handoff{
                Task:         t.Goal,
                Objective:    t.Goal,
                CurrentState: t.CurrentStep,
                FilesChanged: t.FilesChanged,
                Failures:     t.Failures,
                Remaining:    t.OpenQuestions,
        }
        if t.NextStep != "" {
                h.NextAction = t.NextStep
        }
        if len(t.CommandsRun) > 0 {
                h.TestsAndVer = append(h.TestsAndVer, "commands run:")
                for _, c := range t.CommandsRun {
                        h.TestsAndVer = append(h.TestsAndVer, "  - "+c)
                }
        }
        if len(t.TestsRun) > 0 {
                h.TestsAndVer = append(h.TestsAndVer, "tests run:")
                for _, c := range t.TestsRun {
                        h.TestsAndVer = append(h.TestsAndVer, "  - "+c)
                }
        }
        if verificationSummary != "" {
                h.TestsAndVer = append(h.TestsAndVer, "verification: "+verificationSummary)
        }
        if outcome != "" {
                h.ChangesMade = append([]string{"run outcome: " + outcome}, h.ChangesMade...)
        }
        if len(t.Repairs) > 0 {
                h.ChangesMade = append(h.ChangesMade, t.Repairs...)
        }
        // "Do not redo": the settled artifacts (files already written, tests
        // already passing) — the next agent must not repeat completed work.
        h.DoNotRedo = append(h.DoNotRedo, t.FilesChanged...)
        h.DoNotRedo = append(h.DoNotRedo, t.Artifacts...)
        return h
}
