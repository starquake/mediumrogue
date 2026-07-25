import { Container, Graphics } from "pixi.js";

import type { MapResponse, Terrain } from "../protocol.gen";
import { TerrainForest, TerrainGrass, TerrainWater } from "../protocol.gen";
import { hexCorners, hexToPixel, HEX_SIZE } from "./hex";
import { buildTerrainIndex, hexKey, hexNoise, MAX_WATER_DEPTH, waterDepths } from "./terrain";

// Muted retro palette; the CRT filter pass (milestone 9) sits on top of this.
// Rock doubles as the fallback for terrain values this client doesn't know,
// so a newer server renders as "something solid" instead of crashing.
const ROCK_COLOR = 0x45443f;
const TERRAIN_COLORS: Record<Terrain, number> = {
  [TerrainGrass]: 0x35513a,
  [TerrainForest]: 0x22391f,
  [TerrainWater]: 0x1d3d5c,
};
const OUTLINE = { width: 1, color: 0x0b0f0b, alpha: 0.8 };

// Water is shaded by DEPTH rather than textured (#296): a texture ignores the
// shape of a lake, while depth follows its shoreline. The range is deliberately
// narrow and centred on the old flat water colour above — wide enough for depth
// to read, muted enough that lakes do not pop out of the palette.
const WATER_SHALLOW = 0x244c73;
const WATER_DEEP = 0x162e45;

// NOISE_SPREAD is how far per-hex brightness strays from the base colour:
// enough that a region reads as a surface instead of a grid of identical cells,
// small enough that a terrain is never mistaken for a different one.
const NOISE_SPREAD = 0.3;
const NOISE_FLOOR = 1 - NOISE_SPREAD / 2;

// A slow warm->cool tilt across the world so a screenful is never one flat hue.
// Tiny on purpose: it should register as depth of field, not as a gradient
// anyone can point at.
const TILT_STRENGTH = 0.05;
const TILT_GREEN_SHARE = 0.3;

interface Rgb {
  r: number;
  g: number;
  b: number;
}

const toRgb = (hex: number): Rgb => ({ r: (hex >> 16) & 0xff, g: (hex >> 8) & 0xff, b: hex & 0xff });

const clamp255 = (v: number): number => Math.max(0, Math.min(255, Math.round(v)));

const toHex = (c: Rgb): number => (clamp255(c.r) << 16) | (clamp255(c.g) << 8) | clamp255(c.b);

const mix = (a: Rgb, b: Rgb, t: number): Rgb => ({
  r: a.r + (b.r - a.r) * t,
  g: a.g + (b.g - a.g) * t,
  b: a.b + (b.b - a.b) * t,
});

const scale = (c: Rgb, k: number): Rgb => ({ r: c.r * k, g: c.g * k, b: c.b * k });

/** waterColor ramps shallow to deep across the capped depth field. */
function waterColor(depth: number): Rgb {
  const t = (depth - 1) / (MAX_WATER_DEPTH - 1);

  return mix(toRgb(WATER_SHALLOW), toRgb(WATER_DEEP), Math.max(0, Math.min(1, t)));
}

/**
 * tilt shifts a colour warm to the east and cool to the west by a fraction of
 * how far across the world the hex sits. halfWidth is the world's own extent,
 * so the tilt always spans the map rather than a fixed pixel distance.
 */
function tilt(c: Rgb, x: number, halfWidth: number): Rgb {
  const t = halfWidth === 0 ? 0 : Math.max(-1, Math.min(1, x / halfWidth)) * TILT_STRENGTH;

  return { r: c.r * (1 + t), g: c.g * (1 + t * TILT_GREEN_SHARE), b: c.b * (1 - t) };
}

/** Draws the whole map into one container (a single Graphics batch). */
export function buildMapLayer(map: MapResponse): Container {
  const layer = new Container();
  const ground = new Graphics();

  const index = buildTerrainIndex(map.tiles);
  const depths = waterDepths(map.tiles, index);
  const halfWidth = HEX_SIZE * 1.5 * map.radius;

  for (const tile of map.tiles) {
    const { q, r } = tile.hex;
    const center = hexToPixel(tile.hex);

    // Water takes its colour from how far it is from shore; everything else
    // from the palette. `undefined` from the depth field means "not water".
    const depth = depths.get(hexKey(q, r));
    const base = depth === undefined ? toRgb(TERRAIN_COLORS[tile.terrain] ?? ROCK_COLOR) : waterColor(depth);

    // Per-hex brightness, hashed from the hex's own coordinates so it is the
    // same on every client and never shimmers between frames.
    const shaded = scale(base, NOISE_FLOOR + hexNoise(q, r) * NOISE_SPREAD);

    ground
      .poly(hexCorners(center, HEX_SIZE - 0.5))
      .fill(toHex(tilt(shaded, center.x, halfWidth)))
      .stroke(OUTLINE);
  }

  layer.addChild(ground);

  return layer;
}
