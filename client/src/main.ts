// Keeps Pixi off `new Function`, so the server's strict CSP (no unsafe-eval)
// holds. Must load before any other pixi.js import.
import "pixi.js/unsafe-eval";

import { Application, Container } from "pixi.js";

import { connectEvents } from "./net/events";
import { fetchMap } from "./net/map";
import type { TurnEvent } from "./protocol.gen";
import { buildMapLayer } from "./render/map";

// window.game is the debug/testing surface: Playwright (and a curious human
// in devtools) reads live state through it. Testability is a design rule —
// every feature keeps this in sync. See §6 of the plan.
export interface GameDebug {
  turn: number;
  connected: boolean;
  /** Number of map tiles rendered; 0 until the map layer is on stage. */
  tiles: number;
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

window.game = { turn: -1, connected: false, tiles: 0 };

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

async function start(): Promise<void> {
  const app = new Application();
  await app.init({ background: "#0b0f0b", resizeTo: window, antialias: true });
  document.body.appendChild(app.canvas);

  // World container: children live in world pixels around hex (0,0); the
  // container itself centers that origin in the viewport. Camera moves
  // (later milestones) are transforms on this container.
  const world = new Container();
  app.stage.addChild(world);

  const center = (): void => {
    world.position.set(app.screen.width / 2, app.screen.height / 2);
  };
  center();
  app.renderer.on("resize", center);

  const map = await fetchMap();
  world.addChild(buildMapLayer(map));
  window.game.tiles = map.tiles.length;
}

start().catch((err: unknown) => {
  statusEl.textContent = `failed to start: ${String(err)}`;
});
