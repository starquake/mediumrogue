import { expect, test } from "@playwright/test";

// Gear keystone, task 4: C / I toggle the character panel and Escape closes
// it — guarded so none of the three fire while a text input (chat, or the
// join-name field on the start screen) has focus. `i` itself was already
// wired in task 3 (client/src/input/keys.ts, sharing bindMovementKeys' own
// typing-focus guard); this spec exercises `c` and `Escape` too, and the
// chat-focus suppression the brief calls out explicitly.
test("C and I toggle the panel, Esc closes, chat focus suppresses", async ({ page }) => {
  await page.goto("/");

  // Wait for a real join and at least one resolved turn before touching keys
  // (a not-yet-joined character has no panel to toggle).
  await expect.poll(() => page.evaluate(() => window.game.me !== null && window.game.connected)).toBe(true);
  await expect.poll(() => page.evaluate(() => window.game.turn >= 1)).toBe(true);

  expect(await page.evaluate(() => window.game.panelOpen)).toBe(false);

  await page.keyboard.press("c");
  await expect.poll(() => page.evaluate(() => window.game.panelOpen)).toBe(true);

  await page.keyboard.press("c");
  await expect.poll(() => page.evaluate(() => window.game.panelOpen)).toBe(false);

  await page.keyboard.press("i");
  await expect.poll(() => page.evaluate(() => window.game.panelOpen)).toBe(true);

  await page.keyboard.press("Escape");
  await expect.poll(() => page.evaluate(() => window.game.panelOpen)).toBe(false);

  // Escape with the panel already closed is a no-op (not merely a redundant
  // close) — nothing to assert beyond panelOpen staying false, already true.

  // Chat focus suppresses all three keys — typing "ice" in chat must not
  // open the panel (the brief's literal example).
  await page.locator("#chat-input").focus();
  await page.keyboard.press("i");
  expect(await page.evaluate(() => window.game.panelOpen)).toBe(false);
  await page.keyboard.press("c");
  expect(await page.evaluate(() => window.game.panelOpen)).toBe(false);

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
