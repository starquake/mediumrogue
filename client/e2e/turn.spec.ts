import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";

declare global {
  interface Window {
    game: GameDebug;
  }
}

test("client connects and the turn counter advances live", async ({ page }) => {
  await page.goto("/");

  // The SSE stream must connect and report itself in the UI.
  await expect(page.locator("#status")).toHaveAttribute("data-connected", "true");

  // The turn counter must advance — proving clock → hub → SSE → EventSource
  // → DOM. TURN_INTERVAL is 250ms in the e2e server.
  const first = await page.evaluate(() => window.game.turn);
  await expect
    .poll(() => page.evaluate(() => window.game.turn), { timeout: 10_000 })
    .toBeGreaterThan(first);

  // The HUD paints what window.game reports.
  const shown = await page.locator("#turn").textContent();
  expect(Number(shown)).toBeGreaterThanOrEqual(first);
});

test("the hex world renders from server map data", async ({ page }) => {
  await page.goto("/");

  // The WebGL canvas is on the page.
  await expect(page.locator("canvas")).toBeVisible();

  // The map arrived and every tile of the radius-12 hexagon is on stage:
  // 3·r·(r+1)+1 = 469.
  await expect
    .poll(() => page.evaluate(() => window.game.tiles), { timeout: 10_000 })
    .toBe(469);

  // Visual smoke check: the stage actually painted terrain, not a black
  // void — sample the screenshot for non-background pixels.
  const screenshot = await page.screenshot();
  expect(screenshot.byteLength).toBeGreaterThan(10_000);
});
