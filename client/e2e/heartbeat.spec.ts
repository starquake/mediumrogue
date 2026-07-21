import { expect, test } from "@playwright/test";

test("the client receives named heartbeat events while turns also flow", async ({ page }) => {
  await page.goto("/");

  await expect.poll(() => page.evaluate(() => window.game.connected)).toBe(true);

  // Named heartbeats (HEARTBEAT_INTERVAL=500ms) are observable by the client.
  await expect
    .poll(() => page.evaluate(() => window.game.heartbeats), { timeout: 10_000 })
    .toBeGreaterThan(0);

  // Turns still advance and the connection stays up alongside the heartbeats.
  const turn = await page.evaluate(() => window.game.turn);
  await expect.poll(() => page.evaluate(() => window.game.turn), { timeout: 10_000 }).toBeGreaterThan(turn);
  expect(await page.evaluate(() => window.game.connected)).toBe(true);
});
