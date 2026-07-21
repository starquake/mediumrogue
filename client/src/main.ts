// Keeps Pixi off `new Function`, so the server's strict CSP (no unsafe-eval)
// holds. Must load before any other pixi.js import.
import "pixi.js/unsafe-eval";

import { Application, Container } from "pixi.js";

import { mountChat } from "./chat/ChatPanel";
import { appendChat, messages as chatMessages, sendChat as storeSendChat, setChatToken } from "./chat/store";
import { mountCharacter } from "./gear/CharacterPanel";
import { mountPickup } from "./gear/PickupModal";
import { mountStatTooltip } from "./gear/StatTooltip";
import {
  backpack as backpackSignal,
  clearOnePending,
  clearPending,
  equipped as equippedSignal,
  markPending,
  markPickupRejected,
  markTaking,
  panelOpen,
  pending,
  pickupModalMirror,
  refreshPickup,
  setInventory,
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
  onIntentFeedback,
  reclaim,
  submitDrink,
  submitDrop,
  submitEquip,
  submitIntent,
  submitLearnSkill,
  submitPickup,
  submitRecall,
  submitThrow,
  submitUnequip,
  submitUseSkill,
} from "./net/session";
import { mountRoster } from "./party/RosterPanel";
import { setParty } from "./party/store";
import type { GroundItemView, Hex, HitView, ItemView, QuestView, SkillView, TurnEvent } from "./protocol.gen";
import { mountQuests } from "./quest/QuestPanel";
import { mountSkills } from "./skills/SkillsPanel";
import { applyLearnedLocally, panelOpen as skillsPanelOpen, setSkills, toggleSkillsPanel } from "./skills/store";
import { setQuests } from "./quest/store";
import {
  ClassFighter,
  CombatRadius,
  EntityMonster,
  EntityPlayer,
  HumanBonusSkillPoints,
  IntentAttack,
  IntentMove,
  PlaybackSeconds,
  SkillPointsPerLevel,
  SkillPointCost,
  SpeciesHuman,
  TerrainForest,
  TerrainGrass,
  TurnSeconds,
  WeaponTagMagic,
  WeaponTagRanged,
  XPCurveBase,
} from "./protocol.gen";
import { AttackHighlightLayer } from "./render/attack";
import type { HitStyle } from "./render/damage";
import { DamageNumberLayer } from "./render/damage";
import { EntityLayer } from "./render/entities";
import type { CommittedAction } from "./render/feedback";
import { FeedbackLayer } from "./render/feedback";
import { hexDistance, hexToPixel, pixelToHex } from "./render/hex";
import { HoverHighlightLayer, type HoverMoveTile } from "./render/hover";
import { GroundItemLayer } from "./render/items";
import { MoveRangeLayer } from "./render/range";
import { buildMapLayer } from "./render/map";
import { QuestMarkerLayer } from "./render/questmarker";
import * as tactics from "./tactics";
import { mustGet, mustQuery } from "./ui/dom";
import { TurnTimer } from "./ui/timer";

// GameDebug (the window.game shape) now lives in debug-surface.ts; re-export it
// so the e2e specs' `import { GameDebug } from "../src/main"` keeps resolving.
export type { GameDebug } from "./debug-surface";

// Strip a `#t=<token>` character-link fragment and adopt its identity before
// anything else in this module runs — see importIdentityFromFragment's doc
// comment (net/session.ts) for why this must happen this early.
importIdentityFromFragment();

const turnEl = mustGet("turn");
const turnStuckEl = mustGet("turn-stuck");
const turnReceivedEl = mustGet("turn-received");
const clientErrorEl = mustGet("client-error");
const toastEl = mustGet("toast");

// A transient toast for intent rejections and network blips (#193): shows the
// server's own reason ("target is out of range", "backpack full", …) instead
// of swallowing it or guessing. Latest message wins; auto-hides.
let toastTimer = 0;
function showToast(msg: string): void {
  toastEl.textContent = msg;
  toastEl.hidden = false;
  toastEl.style.opacity = "1";
  window.clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => {
    toastEl.style.opacity = "0";
    window.setTimeout(() => (toastEl.hidden = true), 260);
  }, 2600);
}
onIntentFeedback(showToast);

// Self-only level-up banner (#202): fires when MY level increases between
// bundles. Names the points earned (per-level grant + Human bonus) and points
// at the skills panel — the discoverability hook. prevLevel starts at 0 so a
// fresh or reclaimed character's first bundle never flashes.
const levelupEl = mustGet("levelup");
let prevLevel = 0;
let levelupTimer = 0;
function showLevelUp(from: number, to: number, species: string): void {
  const per = SkillPointsPerLevel + (species === SpeciesHuman ? HumanBonusSkillPoints : 0);
  const pts = (to - from) * per;
  window.game.lastLevelUp = to;
  levelupEl.textContent = `LEVEL ${to} · +${pts} skill points — K to spend`;
  levelupEl.hidden = false;
  levelupEl.style.opacity = "1";
  window.clearTimeout(levelupTimer);
  levelupTimer = window.setTimeout(() => {
    levelupEl.style.opacity = "0";
    window.setTimeout(() => (levelupEl.hidden = true), 360);
  }, 3200);
}

/**
 * Show the stuck marker when bundles are arriving but not being applied
 * (#170). One turn of lag is normal — a bundle can land mid-handler — so only
 * a gap of MORE than one counts, or the HUD would flicker every turn.
 */
const stuckAfterTurns = 1;

function applyStatus(): void {
  const behind = window.game.turnReceived - window.game.turnApplied;
  turnStuckEl.hidden = behind <= stuckAfterTurns;
  turnReceivedEl.textContent = String(window.game.turnReceived);
}

/**
 * Raise the crash banner (#170). The failure it replaces was silent: an
 * uncaught throw inside the turn handler stopped all rendering while the SSE
 * stream stayed up, so the HUD said "connected" over a frozen map and nobody
 * — player or maintainer — had a signal to act on.
 *
 * Deliberately not auto-dismissed and deliberately blunt: the client cannot
 * recover its own render loop from here, so the only useful advice is reload.
 */
function reportClientError(what: string): void {
  window.game.clientError = what;
  clientErrorEl.textContent = `the client hit an error and stopped updating — reload the page (${what})`;
  clientErrorEl.hidden = false;
}

window.addEventListener("error", (ev) => {
  reportClientError(ev.message);
});

window.addEventListener("unhandledrejection", (ev) => {
  reportClientError(String((ev as PromiseRejectionEvent).reason));
});
const statusEl = mustGet("status");
const statsEl = mustGet("stats");
const copyLinkEl = mustGet("copy-link") as HTMLButtonElement;
const toggleInventoryEl = mustGet("toggle-inventory") as HTMLButtonElement;
const toggleHelpEl = mustGet("toggle-help") as HTMLButtonElement;
const controlsOverlayEl = mustGet("controls-overlay");

// Controls overlay (#203): the ? key, the HUD button, and the × all route
// here. Shown once automatically on a first-ever join (localStorage flag) so a
// new player is taught the keys without hunting.
function setControlsOverlay(open: boolean): void {
  controlsOverlayEl.hidden = !open;
  window.game.controlsOpen = open;
}
function isControlsOverlayOpen(): boolean {
  return !controlsOverlayEl.hidden;
}
function toggleControlsOverlay(): void {
  setControlsOverlay(!isControlsOverlayOpen());
}

const deathCardEl = mustGet("death-card");
const deathKillerEl = mustGet("death-killer");
const deathLossEl = mustGet("death-loss");
const resetCardEl = mustGet("reset-card");

// Death card (#204): a first-person "YOU DIED" naming the killer and the XP
// lost to the level floor, auto-fading as the respawn lands. Death is detected
// from XP DECREASING — xp only ever drops on the death floor, never on regen —
// so no wire signal is needed. prevXp starts at -1 so the first bundle is never
// read as a death.
let prevXp = -1;
let deathTimer = 0;
function showDeath(killer: string, loss: number): void {
  deathKillerEl.textContent = killer === "" ? "" : `${killer} got you`;
  deathLossEl.textContent = loss > 0 ? `−${loss} XP · respawning…` : "respawning…";
  deathCardEl.hidden = false;
  deathCardEl.style.opacity = "1";
  window.game.died = true;
  window.clearTimeout(deathTimer);
  deathTimer = window.setTimeout(() => {
    deathCardEl.style.opacity = "0";
    window.setTimeout(() => {
      deathCardEl.hidden = true;
      window.game.died = false;
    }, 520);
  }, 2400);
}
const turnTimerEl = mustGet("turn-timer");
const combatPanelEl = mustGet("combat-panel");
const combatWaitingEl = mustGet("combat-waiting");
const combatPatienceEl = mustGet("combat-patience");
const startScreenEl = mustGet("start-screen");
const startNameEl = mustGet("start-name") as HTMLInputElement;
const startEnterEl = mustGet("start-enter") as HTMLButtonElement;
const classCards = Array.from(startScreenEl.querySelectorAll<HTMLElement>(".card[data-class]"));
const speciesCards = Array.from(startScreenEl.querySelectorAll<HTMLElement>(".card[data-species]"));

// The character panel anchors below the HUD's REAL bottom edge (#105): the
// HUD's height varies (the combat panel swaps in for the timer, the copy-link
// button appears after join), and the old hardcoded 8rem offset let a grown
// HUD run underneath the open panel. A ResizeObserver keeps the CSS variable
// current; #character-root's top/max-height read it (index.html).
const hudEl = mustGet("hud");
new ResizeObserver(() => {
  document.documentElement.style.setProperty("--hud-bottom", `${Math.ceil(hudEl.getBoundingClientRect().bottom)}px`);
}).observe(hudEl);

// Enemy hover tooltip (item 13, playtest batch 2).
const hoverTooltipEl = mustGet("hover-tooltip");
const hoverTooltipKindEl = mustQuery(hoverTooltipEl, ".tooltip-kind");
const hoverTooltipHPEl = mustQuery(hoverTooltipEl, ".tooltip-hp");

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

// Wheel-zoom feel constants (#273/#274), all client-only and safe to tweak on
// dev. Zoom is a whole-scene container scale, eased frame-rate-independently
// toward a wheel-driven target, applied around the followed player (Grim-Dawn-
// style follow camera). There is NO pan — the camera always follows the player.
const ZOOM_MIN = 0.5; // most zoomed-OUT (survey the big world)
const ZOOM_MAX = 2.5; // most zoomed-IN
const ZOOM_EASE_RATE = 12; // 1/s — higher eases toward targetZoom faster (1 - e^(-rate·dt))
const ZOOM_WHEEL_SENSITIVITY = 0.0015; // multiplicative zoom per wheel deltaY unit

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
  zoom: 1,
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
  hexToScreen: null,
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
  turnReceived: -1,
  turnApplied: -1,
  clientError: null,
  lastLevelUp: 0,
  skills: [],
  skillPoints: 0,
  skillsPanelOpen: false,
  controlsOpen: false,
  armedSkill: (): string | null => null,
  armedThrow: null,
  died: false,
  pickupModal: { open: false, rows: [] },
  rejectPickupRow: (groundItemId: number): void => {
    markPickupRejected(groundItemId);
    window.game.pickupModal = pickupModalMirror();
  },
  groundItems: [],
  damage: [],
  hits: [],
  hoverAttackTiles: [],
  committedAttackTiles: [],
  hoverMoveTile: null,
  hoverTile: (): void => {},
  combatMoves: [],
  combatRanged: [],
  committedAction: null,
  lastAttackFlash: null,
  pendingItems: [],
  pickupPending: false,
};

// My equipped ranged/magic weapons' range/AoE stats — one entry per held
// weapon across BOTH hands (dual-wield, gear keystone #55) — refreshed every
// turn bundle from Entity.Items (weapon numbers live in the server's item
// registry, internal/game/content.go, not in a client-side literal mirror).
// Kept PER WEAPON, never collapsed into independent maxes: max(range) +
// max(aoe) would synthesize a range/AoE combination NEITHER weapon has
// (e.g. a long single-target bow + a short AoE focus reading as a long AoE),
// and the click hint would promise a blast the server then no-ops. Empty =
// no ranged weapon equipped, which always resolves to a move on click.
// These only drive the click-vs-move UX hint below; the server independently
// re-checks the real equipped weapons on every attack intent regardless. The
// per-weapon range/AoE/hostile RULES themselves live in tactics.ts (#213) —
// the resolver both the click predicate and the highlight run on.
let myRangedWeapons: tactics.RangedWeapon[] = [];

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
  await app.init({
    background: "#0b0f0b",
    resizeTo: window,
    antialias: true,
    // HiDPI: render the backing store at the display's true pixel density so
    // hexes, dots, glyph icons, and text stay crisp on retina/4K screens.
    // autoDensity keeps the canvas's CSS size unchanged — the pointer→world→hex
    // math (getBoundingClientRect / clientX / world.position, all logical
    // pixels) is unaffected, so click hit-testing needs no change.
    resolution: window.devicePixelRatio || 1,
    autoDensity: true,
  });
  document.body.appendChild(app.canvas);

  // devicePixelRatio can change while the app runs — dragging the window from a
  // non-retina monitor onto a retina one (or vice versa). app.init captured it
  // once, so without this the backing store would keep the old density and go
  // blurry on the new screen. A DPR change is a human dragging a window (rare,
  // never latency-sensitive), so a 1 s poll catches it imperceptibly with no
  // per-frame work — a single numeric compare, the resize only running on a
  // real change. (Chosen over a `matchMedia("(resolution: …)")` watcher: that's
  // event-driven but some browser/OS display-switch combos don't fire it.)
  // resizeTo:window still owns plain window resizes — its resize preserves the
  // current resolution.
  window.setInterval(() => {
    const dpr = window.devicePixelRatio || 1;
    if (app.renderer.resolution !== dpr) {
      app.renderer.resize(window.innerWidth, window.innerHeight, dpr);
    }
  }, 1000);

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

  // lastReach mirrors the tactical overlay's move/melee split for click
  // routing (window.game.combatMoves merges them for the e2e surface).
  // Refreshed by onTurn alongside the overlay.
  let lastReach: { moves: Hex[]; melees: Hex[] } = { moves: [], melees: [] };

  // The tactics resolvers (tactics.ts, #213) are pure: each reads a snapshot
  // of the state that decides move-vs-attack routing — my hex, the combat
  // flag, the equipped ranged weapons, entity positions, the static
  // walkability set, and the last computed reach. tacticsCtx builds that
  // snapshot per call so the wrappers below stay correct whenever they run.
  // combatReach's BFS, the attack-tile highlight (attackTilesFor), and the
  // click predicate (isRangedAttackClick) now share ONE resolver instead of
  // re-implementing the per-weapon range/AoE/hostile rules independently.
  const tacticsCtx = (): tactics.TacticsCtx => ({
    me: window.game.me?.hex ?? null,
    inCombat: window.game.inCombat,
    weapons: myRangedWeapons,
    positions: window.game.positions,
    walkable,
    reach: lastReach,
  });
  const inList = tactics.inList;
  const combatReach = (): { moves: Hex[]; melees: Hex[] } => tactics.combatReach(tacticsCtx());
  const aoeReaches = (target: Hex): boolean => tactics.aoeReaches(target, tacticsCtx());
  const maxRangedRange = (): number => tactics.maxRangedRange(myRangedWeapons);
  const isRangedAttackClick = (target: Hex): boolean => tactics.isRangedAttackClick(target, tacticsCtx());
  const attackTilesFor = (target: Hex): Hex[] => tactics.attackTilesFor(target, tacticsCtx());

  // The world hover highlight (#135) is a ground tint like the reach tints;
  // added BELOW the attack layer so #101's ember always reads over it where
  // they would ever coincide (they never do — it's world-only, the ember is
  // combat-only — but draw order keeps it honest).
  const hoverHighlightLayer = new HoverHighlightLayer();
  world.addChild(hoverHighlightLayer.container);

  // The attack-target highlight (#101) sits directly above the reachable-tile
  // tint: same ground plane, but "what will this action hit" must read over
  // "where can I act" where they overlap.
  const attackHighlightLayer = new AttackHighlightLayer();
  world.addChild(attackHighlightLayer.container);

  // Hover + committed highlight state (#101). The hover tiles are re-derived
  // on every change of hovered hex AND every turn bundle (positions, reach,
  // and weapons all shift per bundle); the committed tiles are captured at
  // click time (attackAt/meleeAt) and live exactly as long as
  // committedAction. Both mirror to window.game synchronously — the
  // test-surface design rule.
  let hoveredHex: Hex | null = null;
  let committedAttackTiles: Hex[] = [];

  const refreshAttackHighlight = (): void => {
    const hoverTiles = hoveredHex === null ? [] : attackTilesFor(hoveredHex);
    window.game.hoverAttackTiles = hoverTiles;
    window.game.committedAttackTiles = committedAttackTiles;
    attackHighlightLayer.update(hoverTiles, committedAttackTiles);
  };

  // hoverMoveTileFor mirrors clickTarget's OUT-of-combat routing (#135): what
  // a click on `h` would do, as the tile to light. World-only — in combat the
  // reach tints + #101 ember already answer it, so it returns null there. The
  // walkable check is terrain-only (the static `walkable` Set); a walkable but
  // unreachable island still lights and the click fails gracefully server-side
  // (accepted false positive, decision 6).
  const hoverMoveTileFor = (h: Hex | null): HoverMoveTile | null => {
    if (h === null || window.game.inCombat) {
      return null;
    }

    const me = window.game.me;
    if (me !== null && h.q === me.hex.q && h.r === me.hex.r) {
      return { hex: h, kind: "wait" }; // own hex = wait/cancel (decision 5)
    }

    if (walkable.has(`${h.q},${h.r}`)) {
      return { hex: h, kind: "walk" };
    }

    return null; // rock / water / off-map (decision 3)
  };

  const refreshHoverMove = (): void => {
    window.game.hoverMoveTile = hoverMoveTileFor(hoveredHex);
    hoverHighlightLayer.update(window.game.hoverMoveTile);
  };

  const setHoveredHex = (h: Hex | null): void => {
    if (h !== null && hoveredHex !== null && h.q === hoveredHex.q && h.r === hoveredHex.r) {
      return; // pointermove fires per pixel; recompute only on a hex change
    }

    hoveredHex = h;
    refreshAttackHighlight();
    refreshHoverMove();
  };

  const setCommittedAttackTiles = (tiles: Hex[]): void => {
    committedAttackTiles = tiles;
    refreshAttackHighlight();
  };

  // #138: the committed indicator (attack crosshair + lit target tiles) stays
  // lit through the resolving bundle's PLAYBACK, then clears — the hit animates
  // across the playback window, so clearing the instant the bundle ARRIVES
  // dropped the highlight ~2s before the attack visibly landed. The clear is a
  // deadline (committedClearAtMs = the resolving bundle's playback-end time)
  // that the render ticker watches, NOT a re-armable setTimeout: onTurn fires
  // per bundle, and cancelling+rescheduling a timer each time could starve it
  // forever (the bug that left the highlight stuck on). The deadline is set
  // ONCE, on the first bundle after a commit, and never pushed later.
  let committedClearAtMs: number | null = null;
  const cancelCommittedClear = (): void => {
    committedClearAtMs = null;
  };
  const clearCommittedIndicator = (): void => {
    cancelCommittedClear();
    window.game.committedAction = null;
    feedbackLayer.setCommitted(null);
    setCommittedAttackTiles([]);
  };
  const tickCommittedClear = (): void => {
    if (committedClearAtMs !== null && performance.now() >= committedClearAtMs) {
      clearCommittedIndicator();
    }
  };

  window.game.hoverTile = (q, r): void => setHoveredHex({ q, r });

  // Enemy hover tooltip content (item 13, playtest batch 2; #205). The hex
  // currently under the tooltip scan (#208: pointermove fires per pixel, but
  // resolving the monster + writing the content only needs to run on a hex
  // change) — shared with onTurn, which re-resolves it each bundle.
  let tooltipHex: Hex | null = null;
  // Resolve the monster under `hex` from the LATEST bundle's state and write
  // (or hide) the tooltip's kind/HP lines. Position (left/top) is not touched
  // here — it follows the cursor, set per-pixel in pointermove. Called both on
  // a hex change (pointermove) and on every turn bundle for the still-hovered
  // hex (#205): a monster that moves off/onto the hovered hex, or whose HP
  // changes or who dies, under a STATIONARY cursor would otherwise leave stale
  // content lingering until the next mouse move.
  const refreshTooltipContent = (hex: Hex): void => {
    const monster = window.game.positions.find(
      (p) => p.kind === EntityMonster && p.hex.q === hex.q && p.hex.r === hex.r,
    );
    if (monster === undefined) {
      hoverTooltipEl.hidden = true;
      return;
    }
    // Hover gating (item 6, playtest feedback batch 3): the HP line only shows
    // within CombatRadius of my own entity — scouting a distant monster
    // shouldn't read its exact health through the fog of distance. Beyond that
    // (or before I've joined) it's name-only.
    const me = window.game.me;
    const inRange = me !== null && hexDistance(me.hex, monster.hex) <= CombatRadius;

    hoverTooltipKindEl.textContent = monster.name;
    if (inRange) {
      const hp = window.game.hp[monster.id] ?? 0;
      const maxHp = window.game.maxHp[monster.id] ?? 0;
      // Reach is the one threat stat shown before contact (#201): a ranged
      // monster (Kin Archer) reads "reach 3" so you see it outranges you;
      // melee reads nothing extra. Damage and type are learned by being hit.
      const reach = monster.reach > 0 ? ` · reach ${monster.reach}` : "";
      hoverTooltipHPEl.textContent = `HP ${hp}/${maxHp}${reach}`;
      hoverTooltipHPEl.hidden = false;
    } else {
      hoverTooltipHPEl.textContent = "";
      hoverTooltipHPEl.hidden = true;
    }
    hoverTooltipEl.hidden = false;
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

  // The pending-item swap glyph rides my own dot, so it renders ABOVE the entity
  // layer (the rest of the feedback layer stays under — see above).
  world.addChild(feedbackLayer.overlay);

  // Floating damage numbers render above everything on the map.
  const damageLayer = new DamageNumberLayer(app.ticker);
  world.addChild(damageLayer.container);

  // Follow camera + smooth wheel zoom (#273/#274, Grim-Dawn/Diablo style). The
  // camera re-centres on my entity's *live* (per-frame interpolated) position
  // every frame, so the player is always screen-centred and the pan is as
  // smooth as the sprite's own movement — there is NO manual panning. `zoom`
  // eases toward the wheel-driven `targetZoom`; the whole `world` container
  // scales around the followed player. Runs every frame after EntityLayer's
  // tick (added first, so dot.current is already advanced this frame); reading
  // app.screen each frame keeps it centred across window resizes. Falls back to
  // the origin until my dot exists (pre-join).
  let zoom = 1;
  let targetZoom = 1;

  const updateCamera = (): void => {
    const dtSeconds = app.ticker.deltaMS / 1000;

    // Frame-rate-independent exponential easing toward the wheel's target zoom.
    zoom += (targetZoom - zoom) * (1 - Math.exp(-ZOOM_EASE_RATE * dtSeconds));
    world.scale.set(zoom);

    // Centre the followed player under the scaled world (no pan term).
    const p = entityLayer.myPixel() ?? hexToPixel({ q: 0, r: 0 });
    world.position.set(app.screen.width / 2 - p.x * zoom, app.screen.height / 2 - p.y * zoom);

    window.game.camera = { x: world.position.x, y: world.position.y };
    window.game.zoom = zoom;
  };
  updateCamera();
  app.ticker.add(updateCamera);

  // Mouse-wheel zoom (#273): each notch scales targetZoom multiplicatively
  // (deltaY < 0 = wheel up = zoom in), clamped; the ticker above eases toward
  // it. preventDefault stops the page from scrolling under the canvas.
  app.canvas.addEventListener(
    "wheel",
    (ev: WheelEvent): void => {
      ev.preventDefault();
      const factor = Math.exp(-ev.deltaY * ZOOM_WHEEL_SENSITIVITY);
      targetZoom = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, targetZoom * factor));
    },
    { passive: false },
  );

  // #138: clear the committed indicator the moment its resolving bundle's
  // playback ends (committedClearAtMs), watched here per frame rather than via
  // a timer that onTurn could keep rescheduling.
  app.ticker.add(tickCommittedClear);

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
  let joinedName: string;
  let joinedClass: string;
  let joinedSpecies: string;
  try {
    if (storedIdentity !== null && storedIdentity.token !== "") {
      me = await reclaim(storedIdentity);
      // A reclaim sends no name (reclaim-or-fail contract) and JoinResponse
      // doesn't carry one — the real name arrives with the first bundle
      // (onTurn's `window.game.name = mine.name`). Claiming selectedName here
      // reported the default "traveler" for a returning player until that
      // bundle landed (#208); "" is the honest "not known yet".
      joinedName = "";
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
      joinedName = selectedName;
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
    joinedName = selectedName;
    joinedClass = selectedClass;
    joinedSpecies = selectedSpecies;
  }
  window.game.me = { id: me.entityId, hex: me.hex };
  window.game.name = joinedName;
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
  const supersedeCommitted = (): void => clearCommittedIndicator();

  // A pending panel action (equip/unequip/drink/drop) gets the SAME feedback in
  // and out of combat — the pending state drives it, not the clock: the item's
  // panel badge (markPending) plus a ⇄ swap glyph on my hex. beginItemAction
  // plants both; the turn-bundle handler clears them once the action resolves
  // (re-derived from the pending set), and a superseding map click clears them
  // immediately via clearItemPending.
  const beginItemAction = (itemId: number): void => {
    supersedeCommitted();
    markPending(itemId);
    feedbackLayer.setItemAction(window.game.me?.hex ?? null);
    window.game.pendingItems = [...pending().keys()];
  };
  const clearItemPending = (): void => {
    clearPending();
    feedbackLayer.setItemAction(null);
    window.game.pendingItems = [];
  };
  // Clears one item's pending mark (a rejected action) while leaving any other
  // in-flight action's mark alone; the on-map ⇄ glyph stays while any remain.
  const clearOneItemPending = (itemId: number): void => {
    clearOnePending(itemId);
    const keys = [...pending().keys()];
    if (keys.length === 0) {
      feedbackLayer.setItemAction(null);
    }
    window.game.pendingItems = keys;
  };

  // Inventory panel toggle: the HUD button, the `i`/`c` keys, Escape, and the
  // panel's own close button all route through applyPanelOpen, which keeps
  // the store signal, the HUD button's open-state class, and
  // window.game.panelOpen in sync.
  const applyPanelOpen = (open: boolean): void => {
    if (panelOpen() !== open) {
      togglePanel();
    }
    toggleInventoryEl.classList.toggle("open", panelOpen());
    window.game.panelOpen = panelOpen();
  };
  const toggleInventory = (): void => applyPanelOpen(!panelOpen());

  // The skills panel (#124) toggles independently of the inventory: they are
  // different questions ("what am I carrying" vs "what can I become"), and a
  // player comparing a new skill against their gear wants both open.
  const applySkillsPanel = (): void => {
    toggleSkillsPanel();
    window.game.skillsPanelOpen = skillsPanelOpen();
  };

  mountSkills(mustGet("skills-root"), (skillId: string): void => {
    // Reflect an accepted learn at once (#124 follow-up): the server commits
    // it immediately, so waiting for the next bundle made an immediate action
    // look clock-gated. On rejection nothing changes, and the next bundle is
    // authoritative either way.
    void submitLearnSkill(identity, skillId).then((accepted) => {
      if (accepted) {
        applyLearnedLocally(skillId, SkillPointCost);
      }
    });
  });

  // A rejected panel action (its reason toasts via postIntent) must retract its
  // own pending mark: nothing changed server-side, so resolvePending — which
  // clears on a signature change — never would, leaving the spinner + ⇄ glyph
  // stuck until an unrelated map click (#193). clearOneItemPending clears just
  // this item, so a second in-flight action keeps its own mark.
  const rejectClears = (itemId: number) => (ok: boolean): void => {
    if (!ok) {
      clearOneItemPending(itemId);
    }
  };
  const characterActions = {
    equip: (itemId: number): void => {
      beginItemAction(itemId);
      void submitEquip(identity, itemId).then(rejectClears(itemId));
    },
    unequip: (itemId: number): void => {
      beginItemAction(itemId);
      void submitUnequip(identity, itemId).then(rejectClears(itemId));
    },
    drop: (itemId: number): void => {
      beginItemAction(itemId);
      void submitDrop(identity, itemId).then(rejectClears(itemId));
    },
    drink: (itemId: number): void => {
      beginItemAction(itemId);
      void submitDrink(identity, itemId).then(rejectClears(itemId));
    },
    // #271: arm a flask's throw — the next map click is the aim hex (armThrow
    // closes the panel so the map is clickable).
    arm: (itemId: number): void => armThrow(itemId),
    // #271: use a scroll of recall — teleport to safety (clock-gated, so it
    // gets the same pending badge as drink/equip).
    recall: (itemId: number): void => {
      beginItemAction(itemId);
      void submitRecall(identity, itemId).then(rejectClears(itemId));
    },
    close: (): void => applyPanelOpen(false),
  };

  mountStatTooltip(mustGet("tooltip-root"));
  mountCharacter(mustGet("character-root"), characterActions);
  mountPickup(mustGet("pickup-root"), {
    take: (groundItemId: number): void => {
      supersedeCommitted();
      // Pickup is clock-gated too (applies on a turn bundle), so give it its own
      // on-map indicator — a down-into-backpack glyph on my hex (distinct from
      // the ⇄ swap; a pickup isn't a gear swap) — plus a spinner on this row's
      // "take" button. Cleared on the next bundle (its resolution) or right away
      // if the take is rejected (backpack full).
      markTaking(groundItemId);
      feedbackLayer.setPickup(window.game.me?.hex ?? null);
      window.game.pickupPending = true;
      void submitPickup(identity, groundItemId).then(({ ok, reason }) => {
        if (!ok) {
          markPickupRejected(groundItemId, reason === "" ? undefined : reason);
          feedbackLayer.setPickup(null);
          window.game.pickupPending = false;
        }
      });
    },
  });

  // The HUD toggle button reveals now that there is a character to show; the
  // `i`/`c`/Escape keys are bound via bindMovementKeys below (sharing the
  // typing-focus guard). All route through toggleInventory/applyPanelOpen
  // (defined above).
  toggleInventoryEl.hidden = false;
  toggleInventoryEl.addEventListener("click", toggleInventory);
  mustGet("reset-fresh").addEventListener("click", () => window.location.reload());

  // Controls overlay button + close + first-run auto-show (#203).
  toggleHelpEl.hidden = false;
  toggleHelpEl.addEventListener("click", toggleControlsOverlay);
  mustGet("controls-close").addEventListener("click", () => setControlsOverlay(false));
  if (localStorage.getItem("mediumrogue.seenControls") === null) {
    localStorage.setItem("mediumrogue.seenControls", "1");
    setControlsOverlay(true);
  }

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
      cancelCommittedClear();
      window.game.committedAction = committed;
      feedbackLayer.setCommitted(committed);
      setCommittedAttackTiles([]); // a move/wait replaces any committed attack (#101)
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
  // intent (item 7, playtest batch 2): the server resolves against the
  // victim's pre-move hex (#104), tracking it by id rather than trusting a
  // stale hex.
  const hostileIdAt = (hex: Hex): number | null => {
    const hit = window.game.positions.find((p) => p.kind === EntityMonster && p.hex.q === hex.q && p.hex.r === hex.r);
    return hit === undefined ? null : hit.id;
  };

  // attackAt fires a ranged attack intent at target: no destination bookkeeping
  // (the attacker doesn't move onto it) — a one-shot flash on the target hex
  // acknowledges the click; the turn bundle's HP changes speak for the result.
  // When no held AoE weapon reaches the target (a single-target shot, a bow),
  // it targets the hostile's ENTITY id instead of the bare hex (item 7); with
  // an AoE weapon in reach it stays ground-targeted — the blast radius makes
  // a hex the natural target, and it can land on empty ground and still
  // catch nearby hostiles.
  const attackAt = (target: Hex): Promise<void> => {
    feedbackLayer.flashAttack(target);
    window.game.lastAttackFlash = target;

    const isAoe = aoeReaches(target);
    const targetEntityId = isAoe ? 0 : (hostileIdAt(target) ?? 0);

    // Committed-action indicator (item 6): a persistent crosshair on the
    // target, alongside the flashAttack one-shot ring above — and the full
    // target-tile set stays highlighted until the turn resolves (#101). An
    // AoE gets NO centre crosshair (#138): the lit blast disc IS the
    // indicator, and a single-target mark on the disc's centre misreads as
    // "one victim here". A single-target shot keeps its crosshair.
    const committed: CommittedAction | null = isAoe ? null : { kind: "attack", target };
    cancelCommittedClear();
    window.game.committedAction = committed;
    feedbackLayer.setCommitted(committed);
    const committedTiles = attackTilesFor(target);
    setCommittedAttackTiles(committedTiles);

    // On a rejected attack (stale/out-of-range target — the #130/#133 422 the
    // client used to ignore) the reason toasts via postIntent; here we also
    // retract the committed crosshair + lit tiles, mirroring walkTo. The
    // reference-equality guard leaves a *newer* commit alone: a later click
    // replaces committedAttackTiles with a different array, so a straggler
    // reject for this target no longer matches and is a no-op (#193).
    return submitIntent(identity, target, IntentAttack, targetEntityId).then((accepted) => {
      if (!accepted && committedAttackTiles === committedTiles) {
        clearCommittedIndicator();
      }
    });
  };

  // meleeAt submits a melee attack at an adjacent hostile: mechanically an
  // entity-targeted ATTACK intent now (#116) — one click = one swing, parity
  // with ranged. The server rejects a stale/empty target (targetEntityId 0
  // would be a ground shot a melee-only class can't make), so if
  // hostileIdAt returns null we fall back to walkTo(target): the melee tile
  // routing means a hostile is there in practice, but a bundle race is
  // possible (the hostile left between the overlay computing reach and the
  // click landing).
  const meleeAt = (target: Hex): Promise<void> => {
    const targetEntityId = hostileIdAt(target);
    if (targetEntityId === null) {
      return walkTo(target); // the hostile left this bundle — treat as a step
    }

    feedbackLayer.flashAttack(target);
    window.game.lastAttackFlash = target;

    const committed: CommittedAction = { kind: "attack", target };
    cancelCommittedClear();
    window.game.committedAction = committed;
    feedbackLayer.setCommitted(committed);
    const committedTiles = [target]; // a melee swing hits exactly its victim's tile (#101)
    setCommittedAttackTiles(committedTiles);

    // Retract the committed swing if the server rejects it (the reason toasts
    // via postIntent). Same reference-equality guard as attackAt: a newer
    // commit replaces committedAttackTiles, so a straggler reject is a no-op.
    return submitIntent(identity, target, IntentAttack, targetEntityId).then((accepted) => {
      if (!accepted && committedAttackTiles === committedTiles) {
        clearCommittedIndicator();
      }
    });
  };

  // clickTarget is the single decision point shared by canvas clicks and
  // window.game.tapHex, so tapHex genuinely mirrors "as if the hex were
  // clicked" for tests. Out of combat this is the pre-classes behavior:
  // click-anywhere pathing (ranged clicks only exist in combat). IN combat,
  // the tinted overlay is the contract: blue = step there, strong red
  // (adjacent hostile) = melee attack, light red (weapon reach) = shoot when
  // an enemy is on the hex (or anywhere in it, for AoE), own hex = stand
  // still/cancel; anything else is not a valid selection. One deliberate
  // class nuance: an AoE caster (mage) blasts an adjacent hostile rather
  // than staff-bonking it — its ranged weapon IS its real weapon — while a
  // bow user (rogue) melee-attacks adjacent hostiles with the dagger, the plan's
  // "weapon by distance" identity.
  // Action bar (#185): four slots holding the player's learned ACTIVE skills,
  // in learn order (assignment is automatic first-free-slot with no manual
  // reordering, so the layout is fully derived from the learned set — no
  // server slot state or snapshot bump needed). Keys 1–4 or a click arm the
  // skill; the next map click sends it at that hex. Blink is the only active
  // today, so a player who has learned it sees slot 1 filled, 2–4 empty.
  const actionBarEl = mustGet("action-bar");
  const actionSlotEls = Array.from(actionBarEl.querySelectorAll<HTMLElement>(".aslot"));
  let armedSkill: string | null = null;
  // #271: the owned flask instance id whose throw is armed — the next map click
  // becomes its aim hex. Mutually exclusive with armedSkill (arming one cancels
  // the other). null when nothing is armed to throw.
  let armedThrow: number | null = null;

  const activeSlots = (): { id: string; name: string; ready: boolean; cd: number }[] =>
    window.game.skills
      .filter((sk) => sk.learned && sk.active)
      .map((sk) => ({ id: sk.id, name: sk.name, ready: sk.turnsUntilReady === 0, cd: sk.turnsUntilReady }));

  function renderActionBar(): void {
    const slots = activeSlots();
    actionBarEl.classList.toggle("shown", slots.length > 0);
    actionSlotEls.forEach((el, i) => {
      const s = slots[i];
      const lbl = el.querySelector<HTMLElement>(".lbl");
      const existingCd = el.querySelector(".cd");
      if (existingCd !== null) existingCd.remove();
      el.classList.remove("filled", "cooling", "arming");
      if (s === undefined) {
        if (lbl !== null) lbl.textContent = "—";
        return;
      }
      if (lbl !== null) lbl.textContent = s.name;
      if (s.ready) {
        el.classList.add("filled");
        if (armedSkill === s.id) el.classList.add("arming");
      } else {
        el.classList.add("cooling");
        const cd = document.createElement("span");
        cd.className = "cd";
        cd.textContent = String(s.cd);
        el.appendChild(cd);
      }
    });
  }

  const cancelArm = (): void => {
    if (armedSkill !== null || armedThrow !== null) {
      armedSkill = null;
      armedThrow = null;
      window.game.armedThrow = null;
      renderActionBar();
    }
  };

  const armSlot = (i: number): void => {
    const s = activeSlots()[i];
    if (s === undefined || !s.ready) {
      return; // empty or cooling slot: nothing to arm
    }
    armedThrow = null; // arming a skill cancels a queued throw
    window.game.armedThrow = null;
    armedSkill = armedSkill === s.id ? null : s.id; // toggle
    renderActionBar();
  };
  actionSlotEls.forEach((el, i) => el.addEventListener("click", () => armSlot(i)));
  window.game.armedSkill = (): string | null => armedSkill;

  // armThrow arms a flask's throw (#271): the panel closes so the map is
  // clickable, and the next clickTarget consumes the click as the aim hex.
  // Arming a throw cancels any armed skill (one thing armed at a time).
  const armThrow = (itemId: number): void => {
    armedSkill = null;
    armedThrow = itemId;
    window.game.armedThrow = itemId;
    renderActionBar();
    applyPanelOpen(false);
  };

  const clickTarget = (target: Hex): Promise<void> => {
    // #271: an armed throw consumes the next map click as the flask's aim hex.
    if (armedThrow !== null) {
      const itemId = armedThrow;
      armedThrow = null;
      window.game.armedThrow = null;
      clearItemPending(); // a real intent replaces a queued in-bubble action
      return submitThrow(identity, itemId, target).then(() => undefined);
    }

    // #185: an armed active consumes the next map click as its target.
    if (armedSkill !== null) {
      const skill = armedSkill;
      armedSkill = null;
      renderActionBar();
      return submitUseSkill(identity, skill, target).then(() => undefined);
    }

    if (window.game.inCombat) {
      const self =
        window.game.me !== null && window.game.me.hex.q === target.q && window.game.me.hex.r === target.r;

      if (self || inList(lastReach.moves, target)) {
        clearItemPending(); // a real intent replaces a queued in-bubble action
        return walkTo(target);
      }

      if (inList(lastReach.melees, target)) {
        clearItemPending();
        if (aoeReaches(target)) {
          return attackAt(target); // mage: blast the adjacent hostile
        }
        return meleeAt(target); // melee attack: swing, with attack feedback (#113)
      }

      if (isRangedAttackClick(target)) {
        clearItemPending();
        return attackAt(target);
      }

      return Promise.resolve(); // out of this turn's reach: not a valid selection
    }

    // A map click replaces a queued in-bubble equip server-side (latest intent
    // wins) — release its pending panel badge + swap glyph to match.
    clearItemPending();

    return walkTo(target);
  };

  window.game.tapHex = (q, r): Promise<void> => clickTarget({ q, r });

  // The inverse of the pointerdown mapping below (hex → world pixel → canvas
  // point → client point), reading the live rect/camera so a test's click
  // lands wherever the hex is drawn RIGHT NOW. See GameDebug.hexToScreen.
  window.game.hexToScreen = (q, r): { x: number; y: number } => {
    const rect = app.canvas.getBoundingClientRect();
    const p = hexToPixel({ q, r });

    // world.position already folds in the follow camera + zoom (updateCamera);
    // the world point is then scaled by zoom (#273/#274). Inverse of the
    // pointerdown un-projection, so a click round-trips to the hex drawn now.
    return { x: rect.left + world.position.x + p.x * zoom, y: rect.top + world.position.y + p.y * zoom };
  };

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
      // Stamped BEFORE any work: paired with turnApplied at the bottom, the
      // two bracket the handler, so a mid-handler throw is visible as a gap
      // rather than as silence (#170).
      window.game.turnReceived = event.turn;
      turnReceivedEl.textContent = String(event.turn);
      // Update the stuck marker HERE too, not only at the end: a mid-handler
      // throw never reaches the tail call, so this is the point where
      // turnReceived can lead turnApplied and the marker can actually show.
      applyStatus();

      if (firstWorldID === null) {
        firstWorldID = event.worldId;
      } else if (event.worldId !== firstWorldID) {
        // #204: say the world reset (character gone) instead of a bare reload.
        resetCardEl.hidden = false;
        return;
      }

      // Committed-action indicator (item 6): this bundle resolves what I chose
      // last input window, but the resolution PLAYS OUT over the playback
      // window — so keep the indicator lit and schedule its clear at playback
      // end below (#138), rather than dropping it the instant the bundle
      // arrives (which cut the highlight ~2s before the attack visibly landed).

      // #114: per-hit combat moments. The bundle keeps a few turns of hits
      // for coalescing slack (protocol.HitView's contract) — only those newer
      // than the previously processed bundle are new this frame. They style
      // the HP-diff numbers below; the HP delta stays the authoritative
      // amount (a diff can sum several hits — crit styling wins when a
      // victim took both kinds in one bundle).
      const freshHits = event.hits.filter((h) => h.turn > window.game.turn);
      const momentFor = new Map<number, { crit: boolean; glance: boolean }>();
      for (const h of freshHits) {
        const m = momentFor.get(h.victimId) ?? { crit: false, glance: false };
        m.crit ||= h.crit;
        m.glance ||= h.glance;
        momentFor.set(h.victimId, m);
      }
      window.game.hits = freshHits;

      // Derive floating damage numbers by diffing this bundle's HP against the
      // previous one (still in window.game from the last onTurn): an entity
      // with less HP took a hit; a monster missing entirely died, its killing
      // blow shown as the HP it had left. First bundle diffs against nothing.
      const prevHp = window.game.hp;
      const prevPositions = window.game.positions;
      const damage: { id: number; amount: number; crit: boolean; glance: boolean }[] = [];
      const present = new Set(event.entities.map((e) => e.id));
      const styleFor = (id: number): HitStyle => {
        const m = momentFor.get(id);

        return m?.crit ? "crit" : m?.glance ? "glance" : "normal";
      };
      for (const e of event.entities) {
        const before = prevHp[e.id];
        if (before !== undefined && e.hp < before) {
          const m = momentFor.get(e.id);
          damage.push({ id: e.id, amount: before - e.hp, crit: m?.crit ?? false, glance: m?.glance ?? false });
          damageLayer.spawn(e.hex, before - e.hp, e.kind === EntityPlayer, styleFor(e.id));
        }
      }
      for (const p of prevPositions) {
        const before = prevHp[p.id];
        if (!present.has(p.id) && p.kind === EntityMonster && before !== undefined && before > 0) {
          const m = momentFor.get(p.id);
          damage.push({ id: p.id, amount: before, crit: m?.crit ?? false, glance: m?.glance ?? false });
          damageLayer.spawn(p.hex, before, false, styleFor(p.id));
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
        reach: e.reach,
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

      // #138: on the FIRST bundle after a commit (deadline still unset), keep
      // the indicator lit and schedule its clear for this bundle's playback
      // end. Guarding on `=== null` is what makes it robust: later bundles
      // never push the deadline out, so it can't be starved into never firing
      // (the stuck-highlight bug). A fresh commit resets it via the setters'
      // cancelCommittedClear; tickCommittedClear does the actual clearing.
      if (committedClearAtMs === null && (window.game.committedAction !== null || committedAttackTiles.length > 0)) {
        committedClearAtMs = turnStartedAtMs + playbackMs;
      }

      const mine = event.entities.find((e) => e.id === me.entityId);
      if (mine !== undefined && window.game.me !== null) {
        window.game.me.hex = mine.hex;
        // Arrived at the destination — or, for a hostile-held destination,
        // arrived ADJACENT to it (#116: the server trims such walks one hex
        // short; without this mirror the ring would linger forever).
        const dest = window.game.destination;
        if (dest !== null) {
          const arrived = mine.hex.q === dest.q && mine.hex.r === dest.r;
          const hostileHeld = window.game.positions.some(
            (p) => p.kind === EntityMonster && p.hex.q === dest.q && p.hex.r === dest.r,
          );
          const adjacent = hexDistance(mine.hex, dest) === 1;
          if (arrived || (hostileHeld && adjacent)) {
            window.game.destination = null;
            feedbackLayer.setDestination(null);
          }
        }

        // #204: a drop in xp means I died this bundle (the level floor). Name
        // the killer from this turn's hits and the loss from the delta.
        if (prevXp >= 0 && mine.xp < prevXp) {
          const killerId = freshHits.find((h) => h.victimId === me.entityId)?.attackerId;
          const killer = event.entities.find((e) => e.id === killerId)?.name ?? "";
          showDeath(killer, prevXp - mine.xp);
        }
        prevXp = mine.xp;

        window.game.xp = mine.xp;
        window.game.level = mine.level;
        window.game.class = mine.class;
        window.game.species = mine.species;
        window.game.name = mine.name;
        const xpFloor = XPCurveBase * (mine.level - 1) * (mine.level - 1);
        const xpNext = XPCurveBase * mine.level * mine.level;
        // Position readout (item 9, playtest batch 2): live per bundle, so
        // it never drifts from the server-authoritative hex even mid-tween.
        statsEl.textContent = `Lv ${mine.level} · ${mine.hp}/${mine.maxHp} HP · ${mine.xp - xpFloor}/${xpNext - xpFloor} XP · (${mine.hex.q}, ${mine.hex.r})`;

        // #202: my level rose since the last bundle → flash the banner.
        // prevLevel 0 guards the first bundle (fresh/reclaimed join).
        if (prevLevel > 0 && mine.level > prevLevel) {
          showLevelUp(prevLevel, mine.level, mine.species);
        }
        prevLevel = mine.level;

        // Gear: my owned items ride Entity.Items every bundle (full-snapshot
        // philosophy, same as everything else here). setInventory feeds the
        // paper-doll's slot-keyed equipped map + backpack; the mirrors below
        // expose the same to window.game for e2e. The click-vs-move UX hint
        // above (isRangedAttackClick) reads every held ranged/magic weapon
        // across BOTH hands (dual-wield, gear keystone #55) — an equipped
        // weapon's Type is now the hand it occupies, not the generic
        // taxonomy string, so filtering is on Tags alone (a ranged or magic
        // tag means it fires the ranged/AoE attack path regardless of which
        // hand holds it).
        setInventory(mine.items);
        // Skills ride the same per-turn rebuild as gear (#124). The server
        // sends only this viewer's own rows (own-only) and only the learned
        // + currently-learnable ones (near-sighted), so the client stores
        // exactly what it renders.
        setSkills(mine.skills ?? [], mine.skillPoints ?? 0);
        window.game.skills = (mine.skills ?? []).map((s: SkillView) => ({
          id: s.id,
          name: s.name,
          tree: s.tree,
          learned: s.learned,
          active: s.active,
          cooldownTurns: s.cooldownTurns,
          rangeHex: s.rangeHex,
          turnsUntilReady: s.turnsUntilReady,
        }));
        renderActionBar();
        window.game.skillPoints = mine.skillPoints ?? 0;
        window.game.inventory = mine.items.map((it: ItemView) => ({
          id: it.id,
          defId: it.defId,
          equipped: it.equipped,
        }));
        window.game.equipped = equippedSignal();
        window.game.backpack = backpackSignal();

        myRangedWeapons = mine.items
          // `?? []` is belt-and-braces: the server now guarantees a non-nil
          // tags array (wireTags, world.go), but an exception thrown HERE
          // escapes onTurn and stops all rendering while SSE stays connected
          // — a total freeze from one bad field. Cheap insurance.
          .filter(
            (it: ItemView) =>
              it.equipped &&
              ((it.tags ?? []).includes(WeaponTagRanged) || (it.tags ?? []).includes(WeaponTagMagic)),
          )
          .map((it: ItemView) => ({ rangeHex: it.rangeHex, aoeRadius: it.aoeRadius }));
      }

      // Ground loot: every dropped item currently lying on the map, redrawn
      // wholesale each turn (full-snapshot philosophy) regardless of join
      // status — a drop is visible to everyone, not just its eventual picker.
      groundItemLayer.update(event.groundItems);
      window.game.groundItems = event.groundItems.map((gi: GroundItemView) => ({
        id: gi.id,
        hex: gi.hex,
        count: gi.count,
      }));

      // Pickup modal (inventory-slots milestone): every ground stack lying on
      // MY current hex becomes a modal row (name + type + count). The modal
      // opens on walk-over regardless of the character panel; it stays
      // dismissed while I remain on the hex (refreshPickup tracks a hex change
      // to reopen on re-entry). moved = my hex differs from the previous bundle's.
      const myHex = mine?.hex ?? null;
      const moved = myHex !== null && (lastPickupHex === null || myHex.q !== lastPickupHex.q || myHex.r !== lastPickupHex.r);
      lastPickupHex = myHex;

      // Pending item-action feedback, re-derived from the (setInventory-resolved)
      // pending set: the ⇄ swap glyph rides my hex while any equip/unequip/drink/
      // drop is still in flight, cleared once this bundle reflects the change.
      // Combat-agnostic — the pending set, not the clock, drives it.
      feedbackLayer.setItemAction(pending().size > 0 ? myHex : null);
      window.game.pendingItems = [...pending().keys()];

      // A pending pickup clears on the next bundle — the very bundle that
      // carries the picked-up item into the backpack (or the world tick after
      // an out-of-combat take). Cleared unconditionally: the take set it since
      // the previous bundle, and this bundle is its resolution.
      feedbackLayer.setPickup(null);
      window.game.pickupPending = false;
      const rowsHere =
        myHex === null
          ? []
          : event.groundItems
              .filter((gi) => gi.hex.q === myHex.q && gi.hex.r === myHex.r)
              .map((gi: GroundItemView) => ({
                id: gi.id,
                name: gi.name,
                type: gi.type,
                count: gi.count,
                // Detail fields (#139) — what the item IS, shown before pickup.
                damage: gi.damage,
                rangeHex: gi.rangeHex,
                aoeRadius: gi.aoeRadius,
                stats: gi.stats,
                flavor: gi.flavor,
                tags: gi.tags,
                damageType: gi.damageType,
                twoHanded: gi.twoHanded,
              }));
      refreshPickup(rowsHere, moved);
      window.game.pickupModal = pickupModalMirror();
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
      const wasInCombat = window.game.inCombat;
      window.game.inCombat = (mine?.inCombat ?? false) || myBubble !== null;

      // Entering combat hard-cancels a pending auto-walk (#103): the server
      // clears the queued route on bubble entry, so drop the destination and
      // its ring too — otherwise a stale goal marker lingers for a walk that
      // will never resume.
      if (!wasInCombat && window.game.inCombat && window.game.destination !== null) {
        window.game.destination = null;
        feedbackLayer.setDestination(null);
      }

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
        window.game.combatMoves = [...reach.moves, ...reach.melees];

        // The held ranged weapons' reach: every map tile within the FARTHEST
        // weapon's range (distance-only, no LOS — matching the server's
        // rule), minus the tiles that already act differently on click
        // (moves, melees, self). Max range is right for a wash — it shows
        // "some weapon can act here"; the click routing itself is per-weapon
        // (isRangedAttackClick).
        const ranged: Hex[] = [];
        const meNow = window.game.me;
        const washRange = maxRangedRange();
        if (meNow !== null && washRange > 0) {
          for (const tile of map.tiles) {
            const d = hexDistance(meNow.hex, tile.hex);
            if (
              d >= 1 &&
              d <= washRange &&
              !inList(reach.moves, tile.hex) &&
              !inList(reach.melees, tile.hex)
            ) {
              ranged.push(tile.hex);
            }
          }
        }
        window.game.combatRanged = ranged;
        moveRangeLayer.update(reach.moves, reach.melees, ranged);
      } else {
        lastReach = { moves: [], melees: [] };
        window.game.combatMoves = [];
        window.game.combatRanged = [];
        moveRangeLayer.update([], [], []);
      }

      // Re-derive the hover highlights from the state this handler just
      // refreshed — the mouse hasn't moved, but reach/positions/weapons have
      // (#101), and inCombat / my own hex may have flipped (#135).
      refreshAttackHighlight();
      refreshHoverMove();

      // #205: the enemy hover tooltip's content is otherwise recomputed only on
      // cursor movement, so a monster that moves off/onto the hovered hex, or
      // whose HP changes or who dies, under a STATIONARY cursor leaves a stale
      // tooltip until the next mouse move. Re-resolve the still-hovered hex from
      // this bundle's state (hides the tooltip when the monster left or died).
      // The cursor hasn't moved, so its screen position is untouched.
      if (tooltipHex !== null) {
        refreshTooltipContent(tooltipHex);
      }

      // LAST line of the handler, deliberately: reaching it is the only proof
      // that the whole bundle was applied. Anything that throws above leaves
      // turnApplied behind turnReceived, which is what the HUD and the e2e
      // guard both watch (#170).
      window.game.turnApplied = event.turn;
      applyStatus();
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

  // Keyboard bindings (#273 dropped QWEASD character movement; #274 settled the
  // camera as a pure follow camera with wheel zoom, so there are no camera keys
  // at all — character movement is click/tap only). isBlocked guards the start
  // screen (item 10): a not-yet-real character must never act while its
  // class/species is being chosen.
  bindMovementKeys({
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
    // `i` or `c` toggles the character/inventory panel, Escape closes it —
    // shares the typing-focus guard (input/keys.ts) so typing "i"/"c"/Escape
    // into chat never touches the panel, and the same start-screen block below.
    // (#273 briefly gave `c` to camera-recenter; #274's follow camera has no
    // recenter, so `c` is a panel alias again.) Escape's isPanelOpen gate lives
    // in keys.ts (a no-op while already closed).
    onToggleInventory: toggleInventory,
    onToggleSkills: applySkillsPanel,
    onToggleHelp: toggleControlsOverlay,
    onActionSlot: armSlot,
    // Esc closes ANY open surface (#203): the controls overlay, the character
    // panel, and the skills panel — previously only the character panel.
    onClosePanel: (): void => {
      cancelArm();
      setControlsOverlay(false);
      applyPanelOpen(false);
      if (skillsPanelOpen()) {
        applySkillsPanel();
      }
    },
    isPanelOpen: (): boolean =>
      panelOpen() || skillsPanelOpen() || isControlsOverlayOpen() || armedSkill !== null || armedThrow !== null,
    isBlocked: (): boolean => !startScreenEl.hidden,
  });

  // Click-to-move (or, in combat with a ranged class, click-to-attack): canvas
  // point → world point (undo the follow camera's centring translate, then
  // ÷ zoom — #273/#274) → hex → clickTarget's move-vs-attack decision. A small
  // cursor affordance previews which one a hover would trigger. world.position
  // already folds in the follow + zoom, so no separate pan term is needed.
  app.canvas.addEventListener("pointerdown", (ev: PointerEvent): void => {
    if (ev.button !== 0) {
      return;
    }

    const rect = app.canvas.getBoundingClientRect();
    const worldX = (ev.clientX - rect.left - world.position.x) / zoom;
    const worldY = (ev.clientY - rect.top - world.position.y) / zoom;
    clickTarget(pixelToHex({ x: worldX, y: worldY }));
  });

  app.canvas.addEventListener("pointermove", (ev: PointerEvent): void => {
    const rect = app.canvas.getBoundingClientRect();
    const worldX = (ev.clientX - rect.left - world.position.x) / zoom;
    const worldY = (ev.clientY - rect.top - world.position.y) / zoom;
    // Crosshair wherever a click would attack — a shot OR a melee swing
    // (#113: a melee swing is a committed attack since #104, so it earns the
    // same pre-click affordance as a ranged target; see clickTarget's routing).
    const hover = pixelToHex({ x: worldX, y: worldY });
    const wouldAttack = isRangedAttackClick(hover) || inList(lastReach.melees, hover);
    app.canvas.style.cursor = wouldAttack ? "crosshair" : "default";

    // Attack-target highlight (#101): light up the exact tile(s) a click on
    // the hovered hex would hit (no-op when it wouldn't attack).
    setHoveredHex(hover);

    // Enemy hover tooltip (item 13, playtest batch 2): kind display name +
    // "HP cur/max", near the cursor. pointer-events: none on the tooltip
    // itself (index.html) means it can never intercept the click it's
    // floating over. Content is recomputed only on a hex change (#208) —
    // and, under a still cursor, on each turn bundle (#205, in onTurn); the
    // cheap left/top writes below stay per-pixel so the tooltip keeps
    // following the cursor within a hex.
    if (tooltipHex === null || hover.q !== tooltipHex.q || hover.r !== tooltipHex.r) {
      tooltipHex = hover;
      refreshTooltipContent(hover);
    }
    if (!hoverTooltipEl.hidden) {
      hoverTooltipEl.style.left = `${ev.clientX + 14}px`;
      hoverTooltipEl.style.top = `${ev.clientY + 14}px`;
    }
  });

  app.canvas.addEventListener("pointerleave", () => {
    hoverTooltipEl.hidden = true;
    tooltipHex = null; // re-entering at the same hex must rescan
    setHoveredHex(null);
  });
}

// #204: server-down at load retries with a visible attempt count instead of a
// dead "failed to start" over a black page. A JoinRejectedError is handled
// inside start() (dead identity → start screen); only an infra failure (server
// unreachable) reaches here, and it is worth retrying.
let bootAttempt = 0;
function boot(): void {
  start().catch((err: unknown) => {
    bootAttempt += 1;
    statusEl.textContent = `can't reach the server — retrying… (attempt ${bootAttempt}: ${String(err)})`;
    window.setTimeout(boot, 2000);
  });
}
boot();
