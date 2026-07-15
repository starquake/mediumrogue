package game_test

import (
	"errors"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestMeleeIntentDealsDamageAttackerStays (#116): an entity-targeted attack
// intent against an ADJACENT hostile is a melee swing — valid even for a
// fighter, who holds no ranged weapon — and the attacker's hex never
// changes (attacking is not a move).
func TestMeleeIntentDealsDamageAttackerStays(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	if err := w.SubmitIntent(entityAttackIntent(me.EntityID, me.Token, monsterID)); err != nil {
		t.Fatalf("SubmitIntent(adjacent melee attack): %v", err)
	}

	snap := step(t, w)

	monster, ok := entityOfSnap(snap, monsterID)
	if !ok {
		t.Fatalf("monster %d should survive one sword hit", monsterID)
	}

	if got, want := monster.HP, protocol.MonsterMaxHP-game.ItemDamageForTest("iron-sword"); got != want {
		t.Errorf("monster HP = %d, want %d (the melee swing lands)", got, want)
	}

	if got, want := hexOfSnap(snap, me.EntityID), me.Hex; got != want {
		t.Errorf("attacker hex = %v, want unchanged %v (attacking never moves you)", got, want)
	}
}

// TestMeleeIntentOneClickOneSwing (#116): an attack intent is one-shot —
// with no re-submission, the second turn deals no further damage.
func TestMeleeIntentOneClickOneSwing(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	if err := w.SubmitIntent(entityAttackIntent(me.EntityID, me.Token, monsterID)); err != nil {
		t.Fatalf("SubmitIntent: %v", err)
	}

	step(t, w) // swing lands

	afterSecond := step(t, w) // nothing re-submitted — no second swing

	monster, ok := entityOfSnap(afterSecond, monsterID)
	if !ok {
		t.Fatalf("monster %d should still be alive", monsterID)
	}

	if got, want := monster.HP, protocol.MonsterMaxHP-game.ItemDamageForTest("iron-sword"); got != want {
		t.Errorf("monster HP after idle turn = %d, want %d (one click = one swing)", got, want)
	}
}

// TestMeleeIntentNonAdjacentNoRangedRejected (#116): a fighter (melee only)
// naming a hostile TWO hexes away is rejected with ErrOutOfRange at submit —
// melee reach is exactly 1, and no ranged weapon covers the gap. (This case
// used to reject as ErrNoRangedWeapon; the melee branch folds it into the
// unified reach check.)
func TestMeleeIntentNonAdjacentNoRangedRejected(t *testing.T) {
	t.Parallel()

	w := newWorld()

	rogueID, token := w.PlaceEntityForTest(protocol.Hex{Q: 0, R: 0})
	w.SetClassForTest(rogueID, protocol.ClassFighter)
	monsterID := w.PlaceMonsterForTest(protocol.Hex{Q: 2, R: 0})

	got := w.SubmitIntent(entityAttackIntent(rogueID, token, monsterID))
	if want := game.ErrOutOfRange; !errors.Is(got, want) {
		t.Errorf("err = %v, want %v", got, want)
	}
}

// TestMeleeIntentHitsNamedVictimOnStack (#116): melee on a stacked hex hits
// exactly the NAMED victim, like a bow shot — the conversion path's seeded
// stack pick does not apply to attack intents.
func TestMeleeIntentHitsNamedVictimOnStack(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	monsterHex := walkableNeighbor(t, w, me.Hex)
	firstID := w.PlaceMonsterForTest(monsterHex)
	secondID := w.PlaceMonsterForTest(monsterHex) // stacks on the same hex

	if err := w.SubmitIntent(entityAttackIntent(me.EntityID, me.Token, secondID)); err != nil {
		t.Fatalf("SubmitIntent: %v", err)
	}

	snap := step(t, w)

	if got, want := entityHP(t, snap, secondID), protocol.MonsterMaxHP-game.ItemDamageForTest("iron-sword"); got != want {
		t.Errorf("named victim HP = %d, want %d", got, want)
	}

	if got, want := entityHP(t, snap, firstID), protocol.MonsterMaxHP; got != want {
		t.Errorf("bystander HP = %d, want %d (untouched)", got, want)
	}
}

// TestMeleeIntentDualWieldExclusiveAtDistance1 (#116, self-review item): the
// four tests above only exercise a fighter (melee-only), so they can't tell
// a "melee-exclusive" resolution apart from a "ranged-only" one — a rogue's
// default kit is dagger (4 dmg) + shortbow (4 dmg), and those numbers are
// EQUAL, so asserting "HP drops by 4" would pass whether the swing used the
// dagger, the shortbow, or (bugged) neither alone. This test instead equips
// dagger (melee, main hand) + pack-bow (ranged, off hand, base damage 3 —
// see content.go's idPackBow) so the three possible outcomes are all
// distinct: dagger-only (4, correct — melee is exclusive at distance 1),
// pack-bow-only (3, the PRE-FIX behavior: resolveEntityTargetedLocked used
// to consult only rangedDefsFor, which skips the melee-tagged dagger
// entirely), or dagger+pack-bow (7, the double-fire bug the spec's
// "weapon-by-distance identity" rule forbids).
func TestMeleeIntentDualWieldExclusiveAtDistance1(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	attackerHex := protocol.Hex{Q: 0, R: 0}

	attackerID, token := w.PlaceEntityForTest(attackerHex)
	w.SetClassForTest(attackerID, "") // clear class defaults: both hands start empty

	daggerID := w.GrantItemForTest(attackerID, "dagger")
	if err := w.SubmitIntent(equipIntent(attackerID, token, daggerID)); err != nil {
		t.Fatalf("SubmitIntent(equip dagger): %v", err)
	}

	packBowID := w.GrantItemForTest(attackerID, "pack-bow")
	if err := w.SubmitIntent(equipIntent(attackerID, token, packBowID)); err != nil {
		t.Fatalf("SubmitIntent(equip pack-bow): %v", err)
	}

	monsterHex := walkableNeighbor(t, w, attackerHex)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	if err := w.SubmitIntent(entityAttackIntent(attackerID, token, monsterID)); err != nil {
		t.Fatalf("SubmitIntent(adjacent melee attack): %v", err)
	}

	snap := step(t, w)

	daggerDamage := game.ItemDamageForTest("dagger")
	packBowDamage := game.ItemDamageForTest("pack-bow")

	if got, want := entityHP(t, snap, monsterID), protocol.MonsterMaxHP-daggerDamage; got != want {
		t.Errorf("monster HP = %d, want %d (dagger-only damage %d — melee is exclusive at distance 1: "+
			"not pack-bow-only damage %d, nor dagger+pack-bow damage %d)",
			got, want, daggerDamage, packBowDamage, daggerDamage+packBowDamage)
	}
}

// TestPlayerMoveOntoMonsterBlocks (#116): a player MOVE intent whose next
// step is hostile-held no longer converts to a swing — the mover waits
// (blocked by movePhaseLocked), nobody takes damage, nobody moves.
// NOTE: submit the move via SetPathForTest, not SubmitIntent — Task 3's
// queueMoveLocked trim would turn an adjacent monster-hex click into an
// empty path; this test pins the RESOLUTION rule for a path that ends
// hostile-held anyway (e.g. the monster stepped into a stale route).
func TestPlayerMoveOntoMonsterBlocks(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	w.SetPathForTest(me.EntityID, []protocol.Hex{monsterHex})
	w.ResolveCombatOnlyForTest()

	snap := w.Snapshot()

	if got, want := entityHP(t, snap, monsterID), protocol.MonsterMaxHP; got != want {
		t.Errorf("monster HP = %d, want %d (a blocked move deals no damage)", got, want)
	}

	if got, want := hexOfSnap(snap, me.EntityID), me.Hex; got != want {
		t.Errorf("player hex = %v, want unchanged %v (blocked, not moved)", got, want)
	}
}

// TestWalkToMonsterHexStopsAdjacent (#116): clicking a distant monster's hex
// is a WALK whose path is trimmed at submit — the player ends on an adjacent
// hex and never swings on its own.
func TestWalkToMonsterHexStopsAdjacent(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	near := walkableNeighbor(t, w, me.Hex)
	monsterHex := walkableNeighbor(t, w, near) // two steps out

	if monsterHex == me.Hex {
		t.Skip("cramped map corner — no two-step line from spawn")
	}

	monsterID := w.PlaceMonsterForTest(monsterHex)

	if !submitOK(w, me, monsterHex) {
		t.Fatalf("SubmitIntent(walk to monster hex) failed")
	}

	var last protocol.TurnEvent
	for range 4 { // more turns than the path needs
		last = step(t, w)
	}

	if got, want := entityHP(t, last, monsterID), protocol.MonsterMaxHP; got != want {
		t.Errorf("monster HP = %d, want %d (a walk never swings)", got, want)
	}

	if got := hexOfSnap(last, me.EntityID); got == monsterHex {
		t.Errorf("player hex = %v — walked ONTO the monster hex; the trim should stop adjacent", got)
	}

	if got, want := game.HexDistance(hexOfSnap(last, me.EntityID), monsterHex), 1; got != want {
		t.Errorf("player distance to monster hex = %d, want %d (stopped adjacent)", got, want)
	}
}
