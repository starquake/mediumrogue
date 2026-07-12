import { createSignal } from "solid-js";

import type { ItemView } from "../protocol.gen";
import { BackpackSize } from "../protocol.gen";

// The character/inventory store (inventory-slots milestone, task 5): the
// slot-keyed equipped map + the fixed-size backpack + the per-hex pickup
// modal, all refreshed each turn bundle by main.ts. Mirrors the approved
// paper-doll mockup (scratchpad/inventory-mock.html).

/** One equipped item, keyed in `equipped` by its slot (itemType string). */
export interface SlotItem {
  id: number;
  defId: string;
  name: string;
  type: string;
}

/** One backpack entry — a gear instance or a consumable stack (count>1). */
export interface BackpackEntry {
  id: number;
  defId: string;
  name: string;
  type: string;
  count: number;
}

/** One row of the per-hex pickup modal. */
export interface PickupRow {
  id: number;
  name: string;
  type: string;
  /** Set when a take was rejected (backpack full) — inline feedback shows. */
  rejected: boolean;
}

// equipped: slot (itemType) -> the item filling it. Absent key = empty slot.
const [equipped, setEquippedSig] = createSignal<Record<string, SlotItem>>({});
// backpack: BackpackSize entries, left-packed (nulls trail) — the wire lists
// only non-empty entries, and their index is not player-meaningful (pickup/
// drop use the first free slot), so left-packing matches the mockup exactly.
const [backpack, setBackpackSig] = createSignal<(BackpackEntry | null)[]>(emptyBackpack());
// The class's two weapon-slot itemTypes ([close-ish, ranged-ish]) — set once
// at join so the paper-doll can label and place the two class-shaped slots.
const [weaponSlots, setWeaponSlotsSig] = createSignal<[string, string]>(["", ""]);
// Whether the character panel is open (toggled by the `i` key / HUD button).
// Closed by default — the paper-doll is large, so it is not always-on.
const [panelOpen, setPanelOpenSig] = createSignal(false);
// The pickup modal: its rows plus whether it is open (it appears on walk-over
// regardless of panelOpen).
const [pickupRows, setPickupRowsSig] = createSignal<PickupRow[]>([]);
const [modalOpen, setModalOpenSig] = createSignal(false);
// Item instance ids with an in-flight action (equip/unequip/drop/drink) whose
// result has not yet ridden a turn bundle — shown as a brief pending mark, so
// a click in a combat bubble (where the action waits for the turn) still
// visibly registers.
const [pending, setPendingSig] = createSignal<Set<number>>(new Set());

export { equipped, backpack, weaponSlots, panelOpen, pickupRows, modalOpen, pending };

function emptyBackpack(): (BackpackEntry | null)[] {
  return Array.from({ length: BackpackSize }, () => null);
}

/** slotLabel is the human label for a slot/itemType ("melee-weapon" -> "Melee"). */
export function slotLabel(type: string): string {
  const base = type.replace(/-weapon$/, "");

  return base.charAt(0).toUpperCase() + base.slice(1);
}

/** typeLabel is the ground-row type text ("melee-weapon" -> "melee weapon"). */
export function typeLabel(type: string): string {
  return type.replace(/-/g, " ");
}

/** Sets the class's two weapon-slot itemTypes ([close-ish, ranged-ish]). */
export function setWeaponSlots(slots: [string, string]): void {
  setWeaponSlotsSig(slots);
}

/**
 * Refreshes equipped + backpack from this bundle's ItemView list (equipped
 * items carry Equipped=true; the rest are backpack entries in wire order).
 * Clears the pending mark on any item that reached a settled state (its
 * presence/equipped flag matches nothing still in flight is left as-is — the
 * simplest rule: a fresh bundle clears every pending mark, since by then the
 * server has answered whatever was queued).
 */
export function setInventory(items: ItemView[]): void {
  const eq: Record<string, SlotItem> = {};
  const pack: (BackpackEntry | null)[] = emptyBackpack();

  let next = 0;

  for (const it of items) {
    if (it.equipped) {
      eq[it.type] = { id: Number(it.id), defId: it.defId, name: it.name, type: it.type };
    } else if (next < pack.length) {
      pack[next] = { id: Number(it.id), defId: it.defId, name: it.name, type: it.type, count: it.count };
      next++;
    }
  }

  setEquippedSig(eq);
  setBackpackSig(pack);

  // A turn bundle is the server's answer to whatever was queued — clear
  // every pending mark (a still-queued in-bubble action re-marks itself on
  // the next click if the player re-submits).
  if (pending().size > 0) {
    setPendingSig(new Set<number>());
  }
}

/** Marks an owned item's action as in-flight (shows a pending "…" mark). */
export function markPending(itemId: number): void {
  const s = new Set(pending());
  s.add(itemId);
  setPendingSig(s);
}

/** Clears every pending mark — a map click supersedes a queued in-bubble action. */
export function clearPending(): void {
  if (pending().size > 0) {
    setPendingSig(new Set<number>());
  }
}

/** Toggles the character panel open/closed (the `i` key / HUD button). */
export function togglePanel(): void {
  setPanelOpenSig((v) => !v);
}

/**
 * Refreshes the pickup modal from the ground items lying on the player's own
 * hex. `moved` is true when the player's hex changed since the previous
 * bundle — leaving a hex clears its dismissal (so re-entering reopens the
 * modal, per the spec) and clears any per-row rejection marks. When items are
 * present and the modal is not dismissed for this hex, it opens; when the hex
 * is empty of items, it closes.
 */
export function refreshPickup(rows: { id: number; name: string; type: string }[], moved: boolean): void {
  if (moved) {
    dismissed = false;
  }

  if (rows.length === 0) {
    setModalOpenSig(false);
    setPickupRowsSig([]);

    return;
  }

  // Carry forward rejection marks for ids still present (a rejected row stays
  // rejected until the modal is dismissed / the player moves away).
  const rejectedIds = new Set(pickupRows().filter((r) => r.rejected).map((r) => r.id));
  setPickupRowsSig(rows.map((r) => ({ ...r, rejected: rejectedIds.has(r.id) })));
  setModalOpenSig(!dismissed);
}

// dismissed tracks whether the player closed the modal while still standing on
// the current hex — reset by refreshPickup when the player moves.
let dismissed = false;

/** Closes the modal, leaving the remaining items on the ground (spec: reopens on re-entry). */
export function dismissPickup(): void {
  dismissed = true;
  setModalOpenSig(false);
}

/** Marks a pickup row as rejected (backpack full) — inline feedback shows, row stays. */
export function markPickupRejected(groundItemId: number): void {
  setPickupRowsSig(pickupRows().map((r) => (r.id === groundItemId ? { ...r, rejected: true } : r)));
}
