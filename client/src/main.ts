// Keeps Pixi off `new Function`, so the server's strict CSP (no unsafe-eval)
// holds. Must load before any other pixi.js import.
import "pixi.js/unsafe-eval";

import { Application, Container } from "pixi.js";

import { mountChat } from "./chat/ChatPanel";
import { appendChat, messages as chatMessages, sendChat as storeSendChat, setChatToken } from "./chat/store";
import { bindMovementKeys } from "./input/keys";
import { connectEvents } from "./net/events";
import type { EventsController } from "./net/events";
import { fetchMap } from "./net/map";
import { join, loadIdentity, submitIntent } from "./net/session";
import { mountRoster } from "./party/RosterPanel";
import { setParty } from "./party/store";
import type { Hex, TurnEvent } from "./protocol.gen";
import {
  BowRange,
  ClassFighter,
  ClassMage,
  ClassRogue,
  EntityMonster,
  IntentAttack,
  IntentMove,
  MageRange,
  PlaybackSeconds,
  SpeciesHuman,
  TurnSeconds,
  XPPerLevel,
} from "./protocol.gen";
import { EntityLayer } from "./render/entities";
import { hexDistance, hexToPixel, neighbor, pixelToHex } from "./render/hex";
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
  /** Monster count from the latest turn bundle. */
  monsters: number;
  /** Every entity in the latest bundle, for cross-client observation in tests. */
  positions: { id: number; hex: Hex; kind: string }[];
  /** Current HP by entity id, from the latest bundle — for observing combat in tests. */
  hp: Record<number, number>;
  /** This client's entity's XP, from the latest bundle. 0 until joined. */
  xp: number;
  /** This client's entity's level, from the latest bundle. 1 until joined. */
  level: number;
  /** This client's entity's class ("fighter"/"rogue"/"mage"), from the latest bundle. "" until joined. */
  class: string;
  /** This client's entity's species ("human"/"elf"/"dwarf"), from the latest bundle. "" until joined. */
  species: string;
  /** This client's entity, server-authoritative position. Null until joined. */
  me: { id: number; hex: Hex } | null;
  /** The world container's screen offset — follows `me` so it stays centred. */
  camera: { x: number; y: number };
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
  /** Whether this client's entity is frozen in a combat time bubble right now. */
  inCombat: boolean;
  /**
   * The combat bubble this client is a member of, or null when not in combat.
   * `waitingFor` mirrors the bundle's `waitingForIds` for the bubble.
   */
  bubble: { waitingFor: number[]; patienceRemainingMs: number } | null;
  /**
   * Submit a destination as if the hex were clicked (drives e2e). Returns a
   * promise that resolves once the intent POST has settled, so tests can
   * await a walk (e.g. a path-clearing tap) actually landing server-side
   * before proceeding — callers that don't care are free to ignore it.
   */
  tapHex: (q: number, r: number) => Promise<void>;
  /** This client's chosen display name (chat sender label). "" until joined. */
  name: string;
  /** The global chat log, mirrored live from the chat store's signal. */
  chat: { seq: number; sender: string; text: string }[];
  /** Send a chat line as if typed into the panel (drives e2e). */
  sendChat: (text: string) => Promise<void>;
  /** Names of MY party's members (including me), from the latest bundle. Empty when solo. */
  party: string[];
  /** This client's entity's party id, from the latest bundle. 0 when solo. */
  partyId: number;
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
const statsEl = mustGet("stats");
const turnTimerEl = mustGet("turn-timer");
const combatPanelEl = mustGet("combat-panel");
const combatWaitingEl = mustGet("combat-waiting");
const combatPatienceEl = mustGet("combat-patience");
const classPickerEl = mustGet("class-picker");
const classButtons = Array.from(classPickerEl.querySelectorAll<HTMLButtonElement>("button[data-class]"));
const speciesPickerEl = mustGet("species-picker");
const speciesButtons = Array.from(
  speciesPickerEl.querySelectorAll<HTMLButtonElement>("button[data-species]"),
);
const namePickerEl = mustGet("name-picker");
const nameInputEl = mustGet("name-input") as HTMLInputElement;

// How long this client's entity must be absent from turn bundles before it
// re-joins (see attemptRejoin below) — well above a single coalesced/missed
// bundle, so a normal blip never trips it; only a sustained absence (the
// disconnect-grace sweep really removed the entity) does.
const MISSING_GRACE_MS = 2_000;

// Class picker: a brand-new player (no stored identity) sees this while the
// map/engine load, giving a real window to click before the join call fires
// — a returning player's token already fixes their class server-side (the
// server ignores Class on a token match), so the picker never shows for
// them. Nothing needs to be clicked: the join always fires once assets are
// ready, using whichever class is currently selected (Fighter by default) —
// this keeps a fresh page load joining promptly even if no one ever clicks.
let selectedClass: string = ClassFighter;

function selectClass(cls: string): void {
  selectedClass = cls;
  for (const btn of classButtons) {
    btn.classList.toggle("selected", btn.dataset["class"] === cls);
  }
}

for (const btn of classButtons) {
  btn.addEventListener("click", () => selectClass(btn.dataset["class"] ?? ClassFighter));
}
selectClass(ClassFighter);

// Species picker: mirrors the class picker exactly — same visibility rule
// (brand-new player only; a returning player's token already fixes their
// species server-side), same "nothing needs to be clicked" default (Human).
let selectedSpecies: string = SpeciesHuman;

function selectSpecies(species: string): void {
  selectedSpecies = species;
  for (const btn of speciesButtons) {
    btn.classList.toggle("selected", btn.dataset["species"] === species);
  }
}

for (const btn of speciesButtons) {
  btn.addEventListener("click", () => selectSpecies(btn.dataset["species"] ?? SpeciesHuman));
}
selectSpecies(SpeciesHuman);

// Name field: mirrors the pickers (brand-new player only), but free text
// rather than buttons. Defaults to "traveler" so a fresh page load can still
// join promptly (e2e, or a player who never touches the field) — the input's
// own placeholder communicates the default rather than pre-filling the value,
// so a deliberately-typed name never has to first clear placeholder text.
const DEFAULT_NAME = "traveler";
let selectedName: string = DEFAULT_NAME;

nameInputEl.addEventListener("input", () => {
  const trimmed = nameInputEl.value.trim();
  selectedName = trimmed === "" ? DEFAULT_NAME : trimmed;
});

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
  monsters: 0,
  positions: [],
  hp: {},
  xp: 0,
  level: 1,
  class: "",
  species: "",
  me: null,
  camera: { x: 0, y: 0 },
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
  inCombat: false,
  bubble: null,
  tapHex: (): Promise<void> => Promise.resolve(),
  name: "",
  get chat(): { seq: number; sender: string; text: string }[] {
    return chatMessages();
  },
  sendChat: (text: string): Promise<void> => storeSendChat(text),
  party: [],
  partyId: 0,
};

/** The ranged weapon range for a class, or null for a class with no ranged weapon (fighter). */
function rangedRangeFor(cls: string): number | null {
  if (cls === ClassRogue) {
    return BowRange;
  }
  if (cls === ClassMage) {
    return MageRange;
  }

  return null;
}

/**
 * Decides whether a click on `target` should fire a ranged attack instead of
 * a move. Out of combat, or my class has no ranged weapon (fighter): always
 * a move. In combat with a ranged class: a rogue's bow only fires at a
 * hostile actually standing on the clicked hex, within BowRange — any other
 * click there still walks (mirrors the melee-bump flow). A mage's AoE magic
 * can be aimed at any hex within MageRange — the blast can land on empty
 * ground and still catch nearby hostiles — so any in-range click attacks.
 * Reads window.game (the same state the debug/test surface exposes) rather
 * than closed-over locals, so it stays correct regardless of when it's called.
 */
function isRangedAttackClick(target: Hex): boolean {
  if (!window.game.inCombat) {
    return false;
  }
  const range = rangedRangeFor(window.game.class);
  const me = window.game.me;
  if (range === null || me === null || hexDistance(me.hex, target) > range) {
    return false;
  }
  if (window.game.class === ClassMage) {
    return true;
  }

  return window.game.positions.some(
    (p) => p.kind === EntityMonster && p.hex.q === target.q && p.hex.r === target.r,
  );
}

async function start(): Promise<void> {
  // A brand-new player (no stored identity yet) gets the picker while the
  // map/engine load below — a real window to choose before join() fires.
  const isNewPlayer = loadIdentity() === null;
  classPickerEl.hidden = !isNewPlayer;
  speciesPickerEl.hidden = !isNewPlayer;
  namePickerEl.hidden = !isNewPlayer;

  mountChat(mustGet("chat-root"));
  mountRoster(mustGet("roster-root"));

  const app = new Application();
  await app.init({ background: "#0b0f0b", resizeTo: window, antialias: true });
  document.body.appendChild(app.canvas);

  const world = new Container();
  app.stage.addChild(world);

  const map = await fetchMap();
  world.addChild(buildMapLayer(map));
  window.game.tiles = map.tiles.length;

  const entityLayer = new EntityLayer(app.ticker);
  world.addChild(entityLayer.container);

  // Camera follows my entity's *live* (per-frame interpolated) position rather
  // than snapping to its hex once per turn, so the pan is as smooth as the
  // sprite's own movement. Runs every frame after EntityLayer's tick (added
  // first, so dot.current is already advanced this frame); reading app.screen
  // each frame also keeps it centred across window resizes. Falls back to the
  // origin until my dot exists (pre-join).
  const updateCamera = (): void => {
    const p = entityLayer.myPixel() ?? hexToPixel({ q: 0, r: 0 });
    world.position.set(app.screen.width / 2 - p.x, app.screen.height / 2 - p.y);
    window.game.camera = { x: world.position.x, y: world.position.y };
  };
  updateCamera();
  app.ticker.add(updateCamera);

  const timer = new TurnTimer(app.ticker);

  // The join always fires now, click or not — whichever name/class/species
  // are currently selected ("traveler"/Fighter/Human by default) is what's
  // sent; a returning player's stored choices override class/species
  // regardless (join() itself resends their token, and the server ignores
  // Name/Class/Species on a token match).
  classPickerEl.hidden = true;
  speciesPickerEl.hidden = true;
  namePickerEl.hidden = true;
  const me = await join(selectedName, selectedClass, selectedSpecies);
  window.game.me = { id: me.entityId, hex: me.hex };
  window.game.name = selectedName;
  const identity = { entityId: me.entityId, token: me.token };
  setChatToken(identity.token);

  // Re-join tracking: if this client's entity is absent from turn bundles for
  // a sustained spell, the disconnect-grace sweep removed it server-side (the
  // player was gone too long) — re-join to get a playable (fresh) character
  // back. MISSING_GRACE_MS is deliberately a couple of seconds, well above a
  // single coalesced/missed bundle, so a normal blip never trips it.
  let missingSinceMs: number | null = null;
  let rejoining = false;
  let eventsController: EventsController;

  // attemptRejoin re-sends the (now-orphaned) stored token: the server won't
  // recognize it and mints a fresh entity of the same class (existing
  // behaviour — see session.join()). Adopts the new identity in place (so
  // every closure that captured `identity`/`me` sees the update) and forces
  // the event stream to reconnect with the new token. Guarded by `rejoining`
  // so an in-flight re-join can't be started twice.
  const attemptRejoin = async (): Promise<void> => {
    if (rejoining) {
      return;
    }
    rejoining = true;
    try {
      const rejoinName = window.game.name !== "" ? window.game.name : selectedName;
      const rejoinClass = window.game.class !== "" ? window.game.class : selectedClass;
      const rejoinSpecies = window.game.species !== "" ? window.game.species : selectedSpecies;
      const rejoined = await join(rejoinName, rejoinClass, rejoinSpecies);
      identity.entityId = rejoined.entityId;
      identity.token = rejoined.token;
      me.entityId = rejoined.entityId;
      me.token = rejoined.token;
      me.hex = rejoined.hex;
      window.game.me = { id: rejoined.entityId, hex: rejoined.hex };
      window.game.destination = null;
      setChatToken(identity.token);
      // The token just changed — the stream must reconnect under the new one
      // (a stream opened under the stale token no longer maps to any entity).
      eventsController.reconnect();
    } finally {
      rejoining = false;
    }
  };

  // walkTo submits a move destination and records it for the HUD/tests. The
  // world's answer (movement) only ever arrives via turn bundles. A rejected
  // target (unwalkable / unreachable) never becomes a pending walk, so clear
  // it — unless a newer walkTo has already replaced the destination meanwhile.
  const walkTo = (target: Hex): Promise<void> => {
    window.game.destination = target;

    return submitIntent(identity, target, IntentMove).then((accepted) => {
      const pending = window.game.destination;
      if (!accepted && pending !== null && pending.q === target.q && pending.r === target.r) {
        window.game.destination = null;
      }
    });
  };

  // attackAt fires a ranged attack intent at target: no destination bookkeeping
  // (the attacker doesn't move onto it), just submit and let the turn bundle's
  // HP changes speak for the result.
  const attackAt = (target: Hex): Promise<void> => submitIntent(identity, target, IntentAttack).then(() => undefined);

  // clickTarget is the single decision point shared by canvas clicks and
  // window.game.tapHex, so tapHex genuinely mirrors "as if the hex were
  // clicked" (including the ranged-attack UX) for tests. Out of combat, or
  // for a class with no ranged weapon, this is always a move — identical to
  // the pre-classes behavior.
  const clickTarget = (target: Hex): Promise<void> =>
    isRangedAttackClick(target) ? attackAt(target) : walkTo(target);

  window.game.tapHex = (q, r): Promise<void> => clickTarget({ q, r });

  eventsController = connectEvents(() => identity.token, {
    onTurn: (event: TurnEvent): void => {
      window.game.turn = event.turn;
      window.game.entities = event.entities.length;
      window.game.monsters = event.entities.filter((e) => e.kind === EntityMonster).length;
      window.game.positions = event.entities.map((e) => ({ id: e.id, hex: e.hex, kind: e.kind }));
      window.game.hp = Object.fromEntries(event.entities.map((e) => [e.id, e.hp]));
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

        window.game.xp = mine.xp;
        window.game.level = mine.level;
        window.game.class = mine.class;
        window.game.species = mine.species;
        window.game.name = mine.name;
        const xpIntoLevel = mine.xp % XPPerLevel;
        statsEl.textContent = `Lv ${mine.level} · ${xpIntoLevel}/${XPPerLevel} XP`;
      }

      // Party roster: refreshed every turn from the bundle itself (no separate
      // party-membership stream) — solo (partyId 0) always renders an empty
      // roster, so the panel simply doesn't show.
      const myPartyId = mine?.partyId ?? 0;
      const partyNames =
        myPartyId === 0 ? [] : event.entities.filter((e) => e.partyId === myPartyId).map((e) => e.name);
      setParty(partyNames);
      window.game.party = partyNames;
      window.game.partyId = myPartyId;

      // Absent from this bundle: either a coalesced/momentary blip (ignore —
      // see MISSING_GRACE_MS) or the disconnect-grace sweep really removed
      // this entity (the player was gone too long) — re-join once the
      // absence has been sustained for MISSING_GRACE_MS. Present again →
      // reset the streak, whether that's because it never left or because a
      // re-join just landed a fresh entity.
      if (mine === undefined) {
        missingSinceMs ??= performance.now();
        if (performance.now() - missingSinceMs >= MISSING_GRACE_MS) {
          missingSinceMs = null;
          // Swallow a failed re-join (transient network): the streak restarts and
          // it retries after another MISSING_GRACE_MS — no unhandled rejection.
          void attemptRejoin().catch(() => {});
        }
      } else {
        missingSinceMs = null;
      }

      // A combat bubble freezes this client's turn clock in place of the
      // world's — swap the WeGo timer for a "waiting for…" panel while a
      // member of one, using the bubble's own patience countdown.
      const myBubble = event.bubbles.find((b) => b.memberIds.includes(me.entityId)) ?? null;
      window.game.inCombat = (mine?.inCombat ?? false) || myBubble !== null;
      window.game.bubble =
        myBubble !== null
          ? { waitingFor: myBubble.waitingForIds, patienceRemainingMs: myBubble.patienceRemainingMs }
          : null;

      if (myBubble !== null) {
        turnTimerEl.hidden = true;
        combatPanelEl.hidden = false;
        combatWaitingEl.textContent = myBubble.waitingForIds.join(", ");
        combatPatienceEl.textContent = (myBubble.patienceRemainingMs / 1000).toFixed(1);
      } else {
        combatPanelEl.hidden = true;
        turnTimerEl.hidden = false;
      }

      entityLayer.update(event.entities, me.entityId, mine?.partyId ?? 0, playbackMs);
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
    onChat: (msg): void => {
      appendChat(msg);
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

  // Click-to-move (or, in combat with a ranged class, click-to-attack): canvas
  // point → world point (undo the centering translate) → hex → clickTarget's
  // move-vs-attack decision. A small cursor affordance previews which one a
  // hover would trigger.
  app.canvas.addEventListener("pointerdown", (ev: PointerEvent): void => {
    if (ev.button !== 0) {
      return;
    }

    const rect = app.canvas.getBoundingClientRect();
    const worldX = ev.clientX - rect.left - world.position.x;
    const worldY = ev.clientY - rect.top - world.position.y;
    clickTarget(pixelToHex({ x: worldX, y: worldY }));
  });

  app.canvas.addEventListener("pointermove", (ev: PointerEvent): void => {
    const rect = app.canvas.getBoundingClientRect();
    const worldX = ev.clientX - rect.left - world.position.x;
    const worldY = ev.clientY - rect.top - world.position.y;
    app.canvas.style.cursor = isRangedAttackClick(pixelToHex({ x: worldX, y: worldY })) ? "crosshair" : "default";
  });
}

start().catch((err: unknown) => {
  statusEl.textContent = `failed to start: ${String(err)}`;
});
