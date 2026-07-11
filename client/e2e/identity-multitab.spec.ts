import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";

declare global {
  interface Window {
    game: GameDebug;
  }
}

// Reproduces the seam behind item 2's investigation (playtest feedback batch
// 3, "players swapped identities"): two browser TABS in the same context
// share localStorage's `mediumrogue.identity` key. The playtest report
// describes multiple humans/tabs on one machine — this is that scenario.
//
// Before the fix (net/session.ts), a tab's rejoin/reclaim re-read the token
// to send from `loadIdentity()` (disk) rather than its own in-memory
// identity, so a SECOND tab overwriting the shared key could get silently
// woven into the FIRST tab's next reclaim — the first tab would end up
// controlling (and rendering) the second tab's character. After the fix,
// `reclaim()` always sends the caller's own known token, and a `storage`
// event listener (main.ts) reloads a tab outright the instant another tab
// overwrites the shared identity — turning silent split-brain into an
// obvious, consistent reload instead.
test("a second tab joining as a different player triggers the first tab to reload instead of silently corrupting", async ({
  context,
  page,
}) => {
  // Two full page boots plus a triggered reload-and-rejoin: give it CI
  // headroom (3x the default budget), same rationale as chat.spec.ts.
  test.slow();

  // Tab 1: the seeded storageState identity auto-joins (fighter/human/
  // traveler, no token yet — see playwright.config.ts's storageStateFor).
  await page.goto("/");
  await expect.poll(() => page.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();
  await expect.poll(() => page.evaluate(() => window.game.class)).not.toBe("");

  const tokenA = await page.evaluate(
    () => (JSON.parse(localStorage.getItem("mediumrogue.identity")!) as { token: string }).token,
  );
  expect(tokenA).not.toBe("");

  // Tab 2: a SECOND person on the SAME browser. Clearing the shared identity
  // before the page's own scripts run makes this a genuine "someone else
  // joined in another tab" event (start screen, brand-new character) rather
  // than tab 1's own character opened twice.
  const page2 = await context.newPage();
  await page2.addInitScript(() => localStorage.removeItem("mediumrogue.identity"));
  await page2.goto("/");

  await expect(page2.locator("#start-screen")).toBeVisible();
  await page2.locator("#start-name").fill("bob");
  await page2.locator('.card[data-class="rogue"]').click();
  await page2.locator('.card[data-species="elf"]').click();

  await page2.locator("#start-enter").click();

  await expect.poll(() => page2.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();
  await expect.poll(() => page2.evaluate(() => window.game.class)).not.toBe("");

  const tokenB = await page2.evaluate(
    () => (JSON.parse(localStorage.getItem("mediumrogue.identity")!) as { token: string }).token,
  );
  expect(tokenB).not.toBe(tokenA);

  // Tab 1 must react to the `storage` event (fires only in OTHER tabs, i.e.
  // exactly here) by reloading — never keep running under a token tab 2 is
  // now using. Poll a post-reload OBSERVABLE instead of waitForEvent("load"):
  // an event listener is inherently racy against an indirectly-triggered
  // reload (fire before registration → timeout; this bit CI). The current
  // document's navigation entry is state, not an event — it reads "navigate"
  // on the original load and "reload" on the reloaded document, no matter
  // when we look. The evaluate is wrapped so a mid-navigation destroyed
  // context reads as "not yet" and the poll simply retries.
  await expect
    .poll(
      () =>
        page
          .evaluate(() => {
            const nav = performance.getEntriesByType("navigation")[0] as PerformanceNavigationTiming | undefined;

            return nav?.type ?? "";
          })
          .catch(() => ""),
      { timeout: 15_000 },
    )
    .toBe("reload");

  // Generous timeout: the reloaded tab must re-fetch the app, reclaim, and
  // see a first turn bundle — slow under CI-grade parallel contention.
  await expect
    .poll(() => page.evaluate(() => window.game.me?.id ?? null), { timeout: 15_000 })
    .not.toBeNull();

  // After the reload, tab 1 comes back up consistent with whatever is now
  // on disk (tab 2's identity, since it wrote last) — its rendered state and
  // its persisted token always agree. The load-bearing guarantee is that
  // there's no window where tab 1 silently renders/controls a mismatched
  // mix of the two identities.
  const tokenAfterReload = await page.evaluate(
    () => (JSON.parse(localStorage.getItem("mediumrogue.identity")!) as { token: string }).token,
  );
  expect(tokenAfterReload).toBe(tokenB);

  const idAfterReload = await page.evaluate(() => window.game.me!.id);
  const idPage2 = await page2.evaluate(() => window.game.me!.id);
  expect(idAfterReload).toBe(idPage2);
});

// The root-cause regression test for item 2's fix (revert-proof): reclaim()
// must post the CALLER'S in-memory identity token — never a re-read of
// localStorage. Clobbering the stored identity in the SAME tab (no
// `storage` event fires for same-tab writes, so no reload masks anything)
// and then forcing a rejoin pins exactly the difference between the fixed
// and the pre-fix code: the old join() would send the clobbered token from
// disk; reclaim() sends the tab's own. Without this test, reverting the
// session.ts fix would pass the whole suite (the reload test above only
// covers the cross-tab storage-event mitigation).
test("a forced rejoin reclaims with the tab's own in-memory token, ignoring a clobbered localStorage", async ({
  page,
}) => {
  await page.goto("/");
  await expect.poll(() => page.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();
  await expect.poll(() => page.evaluate(() => window.game.forceRejoin !== null)).toBe(true);

  const myToken = await page.evaluate(
    () => (JSON.parse(localStorage.getItem("mediumrogue.identity")!) as { token: string }).token,
  );
  expect(myToken).not.toBe("");
  const myId = await page.evaluate(() => window.game.me!.id);

  // Capture every /api/join body from here on (request interception —
  // observes the real wire payload, not client internals).
  const joinBodies: { token: string; name: string; class: string; species: string }[] = [];
  await page.route("**/api/join", async (route) => {
    joinBodies.push(route.request().postDataJSON() as (typeof joinBodies)[number]);
    await route.continue();
  });

  // Clobber the shared storage key with a DIFFERENT (bogus) token — the
  // exact state another tab's write leaves behind. Same-tab writes fire no
  // `storage` event, so nothing reloads; the tab just sits on poisoned
  // disk state.
  const bogusToken = "0123456789abcdef0123456789abcdef";
  await page.evaluate((bogus) => {
    localStorage.setItem(
      "mediumrogue.identity",
      JSON.stringify({ entityId: 999, token: bogus, class: "rogue", species: "elf" }),
    );
  }, bogusToken);

  await page.evaluate(() => window.game.forceRejoin!());

  expect(joinBodies.length).toBe(1);
  const posted = joinBodies[0]!;
  // THE pin: the posted token is this tab's own in-memory one — not the
  // clobbered value sitting in localStorage.
  expect(posted.token).toBe(myToken);
  expect(posted.token).not.toBe(bogusToken);
  // And the reclaim-or-fail contract: identity fields ride empty, so an
  // unknown token could never silently mint a fresh character.
  expect(posted.name).toBe("");
  expect(posted.class).toBe("");
  expect(posted.species).toBe("");

  // The live-token reclaim returns the same character, and the response
  // overwrites the poisoned storage — disk converges back to the truth.
  await expect.poll(() => page.evaluate(() => window.game.me!.id)).toBe(myId);
  const tokenAfter = await page.evaluate(
    () => (JSON.parse(localStorage.getItem("mediumrogue.identity")!) as { token: string }).token,
  );
  expect(tokenAfter).toBe(myToken);
});
