// stream-flush-scheduler.test.ts — deterministic proofs for the v1.8.2
// dual-boundary streaming flush (pure module, no browser, no sleeps).
//
// Pinned contracts:
//   - schedule() arms exactly ONE flush across both boundaries;
//   - the first boundary to run flushes; the second is a no-op;
//   - folds arriving while a flush is pending coalesce (schedule() is
//     idempotent while pending);
//   - cancel() drops the pending flush and disarms the frame callback;
//   - the task boundary ALONE is sufficient — the frame callback is
//     never required (the WebView2 rAF-loss class this module repairs).

import assert from "node:assert/strict";
import { test } from "node:test";

import {
  StreamFlushScheduler,
  type FlushFrameController,
  type FlushTaskController,
} from "./stream-flush-scheduler.ts";

// FakeTaskController collects posted callbacks; the test drains them
// explicitly — deterministic, no real event loop involved.
class FakeTaskController implements FlushTaskController {
  posted: (() => void)[] = [];

  post(callback: () => void): void {
    this.posted.push(callback);
  }

  runFirst(): void {
    const cb = this.posted.shift();
    if (cb) cb();
  }
}

// FakeFrameController records the requested callback + cancel handle.
class FakeFrameController implements FlushFrameController {
  callback: (() => void) | null = null;
  cancelled = false;

  request(callback: () => void): () => void {
    this.callback = callback;
    return () => {
      this.cancelled = true;
      this.callback = null;
    };
  }

  run(): void {
    const cb = this.callback;
    this.callback = null;
    if (cb) cb();
  }
}

function makeScheduler(flushes: number[] = []) {
  const task = new FakeTaskController();
  const frame = new FakeFrameController();
  let count = 0;
  const scheduler = new StreamFlushScheduler(task, frame, () => {
    count += 1;
    flushes.push(count);
  });
  return { task, frame, scheduler, flushes };
}

test("schedule arms the task AND the frame boundary once", () => {
  const { task, frame, scheduler } = makeScheduler();

  scheduler.schedule();

  assert.equal(task.posted.length, 1, "one task posted");
  assert.ok(frame.callback, "frame callback armed");
  assert.ok(scheduler.isPending());
});

test("schedule is idempotent while a flush is pending (coalescing latch)", () => {
  const { task, frame, scheduler } = makeScheduler();

  scheduler.schedule();
  scheduler.schedule();
  scheduler.schedule();

  assert.equal(task.posted.length, 1, "no extra tasks while pending");
  assert.ok(frame.callback, "single frame callback");
});

test("the task boundary flushes and disarms the frame callback", () => {
  const { task, frame, scheduler, flushes } = makeScheduler();

  scheduler.schedule();
  task.runFirst();

  assert.deepEqual(flushes, [1], "flushed exactly once");
  assert.ok(frame.cancelled, "frame callback cancelled");
  assert.ok(!scheduler.isPending());
});

test("the frame boundary alone flushes (task not run yet)", () => {
  const { task, frame, scheduler, flushes } = makeScheduler();

  scheduler.schedule();
  frame.run();

  assert.deepEqual(flushes, [1], "flushed exactly once");
  assert.ok(!scheduler.isPending());

  // Draining the task afterwards must NOT flush again.
  task.runFirst();
  assert.deepEqual(flushes, [1], "second boundary is a no-op");
});

test("both boundaries running flush exactly once", () => {
  const { task, frame, scheduler, flushes } = makeScheduler();

  scheduler.schedule();
  task.runFirst();
  frame.run();

  assert.deepEqual(flushes, [1]);
});

test("cancel drops the pending flush and cancels the frame callback", () => {
  const { task, frame, scheduler, flushes } = makeScheduler();

  scheduler.schedule();
  scheduler.cancel();

  assert.ok(!scheduler.isPending());
  assert.ok(frame.cancelled, "frame disarmed");

  task.runFirst();
  frame.run();

  assert.deepEqual(flushes, [], "no flush after cancel");
});

test("folds across consecutive flush windows each produce one flush", () => {
  const { task, frame, scheduler, flushes } = makeScheduler();

  scheduler.schedule();
  task.runFirst(); // window 1 flushed

  scheduler.schedule();
  frame.run(); // window 2 flushed via the frame boundary

  scheduler.schedule();
  task.runFirst(); // window 3 flushed via the task boundary

  assert.deepEqual(flushes, [1, 2, 3]);
});

test("run() without a pending schedule is a no-op", () => {
  const { scheduler, flushes } = makeScheduler();

  scheduler.run();
  scheduler.run();

  assert.deepEqual(flushes, []);
});
