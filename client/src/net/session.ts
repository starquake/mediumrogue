import type { ChatRequest, ErrorResponse, Hex, IntentRequest, JoinRequest, JoinResponse } from "../protocol.gen";
import { IntentEquip } from "../protocol.gen";

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
 * Claims an entity: re-sends the stored token so a page refresh keeps the
 * same character (and the same class/species — the server ignores Class and
 * Species entirely on a token match, so a returning player's stored choices
 * always win over whatever the pickers currently have selected), and stores
 * whatever identity the server answers with (a stale token after a server
 * restart just becomes a fresh entity, joined as `chosenClass`/`chosenSpecies`).
 * `chosenName` is likewise only used for a new/orphaned token — the server
 * ignores Name on a reclaim (an existing entity already has its name).
 */
export async function join(
  chosenName: string,
  chosenClass: string,
  chosenSpecies: string,
): Promise<JoinResponse> {
  const stored = loadIdentity();
  const body: JoinRequest = {
    token: stored?.token ?? "",
    name: chosenName,
    class: stored?.class ?? chosenClass,
    species: stored?.species ?? chosenSpecies,
  };
  const resp = await fetch("/api/join", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!resp.ok) {
    throw new Error(`POST /api/join failed: ${resp.status}`);
  }

  const joined = (await resp.json()) as JoinResponse;
  const identity: Identity = {
    entityId: joined.entityId,
    token: joined.token,
    class: stored?.class ?? chosenClass,
    species: stored?.species ?? chosenSpecies,
  };
  localStorage.setItem(STORAGE_KEY, JSON.stringify(identity));

  return joined;
}

/** POSTs an IntentRequest and reports whether the server accepted it (202). */
async function postIntent(body: IntentRequest): Promise<boolean> {
  const resp = await fetch("/api/intent", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });

  return resp.status === 202;
}

/**
 * Queues an intent for the next turn — "step to target" (kind "move") or a
 * ranged attack at target (kind "attack"). Resolves to true when the server
 * accepted the intent; false on rejection (not adjacent/not walkable for a
 * move, out of range/no ranged weapon for an attack, stale identity).
 * Movement itself only ever arrives via the turn bundle — the client never
 * moves an entity locally. itemId is unused by move/attack — see submitEquip.
 */
export async function submitIntent(
  identity: Pick<Identity, "entityId" | "token">,
  target: Hex,
  kind: string,
): Promise<boolean> {
  return postIntent({ entityId: identity.entityId, token: identity.token, target, kind, itemId: 0 });
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
  return postIntent({
    entityId: identity.entityId,
    token: identity.token,
    target: { q: 0, r: 0 },
    kind: IntentEquip,
    itemId,
  });
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
