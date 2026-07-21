import { expect, test } from "@playwright/test";

import type { MapResponse } from "../src/protocol.gen";
import { pickDistance2Destination } from "./helpers";

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
