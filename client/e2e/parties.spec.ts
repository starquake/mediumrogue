import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";

declare global {
  interface Window {
    game: GameDebug;
  }
}

// Both clients auto-join with the default name "traveler" — that's fine here:
// server-side /invite <name> resolves to the NEAREST player named <name>
// EXCLUDING the sender (see World.nearestPlayerByNameLocked), so when A
// invites "traveler" it necessarily targets B, the only other "traveler".
test("parties: invite, accept, roster, and leave dissolves it", async ({ browser }) => {
  const ctxA = await browser.newContext();
  const ctxB = await browser.newContext();
  const a = await ctxA.newPage();
  const b = await ctxB.newPage();

  await a.goto("/");
  await b.goto("/");

  // 1. Both join + connect, both start solo.
  await expect.poll(() => a.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();
  await expect.poll(() => b.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();
  await expect.poll(() => a.evaluate(() => window.game.connected)).toBe(true);
  await expect.poll(() => b.evaluate(() => window.game.connected)).toBe(true);
  await expect.poll(() => a.evaluate(() => window.game.name)).toBe("traveler");
  await expect.poll(() => b.evaluate(() => window.game.name)).toBe("traveler");

  // Both streams must be live (have observed at least one turn bundle) before
  // A invites — chat is ephemeral (no history), so B must already be
  // subscribed to receive the "invited you" announcement, and the invite
  // itself needs B present in the world.
  await expect.poll(() => a.evaluate(() => window.game.turn)).toBeGreaterThanOrEqual(0);
  await expect.poll(() => b.evaluate(() => window.game.turn)).toBeGreaterThanOrEqual(0);

  await expect.poll(() => a.evaluate(() => window.game.partyId)).toBe(0);
  await expect.poll(() => b.evaluate(() => window.game.partyId)).toBe(0);
  expect(await a.evaluate(() => window.game.party.length)).toBe(0);
  expect(await b.evaluate(() => window.game.party.length)).toBe(0);

  // 2. A invites B (both named "traveler" — nearest-excluding-sender targets
  // B), then B accepts. sendChat awaits the POST response, so by the time
  // each call resolves the server has already applied the invite/accept.
  const bName = await b.evaluate(() => window.game.name);
  await a.evaluate((n) => window.game.sendChat("/invite " + n), bName);
  await b.evaluate(() => window.game.sendChat("/accept"));

  // 3. Both land in the same non-zero party, both see 2 members, both DOMs
  // list 2 roster entries.
  await expect
    .poll(() => a.evaluate(() => window.game.partyId), { timeout: 15_000 })
    .not.toBe(0);
  await expect
    .poll(() => b.evaluate(() => window.game.partyId), { timeout: 15_000 })
    .not.toBe(0);
  await expect
    .poll(() => a.evaluate(() => window.game.party.length), { timeout: 15_000 })
    .toBe(2);
  await expect
    .poll(() => b.evaluate(() => window.game.party.length), { timeout: 15_000 })
    .toBe(2);

  const partyIdA = await a.evaluate(() => window.game.partyId);
  const partyIdB = await b.evaluate(() => window.game.partyId);
  expect(partyIdA).not.toBe(0);
  expect(partyIdA).toBe(partyIdB);

  await expect(a.locator(".roster-member")).toHaveCount(2);
  await expect(b.locator(".roster-member")).toHaveCount(2);
  await expect(a.locator("#roster-panel")).toBeVisible();
  await expect(b.locator("#roster-panel")).toBeVisible();

  // 4. B leaves: the party dissolves (< 2 members), so both are back to solo
  // and the roster panel unmounts (the <Show> hides it) on both clients.
  await b.evaluate(() => window.game.sendChat("/leave"));

  await expect
    .poll(() => a.evaluate(() => window.game.partyId), { timeout: 15_000 })
    .toBe(0);
  await expect
    .poll(() => b.evaluate(() => window.game.partyId), { timeout: 15_000 })
    .toBe(0);
  await expect
    .poll(() => a.evaluate(() => window.game.party.length), { timeout: 15_000 })
    .toBe(0);
  await expect
    .poll(() => b.evaluate(() => window.game.party.length), { timeout: 15_000 })
    .toBe(0);

  await expect(a.locator("#roster-panel")).toHaveCount(0);
  await expect(b.locator("#roster-panel")).toHaveCount(0);

  await ctxA.close();
  await ctxB.close();
});
