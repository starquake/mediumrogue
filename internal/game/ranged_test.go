package game_test

import (
	"errors"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// attackIntent builds a ranged "attack" IntentRequest at target for the given
// identity, so the ranged tests read as one line at the call site.
func attackIntent(id int64, token string, target protocol.Hex) protocol.IntentRequest {
	return protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentAttack, Target: target,
	}
}

// rangedDamage returns a class's level-1 ranged-weapon damage, failing if the
// class has no ranged weapon.
func rangedDamage(t *testing.T, class string) int {
	t.Helper()

	dmg, _, _, ok := game.RangedWeaponForTest(class, 1)
	if !ok {
		t.Fatalf("class %q has no ranged weapon", class)
	}

	return dmg
}

// TestBowIntentDamagesHostileAtRange: a Rogue with a bow submits an attack at a
// monster three hexes away (within the shortbow's range, out of melee); the monster takes
// exactly the level-1 bow damage and the rogue does not move (a shot replaces
// the move). ResolveCombatOnlyForTest runs the combat phases without the monster
// AI, so the monster holds its hex and the shot lands on a fixed target.
func TestBowIntentDamagesHostileAtRange(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	rogueHex := protocol.Hex{Q: 0, R: 0}
	monsterHex := protocol.Hex{Q: 3, R: 0} // distance 3 <= shortbow range (4), not adjacent

	rogueID, token := w.PlaceEntityForTest(rogueHex)
	w.SetClassForTest(rogueID, protocol.ClassRogue)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	if err := w.SubmitIntent(attackIntent(rogueID, token, monsterHex)); err != nil {
		t.Fatalf("SubmitIntent(attack): %v", err)
	}

	w.ResolveCombatOnlyForTest()

	snap := w.Snapshot()

	wantHP := protocol.MonsterMaxHP - rangedDamage(t, protocol.ClassRogue)
	if got := entityHP(t, snap, monsterID); got != wantHP {
		t.Errorf("monster HP = %d, want %d (bow deals its damage at range)", got, wantHP)
	}

	if got, want := hexOfSnap(snap, rogueID), rogueHex; got != want {
		t.Errorf("rogue hex = %v, want %v (a shot does not move the shooter)", got, want)
	}
}

// TestBowIntentOutOfRangeRejected: an attack aimed beyond the shortbow's range is rejected at
// submit with ErrOutOfRange, so no damage is queued.
func TestBowIntentOutOfRangeRejected(t *testing.T) {
	t.Parallel()

	w := newWorld()

	rogueID, token := w.PlaceEntityForTest(protocol.Hex{Q: 0, R: 0})
	w.SetClassForTest(rogueID, protocol.ClassRogue)

	// Distance 5 > shortbow range (4).
	err := w.SubmitIntent(attackIntent(rogueID, token, protocol.Hex{Q: 5, R: 0}))
	if got, want := err, game.ErrOutOfRange; !errors.Is(got, want) {
		t.Errorf("err = %v, want %v", got, want)
	}
}

// TestFighterHasNoRangedWeapon: a Fighter (melee only) submitting an attack
// intent is rejected with ErrNoRangedWeapon.
func TestFighterHasNoRangedWeapon(t *testing.T) {
	t.Parallel()

	w := newWorld()

	// PlaceEntityForTest is a level-1 Fighter by default.
	fighterID, token := w.PlaceEntityForTest(protocol.Hex{Q: 0, R: 0})

	err := w.SubmitIntent(attackIntent(fighterID, token, protocol.Hex{Q: 1, R: 0}))
	if got, want := err, game.ErrNoRangedWeapon; !errors.Is(got, want) {
		t.Errorf("err = %v, want %v", got, want)
	}
}

// TestMageAoEDamagesAllHostiles: a Mage AoE at a target hex hits every hostile
// within the ember-focus's AoE radius — two monsters (one on the hex, one on a neighbour) both
// take the level-1 magic damage.
func TestMageAoEDamagesAllHostiles(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	mageHex := protocol.Hex{Q: 0, R: 0}
	target := protocol.Hex{Q: 3, R: 0}   // distance 3 <= ember-focus range (4)
	neighbor := protocol.Hex{Q: 4, R: 0} // distance 1 from target <= ember-focus AoE radius (1)

	mageID, token := w.PlaceEntityForTest(mageHex)
	w.SetClassForTest(mageID, protocol.ClassMage)
	monsterA := w.PlaceMonsterForTest(target)
	monsterB := w.PlaceMonsterForTest(neighbor)

	if err := w.SubmitIntent(attackIntent(mageID, token, target)); err != nil {
		t.Fatalf("SubmitIntent(attack): %v", err)
	}

	w.ResolveCombatOnlyForTest()

	snap := w.Snapshot()

	wantHP := protocol.MonsterMaxHP - rangedDamage(t, protocol.ClassMage)
	if got := entityHP(t, snap, monsterA); got != wantHP {
		t.Errorf("monster on target HP = %d, want %d", got, wantHP)
	}

	if got := entityHP(t, snap, monsterB); got != wantHP {
		t.Errorf("monster in radius HP = %d, want %d (AoE hits all hostiles)", got, wantHP)
	}
}

// TestMageAoENoFriendlyFire: a Mage AoE whose radius also covers a friendly
// player damages the hostile but leaves the friendly player untouched.
func TestMageAoENoFriendlyFire(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	mageHex := protocol.Hex{Q: 0, R: 0}
	target := protocol.Hex{Q: 3, R: 0}
	friendlyHex := protocol.Hex{Q: 4, R: 0} // distance 1 from target — inside the blast

	mageID, token := w.PlaceEntityForTest(mageHex)
	w.SetClassForTest(mageID, protocol.ClassMage)
	monsterID := w.PlaceMonsterForTest(target)
	friendID, _ := w.PlaceEntityForTest(friendlyHex) // a level-1 Fighter ally

	friendHPBefore := entityHP(t, w.Snapshot(), friendID)

	if err := w.SubmitIntent(attackIntent(mageID, token, target)); err != nil {
		t.Fatalf("SubmitIntent(attack): %v", err)
	}

	w.ResolveCombatOnlyForTest()

	snap := w.Snapshot()

	wantMonsterHP := protocol.MonsterMaxHP - rangedDamage(t, protocol.ClassMage)
	if got := entityHP(t, snap, monsterID); got != wantMonsterHP {
		t.Errorf("monster HP = %d, want %d (hostile takes the AoE)", got, wantMonsterHP)
	}

	if got, want := entityHP(t, snap, friendID), friendHPBefore; got != want {
		t.Errorf("friendly HP = %d, want %d (no friendly fire)", got, want)
	}
}

// TestRangedIntentIsLockIn: inside a bubble, an attack intent counts as the
// player's lock-in — the frozen bubble stays put until the submission, then
// resolves immediately. The rogue's bow lands on the monster and the monster
// bumps back the same turn.
func TestRangedIntentIsLockIn(t *testing.T) {
	t.Parallel()

	w, clk := newTimedWorld(t)
	w.SetCombatPatienceForTest(time.Hour) // never times out during this test

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.SetClassForTest(me.EntityID, protocol.ClassRogue)

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	// Forming poll: world tick forms the bubble (the monster bumps the rogue once).
	clk.advance(time.Second)

	if !w.PollTickForTest() {
		t.Fatalf("world tick did not resolve on the forming poll")
	}

	form := w.Snapshot()
	if !inCombat(t, form, me.EntityID) {
		t.Fatalf("player InCombat = false after forming poll, want true")
	}

	rogueHPBefore := entityHP(t, form, me.EntityID)
	monsterHPBefore := entityHP(t, form, monsterID)

	// The bubble is frozen: a poll without a lock-in must not advance it.
	w.PollTickForTest()

	if got, want := entityHP(t, w.Snapshot(), monsterID), monsterHPBefore; got != want {
		t.Fatalf("monster HP = %d, want unchanged %d (bubble frozen before lock-in)", got, want)
	}

	// Attack intent = lock-in: the sole player readies, so the bubble resolves now.
	if err := w.SubmitIntent(attackIntent(me.EntityID, me.Token, monsterHex)); err != nil {
		t.Fatalf("SubmitIntent(attack): %v", err)
	}

	snap := w.Snapshot()

	wantMonsterHP := monsterHPBefore - rangedDamage(t, protocol.ClassRogue)
	if got := entityHP(t, snap, monsterID); got != wantMonsterHP {
		t.Errorf("monster HP = %d, want %d (attack lock-in runs the bubble turn)", got, wantMonsterHP)
	}

	if got, want := entityHP(t, snap, me.EntityID), rogueHPBefore-game.MonsterDamageForTest("wolf"); got != want {
		t.Errorf("rogue HP = %d, want %d (monster bumps back on the resolved turn)", got, want)
	}
}

// TestRangedAndBumpAccumulateSimultaneously: a bow shot and a monster's bump land
// against pre-attack HP in the same turn. The monster starts on exactly-lethal
// HP for the bow, yet still gets its bump in — proving the two attacks are
// simultaneous, not sequenced (a dead-first monster could not bite back).
func TestRangedAndBumpAccumulateSimultaneously(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	rogueHex := protocol.Hex{Q: 0, R: 0}
	monsterHex := protocol.Hex{Q: 1, R: 0} // adjacent

	rogueID, token := w.PlaceEntityForTest(rogueHex)
	w.SetClassForTest(rogueID, protocol.ClassRogue)
	rogueHPBefore := entityHP(t, w.Snapshot(), rogueID)

	monsterID := w.PlaceMonsterForTest(monsterHex)
	w.SetHPForTest(monsterID, rangedDamage(t, protocol.ClassRogue)) // exactly lethal to the bow
	w.SetPathForTest(monsterID, []protocol.Hex{rogueHex})           // bump the rogue this turn

	if err := w.SubmitIntent(attackIntent(rogueID, token, monsterHex)); err != nil {
		t.Fatalf("SubmitIntent(attack): %v", err)
	}

	w.ResolveCombatOnlyForTest()

	snap := w.Snapshot()

	if _, ok := entityOfSnap(snap, monsterID); ok {
		t.Errorf("monster still alive, want removed (bow was lethal)")
	}

	if got, want := entityHP(t, snap, rogueID), rogueHPBefore-game.MonsterDamageForTest("wolf"); got != want {
		t.Errorf("rogue HP = %d, want %d (monster's bump lands vs pre-attack HP)", got, want)
	}
}
