import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";

declare global {
  interface Window {
    game: GameDebug;
  }
}

test("a keyboard step moves my entity on the next turn", async ({ page }) => {
  await page.goto("/");

  // Joined, connected, and standing somewhere.
  await expect
    .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
    .toBe(true);
  const start = await page.evaluate(() => window.game.me?.hex);
  expect(start).toBeDefined();

  // My entity is in the turn bundle.
  await expect.poll(() => page.evaluate(() => window.game.entities)).toBeGreaterThan(0);

  // Try each movement key: whichever direction is walkable from spawn gets
  // queued (latest valid intent wins server-side; rejected ones don't queue).
  for (const key of ["KeyW", "KeyE", "KeyD", "KeyS", "KeyA", "KeyQ"]) {
    await page.keyboard.press(key);
  }

  // The server-authoritative position changes on a subsequent turn bundle.
  // TURN_INTERVAL is 250ms in the e2e server.
  await expect
    .poll(
      async () => {
        const hex = await page.evaluate(() => window.game.me?.hex);

        return hex !== undefined && (hex.q !== start!.q || hex.r !== start!.r);
      },
      { timeout: 10_000 },
    )
    .toBe(true);
});

test("identity survives a page reload", async ({ page }) => {
  await page.goto("/");
  await expect.poll(() => page.evaluate(() => window.game.me !== null)).toBe(true);
  const firstID = await page.evaluate(() => window.game.me?.id);

  await page.reload();
  await expect.poll(() => page.evaluate(() => window.game.me !== null)).toBe(true);
  const secondID = await page.evaluate(() => window.game.me?.id);

  expect(secondID).toBe(firstID);
});
