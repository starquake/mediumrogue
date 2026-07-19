import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";

declare global {
  interface Window {
    game: GameDebug;
  }
}

// The near-sighted skills panel (#124). Every wait is metered on GAME state
// (window.game fields flipping), never wall-clock — the de-race rule.
test("the skills panel shows learnable skills and hides locked ones", async ({ page }) => {
  await page.goto("/");

  await expect.poll(() => page.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();
  // A bundle must have landed before the panel has anything to render.
  await expect.poll(() => page.evaluate(() => window.game.turn)).toBeGreaterThanOrEqual(0);

  // Near-sightedness, asserted from the wire rather than the DOM: the
  // prereq-gated skill must not be present at all for a fresh character.
  const ids = await page.evaluate(() => window.game.skills.map((s) => s.id));
  expect(ids, "a fresh character should see the unlocked roots").toContain("combat-training");
  expect(ids, "weak-spot is prereq-gated and must never reach the client").not.toContain("weak-spot");

  // The panel is default-closed; open it and CONFIRM before touching contents.
  await page.keyboard.press("KeyK");
  await expect.poll(() => page.evaluate(() => window.game.skillsPanelOpen)).toBe(true);
  await expect(page.locator("#skills-panel")).toBeVisible();

  // Every row the client received is rendered, and no locked row exists.
  await expect(page.locator(".skill-row")).toHaveCount(ids.length);
  await expect(page.locator(".skill-row", { hasText: "Weak Spot" })).toHaveCount(0);

  // A fresh character has no points, so Learn is offered but disabled — the
  // button exists (the skill IS learnable) and simply can't be afforded yet.
  await expect.poll(() => page.evaluate(() => window.game.skillPoints)).toBe(0);
  await expect(page.locator(".skill-learn").first()).toBeDisabled();

  // Toggling is symmetric.
  await page.keyboard.press("KeyK");
  await expect.poll(() => page.evaluate(() => window.game.skillsPanelOpen)).toBe(false);
});
