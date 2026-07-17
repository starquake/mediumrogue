import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";
import type { Hex, MapResponse } from "../src/protocol.gen";

declare global {
  interface Window {
    game: GameDebug;
  }
}

// The world hover highlight (#135): out of combat, hovering a hex a click would
// act on lights that one tile — "walk" for walkable ground, "wait" for my own
// hex — and nothing on rock/water. Asserted through window.game.hoverMoveTile
// (the highlight is a canvas draw); hoverTile plants it synchronously, so each
// case reads it back inside the same page.evaluate that triggers it — no bundle
// can slip in between. Runs on a MONSTER-FREE server (playwright.config.ts), so
// inCombat is always false and the routing is deterministic; the in-combat →
// null half lives in attack-highlight.spec.ts, which is reliably in combat.

// axialNeighbors mirrors internal/game.HexNeighbors (flat-top).
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

test("out of combat, hovering a walkable hex marks it walk, my own hex wait, and rock nothing", async ({ page }) => {
  await page.goto("/");

  await expect.poll(() => page.evaluate(() => window.game.me !== null && window.game.connected)).toBe(true);
  // Monster-free server → never in combat; assert it so the world-only guard is
  // being exercised on its false branch, not passing by accident.
  await expect.poll(() => page.evaluate(() => window.game.inCombat)).toBe(false);

  const start = await page.evaluate(() => window.game.me!.hex);

  const map = await page.evaluate(() => fetch("/api/map").then((r) => r.json() as Promise<MapResponse>));
  const walkable = new Set<string>();
  for (const tile of map.tiles) {
    if (tile.terrain === "grass" || tile.terrain === "forest") {
      walkable.add(`${tile.hex.q},${tile.hex.r}`);
    }
  }

  const walkHex = axialNeighbors(start).find((n) => walkable.has(`${n.q},${n.r}`));
  expect(walkHex, "expected a walkable neighbour of spawn").toBeTruthy();

  // A genuinely non-walkable ON-MAP tile (rock/water), so the "nothing" case is
  // real terrain, not merely off-map.
  const rockTile = map.tiles.find((t) => t.terrain !== "grass" && t.terrain !== "forest");
  expect(rockTile, "expected some non-walkable tile on the generated map").toBeTruthy();
  const rockHex = rockTile!.hex;

  // Walkable neighbour → a "walk" hover on exactly that hex.
  const walk = await page.evaluate((h) => {
    window.game.hoverTile(h.q, h.r);
    return window.game.hoverMoveTile;
  }, walkHex!);
  expect(walk).toEqual({ hex: { q: walkHex!.q, r: walkHex!.r }, kind: "walk" });

  // My own hex → a "wait" hover (clicking it is a wait/cancel).
  const wait = await page.evaluate((h) => {
    window.game.hoverTile(h.q, h.r);
    return window.game.hoverMoveTile;
  }, start);
  expect(wait).toEqual({ hex: { q: start.q, r: start.r }, kind: "wait" });

  // Rock/water → nothing lights.
  const rock = await page.evaluate((h) => {
    window.game.hoverTile(h.q, h.r);
    return window.game.hoverMoveTile;
  }, rockHex);
  expect(rock).toBeNull();
});
