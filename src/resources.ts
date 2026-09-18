// resources.ts — v1.2.6 shared frontend resource loader.
//
// ONE source of truth for the slow background resources Settings, System,
// Agent and Performance all need (models, presets, tools, sysinfo,
// environment, health). The v1.2.5 Settings panel fetched everything with
// ONE global Promise.all — the whole page waited for the slowest endpoint
// (the Windows hardware probe measured 5.4 s), and one failed endpoint
// failed the entire view.
//
// Contract per resource (independently):
//   state:   "unavailable" → never fetched yet
//            "loading"     → a fetch is in flight
//            "ready"       → fresh data (within TTL)
//            "stale"       → showing last-known-good while a refresh runs
//            "error"       → the last fetch failed (last-known-good stays)
//
// Guarantees:
//   - request deduplication (one in-flight fetch per key, every caller
//     awaits the SAME promise);
//   - TTL with stale-while-revalidate (fresh value served instantly,
//     background refresh when older than TTL);
//   - last-known-good retention — a failed refresh NEVER blanks a view
//     that already has data;
//   - AbortController — navigating away cancels the in-flight fetch;
//   - subscribe() — components observe one resource without re-fetching
//     it per mount (the cache is shared app-wide).
//
// Pure TypeScript, no store dependency — unit-testable without a browser.

export type ResourceState =
  | "unavailable"
  | "loading"
  | "ready"
  | "stale"
  | "error";

export type ResourceSnapshot<T> = {
  state: ResourceState;
  data: T | null;
  error: string | null;
  fetchedAt: number | null;
};

type Entry<T> = {
  data: T | null;
  fetchedAt: number | null;
  error: string | null;
  inFlight: Promise<T> | null;
  listeners: Set<(snapshot: ResourceSnapshot<T>) => void>;
};

// Default TTLs per resource class (ms). Tuned for local-loopback latency:
// these values trade freshness for near-zero request volume.
const DEFAULT_TTL_MS = 30_000;
const TTL_BY_KEY: Record<string, number> = {
  config: 15_000,
  models: 60_000,
  presets: 120_000,
  tools: 120_000,
  sysinfo: 120_000,
  environment: 60_000,
  health: 60_000,
  engine: 10_000,
};

function ttlFor(key: string): number {
  return TTL_BY_KEY[key] ?? DEFAULT_TTL_MS;
}

const entries = new Map<string, Entry<unknown>>();

function entry<T>(key: string): Entry<T> {
  let existing = entries.get(key) as Entry<T> | undefined;

  if (!existing) {
    existing = {
      data: null,
      fetchedAt: null,
      error: null,
      inFlight: null,
      listeners: new Set(),
    };

    entries.set(key, existing as Entry<unknown>);
  }

  return existing;
}

function snapshot<T>(key: string): ResourceSnapshot<T> {
  const e = entry<T>(key);

  if (e.data === null && e.fetchedAt === null) {
    if (e.inFlight) {
      return { state: "loading", data: null, error: null, fetchedAt: null };
    }

    return {
      state: e.error ? "error" : "unavailable",
      data: null,
      error: e.error,
      fetchedAt: null,
    };
  }

  if (e.inFlight) {
    return { state: "stale", data: e.data, error: null, fetchedAt: e.fetchedAt };
  }

  if (e.error && e.data !== null) {
    return { state: "stale", data: e.data, error: null, fetchedAt: e.fetchedAt };
  }

  if (e.error) {
    return { state: "error", data: null, error: e.error, fetchedAt: null };
  }

  return { state: "ready", data: e.data, error: null, fetchedAt: e.fetchedAt };
}

function notify<T>(key: string): void {
  const e = entry<T>(key);
  const snap = snapshot<T>(key);

  for (const listener of e.listeners) {
    try {
      listener(snap);
    } catch {
      // A broken listener must never break the resource layer.
    }
  }
}

/**
 * ensure starts (or joins) the fetch for one resource and resolves with
 * the freshest data available after it settles.
 *
 * - no cached data → always fetch (deduplicated);
 * - cached data still inside its TTL → resolve cached, NO network;
 * - cached data older than TTL → resolve cached immediately (stale) and
 *   refresh in the background (stale-while-revalidate).
 */
export function ensure<T>(
  key: string,
  fetcher: (signal: AbortSignal) => Promise<T>,
  options?: { force?: boolean },
): Promise<T | null> {
  const e = entry<T>(key);

  if (e.inFlight) {
    return e.inFlight.then(
      () => e.data,
      () => e.data,
    );
  }

  const fresh =
    e.fetchedAt !== null && Date.now() - e.fetchedAt < ttlFor(key);

  if (e.data !== null && fresh && !options?.force) {
    return Promise.resolve(e.data);
  }

  const controller = new AbortController();
  const promise = new Promise<T>((resolve, reject) => {
    fetcher(controller.signal).then(resolve, reject);
  }).then(
    (data) => {
      e.data = data;
      e.fetchedAt = Date.now();
      e.error = null;
      e.inFlight = null;
      notify(key);
      return data;
    },
    (error: unknown) => {
      // A failed refresh keeps the last-known-good data (stale), or
      // records the error when nothing was ever fetched.
      e.error =
        error instanceof Error ? error.message : "Resource unavailable.";
      e.inFlight = null;
      notify(key);

      if (e.data !== null) {
        return e.data;
      }

      throw error;
    },
  );

  e.inFlight = promise;
  notify(key);

  return promise.then(
    (data) => data,
    () => e.data,
  );
}

/**
 * get returns the current snapshot WITHOUT starting a fetch.
 */
export function get<T>(key: string): ResourceSnapshot<T> {
  return snapshot<T>(key);
}

/**
 * subscribe observes one resource; the listener fires on every state
 * change. Returns the unsubscribe function.
 */
export function subscribe<T>(
  key: string,
  listener: (snapshot: ResourceSnapshot<T>) => void,
): () => void {
  const e = entry<T>(key);

  e.listeners.add(listener);

  try {
    listener(snapshot<T>(key));
  } catch {
    // Initial emission errors are the listener's problem.
  }

  return () => {
    e.listeners.delete(listener);
  };
}

/**
 * invalidate marks one resource's cached data as expired so the next
 * ensure() refreshes it.
 */
export function invalidate(key: string): void {
  const e = entry<unknown>(key);

  if (e) {
    e.fetchedAt = null;
  }
}

/**
 * reset drops one resource entirely (used after a settings save that
 * changes the resource's server-side shape).
 */
export function reset(key: string): void {
  entries.delete(key);
}
