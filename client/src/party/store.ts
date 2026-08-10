import { createSignal } from "solid-js";

import type { PartyInviteView } from "../protocol.gen";

// The member names of MY party (including me), or empty when solo. The roster
// panel renders this; main.ts refreshes it each turn from the bundle.
const [party, setPartyNames] = createSignal<string[]>([]);

// The invite waiting on my answer, or null. Own-only on the wire (#385), so
// this is never populated for anyone but the person being asked.
const [pendingInvite, setPending] = createSignal<PartyInviteView | null>(null);

export { party, pendingInvite };

export function setParty(names: string[]): void {
  setPartyNames(names);
}

export function setPendingInvite(invite: PartyInviteView | null): void {
  setPending(invite);
}
