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

  // Baseline every entity's HP AND the set of monsters alive right now. The
  // alive-set is what makes a LETHAL bow shot observable: a shortbow (dmg 4)
  // one-shots a Rat (maxHP 4 — a real spawn in this world, seen dying in the
  // server log), which removes it from the bundle, so a plain `hp < before`
  // scan of the CURRENT entities can never see that hit (the entity is gone).
  // A monster that was alive at baseline and is now absent IS a landed hit —
  // see the shot loop's landed check (#259).
  const baseline = await page.evaluate(
    (monsterKind) => ({
      hp: { ...window.game.hp } as Record<number, number>,
      aliveMonsters: window.game.positions.filter((p) => p.kind === monsterKind).map((p) => p.id),
    }),
    EntityMonster,
  );

  // Enter a combat bubble against a REACHABLE monster (rotates off an
  // unreachable/leash-treadmill target — the shared #181/#247 helper) rather
  // than the old greedy walk that could fixate on a monster parked in a
  // terrain pocket forever.
  await chaseIntoCombat(page);

  // Then close to bow range of an in-LOS monster and shoot. Two rotation
  // sources drive the target index (their sum is `skipN`):
  //
  //   - APPROACH stall (`approach`, the shared #181/#247 distance tracker):
  //     while closing the gap, an unreachable pocket or a leash-return
  //     treadmill pins the best distance and rotates to the next monster.
  //   - LOS stall (`losSkip`, turn-metered below): once actually in bow range,
  //     a target behind terrain (#233/#244) has its shot silently rejected —
  //     no HP drop — and after a budget of TURNS we rotate off it.
  //
  // Splitting these is the #259 shot-poll hardening. The old loop fed the
  // CONSTANT in-range distance to the approach tracker, which read "not
  // getting closer" as a stall and could rotate off a perfectly good in-range
  // target after 8 polls (2.4s) — on a slow CI runner that fires BEFORE the
  // shot resolves, churning instead of landing. Metering the LOS rotation in
  // turns (a shot resolves in ~1 turn regardless of runner speed) decouples it
  // from wall-clock, and holding the approach tracker (a null note is a no-op)
  // while in range stops it from misreading healthy firing as a stall. This is
  // distinct from #261, which hardened the ENGAGEMENT (chaseIntoCombat) — here
  // the fix is in the shot-observation loop itself.
  const bowRange = 4; // Shortbow rangeHex (internal/game/content.go).
  const approach = progressTracker(8);
  let approachSkip = 0;
  const losBudgetTurns = 3;
  let losSkip = 0;
  let inRangeSinceTurn: number | null = null;

  const shoot = async (base: {
    hp: Record<number, number>;
    aliveMonsters: number[];
  }): Promise<boolean> => {
    const st = await page.evaluate(
      ({ monsterKind, range, skipN }) => {
        const me = window.game.me;
        if (me === null) {
          return { inRange: false, targetDist: null, turn: window.game.turn };
        }

        const dist = (a: Hex, b: Hex): number => {
          const dq = a.q - b.q;
          const dr = a.r - b.r;
          const ds = -dq - dr;

          return (Math.abs(dq) + Math.abs(dr) + Math.abs(ds)) / 2;
        };

        const monsters = window.game.positions.filter((p) => p.kind === monsterKind);
        if (monsters.length === 0) {
          return { inRange: false, targetDist: null, turn: window.game.turn };
        }

        const sorted = monsters
          .slice()
          .sort((a, b) => dist(me.hex, a.hex) - dist(me.hex, b.hex) || a.id - b.id);
        const target = sorted[skipN % sorted.length]!;
        const targetDistance = dist(me.hex, target.hex);

        if (targetDistance >= 1 && targetDistance <= range) {
          // In bow range: the tap on this occupied hex fires the ranged attack
          // (isRangedAttackClick). If a wall blocks it (#233/#244) the server
          // rejects it, no HP drops, and the turn-metered LOS budget rotates
          // us off.
          void window.game.tapHex(target.hex.q, target.hex.r);

          return { inRange: true, targetDist: targetDistance, turn: window.game.turn };
        }

        if (window.game.inCombat && window.game.combatMoves.length > 0) {
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

        return { inRange: false, targetDist: targetDistance, turn: window.game.turn };
      },
      { monsterKind: EntityMonster, range: bowRange, skipN: approachSkip + losSkip },
    );

    if (st.inRange) {
      // Firing in range: constant distance is progress, not a stall — hold the
      // approach tracker (null note is a no-op) and rotate only after the shot
      // has had `losBudgetTurns` turns to land (a still-pinned target is behind
      // terrain, LOS-gated).
      if (inRangeSinceTurn === null) {
        inRangeSinceTurn = st.turn;
      } else if (st.turn - inRangeSinceTurn >= losBudgetTurns) {
        losSkip += 1;
        inRangeSinceTurn = null;
      }
      approach.note(null);
    } else {
      // Approaching: a stalled approach distance rotates targets.
      inRangeSinceTurn = null;
      approachSkip = approach.note(st.targetDist);
    }

    return page.evaluate((b) => {
      const nowHp = window.game.hp;
      const alive = new Set(b.aliveMonsters);

      // The bow LANDED if any entity alive at baseline has lost HP (a surviving
      // monster, or the player taking a monster's melee counter) OR a monster
      // alive at baseline is now GONE. In this non-respawning single-instance
      // world a monster can only leave by dying, and only this rogue's bow
      // damages monsters, so a vanished monster IS an observed hit — the kill
      // branch that makes a one-shot bow kill visible (#259).
      return Object.entries(b.hp).some(([idStr, before]) => {
        const id = Number(idStr);
        const now = nowHp[id];

        return now === undefined ? alive.has(id) && before > 0 : now < before;
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
