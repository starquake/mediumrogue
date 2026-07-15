import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";
import { EntityMonster } from "../src/protocol.gen";
import type { Hex } from "../src/protocol.gen";

declare global {
  interface Window {
    game: GameDebug;
  }
}

// This spec runs against its own server with a short COMBAT_PATIENCE (see
// playwright.config.ts): the bubble auto-resolves turns without this client
// ever locking in, which is what lets us observe that a queued auto-walk does
// NOT keep advancing underneath those resolutions (#103). Before the fix, the
// residual world-time path was consumed one hex per bubble-turn.

test("entering a combat bubble hard-cancels the auto-walk goal and path", async ({ page }) => {
  await page.goto("/");

  await expect
    .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
    .toBe(true);
  await expect.poll(() => page.evaluate(() => window.game.monsters)).toBeGreaterThanOrEqual(1);

  // Walk toward the nearest monster, retargeting every poll — but STOP
  // issuing taps outside CombatRadius + 2, well before the bubble can form.
  // From there the queued auto-walk carries this entity into combat on its
  // own, with hexes still left on the route — the exact state #103 is about.
  // (Tapping any closer risks an in-flight intent landing after bubble entry,
  // which would queue a fresh IN-combat path and walk us legitimately.)
  let taps = 0;

  const walkUntilInCombat = async (): Promise<boolean> => {
    const r = await page.evaluate((monsterKind) => {
      const me = window.game.me;
      if (me === null) {
        return { inCombat: false, tapped: false };
      }

      if (window.game.inCombat) {
        return { inCombat: true, tapped: false };
      }

      const monsters = window.game.positions.filter((p) => p.kind === monsterKind);
      if (monsters.length === 0) {
        return { inCombat: false, tapped: false };
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

      if (bestDist > 8) {
        window.game.tapHex(nearest.hex.q, nearest.hex.r);

        return { inCombat: false, tapped: true };
      }

      return { inCombat: false, tapped: false };
    }, EntityMonster);

    if (r.tapped) {
      taps += 1;
    }

    return r.inCombat;
  };

  await expect
    .poll(walkUntilInCombat, { timeout: 20_000, intervals: [200] })
    .toBe(true);

  // The test only proves something if this client actually queued a walk
  // before combat. Zero taps means the spawn landed inside the tap cutoff —
  // either bad spawn luck (monster placement is random per server boot, and
  // with equal move speeds a too-close spawn can never reopen the gap: #98)
  // or a dirty --repeat-each rerun whose abandoned fight churned around the
  // sanctuary (e2e entities persist for the server session). Both are unmet
  // preconditions, not product bugs — skip rather than fail. MONSTER_COUNT=1
  // (playwright.config.ts) keeps the bad-luck case rare; an occasional skip
  // is expected, a consistently-skipping spec is the signal to investigate.
  test.skip(taps === 0, "spawned too close to a monster (or dirty rerun server) — no walk to cancel");

  // The same bundle that flips inCombat clears the walk goal and its ring —
  // no waiting: the onTurn handler does both synchronously.
  expect(await page.evaluate(() => window.game.destination)).toBeNull();

  // Now the server half. Submit NOTHING more; the short patience resolves
  // bubble-turns anyway (the monster keeps stepping toward us / bumping).
  // Observe two resolutions and assert our own hex never moved — before the
  // fix the residual path advanced one hex per resolution.
  const hexAtFreeze = await page.evaluate(() => window.game.me?.hex ?? null);
  expect(hexAtFreeze).not.toBeNull();

  // OUR bubble resolved a turn iff its patience countdown jumped back UP —
  // the deadline resets after every resolution. This is the only signal
  // scoped to our own bubble: entity positions also change on ordinary WORLD
  // turns (unbubbled monsters chasing), which would fire long before our
  // bubble ever resolved. Two resets ≈ two AFK resolutions the residual path
  // would have walked on, pre-fix.
  let resets = 0;
  let prevPatience = await page.evaluate(() => window.game.bubble?.patienceRemainingMs ?? 0);

  await expect
    .poll(
      async () => {
        const cur = await page.evaluate(() => window.game.bubble?.patienceRemainingMs ?? 0);
        if (cur > prevPatience) {
          resets += 1;
        }
        prevPatience = cur;

        return resets;
      },
      { timeout: 10_000, intervals: [100] },
    )
    .toBeGreaterThanOrEqual(2);

  // The headline asserts: still exactly where combat froze us, goal still
  // clear, still in the bubble.
  expect(await page.evaluate(() => window.game.me?.hex ?? null)).toEqual(hexAtFreeze);
  expect(await page.evaluate(() => window.game.destination)).toBeNull();
  expect(await page.evaluate(() => window.game.inCombat)).toBe(true);
});
