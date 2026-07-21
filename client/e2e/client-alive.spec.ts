import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

import { seedIdentity } from "./helpers";

// client-alive.spec.ts (#170): the client keeps APPLYING turn bundles after an
// inventory action — the regression guard for #167.
//
// That bug shipped because nothing watched the right number. An uncaught throw
// inside onTurn stopped every layer from updating while the SSE stream stayed
// healthy, so the HUD read "connected" over a frozen map. Crucially
// `window.game.turn` is assigned EARLY in the handler and kept advancing right
// through the outage — a test asserting on it would have passed while the game
// was dead. `turnApplied` is stamped on the handler's LAST line, so it only
// moves when a whole bundle survived.

const TURN_GATED = { timeout: 20_000 };

async function seedRogue(page: Page): Promise<void> {
  await seedIdentity(page, { class: "rogue" });
}

/** Waits for `turnApplied` to advance by at least `n` from `from`. */
async function applied(page: Page, from: number, n: number): Promise<void> {
  await expect
    .poll(() => page.evaluate(() => window.game.turnApplied), TURN_GATED)
    .toBeGreaterThanOrEqual(from + n);
}

test("bundles keep being APPLIED after equipping and unequipping", async ({ page }) => {
  test.slow(); // turn-gated journey; see autowalk.spec.ts

  await seedRogue(page);
  await page.goto("/");

  await expect.poll(() => page.evaluate(() => window.game.me?.id ?? null), TURN_GATED).not.toBeNull();
  await expect.poll(() => page.evaluate(() => window.game.turnApplied), TURN_GATED).toBeGreaterThanOrEqual(0);

  // Baseline: bundles are being applied before we touch anything.
  const before = await page.evaluate(() => window.game.turnApplied);
  await applied(page, before, 2);

  // Unequip the main-hand weapon — an inventory action that rewrites the
  // items array the turn handler walks every bundle. #167's throw lived in
  // exactly that walk.
  await expect(page.locator("#toggle-inventory")).toBeVisible();
  if (!(await page.evaluate(() => window.game.panelOpen))) {
    await page.locator("#toggle-inventory").click();
  }
  await expect.poll(() => page.evaluate(() => window.game.panelOpen), TURN_GATED).toBe(true);
  await expect(page.locator("#character-panel")).toBeVisible();

  const mainHand = page.locator('.hex[data-slot="main-hand"]');
  await expect(mainHand).toHaveClass(/filled/);
  await mainHand.click();

  await expect
    .poll(() => page.evaluate(() => window.game.backpack.filter((b) => b !== null).length), TURN_GATED)
    .toBeGreaterThanOrEqual(1);
  await expect(page.locator('.hex[data-slot="main-hand"] .empty')).toBeVisible();

  // THE GUARD: bundles must still be applied, end to end, after the change.
  const afterUnequip = await page.evaluate(() => window.game.turnApplied);
  await applied(page, afterUnequip, 2);

  // And no uncaught error was swallowed along the way (#170's banner hook).
  expect(await page.evaluate(() => window.game.clientError)).toBeNull();
  await expect(page.locator("#client-error")).toBeHidden();

  // Re-equip from the backpack and assert the same thing again: the equipped
  // path renders a different branch of the item walk than the backpack one.
  const daggerCell = page.locator('.cell-use[data-def="dagger"]');
  await expect(daggerCell).toBeVisible();
  await daggerCell.click();
  await expect.poll(() => page.evaluate(() => "main-hand" in window.game.equipped), TURN_GATED).toBe(true);

  const afterEquip = await page.evaluate(() => window.game.turnApplied);
  await applied(page, afterEquip, 2);

  expect(await page.evaluate(() => window.game.clientError)).toBeNull();
});

test("the stuck marker stays hidden while the client is healthy", async ({ page }) => {
  await seedRogue(page);
  await page.goto("/");

  await expect.poll(() => page.evaluate(() => window.game.turnApplied), TURN_GATED).toBeGreaterThanOrEqual(0);

  // received and applied track each other on a healthy client, so the marker
  // never shows. (Its true case — applied falling behind — needs a throw
  // injected mid-handler, which is a unit concern rather than an e2e one.)
  await expect(page.locator("#turn-stuck")).toBeHidden();

  const gap = await page.evaluate(() => window.game.turnReceived - window.game.turnApplied);
  expect(gap).toBeLessThanOrEqual(1);
});
