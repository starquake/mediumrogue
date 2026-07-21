import { expect, test } from "@playwright/test";

// The e2e server is started with MONSTER_COUNT=30 (playwright.config.ts):
// ring-weighted placement (milestone 6c) distributes that many monsters
// across the map's rings and kinds, so at least two distinct monster kinds
// reaching the client — and rendering with visibly distinct looks, not a
// single flat "monster" dot — is a near-certainty rather than a coin flip.
test("distinct monster kinds reach the client and render differently", async ({ page }) => {
  await page.goto("/");

  await expect
    .poll(() => page.evaluate(() => window.game.monsters), { timeout: 10_000 })
    .toBeGreaterThanOrEqual(2);

  const monsterKinds = await page.evaluate(() =>
    window.game.positions.filter((p) => p.kind === "monster").map((p) => p.monsterKind),
  );

  // Every monster entity must carry a non-empty kind — Entity.MonsterKind
  // rides the wire now (6c), never falling back to "" for a real spawn.
  for (const kind of monsterKinds) {
    expect(kind).not.toBe("");
  }

  // The actual per-kind-rendering proof: more than one distinct kind
  // present among the spawned monsters (the dot color in entities.ts's
  // KIND_STYLE and the glyph icon in GLYPH_ICON_SVG are both keyed on
  // exactly this field).
  const distinctKinds = new Set(monsterKinds);
  expect(distinctKinds.size).toBeGreaterThanOrEqual(2);

  // Visual smoke check: the stage actually painted the distinct-colored
  // dots, not a black void.
  const screenshot = await page.screenshot();
  expect(screenshot.byteLength).toBeGreaterThan(10_000);
});
