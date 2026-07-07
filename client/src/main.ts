import { connectEvents } from "./net/events";
import type { TurnEvent } from "./protocol.gen";

// window.game is the debug/testing surface: Playwright (and a curious human
// in devtools) reads live state through it. Testability is a design rule —
// every feature keeps this in sync. See §6 of the plan.
export interface GameDebug {
  turn: number;
  connected: boolean;
}

declare global {
  interface Window {
    game: GameDebug;
  }
}

function mustGet(id: string): HTMLElement {
  const el = document.getElementById(id);
  if (el === null) {
    throw new Error(`required element #${id} missing from index.html`);
  }

  return el;
}

const turnEl = mustGet("turn");
const statusEl = mustGet("status");

window.game = { turn: -1, connected: false };

connectEvents({
  onTurn: (event: TurnEvent): void => {
    window.game.turn = event.turn;
    turnEl.textContent = String(event.turn);
  },
  onConnectionChange: (connected: boolean): void => {
    window.game.connected = connected;
    statusEl.dataset["connected"] = String(connected);
    statusEl.textContent = connected ? "connected" : "reconnecting…";
  },
});
