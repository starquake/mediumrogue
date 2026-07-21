import { expect, test } from "@playwright/test";

import { seedIdentity } from "./helpers";

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

// #57: the Survival tree has content for the first time. It shipped empty in
// v1 (#124), so the panel has only ever rendered its `nothing available yet`
// fallback — this asserts the real branch, since a fallback that never turns
// off is exactly the bug an empty tree could hide.
test("every tree renders real rows — the Survival tree is no longer empty", async ({ page }) => {
  await seedIdentity(page, { class: "fighter" });

  await page.goto("/");
  await expect.poll(() => page.evaluate(() => window.game.me !== null && window.game.connected)).toBe(true);
  await expect.poll(() => page.evaluate(() => window.game.skills.length)).toBeGreaterThan(0);

  // The wire carries a survival-tree skill at all.
  const trees = await page.evaluate(() => window.game.skills.map((s) => s.tree));
  expect(trees, "the survival tree must reach the client").toContain("survival");

  await page.keyboard.press("KeyK");
  await expect.poll(() => page.evaluate(() => window.game.skillsPanelOpen)).toBe(true);
  await expect(page.locator("#skills-panel")).toBeVisible();

  // No tree renders the empty fallback any more.
  await expect(page.locator(".skill-empty")).toHaveCount(0);

  // …and Survivalist specifically is on screen, not merely on the wire.
  await expect(page.locator(".skill-row", { hasText: "Survivalist" })).toHaveCount(1);
});

// #185: the action bar stays hidden for a fresh player (no learned actives).
// The filled-bar + trigger path needs a learned Blink, which requires skill
// points a fresh join on the monster-free core server never has — covered by
// the server SkillView test and window.game instead.
test("the action bar is hidden with no learned actives", async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem("mediumrogue.identity", JSON.stringify({ entityId: 0, token: "", class: "fighter" }));
    localStorage.setItem("mediumrogue.seenControls", "1");
  });

  await page.goto("/");
  await expect.poll(() => page.evaluate(() => window.game.me !== null && window.game.connected)).toBe(true);
  await expect.poll(() => page.evaluate(() => window.game.skills.length)).toBeGreaterThan(0);

  await expect(page.locator("#action-bar")).toBeHidden();
  expect(await page.evaluate(() => window.game.armedSkill())).toBeNull();
});
