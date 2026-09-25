import { expect, test, type Page } from "@playwright/test";
import { startSheytan, type SheytanStack } from "./fixtures/sheytan-server";

/**
 * model-first.spec.ts — THE v1.5.0 MODEL-FIRST STARTUP CONTRACT (§2).
 *
 * Proven through the browser against the REAL stack, starting from the
 * FRESH-INSTALL state (config.model = "" with a real GGUF present in
 * the models dir — the exact shape that used to prewarm an arbitrary
 * "first .gguf"):
 *
 *   launch → local models DISCOVERED without loading one → Model
 *   Selector shown → composer gated → user selects the model →
 *   Analyzing/Configuring/Loading (backend-authoritative) → engine
 *   ready → real streaming generation → persistence.
 *
 * The fixture engine is the real C++ host executing the wide-context
 * synthetic fixture: generation is real (real logits/sampler/streaming);
 * the token CONTENT is synthetic-weights noise — the suite proves the
 * PIPELINE, never model quality.
 */

let stack: SheytanStack | undefined;

test.beforeAll(async () => {
  // noModel: the fresh-install shape — no selected model, engine
  // auto-start ARMED but gated on the selection.
  stack = await startSheytan({ noModel: true });
});

test.afterAll(async () => {
  // v1.5.0: a failed beforeAll leaves stack undefined — the afterAll must
  // never mask the PRIMARY startup failure with a secondary teardown error.
  await stack?.stop();
});

test.beforeEach(async ({ page }) => {
  await page.goto(stack.baseURL + "/");
});

// The composer TEXTAREA (stable locator — the placeholder honestly
// changes with the model-selection state, so placeholder matching would
// only work in one phase).
const composer = (page: Page) => page.locator(".composer textarea");

async function selectTheModel(page: Page): Promise<void> {
  // The Model Selector is the content area on a fresh install. When the
  // model is ALREADY selected (a previous test in this file completed
  // the selection), the picker is closed and the composer is armed —
  // the selection step is legitimately a no-op.
  const card = page.getByTestId("model-card-e2e-wide.gguf");

  if (await card.isVisible().catch(() => false)) {
    await card.getByRole("button", { name: "Use model" }).click();
    return;
  }

  // Not visible: either the picker is still loading or the selection
  // already happened. Distinguish by the backend's own state.
  const engine = await page.evaluate(async () => {
    const res = await fetch("/api/engine");
    return (await res.json()) as { selectionRequired?: boolean };
  });

  if (engine.selectionRequired) {
    // Still unselected — wait for discovery and select for real.
    await expect(card).toBeVisible({ timeout: 30_000 });
    await card.getByRole("button", { name: "Use model" }).click();
  }
}

test("launch → models discovered WITHOUT loading → Model Selector shown", async ({
  page,
}) => {
  // The application shell renders with the product identity.
  await expect(page.getByRole("banner")).toContainText("SHEYTAN", { timeout: 30_000 });

  // The selector is the primary surface (fresh install, no selection).
  await expect(page.getByRole("region", { name: "Model selection" })).toBeVisible({
    timeout: 30_000,
  });

  // Discovery found the fixture model and lists its measured facts —
  // WITHOUT loading it (the engine state is pre-selection).
  const card = page.getByTestId("model-card-e2e-wide.gguf");
  await expect(card).toBeVisible();
  await expect(card).toContainText("e2e-wide");

  // The engine snapshot is the honest pre-selection state: selection
  // required, engine idle — never an arbitrary model boot.
  const state = await page.evaluate(async () => {
    const res = await fetch("/api/engine");
    return (await res.json()) as {
      state: string;
      selectionRequired?: boolean;
      phase?: string;
    };
  });

  expect(state.selectionRequired).toBe(true);
  expect(state.phase).toBe("model-selection");
  expect(["idle", "stopped"]).toContain(state.state);
});

test("fresh install gates the composer until a model is selected", async ({ page }) => {
  await expect(composer(page)).toBeDisabled({ timeout: 30_000 });

  // The honest gate points at the Model Selector.
  await expect(page.locator(".composer-selection-gate")).toContainText(
    "Choose a model to start",
  );
});

test("select → Analyzing/Configuring/Loading → Ready → the selected model serves", async ({
  page,
}) => {
  await selectTheModel(page);

  // The selection flow settles — the composer unlocks once the
  // selection applied and the engine serves the SELECTED model. (The
  // in-picker phase banner is TRANSIENT by design: the picker closes
  // itself the moment the switch applies, so the durable proof is the
  // terminal state asserted below — state-based waits, no sleeps.)
  await expect(composer(page)).toBeEnabled({ timeout: 90_000 });

  // The backend verifies the selected model is what actually serves.
  const state = await page.evaluate(async () => {
    const res = await fetch("/api/engine");
    return (await res.json()) as {
      state: string;
      selectionRequired?: boolean;
      backend?: string;
      loadedPath?: string;
      selection?: { phase: string; model?: string };
    };
  });

  expect(state.selectionRequired).toBeFalsy();
  expect(state.selection?.phase).toBe("ready");
  expect(state.selection?.model).toBe("e2e-wide.gguf");
  expect(state.loadedPath ?? "").toContain("e2e-wide.gguf");
});

test("after selection: send → real streaming → settles → persists (the §7 chat chain)", async ({
  page,
}) => {
  await selectTheModel(page);

  await expect(composer(page)).toBeEnabled({ timeout: 90_000 });

  const marker = `model-first persist ${Date.now()}`;
  await composer(page).fill(marker);
  await page.getByRole("button", { name: "Send", exact: true }).click();

  // The run settles and a non-empty assistant bubble persisted.
  await expect(composer(page)).toBeEnabled({ timeout: 90_000 });
  await expect(page.locator(".message-row.from-user").last()).toContainText(marker);

  const assistant = page.locator(".message-row.from-agent .message-bubble").last();
  await expect(assistant).toBeVisible();
  const text = (await assistant.textContent()) ?? "";
  expect(text.trim().length).toBeGreaterThan(0);

  // Full reload: the conversation remains (real persistence).
  await page.reload();
  await expect(composer(page)).toBeEnabled({ timeout: 30_000 });
  await expect(page.locator(".message-row.from-user").last()).toContainText(marker, {
    timeout: 30_000,
  });
});
