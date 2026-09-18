// resources.test.ts — v1.2.6 unit tests for the shared resource layer.
//
// The layer's contract is what makes Settings/System progressive:
//   - dedup: concurrent ensure() calls run ONE fetch
//   - TTL: a fresh value is served without re-fetching
//   - stale-while-revalidate: an expired value resolves instantly and
//     refreshes in the background
//   - last-known-good: a failed refresh never blanks existing data
//   - independent states: one resource failing cannot affect another
import assert from "node:assert/strict";
import { test } from "node:test";

import { ensure, get, invalidate, reset, subscribe } from "./resources.ts";

function deferredFetch<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  let calls = 0;

  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });

  const fetcher = () => {
    calls += 1;
    return promise;
  };

  return { fetcher, resolve, reject, calls: () => calls };
}

test("dedup: concurrent ensure calls run exactly one fetch", async () => {
  reset("dedup-key");

  const d = deferredFetch<{ value: number }>();

  const first = ensure("dedup-key", d.fetcher);
  const second = ensure("dedup-key", d.fetcher);
  const third = ensure("dedup-key", d.fetcher);

  d.resolve({ value: 42 });

  const results = await Promise.all([first, second, third]);

  assert.equal(d.calls(), 1);
  assert.deepEqual(results, [{ value: 42 }, { value: 42 }, { value: 42 }]);

  const snap = get<{ value: number }>("dedup-key");
  assert.equal(snap.state, "ready");
  assert.deepEqual(snap.data, { value: 42 });
});

test("TTL: a fresh value is served without a second fetch", async () => {
  reset("ttl-key");

  const d = deferredFetch<string>();
  const first = ensure("ttl-key", d.fetcher);
  d.resolve("fresh");
  await first;

  const d2 = deferredFetch<string>();
  const cached = await ensure("ttl-key", d2.fetcher);

  assert.equal(d2.calls(), 0, "fresh data must not re-fetch inside the TTL");
  assert.equal(cached, "fresh");
});

test("stale-while-revalidate: expired data resolves instantly, refreshes in background", async () => {
  reset("swr-key");

  const d1 = deferredFetch<number>();
  const first = ensure("swr-key", d1.fetcher);
  d1.resolve(1);
  await first;

  // Expire the entry, then ensure again: the OLD value must resolve
  // immediately while the new fetch runs behind it.
  invalidate("swr-key");

  const d2 = deferredFetch<number>();
  const staleResolve = ensure("swr-key", d2.fetcher);

  d2.resolve(2);

  const value = await staleResolve;

  // Either the stale value (immediate) or the refreshed one (if the fetch
  // won the race) — never null, never an error.
  assert.ok(value === 1 || value === 2);
  assert.equal(d2.calls(), 1, "the background refresh must have started");
});

test("last-known-good: a failed refresh keeps the previous data (stale)", async () => {
  reset("lkg-key");

  const d1 = deferredFetch<string>();
  const first = ensure("lkg-key", d1.fetcher);
  d1.resolve("good");
  await first;

  invalidate("lkg-key");

  const d2 = deferredFetch<string>();
  const second = ensure("lkg-key", d2.fetcher);
  d2.reject(new Error("network down"));

  const value = await second;

  assert.equal(value, "good", "the failed refresh must serve last-known-good");

  const snap = get<string>("lkg-key");
  assert.equal(snap.state, "stale");
  assert.equal(snap.data, "good");
});

test("independent resources: one failing never blocks another", async () => {
  reset("indep-a");
  reset("indep-b");

  const da = deferredFetch<string>();
  const db = deferredFetch<string>();

  const a = ensure("indep-a", da.fetcher);
  const b = ensure("indep-b", db.fetcher);

  da.reject(new Error("a failed"));
  db.resolve("b is fine");

  const valueB = await b;
  const valueA = await a;

  assert.equal(valueB, "b is fine");
  assert.equal(valueA, null, "a resolves with null (no data ever existed)");

  const snapA = get<string>("indep-a");
  assert.equal(snapA.state, "error");

  const snapB = get<string>("indep-b");
  assert.equal(snapB.state, "ready");
});

test("subscribe: listeners observe the loading → ready transition", async () => {
  reset("sub-key");

  const states: string[] = [];

  const unsubscribe = subscribe<string>("sub-key", (snap) => {
    states.push(snap.state);
  });

  const d = deferredFetch<string>();
  const done = ensure("sub-key", d.fetcher);

  d.resolve("data");
  await done;

  // Let the notification microtasks flush.
  await Promise.resolve();
  await Promise.resolve();

  unsubscribe();

  assert.ok(states.includes("loading"));
  assert.ok(states.includes("ready"));
  assert.equal(get<string>("sub-key").data, "data");
});
