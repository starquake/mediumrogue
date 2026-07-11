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

test("a stored species choice (from a prior start-screen join) rides a fresh join", async ({ page }) => {
  // Seed a "returning player" identity (no token) requesting Dwarf — same
  // technique as ranged.spec.ts/gear.spec.ts, deterministic without touching
  // the start screen itself (whose own species-selection UX is exercised in
  // class.spec.ts). An empty stored token still reaches the server as a
  // brand-new join (Join() only reclaims an existing entity for a token it
  // recognizes), but with the requested species.
  await page.addInitScript(() => {
    localStorage.setItem("mediumrogue.identity", JSON.stringify({ entityId: 0, token: "", species: "dwarf" }));
  });

  await page.goto("/");

  await expect
    .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
    .toBe(true);

  await expect.poll(() => page.evaluate(() => window.game.species)).toBe(SpeciesDwarf);
});
