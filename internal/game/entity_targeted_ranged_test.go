package game_test

import (
	"bytes"
	"errors"
	"log/slog"
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

// TestEntityTargetedShotFollowsSidestepAndHits: a rogue's entity-targeted bow
// shot re-aims at the victim's POST-MOVE hex — a monster that sidesteps one
// hex (but stays within the shortbow's range from the shooter's own,
// unchanged position) still gets hit, exactly as the retreat-dodge rule
// intends for hex-targeted shots, but now tracking the actual entity.
func TestEntityTargetedShotFollowsSidestepAndHits(t *testing.T) {
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

// TestEntityTargetedShotFleeingBeyondRangeFizzles: a monster that flees
// beyond the shortbow's range this same turn dodges the shot — re-aiming at
// its post-move hex finds it now out of range, so the attack fizzles (no
// damage), logged via item 1's combat event log (reason out_of_range).
func TestEntityTargetedShotFleeingBeyondRangeFizzles(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	var buf bytes.Buffer

	w.SetLogger(slog.New(slog.NewJSONHandler(&buf, nil)))

	rogueHex := protocol.Hex{Q: 0, R: 0}
	monsterHex := protocol.Hex{Q: 4, R: 0} // distance 4 == shortbow range — in range at submit
	fled := protocol.Hex{Q: 5, R: 0}       // distance 5 — out of range after the move

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
		t.Fatalf("monster hex = %v, want %v (the flee itself must land)", got, want)
	}

	if got, want := entityHP(t, snap, monsterID), protocol.MonsterMaxHP; got != want {
		t.Errorf("monster HP = %d, want %d (a fizzled shot deals no damage)", got, want)
	}

	events := slogEvents(t, &buf)

	found := false

	for _, f := range eventsOfKind(events, "fizzle") {
		if f["reason"] == "out_of_range" {
			found = true
		}
	}

	if !found {
		t.Errorf("no fizzle(out_of_range) event logged; events = %v", events)
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
