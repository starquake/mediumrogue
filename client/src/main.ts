// Keeps Pixi off `new Function`, so the server's strict CSP (no unsafe-eval)
// holds. Must load before any other pixi.js import.
import "pixi.js/unsafe-eval";

import { Application, Container } from "pixi.js";

import { bindMovementKeys } from "./input/keys";
import { connectEvents } from "./net/events";
import { fetchMap } from "./net/map";
import { join, submitIntent } from "./net/session";
import type { Hex, TurnEvent } from "./protocol.gen";
import { EntityLayer } from "./render/entities";
import { neighbor } from "./render/hex";
import { buildMapLayer } from "./render/map";

// window.game is the debug/testing surface: Playwright (and a curious human
// in devtools) reads live state through it. Testability is a design rule —
// every feature keeps this in sync. See §6 of the plan.
export interface GameDebug {
  turn: number;
  connected: boolean;
  /** Number of map tiles rendered; 0 until the map layer is on stage. */
  tiles: number;
  /** Entity count from the latest turn bundle. */
  entities: number;
  /** This client's entity, server-authoritative position. Null until joined. */
  me: { id: number; hex: Hex } | null;
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

window.game = { turn: -1, connected: false, tiles: 0, entities: 0, me: null };

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

  const entityLayer = new EntityLayer();
  world.addChild(entityLayer.container);

  const me = await join();
  window.game.me = { id: me.entityId, hex: me.hex };

  connectEvents({
    onTurn: (event: TurnEvent): void => {
      window.game.turn = event.turn;
      window.game.entities = event.entities.length;
      turnEl.textContent = String(event.turn);

      const mine = event.entities.find((e) => e.id === me.entityId);
      if (mine !== undefined && window.game.me !== null) {
        window.game.me.hex = mine.hex;
      }

      entityLayer.update(event.entities, me.entityId);
    },
    onConnectionChange: (connected: boolean): void => {
      window.game.connected = connected;
      statusEl.dataset["connected"] = String(connected);
      statusEl.textContent = connected ? "connected" : "reconnecting…";
    },
  });

  const identity = { entityId: me.entityId, token: me.token };
  bindMovementKeys({
    onStep: (dir): void => {
      const from = window.game.me?.hex;
      if (from === undefined) {
        return;
      }

      // Fire and forget: a rejection (wall, water) simply means no intent is
      // queued; the world's answer arrives in the next turn bundle either way.
      void submitIntent(identity, neighbor(from, dir));
    },
  });
}

start().catch((err: unknown) => {
  statusEl.textContent = `failed to start: ${String(err)}`;
});
