import { expect, test } from "@playwright/test";

import { EntityMonster } from "../src/protocol.gen";
import type { Hex } from "../src/protocol.gen";
import { gotoReady } from "./helpers";

// #105: 1920×1080 is the minimum supported viewport. The worst-case left
// column — full HUD (title/turn/status/stats/copy-link/inventory button),
// the combat panel swapped in for the timer, the character panel open, chat
// populated — must show every element without overlap or anything falling
// off-screen. The regression this pins: #character-root used to sit at a
// hardcoded top offset, so the grown in-combat HUD ran underneath the open
// panel; it now anchors to the HUD's measured bottom (--hud-bottom).
test.use({ viewport: { width: 1920, height: 1080 } });

test("worst-case HUD + open panels fit 1920×1080 without overlap", async ({ page }) => {
  await gotoReady(page);
  await expect.poll(() => page.evaluate(() => window.game.turn >= 1)).toBe(true);

  // Open the character panel and put real lines in chat. (#273: `i` is the
  // panel key — `c` is also a panel alias (#274 follow camera has no recenter).)
  await page.keyboard.press("i");
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

  // One atomic in-page measurement, polled until clean. A single evaluate
  // (not per-element locator roundtrips — those can take whole seconds each
  // on a starved CI runner) reads every box in the same frame and NAMES any
  // violation, so a timeout failure says what overlapped. The poll also
  // re-establishes the worst-case state when it decays mid-run — both decays
  // are real on slow CI: a reload gap (the #86 hazard class) comes back with
  // the default-closed panel, and the player can die out of the bubble while
  // measuring. The HUD's ResizeObserver updates --hud-bottom a frame after
  // the combat panel swaps in; polling (never sleeping) absorbs that too.
  const measure = (): Promise<{ panelOpen: boolean; inCombat: boolean; violations: string[] | null }> =>
    page.evaluate(() => {
      const rect = (sel: string): DOMRect | null => {
        const el = document.querySelector<HTMLElement>(sel);
        if (el === null || el.hidden) {
          return null;
        }
        const r = el.getBoundingClientRect();

        return r.width > 0 && r.height > 0 ? r : null;
      };
      const sels = ["#hud", "#combat-panel", "#toggle-inventory", "#character-panel", "#chat-panel"];
      const boxes = new Map<string, DOMRect | null>(sels.map((s) => [s, rect(s)]));
      if ([...boxes.values()].some((b) => b === null)) {
        // A worst-case element is missing/hidden: state decayed (or the RO
        // frame hasn't landed) — the driver re-establishes and re-polls.
        return { panelOpen: window.game.panelOpen, inCombat: window.game.inCombat, violations: null };
      }
      const overlap = (a: DOMRect, b: DOMRect): boolean =>
        Math.min(a.right, b.right) - Math.max(a.x, b.x) > 2 && Math.min(a.bottom, b.bottom) - Math.max(a.y, b.y) > 2;
      const violations: string[] = [];
      for (const [sel, b] of boxes) {
        if (b!.x < 0 || b!.y < 0 || b!.right > window.innerWidth || b!.bottom > window.innerHeight) {
          violations.push(`${sel} off-screen (${Math.round(b!.x)},${Math.round(b!.y)}→${Math.round(b!.right)},${Math.round(b!.bottom)})`);
        }
      }
      // The character panel may cover the quest board (by design: an
      // inventory screen, not a peek) but nothing else on the left column.
      const panel = boxes.get("#character-panel")!;
      for (const sel of ["#hud", "#combat-panel", "#toggle-inventory", "#chat-panel"]) {
        const b = boxes.get(sel)!;
        if (overlap(b, panel)) {
          violations.push(`${sel} overlaps #character-panel (bottom ${Math.round(b.bottom)} vs panel top ${Math.round(panel.y)})`);
        }
      }

      return { panelOpen: window.game.panelOpen, inCombat: window.game.inCombat, violations };
    });

  await expect
    .poll(
      async () => {
        const s = await measure();
        if (!s.panelOpen) {
          await page.keyboard.press("i");

          return "panel closed (reload gap?) — reopened, re-polling";
        }
        if (!s.inCombat) {
          await chase();

          return "not in combat (died?) — re-chasing, re-polling";
        }
        if (s.violations === null) {
          return "a worst-case element has no box yet";
        }

        return s.violations.length === 0 ? "clean" : s.violations.join("; ");
      },
      { timeout: 30_000, intervals: [250] },
    )
    .toBe("clean");
});
