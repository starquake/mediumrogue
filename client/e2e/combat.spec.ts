import { expect, test } from "@playwright/test";

import { EntityMonster } from "../src/protocol.gen";
import type { Hex } from "../src/protocol.gen";
import { gotoReady } from "./helpers";

// This file runs against the COMBAT server (see playwright.config.ts —
// filenames matching /(monsters|combat)\.spec\.ts$/ route to the server
// started with MONSTER_COUNT=3).

test("melee-attacking a monster deals damage, observable via window.game.hp", async ({ page }) => {
  await gotoReady(page);
  await expect.poll(() => page.evaluate(() => window.game.monsters)).toBeGreaterThanOrEqual(1);

  // Snapshot each MONSTER's HP before engaging. Success is "some monster took
  // real damage" — HP only ever decreases from combat, never rises above its
  // starting value, so this can't be satisfied except by real damage happening
  // (not a tautology against a hardcoded max). Monster-scoped (not every
  // entity) so a sibling player's disconnect can never be mistaken for a hit.
  const baseline = await page.evaluate(
    (monsterKind) =>
      Object.fromEntries(
        window.game.positions
          .filter((p) => p.kind === monsterKind)
          .map((p) => [p.id, window.game.hp[p.id] ?? 0]),
      ),
    EntityMonster,
  );

  // Every poll: re-pick whichever monster is currently nearest my entity and
  // tapHex toward it. Monsters hunt the nearest player too (server-side,
  // recomputed every turn), so re-targeting each round — rather than a single
  // tapHex at a fixed destination — converges reliably even as both sides
  // move and spawn positions vary between runs.
  const chase = async (base: Record<number, number>): Promise<boolean> => {
    await page.evaluate((monsterKind) => {
      const me = window.game.me;
      if (me === null) {
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
      let bestDist = dist(me.hex, nearest.hex);
      for (const m of monsters.slice(1)) {
        const d = dist(me.hex, m.hex);
        if (d < bestDist) {
          nearest = m;
          bestDist = d;
        }
      }

      // In a bubble, only this turn's reachable tiles are selectable (the
      // tactical click filter) — step onto whichever reachable tile closes
      // the most distance, exactly like a human player now plays it. The
      // monster's own hex appears in combatMoves as a melee tile once
      // adjacent, so this same pick lands the killing strike.
      if (window.game.inCombat && window.game.combatMoves.length > 0) {
        let step = window.game.combatMoves[0]!;
        for (const h of window.game.combatMoves.slice(1)) {
          if (dist(h, nearest.hex) < dist(step, nearest.hex)) {
            step = h;
          }
        }

        window.game.tapHex(step.q, step.r);

        return;
      }

      window.game.tapHex(nearest.hex.q, nearest.hex.r);
    }, EntityMonster);

    return page.evaluate((b) => {
      const now = window.game.hp;

      // Real damage shows up two ways, and this must accept BOTH (#234): a
      // survivor whose HP dropped below baseline (cur < before), OR a monster
      // that is simply GONE (cur === undefined) — a killing blow. A one-shot
      // kill (the fighter's 4 damage exactly matches a rat's 4 HP) removes the
      // entity outright, so its HP never reads "< before"; it just vanishes.
      // Counting only survivors' drops made this test blind to one-shot kills:
      // against a low-HP monster draw it ground the shared, non-respawning
      // monster pool to nothing while still timing out — and starved the
      // sibling freeze test (:131) of any live monster (both failed together
      // under CI load).
      return Object.entries(b).some(([id, before]) => {
        if (before <= 0) {
          return false;
        }
        const cur = now[Number(id)];

        return cur === undefined || cur < before;
      });
    }, base);
  };

  // TURN_INTERVAL is 250ms in the e2e server; poll a bit slower than that so
  // each round's tapHex has landed in a turn bundle before the next retarget.
  await expect
    .poll(() => chase(baseline), { timeout: 20_000, intervals: [300] })
    .toBe(true);

  // Stop this entity's walk immediately: a melee attack that's still
  // opposing-held keeps its queued path (retained, not consumed), so left
  // unattended this entity would keep autonomously melee-attacking on every future turn —
  // entities persist server-side for the whole shared combat-server session
  // (see playwright.config.ts), and could grind through the fixed monster
  // population that the sibling monsters.spec test also depends on. Retarget
  // to our own current hex: Pathfind(from == to) sets an empty path.
  // Awaited: page.evaluate waits for a returned promise to settle, and
  // tapHex now resolves only once its intent POST has landed — so the
  // browser context can't close (and the test can't move on) before the
  // clear is confirmed server-side.
  await page.evaluate(async () => {
    const me = window.game.me;
    if (me !== null) {
      await window.game.tapHex(me.hex.q, me.hex.r);
    }
  });

  // Visual smoke check: the stage painted something (dots + the new HP bar
  // over the damaged entity), not a black void.
  const screenshot = await page.screenshot();
  expect(screenshot.byteLength).toBeGreaterThan(10_000);
});

// Milestone 6.4's headline behavior: a combat time bubble freezes LOCALLY
// while the WORLD clock keeps running. This test deliberately stops feeding
// the client intents the moment it enters combat — the point is to observe
// the freeze, not to fight — so unlike the damage test above it should not
// consume the shared combat server's fixed (non-respawning) monster pool.
test("entering a combat bubble freezes locally while window.game.turn keeps advancing", async ({ page }) => {
  await gotoReady(page);
  await expect.poll(() => page.evaluate(() => window.game.monsters)).toBeGreaterThanOrEqual(1);

  // Same re-targeting rationale as the damage test: monsters hunt back and
  // spawn positions vary between runs, so retarget the nearest monster every
  // poll rather than walking toward a single fixed destination.
  const chaseNearestMonster = async (): Promise<void> => {
    await page.evaluate((monsterKind) => {
      const me = window.game.me;
      if (me === null) {
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
      let bestDist = dist(me.hex, nearest.hex);
      for (const m of monsters.slice(1)) {
        const d = dist(me.hex, m.hex);
        if (d < bestDist) {
          nearest = m;
          bestDist = d;
        }
      }

      window.game.tapHex(nearest.hex.q, nearest.hex.r);
    }, EntityMonster);
  };

  // Chase until the bubble forms (CombatRadius=6 is well clear of adjacency,
  // so this converges before any melee attack — no HP changes expected
  // here). The instant inCombat flips, stop calling tapHex entirely: no more
  // intents from this client for the rest of the test.
  await expect
    .poll(
      async () => {
        await chaseNearestMonster();

        return page.evaluate(() => window.game.inCombat);
      },
      { timeout: 20_000, intervals: [200] },
    )
    .toBe(true);

  // The combat panel takes over the WeGo timer's spot while frozen.
  await expect(page.locator("#combat-panel")).toBeVisible();
  await expect(page.locator("#turn-timer")).toBeHidden();

  const turnAtFreeze = await page.evaluate(() => window.game.turn);
  const hexAtFreeze = await page.evaluate(() => window.game.me?.hex ?? null);

  // The headline: window.game.turn (the world clock) keeps climbing even
  // though this client has stopped submitting anything — local time is
  // frozen, world time is not.
  await expect
    .poll(() => page.evaluate(() => window.game.turn), { timeout: 5_000, intervals: [100] })
    .toBeGreaterThan(turnAtFreeze);

  // Best-effort: since this client never locked in, its own hex should still
  // hold too. If this ever flakes under CI jitter (a slow tick between the
  // two evaluate() calls letting a patience timeout land first), the turn-
  // advance assertion above is the one that must hold — see task-6-report.md.
  expect(await page.evaluate(() => window.game.me?.hex ?? null)).toEqual(hexAtFreeze);
  expect(await page.evaluate(() => window.game.inCombat)).toBe(true);

  // SPACE = wait (item 11): the same own-hex move a click already
  // waits/cancels with. Pressing it must not move this entity, and — item
  // 6 — window.game.committedAction reports the wait glyph's shape
  // immediately. Dispatched as a synthetic keydown AND read back inside one
  // page.evaluate call (rather than Playwright's page.keyboard.press
  // followed by a separate evaluate) so the read happens on the exact same
  // synchronous JS turn as the dispatch — bindMovementKeys' listener sets
  // committedAction synchronously, before the intent POST's async round
  // trip even starts, but a live 250ms turn cadence means a REAL two-round-
  // trip gap between a keyboard.press and a follow-up evaluate can let the
  // next turn bundle land first and clear it (item 6's "cleared on the next
  // turn bundle" rule, working as designed) — a flake this atomic dispatch
  // avoids without changing that production behavior.
  const committed = await page.evaluate(() => {
    window.dispatchEvent(new KeyboardEvent("keydown", { code: "Space", bubbles: true, cancelable: true }));

    return window.game.committedAction;
  });
  expect(committed).toEqual({ kind: "wait", target: hexAtFreeze });
  expect(await page.evaluate(() => window.game.me?.hex ?? null)).toEqual(hexAtFreeze);

  // Stop this entity's walk immediately: the chase loop above left a queued
  // path aimed at the monster. movePhaseLocked unconditionally consumes a
  // non-empty e.path when the bubble resolves — lock-in gating only controls
  // *when* the bubble resolves, not whether a queued path gets walked. If
  // this bubble ever resolves (e.g. via the e2e server's COMBAT_PATIENCE
  // timeout), the residual path would walk this entity toward the monster
  // and could melee-attack it, draining the shared combat server's fixed
  // (non-respawning) monster pool that monsters.spec.ts also depends on —
  // the same failure class fixed in 84f1471. Retarget to our own current
  // hex: Pathfind(from == to) sets an empty path. Awaited: tapHex resolves
  // only once its intent POST has landed server-side.
  await page.evaluate(async () => {
    const me = window.game.me;
    if (me !== null) {
      await window.game.tapHex(me.hex.q, me.hex.r);
    }
  });

  // Visual smoke check: the combat panel actually painted.
  const screenshot = await page.screenshot();
  expect(screenshot.byteLength).toBeGreaterThan(10_000);
});
