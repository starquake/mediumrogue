import { SpeciesHuman } from "../protocol.gen";
import type { Hex, IntentRequest, JoinRequest, JoinResponse } from "../protocol.gen";

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
 * same character (and the same class — the server ignores Class entirely on
 * a token match, so a returning player's stored class always wins over
 * whatever the picker currently has selected), and stores whatever identity
 * the server answers with (a stale token after a server restart just becomes
 * a fresh entity, joined as `chosenClass`).
 */
export async function join(chosenClass: string): Promise<JoinResponse> {
  const stored = loadIdentity();
  const body: JoinRequest = {
    token: stored?.token ?? "",
    class: stored?.class ?? chosenClass,
    species: stored?.species ?? SpeciesHuman,
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
    species: stored?.species ?? SpeciesHuman,
  };
  localStorage.setItem(STORAGE_KEY, JSON.stringify(identity));

  return joined;
}

/**
 * Queues an intent for the next turn — "step to target" (kind "move") or a
 * ranged attack at target (kind "attack"). Resolves to true when the server
 * accepted the intent; false on rejection (not adjacent/not walkable for a
 * move, out of range/no ranged weapon for an attack, stale identity).
 * Movement itself only ever arrives via the turn bundle — the client never
 * moves an entity locally.
 */
export async function submitIntent(
  identity: Pick<Identity, "entityId" | "token">,
  target: Hex,
  kind: string,
): Promise<boolean> {
  const body: IntentRequest = { entityId: identity.entityId, token: identity.token, target, kind };
  const resp = await fetch("/api/intent", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });

  return resp.status === 202;
}
