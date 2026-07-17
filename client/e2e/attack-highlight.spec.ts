import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";
import { ClassMage, ClassRogue, EntityMonster } from "../src/protocol.gen";
import type { Hex } from "../src/protocol.gen";

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

test("mage: hovering an AoE target highlights the blast disc; clicking keeps it lit until the turn resolves", async ({
  page,
}) => {
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

  // A reachable move tile exists once in combat (the overlay's own e2e
  // covers it; here it anchors the hover target construction below).
  await expect
    .poll(() => page.evaluate(() => !window.game.inCombat || window.game.combatMoves.length > 0))
    .toBe(true);

  // Hover a hex at distance 2 built as "a neighbor of a reachable move tile":
  // distance 2 can never be a move/melee tile itself (those are distance 1),
  // and the move tile it neighbors is walkable and within the blast radius
  // (Ember Focus aoeRadius 1), so the disc is provably non-empty and must
  // contain that tile. Everything — pick, hover, read — happens in ONE
  // evaluate, so no turn bundle can shift positions in between.
  const hover = await page.evaluate(() => {
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

      return { target, anchor, tiles: window.game.hoverAttackTiles };
    }

    return null;
  });

  expect(hover).not.toBeNull();
  const { target, anchor, tiles } = hover!;
  expect(tiles.length).toBeGreaterThanOrEqual(1);
  // The blast disc: every highlighted tile lies within the weapon's
  // aoeRadius (1) of the hovered hex, and the walkable anchor tile is in it.
  for (const t of tiles) {
    expect(dist(t, target)).toBeLessThanOrEqual(1);
  }
  expect(tiles.some((t) => t.q === anchor.q && t.r === anchor.r)).toBe(true);

  // Commit the attack: tapHex on the same target. The committed tile set is
  // planted synchronously (before the intent POST settles), so read it in
  // the same evaluate — then poll for the next bundle clearing it (the
  // committed/pending indicator's lifecycle).
  const committed = await page.evaluate((t) => {
    void window.game.tapHex(t.q, t.r);

    return {
      tiles: window.game.committedAttackTiles,
      action: window.game.committedAction,
    };
  }, target);

  expect(committed.action?.kind).toBe("attack");
  expect(committed.tiles.length).toBeGreaterThanOrEqual(1);
  for (const t of committed.tiles) {
    expect(dist(t, target)).toBeLessThanOrEqual(1);
  }

  await expect
    .poll(() => page.evaluate(() => window.game.committedAttackTiles.length), { timeout: 10_000 })
    .toBe(0);

  // #114: real hits ride the bundle once combat plays out (the wolf hitting
  // us, our blasts landing) — assert the wire shape through window.game.
  await expect
    .poll(() => page.evaluate(() => window.game.hits.length), { timeout: 20_000, intervals: [200] })
    .toBeGreaterThan(0);

  const hit = await page.evaluate(() => window.game.hits[0]!);
  expect(typeof hit.crit).toBe("boolean");
  expect(typeof hit.glance).toBe("boolean");
  expect(hit.amount).toBeGreaterThan(0);
  expect(hit.turn).toBeGreaterThan(0);
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
  let single: { hex: Hex; tiles: Hex[] } | null = null;
  await expect
    .poll(
      async () => {
        single = await page.evaluate((monsterKind) => {
          const me = window.game.me;
          if (me === null || !window.game.inCombat) {
            return null;
          }

          const d = (a: { q: number; r: number }, b: { q: number; r: number }): number => {
            const dq = a.q - b.q;
            const dr = a.r - b.r;

            return (Math.abs(dq) + Math.abs(dr) + Math.abs(-dq - dr)) / 2;
          };

          const target = window.game.positions.find(
            (p) => p.kind === monsterKind && d(me.hex, p.hex) <= 4,
          );
          if (target === undefined) {
            return null;
          }

          window.game.hoverTile(target.hex.q, target.hex.r);

          return { hex: target.hex, tiles: window.game.hoverAttackTiles };
        }, EntityMonster);

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
