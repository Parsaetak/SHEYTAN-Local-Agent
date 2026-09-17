// run-events.ts — v1.2.5 wire-event vocabulary mapping (PURE module).
//
// The backend activity stream carries BOTH the legacy event names
// (response / reasoning / done / tool_start / tool_end / verification /
// error / context / perf / failure / session) and the v1.2.5 additions:
//
//	thinking_start / thinking_delta / thinking_end
//	assistant_delta
//	complete
//	status            (live status chip telemetry: phase · tier · tokens)
//	escalation        (tier move with reason, e.g.
//	                   "FAST 3.8K → STANDARD 7.2K reason=MissingFileContext")
//
// normalizeEventKind maps every wire name onto the canonical kinds the
// store consumes. Legacy names keep their meaning; the new names are
// ALIASES of the same semantics (never a second copy of streamed content
// — the backend emits thinking_start/end as contentless markers and the
// reasoning/response events carry the cumulative text as before).
//
// Pure functions only — unit-testable without a store or browser API.

export type CanonicalEventKind =
  | "assistant_delta" // streamed answer text (cumulative snapshot)
  | "thinking_delta" // streamed reasoning text (cumulative snapshot)
  | "thinking_open" // reasoning began — open the panel
  | "thinking_close" // reasoning finished — fold the panel
  | "tool_start"
  | "tool_end"
  | "verification"
  | "status" // live status telemetry (no content)
  | "escalation" // tier move notice (no content)
  | "progress" // legacy "thinking" progress captions (iterations, offline notes)
  | "context" // context provenance report
  | "perf"
  | "failure"
  | "plan"
  | "session"
  | "idle" // socket has no run attached (recovery signal)
  | "done" // run finished (legacy name)
  | "complete" // run finished (v1.2.5 name)
  | "error"
  | "unknown";

const KIND_MAP: Record<string, CanonicalEventKind> = {
  // streamed content (cumulative snapshots — never deltas to append)
  response: "assistant_delta",
  assistant_delta: "assistant_delta",

  reasoning: "thinking_delta",
  thinking_delta: "thinking_delta",

  // contentless thinking markers
  thinking_start: "thinking_open",
  thinking_end: "thinking_close",

  // tool lifecycle
  tool_start: "tool_start",
  tool_end: "tool_end",

  // verification + run-end
  verification: "verification",
  done: "done",
  complete: "complete",

  // statuses
  thinking: "progress", // legacy caption events keep their meaning
  status: "status",
  escalation: "escalation",
  context: "context",
  perf: "perf",
  failure: "failure",
  plan: "plan",
  session: "session",
  idle: "idle",

  error: "error",
};

// normalizeEventKind maps a wire event type to its canonical kind.
export function normalizeEventKind(type: string): CanonicalEventKind {
  return KIND_MAP[type] ?? "unknown";
}

// isRunEvidence reports whether an event kind proves a run is live (the
// watchdog that recovers stuck composers trusts only these).
export function isRunEvidence(kind: CanonicalEventKind): boolean {
  return (
    kind === "assistant_delta" ||
    kind === "thinking_delta" ||
    kind === "thinking_open" ||
    kind === "thinking_close" ||
    kind === "tool_start" ||
    kind === "tool_end" ||
    kind === "done" ||
    kind === "complete" ||
    kind === "status" ||
    kind === "escalation"
  );
}

// ThinkingControl is the composer's per-request depth control. It is sent
// with every run request and changes the actual backend behaviour.
export type ThinkingControl = "auto" | "fast" | "thinking";

export const THINKING_OPTIONS: {
  value: ThinkingControl;
  label: string;
  hint: string;
}[] = [
  {
    value: "auto",
    label: "Auto",
    hint: "Adaptive: the agent picks the context tier from the task",
  },
  {
    value: "fast",
    label: "Fast",
    hint: "Latency first: smallest context, no reasoning overhead",
  },
  {
    value: "thinking",
    label: "Thinking",
    hint: "Depth first: reasoning enabled, deeper context escalation",
  },
];

// normalizeThinkingControl validates a persisted control value.
export function normalizeThinkingControl(v: unknown): ThinkingControl {
  return v === "fast" || v === "thinking" ? v : "auto";
}

// ToolPolicyMode is the composer's per-request tool control.
export type ToolPolicyMode = "auto" | "manual";

export function normalizeToolPolicyMode(v: unknown): ToolPolicyMode {
  return v === "manual" ? "manual" : "auto";
}

// normalizeToolAllowlist keeps the manual allow-list honest: unique,
// trimmed, non-empty names.
export function normalizeToolAllowlist(list: unknown): string[] {
  if (!Array.isArray(list)) {
    return [];
  }

  const seen = new Set<string>();
  const out: string[] = [];

  for (const item of list) {
    if (typeof item !== "string") {
      continue;
    }

    const name = item.trim();
    if (name === "") {
      continue;
    }

    const key = name.toLowerCase();
    if (seen.has(key)) {
      continue;
    }

    seen.add(key);
    out.push(name);
  }

  return out;
}
