import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";
import { ClassRogue } from "../src/protocol.gen";

declare global {
  interface Window {
    game: GameDebug;
  }
}

// Tests (scope decision, milestone 6b.4 task 7, toggle semantics added
// item 2): the full kill → drop → walk-on-pickup loop is proven over real
// HTTP by test/integration/gear_test.go's TestDropPickupLoop — there's no
// e2e monster-spawn hook (playwright.config.ts only supports a fixed
// MONSTER_COUNT set at server startup, not "spawn one here"), so farming a
// drop from a real browser would be undeterministic and slow. This spec
// instead drives what a fresh join CAN prove deterministically: the wire's
// class-default items render in the gear panel with the right equipped
// state, and — since rogue's two class defaults fill DISTINCT slots and are
// both pre-equipped on join — clicking an "equipped" button is a real
// unequip-then-re-equip round trip, reachable without ever needing a drop.

test("gear panel renders class-default inventory and the equipped button toggles unequip", async ({ page }) => {
  // Seed a "returning player" identity (no token) requesting Rogue, same
  // technique as ranged.spec.ts — deterministic without touching the start
  // screen (whose own class-selection UX is exercised in class.spec.ts). An
  // empty stored token still reaches the server as a brand-new join (Join()
  // only reclaims an existing entity for a token it recognizes), but with the
  // requested class.
  await page.addInitScript(() => {
    localStorage.setItem("mediumrogue.identity", JSON.stringify({ entityId: 0, token: "", class: "rogue" }));
  });

  await page.goto("/");

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

  // 2. The panel itself: title, one row per item, an "equipped" label on an
  // ACTIVE toggle button (item 2 — not disabled) on each (every class
  // default starts equipped, so both rows start this way).
  await expect(page.locator("#gear-panel")).toBeVisible();
  await expect(page.locator("#gear-title")).toHaveText("Gear");
  const rows = page.locator(".gear-row");
  await expect(rows).toHaveCount(2);

  for (let i = 0; i < 2; i++) {
    const row = rows.nth(i);
    await expect(row).toHaveClass(/gear-equipped/);
    const button = row.locator("button");
    await expect(button).toHaveText("equipped");
    await expect(button).toBeEnabled();
  }

  // 3. Click the first row's "equipped" button: it toggles OFF (unequip),
  // free and immediate outside a bubble. The button's label flips to
  // "equip", the row loses gear-equipped, and window.game.inventory agrees.
  const firstRow = rows.nth(0);
  const firstButton = firstRow.locator("button");
  const firstItem = inventory[0]!;

  await firstButton.click();

  await expect.poll(() => page.evaluate(() => window.game.inventory)).toEqual(
    expect.arrayContaining([expect.objectContaining({ id: firstItem.id, equipped: false })]),
  );
  await expect(firstButton).toHaveText("equip");
  await expect(firstRow).not.toHaveClass(/gear-equipped/);

  // 4. Clicking it again re-equips (round trip back to the starting state).
  await firstButton.click();

  await expect.poll(() => page.evaluate(() => window.game.inventory)).toEqual(
    expect.arrayContaining([expect.objectContaining({ id: firstItem.id, equipped: true })]),
  );
  await expect(firstButton).toHaveText("equipped");
  await expect(firstRow).toHaveClass(/gear-equipped/);

  // 5. Ground-loot wiring is present and empty (nothing has dropped on this
  // monster-free server) — the drop side of the pipeline is covered by
  // TestDropPickupLoop instead (see the file-level comment above).
  await expect.poll(() => page.evaluate(() => window.game.groundItems)).toEqual([]);
});
