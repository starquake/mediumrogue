package game_test

import (
	"errors"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// entityAttackIntent builds an entity-targeted "attack" IntentRequest (item
// 7, playtest batch 2): TargetEntityID names the victim instead of Target
// naming a hex.
func entityAttackIntent(id int64, token string, targetEntityID int64) protocol.IntentRequest {
	return protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentAttack, TargetEntityID: targetEntityID,
	}
}

// TestEntityTargetedShotHitsSidesteppingTarget (#104, attacks-before-moves):
// the shot resolves against PRE-MOVE positions, so a monster that sidesteps
// this same turn is hit where it stood; the sidestep itself then lands in
// the move phase. (Pre-#104 this test asserted the post-move re-aim; the
// assertions are identical — hit lands, sidestep lands — only the mechanism
// changed.)
func TestEntityTargetedShotHitsSidesteppingTarget(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	rogueHex := protocol.Hex{Q: 0, R: 0}
	monsterHex := protocol.Hex{Q: 3, R: 0} // distance 3 <= shortbow range (4)
	sidestep := protocol.Hex{Q: 3, R: -1}  // distance 3 from rogueHex — still in range

	rogueID, token := w.PlaceEntityForTest(rogueHex)
	w.SetClassForTest(rogueID, protocol.ClassRogue)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	if err := w.SubmitIntent(entityAttackIntent(rogueID, token, monsterID)); err != nil {
		t.Fatalf("SubmitIntent(entity-targeted attack): %v", err)
	}

	w.SetPathForTest(monsterID, []protocol.Hex{sidestep})

	w.ResolveCombatOnlyForTest()

	snap := w.Snapshot()

	if got, want := hexOfSnap(snap, monsterID), sidestep; got != want {
		t.Fatalf("monster hex = %v, want %v (the sidestep itself must land)", got, want)
	}

	wantHP := protocol.MonsterMaxHP - rangedDamage(t, protocol.ClassRogue)
	if got := entityHP(t, snap, monsterID); got != wantHP {
		t.Errorf("monster HP after sidestep = %d, want %d (the shot re-aims and still hits)", got, wantHP)
	}
}

// TestEntityTargetedShotHitsBeforeFlee (#104, attacks-before-moves): a
// monster in range at the attack phase is hit even when its move this same
// turn would have carried it beyond the shooter's range — attacks resolve
// against pre-move positions, so same-tick flight no longer dodges a
// committed shot. (This replaces TestEntityTargetedShotFleeingBeyondRangeFizzles,
// which asserted the pre-#104 post-move re-aim fizzle.)
func TestEntityTargetedShotHitsBeforeFlee(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	rogueHex := protocol.Hex{Q: 0, R: 0}
	monsterHex := protocol.Hex{Q: 4, R: 0} // distance 4 == shortbow range — in range pre-move
	fled := protocol.Hex{Q: 5, R: 0}       // distance 5 — would be out of range post-move

	rogueID, token := w.PlaceEntityForTest(rogueHex)
	w.SetClassForTest(rogueID, protocol.ClassRogue)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	if err := w.SubmitIntent(entityAttackIntent(rogueID, token, monsterID)); err != nil {
		t.Fatalf("SubmitIntent(entity-targeted attack): %v", err)
	}

	w.SetPathForTest(monsterID, []protocol.Hex{fled})

	w.ResolveCombatOnlyForTest()

	snap := w.Snapshot()

	if got, want := hexOfSnap(snap, monsterID), fled; got != want {
		t.Fatalf("monster hex = %v, want %v (the flee itself still lands, after the hit)", got, want)
	}

	wantHP := protocol.MonsterMaxHP - rangedDamage(t, protocol.ClassRogue)
	if got := entityHP(t, snap, monsterID); got != wantHP {
		t.Errorf("monster HP = %d, want %d (the shot resolves pre-move and hits)", got, wantHP)
	}
}

// TestEntityTargetedAttackUnknownEntityRejected: an entity-targeted attack
// naming an id that does not exist is rejected at submit with
// ErrAttackTargetNotFound.
func TestEntityTargetedAttackUnknownEntityRejected(t *testing.T) {
	t.Parallel()

	w := newWorld()

	rogueID, token := w.PlaceEntityForTest(protocol.Hex{Q: 0, R: 0})
	w.SetClassForTest(rogueID, protocol.ClassRogue)

	const unknownID = 999_999_999

	got := w.SubmitIntent(entityAttackIntent(rogueID, token, unknownID))
	if want := game.ErrAttackTargetNotFound; !errors.Is(got, want) {
		t.Errorf("err = %v, want %v", got, want)
	}
}

// TestEntityTargetedAttackFriendlyRejected: an entity-targeted attack naming
// a same-faction (friendly) entity is rejected at submit with
// ErrAttackTargetNotHostile — ranged attacks only ever hit hostiles.
func TestEntityTargetedAttackFriendlyRejected(t *testing.T) {
	t.Parallel()

	w := newWorld()

	rogueID, token := w.PlaceEntityForTest(protocol.Hex{Q: 0, R: 0})
	w.SetClassForTest(rogueID, protocol.ClassRogue)
	friendID, _ := w.PlaceEntityForTest(protocol.Hex{Q: 1, R: 0})

	got := w.SubmitIntent(entityAttackIntent(rogueID, token, friendID))
	if want := game.ErrAttackTargetNotHostile; !errors.Is(got, want) {
		t.Errorf("err = %v, want %v", got, want)
	}
}

// TestEntityTargetedAttackOutOfRangeAtSubmitRejected: an entity-targeted
// attack naming a hostile beyond the weapon's reach at submit time is
// rejected with ErrOutOfRange — the same submit-time gate the hex-targeted
// path has always had.
func TestEntityTargetedAttackOutOfRangeAtSubmitRejected(t *testing.T) {
	t.Parallel()

	w := newWorld()

	rogueID, token := w.PlaceEntityForTest(protocol.Hex{Q: 0, R: 0})
	w.SetClassForTest(rogueID, protocol.ClassRogue)
	monsterID := w.PlaceMonsterForTest(protocol.Hex{Q: 5, R: 0}) // distance 5 > shortbow range (4)

	got := w.SubmitIntent(entityAttackIntent(rogueID, token, monsterID))
	if want := game.ErrOutOfRange; !errors.Is(got, want) {
		t.Errorf("err = %v, want %v", got, want)
	}
}

// TestEntityTargetedFiresEveryReachingHeldWeapon: entity-targeted routing
// (queueAttackLocked) no longer keys off any single "best" (longest-range)
// held weapon's aoeRadius to decide whether targetEntityID applies (task 2,
// dual-wield) — a player wielding an AoE weapon in the MAIN hand (so it would
// have been "best" under the old single-weapon gate) and a bow in the off
// hand still gets a proper entity-targeted shot: both weapons fire at the
// named victim (the bow directly, the ember focus's AoE around the victim's
// hex), not a bogus ground-targeted shot at the zero-value hex the intent
// never set.
func TestEntityTargetedFiresEveryReachingHeldWeapon(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	mageHex := protocol.Hex{Q: 0, R: 0}
	monsterHex := protocol.Hex{Q: 3, R: 0} // distance 3 <= both weapons' range (4)

	mageID, token := w.PlaceEntityForTest(mageHex)
	w.SetClassForTest(mageID, "") // clear defaults: both hands start empty

	// Ember Focus into main (equipped first, main is free), Shortbow into off
	// (main now taken) — the AoE weapon deliberately sits in the hand
	// rangedDefFor's longest-range tie-break would have picked as "best".
	emberFocusID := w.GrantItemForTest(mageID, "ember-focus")
	if err := w.SubmitIntent(equipIntent(mageID, token, emberFocusID)); err != nil {
		t.Fatalf("SubmitIntent(equip ember focus): %v", err)
	}

	shortbowID := w.GrantItemForTest(mageID, "shortbow")
	if err := w.SubmitIntent(equipIntent(mageID, token, shortbowID)); err != nil {
		t.Fatalf("SubmitIntent(equip shortbow): %v", err)
	}

	monsterID := w.PlaceMonsterForTest(monsterHex)

	if err := w.SubmitIntent(entityAttackIntent(mageID, token, monsterID)); err != nil {
		t.Fatalf("SubmitIntent(entity-targeted attack): %v", err)
	}

	w.ResolveCombatOnlyForTest()

	snap := w.Snapshot()

	wantHP := protocol.MonsterMaxHP - game.ItemDamageForTest("shortbow") - game.ItemDamageForTest("ember-focus")
	if got := entityHP(t, snap, monsterID); got != wantHP {
		t.Errorf("monster HP = %d, want %d (bow hit + focus AoE both land on the named victim)", got, wantHP)
	}
}
