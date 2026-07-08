// Keeps Pixi off `new Function`, so the server's strict CSP (no unsafe-eval)
// holds. Must load before any other pixi.js import.
import "pixi.js/unsafe-eval";

import { Application, Container } from "pixi.js";

import { bindMovementKeys } from "./input/keys";
import { connectEvents } from "./net/events";
import { fetchMap } from "./net/map";
import { join, submitIntent } from "./net/session";
import type { Hex, TurnEvent } from "./protocol.gen";
import { PlaybackSeconds, TurnSeconds } from "./protocol.gen";
import { EntityLayer } from "./render/entities";
import { neighbor, pixelToHex } from "./render/hex";
import { buildMapLayer } from "./render/map";
import { TurnTimer } from "./ui/timer";

// window.game is the debug/testing surface: Playwright (and a curious human in
// devtools) reads live state through it. Testability is a design rule — every
// feature keeps this in sync. See §6 of the plan.
export interface GameDebug {
  turn: number;
  connected: boolean;
  /** Number of map tiles rendered; 0 until the map layer is on stage. */
  tiles: number;
  /** Entity count from the latest turn bundle. */
  entities: number;
  /** Every entity in the latest bundle, for cross-client observation in tests. */
  positions: { id: number; hex: Hex }[];
  /** This client's entity, server-authoritative position. Null until joined. */
  me: { id: number; hex: Hex } | null;
  /** Runtime turn interval from the latest bundle, in ms. */
  intervalMs: number;
  /** Count of named heartbeat frames received — proves the keep-alive is observable. */
  heartbeats: number;
  /** Current turn phase: animating the last result, or awaiting input. */
  phase: "playback" | "input";
  /** Milliseconds left in the current phase. */
  phaseRemainingMs: number;
  /** The hex this client last asked to walk to; null once reached. */
  destination: Hex | null;
  /** Submit a destination as if the hex were clicked (drives e2e). */
  tapHex: (q: number, r: number) => void;
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

// Turn-phase timing, tracked from wall-clock (performance.now) and reset on
// each turn bundle. window.game.phase is computed on read from these, so it
// reports the true phase at any instant — independent of render-frame cadence,
// which headless CI throttles hard enough that a tick-pushed snapshot could
// miss the short playback window entirely. The DOM bar still animates on the
// ticker (cosmetic); the observable state does not depend on it.
let turnStartedAtMs = 0;
let curIntervalMs = 0;
let curPlaybackMs = 0;

window.game = {
  turn: -1,
  connected: false,
  tiles: 0,
  entities: 0,
  positions: [],
  me: null,
  intervalMs: 0,
  heartbeats: 0,
  get phase(): "playback" | "input" {
    if (curIntervalMs === 0) {
      return "input";
    }

    return performance.now() - turnStartedAtMs < curPlaybackMs ? "playback" : "input";
  },
  get phaseRemainingMs(): number {
    if (curIntervalMs === 0) {
      return 0;
    }

    const t = performance.now() - turnStartedAtMs;

    return t < curPlaybackMs ? curPlaybackMs - t : Math.max(0, curIntervalMs - t);
  },
  destination: null,
  tapHex: () => {},
};

async function start(): Promise<void> {
  const app = new Application();
  await app.init({ background: "#0b0f0b", resizeTo: window, antialias: true });
  document.body.appendChild(app.canvas);

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

  const entityLayer = new EntityLayer(app.ticker);
  world.addChild(entityLayer.container);

  const timer = new TurnTimer(app.ticker);

  const me = await join();
  window.game.me = { id: me.entityId, hex: me.hex };
  const identity = { entityId: me.entityId, token: me.token };

  // walkTo submits a destination and records it for the HUD/tests. The world's
  // answer (movement) only ever arrives via turn bundles. A rejected target
  // (unwalkable / unreachable) never becomes a pending walk, so clear it —
  // unless a newer walkTo has already replaced the destination in the meantime.
  const walkTo = (target: Hex): void => {
    window.game.destination = target;
    void submitIntent(identity, target).then((accepted) => {
      const pending = window.game.destination;
      if (!accepted && pending !== null && pending.q === target.q && pending.r === target.r) {
        window.game.destination = null;
      }
    });
  };

  window.game.tapHex = (q, r): void => walkTo({ q, r });

  connectEvents({
    onTurn: (event: TurnEvent): void => {
      window.game.turn = event.turn;
      window.game.entities = event.entities.length;
      window.game.positions = event.entities.map((e) => ({ id: e.id, hex: e.hex }));
      window.game.intervalMs = event.intervalMs;
      turnEl.textContent = String(event.turn);

      const playbackMs = event.intervalMs * (PlaybackSeconds / TurnSeconds);
      curIntervalMs = event.intervalMs;
      curPlaybackMs = playbackMs;
      turnStartedAtMs = performance.now();

      const mine = event.entities.find((e) => e.id === me.entityId);
      if (mine !== undefined && window.game.me !== null) {
        window.game.me.hex = mine.hex;
        // Arrived at the destination → clear it.
        if (
          window.game.destination !== null &&
          mine.hex.q === window.game.destination.q &&
          mine.hex.r === window.game.destination.r
        ) {
          window.game.destination = null;
        }
      }

      entityLayer.update(event.entities, me.entityId, playbackMs);
      timer.onTurn(event.intervalMs, playbackMs);
    },
    onConnectionChange: (connected: boolean): void => {
      window.game.connected = connected;
      statusEl.dataset["connected"] = String(connected);
      statusEl.textContent = connected ? "connected" : "reconnecting…";
    },
    onHeartbeat: (): void => {
      window.game.heartbeats += 1;
    },
  });

  // Keyboard: a step is a one-hex destination — same code path as a click.
  bindMovementKeys({
    onStep: (dir): void => {
      const from = window.game.me?.hex;
      if (from === undefined) {
        return;
      }
      walkTo(neighbor(from, dir));
    },
  });

  // Click-to-move: canvas point → world point (undo the centering translate) →
  // hex → destination.
  app.canvas.addEventListener("pointerdown", (ev: PointerEvent): void => {
    if (ev.button !== 0) {
      return;
    }

    const rect = app.canvas.getBoundingClientRect();
    const worldX = ev.clientX - rect.left - world.position.x;
    const worldY = ev.clientY - rect.top - world.position.y;
    walkTo(pixelToHex({ x: worldX, y: worldY }));
  });
}

start().catch((err: unknown) => {
  statusEl.textContent = `failed to start: ${String(err)}`;
});
