import { createSignal } from "solid-js";

// The member names of MY party (including me), or empty when solo. The roster
// panel renders this; main.ts refreshes it each turn from the bundle.
const [party, setPartyNames] = createSignal<string[]>([]);

export { party };

export function setParty(names: string[]): void {
  setPartyNames(names);
}
