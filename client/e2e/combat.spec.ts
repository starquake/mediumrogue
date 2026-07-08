import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";
import { EntityMonster } from "../src/protocol.gen";
import type { Hex } from "../src/protocol.gen";

declare global {
  interface Window {
    game: GameDebug;
  }
}

// This file runs against the COMBAT server (see playwright.config.ts —
// filenames matching /(monsters|combat)\.spec\.ts$/ route to the server
// started with MONSTER_COUNT=3).

test("bumping into a monster deals damage, observable via window.game.hp", async ({ page }) => {
  await page.goto("/");

  await expect
    .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
    .toBe(true);
  await expect.poll(() => page.evaluate(() => window.game.monsters)).toBeGreaterThanOrEqual(1);

  // Snapshot every entity's HP before engaging. Success is "some entity's HP
  // dropped below this baseline" — HP only ever decreases from combat, never
  // rises above its starting value, so this can't be satisfied except by real
  // damage happening (not a tautology against a hardcoded max).
  const baseline = await page.evaluate(() => ({ ...window.game.hp }));

  // Every poll: re-pick whichever monster is currently nearest my entity and
  // tapHex toward it. Monsters hunt the nearest player too (server-side,
  // recomputed every turn), so re-targeting each round — rather than a single
  // tapHex at a fixed destination — converges reliably even as both sides
  // move and spawn positions vary between runs.
  const chase = async (base: Record<number, number>): Promise<boolean> => {
    await page.evaluate((monsterKind) => {
      const me = window.game.me;
      if (me === null) {
        return;
      }

      const monsters = window.game.positions.filter((p) => p.kind === monsterKind);
      if (monsters.length === 0) {
        return;
      }

      const dist = (a: Hex, b: Hex): number => {
        const dq = a.q - b.q;
        const dr = a.r - b.r;
        const ds = -dq - dr;

        return (Math.abs(dq) + Math.abs(dr) + Math.abs(ds)) / 2;
      };

      let nearest = monsters[0]!;
      let bestDist = dist(me.hex, nearest.hex);
      for (const m of monsters.slice(1)) {
        const d = dist(me.hex, m.hex);
        if (d < bestDist) {
          nearest = m;
          bestDist = d;
        }
      }

      window.game.tapHex(nearest.hex.q, nearest.hex.r);
    }, EntityMonster);

    return page.evaluate((b) => {
      return Object.entries(window.game.hp).some(([id, hp]) => {
        const before = b[Number(id)];

        return before !== undefined && hp < before;
      });
    }, base);
  };

  // TURN_INTERVAL is 250ms in the e2e server; poll a bit slower than that so
  // each round's tapHex has landed in a turn bundle before the next retarget.
  await expect
    .poll(() => chase(baseline), { timeout: 20_000, intervals: [300] })
    .toBe(true);

  // Stop this entity's walk immediately: a bump that's still opposing-held
  // keeps its queued path (retained, not consumed), so left unattended this
  // entity would keep autonomously bump-attacking on every future turn —
  // entities persist server-side for the whole shared combat-server session
  // (see playwright.config.ts), and could grind through the fixed monster
  // population that the sibling monsters.spec test also depends on. Retarget
  // to our own current hex: Pathfind(from == to) sets an empty path.
  await page.evaluate(() => {
    const me = window.game.me;
    if (me !== null) {
      window.game.tapHex(me.hex.q, me.hex.r);
    }
  });

  // Visual smoke check: the stage painted something (dots + the new HP bar
  // over the damaged entity), not a black void.
  const screenshot = await page.screenshot();
  expect(screenshot.byteLength).toBeGreaterThan(10_000);
});
