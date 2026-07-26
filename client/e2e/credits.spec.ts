import { expect, test } from "@playwright/test";

import { gotoReady } from "./helpers";

// credits.spec.ts (#298): the sound packs are CC0 and ask only that credit
// "would be nice" — so the credit has to actually be reachable.
//
// It lives on the controls overlay, not only the start screen: `isNewPlayer`
// skips the start screen for anyone with a stored identity, so attribution
// placed only there would be invisible to exactly the people who play most.

test("the controls overlay credits the asset authors, and the links are clickable", async ({
  page,
}) => {
  await gotoReady(page);

  // The overlay is default-OPEN on a first-ever join (#203 shows it once, keyed
  // on a localStorage flag), so clicking the HUD button blind would CLOSE it.
  // Confirm the state, then only toggle if it needs toggling.
  //
  // The button rather than the "?" key: that key binds on the physical code
  // ("Slash"), so a synthetic press of the character "?" never reaches it.
  if (!(await page.evaluate(() => window.game.controlsOpen))) {
    await page.click("#toggle-help");
  }
  await expect.poll(() => page.evaluate(() => window.game.controlsOpen)).toBe(true);

  const credits = page.locator("#controls-overlay .credits");
  await expect(credits).toBeVisible();
  await expect(credits).toContainText("Kenney");
  await expect(credits).toContainText("game-icons.net");

  // #controls-overlay is pointer-events: none so the map stays clickable
  // beneath it; anything interactive inside must re-enable it on itself. This
  // is the bug that made #toggle-sound render perfectly and swallow every
  // click, so it is asserted by HIT-TESTING rather than by reading the CSS.
  const link = credits.getByRole("link", { name: "Kenney" });
  const box = await link.boundingBox();
  expect(box).not.toBeNull();

  const topmostIsTheLink = await page.evaluate(({ x, y }) => {
    const el = document.elementFromPoint(x, y);
    return el?.closest("#controls-overlay .credits a") !== null;
  }, { x: (box?.x ?? 0) + (box?.width ?? 0) / 2, y: (box?.y ?? 0) + (box?.height ?? 0) / 2 });

  expect(topmostIsTheLink).toBe(true);
});
