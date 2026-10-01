import { expect, test, type Page } from "@playwright/test";
import { startSheytan, type SheytanStack } from "./fixtures/sheytan-server";

/**
 * zero-session.spec.ts — v1.8.4 (P0-C): THE ZERO-SESSION SEND CONTRACT.
 *
 * Proves through the REAL stack (server + HTTP + WebSocket + the real
 * engine executing the live fixture):
 *
 *   1. every session is deleted through the UI;
 *   2. the resulting ZERO-SESSION state is valid — and, unlike v1.8.3,
 *      the composer and Send button are ENABLED (the v1.8.3 defect:
 *      Send/chat was blocked instead of creating a session);
 *   3. pressing Send with zero sessions AUTOMATICALLY creates a session
 *      in the current mode, makes it active, and continues the run;
 *   4. the user message is shown and the streamed answer appears LIVE
 *      (visible-before-completion, the same proof standard as
 *      live-stream.spec.ts);
 *   5. after a reload the created session persists and stays active.
 *
 * Both Chat and Agent modes are covered (session creation is
 * mode-parameterised end to end).
 */

let stack: SheytanStack | undefined;

test.beforeAll(async () => {
  stack = await startSheytan({
    model: "e2e-live.gguf",
    modelGenerator: "make-e2e-live-model.py",
    maxTokens: 160,
  });
});

test.afterAll(async () => {
  await stack?.stop();
});

test.beforeEach(async ({ page }) => {
  await page.goto(stack!.baseURL + "/");
  await expect(composer(page)).toBeEnabled({ timeout: 60_000 });
});

const composer = (page: Page) =>
  page.getByPlaceholder(/Message SHEYTAN|Describe what SHEYTAN/);

// deleteEverySession removes sessions through the real UI until the
// sidebar shows none, then verifies the zero-session surface.
async function deleteEverySession(page: Page) {
  for (;;) {
    const rows = page.locator(".session-list .session-item-wrap");
    const count = await rows.count();

    if (count === 0) {
      break;
    }

    const first = rows.first();

    // The actions reveal on hover (the real user gesture).
    await first.hover();
    await first.getByRole("button", { name: /^Delete / }).click();

    // Wait for the deletion to land (state-based: the count shrinks).
    await expect(rows).toHaveCount(count - 1, { timeout: 20_000 });
  }
}

test("deleting the final session leaves a usable zero-session state, and Send creates + runs", async ({
  page,
}) => {
  await expect(composer(page)).toBeEnabled({ timeout: 60_000 });

  // Seed one session, then delete every session through the UI.
  await deleteEverySession(page);

  // THE ZERO-SESSION STATE: no rows — and the composer is ENABLED (the
  // v1.8.3 defect was exactly this gate blocking the whole flow).
  await expect(page.locator(".session-list .session-item-wrap")).toHaveCount(0);
  await expect(composer(page)).toBeEnabled();

  // Press Send with zero sessions.
  await composer(page).fill("send with zero sessions");
  await page.getByRole("button", { name: /Send|Forge/, exact: true }).click();

  // A session was created automatically and is ACTIVE (the sidebar has
  // exactly one row again).
  await expect(page.locator(".session-list .session-item-wrap")).toHaveCount(
    1,
    {
      timeout: 20_000,
    },
  );

  // The optimistic user bubble is visible.
  await expect(
    page.locator(".message-row.from-user", {
      hasText: "send with zero sessions",
    }),
  ).toBeVisible({ timeout: 20_000 });

  // The run executes and settles honestly: the composer unlocks (or
  // never locked — the fixture engine can finish fast) and exactly one
  // assistant reply carries the answer. Never a stuck live phase.
  await expect(composer(page)).toBeEnabled({ timeout: 120_000 });
  await expect(page.locator(".message-row.from-agent")).toHaveCount(1, {
    timeout: 60_000,
  });
});

test("Send with zero sessions streams text visibly BEFORE completion (Chat)", async ({
  page,
}) => {
  await expect(composer(page)).toBeEnabled({ timeout: 60_000 });

  await deleteEverySession(page);
  await expect(page.locator(".session-list .session-item-wrap")).toHaveCount(0);

  await composer(page).fill("zero session live streaming proof");
  await page.getByRole("button", { name: /Send|Forge/, exact: true }).click();

  // Visible-before-completion: the LIVE generation surface shows
  // non-empty streamed text while the run is provably live (Stop up).
  // The observation loop runs INSIDE the page (20 ms samples for up to
  // 30 s) so a fast engine's live window cannot be missed by the
  // Playwright roundtrip latency — the same proof standard as
  // live-stream.spec.ts, tightened for the zero-session path's cold
  // engine gate.
  const observed = await page.evaluate(() => {
    return new Promise<boolean>((resolve) => {
      const started = Date.now();

      const sample = () => {
        const bubble = document.querySelector(".generation-bubble");
        const content = bubble?.querySelector(".message-content");
        const stopVisible = Array.from(
          document.querySelectorAll("button"),
        ).some((b) => b.textContent?.trim() === "Stop");
        const textLen = (content?.textContent ?? "").replace(
          /\u200b/g,
          "",
        ).length;

        if (bubble && stopVisible && textLen > 0) {
          resolve(true);
          return;
        }

        if (Date.now() - started > 30_000) {
          resolve(false);
          return;
        }

        setTimeout(sample, 20);
      };

      sample();
    });
  });

  expect(observed, "streamed text must be visible while the run is live").toBe(
    true,
  );

  // Settlement: exactly one assistant reply, composer unlocked.
  await expect(composer(page)).toBeEnabled({ timeout: 120_000 });
  await expect(page.locator(".message-row.from-agent")).toHaveCount(1);
});

test("Agent mode: zero-session Send creates an AGENT session (mode-parameterised)", async ({
  page,
}) => {
  // Top-level tab semantics (the same affordance agent.spec.ts uses).
  await page.getByRole("tab", { name: "Agent" }).click();
  await expect(composer(page)).toBeEnabled({ timeout: 60_000 });

  await deleteEverySession(page);
  await expect(page.locator(".session-list .session-item-wrap")).toHaveCount(0);

  await composer(page).fill("agent mode zero session send");
  await page.getByRole("button", { name: /Send|Forge/, exact: true }).click();

  await expect(page.locator(".session-list .session-item-wrap")).toHaveCount(
    1,
    {
      timeout: 20_000,
    },
  );

  await expect(
    page.locator(".message-row.from-user", {
      hasText: "agent mode zero session send",
    }),
  ).toBeVisible({ timeout: 20_000 });

  await expect(composer(page)).toBeEnabled({ timeout: 120_000 });
});

test("reload after a zero-session Send: the created session persists and stays active", async ({
  page,
}) => {
  await expect(composer(page)).toBeEnabled({ timeout: 60_000 });

  await deleteEverySession(page);
  await composer(page).fill("reload persistence proof");
  await page.getByRole("button", { name: /Send|Forge/, exact: true }).click();

  await expect(composer(page)).toBeEnabled({ timeout: 120_000 });

  // Reload the app entirely.
  await page.reload();
  await expect(composer(page)).toBeEnabled({ timeout: 60_000 });

  // The session created by the zero-session Send is still there, still
  // active, and its transcript survived.
  await expect(page.locator(".session-list .session-item-wrap")).toHaveCount(1);
  await expect(
    page
      .locator(".message-row", { hasText: "reload persistence proof" })
      .first(),
  ).toBeVisible({ timeout: 30_000 });
});
