import { describe, expect, it } from "vitest";

import { assign, defaultAssignment, EMPTY, reconcile, SLOT_COUNT, unslotted } from "./slots";

// slots.test.ts (#304): the action-bar assignment rules.
//
// Tested here rather than through an e2e because the e2e server grants a fresh
// join no skill points, so no active is ever learnable in a browser and such a
// spec could only ever skip.

const FIVE = ["blink", "second-wind", "bulwark", "expose", "ember-nova"];

describe("defaultAssignment", () => {
  it("fills the bar with the first four learned actives", () => {
    expect(defaultAssignment(FIVE)).toEqual(["blink", "second-wind", "bulwark", "expose"]);
  });

  it("leaves the rest empty when you have learned fewer than four", () => {
    expect(defaultAssignment(["blink"])).toEqual(["blink", EMPTY, EMPTY, EMPTY]);
  });

  it("is always exactly the bar's width", () => {
    expect(defaultAssignment([])).toHaveLength(SLOT_COUNT);
    expect(defaultAssignment(FIVE)).toHaveLength(SLOT_COUNT);
  });
});

describe("reconcile", () => {
  it("keeps a stored assignment that is still valid", () => {
    const stored = ["ember-nova", EMPTY, "blink", EMPTY];

    expect(reconcile(stored, FIVE).slice(0, 3)).toEqual(["ember-nova", "second-wind", "blink"]);
  });

  it("empties a slot naming a skill this character has not learned", () => {
    // localStorage is per BROWSER, not per character, so a stored assignment
    // outliving the character it was made for is the normal case after a
    // reroll — not an edge one.
    const stored = ["ember-nova", EMPTY, EMPTY, EMPTY];

    expect(reconcile(stored, ["blink"])).toEqual(["blink", EMPTY, EMPTY, EMPTY]);
  });

  it("never displaces an assigned skill when a new one is learned", () => {
    // Blink stays in slot 3. Silently pushing it out because you learned
    // something else is what gets noticed mid-fight.
    const stored = [EMPTY, EMPTY, "blink", EMPTY];
    const got = reconcile(stored, ["blink", "bulwark"]);

    expect(got[2]).toBe("blink");
    expect(got[0]).toBe("bulwark");
  });

  it("leaves the fifth active unslotted rather than growing the bar", () => {
    const got = reconcile([], FIVE);

    expect(got).toHaveLength(SLOT_COUNT);
    expect(unslotted(got, FIVE)).toEqual(["ember-nova"]);
  });
});

describe("assign", () => {
  it("puts a skill in the chosen slot", () => {
    expect(assign([EMPTY, EMPTY, EMPTY, EMPTY], 2, "blink")[2]).toBe("blink");
  });

  it("never lets one skill occupy two slots", () => {
    // Otherwise assigning a skill that is already placed silently costs the
    // player a slot.
    const got = assign(["blink", EMPTY, EMPTY, EMPTY], 3, "blink");

    expect(got).toEqual([EMPTY, EMPTY, EMPTY, "blink"]);
  });

  it("blanks a slot when given EMPTY", () => {
    expect(assign(["blink", EMPTY, EMPTY, EMPTY], 0, EMPTY)[0]).toBe(EMPTY);
  });

  it("ignores a slot index outside the bar", () => {
    const before = ["blink", EMPTY, EMPTY, EMPTY];

    expect(assign(before, 9, "bulwark")).toEqual(before);
  });
});
