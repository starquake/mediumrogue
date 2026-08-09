import { Howl, Howler } from "howler";

import { InterestRadius } from "../protocol.gen";

// sound.ts (#298): every sound the game makes, and the rules for when it is
// allowed to make one.
//
// Effects only — no music, no ambient (maintainer's call). Individual files
// rather than one sprite: ~18 clips of ~10 KB served over HTTP/2 from the same
// binary, so the sprite's one-request win does not pay for its build step and
// offset map.

/** SoundName is every effect the game can play. */
export type SoundName =
  | "hit"
  | "crit"
  | "glance"
  | "shoot"
  | "death"
  | "blast"
  | "levelUp"
  | "footstep"
  | "pickup"
  | "drop"
  | "equip"
  | "panelOpen"
  | "panelClose"
  | "uiClick"
  | "invite";

// Variants exist only where repetition would grate — a single footstep clip
// turns walking into a metronome. One entry means one clip, deliberately.
export const SOURCES: Record<SoundName, string[]> = {
  hit: ["knifeSlice.ogg", "knifeSlice2.ogg"],
  crit: ["chop.ogg"],
  glance: ["cloth1.ogg", "cloth2.ogg"],
  // A bowstring/whoosh. RPG Audio has no bow at all, so this is the pack's
  // cloth movement standing in for one — the closest honest approximation,
  // and it stays inside a pack already shipped rather than adding one.
  shoot: ["cloth3.ogg", "cloth4.ogg"],
  death: ["impactSoft_heavy_000.ogg", "impactSoft_heavy_001.ogg"],
  blast: ["explosionCrunch_000.ogg"],
  levelUp: ["jingles_STEEL00.ogg"],
  footstep: ["footstep00.ogg", "footstep01.ogg", "footstep02.ogg", "footstep03.ogg", "footstep04.ogg"],
  pickup: ["handleCoins.ogg", "handleSmallLeather.ogg"],
  drop: ["dropLeather.ogg"],
  equip: ["clothBelt.ogg", "drawKnife1.ogg", "metalClick.ogg"],
  panelOpen: ["bookOpen.ogg"],
  panelClose: ["bookClose.ogg"],
  uiClick: ["click1.ogg", "click2.ogg"],
  // A party invite arriving (#385). Shares panelOpen's clip: the prompt IS a
  // panel appearing, and every one of the 26 shipped clips is already spoken
  // for — none of them means "someone is asking you something", and the rule
  // here is to stay inside a pack already shipped rather than add one.
  //
  // A distinct NAME rather than just calling play("panelOpen"), because the two
  // are different events: this one is the panel you did not open, it is the
  // only cue for a passive prompt you can otherwise miss, and window.game's
  // debug.last has to be able to tell them apart. Re-skinning it later is then
  // a one-line change here.
  invite: ["bookOpen.ogg"],
};

const AUDIO_BASE = "audio/";
const MUTE_KEY = "mediumrogue.muted";

// A turn can resolve many hits at once — a six-way bubble would otherwise fire
// six clips in the same frame and read as noise rather than as a fight.
const MAX_PER_TURN = 4;

// Distance falloff. Everything in a bundle is within InterestRadius (#289), so
// the ramp is over a known, bounded range and needs no far-distance guard.
const MIN_VOLUME = 0.15;

/** AudioDebug is what window.game exposes; an e2e cannot listen, so it counts. */
export interface AudioDebug {
  /** Total sounds actually started since load — 0 while muted or locked. */
  played: number;
  /** The most recent sound that played, or "" before the first. */
  last: SoundName | "";
  /** False until a user gesture unlocks playback (see unlockOnFirstGesture). */
  unlocked: boolean;
  /** Mirrors the persisted mute setting. */
  muted: boolean;
}

const clips = new Map<SoundName, Howl[]>();
let budget = MAX_PER_TURN;

/** debug is the live counter object; main.ts hands the same reference to window.game. */
export const debug: AudioDebug = { played: 0, last: "", unlocked: false, muted: false };

function readMuted(): boolean {
  try {
    return localStorage.getItem(MUTE_KEY) === "1";
  } catch {
    // Private-mode browsers can throw on localStorage; sound is not important
    // enough to break the client over.
    return false;
  }
}

/**
 * load builds a Howl per clip. Called after the map and first bundle are in, so
 * audio never sits on the critical path to a playable screen.
 */
export function load(): void {
  if (clips.size > 0) return;

  debug.muted = readMuted();
  Howler.mute(debug.muted);

  for (const [name, files] of Object.entries(SOURCES) as [SoundName, string[]][]) {
    clips.set(
      name,
      files.map((f) => new Howl({ src: [AUDIO_BASE + f], preload: true })),
    );
  }
}

/**
 * unlockOnFirstGesture arms a one-shot listener that resumes the audio context.
 *
 * This is not optional, and it is not only about the browser's autoplay policy
 * in the abstract: a RETURNING player never clicks anything before the world
 * appears. main.ts's `isNewPlayer` skips the start screen entirely when a
 * stored identity matches, so a regular lands in the game having made no
 * gesture at all. Without this, sound would silently never work for exactly
 * the people who play most, while working perfectly in any fresh browser a
 * developer tests with.
 */
export function unlockOnFirstGesture(): void {
  if (debug.unlocked) return;

  const unlock = (): void => {
    if (debug.unlocked) return;

    debug.unlocked = true;
    Howler.ctx?.resume().catch(() => {
      // A refused resume leaves `unlocked` true and playback silent — there is
      // nothing further to try, and it must not throw on an input handler.
    });
  };

  for (const ev of ["pointerdown", "keydown", "touchstart"]) {
    window.addEventListener(ev, unlock, { once: true, passive: true });
  }
}

/** setMuted flips and persists the mute setting. */
export function setMuted(muted: boolean): void {
  debug.muted = muted;
  Howler.mute(muted);
  try {
    localStorage.setItem(MUTE_KEY, muted ? "1" : "0");
  } catch {
    // Setting is session-only if storage is unavailable; not worth surfacing.
  }
}

export function isMuted(): boolean {
  return debug.muted;
}

/** startTurn refills the per-turn budget; called once per turn bundle. */
export function startTurn(): void {
  budget = MAX_PER_TURN;
}

/**
 * volumeForDistance ramps volume down with hex distance from the viewer.
 * The viewer's own actions pass distance 0 and play at full.
 */
export function volumeForDistance(hexes: number): number {
  const t = Math.max(0, Math.min(1, hexes / InterestRadius));

  return 1 - (1 - MIN_VOLUME) * t;
}

/**
 * play starts one clip, chosen at RANDOM among that sound's variants.
 *
 * Random is a deliberate exception to this codebase's determinism rule, which
 * hashes everything from coordinates. Nothing about audio is snapshotted,
 * replayed or asserted on, and a hashed choice would make a player walking a
 * straight line hear the same footstep every time — the machine-gun artefact
 * variant packs exist to prevent.
 *
 * Returns false when nothing played, so a caller can tell "muted" from "played".
 */
export function play(name: SoundName, volume = 1): boolean {
  if (debug.muted || !debug.unlocked || budget <= 0) return false;

  const variants = clips.get(name);
  if (variants === undefined || variants.length === 0) return false;

  const clip = variants[Math.floor(Math.random() * variants.length)];
  if (clip === undefined) return false;

  clip.volume(Math.max(0, Math.min(1, volume)));
  clip.play();

  budget--;
  debug.played++;
  debug.last = name;

  return true;
}
