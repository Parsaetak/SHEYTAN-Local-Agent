// stream-flush-scheduler.ts — v1.8.2 DUAL-BOUNDARY STREAMING FLUSH.
//
// THE DEFECT THIS REPAIRS (P0: streamed text visible only after Stop):
// the store scheduled the streaming flush EXCLUSIVELY via
// requestAnimationFrame. In the user's WebView2 runtime the compositor's
// frame callbacks can be throttled or suspended (occlusion heuristics,
// DWM/GPU states) while the JS event loop, the WebSocket and React's own
// scheduler keep running. The observed split matches exactly:
//
//   - phase label + elapsed clock update (direct setState renders fine)
//   - streamed text NEVER appears (the rAF flush never fires)
//   - pressing Stop reveals everything at once (the stop path flushes
//     synchronously, not via rAF)
//
// THE REPAIR: the flush is scheduled on TWO deterministic boundaries —
// an event-loop task (MessageChannel, the same primitive React's
// scheduler uses; delivered as a macrotask the moment the current turn
// ends) AND an animation frame. Whichever runs first performs the flush;
// the flush itself is idempotent and drains-then-clears, so the second
// boundary is a harmless no-op. Properties:
//
//   - exactly ONE coalescing latch: folds that arrive before either
//     boundary runs coalesce into one flush (no per-token renders);
//   - visibility no longer REQUIRES frame callbacks at all;
//   - no sleeps, no timers, no retry loops — both boundaries are the
//     platform's own scheduling primitives;
//   - the rAF boundary keeps the frame-aligned cadence healthy renderers
//     already have (multiple folds inside one frame = one render).
//
// The scheduling DECISION is a pure class with injectable controllers so
// it is unit-testable without a browser (stream-flush-scheduler.test.ts),
// mirroring the stream-fast-path.ts approach.

export interface FlushTaskController {
  // post schedules callback as an event-loop task (macrotask).
  post(callback: () => void): void;
}

export interface FlushFrameController {
  // request schedules callback on the next animation frame; returns a
  // cancel handle.
  request(callback: () => void): () => void;
}

export class StreamFlushScheduler {
  private pending = false;

  private readonly task: FlushTaskController;
  private readonly frame: FlushFrameController;
  private readonly flush: () => void;

  private cancelFrame: (() => void) | null = null;

  constructor(
    task: FlushTaskController,
    frame: FlushFrameController,
    flush: () => void,
  ) {
    this.task = task;
    this.frame = frame;
    this.flush = flush;
  }

  // schedule arms both boundaries exactly once while a flush is pending.
  // Idempotent: folds that arrive while a flush is pending only coalesce
  // into the pending flush.
  schedule(): void {
    if (this.pending) {
      return;
    }

    this.pending = true;

    // The task boundary fires as soon as the current JS turn ends — in
    // every renderer state where JavaScript runs at all. This is the
    // boundary that restores visibility when frame callbacks are lost.
    this.task.post(() => this.run());

    // The frame boundary keeps the frame-aligned cadence in healthy
    // renderers. The task usually wins; the frame is the natural cadence
    // for folds that arrive inside the same frame window.
    this.cancelFrame = this.frame.request(() => this.run());
  }

  // run performs the pending flush exactly once. Both boundaries invoke
  // it; the latch guarantees one execution per schedule().
  run(): void {
    if (!this.pending) {
      return;
    }

    this.pending = false;
    this.disposeFrame();
    this.flush();
  }

  // cancel drops a pending flush (socket reset / session switch /
  // accumulator reset — mirrors resetPendingStreaming).
  cancel(): void {
    this.pending = false;
    this.disposeFrame();
  }

  // isPending reports whether a flush is armed (test/diagnostic seam).
  isPending(): boolean {
    return this.pending;
  }

  private disposeFrame(): void {
    if (this.cancelFrame !== null) {
      this.cancelFrame();
      this.cancelFrame = null;
    }
  }
}

// Browser controllers — the real bindings the store installs.

export class MessageChannelTaskController implements FlushTaskController {
  private channel: MessageChannel | null = null;
  private queued: (() => void) | null = null;

  constructor() {
    if (typeof MessageChannel === "function") {
      try {
        const ch = new MessageChannel();

        ch.port1.onmessage = () => {
          this.channel = null;
          const cb = this.queued;
          this.queued = null;
          if (cb) {
            cb();
          }
        };

        this.channel = ch;
      } catch {
        this.channel = null;
      }
    }
  }

  post(callback: () => void): void {
    if (this.channel) {
      // One in-flight posted task at a time: if a previous post has not
      // been delivered yet, chain the newest callback behind it (the
      // scheduler's latch means this only happens across cancel/schedule
      // cycles, never inside one pending window).
      if (this.queued !== null) {
        const prev = this.queued;
        this.queued = () => {
          prev();
          callback();
        };
        return;
      }

      this.queued = callback;
      this.channel.port2.postMessage(null);
      return;
    }

    // Fallbacks when MessageChannel is unavailable: queueMicrotask keeps
    // the no-frame-dependency property with a microtask boundary.
    if (typeof queueMicrotask === "function") {
      queueMicrotask(callback);
      return;
    }

    callback();
  }
}

export class AnimationFrameController implements FlushFrameController {
  request(callback: () => void): () => void {
    if (typeof requestAnimationFrame === "function") {
      const handle = requestAnimationFrame(callback);
      return () => cancelAnimationFrame(handle);
    }

    // No frame callbacks in this environment: run nothing (the task
    // boundary alone owns the flush).
    return () => {};
  }
}
