import { expect, test } from "@playwright/test";

import { gotoReady } from "./helpers";

// returning.spec.ts (#303): the start screen a RETURNING player now sees.
//
// Unlike the actives specs, this one is genuinely reachable: it needs only a
// stored identity, which the e2e storage-state template already provides — so
// every project in playwright.config.ts starts as a returning player.

test("a returning player is offered Continue, not the creation form", async ({ page }) => {
  await page.goto("/");

  await expect(page.locator("#returning")).toBeVisible();
  await expect(page.locator("#creation")).toBeHidden();
  await expect(page.locator("#continue-btn")).toBeVisible();

  // Continue commits, and the world comes up.
  await page.click("#continue-btn");
  await expect.poll(() => page.evaluate(() => window.game.connected)).toBe(true);
  await expect(page.locator("#start-screen")).toBeHidden();
});

test("Start over asks first, and offers the link back", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator("#returning")).toBeVisible();

  // The confirm is closed until asked for — this is the destructive path.
  await expect(page.locator("#startover-confirm")).toBeHidden();

  await page.click("#startover-btn");
  await expect(page.locator("#startover-confirm")).toBeVisible();
  await expect.poll(() => page.evaluate(() => window.game.startOverConfirmOpen)).toBe(true);

  // The link is the whole reason "recoverable" is true: it IS the token, and
  // clearing the identity is what would otherwise make the character
  // unreachable. An empty box here would make the promise false.
  const link = await page.inputValue("#startover-link");
  expect(link).toContain("#t=");

  // Cancel leaves the character alone.
  await page.click("#startover-cancel");
  await expect(page.locator("#startover-confirm")).toBeHidden();
  await expect(page.locator("#returning")).toBeVisible();
});

test("confirming Start over drops to the creation form", async ({ page }) => {
  await page.goto("/");
  await page.click("#startover-btn");
  await page.click("#startover-go");

  await expect(page.locator("#creation")).toBeVisible();
  await expect(page.locator("#returning")).toBeHidden();
  await expect.poll(() => page.evaluate(() => window.game.startMode)).toBe("fresh");
});
