// window.game is the debug/testing surface: Playwright (and a curious human in
// devtools) reads live state through it. Testability is a design rule — every
// feature keeps this in sync. See §6 of the plan.
//
// This is the SHAPE only (the interface + the `Window.game` global) — extracted
// from main.ts (#213) as a pure-type module with no runtime logic. main.ts
// still owns the live object and writes every field at the same times it always
// did, and re-exports GameDebug so the e2e specs' `import { GameDebug } from
// "../src/main"` keeps resolving unchanged.
import type { Hex, HitView, QuestView } from "./protocol.gen";
import type { CommittedAction } from "./render/feedback";
import type { HoverMoveTile } from "./render/hover";

export interface GameDebug {
  turn: number;
  connected: boolean;
  /**
   * The turn number of the last bundle RECEIVED — stamped at the very top of
   * onTurn, before any work (#170).
   */
  turnReceived: number;
  /**
   * The turn number of the last bundle fully APPLIED — stamped at the very
   * END of onTurn, after every layer has been updated.
   *
   * The gap between these two is the whole point: an exception mid-handler
   * leaves rendering dead while the stream stays healthy, and on 2026-07-19
   * that shipped as "connected" plus a frozen map. `turn` itself is no use as
   * a guard, because it is assigned EARLY in the handler and kept advancing
   * right through that outage.
   */
  turnApplied: number;
  /**
   * Count of turn bundles PROCESSED this session — increments by exactly 1
   * per applied bundle (#252). Unlike `turn`, which can jump when the hub
   * coalesces, this is the clock for "N bundles later" test contracts: the
   * committed-indicator clear is bounded in bundles seen, not turn numbers.
   */
  bundles: number;
  /**
   * The last uncaught client error, or null. Set by the global handlers that
   * also raise the on-screen banner.
   */
  clientError: string | null;
  /** The level my last level-up banner announced (#202); 0 = none yet. */
  lastLevelUp: number;
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
  positions: { id: number; hex: Hex; kind: string; monsterKind: string; name: string; reach: number }[];
  /**
   * The viewer's OWN skills from the latest bundle (#124) — id, tree, and
   * whether it is learned. Near-sighted by construction: a locked skill is
   * never sent, so a missing id here means "not learnable yet", not "hidden".
   */
  skills: {
    id: string;
    name: string;
    tree: string;
    learned: boolean;
    active: boolean;
    cooldownTurns: number;
    rangeHex: number;
    turnsUntilReady: number;
  }[];
  /** The viewer's unspent skill-point bank. */
  skillPoints: number;
  /** Whether the skills panel is open (toggled by the `k` key). */
  skillsPanelOpen: boolean;
  /** Whether the controls overlay is open (#203). */
  controlsOpen: boolean;
  /** The action-bar skill currently armed for targeting, or null (#185). */
  armedSkill: () => string | null;
  /** The flask instance id whose throw is armed for targeting, or null (#271). */
  armedThrow: number | null;
  /** Whether the death card is showing (#204). */
  died: boolean;
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
  /**
   * The world container's live screen offset (`world.position`) under the
   * follow camera (#273/#274): it re-centres on `me` every frame and folds in
   * the zoom scale (`− playerPixel × zoom`). This is the exact translate a
   * screen→world un-projection must subtract, and an entity's live screen pixel
   * is `rect.left + camera.x + hexPixel.x × zoom`.
   */
  camera: { x: number; y: number };
  /**
   * The current follow-camera zoom, eased toward the wheel's target each frame
   * (#273/#274). 1 = unzoomed; clamped to [0.5, 2.5]. A screen→world un-projection
   * divides by this after subtracting `camera`; hexToScreen multiplies by it.
   */
  zoom: number;
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
  /**
   * Convert a hex to VIEWPORT pixel coordinates at its centre — the exact
   * inverse of the canvas pointerdown mapping (client point − canvas rect −
   * world offset → hex), computed from the live canvas rect and camera at
   * call time. Lets an e2e drive a REAL page.mouse.click on the canvas
   * (chat.spec.ts's pointer-events guard, #89) instead of tapHex's synthetic
   * clickTarget path. Null until the renderer is on stage.
   */
  hexToScreen: ((q: number, r: number) => { x: number; y: number }) | null;
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
   * My equipped gear keyed by one of the eight equip slots (helmet, amulet,
   * gloves, ring, main-hand, chest, off-hand, boots) — the paper-doll's
   * filled slots, from the latest bundle. Empty slots are absent keys.
   * Exposed for e2e (the panel itself is DOM).
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
   * ground stack's id/name/type/count and whether a take was rejected).
   * Exposed for e2e; the modal itself is DOM.
   */
  pickupModal: {
    open: boolean;
    rows: {
      id: number;
      name: string;
      type: string;
      count: number;
      rejected: boolean;
      damage: number;
      rangeHex: number;
      aoeRadius: number;
    }[];
  };
  /**
   * Test hook: mark a pickup-modal row rejected (the backpack-full feedback
   * render path), so e2e can exercise the inline ".full" feedback without a
   * server that can produce a full backpack from class defaults. Drives only
   * the client render — the server rejection itself is integration-tested.
   */
  rejectPickupRow: (groundItemId: number) => void;
  /** Every item lying on the ground, from the latest bundle (count = stack size). */
  groundItems: { id: number; hex: Hex; count: number }[];
  /**
   * Damage inferred from the latest bundle by diffing HP against the previous
   * one (the wire carries state — the HP delta stays the authoritative
   * number): one entry per entity that lost HP, plus one per monster that
   * vanished (its killing blow, shown as the HP it had left). Since #114 each
   * entry also carries crit/glance, derived from the bundle's per-hit Hits
   * view (see `hits`), which styles the floating combat numbers. Exposed for
   * e2e.
   */
  damage: { id: number; amount: number; crit: boolean; glance: boolean }[];
  /**
   * The per-hit combat moments new in the latest bundle (#114): every
   * TurnEvent.Hits entry whose turn is newer than the previously processed
   * bundle (the wire keeps a few turns of hits for coalescing slack — see
   * protocol.HitView). crit = an attacker-side chance boost fired (elf
   * passive, Misericorde, Duelist's Saber); glance = the defender-side
   * halving (Rogue passive). Purely cosmetic; exposed for e2e.
   */
  hits: HitView[];
  /**
   * The tiles the attack a click on the currently hovered hex would hit
   * (#101): empty when the hover would not attack (out of combat, a move
   * tile, out of range). A single-target attack (melee swing, bow shot)
   * lights one tile; a ground-targeted AoE (weapon aoeRadius > 0) lights the
   * full blast disc. Drives AttackHighlightLayer's hover state; exposed for
   * e2e (the highlight itself is a canvas draw).
   */
  hoverAttackTiles: Hex[];
  /**
   * The world (out-of-combat) hover highlight (#135): the single tile a click
   * on the currently hovered hex would act on — `"walk"` for a walkable hex,
   * `"wait"` for my own hex (a wait/cancel) — or null in combat, on unwalkable
   * ground, or with no hover. Drives HoverHighlightLayer; exposed for e2e.
   */
  hoverMoveTile: HoverMoveTile | null;
  /**
   * The tiles my committed attack will hit (#101), set the moment an attack
   * intent is submitted and cleared when the next bundle resolves it (or a
   * later intent supersedes it) — the on-map committed/pending indicator's
   * tile set, same lifecycle as committedAction. Exposed for e2e.
   */
  committedAttackTiles: Hex[];
  /**
   * Report a pointer hover on a hex as if the mouse moved there (drives
   * e2e — the same code path as the canvas pointermove handler's highlight
   * computation). Synchronous: hoverAttackTiles is up to date on return.
   */
  hoverTile: (q: number, r: number) => void;
  /**
   * The hexes my entity can act on THIS combat turn (moves + melee attacks),
   * from the latest bundle. Empty outside a bubble — out there, click-anywhere
   * pathing applies and no restriction exists. Drives the tactical overlay
   * and the in-combat click filter; exposed for e2e.
   */
  combatMoves: Hex[];
  /**
   * The hexes within my equipped ranged weapon's reach this combat turn
   * (excluding move/melee tiles — those act differently on click). Clicking
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
  /**
   * The target hex of the most recent attack-feedback flash
   * (FeedbackLayer.flashAttack) — set synchronously by both a ranged click
   * and a melee click (#113), never cleared (a "last event" record,
   * not live state; the flash itself fades in FLASH_DURATION_MS). Exposed
   * for e2e: the flash is a 450ms one-shot, so tests read this instead of
   * racing the animation.
   */
  lastAttackFlash: Hex | null;
  /**
   * Item ids with an in-flight panel action (equip/unequip/drink/drop) whose
   * result hasn't ridden a turn bundle yet — the same pending set that drives
   * the panel badge and the on-map ⇄ swap glyph (FeedbackLayer.setItemAction).
   * Combat-agnostic; exposed for e2e. Empty when nothing is pending.
   */
  pendingItems: number[];
  /**
   * Whether a ground-item pickup is in flight (from the take click until the
   * next turn bundle resolves it) — drives the on-map pickup glyph
   * (FeedbackLayer.setPickup). Distinct from pendingItems (owned-item actions).
   */
  pickupPending: boolean;
}

declare global {
  interface Window {
    game: GameDebug;
  }
}
