// model-selection.test.ts — v1.5.0 unit tests for the model-first
// selection contracts (the logic ModelPicker renders).
import { test } from "node:test";
import {
  classifyFit,
  recommendedOnThisMachine,
  selectionBusy,
} from "./model-selection.ts";

test("selection busy phases: exactly the in-flight backend phases", () => {
  // The backend-authoritative chain — every in-flight phase locks.
  for (const phase of ["select", "analyzing", "configuring", "loading", "calibrating"]) {
    if (!selectionBusy(phase)) {
      throw new Error(`phase "${phase}" must be busy (a selection is in flight)`);
    }
  }

  // Terminal phases never lock the picker.
  for (const phase of ["ready", "failed"]) {
    if (selectionBusy(phase)) {
      throw new Error(`terminal phase "${phase}" must not be busy`);
    }
  }

  // No selection flow at all (fresh install) is not busy either.
  if (selectionBusy(null) || selectionBusy(undefined)) {
    throw new Error("no selection flow must not be busy");
  }
});

test("fit hint: a measured sizing verdict, never a guess", () => {
  // Unknown values → no hint.
  if (classifyFit(undefined, 16e9) !== null) throw new Error("no estimate → no hint");
  if (classifyFit(4e9, undefined) !== null) throw new Error("no measured RAM → no hint");
  if (classifyFit(0, 16e9) !== null) throw new Error("zero estimate → no hint");

  // Measured ratios → the honest verdicts.
  if (classifyFit(4e9, 16e9) !== "fits") throw new Error("25% of RAM fits");
  if (classifyFit(14e9, 16e9) !== "tight") throw new Error("87.5% of RAM is tight");
  if (classifyFit(20e9, 16e9) !== "over") throw new Error("125% of RAM is over");

  // The 0.85 boundary is exact.
  if (classifyFit(13.6e9, 16e9) !== "fits") throw new Error("85% exactly is fits");
  if (classifyFit(13.7e9, 16e9) !== "tight") throw new Error("85.6% is tight");
});

test("recommended label: ONLY from measured recommendation evidence", () => {
  // No evidence at all → never.
  if (recommendedOnThisMachine(null)) throw new Error("no evidence → never recommended");

  // Evidence WITHOUT measured hardware → never (the engine did not
  // actually assess this machine).
  if (recommendedOnThisMachine({ class: "safe", hardwareMeasured: false })) {
    throw new Error("unmeasured hardware → never recommended");
  }

  // Measured evidence with a NON-safe verdict → never.
  if (recommendedOnThisMachine({ class: "caution", hardwareMeasured: true })) {
    throw new Error("caution verdict → never recommended");
  }
  if (recommendedOnThisMachine({ class: "unsupported", hardwareMeasured: true })) {
    throw new Error("unsupported verdict → never recommended");
  }
  if (recommendedOnThisMachine({ class: "", hardwareMeasured: true })) {
    throw new Error("no verdict (card-less file) → never recommended");
  }

  // Measured + safe → the ONLY shape that earns the label.
  if (!recommendedOnThisMachine({ class: "safe", hardwareMeasured: true })) {
    throw new Error("measured safe evidence must earn the label");
  }
});
