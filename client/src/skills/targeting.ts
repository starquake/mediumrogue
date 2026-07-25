import { SkillAimEntity, SkillAimSelf } from "../protocol.gen";

// targeting.ts (#300): what the action bar does when you press an active, and
// what the next map click means.
//
// Three behaviour kinds shipped with three different flows, and the rules are
// small enough to be invisible in main.ts's click handler and wrong in a way
// nothing would catch. They live here as pure functions so they can be tested
// at all — main.ts's arming state is closure-local and unreachable from a
// test, and an e2e cannot reach it either (the e2e server grants a fresh join
// no skill points, so no active is ever learnable there).
//
// The aim itself is NOT decided here: it is a property of the skill's server
// behaviour kind, sent on SkillView.aim. This module only routes on it.

/** What pressing an action-bar slot should do. */
export type PressOutcome =
  /** Send it now — a self-cast has nothing to aim at. */
  | { kind: "fire" }
  /** Arm it and wait for a map click. */
  | { kind: "arm" };

/** What a map click means while an active is armed. */
export type ClickOutcome =
  /** Send the intent. targetEntityId is 0 for a hex-aimed active. */
  | { kind: "send"; targetEntityId: number }
  /** Not a usable target — stay armed and wait for a better click. */
  | { kind: "ignore" };

/**
 * pressOutcome decides whether a press fires or arms.
 *
 * A self-cast fires immediately: an arm-then-click-yourself flow would be a
 * worse version of the button the player just pressed, and there is no hex
 * whose selection could add information.
 */
export function pressOutcome(aim: string): PressOutcome {
  return aim === SkillAimSelf ? { kind: "fire" } : { kind: "arm" };
}

/**
 * clickOutcome decides what an armed active does with a map click.
 *
 * An entity-aimed active sends an entity id, so a click on bare ground carries
 * nothing it can use. It stays ARMED rather than spending the arm on a
 * mis-click: the server would only reject it, and the player would then have
 * to press the slot again to try the click they meant.
 *
 * A hex-aimed active takes any hex — including an occupied one, which for a
 * blast is the normal case. Whether it is a legal target is the server's call
 * (range, sight, and for a blink walkability), surfaced as a rejection toast.
 */
export function clickOutcome(aim: string, hostileIdAtTarget: number | null): ClickOutcome {
  if (aim !== SkillAimEntity) return { kind: "send", targetEntityId: 0 };

  return hostileIdAtTarget === null ? { kind: "ignore" } : { kind: "send", targetEntityId: hostileIdAtTarget };
}
