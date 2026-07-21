// The camera-survey experiment (#273) dropped the QWEASD hex-direction
// character-movement keys: character movement is now click/tap only
// (clickTarget / window.game.tapHex). WASD instead pans the survey camera
// (see bindCameraKeys below); Q and E are unbound. SPACE is still the explicit
// wait key (item 11, playtest batch 2) — the same own-hex move that a click
// already waits/cancels with; standing still without SPACE is still simply not
// sending an intent.
const WAIT_KEY = "Space";
const INVENTORY_KEY = "KeyI";
// `c` used to be a second mnemonic for the character panel; #273 reassigned it
// to RECENTER the survey camera on the player, so the panel is `i`-only now.
const RECENTER_KEY = "KeyC";
// `k` for the skills panel (#124). NOT `s` — that pans the camera (#273), and a
// pan key that sometimes opens a panel is exactly the kind of thing that gets
// reported as "my character froze".
const SKILLS_KEY = "KeyK";
const HELP_KEY = "Slash"; // "?" is Shift+/ — the physical key is Slash (#203)
const ACTION_KEYS: Record<string, number> = { Digit1: 0, Digit2: 1, Digit3: 2, Digit4: 3 }; // action bar (#185)
const ESCAPE_KEY = "Escape";

export interface KeyCallbacks {
  /**
   * SPACE: wait (item 11) — the same own-hex move intent a click on my own
   * hex already sends (clears any queued path; inside a bubble it locks in,
   * a normal move intent).
   */
  onWait: () => void;
  /**
   * `i`: toggle the character/inventory panel (inventory-slots milestone, gear
   * keystone task 4) — subject to the same typing-target and isBlocked guards
   * as everything here, so typing "i" into chat never opens it. Was also `c`
   * until #273 handed `c` to onRecenter.
   */
  onToggleInventory?: () => void;
  /**
   * `c`: recenter the survey camera on the player (#273), discarding any WASD
   * pan offset. Same typing/isBlocked guards; a no-op provider is fine.
   */
  onRecenter?: () => void;
  /**
   * `k`: toggle the skills panel (#124) — same typing-target and isBlocked
   * guards as everything else here, so typing "k" into chat never opens it.
   * Independent of the inventory panel: they answer different questions
   * ("what am I carrying" vs "what can I become") and a player comparing a
   * skill against their gear wants both.
   */
  onToggleSkills?: () => void;
  /** "?" (Slash): toggle the controls overlay (#203). Same typing guards. */
  onToggleHelp?: () => void;
  /** Keys 1–4: trigger action-bar slot 0–3 (#185). Same typing guards. */
  onActionSlot?: (slot: number) => void;
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

// isTypingTarget reports whether el is (or would receive) text input — the
// pan/panel keys are letters, not just controls, so typing them into chat (or
// any other text field) must never also move the camera or open a panel (item
// 10, a real playtest bug report).
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

    if (ev.code === INVENTORY_KEY && callbacks.onToggleInventory !== undefined) {
      callbacks.onToggleInventory();

      return;
    }

    if (ev.code === RECENTER_KEY && callbacks.onRecenter !== undefined) {
      callbacks.onRecenter();

      return;
    }

    if (ev.code === SKILLS_KEY && callbacks.onToggleSkills !== undefined) {
      callbacks.onToggleSkills();

      return;
    }

    if (ev.code === HELP_KEY && callbacks.onToggleHelp !== undefined) {
      callbacks.onToggleHelp();

      return;
    }

    const slot = ACTION_KEYS[ev.code];
    if (slot !== undefined && callbacks.onActionSlot !== undefined) {
      callbacks.onActionSlot(slot);

      return;
    }

    if (ev.code === ESCAPE_KEY && callbacks.onClosePanel !== undefined && (callbacks.isPanelOpen?.() ?? false)) {
      callbacks.onClosePanel();

      return;
    }
  });
}

// WASD → a survey-camera pan axis (#273). Each held key reveals more of the
// world in that compass direction; the axis is integrated into a persistent
// pan offset by the render ticker (main.ts), so the FEEL of the pan speed lives
// there, not here. Values are screen-space intent: +x reveals WEST (world
// slides right), +y reveals NORTH (world slides down) — the inverse of the
// centering translate.
const PAN_KEYS: Record<string, { x: number; y: number }> = {
  KeyW: { x: 0, y: 1 }, // reveal north
  KeyS: { x: 0, y: -1 }, // reveal south
  KeyA: { x: 1, y: 0 }, // reveal west
  KeyD: { x: -1, y: 0 }, // reveal east
};

export interface CameraKeyGuards {
  /**
   * Same start-screen guard the movement keys use: no panning while the
   * character is still being chosen (or a text field has focus — that guard is
   * built in). Optional; absent means "never blocked".
   */
  isBlocked?: () => boolean;
}

export interface CameraPan {
  /**
   * The current pan intent from the WASD keys held RIGHT NOW, each component
   * the sum of the held keys' contributions (so opposing keys cancel). Read
   * once per frame by the ticker.
   */
  panAxis: () => { x: number; y: number };
}

/**
 * Binds WASD as a held-state survey-camera pan for the page's lifetime (#273),
 * returning a live accessor for the current pan axis. Keyup and window-blur
 * both release keys, so a key held across an alt-tab can't get stuck down.
 */
export function bindCameraKeys(guards: CameraKeyGuards = {}): CameraPan {
  const held = new Set<string>();

  window.addEventListener("keydown", (ev: KeyboardEvent) => {
    if (ev.ctrlKey || ev.altKey || ev.metaKey || PAN_KEYS[ev.code] === undefined) {
      return;
    }
    // Same guards as the rest: typing WASD into chat pans nothing, and neither
    // does WASD while the start screen is up.
    if (isTypingTarget(ev.target) || isTypingTarget(document.activeElement) || (guards.isBlocked?.() ?? false)) {
      return;
    }
    held.add(ev.code);
  });
  window.addEventListener("keyup", (ev: KeyboardEvent) => {
    held.delete(ev.code);
  });
  window.addEventListener("blur", () => {
    held.clear();
  });

  return {
    panAxis: (): { x: number; y: number } => {
      let x = 0;
      let y = 0;
      for (const code of held) {
        const v = PAN_KEYS[code]!;
        x += v.x;
        y += v.y;
      }

      return { x, y };
    },
  };
}
