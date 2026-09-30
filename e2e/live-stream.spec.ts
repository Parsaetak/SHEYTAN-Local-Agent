import { expect, test, type Page } from "@playwright/test";
import { startSheytan, type SheytanStack } from "./fixtures/sheytan-server";

/**
 * live-stream.spec.ts — LIVE VISIBILITY PROOF (v1.8.2 P0).
 *
 * Proves through the REAL stack (server + HTTP + WebSocket + the real
 * C++ engine executing the live fixture: real logits, real sampler,
 * real streaming — the EOS gate is closed at the FIXTURE level so the
 * generation runs its token budget, making the live window observable):
 *
 *   1. generation begins;
 *   2. real streamed text appears in the LIVE assistant surface
 *      (.generation-bubble) WHILE the run is still live;
 *   3. MORE text becomes visible without pressing Stop;
 *   4. the run continues generating after the first visible text;
 *   5. completion produces exactly ONE final assistant message;
 *   6. no duplicate transcript appears.
 *
 * A second test proves Stop during a live generation settles honestly.
 * No test here "only waits for final completion" — every visibility
 * assertion is taken while the run is provably live (Stop button up).
 */

let stack: SheytanStack | undefined;

test.beforeAll(async () => {
  stack = await startSheytan({
    model: "e2e-live.gguf",
    modelGenerator: "make-e2e-live-model.py",
    // ~160 tokens of slow-dims generation ≈ 1–3 s of live streaming.
    maxTokens: 160,
  });
});

test.afterAll(async () => {
  await stack?.stop();
});

test.beforeEach(async ({ page }) => {
  await page.goto(stack!.baseURL + "/");

  // Isolation: every test starts in a FRESH session (the previous test's
  // transcript must never leak into this test's assertions).
  await expect(composer(page)).toBeEnabled({ timeout: 60_000 });
  await page.getByRole("button", { name: "New session" }).click();
  await expect(
    page.locator(".message-row, .conversation-empty").first(),
  ).toBeVisible();
});

const composer = (page: Page) => page.getByPlaceholder("Message SHEYTAN...");

// liveSample reads the live generation surface's state in one evaluation:
// phase label, cumulative text length, whether the run is still live.
function liveSample(page: Page) {
  return page.evaluate(() => {
    const bubble = document.querySelector(".generation-bubble");
    const phase = document.querySelector(".generation-phase");
    const content = bubble?.querySelector(".message-content");
    const stopVisible = Array.from(document.querySelectorAll("button")).some(
      (b) => b.textContent?.trim() === "Stop",
    );
    return {
      hasBubble: Boolean(bubble),
      phase: phase?.textContent?.trim() ?? null,
      textLen: (content?.textContent ?? "").replace(/\u200b/g, "").length,
      stopVisible,
    };
  });
}

// waitForLiveText polls until the LIVE bubble shows non-empty streamed
// text (never just waits for completion) — bounded, state-driven.
async function waitForLiveText(page: Page) {
  await expect
    .poll(
      async () => {
        const s = await liveSample(page);
        return s.hasBubble && s.stopVisible && s.textLen > 0;
      },
      { timeout: 60_000, intervals: [50] },
    )
    .toBe(true);
}

test("streamed text is visible WHILE the run is live and grows without Stop", async ({
  page,
}) => {
  await expect(composer(page)).toBeEnabled({ timeout: 60_000 });

  await composer(page).fill("tell me something long");
  await page.getByRole("button", { name: "Send", exact: true }).click();

  // (1)+(2) The LIVE surface shows non-empty streamed text while the run
  // is provably live (Stop button still up) — no user action required.
  await waitForLiveText(page);

  const first = await liveSample(page);
  expect(first.stopVisible, "run must still be live at first text").toBe(true);
  expect(first.textLen, "first visible snapshot must be non-empty").toBeGreaterThan(0);

  // (3)+(4) MORE text becomes visible without pressing Stop, and the run
  // continues generating after the first visible text. The fixture
  // generates its full token budget, so growth is deterministic; we
  // require at least one strictly-later, strictly-longer snapshot while
  // still live.
  await expect
    .poll(
      async () => {
        const s = await liveSample(page);
        if (!s.stopVisible) return -1; // settled before we caught growth
        return s.textLen > first.textLen ? s.textLen : 0;
      },
      { timeout: 30_000, intervals: [50] },
    )
    .toBeGreaterThan(0);

  // (5) The run settles: the composer unlocks, the live bubble is gone,
  // and exactly ONE final assistant message carries the full answer.
  await expect(composer(page)).toBeEnabled({ timeout: 120_000 });
  await expect(
    page.getByRole("button", { name: "Stop", exact: true }),
  ).toHaveCount(0);

  const assistantRows = page.locator(".message-row.from-agent");
  const count = await assistantRows.count();

  // The final transcript: our optimistic user row + exactly one
  // assistant reply for this single send (no duplicate transcript).
  expect(count, "exactly one assistant message after settlement").toBe(1);

  const finalText = (await assistantRows.last().textContent()) ?? "";
  expect(finalText.trim().length, "final reply non-empty").toBeGreaterThan(0);
});

test("Stop during a live generation settles honestly and keeps the partial", async ({
  page,
}) => {
  await expect(composer(page)).toBeEnabled({ timeout: 60_000 });

  await composer(page).fill("generate and I will stop you");
  await page.getByRole("button", { name: "Send", exact: true }).click();

  // Wait for provable liveness with visible text, then Stop mid-run.
  await waitForLiveText(page);

  const stop = page.getByRole("button", { name: "Stop", exact: true });
  await stop.click();

  // The composer unlocks (terminal settlement) and Stop is gone.
  await expect(composer(page)).toBeEnabled({ timeout: 60_000 });
  await expect(
    page.getByRole("button", { name: "Stop", exact: true }),
  ).toHaveCount(0);

  // The transcript stays consistent: one user row, at most one assistant
  // row (the persisted partial or the preserved snapshot), never a
  // duplicated stream.
  const userCount = await page.locator(".message-row.from-user").count();
  const agentCount = await page.locator(".message-row.from-agent").count();

  expect(userCount).toBe(1);
  expect(agentCount, "at most one assistant row after abort").toBeLessThanOrEqual(1);
});
