import type { ChatRequest, ErrorResponse, Hex, IntentRequest, JoinRequest, JoinResponse } from "../protocol.gen";
import {
  IntentDrink,
  IntentDrop,
  IntentEquip,
  IntentLearnSkill,
  IntentPickup,
  IntentUnequip,
  IntentUseSkill,
} from "../protocol.gen";

const STORAGE_KEY = "mediumrogue.identity";

export interface Identity {
  entityId: number;
  token: string;
  /** The class this identity joined as. */
  class: string;
  /** The species this identity joined as. */
  species: string;
}

/**
 * Imports an identity from a `#t=<token>` character-link fragment (see
 * main.ts's copy-link HUD button) and strips the fragment via
 * history.replaceState. MUST be called before anything else runs — in
 * particular before any loadIdentity() read — so the token: (1) always wins
 * over whatever identity was already stored (following a link is a
 * deliberate "become this character" action, even on a browser that already
 * has one), (2) never survives in the URL bar to be copy-pasted into chat or
 * shared a second time by accident, and (3) never reaches the server as part
 * of a request — a URL hash is never sent over HTTP, so this is the only
 * place the raw token from a link is ever read at all. class/species are
 * left unset: exactly like any other token-only reclaim, the server ignores
 * both on a live reclaim or an archived restore (world.go's Join). A no-op
 * when the URL carries no `#t=` fragment.
 */
export function importIdentityFromFragment(): void {
  const match = /^#t=(.+)$/.exec(window.location.hash);
  const token = match?.[1];
  if (token === undefined || token === "") {
    return;
  }

  const identity: Identity = { entityId: 0, token, class: "", species: "" };
  localStorage.setItem(STORAGE_KEY, JSON.stringify(identity));

  const url = new URL(window.location.href);
  url.hash = "";
  window.history.replaceState(null, "", url.toString());
}

/**
 * Reads the persisted identity, if any. Exported so callers (the class
 * picker) can tell a brand-new player (no identity yet) from a returning one
 * without duplicating the localStorage/JSON-parse dance.
 */
export function loadIdentity(): Identity | null {
  const raw = localStorage.getItem(STORAGE_KEY);
  if (raw === null) {
    return null;
  }
  try {
    return JSON.parse(raw) as Identity;
  } catch {
    return null;
  }
}

/**
 * Discards the persisted identity. Used by the join-rejection recovery path
 * (main.ts): a stored identity the server refuses (e.g. an imported
 * character link whose token the server no longer knows) must not survive to
 * re-fail every subsequent page load.
 */
export function clearIdentity(): void {
  localStorage.removeItem(STORAGE_KEY);
}

/**
 * The server REJECTED the join as invalid (a 4xx) — as opposed to a network
 * failure or a server-side error, after which the stored identity may still
 * be perfectly good. Callers use this distinction to decide whether to
 * discard the stored identity (see main.ts's recovery path): only a
 * deliberate rejection may clear it; a flaky network never should.
 */
export class JoinRejectedError extends Error {
  constructor(status: number) {
    super(`POST /api/join rejected: ${status}`);
    this.name = "JoinRejectedError";
  }
}

/** POSTs /api/join and maps a 4xx to JoinRejectedError. Shared by join/reclaim. */
async function postJoin(body: JoinRequest): Promise<JoinResponse> {
  const resp = await fetch("/api/join", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (resp.status >= 400 && resp.status < 500) {
    throw new JoinRejectedError(resp.status);
  }
  if (!resp.ok) {
    throw new Error(`POST /api/join failed: ${resp.status}`);
  }

  return (await resp.json()) as JoinResponse;
}

/**
 * Claims a BRAND-NEW entity: always sends an empty token (never reads
 * localStorage — see `reclaim` below for returning players), so the server
 * mints a fresh character named/classed/specied as given. Stores the
 * resulting identity. Callers: the start-screen flow (a genuinely new
 * player, or a rejected/dead stored identity that was just cleared).
 */
export async function join(
  chosenName: string,
  chosenClass: string,
  chosenSpecies: string,
): Promise<JoinResponse> {
  const joined = await postJoin({ token: "", name: chosenName, class: chosenClass, species: chosenSpecies });
  const identity: Identity = {
    entityId: joined.entityId,
    token: joined.token,
    class: chosenClass,
    species: chosenSpecies,
  };
  localStorage.setItem(STORAGE_KEY, JSON.stringify(identity));

  return joined;
}

/**
 * Reclaims an ALREADY-KNOWN identity by its own token, passed explicitly by
 * the caller — deliberately never re-reads `loadIdentity()` for the token to
 * send. Two browser tabs sharing one origin share localStorage; if a reclaim
 * re-read the token from disk, tab A reclaiming right after tab B overwrote
 * the shared key would silently reclaim tab B's character instead of its
 * own — the seam behind the "players swapped identities" playtest report
 * (item 2, playtest feedback batch 3). Every caller here already has its own
 * trusted `identity` value in memory (from its own prior join/reclaim, never
 * from a fresh disk read), so that's what gets sent.
 *
 * Sends empty name/class/species: the reclaim-or-fail contract. The server
 * ignores all three for a token it still recognizes (live reclaim or
 * archived restore), so this always succeeds for a real returning player.
 * For a token the server no longer knows at all (item 4: the world reset
 * out from under this client, e.g. a restart with no matching snapshot/
 * archive entry), the empty name — checked first by the server, before the
 * equally-empty class/species — is invalid for a NEW entity, so the server
 * REJECTS with 422 (JoinRejectedError) instead of silently minting a
 * brand-new, level-1 stranger in the old character's place — callers must
 * treat that rejection as "this identity is truly gone" (main.ts clears it
 * and falls back to the start screen), never retry-and-mint.
 *
 * identity.class/species are never sent to the server (which ignores them on
 * a reclaim/restore) — they're only echoed back into the freshly persisted
 * record, since JoinResponse doesn't carry them and a restored/reclaimed
 * character's class/species never change.
 */
export async function reclaim(identity: Pick<Identity, "token" | "class" | "species">): Promise<JoinResponse> {
  const joined = await postJoin({ token: identity.token, name: "", class: "", species: "" });
  const next: Identity = {
    entityId: joined.entityId,
    token: joined.token,
    class: identity.class,
    species: identity.species,
  };
  localStorage.setItem(STORAGE_KEY, JSON.stringify(next));

  return joined;
}

/**
 * Registers a `storage` event listener (fires only in OTHER tabs/windows —
 * never the one that made the write) that invokes onForeignChange whenever
 * the persisted identity is overwritten with a DIFFERENT token than
 * currentToken() currently returns. Multi-tab hardening for item 2
 * (playtest feedback batch 3): `reclaim` above closes the "silently
 * reclaims a foreign token" seam, but a tab whose OWN identity gets
 * clobbered by another tab's join/reclaim (e.g. two different people
 * sharing one browser) would otherwise keep running with a token no longer
 * safe to trust — every subsequent intent it sends would race the other
 * tab's. Rather than try to patch that up in place, the recommended
 * onForeignChange is a full page reload: a clean reinitialization beats a
 * silently-corrupted one, and is immediately legible to whoever's at the
 * keyboard instead of manifesting as unexplained rubber-banding.
 */
export function onForeignIdentityChange(currentToken: () => string, onForeignChange: () => void): void {
  window.addEventListener("storage", (e) => {
    if (e.key !== STORAGE_KEY || e.newValue === null) {
      return;
    }
    try {
      const next = JSON.parse(e.newValue) as Identity;
      if (next.token !== currentToken()) {
        onForeignChange();
      }
    } catch {
      // Malformed value written by another tab — ignore; our own identity
      // stays authoritative until something concrete forces a reload.
    }
  });
}

// The client shows the server's own rejection reason (#193): the 422 body
// carries a precise string ("target is out of range", "backpack full", …) that
// used to be thrown away. A registered handler renders it as a toast; the net
// layer stays UI-agnostic by calling through this indirection.
let notify: (msg: string) => void = () => {};

/** Register the toast sink for intent rejections and network blips (#193). */
export function onIntentFeedback(fn: (msg: string) => void): void {
  notify = fn;
}

/**
 * POSTs an IntentRequest and reports whether the server accepted it (202).
 * On a typed rejection it surfaces the server's reason via the feedback sink;
 * on a network failure it surfaces a transient message and returns false —
 * NOT re-thrown, so a wifi/deploy blip never reaches the global
 * unhandledrejection handler and its "client stopped updating" crash banner
 * (#193, the false-alarm half).
 */
/** The outcome of a posted intent: accepted, or rejected with the server's reason. */
export interface IntentOutcome {
  ok: boolean;
  /** The server's rejection reason on a 422; "" on accept or a network blip. */
  reason: string;
}

/**
 * POSTs an IntentRequest and returns the outcome with the server's reason.
 * `toastReason` (default true) surfaces a 422 reason as a toast — a caller that
 * shows the reason on its own surface (the pickup modal's inline row) passes
 * false to avoid double-surfacing. A network failure always toasts a transient
 * blip and is never re-thrown (no false "client stopped updating" banner).
 */
async function postIntentDetailed(body: IntentRequest, toastReason = true): Promise<IntentOutcome> {
  let resp: Response;
  try {
    resp = await fetch("/api/intent", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
  } catch {
    notify("couldn't reach the server — retrying…");

    return { ok: false, reason: "" };
  }

  if (resp.status === 202) {
    return { ok: true, reason: "" };
  }

  const reason = await resp
    .json()
    .then((body: ErrorResponse) => body.error)
    .catch(() => "");
  if (reason !== "" && toastReason) {
    notify(reason);
  }

  return { ok: false, reason };
}

async function postIntent(body: IntentRequest): Promise<boolean> {
  return (await postIntentDetailed(body)).ok;
}

/**
 * Queues an intent for the next turn — "step to target" (kind "move") or a
 * ranged attack at target (kind "attack"). Resolves to true when the server
 * accepted the intent; false on rejection (not adjacent/not walkable for a
 * move, out of range/no ranged weapon for an attack, stale identity).
 * Movement itself only ever arrives via the turn bundle — the client never
 * moves an entity locally. itemId is unused by move/attack — see
 * submitEquip. targetEntityId (item 7, playtest batch 2) names a
 * single-target ranged attack's victim by entity id instead of relying on
 * target alone — 0 (the default) means none: a move, or a ground-targeted
 * (AoE) attack.
 */
export async function submitIntent(
  identity: Pick<Identity, "entityId" | "token">,
  target: Hex,
  kind: string,
  targetEntityId = 0,
): Promise<boolean> {
  return postIntent({
    entityId: identity.entityId,
    token: identity.token,
    target,
    kind,
    itemId: 0,
    groundItemId: 0,
    targetEntityId,
    skillId: "",
  });
}

/**
 * Queues an equip intent for an owned item. Outside a combat bubble the
 * server applies the swap immediately (still just a 202 ack here — the
 * result rides the next turn bundle's Entity.Items); inside one it becomes
 * this turn's action, same as move/attack. target is unused by an equip
 * intent server-side — the zero hex is sent to satisfy the wire shape.
 */
export async function submitEquip(
  identity: Pick<Identity, "entityId" | "token">,
  itemId: number,
): Promise<boolean> {
  return submitItemAction(identity, IntentEquip, itemId);
}

/**
 * Posts an inventory action naming an OWNED item by instance id — equip,
 * unequip, drop, or drink. Every one follows the shared free-outside/
 * turn-inside rule server-side; the target hex is unused (the zero hex
 * satisfies the wire shape). Resolves true on a 202 accept, false on a
 * typed rejection (e.g. backpack full for an unequip, not drinkable, ...).
 */
export async function submitItemAction(
  identity: Pick<Identity, "entityId" | "token">,
  kind: string,
  itemId: number,
): Promise<boolean> {
  return postIntent({
    entityId: identity.entityId,
    token: identity.token,
    target: { q: 0, r: 0 },
    kind,
    itemId,
    groundItemId: 0,
    targetEntityId: 0,
    skillId: "",
  });
}

/** Posts an unequip intent for an owned equipped item (rejected if the backpack is full). */
export async function submitUnequip(
  identity: Pick<Identity, "entityId" | "token">,
  itemId: number,
): Promise<boolean> {
  return submitItemAction(identity, IntentUnequip, itemId);
}

/** Posts a drop intent for an owned item (equipped or in the backpack; a stack drops whole). */
export async function submitDrop(
  identity: Pick<Identity, "entityId" | "token">,
  itemId: number,
): Promise<boolean> {
  return submitItemAction(identity, IntentDrop, itemId);
}

/** Posts a drink intent for an owned consumable stack. */
export async function submitDrink(
  identity: Pick<Identity, "entityId" | "token">,
  itemId: number,
): Promise<boolean> {
  return submitItemAction(identity, IntentDrink, itemId);
}

/**
 * Posts a pickup intent for one ground item on the player's own hex. Returns
 * the outcome: `ok` false on a typed rejection (backpack full, or the item is
 * no longer there), with `reason` carrying the server's own message. The
 * caller surfaces it as per-row modal feedback, so this suppresses the global
 * toast (`toastReason: false`) to avoid double-surfacing the same reason (#193).
 */
export async function submitPickup(
  identity: Pick<Identity, "entityId" | "token">,
  groundItemId: number,
): Promise<IntentOutcome> {
  return postIntentDetailed(
    {
      entityId: identity.entityId,
      token: identity.token,
      target: { q: 0, r: 0 },
      kind: IntentPickup,
      itemId: 0,
      groundItemId,
      targetEntityId: 0,
      skillId: "",
    },
    false,
  );
}

/**
 * POSTs a chat line. Throws with the server's message on a non-2xx (e.g. a
 * 422 command error or a 401 stale token), so the caller (the chat store)
 * can surface it locally as a system line.
 */
export async function sendChat(token: string, text: string): Promise<void> {
  const body: ChatRequest = { token, text };
  const resp = await fetch("/api/chat", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!resp.ok) {
    const detail = await resp
      .json()
      .then((body: ErrorResponse) => body.error)
      .catch(() => "");
    throw new Error(detail || `chat failed (${resp.status})`);
  }
}

/**
 * POSTs a learn-skill intent (#124). Resolves true on accept, false on a
 * typed rejection (no points, prerequisite unmet, already learned, or in
 * combat — learning is a between-fights decision and the server rejects it
 * inside a bubble rather than queueing it).
 */
export async function submitLearnSkill(
  identity: Pick<Identity, "entityId" | "token">,
  skillId: string,
): Promise<boolean> {
  return postIntent({
    entityId: identity.entityId,
    token: identity.token,
    target: { q: 0, r: 0 },
    kind: IntentLearnSkill,
    itemId: 0,
    groundItemId: 0,
    targetEntityId: 0,
    skillId,
  });
}

/**
 * Triggers a learned ACTIVE skill (#185) at a target hex — Blink and future
 * actives. The server validates learned/cooldown/range/walkable/LOS and, on a
 * rejection, surfaces the reason via the #193 toast; a false here is a no-op.
 */
export async function submitUseSkill(
  identity: Pick<Identity, "entityId" | "token">,
  skillId: string,
  target: Hex,
): Promise<boolean> {
  return postIntent({
    entityId: identity.entityId,
    token: identity.token,
    target,
    kind: IntentUseSkill,
    itemId: 0,
    groundItemId: 0,
    targetEntityId: 0,
    skillId,
  });
}
