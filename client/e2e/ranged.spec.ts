import { expect, test } from "@playwright/test";

import { ClassRogue, EntityMonster } from "../src/protocol.gen";
import type { Hex } from "../src/protocol.gen";
import { chaseIntoCombat, gotoReady, progressTracker, seedIdentity } from "./helpers";

// This file runs against its OWN private monster server (see
// playwright.config.ts's "ranged" project) rather than sharing combat.spec.ts's
// server. A combat bubble is player-anchored (bubble.go): a new player whose
// bubble ends up connected — via a shared monster — to an earlier spec's
// already-closed page gets stuck waiting on a lock-in that page can never
// submit again (only the far-off COMBAT_PATIENCE AFK timeout would free it).
// This test is the only entity ever joined to this server, so that can't happen
// — which also means it MUST be run single-instance: driving it under
// `--repeat-each` re-joins the same shared world while earlier players are
// still in their disconnect-grace window and the non-respawning monster pool
// has drifted after being chased, so the later repeats fail on accumulated
// world state, not on this engagement (#259). CI runs it once, on a fresh
// world, which is what this test is built for.

// Milestone 6b.2's ranged path: a rogue's bow. Engagement is the #181/#247
// robust pattern, not a bespoke nearest-only walk: chaseIntoCombat rotates off
// an unreachable/leash-treadmill target to reach a REACHABLE monster's bubble,
// then the shot loop rotates targets on stalled progress. That robustness
// matters here because #233/#244 made a ranged shot line-of-sight-gated — a
// shot at a target behind terrain is rejected (no HP drop), and the nearest
// in-range monster can be exactly that; fixating on it (the old loop) times out
// on an unlucky spawn (the #259 flake theory). The shot itself is driven
// through window.game.tapHex, which decides move-vs-attack (src/main.ts's
// isRangedAttackClick): once this rogue is in a combat bubble and clicks an
// occupied hex within BowRange, tapHex fires a ranged "attack" intent instead
// of a move, for ANY distance up to BowRange (including adjacent — a rogue's
// dagger melee attack is unreachable through this click path while in combat,
// since isRangedAttackClick always wins for an occupied, in-range target hex).
// So any HP drop observed here, over a real browser + HTTP round trip, is the
// bow landing, not a disguised melee attack.
test("a rogue's ranged bow attack damages a monster from range, observable via window.game.hp", async ({
  page,
}) => {
  // Approach + LOS-rotation is metered on GAME progress (turn advances), not
  // wall-clock: chaseIntoCombat and the shot loop each carry their own 20s
  // budget, and on a contended runner the turns they need can legitimately
  // stretch past the default 30s test budget while nothing is wrong (same
  // turn-metered reasoning as autowalk.spec / the monsters.spec tooltip test).
  test.slow();

  // Seed the "returning player" identity localStorage read at module load
  // (src/net/session.ts's loadIdentity/join), so this joins as Rogue
  // deterministically without touching the start screen (its own
  // class-selection UX is exercised in class.spec.ts). An empty stored token
  // still reaches the server as a brand-new join — Join() only reclaims an
  // existing entity for a token it recognizes — but with the requested class.
  await seedIdentity(page, { class: "rogue" });

  await gotoReady(page);
  await expect.poll(() => page.evaluate(() => window.game.class)).toBe(ClassRogue);
  await expect.poll(() => page.evaluate(() => window.game.monsters)).toBeGreaterThanOrEqual(1);

  const baseline = await page.evaluate(() => ({ ...window.game.hp }));

  // Enter a combat bubble against a REACHABLE monster (rotates off an
  // unreachable/leash-treadmill target — the shared #181/#247 helper) rather
  // than the old greedy walk that could fixate on a monster parked in a
  // terrain pocket forever.
  await chaseIntoCombat(page);

  // Then close to bow range of an in-LOS monster and shoot. progressTracker
  // rotates targets when the gap to the current one stops shrinking: an
  // unreachable pocket, a leash-return treadmill, OR an in-range target we
  // cannot see (its distance sits pinned while the LOS-gated shot is silently
  // rejected) all read as stalled progress and switch to the next-nearest
  // monster — so the loop keeps trying until it finds a monster it can both
  // reach AND see.
  const bowRange = 4; // Shortbow rangeHex (internal/game/content.go).
  const tracker = progressTracker(8);
  let skip = 0;

  const shoot = async (base: Record<number, number>): Promise<boolean> => {
    const targetDist = await page.evaluate(
      ({ monsterKind, range, skipN }) => {
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

        const sorted = monsters
          .slice()
          .sort((a, b) => dist(me.hex, a.hex) - dist(me.hex, b.hex) || a.id - b.id);
        const target = sorted[skipN % sorted.length]!;
        const targetDistance = dist(me.hex, target.hex);

        if (targetDistance >= 1 && targetDistance <= range) {
          // In bow range: the tap on this occupied hex fires the ranged attack
          // (isRangedAttackClick). If a wall blocks it (#233/#244) the server
          // rejects it, no HP drops, and the pinned distance rotates us off.
          void window.game.tapHex(target.hex.q, target.hex.r);
        } else if (window.game.inCombat && window.game.combatMoves.length > 0) {
          // Beyond bow range inside a bubble (bow 4 < bubble radius 6): moves
          // are restricted to this turn's reachable tiles. Step onto the one
          // that closes the most distance to the target until it is in range.
          let step = window.game.combatMoves[0]!;
          for (const h of window.game.combatMoves.slice(1)) {
            if (dist(h, target.hex) < dist(step, target.hex)) {
              step = h;
            }
          }
          void window.game.tapHex(step.q, step.r);
        } else {
          // The bubble lapsed (no reachable move tiles): tap the monster so the
          // server pathfinds back toward it out of combat.
          void window.game.tapHex(target.hex.q, target.hex.r);
        }

        return targetDistance;
      },
      { monsterKind: EntityMonster, range: bowRange, skipN: skip },
    );

    skip = tracker.note(targetDist);

    return page.evaluate((b) => {
      return Object.entries(window.game.hp).some(([id, hp]) => {
        const before = b[Number(id)];

        return before !== undefined && hp < before;
      });
    }, base);
  };

  await expect
    .poll(() => shoot(baseline), { timeout: 20_000, intervals: [300] })
    .toBe(true);

  // No cleanup needed here (unlike combat.spec.ts's melee attack test): an
  // "attack" intent never queues a path (queueAttackLocked clears it), so
  // once this client stops clicking, there is nothing queued that could keep
  // firing or walking on its own — and this server has no other spec to
  // disturb regardless.
});
