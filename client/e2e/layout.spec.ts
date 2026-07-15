import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";
import { EntityMonster } from "../src/protocol.gen";
import type { Hex } from "../src/protocol.gen";

declare global {
  interface Window {
    game: GameDebug;
  }
}

// #105: 1920×1080 is the minimum supported viewport. The worst-case left
// column — full HUD (title/turn/status/stats/copy-link/inventory button),
// the combat panel swapped in for the timer, the character panel open, chat
// populated — must show every element without overlap or anything falling
// off-screen. The regression this pins: #character-root used to sit at a
// hardcoded top offset, so the grown in-combat HUD ran underneath the open
// panel; it now anchors to the HUD's measured bottom (--hud-bottom).
test.use({ viewport: { width: 1920, height: 1080 } });

type Box = { x: number; y: number; width: number; height: number };

// Overlap with a 2px tolerance: adjacent borders may touch, real overlap
// (content under content) may not.
const intersects = (a: Box, b: Box): boolean =>
  Math.min(a.x + a.width, b.x + b.width) - Math.max(a.x, b.x) > 2 &&
  Math.min(a.y + a.height, b.y + b.height) - Math.max(a.y, b.y) > 2;

const within = (b: Box, w: number, h: number): boolean =>
  b.x >= 0 && b.y >= 0 && b.x + b.width <= w && b.y + b.height <= h;

test("worst-case HUD + open panels fit 1920×1080 without overlap", async ({ page }) => {
  await page.goto("/");

  await expect.poll(() => page.evaluate(() => window.game.me !== null && window.game.connected)).toBe(true);
  await expect.poll(() => page.evaluate(() => window.game.turn >= 1)).toBe(true);

  // Open the character panel and put real lines in chat.
  await page.keyboard.press("c");
  await expect.poll(() => page.evaluate(() => window.game.panelOpen)).toBe(true);
  await page.fill("#chat-input", "layout guard line");
  await page.keyboard.press("Enter");

  // Enter combat so the HUD is at its tallest (combat panel + copy-link).
  const chase = (): Promise<void> =>
    page.evaluate((monsterKind) => {
      const me = window.game.me;
      if (me === null || window.game.inCombat) {
        return;
      }
      const monsters = window.game.positions.filter((p) => p.kind === monsterKind);
      if (monsters.length === 0) {
        return;
      }
      const dist = (a: Hex, b: Hex): number => {
        const dq = a.q - b.q;
        const dr = a.r - b.r;
        const ds = -dq - dr;

        return (Math.abs(dq) + Math.abs(dr) + Math.abs(ds)) / 2;
      };
      let nearest = monsters[0]!;
      let best = dist(me.hex, nearest.hex);
      for (const m of monsters.slice(1)) {
        const d = dist(me.hex, m.hex);
        if (d < best) {
          nearest = m;
          best = d;
        }
      }
      window.game.tapHex(nearest.hex.q, nearest.hex.r);
    }, EntityMonster);

  await expect
    .poll(
      async () => {
        await chase();

        return page.evaluate(() => window.game.inCombat);
      },
      { timeout: 20_000, intervals: [250] },
    )
    .toBe(true);

  // The chase may have closed the panel? It cannot (only c/i/Esc do) — but
  // assert the full worst-case state explicitly before measuring.
  await expect(page.locator("#combat-panel")).toBeVisible();
  await expect(page.locator("#character-panel")).toBeVisible();
  await expect(page.locator("#chat-panel")).toBeVisible();

  const box = async (sel: string): Promise<Box> => {
    const b = await page.locator(sel).boundingBox();
    expect(b, `${sel} has no bounding box`).not.toBeNull();

    return b!;
  };

  // The HUD's ResizeObserver updates --hud-bottom a frame after the combat
  // panel appears — poll until the panel has settled below the HUD rather
  // than sleeping.
  await expect
    .poll(async () => intersects(await box("#hud"), await box("#character-panel")), { intervals: [100] })
    .toBe(false);

  const panels = ["#hud", "#combat-panel", "#toggle-inventory", "#character-panel", "#chat-panel"];
  const boxes = new Map<string, Box>();
  for (const sel of panels) {
    boxes.set(sel, await box(sel));
  }

  for (const [sel, b] of boxes) {
    expect(within(b, 1920, 1080), `${sel} extends off-screen: ${JSON.stringify(b)}`).toBe(true);
  }

  // The character panel may cover the quest board (by design: an inventory
  // screen, not a peek) but nothing else on the left column.
  for (const sel of ["#hud", "#combat-panel", "#toggle-inventory", "#chat-panel"]) {
    expect(
      intersects(boxes.get(sel)!, boxes.get("#character-panel")!),
      `${sel} overlaps #character-panel: ${JSON.stringify(boxes.get(sel))} vs ${JSON.stringify(boxes.get("#character-panel"))}`,
    ).toBe(false);
  }
});
