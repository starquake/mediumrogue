import { expect, test } from "@playwright/test";

import { continueIfReturning } from "./helpers";

// menu.spec.ts (#367): the menu behind the HUD button — mouse help, and the
// way back to character creation.
test("the menu documents the mouse and offers a new character behind a confirm", async ({ page }) => {
  await page.goto("/");
  await continueIfReturning(page);

  await expect.poll(() => page.evaluate(() => window.game.connected)).toBe(true);

  // A first-time visitor gets the menu opened FOR them (seenControls unset),
  // and every e2e run is a fresh browser context — so close it, then prove the
  // HUD button reopens it. Asserting it starts hidden would be asserting the
  // opposite of the intended first-run behaviour.
  const overlay = page.locator("#controls-overlay");
  await expect(overlay).toBeVisible();

  await page.locator("#controls-close").click();
  await expect(overlay).toBeHidden();

  await page.locator("#toggle-help").click();
  await expect(overlay).toBeVisible();

  // The mouse section is the gap #367 existed to close — the overlay used to
  // document keys only.
  await expect(overlay).toContainText("Click a tile");
  await expect(overlay).toContainText("click a monster");

  // Destructive from this browser's side, so it is two steps, not one.
  const confirm = page.locator("#new-char-confirm");
  await expect(confirm).toBeHidden();

  await page.locator("#new-character").click();
  await expect(confirm).toBeVisible();

  // The warning must name what survives: the character is not deleted, it is
  // only unreachable without its link.
  await expect(confirm).toContainText("stays in the world");

  // Cancel really cancels — the identity is untouched.
  await page.locator("#new-character-cancel").click();
  await expect(confirm).toBeHidden();
  expect(await page.evaluate(() => window.game.connected)).toBe(true);
});

// Buttons inside an overlay that is `pointer-events: none` are only clickable
// because they opt back in. Without that they render, look enabled, and
// silently do nothing — so this asserts the click LANDS, not just that the
// element exists.
test("the menu's buttons are clickable despite the overlay ignoring pointer events", async ({ page }) => {
  await page.goto("/");
  await continueIfReturning(page);

  await expect.poll(() => page.evaluate(() => window.game.connected)).toBe(true);

  // Opened for us on a first visit; no need to click the HUD button.
  await expect(page.locator("#controls-overlay")).toBeVisible();
  await page.locator("#new-character").click();

  await expect(page.locator("#new-char-confirm")).toBeVisible();
});
