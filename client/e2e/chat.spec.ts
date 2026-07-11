import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";
import type { Hex, MapResponse } from "../src/protocol.gen";

declare global {
  interface Window {
    game: GameDebug;
  }
}

// axialNeighbors mirrors internal/game.HexNeighbors (see walk.spec.ts).
function axialNeighbors(h: Hex): Hex[] {
  return [
    { q: h.q, r: h.r - 1 },
    { q: h.q + 1, r: h.r - 1 },
    { q: h.q + 1, r: h.r },
    { q: h.q, r: h.r + 1 },
    { q: h.q - 1, r: h.r + 1 },
    { q: h.q - 1, r: h.r },
  ];
}

// pickWalkableNeighbor finds any immediately-walkable neighbor of `start` by
// inspecting the real map (never assumes a fixed offset is walkable — see
// walk.spec.ts for why).
function pickWalkableNeighbor(map: MapResponse, start: Hex): Hex | null {
  const walkable = new Set<string>();
  for (const tile of map.tiles) {
    if (tile.terrain === "grass" || tile.terrain === "forest") {
      walkable.add(`${tile.hex.q},${tile.hex.r}`);
    }
  }

  return axialNeighbors(start).find((n) => walkable.has(`${n.q},${n.r}`)) ?? null;
}

test("chat: cross-client delivery, /here, readable command errors, and map clicks still work", async ({
  browser,
}) => {
  // Deliberately one long multi-part journey (two browser contexts, five
  // numbered sections, several 15s message-delivery polls): under CI-grade
  // CPU contention its cumulative wall time can legitimately exceed the
  // default 30s test budget without anything being wrong. 3x it.
  test.slow();

  const ctxA = await browser.newContext();
  const ctxB = await browser.newContext();
  const a = await ctxA.newPage();
  const b = await ctxB.newPage();

  await a.goto("/");
  await b.goto("/");

  // 1. Both auto-join (default name "traveler") and connect.
  await expect.poll(() => a.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();
  await expect.poll(() => b.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();
  await expect.poll(() => a.evaluate(() => window.game.connected)).toBe(true);
  await expect.poll(() => b.evaluate(() => window.game.connected)).toBe(true);
  await expect.poll(() => a.evaluate(() => window.game.name)).toBe("traveler");
  await expect.poll(() => b.evaluate(() => window.game.name)).toBe("traveler");

  // Both clients must have observed at least one turn bundle — i.e. their SSE
  // stream is actually flowing and subscribed to the (non-coalescing, no
  // history) chat broker — before A sends, or B's subscription could still be
  // spinning up and simply miss the ephemeral message.
  await expect.poll(() => a.evaluate(() => window.game.turn)).toBeGreaterThanOrEqual(0);
  await expect.poll(() => b.evaluate(() => window.game.turn)).toBeGreaterThanOrEqual(0);

  const nameA = await a.evaluate(() => window.game.name);

  // 2. A plain chat line reaches B: window.game.chat AND the rendered DOM.
  await a.evaluate(() => window.game.sendChat("hello from A"));
  await expect
    .poll(
      () => b.evaluate((n) => window.game.chat.some((m) => m.sender === n && m.text === "hello from A"), nameA),
      { timeout: 15_000 },
    )
    .toBe(true);
  await expect(b.locator("#chat-messages")).toContainText("hello from A");
  await expect(b.locator("#chat-messages")).toContainText(nameA);

  // 3. "/here" broadcasts a readable location line: coordinates + the pin.
  await a.evaluate(() => window.game.sendChat("/here"));
  await expect
    .poll(
      () =>
        b.evaluate((n) => {
          const line = window.game.chat.find((m) => m.sender === n && /\(-?\d+, -?\d+\)/.test(m.text));

          return line?.text ?? null;
        }, nameA),
      { timeout: 15_000 },
    )
    .toMatch(/📍/);

  // 4. An unknown "/" command surfaces a readable LOCAL system line on A —
  // never the raw JSON error body ({"error": "..."}) that used to leak
  // through before the fix this e2e locks in.
  await a.evaluate(() => window.game.sendChat("/badcmd"));
  await expect
    .poll(
      () =>
        a.evaluate(() => {
          const line = window.game.chat.find((m) => m.sender === "system" && m.text.includes("unknown command"));

          return line?.text ?? null;
        }),
      { timeout: 15_000 },
    )
    .toContain("/badcmd");

  const systemLine = await a.evaluate(
    () => window.game.chat.find((m) => m.sender === "system" && m.text.includes("unknown command"))!.text,
  );
  expect(systemLine).not.toContain('{"error"');
  expect(systemLine).not.toContain("{");
  // B never sees A's local-only system line (it's client-side, not broadcast).
  expect(await b.evaluate(() => window.game.chat.some((m) => m.sender === "system"))).toBe(false);

  // 4.5. Typing must not move the character (item 10, playtest batch 2 — a
  // real bug report): w/a/s/d are movement keys AND ordinary letters. Typing
  // "wasd" into the focused chat input must leave A's hex untouched.
  //
  // De-raced for CI (slow runners): A can legitimately still be WALKING here
  // (e.g. a retried run reclaims the same token, whose server-side queued
  // path from a previous attempt's map click is still draining), which made
  // the baseline hex a moving target. Cancel any queued movement first (an
  // own-hex tap is the wait/cancel; tapHex resolves once the intent POST
  // settles), then poll on the TURN COUNTER until the hex holds still across
  // two consecutive turn bundles before taking the baseline. No sleeps.
  await a.evaluate(() => window.game.tapHex(window.game.me!.hex.q, window.game.me!.hex.r));

  const readTurnHex = (): Promise<{ turn: number; hex: Hex }> =>
    a.evaluate(() => ({ turn: window.game.turn, hex: window.game.me!.hex }));

  let hexBeforeTyping: Hex | null = null;
  for (let i = 0; i < 40 && hexBeforeTyping === null; i++) {
    const before = await readTurnHex();
    await expect
      .poll(() => a.evaluate(() => window.game.turn), { timeout: 15_000 })
      .toBeGreaterThan(before.turn);
    const after = await readTurnHex();
    if (after.hex.q === before.hex.q && after.hex.r === before.hex.r) {
      hexBeforeTyping = after.hex;
    }
  }
  expect(hexBeforeTyping, "A's hex never stabilized across two consecutive turn bundles").not.toBeNull();

  await a.locator("#chat-input").click();
  await a.locator("#chat-input").pressSequentially("wasd");
  await expect(a.locator("#chat-input")).toHaveValue("wasd");

  // Assert across a FULL further bundle, not instantaneously: a move intent
  // the typing had wrongly queued would only surface as a hex change on the
  // next resolution, which an immediate read could miss.
  const turnAfterTyping = await a.evaluate(() => window.game.turn);
  await expect
    .poll(() => a.evaluate(() => window.game.turn), { timeout: 15_000 })
    .toBeGreaterThan(turnAfterTyping);
  expect(await a.evaluate(() => window.game.me!.hex)).toEqual(hexBeforeTyping);
  await a.locator("#chat-input").fill(""); // don't pollute the map-click step below

  // 5. Pointer-events guard: with the chat panel mounted, a "map click"
  // (tapHex — the same clickTarget code path a real canvas pointerdown uses,
  // see walk.spec.ts) still moves A. This proves #chat-root's click-through
  // overlay isn't swallowing map interaction.
  const map = await a.evaluate(() => fetch("/api/map").then((r) => r.json() as Promise<MapResponse>));
  const start = await a.evaluate(() => window.game.me!.hex);
  const dest = pickWalkableNeighbor(map, start);
  expect(dest, "expected a walkable neighbor from spawn on the static map").not.toBeNull();

  await a.evaluate((d) => window.game.tapHex(d!.q, d!.r), dest);
  await expect
    .poll(
      () =>
        a.evaluate((d) => {
          const hex = window.game.me!.hex;

          return hex.q === d!.q && hex.r === d!.r;
        }, dest),
      { timeout: 10_000 },
    )
    .toBe(true);

  await ctxA.close();
  await ctxB.close();
});
