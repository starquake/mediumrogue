import { expect, test } from "@playwright/test";

// This file runs on its own private server (playwright.config.ts's specs
// list) with the default pre-seeded identity for the `page` fixture (see
// storageStateFor) — a normal auto-join, same as most other specs. The
// second context below is deliberately blank (no localStorage at all): the
// point of this test is that a genuinely brand-new browser, given nothing
// but the copied link, ends up as the SAME character.

test("a copied character link rejoins the SAME character on a second browser context, skipping the start screen", async ({
  page,
  browser,
}) => {
  await page.goto("/");

  await expect.poll(() => page.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();
  // Wait for a real turn bundle: window.game.class/species/name/xp start out
  // as local join guesses and only become server-authoritative once a bundle
  // carrying this entity arrives (main.ts's onTurn handler) — class starts
  // "" until then, so a non-empty class is the signal a bundle landed.
  await expect.poll(() => page.evaluate(() => window.game.class)).not.toBe("");

  const idA = await page.evaluate(() => window.game.me!.id);
  const nameA = await page.evaluate(() => window.game.name);
  const classA = await page.evaluate(() => window.game.class);
  const speciesA = await page.evaluate(() => window.game.species);
  const xpA = await page.evaluate(() => window.game.xp);

  const link = await page.evaluate(() => window.game.identityLink);
  expect(link).toContain("#t=");
  expect(link.startsWith(new URL(page.url()).origin)).toBe(true);

  // A genuinely blank context — no cookies, no localStorage — simulating a
  // friend's browser that has never visited before.
  const ctxB = await browser.newContext({ storageState: { cookies: [], origins: [] } });
  const b = await ctxB.newPage();

  await b.goto(link);

  // The fragment never survives navigation: net/session.ts's
  // importIdentityFromFragment strips it via history.replaceState before
  // anything else in main.ts runs.
  expect(new URL(b.url()).hash).toBe("");

  // A blank context would normally see the start screen (class.spec.ts) — it
  // is skipped here specifically because the imported token makes this a
  // "returning player" the instant main.ts's isNewPlayer check runs, before
  // the map/engine even finish loading.
  await expect(b.locator("#start-screen")).toBeHidden();

  await expect.poll(() => b.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();

  const idB = await b.evaluate(() => window.game.me!.id);
  expect(idB).toBe(idA); // the SAME entity — a reclaim, not a fresh join

  await expect.poll(() => b.evaluate(() => window.game.name)).toBe(nameA);
  await expect.poll(() => b.evaluate(() => window.game.class)).toBe(classA);
  await expect.poll(() => b.evaluate(() => window.game.species)).toBe(speciesA);
  await expect.poll(() => b.evaluate(() => window.game.xp)).toBe(xpA);

  await ctxB.close();
});

test("importing a link with an unknown token falls back to the start screen instead of wedging", async ({
  browser,
}) => {
  // A blank context importing a token the server has never seen — the exact
  // shape of following a character link after the server lost its state
  // (snapshot off across a restart, or a rejected version/seed mismatch).
  // The import stores class:""/species:"" alongside the token; join sends
  // them as-is, so an unknown token is REJECTED (422) instead of silently
  // minting a default fighter — and the client must recover: clear the dead
  // identity, show the start screen, and let a normal join proceed. Before
  // the fix this wedged forever: the 422 killed start(), and the broken
  // identity persisted so every refresh re-failed.
  const ctx = await browser.newContext({ storageState: { cookies: [], origins: [] } });
  const page = await ctx.newPage();

  await page.goto("/#t=deadbeef");

  // The fragment is stripped regardless of what happens next.
  expect(new URL(page.url()).hash).toBe("");

  // The rejected join surfaces the start screen — not a dead error state.
  await expect(page.locator("#start-screen")).toBeVisible({ timeout: 15_000 });

  // And a normal join from it works; the dead identity is gone.
  await page.locator("#start-enter").click();
  await expect.poll(() => page.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();

  const raw = await page.evaluate(() => localStorage.getItem("mediumrogue.identity"));
  expect(raw).not.toBeNull();
  expect(raw).not.toContain("deadbeef");

  await ctx.close();
});

test("the copy-link button is hidden until joined, then reveals a link and flashes 'copied!' on click", async ({
  page,
}) => {
  await page.goto("/");

  const button = page.locator("#copy-link");

  await expect.poll(() => page.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();
  await expect(button).toBeVisible();
  await expect.poll(() => page.evaluate(() => window.game.identityLink)).toContain("#t=");

  // Grant clipboard access so the click handler's navigator.clipboard.writeText
  // actually resolves (instead of rejecting on a permission prompt) and this
  // test can read the clipboard back to confirm what landed there.
  await page.context().grantPermissions(["clipboard-write", "clipboard-read"]);

  await button.click();
  await expect(button).toHaveText("copied!");

  const copied = await page.evaluate(() => navigator.clipboard.readText());
  const link = await page.evaluate(() => window.game.identityLink);
  expect(copied).toBe(link);

  // The flash reverts on its own.
  await expect(button).toHaveText("copy character link", { timeout: 3_000 });
});
