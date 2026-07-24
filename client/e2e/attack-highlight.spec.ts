import { expect, test } from "@playwright/test";

import { ClassMage, ClassRogue, EntityMonster } from "../src/protocol.gen";
import type { Hex, HitView } from "../src/protocol.gen";
import { chaseIntoCombat, dumpState, gotoReady, hexDist, progressTracker, seedIdentity } from "./helpers";

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
//
// #181: both approach loops below are PROGRESS-AWARE, because the naive
// versions starved. Root cause, captured live at MONSTER_COUNT=2: the loops
// pinned on the NEAREST monster and stepped greedily one hex at a time —
// but a nearest monster can be permanently unreachable (spawn placement
// checks walkability, not connectivity, so a monster can sit in a terrain
// pocket — its own Pathfind returns nil and it parks forever), and greedy
// stepping deadlocks on any equal-distance local minimum (the captured
// failure: rogue oscillating between two distance-5 hexes for 20s, one hex
// outside bow range, behind an obstacle). Under --repeat-each parallelism
// the abandoned players of sibling instances pile onto the same corridor
// and make the jam stable. The fixes: rotate to the next-nearest monster
// when position stops changing, tabu the recently-visited hexes so an
// equal-distance pair can't trap the stepper, and re-engage via the
// server's real pathfinding whenever we're out of combat (fresh join,
// fled bubble, or death-respawn).

// Whatever a test leaves behind keeps ACTING: entities never leave the world
// (#21), and a standing move intent whose next step is monster-held keeps
// melee-swinging every bubble turn. On this SHARED server that lets the
// abandoned player of one instance grind down the monster pool that sibling
// instances still need (#181's depletion path). A tap on our own hex is a
// wait intent — the server clears path AND attack targets — so each test
// hands back an inert body, pass or fail.
test.afterEach(async ({ page }) => {
  try {
    await page.evaluate(() => {
      const me = window.game.me;

      return me === null ? undefined : window.game.tapHex(me.hex.q, me.hex.r);
    });
  } catch {
    // page already gone (crash/teardown) — nothing to disengage.
  }
});

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
  await seedIdentity(page, { class: "mage" });

  await gotoReady(page);
  await expect.poll(() => page.evaluate(() => window.game.class)).toBe(ClassMage);
  await expect.poll(() => page.evaluate(() => window.game.monsters)).toBeGreaterThanOrEqual(1);

  await chaseIntoCombat(page);

  // #135: the world hover highlight is OUT-of-combat only. Now that we're in
  // combat, hovering any hex leaves window.game.hoverMoveTile null — the reach
  // tints + #101 ember own the in-combat hover. (hover.spec.ts covers the
  // walk/wait/rock routing out of combat on a monster-free server; this is the
  // in-combat → null half, which needs a real bubble.)
  //
  // #181: captured atomically inside a re-engaging poll. The bubble can
  // collapse between chaseIntoCombat and this hover (a monster dies — likelier
  // with fewer of them), dropping us out of combat, where hovering our own hex
  // IS a valid wait move and hoverMoveTile is non-null — a spurious failure of
  // an in-combat-only assertion. The poll captures {inCombat, hoverMoveTile}
  // in ONE evaluate and only settles while inCombat is true, so the value we
  // assert on is a genuine in-combat reading; a real regression (in-combat
  // hover returning a tile) still fails the toBeNull below.
  let inCombatHover: unknown = null;
  const hoverPoll = expect
    .poll(
      async () => {
        const res = await page.evaluate((monsterKind) => {
          const me = window.game.me;
          if (me === null) {
            return { inCombat: false, hoverMoveTile: null };
          }

          if (!window.game.inCombat) {
            const d = (a: { q: number; r: number }, b: { q: number; r: number }): number => {
              const dq = a.q - b.q;
              const dr = a.r - b.r;

              return (Math.abs(dq) + Math.abs(dr) + Math.abs(-dq - dr)) / 2;
            };
            const monsters = window.game.positions.filter((p) => p.kind === monsterKind);
            if (monsters.length > 0) {
              let nearest = monsters[0]!;
              for (const m of monsters.slice(1)) {
                if (d(me.hex, m.hex) < d(me.hex, nearest.hex)) {
                  nearest = m;
                }
              }
              void window.game.tapHex(nearest.hex.q, nearest.hex.r);
            }

            return { inCombat: false, hoverMoveTile: null };
          }

          window.game.hoverTile(me.hex.q, me.hex.r);

          return { inCombat: true, hoverMoveTile: window.game.hoverMoveTile };
        }, EntityMonster);

        inCombatHover = res.hoverMoveTile;

        return res.inCombat;
      },
      { timeout: 20_000, intervals: [200] },
    )
    .toBe(true);

  try {
    await hoverPoll;
  } catch (err) {
    await dumpState(page, "mage-incombat-hover");
    throw err;
  }
  expect(inCombatHover).toBeNull();

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
  //
  // #181: the whole capture is POLLED, not a one-shot evaluate. The old code
  // ran the standalone precondition poll (which accepts !inCombat, so it
  // passes the instant a collapsing bubble drops us out) and then a single
  // shot evaluate that returned null whenever combat/moves/a-dist-2-target
  // weren't ALL true at that exact instant — a null that failed the test.
  // Fewer monsters make the bubble collapse sooner, so this surfaced under
  // load. Polling re-engages if we're knocked out of combat and retries the
  // atomic pick+commit until it lands consistently; the committed read still
  // rides the same evaluate as the tap, so no bundle slips between them.
  let shot: {
    target: Hex;
    anchor: Hex;
    hoverTiles: Hex[];
    committedTiles: Hex[];
    committedAction: { kind: string; target: Hex } | null;
  } | null = null;

  const shotPoll = expect
    .poll(
      async () => {
        shot = await page.evaluate((monsterKind) => {
          const me = window.game.me;
          if (me === null) {
            return null;
          }

          const d = (a: { q: number; r: number }, b: { q: number; r: number }): number => {
            const dq = a.q - b.q;
            const dr = a.r - b.r;

            return (Math.abs(dq) + Math.abs(dr) + Math.abs(-dq - dr)) / 2;
          };

          if (!window.game.inCombat || window.game.combatMoves.length === 0) {
            // Knocked out of combat (a collapsed bubble): re-engage the
            // nearest monster and let the server route us back into one.
            const monsters = window.game.positions.filter((p) => p.kind === monsterKind);
            if (monsters.length > 0) {
              let nearest = monsters[0]!;
              for (const m of monsters.slice(1)) {
                if (d(me.hex, m.hex) < d(me.hex, nearest.hex)) {
                  nearest = m;
                }
              }
              void window.game.tapHex(nearest.hex.q, nearest.hex.r);
            }

            return null;
          }

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
        }, EntityMonster);

        return shot !== null;
      },
      { timeout: 20_000, intervals: [200] },
    )
    .toBe(true);

  try {
    await shotPoll;
  } catch (err) {
    await dumpState(page, "mage-shot");
    throw err;
  }

  const { target, anchor, hoverTiles, committedTiles, committedAction } = shot!;

  // Hover lights the blast disc: every tile within the weapon's aoeRadius (1)
  // of the hovered hex, and the walkable anchor tile is provably in it.
  expect(hoverTiles.length).toBeGreaterThanOrEqual(1);
  for (const t of hoverTiles) {
    expect(hexDist(t, target)).toBeLessThanOrEqual(1);
  }
  expect(hoverTiles.some((t) => t.q === anchor.q && t.r === anchor.r)).toBe(true);

  // #138: committing an AoE plants NO single-target crosshair — committedAction
  // is null — because the lit blast disc is the indicator; a centre mark would
  // misread as "one victim here". The disc (committedTiles) stays lit, every
  // tile within the weapon's aoeRadius of the target.
  expect(committedAction).toBeNull();
  expect(committedTiles.length).toBeGreaterThanOrEqual(1);
  for (const t of committedTiles) {
    expect(hexDist(t, target)).toBeLessThanOrEqual(1);
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
  const hitPoll = expect
    .poll(
      async () => {
        hit = await page.evaluate((monsterKind) => {
          // #181-class de-race: the short-patience bubble can resolve and
          // collapse before any hit rides the bundle, dropping us out of
          // combat — a passive hits[0] read then starves forever (the mage-hit
          // CI flake). If we've fallen out of combat, re-engage the nearest
          // monster (same pattern as the shot poll above) so combat resumes and
          // a real hit — ours or the monster's on us — can still land.
          if (!window.game.inCombat) {
            const me = window.game.me;
            if (me !== null) {
              const d = (a: { q: number; r: number }, b: { q: number; r: number }): number =>
                (Math.abs(a.q - b.q) + Math.abs(a.q + a.r - b.q - b.r) + Math.abs(a.r - b.r)) / 2;
              const monsters = window.game.positions.filter((p) => p.kind === monsterKind);
              if (monsters.length > 0) {
                let nearest = monsters[0]!;
                for (const m of monsters.slice(1)) {
                  if (d(me.hex, m.hex) < d(me.hex, nearest.hex)) {
                    nearest = m;
                  }
                }
                void window.game.tapHex(nearest.hex.q, nearest.hex.r);
              }
            }
          }

          return window.game.hits[0] ?? null;
        }, EntityMonster);

        return hit !== null;
      },
      { timeout: 20_000, intervals: [200] },
    )
    .toBe(true);

  try {
    await hitPoll;
  } catch (err) {
    await dumpState(page, "mage-hit");
    throw err;
  }

  expect(typeof hit!.crit).toBe("boolean");
  expect(typeof hit!.glance).toBe("boolean");
  expect(hit!.amount).toBeGreaterThan(0);
  expect(hit!.turn).toBeGreaterThan(0);
});

test("rogue: hovering a hostile in bow range highlights exactly its tile; a plain move tile highlights nothing", async ({
  page,
}) => {
  // Same game-progress metering as the mage test above: the two approach
  // polls alone are budgeted 20s each, which the default 30s test timeout
  // cannot contain even when every poll is making healthy progress.
  test.slow();

  await seedIdentity(page, { class: "rogue" });

  await page.goto("/");

  try {
    await expect
      .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
      .toBe(true);
    await expect.poll(() => page.evaluate(() => window.game.class)).toBe(ClassRogue);
    await expect.poll(() => page.evaluate(() => window.game.monsters)).toBeGreaterThanOrEqual(1);
  } catch (err) {
    await dumpState(page, "rogue-join");
    throw err;
  }

  await chaseIntoCombat(page);

  // Poll until a monster sits within bow range (4) AND hovering it lights
  // something, then read the tiles — pick, hover, and read in ONE evaluate
  // so the positions can't shift under us. A single-target weapon (and the
  // adjacent melee swing alike) lights EXACTLY the victim's tile — never a
  // disc; the exactness is asserted OUTSIDE the poll so a real regression
  // (a disc, the wrong tile) still fails loudly instead of timing out.
  //
  // Entering a bubble only guarantees CombatRadius (6), not bow range (4),
  // and a monster already busy fighting someone else may never close the
  // gap on its own — so keep STEPPING toward one until it is in range,
  // exactly as ranged.spec.ts does. The stepping is progress-aware per the
  // #181 header: strictly-closing steps first, tabu on recently-visited
  // hexes (an equal-distance tile pair otherwise traps the greedy step in
  // a permanent oscillation — the captured deadlock), rotation to the
  // next-nearest monster when our hex stops changing, and out-of-combat
  // re-engagement (death-respawn lands us out of the bubble; the old loop
  // bailed to null forever there).
  const BOW_RANGE = 4;
  let visited: Hex[] = [];
  const tracker = progressTracker(10);
  let skip = 0;
  let single: { hex: Hex; tiles: Hex[] } | null = null;

  const bowPoll = expect
    .poll(
      async () => {
        const st = await page.evaluate(
          ({ monsterKind, bowRange, skip: skipN, avoid }) => {
            const me = window.game.me;
            if (me === null) {
              return { done: false as const, targetDist: null };
            }

            const d = (a: { q: number; r: number }, b: { q: number; r: number }): number => {
              const dq = a.q - b.q;
              const dr = a.r - b.r;

              return (Math.abs(dq) + Math.abs(dr) + Math.abs(-dq - dr)) / 2;
            };

            const monsters = window.game.positions.filter((p) => p.kind === monsterKind);
            if (monsters.length === 0) {
              return { done: false as const, targetDist: null };
            }

            const sorted = monsters
              .slice()
              .sort((a, b) => d(me.hex, a.hex) - d(me.hex, b.hex) || a.id - b.id);
            const target = sorted[skipN % sorted.length]!;
            const distTo = d(me.hex, target.hex);

            if (distTo <= bowRange && window.game.inCombat) {
              window.game.hoverTile(target.hex.q, target.hex.r);
              const tiles = window.game.hoverAttackTiles;
              if (tiles.length > 0) {
                return { done: true as const, hex: target.hex, tiles };
              }
              // In range but nothing lights (e.g. the target sits in another
              // resolving domain): report no progress so rotation kicks in.
              return { done: false as const, targetDist: null };
            }

            if (!window.game.inCombat) {
              // Fresh join, fled bubble, or death-respawn: tap the monster's
              // own hex and let the server pathfind the route.
              void window.game.tapHex(target.hex.q, target.hex.r);

              return { done: false as const, targetDist: distTo };
            }

            // In combat and out of range: single-step via this bubble turn's
            // reachable tiles. Strictly-closing steps first; otherwise any
            // not-recently-visited tile (the tabu that breaks equal-distance
            // oscillation); otherwise anything — never stand still.
            const moves = window.game.combatMoves;
            if (moves.length > 0) {
              const avoided = (h: { q: number; r: number }): boolean =>
                avoid.some((a) => a.q === h.q && a.r === h.r);

              let cands = moves.filter((h) => d(h, target.hex) < distTo);
              if (cands.length === 0) {
                cands = moves.filter((h) => !avoided(h));
              }
              if (cands.length === 0) {
                cands = moves;
              }

              let step = cands[0]!;
              for (const h of cands.slice(1)) {
                if (d(h, target.hex) < d(step, target.hex)) {
                  step = h;
                }
              }

              void window.game.tapHex(step.q, step.r);
            }

            return { done: false as const, targetDist: distTo, at: me.hex };
          },
          { monsterKind: EntityMonster, bowRange: BOW_RANGE, skip, avoid: visited },
        );

        if (st.done) {
          single = { hex: st.hex, tiles: st.tiles };

          return true;
        }

        // Tabu the in-combat hexes we've stood on so the greedy stepper can't
        // oscillate between an equal-distance pair.
        if ("at" in st && st.at !== undefined && !visited.some((v) => v.q === st.at!.q && v.r === st.at!.r)) {
          visited.push(st.at);
        }

        const prevSkip = skip;
        skip = tracker.note(st.targetDist);
        if (skip !== prevSkip) {
          visited = []; // fresh tabu for the fresh target
        }

        return false;
      },
      { timeout: 20_000, intervals: [300] },
    )
    .toBe(true);

  try {
    await bowPoll;
  } catch (err) {
    await dumpState(page, "bow-range");
    throw err;
  }

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

test("committed attack indicator is cleared by the next turn-over (#252)", async ({ page }) => {
  // #252's contract: the committed indicator (crosshair + lit tiles) clears at
  // the resolving bundle's playback end, and — the reliability half — is GONE
  // no later than the arrival of a bundle NEWER than the resolving one. The
  // failing case (a throttled rAF starving the wall-clock deadline check) is
  // not reproducible headless (the ticker always runs here), so this spec pins
  // the turn-driven contract itself; the throttle path is covered by the
  // onTurn backstop this contract forces. De-raced per the repo rule: every
  // assert reads state captured in the same evaluate as the poll condition —
  // no sleeps, no wall-clock.
  test.slow();

  await seedIdentity(page, { class: "rogue" });

  await page.goto("/");

  try {
    await expect
      .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
      .toBe(true);
    await expect.poll(() => page.evaluate(() => window.game.class)).toBe(ClassRogue);
    await expect.poll(() => page.evaluate(() => window.game.monsters)).toBeGreaterThanOrEqual(1);
  } catch (err) {
    await dumpState(page, "clear-contract-join");
    throw err;
  }

  await chaseIntoCombat(page);

  // Approach until a monster is in bow range, then tap it and capture the
  // planted indicator AND the current turn in the SAME evaluate (tapHex plants
  // synchronously, before the intent POST settles — the same atomicity the
  // mage shot poll above leans on). A tap that routed as a move plants
  // nothing and returns null, so the poll retries. Progress-aware stepping
  // per the #181 header: strictly-closing steps, tabu, target rotation,
  // out-of-combat re-engagement.
  const BOW = 4;
  let visited: Hex[] = [];
  const tracker = progressTracker(10);
  let skip = 0;
  let shot: { tapTurn: number; tiles: Hex[]; action: { kind: string; target: Hex } | null } | null = null;

  const commitPoll = expect
    .poll(
      async () => {
        const st = await page.evaluate(
          ({ monsterKind, bowRange, skip: skipN, avoid }) => {
            const me = window.game.me;
            if (me === null) {
              return { done: false as const, targetDist: null };
            }

            const d = (a: { q: number; r: number }, b: { q: number; r: number }): number => {
              const dq = a.q - b.q;
              const dr = a.r - b.r;

              return (Math.abs(dq) + Math.abs(dr) + Math.abs(-dq - dr)) / 2;
            };

            const monsters = window.game.positions.filter((p) => p.kind === monsterKind);
            if (monsters.length === 0) {
              return { done: false as const, targetDist: null };
            }

            const sorted = monsters
              .slice()
              .sort((a, b) => d(me.hex, a.hex) - d(me.hex, b.hex) || a.id - b.id);
            const target = sorted[skipN % sorted.length]!;
            const distTo = d(me.hex, target.hex);

            if (distTo <= bowRange && window.game.inCombat) {
              const tapTurn = window.game.turn;
              void window.game.tapHex(target.hex.q, target.hex.r);
              const tiles = window.game.committedAttackTiles;
              const action = window.game.committedAction;
              if (tiles.length > 0 || action !== null) {
                return { done: true as const, tapTurn, tiles, action };
              }
              // Routed as a move (positions shifted under the tap): no
              // progress, let rotation kick in.
              return { done: false as const, targetDist: null };
            }

            if (!window.game.inCombat) {
              void window.game.tapHex(target.hex.q, target.hex.r);

              return { done: false as const, targetDist: distTo };
            }

            const moves = window.game.combatMoves;
            if (moves.length > 0) {
              const avoided = (h: { q: number; r: number }): boolean =>
                avoid.some((a) => a.q === h.q && a.r === h.r);

              let cands = moves.filter((h) => d(h, target.hex) < distTo);
              if (cands.length === 0) {
                cands = moves.filter((h) => !avoided(h));
              }
              if (cands.length === 0) {
                cands = moves;
              }

              let step = cands[0]!;
              for (const h of cands.slice(1)) {
                if (d(h, target.hex) < d(step, target.hex)) {
                  step = h;
                }
              }

              void window.game.tapHex(step.q, step.r);
            }

            return { done: false as const, targetDist: distTo, at: me.hex };
          },
          { monsterKind: EntityMonster, bowRange: BOW, skip, avoid: visited },
        );

        if (st.done) {
          shot = { tapTurn: st.tapTurn, tiles: st.tiles, action: st.action };

          return true;
        }

        if ("at" in st && st.at !== undefined && !visited.some((v) => v.q === st.at!.q && v.r === st.at!.r)) {
          visited.push(st.at);
        }

        const prevSkip = skip;
        skip = tracker.note(st.targetDist);
        if (skip !== prevSkip) {
          visited = [];
        }

        return false;
      },
      { timeout: 20_000, intervals: [300] },
    )
    .toBe(true);

  try {
    await commitPoll;
  } catch (err) {
    await dumpState(page, "clear-contract-commit");
    throw err;
  }

  // Something is lit at commit time (crosshair for the single-target bow;
  // tiles for either shape) — the precondition the clear assert consumes.
  expect(shot!.tiles.length > 0 || shot!.action !== null).toBe(true);

  // tapTurn is the last bundle BEFORE the commit, so the resolving bundle is
  // tapTurn+1 and any bundle at tapTurn+2 or later is past the turn-over.
  // Poll turn and indicator in ONE evaluate: reading them separately would
  // let a bundle slip between the poll and the assert. No taps happen after
  // the commit, so nothing can legitimately re-light the indicator.
  let after: { turn: number; tiles: Hex[]; action: { kind: string; target: Hex } | null } | null = null;
  const clearPoll = expect
    .poll(
      async () => {
        after = await page.evaluate(() => ({
          turn: window.game.turn,
          tiles: window.game.committedAttackTiles,
          action: window.game.committedAction,
        }));

        return after !== null && after.turn >= shot!.tapTurn + 2;
      },
      { timeout: 20_000, intervals: [100] },
    )
    .toBe(true);

  try {
    await clearPoll;
  } catch (err) {
    await dumpState(page, "clear-contract-turnover");
    throw err;
  }

  expect(after!.tiles).toEqual([]);
  expect(after!.action).toBeNull();
});
