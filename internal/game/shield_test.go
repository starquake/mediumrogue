package game_test

// shield_test.go: shields v1 (#90, S4 of #55) — the shield item type equips
// off-hand only (with the two-handed eviction rule in both directions) and
// its flat take-damage −N card folds at the live combat site (spec:
// docs/superpowers/specs/2026-07-15-shields-design.md). Placement tests
// mirror equip_test.go; the combat tests mirror species_test.go's dwarf-DR
// and starter_content_test.go's leather-armor style.

import (
	"errors"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// Def ids this file exercises (content.go's registry) — named so goconst
// stays quiet and a typo is one edit away from every use.
const (
	defWoodenBuckler  = "wooden-buckler"
	defIronKiteShield = "iron-kite-shield"
	defGreatsword     = "wyrmslayer-greatsword"
)

// joinFighter joins a fresh human fighter (iron sword in main) and fails the
// test on error.
func joinFighter(t *testing.T, w *game.World) protocol.JoinResponse {
	t.Helper()

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	return me
}

// grantAndEquip grants one instance of defID and equips it through the real
// intent path, returning the instance id.
func grantAndEquip(t *testing.T, w *game.World, me protocol.JoinResponse, defID string) int64 {
	t.Helper()

	instID := w.GrantItemForTest(me.EntityID, defID)
	if err := w.SubmitIntent(equipIntent(me.EntityID, me.Token, instID)); err != nil {
		t.Fatalf("SubmitIntent equip %s: %v", defID, err)
	}

	return instID
}

// backpackHas reports whether any backpack entry of the entity holds defID.
func backpackHas(w *game.World, entityID int64, defID string) bool {
	for _, be := range w.BackpackForTest(entityID) {
		if be.DefID == defID {
			return true
		}
	}

	return false
}

// TestEquipShieldLandsOffHand: a shield always equips into the off-hand
// (slotForType) — the main hand's weapon stays put.
func TestEquipShieldLandsOffHand(t *testing.T) {
	t.Parallel()

	w := newWorld()
	me := joinFighter(t, w)

	mainBefore, _ := w.EquippedSlotsForTest(me.EntityID) // the class-default iron sword

	instID := grantAndEquip(t, w, me, defWoodenBuckler)

	mainInst, offInst := w.EquippedSlotsForTest(me.EntityID)
	if got, want := offInst, instID; got != want {
		t.Errorf("off-hand slot = %d, want %d (a shield equips off-hand)", got, want)
	}

	if got, want := mainInst, mainBefore; got != want {
		t.Errorf("main-hand slot = %d, want unchanged %d", got, want)
	}
}

// TestEquipShieldTogglesOff: naming an already-equipped shield unequips it
// back into the backpack (the equip-intent-as-toggle contract).
func TestEquipShieldTogglesOff(t *testing.T) {
	t.Parallel()

	w := newWorld()
	me := joinFighter(t, w)

	instID := grantAndEquip(t, w, me, defWoodenBuckler)

	if err := w.SubmitIntent(equipIntent(me.EntityID, me.Token, instID)); err != nil {
		t.Fatalf("SubmitIntent toggle-off: %v", err)
	}

	if _, offInst := w.EquippedSlotsForTest(me.EntityID); offInst != 0 {
		t.Errorf("off-hand slot = %d, want empty (0) after the toggle-off", offInst)
	}

	if !backpackHas(w, me.EntityID, defWoodenBuckler) {
		t.Errorf("backpack = %v, want the wooden-buckler back in an entry", w.BackpackForTest(me.EntityID))
	}
}

// TestEquipShieldEvictsTwoHander: a two-handed weapon in main locks the
// off-hand — equipping a shield evicts it to the backpack first, then the
// shield lands off-hand.
func TestEquipShieldEvictsTwoHander(t *testing.T) {
	t.Parallel()

	w := newWorld()
	me := joinFighter(t, w)

	grantAndEquip(t, w, me, defGreatsword)
	bucklerID := grantAndEquip(t, w, me, defWoodenBuckler)

	mainInst, offInst := w.EquippedSlotsForTest(me.EntityID)
	if got, want := offInst, bucklerID; got != want {
		t.Errorf("off-hand slot = %d, want the buckler %d", got, want)
	}

	if got, want := mainInst, int64(0); got != want {
		t.Errorf("main-hand slot = %d, want empty (%d) — the two-hander must be evicted", got, want)
	}

	if !backpackHas(w, me.EntityID, defGreatsword) {
		t.Errorf("backpack = %v, want the evicted greatsword in an entry", w.BackpackForTest(me.EntityID))
	}
}

// TestEquipShieldBackpackFullRejected: evicting the two-hander needs a free
// backpack entry — with none, the equip fails politely (ErrBackpackFull)
// BEFORE any state change, mirroring equipWeaponLocked's rule.
func TestEquipShieldBackpackFullRejected(t *testing.T) {
	t.Parallel()

	w := newWorld()
	me := joinFighter(t, w)

	greatswordID := grantAndEquip(t, w, me, defGreatsword)

	// The greatsword swap left the class-default sword in one backpack entry;
	// the buckler takes another; daggers fill the rest (protocol.BackpackSize
	// entries total — gear never stacks).
	bucklerID := w.GrantItemForTest(me.EntityID, defWoodenBuckler)

	for i := 0; ; i++ {
		if w.GrantItemForTest(me.EntityID, "dagger") == 0 {
			break // backpack full
		}

		if i > protocol.BackpackSize {
			t.Fatalf("backpack never filled after %d dagger grants", i)
		}
	}

	got := w.SubmitIntent(equipIntent(me.EntityID, me.Token, bucklerID))
	if want := game.ErrBackpackFull; !errors.Is(got, want) {
		t.Errorf("err = %v, want %v", got, want)
	}

	// Nothing changed: the two-hander still holds main, the off-hand is still
	// empty, and the buckler still sits in its backpack entry.
	mainInst, offInst := w.EquippedSlotsForTest(me.EntityID)
	if got, want := mainInst, greatswordID; got != want {
		t.Errorf("main-hand slot = %d, want unchanged %d", got, want)
	}

	if got, want := offInst, int64(0); got != want {
		t.Errorf("off-hand slot = %d, want still empty (%d)", got, want)
	}

	if !backpackHas(w, me.EntityID, defWoodenBuckler) {
		t.Errorf("backpack = %v, want the wooden-buckler still in its entry", w.BackpackForTest(me.EntityID))
	}
}

// TestEquipTwoHanderEvictsShield: the other direction — equipping a
// two-handed weapon while a shield holds the off-hand evicts the shield
// (equipWeaponLocked's existing any-off-hand-occupant rule; coverage, not
// new code).
func TestEquipTwoHanderEvictsShield(t *testing.T) {
	t.Parallel()

	w := newWorld()
	me := joinFighter(t, w)

	grantAndEquip(t, w, me, defWoodenBuckler)
	greatswordID := grantAndEquip(t, w, me, defGreatsword)

	mainInst, offInst := w.EquippedSlotsForTest(me.EntityID)
	if got, want := mainInst, greatswordID; got != want {
		t.Errorf("main-hand slot = %d, want the greatsword %d", got, want)
	}

	if got, want := offInst, int64(0); got != want {
		t.Errorf("off-hand slot = %d, want empty (%d) — the shield must be evicted", got, want)
	}

	if !backpackHas(w, me.EntityID, defWoodenBuckler) {
		t.Errorf("backpack = %v, want the evicted wooden-buckler in an entry", w.BackpackForTest(me.EntityID))
	}
}

// TestEquipOneHanderLeavesShield: equipping a one-handed weapon while a
// shield holds the off-hand swaps MAIN (weaponTargetSlot's fall-through) —
// the shield stays put.
func TestEquipOneHanderLeavesShield(t *testing.T) {
	t.Parallel()

	w := newWorld()
	me := joinFighter(t, w)

	bucklerID := grantAndEquip(t, w, me, defWoodenBuckler)
	warhammerID := grantAndEquip(t, w, me, "iron-warhammer")

	mainInst, offInst := w.EquippedSlotsForTest(me.EntityID)
	if got, want := mainInst, warhammerID; got != want {
		t.Errorf("main-hand slot = %d, want the warhammer %d (one-hander swaps main)", got, want)
	}

	if got, want := offInst, bucklerID; got != want {
		t.Errorf("off-hand slot = %d, want the buckler %d untouched", got, want)
	}
}

// shieldedDamageTaken places a fighter at origin, equips it with the given
// gear def ids (in order), optionally re-species it, puts a wolf adjacent,
// drives the wolf's melee attack (SetPathForTest onto the player's hex — the
// monster conversion path), and returns how much HP the player lost. The
// player queues nothing, so it does not hit back. No card in play carries a
// chance condition (human/dwarf fighter), so no combat rng is consumed —
// the outcome is seed-independent.
func shieldedDamageTaken(t *testing.T, species string, gearIDs ...string) int {
	t.Helper()

	w := newWorld()
	w.SetSeedForTest(40)

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	pid, token := w.PlaceEntityForTest(center) // level-1 Fighter
	if species != "" {
		w.SetSpeciesForTest(pid, species)
	}

	for _, defID := range gearIDs {
		instID := w.GrantItemForTest(pid, defID)
		if err := w.SubmitIntent(protocol.IntentRequest{
			EntityID: pid, Token: token, Kind: protocol.IntentEquip, ItemID: instID,
		}); err != nil {
			t.Fatalf("SubmitIntent equip %s: %v", defID, err)
		}
	}

	monsterID := w.PlaceMonsterForTest(walkableNeighbor(t, w, center))

	w.SetPathForTest(monsterID, []protocol.Hex{center})
	w.ResolveCombatOnlyForTest()

	player, ok := entityOfSnap(w.Snapshot(), pid)
	if !ok {
		t.Fatalf("player %d missing after a monster melee attack", pid)
	}

	return game.MaxHPForTest(protocol.ClassFighter, 1) - player.HP
}

// TestShieldReducesTakenDamage: a wolf melee hit on a buckler-bearing
// fighter lands for exactly one less than its claws damage — the shield's
// take-damage −1 card folded at the live combat site.
func TestShieldReducesTakenDamage(t *testing.T) {
	t.Parallel()

	want := game.MonsterDamageForTest("wolf") - 1
	if got := shieldedDamageTaken(t, "", defWoodenBuckler); got != want {
		t.Errorf("buckler-bearing fighter lost %d HP to a wolf melee attack, want %d (take-damage -1)", got, want)
	}
}

// TestShieldStackingClampsAtOne: the iron kite shield (−2) stacks additively
// with leather armor (−1) and the dwarf passive (−1) inside the take-damage
// fold, but applyRules' event-level clamp keeps every landed hit ≥ 1: a
// wolf's 3 damage lands for exactly 1, not 0 or less.
func TestShieldStackingClampsAtOne(t *testing.T) {
	t.Parallel()

	if got, want := game.MonsterDamageForTest("wolf"), 3; got != want {
		t.Fatalf("wolf damage = %d, want %d (this test's clamp premise: 3-1-1-2 < 1)", got, want)
	}

	got := shieldedDamageTaken(t, protocol.SpeciesDwarf, "leather-armor", defIronKiteShield)
	if want := 1; got != want {
		t.Errorf("dwarf in leather armor with the kite shield lost %d HP, want %d (clamped floor)", got, want)
	}
}
