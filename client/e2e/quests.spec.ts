import { expect, test } from "@playwright/test";

import { SpeciesDwarf, XPCurveBase } from "../src/protocol.gen";
import type { Hex, QuestView } from "../src/protocol.gen";
import { gotoReady, seedIdentity } from "./helpers";

// hexDistance mirrors internal/game's cube-distance helper (see walk.spec.ts).
function hexDistance(a: Hex, b: Hex): number {
  const dq = a.q - b.q;
  const dr = a.r - b.r;
  const ds = -dq - dr;

  return (Math.abs(dq) + Math.abs(dr) + Math.abs(ds)) / 2;
}

test("quests: take a reach quest, walk to its goal, and it completes with a visible XP jump", async ({ page }) => {
  // Species must be a non-human (dwarf) — no XP% passive — so the reward XP
  // is an exact, predictable number to assert against. Seed a "returning
  // player" identity (no token) requesting Dwarf, same technique as
  // ranged.spec.ts/gear.spec.ts, deterministic without touching the start
  // screen itself (whose own species-selection UX is exercised in
  // class.spec.ts).
  await seedIdentity(page, { species: "dwarf" });

  await gotoReady(page);
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

  // 3.5. The quest goal marker (item 12, playtest batch 2) tracks my active
  // reach quest's goal hex.
  await expect.poll(() => page.evaluate(() => window.game.questGoalMarker)).toEqual(target.goalHex);

  // 3.6. Item 14, playtest batch 2: taking a SECOND quest no longer errors
  // (the old one-slot rule is gone) — the panel shows both as distinct
  // rows, and window.game.myQuests carries both. Abandon it again right
  // away so the rest of this test's single-quest completion flow below is
  // unaffected.
  const second = (await page.evaluate(() => window.game.quests)).find(
    (q: QuestView) => q.state === "available" && q.id !== target.id,
  );
  expect(second, "expected a second available quest distinct from target").toBeDefined();

  await page.evaluate((id) => window.game.sendChat("/quest " + id), second!.id);
  await expect.poll(() => page.evaluate(() => window.game.myQuests.length)).toBe(2);
  await expect(page.locator("#quest-mine .quest-mine-row")).toHaveCount(2);
  await expect(page.locator("#quest-mine")).toContainText(second!.name);

  await page.evaluate((id) => window.game.sendChat("/abandon " + id), second!.id);
  await expect.poll(() => page.evaluate(() => window.game.myQuests.length)).toBe(1);
  await expect.poll(() => page.evaluate(() => window.game.quest?.id ?? null)).toBe(target.id);

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
  // xpIntoLevel/xpForLevel XP" — see main.ts). The fresh player starts at 0
  // XP and the reach reward (questReachRewardXP=20) stays well under the
  // level-2 floor (XPCurveBase=100), so this stays level 1 the whole test —
  // level 1's floor is 0 and its next-level XP is XPCurveBase itself.
  const xpAfter = xpBefore + target.rewardXp;
  await expect.poll(() => page.evaluate(() => window.game.xp)).toBe(xpAfter);
  await expect
    .poll(() => page.locator("#stats").textContent())
    .not.toBe(statsBefore);
  await expect(page.locator("#stats")).toContainText(`${xpAfter}/${XPCurveBase} XP`);

  // 6. A system chat line announced the completion.
  await expect
    .poll(() => page.evaluate(() => window.game.chat.some((m) => m.sender === "system" && m.text.includes("Quest complete"))))
    .toBe(true);

  // 7. The goal marker clears once the quest completes (item 12).
  await expect.poll(() => page.evaluate(() => window.game.questGoalMarker)).toBeNull();
});
