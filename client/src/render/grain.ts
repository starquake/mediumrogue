import { Texture } from "pixi.js";

import type { Terrain } from "../protocol.gen";
import { TerrainForest, TerrainGrass } from "../protocol.gen";
import { hexNoise } from "./terrain";

// grain.ts (#296): the per-terrain surface texture, generated at runtime.
//
// Procedural rather than shipped as an image: the palette in map.ts stays the
// single source of truth for what grass IS, nothing extra lands in the bundle,
// and the whole thing is a few hundred draw calls once at startup.
//
// Each texture tiles in WORLD space (see GRAIN_MATRIX at the call site), not
// per hex. Tiling inside each hex would print a visible repeat in every cell —
// the exact grid effect this slice is removing.

/** GRAIN_PX is the tile size. Power of two so `repeat` wrapping is universally safe. */
const GRAIN_PX = 64;

/** How far grain strays from the terrain's base colour, per feature type. */
const SPECKLE_COUNT = 420;
const SPECKLE_SPREAD = 0.34;
const MOTTLE_COUNT = 150;
const MOTTLE_SPREAD = 0.5;
const FRACTURE_COUNT = 22;
const FLECK_COUNT = 190;

const clamp255 = (v: number): number => Math.max(0, Math.min(255, Math.round(v)));

function css(hex: number, k: number): string {
  const r = clamp255(((hex >> 16) & 0xff) * k);
  const g = clamp255(((hex >> 8) & 0xff) * k);
  const b = clamp255((hex & 0xff) * k);

  return `rgb(${r},${g},${b})`;
}

/** rnd is hexNoise reused as a plain 2D hash, so a texture is identical every run. */
const rnd = (i: number, salt: number): number => hexNoise(i * 31 + salt, salt * 17 + 3);

function paintSpeckle(ctx: CanvasRenderingContext2D, base: number): void {
  for (let i = 0; i < SPECKLE_COUNT; i++) {
    ctx.fillStyle = css(base, 1 - SPECKLE_SPREAD / 2 + rnd(i, 3) * SPECKLE_SPREAD);
    ctx.fillRect(rnd(i, 1) * GRAIN_PX, rnd(i, 2) * GRAIN_PX, 1, 1);
  }
}

function paintMottle(ctx: CanvasRenderingContext2D, base: number): void {
  for (let i = 0; i < MOTTLE_COUNT; i++) {
    ctx.fillStyle = css(base, 1 - MOTTLE_SPREAD / 2 + rnd(i, 7) * MOTTLE_SPREAD);
    ctx.beginPath();
    ctx.arc(rnd(i, 4) * GRAIN_PX, rnd(i, 5) * GRAIN_PX, 1.2 + rnd(i, 6) * 2.6, 0, Math.PI * 2);
    ctx.fill();
  }
}

function paintFracture(ctx: CanvasRenderingContext2D, base: number): void {
  ctx.strokeStyle = css(base, 0.72);
  ctx.lineWidth = 1;
  for (let i = 0; i < FRACTURE_COUNT; i++) {
    const x = rnd(i, 8) * GRAIN_PX;
    const y = rnd(i, 9) * GRAIN_PX;
    ctx.beginPath();
    ctx.moveTo(x, y);
    ctx.lineTo(x + (rnd(i, 10) - 0.5) * 26, y + (rnd(i, 11) - 0.5) * 26);
    ctx.stroke();
  }
  for (let i = 0; i < FLECK_COUNT; i++) {
    ctx.fillStyle = css(base, 1.06 + rnd(i, 12) * 0.2);
    ctx.fillRect(rnd(i, 13) * GRAIN_PX, rnd(i, 14) * GRAIN_PX, 1, 1);
  }
}

/**
 * grainTexture builds the tiling surface for one terrain, with its base colour
 * already baked in — the per-hex noise and warm/cool tilt then ride as a tint
 * on the fill, so a hex still varies without needing a texture of its own.
 *
 * Returns Texture.EMPTY when there is no 2D context (headless oddities), which
 * degrades to the flat colour rather than throwing on a render path.
 */
export function grainTexture(terrain: Terrain, base: number): Texture {
  const canvas = document.createElement("canvas");
  canvas.width = GRAIN_PX;
  canvas.height = GRAIN_PX;

  const ctx = canvas.getContext("2d");
  if (ctx === null) return Texture.EMPTY;

  ctx.fillStyle = css(base, 1);
  ctx.fillRect(0, 0, GRAIN_PX, GRAIN_PX);

  if (terrain === TerrainGrass) paintSpeckle(ctx, base);
  else if (terrain === TerrainForest) paintMottle(ctx, base);
  else paintFracture(ctx, base);

  const texture = Texture.from(canvas);
  // Repeat so one small tile covers the whole world; nearest keeps the retro
  // crispness rather than smearing the speckle at zoom.
  texture.source.wrapMode = "repeat";
  texture.source.scaleMode = "nearest";

  return texture;
}
