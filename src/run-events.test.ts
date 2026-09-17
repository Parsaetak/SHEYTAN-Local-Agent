// run-events.test.ts — v1.2.5 unit tests for the wire-event vocabulary
// mapping, control normalization and the manual-tool policy helpers.
import test from "node:test";
import assert from "node:assert/strict";

import {
  isRunEvidence,
  normalizeEventKind,
  normalizeThinkingControl,
  normalizeToolAllowlist,
  normalizeToolPolicyMode,
} from "./run-events.ts";

test("legacy event names keep their canonical meaning", () => {
  assert.equal(normalizeEventKind("response"), "assistant_delta");
  assert.equal(normalizeEventKind("reasoning"), "thinking_delta");
  assert.equal(normalizeEventKind("tool_start"), "tool_start");
  assert.equal(normalizeEventKind("tool_end"), "tool_end");
  assert.equal(normalizeEventKind("verification"), "verification");
  assert.equal(normalizeEventKind("done"), "done");
  assert.equal(normalizeEventKind("error"), "error");
  assert.equal(normalizeEventKind("session"), "session");
});

test("v1.2.5 event names alias the same semantics", () => {
  assert.equal(normalizeEventKind("assistant_delta"), "assistant_delta");
  assert.equal(normalizeEventKind("thinking_delta"), "thinking_delta");
  assert.equal(normalizeEventKind("thinking_start"), "thinking_open");
  assert.equal(normalizeEventKind("thinking_end"), "thinking_close");
  assert.equal(normalizeEventKind("complete"), "complete");
  assert.equal(normalizeEventKind("status"), "status");
  assert.equal(normalizeEventKind("escalation"), "escalation");
});

test("legacy thinking captions and idle map through (nothing dropped)", () => {
  assert.equal(normalizeEventKind("thinking"), "progress");
  assert.equal(normalizeEventKind("idle"), "idle");
  assert.equal(normalizeEventKind("context"), "context");
  assert.equal(normalizeEventKind("perf"), "perf");
  assert.equal(normalizeEventKind("failure"), "failure");
});

test("unknown names degrade to unknown, never a guess", () => {
  assert.equal(normalizeEventKind("nonsense"), "unknown");
  assert.equal(normalizeEventKind(""), "unknown");
});

test("run evidence covers every live-proof event kind", () => {
  for (const kind of [
    "assistant_delta",
    "thinking_delta",
    "thinking_open",
    "thinking_close",
    "tool_start",
    "tool_end",
    "done",
    "complete",
    "status",
    "escalation",
  ] as const) {
    assert.ok(isRunEvidence(kind), `${kind} must be run evidence`);
  }

  assert.equal(isRunEvidence("context"), false);
  assert.equal(isRunEvidence("unknown"), false);
});

test("thinking control normalization", () => {
  assert.equal(normalizeThinkingControl("fast"), "fast");
  assert.equal(normalizeThinkingControl("thinking"), "thinking");
  assert.equal(normalizeThinkingControl("AUTO"), "auto");
  assert.equal(normalizeThinkingControl(null), "auto");
  assert.equal(normalizeThinkingControl(undefined), "auto");
  assert.equal(normalizeThinkingControl("bogus"), "auto");
});

test("tool policy mode normalization", () => {
  assert.equal(normalizeToolPolicyMode("manual"), "manual");
  assert.equal(normalizeToolPolicyMode("AUTO"), "auto");
  assert.equal(normalizeToolPolicyMode("nonsense"), "auto");
  assert.equal(normalizeToolPolicyMode(null), "auto");
});

test("tool allow-list normalization dedupes and trims", () => {
  assert.deepEqual(
    normalizeToolAllowlist(["files", " files ", "Shell", ""]),
    ["files", "Shell"],
  );

  assert.deepEqual(normalizeToolAllowlist(null), []);
  assert.deepEqual(normalizeToolAllowlist("files"), []);
  assert.deepEqual(
    normalizeToolAllowlist([1, true, undefined, "git"]),
    ["git"],
  );
});
