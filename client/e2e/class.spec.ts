import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";
import { ClassFighter, ClassMage, ClassRogue } from "../src/protocol.gen";

declare global {
  interface Window {
    game: GameDebug;
  }
}

// This file runs on the CORE (monster-free) server — see playwright.config.ts's
// combatSpecs regex, which this filename does not match. Class selection/
// exposure has nothing to do with combat, so it doesn't need to contend with
// the fixed, non-respawning monster pool the combat project shares.

test("a fresh join exposes window.game.class as a valid, deterministic class", async ({ page }) => {
  await page.goto("/");

  await expect
    .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
    .toBe(true);

  // No picker interaction here — a brand-new page load joins with whichever
  // class is selected by default (Fighter: src/main.ts calls
  // selectClass(ClassFighter) at module init, before any click). That makes
  // this deterministic on any server/run, not a guess: the assertion is a
  // real round trip through join -> server normalization -> the turn bundle
  // -> window.game.class, so it would fail if that plumbing broke (e.g. the
  // server stopped echoing Class, or the client stopped reading mine.class).
  const cls = await page.evaluate(() => window.game.class);
  expect([ClassFighter, ClassRogue, ClassMage]).toContain(cls);
  expect(cls).toBe(ClassFighter);
});

test("clicking a class-picker button before join changes window.game.class", async ({ page }) => {
  // src/main.ts shows the picker (classPickerEl.hidden = false) as soon as
  // the page decides this is a new player, then hides it again and fires
  // join() right after `await fetchMap()` resolves — in practice that round
  // trip is fast enough (a local dev/CI server) that a real click can miss
  // the window entirely. Delay the map response deliberately so the picker
  // is reliably still visible when Playwright's click lands, without
  // touching src/main.ts itself — this is testing the picker-to-join wiring,
  // not how fast the network happens to be.
  await page.route("**/api/map", async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 300));
    await route.continue();
  });

  await page.goto("/");

  const rogueButton = page.locator('#class-picker button[data-class="rogue"]');
  await expect(rogueButton).toBeVisible();
  await rogueButton.click();

  await expect
    .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
    .toBe(true);

  await expect.poll(() => page.evaluate(() => window.game.class)).toBe(ClassRogue);
});
