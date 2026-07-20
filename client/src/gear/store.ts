import { createSignal } from "solid-js";

import type { ItemView, StatView } from "../protocol.gen";
import {
  BackpackSize,
  ItemTypeShield,
  ItemTypeWeapon,
  SlotAmulet,
  SlotBoots,
  SlotChest,
  SlotGloves,
  SlotHelmet,
  SlotMainHand,
  SlotOffHand,
  SlotRing,
} from "../protocol.gen";

// The character/inventory store: the slot-keyed equipped map + the
// fixed-size backpack + the per-hex pickup modal, all refreshed each turn
// bundle by main.ts. Slot model per the gear keystone's approved ARPG panel
// (docs/superpowers/specs/2026-07-13-arpg-character-inventory-design.md):
// eight named slots, two of them hands, a two-handed weapon greying the
// off-hand.

/** The eight equip slots in the paper-doll's fixed render order. */
export const SLOT_ORDER = [
  SlotHelmet,
  SlotAmulet,
  SlotGloves,
  SlotRing,
  SlotMainHand,
  SlotChest,
  SlotOffHand,
  SlotBoots,
] as const;

/** The approved mockup's slot labels, keyed by slot name. */
export const SLOT_LABELS: Record<string, string> = {
  [SlotHelmet]: "Helmet",
  [SlotAmulet]: "Amulet",
  [SlotGloves]: "Gloves",
  [SlotRing]: "Ring",
  [SlotMainHand]: "Main Hand",
  [SlotChest]: "Chest",
  [SlotOffHand]: "Off-Hand",
  [SlotBoots]: "Boots",
};

/** The comparable stats an item carries on the wire (ItemView), surfaced in
 *  the hover tooltip. `damage`/`rangeHex`/`aoeRadius` are 0 for stat-less gear;
 *  `desc` is the authored effect text ("×1.5 damage vs dragons"), "" if none.
 *  `tags`/`twoHanded` are weapon-only (empty/false otherwise) — twoHanded
 *  drives the off-hand's greyed "two-handed grip" lock. */
export interface ItemStats {
  damage: number;
  rangeHex: number;
  aoeRadius: number;
  /** Rendered stat lines (#171), derived server-side from the rule cards. */
  stats: StatView[];
  /** The authored lore/"Fantasy" line; "" for items without lore. */
  flavor: string;
  tags: string[];
  twoHanded: boolean;
  /** The damage type a weapon deals (#92); "" for a non-weapon. */
  damageType: string;
}

/**
 * One equipped item, keyed in `equipped` by the SLOT it occupies — one of
 * the eight SLOT_ORDER names. `type` is the item's ItemView.Type: for
 * armor/jewelry that already equals the slot key, but for a weapon the
 * server sets it to the occupied HAND (SlotMainHand/SlotOffHand) rather than
 * the generic "weapon" taxonomy string, precisely so two held weapons never
 * collide under one key here.
 */
export interface SlotItem extends ItemStats {
  id: number;
  defId: string;
  name: string;
  type: string;
}

/** One backpack entry — a gear instance or a consumable stack (count>1).
 *  `type` here is the generic taxonomy string (a weapon has no hand yet —
 *  see targetSlotFor). */
export interface BackpackEntry extends ItemStats {
  id: number;
  defId: string;
  name: string;
  type: string;
  count: number;
}

/** One row of the per-hex pickup modal. */
export interface PickupRow extends ItemStats {
  id: number;
  name: string;
  type: string;
  /** Stack size on the ground (a consumable stack drops whole; 1 for gear). */
  count: number;
  /** Set when a take was rejected (backpack full) — inline feedback shows. */
  rejected: boolean;
  /**
   * The server's own rejection reason for this row (#193) — shown inline
   * instead of a hardcoded "backpack full", which was wrong for any other
   * cause. Empty while the row is not rejected.
   */
  rejectedReason: string;
}

// equipped: slot (one of the eight SLOT_ORDER names) -> the item filling it.
// Absent key = empty slot.
const [equipped, setEquippedSig] = createSignal<Record<string, SlotItem>>({});
// backpack: BackpackSize entries, left-packed (nulls trail) — the wire lists
// only non-empty entries, and their index is not player-meaningful (pickup/
// drop use the first free slot), so left-packing matches the mockup exactly.
const [backpack, setBackpackSig] = createSignal<(BackpackEntry | null)[]>(emptyBackpack());
// Whether the character panel is open (toggled by the `i` key / HUD button).
// Closed by default — the paper-doll is large, so it is not always-on.
const [panelOpen, setPanelOpenSig] = createSignal(false);
// The pickup modal: its rows plus whether it is open (it appears on walk-over
// regardless of panelOpen).
const [pickupRows, setPickupRowsSig] = createSignal<PickupRow[]>([]);
const [modalOpen, setModalOpenSig] = createSignal(false);
// Ground item ids with a take in flight — the modal shows a spinner on that
// row's "take" button until the pickup resolves (the row vanishes as the item
// leaves the ground) or is rejected (backpack full).
const [taking, setTakingSig] = createSignal<Set<number>>(new Set());
// Item instance ids with an in-flight action (equip/unequip/drop/drink) whose
// result has not yet ridden a turn bundle — shown as a brief pending mark, so
// a click in a combat bubble (where the action waits for the turn) still
// visibly registers. The value is the item's observable SIGNATURE at
// mark-time (see itemSignature); a bundle whose signature still matches means
// the action has not resolved yet (an in-bubble action, or a coalesced
// world-turn bundle), so the mark is HELD — it clears only once the item's
// state actually changed, not on any arriving bundle.
const [pending, setPendingSig] = createSignal<Map<number, string>>(new Map());

export { equipped, backpack, panelOpen, pickupRows, modalOpen, pending, taking };

/** Marks a ground item's take as in flight (spinner on its "take" button). */
export function markTaking(groundItemId: number): void {
  const next = new Set(taking());
  next.add(groundItemId);
  setTakingSig(next);
}

/** Drops a taking mark (the take resolved or was rejected). */
export function clearTaking(groundItemId: number): void {
  if (!taking().has(groundItemId)) {
    return;
  }
  const next = new Set(taking());
  next.delete(groundItemId);
  setTakingSig(next);
}

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

/** typeLabel is the ground-row type text ("healing-potion" -> ground row's
 *  "consumable" -> "consumable"; kept generic for any future hyphenated type). */
export function typeLabel(type: string): string {
  return type.replace(/-/g, " ");
}

/**
 * offHandLocked reports whether the off-hand slot is locked by a two-handed
 * main-hand weapon (the approved mockup's greyed "two-handed grip" state).
 */
export function offHandLocked(): boolean {
  return equipped()[SlotMainHand]?.twoHanded === true;
}

/**
 * targetSlotFor mirrors the server's weaponTargetSlot placement rule
 * (internal/game/items.go) client-side, so the hover tooltip can compare a
 * backpack weapon against the hand it would actually land in: two-handed or
 * an empty main-hand -> main-hand; else an empty off-hand -> off-hand; else
 * main-hand (the swap case). A shield always equips off-hand — the server's
 * slotForType rule (#90) — regardless of occupancy. Any other non-weapon
 * item's slot equals its type already (armor/jewelry); a consumable has no
 * slot ("" — never compared, the tooltip only compares combat-stat items).
 */
export function targetSlotFor(item: { type: string; twoHanded: boolean }): string {
  if (item.type === ItemTypeShield) {
    return SlotOffHand; // a shield equips off-hand only (#90)
  }

  if (item.type !== ItemTypeWeapon) {
    return item.type;
  }

  if (item.twoHanded || equipped()[SlotMainHand] === undefined) {
    return SlotMainHand;
  }

  if (equipped()[SlotOffHand] === undefined) {
    return SlotOffHand;
  }

  return SlotMainHand;
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
      stats: it.stats,
      flavor: it.flavor,
      tags: it.tags,
      damageType: it.damageType,
      twoHanded: it.twoHanded,
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
export function refreshPickup(rows: Omit<PickupRow, "rejected" | "rejectedReason">[], moved: boolean): void {
  if (moved) {
    dismissed = false;
  }

  // A rejected row clears when a backpack slot actually FREES (#193) — the free
  // count going UP since the last bundle, i.e. the player dropped/consumed
  // something — not merely "there's room now". A backpack-full rejection can
  // only happen while full, so "freed" is the true retry signal; keying off a
  // static non-full backpack would wrongly wipe a just-rejected row the same
  // bundle it appeared.
  const freeSlots = backpack().filter((e) => e === null).length;
  const freed = lastFreeSlots >= 0 && freeSlots > lastFreeSlots;
  lastFreeSlots = freeSlots;

  if (rows.length === 0) {
    setModalOpenSig(false);
    setPickupRowsSig([]);
    if (taking().size > 0) {
      setTakingSig(new Set<number>());
    }

    return;
  }

  // Carry forward rejection marks for ids still present (a rejected row stays
  // rejected until the modal is dismissed / the player moves away / a slot frees).
  const prevReasons = new Map(pickupRows().filter((r) => r.rejected).map((r) => [r.id, r.rejectedReason]));
  const rejectedIds = freed ? new Set<number>() : new Set(prevReasons.keys());
  setPickupRowsSig(
    rows.map((r) => ({
      ...r,
      rejected: rejectedIds.has(r.id),
      rejectedReason: rejectedIds.has(r.id) ? (prevReasons.get(r.id) ?? "") : "",
    })),
  );
  setModalOpenSig(!dismissed);

  // Drop taking marks for items no longer on the ground — their take resolved
  // and the row is gone, so the spinner should stop.
  const present = new Set(rows.map((r) => r.id));
  const cur = taking();
  if ([...cur].some((id) => !present.has(id))) {
    setTakingSig(new Set([...cur].filter((id) => present.has(id))));
  }
}

// dismissed tracks whether the player closed the modal while still standing on
// the current hex — reset by refreshPickup when the player moves.
let dismissed = false;

// lastFreeSlots is the backpack's free-slot count at the previous bundle, so
// refreshPickup can tell a slot FREEING (retry signal for a rejected row) from
// a backpack that was simply never full. -1 means "no baseline yet".
let lastFreeSlots = -1;

/** Closes the modal, leaving the remaining items on the ground (spec: reopens on re-entry). */
export function dismissPickup(): void {
  dismissed = true;
  setModalOpenSig(false);
}

/**
 * Marks a pickup row as rejected — inline feedback shows, row stays. `reason`
 * is the server's own message (#193); it defaults to the backpack-full text for
 * the by-far commonest cause and the client-only test hook.
 */
export function markPickupRejected(groundItemId: number, reason = "backpack full — drop something first"): void {
  setPickupRowsSig(
    pickupRows().map((r) => (r.id === groundItemId ? { ...r, rejected: true, rejectedReason: reason } : r)),
  );
  clearTaking(groundItemId); // stop the spinner; the rejected message shows instead
}

/**
 * Clears a single item's pending mark (#193) — a rejected equip/unequip/drop/
 * drink whose signature never changed, so resolvePending would otherwise leave
 * its spinner spinning until an unrelated map click. Unlike clearPending it
 * leaves any *other* in-flight action's mark alone.
 */
export function clearOnePending(itemId: number): void {
  const cur = pending();
  if (!cur.has(itemId)) {
    return;
  }

  const next = new Map(cur);
  next.delete(itemId);
  setPendingSig(next);
}
