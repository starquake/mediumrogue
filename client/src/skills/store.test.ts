import { describe, expect, test } from "vitest";

import type { SkillView } from "../protocol.gen";
import { applyLearnedLocally, points, setSkills, skills } from "./store";

// store.test.ts covers the immediate-learn path that no e2e can reach: the
// monster-free e2e server hands a fresh join ZERO skill points, and there is
// no grant hook to seed a bank, so an e2e for "learning updates the panel at
// once" can only ever skip. Same reasoning as gear/store.test.ts.
//
// What it protects: learning is NOT clock-gated — the server commits it
// immediately under the world mutex — but the panel is rebuilt from turn
// bundles, so before this the button looked dead for up to a whole turn.

function view(overrides: Partial<SkillView>): SkillView {
  return { id: "", name: "", tree: "survival", learned: false, stats: [], flavor: "", ...overrides } as SkillView;
}

describe("applyLearnedLocally", () => {
  test("marks the skill learned and debits the bank without a turn bundle", () => {
    setSkills([view({ id: "survivalist" }), view({ id: "hardy" })], 6);

    applyLearnedLocally("survivalist", 3);

    expect(skills().find((s) => s.id === "survivalist")?.learned).toBe(true);
    expect(points()).toBe(3);
  });

  test("leaves other skills alone", () => {
    setSkills([view({ id: "survivalist" }), view({ id: "hardy" })], 6);

    applyLearnedLocally("survivalist", 3);

    expect(skills().find((s) => s.id === "hardy")?.learned).toBe(false);
  });

  test("never shows a negative bank", () => {
    setSkills([view({ id: "survivalist" })], 1);

    applyLearnedLocally("survivalist", 3);

    expect(points()).toBe(0);
  });

  test("a later turn bundle overwrites it — the server stays authoritative", () => {
    setSkills([view({ id: "survivalist" })], 6);
    applyLearnedLocally("survivalist", 3);

    // The bundle disagreeing (e.g. the learn was rejected after all) wins.
    setSkills([view({ id: "survivalist", learned: false })], 6);

    expect(skills()[0]?.learned).toBe(false);
    expect(points()).toBe(6);
  });
});
