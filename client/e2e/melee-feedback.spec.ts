import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";
import { EntityMonster } from "../src/protocol.gen";
import type { Hex } from "../src/protocol.gen";

declare global {
  interface Window {
    game: GameDebug;
  }
}

// This file runs against its OWN server (see playwright.config.ts:
// MONSTER_COUNT=3, COMBAT_PATIENCE=700ms — the short patience keeps bubble
// turns flowing even when a --repeat-each sibling instance's player shares a
// bubble and goes AFK; at the default 60s patience that wedge outlasts the
// test's whole deadline).

// #113: a melee (bump-to-attack) click gets ATTACK feedback — the one-shot flash
// (window.game.lastAttackFlash) and the committed crosshair (kind "attack")
// — never walk feedback. Pre-#113 the click routed through walkTo and
// planted the blue "move" marker on the enemy's hex, which reads as
// "walking there" now that a committed bump always lands (#104).
test("a melee-attack click commits the attack glyph, not a walk marker", async ({ page }) => {
  await page.goto("/");

  await expect
    .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
    .toBe(true);
  await expect.poll(() => page.evaluate(() => window.game.monsters)).toBeGreaterThanOrEqual(1);

  // combat.spec's damage-test chase pick (the reachable tile that closes the
  // most distance — the monster's own hex, a bump tile, once adjacent), but
  // instead of watching HP, read the CLICK FEEDBACK synchronously after each
  // tap: clickTarget's routing sets committedAction/lastAttackFlash before
  // the intent POST even starts, so the read can't race a turn bundle. A
  // plain step reads back kind "move" (or nothing, out of combat) and the
  // chase continues; the tap that lands on the bump tile reads back kind
  // "attack" — the #113 behavior under test — and stops. Deliberately NO
  // "wait until adjacent" pre-check: at a 250ms turn cadence any adjacency
  // observed in one evaluate is stale by the next, so a precondition-gated
  // version of this test timed out under parallel-worker contention; letting
  // the routing itself decide converges like the damage test does. The
  // awaited own-hex retarget afterwards replaces the standing melee intent within
  // the same input window (latest intent wins), so a repeat-each sibling
  // still finds monsters alive in the shared fixed pool.
  interface MeleeFeedback {
    target: Hex;
    committed: { kind: string; target: Hex } | null;
    flash: Hex | null;
  }

  let feedback: MeleeFeedback | null = null;

  const tryMeleeClick = (): Promise<MeleeFeedback | null> =>
    page.evaluate(async (monsterKind) => {
      const me = window.game.me;
      if (me === null) {
        return null;
      }

      const dist = (a: Hex, b: Hex): number => {
        const dq = a.q - b.q;
        const dr = a.r - b.r;
        const ds = -dq - dr;

        return (Math.abs(dq) + Math.abs(dr) + Math.abs(ds)) / 2;
      };

      const monsters = window.game.positions.filter((p) => p.kind === monsterKind);
      if (monsters.length === 0) {
        return null;
      }

      let nearest = monsters[0]!;
      for (const m of monsters.slice(1)) {
        if (dist(me.hex, m.hex) < dist(me.hex, nearest.hex)) {
          nearest = m;
        }
      }

      let target = nearest.hex;
      if (window.game.inCombat && window.game.combatMoves.length > 0) {
        target = window.game.combatMoves[0]!;
        for (const h of window.game.combatMoves.slice(1)) {
          if (dist(h, nearest.hex) < dist(target, nearest.hex)) {
            target = h;
          }
        }
      }

      void window.game.tapHex(target.q, target.r);

      const committed = window.game.committedAction;
      const flash = window.game.lastAttackFlash;

      if (committed === null || committed.kind !== "attack") {
        return null; // that tap was a step (or an out-of-combat walk) — keep converging
      }

      await window.game.tapHex(me.hex.q, me.hex.r); // cancel the standing melee swing

      return { target, committed, flash };
    }, EntityMonster);

  await expect
    .poll(
      async () => {
        feedback = await tryMeleeClick();

        return feedback !== null;
      },
      { timeout: 20_000, intervals: [300] },
    )
    .toBe(true);

  const fb = feedback!;
  expect(fb.committed).toEqual({ kind: "attack", target: fb.target });
  expect(fb.flash).toEqual(fb.target);
});
