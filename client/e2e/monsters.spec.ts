import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";
import { CombatRadius } from "../src/protocol.gen";

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
// tooltip near the cursor with its kind display name + "HP cur/max". Item 6
// (playtest feedback batch 3): the HP line is now gated by distance — only
// shown within CombatRadius of my own entity, name-only beyond it (scouting
// shouldn't read exact health through the fog of distance). The e2e server's
// monsters spawn randomly (SanctuaryRadius keeps them away from the origin,
// where I spawn), so which side of the gate a given run lands on isn't
// fixed — this test computes the real hex distance itself and asserts
// whichever outcome that implies, exercising the actual gating logic either
// way instead of assuming one side.
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
test("hovering a monster shows its kind, and its HP only within CombatRadius", async ({ page }) => {
  await page.goto("/");

  await expect
    .poll(() => page.evaluate(() => window.game.monsters), { timeout: 10_000 })
    .toBeGreaterThanOrEqual(1);

  const hover = await page.evaluate(() => {
    const HEX_SIZE = 32; // keep in sync with render/hex.ts
    const hexToPixel = (hex: { q: number; r: number }): { x: number; y: number } => ({
      x: HEX_SIZE * 1.5 * hex.q,
      y: HEX_SIZE * ((Math.sqrt(3) / 2) * hex.q + Math.sqrt(3) * hex.r),
    });
    // Axial hex distance — same formula as render/hex.ts's hexDistance.
    const hexDistance = (a: { q: number; r: number }, b: { q: number; r: number }): number =>
      (Math.abs(a.q - b.q) + Math.abs(a.q + a.r - b.q - b.r) + Math.abs(a.r - b.r)) / 2;

    const monster = window.game.positions.find((p) => p.kind === "monster");
    const me = window.game.me;
    if (monster === undefined || me === null) {
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
      distance: hexDistance(me.hex, monster.hex),
      hidden: tooltip.hidden,
      kindText: tooltip.querySelector(".tooltip-kind")?.textContent ?? "",
      hpHidden: (tooltip.querySelector(".tooltip-hp") as HTMLElement | null)?.hidden ?? true,
      hpText: tooltip.querySelector(".tooltip-hp")?.textContent ?? "",
    };
  });

  expect(hover).not.toBeNull();
  expect(hover?.hidden).toBe(false);
  expect(hover?.kindText).toBe(hover?.name);

  if ((hover?.distance ?? Infinity) <= CombatRadius) {
    expect(hover?.hpHidden).toBe(false);
    expect(hover?.hpText).toBe(`HP ${hover?.hp}/${hover?.maxHp}`);
  } else {
    expect(hover?.hpHidden).toBe(true);
    expect(hover?.hpText).toBe("");
  }

  // Hovering somewhere with no entity hides it again.
  const hiddenAfter = await page.evaluate(() => {
    const canvas = document.querySelector("canvas")!;
    canvas.dispatchEvent(new PointerEvent("pointermove", { clientX: -9999, clientY: -9999, bubbles: true }));

    return document.getElementById("hover-tooltip")!.hidden;
  });
  expect(hiddenAfter).toBe(true);
});

// #205: the tooltip content was recomputed only on pointermove, so a monster
// that moved off (or died on) the hovered hex under a STATIONARY cursor left a
// stale tooltip lingering until the next mouse move. The fix re-resolves the
// hovered hex on every turn bundle (onTurn), hiding the tooltip when the
// monster has left. This test hovers a monster, then advances turns WITHOUT
// touching the cursor — the e2e monsters wander every turn — and asserts the
// tooltip clears itself the moment the hovered hex is empty.
//
// The poll's function returns "cleared" only when, in ONE atomic page
// snapshot, the hovered hex holds no monster AND the tooltip is hidden — the
// two are updated together inside onTurn (positions first, then the tooltip),
// so a consistent snapshot can never show an empty hex with a visible tooltip
// once the fix is in. With the bug it stays "stale" until timeout. The initial
// pointermove is the ONLY synthetic pointer event; nothing moves the cursor
// after, so the clearing is driven purely by the turn clock.
test("tooltip clears itself when the hovered monster wanders off under a still cursor", async ({
  page,
}) => {
  await page.goto("/");

  await expect
    .poll(() => page.evaluate(() => window.game.monsters), { timeout: 10_000 })
    .toBeGreaterThanOrEqual(1);

  // Hover a monster and remember its hex. Returns the hovered hex so the poll
  // below can watch that exact hex empty out.
  const hoveredHex = await page.evaluate(() => {
    const HEX_SIZE = 32; // keep in sync with render/hex.ts
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

    return monster.hex;
  });
  expect(hoveredHex).not.toBeNull();

  // The tooltip is up right after the hover.
  expect(await page.evaluate(() => document.getElementById("hover-tooltip")!.hidden)).toBe(false);

  // Advance turns without moving the cursor. Once no monster occupies the
  // hovered hex, the tooltip must already be hidden (cleared by onTurn).
  await expect
    .poll(
      () =>
        page.evaluate((hex) => {
          const occupied = window.game.positions.some(
            (p) => p.kind === "monster" && p.hex.q === hex!.q && p.hex.r === hex!.r,
          );
          const hidden = document.getElementById("hover-tooltip")!.hidden;
          if (occupied) {
            return "occupied"; // monster still there — keep waiting for it to wander
          }
          return hidden ? "cleared" : "stale";
        }, hoveredHex),
      { timeout: 15_000 },
    )
    .toBe("cleared");
});
