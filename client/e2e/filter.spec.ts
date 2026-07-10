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

// pickAdjacentDestination finds a walkable neighbor of start — mirrors the
// map-driven discovery in walk.spec.ts (never assumes a fixed offset is
// walkable on the procedurally-generated map).
function pickAdjacentDestination(map: MapResponse, start: Hex): Hex | null {
  const walkable = new Set<string>();
  for (const tile of map.tiles) {
    if (tile.terrain === "grass" || tile.terrain === "forest") {
      walkable.add(`${tile.hex.q},${tile.hex.r}`);
    }
  }

  for (const n of axialNeighbors(start)) {
    if (walkable.has(`${n.q},${n.r}`)) {
      return n;
    }
  }

  return null;
}

test("the CRT filter is on by default and the HUD reflects it", async ({ page }) => {
  await page.goto("/");

  await expect.poll(() => page.evaluate(() => window.game.filter)).toBe("crt");
  await expect(page.locator("#filter-toggle")).toContainText("crt");
});

test("toggling the filter persists across a reload", async ({ page }) => {
  await page.goto("/");

  await expect.poll(() => page.evaluate(() => window.game.filter)).toBe("crt");

  await page.locator("#filter-toggle").click();
  await expect.poll(() => page.evaluate(() => window.game.filter)).toBe("none");
  await expect(page.locator("#filter-toggle")).toContainText("none");

  // localStorage survives the reload (same browser context — the
  // move.spec.ts identity-survives-reload pattern), proving the choice is
  // actually persisted, not just held in the running page's memory.
  await page.reload();
  await expect
    .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
    .toBe(true);
  await expect.poll(() => page.evaluate(() => window.game.filter)).toBe("none");
  await expect(page.locator("#filter-toggle")).toContainText("none");
});

test("re-enabling the filter still lets a walk go through", async ({ page }) => {
  await page.goto("/");

  await expect
    .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
    .toBe(true);

  // Turn it off, then back on via the debug setter (mirrors the HUD toggle).
  await page.evaluate(() => window.game.setFilter("none"));
  await expect.poll(() => page.evaluate(() => window.game.filter)).toBe("none");

  await page.evaluate(() => window.game.setFilter("crt"));
  await expect.poll(() => page.evaluate(() => window.game.filter)).toBe("crt");
  await expect(page.locator("#filter-toggle")).toContainText("crt");

  // One real walk step with the filter active — proves input/render still
  // work with the post-processing pass applied to the stage.
  const start = await page.evaluate(() => window.game.me!.hex);
  const map = await page.evaluate(() => fetch("/api/map").then((r) => r.json() as Promise<MapResponse>));
  const dest = pickAdjacentDestination(map, start);
  expect(dest, "expected a walkable neighbor of spawn on the static map").not.toBeNull();

  await page.evaluate((d) => window.game.tapHex(d!.q, d!.r), dest);

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
});
