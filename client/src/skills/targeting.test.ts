import { describe, expect, it } from "vitest";

import { SkillAimEntity, SkillAimHex, SkillAimSelf } from "../protocol.gen";
import { clickOutcome, pressOutcome } from "./targeting";

describe("pressOutcome", () => {
  it("fires a self-cast on the press", () => {
    // The whole point of the aim field reaching the client: no targeting step.
    expect(pressOutcome(SkillAimSelf)).toEqual({ kind: "fire" });
  });

  it("arms anything that points at something", () => {
    expect(pressOutcome(SkillAimHex)).toEqual({ kind: "arm" });
    expect(pressOutcome(SkillAimEntity)).toEqual({ kind: "arm" });
  });

  it("arms an unknown aim rather than firing it", () => {
    // A newer server naming a kind this build has never heard of must not
    // fire it blind — arming is the recoverable half of the guess.
    expect(pressOutcome("some-future-aim")).toEqual({ kind: "arm" });
  });
});

describe("clickOutcome", () => {
  it("sends a hex-aimed active with no entity id", () => {
    expect(clickOutcome(SkillAimHex, null)).toEqual({ kind: "send", targetEntityId: 0 });
  });

  it("sends a hex-aimed active even where something is standing", () => {
    // A blast aimed at an occupied hex is the NORMAL case — it is where the
    // monsters are — so an occupant must not turn into an entity target.
    expect(clickOutcome(SkillAimHex, 42)).toEqual({ kind: "send", targetEntityId: 0 });
  });

  it("sends an entity-aimed active with the hostile's id", () => {
    expect(clickOutcome(SkillAimEntity, 42)).toEqual({ kind: "send", targetEntityId: 42 });
  });

  it("ignores an entity-aimed click on bare ground", () => {
    // Stays armed: spending the arm on a mis-click would make the player press
    // the slot again to try the click they meant.
    expect(clickOutcome(SkillAimEntity, null)).toEqual({ kind: "ignore" });
  });
});
