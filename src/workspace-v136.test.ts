// workspace-v136.test.ts — v1.3.6 (spec §21/§22) regression contract:
// the Research workspace layer is REMOVED. Net Search lives in the
// Chat/Agent composers, not in a separate workspace surface. No dead
// route, no nav item, no orphan hash.
import assert from "node:assert/strict";
import { test } from "node:test";

import {
  WORKSPACE_LAYERS,
  getWorkspaceLayer,
  isWorkspaceView,
  parseWorkspaceHash,
  viewModeBinding,
  visibleWorkspaceLayers,
} from "./workspace.ts";

test("the research layer is removed from the workspace vocabulary", () => {
  assert.equal(
    WORKSPACE_LAYERS.some((l) => l.id === "research"),
    false,
    "no Research layer may exist in WORKSPACE_LAYERS",
  );
  assert.equal(isWorkspaceView("research"), false);
});

test("no layer may claim the removed surface", () => {
  for (const layer of WORKSPACE_LAYERS) {
    assert.notEqual(layer.label, "Research");
    assert.notEqual(layer.icon, "⌕");
  }
});

test("an old #research hash resolves to Agent, never to a hidden panel", () => {
  assert.equal(parseWorkspaceHash("#research"), "agent");
  assert.equal(getWorkspaceLayer("research").id, "agent");
});

test("both modes still expose the surviving layers", () => {
  for (const mode of ["chat", "agent"] as const) {
    const layers = visibleWorkspaceLayers(mode);
    assert.ok(layers.length > 0);
    // v1.6.0 (spec §6): CHAT leads the navigation — Chat and Agent are
    // both TOP-LEVEL views (the internal segmented selector is gone).
    assert.equal(layers[0].id, "chat");
    assert.equal(layers[1].id, "agent");
  }
});

// v1.6.0 (spec §6): the top-level Chat/Agent navigation contract. Chat
// and Agent are real views with their own hashes; the conversation-space
// mode follows the view; shared layers never rebind the mode.
test("chat and agent are top-level views with view→mode binding", () => {
  assert.equal(isWorkspaceView("chat"), true);
  assert.equal(getWorkspaceLayer("chat").id, "chat");
  assert.equal(getWorkspaceLayer("chat").label, "Chat");

  assert.equal(viewModeBinding("chat"), "chat");
  assert.equal(viewModeBinding("agent"), "agent");
  assert.equal(viewModeBinding("workspace"), null);
  assert.equal(viewModeBinding("system"), null);
  assert.equal(viewModeBinding("settings"), null);
});

// parseWorkspaceHash resolves #chat / #agent (and keeps the v1.3.6
// invalid-hash fallback to the Agent view). The hash source is
// window.location — stubbed here for the Node test runner.
test("the #chat and #agent hashes resolve to their views", () => {
  const originalWindow = (globalThis as Record<string, unknown>).window;

  for (const [hash, expected] of [
    ["#chat", "chat"],
    ["#agent", "agent"],
    ["#research", "agent"],
    ["", "agent"],
  ] as const) {
    (globalThis as Record<string, unknown>).window = {
      location: { hash },
    };

    assert.equal(parseWorkspaceHash(), expected, `hash ${hash || "(empty)"}`);
  }

  (globalThis as Record<string, unknown>).window = originalWindow;
});
