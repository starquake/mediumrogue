import { expect, test } from "@playwright/test";

import { displayLabel } from "../src/identity/labels";
import { XPCurveBase } from "../src/protocol.gen";

import { E2E_WORLD_RADIUS, continueIfReturning } from "./helpers";

/**
 * hexToRgb converts a CSS hex colour to the `rgb(r, g, b)` form getComputedStyle
 * returns, so a palette variable can be compared against a resolved colour.
 */
function hexToRgb(hex: string): string {
  const h = hex.replace("#", "");
  const n = Number.parseInt(h, 16);

  return `rgb(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255})`;
}

test("client connects and the turn counter advances live", async ({ page }) => {
  await page.goto("/");
  await continueIfReturning(page);

  // The SSE stream must connect and report itself in the UI.
  await expect(page.locator("#status")).toHaveAttribute("data-connected", "true");

  // #433: a healthy connection reads GREEN, never the accent crimson. The HUD
  // used to show a red dot beside the word "connected" while a dropped stream
  // was the dimmest thing on screen. Compared against the --ok and --accent
  // tokens rather than literal hexes, so retuning the palette does not fail the
  // build but re-introducing the alarm does.
  const dot = await page.evaluate(() => {
    const el = document.querySelector("#status");

    return el === null ? "" : getComputedStyle(el, "::before").color;
  });
  const token = (name: string): Promise<string> =>
    page.evaluate(
      (n) => getComputedStyle(document.documentElement).getPropertyValue(n).trim(),
      name,
    );
  expect(dot).toBe(hexToRgb(await token("--ok")));
  expect(dot).not.toBe(hexToRgb(await token("--accent")));

  // The turn counter must advance — proving clock → hub → SSE → EventSource
  // → DOM. TURN_INTERVAL is 250ms in the e2e server.
  const first = await page.evaluate(() => window.game.turn);
  await expect
    .poll(() => page.evaluate(() => window.game.turn), { timeout: 10_000 })
    .toBeGreaterThan(first);

  // The HUD paints the clock. There is no turn NUMBER any more (#314) — the
  // progress bar IS the readout — so what is asserted is the surface that
  // remains: the bar is on screen and reports a real phase, which only the
  // per-turn wiring can set.
  await expect(page.locator("#turn-timer")).toBeVisible();
  await expect
    .poll(() => page.locator("#turn-timer").getAttribute("data-phase"))
    .toMatch(/^(input|playback)$/);
});

test("a fresh player starts at level 1 with 0 XP, exposed on window.game and the stats HUD", async ({
  page,
}) => {
  await page.goto("/");
  await continueIfReturning(page);

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

  // The stats HUD paints what window.game reports, including the live
  // position readout (item 9, playtest batch 2) — the spawn hex varies per
  // run, so read it from window.game.me rather than hardcoding one. Level 1's
  // XP-to-next is XPCurveBase*1^2 == XPCurveBase (same number as the old flat
  // curve at level 1, but now a curve rather than a coincidence).
  const hex = await page.evaluate(() => window.game.me?.hex ?? null);
  expect(hex).not.toBeNull();
  const meId = await page.evaluate(() => window.game.me?.id ?? -1);
  const hp = await page.evaluate((id) => window.game.hp[id] ?? 0, meId);
  const maxHp = await page.evaluate((id) => window.game.maxHp[id] ?? 0, meId);
  // #201: the line carries the player's own HP between level and XP.
  // #428: and opens with class and species, which were previously visible
  // nowhere once you were in the world.
  const cls = await page.evaluate(() => window.game.class);
  const species = await page.evaluate(() => window.game.species);
  expect(cls).not.toBe("");
  expect(species).not.toBe("");
  await expect(page.locator("#stats")).toHaveText(
    `${displayLabel(cls)} · ${displayLabel(species)} · Lv 1 · ${hp}/${maxHp} HP · 0/${XPCurveBase} XP · (${hex?.q}, ${hex?.r})`,
  );
});

test("the hex world renders from server map data", async ({ page }) => {
  await page.goto("/");
  await continueIfReturning(page);

  // The WebGL canvas is on the page.
  await expect(page.locator("canvas")).toBeVisible();

  // The map arrived and every tile of the generated hexagon is on stage:
  // 3·r·(r+1)+1. Derived from E2E_WORLD_RADIUS rather than hardcoded — the
  // subject here is "the whole map reached the client", not one particular
  // world size, and the size moved once already (#289).
  await expect
    .poll(() => page.evaluate(() => window.game.tiles), { timeout: 10_000 })
    .toBe(3 * E2E_WORLD_RADIUS * (E2E_WORLD_RADIUS + 1) + 1);

  // Visual smoke check: the stage actually painted terrain, not a black
  // void — sample the screenshot for non-background pixels.
  const screenshot = await page.screenshot();
  expect(screenshot.byteLength).toBeGreaterThan(10_000);
});

// #201: the HUD stats line shows the player's own HP number (it previously
// showed only level/XP/coords, while any monster's HP was hover-readable).
test("the HUD stats line shows the player's HP", async ({ page }) => {
  await page.goto("/");
  await continueIfReturning(page);
  await expect(page.locator("#status")).toHaveAttribute("data-connected", "true");

  await expect
    .poll(() => page.evaluate(() => document.getElementById("stats")?.textContent ?? ""))
    .toMatch(/\d+\/\d+ HP/);
});
