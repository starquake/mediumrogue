import { expect, test } from "@playwright/test";

// #317: the start screen's buttons must be LIVE the instant they are visible.
//
// The listeners used to be attached only when the code got round to awaiting
// them — after the engine and the whole ~43,561-tile map had loaded — so the
// buttons were painted, hover-responsive and dead in between, and a click
// landing in that window was dropped with no feedback at all.
//
// This is asserted as an ORDERING rather than by racing a click. Racing it is
// not reproducible: at the suite's world size the window is sub-frame, and a
// larger world only moves the odds — a test that cannot reliably fail is worse
// than no test, because it reads as coverage. (Tried and discarded: a project
// with a 60-radius world, to widen the window — it passed with the bug still
// in, because the map still built faster than a click could arrive.)
//
// `window.game.startBoundAtTiles` records how much map was on stage when the
// buttons were bound, which makes the ordering a fact the client states rather
// than a race the test has to win. Verified both ways: 0 with the fix, 331
// without it.
test("the start screen's buttons are bound before the world finishes loading", async ({ page }) => {
  await page.goto("/");

  await page.locator("#continue-btn").waitFor({ state: "visible" });

  // 0 tiles on stage at bind time = the buttons were live before the map was
  // built. Before the fix this read the full tile count, because binding
  // happened after the load the player was waiting through.
  await expect
    .poll(() => page.evaluate(() => window.game.startBoundAtTiles))
    .toBe(0);

  // The click still works, of course.
  await page.locator("#continue-btn").click();
  await expect.poll(() => page.evaluate(() => window.game.connected), { timeout: 20_000 }).toBe(true);
});
