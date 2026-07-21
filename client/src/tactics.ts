// Combat tactics: the pure geometry/routing that turns "where am I, what do I
// hold, who's around" into the reachable-tile sets and the attack-tile
// resolver the renderer and click handler both consume. Extracted from main.ts
// (#213) so the per-weapon range/AoE/hostile rules live in ONE resolver
// (rangedAttackTiles) feeding BOTH the click predicate (isRangedAttackClick)
// and the highlight (attackTilesFor) — previously two independent
// re-implementations that had to agree tile-for-tile. Every function is pure:
// it reads a TacticsCtx snapshot (built by main.ts from window.game + the
// static walkability set), never module state, so it stays correct regardless
// of when it's called. The server independently re-checks everything on
// resolution — these only drive the UX preview.
import { EntityMonster, StackCap } from "./protocol.gen";
import type { Hex } from "./protocol.gen";
import { DIRECTIONS, hexDistance, neighbor } from "./render/hex";

// One held ranged/magic weapon's range/AoE stats. Kept PER WEAPON, never
// collapsed into independent maxes: max(range) + max(aoe) would synthesize a
// range/AoE combination NEITHER weapon has.
export interface RangedWeapon {
  rangeHex: number;
  aoeRadius: number;
}

// A snapshot of everything the tactics functions need, built once per call by
// main.ts from window.game (me/inCombat/positions), the equipped ranged
// weapons, the static walkability set, and the last computed reach.
export interface TacticsCtx {
  me: Hex | null;
  inCombat: boolean;
  weapons: RangedWeapon[];
  positions: { kind: string; hex: Hex }[];
  walkable: Set<string>;
  reach: { moves: Hex[]; melees: Hex[] };
}

// How many hexes an entity can cover in one action-gated combat turn. 1 is
// the current rule (one step per turn, same as the resolution walks paths);
// combatReach is a BFS precisely so a future run/jump ability — or a
// pipeline-supplied per-entity movement range — only changes this number (or
// its source), not the structure.
const COMBAT_MOVE_RANGE = 1;

/** True iff hexes `a` and `b` are the same. */
function sameHex(a: Hex, b: Hex): boolean {
  return a.q === b.q && a.r === b.r;
}

/** Whether `h` appears in `list` (by value). Shared by the routing checks. */
export function inList(list: Hex[], h: Hex): boolean {
  return list.some((x) => sameHex(x, h));
}

// hexesWithin enumerates every hex within `radius` of center (the axial-disc
// loop from Red Blob's hex guide) — an AoE blast's footprint (#101). Small
// radii only (weapon aoeRadius is 1 today), so no reason to scan map tiles.
function hexesWithin(center: Hex, radius: number): Hex[] {
  const out: Hex[] = [];
  for (let dq = -radius; dq <= radius; dq++) {
    for (let dr = Math.max(-radius, -dq - radius); dr <= Math.min(radius, -dq + radius); dr++) {
      out.push({ q: center.q + dq, r: center.r + dr });
    }
  }

  return out;
}

// aoeReachesDist: some held AoE-capable (aoeRadius > 0) ranged/magic weapon
// reaches dist — the weapon that lets a click on empty ground still attack.
function aoeReachesDist(weapons: RangedWeapon[], dist: number): boolean {
  return weapons.some((w) => w.aoeRadius > 0 && dist <= w.rangeHex);
}

// aoeReaches: aoeReachesDist measured from my own hex to target.
export function aoeReaches(target: Hex, ctx: TacticsCtx): boolean {
  return ctx.me !== null && aoeReachesDist(ctx.weapons, hexDistance(ctx.me, target));
}

// maxRangedRange: the farthest any held ranged/magic weapon reaches (0 when
// none held) — drives the red range-wash overlay, where "some weapon can act
// on this tile" is the right rendering question even if not every weapon can.
export function maxRangedRange(weapons: RangedWeapon[]): number {
  return weapons.reduce((m, w) => Math.max(m, w.rangeHex), 0);
}

/**
 * The single resolver both the click and the highlight run on: the exact tiles
 * a ranged/magic attack CLICK on `target` would hit, mirroring the server's
 * per-weapon rule (rangedDefsFor: each held ranged/magic weapon fires iff ITS
 * OWN rangeHex reaches the target). An AoE weapon (aoeRadius > 0) in range
 * blasts every hex within its radius of the target; a single-target weapon (a
 * rogue's bow) only fires at a hostile actually standing on the clicked hex —
 * so a short AoE weapon never green-lights an empty-ground click that only a
 * longer single-target weapon reaches.
 *
 * `filterWalkable` distinguishes the two consumers with no behavioural drift:
 *  - highlight (attackTilesFor) passes true — entities only ever stand on
 *    walkable tiles, so the disc is trimmed to the hexes a blast can actually
 *    hit.
 *  - predicate (isRangedAttackClick) passes false — an AoE disc always
 *    contains its own centre, so a non-empty raw set is exactly the old
 *    "aoeReachesDist OR (single-target-in-range AND hostile)" test.
 */
function rangedAttackTiles(target: Hex, ctx: TacticsCtx, filterWalkable: boolean): Hex[] {
  const me = ctx.me;
  if (me === null) {
    return [];
  }

  const dist = hexDistance(me, target);
  const hostileAtTarget = ctx.positions.some((p) => p.kind === EntityMonster && sameHex(p.hex, target));
  const tiles = new Map<string, Hex>();
  for (const w of ctx.weapons) {
    if (dist > w.rangeHex) {
      continue;
    }

    if (w.aoeRadius > 0) {
      for (const h of hexesWithin(target, w.aoeRadius)) {
        if (!filterWalkable || ctx.walkable.has(`${h.q},${h.r}`)) {
          tiles.set(`${h.q},${h.r}`, h);
        }
      }
    } else if (hostileAtTarget) {
      tiles.set(`${target.q},${target.r}`, target);
    }
  }

  return [...tiles.values()];
}

/**
 * Decides whether a click on `target` should fire a ranged attack instead of a
 * move. Out of combat, or no ranged weapon equipped: always a move. Otherwise
 * it is a ranged attack exactly when the unfiltered resolver lights at least
 * one tile — an AoE weapon in range (its disc always includes the target), or
 * a single-target weapon in range with a hostile on the clicked hex.
 */
export function isRangedAttackClick(target: Hex, ctx: TacticsCtx): boolean {
  if (!ctx.inCombat || ctx.me === null) {
    return false;
  }

  return rangedAttackTiles(target, ctx, false).length > 0;
}

/**
 * The exact hexes an attack CLICK on `target` would hit, or empty when a click
 * there would not attack (out of combat, own hex, a step, out of every
 * weapon's range). One rule per branch of the server's resolution: an adjacent
 * hostile without an AoE weapon in reach is a melee swing (one tile — the
 * weapon-by-distance identity, #116); anything else fires every held
 * ranged/magic weapon whose OWN range covers the target (rangedAttackTiles,
 * walkable-filtered). Mirrors clickTarget's move-vs-attack routing tile for
 * tile — purely a UX preview.
 */
export function attackTilesFor(target: Hex, ctx: TacticsCtx): Hex[] {
  const me = ctx.me;
  if (!ctx.inCombat || me === null) {
    return [];
  }

  if (sameHex(me, target)) {
    return []; // own hex: wait/cancel, never an attack
  }

  if (inList(ctx.reach.moves, target)) {
    return []; // a step, not an attack
  }

  const dist = hexDistance(me, target);
  if (inList(ctx.reach.melees, target) && !aoeReachesDist(ctx.weapons, dist)) {
    return [target]; // melee swing: the one adjacent victim tile
  }

  return rangedAttackTiles(target, ctx, true);
}

/**
 * combatReach BFS-expands from my hex up to COMBAT_MOVE_RANGE steps through
 * walkable, non-hostile, non-full tiles. A hostile-held tile on the frontier
 * is a melee-attack target (stepping in swings), never expanded through.
 * Occupancy and hostility read the snapshot's positions.
 */
export function combatReach(ctx: TacticsCtx): { moves: Hex[]; melees: Hex[] } {
  const me = ctx.me;
  if (me === null) {
    return { moves: [], melees: [] };
  }

  const occupants = new Map<string, { n: number; hostile: boolean }>();
  for (const p of ctx.positions) {
    const key = `${p.hex.q},${p.hex.r}`;
    const o = occupants.get(key) ?? { n: 0, hostile: false };
    o.n += 1;
    o.hostile ||= p.kind === EntityMonster;
    occupants.set(key, o);
  }

  const moves: Hex[] = [];
  const melees: Hex[] = [];
  const seen = new Set<string>([`${me.q},${me.r}`]);
  let frontier: Hex[] = [me];

  for (let step = 0; step < COMBAT_MOVE_RANGE; step++) {
    const next: Hex[] = [];

    for (const from of frontier) {
      for (const dir of Object.keys(DIRECTIONS) as (keyof typeof DIRECTIONS)[]) {
        const h = neighbor(from, dir);
        const key = `${h.q},${h.r}`;
        if (seen.has(key) || !ctx.walkable.has(key)) {
          continue;
        }
        seen.add(key);

        const occ = occupants.get(key);
        if (occ?.hostile) {
          melees.push(h); // swing in, never walk through
        } else if ((occ?.n ?? 0) < StackCap) {
          moves.push(h);
          next.push(h); // a future range >1 keeps expanding from here
        }
      }
    }

    frontier = next;
  }

  return { moves, melees };
}
