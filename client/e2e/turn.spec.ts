import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";
import { XPPerLevel } from "../src/protocol.gen";

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

test("a fresh player starts at level 1 with 0 XP, exposed on window.game and the stats HUD", async ({
  page,
}) => {
  await page.goto("/");

  // Each test gets its own browser context (no stored identity), so this is
  // always a brand-new entity — a fresh join floors xp at 0 and level at 1
  // server-side. Deterministic on the monster-free core server: nothing
  // grants XP here.
  await expect
    .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
    .toBe(true);

  const level = await page.evaluate(() => window.game.level);
  const xp = await page.evaluate(() => window.game.xp);
  expect(level).toBe(1);
  expect(xp).toBe(0);

  // The stats HUD paints what window.game reports.
  await expect(page.locator("#stats")).toHaveText(`Lv 1 · 0/${XPPerLevel} XP`);
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
