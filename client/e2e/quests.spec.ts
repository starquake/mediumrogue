import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";
import { SpeciesDwarf, XPPerLevel } from "../src/protocol.gen";
import type { Hex, QuestView } from "../src/protocol.gen";

declare global {
  interface Window {
    game: GameDebug;
  }
}

// hexDistance mirrors internal/game's cube-distance helper (see walk.spec.ts).
function hexDistance(a: Hex, b: Hex): number {
  const dq = a.q - b.q;
  const dr = a.r - b.r;
  const ds = -dq - dr;

  return (Math.abs(dq) + Math.abs(dr) + Math.abs(ds)) / 2;
}

test("quests: take a reach quest, walk to its goal, and it completes with a visible XP jump", async ({ page }) => {
  // Species must be picked BEFORE join so this run is a non-human (dwarf) —
  // no XP% passive — making the reward XP an exact, predictable number to
  // assert against. Same delay trick as species.spec.ts: delay /api/map so
  // the picker is still visible when the click lands (src/main.ts hides the
  // picker and joins right after fetchMap() resolves).
  await page.route("**/api/map", async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 300));
    await route.continue();
  });

  await page.goto("/");

  const dwarfButton = page.locator('#species-picker button[data-species="dwarf"]');
  await expect(dwarfButton).toBeVisible();
  await dwarfButton.click();

  await expect
    .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
    .toBe(true);
  await expect.poll(() => page.evaluate(() => window.game.species)).toBe(SpeciesDwarf);

  // 1. The seeded 6-quest board rides the very first turn bundle.
  await expect.poll(() => page.evaluate(() => window.game.quests.length)).toBe(6);

  // 2. Pick the closest available reach quest — the walk needs to actually
  // finish inside this test's timeout (goals are ≥8 hexes out; the ms turn
  // cadence still means several real turns of walking).
  const quests = await page.evaluate(() => window.game.quests);
  const me = await page.evaluate(() => window.game.me!.hex);
  const reachQuests = quests.filter((q: QuestView) => q.kind === "reach" && q.state === "available");
  expect(reachQuests.length).toBeGreaterThan(0);

  let target = reachQuests[0]!;
  let bestDist = hexDistance(me, target.goalHex);
  for (const q of reachQuests.slice(1)) {
    const d = hexDistance(me, q.goalHex);
    if (d < bestDist) {
      target = q;
      bestDist = d;
    }
  }

  // 3. Record the "before" state, take the quest via chat command, and wait
  // for the panel to reflect it.
  const xpBefore = await page.evaluate(() => window.game.xp);
  const statsBefore = await page.locator("#stats").textContent();

  await page.evaluate((id) => window.game.sendChat("/quest " + id), target.id);

  await expect.poll(() => page.evaluate(() => window.game.quest?.id ?? null)).toBe(target.id);

  await expect(page.locator("#quest-mine")).toBeVisible();
  await expect(page.locator("#quest-mine")).toContainText(target.name);
  await expect(page.locator("#quest-mine")).toContainText("XP");

  // 4. Walk to the goal hex — server-authoritative BFS path queue, one hex
  // per turn, same click-to-move path as walk.spec.ts. Generous timeout: the
  // goal can be many turns away even at the fast ms test cadence.
  await page.evaluate((h) => window.game.tapHex(h.q, h.r), target.goalHex);

  await expect
    .poll(
      () =>
        page.evaluate((id) => {
          const q = window.game.quests.find((x) => x.id === id);

          return window.game.quest === null && q?.state === "completed";
        }, target.id),
      { timeout: 30_000 },
    )
    .toBe(true);

  // 5. XP visibly jumped: exact reward (dwarf has no XP% passive) reflected
  // both in window.game.xp and the rendered #stats HUD text ("Lv L ·
  // xpIntoLevel/XPPerLevel XP" — see main.ts).
  const xpAfter = xpBefore + target.rewardXp;
  await expect.poll(() => page.evaluate(() => window.game.xp)).toBe(xpAfter);
  await expect
    .poll(() => page.locator("#stats").textContent())
    .not.toBe(statsBefore);
  await expect(page.locator("#stats")).toContainText(`${xpAfter % XPPerLevel}/${XPPerLevel} XP`);

  // 6. A system chat line announced the completion.
  await expect
    .poll(() => page.evaluate(() => window.game.chat.some((m) => m.sender === "system" && m.text.includes("Quest complete"))))
    .toBe(true);
});
