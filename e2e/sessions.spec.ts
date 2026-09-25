import { expect, test } from "@playwright/test";
import { startSheytan, type SheytanStack } from "./fixtures/sheytan-server";

/**
 * sessions.spec.ts — SESSION LIFECYCLE + CHAT/AGENT MODE SEPARATION (§5/§8).
 *
 *   new session → select session → rename → delete → switch sessions
 *   Chat A → switch Agent (B) → switch back Chat → A intact
 *
 * Mode-separated conversation spaces are asserted through the UI: each
 * space keeps its own active session; switching modes never leaks one
 * space's transcript into the other.
 */

let stack: SheytanStack;

test.beforeAll(async () => {
  stack = await startSheytan();
});

test.afterAll(async () => {
  await stack.stop();
});

test.beforeEach(async ({ page }) => {
  await page.goto(stack.baseURL + "/");
  await expect(page.getByPlaceholder("Message SHEYTAN...")).toBeEnabled({ timeout: 30_000 });
});

test("new session → empty and ready; the old transcript is not shown", async ({ page }) => {
  const composerBox = page.getByPlaceholder("Message SHEYTAN...");
  await composerBox.fill("belongs to the first session");
  await page.getByRole("button", { name: "Send", exact: true }).click();
  await expect(page.getByPlaceholder("Message SHEYTAN...")).toBeEnabled({ timeout: 90_000 });

  await page.locator(".sidebar").getByRole("button", { name: "New session" }).click();

  // The new session's surface shows the (truthful) empty invitation —
  // the previous session's messages are gone from THIS surface.
  await expect(page.locator(".message-row")).toHaveCount(0, { timeout: 20_000 });
  await expect(page.getByText("Ready when you are")).toBeVisible();
});

test("switching sessions preserves each transcript", async ({ page }) => {
  const composerBox = page.getByPlaceholder("Message SHEYTAN...");

  // Session A with a recognizable marker.
  await composerBox.fill("marker alpha session");
  await page.getByRole("button", { name: "Send", exact: true }).click();
  await expect(page.getByPlaceholder("Message SHEYTAN...")).toBeEnabled({ timeout: 90_000 });

  // Session B.
  await page.locator(".sidebar").getByRole("button", { name: "New session" }).click();
  await expect(page.locator(".message-row")).toHaveCount(0, { timeout: 20_000 });

  // Back to session A (the item now carries the derived title from its
  // first message — the title-clobber regression is pinned here).
  const itemA = page
    .locator(".session-list .session-item", { hasText: "marker alpha session" })
    .first();
  await expect(itemA).toBeVisible({ timeout: 20_000 });
  await itemA.click();
  await expect(page.locator(".message-row.from-user").last()).toContainText(
    "marker alpha session",
    { timeout: 30_000 },
  );
});

test("rename session updates the sidebar label", async ({ page }) => {
  await page.locator(".sidebar").getByRole("button", { name: "New session" }).click();
  await expect(page.locator(".session-list .session-item").first()).toBeVisible();

  // Rename via double-click on the session title (the documented
  // gesture; the ✎ action button reveals on hover as the alternative).
  await page.locator(".session-list .session-item").first().dblclick();
  const renameBox = page.locator(".session-rename-input");
  await expect(renameBox).toBeVisible();
  await renameBox.fill("renamed-e2e-session");
  await renameBox.press("Enter");

  await expect(
    page.locator(".session-list .session-item", { hasText: "renamed-e2e-session" }).first(),
  ).toBeVisible({ timeout: 15_000 });
});

test("delete session removes it and activates a remaining one", async ({ page }) => {
  await page.locator(".sidebar").getByRole("button", { name: "New session" }).click();
  await expect(page.locator(".session-list .session-item").first()).toBeVisible();

  const before = await page.locator(".session-list .session-item").count();
  expect(before).toBeGreaterThanOrEqual(1);

  // Delete via the per-session action button. The actions reveal on
  // hover (the real user gesture); Playwright's hover makes them
  // actionable, then the click runs.
  const first = page.locator(".session-list .session-item-wrap").first();
  await first.hover();
  await first.getByRole("button", { name: /^Delete / }).click();

  // One fewer session in this space afterwards (or zero → still usable).
  await expect(async () => {
    const after = await page.locator(".session-list .session-item").count();
    expect(after).toBeLessThan(before);
  }).toPass({ timeout: 15_000 });
});

test("Chat ↔ Agent modes keep independent conversation spaces", async ({ page }) => {
  const chatComposer = page.getByPlaceholder("Message SHEYTAN...");
  await expect(chatComposer).toBeEnabled();

  // Chat space marker.
  await chatComposer.fill("chat space marker");
  await page.getByRole("button", { name: "Send", exact: true }).click();
  await expect(page.getByPlaceholder("Message SHEYTAN...")).toBeEnabled({ timeout: 90_000 });

  // Switch to the Agent space. With no Agent sessions yet, the space
  // shows its honest empty state; creating one (the real user action)
  // arms the composer.
  await page.getByRole("tab", { name: "Agent" }).click();
  await page.locator(".sidebar").getByRole("button", { name: "New session" }).click();
  const agentComposer = page.getByPlaceholder("Describe what SHEYTAN should forge...");
  await expect(agentComposer).toBeEnabled({ timeout: 30_000 });

  // The Agent space does NOT show the Chat transcript.
  await expect(page.locator(".message-row")).toHaveCount(0, { timeout: 20_000 });
  await agentComposer.fill("agent space own marker");
  await page.getByRole("button", { name: "Forge →" }).click();
  await expect(agentComposer).toBeEnabled({ timeout: 120_000 });

  // Switch back to Chat: the marker conversation is intact.
  await page.getByRole("tab", { name: "Chat" }).click();
  await expect(page.getByPlaceholder("Message SHEYTAN...")).toBeEnabled();
  await expect(page.locator(".message-row.from-user").last()).toContainText("chat space marker", {
    timeout: 30_000,
  });
  await expect(
    page.locator(".message-row", { hasText: "agent space own marker" }),
  ).toHaveCount(0, { timeout: 10_000 });
});
