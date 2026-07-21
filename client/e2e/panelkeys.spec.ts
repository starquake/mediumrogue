import { expect, test } from "@playwright/test";

// Gear keystone, task 4 (amended #273): `I` toggles the character panel and
// Escape closes it — guarded so neither fires while a text input (chat, or the
// join-name field on the start screen) has focus. #273 handed `c` to the survey
// camera (recenter), so `c` no longer opens the panel; this spec pins both the
// `i` toggle and that `c` recenters (zeroing a WASD pan) instead of toggling.
test("I toggles the panel, Esc closes, chat focus suppresses; C recenters the camera", async ({ page }) => {
  await page.goto("/");

  // Wait for a real join and at least one resolved turn before touching keys
  // (a not-yet-joined character has no panel to toggle).
  await expect.poll(() => page.evaluate(() => window.game.me !== null && window.game.connected)).toBe(true);
  await expect.poll(() => page.evaluate(() => window.game.turn >= 1)).toBe(true);

  expect(await page.evaluate(() => window.game.panelOpen)).toBe(false);

  await page.keyboard.press("i");
  await expect.poll(() => page.evaluate(() => window.game.panelOpen)).toBe(true);

  await page.keyboard.press("i");
  await expect.poll(() => page.evaluate(() => window.game.panelOpen)).toBe(false);

  await page.keyboard.press("Escape");
  await expect.poll(() => page.evaluate(() => window.game.panelOpen)).toBe(false);

  // `c` no longer toggles the panel (#273) — it recenters the survey camera.
  // Prove it functionally: hold W to build a pan offset (the ticker integrates
  // held WASD every frame), then press C and watch the pan zero out — all via
  // polled game state, never a sleep.
  await page.keyboard.down("KeyW");
  await expect.poll(() => page.evaluate(() => window.game.pan.y)).toBeGreaterThan(0);
  await page.keyboard.up("KeyW");
  expect(await page.evaluate(() => window.game.panelOpen)).toBe(false); // panning never opened a panel

  await page.keyboard.press("c");
  await expect.poll(() => page.evaluate(() => window.game.pan)).toEqual({ x: 0, y: 0 });
  expect(await page.evaluate(() => window.game.panelOpen)).toBe(false); // C never opens the panel

  // Chat focus suppresses the panel key AND the pan/recenter keys — typing
  // "wia" in chat must neither open the panel nor pan the camera.
  await page.locator("#chat-input").focus();
  await page.keyboard.press("i");
  expect(await page.evaluate(() => window.game.panelOpen)).toBe(false);
  await page.keyboard.down("KeyW");
  await page.keyboard.up("KeyW");
  expect(await page.evaluate(() => window.game.pan)).toEqual({ x: 0, y: 0 }); // no pan while typing

  // Open the panel first (via the HUD button, not a key, to isolate the next
  // assertion), then confirm Escape while chat is focused does not steal the
  // key either.
  await page.locator("#chat-input").fill("");
  await page.locator("#toggle-inventory").click();
  await expect.poll(() => page.evaluate(() => window.game.panelOpen)).toBe(true);
  await page.locator("#chat-input").focus();
  await page.keyboard.press("Escape");
  expect(await page.evaluate(() => window.game.panelOpen)).toBe(true);
});

// #203: the controls overlay opens with "?" and closes with Esc; Esc also
// closes the skills panel (previously Esc only closed the character panel).
test("the ? controls overlay toggles, and Esc closes skills too", async ({ page }) => {
  // Suppress the first-run auto-open so this test drives the toggle itself.
  await page.addInitScript(() => localStorage.setItem("mediumrogue.seenControls", "1"));
  await page.goto("/");
  await expect.poll(() => page.evaluate(() => window.game.me !== null && window.game.connected)).toBe(true);
  await expect.poll(() => page.evaluate(() => window.game.turn >= 1)).toBe(true);

  expect(await page.evaluate(() => window.game.controlsOpen)).toBe(false);

  // "?" is Shift+/ — the physical key is Slash.
  await page.keyboard.press("Shift+Slash");
  await expect.poll(() => page.evaluate(() => window.game.controlsOpen)).toBe(true);

  await page.keyboard.press("Escape");
  await expect.poll(() => page.evaluate(() => window.game.controlsOpen)).toBe(false);

  // Esc now also closes the skills panel.
  await page.keyboard.press("k");
  await expect.poll(() => page.evaluate(() => window.game.skillsPanelOpen)).toBe(true);
  await page.keyboard.press("Escape");
  await expect.poll(() => page.evaluate(() => window.game.skillsPanelOpen)).toBe(false);
});

// #203: a first-ever join auto-shows the controls overlay once.
test("controls overlay auto-shows on a first-ever join", async ({ page }) => {
  await page.goto("/"); // no seenControls flag set → first run
  await expect.poll(() => page.evaluate(() => window.game.me !== null && window.game.connected)).toBe(true);
  await expect.poll(() => page.evaluate(() => window.game.controlsOpen)).toBe(true);
});
