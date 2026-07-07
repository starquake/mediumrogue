import type { Hex, IntentRequest, JoinRequest, JoinResponse } from "../protocol.gen";

const STORAGE_KEY = "mediumrogue.identity";

export interface Identity {
  entityId: number;
  token: string;
}

function storedIdentity(): Identity | null {
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
 * same character, and stores whatever identity the server answers with (a
 * stale token after a server restart just becomes a fresh entity).
 */
export async function join(): Promise<JoinResponse> {
  const body: JoinRequest = { token: storedIdentity()?.token ?? "" };
  const resp = await fetch("/api/join", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!resp.ok) {
    throw new Error(`POST /api/join failed: ${resp.status}`);
  }

  const joined = (await resp.json()) as JoinResponse;
  const identity: Identity = { entityId: joined.entityId, token: joined.token };
  localStorage.setItem(STORAGE_KEY, JSON.stringify(identity));

  return joined;
}

/**
 * Queues "step to target" for the next turn. Resolves to true when the
 * server accepted the intent; false on rejection (not adjacent, not
 * walkable, stale identity). Movement itself only ever arrives via the turn
 * bundle — the client never moves an entity locally.
 */
export async function submitIntent(identity: Identity, target: Hex): Promise<boolean> {
  const body: IntentRequest = { entityId: identity.entityId, token: identity.token, target };
  const resp = await fetch("/api/intent", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });

  return resp.status === 202;
}
