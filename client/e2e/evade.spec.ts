import { expect, test } from "@playwright/test";

import { continueIfReturning } from "./helpers";

// Evade (#322): the mechanic everyone has. This is e2e rather than a unit test
// because the whole point is the KEY — a keypress that carries no destination,
// aimed by wherever the pointer happens to be. Neither half exists below the
// browser.
//
// Every wait is metered on game state (window.game fields flipping), never
// wall-clock — the de-race rule.
test("Space evades to the hex under the cursor and starts its cooldown", async ({ page }) => {
  await page.goto("/");
  await continueIfReturning(page);

  await expect.poll(() => page.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();
  await expect.poll(() => page.evaluate(() => window.game.evadeReadyIn)).toBe(0);

  // Nobody learned anything: evade is universal, so a fresh character has it
  // and it is NOT in the skill list.
  const ids = await page.evaluate(() => window.game.skills.map((s) => s.id));
  expect(ids, "evade is universal — it has no panel row to learn").not.toContain("evade");

  const from = await page.evaluate(() => window.game.me?.hex ?? null);

  // The pointer is the aim. Hover a few tiles to the right of centre, which is
  // where the follow camera keeps the player.
  const canvas = await page.locator("canvas").boundingBox();
  if (canvas === null) {
    throw new Error("no canvas to aim across");
  }

  await page.mouse.move(canvas.x + canvas.width / 2 + 96, canvas.y + canvas.height / 2);
  await page.keyboard.press("Space");

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
});

test("evade with the cursor off the map does nothing", async ({ page }) => {
  await page.goto("/");
  await continueIfReturning(page);

  await expect.poll(() => page.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();
  await expect.poll(() => page.evaluate(() => window.game.evadeReadyIn)).toBe(0);

  const from = await page.evaluate(() => window.game.me?.hex ?? null);

  // Park the pointer over the chat panel: there is no hex under it, so the
  // keypress has nothing to aim at and must not guess a direction.
  await page.locator("#chat-input").hover();
  await page.keyboard.press("Space");

  // Give the world a couple of turns to prove nothing was queued.
  const start = await page.evaluate(() => window.game.turn);
  await expect.poll(() => page.evaluate(() => window.game.turn)).toBeGreaterThan(start + 1);

  expect(await page.evaluate(() => window.game.evadeReadyIn), "no evade should have fired").toBe(0);
  expect(await page.evaluate(() => window.game.me?.hex ?? null), "the player should not have moved").toEqual(from);
});
