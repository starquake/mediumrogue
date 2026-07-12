import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";
import { ClassRogue } from "../src/protocol.gen";

declare global {
  interface Window {
    game: GameDebug;
  }
}

// Inventory-slots milestone (task 5): the paper-doll character panel + the
// per-hex pickup modal, driven from a real browser against the real binary.
//
// Scope note (inherited from the old gear.spec, still true): the e2e server
// is monster-free with no spawn/grant hook, so a fresh join only ever has its
// class-default items (a rogue: dagger + shortbow, both equipped, empty
// backpack). That is enough to drive the toggle, the paper-doll's equipped
// render, unequip -> backpack -> re-equip, and — via DROP, which lands an
// owned item on the player's own hex — the full pickup MODAL (open on
// walk-over, per-row take, close-leaves-the-rest). The three flows a
// monster-free server cannot reach from class defaults — a FULL backpack
// (needs 4+ owned items; a class grants at most 2) for the backpack-full
// rejection, a consumable STACK render, and DRINK (both need a potion, which
// only drops from a monster) — are covered instead at the integration layer
// over real HTTP (test/integration/inventory_test.go: the exact
// backpack-full 422, stack count on the wire, drink heal + decrement).

// seedRogue seeds a "returning player" identity (empty token) requesting
// rogue, so the start screen never appears and the join is a fresh rogue.
async function seedRogue(page: import("@playwright/test").Page): Promise<void> {
  await page.addInitScript(() => {
    localStorage.setItem("mediumrogue.identity", JSON.stringify({ entityId: 0, token: "", class: "rogue" }));
  });
  await page.goto("/");
  await expect.poll(() => page.evaluate(() => window.game.me !== null && window.game.connected)).toBe(true);
  await expect.poll(() => page.evaluate(() => window.game.class)).toBe(ClassRogue);
  // Both class defaults equipped into their class-shaped weapon slots.
  await expect
    .poll(() => page.evaluate(() => Object.keys(window.game.equipped).sort().join(",")))
    .toBe("melee-weapon,ranged-weapon");
}

test("paper-doll renders equipped slots; the panel toggles via the i-key, HUD button, and close", async ({
  page,
}) => {
  await seedRogue(page);

  // Closed by default (the paper-doll is large — not always-on).
  expect(await page.evaluate(() => window.game.panelOpen)).toBe(false);
  await expect(page.locator("#character-panel")).toBeHidden();

  // `i` opens it (canvas has focus by default — no typing target).
  await page.keyboard.press("i");
  await expect.poll(() => page.evaluate(() => window.game.panelOpen)).toBe(true);
  await expect(page.locator("#character-panel")).toBeVisible();
  await expect(page.locator("#toggle-inventory")).toHaveClass(/open/);

  // The two class-shaped weapon slots carry per-class labels + their items;
  // the six body slots render empty.
  const melee = page.locator('.hex[data-slot="melee-weapon"]');
  const ranged = page.locator('.hex[data-slot="ranged-weapon"]');
  await expect(melee).toHaveClass(/filled/);
  await expect(melee.locator(".slotname")).toHaveText("Melee");
  await expect(melee.locator(".itemname")).toHaveText("Dagger");
  await expect(ranged).toHaveClass(/filled/);
  await expect(ranged.locator(".slotname")).toHaveText("Ranged");
  await expect(ranged.locator(".itemname")).toHaveText("Shortbow");
  await expect(page.locator('.hex[data-slot="head"] .empty')).toBeVisible();
  await expect(page.locator('.hex[data-slot="body"] .empty')).toBeVisible();

  // `i` closes it again.
  await page.keyboard.press("i");
  await expect.poll(() => page.evaluate(() => window.game.panelOpen)).toBe(false);
  await expect(page.locator("#character-panel")).toBeHidden();

  // Typing "i" into the chat input must NOT toggle it (the shared focus guard).
  await page.locator("#chat-input").fill("i");
  expect(await page.evaluate(() => window.game.panelOpen)).toBe(false);
  await page.locator("#chat-input").fill("");

  // The HUD button opens; the in-panel × closes.
  await page.locator("#toggle-inventory").click();
  await expect.poll(() => page.evaluate(() => window.game.panelOpen)).toBe(true);
  await page.locator(".panel-close").click();
  await expect.poll(() => page.evaluate(() => window.game.panelOpen)).toBe(false);
  await expect(page.locator("#toggle-inventory")).not.toHaveClass(/open/);
});

test("unequip moves an item into the backpack; equipping from the backpack returns it", async ({ page }) => {
  await seedRogue(page);
  await page.keyboard.press("i");
  await expect(page.locator("#character-panel")).toBeVisible();

  // Click the filled melee slot -> unequip. The dagger leaves the slot and
  // lands in the backpack (never nowhere).
  await page.locator('.hex[data-slot="melee-weapon"]').click();
  await expect
    .poll(() => page.evaluate(() => window.game.backpack.filter((e) => e !== null && e.defId === "dagger").length))
    .toBe(1);
  expect(await page.evaluate(() => "melee-weapon" in window.game.equipped)).toBe(false);
  await expect(page.locator('.hex[data-slot="melee-weapon"] .empty')).toBeVisible();

  // Click the backpack cell -> equip it back into its slot.
  await page.locator('.cell-use[data-def="dagger"]').click();
  await expect.poll(() => page.evaluate(() => "melee-weapon" in window.game.equipped)).toBe(true);
  await expect.poll(() => page.evaluate(() => window.game.backpack.every((e) => e === null))).toBe(true);
  await expect(page.locator('.hex[data-slot="melee-weapon"] .itemname')).toHaveText("Dagger");
});

test("dropping a backpack item opens the pickup modal; taking a row returns the item", async ({ page }) => {
  await seedRogue(page);
  await page.keyboard.press("i");
  await expect(page.locator("#character-panel")).toBeVisible();

  // Unequip the shortbow into the backpack, then drop it — it lands on my own
  // hex, so the pickup modal opens over it (walk-over, standing still).
  await page.locator('.hex[data-slot="ranged-weapon"]').click();
  await expect
    .poll(() => page.evaluate(() => window.game.backpack.some((e) => e !== null && e.defId === "shortbow")))
    .toBe(true);
  await page.locator(".cell .drop").click();

  await expect.poll(() => page.evaluate(() => window.game.pickupModal.open)).toBe(true);
  await expect(page.locator("#pickup-modal")).toBeVisible();
  const rows = page.locator("#pickup-modal .grow");
  await expect(rows).toHaveCount(1);
  await expect(rows.first().locator(".itemline")).toHaveText("Shortbow");
  await expect(rows.first().locator(".typeline")).toContainText("ranged weapon");
  // The item is on the ground, no longer owned.
  await expect.poll(() => page.evaluate(() => window.game.groundItems.length)).toBe(1);
  expect(await page.evaluate(() => window.game.backpack.every((e) => e === null))).toBe(true);

  // Take it back — the row leaves the modal, the ground clears, and it
  // returns to the backpack (unequipped: items never auto-equip on pickup).
  await rows.first().locator("button.yes").click();
  await expect.poll(() => page.evaluate(() => window.game.groundItems.length)).toBe(0);
  await expect
    .poll(() => page.evaluate(() => window.game.backpack.some((e) => e !== null && e.defId === "shortbow")))
    .toBe(true);
  await expect.poll(() => page.evaluate(() => window.game.pickupModal.open)).toBe(false);
});

test("closing the modal leaves the remaining items on the ground", async ({ page }) => {
  await seedRogue(page);
  await page.keyboard.press("i");
  await expect(page.locator("#character-panel")).toBeVisible();

  // Unequip BOTH weapons and drop both — two ground items on my hex, so the
  // modal lists two rows.
  await page.locator('.hex[data-slot="melee-weapon"]').click();
  await page.locator('.hex[data-slot="ranged-weapon"]').click();
  await expect.poll(() => page.evaluate(() => window.game.backpack.filter((e) => e !== null).length)).toBe(2);

  const cellFor = (def: string) => page.locator(".cell", { has: page.locator(`.cell-use[data-def="${def}"]`) });
  await cellFor("dagger").locator(".drop").click();
  await cellFor("shortbow").locator(".drop").click();

  await expect.poll(() => page.evaluate(() => window.game.groundItems.length)).toBe(2);
  await expect(page.locator("#pickup-modal .grow")).toHaveCount(2);

  // Close — the items stay on the ground (they are not granted).
  await page.locator(".pickup-close").click();
  await expect.poll(() => page.evaluate(() => window.game.pickupModal.open)).toBe(false);
  await expect(page.locator("#pickup-modal")).toBeHidden();
  expect(await page.evaluate(() => window.game.groundItems.length)).toBe(2);
  expect(await page.evaluate(() => window.game.backpack.every((e) => e === null))).toBe(true);
});
