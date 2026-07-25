import { describe, expect, it } from "vitest";

import { DIRECTIONS, EDGE_DIRECTIONS, hexCorners, hexToPixel } from "./hex";

describe("EDGE_DIRECTIONS", () => {
  it("names, for every edge, the neighbour that edge actually faces", () => {
    // Geometric proof rather than a restatement of the table: the midpoint of
    // edge i must be nearer the centre of the neighbour EDGE_DIRECTIONS[i]
    // than the centre of any other neighbour. A rotated or reversed mapping
    // fails this immediately.
    const origin = { q: 0, r: 0 };
    const corners = hexCorners(hexToPixel(origin));

    EDGE_DIRECTIONS.forEach((dir, i) => {
      const ax = corners[i * 2] ?? 0;
      const ay = corners[i * 2 + 1] ?? 0;
      const j = ((i + 1) % 6) * 2;
      const bx = corners[j] ?? 0;
      const by = corners[j + 1] ?? 0;
      const mid = { x: (ax + bx) / 2, y: (ay + by) / 2 };

      const distances = Object.entries(DIRECTIONS).map(([name, d]) => {
        const c = hexToPixel({ q: d.q, r: d.r });
        return { name, d: Math.hypot(c.x - mid.x, c.y - mid.y) };
      });
      distances.sort((a, b) => a.d - b.d);

      expect(distances[0]?.name).toBe(dir);
    });
  });

  it("covers all six neighbours exactly once", () => {
    expect(new Set(EDGE_DIRECTIONS).size).toBe(6);
    expect(EDGE_DIRECTIONS).toHaveLength(6);
  });
});
