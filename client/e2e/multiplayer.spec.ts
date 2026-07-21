import { expect, test } from "@playwright/test";

import type { MapResponse } from "../src/protocol.gen";

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

  // A walks EXACTLY one step: pick the one walkable direction from A's spawn and
  // press only that key. The old version pressed all six direction keys, which
  // under load queues several steps across turns — A then walks PAST the hex we
  // captured before B observes it, and B's poll times out (#144). One step to a
  // known target hex is stable regardless of timing.
  const startA = await a.evaluate(() => window.game.me!.hex);
  const map = await a.evaluate(() => fetch("/api/map").then((r) => r.json() as Promise<MapResponse>));
  const walkable = new Set<string>();
  for (const tile of map.tiles) {
    if (tile.terrain === "grass" || tile.terrain === "forest") {
      walkable.add(`${tile.hex.q},${tile.hex.r}`);
    }
  }
  // Key → hex offset, mirroring client/src/input/keys.ts + render/hex.ts.
  const steps = [
    { key: "KeyW", dq: 0, dr: -1 }, // n
    { key: "KeyE", dq: 1, dr: -1 }, // ne
    { key: "KeyD", dq: 1, dr: 0 }, // se
    { key: "KeyS", dq: 0, dr: 1 }, // s
    { key: "KeyA", dq: -1, dr: 1 }, // sw
    { key: "KeyQ", dq: -1, dr: 0 }, // nw
  ];
  const step = steps.find((s) => walkable.has(`${startA.q + s.dq},${startA.r + s.dr}`));
  expect(step, "expected a walkable neighbour of A's spawn").toBeTruthy();
  const movedA = { q: startA.q + step!.dq, r: startA.r + step!.dr };

  await a.keyboard.press(step!.key);
  await expect
    .poll(
      () => a.evaluate((h) => { const m = window.game.me!.hex; return m.q === h.q && m.r === h.r; }, movedA),
      { timeout: 10_000 },
    )
    .toBe(true);

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
