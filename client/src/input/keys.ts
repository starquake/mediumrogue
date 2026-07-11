import type { Direction } from "../render/hex";

// The movement keys from the plan (§5): QWE on the top row for the northern
// directions, ASD below for the southern ones. Six keys, six hex directions,
// no modifier states. There is no explicit "wait" key — standing still is
// simply not sending an intent.
const KEY_TO_DIRECTION: Record<string, Direction> = {
  KeyQ: "nw",
  KeyW: "n",
  KeyE: "ne",
  KeyA: "sw",
  KeyS: "s",
  KeyD: "se",
};

export interface KeyCallbacks {
  onStep: (dir: Direction) => void;
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

    const dir = KEY_TO_DIRECTION[ev.code];
    if (dir !== undefined) {
      callbacks.onStep(dir);
    }
  });
}
