import { createSignal } from "solid-js";

import type { ItemView } from "../protocol.gen";
import { BackpackSize } from "../protocol.gen";

// The character/inventory store (inventory-slots milestone, task 5): the
// slot-keyed equipped map + the fixed-size backpack + the per-hex pickup
// modal, all refreshed each turn bundle by main.ts. Mirrors the approved
// paper-doll mockup (scratchpad/inventory-mock.html).

/** The comparable stats an item carries on the wire (ItemView), surfaced in
 *  the hover tooltip. `damage`/`rangeHex`/`aoeRadius` are 0 for stat-less gear;
 *  `desc` is the authored effect text ("×1.5 damage vs dragons"), "" if none. */
export interface ItemStats {
  damage: number;
  rangeHex: number;
  aoeRadius: number;
  /** The mechanical effect line ("×1.5 damage vs dragons"). */
  desc: string;
  /** The authored lore/"Fantasy" line; "" for items without lore. */
  flavor: string;
}

/** One equipped item, keyed in `equipped` by its slot (itemType string). */
export interface SlotItem extends ItemStats {
  id: number;
  defId: string;
  name: string;
  type: string;
}

/** One backpack entry — a gear instance or a consumable stack (count>1). */
export interface BackpackEntry extends ItemStats {
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
  /** Stack size on the ground (a consumable stack drops whole; 1 for gear). */
  count: number;
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
// visibly registers. The value is the item's observable SIGNATURE at
// mark-time (see itemSignature); a bundle whose signature still matches means
// the action has not resolved yet (an in-bubble action, or a coalesced
// world-turn bundle), so the mark is HELD — it clears only once the item's
// state actually changed, not on any arriving bundle.
const [pending, setPendingSig] = createSignal<Map<number, string>>(new Map());

export { equipped, backpack, weaponSlots, panelOpen, pickupRows, modalOpen, pending };

// lastItems is the most recent ItemView list — markPending reads it to
// snapshot an item's signature at click time.
let lastItems: ItemView[] = [];

// itemSignature is an item's observable state on the wire: "eq" while
// equipped, "bp:<count>" while a backpack entry, "" when absent. A pending
// action is resolved the moment this value differs from the one captured
// when the action was fired (equip↔unequip flip, drink decrement, drop →
// gone).
function itemSignature(items: ItemView[], id: number): string {
  const it = items.find((i) => Number(i.id) === id);
  if (it === undefined) {
    return "";
  }

  return it.equipped ? "eq" : `bp:${it.count}`;
}

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
 * items carry Equipped=true; the rest are backpack entries in wire order),
 * then clears the pending mark on any item whose signature CHANGED since the
 * action was fired (or that vanished) — the server's answer arrived. An item
 * still at its mark-time signature keeps its mark (a queued in-bubble action,
 * or a coalesced world-turn bundle that resolved nothing about it), so the
 * "…" doesn't flash off early.
 */
export function setInventory(items: ItemView[]): void {
  const eq: Record<string, SlotItem> = {};
  const pack: (BackpackEntry | null)[] = emptyBackpack();

  let next = 0;

  for (const it of items) {
    const stats: ItemStats = {
      damage: it.damage,
      rangeHex: it.rangeHex,
      aoeRadius: it.aoeRadius,
      desc: it.desc,
      flavor: it.flavor,
    };
    if (it.equipped) {
      eq[it.type] = { id: Number(it.id), defId: it.defId, name: it.name, type: it.type, ...stats };
    } else if (next < pack.length) {
      pack[next] = { id: Number(it.id), defId: it.defId, name: it.name, type: it.type, count: it.count, ...stats };
      next++;
    }
  }

  setEquippedSig(eq);
  setBackpackSig(pack);

  resolvePending(items);
  lastItems = items;
}

// resolvePending drops any pending mark whose item's signature now differs
// from the one captured at mark time (the action resolved) — holding the rest.
function resolvePending(items: ItemView[]): void {
  const cur = pending();
  if (cur.size === 0) {
    return;
  }

  let changed = false;
  const next = new Map(cur);

  for (const [id, sig] of cur) {
    if (itemSignature(items, id) !== sig) {
      next.delete(id);
      changed = true;
    }
  }

  if (changed) {
    setPendingSig(next);
  }
}

/**
 * Marks an owned item's action as in-flight (shows a "…" mark), capturing the
 * item's current signature so the mark holds until that state actually
 * changes (see resolvePending).
 */
export function markPending(itemId: number): void {
  const next = new Map(pending());
  next.set(itemId, itemSignature(lastItems, itemId));
  setPendingSig(next);
}

/** Clears every pending mark — a map click supersedes a queued in-bubble action. */
export function clearPending(): void {
  if (pending().size > 0) {
    setPendingSig(new Map());
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
export function refreshPickup(rows: { id: number; name: string; type: string; count: number }[], moved: boolean): void {
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
