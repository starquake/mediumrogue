import { expect, test } from "@playwright/test";

import { continueIfReturning } from "./helpers";

// The health and energy globes and their keys (#322 slice 3). e2e because the
// subject is the HUD and two keybindings — neither exists below the browser.
//
// Every wait is metered on game state, never wall-clock — the de-race rule.
test("both globes render, and E drinks the energy draught", async ({ page }) => {
  await page.goto("/");
  await continueIfReturning(page);

  await expect.poll(() => page.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();
  await expect.poll(() => page.evaluate(() => window.game.maxEnergy)).toBeGreaterThan(0);

  await expect(page.locator("#globe-health")).toBeVisible();
  await expect(page.locator("#globe-energy")).toBeVisible();

  // Ready: the corner shows the draught's own glyph rather than a number, so
  // the shape does not jump when a cooldown appears. Assert the glyph is
  // actually there — an empty corner would satisfy "no number".
  await expect(page.locator("#globe-energy .cd svg.hud-glyph")).toBeVisible();

  await page.keyboard.press("KeyE");

  // The cooldown starting is the server's acknowledgement.
  await expect
    .poll(() => page.evaluate(() => window.game.energyPotionReadyIn), { timeout: 10_000 })
    .toBeGreaterThan(0);

  await expect.poll(() => page.locator("#globe-energy .cd").textContent()).toMatch(/^\d+$/);

  // Independent cooldowns: the health draught is still available.
  expect(await page.evaluate(() => window.game.healthPotionReadyIn), "health must not cool with energy").toBe(0);
});
