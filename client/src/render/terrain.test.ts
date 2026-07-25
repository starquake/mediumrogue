import { describe, expect, it } from "vitest";

import type { Tile } from "../protocol.gen";
import { TerrainForest, TerrainGrass, TerrainWater } from "../protocol.gen";
import { buildTerrainIndex, hexKey, hexNoise, MAX_WATER_DEPTH, waterDepths } from "./terrain";

/** tile is a terse Tile literal so a fixture map reads as a shape, not a wall of objects. */
const tile = (q: number, r: number, terrain: Tile["terrain"]): Tile => ({ hex: { q, r }, terrain });

/** Every hex within `radius` of the origin, all grass unless `water` says otherwise. */
function disc(radius: number, water: [number, number][] = []): Tile[] {
  const isWater = new Set(water.map(([q, r]) => hexKey(q, r)));
  const tiles: Tile[] = [];
  for (let q = -radius; q <= radius; q++) {
    for (let r = Math.max(-radius, -q - radius); r <= Math.min(radius, -q + radius); r++) {
      tiles.push(tile(q, r, isWater.has(hexKey(q, r)) ? TerrainWater : TerrainGrass));
    }
  }
  return tiles;
}

describe("buildTerrainIndex", () => {
  it("looks a tile up by its axial coordinates", () => {
    const index = buildTerrainIndex([tile(0, 0, TerrainGrass), tile(2, -1, TerrainForest)]);

    expect(index.get(hexKey(0, 0))).toBe(TerrainGrass);
    expect(index.get(hexKey(2, -1))).toBe(TerrainForest);
  });

  it("returns undefined off the map, which callers read as 'not this terrain'", () => {
    const index = buildTerrainIndex([tile(0, 0, TerrainGrass)]);

    expect(index.get(hexKey(9, 9))).toBeUndefined();
  });
});

describe("waterDepths", () => {
  it("gives dry land no depth at all", () => {
    const tiles = disc(2, [[0, 0]]);
    const depths = waterDepths(tiles, buildTerrainIndex(tiles));

    expect(depths.get(hexKey(1, 0))).toBeUndefined();
  });

  it("puts a lone water hex in the shallows", () => {
    const tiles = disc(2, [[0, 0]]);
    const depths = waterDepths(tiles, buildTerrainIndex(tiles));

    expect(depths.get(hexKey(0, 0))).toBe(1);
  });

  it("deepens toward the middle of a lake", () => {
    // A radius-2 lake inside a radius-5 landmass: the rim is shallow, the
    // centre is two steps from any shore.
    const lake: [number, number][] = [];
    for (let q = -2; q <= 2; q++) {
      for (let r = Math.max(-2, -q - 2); r <= Math.min(2, -q + 2); r++) lake.push([q, r]);
    }
    const tiles = disc(5, lake);
    const depths = waterDepths(tiles, buildTerrainIndex(tiles));

    expect(depths.get(hexKey(2, 0))).toBe(1); // on the rim, touching land
    expect(depths.get(hexKey(1, 0))).toBe(2);
    expect(depths.get(hexKey(0, 0))).toBe(3); // dead centre, furthest from shore
  });

  it("caps depth so a big ocean does not run off the colour ramp", () => {
    // An all-water disc far wider than the cap: the centre must clamp.
    const all: [number, number][] = [];
    for (let q = -12; q <= 12; q++) {
      for (let r = Math.max(-12, -q - 12); r <= Math.min(12, -q + 12); r++) all.push([q, r]);
    }
    const tiles = disc(12, all);
    const depths = waterDepths(tiles, buildTerrainIndex(tiles));

    expect(depths.get(hexKey(0, 0))).toBe(MAX_WATER_DEPTH);
  });

  it("treats the map edge as a shore", () => {
    // Water at the rim of the world has no land neighbour — but it has no
    // NEIGHBOUR at all on that side, and open sky is not deep water.
    const tiles = disc(1, [
      [0, 0],
      [1, 0],
      [1, -1],
      [0, -1],
      [-1, 0],
      [-1, 1],
      [0, 1],
    ]);
    const depths = waterDepths(tiles, buildTerrainIndex(tiles));

    expect(depths.get(hexKey(1, 0))).toBe(1);
    expect(depths.get(hexKey(0, 0))).toBe(2);
  });
});

describe("hexNoise", () => {
  it("is stable for a coordinate", () => {
    expect(hexNoise(3, -7)).toBe(hexNoise(3, -7));
  });

  it("stays inside [0, 1)", () => {
    for (let q = -30; q <= 30; q += 7) {
      for (let r = -30; r <= 30; r += 7) {
        const n = hexNoise(q, r);
        expect(n).toBeGreaterThanOrEqual(0);
        expect(n).toBeLessThan(1);
      }
    }
  });

  it("differs between neighbours, so terrain does not band", () => {
    // Adjacent hexes sharing a value would produce visible stripes; sample a
    // patch and require a healthy spread of distinct values.
    const seen = new Set<number>();
    for (let q = 0; q < 20; q++) for (let r = 0; r < 20; r++) seen.add(hexNoise(q, r));

    expect(seen.size).toBeGreaterThan(390);
  });
});
