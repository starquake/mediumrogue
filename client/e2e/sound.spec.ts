import { expect, test } from "@playwright/test";

import { gotoReady, continueIfReturning } from "./helpers";

// sound.spec.ts (#298): the audio a test can verify without hearing anything.
//
// window.game.audio is the whole point — an e2e has no ears, so "a sound
// played" is only observable as a counter. These specs assert the RULES
// (unlock gating, mute, the panel hook), not that a speaker made a noise.

test("sound stays locked until a gesture, then plays", async ({ page }) => {
  await gotoReady(page);

  // Before any input the browser's autoplay policy has not been released. This
  // is the state a RETURNING player lands in — main.ts's isNewPlayer skips the
  // start screen entirely when a stored identity matches, so they reach the
  // world having clicked nothing.
  await expect.poll(() => page.evaluate(() => window.game.audio.unlocked)).toBe(false);
  expect(await page.evaluate(() => window.game.audio.played)).toBe(0);

  // Any gesture releases it. The HUD sound button is a real one and is also
  // the thing under test below.
  await page.click("#toggle-sound");
  await expect.poll(() => page.evaluate(() => window.game.audio.unlocked)).toBe(true);
});

test("the HUD toggle mutes, persists, and silences playback", async ({ page }) => {
  await gotoReady(page);

  await expect(page.locator("#toggle-sound")).toBeVisible();
  await expect(page.locator("#toggle-sound")).toHaveText("sound: on");

  await page.click("#toggle-sound");
  await expect(page.locator("#toggle-sound")).toHaveText("sound: off");
  await expect.poll(() => page.evaluate(() => window.game.audio.muted)).toBe(true);

  // Muted means nothing starts, whatever happens in the world.
  const before = await page.evaluate(() => window.game.audio.played);
  const opened = await page.evaluate(() => {
    window.game.tapHex(0, 0);
    return window.game.audio.played;
  });
  expect(opened).toBe(before);

  // The setting survives a reload — it is stored, not just held in memory.
  await page.reload();
  await continueIfReturning(page);
  await expect.poll(() => page.evaluate(() => window.game.audio.muted)).toBe(true);
  await expect(page.locator("#toggle-sound")).toHaveText("sound: off");
});

test("opening a panel plays a sound once unlocked", async ({ page }) => {
  await gotoReady(page);

  // Unlock with a gesture that is not the panel, so the assertion below is
  // about the panel and not about the unlock click.
  await page.click("#toggle-sound"); // mutes
  await page.click("#toggle-sound"); // unmutes again — now unlocked and audible
  await expect.poll(() => page.evaluate(() => window.game.audio.unlocked)).toBe(true);
  await expect.poll(() => page.evaluate(() => window.game.audio.muted)).toBe(false);

  const before = await page.evaluate(() => window.game.audio.played);
  await page.keyboard.press("i");
  await expect.poll(() => page.evaluate(() => window.game.panelOpen)).toBe(true);

  await expect.poll(() => page.evaluate(() => window.game.audio.played)).toBeGreaterThan(before);
  await expect.poll(() => page.evaluate(() => window.game.audio.last)).toBe("panelOpen");
});
