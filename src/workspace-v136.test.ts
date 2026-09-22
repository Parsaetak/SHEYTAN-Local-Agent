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
    assert.equal(layers[0].id, "agent");
  }
});
