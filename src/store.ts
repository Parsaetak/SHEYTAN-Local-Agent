import { create } from "zustand";

import {
  api,
  type ActivityEvent as APIActivityEvent,
  type AppState,
  type Attachment,
  type ChatMessage,
  type EngineSnapshot,
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
import { activityWebSocketURL } from "./config";

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
  presets: Preset[];
  tools: ToolInfo[];

  sessions: Session[];
  activeSessionId: string | null;

  connection: ConnectionState;
  loading: boolean;
  error: string | null;

  activity: ActivityEvent[];
  running: boolean;

  // v1.1.3Z: authoritative engine state (polled + WS-pushed).
  engine: EngineSnapshot | null;

  // v1.1.6: backend-resolved context status for the active session
  // (selector options, usage, pressure). Backend is authoritative.
  sessionContext: SessionContextStatus | null;
  sessionContextError: string | null;
  setSessionContext: (tokens: number) => Promise<void>;
  refreshSessionContext: () => Promise<void>;

  // v1.1.3Z: real conversation history for the active session plus the
  // streaming assistant bubble.
  messages: ChatMessage[];
  streaming: { content: string; reasoning: string } | null;

  // v1.1.3Z: staged attachments for the composer.
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
  // v1.1.4Z: the engine poll previously ran for the app's LIFETIME once
  // started (no stop function existed) — even on other views.
  stopEnginePolling: () => void;

  // v1.1.4Z: recall feedback (thumbs up/down on past exchanges) — the
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
};

const MAX_ACTIVITY_EVENTS = 500;

let enginePollTimer: number | null = null;

let socket: WebSocket | null = null;
let activitySequence = 0;
let activitySessionId: string | null = null;

// v1.1.4Z: automatic WebSocket reconnection. The old store gave up on the
// first close — a mid-run disconnect left `running` stuck true forever (the
// dead-composer bug's last live variant: no `done` event could ever arrive).
let reconnectTimer: number | null = null;
let reconnectAttempts = 0;

const RECONNECT_BASE_DELAY_MS = 1500;
const RECONNECT_MAX_DELAY_MS = 15000;
const RECONNECT_MAX_ATTEMPTS = 20;

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
let streamingFlushFrame: number | null = null;
let pendingStreamingContent = "";
let pendingStreamingReasoning = "";
let pendingStreamingHadContent = false;
let pendingStreamingHadReasoning = false;

function resetPendingStreaming(): void {
  if (streamingFlushFrame !== null) {
    cancelAnimationFrame(streamingFlushFrame);

    streamingFlushFrame = null;
  }

  pendingStreamingContent = "";
  pendingStreamingReasoning = "";
  pendingStreamingHadContent = false;
  pendingStreamingHadReasoning = false;
}

// flushStreaming writes the accumulated content/reasoning to the store
// in ONE setState, then resets the buffers. Runs on a rAF boundary so
// multiple token chunks arriving within one frame coalesce into a
// single render.
function flushStreaming(): void {
  streamingFlushFrame = null;

  if (!pendingStreamingHadContent && !pendingStreamingHadReasoning) {
    return;
  }

  // Read the current streaming state ONCE (cheap; no re-render), merge
  // the pending deltas, and write back in a single setState.
  const current = useRuntimeStore.getState().streaming;

  const nextContent = pendingStreamingHadContent
    ? (current?.content ?? "") + pendingStreamingContent
    : (current?.content ?? "");

  const nextReasoning = pendingStreamingHadReasoning
    ? (current?.reasoning ?? "") + pendingStreamingReasoning
    : (current?.reasoning ?? "");

  useRuntimeStore.setState({
    streaming: {
      content: nextContent,
      reasoning: nextReasoning,
    },
  });

  // Phase 4 perf HUD: count this as one coalesced stream update so the
  // HUD can measure update frequency (should be <= display refresh rate,
  // never one-per-token). The recordStreamUpdate import is dynamic so
  // this file stays decoupled from perf-hud.ts when the HUD is disabled.
  recordStreamUpdateSafe();

  pendingStreamingContent = "";
  pendingStreamingReasoning = "";
  pendingStreamingHadContent = false;
  pendingStreamingHadReasoning = false;
}

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

// queueStreamingContent appends one response chunk to the content buffer
// and schedules a frame-aligned flush.
function queueStreamingContent(chunk: string): void {
  if (!chunk) return;

  pendingStreamingContent += chunk;
  pendingStreamingHadContent = true;
  scheduleStreamingFlush();
}

// queueStreamingReasoning appends one reasoning chunk to the reasoning
// buffer and schedules a frame-aligned flush.
function queueStreamingReasoning(chunk: string): void {
  if (!chunk) return;

  pendingStreamingReasoning += chunk;
  pendingStreamingHadReasoning = true;
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

function resolveActiveSessionID(
  sessions: Session[],
  currentID: string | null,
): string | null {
  if (currentID && sessions.some((session) => session.id === currentID)) {
    return currentID;
  }

  return sessions[0]?.id ?? null;
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

  // v1.1.3Z: route conversation-relevant events into the message pipeline
  // (streaming bubbles + the run-end bookkeeping that used to leave the
  // composer permanently disabled after one message).
  for (const event of batch) {
    handleConversationEvent(event);
  }
}

// handleConversationEvent mirrors activity stream events into the real
// conversation view and repairs the run state machine.
//
// Phase 4: streaming response/reasoning chunks are COALESCED through
// queueStreamingContent / queueStreamingReasoning and flushed on a rAF
// boundary. This means a model emitting 200 tokens/sec no longer
// triggers 200 React renders/sec — the UI updates at most once per
// frame, naturally capped by the display's refresh rate. Lifecycle
// events (done/error/session) bypass the coalescer and reset state
// immediately so `running` clears without delay.
function handleConversationEvent(event: ActivityEvent): void {
  switch (event.type) {
    case "response": {
      const content =
        typeof event.data.caption === "string" ? event.data.caption : "";

      if (content) {
        queueStreamingContent(content);
      }

      break;
    }

    case "reasoning": {
      const reasoning =
        typeof event.data.caption === "string" ? event.data.caption : "";

      if (reasoning) {
        queueStreamingReasoning(reasoning);
      }

      break;
    }

    case "session": {
      // v1.1.4Z: Continuum chapter rollover — the backend distilled the
      // conversation into a fresh chapter session and tells the UI here.
      // Follow the thread into the new chapter automatically.
      const nextSessionId =
        typeof event.data.sessionId === "string" ? event.data.sessionId : "";
      const currentId = useRuntimeStore.getState().activeSessionId;

      if (nextSessionId && nextSessionId !== currentId) {
        // Drop any pending streaming chunks — the chapter is closing.
        resetPendingStreaming();

        useRuntimeStore.setState({ running: false, streaming: null });
        void useRuntimeStore.getState().selectSession(nextSessionId);
        void useRuntimeStore.getState().refreshSessions();
      }

      break;
    }

    case "done":
    case "error": {
      // THE v1.1.2Z dead-composer fix: a finished or failed run must
      // always release the composer. The old code only reset `running`
      // on error paths, so a successful reply left it disabled forever.
      //
      // Phase 4: flush any pending streaming chunks FIRST so the final
      // content is visible before the streaming bubble closes. Then
      // reset running + streaming.
      flushStreaming();

      useRuntimeStore.setState({ running: false });

      // Reload the persisted conversation so the final assistant message
      // (written by the run goroutine after the done event) replaces the
      // optimistic streaming bubble with the authoritative history.
      const sessionId = useRuntimeStore.getState().activeSessionId;

      if (event.type === "done" && sessionId) {
        window.setTimeout(() => {
          const current = useRuntimeStore.getState();

          if (current.activeSessionId === sessionId) {
            void current.loadSession(sessionId);
            // v1.1.6: the turn changed the session's context usage —
            // refresh the authoritative status (used/pressure).
            void current.refreshSessionContext();
          }
        }, 400);
      }

      useRuntimeStore.setState({ streaming: null });

      break;
    }

    default:
      break;
  }
}

export const useRuntimeStore = create<RuntimeState>((set, get) => ({
  app: null,
  sysinfo: null,
  models: null,
  presets: [],
  tools: [],

  sessions: [],
  activeSessionId: null,

  connection: "idle",
  loading: false,
  error: null,

  activity: [],
  running: false,

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
    try {
      const models = await api.models();

      set({ models });
    } catch (error) {
      set({
        error:
          error instanceof Error ? error.message : "Failed to refresh models.",
      });
    }
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
    try {
      const sessions = await api.sessions();

      const current = get().activeSessionId;

      const activeSessionId = resolveActiveSessionID(sessions, current);

      set({
        sessions,
        activeSessionId,
      });
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
      const detail = await api.sessionDetail(id);

      // Only apply if the session is still the active one.
      if (useRuntimeStore.getState().activeSessionId !== id) {
        return;
      }

      set({
        messages: detail.messages ?? [],
      });
    } catch {
      // Session detail unavailable (fresh session not yet persisted) —
      // an empty conversation is the correct view.
      if (useRuntimeStore.getState().activeSessionId === id) {
        set({ messages: [] });
      }
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
    const session = await api.createSession();

    // v1.1.4Z: createSession previously only prepended the session and
    // switched the id — the socket stayed bound to the OLD session (the
    // stale-guard then silently discarded every event for the new one)
    // and messages/streaming/running were never reset. First message on
    // a fresh session never streamed and the composer stuck "running".
    get().disconnectActivity();

    // Phase 4: drop any pending streaming chunks for the OLD session.
    resetPendingStreaming();

    set((state) => ({
      sessions: [session, ...state.sessions],
      activeSessionId: session.id,
      error: null,
      activity: [],
      messages: [],
      streaming: null,
      running: false,
      pendingAttachments: [],
    }));

    get().connectActivity();

    return session;
  },

  selectSession: (id) => {
    if (get().activeSessionId === id) {
      return;
    }

    get().disconnectActivity();

    set({
      activeSessionId: id,
      error: null,
      activity: [],
      messages: [],
      streaming: null,
      running: false,
      pendingAttachments: [],
    });

    // Phase 4: drop any pending streaming chunks for the OLD session.
    resetPendingStreaming();

    if (id) {
      get().connectActivity();
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

      const activeSessionId = resolveActiveSessionID(
        sessions,
        state.activeSessionId === id ? null : state.activeSessionId,
      );

      return {
        sessions,
        activeSessionId,
        // v1.1.4Z: the deleted session's conversation previously stayed
        // on screen (and kept streaming state) until the next manual switch.
        messages: state.activeSessionId === id ? [] : state.messages,
        activity: state.activeSessionId === id ? [] : state.activity,
        streaming: null,
        running: false,
      };
    });

    const nextId = get().activeSessionId;

    if (nextId) {
      get().connectActivity();
      void get().loadSession(nextId);
    }
  },

  run: async (message) => {
    const sessionId = get().activeSessionId;

    if (!sessionId) {
      throw new Error("No active session.");
    }

    if (!message.trim()) {
      return;
    }

    set({
      running: true,
      error: null,
    });

    // v1.1.3Z: optimistic user bubble — the conversation shows the sent
    // message immediately, before any streaming event arrives.
    const attachmentNames = get().pendingAttachments.map((item) => item.name);

    set((state) => ({
      messages: [
        ...state.messages,
        {
          role: "user" as const,
          content: message.trim(),
          ...(attachmentNames.length > 0 ? { attachments: attachmentNames } : {}),
        },
      ],
      streaming: null,
    }));

    const attachmentIds = get().pendingAttachments.map((item) => item.id);

    set({ pendingAttachments: [] });

    try {
      await api.run({
        sessionId,
        message: message.trim(),
        ...(attachmentIds.length > 0 ? { attachmentIds } : {}),
      });

      await get().refreshSessions();
    } catch (error) {
      set({
        error: error instanceof Error ? error.message : "Agent run failed.",
        running: false,
      });

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

    set({
      running: true,
      error: null,
      streaming: null,
    });

    try {
      await api.run({ sessionId, message: "", regenerate: true });

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
      });

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
        pendingAttachments: [...state.pendingAttachments, ...response.attachments],
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
      });

      return;
    }

    try {
      await api.abort(sessionId);
    } finally {
      set({
        running: false,
      });
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

      // v1.1.4Z: no rethrow — the labError state IS the user-facing
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

      // v1.1.4Z: no rethrow (see runLabAction — the panel calls this
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

    const ws = new WebSocket(activityWebSocketURL(sessionId));

    socket = ws;

    ws.onopen = () => {
      if (socket !== ws || activitySessionId !== get().activeSessionId) {
        return;
      }

      // v1.1.4Z: a successful (re)connection resets the backoff ladder.
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
      if (socket !== ws || activitySessionId !== get().activeSessionId) {
        return;
      }

      socket = null;

      set({
        connection: "disconnected",
      });

      // v1.1.4Z: auto-reconnect while the session is still active. Without
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

  // v1.1.8: mode switch — a UI-only concern, so it lives at the end of
  // the store with its localStorage persistence. Sessions, model state,
  // and the engine are untouched: Chat and Agent share one runtime.
  mode: initialWorkspaceMode(),
  setMode: (mode) => {
    persistWorkspaceMode(mode);
    set({ mode });
  },
}));

export function getRuntimeState(): RuntimeState {
  return useRuntimeStore.getState();
}
