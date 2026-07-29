package game_test

import (
	"errors"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// evade_escape_test.go (#161): the settled decisions that shipped untested.
//
// #161's plan promised a "live-pipeline test: a player evades out of a bubble
// and the bubble recomputes without them", and it was never written — so the
// bubble interaction the whole ticket exists for went unasserted. Writing it
// turned up that the promised behaviour is not what ships (see
// TestEvadeBuysDistanceButDoesNotClearABubble), which is exactly why the test
// was worth writing.
//
// Decision 4 (player-only) is deliberately NOT tested here: monsterDef has no
// skills field at all, so a monster cannot hold an active in the first place.
// A test would assert a property of a struct that does not exist, and pass
// forever without exercising anything.

// TestEvadeIntoABubbleJoinsTheFight pins decision 5. Bubbles are rebuilt from
// positions every tick, so arriving next to a fight joins it "for free" — the
// kind of claim that holds until someone changes recomputeBubblesLocked and
// nothing notices.
func TestEvadeIntoABubbleJoinsTheFight(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	// A fight: a fighter with a monster adjacent.
	fightHex := protocol.Hex{Q: 0, R: 0}
	monsterHex := protocol.Hex{Q: 1, R: 0}

	fighter, err := w.Join("", "fighter", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.SetHexForTest(fighter.EntityID, fightHex)
	clearLine(w, fightHex, monsterHex)
	w.PlaceMonsterForTest(monsterHex)

	// A evader parked outside that fight's radius (CombatRadius is 6).
	farHex := protocol.Hex{Q: -9, R: 0}

	evader, err := w.Join("", "evader", protocol.ClassRogue, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.SetHexForTest(evader.EntityID, farHex)
	w.SetSkillStateForTest(evader.EntityID, []string{skillSurvivalistID, skillEvadeID}, 0, 1)

	snap := step(t, w)
	if inCombat(t, snap, evader.EntityID) {
		t.Fatal("evader started in combat — it must join by evading, not by standing there")
	}

	// Evade 3 hexes toward the fight: -9 -> -6, which is exactly CombatRadius
	// from the monster at +1... still outside. Step in twice more first so the
	// single evade is what crosses the boundary.
	w.SetHexForTest(evader.EntityID, protocol.Hex{Q: -8, R: 0})

	target := protocol.Hex{Q: -5, R: 0}
	clearLine(w, protocol.Hex{Q: -8, R: 0}, protocol.Hex{Q: -7, R: 0},
		protocol.Hex{Q: -6, R: 0}, target,
		protocol.Hex{Q: -4, R: 0}, protocol.Hex{Q: -3, R: 0},
		protocol.Hex{Q: -2, R: 0}, protocol.Hex{Q: -1, R: 0})

	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: evader.EntityID, Token: evader.Token, Kind: protocol.IntentUseSkill,
		SkillID: skillEvadeID, Target: target,
	}); err != nil {
		t.Fatalf("SubmitIntent use-skill: %v", err)
	}

	snap = step(t, w)

	if got, want := entityHexIn(t, snap, evader.EntityID), target; got != want {
		t.Fatalf("evader at %v, want %v", got, want)
	}

	if !inCombat(t, snap, evader.EntityID) {
		t.Error("evader landed beside a fight and did not join it")
	}
}

// TestEvadeBuysDistanceButDoesNotClearABubble pins what actually ships, which
// is NOT what #161's plan promised.
//
// The plan's task 4 said a player would "evade out of a bubble and the bubble
// recompute without them". It cannot, by arithmetic: CombatRadius is 6 and
// Evade's range is 3, so a evade directly away from an ADJACENT monster ends
// at distance 4 — comfortably still inside the bubble. (The plan's own option
// note, "4+ can clear a bubble in one jump", was wrong for the same reason:
// clearing from adjacent needs range 6.)
//
// That is not a bug. Range 3 was chosen as "a head start you still have to run
// with", and this test pins that reading so nobody later reads the ticket's
// escape framing as a broken promise — or "fixes" it by widening the range.
func TestEvadeBuysDistanceButDoesNotClearABubble(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	origin := protocol.Hex{Q: 0, R: 0}
	monsterHex := protocol.Hex{Q: 1, R: 0}
	away := protocol.Hex{Q: -3, R: 0}
	clearLine(w, origin, monsterHex, protocol.Hex{Q: -1, R: 0}, protocol.Hex{Q: -2, R: 0}, away)

	me, err := w.Join("", "evader", protocol.ClassRogue, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.SetHexForTest(me.EntityID, origin)
	w.SetSkillStateForTest(me.EntityID, []string{skillSurvivalistID, skillEvadeID}, 0, 1)
	w.PlaceMonsterForTest(monsterHex)

	if snap := step(t, w); !inCombat(t, snap, me.EntityID) {
		t.Fatal("no bubble formed — nothing to try to escape")
	}

	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: me.EntityID, Token: me.Token, Kind: protocol.IntentUseSkill,
		SkillID: skillEvadeID, Target: away,
	}); err != nil {
		t.Fatalf("SubmitIntent use-skill: %v", err)
	}

	snap := step(t, w)

	got := entityHexIn(t, snap, me.EntityID)
	if got != away {
		t.Fatalf("evader at %v, want %v", got, away)
	}

	// Distance bought: 1 -> 4.
	if d, want := game.HexDistance(got, monsterHex), 4; d != want {
		t.Errorf("distance after evade = %d, want %d", d, want)
	}

	// ...and 4 is still inside CombatRadius 6, so the fight follows.
	if !inCombat(t, snap, me.EntityID) {
		t.Errorf("evader left the bubble at distance %d — CombatRadius is %d, so this "+
			"should NOT clear it; if the range or radius changed, re-derive this test",
			game.HexDistance(got, monsterHex), protocol.CombatRadius)
	}
}

// TestEvadeDoesNotPassThroughWalls pins decision 2's distinctive half, which is
// deliberately the OPPOSITE of the classic ARPG evade. That inversion is worth
// a test precisely because the genre default is the other way: someone
// "fixing" it to match Path of Exile would be reverting a decision, not
// correcting an oversight. A rock wall stays real cover.
func TestEvadeDoesNotPassThroughWalls(t *testing.T) {
	t.Parallel()

	w := newWorld()

	origin := protocol.Hex{Q: 0, R: 0}
	id, token := w.PlaceEntityForTest(origin)
	w.SetSkillStateForTest(id, []string{skillSurvivalistID, skillEvadeID}, 0, 1)

	// A clear line first, so the ONLY difference below is the wall.
	target := protocol.Hex{Q: 2, R: 0}
	clearLine(w, origin, protocol.Hex{Q: 1, R: 0}, target)

	use := protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
		SkillID: skillEvadeID, Target: target,
	}

	if err := w.SubmitIntent(use); err != nil {
		t.Fatalf("evade over clear ground rejected: %v", err)
	}

	w.SetTerrainForTest(protocol.Hex{Q: 1, R: 0}, protocol.TerrainRock)

	if got, want := w.SubmitIntent(use), game.ErrNoLineOfSight; !errors.Is(got, want) {
		t.Errorf("evade through rock = %v, want %v", got, want)
	}
}
