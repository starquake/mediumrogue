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
}

/** Binds the movement keys for the page's lifetime. */
export function bindMovementKeys(callbacks: KeyCallbacks): void {
  window.addEventListener("keydown", (ev: KeyboardEvent) => {
    if (ev.repeat || ev.ctrlKey || ev.altKey || ev.metaKey) {
      return;
    }

    const dir = KEY_TO_DIRECTION[ev.code];
    if (dir !== undefined) {
      callbacks.onStep(dir);
    }
  });
}
