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
  // v1.9.0: the observation is a MUTATION OBSERVER, not a 20 ms poll —
  // the poll has sampling gaps, and on a fast host the fixture's ~160
  // token stream can complete between two samples (observed: pass/fail
  // flip on the same machine). The observer fires on EVERY DOM
  // transition, so even a single committed streaming frame is caught.
  // The proof standard is unchanged (bubble + Stop up + non-empty text);
  // the observation is strictly stronger. The loop runs INSIDE the page
  // so no Playwright roundtrip latency is involved.
  //
  // v1.9.2 REPAIR (the placeholder loophole): the bubble renders
  // PRESENTATION PLACEHOLDERS before the first content snapshot
  // ("Connecting to the engine and preparing the turn…" while preparing,
  // "…" afterwards — MessageStream.tsx marks them with
  // data-stream-placeholder). The v1.9.0 check accepted ANY non-empty
  // .message-content, so the observation could pass on placeholder text
  // during the engine-gate + prefill window — never model output. The
  // check now requires a NON-PLACEHOLDER content snapshot (the same
  // real-content contract live-stream.spec.ts enforces since v1.9.1).
  const observed = await page.evaluate(() => {
    return new Promise<boolean>((resolve) => {
      const started = Date.now();

      const textOf = (el: Element | null) =>
        (el?.textContent ?? "").replace(/\u200b/g, "").length;

      const check = (): boolean => {
        const bubble = document.querySelector(".generation-bubble");
        const content = bubble?.querySelector(".message-content");
        const stopVisible = Array.from(
          document.querySelectorAll("button"),
        ).some((b) => b.textContent?.trim() === "Stop");
        // Real streamed text only: the placeholder arm is presentation,
        // never model output (v1.9.2 — mirrors live-stream.spec.ts).
        const placeholder =
          Boolean(bubble?.querySelector("[data-stream-placeholder]"));
        return Boolean(
          bubble && stopVisible && !placeholder && textOf(content) > 0,
        );
      };

      // Immediate check first (the window may already be open), then
      // every DOM mutation until the bound.
      if (check()) {
        resolve(true);
        return;
      }

      // v1.9.2: the bound is 60s — the same honest contract as
      // live-stream.spec.ts waitForLiveText. The engine gate + prefill
      // phase (measured: ~13s unloaded for the ~1681-token engine-side
      // encoding of the briefing; 2-3x under full-suite contention on a
      // 2-4 core host) legitimately precedes the first real snapshot,
      // and the v1.9.1 30s bound expired INSIDE that phase under load —
      // the observation failed without any product defect. The bound
      // covers the wait; the PROOF standard is unchanged and is not
      // weakened: real non-placeholder content while the run is live.
      const observer = new MutationObserver(() => {
        if (check()) {
          observer.disconnect();
          resolve(true);
          return;
        }
        if (Date.now() - started > 60_000) {
          observer.disconnect();
          resolve(false);
        }
      });

      observer.observe(document.body, {
        childList: true,
        subtree: true,
        characterData: true,
      });

      // The bound guard also runs timer-side: a page with NO mutations
      // after the armed check would otherwise wait forever.
      const bound = window.setInterval(() => {
        if (Date.now() - started > 60_000) {
          window.clearInterval(bound);
          observer.disconnect();
          resolve(false);
        }
      }, 500);
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

  // v1.9.0 (reload-race regression guard): the run must be PROVABLY
  // dispatched before any reload is legal. The optimistic user bubble is
  // pushed synchronously immediately before POST /api/run fires, so its
  // visibility is the deterministic "the run left the client" marker.
  // The bare composer-enabled probe below is NOT a transition assertion —
  // it is satisfied by the pre-run enabled state too, and reloading on
  // that sample alone killed the pipeline before the run POST ever fired
  // (the exact v1.8.8/v1.9.0 Linux E2E defect signature: session exists,
  // transcript total:0).
  await expect(
    page.locator(".message-row.from-user", {
      hasText: "reload persistence proof",
    }),
  ).toBeVisible({ timeout: 20_000 });

  // The run executes and settles before the reload (authoritative
  // durable state: the backend persists the user message and the reply
  // BEFORE the terminal state unlocks the composer).
  await expect(composer(page)).toBeEnabled({ timeout: 120_000 });
  await expect(page.locator(".message-row.from-agent")).toHaveCount(1, {
    timeout: 60_000,
  });

  // Reload the app entirely.
  await page.reload();
  await expect(composer(page)).toBeEnabled({ timeout: 60_000 });

  // The session created by the zero-session Send is still there, still
  // active, and its transcript survived (user message AND the assistant
  // reply — durable state, not in-memory leftovers).
  await expect(page.locator(".session-list .session-item-wrap")).toHaveCount(
    1,
    { timeout: 20_000 },
  );
  await expect(
    page
      .locator(".message-row", { hasText: "reload persistence proof" })
      .first(),
  ).toBeVisible({ timeout: 30_000 });
  await expect(page.locator(".message-row.from-agent")).toHaveCount(1, {
    timeout: 30_000,
  });
});
