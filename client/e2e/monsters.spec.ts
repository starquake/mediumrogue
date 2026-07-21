import { expect, test } from "@playwright/test";

import { CombatRadius, MonsterAggroRadius } from "../src/protocol.gen";
import { gotoReady, progressTracker } from "./helpers";

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
    // #273/#274: window.game.camera is world.position (the follow camera's live
    // screen offset, centred on the player); the world point is then scaled by
    // zoom. Read fresh each time — matching main.ts's own hexToScreen inverse.
    // Identity translate at the default zoom=1.
    const clientX = rect.left + window.game.camera.x + x * window.game.zoom;
    const clientY = rect.top + window.game.camera.y + y * window.game.zoom;
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
    // #249: a ranged monster (e.g. Kin Archer) appends the #201 reach suffix
    // ("HP 12/12 · reach 3"), so allow the optional "· reach N" tail rather
    // than pinning the exact string — the kind that spawns first is random.
    expect(hover?.hpText).toMatch(new RegExp(`^HP ${hover?.hp}/${hover?.maxHp}(?: · reach \\d+)?$`));
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
  // Metered on GAME progress (turn advances), never wall-clock: approaching a
  // reachable monster into aggro range legitimately needs many turns, and on a
  // contended CI runner those turns stretch past the default 30s test budget
  // while nothing is wrong (same turn-metered reasoning as autowalk.spec.ts).
  test.slow();

  await gotoReady(page);
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

  // #181/#247: the NEAREST monster can be permanently unreachable (spawn
  // placement checks walkability, not connectivity — a terrain pocket parks
  // it, its Pathfind returns nil), so the old nearest-only greedy walk could
  // pin on one forever and dislodge nothing ("occupied" for all 120 turns —
  // the #247 CI failure). This loop, all metered on turn advances, drives:
  //   - progress-aware target rotation (progressTracker, the shared #181
  //     helper): rotate to the next-nearest monster when the gap to the
  //     current one stops closing (unreachable, or a leash-return treadmill);
  //   - continuous movement toward that target — out of combat the server
  //     pathfinds; INSIDE a bubble we keep stepping via combatMoves, which
  //     locks in every turn so the bubble RESOLVES rather than freezing (the
  //     old loop stopped acting on inCombat, so a bubble — and the monster on
  //     the hovered hex — froze solid on this long-patience server);
  //   - a single hover (the ONLY synthetic pointer event) once we are within
  //     aggro range of the pursued monster.
  // Keeping that monster aggroed and pathing is what makes it step off (or die
  // on) the hovered hex — which is what clears the tooltip.
  const tracker = progressTracker(12);
  let skip = 0;
  let hoveredHex: { q: number; r: number } | null = null;
  let state: "occupied" | "cleared" | "stale" = "occupied";

  const classify = (hex: { q: number; r: number }): Promise<"occupied" | "cleared" | "stale"> =>
    page.evaluate((h) => {
      const occupied = window.game.positions.some((p) => p.kind === "monster" && p.hex.q === h.q && p.hex.r === h.r);
      const hidden = document.getElementById("hover-tooltip")!.hidden;
      if (occupied) {
        return "occupied"; // monster still on the hovered hex
      }
      return hidden ? "cleared" : "stale";
    }, hex);

  const maxTurns = 100;
  for (let i = 0; i < maxTurns && state !== "cleared"; i++) {
    const st: { targetDist: number | null; hovered: { q: number; r: number } | null } = await page.evaluate(
      ({ aggro, skip: skipN, haveHover }) => {
        const HEX_SIZE = 32; // keep in sync with render/hex.ts
        const me = window.game.me;
        if (me === null) {
          return { targetDist: null, hovered: null };
        }
        const d = (a: { q: number; r: number }, b: { q: number; r: number }): number =>
          (Math.abs(a.q - b.q) + Math.abs(a.q + a.r - b.q - b.r) + Math.abs(a.r - b.r)) / 2;
        const monsters = window.game.positions.filter((p) => p.kind === "monster");
        if (monsters.length === 0) {
          return { targetDist: null, hovered: null };
        }
        const sorted = monsters.slice().sort((a, b) => d(me.hex, a.hex) - d(me.hex, b.hex) || a.id - b.id);
        const target = sorted[skipN % sorted.length]!;
        const dist = d(me.hex, target.hex);

        // Drive toward the target every turn so it stays aggroed and pathing.
        if (window.game.inCombat) {
          // Step via this bubble turn's reachable tiles: submitting a move locks
          // in, so the bubble resolves (never freezes) and the monster keeps
          // acting.
          const moves = window.game.combatMoves;
          if (moves.length > 0) {
            let step = moves[0]!;
            for (const h of moves.slice(1)) {
              if (d(h, target.hex) < d(step, target.hex)) {
                step = h;
              }
            }
            void window.game.tapHex(step.q, step.r);
          }
        } else if (window.game.destination === null) {
          void window.game.tapHex(target.hex.q, target.hex.r);
        }

        // Hover the pursued monster the moment it is within aggro range and we
        // have not locked in a hover yet. #260: dispatch the hover at its LIVE
        // hex and confirm the tooltip actually came up — both in THIS atomic
        // evaluate — reporting `hovered` (the exact hex it showed on) only then.
        // The old code dispatched here but asserted visibility in a SEPARATE
        // later evaluate; if the monster stepped off the hovered pixel (it
        // wanders every turn) or a turn bundle re-resolved and hid the tooltip
        // in between, that assert flaked (Received hidden === true). If the
        // tooltip is not up this turn, `hovered` stays null and the next turn
        // re-dispatches at the monster's new hex — the loop keeps the cursor on
        // the still-aggroed monster until the tooltip is confirmed visible.
        let hovered: { q: number; r: number } | null = null;
        if (!haveHover && dist <= aggro) {
          const canvas = document.querySelector("canvas")!;
          const rect = canvas.getBoundingClientRect();
          const x = HEX_SIZE * 1.5 * target.hex.q;
          const y = HEX_SIZE * ((Math.sqrt(3) / 2) * target.hex.q + Math.sqrt(3) * target.hex.r);
          // #273/#274: scale the world point by zoom (window.game.camera is the
          // follow camera's live screen offset, centred on the player).
          canvas.dispatchEvent(
            new PointerEvent("pointermove", {
              clientX: rect.left + window.game.camera.x + x * window.game.zoom,
              clientY: rect.top + window.game.camera.y + y * window.game.zoom,
              bubbles: true,
            }),
          );
          if (!document.getElementById("hover-tooltip")!.hidden) {
            hovered = target.hex;
          }
        }

        return { targetDist: dist, hovered };
      },
      { aggro: MonsterAggroRadius, skip, haveHover: hoveredHex !== null },
    );

    if (st.hovered !== null && hoveredHex === null) {
      // st.hovered is set only once the atomic hover above confirmed the
      // tooltip visible on this exact hex, so pinning hoveredHex here needs no
      // separate (racy) visibility assert — the tooltip WAS up on it.
      hoveredHex = st.hovered;
    }
    skip = tracker.note(st.targetDist);

    // Movement only happens on turn resolutions: advance one, then re-check.
    const turnBefore = await page.evaluate(() => window.game.turn);
    await expect
      .poll(() => page.evaluate(() => window.game.turn), { timeout: 15_000 })
      .toBeGreaterThan(turnBefore);

    if (hoveredHex !== null) {
      state = await classify(hoveredHex);
    }
  }

  // Unmet preconditions (not the #205 bug), skipped like autowalk.spec's guards:
  test.skip(hoveredHex === null, "never got within aggro range of a reachable monster — nothing to hover");
  test.skip(state === "occupied", "the pursued monster never vacated the hovered hex within the turn budget");

  // The hovered monster left (or died on) its hex and the tooltip cleared
  // itself — no cursor move since the hover. "stale" would be the #205 bug
  // (empty hex, still-visible tooltip); "cleared" is the pass.
  expect(state).toBe("cleared");
});
