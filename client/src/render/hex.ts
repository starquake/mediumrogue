// Flat-top hex geometry, straight from Red Blob Games' hex guide. The server
// owns all game-rule hex math (distance, neighbors); the client only ever
// converts axial coordinates to pixels for drawing.
import type { Hex } from "../protocol.gen";

/** Distance from a hex's center to any of its six corners, in pixels. */
export const HEX_SIZE = 22;

export interface Point {
  x: number;
  y: number;
}

/** Center of a hex in world pixels (flat-top orientation). */
export function hexToPixel(hex: Hex): Point {
  return {
    x: HEX_SIZE * 1.5 * hex.q,
    y: HEX_SIZE * ((Math.sqrt(3) / 2) * hex.q + Math.sqrt(3) * hex.r),
  };
}

/** The six corner points of a hex, as a flat [x0, y0, x1, y1, …] array for PixiJS. */
export function hexCorners(center: Point, size: number = HEX_SIZE): number[] {
  const points: number[] = [];
  for (let i = 0; i < 6; i++) {
    const angle = (Math.PI / 180) * (60 * i);
    points.push(center.x + size * Math.cos(angle), center.y + size * Math.sin(angle));
  }

  return points;
}
