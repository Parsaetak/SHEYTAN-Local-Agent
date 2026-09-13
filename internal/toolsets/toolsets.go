// Package toolsets implements Phase 7A dynamic toolsets: capability groups
// that let the agent receive ONLY the tools relevant to the active task.
//
// Every tool schema costs context tokens — on a small local model the full
// registry can crowd out the very conversation it should serve. Toolsets
// make the tool surface a budgeted decision: the orchestrator measures the
// full registry first; when it exceeds the tool budget or the context plan
// overflows, it selects the smallest task-relevant set instead. Selection
// is deterministic (pure functions, no I/O) and always logs what was
// included and why, so the behavior stays auditable.
package toolsets

import (
	"sort"
	"strings"
)

// Group names — the stable capability taxonomy.
const (
	GroupCoding       = "coding"
	GroupResearch     = "research"
	GroupVerification = "verification"
	GroupFilesystem   = "filesystem"
	GroupGit          = "git"
	GroupBrowser      = "browser"
	GroupComputer     = "computer"
	GroupSystem       = "system"
	GroupData         = "data"
	GroupMemory       = "memory"
)

// ToolGroups maps every registered SHEYTAN tool to its capability groups.
// A tool may belong to several groups; group membership is the single
// authority for dynamic selection.
var ToolGroups = map[string][]string{
	"shell":        {GroupSystem, GroupCoding, GroupVerification},
	"files":        {GroupFilesystem, GroupCoding},
	"codeExec":     {GroupCoding, GroupVerification},
	"webSearch":    {GroupResearch},
	"git":          {GroupGit, GroupCoding},
	"browser":      {GroupBrowser, GroupComputer},
	"dataAnalysis": {GroupData},
	"json":         {GroupData},
	"archive":      {GroupFilesystem},
	"fetch":        {GroupResearch},
	"diff":         {GroupCoding, GroupVerification},
	"screenshot":   {GroupComputer},
	"linux":        {GroupSystem},
	"coding_lab":   {GroupVerification, GroupCoding},
	"research":     {GroupResearch},
	"memory":       {GroupMemory, GroupSystem},
	"pipeline":     {GroupVerification, GroupCoding},
}

// CoreTools are always offered regardless of the task: the agent loop
// cannot read project state or persist facts without them.
var CoreTools = []string{"files", "shell", "memory"}

// TaskSignals maps task keywords to capability groups. Scoring is additive
// across matching signals.
var TaskSignals = []struct {
	Keywords []string
	Group    string
}{
	{[]string{"build", "compile", "fix", "refactor", "implement", "code", "function", "bug", "error", "write", "program"}, GroupCoding},
	{[]string{"test", "verify", "prove", "validate", "check", "benchmark"}, GroupVerification},
	{[]string{"search", "research", "find online", "web", "internet", "look up", "latest", "news"}, GroupResearch},
	{[]string{"git", "commit", "branch", "merge", "rebase", "diff", "pull request"}, GroupGit},
	{[]string{"browser", "website", "url", "page", "click", "navigate", "web page"}, GroupBrowser},
	{[]string{"screenshot", "screen", "desktop", "window"}, GroupComputer},
	{[]string{"csv", "json", "data", "statistics", "chart", "plot", "analyze"}, GroupData},
	{[]string{"file", "directory", "folder", "move", "copy", "rename", "archive", "zip"}, GroupFilesystem},
	{[]string{"remember", "recall", "memory", "preference"}, GroupMemory},
}

// SelectForTask chooses the tool subset for a task: core tools plus every
// group the task text signals, bounded by maxTools. Deterministic: equal
// candidates resolve alphabetically.
func SelectForTask(available []string, task string, maxTools int) []string {
	taskLower := strings.ToLower(task)

	groups := map[string]bool{}
	for _, sig := range TaskSignals {
		for _, kw := range sig.Keywords {
			if strings.Contains(taskLower, kw) {
				groups[sig.Group] = true
				break
			}
		}
	}

	// Any task that builds or touches git implies verification.
	if groups[GroupCoding] || groups[GroupGit] {
		groups[GroupVerification] = true
	}

	chosen := map[string]bool{}
	for _, name := range CoreTools {
		if contains(available, name) {
			chosen[name] = true
		}
	}

	for _, name := range available {
		if chosen[name] {
			continue
		}
		for _, g := range ToolGroups[name] {
			if groups[g] {
				chosen[name] = true
				break
			}
		}
	}

	out := make([]string, 0, len(chosen))
	for name := range chosen {
		out = append(out, name)
	}
	sort.Strings(out)

	if maxTools > 0 && len(out) > maxTools {
		// Keep core tools first, then alphabetical order — a stable,
		// explainable cut.
		kept := make([]string, 0, maxTools)
		for _, name := range CoreTools {
			if len(kept) == maxTools {
				break
			}
			if contains(out, name) {
				kept = append(kept, name)
			}
		}
		for _, name := range out {
			if len(kept) == maxTools {
				break
			}
			if !contains(kept, name) {
				kept = append(kept, name)
			}
		}
		out = kept
	}

	return out
}

// NamesForGroup returns the sorted tools of one group within available.
func NamesForGroup(available []string, group string) []string {
	var out []string
	for _, name := range available {
		if contains(ToolGroups[name], group) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
