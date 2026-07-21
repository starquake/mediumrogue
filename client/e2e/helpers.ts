// Shared e2e helpers (#210): the identity/goto preamble, the progress-aware
// walk-into-a-bubble driver, and the Node-side hex geometry that specs used to
// hand-roll a copy of each. Behaviour-preserving extraction — every function
// here is byte-for-byte what the specs already ran inline; only the location
// changed. Browser-context helpers (the `dist` re-declared inside individual
// page.evaluate blocks) can't live here: page.evaluate serializes its callback
// and can't close over an imported symbol, so those stay in their specs.
import { expect, type Page } from "@playwright/test";

import { EntityMonster } from "../src/protocol.gen";
import type { Hex, MapResponse } from "../src/protocol.gen";

/**
 * Node-side axial hex distance (cube-coordinate form). The IN-page copies —
 * declared inside page.evaluate blocks so they run in the browser — can't
 * import this; this serves the Node-side callers (assertions, approach math).
 */
export function hexDist(a: Hex, b: Hex): number {
  const dq = a.q - b.q;
  const dr = a.r - b.r;
  const ds = -dq - dr;

  return (Math.abs(dq) + Math.abs(dr) + Math.abs(ds)) / 2;
}

// axialNeighbors mirrors internal/game.HexNeighbors: the six adjacent axial
// hexes, flat-top orientation.
function axialNeighbors(h: Hex): Hex[] {
  return [
    { q: h.q, r: h.r - 1 },
    { q: h.q + 1, r: h.r - 1 },
    { q: h.q + 1, r: h.r },
    { q: h.q, r: h.r + 1 },
    { q: h.q - 1, r: h.r + 1 },
    { q: h.q - 1, r: h.r },
  ];
}

/**
 * pickDistance2Destination finds a walkable hex exactly two steps from start:
 * a walkable neighbor of a walkable neighbor. This mirrors the server-side
 * discovery in internal/game/world_test.go (geometry-independent — it never
 * assumes a fixed offset is walkable on the map). Shared by walk.spec.ts and
 * procgen.spec.ts, which used byte-identical copies.
 */
export function pickDistance2Destination(map: MapResponse, start: Hex): Hex | null {
  const walkable = new Set<string>();
  for (const tile of map.tiles) {
    if (tile.terrain === "grass" || tile.terrain === "forest") {
      walkable.add(`${tile.hex.q},${tile.hex.r}`);
    }
  }
  const isWalkable = (h: Hex): boolean => walkable.has(`${h.q},${h.r}`);

  for (const n1 of axialNeighbors(start)) {
    if (!isWalkable(n1)) {
      continue;
    }
    for (const n2 of axialNeighbors(n1)) {
      if (isWalkable(n2) && hexDist(start, n2) === 2) {
        return n2;
      }
    }
  }

  return null;
}

/**
 * seedIdentity plants the "returning player" identity localStorage read at
 * module load (src/net/session.ts's loadIdentity/join) via addInitScript, so a
 * spec joins with a chosen class/species deterministically without touching the
 * start screen. An empty stored token still reaches the server as a brand-new
 * join — Join() only reclaims an existing entity for a token it recognizes —
 * but with the requested class/species. Must be called before gotoReady/goto.
 */
export async function seedIdentity(page: Page, opts: { class?: string; species?: string }): Promise<void> {
  await page.addInitScript((o) => {
    localStorage.setItem("mediumrogue.identity", JSON.stringify({ entityId: 0, token: "", ...o }));
  }, opts);
}

/**
 * gotoReady loads the client and waits until this player has joined and the SSE
 * stream is live — the `goto("/")` + poll(me !== null && connected) preamble
 * that opens nearly every spec.
 */
export async function gotoReady(page: Page): Promise<void> {
  await page.goto("/");

  await expect
    .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
    .toBe(true);
}

// dumpState logs the live client state when an approach poll starves —
// #181 went two full passes without a diagnosis because the timeouts
// carried no state; this makes the next sighting self-describing in the
// CI log (search for "STARVED").
export async function dumpState(page: Page, label: string): Promise<void> {
  const dump = await page.evaluate(
    (monsterKind) => ({
      me: window.game.me,
      died: window.game.died,
      inCombat: window.game.inCombat,
      connected: window.game.connected,
      turn: window.game.turn,
      combatMoves: window.game.combatMoves,
      monstersCounter: window.game.monsters,
      monsters: window.game.positions.filter((p) => p.kind === monsterKind).map((p) => ({ id: p.id, hex: p.hex })),
      positionsCount: window.game.positions.length,
    }),
    EntityMonster,
  );
  console.log(`STARVED[${label}] ` + JSON.stringify(dump));
}

// progressTracker rotates targets on stalled DISTANCE, not stalled position:
// a monster walking away (leash-return home) at the shared 1-hex-per-turn
// speed is an uncatchable treadmill — the chaser moves every turn yet never
// closes, which a position-based stuck check can never see (a captured #181
// failure mode: 20s of healthy walking, gap pinned at 10). If the best
// distance ever achieved toward the current target hasn't improved within
// `window` polls, switch to the next-nearest monster.
export const progressTracker = (window: number): { note: (targetDist: number | null) => number } => {
  let best: number | null = null;
  let stalePolls = 0;
  let skip = 0;

  return {
    note: (targetDist: number | null): number => {
      if (targetDist === null) {
        return skip; // no target/self yet: nothing to measure
      }

      if (best === null || targetDist < best) {
        best = targetDist;
        stalePolls = 0;
      } else {
        stalePolls += 1;
        if (stalePolls >= window) {
          skip += 1;
          best = null;
          stalePolls = 0;
        }
      }

      return skip;
    },
  };
};

// chaseIntoCombat drives the player until the client reports a combat
// bubble. Out of combat every tap goes to a monster's own hex, so the
// SERVER pathfinds the route (it walks around terrain; bodies never block
// out-of-combat movement). Progress-aware per the #181 header: when the
// gap to the current target stops shrinking (unreachable pocket, jammed
// route, or the treadmill above), rotate to the next-nearest monster.
export const chaseIntoCombat = async (page: Page): Promise<void> => {
  const tracker = progressTracker(10);
  let skip = 0;

  const poll = expect
    .poll(
      async () => {
        const st = await page.evaluate(
          ({ monsterKind, skip: skipN }) => {
            if (window.game.inCombat) {
              return { done: true, targetDist: null };
            }

            const me = window.game.me;
            if (me === null) {
              return { done: false, targetDist: null };
            }

            const d = (a: { q: number; r: number }, b: { q: number; r: number }): number => {
              const dq = a.q - b.q;
              const dr = a.r - b.r;

              return (Math.abs(dq) + Math.abs(dr) + Math.abs(-dq - dr)) / 2;
            };

            const monsters = window.game.positions.filter((p) => p.kind === monsterKind);
            if (monsters.length === 0) {
              return { done: false, targetDist: null };
            }

            const sorted = monsters
              .slice()
              .sort((a, b) => d(me.hex, a.hex) - d(me.hex, b.hex) || a.id - b.id);
            const target = sorted[skipN % sorted.length]!;

            void window.game.tapHex(target.hex.q, target.hex.r);

            return { done: false, targetDist: d(me.hex, target.hex) };
          },
          { monsterKind: EntityMonster, skip },
        );

        if (st.done) {
          return true;
        }

        skip = tracker.note(st.targetDist);

        return false;
      },
      { timeout: 20_000, intervals: [300] },
    )
    .toBe(true);

  try {
    await poll;
  } catch (err) {
    await dumpState(page, "chase");
    throw err;
  }
};
