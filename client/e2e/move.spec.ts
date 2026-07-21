import { expect, test } from "@playwright/test";

import type { Hex, MapResponse } from "../src/protocol.gen";

test("a one-hex tap move moves my entity on the next turn", async ({ page }) => {
  // #273 dropped the QWEASD movement keys — character movement is click/tap
  // only now (window.game.tapHex), so this exercises a single-step tap rather
  // than a key press. The multi-turn walk lives in walk.spec.ts.
  await page.goto("/");

  // Joined, connected, and standing somewhere.
  await expect
    .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
    .toBe(true);
  const start = await page.evaluate(() => window.game.me!.hex);

  // My entity is in the turn bundle.
  await expect.poll(() => page.evaluate(() => window.game.entities)).toBeGreaterThan(0);

  // Tap a walkable neighbour of spawn: mirror the old "one walkable direction"
  // pick from the real map so we never target water/rock (the server rejects
  // and nothing queues). The six axial neighbours, flat-top orientation.
  const map = await page.evaluate(() => fetch("/api/map").then((r) => r.json() as Promise<MapResponse>));
  const dest = await page.evaluate(
    ({ m, s }) => {
      const walkable = new Set<string>();
      for (const tile of m.tiles) {
        if (tile.terrain === "grass" || tile.terrain === "forest") {
          walkable.add(`${tile.hex.q},${tile.hex.r}`);
        }
      }
      const neighbors: Hex[] = [
        { q: s.q, r: s.r - 1 },
        { q: s.q + 1, r: s.r - 1 },
        { q: s.q + 1, r: s.r },
        { q: s.q, r: s.r + 1 },
        { q: s.q - 1, r: s.r + 1 },
        { q: s.q - 1, r: s.r },
      ];

      return neighbors.find((n) => walkable.has(`${n.q},${n.r}`)) ?? null;
    },
    { m: map, s: start },
  );
  expect(dest, "expected a walkable neighbour of spawn").not.toBeNull();

  await page.evaluate((d) => window.game.tapHex(d!.q, d!.r), dest);

  // The server-authoritative position reaches that hex on a subsequent turn
  // bundle. TURN_INTERVAL is 250ms in the e2e server.
  await expect
    .poll(
      () => page.evaluate((d) => { const h = window.game.me!.hex; return h.q === d!.q && h.r === d!.r; }, dest),
      { timeout: 10_000 },
    )
    .toBe(true);
});

test("identity survives a page reload", async ({ page }) => {
  await page.goto("/");
  await expect.poll(() => page.evaluate(() => window.game.me !== null)).toBe(true);
  const firstID = await page.evaluate(() => window.game.me?.id);

  await page.reload();
  await expect.poll(() => page.evaluate(() => window.game.me !== null)).toBe(true);
  const secondID = await page.evaluate(() => window.game.me?.id);

  expect(secondID).toBe(firstID);
});
