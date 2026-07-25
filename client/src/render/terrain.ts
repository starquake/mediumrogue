import type { Terrain, Tile } from "../protocol.gen";
import { TerrainWater } from "../protocol.gen";
import { DIRECTIONS } from "./hex";

// terrain.ts (#296): the two lookups the atmospheric ground pass needs, both
// derived once at map build and never touched again.
//
// Neither is game state — the server owns terrain and never sends this. It is
// purely "what does this hex need to know about its surroundings in order to
// be DRAWN", which is why it lives in render/ and is computed on the client.

/** MAX_WATER_DEPTH caps the depth ramp: past this, water is simply "deep". */
export const MAX_WATER_DEPTH = 5;

/**
 * hexNoise is a stable pseudo-random value in [0, 1) for an axial coordinate.
 *
 * Deterministic by design: the same hex is the same value on every frame, on
 * every client, forever. That is what lets terrain variation be baked once at
 * map build instead of animated, and it is why two players standing on the
 * same tile see the same ground.
 */
export function hexNoise(q: number, r: number): number {
  let h = (q * 374761393 + r * 668265263) ^ 0x5bf03635;
  h = (h ^ (h >>> 13)) * 1274126177;

  return ((h ^ (h >>> 16)) >>> 0) / 4294967296;
}

/** hexKey is the axial coordinate as a Map key — cheaper than nesting maps. */
export function hexKey(q: number, r: number): string {
  return `${q},${r}`;
}

/** buildTerrainIndex makes terrain answerable by coordinate rather than by scan. */
export function buildTerrainIndex(tiles: readonly Tile[]): Map<string, Terrain> {
  const index = new Map<string, Terrain>();
  for (const t of tiles) index.set(hexKey(t.hex.q, t.hex.r), t.terrain);

  return index;
}

/**
 * waterDepths returns, for every water hex, its hex distance to the nearest
 * shore — 1 in the shallows, rising to MAX_WATER_DEPTH out in the middle. Land
 * hexes are absent from the map rather than zero, so a caller reads
 * "undefined" as "not water" without a second lookup.
 *
 * Multi-source BFS seeded from every water hex that touches something which is
 * not water, so the whole field costs one pass over the map regardless of how
 * many lakes there are.
 *
 * The MAP EDGE counts as a shore: a hex at the rim of the world has no
 * neighbour on that side, and open sky is not deep ocean. Without this, the
 * boundary ring of a water-edged world would render as the deepest water on
 * the map.
 */
export function waterDepths(tiles: readonly Tile[], index: Map<string, Terrain>): Map<string, number> {
  const depths = new Map<string, number>();
  let frontier: { q: number; r: number }[] = [];

  for (const t of tiles) {
    if (t.terrain !== TerrainWater) continue;

    const { q, r } = t.hex;
    const touchesShore = Object.values(DIRECTIONS).some((d) => index.get(hexKey(q + d.q, r + d.r)) !== TerrainWater);
    if (touchesShore) {
      depths.set(hexKey(q, r), 1);
      frontier.push({ q, r });
    }
  }

  for (let depth = 2; depth <= MAX_WATER_DEPTH && frontier.length > 0; depth++) {
    const next: { q: number; r: number }[] = [];

    for (const { q, r } of frontier) {
      for (const d of Object.values(DIRECTIONS)) {
        const nq = q + d.q;
        const nr = r + d.r;
        const key = hexKey(nq, nr);
        if (index.get(key) !== TerrainWater || depths.has(key)) continue;

        depths.set(key, depth);
        next.push({ q: nq, r: nr });
      }
    }

    frontier = next;
  }

  // Anything still unvisited is water enclosed further than the cap — clamp it
  // rather than leaving it undefined, which a caller would read as land.
  for (const t of tiles) {
    if (t.terrain !== TerrainWater) continue;

    const key = hexKey(t.hex.q, t.hex.r);
    if (!depths.has(key)) depths.set(key, MAX_WATER_DEPTH);
  }

  return depths;
}
