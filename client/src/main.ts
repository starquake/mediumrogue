// Keeps Pixi off `new Function`, so the server's strict CSP (no unsafe-eval)
// holds. Must load before any other pixi.js import.
import "pixi.js/unsafe-eval";

import { Application, Container } from "pixi.js";

import { mountChat } from "./chat/ChatPanel";
import { appendChat, messages as chatMessages, sendChat as storeSendChat, setChatToken } from "./chat/store";
import { mountCharacter } from "./gear/CharacterPanel";
import { mountPickup } from "./gear/PickupModal";
import {
  backpack as backpackSignal,
  clearPending,
  equipped as equippedSignal,
  markPending,
  markPickupRejected,
  modalOpen,
  panelOpen,
  pickupRows,
  refreshPickup,
  setInventory,
  setWeaponSlots,
  togglePanel,
} from "./gear/store";
import { bindMovementKeys } from "./input/keys";
import { connectEvents } from "./net/events";
import type { EventsController } from "./net/events";
import { fetchMap } from "./net/map";
import {
  clearIdentity,
  importIdentityFromFragment,
  join,
  JoinRejectedError,
  loadIdentity,
  onForeignIdentityChange,
  reclaim,
  submitDrink,
  submitDrop,
  submitEquip,
  submitIntent,
  submitPickup,
  submitUnequip,
} from "./net/session";
import { mountRoster } from "./party/RosterPanel";
import { setParty } from "./party/store";
import type { GroundItemView, Hex, ItemView, QuestView, TurnEvent } from "./protocol.gen";
import { mountQuests } from "./quest/QuestPanel";
import { setQuests } from "./quest/store";
import {
  ClassFighter,
  ClassMage,
  ClassRogue,
  CombatRadius,
  EntityMonster,
  EntityPlayer,
  IntentAttack,
  IntentMove,
  ItemTypeMeleeWeapon,
  ItemTypeRangedWeapon,
  ItemTypeStaff,
  ItemTypeThrownWeapon,
  ItemTypeWand,
  PlaybackSeconds,
  SpeciesHuman,
  StackCap,
  TerrainForest,
  TerrainGrass,
  TurnSeconds,
  XPPerLevel,
} from "./protocol.gen";
import { DamageNumberLayer } from "./render/damage";
import { EntityLayer } from "./render/entities";
import type { CommittedAction } from "./render/feedback";
import { FeedbackLayer } from "./render/feedback";
import { DIRECTIONS, hexDistance, hexToPixel, neighbor, pixelToHex } from "./render/hex";
import { GroundItemLayer } from "./render/items";
import { MoveRangeLayer } from "./render/range";
import { buildMapLayer } from "./render/map";
import { QuestMarkerLayer } from "./render/questmarker";
import { TurnTimer } from "./ui/timer";

// Strip a `#t=<token>` character-link fragment and adopt its identity before
// anything else in this module runs — see importIdentityFromFragment's doc
// comment (net/session.ts) for why this must happen this early.
importIdentityFromFragment();

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
  /**
   * Every entity in the latest bundle, for cross-client observation in
   * tests. monsterKind is the monster-kind registry id ("wolf", "dragon",
   * ...), empty for a player — lets an e2e spec assert distinct kinds
   * actually rendered (milestone 6c). name is the display name (a player's
   * chosen name, or a monster kind's display name — "Wolf", "Dragon" — used
   * by the enemy hover tooltip, item 13).
   */
  positions: { id: number; hex: Hex; kind: string; monsterKind: string; name: string }[];
  /** Current HP by entity id, from the latest bundle — for observing combat in tests. */
  hp: Record<number, number>;
  /**
   * Max HP by entity id, from the latest bundle — drives the enemy hover
   * tooltip's "HP cur/max" (item 13, playtest batch 2).
   */
  maxHp: Record<number, number>;
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
  /**
   * The copyable character link for this client's identity —
   * `<origin>/#t=<token>` — or "" until joined. Opening this URL on any
   * browser/device imports the token (net/session.ts's
   * importIdentityFromFragment) and rejoins the SAME character. Exposed for
   * e2e (client/e2e/identity.spec.ts): a test reads this directly rather
   * than driving the copy-to-clipboard button, since clipboard permissions
   * are extra ceremony a headless browser doesn't need for the round trip
   * that actually matters — the link string itself, and the join it drives.
   */
  identityLink: string;
  /**
   * Force one attemptRejoin pass NOW (drives e2e — the real trigger, my
   * entity being absent from bundles for MISSING_GRACE_MS after a
   * disconnect-grace sweep, is impractical to arrange in a browser test).
   * Same code path as the organic trigger: reclaim() with this tab's own
   * in-memory identity. Null until joined.
   */
  forceRejoin: (() => Promise<void>) | null;
  /** The global chat log, mirrored live from the chat store's signal. */
  chat: { seq: number; sender: string; text: string }[];
  /** Send a chat line as if typed into the panel (drives e2e). */
  sendChat: (text: string) => Promise<void>;
  /** Names of MY party's members (including me), from the latest bundle. Empty when solo. */
  party: string[];
  /** This client's entity's party id, from the latest bundle. 0 when solo. */
  partyId: number;
  /**
   * My FIRST active quest (taken by me or my party), from the latest
   * bundle — null when I hold none. Kept for backward compatibility with
   * the single-quest model; see myQuests for the full list (item 14,
   * playtest batch 2: I may hold several personal quests concurrently,
   * plus my party's, if any).
   */
  quest: QuestView | null;
  /** Every quest currently active for me (personal, plural, plus my party's if any), from the latest bundle. */
  myQuests: QuestView[];
  /** The whole quest board, from the latest bundle. */
  quests: QuestView[];
  /**
   * My FIRST active reach quest's goal hex, or null. Kept for backward
   * compatibility; see questGoalMarkers for every active reach quest's goal
   * (item 14). Drives QuestMarkerLayer (item 12); exposed for e2e since the
   * marker itself is only a canvas draw.
   */
  questGoalMarker: Hex | null;
  /** Every active reach quest's goal hex, keyed by quest id (item 14, playtest batch 2). */
  questGoalMarkers: { id: number; hex: Hex }[];
  /** This client's entity's owned items (id/defId/equipped), from the latest bundle. Empty until joined. */
  inventory: { id: number; defId: string; equipped: boolean }[];
  /**
   * My equipped gear keyed by slot (itemType) — the paper-doll's filled
   * slots, from the latest bundle. Empty slots are absent keys. Exposed for
   * e2e (the panel itself is DOM).
   */
  equipped: Record<string, { id: number; defId: string; name: string; type: string }>;
  /**
   * My backpack: BackpackSize entries, left-packed (nulls trail). A
   * consumable entry carries count>1. Exposed for e2e.
   */
  backpack: ({ id: number; defId: string; name: string; type: string; count: number } | null)[];
  /** Whether the character/paper-doll panel is open (the `i` key / HUD button). */
  panelOpen: boolean;
  /**
   * The per-hex pickup modal: whether it is open plus its rows (each a
   * ground item's id/name/type and whether a take was rejected). Exposed for
   * e2e; the modal itself is DOM.
   */
  pickupModal: { open: boolean; rows: { id: number; name: string; type: string; rejected: boolean }[] };
  /** Every item lying on the ground, from the latest bundle. */
  groundItems: { id: number; hex: Hex }[];
  /**
   * Damage inferred from the latest bundle by diffing HP against the previous
   * one (the wire carries state, not hit events): one entry per entity that
   * lost HP, plus one per monster that vanished (its killing blow, shown as
   * the HP it had left). Drives the floating combat numbers; exposed for e2e.
   */
  damage: { id: number; amount: number }[];
  /**
   * The hexes my entity can act on THIS combat turn (moves + bump-attacks),
   * from the latest bundle. Empty outside a bubble — out there, click-anywhere
   * pathing applies and no restriction exists. Drives the tactical overlay
   * and the in-combat click filter; exposed for e2e.
   */
  combatMoves: Hex[];
  /**
   * The hexes within my equipped ranged weapon's reach this combat turn
   * (excluding move/bump tiles — those act differently on click). Clicking
   * one shoots when a hostile stands there (or regardless, for AoE). Empty
   * outside a bubble or with no ranged weapon. Drives the red range wash.
   */
  combatRanged: Hex[];
  /**
   * What I committed to THIS bubble-turn — move/attack/wait plus its target
   * hex — or null when I haven't acted yet (or it already resolved). Set the
   * moment an intent is submitted while inCombat (item 6); cleared on the
   * next turn bundle, or by a later intent that replaces it (an equip
   * supersedes a queued move/attack server-side the same way). Drives the
   * committed-action indicator (FeedbackLayer.setCommitted); exposed for e2e.
   */
  committedAction: CommittedAction | null;
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

// weaponSlotsForClass mirrors internal/game's weaponSlotsFor: the two
// class-shaped weapon-slot itemTypes ([close-ish, ranged-ish]) the paper-doll
// labels and places. Fighter's thrown slot ships empty (no thrown content),
// so a fighter's ranged-ish slot always renders empty — matching the server.
function weaponSlotsForClass(cls: string): [string, string] {
  switch (cls) {
    case ClassRogue:
      return [ItemTypeMeleeWeapon, ItemTypeRangedWeapon];
    case ClassMage:
      return [ItemTypeStaff, ItemTypeWand];
    default: // fighter (and any unknown class): melee + (empty) thrown
      return [ItemTypeMeleeWeapon, ItemTypeThrownWeapon];
  }
}

const turnEl = mustGet("turn");
const statusEl = mustGet("status");
const statsEl = mustGet("stats");
const copyLinkEl = mustGet("copy-link") as HTMLButtonElement;
const toggleInventoryEl = mustGet("toggle-inventory") as HTMLButtonElement;
const turnTimerEl = mustGet("turn-timer");
const combatPanelEl = mustGet("combat-panel");
const combatWaitingEl = mustGet("combat-waiting");
const combatPatienceEl = mustGet("combat-patience");
const startScreenEl = mustGet("start-screen");
const startNameEl = mustGet("start-name") as HTMLInputElement;
const startEnterEl = mustGet("start-enter") as HTMLButtonElement;
const classCards = Array.from(startScreenEl.querySelectorAll<HTMLElement>(".card[data-class]"));
const speciesCards = Array.from(startScreenEl.querySelectorAll<HTMLElement>(".card[data-species]"));

// Enemy hover tooltip (item 13, playtest batch 2).
const hoverTooltipEl = mustGet("hover-tooltip");
const hoverTooltipKindEl = mustQuery(hoverTooltipEl, ".tooltip-kind");
const hoverTooltipHPEl = mustQuery(hoverTooltipEl, ".tooltip-hp");

function mustQuery(root: HTMLElement, selector: string): HTMLElement {
  const el = root.querySelector<HTMLElement>(selector);
  if (el === null) {
    throw new Error(`required element ${selector} missing under #${root.id}`);
  }

  return el;
}

// How long this client's entity must be absent from turn bundles before it
// re-joins (see attemptRejoin below) — well above a single coalesced/missed
// bundle, so a normal blip never trips it; only a sustained absence (the
// disconnect-grace sweep really removed the entity) does.
const MISSING_GRACE_MS = 2_000;

// Start screen: a brand-new player (no stored identity) sees this while the
// map/engine load, giving a real window to pick a class/species/name before
// the join call fires — a returning player's token already fixes their class
// and species server-side (the server ignores Class/Species on a token
// match), so the screen never shows for them; see isNewPlayer in start().
let selectedClass: string = ClassFighter;

function selectClass(cls: string): void {
  selectedClass = cls;
  for (const card of classCards) {
    card.classList.toggle("selected", card.dataset["class"] === cls);
  }
}

for (const card of classCards) {
  card.addEventListener("click", () => selectClass(card.dataset["class"] ?? ClassFighter));
}
selectClass(ClassFighter);

// Species cards mirror the class cards exactly — same visibility rule
// (brand-new player only; a returning player's token already fixes their
// species server-side), same Human default.
let selectedSpecies: string = SpeciesHuman;

function selectSpecies(species: string): void {
  selectedSpecies = species;
  for (const card of speciesCards) {
    card.classList.toggle("selected", card.dataset["species"] === species);
  }
}

for (const card of speciesCards) {
  card.addEventListener("click", () => selectSpecies(card.dataset["species"] ?? SpeciesHuman));
}
selectSpecies(SpeciesHuman);

// Name field: free text rather than cards. Defaults to "traveler" so a fresh
// page load can still join with a sensible name (e.g. a test that never
// touches the field) — the input's own placeholder communicates the default
// rather than pre-filling the value, so a deliberately-typed name never has
// to first clear placeholder text.
const DEFAULT_NAME = "traveler";
let selectedName: string = DEFAULT_NAME;

function readStartName(): string {
  const trimmed = startNameEl.value.trim();

  return trimmed === "" ? DEFAULT_NAME : trimmed;
}

startNameEl.addEventListener("input", () => {
  selectedName = readStartName();
});

/**
 * Resolves once a brand-new player commits to their choices — clicking
 * "Enter the world", or pressing Enter in the name field. Re-reads (and
 * trims/defaults) the name field one more time at that instant, so a value
 * typed without triggering the `input` listener (unlikely, but cheap
 * insurance) is still captured. Never shown for a returning player — see
 * isNewPlayer in start(), which skips awaiting this entirely.
 */
function waitForEnter(): Promise<void> {
  return new Promise((resolve) => {
    const onEnter = (): void => {
      selectedName = readStartName();
      startEnterEl.removeEventListener("click", onEnter);
      startNameEl.removeEventListener("keydown", onKeydown);
      resolve();
    };
    const onKeydown = (ev: KeyboardEvent): void => {
      if (ev.key === "Enter") {
        onEnter();
      }
    };
    startEnterEl.addEventListener("click", onEnter);
    startNameEl.addEventListener("keydown", onKeydown);
  });
}

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
  maxHp: {},
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
  identityLink: "",
  forceRejoin: null,
  get chat(): { seq: number; sender: string; text: string }[] {
    return chatMessages();
  },
  sendChat: (text: string): Promise<void> => storeSendChat(text),
  party: [],
  partyId: 0,
  quest: null,
  myQuests: [],
  quests: [],
  questGoalMarker: null,
  questGoalMarkers: [],
  inventory: [],
  equipped: {},
  backpack: [],
  panelOpen: false,
  pickupModal: { open: false, rows: [] },
  groundItems: [],
  damage: [],
  combatMoves: [],
  combatRanged: [],
  committedAction: null,
};

// How many hexes an entity can cover in one action-gated combat turn. 1 is
// the current rule (one step per turn, same as the resolution walks paths);
// the reach computation below is a BFS precisely so a future run/jump
// ability — or a pipeline-supplied per-entity movement range — only changes
// this number (or its source), not the structure.
const COMBAT_MOVE_RANGE = 1;

// My equipped ranged weapon's range/AoE radius, refreshed every turn bundle
// from Entity.Items (Milestone 6b.4: weapon numbers now live in the server's
// item registry, internal/game/content.go, not in a client-side literal
// mirror) — null range means "no ranged weapon equipped" (a fighter, or a
// rogue/mage that somehow unequipped theirs), which always resolves to a
// move on click. These only drive the click-vs-move UX hint below; the
// server independently re-checks the real equipped weapon on every attack
// intent regardless.
let myRangedRangeHex: number | null = null;
let myRangedAoeRadius = 0;

/**
 * Decides whether a click on `target` should fire a ranged attack instead of
 * a move. Out of combat, or no ranged weapon equipped: always a move. In
 * combat with a ranged weapon equipped: a single-target weapon (a rogue's
 * bow) only fires at a hostile actually standing on the clicked hex, within
 * range — any other click there still walks (mirrors the melee-bump flow).
 * An AoE weapon (a mage's staff, aoeRadius > 0) can be aimed at any hex
 * within range — the blast can land on empty ground and still catch nearby
 * hostiles — so any in-range click attacks. Reads window.game (the same
 * state the debug/test surface exposes) rather than closed-over locals, so
 * it stays correct regardless of when it's called.
 */
function isRangedAttackClick(target: Hex): boolean {
  if (!window.game.inCombat) {
    return false;
  }
  const me = window.game.me;
  if (myRangedRangeHex === null || me === null || hexDistance(me.hex, target) > myRangedRangeHex) {
    return false;
  }
  if (myRangedAoeRadius > 0) {
    return true;
  }

  return window.game.positions.some(
    (p) => p.kind === EntityMonster && p.hex.q === target.q && p.hex.r === target.r,
  );
}

async function start(): Promise<void> {
  // A brand-new player — no stored identity at all, or one with no token and
  // no class/species choice yet (the shape a cleared-then-partially-seeded
  // e2e storage state can produce) — gets the start screen while the map/
  // engine load below, and join() waits for it (see waitForEnter below). A
  // returning player's stored identity always carries a token and/or a
  // class/species choice, so this never shows for them: join() fires exactly
  // as before, immediately once assets are ready.
  const storedIdentity = loadIdentity();
  const isNewPlayer =
    storedIdentity === null ||
    (storedIdentity.token === "" && storedIdentity.class === "" && storedIdentity.species === "");
  startScreenEl.hidden = !isNewPlayer;

  mountChat(mustGet("chat-root"));
  mountRoster(mustGet("roster-root"));
  mountQuests(mustGet("quest-root"));

  const app = new Application();
  await app.init({ background: "#0b0f0b", resizeTo: window, antialias: true });
  document.body.appendChild(app.canvas);

  const world = new Container();
  app.stage.addChild(world);

  const map = await fetchMap();
  world.addChild(buildMapLayer(map));
  window.game.tiles = map.tiles.length;

  // Walkability lookup for the combat movement overlay: grass and forest are
  // walkable (the same rule the server's map applies); everything else —
  // water, rock, off-map — is not. Static for the map's lifetime.
  const walkable = new Set<string>();
  for (const tile of map.tiles) {
    if (tile.terrain === TerrainGrass || tile.terrain === TerrainForest) {
      walkable.add(`${tile.hex.q},${tile.hex.r}`);
    }
  }

  // The tactical overlay tints reachable tiles directly on the ground, under
  // loot and entities alike.
  const moveRangeLayer = new MoveRangeLayer();
  world.addChild(moveRangeLayer.container);

  // combatReach BFS-expands from my hex up to COMBAT_MOVE_RANGE steps through
  // walkable, non-hostile, non-full tiles. A hostile-held tile on the
  // frontier is a bump-attack target (stepping in swings), never expanded
  // through. Occupancy and hostility read the latest bundle via window.game.
  const combatReach = (): { moves: Hex[]; bumps: Hex[] } => {
    const me = window.game.me;
    if (me === null) {
      return { moves: [], bumps: [] };
    }

    const occupants = new Map<string, { n: number; hostile: boolean }>();
    for (const p of window.game.positions) {
      const key = `${p.hex.q},${p.hex.r}`;
      const o = occupants.get(key) ?? { n: 0, hostile: false };
      o.n += 1;
      o.hostile ||= p.kind === EntityMonster;
      occupants.set(key, o);
    }

    const moves: Hex[] = [];
    const bumps: Hex[] = [];
    const seen = new Set<string>([`${me.hex.q},${me.hex.r}`]);
    let frontier: Hex[] = [me.hex];

    for (let step = 0; step < COMBAT_MOVE_RANGE; step++) {
      const next: Hex[] = [];

      for (const from of frontier) {
        for (const dir of Object.keys(DIRECTIONS) as (keyof typeof DIRECTIONS)[]) {
          const h = neighbor(from, dir);
          const key = `${h.q},${h.r}`;
          if (seen.has(key) || !walkable.has(key)) {
            continue;
          }
          seen.add(key);

          const occ = occupants.get(key);
          if (occ?.hostile) {
            bumps.push(h); // swing in, never walk through
          } else if ((occ?.n ?? 0) < StackCap) {
            moves.push(h);
            next.push(h); // a future range >1 keeps expanding from here
          }
        }
      }

      frontier = next;
    }

    return { moves, bumps };
  };

  // Ground-loot layer sits under the entity layer (added first) — a dropped
  // item never occludes a player/monster dot standing over it.
  const groundItemLayer = new GroundItemLayer();
  world.addChild(groundItemLayer.container);

  // Quest goal marker (item 12) sits above ground loot, below entities —
  // same reasoning as the ground layer: a player standing on the goal hex
  // still reads as a player, with the marker as backdrop.
  const questMarkerLayer = new QuestMarkerLayer(app.ticker);
  world.addChild(questMarkerLayer.container);

  // Click feedback (destination ring, attack flash) sits under the entities:
  // acknowledgement, not occlusion.
  const feedbackLayer = new FeedbackLayer(app.ticker);
  world.addChild(feedbackLayer.container);

  const entityLayer = new EntityLayer(app.ticker);
  world.addChild(entityLayer.container);

  // Floating damage numbers render above everything on the map.
  const damageLayer = new DamageNumberLayer(app.ticker);
  world.addChild(damageLayer.container);

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

  // A brand-new player's join waits here for the start screen's Enter — the
  // map/engine above are already loaded, so the world is ready the instant
  // they commit. A returning player (storedIdentity has a token) skips
  // straight through and reclaims by that exact token (see session.reclaim's
  // doc — never re-reads localStorage, so a stale start-screen picker
  // selection is irrelevant either way).
  if (isNewPlayer) {
    await waitForEnter();
  }
  startScreenEl.hidden = true;

  let me;
  let joinedClass: string;
  let joinedSpecies: string;
  try {
    if (storedIdentity !== null && storedIdentity.token !== "") {
      me = await reclaim(storedIdentity);
      joinedClass = storedIdentity.class;
      joinedSpecies = storedIdentity.species;
    } else {
      // No token to reclaim (nothing stored, or a class/species-only
      // pre-seeded identity with an empty token — a technique some e2e
      // specs use to join deterministically without touching the start
      // screen): a brand-new join, but a stored class/species preference
      // (if any) still wins over the picker's live selection, same as
      // before this batch's reclaim/join split — there is no token
      // involved here, so no cross-tab reclaim hazard to guard against.
      joinedClass = storedIdentity?.class || selectedClass;
      joinedSpecies = storedIdentity?.species || selectedSpecies;
      me = await join(selectedName, joinedClass, joinedSpecies);
    }
  } catch (err) {
    // A REJECTED reclaim (4xx) means the stored identity itself is dead —
    // most plausibly a character link whose token the server no longer
    // knows, or a world reset (snapshot off across a restart, or discarded
    // on a version/seed/worldId mismatch — see item 4, playtest feedback
    // batch 3): reclaim()'s reclaim-or-fail contract only accepts an empty
    // class/species alongside a token the server still recognizes. Clear the
    // dead identity so refreshes stop re-failing, and fall back to the start
    // screen for a proper new character. Anything else (network down, world
    // full) rethrows — the stored identity may still be perfectly good, so
    // it must survive.
    if (!(err instanceof JoinRejectedError)) {
      throw err;
    }
    clearIdentity();
    startScreenEl.hidden = false;
    await waitForEnter();
    startScreenEl.hidden = true;
    me = await join(selectedName, selectedClass, selectedSpecies);
    joinedClass = selectedClass;
    joinedSpecies = selectedSpecies;
  }
  window.game.me = { id: me.entityId, hex: me.hex };
  window.game.name = selectedName;
  const identity = { entityId: me.entityId, token: me.token, class: joinedClass, species: joinedSpecies };
  setChatToken(identity.token);

  // Multi-tab hardening (item 2, playtest feedback batch 3): if another tab
  // sharing this browser's localStorage overwrites the persisted identity
  // with a different token (a different person joining in a second tab, or
  // that tab's own re-join racing ours), reload rather than keep running
  // with a token another tab may already be reclaiming/using — see
  // session.onForeignIdentityChange's doc.
  onForeignIdentityChange(
    () => identity.token,
    () => {
      window.location.reload();
    },
  );

  // Character link: reveal the copy button now that there is an identity to
  // link (hidden until joined — see index.html), and keep it in sync across
  // a re-join (attemptRejoin below — a reclaim keeps the same token since
  // item 2's fix, but the link is re-derived alongside the rest of the
  // adopted identity regardless).
  const COPY_LABEL = "copy character link";
  const COPIED_LABEL = "copied!";
  let copiedFlashTimer: ReturnType<typeof setTimeout> | undefined;

  const setIdentityLink = (token: string): void => {
    window.game.identityLink = `${window.location.origin}/#t=${token}`;
  };
  setIdentityLink(identity.token);
  copyLinkEl.hidden = false;
  copyLinkEl.textContent = COPY_LABEL;
  copyLinkEl.addEventListener("click", () => {
    void navigator.clipboard.writeText(window.game.identityLink).then(() => {
      copyLinkEl.textContent = COPIED_LABEL;
      copyLinkEl.classList.add("copied");
      clearTimeout(copiedFlashTimer);
      copiedFlashTimer = setTimeout(() => {
        copyLinkEl.textContent = COPY_LABEL;
        copyLinkEl.classList.remove("copied");
      }, 1500);
    });
  });

  // The character panel's inventory actions (equip/unequip/drop/drink) and
  // the pickup modal's take all POST the matching intent. Outside a bubble
  // the server applies immediately (the result rides the next turn bundle);
  // inside one the action becomes this turn's committed action, superseding
  // any queued move/attack — so clear the committed-action indicator to
  // match (item 6), and mark the item pending so the click visibly registers
  // until the next bundle answers. A pickup's target is a GROUND item id (not
  // owned), so it does not take a pending mark; a rejected pickup (backpack
  // full — the server 422s, submitPickup resolves false) marks its row.
  const supersedeCommitted = (): void => {
    window.game.committedAction = null;
    feedbackLayer.setCommitted(null);
  };

  // Inventory panel toggle: the HUD button, the `i` key, and the panel's own
  // close button all route through applyPanelOpen, which keeps the store
  // signal, the HUD button's open-state class, and window.game.panelOpen in
  // sync.
  const applyPanelOpen = (open: boolean): void => {
    if (panelOpen() !== open) {
      togglePanel();
    }
    toggleInventoryEl.classList.toggle("open", panelOpen());
    window.game.panelOpen = panelOpen();
  };
  const toggleInventory = (): void => applyPanelOpen(!panelOpen());

  const characterActions = {
    equip: (itemId: number): void => {
      supersedeCommitted();
      markPending(itemId);
      void submitEquip(identity, itemId);
    },
    unequip: (itemId: number): void => {
      supersedeCommitted();
      markPending(itemId);
      void submitUnequip(identity, itemId);
    },
    drop: (itemId: number): void => {
      supersedeCommitted();
      markPending(itemId);
      void submitDrop(identity, itemId);
    },
    drink: (itemId: number): void => {
      supersedeCommitted();
      markPending(itemId);
      void submitDrink(identity, itemId);
    },
    close: (): void => applyPanelOpen(false),
  };

  mountCharacter(mustGet("character-root"), characterActions);
  mountPickup(mustGet("pickup-root"), {
    take: (groundItemId: number): void => {
      supersedeCommitted();
      void submitPickup(identity, groundItemId).then((ok) => {
        if (!ok) markPickupRejected(groundItemId);
      });
    },
  });

  // The character panel's weapon slots are class-shaped — set them once, now
  // that the joined class is known, so the paper-doll labels and places the
  // two weapon slots correctly (fighter melee+thrown, rogue melee+ranged,
  // mage staff+wand).
  setWeaponSlots(weaponSlotsForClass(joinedClass));

  // The HUD toggle button reveals now that there is a character to show; the
  // `i` key is bound via bindMovementKeys below (sharing the typing-focus
  // guard). Both call toggleInventory (defined above).
  toggleInventoryEl.hidden = false;
  toggleInventoryEl.addEventListener("click", toggleInventory);

  // Re-join tracking: if this client's entity is absent from turn bundles for
  // a sustained spell, the disconnect-grace sweep removed it server-side (the
  // player was gone too long) — re-join to get a playable (fresh) character
  // back. MISSING_GRACE_MS is deliberately a couple of seconds, well above a
  // single coalesced/missed bundle, so a normal blip never trips it.
  let missingSinceMs: number | null = null;
  let rejoining = false;
  let eventsController: EventsController;
  // My hex on the previous bundle, so the pickup modal can tell a walk-over
  // (open) from staying put (respect a dismissal) — see refreshPickup.
  let lastPickupHex: Hex | null = null;

  // attemptRejoin reclaims OUR OWN already-known token (never re-reads
  // localStorage — see session.reclaim's doc, item 2 playtest feedback
  // batch 3) after the disconnect-grace sweep archived this entity. A
  // successful reclaim keeps the same token but restores a fresh entity
  // (new id, new spawn hex, progression intact) — adopted in place so every
  // closure that captured `identity`/`me` sees the update, then the event
  // stream reconnects (its Last-Event-ID watermark is now stale for the new
  // entity's turn history). Guarded by `rejoining` so an in-flight re-join
  // can't be started twice. If the server no longer knows our token AT ALL
  // (reclaim's reclaim-or-fail contract rejects with JoinRejectedError), the
  // world reset out from under us (item 4) — reload rather than silently
  // mint a brand-new, level-1 stranger in this character's place; a
  // non-rejection error (network blip) rethrows for the caller's
  // `.catch(() => {})` to swallow, so the missing-streak just retries.
  const attemptRejoin = async (): Promise<void> => {
    if (rejoining) {
      return;
    }
    rejoining = true;
    try {
      let rejoined;
      try {
        rejoined = await reclaim(identity);
      } catch (err) {
        if (err instanceof JoinRejectedError) {
          window.location.reload();
          return;
        }
        throw err;
      }
      identity.entityId = rejoined.entityId;
      identity.token = rejoined.token;
      me.entityId = rejoined.entityId;
      me.token = rejoined.token;
      me.hex = rejoined.hex;
      window.game.me = { id: rejoined.entityId, hex: rejoined.hex };
      window.game.destination = null;
      feedbackLayer.setDestination(null);
      setChatToken(identity.token);
      setIdentityLink(identity.token);
      eventsController.reconnect();
    } finally {
      rejoining = false;
    }
  };
  window.game.forceRejoin = attemptRejoin;

  // walkTo submits a move destination and records it for the HUD/tests. The
  // world's answer (movement) only ever arrives via turn bundles. A rejected
  // target (unwalkable / unreachable) never becomes a pending walk, so clear
  // it — unless a newer walkTo has already replaced the destination meanwhile.
  const walkTo = (target: Hex): Promise<void> => {
    window.game.destination = target;
    // Instant acknowledgement — the ring appears on click, not on the next
    // turn bundle. Cleared alongside window.game.destination everywhere.
    feedbackLayer.setDestination(target);

    // Committed-action indicator (item 6): inside a bubble, a move intent is
    // this turn's action — my own hex is a "wait" (own-hex move already
    // waits/cancels), anything else a "move". Outside a bubble there is
    // nothing to commit to (no action gating), so leave it null.
    if (window.game.inCombat) {
      const self = window.game.me !== null && window.game.me.hex.q === target.q && window.game.me.hex.r === target.r;
      const committed: CommittedAction = { kind: self ? "wait" : "move", target };
      window.game.committedAction = committed;
      feedbackLayer.setCommitted(committed);
    }

    return submitIntent(identity, target, IntentMove).then((accepted) => {
      const pending = window.game.destination;
      if (!accepted && pending !== null && pending.q === target.q && pending.r === target.r) {
        window.game.destination = null;
        feedbackLayer.setDestination(null);
      }
    });
  };

  // hostileIdAt returns the entity id of a monster standing on hex, or null.
  // Resolves a single-target ranged click into an entity-targeted attack
  // intent (item 7, playtest batch 2): the server re-aims at the victim's
  // post-move hex at resolution time, tracking a sidestep or retreat a
  // hex-pinned shot never could.
  const hostileIdAt = (hex: Hex): number | null => {
    const hit = window.game.positions.find((p) => p.kind === EntityMonster && p.hex.q === hex.q && p.hex.r === hex.r);
    return hit === undefined ? null : hit.id;
  };

  // attackAt fires a ranged attack intent at target: no destination bookkeeping
  // (the attacker doesn't move onto it) — a one-shot flash on the target hex
  // acknowledges the click; the turn bundle's HP changes speak for the result.
  // A single-target weapon (myRangedAoeRadius 0, a bow) targets the
  // hostile's ENTITY id instead of the bare hex (item 7); an AoE weapon
  // stays ground-targeted — its blast radius makes a hex the natural target,
  // and it can land on empty ground and still catch nearby hostiles.
  const attackAt = (target: Hex): Promise<void> => {
    feedbackLayer.flashAttack(target);

    const targetEntityId = myRangedAoeRadius === 0 ? (hostileIdAt(target) ?? 0) : 0;

    // Committed-action indicator (item 6): a persistent crosshair on the
    // target, alongside the flashAttack one-shot ring above.
    const committed: CommittedAction = { kind: "attack", target };
    window.game.committedAction = committed;
    feedbackLayer.setCommitted(committed);

    return submitIntent(identity, target, IntentAttack, targetEntityId).then(() => undefined);
  };

  // lastReach mirrors the tactical overlay's move/bump split for click
  // routing (window.game.combatMoves merges them for the e2e surface).
  // Refreshed by onTurn alongside the overlay.
  let lastReach: { moves: Hex[]; bumps: Hex[] } = { moves: [], bumps: [] };
  const inList = (list: Hex[], h: Hex): boolean => list.some((x) => x.q === h.q && x.r === h.r);

  // clickTarget is the single decision point shared by canvas clicks and
  // window.game.tapHex, so tapHex genuinely mirrors "as if the hex were
  // clicked" for tests. Out of combat this is the pre-classes behavior:
  // click-anywhere pathing (ranged clicks only exist in combat). IN combat,
  // the tinted overlay is the contract: blue = step there, strong red
  // (adjacent hostile) = bump-attack, light red (weapon reach) = shoot when
  // an enemy is on the hex (or anywhere in it, for AoE), own hex = stand
  // still/cancel; anything else is not a valid selection. One deliberate
  // class nuance: an AoE caster (mage) blasts an adjacent hostile rather
  // than staff-bonking it — its ranged weapon IS its real weapon — while a
  // bow user (rogue) bumps adjacent hostiles with the dagger, the plan's
  // "weapon by distance" identity.
  const clickTarget = (target: Hex): Promise<void> => {
    if (window.game.inCombat) {
      const self =
        window.game.me !== null && window.game.me.hex.q === target.q && window.game.me.hex.r === target.r;

      if (self || inList(lastReach.moves, target)) {
        clearPending(); // a real intent replaces a queued in-bubble action
        return walkTo(target);
      }

      if (inList(lastReach.bumps, target)) {
        clearPending();
        if (myRangedAoeRadius > 0 && isRangedAttackClick(target)) {
          return attackAt(target); // mage: blast the adjacent hostile
        }
        return walkTo(target); // bump-attack: step in and swing
      }

      if (isRangedAttackClick(target)) {
        clearPending();
        return attackAt(target);
      }

      return Promise.resolve(); // out of this turn's reach: not a valid selection
    }

    // A map click replaces a queued in-bubble equip server-side (latest
    // intent wins) — release its button's pending "…" to match.
    clearPending();

    return walkTo(target);
  };

  window.game.tapHex = (q, r): Promise<void> => clickTarget({ q, r });

  // World-reset signal (item 4, playtest feedback batch 3): remember the
  // first WorldID this session ever sees. A later bundle carrying a
  // DIFFERENT WorldID means the world underneath this client changed — a
  // restart with no matching snapshot/archive entry, not an ordinary
  // reconnect (a restore keeps its predecessor's WorldID — see
  // World.worldID's doc, internal/game/world.go). A full reload is the
  // simplest correct recovery: it re-runs this whole module from scratch,
  // and the existing dead-token reclaim-or-fail path (this function's
  // catch block, above) already falls back to the start screen if the
  // server truly no longer knows this identity's token — no separate
  // clear-identity step needed here.
  let firstWorldID: string | null = null;

  eventsController = connectEvents(() => identity.token, {
    onTurn: (event: TurnEvent): void => {
      if (firstWorldID === null) {
        firstWorldID = event.worldId;
      } else if (event.worldId !== firstWorldID) {
        window.location.reload();
        return;
      }

      // Committed-action indicator (item 6): clear on the next turn bundle,
      // whether it resolved my action or not — a fresh bundle always means
      // "no longer showing what I chose last time," the simplest rule that
      // still reads as "shown until it resolves" in the common case (a solo
      // or last-to-lock-in bubble resolves the instant this client submits,
      // so its very next bundle IS that resolution).
      window.game.committedAction = null;
      feedbackLayer.setCommitted(null);

      // Derive floating damage numbers by diffing this bundle's HP against the
      // previous one (still in window.game from the last onTurn): an entity
      // with less HP took a hit; a monster missing entirely died, its killing
      // blow shown as the HP it had left. First bundle diffs against nothing.
      const prevHp = window.game.hp;
      const prevPositions = window.game.positions;
      const damage: { id: number; amount: number }[] = [];
      const present = new Set(event.entities.map((e) => e.id));
      for (const e of event.entities) {
        const before = prevHp[e.id];
        if (before !== undefined && e.hp < before) {
          damage.push({ id: e.id, amount: before - e.hp });
          damageLayer.spawn(e.hex, before - e.hp, e.kind === EntityPlayer);
        }
      }
      for (const p of prevPositions) {
        const before = prevHp[p.id];
        if (!present.has(p.id) && p.kind === EntityMonster && before !== undefined && before > 0) {
          damage.push({ id: p.id, amount: before });
          damageLayer.spawn(p.hex, before, false);
        }
      }
      window.game.damage = damage;

      window.game.turn = event.turn;
      window.game.entities = event.entities.length;
      window.game.monsters = event.entities.filter((e) => e.kind === EntityMonster).length;
      window.game.positions = event.entities.map((e) => ({
        id: e.id,
        hex: e.hex,
        kind: e.kind,
        monsterKind: e.monsterKind,
        name: e.name,
      }));
      window.game.hp = Object.fromEntries(event.entities.map((e) => [e.id, e.hp]));
      window.game.maxHp = Object.fromEntries(event.entities.map((e) => [e.id, e.maxHp]));
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
          feedbackLayer.setDestination(null);
        }

        window.game.xp = mine.xp;
        window.game.level = mine.level;
        window.game.class = mine.class;
        window.game.species = mine.species;
        window.game.name = mine.name;
        const xpIntoLevel = mine.xp % XPPerLevel;
        // Position readout (item 9, playtest batch 2): live per bundle, so
        // it never drifts from the server-authoritative hex even mid-tween.
        statsEl.textContent = `Lv ${mine.level} · ${xpIntoLevel}/${XPPerLevel} XP · (${mine.hex.q}, ${mine.hex.r})`;

        // Gear: my owned items ride Entity.Items every bundle (full-snapshot
        // philosophy, same as everything else here). setInventory feeds the
        // paper-doll's slot-keyed equipped map + backpack; the mirrors below
        // expose the same to window.game for e2e. The equipped ranged item's
        // range/AoE radius (if any) also drives the click-vs-move UX hint
        // above (isRangedAttackClick) — the ranged-ish weapon types are
        // thrown-weapon/ranged-weapon/wand.
        setInventory(mine.items);
        window.game.inventory = mine.items.map((it: ItemView) => ({
          id: it.id,
          defId: it.defId,
          equipped: it.equipped,
        }));
        window.game.equipped = equippedSignal();
        window.game.backpack = backpackSignal();

        const rangedTypes: string[] = [ItemTypeThrownWeapon, ItemTypeRangedWeapon, ItemTypeWand];
        const rangedItem = mine.items.find((it: ItemView) => rangedTypes.includes(it.type) && it.equipped);
        myRangedRangeHex = rangedItem?.rangeHex ?? null;
        myRangedAoeRadius = rangedItem?.aoeRadius ?? 0;
      }

      // Ground loot: every dropped item currently lying on the map, redrawn
      // wholesale each turn (full-snapshot philosophy) regardless of join
      // status — a drop is visible to everyone, not just its eventual picker.
      groundItemLayer.update(event.groundItems);
      window.game.groundItems = event.groundItems.map((gi: GroundItemView) => ({ id: gi.id, hex: gi.hex }));

      // Pickup modal (inventory-slots milestone): every ground item lying on
      // MY current hex becomes a modal row (name + type). The modal opens on
      // walk-over regardless of the character panel; it stays dismissed while
      // I remain on the hex (refreshPickup tracks a hex change to reopen on
      // re-entry). moved = my hex differs from the previous bundle's.
      const myHex = mine?.hex ?? null;
      const moved = myHex !== null && (lastPickupHex === null || myHex.q !== lastPickupHex.q || myHex.r !== lastPickupHex.r);
      lastPickupHex = myHex;
      const rowsHere =
        myHex === null
          ? []
          : event.groundItems
              .filter((gi) => gi.hex.q === myHex.q && gi.hex.r === myHex.r)
              .map((gi: GroundItemView) => ({ id: gi.id, name: gi.name, type: gi.type }));
      refreshPickup(rowsHere, moved);
      window.game.pickupModal = {
        open: modalOpen(),
        rows: pickupRows().map((r) => ({ id: r.id, name: r.name, type: r.type, rejected: r.rejected })),
      };
      window.game.panelOpen = panelOpen();

      // Party roster: refreshed every turn from the bundle itself (no separate
      // party-membership stream) — solo (partyId 0) always renders an empty
      // roster, so the panel simply doesn't show.
      const myPartyId = mine?.partyId ?? 0;
      const partyNames =
        myPartyId === 0 ? [] : event.entities.filter((e) => e.partyId === myPartyId).map((e) => e.name);
      setParty(partyNames);
      window.game.party = partyNames;
      window.game.partyId = myPartyId;

      // Quest board: refreshed every turn from the bundle itself (full-snapshot
      // philosophy — no separate quest-membership stream). My active quests are
      // every "taken" quest held by me or (if I'm in a party) my party — item
      // 14, playtest batch 2: a player may hold SEVERAL personal quests
      // concurrently now, plus at most one party quest, so this is a list.
      window.game.quests = event.quests;
      setQuests(event.quests, me.entityId, myPartyId);
      window.game.myQuests = event.quests.filter(
        (q) =>
          q.state === "taken" &&
          (q.holderEntityId === me.entityId || (myPartyId !== 0 && q.holderPartyId === myPartyId)),
      );
      window.game.quest = window.game.myQuests[0] ?? null; // back-compat: first of myQuests

      // Quest goal markers (item 12, plural since item 14): one gold marker
      // per active "reach" quest — a kill quest gets no marker. A marker
      // clears automatically once its quest drops out of myQuests
      // (completed/abandoned).
      window.game.questGoalMarkers = window.game.myQuests
        .filter((q) => q.kind === "reach")
        .map((q) => ({ id: q.id, hex: q.goalHex }));
      window.game.questGoalMarker = window.game.questGoalMarkers[0]?.hex ?? null; // back-compat
      questMarkerLayer.setGoals(window.game.questGoalMarkers);

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
        // Item 3 (playtest feedback batch 3): the panel used to list raw
        // entity ids ("waiting for: 3, 7") — map each to its display name
        // from this bundle's entities, falling back to "#id" for anything
        // not present (shouldn't happen — a bubble member always rides the
        // same bundle — but keeps the panel legible instead of blank/NaN if
        // it ever does).
        combatWaitingEl.textContent = myBubble.waitingForIds
          .map((id) => event.entities.find((e) => e.id === id)?.name ?? `#${id}`)
          .join(", ");
        combatPatienceEl.textContent = (myBubble.patienceRemainingMs / 1000).toFixed(1);
      } else {
        combatPanelEl.hidden = true;
        turnTimerEl.hidden = false;
      }

      entityLayer.update(event.entities, me.entityId, mine?.partyId ?? 0, playbackMs);
      timer.onTurn(event.intervalMs, playbackMs);

      // Tactical overlay: reachable tiles + ranged reach while in a bubble,
      // nothing outside one. Computed last — it reads the me/positions/
      // inCombat state this handler just refreshed.
      if (window.game.inCombat) {
        const reach = combatReach();
        lastReach = reach;
        window.game.combatMoves = [...reach.moves, ...reach.bumps];

        // The equipped ranged weapon's reach: every map tile within range
        // (distance-only, no LOS — matching the server's rule), minus the
        // tiles that already act differently on click (moves, bumps, self).
        const ranged: Hex[] = [];
        const meNow = window.game.me;
        if (meNow !== null && myRangedRangeHex !== null) {
          for (const tile of map.tiles) {
            const d = hexDistance(meNow.hex, tile.hex);
            if (
              d >= 1 &&
              d <= myRangedRangeHex &&
              !inList(reach.moves, tile.hex) &&
              !inList(reach.bumps, tile.hex)
            ) {
              ranged.push(tile.hex);
            }
          }
        }
        window.game.combatRanged = ranged;
        moveRangeLayer.update(reach.moves, reach.bumps, ranged);
      } else {
        lastReach = { moves: [], bumps: [] };
        window.game.combatMoves = [];
        window.game.combatRanged = [];
        moveRangeLayer.update([], [], []);
      }
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
  // isBlocked additionally guards the start screen (item 10): a not-yet-real
  // character must never move while its class/species is still being chosen.
  bindMovementKeys({
    onStep: (dir): void => {
      const from = window.game.me?.hex;
      if (from === undefined) {
        return;
      }
      walkTo(neighbor(from, dir));
    },
    // SPACE = wait (item 11): the same own-hex move a click on my own hex
    // already sends — clickTarget's "self" branch, reached here via
    // clickTarget itself so the two code paths stay identical (clears
    // equip-pending too, and shows the item-6 wait glyph via walkTo's own
    // committedAction logic).
    onWait: (): void => {
      const me = window.game.me;
      if (me === null) {
        return;
      }
      void clickTarget(me.hex);
    },
    // `i` toggles the character/inventory panel — shares the movement keys'
    // typing-focus guard (input/keys.ts) so typing "i" into chat never opens
    // it, and the same start-screen block below.
    onToggleInventory: toggleInventory,
    isBlocked: (): boolean => !startScreenEl.hidden,
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
    // Crosshair only where a click would actually shoot — a bump tile with a
    // single-target weapon swings instead (see clickTarget's routing).
    const hover = pixelToHex({ x: worldX, y: worldY });
    const wouldShoot =
      isRangedAttackClick(hover) && !(myRangedAoeRadius === 0 && inList(lastReach.bumps, hover));
    app.canvas.style.cursor = wouldShoot ? "crosshair" : "default";

    // Enemy hover tooltip (item 13, playtest batch 2): kind display name +
    // "HP cur/max", near the cursor. pointer-events: none on the tooltip
    // itself (index.html) means it can never intercept the click it's
    // floating over.
    //
    // Hover gating (item 6, playtest feedback batch 3): the HP line only
    // shows within CombatRadius of my own entity — scouting a distant
    // monster shouldn't read its exact health through the fog of distance.
    // Beyond that (or before I've joined) it's name-only.
    const monster = window.game.positions.find(
      (p) => p.kind === EntityMonster && p.hex.q === hover.q && p.hex.r === hover.r,
    );
    if (monster === undefined) {
      hoverTooltipEl.hidden = true;
    } else {
      const me = window.game.me;
      const inRange = me !== null && hexDistance(me.hex, monster.hex) <= CombatRadius;

      hoverTooltipKindEl.textContent = monster.name;
      if (inRange) {
        const hp = window.game.hp[monster.id] ?? 0;
        const maxHp = window.game.maxHp[monster.id] ?? 0;
        hoverTooltipHPEl.textContent = `HP ${hp}/${maxHp}`;
        hoverTooltipHPEl.hidden = false;
      } else {
        hoverTooltipHPEl.textContent = "";
        hoverTooltipHPEl.hidden = true;
      }
      hoverTooltipEl.style.left = `${ev.clientX + 14}px`;
      hoverTooltipEl.style.top = `${ev.clientY + 14}px`;
      hoverTooltipEl.hidden = false;
    }
  });

  app.canvas.addEventListener("pointerleave", () => {
    hoverTooltipEl.hidden = true;
  });
}

start().catch((err: unknown) => {
  statusEl.textContent = `failed to start: ${String(err)}`;
});
