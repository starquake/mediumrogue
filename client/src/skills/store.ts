import { createSignal } from "solid-js";

import type { SkillView } from "../protocol.gen";

// The skills store (#124): the panel's state, rebuilt from every turn bundle
// like the gear and quest stores. The server sends only what this player has
// learned or can learn next (near-sighted, enforced server-side), so the
// client never has to decide what to hide — it renders what it is given.

const [skills, setSkillsSignal] = createSignal<SkillView[]>([]);
const [points, setPointsSignal] = createSignal(0);
const [panelOpen, setPanelOpenSignal] = createSignal(false);

export { panelOpen, points, skills };

/** Replace the skill list and point bank from a turn bundle. */
export function setSkills(next: SkillView[], bank: number): void {
  setSkillsSignal(next);
  setPointsSignal(bank);
}

/**
 * Apply an ACCEPTED learn locally, without waiting for the next turn bundle.
 *
 * Learning is not clock-gated: the server commits it immediately under the
 * world mutex (`learnSkillLocked`) and rejects it in combat rather than
 * queueing it. But the panel is rebuilt from turn bundles, so before this the
 * button looked dead for up to a whole turn interval — an immediate action
 * that read as a broken one.
 *
 * Only ever called after the POST resolved TRUE, i.e. the server has already
 * committed; the next bundle then overwrites this with the authoritative
 * state. It marks the skill learned and debits the bank — it deliberately
 * does NOT reveal whatever the skill unlocks next, because near-sightedness
 * is decided server-side and guessing it here would be a second, divergent
 * implementation of that rule.
 */
export function applyLearnedLocally(skillId: string, cost: number): void {
  setSkillsSignal((prev) => prev.map((s) => (s.id === skillId ? { ...s, learned: true } : s)));
  setPointsSignal((bank) => Math.max(0, bank - cost));
}

/** Open/close the panel (the `k` key — `s` is already a movement key). */
export function toggleSkillsPanel(): void {
  setPanelOpenSignal((open) => !open);
}

/** Force the panel shut — used when the start screen takes over. */
export function closeSkillsPanel(): void {
  setPanelOpenSignal(false);
}

/** The three trees, in display order. Mirrors the engine's tree ids. */
export const TREES: { id: string; label: string }[] = [
  { id: "class", label: "Class" },
  { id: "adventure", label: "Adventure" },
  { id: "survival", label: "Survival" },
];
