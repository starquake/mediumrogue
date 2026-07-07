import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";
import type { Hex, MapResponse } from "../src/protocol.gen";

declare global {
  interface Window {
    game: GameDebug;
  }
}

// axialNeighbors mirrors internal/game.HexNeighbors: the six adjacent axial
// hexes, flat-top orientation.
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
// assumes a fixed offset is walkable on the static map).
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

test("clicking (tapHex) a distant hex walks my entity there over turns", async ({ page }) => {
  await page.goto("/");

  await expect
    .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
    .toBe(true);

  const start = await page.evaluate(() => window.game.me!.hex);

  // Pick a reachable destination two hexes from spawn by inspecting the real
  // map: the server rejects unwalkable/unreachable targets (no queue), so a
  // fixed offset (e.g. {q, r-2}) can land on water/rock depending on where
  // this client happened to spawn. Discovering a walkable distance-2 hex from
  // the actual map keeps the test genuinely exercising a multi-turn walk
  // without guessing at map geometry.
  const map = await page.evaluate(() => fetch("/api/map").then((r) => r.json() as Promise<MapResponse>));
  const dest = pickDistance2Destination(map, start);
  expect(dest, "expected a walkable distance-2 hex from spawn on the static map").not.toBeNull();

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

  // Arrival clears the pending destination.
  await expect.poll(() => page.evaluate(() => window.game.destination)).toBeNull();

  // It actually moved (guards against dest == start).
  const end = await page.evaluate(() => window.game.me!.hex);
  expect(end).not.toEqual(start);
});

test("the turn timer bar exists and animates across a turn", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator("#turn-timer")).toBeVisible();

  await expect.poll(() => page.evaluate(() => window.game.intervalMs)).toBeGreaterThan(0);

  // The phase cycles through playback and input over successive turns.
  const sawPlayback = await page.evaluate(async () => {
    for (let i = 0; i < 200; i++) {
      if (window.game.phase === "playback") {
        return true;
      }
      await new Promise((res) => setTimeout(res, 10));
    }

    return false;
  });
  expect(sawPlayback).toBe(true);
});
