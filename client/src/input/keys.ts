import type { Direction } from "../render/hex";

// The movement keys from the plan (§5): QWE on the top row for the northern
// directions, ASD below for the southern ones. Six keys, six hex directions,
// no modifier states. SPACE is the explicit wait key (item 11, playtest
// batch 2) — the same own-hex move that a click already waits/cancels with;
// standing still without SPACE is still simply not sending an intent.
const KEY_TO_DIRECTION: Record<string, Direction> = {
  KeyQ: "nw",
  KeyW: "n",
  KeyE: "ne",
  KeyA: "sw",
  KeyS: "s",
  KeyD: "se",
};

const WAIT_KEY = "Space";
const INVENTORY_KEY = "KeyI";
const CHARACTER_KEY = "KeyC";
// `k` for the skills panel (#124). NOT `s` — that is already the south
// movement direction, and a movement key that sometimes opens a panel is
// exactly the kind of thing that gets reported as "my character froze".
const SKILLS_KEY = "KeyK";
const ESCAPE_KEY = "Escape";

export interface KeyCallbacks {
  onStep: (dir: Direction) => void;
  /**
   * SPACE: wait (item 11) — the same own-hex move intent a click on my own
   * hex already sends (clears any queued path; inside a bubble it locks in,
   * a normal move intent).
   */
  onWait: () => void;
  /**
   * `i` or `c` (two mnemonics, "inventory" / "character", same action):
   * toggle the character/inventory panel (inventory-slots milestone, gear
   * keystone task 4) — subject to the same typing-target and isBlocked
   * guards as movement, so typing "i" or "c" into chat never opens it.
   */
  onToggleInventory?: () => void;
  /**
   * `k`: toggle the skills panel (#124) — same typing-target and isBlocked
   * guards as everything else here, so typing "k" into chat never opens it.
   * Independent of the inventory panel: they answer different questions
   * ("what am I carrying" vs "what can I become") and a player comparing a
   * skill against their gear wants both.
   */
  onToggleSkills?: () => void;
  /**
   * Escape: close the character/inventory panel (gear keystone task 4) —
   * only invoked while isPanelOpen() reports true, so Escape is a no-op
   * (and never steals the key) while the panel is already closed.
   */
  onClosePanel?: () => void;
  /**
   * Reports whether the character/inventory panel is currently open —
   * gates onClosePanel above (Escape closes it only when open).
   */
  isPanelOpen?: () => boolean;
  /**
   * Reports whether movement keys should be ignored right now, beyond the
   * typing-target guard below — the start screen being visible (item 10,
   * playtest batch 2): a character that doesn't exist yet must never move
   * while its class/species is still being chosen.
   */
  isBlocked?: () => boolean;
}

// isTypingTarget reports whether el is (or would receive) text input —
// wasd/qe are letters, not just movement keys, so typing them into chat (or
// any other text field) must never also walk the character (item 10, a real
// playtest bug report).
function isTypingTarget(el: EventTarget | Element | null): boolean {
  if (!(el instanceof HTMLElement)) {
    return false;
  }

  return el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.isContentEditable;
}

/** Binds the movement keys for the page's lifetime. */
export function bindMovementKeys(callbacks: KeyCallbacks): void {
  window.addEventListener("keydown", (ev: KeyboardEvent) => {
    if (ev.repeat || ev.ctrlKey || ev.altKey || ev.metaKey) {
      return;
    }

    if (isTypingTarget(ev.target) || isTypingTarget(document.activeElement) || (callbacks.isBlocked?.() ?? false)) {
      return;
    }

    if (ev.code === WAIT_KEY) {
      ev.preventDefault(); // SPACE also scrolls/activates a focused button by default
      callbacks.onWait();

      return;
    }

    if ((ev.code === INVENTORY_KEY || ev.code === CHARACTER_KEY) && callbacks.onToggleInventory !== undefined) {
      callbacks.onToggleInventory();

      return;
    }

    if (ev.code === SKILLS_KEY && callbacks.onToggleSkills !== undefined) {
      callbacks.onToggleSkills();

      return;
    }

    if (ev.code === ESCAPE_KEY && callbacks.onClosePanel !== undefined && (callbacks.isPanelOpen?.() ?? false)) {
      callbacks.onClosePanel();

      return;
    }

    const dir = KEY_TO_DIRECTION[ev.code];
    if (dir !== undefined) {
      callbacks.onStep(dir);
    }
  });
}
