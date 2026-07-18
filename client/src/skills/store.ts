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
