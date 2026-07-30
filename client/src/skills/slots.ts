// slots.ts (#304): which active sits in which action-bar slot.
//
// The bar stays at FOUR slots (maintainer's call) while the game ships five
// actives, so the assignment is a player CHOICE rather than an overflow bug:
// right-clicking a slot picks what lives there, and a skill you have not
// slotted is simply unslotted.
//
// Stored client-side in localStorage, like the mute setting — it is a UI
// preference, not world state, so it needs no wire field and no snapshot bump.
// The consequence is that it does not follow you to another browser.
//
// Every rule here is a pure function on purpose: the e2e server grants a fresh
// join no skill points, so no active is ever learnable in a browser test and a
// spec covering this could only ever skip.

/** SLOT_COUNT is the bar's fixed width — four slots, four keybinds (1-4). */
export const SLOT_COUNT = 4;

/** EMPTY marks a deliberately blank slot, distinct from "never assigned". */
export const EMPTY = "";

const STORAGE_KEY = "mediumrogue.actionSlots";

/**
 * defaultAssignment fills the slots with the first SLOT_COUNT learned actives,
 * in the order given (the registry's, as the wire sends it) — so a player who
 * has learned four or fewer never has to touch the menu at all.
 */
export function defaultAssignment(learnedActiveIDs: string[]): string[] {
  const out = Array.from({ length: SLOT_COUNT }, () => EMPTY);

  for (let i = 0; i < Math.min(SLOT_COUNT, learnedActiveIDs.length); i++) {
    out[i] = learnedActiveIDs[i] ?? EMPTY;
  }

  return out;
}

/**
 * reconcile takes a stored assignment and the currently learned actives, and
 * returns the assignment to actually render.
 *
 * Two rules, both about not surprising the player:
 *
 *  - A slot naming a skill that is NOT learned is emptied. A stored assignment
 *    outlives the character it was made for (localStorage is per browser, not
 *    per character), so a dead slot is otherwise the normal case after a
 *    reroll, not an edge one.
 *  - Learning a new active never DISPLACES an assigned one. It fills empty
 *    slots only. Silently pushing Evade out of slot 1 because you learned
 *    something else is the kind of thing that gets noticed mid-fight.
 */
export function reconcile(stored: string[], learnedActiveIDs: string[]): string[] {
  const learned = new Set(learnedActiveIDs);
  const out = Array.from({ length: SLOT_COUNT }, (_, i) => {
    const want = stored[i] ?? EMPTY;

    return want !== EMPTY && learned.has(want) ? want : EMPTY;
  });

  // Fill any gaps with learned actives that are not already placed, so a fresh
  // player (nothing stored) and a player who just learned their first active
  // both end up with a usable bar without opening the menu.
  const placed = new Set(out.filter((s) => s !== EMPTY));

  for (const id of learnedActiveIDs) {
    if (placed.has(id)) continue;

    const gap = out.indexOf(EMPTY);
    if (gap === -1) break; // bar full — the rest stay unslotted, by design

    out[gap] = id;
    placed.add(id);
  }

  return out;
}

/**
 * assign puts id into slot, and clears it from any OTHER slot first so one
 * skill can never occupy two — which would silently cost the player a slot.
 * Pass EMPTY to blank a slot.
 */
export function assign(current: string[], slot: number, id: string): string[] {
  if (slot < 0 || slot >= SLOT_COUNT) return current;

  const out = current.map((s) => (id !== EMPTY && s === id ? EMPTY : s));
  out[slot] = id;

  return out;
}

/** unslotted lists learned actives that no slot currently holds. */
export function unslotted(current: string[], learnedActiveIDs: string[]): string[] {
  const placed = new Set(current.filter((s) => s !== EMPTY));

  return learnedActiveIDs.filter((id) => !placed.has(id));
}

/** load reads the stored assignment; an unreadable store is simply "nothing stored". */
export function load(): string[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw === null) return [];

    const parsed: unknown = JSON.parse(raw);

    return Array.isArray(parsed) ? parsed.filter((v): v is string => typeof v === "string") : [];
  } catch {
    // Private-mode browsers throw on localStorage, and a hand-edited value can
    // be any shape. Neither is worth breaking the action bar over.
    return [];
  }
}

/** save persists the assignment. Best-effort, for the same reasons as load. */
export function save(assignment: string[]): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(assignment));
  } catch {
    // Session-only if storage is unavailable.
  }
}
