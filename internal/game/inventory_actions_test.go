package game_test

// inventory_actions_test.go: task 2 of the inventory-slots milestone — the
// unequip/drop/pickup/drink intents (equip's own coverage lives in
// equip_test.go/unequip_test.go), the pickup priority (merge > free entry >
// reject with the exact client-facing error), drop→ground→re-pickup
// identity, drink heal/clamp/decrement/free, and the free-outside/
// turn-inside rule for the new actions (mirroring equip's bubble tests).

import (
	"errors"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// intentFor builds an inventory-action IntentRequest.
func intentFor(id int64, token, kind string, itemID int64) protocol.IntentRequest {
	return protocol.IntentRequest{EntityID: id, Token: token, Kind: kind, ItemID: itemID}
}

// Item def ids used across these tests, named so goconst is satisfied and a
// typo is a compile error.
const (
	defIronSword     = "iron-sword"
	defIronWarhammer = "iron-warhammer"
	defHealingPotion = "healing-potion"
	defVenomFang     = "venom-fang"
)

// pickupIntent builds a pickup IntentRequest (GroundItemID, not ItemID).
func pickupIntent(id int64, token string, groundItemID int64) protocol.IntentRequest {
	return protocol.IntentRequest{EntityID: id, Token: token, Kind: protocol.IntentPickup, GroundItemID: groundItemID}
}

// TestUnequipMovesItemToBackpack: outside a bubble, unequip is free and
// immediate — the slot clears and the item lands in the first free backpack
// entry (fists fallback takes over for melee).
func TestUnequipMovesItemToBackpack(t *testing.T) {
	t.Parallel()

	w := newWorld()

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	closeInst, _ := w.EquippedSlotsForTest(me.EntityID)
	if closeInst == 0 {
		t.Fatal("joined fighter has no equipped melee weapon to unequip")
	}

	if err := w.SubmitIntent(intentFor(me.EntityID, me.Token, protocol.IntentUnequip, closeInst)); err != nil {
		t.Fatalf("SubmitIntent unequip: %v", err)
	}

	if got, want := firstOf(w.EquippedSlotsForTest(me.EntityID)), int64(0); got != want {
		t.Errorf("melee slot after unequip = %d, want %d (empty)", got, want)
	}

	pack := w.BackpackForTest(me.EntityID)
	if got, want := pack[0].DefID, defIronSword; got != want {
		t.Errorf("backpack[0] = %q, want %q", got, want)
	}

	if got, want := pack[0].Count, 1; got != want {
		t.Errorf("backpack[0].Count = %d, want %d", got, want)
	}
}

// firstOf collapses EquippedSlotsForTest's pair to its first value at a call
// site that only cares about the melee-ish slot.
func firstOf(a, _ int64) int64 { return a }

// TestUnequipRejectedWhenBackpackFull: unequip needs a free backpack entry —
// with all four full it is rejected with ErrBackpackFull, whose exact text
// is the spec's client-facing feedback line.
func TestUnequipRejectedWhenBackpackFull(t *testing.T) {
	t.Parallel()

	w := newWorld()

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	for range protocol.BackpackSize {
		if w.GrantItemForTest(me.EntityID, defIronWarhammer) == 0 {
			t.Fatal("GrantItemForTest failed to fill the backpack")
		}
	}

	closeInst, _ := w.EquippedSlotsForTest(me.EntityID)

	err = w.SubmitIntent(intentFor(me.EntityID, me.Token, protocol.IntentUnequip, closeInst))
	if got, want := err, game.ErrBackpackFull; !errors.Is(got, want) {
		t.Fatalf("err = %v, want %v", got, want)
	}

	if got, want := err.Error(), "backpack full — drop something first"; got != want {
		t.Errorf("err.Error() = %q, want %q (the client surfaces this text as feedback)", got, want)
	}

	if got, _ := w.EquippedSlotsForTest(me.EntityID); got != closeInst {
		t.Errorf("melee slot = %d, want %d still equipped", got, closeInst)
	}
}

// TestUnequipNotEquippedRejected: unequip of an owned but backpack-resident
// item is ErrItemNotEquipped.
func TestUnequipNotEquippedRejected(t *testing.T) {
	t.Parallel()

	w := newWorld()

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	instID := w.GrantItemForTest(me.EntityID, defIronWarhammer)

	if got, want := w.SubmitIntent(intentFor(me.EntityID, me.Token, protocol.IntentUnequip, instID)),
		game.ErrItemNotEquipped; !errors.Is(got, want) {
		t.Errorf("err = %v, want %v", got, want)
	}
}

// TestDropEquippedItemLandsOnOwnHex: dropping an equipped item clears its
// slot and lands it on the player's own hex — visible as a GroundItemView
// with the item's type (the pickup prompt's name + type).
func TestDropEquippedItemLandsOnOwnHex(t *testing.T) {
	t.Parallel()

	w := newWorld()

	target := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, target) {
		t.Skip("origin is not walkable on this map")
	}

	id, token := w.PlaceEntityForTest(target)
	closeInst, _ := w.EquippedSlotsForTest(id)

	if err := w.SubmitIntent(intentFor(id, token, protocol.IntentDrop, closeInst)); err != nil {
		t.Fatalf("SubmitIntent drop: %v", err)
	}

	if got, _ := w.EquippedSlotsForTest(id); got != 0 {
		t.Errorf("melee slot after drop = %d, want 0", got)
	}

	snap := w.Snapshot()
	if got, want := len(snap.GroundItems), 1; got != want {
		t.Fatalf("len(GroundItems) = %d, want %d", got, want)
	}

	ground := snap.GroundItems[0]
	if got, want := ground.Hex, target; got != want {
		t.Errorf("ground item hex = %v, want the dropper's own hex %v", got, want)
	}

	if got, want := ground.DefID, defIronSword; got != want {
		t.Errorf("ground item def = %q, want %q", got, want)
	}

	if got, want := ground.Type, protocol.ItemTypeWeapon; got != want {
		t.Errorf("ground item type = %q, want %q", got, want)
	}
}

// TestDropThenRePickupKeepsIdentity: a dropped gear item keeps its instance
// id on the ground and comes back with the same id — into the BACKPACK, not
// its old slot (items never auto-equip on pickup, even with the matching
// slot empty).
func TestDropThenRePickupKeepsIdentity(t *testing.T) {
	t.Parallel()

	w := newWorld()

	target := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, target) {
		t.Skip("origin is not walkable on this map")
	}

	id, token := w.PlaceEntityForTest(target)
	closeInst, _ := w.EquippedSlotsForTest(id)

	if err := w.SubmitIntent(intentFor(id, token, protocol.IntentDrop, closeInst)); err != nil {
		t.Fatalf("SubmitIntent drop: %v", err)
	}

	snap := w.Snapshot()
	if got, want := snap.GroundItems[0].ID, closeInst; got != want {
		t.Fatalf("ground instance id = %d, want the dropped instance %d", got, want)
	}

	if err := w.SubmitIntent(pickupIntent(id, token, closeInst)); err != nil {
		t.Fatalf("SubmitIntent pickup: %v", err)
	}

	if got, _ := w.EquippedSlotsForTest(id); got != 0 {
		t.Errorf("melee slot after re-pickup = %d, want 0 (no auto-equip)", got)
	}

	pack := w.BackpackForTest(id)
	if got, want := pack[0].DefID, defIronSword; got != want {
		t.Errorf("backpack[0] = %q, want %q (the re-picked sword)", got, want)
	}

	if got := w.EquippedInSlotForTest(id, protocol.SlotMainHand); got != 0 {
		t.Errorf("main-hand slot instance = %d, want 0", got)
	}
}

// TestDropStackDropsWhole: a consumable stack drops WHOLE — its backpack
// entry frees and it lands as ONE ground stack carrying its count (not N
// single instances), and a re-pickup takes the whole stack back in one go.
func TestDropStackDropsWhole(t *testing.T) {
	t.Parallel()

	w := newWorld()

	target := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, target) {
		t.Skip("origin is not walkable on this map")
	}

	id, token := w.PlaceEntityForTest(target)

	stackID := w.GrantItemForTest(id, defHealingPotion)
	w.GrantItemForTest(id, defHealingPotion)
	w.GrantItemForTest(id, defHealingPotion)

	pack := w.BackpackForTest(id)
	if got, want := pack[0].Count, 3; got != want {
		t.Fatalf("backpack[0].Count = %d, want %d (grants must merge into one stack)", got, want)
	}

	if err := w.SubmitIntent(intentFor(id, token, protocol.IntentDrop, stackID)); err != nil {
		t.Fatalf("SubmitIntent drop stack: %v", err)
	}

	pack = w.BackpackForTest(id)
	if got, want := pack[0].Count, 0; got != want {
		t.Errorf("backpack[0].Count after drop = %d, want %d (entry freed)", got, want)
	}

	snap := w.Snapshot()
	if got, want := len(snap.GroundItems), 1; got != want {
		t.Fatalf("len(GroundItems) = %d, want %d (dropped whole as ONE stack)", got, want)
	}

	ground := snap.GroundItems[0]
	if got, want := ground.DefID, defHealingPotion; got != want {
		t.Errorf("ground def = %q, want %q", got, want)
	}

	if got, want := ground.Count, 3; got != want {
		t.Errorf("ground stack Count = %d, want %d", got, want)
	}

	// Re-pickup: the whole stack returns to the backpack in one intent.
	if err := w.SubmitIntent(pickupIntent(id, token, stackID)); err != nil {
		t.Fatalf("SubmitIntent pickup whole stack: %v", err)
	}

	if got, want := len(w.Snapshot().GroundItems), 0; got != want {
		t.Errorf("len(GroundItems) after re-pickup = %d, want %d", got, want)
	}

	pack = w.BackpackForTest(id)
	if got, want := pack[0].Count, 3; got != want {
		t.Errorf("backpack[0].Count after re-pickup = %d, want %d (whole stack back)", got, want)
	}
}

// TestPickupStackPartialFitLeavesRemainder: picking up a ground stack that
// only partly fits (a nearly-full matching stack + no free entry beyond it)
// takes what fits and leaves the remainder on the ground as a smaller stack.
func TestPickupStackPartialFitLeavesRemainder(t *testing.T) {
	t.Parallel()

	w := newWorld()

	target := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, target) {
		t.Skip("origin is not walkable on this map")
	}

	id, token := w.PlaceEntityForTest(target)

	// Backpack: a 4-potion stack in entry 0, three gear items filling the
	// rest — so the only room for potions is the 1 slot left in that stack.
	for range 4 {
		w.GrantItemForTest(id, defHealingPotion)
	}

	for range protocol.BackpackSize - 1 {
		w.GrantItemForTest(id, defIronWarhammer)
	}

	// A 3-potion stack on the ground: only 1 fits (topping the stack to 5),
	// 2 remain on the ground.
	groundID := w.GroundStackForTest(target, defHealingPotion, 3)

	if err := w.SubmitIntent(pickupIntent(id, token, groundID)); err != nil {
		t.Fatalf("SubmitIntent partial pickup: %v", err)
	}

	pack := w.BackpackForTest(id)
	if got, want := pack[0].Count, protocol.ItemStackCap; got != want {
		t.Errorf("potion stack after partial pickup = %d, want the cap %d", got, want)
	}

	snap := w.Snapshot()
	if got, want := len(snap.GroundItems), 1; got != want {
		t.Fatalf("len(GroundItems) = %d, want %d (remainder stays)", got, want)
	}

	if got, want := snap.GroundItems[0].Count, 2; got != want {
		t.Errorf("ground remainder Count = %d, want %d", got, want)
	}

	if got, want := snap.GroundItems[0].ID, groundID; got != want {
		t.Errorf("ground remainder keeps its id = %d, want %d", got, want)
	}
}

// TestPickupPriorityMergeThenFreeThenReject pins the spec's pickup order:
// a consumable merges into an existing stack first (no new entry), a fresh
// def takes a free entry, and with no home at all the pickup is rejected
// with the exact "backpack full" feedback error.
func TestPickupPriorityMergeThenFreeThenReject(t *testing.T) {
	t.Parallel()

	w := newWorld()

	target := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, target) {
		t.Skip("origin is not walkable on this map")
	}

	id, token := w.PlaceEntityForTest(target)

	// One potion in the backpack; three gear items fill the rest.
	w.GrantItemForTest(id, defHealingPotion)

	for range protocol.BackpackSize - 1 {
		w.GrantItemForTest(id, defIronWarhammer)
	}

	// MERGE: a ground potion merges into the existing stack even though no
	// entry is free.
	groundPotion := w.GroundItemForTest(target, defHealingPotion)
	if err := w.SubmitIntent(pickupIntent(id, token, groundPotion)); err != nil {
		t.Fatalf("SubmitIntent pickup (merge): %v", err)
	}

	pack := w.BackpackForTest(id)
	if got, want := pack[0].Count, 2; got != want {
		t.Errorf("stack count after merge-pickup = %d, want %d", got, want)
	}

	// REJECT: gear cannot merge; with every entry taken it is rejected with
	// the exact client-facing feedback line.
	groundGear := w.GroundItemForTest(target, defVenomFang)

	err := w.SubmitIntent(pickupIntent(id, token, groundGear))
	if got, want := err, game.ErrBackpackFull; !errors.Is(got, want) {
		t.Fatalf("err = %v, want %v", got, want)
	}

	if got, want := err.Error(), "backpack full — drop something first"; got != want {
		t.Errorf("err.Error() = %q, want %q", got, want)
	}

	// The rejected item is still on the ground.
	snap := w.Snapshot()
	if got, want := len(snap.GroundItems), 1; got != want {
		t.Fatalf("len(GroundItems) = %d, want %d (rejected pickup must not consume the item)", got, want)
	}

	// FREE ENTRY: dropping a gear entry frees a home; the pickup then lands
	// in it.
	pack = w.BackpackForTest(id)

	var hammerID int64

	for i := range pack {
		if pack[i].DefID == defIronWarhammer {
			hammerID = backpackInstanceID(t, w, id, i)

			break
		}
	}

	if err := w.SubmitIntent(intentFor(id, token, protocol.IntentDrop, hammerID)); err != nil {
		t.Fatalf("SubmitIntent drop (free an entry): %v", err)
	}

	if err := w.SubmitIntent(pickupIntent(id, token, groundGear)); err != nil {
		t.Fatalf("SubmitIntent pickup (free entry): %v", err)
	}

	found := false

	for _, be := range w.BackpackForTest(id) {
		if be.DefID == defVenomFang {
			found = true
		}
	}

	if !found {
		t.Error("venom-fang not in backpack after free-entry pickup")
	}
}

// backpackInstanceID resolves the instance id sitting in backpack entry idx
// via the wire snapshot (BackpackForTest exposes def ids only; the item view
// carries the id).
func backpackInstanceID(t *testing.T, w *game.World, entityID int64, idx int) int64 {
	t.Helper()

	pack := w.BackpackForTest(entityID)
	wantDef := pack[idx].DefID

	snap := w.Snapshot()

	e, ok := entityOfSnap(snap, entityID)
	if !ok {
		t.Fatalf("entity %d missing from snapshot", entityID)
	}

	for _, it := range e.Items {
		if !it.Equipped && it.DefID == wantDef {
			return it.ID
		}
	}

	t.Fatalf("no unequipped %q in entity %d's items", wantDef, entityID)

	return 0
}

// TestStackNeverExceedsCap: a stack at ItemStackCap does not absorb another
// unit — with no free entry the pickup is rejected (stacks never split, so
// there is no partial merge), and with a free entry the unit starts a NEW
// stack.
func TestStackNeverExceedsCap(t *testing.T) {
	t.Parallel()

	w := newWorld()

	target := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, target) {
		t.Skip("origin is not walkable on this map")
	}

	id, token := w.PlaceEntityForTest(target)

	for range protocol.ItemStackCap {
		w.GrantItemForTest(id, defHealingPotion)
	}

	pack := w.BackpackForTest(id)
	if got, want := pack[0].Count, protocol.ItemStackCap; got != want {
		t.Fatalf("stack count = %d, want the cap %d", got, want)
	}

	ground := w.GroundItemForTest(target, defHealingPotion)
	if err := w.SubmitIntent(pickupIntent(id, token, ground)); err != nil {
		t.Fatalf("SubmitIntent pickup: %v", err)
	}

	pack = w.BackpackForTest(id)
	if got, want := pack[0].Count, protocol.ItemStackCap; got != want {
		t.Errorf("capped stack count = %d, want unchanged %d", got, want)
	}

	if got, want := pack[1].DefID, defHealingPotion; got != want {
		t.Errorf("backpack[1] = %q, want a NEW potion stack %q", got, want)
	}

	if got, want := pack[1].Count, 1; got != want {
		t.Errorf("backpack[1].Count = %d, want %d", got, want)
	}
}

// TestDrinkHealsDecrementsAndFrees: drinking applies the potion's heal
// (clamped to max HP), decrements the stack, and frees the entry at zero.
func TestDrinkHealsDecrementsAndFrees(t *testing.T) {
	t.Parallel()

	w := newWorld()

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	stackID := w.GrantItemForTest(me.EntityID, defHealingPotion)
	w.GrantItemForTest(me.EntityID, defHealingPotion)

	maxHP := game.MaxHPForTest(protocol.ClassFighter, 1)
	w.SetHPForTest(me.EntityID, maxHP-7)

	if err := w.SubmitIntent(intentFor(me.EntityID, me.Token, protocol.IntentDrink, stackID)); err != nil {
		t.Fatalf("SubmitIntent drink: %v", err)
	}

	snap := w.Snapshot()

	e, ok := entityOfSnap(snap, me.EntityID)
	if !ok {
		t.Fatal("player missing from snapshot")
	}

	if got, want := e.HP, maxHP-2; got != want { // +5 heal
		t.Errorf("HP after drink = %d, want %d", got, want)
	}

	pack := w.BackpackForTest(me.EntityID)
	if got, want := pack[0].Count, 1; got != want {
		t.Errorf("stack count after drink = %d, want %d", got, want)
	}

	// Second drink: heal clamps at max HP (only 2 below), stack empties, the
	// entry frees.
	if err := w.SubmitIntent(intentFor(me.EntityID, me.Token, protocol.IntentDrink, stackID)); err != nil {
		t.Fatalf("SubmitIntent drink (second): %v", err)
	}

	snap = w.Snapshot()
	e, _ = entityOfSnap(snap, me.EntityID)

	if got, want := e.HP, maxHP; got != want {
		t.Errorf("HP after clamped drink = %d, want max %d", got, want)
	}

	pack = w.BackpackForTest(me.EntityID)
	if got, want := pack[0].Count, 0; got != want {
		t.Errorf("stack entry after emptying = count %d, want %d (freed)", got, want)
	}

	// The emptied stack is gone: a third drink is not owned anymore.
	if got, want := w.SubmitIntent(intentFor(me.EntityID, me.Token, protocol.IntentDrink, stackID)),
		game.ErrItemNotOwned; !errors.Is(got, want) {
		t.Errorf("drink of an emptied stack err = %v, want %v", got, want)
	}
}

// TestDrinkGearRejected: drinking a weapon is ErrNotDrinkable.
func TestDrinkGearRejected(t *testing.T) {
	t.Parallel()

	w := newWorld()

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	closeInst, _ := w.EquippedSlotsForTest(me.EntityID)

	if got, want := w.SubmitIntent(intentFor(me.EntityID, me.Token, protocol.IntentDrink, closeInst)),
		game.ErrNotDrinkable; !errors.Is(got, want) {
		t.Errorf("err = %v, want %v", got, want)
	}
}

// TestEquipConsumableRejectedAsNotEquippable: equipping a consumable is
// ErrNotEquippable ("that item can't be equipped") — a consumable has no
// equip slot at all (drink, not equip, is its action); class gates are gone
// entirely (#56), so this is the only equip-intent rejection left.
func TestEquipConsumableRejectedAsNotEquippable(t *testing.T) {
	t.Parallel()

	w := newWorld()

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	potionID := w.GrantItemForTest(me.EntityID, defHealingPotion)

	if got, want := w.SubmitIntent(intentFor(me.EntityID, me.Token, protocol.IntentEquip, potionID)),
		game.ErrNotEquippable; !errors.Is(got, want) {
		t.Errorf("err = %v, want %v", got, want)
	}
}

// TestPickupAtRangeRejected: a pickup naming a ground item on a DIFFERENT
// hex is ErrNoSuchGroundItem — you must stand on the item.
func TestPickupAtRangeRejected(t *testing.T) {
	t.Parallel()

	w := newWorld()

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	elsewhere := walkableNeighbor(t, w, me.Hex)
	ground := w.GroundItemForTest(elsewhere, defVenomFang)

	if got, want := w.SubmitIntent(pickupIntent(me.EntityID, me.Token, ground)),
		game.ErrNoSuchGroundItem; !errors.Is(got, want) {
		t.Errorf("err = %v, want %v", got, want)
	}
}

// TestDrinkInBubbleConsumesTurn: inside a combat bubble, drinking is the
// player's action for the turn — the heal does not apply at submit, the
// submission marks the player ready, and the heal lands when the bubble
// turn resolves (before that same turn's damage, which then applies on
// top). Mirrors TestEquipInBubbleQueuesClearsPathAppliesAfterTurn.
func TestDrinkInBubbleConsumesTurn(t *testing.T) {
	t.Parallel()

	w := newWorld()
	idA, tokA, idB, tokB, monsterID, form := twoPlayerBubble(t, w)

	stackID := w.GrantItemForTest(idA, defHealingPotion)

	maxHP := game.MaxHPForTest(protocol.ClassFighter, 1)
	w.SetHPForTest(idA, maxHP-10)

	if err := w.SubmitIntent(intentFor(idA, tokA, protocol.IntentDrink, stackID)); err != nil {
		t.Fatalf("SubmitIntent drink: %v", err)
	}

	// Not applied yet: B has not locked in.
	if got, want := entityHP(t, w.Snapshot(), idA), maxHP-10; got != want {
		t.Fatalf("HP after in-bubble drink submit = %d, want unchanged %d", got, want)
	}

	waiting := w.Snapshot().Bubbles[0].WaitingForIDs
	if len(waiting) != 1 || waiting[0] != idB {
		t.Fatalf("WaitingForIDs = %v, want only %d (drink must mark A ready)", waiting, idB)
	}

	// B locks in -> the bubble resolves: A heals +5, then the monster's melee
	// attack (it targets A — nearest, lowest-id tie-break) deals its damage.
	hexB0 := hexOfSnap(form, idB)
	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: idB, Token: tokB, Kind: protocol.IntentMove, Target: hexB0,
	}); err != nil {
		t.Fatalf("SubmitIntent B move: %v", err)
	}

	wolfDamage := game.MonsterDamageForTest(w.MonsterKindForTest(monsterID))

	if got, want := entityHP(t, w.Snapshot(), idA), maxHP-10+5-wolfDamage; got != want {
		t.Errorf("HP after bubble resolution = %d, want %d (heal, then this turn's melee attack)", got, want)
	}

	pack := w.BackpackForTest(idA)
	if got, want := pack[0].Count, 0; got != want {
		t.Errorf("stack count after resolution = %d, want %d", got, want)
	}
}

// TestPickupInBubbleConsumesTurn: inside a bubble a pickup queues as the
// turn's action and grants only at resolution.
func TestPickupInBubbleConsumesTurn(t *testing.T) {
	t.Parallel()

	w := newWorld()
	idA, tokA, idB, tokB, _, form := twoPlayerBubble(t, w)

	hexA := hexOfSnap(form, idA)
	ground := w.GroundItemForTest(hexA, defIronWarhammer)

	if err := w.SubmitIntent(pickupIntent(idA, tokA, ground)); err != nil {
		t.Fatalf("SubmitIntent pickup: %v", err)
	}

	if got, want := len(w.Snapshot().GroundItems), 1; got != want {
		t.Fatalf("GroundItems after in-bubble pickup submit = %d, want %d (not applied yet)", got, want)
	}

	hexB0 := hexOfSnap(form, idB)
	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: idB, Token: tokB, Kind: protocol.IntentMove, Target: hexB0,
	}); err != nil {
		t.Fatalf("SubmitIntent B move: %v", err)
	}

	if got, want := len(w.Snapshot().GroundItems), 0; got != want {
		t.Errorf("GroundItems after resolution = %d, want %d", got, want)
	}

	found := false

	for _, be := range w.BackpackForTest(idA) {
		if be.DefID == defIronWarhammer {
			found = true
		}
	}

	if !found {
		t.Error("iron-warhammer not in A's backpack after the bubble turn resolved")
	}
}
