import { describe, expect, it } from "vitest";

import {
  ClassFighter,
  ClassMage,
  ClassRogue,
  SpeciesDwarf,
  SpeciesElf,
  SpeciesHuman,
} from "../protocol.gen";
import { displayLabel } from "./labels";

// The names the START SCREEN prints on its cards (index.html, .card-name).
// displayLabel derives rather than looks up (see labels.ts), and this is what
// keeps the derivation honest: if a future class id stops capitalizing into its
// own card name — "half-elf", say, or an id that wants different casing — this
// fails and the lookup table becomes the right answer for that id.
const CARD_NAMES: ReadonlyArray<[string, string]> = [
  [ClassFighter, "Fighter"],
  [ClassRogue, "Rogue"],
  [ClassMage, "Mage"],
  [SpeciesHuman, "Human"],
  [SpeciesElf, "Elf"],
  [SpeciesDwarf, "Dwarf"],
];

describe("displayLabel", () => {
  it("renders every class and species as the start screen names it", () => {
    for (const [id, want] of CARD_NAMES) {
      expect(displayLabel(id)).toBe(want);
    }
  });

  it("passes an empty id through, so a pre-bundle HUD shows nothing", () => {
    expect(displayLabel("")).toBe("");
  });
});
