import { describe, expect, test } from "vitest";

import { ALL_PAIRS, everyNameFor, generateName, MaxNameLen, SystemSender } from "./names";

// names.test.ts (#402): the generator's guarantees, checked over EVERY
// combination rather than a sample — a name that fails server validation is a
// player who cannot join, and it would only appear for whoever happened to
// roll the long one.

describe("generateName", () => {
  test("covers all nine class/species pairs", () => {
    expect(ALL_PAIRS).toHaveLength(9);

    for (const { cls, species } of ALL_PAIRS) {
      expect(everyNameFor(cls, species).length, `${cls}/${species} has no names`).toBeGreaterThan(0);
    }
  });

  test("no combination exceeds MaxNameLen", () => {
    for (const { cls, species } of ALL_PAIRS) {
      for (const name of everyNameFor(cls, species)) {
        // Runes, not bytes — the server caps runes, and a pool word with an
        // accent would otherwise pass here and fail there.
        expect([...name].length, `${name} (${cls}/${species})`).toBeLessThanOrEqual(MaxNameLen);
      }
    }
  });

  test("no combination is the reserved sender name", () => {
    for (const { cls, species } of ALL_PAIRS) {
      for (const name of everyNameFor(cls, species)) {
        expect(name.toLowerCase()).not.toBe(SystemSender);
      }
    }
  });

  test("no combination is empty or untrimmed", () => {
    for (const { cls, species } of ALL_PAIRS) {
      for (const name of everyNameFor(cls, species)) {
        expect(name.trim()).toBe(name);
        expect(name.length).toBeGreaterThan(0);
      }
    }
  });

  test("produces a name the enumeration knows about", () => {
    // The injected picker makes this deterministic without seeding anything in
    // the shipping path — generateName uses Math.random by design (#402).
    for (const { cls, species } of ALL_PAIRS) {
      const first = generateName(cls, species, () => 0);
      expect(everyNameFor(cls, species)).toContain(first);
    }
  });

  test("the surname reads the class and the given name reads the species", () => {
    // The whole point of 3+3 pools: swapping one axis changes exactly one half.
    const humanFighter = generateName("fighter", "human", () => 0);
    const humanMage = generateName("mage", "human", () => 0);
    const elfFighter = generateName("fighter", "elf", () => 0);

    const given = (n: string): string => n.split(" ")[0] ?? "";
    const surname = (n: string): string => n.split(" ")[1] ?? "";

    expect(given(humanFighter)).toBe(given(humanMage));
    expect(surname(humanFighter)).not.toBe(surname(humanMage));

    expect(surname(humanFighter)).toBe(surname(elfFighter));
    expect(given(humanFighter)).not.toBe(given(elfFighter));
  });

  test("an unknown class or species falls back rather than throwing", () => {
    // A thrown error on the start screen blocks joining; a dull name does not.
    expect(generateName("bard", "human")).toBe("traveler");
    expect(generateName("fighter", "orc")).toBe("traveler");
  });
});
