import { expect, test, type Page } from "@playwright/test";

import { seedIdentity } from "./helpers";

// throwables.spec.ts (#271): the targeted-consumable client paths — arming a
// flask's throw and consuming a map click as its aim, and using a scroll of
// recall — driven through the real embedded-client binary. This spec's server
// is configured with STARTER_CONSUMABLES (playwright.config.ts) so a fresh
// join carries a flask and a scroll; the standard monster-free world has no
// other way to hand a player a consumable (see inventory.spec.ts).
//
// Every wait is metered on window.game state (the de-race rule), never wall
// clock; the panel is opened and CONFIRMED before any cell click; lists render
// under <Index> so the cell DOM is stable across the 250ms turn bundles.

const TURN_GATED = { timeout: 20_000 };

const flaskDef = "flask-of-fire";
const scrollDef = "scroll-of-recall";

async function seedWithStarterKit(page: Page): Promise<void> {
  await seedIdentity(page, { class: "fighter" });
  await page.goto("/");
  await expect.poll(() => page.evaluate(() => window.game.me !== null && window.game.connected), TURN_GATED).toBe(true);
  // The starter flask and scroll must land in the backpack before the panel
  // can render their cells.
  await expect
    .poll(() => page.evaluate((d) => window.game.backpack.some((e) => e !== null && e.defId === d), flaskDef), TURN_GATED)
    .toBe(true);
  await expect
    .poll(
      () => page.evaluate((d) => window.game.backpack.some((e) => e !== null && e.defId === d), scrollDef),
      TURN_GATED,
    )
    .toBe(true);
}

async function openPanel(page: Page): Promise<void> {
  await expect(page.locator("#toggle-inventory")).toBeVisible();
  if (!(await page.evaluate(() => window.game.panelOpen))) {
    await page.locator("#toggle-inventory").click();
  }
  await expect.poll(() => page.evaluate(() => window.game.panelOpen), TURN_GATED).toBe(true);
  await expect(page.locator("#character-panel")).toBeVisible();
}

async function backpackCount(page: Page, defId: string): Promise<number> {
  return page.evaluate(
    (d) => window.game.backpack.reduce((n, e) => (e !== null && e.defId === d ? n + e.count : n), 0),
    defId,
  );
}

test("arming a flask consumes the next map click as a throw, spending the flask", async ({ page }) => {
  await seedWithStarterKit(page);
  await openPanel(page);

  // The flask's backpack cell offers THROW (its title verb), not drink.
  const flaskCell = page.locator(`.cell-use[data-def="${flaskDef}"]`);
  await expect(flaskCell).toBeVisible();
  await expect(flaskCell).toHaveAttribute("title", "throw");

  const flaskId = await page.evaluate(
    (d) => window.game.backpack.find((e) => e !== null && e.defId === d)?.id ?? 0,
    flaskDef,
  );
  expect(flaskId).toBeGreaterThan(0);

  // Clicking the flask cell ARMS the throw (and closes the panel so the map is
  // clickable) — window.game.armedThrow reflects the armed flask.
  await flaskCell.click();
  await expect.poll(() => page.evaluate(() => window.game.armedThrow), TURN_GATED).toBe(flaskId);
  await expect.poll(() => page.evaluate(() => window.game.panelOpen), TURN_GATED).toBe(false);

  // The next map click is the throw's aim hex — throwing at the player's own
  // hex is in range and always visible, so the action is accepted and the
  // flask is spent when the turn resolves. tapHex routes the click through the
  // same clickTarget the canvas uses.
  await page.evaluate(() => {
    const me = window.game.me;
    if (me !== null) void window.game.tapHex(me.hex.q, me.hex.r);
  });

  await expect.poll(() => backpackCount(page, flaskDef), TURN_GATED).toBe(0);
  await expect.poll(() => page.evaluate(() => window.game.armedThrow), TURN_GATED).toBeNull();
});

test("a scroll of recall is used from the backpack and consumed", async ({ page }) => {
  await seedWithStarterKit(page);
  await openPanel(page);

  // The scroll's cell offers RECALL, not drink.
  const scrollCell = page.locator(`.cell-use[data-def="${scrollDef}"]`);
  await expect(scrollCell).toBeVisible();
  await expect(scrollCell).toHaveAttribute("title", "recall");

  await expect.poll(() => backpackCount(page, scrollDef), TURN_GATED).toBe(1);

  // Using the scroll teleports the player to safety and consumes it when the
  // turn resolves.
  await scrollCell.click();
  await expect.poll(() => backpackCount(page, scrollDef), TURN_GATED).toBe(0);
});
