import { create } from "zustand";

import {
  api,
  type ActivityEvent as APIActivityEvent,
  type AppState,
  type Attachment,
  type ChatMessage,
  type EngineSnapshot,
  type HistoryHit,
  type HistoryRef,
  type LabListResponse,
  type LabTaskSessionSnapshot,
  type ModelsResponse,
  type Preset,
  type ResearchConfig,
  type ResearchResponse,
  type Session,
  type SysInfo,
  type ToolInfo,
  type SessionContextStatus,
} from "./api";
import {
  mergeHistoryRefs,
  normalizeHistoryRefs,
  removeHistoryRef as removeHistoryRefById,
} from "./history-ref";
import { parseEndedAt } from "./run-recovery";
import {
  crossModePickerFilter,
  resolveActiveForMode,
  resolveModeSwitchTarget,
} from "./mode-sessions";
import { activityWebSocketURL } from "./config";
import {
  isLivePhase,
  nextPhase,
  type RunEventKind,
  type RunPhase,
} from "./run-phase";
import {
  normalizeEventKind,
  normalizeThinkingControl,
  normalizeToolAllowlist,
  normalizeToolPolicyMode,
  type ThinkingControl,
  type ToolPolicyMode,
} from "./run-events";
import {
  applyReasoningSnapshot,
  applyResponseSnapshot,
  createStreamingAccumulator,
  flush as flushAccumulator,
  isEmpty as accumulatorIsEmpty,
  mergeSnapshot,
} from "./stream-accumulator";

export type ConnectionState =
  "idle" | "connecting" | "connected" | "disconnected" | "error";

// v1.1.8: top-level mode separation. Chat is the calm conversation
// surface; Agent is the engineering/action surface. Both reuse the same
// sessions, model runtime, and engine infrastructure underneath — only
// the exposed controls differ.
export type WorkspaceMode = "chat" | "agent";

const MODE_STORAGE_KEY = "sheytan.mode";

function initialWorkspaceMode(): WorkspaceMode {
  try {
    const stored = window.localStorage.getItem(MODE_STORAGE_KEY);
    return stored === "agent" ? "agent" : "chat";
  } catch {
    return "chat";
  }
}

function persistWorkspaceMode(mode: WorkspaceMode): void {
  try {
    window.localStorage.setItem(MODE_STORAGE_KEY, mode);
  } catch {
    // Storage can be unavailable (private mode) — mode stays in memory.
  }
}

// v1.2.8.1: the PER-MODE active session selection persists like the mode
// itself. v1.2.8 kept activeSessionByMode memory-only, so a reload landed
// on the newest session of the startup space instead of the conversation
// the user had actually selected (and the other space's selection was
// forgotten entirely).
const ACTIVE_SESSIONS_STORAGE_KEY = "sheytan.activeSessionByMode";

function initialActiveSessionByMode(): Record<WorkspaceMode, string | null> {
  try {
    const raw = window.localStorage.getItem(ACTIVE_SESSIONS_STORAGE_KEY);

    if (!raw) {
      return { chat: null, agent: null };
    }

    const parsed = JSON.parse(raw) as Partial<
      Record<WorkspaceMode, string | null>
    >;

    return {
      chat: typeof parsed.chat === "string" ? parsed.chat : null,
      agent: typeof parsed.agent === "string" ? parsed.agent : null,
    };
  } catch {
    return { chat: null, agent: null };
  }
}

function persistActiveSessionByMode(
  map: Record<WorkspaceMode, string | null>,
): void {
  try {
    window.localStorage.setItem(
      ACTIVE_SESSIONS_STORAGE_KEY,
      JSON.stringify(map),
    );
  } catch {
    // in-memory only
  }
}

// v1.2.5: per-request composer controls persist the same way — the
// thinking control and the manual tool selection survive reloads and are
// sent with EVERY run request (they change the actual backend behaviour).
const THINKING_STORAGE_KEY = "sheytan.thinking";
const TOOLMODE_STORAGE_KEY = "sheytan.toolMode";
const TOOLALLOW_STORAGE_KEY = "sheytan.toolAllow";

function initialThinkingControl(): ThinkingControl {
  try {
    return normalizeThinkingControl(
      window.localStorage.getItem(THINKING_STORAGE_KEY),
    );
  } catch {
    return "auto";
  }
}

function persistThinkingControl(v: ThinkingControl): void {
  try {
    window.localStorage.setItem(THINKING_STORAGE_KEY, v);
  } catch {
    // in-memory only
  }
}

function initialToolPolicyMode(): ToolPolicyMode {
  try {
    return normalizeToolPolicyMode(
      window.localStorage.getItem(TOOLMODE_STORAGE_KEY),
    );
  } catch {
    return "auto";
  }
}

function persistToolPolicyMode(v: ToolPolicyMode): void {
  try {
    window.localStorage.setItem(TOOLMODE_STORAGE_KEY, v);
  } catch {
    // in-memory only
  }
}

function initialToolAllowlist(): string[] {
  try {
    const raw = window.localStorage.getItem(TOOLALLOW_STORAGE_KEY);
    if (!raw) {
      return [];
    }
    return normalizeToolAllowlist(JSON.parse(raw));
  } catch {
    return [];
  }
}

function persistToolAllowlist(list: string[]): void {
  try {
    window.localStorage.setItem(TOOLALLOW_STORAGE_KEY, JSON.stringify(list));
  } catch {
    // in-memory only
  }
}

export type ActivityEvent = {
  id: string;
  type: string;
  timestamp: number;
  data: Record<string, unknown>;
};

type RuntimeState = {
  app: AppState | null;
  sysinfo: SysInfo | null;
  models: ModelsResponse | null;
  /** v1.2.3: true while /api/models is in flight (skeleton, not fake empty). */
  modelsLoading: boolean;
  presets: Preset[];
  tools: ToolInfo[];

  sessions: Session[];
  activeSessionId: string | null;

  connection: ConnectionState;
  loading: boolean;
  error: string | null;

  activity: ActivityEvent[];
  running: boolean;

  // v1.2.2: the live generation timeline. `runPhase` is the lifecycle
  // state machine (run-phase.ts) driven ONLY by real backend events;
  // `runStartedAt` powers the elapsed clock; `runNote` carries the
  // backend's own done/error caption verbatim (never invented).
  runPhase: RunPhase;
  runStartedAt: number | null;
  runNote: string | null;

  // v1.2.5: live status chip (backend "status" events only — measured
  // tier/token telemetry, never fabricated) + the tier-escalation trail
  // of the current/last run.
  liveStatus: string | null;
  tierEscalations: string[];

  // v1.2.5: the thinking panel's open/fold state, driven ONLY by the
  // backend's thinking_start/thinking_end markers (plus run end).
  thinkingPanelOpen: boolean;

  // v1.2.5: per-request composer controls (persisted; sent with every
  // run — they shape the actual backend request).
  thinkingControl: ThinkingControl;
  setThinkingControl: (v: ThinkingControl) => void;
  toolPolicyMode: ToolPolicyMode;
  setToolPolicyMode: (v: ToolPolicyMode) => void;
  toolAllowlist: string[];
  setToolAllowed: (name: string, allowed: boolean) => void;

  // v1.2.2: socket/poll ownership. AgentBody acquires on mount and
  // releases on unmount; a LIVE run keeps the activity socket and the
  // engine poll alive across workspace tab switches (the backend hub has
  // no replay — dropping the socket mid-run lost every event, including
  // `done`, which froze the composer forever).
  acquireActivity: () => void;
  releaseActivity: () => void;
  acquireEnginePolling: () => void;
  releaseEnginePolling: () => void;

  // v1.1.3: authoritative engine state (polled + WS-pushed).
  engine: EngineSnapshot | null;

  // v1.1.6: backend-resolved context status for the active session
  // (selector options, usage, pressure). Backend is authoritative.
  sessionContext: SessionContextStatus | null;
  sessionContextError: string | null;
  setSessionContext: (tokens: number) => Promise<void>;
  refreshSessionContext: () => Promise<void>;

  // v1.1.3: real conversation history for the active session plus the
  // streaming assistant bubble.
  //
  // v1.2.2 WIRE CONTRACT FIX: the orchestrator's emitProgress publishes
  // CUMULATIVE snapshots (each `response`/`reasoning` caption is the FULL
  // text so far). The old store appended every caption, duplicating the
  // streamed text and re-duplicating it after reconnects. The streaming
  // state now holds the LATEST authoritative snapshot per stream (see
  // stream-accumulator.ts).
  messages: ChatMessage[];
  streaming: { content: string; reasoning: string } | null;

  // v1.1.3: staged attachments for the composer.
  pendingAttachments: Attachment[];
  attachmentsUploading: boolean;

  lab: LabListResponse | null;
  labLoading: boolean;
  labError: string | null;
  activeLabTaskId: string | null;
  activeLabTask: LabTaskSessionSnapshot | null;

  researchConfig: ResearchConfig | null;
  research: ResearchResponse | null;
  researchLoading: boolean;
  researchError: string | null;

  refreshSysinfo: () => Promise<void>;
  refreshModels: () => Promise<void>;
  refreshPresets: () => Promise<void>;
  refreshSessions: () => Promise<void>;
  refreshTools: () => Promise<void>;
  refreshAgentResources: () => Promise<void>;
  refreshEngine: () => Promise<void>;
  startEnginePolling: () => void;
  // v1.1.4: the engine poll previously ran for the app's LIFETIME once
  // started (no stop function existed) — even on other views.
  stopEnginePolling: () => void;

  // v1.1.4: recall feedback (thumbs up/down on past exchanges) — the
  // backend steering existed since v1.0.6 with no write path.
  sendFeedback: (query: string, liked: boolean) => Promise<void>;

  loadSession: (id: string) => Promise<void>;

  refreshLab: () => Promise<void>;
  loadLabTask: (id: string) => Promise<void>;

  createSession: () => Promise<Session>;
  selectSession: (id: string | null) => void;
  deleteSession: (id: string) => Promise<void>;

  run: (message: string) => Promise<void>;
  abort: () => Promise<void>;
  regenerate: () => Promise<void>;

  uploadFiles: (files: File[]) => Promise<void>;
  removePendingAttachment: (id: string) => Promise<void>;

  runLabAction: (payload: Record<string, unknown>) => Promise<unknown>;

  loadResearchConfig: () => Promise<void>;
  searchResearch: (payload: {
    query: string;
    backend?: string;
    maxResults?: number;
    timeoutSec?: number;
  }) => Promise<ResearchResponse | undefined>;

  connectActivity: () => void;
  disconnectActivity: () => void;
  clearActivity: () => void;

  // v1.1.8: Chat / Agent mode switch (persisted per device).
  mode: WorkspaceMode;
  setMode: (mode: WorkspaceMode) => void;

  // v1.2.8: MODE-SEPARATED HISTORIES. One active session PER mode —
  // switching mode switches the conversation space, it never silently
  // switches (or merges) the active conversation.
  activeSessionByMode: Record<WorkspaceMode, string | null>;

  // v1.2.8: cross-mode history references attached to the ACTIVE session.
  // DATA, never authority — the backend retrieves only the relevant
  // portions per run and labels provenance.
  historyRefs: HistoryRef[];
  attachHistoryRefs: (refs: HistoryRef[]) => Promise<void>;
  detachHistoryRef: (sessionId: string) => Promise<void>;

  // v1.2.8: history picker state (search + mode filter + results).
  historyPickerOpen: boolean;
  setHistoryPickerOpen: (open: boolean) => void;
  historyHits: HistoryHit[];
  historySearchLoading: boolean;
  historySearchMode: WorkspaceMode | "all";
  setHistorySearchMode: (mode: WorkspaceMode | "all") => void;
  searchHistory: (q: string) => Promise<void>;

  // v1.2.8: bounded agent task state (rendered by the agent pipeline
  // panel; restored from run snapshots on reconnect).
  agentTask: Record<string, unknown> | null;

  // v1.2.8: lazy history paging — the newest page loads first, older
  // messages are fetched on demand ("Load earlier").
  olderHasMore: boolean;
  olderLoading: boolean;
  olderNextBefore: number | null;
  loadOlderMessages: () => Promise<void>;

  // v1.2.8: session rename (the backend PUT existed; the UI now uses it).
  renameSession: (id: string, title: string) => Promise<void>;
};

const MAX_ACTIVITY_EVENTS = 500;

let enginePollTimer: number | null = null;

// v1.2.2: engine-poll ownership refcount (0 = nobody needs the poll).
let enginePollConsumers = 0;

let socket: WebSocket | null = null;

// v1.2.6 continuation: the DETERMINISTIC ATTACH CONTRACT.
//
// POST completion must never imply the live transport was attached. The
// backend sends an explicit `attached` frame as the FIRST message after
// the protocol upgrade; connectActivity records a deferred that resolves
// on that frame, and run()/regenerate() AWAIT it before POSTing /api/run.
// No sleeps, no guessed grace windows — the acknowledgement is the proof.
let activityAttachResolve: (() => void) | null = null;
let activityAttachReject: (() => void) | null = null;
let activityAttachPromise: Promise<void> | null = null;

// beginActivityAttach arms the attach acknowledgement for a new socket.
function beginActivityAttach(): void {
  activityAttachPromise?.catch(() => {
    // A previous deferred's rejection is handled by its own awaiter.
  });

  activityAttachPromise = new Promise<void>((resolve, reject) => {
    activityAttachResolve = resolve;
    activityAttachReject = reject;
  });
}

// settleActivityAttach resolves the pending acknowledgement (the
// `attached` frame arrived — the transport is live and session-bound).
function settleActivityAttach(): void {
  if (activityAttachResolve) {
    activityAttachResolve();
    activityAttachResolve = null;
    activityAttachReject = null;
  }
}

// failActivityAttach rejects the pending acknowledgement (the socket died
// before the server acknowledged the attachment).
function failActivityAttach(): void {
  if (activityAttachReject) {
    activityAttachReject();
    activityAttachResolve = null;
    activityAttachReject = null;
  }
}

// ACTIVITY_ATTACH_COMPAT_MS bounds how long run()/regenerate() wait for
// the `attached` acknowledgement. The acknowledgement is the PRIMARY
// mechanism (deterministic, no sleeps); this bound exists ONLY so a
// backend that predates the `attached` frame (mixed deployment) can never
// hang the composer — after the bound the POST proceeds and the runId
// replay + bounded grace re-check own the resync, exactly like v1.2.6.
const ACTIVITY_ATTACH_COMPAT_MS = 5000;

// waitForActivityAttached resolves when the CURRENT activity socket has
// been acknowledged by the server (the `attached` frame), or immediately
// when a socket for the active session is already connected AND
// acknowledged. It REJECTS when the socket dies first — callers proceed
// anyway (the runId replay + the bounded grace re-check recover); the
// await exists to remove the race, not to gate the POST.
function waitForActivityAttached(): Promise<void> {
  let base: Promise<void>;

  if (
    socket &&
    socket.readyState === WebSocket.OPEN &&
    activityAttachResolve === null &&
    activityAttachPromise
  ) {
    // Already acknowledged (resolve consumed) — attach is proven.
    base = activityAttachPromise;
  } else if (activityAttachPromise) {
    base = activityAttachPromise;
  } else {
    base = Promise.reject(new Error("activity socket not connecting"));
  }

  // Compatibility bound: first settlement wins (ack, close, or bound).
  return Promise.race([
    base,
    new Promise<void>((resolve) => {
      window.setTimeout(resolve, ACTIVITY_ATTACH_COMPAT_MS);
    }),
  ]);
}

// v1.2.6 continuation: AUTHORITATIVE RUN REPLAY tracking.
//
// activeRunId is the run the UI is currently bound to (from the POST
// response or an adopted run_snapshot); lastRunSeq is the highest sequence
// number folded into the visible state. Events from other runs (stale
// drains of a replaced run's hub) and duplicate/replayed events (seq ≤
// lastRunSeq — already inside the latest cumulative snapshot) are dropped
// BEFORE they reach the pipeline.
let activeRunId: string | null = null;
let lastRunSeq = 0;

// resetRunReplayTracking resets the run-scoped filters for a fresh run.
function resetRunReplayTracking(runId: string | null): void {
  activeRunId = runId;
  lastRunSeq = 0;
}

// isStaleRunEvent reports whether one activity frame is stale for the
// run the UI is bound to: a different (older) run's drain, or a sequence
// number already folded into the current cumulative state.
function isStaleRunEvent(payload: Record<string, unknown>): boolean {
  const frameRunId = typeof payload.runId === "string" ? payload.runId : "";

  if (frameRunId && activeRunId && frameRunId !== activeRunId) {
    return true;
  }

  const seq = typeof payload.seq === "number" ? payload.seq : 0;

  if (seq > 0 && seq <= lastRunSeq) {
    return true;
  }

  // v1.2.8.1: only frames attributable to the tracked run (or arriving
  // while no run is bound yet) advance the dedup watermark. The previous
  // unconditional bump let a foreign frame WITHOUT a runId raise the
  // watermark and silently drop legitimate later frames of the tracked
  // run.
  if (seq > 0 && frameRunId) {
    if (!activeRunId || frameRunId === activeRunId) {
      lastRunSeq = seq;
    }
  }

  return false;
}
let activitySequence = 0;
let activitySessionId: string | null = null;

// v1.2.2: activity-socket ownership refcount. While any AgentBody is
// mounted the count is ≥ 1. A live run holds an implicit lease even at
// count 0 so tab switches mid-generation never drop the event stream.
let activityConsumers = 0;

// v1.1.4: automatic WebSocket reconnection. The old store gave up on the
// first close — a mid-run disconnect left `running` stuck true forever (the
// dead-composer bug's last live variant: no `done` event could ever arrive).
let reconnectTimer: number | null = null;
let reconnectAttempts = 0;

const RECONNECT_BASE_DELAY_MS = 1500;
const RECONNECT_MAX_DELAY_MS = 15000;
const RECONNECT_MAX_ATTEMPTS = 20;

// v1.2.6: bounded idle-grace re-check. When an idle sentinel arrives inside
// the attach-race grace window with no run evidence yet, it may be EITHER the
// harmless standby marker OR the only signal that the run already finished
// (fast run + late socket). The v1.2.5 code dropped it and never re-checked
// — the "no visible answer" bug. The re-check fires once when the grace
// window expires; if there is STILL no run evidence by then, the run is
// recovered from the authoritative history.
let idleGraceRecheckTimer: number | null = null;

function clearIdleGraceRecheck(): void {
  if (idleGraceRecheckTimer !== null) {
    window.clearTimeout(idleGraceRecheckTimer);
    idleGraceRecheckTimer = null;
  }
}

function clearReconnectTimer(): void {
  if (reconnectTimer !== null) {
    window.clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
}

let activityFlushFrame: number | null = null;
let pendingActivity: ActivityEvent[] = [];
let flushingActivity: ActivityEvent[] = [];
let pendingRunning: boolean | undefined;

// --- Phase 4: streaming coalescing ---------------------------------------
//
// High token rates (100+ tokens/sec) can swamp React with one setState
// per token, each re-rendering the whole message tree. The streaming
// coalescer accumulates response/reasoning chunks into a single buffer
// and flushes on the next animation frame — so no matter how fast the
// model emits, the UI updates at most once per frame (capped by the
// display's refresh rate, naturally degrading to 60 Hz on a 60 Hz
// display without wasting CPU on 120 meaningless updates).
//
// Coalescing only batches the CONTENT payload; lifecycle events
// (done/error/session) are still delivered immediately because they
// close the streaming bubble and must reset `running`.
//
// v1.2.2: the buffer holds the LATEST CUMULATIVE snapshot per stream
// (backend contract — see stream-accumulator.ts), not appended deltas.
let streamingFlushFrame: number | null = null;
const streamAccumulator = createStreamingAccumulator();

function resetPendingStreaming(): void {
  if (streamingFlushFrame !== null) {
    cancelAnimationFrame(streamingFlushFrame);

    streamingFlushFrame = null;
  }

  streamAccumulator.content = null;
  streamAccumulator.reasoning = null;
}

// flushStreaming writes the latest cumulative snapshots to the store in
// ONE setState, then resets the buffers. Runs on a rAF boundary so
// multiple token chunks arriving within one frame coalesce into a
// single render.
function flushStreaming(): void {
  streamingFlushFrame = null;

  if (accumulatorIsEmpty(streamAccumulator)) {
    return;
  }

  const update = flushAccumulator(streamAccumulator);

  const current = useRuntimeStore.getState().streaming;

  useRuntimeStore.setState({
    streaming: mergeSnapshot(current, update),
  });

  // Phase 4 perf HUD: count this as one coalesced stream update so the
  // HUD can measure update frequency (should be <= display refresh rate,
  // never one-per-token). The recordStreamUpdate import is dynamic so
  // this file stays decoupled from perf-hud.ts when the HUD is disabled.
  recordStreamUpdateSafe();
}

// captureRunBaseline records how many assistant messages exist when the
// first streamed token of THIS run arrives. The finalisation path uses it
// to decide whether the authoritative history actually gained a reply —
// and, when it did not, to preserve the partial output locally instead of
// silently losing the entire response (v1.2.2).
let runAssistantBaseline: number | null = null;

function captureRunBaselineIfNeeded(): void {
  if (runAssistantBaseline === null) {
    runAssistantBaseline = useRuntimeStore
      .getState()
      .messages.filter((message) => message.role === "assistant").length;
  }
}

// flushStreaming's legacy inline body was replaced by the cumulative
// snapshot merge above (v1.2.2); the buffers reset inside flushAccumulator.

// recordStreamUpdateSafe is a thin wrapper around perf-hud's counter.
// Kept as a separate function so the store never throws if the perf-hud
// module fails to load (it's a diagnostic; never let it break the app).
let recordStreamUpdateFn: (() => void) | null = null;

// setStreamUpdateRecorder is exported so main.tsx can wire the perf-hud
// counter into the store after both modules load (avoids a circular
// import: store.ts ↔ perf-hud.ts).
export function setStreamUpdateRecorder(fn: (() => void) | null): void {
  recordStreamUpdateFn = fn;
}

function recordStreamUpdateSafe(): void {
  if (recordStreamUpdateFn !== null) {
    try {
      recordStreamUpdateFn();
    } catch {
      // Swallow — the HUD is diagnostic only.
    }
  }
}

function scheduleStreamingFlush(): void {
  if (streamingFlushFrame !== null) {
    return;
  }

  streamingFlushFrame = requestAnimationFrame(flushStreaming);
}

// queueStreamingContent folds one cumulative `response` snapshot (the
// caption IS the full text so far) and schedules a frame-aligned flush.
function queueStreamingContent(snapshot: string): void {
  if (!snapshot) return;

  captureRunBaselineIfNeeded();
  applyResponseSnapshot(streamAccumulator, snapshot);
  scheduleStreamingFlush();
}

// queueStreamingReasoning folds one cumulative `reasoning` snapshot.
function queueStreamingReasoning(snapshot: string): void {
  if (!snapshot) return;

  captureRunBaselineIfNeeded();
  applyReasoningSnapshot(streamAccumulator, snapshot);
  scheduleStreamingFlush();
}

function createActivityID(): string {
  activitySequence += 1;

  return `${Date.now()}-${activitySequence}`;
}

function normalizeActivity(payload: unknown): ActivityEvent {
  if (payload && typeof payload === "object") {
    const value = payload as Record<string, unknown>;

    const rawTimestamp = value.timestamp;

    let timestamp = Date.now();

    if (typeof rawTimestamp === "number") {
      timestamp = rawTimestamp;
    } else if (typeof rawTimestamp === "string") {
      const parsed = Date.parse(rawTimestamp);

      if (!Number.isNaN(parsed)) {
        timestamp = parsed;
      }
    }

    return {
      id: createActivityID(),
      type: typeof value.type === "string" ? value.type : "activity",
      timestamp,
      data: value,
    };
  }

  return {
    id: createActivityID(),
    type: "activity",
    timestamp: Date.now(),
    data: {
      value: payload,
    },
  };
}

function resetPendingActivity(): void {
  if (activityFlushFrame !== null) {
    cancelAnimationFrame(activityFlushFrame);

    activityFlushFrame = null;
  }

  pendingActivity.length = 0;
  flushingActivity.length = 0;
  pendingRunning = undefined;
}

function flushActivity(): void {
  activityFlushFrame = null;

  if (pendingActivity.length === 0 && pendingRunning === undefined) {
    return;
  }

  const batch = pendingActivity;

  pendingActivity = flushingActivity;
  flushingActivity = batch;

  const running = pendingRunning;
  pendingRunning = undefined;

  if (
    !activitySessionId ||
    activitySessionId !== useRuntimeStore.getState().activeSessionId
  ) {
    flushingActivity.length = 0;

    return;
  }

  setActivityBatch(batch, running);

  batch.length = 0;
}

function scheduleActivityFlush(): void {
  if (activityFlushFrame !== null) {
    return;
  }

  activityFlushFrame = requestAnimationFrame(flushActivity);
}

function queueActivity(activity: ActivityEvent): void {
  pendingActivity.push(activity);

  const running = activity.data.running;

  if (typeof running === "boolean") {
    pendingRunning = running;
  }

  scheduleActivityFlush();
}

function setActivityBatch(
  batch: ActivityEvent[],
  running: boolean | undefined,
): void {
  useRuntimeStore.setState((state) => {
    let activity = state.activity;

    if (batch.length > 0) {
      if (batch.length >= MAX_ACTIVITY_EVENTS) {
        activity = batch.slice(-MAX_ACTIVITY_EVENTS);
      } else if (state.activity.length + batch.length <= MAX_ACTIVITY_EVENTS) {
        activity = [...state.activity, ...batch];
      } else {
        const keep = MAX_ACTIVITY_EVENTS - batch.length;

        activity = [...state.activity.slice(-keep), ...batch];
      }
    }

    if (running === undefined) {
      return {
        activity,
      };
    }

    return {
      activity,
      running,
    };
  });

  // v1.1.3: route conversation-relevant events into the message pipeline
  // (streaming bubbles + the run-end bookkeeping that used to leave the
  // composer permanently disabled after one message).
  for (const event of batch) {
    handleConversationEvent(event);
  }
}

// transitionPhase advances the run lifecycle machine (run-phase.ts) from a
// real wire event. Unknown/stale events are no-ops — the machine itself
// decides which transitions are legal from which phase.
function transitionPhase(event: RunEventKind): void {
  const state = useRuntimeStore.getState();
  const next = nextPhase(state.runPhase, event);

  if (next !== state.runPhase) {
    useRuntimeStore.setState({ runPhase: next });
  }
}

// --- v1.2.2: run finalisation ----------------------------------------------
//
// done/error/abort/disconnect/idle must ALWAYS leave the UI in a correct
// non-running state with the run's output preserved. The backend persists
// the assistant reply AFTER emitting `done` (and not at all on error or
// abort), so finalisation:
//   1. reloads the authoritative history;
//   2. confirms it actually gained a new assistant message;
//   3. if it did NOT, promotes the streamed partial into the conversation
//      locally so the user never loses the entire response.
let runFinalizeTimer: number | null = null;
let runOutcome: "done" | "aborted" | "lost" | null = null;

// runEventsReceived: did THIS run deliver any hub event over the socket?
// Distinguishes "socket never attached / run gone" from "still preparing"
// when an idle sentinel arrives (see recoverRunFromIdle).
let runEventsReceived = false;

const RUN_EVENT_TYPES = new Set([
  "response",
  "reasoning",
  "thinking",
  "context",
  "tool_start",
  "tool_end",
  "done",
  "error",
  "session",
  // v1.2.5 additions.
  "assistant_delta",
  "thinking_delta",
  "thinking_start",
  "thinking_end",
  "complete",
  "status",
  "escalation",
]);

function clearRunFinalizeTimer(): void {
  if (runFinalizeTimer !== null) {
    window.clearTimeout(runFinalizeTimer);
    runFinalizeTimer = null;
  }

  // v1.2.6: the idle-grace re-check shares the run's lifetime — it must
  // never outlive the run bookkeeping it guards.
  clearIdleGraceRecheck();
}

// releaseIdleOwnedResources drops the activity socket and the engine poll
// when no mounted component owns them AND no run is live — the implicit
// run lease is over. Prevents both duplicate sockets after tab switches
// and post-run resource leaks.
function releaseIdleOwnedResources(): void {
  const state = useRuntimeStore.getState();

  if (
    activityConsumers === 0 &&
    !state.running &&
    !isLivePhase(state.runPhase)
  ) {
    state.disconnectActivity();
  }

  if (
    enginePollConsumers === 0 &&
    !state.running &&
    !isLivePhase(state.runPhase)
  ) {
    state.stopEnginePolling();
  }
}

// promotePartialOutput appends the streamed partial as a local assistant
// message so errors/aborts/lost-runs never discard the entire response.
// The message is marked via its `at` stamp + a visible note appended to
// the content (never presented as a fully persisted reply).
function promotePartialOutput(note: string | null): void {
  const state = useRuntimeStore.getState();
  const partial = state.streaming;

  if (!partial || (!partial.content && !partial.reasoning)) {
    return;
  }

  const marker = note ? `\n\n— ${note}` : "";

  useRuntimeStore.setState({
    messages: [
      ...state.messages,
      {
        role: "assistant" as const,
        content: (partial.content || "*(no answer text arrived)*") + marker,
        ...(partial.reasoning ? { reasoning: partial.reasoning } : {}),
      },
    ],
    streaming: null,
  });
}

// finaliseRun performs the authoritative-history confirmation for one run
// end. Called via setTimeout AFTER the backend had time to persist, and
// again as a bounded fallback if the first attempt saw no reply land.
async function finaliseRun(sessionId: string): Promise<void> {
  const state = useRuntimeStore.getState();

  if (state.activeSessionId !== sessionId) {
    // Session switched/rolled over — nothing to confirm here anymore.
    return;
  }

  const hadPartial = Boolean(
    state.streaming && (state.streaming.content || state.streaming.reasoning),
  );

  await state.loadSession(sessionId);

  const after = useRuntimeStore.getState();

  if (after.activeSessionId !== sessionId) {
    return;
  }

  // v1.1.6: the turn changed the session's context usage — refresh the
  // authoritative status (used/pressure).
  void after.refreshSessionContext();

  const assistants = after.messages.filter(
    (message) => message.role === "assistant",
  ).length;

  const historyGainedReply =
    runAssistantBaseline === null ? true : assistants > runAssistantBaseline;

  if (!historyGainedReply && hadPartial) {
    // The reply never persisted (abort, lost run, persistence failure) —
    // keep the partial output visible instead of dropping it.
    promotePartialOutput(
      runOutcome === "done"
        ? "partial reply preserved locally (session history did not record it)"
        : runOutcome === "aborted"
          ? "stopped — partial reply preserved locally"
          : "run ended while the view was away — partial reply preserved locally",
    );
  } else {
    useRuntimeStore.setState({ streaming: null });
  }

  useRuntimeStore.setState({
    // "lost" (run ended while detached) settles as Stopped — honest: the
    // run is no longer active and its state was re-synced from history.
    runPhase:
      runOutcome === "done"
        ? "complete"
        : runOutcome === "lost"
          ? "aborted"
          : (runOutcome ?? "aborted"),
  });

  runOutcome = null;
  runAssistantBaseline = null;
  releaseIdleOwnedResources();
}

// scheduleRunFinalisation arms the confirm pass (and one retry — the
// backend writes the reply right after `done`; a slow disk can beat the
// first reload by a hair).
function scheduleRunFinalisation(sessionId: string, delayMs: number): void {
  clearRunFinalizeTimer();

  runFinalizeTimer = window.setTimeout(() => {
    runFinalizeTimer = null;

    const state = useRuntimeStore.getState();

    if (state.activeSessionId !== sessionId || state.running) {
      return;
    }

    void finaliseRun(sessionId).then(() => {
      // Bounded second attempt: if the first reload raced the persist
      // write AND no partial was promoted, retry once more.
      const current = useRuntimeStore.getState();

      if (
        current.activeSessionId === sessionId &&
        current.runPhase === "finalising" &&
        !current.streaming
      ) {
        void finaliseRun(sessionId);
      }
    });
  }, delayMs);
}

// recoverRunFromIdle handles the backend's `idle` sentinel ("No active
// run"): the UI believed a run was live but the backend has none
// registered — the run finished while this (or a previous) socket was
// detached. Resync from the authoritative history instead of staying
// stuck with running=true and a frozen composer.
//
// RACE GUARD: the backend also sends the idle sentinel when a socket
// PARKS IN STANDBY — including the attach race window right after Send
// (POST /run accepted → run registered → standby sockets woken). An
// idle frame arriving within the grace window while this run has seen
// no events yet is that harmless standby marker, not evidence the run
// is gone; recovering then would abort a live run.
//
// v1.2.6 fixes the hole this guard left:
//   1. The sentinel now carries the backend's AUTHORITATIVE last-run
//      outcome (runId, endedAt, result, persisted). When the recorded
//      run ended at/after this run started, that is PROOF the run is
//      over — recover immediately, grace window or not.
//   2. A grace-ignored sentinel now arms a BOUNDED re-check at the end
//      of the grace window: if no run evidence arrived by then either,
//      the run finalises — the composer can never stay frozen forever.
const RUN_IDLE_GRACE_MS = 2500;

// readLastRunOutcome extracts the v1.2.6 authoritative last-run block from an
// idle frame (absent on older backends — treated as no evidence).
function readLastRunOutcome(
  event: ActivityEvent,
): { endedAt: number; outcome: string; runId?: string } | null {
  const lastRun = (event.data as { lastRun?: unknown }).lastRun;

  if (!lastRun || typeof lastRun !== "object") {
    return null;
  }

  const value = lastRun as Record<string, unknown>;
  const endedAt = parseEndedAt(value.endedAt);
  const outcome = value.outcome;

  // v1.2.8 ROOT-CAUSE FIX: the backend marshals runOutcome.EndedAt
  // (time.Time) as an RFC3339 STRING on the wire (pinned by the Go
  // contract test), while this parser previously accepted ONLY an
  // epoch-ms number — the authoritative fast-path recovery in
  // recoverRunFromIdle could therefore never fire and every recovery
  // silently fell through to the grace re-check. Both wire shapes are
  // now accepted; no backend contract is broken.
  if (endedAt === null || typeof outcome !== "string") {
    return null;
  }

  const runId = typeof value.runId === "string" ? value.runId : undefined;

  return { endedAt, outcome, runId };
}

// handleRunSnapshot folds the backend's AUTHORITATIVE run snapshot into
// the UI (v1.2.6 continuation). The server sends this frame on EVERY
// WebSocket attachment — a reconnect during generation therefore renders
// the run's current state immediately (cumulative snapshots are
// replacement-safe by contract) instead of waiting for a future token.
//
// Terminal snapshots finalise the run exactly like the done/error paths:
// the composer unlocks, the reply snapshot is preserved, and the
// authoritative history reload confirms what was persisted.
function handleRunSnapshot(payload: Record<string, unknown>): void {
  const state = useRuntimeStore.getState();

  const runId = typeof payload.runId === "string" ? payload.runId : "";
  const running = payload.running === true;
  const sequence = typeof payload.sequence === "number" ? payload.sequence : 0;
  const phase = typeof payload.phase === "string" ? payload.phase : "";
  const terminal =
    typeof payload.terminalOutcome === "string"
      ? payload.terminalOutcome
      : "";
  const latestResponse =
    typeof payload.latestResponse === "string" ? payload.latestResponse : "";
  const latestReasoning =
    typeof payload.latestReasoning === "string" ? payload.latestReasoning : "";

  // Adopt the run identity: a socket that attached without a POST (page
  // reload mid-run, chapter switch) binds to whatever run is live.
  if (runId) {
    activeRunId = runId;
    lastRunSeq = sequence;
  }

  // v1.2.8: restore the bounded task-state view from the snapshot — a
  // reconnect mid-run recovers the whole working state (goal, step,
  // files, commands, tests, verification), not just the streamed text.
  if (payload.task && typeof payload.task === "object") {
    useRuntimeStore.setState({
      agentTask: payload.task as Record<string, unknown>,
    });
  }

  // Terminal state: finalise through the same path the done/error events
  // use (the composer unlocks, the history reload confirms persistence).
  if (!running || terminal) {
    const outcome = terminal || "done";

    // Replay whatever the run produced so the partial output is visible.
    if (latestResponse) {
      queueStreamingContent(latestResponse);
    }

    if (latestReasoning) {
      queueStreamingReasoning(latestReasoning);
    }

    flushStreaming();

    if (outcome === "error") {
      transitionPhase("error");

      useRuntimeStore.setState({
        running: false,
        runPhase: "error",
        error:
          typeof payload.error === "string" && payload.error
            ? payload.error
            : "Agent run failed.",
      });
    } else {
      transitionPhase("done");

      runOutcome = outcome === "aborted" ? "aborted" : "done";

      useRuntimeStore.setState({
        running: false,
        runNote: null,
      });
    }

    const sessionId = useRuntimeStore.getState().activeSessionId;

    if (sessionId) {
      scheduleRunFinalisation(sessionId, 50);
    } else {
      useRuntimeStore.setState({ streaming: null });
      runOutcome = null;
      runAssistantBaseline = null;
      releaseIdleOwnedResources();
    }

    return;
  }

  // LIVE run: replay the cumulative snapshots and advance the phase
  // machine from OBSERVED evidence only. A snapshot for a run the UI is
  // not tracking (fresh page load) still renders — the run is real and
  // the backend is authoritative.
  if (!state.running && !isLivePhase(state.runPhase)) {
    // The UI was idle (page reload mid-run): open a fresh timeline.
    useRuntimeStore.setState({
      running: true,
      runPhase: "preparing",
      runStartedAt: Date.now(),
      runNote: null,
      liveStatus: null,
      tierEscalations: [],
    });
  }

  runEventsReceived = true;

  if (latestReasoning) {
    queueStreamingReasoning(latestReasoning);
    transitionPhase("reasoning_delta");
  }

  if (latestResponse) {
    queueStreamingContent(latestResponse);
    transitionPhase("response_delta");
  }

  if (phase === "thinking" && !latestResponse) {
    transitionPhase("thinking_activity");
  }

  // The backend's own status line, replayed verbatim.
  const status =
    typeof payload.latestStatus === "string" ? payload.latestStatus : "";

  if (status) {
    useRuntimeStore.setState({ liveStatus: status });
  }
}

function recoverRunFromIdle(event: ActivityEvent): void {
  const state = useRuntimeStore.getState();

  const runStartedAt = state.runStartedAt;
  const sawRunEvidence = state.streaming !== null || runEventsReceived;

  // v1.2.6 path 1 — authoritative evidence: the backend's own last-run
  // record ended at/after this run started, so the run is DEFINITIVELY over.
  // This is exactly the fast-run-late-socket case that used to strand the
  // UI: the run finished before the socket attached, the sentinel was the
  // only signal, and the grace guard dropped it.
  const lastRun = readLastRunOutcome(event);

  if (
    lastRun &&
    runStartedAt !== null &&
    lastRun.endedAt + 500 >= runStartedAt
  ) {
    if (runOutcome === null) {
      switch (lastRun.outcome) {
        case "error":
          runOutcome = "lost";
          break;
        case "aborted":
          runOutcome = "aborted";
          break;
        default:
          runOutcome = "done";
          break;
      }
    }

    flushStreaming();

    useRuntimeStore.setState({ running: false });

    if (state.activeSessionId) {
      scheduleRunFinalisation(state.activeSessionId, 50);
    } else {
      useRuntimeStore.setState({ streaming: null, runPhase: "aborted" });
      runOutcome = null;
      runAssistantBaseline = null;
      releaseIdleOwnedResources();
    }

    return;
  }

  if (
    runStartedAt !== null &&
    !sawRunEvidence &&
    Date.now() - runStartedAt < RUN_IDLE_GRACE_MS
  ) {
    // Standby-entry marker during the attach race — ignore for now, but
    // arm the BOUNDED re-check: if the run produces no evidence by the end
    // of the grace window, it is recovered (never frozen forever).
    if (idleGraceRecheckTimer === null) {
      const dueAt = runStartedAt + RUN_IDLE_GRACE_MS + 200;
      const delay = Math.max(0, dueAt - Date.now());

      idleGraceRecheckTimer = window.setTimeout(() => {
        idleGraceRecheckTimer = null;

        const now = useRuntimeStore.getState();

        if (
          now.running &&
          now.runStartedAt === runStartedAt &&
          !now.streaming &&
          !runEventsReceived
        ) {
          // Still no evidence of the run the UI is waiting for — recover
          // from the authoritative history.
          runOutcome = runOutcome ?? "lost";

          useRuntimeStore.setState({ running: false });

          if (now.activeSessionId) {
            scheduleRunFinalisation(now.activeSessionId, 50);
          } else {
            useRuntimeStore.setState({ streaming: null, runPhase: "aborted" });
            runOutcome = null;
            runAssistantBaseline = null;
            releaseIdleOwnedResources();
          }
        }
      }, delay);
    }

    return;
  }

  if (runOutcome === null) {
    runOutcome = "lost";
  }

  flushStreaming();

  useRuntimeStore.setState({ running: false });

  if (state.activeSessionId) {
    scheduleRunFinalisation(state.activeSessionId, 50);
  } else {
    useRuntimeStore.setState({ streaming: null, runPhase: "aborted" });
    runOutcome = null;
    runAssistantBaseline = null;
    releaseIdleOwnedResources();
  }
}

// handleConversationEvent mirrors activity stream events into the real
// conversation view and repairs the run state machine.
//
// Phase 4: streaming response/reasoning snapshots are COALESCED through
// queueStreamingContent / queueStreamingReasoning and flushed on a rAF
// boundary. This means a model emitting 200 tokens/sec no longer
// triggers 200 React renders/sec — the UI updates at most once per
// frame, naturally capped by the display's refresh rate. Lifecycle
// events (done/error/session) bypass the coalescer and reset state
// immediately so `running` clears without delay.
function handleConversationEvent(event: ActivityEvent): void {
  if (RUN_EVENT_TYPES.has(event.type)) {
    runEventsReceived = true;
  }

  // v1.2.5: one canonical mapping for the whole wire vocabulary — legacy
  // names and the new aliases flow through the SAME handlers, so nothing
  // is processed (or displayed) twice.
  const kind = normalizeEventKind(event.type);

  switch (kind) {
    case "assistant_delta": {
      const content =
        typeof event.data.caption === "string" ? event.data.caption : "";

      if (content) {
        queueStreamingContent(content);
        transitionPhase("response_delta");
      }

      break;
    }

    case "thinking_delta": {
      const reasoning =
        typeof event.data.caption === "string" ? event.data.caption : "";

      if (reasoning) {
        queueStreamingReasoning(reasoning);
        transitionPhase("reasoning_delta");
      }

      break;
    }

    case "thinking_open": {
      // The backend began a reasoning burst — open the panel (actual
      // backend event, never fabricated).
      useRuntimeStore.setState({ thinkingPanelOpen: true });
      transitionPhase("thinking_activity");
      break;
    }

    case "thinking_close": {
      // Reasoning finished — fold the panel by default.
      useRuntimeStore.setState({ thinkingPanelOpen: false });
      transitionPhase("thinking_activity");
      break;
    }

    case "status": {
      // v1.2.5: the backend's own live status line (measured tier/token
      // telemetry) — displayed verbatim, never invented.
      const caption =
        typeof event.data.caption === "string" ? event.data.caption : "";

      if (caption) {
        useRuntimeStore.setState({ liveStatus: caption });
      }

      transitionPhase("thinking_activity");
      break;
    }

    case "escalation": {
      // v1.2.5: a tier move with its evidence reason — kept for the
      // timeline and the post-run summary.
      const caption =
        typeof event.data.caption === "string" ? event.data.caption : "";

      useRuntimeStore.setState((state) => ({
        tierEscalations:
          caption && !state.tierEscalations.includes(caption)
            ? [...state.tierEscalations, caption]
            : state.tierEscalations,
      }));

      transitionPhase("thinking_activity");
      break;
    }

    case "task": {
      // v1.2.8: the bounded agent task-state snapshot — the agent pipeline
      // panel renders the REAL runtime state (goal, current step, files,
      // commands, tests, verification). Detail carries the bounded state.
      if (event.data.detail && typeof event.data.detail === "object") {
        useRuntimeStore.setState({
          agentTask: event.data.detail as Record<string, unknown>,
        });
      }

      transitionPhase("thinking_activity");
      break;
    }

    case "handoff": {
      // v1.2.8: agent.md handoff written at settlement — a timeline
      // event only; the durable state lives in the workspace file.
      transitionPhase("thinking_activity");
      break;
    }

    case "tool_start":
    case "tool_end":
    case "context":
    case "perf":
    case "failure":
    case "plan":
    case "progress": {
      // Progressive activity alongside the generation — these advance
      // Preparing → Thinking but never demote Generating.
      transitionPhase("thinking_activity");

      break;
    }

    case "idle": {
      // Sent by the backend whenever this socket has no run attached.
      // Only meaningful when the UI believes a run is live — see
      // recoverRunFromIdle.
      const state = useRuntimeStore.getState();

      if (state.running || isLivePhase(state.runPhase)) {
        recoverRunFromIdle(event);
      }

      break;
    }

    case "session": {
      // v1.1.4: Continuum chapter rollover — the backend distilled the
      // conversation into a fresh chapter session and tells the UI here.
      // Follow the thread into the new chapter automatically.
      const nextSessionId =
        typeof event.data.sessionId === "string" ? event.data.sessionId : "";
      const currentId = useRuntimeStore.getState().activeSessionId;

      if (nextSessionId && nextSessionId !== currentId) {
        // Drop any pending streaming chunks — the chapter is closing.
        resetPendingStreaming();
        clearRunFinalizeTimer();
        runOutcome = null;
        runAssistantBaseline = null;

        useRuntimeStore.setState({
          running: false,
          streaming: null,
          runPhase: "complete",
          runNote: null,
        });
        void useRuntimeStore.getState().selectSession(nextSessionId);
        void useRuntimeStore.getState().refreshSessions();
      }

      break;
    }

    case "done":
    case "complete":
    case "error": {
      // THE v1.1.2 dead-composer fix: a finished or failed run must
      // always release the composer. The old code only reset `running`
      // on error paths, so a successful reply left it disabled forever.
      //
      // v1.2.2: flush pending snapshots FIRST so the final content is
      // visible, then run the full finalisation state machine. The
      // streaming bubble stays visible through "Finalising" until the
      // authoritative history (or the preserved partial) replaces it —
      // no blink, no lost output.
      flushStreaming();

      // v1.2.5: the run ended — fold the thinking panel and freeze the
      // status chip (it will refresh on the next run).
      useRuntimeStore.setState({ thinkingPanelOpen: false });

      const caption =
        typeof event.data.caption === "string" ? event.data.caption : "";

      const state = useRuntimeStore.getState();
      const sessionId = state.activeSessionId;

      if (event.type === "error") {
        // Terminal failure: the backend did NOT persist a reply. Unlock
        // the composer, surface the backend's own caption, and keep the
        // partial output as a local assistant message.
        transitionPhase("error");

        useRuntimeStore.setState({
          running: false,
          error: caption || "Agent run failed.",
          runNote: caption || null,
        });

        promotePartialOutput(
          caption
            ? `run failed — partial reply preserved locally (${caption})`
            : "run failed — partial reply preserved locally",
        );

        runOutcome = null;
        runAssistantBaseline = null;
        releaseIdleOwnedResources();

        break;
      }

      transitionPhase("done");

      // v1.2.2: the backend's own abort captions mark stopped runs so the
      // timeline settles on "Stopped", not "Complete".
      const captionLower = caption.toLowerCase();
      const abortedByBackend =
        captionLower === "aborted by user" ||
        captionLower.startsWith("run stopped");

      runOutcome = abortedByBackend ? "aborted" : "done";

      useRuntimeStore.setState({
        running: false,
        runNote: caption || null,
      });

      if (sessionId) {
        scheduleRunFinalisation(sessionId, 400);
      } else {
        useRuntimeStore.setState({ streaming: null, runPhase: "complete" });
        runOutcome = null;
        runAssistantBaseline = null;
        releaseIdleOwnedResources();
      }

      break;
    }

    default:
      break;
  }
}

// v1.2.8.1: boot-time mode + per-mode selections, resolved ONCE so the
// initial state (active session of the startup space) and the persisted
// map agree.
const bootMode = initialWorkspaceMode();
const bootSessions = initialActiveSessionByMode();

export const useRuntimeStore = create<RuntimeState>((set, get) => ({
  app: null,
  sysinfo: null,
  models: null,
  modelsLoading: false,
  presets: [],
  tools: [],

  sessions: [],
  // (activeSessionId is initialized from the persisted per-mode map at
  // the mode block below — v1.2.8.1.)

  connection: "idle",
  loading: false,
  error: null,

  activity: [],
  running: false,

  // v1.2.2: generation timeline state.
  runPhase: "idle",
  runStartedAt: null,
  runNote: null,

  // v1.2.5: live status + escalation trail + composer controls.
  liveStatus: null,
  tierEscalations: [],
  thinkingPanelOpen: false,
  thinkingControl: initialThinkingControl(),
  toolPolicyMode: initialToolPolicyMode(),
  toolAllowlist: initialToolAllowlist(),

  engine: null,

  // v1.1.6: per-session context status (fetched per active session).
  sessionContext: null,
  sessionContextError: null,

  messages: [],
  streaming: null,

  pendingAttachments: [],
  attachmentsUploading: false,

  lab: null,
  labLoading: false,
  labError: null,
  activeLabTaskId: null,
  activeLabTask: null,

  researchConfig: null,
  research: null,
  researchLoading: false,
  researchError: null,

  refreshSysinfo: async () => {
    try {
      const sysinfo = await api.sysinfo();

      set({ sysinfo });
    } catch (error) {
      set({
        error:
          error instanceof Error
            ? error.message
            : "Failed to refresh system information.",
      });
    }
  },

  refreshModels: async () => {
    // v1.2.3: surface the in-flight state so UIs render a skeleton
    // instead of mistaking "not loaded yet" for "no models".
    set({ modelsLoading: true });
    try {
      const models = await api.models();

      set({ models, modelsLoading: false });
    } catch (error) {
      set({
        modelsLoading: false,
        error:
          error instanceof Error ? error.message : "Failed to refresh models.",
      });
    }
  },

  // v1.2.5: composer control actions — persisted + sent with every run.
  setThinkingControl: (v) => {
    const control = normalizeThinkingControl(v);
    persistThinkingControl(control);
    set({ thinkingControl: control });
  },

  setToolPolicyMode: (v) => {
    const mode = normalizeToolPolicyMode(v);
    persistToolPolicyMode(mode);
    set({ toolPolicyMode: mode });
  },

  setToolAllowed: (name, allowed) => {
    const clean = name.trim();
    if (!clean) {
      return;
    }

    const current = get().toolAllowlist;
    const key = clean.toLowerCase();

    const next = allowed
      ? current.some((n) => n.toLowerCase() === key)
        ? current
        : [...current, clean]
      : current.filter((n) => n.toLowerCase() !== key);

    persistToolAllowlist(next);
    set({ toolAllowlist: next });
  },

  refreshPresets: async () => {
    try {
      const presets = await api.presets();

      set({ presets });
    } catch (error) {
      set({
        error:
          error instanceof Error ? error.message : "Failed to refresh presets.",
      });
    }
  },

  refreshSessions: async () => {
    // v1.2.8.1: capture the mode BEFORE the await — a refresh started in
    // one mode must never land after a mode switch and overwrite the new
    // space's list/selection with the old mode's sessions.
    const mode = get().mode;

    try {
      // v1.2.8: the sidebar shows ONE conversation space. The full list is
      // still reachable through the history picker (cross-mode search).
      const sessions = await api.sessions(mode);

      const after = get();

      if (after.mode !== mode) {
        return; // the user switched modes while this fetch was in flight
      }

      const previous = after.activeSessionId;

      // The remembered selection only survives when it still exists IN
      // THIS mode's space (never silently switched to another space).
      const activeSessionId = resolveActiveForMode(
        sessions,
        mode,
        previous,
      );

      set((state) => ({
        sessions,
        activeSessionId,
        activeSessionByMode: {
          ...state.activeSessionByMode,
          [mode]: activeSessionId,
        },
      }));

      // v1.2.8.1: when the re-resolution CHANGED the active id (remembered
      // session deleted, or first entry into a space with no memory), the
      // conversation follows the new selection — the UI must never show an
      // empty conversation merely because the session list refreshed
      // asynchronously.
      if (activeSessionId && activeSessionId !== previous) {
        if (activityConsumers > 0) {
          get().connectActivity();
        }

        void get().loadSession(activeSessionId);
        void get().refreshSessionContext();
      }
    } catch (error) {
      set({
        error:
          error instanceof Error
            ? error.message
            : "Failed to refresh sessions.",
      });
    }
  },

  refreshTools: async () => {
    try {
      const tools = await api.tools();

      set({ tools });
    } catch (error) {
      set({
        error:
          error instanceof Error ? error.message : "Failed to refresh tools.",
      });
    }
  },

  refreshAgentResources: async () => {
    set({
      error: null,
    });

    try {
      const [sysinfo, presets, tools] = await Promise.all([
        api.sysinfo(),
        api.presets(),
        api.tools(),
      ]);

      set({
        sysinfo,
        presets,
        tools,
      });
    } catch (error) {
      set({
        error:
          error instanceof Error
            ? error.message
            : "Failed to load Agent resources.",
      });
    }
  },

  refreshEngine: async () => {
    try {
      const engine = await api.engine();

      // v1.2.4: skip the state write when the snapshot is unchanged. The
      // poll fires every 2.5 s; a fresh object identity used to re-render
      // every component that selects `engine` (AgentBody, AgentHeader,
      // ModelPicker) even when nothing changed.
      const prev = useRuntimeStore.getState().engine;
      if (prev && JSON.stringify(prev) === JSON.stringify(engine)) {
        return;
      }

      set({ engine });
    } catch {
      // Engine endpoint unreachable — connection state already reflects
      // backend health; leave the last known engine snapshot in place.
    }
  },

  startEnginePolling: () => {
    if (enginePollTimer !== null) {
      return;
    }

    void get().refreshEngine();

    enginePollTimer = window.setInterval(() => {
      void useRuntimeStore.getState().refreshEngine();
    }, 2500);
  },

  stopEnginePolling: () => {
    if (enginePollTimer !== null) {
      window.clearInterval(enginePollTimer);
      enginePollTimer = null;
    }
  },

  // v1.2.2: ownership API. acquire/release bracket the mounted lifetime
  // of the Agent surface. A LIVE run keeps the socket + poll alive across
  // workspace tab switches (the backend activity hub has no replay);
  // they are released the moment the run settles if nothing is mounted.
  acquireActivity: () => {
    activityConsumers += 1;

    if (get().activeSessionId) {
      get().connectActivity();
    }
  },

  releaseActivity: () => {
    activityConsumers = Math.max(0, activityConsumers - 1);

    const state = get();

    if (
      activityConsumers === 0 &&
      !state.running &&
      !isLivePhase(state.runPhase)
    ) {
      state.disconnectActivity();
    }
  },

  acquireEnginePolling: () => {
    enginePollConsumers += 1;

    get().startEnginePolling();
  },

  releaseEnginePolling: () => {
    enginePollConsumers = Math.max(0, enginePollConsumers - 1);

    const state = get();

    if (
      enginePollConsumers === 0 &&
      !state.running &&
      !isLivePhase(state.runPhase)
    ) {
      state.stopEnginePolling();
    }
  },

  // v1.1.6: fetch the backend-resolved context status for the active
  // session. Switching sessions restores each chat's own policy — the
  // status is per-session and never mutates other chats.
  refreshSessionContext: async () => {
    const id = get().activeSessionId;
    if (!id) {
      set({ sessionContext: null, sessionContextError: null });
      return;
    }
    try {
      const status = await api.sessionContext(id);
      if (useRuntimeStore.getState().activeSessionId !== id) {
        return;
      }
      set({ sessionContext: status, sessionContextError: null });
    } catch (err) {
      if (useRuntimeStore.getState().activeSessionId !== id) {
        return;
      }
      set({
        sessionContext: null,
        sessionContextError:
          err instanceof Error ? err.message : "context status unavailable",
      });
    }
  },

  // v1.1.6: set THIS session's context policy; the backend persists it
  // with the session and returns the resolved status.
  setSessionContext: async (tokens) => {
    const id = get().activeSessionId;
    if (!id) {
      return;
    }
    try {
      const status = await api.setSessionContext(id, tokens);
      if (useRuntimeStore.getState().activeSessionId !== id) {
        return;
      }
      set({ sessionContext: status, sessionContextError: null });
    } catch (err) {
      set({
        sessionContextError:
          err instanceof Error ? err.message : "could not set the context",
      });
    }
  },

  loadSession: async (id) => {
    try {
      // v1.2.8: LAZY HISTORY PAGING — load the newest page instead of the
      // whole transcript. Older pages fetch on demand (loadOlderMessages);
      // the authoritative full session remains on the backend.
      //
      // v1.2.8.1: when reloading the session the user is ALREADY reading
      // (e.g. run finalisation reloads after `done`), the fetched page
      // covers everything currently visible so the older pages the user
      // expanded are not silently collapsed back to the newest page.
      // The backend page cap (200) bounds the request.
      const prev = useRuntimeStore.getState();
      const preservePages =
        prev.activeSessionId === id && prev.messages.length > 60;
      const limit = preservePages
        ? Math.min(200, Math.max(60, prev.messages.length + 1))
        : 60;

      const page = await api.sessionMessagesPage(id, undefined, limit);

      // Only apply if the session is still the active one.
      if (useRuntimeStore.getState().activeSessionId !== id) {
        return;
      }

      set({
        messages: page.messages.map((entry) => entry.message),
        olderHasMore: page.hasMore,
        olderNextBefore: page.hasMore ? page.nextBefore : null,
      });
    } catch {
      // Session detail unavailable (fresh session not yet persisted) —
      // an empty conversation is the correct view.
      if (useRuntimeStore.getState().activeSessionId === id) {
        set({ messages: [], olderHasMore: false, olderNextBefore: null });
      }
    }

    // v1.2.8: restore the attached cross-mode history references with
    // the session (best-effort — a session without refs stays empty).
    try {
      const detail = await api.sessionDetail(id);

      if (useRuntimeStore.getState().activeSessionId === id) {
        set({
          historyRefs: normalizeHistoryRefs(
            detail.context?.historyRefs,
            id,
          ),
        });
      }
    } catch {
      // Refs are optional metadata; silence is correct here.
    }
  },

  refreshLab: async () => {
    set({
      labLoading: true,
      labError: null,
    });

    try {
      const lab = await api.lab();

      const activeLabTaskId = get().activeLabTaskId;

      const activeLabTask = activeLabTaskId
        ? (lab.tasks.find((item) => item.id === activeLabTaskId) ?? null)
        : null;

      set({
        lab,
        labLoading: false,
        activeLabTask,
      });
    } catch (error) {
      set({
        labLoading: false,
        labError:
          error instanceof Error
            ? error.message
            : "Failed to refresh Coding Lab.",
      });
    }
  },

  loadLabTask: async (id) => {
    const taskId = id.trim();

    if (!taskId) {
      set({
        activeLabTaskId: null,
        activeLabTask: null,
      });

      return;
    }

    set({
      activeLabTaskId: taskId,
      labLoading: true,
      labError: null,
    });

    try {
      const activeLabTask = await api.labTask(taskId);

      set({
        activeLabTask,
        labLoading: false,
      });
    } catch (error) {
      set({
        labLoading: false,
        labError:
          error instanceof Error
            ? error.message
            : "Failed to load Coding Lab task.",
      });
    }
  },

  createSession: async () => {
    // v1.2.8: the new session is created in the CURRENT conversation
    // space and becomes that space's active session.
    const session = await api.createSession(get().mode);

    // v1.1.4: createSession previously only prepended the session and
    // switched the id — the socket stayed bound to the OLD session (the
    // stale-guard then silently discarded every event for the new one)
    // and messages/streaming/running were never reset. First message on
    // a fresh session never streamed and the composer stuck "running".
    get().disconnectActivity();

    // Phase 4: drop any pending streaming chunks for the OLD session.
    resetPendingStreaming();

    // v1.2.2: a fresh session ends any in-flight timeline for the old one.
    clearRunFinalizeTimer();
    runOutcome = null;
    runAssistantBaseline = null;
    runEventsReceived = false;
    resetRunReplayTracking(null);

    set((state) => ({
      sessions: [session, ...state.sessions],
      activeSessionId: session.id,
      activeSessionByMode: {
        ...state.activeSessionByMode,
        [state.mode]: session.id,
      },
      error: null,
      activity: [],
      messages: [],
      streaming: null,
      running: false,
      runPhase: "idle",
      runStartedAt: null,
      runNote: null,
      pendingAttachments: [],
      historyRefs: [],
      agentTask: null,
      olderHasMore: false,
      olderNextBefore: null,
    }));

    // v1.2.2: only a mounted consumer keeps the new session's socket.
    if (activityConsumers > 0) {
      get().connectActivity();
    }

    return session;
  },

  selectSession: (id) => {
    if (get().activeSessionId === id) {
      return;
    }

    get().disconnectActivity();

    // v1.2.2: switching sessions ends the previous run's timeline.
    clearRunFinalizeTimer();
    runOutcome = null;
    runAssistantBaseline = null;
    runEventsReceived = false;
    resetRunReplayTracking(null);

    set((state) => ({
      activeSessionId: id,
      // v1.2.8: remember the selection PER MODE so switching modes never
      // silently switches the active conversation of either space.
      activeSessionByMode: {
        ...state.activeSessionByMode,
        [state.mode]: id,
      },
      error: null,
      activity: [],
      messages: [],
      streaming: null,
      running: false,
      runPhase: "idle",
      runStartedAt: null,
      runNote: null,
      pendingAttachments: [],
      historyRefs: [],
      agentTask: null,
      olderHasMore: false,
      olderNextBefore: null,
    }));

    // Phase 4: drop any pending streaming chunks for the OLD session.
    resetPendingStreaming();

    if (id) {
      // v1.2.2: only a mounted consumer (Agent surface) keeps a live
      // socket — switching chapters from a background view must not
      // leak an ownerless connection.
      if (activityConsumers > 0) {
        get().connectActivity();
      }

      void get().loadSession(id);
      void get().refreshSessionContext();
    } else {
      set({ sessionContext: null, sessionContextError: null });
    }
  },

  deleteSession: async (id) => {
    await api.deleteSession(id);

    if (get().activeSessionId === id) {
      get().disconnectActivity();
    }

    set((state) => {
      const sessions = state.sessions.filter((session) => session.id !== id);

      // v1.2.8: the replacement selection stays INSIDE the current mode's
      // space (the list is already mode-filtered; this re-resolves).
      const activeSessionId = resolveActiveForMode(
        sessions,
        state.mode,
        state.activeSessionId === id ? null : state.activeSessionId,
      );

      return {
        sessions,
        activeSessionId,
        activeSessionByMode: {
          ...state.activeSessionByMode,
          [state.mode]: activeSessionId,
        },
        // v1.1.4: the deleted session's conversation previously stayed
        // on screen (and kept streaming state) until the next manual switch.
        messages: state.activeSessionId === id ? [] : state.messages,
        activity: state.activeSessionId === id ? [] : state.activity,
        streaming: null,
        running: false,
        runPhase: state.activeSessionId === id ? "idle" : state.runPhase,
        runStartedAt: state.activeSessionId === id ? null : state.runStartedAt,
        runNote: state.activeSessionId === id ? null : state.runNote,
      };
    });

    const nextId = get().activeSessionId;

    if (nextId) {
      // v1.2.2: same ownership guard as selectSession.
      if (activityConsumers > 0) {
        get().connectActivity();
      }

      void get().loadSession(nextId);
    }
  },

  run: async (message) => {
    // v1.2.8.1: a session is created LAZILY on the first message when the
    // current space has none (the documented v1.2.8 behavior — the
    // composer previously threw "No active session." instead).
    if (!get().activeSessionId) {
      try {
        await get().createSession();
      } catch {
        // fall through to the explicit error below when creation fails
      }
    }

    const sessionId = get().activeSessionId;

    if (!sessionId) {
      throw new Error("No active session.");
    }

    if (!message.trim()) {
      return;
    }

    // v1.2.2: reset this run's bookkeeping BEFORE the optimistic bubble —
    // the generation timeline becomes visible the instant Send is
    // accepted (no dead visual gap while the engine gate warms up).
    clearRunFinalizeTimer();
    runOutcome = null;
    runAssistantBaseline = null;
    runEventsReceived = false;
    resetPendingStreaming();
    resetRunReplayTracking(null);

    // v1.2.6 continuation: the DETERMINISTIC ATTACH CONTRACT — the POST
    // fires only after the server acknowledged this session's socket
    // (`attached` frame). The v1.2.6 code merely started the connection
    // (CONNECTING) and POSTed immediately: a fast run could finish before
    // the socket ever attached, leaving the recovery paths to paper over
    // the race. Awaiting the acknowledgement REMOVES the race. A rejected
    // acknowledgement (socket died) proceeds anyway — the runId replay
    // and the bounded grace re-check remain the safety net for older
    // backends and hard failures; neither is the primary mechanism here.
    get().connectActivity();

    try {
      await waitForActivityAttached();
    } catch {
      // No acknowledged transport — the run still POSTs; recovery paths
      // (run_snapshot replay, idle grace re-check) own the resync.
    }

    set({
      running: true,
      error: null,
      runPhase: "preparing",
      runStartedAt: Date.now(),
      runNote: null,
      // v1.2.5: fresh run — fresh status line and escalation trail.
      liveStatus: null,
      tierEscalations: [],
      // v1.2.8: fresh run — fresh task-state view.
      agentTask: null,
    });

    // v1.1.3: optimistic user bubble — the conversation shows the sent
    // message immediately, before any streaming event arrives.
    const attachmentNames = get().pendingAttachments.map((item) => item.name);

    set((state) => ({
      messages: [
        ...state.messages,
        {
          role: "user" as const,
          content: message.trim(),
          ...(attachmentNames.length > 0
            ? { attachments: attachmentNames }
            : {}),
        },
      ],
      streaming: null,
    }));

    const attachmentIds = get().pendingAttachments.map((item) => item.id);

    set({ pendingAttachments: [] });

    try {
      const state = get();

      const response = await api.run({
        sessionId,
        message: message.trim(),
        ...(attachmentIds.length > 0 ? { attachmentIds } : {}),
        // v1.2.5: the composer controls travel with the request — they
        // change the actual backend behaviour (tier posture, tool surface).
        ...(state.thinkingControl !== "auto"
          ? { thinking: state.thinkingControl }
          : {}),
        ...(state.toolPolicyMode === "manual"
          ? { toolMode: "manual", toolAllow: state.toolAllowlist }
          : {}),
        // v1.2.8: the attached cross-mode history references travel with
        // every request — the backend retrieves only the relevant portions
        // and labels provenance (the source sessions are never modified).
        ...(state.historyRefs.length > 0
          ? { historyRefs: state.historyRefs }
          : {}),
      });

      // v1.2.6 continuation: bind the UI to the authoritative runId the
      // POST returned — every subsequent frame of THIS run is accepted,
      // and stale drains of a replaced run are dropped (isStaleRunEvent).
      if (response.runId) {
        activeRunId = response.runId;
        lastRunSeq = 0;
      }

      await get().refreshSessions();
    } catch (error) {
      set({
        error: error instanceof Error ? error.message : "Agent run failed.",
        running: false,
        runPhase: "error",
        runNote:
          error instanceof Error ? error.message : "The run request failed.",
      });

      releaseIdleOwnedResources();

      throw error;
    }
  },

  regenerate: async () => {
    const sessionId = get().activeSessionId;

    if (!sessionId) {
      return;
    }

    if (get().running) {
      return;
    }

    // v1.2.2: same timeline reset as a fresh run.
    clearRunFinalizeTimer();
    runOutcome = null;
    runAssistantBaseline = null;
    runEventsReceived = false;
    resetPendingStreaming();
    resetRunReplayTracking(null);

    // v1.2.6 continuation: same deterministic attach contract as run().
    get().connectActivity();

    try {
      await waitForActivityAttached();
    } catch {
      // Proceed — the replay + grace re-check recover.
    }

    set({
      running: true,
      error: null,
      streaming: null,
      runPhase: "preparing",
      runStartedAt: Date.now(),
      runNote: null,
      liveStatus: null,
      tierEscalations: [],
      // v1.2.8.1: a regenerated run is a FRESH timeline — same parity as
      // run(); the previous run's task panel must not linger.
      agentTask: null,
    });

    try {
      const state = get();

      const response = await api.run({
        sessionId,
        message: "",
        regenerate: true,
        ...(state.thinkingControl !== "auto"
          ? { thinking: state.thinkingControl }
          : {}),
        ...(state.toolPolicyMode === "manual"
          ? { toolMode: "manual", toolAllow: state.toolAllowlist }
          : {}),
        // v1.2.8.1 REPAIR: the regenerated turn runs with the SAME
        // cross-mode history references as the original run. The payload
        // previously omitted historyRefs entirely and the backend had no
        // fallback, so regeneration silently lost every attached history
        // context. (The backend now also falls back to the persisted
        // session context — this is the explicit belt to that brace.)
        ...(state.historyRefs.length > 0
          ? { historyRefs: state.historyRefs }
          : {}),
      });

      if (response.runId) {
        activeRunId = response.runId;
        lastRunSeq = 0;
      }

      // Drop the trailing assistant bubble optimistically; the reload on
      // done restores the authoritative history.
      set((state) => {
        const messages = [...state.messages];

        for (;;) {
          const last = messages[messages.length - 1];

          if (
            messages.length > 0 &&
            last &&
            (last.role === "assistant" || last.role === "tool")
          ) {
            messages.pop();

            continue;
          }

          break;
        }

        return { messages };
      });

      await get().refreshSessions();
    } catch (error) {
      set({
        error: error instanceof Error ? error.message : "Regenerate failed.",
        running: false,
        runPhase: "error",
        runNote:
          error instanceof Error
            ? error.message
            : "The regenerate request failed.",
      });

      releaseIdleOwnedResources();

      throw error;
    }
  },

  uploadFiles: async (files) => {
    const sessionId = get().activeSessionId;

    if (!sessionId || files.length === 0) {
      return;
    }

    set({ attachmentsUploading: true, error: null });

    try {
      const response = await api.uploadAttachments(sessionId, files);

      set((state) => ({
        pendingAttachments: [
          ...state.pendingAttachments,
          ...response.attachments,
        ],
        attachmentsUploading: false,
      }));

      if (response.failed.length > 0) {
        set({
          error: response.failed
            .map((item) => `${item.name}: ${item.error}`)
            .join("; "),
        });
      }
    } catch (error) {
      set({
        attachmentsUploading: false,
        error: error instanceof Error ? error.message : "Upload failed.",
      });

      throw error;
    }
  },

  removePendingAttachment: async (id) => {
    set((state) => ({
      pendingAttachments: state.pendingAttachments.filter(
        (item) => item.id !== id,
      ),
    }));

    try {
      await api.deleteAttachment(id);
    } catch {
      // The staged file will be cleaned with the store; removing it from
      // the composer is the user-visible contract and must not fail.
    }
  },

  sendFeedback: async (query, liked) => {
    const sessionId = get().activeSessionId;

    if (!sessionId || !query.trim()) {
      return;
    }

    await api.feedback({
      sessionId,
      query,
      liked,
    });
  },

  abort: async () => {
    const sessionId = get().activeSessionId;

    if (!sessionId) {
      set({
        running: false,
        runPhase: "aborted",
      });

      releaseIdleOwnedResources();

      return;
    }

    // v1.2.2: unlock the composer immediately; the phase stays live so
    // the generation timeline keeps showing the partial output until the
    // backend's `done`(abort) event — or a bounded fallback — finalises
    // the run with the partial preserved.
    flushStreaming();

    useRuntimeStore.setState({ running: false });

    try {
      await api.abort(sessionId);
    } finally {
      // Fallback finalisation: if the backend's done(abort) event never
      // arrives (detached socket, engine hiccup), settle the run locally
      // after a short grace period — never a stuck live phase.
      if (runOutcome === null) {
        runOutcome = "aborted";
      }

      const phase = useRuntimeStore.getState().runPhase;

      if (isLivePhase(phase)) {
        scheduleRunFinalisation(sessionId, 1200);
      }
    }
  },

  runLabAction: async (payload) => {
    set({
      labLoading: true,
      labError: null,
    });

    try {
      const response = await api.labAction(payload);

      if (!response.ok) {
        throw new Error(response.error || "Coding Lab action failed.");
      }

      await get().refreshLab();

      const activeTaskId = get().activeLabTaskId;

      if (activeTaskId) {
        await get().loadLabTask(activeTaskId);
      }

      return response.result;
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Coding Lab action failed.";

      set({
        labLoading: false,
        labError: message,
      });

      // v1.1.4: no rethrow — the labError state IS the user-facing
      // failure surface. The previous `throw` escaped every fire-and-forget
      // call site (LabPanel's `void onAction(...)`) as an unhandled
      // promise rejection.
    }
  },

  loadResearchConfig: async () => {
    set({
      researchError: null,
    });

    try {
      const researchConfig = await api.researchConfig();

      set({
        researchConfig,
      });
    } catch (error) {
      set({
        researchError:
          error instanceof Error
            ? error.message
            : "Failed to load research configuration.",
      });
    }
  },

  searchResearch: async (payload) => {
    const query = payload.query.trim();

    if (!query) {
      throw new Error("Research query is required.");
    }

    set({
      researchLoading: true,
      researchError: null,
    });

    try {
      const research = await api.research({
        ...payload,
        query,
      });

      set({
        research,
        researchLoading: false,
      });

      return research;
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Research request failed.";

      set({
        researchLoading: false,
        researchError: message,
      });

      // v1.1.4: no rethrow (see runLabAction — the panel calls this
      // fire-and-forget; the rethrow was an unhandled rejection).
      return undefined;
    }
  },

  connectActivity: () => {
    const sessionId = get().activeSessionId;

    if (!sessionId) {
      return;
    }

    if (
      socket &&
      activitySessionId === sessionId &&
      (socket.readyState === WebSocket.OPEN ||
        socket.readyState === WebSocket.CONNECTING)
    ) {
      return;
    }

    resetPendingActivity();

    if (socket) {
      socket.close();
      socket = null;
    }

    clearReconnectTimer();
    reconnectAttempts = 0;

    activitySessionId = sessionId;

    set({
      connection: "connecting",
    });

    // v1.2.6 continuation: arm the deterministic attach acknowledgement —
    // resolved by the server's first `attached` frame (see ws.onmessage).
    beginActivityAttach();

    const ws = new WebSocket(activityWebSocketURL(sessionId));

    socket = ws;

    ws.onopen = () => {
      if (socket !== ws || activitySessionId !== get().activeSessionId) {
        return;
      }

      // v1.1.4: a successful (re)connection resets the backoff ladder.
      reconnectAttempts = 0;

      set({
        connection: "connected",
      });
    };

    ws.onmessage = (event) => {
      if (socket !== ws || activitySessionId !== get().activeSessionId) {
        return;
      }

      try {
        const payload = JSON.parse(event.data) as APIActivityEvent;

        // v1.2.6 continuation: the attach acknowledgement — the server
        // confirmed this socket is live and session-bound. Resolves the
        // deterministic attach contract (never queued as activity).
        if ((payload as Record<string, unknown>).type === "attached") {
          settleActivityAttach();
          return;
        }

        // v1.2.6 continuation: the AUTHORITATIVE RUN SNAPSHOT — sent on
        // every attachment (mid-run reconnect included). Replays the
        // cumulative response/reasoning snapshots and the terminal state
        // without waiting for a future token.
        if ((payload as Record<string, unknown>).type === "run_snapshot") {
          handleRunSnapshot(payload as unknown as Record<string, unknown>);
          return;
        }

        // Stale-run filter: drop drains of a replaced run and
        // duplicate/replayed sequences before they reach the pipeline.
        if (isStaleRunEvent(payload as Record<string, unknown>)) {
          return;
        }

        queueActivity(normalizeActivity(payload));
      } catch {
        // Ignore malformed activity frames.
      }
    };

    ws.onerror = () => {
      if (socket !== ws || activitySessionId !== get().activeSessionId) {
        return;
      }

      set({
        connection: "error",
      });
    };

    ws.onclose = () => {
      // The socket died — release anyone waiting on the deterministic
      // attach contract FIRST, unconditionally: the deferred belongs to
      // whichever socket was current, and a new connectActivity() always
      // arms a fresh one (failActivityAttach is a no-op when already
      // settled).
      failActivityAttach();

      if (socket !== ws || activitySessionId !== get().activeSessionId) {
        return;
      }

      socket = null;

      set({
        connection: "disconnected",
      });

      // v1.1.4: auto-reconnect while the session is still active. Without
      // this, ANY mid-run drop (backend restart, transient network blip)
      // permanently killed event delivery — `running` could never clear.
      if (reconnectAttempts >= RECONNECT_MAX_ATTEMPTS) {
        return;
      }

      const delay = Math.min(
        RECONNECT_BASE_DELAY_MS * 2 ** reconnectAttempts,
        RECONNECT_MAX_DELAY_MS,
      );

      reconnectAttempts += 1;

      reconnectTimer = window.setTimeout(() => {
        reconnectTimer = null;

        const current = useRuntimeStore.getState();

        if (
          current.activeSessionId &&
          current.activeSessionId === activitySessionId
        ) {
          current.connectActivity();
        }
      }, delay);
    };
  },

  disconnectActivity: () => {
    activitySessionId = null;

    // v1.2.6 continuation: dropping the socket must also release anyone
    // waiting on its attach acknowledgement (ws.onclose fires only for a
    // live transport; an already-null socket leaves the deferred armed).
    failActivityAttach();

    resetPendingActivity();

    clearReconnectTimer();
    reconnectAttempts = 0;

    if (socket) {
      socket.close();
      socket = null;
    }

    set({
      connection: "idle",
    });
  },

  clearActivity: () => {
    resetPendingActivity();

    set({
      activity: [],
    });
  },

  // v1.2.8: MODE-SEPARATED history switching. The old setMode flipped a
  // display flag over ONE shared transcript; two spaces that share one
  // active conversation are NOT two histories. Now:
  //   - the current mode's active session is remembered in
  //     activeSessionByMode (never lost, persisted across reloads);
  //   - v1.2.8.1: the OTHER mode's remembered session IS the active one
  //     (resolved by setMode from the per-mode memory — never from the
  //     stale session list of the previous mode), and its transcript is
  //     loaded immediately; a fresh session is created lazily on the
  //     first message when the space has none;
  //   - the run timeline resets for the newly active session: the backend
  //     run of the previous session (if any) KEEPS RUNNING server-side and
  //     reconnecting to it later replays its authoritative snapshot —
  //     switching the visible space never restarts or cancels a run.
  mode: bootMode,
  activeSessionId: bootSessions[bootMode],
  activeSessionByMode: bootSessions,
  historyRefs: [],
  historyPickerOpen: false,
  historyHits: [],
  historySearchLoading: false,
  // v1.2.8.1: the cross-mode picker defaults to the OTHER space — the
  // whole point of the picker is attaching history from the other mode;
  // the backend now rejects same-mode references outright.
  historySearchMode: crossModePickerFilter(bootMode),
  agentTask: null,
  olderHasMore: false,
  olderLoading: false,
  olderNextBefore: null,
  setMode: (mode) => {
    const state = get();

    if (state.mode === mode) {
      persistWorkspaceMode(mode);
      return;
    }

    // Remember where the CURRENT space was (so returning restores it).
    const remembered: Record<WorkspaceMode, string | null> = {
      ...state.activeSessionByMode,
      [state.mode]: state.activeSessionId,
    };

    // A run live in the current session keeps running on the backend;
    // this UI switch only changes the visible space.
    get().disconnectActivity();
    clearRunFinalizeTimer();
    runOutcome = null;
    runAssistantBaseline = null;
    runEventsReceived = false;
    resetRunReplayTracking(null);
    resetPendingStreaming();

    persistWorkspaceMode(mode);

    // v1.2.8.1 REPAIR (the v1.2.8 empty-conversation regression): the
    // target mode's active session is the PER-MODE MEMORY, not a filter
    // over the current `sessions` array. Since v1.2.8 the list is always
    // single-mode (the CURRENT mode's), so the old filter produced an
    // empty subset for the TARGET mode and nextActive was ALWAYS null —
    // no transcript load, no activity socket, no context policy. The
    // remembered id is validated against the (refreshed) list
    // asynchronously; refreshSessions re-resolves and loads if the
    // remembered session no longer exists.
    const nextActive = resolveModeSwitchTarget(remembered, mode);

    set({
      mode,
      activeSessionId: nextActive,
      activeSessionByMode: remembered,
      error: null,
      activity: [],
      messages: [],
      streaming: null,
      running: false,
      runPhase: "idle",
      runStartedAt: null,
      runNote: null,
      liveStatus: null,
      tierEscalations: [],
      pendingAttachments: [],
      historyRefs: [],
      agentTask: null,
      olderHasMore: false,
      olderNextBefore: null,
      sessionContext: null,
      sessionContextError: null,
      // v1.2.8.1: the picker retargets the new OTHER space.
      historySearchMode: crossModePickerFilter(mode),
    });

    // Load the target space's list (it also re-validates the selection).
    void get().refreshSessions();

    if (nextActive) {
      // v1.2.8.1: reconnect the run transport for the RETURNED session —
      // a live run in this space replays its authoritative snapshot here
      // (the backend run was never cancelled by the mode switch).
      if (activityConsumers > 0) {
        get().connectActivity();
      }

      void get().loadSession(nextActive);
      void get().refreshSessionContext();
    }
  },

  attachHistoryRefs: async (refs) => {
    const state = get();

    const sessionId = state.activeSessionId;
    if (!sessionId) {
      return;
    }

    const merged = mergeHistoryRefs(state.historyRefs, refs, sessionId);

    set({ historyRefs: merged, historyPickerOpen: false });

    // Persist on the session context (mirrors the staged attachment ids):
    // the references survive reloads and travel with later turns.
    // v1.2.8.1: the delta goes through the dedicated `historyRefs` PUT
    // field, applied ATOMICALLY server-side under the store lock — the
    // previous GET-whole-context-modify-PUT could silently revert a
    // concurrent context update (token policy, attachment association).
    try {
      await api.updateSession(sessionId, { historyRefs: merged });
    } catch (error) {
      set({
        error:
          error instanceof Error
            ? error.message
            : "Failed to persist the attached history references.",
      });
    }
  },

  detachHistoryRef: async (refSessionId) => {
    const state = get();

    const sessionId = state.activeSessionId;
    if (!sessionId) {
      return;
    }

    const next = removeHistoryRefById(state.historyRefs, refSessionId);

    set({ historyRefs: next });

    // v1.2.8.1: atomic `historyRefs` delta (see attachHistoryRefs).
    try {
      await api.updateSession(sessionId, { historyRefs: next });
    } catch (error) {
      set({
        error:
          error instanceof Error
            ? error.message
            : "Failed to update the attached history references.",
      });
    }
  },

  setHistoryPickerOpen: (open) => {
    set({ historyPickerOpen: open });

    if (open) {
      // Seed the picker with the newest sessions across BOTH spaces.
      void get().searchHistory("");
    }
  },

  setHistorySearchMode: (filter) => {
    set({ historySearchMode: filter });

    void get().searchHistory("");
  },

  searchHistory: async (q) => {
    const mode = get().historySearchMode;

    set({ historySearchLoading: true });

    try {
      const hits = await api.historySearch(
        q,
        mode === "all" ? undefined : mode,
        12,
      );

      set({ historyHits: hits, historySearchLoading: false });
    } catch (error) {
      set({
        historySearchLoading: false,
        historyHits: [],
        error:
          error instanceof Error
            ? error.message
            : "Failed to search history.",
      });
    }
  },

  loadOlderMessages: async () => {
    const state = get();

    const sessionId = state.activeSessionId;

    if (!sessionId || !state.olderHasMore || state.olderLoading) {
      return;
    }

    const before = state.olderNextBefore;
    if (before === null || before <= 0) {
      set({ olderHasMore: false, olderNextBefore: null });
      return;
    }

    set({ olderLoading: true });

    try {
      const page = await api.sessionMessagesPage(sessionId, before);

      // Only apply if the session is still the active one (a switch must
      // never splice pages of another conversation).
      if (useRuntimeStore.getState().activeSessionId !== sessionId) {
        // v1.2.8.1: ALWAYS clear the loading flag — the previous early
        // return leaked `olderLoading: true`, which permanently disabled
        // the "Load earlier" button (also for the session the user
        // switched TO, since the flag is store-global).
        set({ olderLoading: false });
        return;
      }

      set((current) => ({
        messages: [
          ...page.messages.map((entry) => entry.message),
          ...current.messages,
        ],
        olderHasMore: page.hasMore,
        olderNextBefore: page.hasMore ? page.nextBefore : null,
        olderLoading: false,
      }));
    } catch {
      // v1.2.8.1: clear unconditionally — a failed page load for a session
      // the user already left must not wedge the NEW session's button.
      set({ olderLoading: false });
    }
  },

  renameSession: async (id, title) => {
    const clean = title.trim();

    if (!clean) {
      return;
    }

    try {
      const updated = await api.updateSession(id, { title: clean });

      set((state) => ({
        sessions: state.sessions.map((session) =>
          session.id === id ? { ...session, title: updated.title } : session,
        ),
      }));
    } catch (error) {
      set({
        error:
          error instanceof Error ? error.message : "Failed to rename session.",
      });
    }
  },
}));

export function getRuntimeState(): RuntimeState {
  return useRuntimeStore.getState();
}

// v1.2.8.1: the per-mode active-session map persists on every change
// (mode switches, selections, creations, deletions, re-resolutions) — one
// subscription instead of a persist call at every mutation site.
useRuntimeStore.subscribe((state, prevState) => {
  if (state.activeSessionByMode !== prevState.activeSessionByMode) {
    persistActiveSessionByMode(state.activeSessionByMode);
  }
});
