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
  // Every phase below is metered on GAME progress (turn increments, bubble
  // resolutions), never a single wall-clock cap, so under CI-grade contention
  // the journey can legitimately need more than the default 30s test budget
  // without anything being wrong (same reasoning as chat.spec.ts). 3x it.
  test.slow();

  await page.goto("/");

  await expect
    .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
    .toBe(true);
  await expect.poll(() => page.evaluate(() => window.game.monsters)).toBeGreaterThanOrEqual(1);

  // Walk toward the nearest monster — but STOP issuing taps outside
  // CombatRadius + 2, well before the bubble can form. From there the queued
  // auto-walk carries this entity into combat on its own, with hexes still
  // left on the route — the exact state #103 is about. (Tapping any closer
  // risks an in-flight intent landing after bubble entry, which would queue
  // a fresh IN-combat path and walk us legitimately.)
  //
  // Taps are deliberately RARE (#117 Mode B): only when no walk goal is
  // active (destination === null), never a per-turn retarget, and each tap's
  // POST is awaited before the step returns. A tap decided on a stale
  // not-yet-in-combat bundle can land AFTER a bubble (often carried to us by
  // a sibling --repeat-each player) has already engulfed us, queueing a
  // fresh path the server then walks legitimately — observably identical to
  // the #103 regression. One long path per journey instead of a tap every
  // turn shrinks that window by an order of magnitude AND guarantees plenty
  // of route left at bubble entry; the residual ambiguity is classified by
  // the "moved" skip below.
  let taps = 0;

  const walkStep = async (): Promise<boolean> => {
    const r = await page.evaluate(async (monsterKind) => {
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

      if (bestDist > 8 && window.game.destination === null) {
        await window.game.tapHex(nearest.hex.q, nearest.hex.r);

        return { inCombat: false, tapped: true };
      }

      return { inCombat: false, tapped: false };
    }, EntityMonster);

    if (r.tapped) {
      taps += 1;
    }

    return r.inCombat;
  };

  // Mode A de-race (#117): meter the walk phase on TURN ADVANCEMENT, never
  // wall-clock. Movement only happens on turn resolutions, so the old fixed
  // 20s cap was a hidden cadence assumption — on a slow/contended runner
  // turns stretch far past the configured 250ms and the walk legitimately
  // needs more wall-clock while making perfectly good per-turn progress.
  // Budget in TURNS instead (90 ≈ the 20s cap at healthy 250ms cadence, and
  // the walk closes ≥1 hex/turn once pathing): one walkStep per bundle, each
  // bundle awaited with a generous single-turn bound.
  const maxWalkTurns = 90;
  let inCombat = false;
  for (let i = 0; i < maxWalkTurns && !inCombat; i++) {
    inCombat = await walkStep();
    if (!inCombat) {
      const turnBefore = await page.evaluate(() => window.game.turn);
      await expect
        .poll(() => page.evaluate(() => window.game.turn), { timeout: 15_000 })
        .toBeGreaterThan(turnBefore);
    }
  }
  // Classify BEFORE asserting (#117 recurrence, 2026-07-18): this skip used to
  // sit *below* the inCombat assertion, so a run whose precondition was never
  // met failed red — "walk never reached a combat bubble within 90 turns" —
  // instead of skipping. Zero taps means the walk never even started: the
  // spawn landed inside the tap cutoff (bad spawn luck — monster placement is
  // random per server boot, and with equal move speeds a too-close spawn can
  // never reopen the gap: #98), or a dirty --repeat-each rerun churned the
  // sanctuary (e2e entities persist for the server session). Both are unmet
  // preconditions, not product bugs. MONSTER_COUNT=1 (playwright.config.ts)
  // keeps the bad-luck case rare; an occasional skip is expected, a
  // consistently-skipping spec is the signal to investigate.
  //
  // Order matters for DIAGNOSIS too: with the skip first, any surviving red on
  // this assertion now proves taps > 0 — the walk really was queued and really
  // did fail to arrive, which is a product signal worth chasing rather than
  // spawn luck.
  test.skip(taps === 0, "spawned too close to a monster (or dirty rerun server) — no walk to cancel");

  expect(inCombat, `walk never reached a combat bubble within ${maxWalkTurns} turns`).toBe(true);

  // The same bundle that flips inCombat clears the walk goal and its ring —
  // no waiting: the onTurn handler does both synchronously.
  expect(await page.evaluate(() => window.game.destination)).toBeNull();

  // Now the server half. Submit NOTHING more; the short patience resolves
  // bubble-turns anyway (the monster keeps stepping toward us / striking).
  // Observe two resolutions and assert our own hex never moved — before the
  // fix the residual path advanced one hex per resolution.
  //
  // OUR bubble resolved a turn iff its patience countdown jumped back UP —
  // the deadline resets after every resolution. This is the only signal
  // scoped to our own bubble: entity positions also change on ordinary WORLD
  // turns (unbubbled monsters chasing), which would fire long before our
  // bubble ever resolved. Two resets ≈ two AFK resolutions the residual path
  // would have walked on, pre-fix.
  const readObs = (): Promise<{ hex: Hex; hp: number; patience: number; inCombat: boolean } | null> =>
    page.evaluate(() => {
      const me = window.game.me;
      if (me === null) {
        return null;
      }

      return {
        hex: me.hex,
        hp: window.game.hp[me.id] ?? 0,
        patience: window.game.bubble?.patienceRemainingMs ?? 0,
        inCombat: window.game.inCombat,
      };
    });

  const hexDist = (a: Hex, b: Hex): number => {
    const dq = a.q - b.q;
    const dr = a.r - b.r;

    return (Math.abs(dq) + Math.abs(dr) + Math.abs(-dq - dr)) / 2;
  };

  const atFreeze = await readObs();
  expect(atFreeze).not.toBeNull();

  // Mode B de-race (#117): check the hex at EVERY sample and STOP the moment
  // the second reset is seen — the assertion window is bounded in bubble
  // RESOLUTIONS, not wall-clock. The old shape counted two resets and only
  // then re-read the hex once, leaving an open-ended window in which further
  // resolutions kept chipping this 30 HP fighter (under --repeat-each
  // contention every sibling instance shares this server and its bubble, so
  // the window stretched to ~10 resolutions); a death respawns us on a random
  // hex and the test failed on that fixture death, not the product. A death
  // is an unmet precondition, not a regression — detect it (HP jumping back
  // UP toward full, a >1-hex teleport, or the bubble dissolving around us)
  // and skip, exactly like the taps guard above.
  // Typed `string`, not a literal union: it's only ever assigned inside the
  // poll callback, and TS doesn't track closure assignments — a union type
  // makes the post-poll comparisons "unintentional" (TS2367).
  let verdict: string = "frozen";
  let resets = 0;
  let prevPatience = atFreeze!.patience;
  let prevHp = atFreeze!.hp;

  await expect
    .poll(
      async () => {
        const cur = await readObs();
        if (cur === null || cur.hp > prevHp || !cur.inCombat || hexDist(cur.hex, atFreeze!.hex) > 1) {
          verdict = "died";

          return 2; // exit the poll; the skip below tells the real story
        }

        if (cur.hex.q !== atFreeze!.hex.q || cur.hex.r !== atFreeze!.hex.r) {
          verdict = "moved";

          return 2; // exit the poll; the assert below fails with the real story
        }

        if (cur.patience > prevPatience) {
          resets += 1;
        }
        prevPatience = cur.patience;
        prevHp = cur.hp;

        return resets;
      },
      { timeout: 10_000, intervals: [100] },
    )
    .toBeGreaterThanOrEqual(2);

  test.skip(
    verdict === "died",
    "fixture death mid-observation (respawn teleport) — the wolf killed this fighter, not a product bug",
  );

  // "moved" on a server carrying OTHER players is ambiguous: despite the tap
  // guard, a sufficiently late straggler intent (landing after a
  // sibling-carried bubble engulfed us) queues a fresh path the server walks
  // legitimately — the same observable as the #103 regression. Skip it there;
  // SOLO (the CI/make e2e shape — this spec's server admits one client) a
  // moved hex has no innocent explanation and stays a hard failure.
  const otherPlayers = await page.evaluate(
    (monsterKind) =>
      window.game.positions.filter((p) => p.kind !== monsterKind && p.id !== window.game.me?.id).length,
    EntityMonster,
  );
  test.skip(
    verdict === "moved" && otherPlayers > 0,
    "hex moved with sibling players on the server — straggler-intent ambiguity, not provably #103",
  );
  expect(verdict, "hex moved while frozen in a bubble — residual auto-walk path advanced (#103)").toBe("frozen");

  // No post-window re-read of the hex: sampling stops at the second reset by
  // design — re-reading later would reopen the unbounded death window this
  // fix removes. destination is safe to assert after the fact: it cannot
  // change unless this client itself acts, and it submits nothing.
  expect(await page.evaluate(() => window.game.destination)).toBeNull();
});
