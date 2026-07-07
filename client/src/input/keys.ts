import type { Direction } from "../render/hex";

// The movement keys from the plan (§5): QWE on the top row, ASD in the
// middle, X below — mapping onto the six flat-top hex directions, with S as
// stay/wait in the center.
const KEY_TO_DIRECTION: Record<string, Direction> = {
  KeyW: "n",
  KeyE: "ne",
  KeyD: "se",
  KeyX: "s",
  KeyA: "sw",
  KeyQ: "nw",
};

export interface KeyCallbacks {
  onStep: (dir: Direction) => void;
  onWait: () => void;
}

/** Binds the movement keys for the page's lifetime. */
export function bindMovementKeys(callbacks: KeyCallbacks): void {
  window.addEventListener("keydown", (ev: KeyboardEvent) => {
    if (ev.repeat || ev.ctrlKey || ev.altKey || ev.metaKey) {
      return;
    }

    if (ev.code === "KeyS") {
      callbacks.onWait();

      return;
    }

    const dir = KEY_TO_DIRECTION[ev.code];
    if (dir !== undefined) {
      callbacks.onStep(dir);
    }
  });
}
