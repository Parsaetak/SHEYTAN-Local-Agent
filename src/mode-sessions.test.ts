// mode-sessions.test.ts — v1.2.8 mode-separated history helpers (PURE
// module tests, node:test).

import assert from "node:assert/strict";
import { test } from "node:test";

import {
  filterSessionsByMode,
  normalizeMode,
  rememberActiveForMode,
  resolveActiveForMode,
  sessionMode,
  sessionTitleLine,
} from "./mode-sessions.ts";

const CHAT_A = { id: "chat-a", mode: "chat" };
const CHAT_B = { id: "chat-b", mode: "chat" };
const AGENT_A = { id: "agent-a", mode: "agent" };
const LEGACY = { id: "legacy-1" }; // pre-v1.2.8 session: no mode field

test("sessionMode migrates legacy sessions to agent deterministically", () => {
  assert.equal(sessionMode(LEGACY), "agent");
  assert.equal(sessionMode(CHAT_A), "chat");
  assert.equal(sessionMode(AGENT_A), "agent");
  assert.equal(sessionMode(null), "agent");
});

test("filterSessionsByMode keeps exactly one conversation space", () => {
  const all = [CHAT_A, AGENT_A, LEGACY, CHAT_B];

  assert.deepEqual(
    filterSessionsByMode(all, "chat").map((s) => s.id),
    ["chat-a", "chat-b"],
  );

  assert.deepEqual(
    filterSessionsByMode(all, "agent").map((s) => s.id),
    ["agent-a", "legacy-1"],
  );
});

test("resolveActiveForMode keeps the remembered selection inside its space", () => {
  const all = [CHAT_A, AGENT_A, CHAT_B];

  // Remembered chat session still exists in chat space → kept.
  assert.equal(resolveActiveForMode(all, "chat", "chat-b"), "chat-b");

  // Remembered session from ANOTHER space → falls to the space's newest,
  // never silently switches the active conversation across spaces.
  assert.equal(resolveActiveForMode(all, "chat", "agent-a"), "chat-a");

  // Empty space → null (a fresh session is created lazily).
  assert.equal(resolveActiveForMode([AGENT_A], "chat", null), null);
});

test("rememberActiveForMode updates one slot without touching the other", () => {
  const map = rememberActiveForMode(
    { chat: "chat-a", agent: "agent-a" },
    "chat",
    "chat-b",
  );

  assert.deepEqual(map, { chat: "chat-b", agent: "agent-a" });
});

test("normalizeMode is total", () => {
  assert.equal(normalizeMode("chat"), "chat");
  assert.equal(normalizeMode("agent"), "agent");
  assert.equal(normalizeMode("nonsense"), "agent");
  assert.equal(normalizeMode(undefined), "agent");
});

test("sessionTitleLine falls back for untitled sessions", () => {
  assert.equal(sessionTitleLine("Deploy fix", "s123456abcdef"), "Deploy fix");
  assert.equal(sessionTitleLine("", "s123456abcdef"), "Untitled s123456a");
  assert.equal(sessionTitleLine(undefined, "s123456abcdef"), "Untitled s123456a");
});
