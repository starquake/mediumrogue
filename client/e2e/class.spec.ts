import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";
import { ClassMage, SpeciesElf } from "../src/protocol.gen";

declare global {
  interface Window {
    game: GameDebug;
  }
}

// This file runs on the CORE (monster-free) server — see playwright.config.ts's
// specs list, where "class" has no `monsters` entry.
//
// Every OTHER project's browser context is pre-seeded (playwright.config.ts's
// storageStateFor) with a "remembered" identity, so the start screen never
// appears for them and they all auto-join exactly as before that screen
// existed. THIS spec is the one place the screen itself gets exercised, so it
// deliberately clears storageState — a brand-new player, exactly as if
// they'd never visited before.
test.use({ storageState: { cookies: [], origins: [] } });

test("a brand-new player sees the start screen, picks class/species, and joins only on Enter", async ({
  page,
}) => {
  await page.goto("/");

  await expect(page.locator("#start-screen")).toBeVisible();

  // No join fires just from the screen being up and the map loading behind
  // it — window.game.me stays null for a real couple of seconds, not just
  // "hasn't happened yet on this tick".
  await page.waitForTimeout(2_000);
  expect(await page.evaluate(() => window.game.me)).toBeNull();
  await expect(page.locator("#start-screen")).toBeVisible();

  // Fighter/Human are preselected by default.
  await expect(page.locator('.card[data-class="fighter"]')).toHaveClass(/selected/);
  await expect(page.locator('.card[data-species="human"]')).toHaveClass(/selected/);

  await page.locator("#start-name").fill("Starquake");
  await page.locator('.card[data-class="mage"]').click();
  await page.locator('.card[data-species="elf"]').click();

  await expect(page.locator('.card[data-class="mage"]')).toHaveClass(/selected/);
  await expect(page.locator('.card[data-class="fighter"]')).not.toHaveClass(/selected/);
  await expect(page.locator('.card[data-species="elf"]')).toHaveClass(/selected/);
  await expect(page.locator('.card[data-species="human"]')).not.toHaveClass(/selected/);

  await page.locator("#start-enter").click();

  await expect(page.locator("#start-screen")).toBeHidden();
  await expect
    .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
    .toBe(true);
  await expect.poll(() => page.evaluate(() => window.game.class)).toBe(ClassMage);
  await expect.poll(() => page.evaluate(() => window.game.species)).toBe(SpeciesElf);
  expect(await page.evaluate(() => window.game.name)).toBe("Starquake");

  const entityId = await page.evaluate(() => window.game.me!.id);

  // Reload: identity is now remembered (join() persisted it to localStorage)
  // — the screen must not reappear, and the same character is reclaimed
  // (same entity id, same class/species/name) rather than a fresh one.
  await page.reload();

  await expect(page.locator("#start-screen")).toBeHidden();
  await expect
    .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
    .toBe(true);
  expect(await page.evaluate(() => window.game.me!.id)).toBe(entityId);
  await expect.poll(() => page.evaluate(() => window.game.class)).toBe(ClassMage);
  await expect.poll(() => page.evaluate(() => window.game.species)).toBe(SpeciesElf);
  expect(await page.evaluate(() => window.game.name)).toBe("Starquake");
});
