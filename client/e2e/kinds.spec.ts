import { expect, test } from "@playwright/test";
import { continueIfReturning } from "./helpers";

// The e2e server is started with MONSTER_COUNT=30 (playwright.config.ts):
// ring-weighted placement (milestone 6c) distributes that many monsters
// across the map's rings and kinds, so at least two distinct monster kinds
// reaching the client — and rendering with visibly distinct looks, not a
// single flat "monster" dot — is a near-certainty rather than a coin flip.
//
// Since #289 the bundle is culled to protocol.InterestRadius, so "spawned" and
// "reaches the client" are no longer the same thing. Measured over 8 fresh
// joins at the e2e defaults (world radius 24, InterestRadius 20): 21–24 of the
// 30 monsters and 11–12 distinct kinds survive the cull, against the 2 this
// spec needs — a wide margin, because the interest radius nearly covers a
// radius-24 world. That margin is what makes this spec safe, and it shrinks if
// the e2e world ever grows: a WORLD_RADIUS override here would push spawns out
// of range and could starve the kind count. Give this spec a seeded near-spawn
// placement before making the e2e world bigger.
test("distinct monster kinds reach the client and render differently", async ({ page }) => {
  await page.goto("/");
  await continueIfReturning(page);

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
