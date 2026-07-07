import { Container, Graphics } from "pixi.js";

import type { MapResponse, Terrain } from "../protocol.gen";
import { TerrainForest, TerrainGrass, TerrainWater } from "../protocol.gen";
import { hexCorners, hexToPixel, HEX_SIZE } from "./hex";

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

/** Draws the whole map into one container (a single Graphics batch). */
export function buildMapLayer(map: MapResponse): Container {
  const layer = new Container();
  const ground = new Graphics();

  for (const tile of map.tiles) {
    const center = hexToPixel(tile.hex);
    ground
      .poly(hexCorners(center, HEX_SIZE - 0.5))
      .fill(TERRAIN_COLORS[tile.terrain] ?? ROCK_COLOR)
      .stroke(OUTLINE);
  }

  layer.addChild(ground);

  return layer;
}
