import { describe, expect, test } from "vitest";

import type { ItemView } from "../protocol.gen";
import { BackpackSize, ItemTypeConsumable, ItemTypeShield, ItemTypeWeapon, SlotChest, SlotMainHand, SlotOffHand } from "../protocol.gen";
import type { PickupRow } from "./store";
import {
  backpack,
  clearOnePending,
  equipped,
  markPending,
  markPickupRejected,
  offHandLocked,
  pending,
  pickupRows,
  refreshPickup,
  setInventory,
  targetSlotFor,
} from "./store";

// store.test.ts covers the slot-placement logic no e2e path can reach: the
// approved mockup's greyed "two-handed grip" off-hand needs a two-handed
// weapon in inventory, and the monster-free e2e server has no drop/spawn
// hook that can hand a fresh join one (see gear.spec.ts's file comment) —
// per the task-3 plan, that state is asserted here at the store level
// instead of inventing a spawn hook.

// itemView builds a minimal ItemView fixture; only the fields these tests
// read need real values.
function itemView(overrides: Partial<ItemView>): ItemView {
  return {
    id: 0,
    defId: "",
    name: "",
    type: ItemTypeWeapon,
    tags: [],
    damageType: "",
    twoHanded: false,
    damage: 0,
    rangeHex: 0,
    aoeRadius: 0,
    stats: [],
    flavor: "",
    equipped: false,
    count: 1,
    throwable: false,
    recall: false,
    ...overrides,
  };
}

describe("offHandLocked", () => {
  test("false with an empty main-hand", () => {
    setInventory([]);
    expect(offHandLocked()).toBe(false);
  });

  test("false with a one-handed main-hand weapon", () => {
    setInventory([itemView({ id: 1, name: "Iron Sword", type: SlotMainHand, equipped: true, twoHanded: false })]);
    expect(offHandLocked()).toBe(false);
  });

  // The greyed-off-hand state itself: a two-handed weapon in main-hand.
  // No e2e path reaches this (see file comment) — asserted here instead.
  test("true with a two-handed main-hand weapon", () => {
    setInventory([
      itemView({ id: 1, name: "Wyrmslayer Greatsword", type: SlotMainHand, equipped: true, twoHanded: true }),
    ]);
    expect(offHandLocked()).toBe(true);
  });
});

describe("targetSlotFor", () => {
  test("armor/jewelry: the slot equals the type", () => {
    setInventory([]);
    expect(targetSlotFor({ type: SlotChest, twoHanded: false })).toBe(SlotChest);
  });

  test("a two-handed weapon always targets main-hand, even mid-swap", () => {
    setInventory([itemView({ id: 1, name: "Dagger", type: SlotMainHand, equipped: true, twoHanded: false })]);
    expect(targetSlotFor({ type: ItemTypeWeapon, twoHanded: true })).toBe(SlotMainHand);
  });

  test("a one-handed weapon: main-hand when empty", () => {
    setInventory([]);
    expect(targetSlotFor({ type: ItemTypeWeapon, twoHanded: false })).toBe(SlotMainHand);
  });

  test("a one-handed weapon: off-hand when main is taken", () => {
    setInventory([itemView({ id: 1, name: "Dagger", type: SlotMainHand, equipped: true, twoHanded: false })]);
    expect(targetSlotFor({ type: ItemTypeWeapon, twoHanded: false })).toBe(SlotOffHand);
  });

  test("a one-handed weapon: swaps main when both hands are full", () => {
    setInventory([
      itemView({ id: 1, name: "Dagger", type: SlotMainHand, equipped: true, twoHanded: false }),
      itemView({ id: 2, name: "Shortbow", type: SlotOffHand, equipped: true, twoHanded: false }),
    ]);
    expect(targetSlotFor({ type: ItemTypeWeapon, twoHanded: false })).toBe(SlotMainHand);
  });

  // A shield ALWAYS targets the off-hand (#90) — unlike a weapon's placement
  // matrix, occupancy never redirects it. Without the mapping, the raw
  // "shield" type string matches no slot key and the tooltip's
  // compare-against-equipped silently shows nothing.
  test("a shield targets off-hand when empty", () => {
    setInventory([]);
    expect(targetSlotFor({ type: ItemTypeShield, twoHanded: false })).toBe(SlotOffHand);
  });

  test("a shield targets off-hand even when occupied", () => {
    setInventory([itemView({ id: 1, name: "Shortbow", type: SlotOffHand, equipped: true, twoHanded: false })]);
    expect(targetSlotFor({ type: ItemTypeShield, twoHanded: false })).toBe(SlotOffHand);
  });
});

// K1 review finding, pinned: two dual-wielded weapons must land under
// DISTINCT keys in equipped() — before the gear-keystone wire fix
// (itemViewOf now sets an equipped weapon's Type to its hand, not the
// generic "weapon" taxonomy string), both would collide under "weapon" and
// the second equip would silently clobber the first in this map.
test("dual-wielded weapons key equipped() by hand, not by the shared item type", () => {
  setInventory([
    itemView({ id: 1, name: "Dagger", type: SlotMainHand, equipped: true }),
    itemView({ id: 2, name: "Shortbow", type: SlotOffHand, equipped: true }),
    itemView({ id: 3, name: "Leather Armor", type: SlotChest, equipped: true, tags: [], twoHanded: false }),
  ]);

  const eq = equipped();
  expect(eq[SlotMainHand]?.name).toBe("Dagger");
  expect(eq[SlotOffHand]?.name).toBe("Shortbow");
  expect(eq[SlotChest]?.name).toBe("Leather Armor");
  expect(Object.keys(eq).sort()).toEqual([SlotChest, SlotMainHand, SlotOffHand].sort());
});

test("a backpack item's type stays generic — a weapon has no hand until equipped", () => {
  setInventory([itemView({ id: 1, name: "Spare Sword", type: ItemTypeWeapon, equipped: false })]);
  expect(equipped()).toEqual({});
  expect(backpack()[0]?.type).toBe(ItemTypeWeapon);
});

test("a consumable backpack stack is never equipped", () => {
  setInventory([itemView({ id: 1, name: "Healing Potion", type: ItemTypeConsumable, equipped: false, count: 3 })]);
  expect(equipped()).toEqual({});
});

// --- #193: rejection feedback surfaces the real reason and clears itself ---

function pickupRow(id: number): Omit<PickupRow, "rejected" | "rejectedReason"> {
  return {
    id,
    name: `item${id}`,
    type: ItemTypeWeapon,
    count: 1,
    damage: 0,
    rangeHex: 0,
    aoeRadius: 0,
    stats: [],
    flavor: "",
    tags: [],
    damageType: "",
    twoHanded: false,
  };
}

function fillBackpack(n: number): void {
  setInventory(
    Array.from({ length: n }, (_, i) => itemView({ id: 100 + i, name: `bp${i}`, type: ItemTypeConsumable })),
  );
}

describe("pickup rejection feedback (#193)", () => {
  test("markPickupRejected stores the server's reason; defaults to backpack-full text", () => {
    fillBackpack(BackpackSize); // no room, so the mark isn't auto-cleared below
    refreshPickup([pickupRow(1), pickupRow(2)], false);

    markPickupRejected(1, "that item is no longer there");
    markPickupRejected(2); // no reason → sensible default

    const rows = pickupRows();
    expect(rows.find((r) => r.id === 1)).toMatchObject({ rejected: true, rejectedReason: "that item is no longer there" });
    expect(rows.find((r) => r.id === 2)).toMatchObject({ rejected: true, rejectedReason: "backpack full — drop something first" });
  });

  test("a rejected row clears once a backpack slot frees, without leaving the hex", () => {
    fillBackpack(BackpackSize);
    refreshPickup([pickupRow(3)], false);
    markPickupRejected(3);
    expect(pickupRows().find((r) => r.id === 3)?.rejected).toBe(true);

    // full → still rejected on the next bundle
    refreshPickup([pickupRow(3)], false);
    expect(pickupRows().find((r) => r.id === 3)?.rejected).toBe(true);

    // a slot frees (dropped something) → the mark clears so the player can retry
    fillBackpack(BackpackSize - 1);
    refreshPickup([pickupRow(3)], false);
    expect(pickupRows().find((r) => r.id === 3)).toMatchObject({ rejected: false, rejectedReason: "" });
  });
});

describe("per-item pending clear (#193)", () => {
  test("clearOnePending drops only the named item's mark", () => {
    setInventory([itemView({ id: 7, name: "sword" }), itemView({ id: 8, name: "shield", type: ItemTypeShield })]);
    markPending(7);
    markPending(8);
    expect(pending().size).toBe(2);

    clearOnePending(7);
    expect(pending().has(7)).toBe(false);
    expect(pending().has(8)).toBe(true);
  });
});
