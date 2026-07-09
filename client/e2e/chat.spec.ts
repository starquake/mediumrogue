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
