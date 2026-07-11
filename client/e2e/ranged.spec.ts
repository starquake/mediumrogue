import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";
import { ClassRogue, EntityMonster } from "../src/protocol.gen";
import type { Hex } from "../src/protocol.gen";

declare global {
  interface Window {
    game: GameDebug;
  }
}

// This file runs against its OWN private monster server (see
// playwright.config.ts's "ranged" project) rather than sharing combat.spec.ts's
// server. A combat bubble is player-anchored (bubble.go): a new player whose
// bubble ends up connected — via a shared monster — to an earlier spec's
// already-closed page gets stuck waiting on a lock-in that page can never
// submit again (only the far-off COMBAT_PATIENCE AFK timeout would free it).
// This test is the only entity ever joined to this server, so that can't happen.

// Milestone 6b.2's ranged path: a rogue's bow. This reuses the same
// nearest-monster chase loop as combat.spec.ts's melee damage test, driven
// through window.game.tapHex — which itself decides move-vs-attack
// (src/main.ts's isRangedAttackClick): once this rogue is in a combat bubble
// (CombatRadius=6 of a monster) and clicks an occupied hex within BowRange,
// tapHex fires a ranged "attack" intent instead of a move, for ANY distance up
// to BowRange (including adjacent — a rogue's dagger melee bump is
// unreachable through this click path while in combat, since
// isRangedAttackClick always wins for an occupied, in-range target hex). So
// any HP drop observed here, over a real browser + HTTP round trip, is the
// bow landing, not a disguised bump.
test("a rogue's ranged bow attack damages a monster from range, observable via window.game.hp", async ({
  page,
}) => {
  // Seed the "returning player" identity localStorage read at module load
  // (src/net/session.ts's loadIdentity/join), so this joins as Rogue
  // deterministically without touching the start screen (its own
  // class-selection UX is exercised in class.spec.ts). An empty stored token
  // still reaches the server as a brand-new join — Join() only reclaims an
  // existing entity for a token it recognizes — but with the requested class.
  await page.addInitScript(() => {
    localStorage.setItem("mediumrogue.identity", JSON.stringify({ entityId: 0, token: "", class: "rogue" }));
  });

  await page.goto("/");

  await expect
    .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
    .toBe(true);
  await expect.poll(() => page.evaluate(() => window.game.class)).toBe(ClassRogue);
  await expect.poll(() => page.evaluate(() => window.game.monsters)).toBeGreaterThanOrEqual(1);

  const baseline = await page.evaluate(() => ({ ...window.game.hp }));

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

      // Inside a bubble but still beyond bow range (bow 4 < bubble radius 6),
      // a tap on the monster would be a move — and moves are now restricted
      // to this turn's reachable tiles. Step onto the reachable tile that
      // closes the most distance until the monster is in range; from there
      // the tap IS the ranged attack and goes straight through.
      const bowRange = 4;
      if (window.game.inCombat && bestDist > bowRange && window.game.combatMoves.length > 0) {
        let step = window.game.combatMoves[0]!;
        for (const h of window.game.combatMoves.slice(1)) {
          if (dist(h, nearest.hex) < dist(step, nearest.hex)) {
            step = h;
          }
        }

        window.game.tapHex(step.q, step.r);

        return;
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

  await expect
    .poll(() => chase(baseline), { timeout: 20_000, intervals: [300] })
    .toBe(true);

  // No cleanup needed here (unlike combat.spec.ts's melee bump test): an
  // "attack" intent never queues a path (queueAttackLocked clears it), so
  // once this client stops clicking, there is nothing queued that could keep
  // firing or walking on its own — and this server has no other spec to
  // disturb regardless.
});
