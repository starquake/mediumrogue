import { expect, test } from "@playwright/test";

import { continueIfReturning } from "./helpers";

// Evade (#322): the mechanic everyone has. This is e2e rather than a unit test
// because the whole point is the INTERACTION — a key that arms, an overlay that
// shows the reach, and a click that spends it. None of those exist below the
// browser.
//
// Every wait is metered on game state (window.game fields flipping), never
// wall-clock — the de-race rule.
test("Space arms evade, a click spends it, and the cooldown starts", async ({ page }) => {
  await page.goto("/");
  await continueIfReturning(page);

  await expect.poll(() => page.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();
  await expect.poll(() => page.evaluate(() => window.game.evadeReadyIn)).toBe(0);

  // Nobody learned anything: evade is universal, so a fresh character has it
  // and it is NOT in the skill list.
  const ids = await page.evaluate(() => window.game.skills.map((s) => s.id));
  expect(ids, "evade is universal — it has no panel row to learn").not.toContain("evade");

  // Nothing is painted until evade is armed — an always-on overlay is
  // wallpaper.
  expect(await page.evaluate(() => window.game.skillTiles.length), "no overlay before arming").toBe(0);

  await page.keyboard.press("Space");
  await expect.poll(() => page.evaluate(() => window.game.armedSkill())).toBe("evade");

  // Arming paints the reach, so the limit is visible BEFORE committing.
  await expect.poll(() => page.evaluate(() => window.game.skillTiles.length)).toBeGreaterThan(0);

  const from = await page.evaluate(() => window.game.me?.hex ?? null);
  if (from === null) {
    throw new Error("no player hex");
  }

  // Click a painted tile: that is what spends the arm.
  const dest = await page.evaluate(() => window.game.skillTiles[0] ?? null);
  if (dest === null) {
    throw new Error("evade armed but painted no tiles");
  }

  expect(await page.evaluate(() => window.game.hexToScreen !== null)).toBe(true);

  const at = await page.evaluate((d) => window.game.hexToScreen!(d.q, d.r), dest);
  await page.mouse.click(at.x, at.y);

  // The cooldown starting is the server's acknowledgement — it is stamped by
  // the same commit that moves the player, so polling it avoids racing the
  // move animation.
  await expect
    .poll(() => page.evaluate(() => window.game.evadeReadyIn), { timeout: 10_000 })
    .toBeGreaterThan(0);

  const to = await page.evaluate(() => window.game.me?.hex ?? null);
  expect(to, "evade should have moved the player off their hex").not.toEqual(from);

  // The ball is the only place the cooldown is visible, since a universal
  // mechanic has no action-bar slot.
  await expect(page.locator("#evade-ball")).toBeVisible();
  await expect.poll(() => page.locator("#evade-ball .n").textContent()).not.toBe("✦");

  // Cooling: the overlay clears, because tiles you cannot reach are a lie.
  await expect.poll(() => page.evaluate(() => window.game.skillTiles.length)).toBe(0);
});

test("pressing Space twice cancels the arm without evading", async ({ page }) => {
  await page.goto("/");
  await continueIfReturning(page);

  await expect.poll(() => page.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();
  await expect.poll(() => page.evaluate(() => window.game.evadeReadyIn)).toBe(0);

  const from = await page.evaluate(() => window.game.me?.hex ?? null);

  await page.keyboard.press("Space");
  await expect.poll(() => page.evaluate(() => window.game.armedSkill())).toBe("evade");

  await page.keyboard.press("Space");
  await expect.poll(() => page.evaluate(() => window.game.armedSkill())).toBeNull();
  await expect.poll(() => page.evaluate(() => window.game.skillTiles.length)).toBe(0);

  // Give the world a couple of turns to prove nothing was queued.
  const start = await page.evaluate(() => window.game.turn);
  await expect.poll(() => page.evaluate(() => window.game.turn)).toBeGreaterThan(start + 1);

  expect(await page.evaluate(() => window.game.evadeReadyIn), "no evade should have fired").toBe(0);
  expect(await page.evaluate(() => window.game.me?.hex ?? null), "the player should not have moved").toEqual(from);
});
