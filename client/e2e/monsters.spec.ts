import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";

declare global {
  interface Window {
    game: GameDebug;
  }
}

test("monsters spawned server-side reach the client and render", async ({ page }) => {
  await page.goto("/");

  // The e2e server is started with MONSTER_COUNT=3: the turn bundle must
  // carry at least one monster entity through to window.game.
  await expect
    .poll(() => page.evaluate(() => window.game.monsters), { timeout: 10_000 })
    .toBeGreaterThanOrEqual(1);

  // Visual smoke check: the stage actually painted something (the hostile-
  // coloured monster dots among the terrain), not a black void.
  const screenshot = await page.screenshot();
  expect(screenshot.byteLength).toBeGreaterThan(10_000);
});

// Item 13, playtest batch 2: hovering a monster's hex shows a small DOM
// tooltip near the cursor with its kind display name + "HP cur/max".
//
// Dispatches a synthetic "pointermove" directly on the canvas (rather than
// driving a real OS-level page.mouse.move) with clientX/clientY computed
// from the SAME hexToPixel formula main.ts's own handler uses — entirely
// inside one page.evaluate call, so reading the monster's current hex and
// dispatching the event happen atomically on the page's single JS thread,
// with no round trip for the AI to move it in between (thinkMonstersLocked
// wanders every turn, ~250ms here). It also sidesteps needing the monster
// to be within the actual visible viewport — a real mouse move can't reach
// off-screen coordinates, but a synthetic event can carry any clientX/Y and
// main.ts's listener does the same math regardless.
test("hovering a monster shows its kind and HP in a tooltip", async ({ page }) => {
  await page.goto("/");

  await expect
    .poll(() => page.evaluate(() => window.game.monsters), { timeout: 10_000 })
    .toBeGreaterThanOrEqual(1);

  const hover = await page.evaluate(() => {
    const HEX_SIZE = 22;
    const hexToPixel = (hex: { q: number; r: number }): { x: number; y: number } => ({
      x: HEX_SIZE * 1.5 * hex.q,
      y: HEX_SIZE * ((Math.sqrt(3) / 2) * hex.q + Math.sqrt(3) * hex.r),
    });

    const monster = window.game.positions.find((p) => p.kind === "monster");
    if (monster === undefined) {
      return null;
    }

    const canvas = document.querySelector("canvas")!;
    const rect = canvas.getBoundingClientRect();
    const { x, y } = hexToPixel(monster.hex);
    const clientX = rect.left + window.game.camera.x + x;
    const clientY = rect.top + window.game.camera.y + y;
    canvas.dispatchEvent(new PointerEvent("pointermove", { clientX, clientY, bubbles: true }));

    const tooltip = document.getElementById("hover-tooltip")!;

    return {
      name: monster.name,
      hp: window.game.hp[monster.id],
      maxHp: window.game.maxHp[monster.id],
      hidden: tooltip.hidden,
      kindText: tooltip.querySelector(".tooltip-kind")?.textContent ?? "",
      hpText: tooltip.querySelector(".tooltip-hp")?.textContent ?? "",
    };
  });

  expect(hover).not.toBeNull();
  expect(hover?.hidden).toBe(false);
  expect(hover?.kindText).toBe(hover?.name);
  expect(hover?.hpText).toBe(`HP ${hover?.hp}/${hover?.maxHp}`);

  // Hovering somewhere with no entity hides it again.
  const hiddenAfter = await page.evaluate(() => {
    const canvas = document.querySelector("canvas")!;
    canvas.dispatchEvent(new PointerEvent("pointermove", { clientX: -9999, clientY: -9999, bubbles: true }));

    return document.getElementById("hover-tooltip")!.hidden;
  });
  expect(hiddenAfter).toBe(true);
});
