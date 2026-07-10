import { createSignal } from "solid-js";

import type { ItemView } from "../protocol.gen";

// My inventory, refreshed each turn from the bundle by main.ts.
const [inventory, setInventorySignal] = createSignal<ItemView[]>([]);

// The item instance id of an equip click still awaiting the server's answer
// (0 = none). Outside a combat bubble that's milliseconds; inside one the
// swap is the turn's action and can pend until the bubble turn resolves —
// the button shows "…" the whole time, so the click visibly registered.
const [pendingEquip, setPendingEquipSignal] = createSignal<number>(0);

export { inventory, pendingEquip };

export function setInventory(items: ItemView[]): void {
  setInventorySignal(items);

  // A pending equip is acknowledged once the item shows up equipped (or has
  // vanished from the inventory entirely) — the server's answer arrived.
  const p = pendingEquip();
  if (p !== 0) {
    const item = items.find((it) => it.id === p);
    if (item === undefined || item.equipped) {
      setPendingEquipSignal(0);
    }
  }
}

/** An equip was just clicked: show "…" on its button until answered. */
export function markEquipPending(itemId: number): void {
  setPendingEquipSignal(itemId);
}

/**
 * Drop the pending state without an answer — a later map click replaces a
 * queued in-bubble equip server-side (latest intent wins), so its "…" would
 * otherwise dangle forever.
 */
export function clearEquipPending(): void {
  setPendingEquipSignal(0);
}
