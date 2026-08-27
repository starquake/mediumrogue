import { Graphics } from "pixi.js";

import type { MapResponse } from "../protocol.gen";
import { TerrainForest, TerrainGrass, TerrainMud } from "../protocol.gen";
import { hexToPixel, HEX_SIZE } from "./hex";
import { hexKey, hexNoise, MAX_WATER_DEPTH } from "./terrain";

// scatter.ts (#296): the sparse motifs that give a terrain character — tufts on
// forest, flecks on rock, glints on shallow water.
//
// This is the one pass that adds real geometry rather than just changing a
// colour, so it lives in its own Graphics: separable, separately measurable,
// and separately cuttable if it ever costs too much.
//
// Every position is hashed from the hex's own coordinates, so motifs are
// identical on every client and every reload. Nothing here animates — a tuft
// that moved between frames would read as noise, not as a tree.

const TUFTS_MIN = 2;
const TUFTS_VARY = 3;
const TUFT_HEIGHT = 4.5;
const TUFT_HEIGHT_VARY = 3.5;
const TUFT_DARK = 0x122010;
const TUFT_LIT = 0x486840;
const TUFT_LIT_ALPHA = 0.55;

const FLECKS_MIN = 1;
const FLECKS_VARY = 3;
const FLECK_SIZE = 2.4;
const FLECK_SIZE_VARY = 3.2;
const FLECK_COLOR = 0x605e56;
const FLECK_ALPHA = 0.75;

const GLINT_COLOR = 0xa8d6f0;
const GLINT_MAX = 3;
const GLINT_SKIP = 0.45;
const GLINT_ALPHA_BASE = 0.16;
const GLINT_ALPHA_SHALLOW = 0.3;

/** spread places motif `i` somewhere inside a hex, deterministically. */
function spread(cx: number, cy: number, q: number, r: number, i: number, reach: number): { x: number; y: number } {
  const angle = hexNoise(q + i, r + 7) * Math.PI * 2;
  const dist = hexNoise(q + i, r + 8) * HEX_SIZE * reach;

  return { x: cx + Math.cos(angle) * dist, y: cy + Math.sin(angle) * dist };
}

function tufts(g: Graphics, cx: number, cy: number, q: number, r: number): void {
  const n = TUFTS_MIN + Math.floor(hexNoise(q, r + 99) * TUFTS_VARY);

  for (let i = 0; i < n; i++) {
    const { x, y } = spread(cx, cy, q, r, i, 0.52);
    const h = TUFT_HEIGHT + hexNoise(q + i, r + 9) * TUFT_HEIGHT_VARY;

    g.poly([x, y - h, x - h * 0.42, y + h * 0.34, x + h * 0.42, y + h * 0.34]).fill(TUFT_DARK);
    // A lit face on one side, so a tuft has a direction instead of reading as
    // a flat triangle.
    g.poly([x - 0.6, y - h + 1.4, x - h * 0.26, y + h * 0.2, x + h * 0.06, y + h * 0.2]).fill({
      color: TUFT_LIT,
      alpha: TUFT_LIT_ALPHA,
    });
  }
}

function flecks(g: Graphics, cx: number, cy: number, q: number, r: number): void {
  const n = FLECKS_MIN + Math.floor(hexNoise(q, r + 55) * FLECKS_VARY);

  for (let i = 0; i < n; i++) {
    const { x, y } = spread(cx, cy, q, r, i, 0.5);
    const w = FLECK_SIZE + hexNoise(q + i, r + 13) * FLECK_SIZE_VARY;

    g.poly([x - w, y + w * 0.5, x - w * 0.3, y - w * 0.7, x + w * 0.8, y - w * 0.1, x + w * 0.4, y + w * 0.6]).fill({
      color: FLECK_COLOR,
      alpha: FLECK_ALPHA,
    });
  }
}

function glints(g: Graphics, cx: number, cy: number, q: number, r: number, depth: number): void {
  // Denser and brighter in the shallows: light catches a surface near the
  // shore, and the middle of a lake stays dark.
  const shallow = Math.max(0, 1 - (depth - 1) / (MAX_WATER_DEPTH - 2));
  const n = Math.round(1 + shallow * (GLINT_MAX - 1));

  for (let i = 0; i < n; i++) {
    if (hexNoise(q + i * 13, r + 31) < GLINT_SKIP) continue;

    const { x, y } = spread(cx, cy, q, r, i, 0.55);
    const len = 1.6 + hexNoise(q + i, r + 43) * 1.6;

    g.poly([x, y, x + len, y, x + len, y + 1, x, y + 1]).fill({
      color: GLINT_COLOR,
      alpha: GLINT_ALPHA_BASE + shallow * GLINT_ALPHA_SHALLOW,
    });
  }
}

/**
 * buildScatter draws every terrain's motifs into one Graphics.
 *
 * Forest and rock are keyed off terrain; water uses the depth field so glints
 * cluster near the shore. Any other terrain is left bare — a motif has to earn
 * its place, and grass already reads through grain and per-hex noise.
 */
export function buildScatter(map: MapResponse, depths: Map<string, number>): Graphics {
  const g = new Graphics();

  for (const tile of map.tiles) {
    const { q, r } = tile.hex;
    const { x, y } = hexToPixel(tile.hex);

    const depth = depths.get(hexKey(q, r));
    if (depth !== undefined) {
      glints(g, x, y, q, r, depth);
    } else if (tile.terrain === TerrainForest) {
      tufts(g, x, y, q, r);
    } else if (tile.terrain !== TerrainGrass && tile.terrain !== TerrainMud) {
      // Rock, and anything this client does not recognise — which already
      // renders in the rock colour (map.ts's ROCK_COLOR fallback), so it gets
      // the stone treatment to match. GRASS is deliberately bare: it already
      // reads through grain and per-hex noise, and tufting it would turn open
      // ground into visual noise. MUD is bare for the same reason (#437): its
      // character is in the grain's puddles, and flecking it would have given
      // a bog the stone treatment.
      flecks(g, x, y, q, r);
    }
  }

  return g;
}
