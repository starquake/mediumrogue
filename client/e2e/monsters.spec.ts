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
// monster has left. This test hovers a monster, then makes it VACATE that hex
// while the cursor stays put, and asserts the tooltip clears itself.
//
// A world-domain monster only moves once a player is inside its aggro radius
// (thinkMonstersLocked: `m.path = nil` — "stand still" — for a monster that
// notices nobody); MonsterAggroRadius (10) > CombatRadius (6). So the reliable
// way to make the hovered monster leave its hex is to walk MY player toward it
// (aggroing it), exactly like autowalk.spec drives movement — the monster then
// steps off its spawn hex to path toward me. The cursor is never touched after
// the initial hover, so the tooltip clearing is driven purely by onTurn.
//
// The classifier returns "cleared" only when, in ONE atomic page snapshot, the
// hovered hex holds no monster AND the tooltip is hidden — the two are updated
// together inside onTurn (positions first, then the tooltip), so a consistent
// snapshot can never show an empty hex with a visible tooltip once the fix is
// in. With the bug it stays "stale". Movement is metered on turn advancement,
// never wall-clock, so a slow/contended runner just needs more turns (#117).
test("tooltip clears itself when the hovered monster leaves its hex under a still cursor", async ({
  page,
}) => {
  await page.goto("/");

  await expect
    .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
    .toBe(true);
  await expect
    .poll(() => page.evaluate(() => window.game.monsters), { timeout: 10_000 })
    .toBeGreaterThanOrEqual(1);

  // A monster spawned within CombatRadius forms a bubble at once and both sides
  // freeze — there is then no clean "monster steps off its hex" to observe.
  // That is the rare too-close spawn; skip it (autowalk.spec skips the mirror
  // case) rather than assert on a precondition this run never established.
  test.skip(
    await page.evaluate(() => window.game.inCombat),
    "spawned already in a combat bubble — no free step to observe",
  );

  // Hover a monster and remember its hex, so the classifier can watch that
  // exact hex empty out. This dispatch is the ONLY synthetic pointer event.
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

  // In one atomic snapshot: is a monster still on the hovered hex, and is the
  // tooltip hidden? Driven purely by the turn clock — no cursor move.
  const classify = (): Promise<"occupied" | "cleared" | "stale"> =>
    page.evaluate((hex) => {
      const occupied = window.game.positions.some(
        (p) => p.kind === "monster" && p.hex.q === hex!.q && p.hex.r === hex!.r,
      );
      const hidden = document.getElementById("hover-tooltip")!.hidden;
      if (occupied) {
        return "occupied"; // monster hasn't left the hovered hex yet
      }
      return hidden ? "cleared" : "stale";
    }, hoveredHex);

  // Walk toward the nearest monster to aggro it (re-tap only when idle and out
  // of combat, mirroring autowalk.spec), one step per turn, until the hovered
  // hex is vacated. Metered on turn advancement, budgeted in TURNS.
  const maxTurns = 120;
  let state = await classify();
  for (let i = 0; i < maxTurns && state === "occupied"; i++) {
    await page.evaluate(() => {
      if (window.game.inCombat || window.game.destination !== null) {
        return; // let an in-flight walk / a bubble resolve
      }
      const me = window.game.me;
      const monsters = window.game.positions.filter((p) => p.kind === "monster");
      if (me === null || monsters.length === 0) {
        return;
      }
      const dist = (a: { q: number; r: number }, b: { q: number; r: number }): number =>
        (Math.abs(a.q - b.q) + Math.abs(a.q + a.r - b.q - b.r) + Math.abs(a.r - b.r)) / 2;
      let nearest = monsters[0]!;
      for (const m of monsters.slice(1)) {
        if (dist(me.hex, m.hex) < dist(me.hex, nearest.hex)) {
          nearest = m;
        }
      }
      void window.game.tapHex(nearest.hex.q, nearest.hex.r);
    });

    const turnBefore = await page.evaluate(() => window.game.turn);
    await expect
      .poll(() => page.evaluate(() => window.game.turn), { timeout: 15_000 })
      .toBeGreaterThan(turnBefore);

    state = await classify();
  }

  // The hovered monster left its hex and the tooltip cleared itself — no cursor
  // move since the initial hover. "stale" would be the #205 bug (empty hex,
  // visible tooltip); "occupied" means the aggro walk never dislodged it.
  expect(state).toBe("cleared");
});
