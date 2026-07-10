import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";
import { ClassRogue } from "../src/protocol.gen";

declare global {
  interface Window {
    game: GameDebug;
  }
}

// Tests (scope decision, milestone 6b.4 task 7): the full kill → drop →
// walk-on-pickup loop is proven over real HTTP by
// test/integration/gear_test.go's TestDropPickupLoop — there's no e2e
// monster-spawn hook (playwright.config.ts only supports a fixed
// MONSTER_COUNT set at server startup, not "spawn one here"), so farming a
// drop from a real browser would be undeterministic and slow. This spec
// instead drives what a fresh join CAN prove deterministically: the wire's
// class-default items render in the gear panel with the right equipped
// state, and an equipped item's button is inert — the closest thing to an
// "equip round trip" reachable without a drop (rogue's two class defaults
// fill DISTINCT slots and are both pre-equipped on join, so there's no
// un-equipped item here to click a real "equip" on).

test("gear panel renders class-default inventory with equipped items disabled", async ({ page }) => {
  // Same delay trick as class.spec.ts: keep the picker visible long enough
  // for a real click to land (src/main.ts hides it and joins right after
  // fetchMap() resolves).
  await page.route("**/api/map", async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 300));
    await route.continue();
  });

  await page.goto("/");

  const rogueButton = page.locator('#class-picker button[data-class="rogue"]');
  await expect(rogueButton).toBeVisible();
  await rogueButton.click();

  await expect
    .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
    .toBe(true);
  await expect.poll(() => page.evaluate(() => window.game.class)).toBe(ClassRogue);

  // 1. window.game.inventory: rogue's two class defaults (dagger + shortbow),
  // both pre-equipped — grantDefaultsLocked equips every class default at
  // join, and they fill distinct slots (close/ranged) so nothing conflicts.
  await expect.poll(() => page.evaluate(() => window.game.inventory.length)).toBe(2);
  const inventory = await page.evaluate(() => window.game.inventory);
  expect(inventory.every((it) => it.equipped)).toBe(true);
  expect(new Set(inventory.map((it) => it.defId)).size).toBe(2);

  // 2. The panel itself: title, one row per item, an "equipped" label +
  // disabled button on each (every class default starts equipped, so no row
  // ever shows a live "equip" button here).
  await expect(page.locator("#gear-panel")).toBeVisible();
  await expect(page.locator("#gear-title")).toHaveText("Gear");
  const rows = page.locator(".gear-row");
  await expect(rows).toHaveCount(2);

  for (let i = 0; i < 2; i++) {
    const row = rows.nth(i);
    await expect(row).toHaveClass(/gear-equipped/);
    const button = row.locator("button");
    await expect(button).toHaveText("equipped");
    await expect(button).toBeDisabled();
  }

  // 3. Ground-loot wiring is present and empty (nothing has dropped on this
  // monster-free server) — the drop side of the pipeline is covered by
  // TestDropPickupLoop instead (see the file-level comment above).
  await expect.poll(() => page.evaluate(() => window.game.groundItems)).toEqual([]);
});
