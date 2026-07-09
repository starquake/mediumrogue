import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";

declare global {
  interface Window {
    game: GameDebug;
  }
}

// The e2e server is shared across the whole suite and entities never despawn,
// so assert >= 2 and track specific entity ids rather than an exact count.
test("two clients share one world and see each other move", async ({ browser }) => {
  const ctxA = await browser.newContext();
  const ctxB = await browser.newContext();
  const a = await ctxA.newPage();
  const b = await ctxB.newPage();

  await a.goto("/");
  await b.goto("/");

  await expect.poll(() => a.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();
  await expect.poll(() => b.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();

  const idA = await a.evaluate(() => window.game.me!.id);
  const idB = await b.evaluate(() => window.game.me!.id);
  expect(idA).not.toBe(idB); // distinct identities, separate localStorage

  await expect.poll(() => a.evaluate(() => window.game.entities)).toBeGreaterThanOrEqual(2);
  await expect.poll(() => b.evaluate(() => window.game.entities)).toBeGreaterThanOrEqual(2);

  // A walks one step (whichever direction is walkable from its spawn).
  const startA = await a.evaluate(() => window.game.me!.hex);
  for (const key of ["KeyW", "KeyE", "KeyD", "KeyS", "KeyA", "KeyQ"]) {
    await a.keyboard.press(key);
  }
  await expect
    .poll(
      () => a.evaluate((s) => { const h = window.game.me!.hex; return h.q !== s.q || h.r !== s.r; }, startA),
      { timeout: 10_000 },
    )
    .toBe(true);
  const movedA = await a.evaluate(() => window.game.me!.hex);

  // B observes A's entity at the moved hex — two clients, one shared world.
  await expect
    .poll(
      () =>
        b.evaluate(
          (args) => {
            const p = window.game.positions.find((x) => x.id === args.id);

            return p ? p.hex.q === args.hex.q && p.hex.r === args.hex.r : false;
          },
          { id: idA, hex: movedA },
        ),
      { timeout: 10_000 },
    )
    .toBe(true);

  await ctxA.close();
  await ctxB.close();
});

test("the client stays disconnected while the stream is down, then reconnects and resyncs", async ({ page }) => {
  // A true mid-session severance of an already-open SSE stream isn't reproducible
  // in the sandbox — setOffline leaves the open stream flowing. So block the
  // stream at the network layer before the client connects: the client must
  // report disconnected and keep retrying (EventSource retry + the liveness
  // watchdog), then recover when the stream returns.
  // "**" also covers the "?token=..." query string the client now sends —
  // without it, the glob wouldn't match the token-bearing request and this
  // route would silently stop intercepting the stream.
  await page.route("**/api/events**", (route) => route.abort());
  await page.goto("/");

  // Join (POST /api/join is not blocked) succeeds, but the stream is down: no
  // turn bundle has arrived (turn stays -1) and the HUD reports disconnected.
  await expect.poll(() => page.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();
  await expect.poll(() => page.evaluate(() => window.game.connected)).toBe(false);
  expect(await page.evaluate(() => window.game.turn)).toBe(-1);

  // Restore the stream: the client reconnects and resyncs to the latest turn.
  await page.unroute("**/api/events**");
  await expect.poll(() => page.evaluate(() => window.game.connected), { timeout: 15_000 }).toBe(true);
  await expect
    .poll(() => page.evaluate(() => window.game.turn), { timeout: 15_000 })
    .toBeGreaterThanOrEqual(0);
});
