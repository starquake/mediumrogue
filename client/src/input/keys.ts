// The camera-survey experiment (#273) dropped the QWEASD hex-direction
// character-movement keys: character movement is now click/tap only
// (clickTarget / window.game.tapHex). #274 settled the camera as a pure
// Grim-Dawn-style FOLLOW camera with mouse-wheel zoom (main.ts) — so there are
// NO camera keys at all: WASD does nothing, there is no pan and nothing to
// recenter. SPACE is still the explicit wait key (item 11, playtest batch 2) —
// the same own-hex move that a click already waits/cancels with; standing still
// without SPACE is still simply not sending an intent.
const WAIT_KEY = "Space";
const INVENTORY_KEY = "KeyI";
// `c` is a second mnemonic for the character panel ("character" / "inventory"),
// alongside `i`: #273 briefly repurposed it to camera-recenter, but #274's pure
// follow camera has nothing to recenter, so `c` is the panel alias again.
const CHARACTER_KEY = "KeyC";
// `k` for the skills panel (#124). NOT `s` — a letter key that sometimes opens a
// panel is exactly the kind of thing that gets reported as "my character froze".
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
   * `i` or `c` (two mnemonics, "inventory" / "character", same action): toggle
   * the character/inventory panel (inventory-slots milestone, gear keystone
   * task 4) — subject to the same typing-target and isBlocked guards as
   * everything here, so typing "i" or "c" into chat never opens it.
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
// panel keys are letters, not just controls, so typing them into chat (or any
// other text field) must never also open a panel (item 10, a real playtest
// bug report).
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
