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

/**
 * Flat-top axial neighbor offsets, in the same N, NE, SE, S, SW, NW order as
 * the server's HexNeighbors — and as the movement keys W, E, D, X, A, Q.
 */
export const DIRECTIONS = {
  n: { q: 0, r: -1 },
  ne: { q: 1, r: -1 },
  se: { q: 1, r: 0 },
  s: { q: 0, r: 1 },
  sw: { q: -1, r: 1 },
  nw: { q: -1, r: 0 },
} as const;

export type Direction = keyof typeof DIRECTIONS;

/** The hex one step from `from` in the given direction. */
export function neighbor(from: Hex, dir: Direction): Hex {
  const d = DIRECTIONS[dir];

  return { q: from.q + d.q, r: from.r + d.r };
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

/**
 * The hex containing a world-pixel point — the inverse of hexToPixel for the
 * flat-top layout, via fractional axial → cube rounding (Red Blob Games).
 * Used to turn a click into a destination hex; the server owns reachability.
 */
export function pixelToHex(point: Point): Hex {
  const qf = ((2 / 3) * point.x) / HEX_SIZE;
  const rf = (-point.x / 3 + (Math.sqrt(3) / 3) * point.y) / HEX_SIZE;

  return cubeRound(qf, rf);
}

/** Rounds fractional axial coordinates to the nearest hex (cube rounding). */
function cubeRound(qf: number, rf: number): Hex {
  const sf = -qf - rf;
  let q = Math.round(qf);
  let r = Math.round(rf);
  const s = Math.round(sf);

  const dq = Math.abs(q - qf);
  const dr = Math.abs(r - rf);
  const ds = Math.abs(s - sf);

  if (dq > dr && dq > ds) {
    q = -r - s;
  } else if (dr > ds) {
    r = -q - s;
  }

  return { q: q + 0, r: r + 0 };
}
