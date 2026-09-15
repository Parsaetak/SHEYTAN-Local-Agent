import assert from "node:assert/strict";
import { test } from "node:test";

import {
  isLivePhase,
  isTerminalPhase,
  nextPhase,
  PHASE_LABELS,
  type RunPhase,
} from "./run-phase.ts";

// v1.2.2 regression tests — the generation lifecycle state machine.
//
// These pin the transitions the live timeline depends on:
//   Preparing → Thinking → Generating → Finalising → Complete
// with error/abort safety and no illegal demotions.

test("fresh run starts in Preparing", () => {
  assert.equal(nextPhase("idle", "run_started"), "preparing");
  assert.equal(nextPhase("complete", "run_started"), "preparing");
  assert.equal(nextPhase("error", "run_started"), "preparing");
  assert.equal(nextPhase("aborted", "run_started"), "preparing");
});

test("reasoning moves Preparing to Thinking, never backwards from Generating", () => {
  assert.equal(nextPhase("preparing", "reasoning_delta"), "thinking");
  assert.equal(nextPhase("thinking", "reasoning_delta"), "thinking");
  // A late reasoning event after the answer started must NOT demote
  // Generating back to Thinking.
  assert.equal(nextPhase("generating", "reasoning_delta"), "generating");
});

test("first response token moves Preparing/Thinking to Generating", () => {
  assert.equal(nextPhase("preparing", "response_delta"), "generating");
  assert.equal(nextPhase("thinking", "response_delta"), "generating");
  assert.equal(nextPhase("generating", "response_delta"), "generating");
});

test("thinking/tool activity advances Preparing but never demotes", () => {
  assert.equal(nextPhase("preparing", "thinking_activity"), "thinking");
  assert.equal(nextPhase("generating", "thinking_activity"), "generating");
  assert.equal(nextPhase("thinking", "thinking_activity"), "thinking");
});

test("done settles any live phase into Finalising", () => {
  for (const phase of ["preparing", "thinking", "generating"] as RunPhase[]) {
    assert.equal(nextPhase(phase, "done"), "finalising");
  }
});

test("history confirmation completes a Finalising run", () => {
  assert.equal(nextPhase("finalising", "history_confirmed"), "complete");
  // History confirmation outside Finalising is a no-op.
  assert.equal(nextPhase("complete", "history_confirmed"), "complete");
  assert.equal(nextPhase("idle", "history_confirmed"), "idle");
});

test("error is terminal from every live phase and ignored after settling", () => {
  for (const phase of [
    "preparing",
    "thinking",
    "generating",
    "finalising",
  ] as RunPhase[]) {
    assert.equal(nextPhase(phase, "error"), "error");
  }
  assert.equal(nextPhase("complete", "error"), "complete");
  assert.equal(nextPhase("aborted", "error"), "aborted");
});

test("abort is terminal from every live phase", () => {
  for (const phase of [
    "preparing",
    "thinking",
    "generating",
    "finalising",
  ] as RunPhase[]) {
    assert.equal(nextPhase(phase, "aborted"), "aborted");
  }
  assert.equal(nextPhase("complete", "aborted"), "complete");
});

test("done arriving after abort does not resurrect the run", () => {
  // The backend emits done(abortCaption) AFTER the user pressed Stop —
  // the phase must stay Aborted, not flip to Finalising/Complete.
  assert.equal(nextPhase("aborted", "done"), "aborted");
});

test("live and terminal classification", () => {
  assert.equal(isLivePhase("preparing"), true);
  assert.equal(isLivePhase("thinking"), true);
  assert.equal(isLivePhase("generating"), true);
  assert.equal(isLivePhase("finalising"), true);

  assert.equal(isLivePhase("idle"), false);
  assert.equal(isLivePhase("complete"), false);
  assert.equal(isLivePhase("error"), false);
  assert.equal(isLivePhase("aborted"), false);

  assert.equal(isTerminalPhase("complete"), true);
  assert.equal(isTerminalPhase("error"), true);
  assert.equal(isTerminalPhase("aborted"), true);
  assert.equal(isTerminalPhase("idle"), false);
  assert.equal(isTerminalPhase("generating"), false);
});

test("every phase has a user-facing label", () => {
  const phases: RunPhase[] = [
    "idle",
    "preparing",
    "thinking",
    "generating",
    "finalising",
    "complete",
    "error",
    "aborted",
  ];

  for (const phase of phases) {
    assert.ok(PHASE_LABELS[phase].length > 0, `label missing for ${phase}`);
  }
});
