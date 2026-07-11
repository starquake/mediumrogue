import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";

declare global {
  interface Window {
    game: GameDebug;
  }
}

// Reproduces the seam behind item 2's investigation (playtest feedback batch
// 3, "players swapped identities"): two browser TABS in the same context
// share localStorage's `mediumrogue.identity` key. The playtest report
// describes multiple humans/tabs on one machine — this is that scenario.
//
// Before the fix (net/session.ts), a tab's rejoin/reclaim re-read the token
// to send from `loadIdentity()` (disk) rather than its own in-memory
// identity, so a SECOND tab overwriting the shared key could get silently
// woven into the FIRST tab's next reclaim — the first tab would end up
// controlling (and rendering) the second tab's character. After the fix,
// `reclaim()` always sends the caller's own known token, and a `storage`
// event listener (main.ts) reloads a tab outright the instant another tab
// overwrites the shared identity — turning silent split-brain into an
// obvious, consistent reload instead.
test("a second tab joining as a different player triggers the first tab to reload instead of silently corrupting", async ({
  context,
  page,
}) => {
  // Tab 1: the seeded storageState identity auto-joins (fighter/human/
  // traveler, no token yet — see playwright.config.ts's storageStateFor).
  await page.goto("/");
  await expect.poll(() => page.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();
  await expect.poll(() => page.evaluate(() => window.game.class)).not.toBe("");

  const tokenA = await page.evaluate(
    () => (JSON.parse(localStorage.getItem("mediumrogue.identity")!) as { token: string }).token,
  );
  expect(tokenA).not.toBe("");

  // Tab 2: a SECOND person on the SAME browser. Clearing the shared identity
  // before the page's own scripts run makes this a genuine "someone else
  // joined in another tab" event (start screen, brand-new character) rather
  // than tab 1's own character opened twice.
  const page2 = await context.newPage();
  await page2.addInitScript(() => localStorage.removeItem("mediumrogue.identity"));
  await page2.goto("/");

  await expect(page2.locator("#start-screen")).toBeVisible();
  await page2.locator("#start-name").fill("bob");
  await page2.locator('.card[data-class="rogue"]').click();
  await page2.locator('.card[data-species="elf"]').click();
  await page2.locator("#start-enter").click();

  await expect.poll(() => page2.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();
  await expect.poll(() => page2.evaluate(() => window.game.class)).not.toBe("");

  const tokenB = await page2.evaluate(
    () => (JSON.parse(localStorage.getItem("mediumrogue.identity")!) as { token: string }).token,
  );
  expect(tokenB).not.toBe(tokenA);

  // Tab 1 must react to the `storage` event (fires only in OTHER tabs, i.e.
  // exactly here) by reloading — never keep running under a token tab 2 is
  // now using.
  await page.waitForEvent("load", { timeout: 10_000 });

  await expect.poll(() => page.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();

  // After the reload, tab 1 comes back up consistent with whatever is now
  // on disk (tab 2's identity, since it wrote last) — its rendered state and
  // its persisted token always agree. The load-bearing guarantee is that
  // there's no window where tab 1 silently renders/controls a mismatched
  // mix of the two identities.
  const tokenAfterReload = await page.evaluate(
    () => (JSON.parse(localStorage.getItem("mediumrogue.identity")!) as { token: string }).token,
  );
  expect(tokenAfterReload).toBe(tokenB);

  const idAfterReload = await page.evaluate(() => window.game.me!.id);
  const idPage2 = await page2.evaluate(() => window.game.me!.id);
  expect(idAfterReload).toBe(idPage2);
});
