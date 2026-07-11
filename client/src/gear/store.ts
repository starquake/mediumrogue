import { createSignal } from "solid-js";

import type { ItemView } from "../protocol.gen";

// My inventory, refreshed each turn from the bundle by main.ts.
const [inventory, setInventorySignal] = createSignal<ItemView[]>([]);

// The item instance id of an equip/unequip click still awaiting the
// server's answer (0 = none). Outside a combat bubble that's milliseconds;
// inside one the toggle is the turn's action and can pend until the bubble
// turn resolves — the button shows "…" the whole time, so the click visibly
// registered.
const [pendingEquip, setPendingEquipSignal] = createSignal<number>(0);
// The equipped state the pending click is expected to produce (true = equip
// on, false = unequip) — item 2's toggle semantics mean a click can head
// either direction, so the resolution check below can no longer hardcode
// "equipped" as the answer.
let pendingEquipTarget = false;

export { inventory, pendingEquip };

export function setInventory(items: ItemView[]): void {
  setInventorySignal(items);

  // A pending equip/unequip is acknowledged once the item reaches its
  // expected equipped state (or has vanished from the inventory entirely)
  // — the server's answer arrived.
  const p = pendingEquip();
  if (p !== 0) {
    const item = items.find((it) => it.id === p);
    if (item === undefined || item.equipped === pendingEquipTarget) {
      setPendingEquipSignal(0);
    }
  }
}

/**
 * An equip/unequip was just clicked: show "…" on its button until answered.
 * wasEquipped is the item's equipped state at click time — the toggle's
 * target is its opposite.
 */
export function markEquipPending(itemId: number, wasEquipped: boolean): void {
  pendingEquipTarget = !wasEquipped;
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
