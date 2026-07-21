import { expect, test } from "@playwright/test";

import type { MapResponse } from "../src/protocol.gen";
import { gotoReady, pickDistance2Destination } from "./helpers";

test("clicking (tapHex) a distant hex walks my entity there over turns", async ({ page }) => {
  await gotoReady(page);

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

  // The phase clock cycles into "playback" for the first slice of every turn.
  // Poll from the Node side (not an in-page setTimeout loop, which headless CI
  // throttles hard enough to blow the test timeout) and sample often enough to
  // catch the ~100ms playback window that recurs each 250ms turn.
  await expect
    .poll(() => page.evaluate(() => window.game.phase), { timeout: 10_000, intervals: [40] })
    .toBe("playback");
});
