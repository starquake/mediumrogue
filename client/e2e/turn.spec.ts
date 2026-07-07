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
  // → DOM end to end. TURN_INTERVAL is 250ms in the e2e server.
  const first = await page.evaluate(() => window.game.turn);
  await expect
    .poll(() => page.evaluate(() => window.game.turn), { timeout: 10_000 })
    .toBeGreaterThan(first);

  // The HUD paints what window.game reports.
  const shown = await page.locator("#turn").textContent();
  expect(Number(shown)).toBeGreaterThanOrEqual(first);
});
