import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";
import { ClassMage, ClassRogue, EntityMonster } from "../src/protocol.gen";
import type { Hex, HitView } from "../src/protocol.gen";

declare global {
  interface Window {
    game: GameDebug;
  }
}

// This file runs against its OWN private monster server (see
// playwright.config.ts) with a short COMBAT_PATIENCE, for the same reasons as
// melee-feedback.spec.ts: each test abandons its player mid-bubble, and only
// a fast AFK timeout keeps sibling bubble turns flowing.
//
// Attack-target highlights (#101) + crit/glance hit moments (#114), all
// asserted through window.game (the canvas draws themselves are not
// DOM-assertable — the mirrors are synced in the same code paths that drive
// the pixels). De-raced per the repo rule: every assertion polls a
// window.game state flip; the synchronous ones (hoverTile, tapHex's
// committed set) read their result inside the same page.evaluate that
// triggers them, so no bundle can slip in between.

const dist = (a: Hex, b: Hex): number => {
  const dq = a.q - b.q;
  const dr = a.r - b.r;
  const ds = -dq - dr;

  return (Math.abs(dq) + Math.abs(dr) + Math.abs(ds)) / 2;
};

// chaseIntoCombat re-picks the nearest monster each poll and taps toward it
// until the client reports a combat bubble — the combat.spec.ts loop, shared
// by both tests here.
const chaseIntoCombat = async (page: import("@playwright/test").Page): Promise<void> => {
  await expect
    .poll(
      () =>
        page.evaluate((monsterKind) => {
          if (window.game.inCombat) {
            return true;
          }

          const me = window.game.me;
          if (me === null) {
            return false;
          }

          const monsters = window.game.positions.filter((p) => p.kind === monsterKind);
          if (monsters.length === 0) {
            return false;
          }

          const d = (a: { q: number; r: number }, b: { q: number; r: number }): number => {
            const dq = a.q - b.q;
            const dr = a.r - b.r;

            return (Math.abs(dq) + Math.abs(dr) + Math.abs(-dq - dr)) / 2;
          };

          let nearest = monsters[0]!;
          for (const m of monsters.slice(1)) {
            if (d(me.hex, m.hex) < d(me.hex, nearest.hex)) {
              nearest = m;
            }
          }

          void window.game.tapHex(nearest.hex.q, nearest.hex.r);

          return false;
        }, EntityMonster),
      { timeout: 20_000, intervals: [300] },
    )
    .toBe(true);
};

test("mage: hovering an AoE target highlights the blast disc; clicking keeps the disc lit with NO centre crosshair until the turn resolves", async ({
  page,
}) => {
  // Metered on GAME progress, not wall clock: this test joins, waits for a
  // monster, closes to AoE range, fires, waits for the turn to resolve, and
  // only then waits for a real hit to ride the bundle (#114). That is many
  // turns, and on a contended CI runner they legitimately take longer than
  // the default 30s budget while nothing is actually wrong — the inner poll
  // alone is allowed 20s. Same reasoning (and same fix) as autowalk.spec.ts
  // and the inventory journeys.
  //
  // CI hit exactly that on 2026-07-19: "Test timeout of 30000ms exceeded"
  // with the hit-poll still waiting, un-reproducible locally at
  // --repeat-each=3 --workers=6 (6/6 green).
  test.slow();

  // Join as mage (Ember Focus: aoeRadius > 0) — the identity-seeding
  // technique ranged.spec.ts uses, skipping the start screen.
  await page.addInitScript(() => {
    localStorage.setItem("mediumrogue.identity", JSON.stringify({ entityId: 0, token: "", class: "mage" }));
  });

  await page.goto("/");

  await expect
    .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
    .toBe(true);
  await expect.poll(() => page.evaluate(() => window.game.class)).toBe(ClassMage);
  await expect.poll(() => page.evaluate(() => window.game.monsters)).toBeGreaterThanOrEqual(1);

  await chaseIntoCombat(page);

  // #135: the world hover highlight is OUT-of-combat only. Now that we're in
  // combat, hovering any hex leaves window.game.hoverMoveTile null — the reach
  // tints + #101 ember own the in-combat hover. (hover.spec.ts covers the
  // walk/wait/rock routing out of combat on a monster-free server; this is the
  // in-combat → null half, which needs a real bubble.)
  const inCombatHover = await page.evaluate(() => {
    const me = window.game.me!;
    window.game.hoverTile(me.hex.q, me.hex.r);
    return window.game.hoverMoveTile;
  });
  expect(inCombatHover).toBeNull();

  // A reachable move tile exists once in combat (the overlay's own e2e
  // covers it; here it anchors the hover target construction below).
  await expect
    .poll(() => page.evaluate(() => !window.game.inCombat || window.game.combatMoves.length > 0))
    .toBe(true);

  // Pick the AoE target, hover it, AND commit it — all in ONE evaluate. A
  // distance-2 hex is never a move/melee tile itself (those are distance 1),
  // built as a neighbor of a reachable move tile so it's walkable and within
  // Ember Focus's blast radius (aoeRadius 1) — the disc is provably non-empty
  // and contains that tile, and clickTarget routes the tap as an AoE cast.
  // Doing pick+hover+commit in one evaluate is load-bearing: split across two
  // evaluates, a turn bundle can step `me` in between, turning the distance-2
  // target into a distance-1 MOVE tile — which clickTarget walks to, not casts
  // (the committedAction then reads {kind:"move"}, flaking under --repeat-each).
  // The committed indicator is planted synchronously by tapHex (before the
  // intent POST settles), so it reads back in the same evaluate.
  const shot = await page.evaluate(() => {
    const me = window.game.me;
    if (me === null || !window.game.inCombat || window.game.combatMoves.length === 0) {
      return null;
    }

    const d = (a: { q: number; r: number }, b: { q: number; r: number }): number => {
      const dq = a.q - b.q;
      const dr = a.r - b.r;

      return (Math.abs(dq) + Math.abs(dr) + Math.abs(-dq - dr)) / 2;
    };

    const anchor = window.game.combatMoves.find((h) => d(me.hex, h) === 1);
    if (anchor === undefined) {
      return null;
    }

    const dirs = [
      { q: 1, r: 0 },
      { q: 1, r: -1 },
      { q: 0, r: -1 },
      { q: -1, r: 0 },
      { q: -1, r: 1 },
      { q: 0, r: 1 },
    ];
    for (const dir of dirs) {
      const target = { q: anchor.q + dir.q, r: anchor.r + dir.r };
      if (d(me.hex, target) !== 2) {
        continue;
      }

      window.game.hoverTile(target.q, target.r);
      const hoverTiles = window.game.hoverAttackTiles;

      void window.game.tapHex(target.q, target.r);

      return {
        target,
        anchor,
        hoverTiles,
        committedTiles: window.game.committedAttackTiles,
        committedAction: window.game.committedAction,
      };
    }

    return null;
  });

  expect(shot).not.toBeNull();
  const { target, anchor, hoverTiles, committedTiles, committedAction } = shot!;

  // Hover lights the blast disc: every tile within the weapon's aoeRadius (1)
  // of the hovered hex, and the walkable anchor tile is provably in it.
  expect(hoverTiles.length).toBeGreaterThanOrEqual(1);
  for (const t of hoverTiles) {
    expect(dist(t, target)).toBeLessThanOrEqual(1);
  }
  expect(hoverTiles.some((t) => t.q === anchor.q && t.r === anchor.r)).toBe(true);

  // #138: committing an AoE plants NO single-target crosshair — committedAction
  // is null — because the lit blast disc is the indicator; a centre mark would
  // misread as "one victim here". The disc (committedTiles) stays lit, every
  // tile within the weapon's aoeRadius of the target.
  expect(committedAction).toBeNull();
  expect(committedTiles.length).toBeGreaterThanOrEqual(1);
  for (const t of committedTiles) {
    expect(dist(t, target)).toBeLessThanOrEqual(1);
  }

  await expect
    .poll(() => page.evaluate(() => window.game.committedAttackTiles.length), { timeout: 10_000 })
    .toBe(0);

  // #114: real hits ride the bundle once combat plays out (the wolf hitting
  // us, our blasts landing) — assert the wire shape through window.game.
  // window.game.hits holds only the hits NEW in the latest bundle, so a
  // quiet turn empties it again: capture the hit INSIDE the poll rather than
  // re-reading it afterwards, or a quiet bundle landing in between yields
  // undefined (a real race this spec hit under --repeat-each).
  let hit: HitView | null = null;
  await expect
    .poll(
      async () => {
        hit = await page.evaluate(() => window.game.hits[0] ?? null);

        return hit !== null;
      },
      { timeout: 20_000, intervals: [200] },
    )
    .toBe(true);

  expect(typeof hit!.crit).toBe("boolean");
  expect(typeof hit!.glance).toBe("boolean");
  expect(hit!.amount).toBeGreaterThan(0);
  expect(hit!.turn).toBeGreaterThan(0);
});

test("rogue: hovering a hostile in bow range highlights exactly its tile; a plain move tile highlights nothing", async ({
  page,
}) => {
  await page.addInitScript(() => {
    localStorage.setItem("mediumrogue.identity", JSON.stringify({ entityId: 0, token: "", class: "rogue" }));
  });

  await page.goto("/");

  await expect
    .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
    .toBe(true);
  await expect.poll(() => page.evaluate(() => window.game.class)).toBe(ClassRogue);
  await expect.poll(() => page.evaluate(() => window.game.monsters)).toBeGreaterThanOrEqual(1);

  await chaseIntoCombat(page);

  // Poll until a monster sits within bow range (4), then hover it — pick,
  // hover, and read in ONE evaluate so the positions can't shift under us.
  // A single-target weapon (and the adjacent melee swing alike) lights
  // EXACTLY the victim's tile — never a disc.
  //
  // Entering a bubble only guarantees CombatRadius (6), not bow range (4),
  // and a monster already busy fighting someone else may never close the
  // gap on its own — so keep STEPPING toward the nearest one until it is in
  // range, exactly as ranged.spec.ts does. Without this the poll waits
  // passively and times out whenever the monster has another target
  // (reproduced under --repeat-each=3 --workers=9).
  const BOW_RANGE = 4;
  let single: { hex: Hex; tiles: Hex[] } | null = null;
  await expect
    .poll(
      async () => {
        single = await page.evaluate(
          ({ monsterKind, bowRange }) => {
            const me = window.game.me;
            if (me === null || !window.game.inCombat) {
              return null;
            }

            const d = (a: { q: number; r: number }, b: { q: number; r: number }): number => {
              const dq = a.q - b.q;
              const dr = a.r - b.r;

              return (Math.abs(dq) + Math.abs(dr) + Math.abs(-dq - dr)) / 2;
            };

            const monsters = window.game.positions.filter((p) => p.kind === monsterKind);
            if (monsters.length === 0) {
              return null;
            }

            let nearest = monsters[0]!;
            for (const m of monsters.slice(1)) {
              if (d(me.hex, m.hex) < d(me.hex, nearest.hex)) {
                nearest = m;
              }
            }

            // Still out of bow range: step onto the reachable tile that
            // closes the most distance and try again next poll.
            if (d(me.hex, nearest.hex) > bowRange) {
              if (window.game.combatMoves.length > 0) {
                let step = window.game.combatMoves[0]!;
                for (const h of window.game.combatMoves.slice(1)) {
                  if (d(h, nearest.hex) < d(step, nearest.hex)) {
                    step = h;
                  }
                }

                void window.game.tapHex(step.q, step.r);
              }

              return null;
            }

            window.game.hoverTile(nearest.hex.q, nearest.hex.r);

            return { hex: nearest.hex, tiles: window.game.hoverAttackTiles };
          },
          { monsterKind: EntityMonster, bowRange: BOW_RANGE },
        );

        return single !== null;
      },
      { timeout: 20_000, intervals: [300] },
    )
    .toBe(true);

  expect(single!.tiles).toEqual([single!.hex]);

  // A hex with no monster on it that I can step onto is a MOVE on click —
  // hovering it must highlight nothing.
  const moveHover = await page.evaluate((monsterKind) => {
    const open = window.game.combatMoves.find(
      (h) => !window.game.positions.some((p) => p.kind === monsterKind && p.hex.q === h.q && p.hex.r === h.r),
    );
    if (open === undefined) {
      return null;
    }

    window.game.hoverTile(open.q, open.r);

    return window.game.hoverAttackTiles;
  }, EntityMonster);

  if (moveHover !== null) {
    expect(moveHover).toEqual([]);
  }
});
