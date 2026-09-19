import { api, type Session } from "./api";
import { useRuntimeStore } from "./store";
import {
  resolveActiveForMode,
  sessionMode,
  type WorkspaceMode,
} from "./mode-sessions";

const INITIALIZATION_TTL_MS = 30_000;

let initializationPromise: Promise<void> | null = null;

let initializedAt = 0;

/**
 * v1.2.8: resolve the active session WITHIN one conversation space. The
 * remembered selection only survives when it still exists in THAT space —
 * Chat and Agent histories are independent and never cross-select.
 */
function resolveActiveInSpace(
  sessions: Session[],
  mode: WorkspaceMode,
  currentID: string | null,
): string | null {
  const inSpace = sessions.filter((session) => sessionMode(session) === mode);

  return resolveActiveForMode(inSpace, mode, currentID);
}

async function initializeAgentOnce(): Promise<void> {
  useRuntimeStore.setState({
    loading: true,
    error: null,
  });

  try {
    const mode = useRuntimeStore.getState().mode;

    let [app, sessions] = await Promise.all([
      api.state(),
      api.sessions(mode),
    ]);

    // AAA polish (v1.1.2Z): a fresh install starts with zero sessions, which
    // left the runtime status on "Offline" and the composer inert — the app
    // LOOKED broken on first launch. Create the initial session eagerly so
    // the workspace is immediately live: WebSocket connects, activity
    // streams, and the composer is usable from the first paint.
    //
    // v1.2.8: the eager session is created in the CURRENT conversation
    // space (mode-separated histories).
    if (sessions.length === 0) {
      const session = await api.createSession(mode);
      sessions = [session];
    }

    const current = useRuntimeStore.getState().activeSessionId;

    const activeSessionId = resolveActiveInSpace(sessions, mode, current);

    useRuntimeStore.setState({
      app,
      sessions,
      activeSessionId,
      activeSessionByMode: {
        ...useRuntimeStore.getState().activeSessionByMode,
        [mode]: activeSessionId,
      },
      loading: false,
    });

    void useRuntimeStore.getState().refreshModels();
  } catch (error) {
    useRuntimeStore.setState({
      loading: false,
      error:
        error instanceof Error ? error.message : "Failed to initialize Agent.",
    });

    throw error;
  }
}

export function initializeAgent(): Promise<void> {
  if (initializationPromise) {
    return initializationPromise;
  }

  const now = Date.now();

  if (initializedAt > 0 && now - initializedAt < INITIALIZATION_TTL_MS) {
    return Promise.resolve();
  }

  initializationPromise = initializeAgentOnce()
    .then(() => {
      initializedAt = Date.now();
    })
    .catch((error) => {
      initializedAt = 0;
      throw error;
    })
    .finally(() => {
      initializationPromise = null;
    });

  return initializationPromise;
}
