import { expect, test } from "@playwright/test";

// The default e2e identity is a fighter/human/traveler (playwright.config's
// storageState) and this server is monster-free, so the player is always OUT of
// combat here — exactly the case the pending-action feedback must cover on a
// general level (the same mechanism serves the in-combat case; see main.ts's
// beginItemAction — the pending set drives it, not the clock). Unequipping the
// fighter's main-hand weapon is a clock-gated action: it applies on the next
// turn bundle, and until then the panel badge AND window.game.pendingItems must
// reflect it, then both clear once it resolves.
test("out-of-combat item action shows pending feedback, then clears", async ({ page }) => {
  await page.goto("/");
  await expect.poll(() => page.evaluate(() => window.game.turn), { timeout: 10_000 }).toBeGreaterThan(0);
  expect(await page.evaluate(() => window.game.inCombat)).toBe(false);

  // Open the character panel; the fighter starts with an Iron Sword in hand.
  await page.locator("#toggle-inventory").click();
  await expect(page.locator("#character-panel")).toBeVisible();
  const mainHand = page.locator('.hex[data-slot="main-hand"]');
  await expect(mainHand.locator(".itemname")).toHaveText("Iron Sword");

  // Unequip it — a clock-gated action, out of combat. Pending feedback registers
  // the instant the click handler runs (beginItemAction sets it synchronously,
  // and SolidJS flushes the badge/class before the handler returns), and holds
  // only until the next bundle (a fast 250 ms here) resolves it. That window is
  // too short to observe reliably across a separate Playwright round-trip, so
  // fire the click AND read the state inside ONE evaluate: it runs synchronously
  // to completion, and no SSE bundle can interleave (JS is single-threaded).
  const snap = await page.evaluate(() => {
    const hex = document.querySelector('.hex[data-slot="main-hand"]') as HTMLElement;
    hex.click();
    return {
      pending: window.game.pendingItems.length,
      hexPending: hex.classList.contains("pending"),
      spinner: !!hex.querySelector(".spinner"),
      name: hex.querySelector(".itemname")?.textContent ?? null,
    };
  });
  expect(snap.pending, "pendingItems lists the in-flight item").toBeGreaterThan(0);
  expect(snap.hexPending, "the hex shows the pending state").toBe(true);
  expect(snap.spinner, "the pending spinner is shown").toBe(true);
  expect(snap.name, "the item stays named, not a bare ellipsis").toBe("Iron Sword");

  // It resolves on a turn bundle: the sword moves to the backpack and the
  // pending feedback clears (hex no longer pending, slot empty).
  await expect.poll(() => page.evaluate(() => window.game.pendingItems.length), { timeout: 5_000 }).toBe(0);
  await expect(mainHand).not.toHaveClass(/pending/);
  await expect(page.locator('.hex[data-slot="main-hand"] .empty')).toBeVisible();
});
