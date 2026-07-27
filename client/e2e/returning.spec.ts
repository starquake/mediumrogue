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

// TestStartOverActuallyStartsOver — the regression this spec file was missing.
//
// The first version stopped at "the creation form appeared", which passed
// while the join that followed still RECLAIMED the abandoned character: the
// join read a copy of the identity captured at page load, so clearing
// localStorage changed nothing it looked at. Reported as "the character is not
// reset, I'm still level 3".
//
// So this asserts the only thing that actually matters: the character you end
// up playing is a NEW one.
test("Start over yields a genuinely new character, not the old one reclaimed", async ({ page }) => {
  await page.goto("/");

  // Establish the old character and remember its token.
  await page.click("#continue-btn");
  await expect.poll(() => page.evaluate(() => window.game.connected)).toBe(true);

  const oldToken = await page.evaluate(
    () => (JSON.parse(localStorage.getItem("mediumrogue.identity") ?? "{}") as { token?: string }).token ?? "",
  );
  expect(oldToken).not.toBe("");

  // Start over, and pick a name that could not belong to the old character.
  await page.reload();
  await page.click("#startover-btn");
  await page.click("#startover-go");
  await page.locator("#start-name").fill("brandnew");
  await page.locator("#start-enter").click();

  await expect.poll(() => page.evaluate(() => window.game.connected)).toBe(true);

  // A different token means a different character — the reclaim did not fire.
  const newToken = await page.evaluate(
    () => (JSON.parse(localStorage.getItem("mediumrogue.identity") ?? "{}") as { token?: string }).token ?? "",
  );
  expect(newToken).not.toBe(oldToken);

  // And it is the character just created, at the start of progression.
  await expect.poll(() => page.evaluate(() => window.game.name)).toBe("brandnew");
  await expect.poll(() => page.evaluate(() => window.game.level)).toBe(1);
  await expect.poll(() => page.evaluate(() => window.game.xp)).toBe(0);
});
