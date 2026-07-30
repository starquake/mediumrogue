import { expect, test } from "@playwright/test";

import { continueIfReturning } from "./helpers";

// Evade (#322): the mechanic everyone has. This is e2e rather than a unit test
// because the whole point is the INTERACTION — a key that arms, an overlay that
// shows the reach, and a click that spends it. None of those exist below the
// browser.
//
// Every wait is metered on game state (window.game fields flipping), never
// wall-clock — the de-race rule.
test("Space arms evade, a click spends it, and the cooldown starts", async ({ page }) => {
  await page.goto("/");
  await continueIfReturning(page);

  await expect.poll(() => page.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();
  await expect.poll(() => page.evaluate(() => window.game.evadeReadyIn)).toBe(0);

  // Nobody learned anything: evade is universal, so a fresh character has it
  // and it is NOT in the skill list.
  const ids = await page.evaluate(() => window.game.skills.map((s) => s.id));
  expect(ids, "evade is universal — it has no panel row to learn").not.toContain("evade");

  // Nothing is painted until evade is armed — an always-on overlay is
  // wallpaper.
  expect(await page.evaluate(() => window.game.skillTiles.length), "no overlay before arming").toBe(0);

  await page.keyboard.press("Space");
  await expect.poll(() => page.evaluate(() => window.game.armedSkill())).toBe("evade");

  // Arming paints the reach, so the limit is visible BEFORE committing.
  await expect.poll(() => page.evaluate(() => window.game.skillTiles.length)).toBeGreaterThan(0);

  const from = await page.evaluate(() => window.game.me?.hex ?? null);
  if (from === null) {
    throw new Error("no player hex");
  }

  // Click a painted tile that the CANVAS actually receives. The action bar,
  // the globes and the chat panel sit over the map, so a tile behind one of
  // them is painted but unclickable — hit-test rather than assuming.
  //
  // NEAREST FIRST, and that is load-bearing. skillTargetTiles is documented as
  // "PREVIEW ONLY ... deliberately a SUPERSET" (client/src/tactics.ts): it
  // models neither line of sight nor occupancy, so a painted tile behind a rock
  // is painted AND refused on submit with 422 "no line of sight". That is
  // intended — the overlay shows reach, the server owns legality.
  //
  // So a test may not click an arbitrary painted tile: it must pick one the
  // server will accept. The NEAREST tile is adjacent, has nothing in between,
  // and therefore can never be sight-blocked. Taking overlay order instead made
  // this spec a coin flip on where the player spawned, which is how it failed
  // on main at e6665db after passing three times on its own branch.
  const dest = await page.evaluate(() => {
    const me = window.game.me;
    if (me === null) {
      return null;
    }

    const dist = (t: { q: number; r: number }): number => {
      const dq = t.q - me.hex.q;
      const dr = t.r - me.hex.r;

      return Math.max(Math.abs(dq), Math.abs(dr), Math.abs(-dq - dr));
    };

    for (const t of [...window.game.skillTiles].sort((a, b) => dist(a) - dist(b))) {
      const at = window.game.hexToScreen?.(t.q, t.r);
      if (at === undefined) continue;
      const el = document.elementFromPoint(at.x, at.y);
      if (el instanceof HTMLCanvasElement) return t;
    }
    return null;
  });
  if (dest === null) {
    throw new Error("evade armed but painted no tile the canvas can receive");
  }

  expect(await page.evaluate(() => window.game.hexToScreen !== null)).toBe(true);

  // Watch the intent's own response, so a refusal reports WHY instead of
  // surfacing 10s later as an unexplained "cooldown never started".
  const intent = page.waitForResponse((r) => r.url().includes("/api/intent"));

  const at = await page.evaluate((d) => window.game.hexToScreen!(d.q, d.r), dest);
  await page.mouse.click(at.x, at.y);

  const res = await intent;
  if (res.status() >= 300) {
    // Read the body ONLY here: a 2xx on this route has no retrievable body, so
    // reading it unconditionally would fail the passing case.
    const why = await res.text().catch(() => "<body unavailable>");

    throw new Error(`the server refused the evade with ${res.status()}: ${why} — the overlay is a preview superset`);
  }

  // The cooldown starting is the server's acknowledgement — it is stamped by
  // the same commit that moves the player, so polling it avoids racing the
  // move animation.
  await expect
    .poll(() => page.evaluate(() => window.game.evadeReadyIn), { timeout: 10_000 })
    .toBeGreaterThan(0);

  const to = await page.evaluate(() => window.game.me?.hex ?? null);
  expect(to, "evade should have moved the player off their hex").not.toEqual(from);

  // The ball is the only place the cooldown is visible, since a universal
  // mechanic has no action-bar slot. Ready draws a glyph and no text, cooling
  // swaps in the count — so a number IS the "not ready" assertion. Matching on
  // a digit rather than on "not the ready state" keeps it a real check: the
  // ready state is an <svg> with empty textContent, which any typo also
  // satisfies.
  await expect(page.locator("#evade-ball")).toBeVisible();
  await expect.poll(() => page.locator("#evade-ball .n").textContent()).toMatch(/^\d+$/);

  // Cooling: the overlay clears, because tiles you cannot reach are a lie.
  await expect.poll(() => page.evaluate(() => window.game.skillTiles.length)).toBe(0);
});

test("pressing Space twice cancels the arm without evading", async ({ page }) => {
  await page.goto("/");
  await continueIfReturning(page);

  await expect.poll(() => page.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();
  await expect.poll(() => page.evaluate(() => window.game.evadeReadyIn)).toBe(0);

  const from = await page.evaluate(() => window.game.me?.hex ?? null);

  await page.keyboard.press("Space");
  await expect.poll(() => page.evaluate(() => window.game.armedSkill())).toBe("evade");

  await page.keyboard.press("Space");
  await expect.poll(() => page.evaluate(() => window.game.armedSkill())).toBeNull();
  await expect.poll(() => page.evaluate(() => window.game.skillTiles.length)).toBe(0);

  // Give the world a couple of turns to prove nothing was queued.
  const start = await page.evaluate(() => window.game.turn);
  await expect.poll(() => page.evaluate(() => window.game.turn)).toBeGreaterThan(start + 1);

  expect(await page.evaluate(() => window.game.evadeReadyIn), "no evade should have fired").toBe(0);
  expect(await page.evaluate(() => window.game.me?.hex ?? null), "the player should not have moved").toEqual(from);
});
