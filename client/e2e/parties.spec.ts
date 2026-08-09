import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

import { continueIfReturning } from "./helpers";

// Both clients auto-join with the default name "traveler" — that's fine here:
// server-side /invite <name> resolves to the NEAREST player named <name>
// EXCLUDING the sender (see World.nearestPlayerByNameLocked), so when A
// invites "traveler" it necessarily targets B, the only other "traveler".
//
// THAT HOLDS ONLY WHILE THIS IS THE SOLE INVITE TEST IN THE FILE. Tests here
// run as parallel workers against one shared project server, so a second test
// that also invites "traveler" puts four of them in the world and "the only
// other one" becomes "whichever is nearest" — a coin flip that flakes BOTH
// tests, not just the new one (#385: 3 clean rounds in 6). Adding one? Give
// its players unique names via a cleared storageState (#395).
test("parties: invite, accept, roster, and leave dissolves it", async ({ browser }) => {
  const ctxA = await browser.newContext();
  const ctxB = await browser.newContext();
  const a = await ctxA.newPage();
  const b = await ctxB.newPage();

  await a.goto("/");

  await continueIfReturning(a);
  await b.goto("/");
  await continueIfReturning(b);

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

// The invite PROMPT (#385): the panel that replaced "read a chat line and type
// /accept". Two contexts, because the whole point of the field is that it is
// own-only — one client must see the prompt and the other must not.
//
// UNIQUE NAMES here, unlike the test above, and this is load-bearing rather
// than tidiness. Both tests in this file run as parallel workers against the
// SAME project server, and /invite resolves a name to the NEAREST player
// carrying it. With everyone called "traveler", this test's invite can target
// the other test's player and vice versa: adding this spec with the default
// name made BOTH tests flake (3 clean rounds in 6 independent runs). Distinct
// names make each invite unambiguous no matter who else is in the world.
//
// Fresh storage state is what buys the name: every other project is pre-seeded
// with a remembered identity that auto-joins as "traveler" (see
// playwright.config.ts's storageStateFor), so the creation form only appears
// for a context that has cleared it.
test.describe("invite prompt", () => {
  test.use({ storageState: { cookies: [], origins: [] } });

  /** Join as a brand-new player with an explicit, unique name. */
  async function joinAs(page: Page, name: string): Promise<void> {
    await page.goto("/");
    await continueIfReturning(page);
    await page.locator("#start-name").fill(name);
    await page.locator("#start-enter").click();
    await expect(page.locator("#start-screen")).toBeHidden();
    await expect
      .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected), { timeout: 20_000 })
      .toBe(true);
  }

  test("appears for the invitee only, declines, and re-invites", async ({ browser }) => {
    const ctxA = await browser.newContext({ storageState: { cookies: [], origins: [] } });
    const ctxB = await browser.newContext({ storageState: { cookies: [], origins: [] } });
    const a = await ctxA.newPage();
    const b = await ctxB.newPage();

    // Names unique to THIS test, so a sibling test's players can never be the
    // nearest match for either invite below.
    const nameA = "promptHost";
    const nameB = "promptGuest";

    await joinAs(a, nameA);
    await joinAs(b, nameB);

    // Both streams live before the invite: the bundle field drives the panel,
    // so B has to be receiving bundles for it to appear at all.
    await expect.poll(() => a.evaluate(() => window.game.turn)).toBeGreaterThanOrEqual(0);
    await expect.poll(() => b.evaluate(() => window.game.turn)).toBeGreaterThanOrEqual(0);

    // Nobody has a prompt before anyone asks.
    await expect(a.locator("#invite-prompt")).toHaveCount(0);
    await expect(b.locator("#invite-prompt")).toHaveCount(0);

    await a.evaluate((n) => window.game.sendChat("/invite " + n), nameB);

    // 1. B gets the prompt naming A; A does NOT. The negative is the own-only
    // pin — asserted on A's DOM as well as the wire field, because a panel that
    // renders for the inviter is exactly what the per-viewer culling exists to
    // prevent and a server test cannot see it.
    await expect
      .poll(() => b.evaluate(() => window.game.pendingInvite?.inviterName ?? null), { timeout: 20_000 })
      .toBe(nameA);
    await expect(b.locator("#invite-prompt")).toBeVisible();
    expect(await a.evaluate(() => window.game.pendingInvite)).toBeNull();
    await expect(a.locator("#invite-prompt")).toHaveCount(0);

    // A is solo, so there is no roster to show and no party for B to leave.
    await expect(b.locator(".invite-member")).toHaveCount(0);
    await expect(b.locator(".invite-warning")).toHaveCount(0);

    // 2. Decline through the panel's own button — the interaction this slice
    // exists to add, not the /decline command underneath it.
    await b.locator("#invite-prompt button", { hasText: "Decline" }).click();

    await expect.poll(() => b.evaluate(() => window.game.pendingInvite), { timeout: 20_000 }).toBeNull();
    await expect(b.locator("#invite-prompt")).toHaveCount(0);

    // Declining is not accepting: neither ends up in a party.
    expect(await a.evaluate(() => window.game.partyId)).toBe(0);
    expect(await b.evaluate(() => window.game.partyId)).toBe(0);

    // 3. A re-invites immediately — no cooldown (maintainer's call) — and B
    // accepts through the panel this time.
    await a.evaluate((n) => window.game.sendChat("/invite " + n), nameB);
    await expect
      .poll(() => b.evaluate(() => window.game.pendingInvite?.inviterName ?? null), { timeout: 20_000 })
      .toBe(nameA);

    await b.locator("#invite-prompt button", { hasText: "Accept" }).click();

    await expect.poll(() => b.evaluate(() => window.game.partyId), { timeout: 20_000 }).not.toBe(0);
    await expect.poll(() => a.evaluate(() => window.game.partyId), { timeout: 20_000 }).not.toBe(0);

    // The prompt clears itself off the accept, without its own clearing step:
    // the bundle simply stops carrying an invite and the <Show> unmounts.
    await expect.poll(() => b.evaluate(() => window.game.pendingInvite), { timeout: 20_000 }).toBeNull();
    await expect(b.locator("#invite-prompt")).toHaveCount(0);
    await expect(b.locator("#roster-panel")).toBeVisible();

    await ctxA.close();
    await ctxB.close();
  });
});
