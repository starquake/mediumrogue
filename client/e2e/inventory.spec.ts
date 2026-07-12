import type { Page } from "@playwright/test";
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
// walk-over, per-row take, close-leaves-the-rest, and — via the
// window.game.rejectPickupRow hook — the backpack-full feedback render). The
// flows a monster-free server cannot reach from class defaults — the SERVER
// backpack-full rejection (needs 4+ owned items), a consumable STACK on the
// ground/backpack, and DRINK (both need a potion, monster-only) — are covered
// at the integration layer over real HTTP (test/integration/inventory_test.go).
//
// CI de-race: every slot/backpack/modal interaction opens+confirms the panel
// first, waits for actionability, and polls window.game (turn-gated by the
// 250ms e2e cadence) rather than assuming immediate DOM. Modal assertions are
// scoped to MY dropped item's id (the ground stack keeps the dropped
// instance's id), never a global ground count — the shared per-spec server
// accumulates other tests' un-picked-up drops.

// TURN_GATED is a generous timeout for state that only settles after one or
// more turn bundles (drop/unequip/pickup landing), so a slow headless CI
// runner doesn't miss it.
const TURN_GATED = { timeout: 20_000 };

// seedRogue seeds a "returning player" identity (empty token) requesting
// rogue, so the start screen never appears and the join is a fresh rogue.
async function seedRogue(page: Page): Promise<void> {
  await page.addInitScript(() => {
    localStorage.setItem("mediumrogue.identity", JSON.stringify({ entityId: 0, token: "", class: "rogue" }));
  });
  await page.goto("/");
  await expect.poll(() => page.evaluate(() => window.game.me !== null && window.game.connected), TURN_GATED).toBe(true);
  await expect.poll(() => page.evaluate(() => window.game.class), TURN_GATED).toBe(ClassRogue);
  // Both class defaults equipped into their class-shaped weapon slots.
  await expect
    .poll(() => page.evaluate(() => Object.keys(window.game.equipped).sort().join(",")), TURN_GATED)
    .toBe("melee-weapon,ranged-weapon");
}

// openPanel opens the character panel via the HUD button (a direct, always-
// actionable DOM click — more robust than the `i` key, whose own coverage is
// the first test) and confirms it is open before any slot/backpack click.
async function openPanel(page: Page): Promise<void> {
  await expect(page.locator("#toggle-inventory")).toBeVisible();
  if (!(await page.evaluate(() => window.game.panelOpen))) {
    await page.locator("#toggle-inventory").click();
  }
  await expect.poll(() => page.evaluate(() => window.game.panelOpen), TURN_GATED).toBe(true);
  await expect(page.locator("#character-panel")).toBeVisible();
}

// idInBackpack returns the instance id of the backpack entry with defId (or 0),
// captured before a drop so the resulting ground stack (which keeps that id)
// and its modal row can be found by id, immune to other tests' leaked drops.
async function idInBackpack(page: Page, defId: string): Promise<number> {
  return page.evaluate((d) => window.game.backpack.find((e) => e !== null && e.defId === d)?.id ?? 0, defId);
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
  await expect.poll(() => page.evaluate(() => window.game.panelOpen), TURN_GATED).toBe(true);
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
  await expect.poll(() => page.evaluate(() => window.game.panelOpen), TURN_GATED).toBe(false);
  await expect(page.locator("#character-panel")).toBeHidden();

  // Typing "i" into the chat input must NOT toggle it (the shared focus guard).
  await expect(page.locator("#chat-input")).toBeVisible();
  await page.locator("#chat-input").fill("i");
  expect(await page.evaluate(() => window.game.panelOpen)).toBe(false);
  await page.locator("#chat-input").fill("");

  // The HUD button opens; the in-panel × closes.
  await page.locator("#toggle-inventory").click();
  await expect.poll(() => page.evaluate(() => window.game.panelOpen), TURN_GATED).toBe(true);
  await expect(page.locator(".panel-close")).toBeVisible();
  await page.locator(".panel-close").click();
  await expect.poll(() => page.evaluate(() => window.game.panelOpen), TURN_GATED).toBe(false);
  await expect(page.locator("#toggle-inventory")).not.toHaveClass(/open/);
});

test("unequip moves an item into the backpack; equipping from the backpack returns it", async ({ page }) => {
  test.slow(); // multi-step, turn-gated journey

  await seedRogue(page);
  await openPanel(page);

  // Click the filled melee slot -> unequip. The dagger leaves the slot and
  // lands in the backpack (never nowhere).
  const melee = page.locator('.hex[data-slot="melee-weapon"]');
  await expect(melee).toHaveClass(/filled/);
  await melee.click();
  await expect
    .poll(() => page.evaluate(() => window.game.backpack.filter((e) => e !== null && e.defId === "dagger").length), TURN_GATED)
    .toBe(1);
  expect(await page.evaluate(() => "melee-weapon" in window.game.equipped)).toBe(false);
  await expect(page.locator('.hex[data-slot="melee-weapon"] .empty')).toBeVisible();

  // Click the backpack cell -> equip it back into its slot.
  const daggerCell = page.locator('.cell-use[data-def="dagger"]');
  await expect(daggerCell).toBeVisible();
  await daggerCell.click();
  await expect.poll(() => page.evaluate(() => "melee-weapon" in window.game.equipped), TURN_GATED).toBe(true);
  await expect.poll(() => page.evaluate(() => window.game.backpack.every((e) => e === null)), TURN_GATED).toBe(true);
  await expect(page.locator('.hex[data-slot="melee-weapon"] .itemname')).toHaveText("Dagger");
});

test("dropping a backpack item opens the pickup modal; taking a row returns the item", async ({ page }) => {
  test.slow();

  await seedRogue(page);
  await openPanel(page);

  // Unequip the shortbow into the backpack, then drop it — it lands on my own
  // hex, so the pickup modal opens over it (walk-over, standing still).
  const ranged = page.locator('.hex[data-slot="ranged-weapon"]');
  await expect(ranged).toHaveClass(/filled/);
  await ranged.click();
  await expect
    .poll(() => page.evaluate(() => window.game.backpack.some((e) => e !== null && e.defId === "shortbow")), TURN_GATED)
    .toBe(true);

  const shortbowId = await idInBackpack(page, "shortbow");
  const shortbowCell = page.locator(".cell", { has: page.locator('.cell-use[data-def="shortbow"]') });
  await expect(shortbowCell.locator(".drop")).toBeVisible();
  await shortbowCell.locator(".drop").click();

  // Turn-gated: the drop lands, then the modal opens over MY hex with a row
  // carrying the dropped instance's id.
  await expect
    .poll(() => page.evaluate((id) => window.game.pickupModal.rows.some((r) => r.id === id), shortbowId), TURN_GATED)
    .toBe(true);
  await expect.poll(() => page.evaluate(() => window.game.pickupModal.open), TURN_GATED).toBe(true);
  await expect(page.locator("#pickup-modal")).toBeVisible();

  const myRow = page.locator(`#pickup-modal .grow[data-ground="${shortbowId}"]`);
  await expect(myRow).toBeVisible();
  await expect(myRow.locator(".itemline")).toHaveText("Shortbow");
  await expect(myRow.locator(".typeline")).toContainText("ranged weapon");
  // A single gear item is count 1 — no ×N suffix.
  await expect(myRow.locator(".itemline")).not.toContainText("×");
  expect(await page.evaluate((id) => window.game.pickupModal.rows.find((r) => r.id === id)?.count, shortbowId)).toBe(1);
  expect(await page.evaluate(() => window.game.backpack.every((e) => e === null))).toBe(true);

  // Take it back — the row leaves the modal, and it returns to the backpack
  // (unequipped: items never auto-equip on pickup).
  await myRow.locator("button.yes").click();
  await expect
    .poll(() => page.evaluate((id) => window.game.pickupModal.rows.some((r) => r.id === id), shortbowId), TURN_GATED)
    .toBe(false);
  await expect
    .poll(() => page.evaluate(() => window.game.backpack.some((e) => e !== null && e.defId === "shortbow")), TURN_GATED)
    .toBe(true);
  // My specific dropped stack is gone from the ground.
  expect(await page.evaluate((id) => window.game.groundItems.some((g) => g.id === id), shortbowId)).toBe(false);
});

test("a rejected pickup row shows inline backpack-full feedback and disables its take button", async ({ page }) => {
  test.slow();

  // The monster-free e2e server can't hand a fresh join enough items to fill
  // the backpack, so the SERVER rejection is integration-tested
  // (test/integration/inventory_test.go) — this drives the CLIENT render path
  // (submitPickup -> false -> markPickupRejected -> the ".full" row) via the
  // window.game.rejectPickupRow test hook, then asserts the DOM.
  await seedRogue(page);
  await openPanel(page);

  const ranged = page.locator('.hex[data-slot="ranged-weapon"]');
  await expect(ranged).toHaveClass(/filled/);
  await ranged.click();
  await expect
    .poll(() => page.evaluate(() => window.game.backpack.some((e) => e !== null && e.defId === "shortbow")), TURN_GATED)
    .toBe(true);

  const shortbowId = await idInBackpack(page, "shortbow");
  const shortbowCell = page.locator(".cell", { has: page.locator('.cell-use[data-def="shortbow"]') });
  await expect(shortbowCell.locator(".drop")).toBeVisible();
  await shortbowCell.locator(".drop").click();

  await expect
    .poll(() => page.evaluate((id) => window.game.pickupModal.rows.some((r) => r.id === id), shortbowId), TURN_GATED)
    .toBe(true);

  await page.evaluate((id) => window.game.rejectPickupRow(id), shortbowId);

  const myRow = page.locator(`#pickup-modal .grow[data-ground="${shortbowId}"]`);
  await expect(myRow).toHaveClass(/rejected/);
  await expect(myRow.locator(".full")).toContainText("backpack full — drop something first");
  await expect(myRow.locator("button.yes")).toBeDisabled();
  expect(await page.evaluate((id) => window.game.pickupModal.rows.find((r) => r.id === id)?.rejected, shortbowId)).toBe(
    true,
  );
});

test("closing the modal leaves the remaining items on the ground", async ({ page }) => {
  test.slow();

  await seedRogue(page);
  await openPanel(page);

  // Unequip BOTH weapons and drop both — two ground stacks on my hex, so the
  // modal lists both my rows.
  const melee = page.locator('.hex[data-slot="melee-weapon"]');
  const ranged = page.locator('.hex[data-slot="ranged-weapon"]');
  await expect(melee).toHaveClass(/filled/);
  await melee.click();
  await expect(ranged).toHaveClass(/filled/);
  await ranged.click();
  await expect
    .poll(() => page.evaluate(() => window.game.backpack.filter((e) => e !== null).length), TURN_GATED)
    .toBe(2);

  const daggerId = await idInBackpack(page, "dagger");
  const shortbowId = await idInBackpack(page, "shortbow");
  const cellFor = (def: string) => page.locator(".cell", { has: page.locator(`.cell-use[data-def="${def}"]`) });
  await expect(cellFor("dagger").locator(".drop")).toBeVisible();
  await cellFor("dagger").locator(".drop").click();
  await expect(cellFor("shortbow").locator(".drop")).toBeVisible();
  await cellFor("shortbow").locator(".drop").click();

  // Both my dropped stacks show as modal rows (scoped by id — the shared
  // server accumulates other tests' drops in the global ground list).
  await expect
    .poll(
      () => page.evaluate((ids) => ids.every((id) => window.game.pickupModal.rows.some((r) => r.id === id)), [
        daggerId,
        shortbowId,
      ]),
      TURN_GATED,
    )
    .toBe(true);
  await expect(page.locator(`#pickup-modal .grow[data-ground="${daggerId}"]`)).toBeVisible();
  await expect(page.locator(`#pickup-modal .grow[data-ground="${shortbowId}"]`)).toBeVisible();

  // Close — my two items stay on the ground (they are not granted).
  await expect(page.locator(".pickup-close")).toBeVisible();
  await page.locator(".pickup-close").click();
  await expect.poll(() => page.evaluate(() => window.game.pickupModal.open), TURN_GATED).toBe(false);
  await expect(page.locator("#pickup-modal")).toBeHidden();
  const remaining = await page.evaluate(() => window.game.groundItems.map((g) => g.id));
  expect(remaining).toContain(daggerId);
  expect(remaining).toContain(shortbowId);
  expect(await page.evaluate(() => window.game.backpack.every((e) => e === null))).toBe(true);
});
