import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";
import type { Hex, MapResponse } from "../src/protocol.gen";

declare global {
  interface Window {
    game: GameDebug;
  }
}

// axialNeighbors mirrors internal/game.HexNeighbors: the six adjacent axial
// hexes, flat-top orientation. Duplicated from walk.spec.ts (each e2e spec
// keeps its own small geometry helpers — no shared test module in this repo).
function axialNeighbors(h: Hex): Hex[] {
  return [
    { q: h.q, r: h.r - 1 },
    { q: h.q + 1, r: h.r - 1 },
    { q: h.q + 1, r: h.r },
    { q: h.q, r: h.r + 1 },
    { q: h.q - 1, r: h.r + 1 },
    { q: h.q - 1, r: h.r },
  ];
}

function hexDistance(a: Hex, b: Hex): number {
  const dq = a.q - b.q;
  const dr = a.r - b.r;
  const ds = -dq - dr;

  return (Math.abs(dq) + Math.abs(dr) + Math.abs(ds)) / 2;
}

// pickDistance2Destination finds a walkable hex exactly two steps from start:
// a walkable neighbor of a walkable neighbor. This mirrors the server-side
// discovery in internal/game/world_test.go (geometry-independent — it never
// assumes a fixed offset is walkable on the procedurally generated map).
function pickDistance2Destination(map: MapResponse, start: Hex): Hex | null {
  const walkable = new Set<string>();
  for (const tile of map.tiles) {
    if (tile.terrain === "grass" || tile.terrain === "forest") {
      walkable.add(`${tile.hex.q},${tile.hex.r}`);
    }
  }
  const isWalkable = (h: Hex): boolean => walkable.has(`${h.q},${h.r}`);

  for (const n1 of axialNeighbors(start)) {
    if (!isWalkable(n1)) {
      continue;
    }
    for (const n2 of axialNeighbors(n1)) {
      if (isWalkable(n2) && hexDistance(start, n2) === 2) {
        return n2;
      }
    }
  }

  return null;
}

test("the procedural world renders and the camera follows my movement", async ({ page }) => {
  await page.goto("/");

  // The generated map (radius 24 by default) is on stage.
  await expect.poll(() => page.evaluate(() => window.game.tiles)).toBeGreaterThan(0);

  await expect
    .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
    .toBe(true);

  const start = await page.evaluate(() => window.game.me!.hex);
  const before = await page.evaluate(() => window.game.camera);

  // Walk several hexes across the procedurally generated world — same
  // destination-discovery + tapHex + multi-turn-poll pattern as walk.spec.ts,
  // so the camera has real, server-authoritative movement to follow.
  const map = await page.evaluate(() => fetch("/api/map").then((r) => r.json() as Promise<MapResponse>));
  const dest = pickDistance2Destination(map, start);
  expect(dest, "expected a walkable distance-2 hex from spawn on the generated map").not.toBeNull();

  await page.evaluate((d) => window.game.tapHex(d!.q, d!.r), dest);

  // The server-authoritative position reaches the destination over several
  // 250ms turns.
  await expect
    .poll(
      () =>
        page.evaluate((d) => {
          const hex = window.game.me!.hex;

          return hex.q === d!.q && hex.r === d!.r;
        }, dest),
      { timeout: 10_000 },
    )
    .toBe(true);

  // The camera panned to keep me centred — it must have moved along with me.
  await page.waitForFunction(
    (b) => {
      const c = window.game.camera;

      return c.x !== b.x || c.y !== b.y;
    },
    before,
    { timeout: 10_000 },
  );

  const after = await page.evaluate(() => window.game.camera);
  expect(after.x !== before.x || after.y !== before.y).toBeTruthy();
});
