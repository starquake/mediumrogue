import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";
import { SpeciesDwarf, SpeciesElf, SpeciesHuman } from "../src/protocol.gen";

declare global {
  interface Window {
    game: GameDebug;
  }
}

// This file runs on the CORE (monster-free) server — see playwright.config.ts's
// specs list, where "species" has no `monsters` entry. Species selection/
// exposure has nothing to do with combat, so it doesn't need to contend with a
// fixed, non-respawning monster pool — mirrors class.spec.ts exactly.

test("a fresh join exposes window.game.species as a valid, deterministic default", async ({ page }) => {
  await page.goto("/");

  await expect
    .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
    .toBe(true);

  // No picker interaction here — a brand-new page load joins with whichever
  // species is selected by default (Human: src/main.ts calls
  // selectSpecies(SpeciesHuman) at module init, before any click). That makes
  // this deterministic on any server/run, not a guess: the assertion is a real
  // round trip through join -> server normalization -> the turn bundle ->
  // window.game.species, so it would fail if that plumbing broke (e.g. the
  // server stopped echoing Species, or the client stopped reading
  // mine.species).
  const species = await page.evaluate(() => window.game.species);
  expect([SpeciesHuman, SpeciesElf, SpeciesDwarf]).toContain(species);
  expect(species).toBe(SpeciesHuman);
});

test("clicking a species-picker button before join changes window.game.species", async ({ page }) => {
  // Same delay trick as class.spec.ts: src/main.ts shows the picker as soon as
  // the page decides this is a new player, then hides it again and fires
  // join() right after `await fetchMap()` resolves — in practice that round
  // trip is fast enough (a local dev/CI server) that a real click can miss the
  // window entirely. Delay the map response deliberately so the picker is
  // reliably still visible when Playwright's click lands, without touching
  // src/main.ts itself — this is testing the picker-to-join wiring, not how
  // fast the network happens to be.
  await page.route("**/api/map", async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 300));
    await route.continue();
  });

  await page.goto("/");

  const dwarfButton = page.locator('#species-picker button[data-species="dwarf"]');
  await expect(dwarfButton).toBeVisible();
  await dwarfButton.click();

  await expect
    .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
    .toBe(true);

  await expect.poll(() => page.evaluate(() => window.game.species)).toBe(SpeciesDwarf);
});
