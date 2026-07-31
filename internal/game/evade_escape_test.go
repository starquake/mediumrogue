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
//
// Still true after #313 made evade destination-only: this evade crosses OPEN
// ground, where the change alters nothing. What #313 added is a second way
// out — landing behind cover, which breaks the bubble's sight half rather than
// its distance half (TestEvadeBehindAWallClearsABubble). So "distance alone
// never clears a bubble" holds; "an evade escapes only by walking a corner"
// no longer does.
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

// TestEvadePassesThroughWalls pins the REVERSAL decided on 2026-07-31 (#313):
// evade gates on its destination alone, so a rock between you and a legal
// landing hex no longer refuses the jump.
//
// This test previously asserted the opposite — that a wall was real cover,
// deliberately unlike the classic ARPG evade (#322 decision 2). It is inverted
// rather than deleted because the direction is still the thing worth pinning:
// whichever way the rule points, someone changing it should be reverting a
// decision knowingly rather than correcting what looks like an oversight.
//
// The wall is now the ONLY difference between the two submits, and both pass.
func TestEvadePassesThroughWalls(t *testing.T) {
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

	if err := w.SubmitIntent(use); err != nil {
		t.Errorf("evade through rock = %v, want it accepted (#313: destination-only)", err)
	}

	// The DESTINATION still gates: rock underfoot is not walkable, so landing
	// ON the wall stays refused. Losing this would make evade a way to stand
	// inside terrain, which is not what was decided.
	onTheWall := use
	onTheWall.Target = protocol.Hex{Q: 1, R: 0}

	if got, want := w.SubmitIntent(onTheWall), game.ErrNotWalkable; !errors.Is(got, want) {
		t.Errorf("evade onto rock = %v, want %v", got, want)
	}
}

// TestEvadeBehindAWallClearsABubble pins the consequence of #313's
// destination-only rule, and it is a BALANCE change rather than a rules tidy-up
// — so it is asserted directly rather than left implied.
//
// Before, the pair of decisions in TestEvadeBuysDistanceButDoesNotClearABubble
// and the old TestEvadeDoesNotPassThroughWalls meant a bubble could not be
// cleared in one jump: range 3 against CombatRadius 6 never bought enough
// distance, and you could not hop behind cover because you could not evade to
// a hex you could not see. Breaking contact required WALKING a corner.
//
// Now the wall hop is legal, and a bubble is sight-gated as well as
// distance-gated (#95), so landing behind rock dissolves the fight at
// distance 4 — well inside CombatRadius. Evade is a dependable disengage where
// cover exists. That was put to the maintainer with this consequence spelled
// out and chosen deliberately; see design-decisions.md.
func TestEvadeBehindAWallClearsABubble(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	origin := protocol.Hex{Q: 0, R: 0}
	monsterHex := protocol.Hex{Q: 1, R: 0}
	wall := protocol.Hex{Q: -2, R: 0}
	behind := protocol.Hex{Q: -3, R: 0}

	clearLine(w, origin, monsterHex, protocol.Hex{Q: -1, R: 0}, wall, behind)

	me, err := w.Join("", "evader", protocol.ClassRogue, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.SetHexForTest(me.EntityID, origin)
	w.SetSkillStateForTest(me.EntityID, []string{skillSurvivalistID, skillEvadeID}, 0, 1)
	w.PlaceMonsterForTest(monsterHex)

	if snap := step(t, w); !inCombat(t, snap, me.EntityID) {
		t.Fatal("no bubble formed — nothing to escape")
	}

	// The wall goes up only now, so the bubble above formed over clear ground
	// and the ONLY thing that changes is the cover.
	w.SetTerrainForTest(wall, protocol.TerrainRock)

	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: me.EntityID, Token: me.Token, Kind: protocol.IntentUseSkill,
		SkillID: skillEvadeID, Target: behind,
	}); err != nil {
		t.Fatalf("evade past a wall rejected: %v", err)
	}

	snap := step(t, w)

	if got := entityHexIn(t, snap, me.EntityID); got != behind {
		t.Fatalf("evader at %v, want %v", got, behind)
	}

	// Distance alone would NOT have done it: 4 is inside CombatRadius 6, the
	// same arithmetic that keeps the open-ground evade in its bubble. The rock
	// is what breaks contact.
	if d, want := game.HexDistance(behind, monsterHex), 4; d != want {
		t.Fatalf("distance after evade = %d, want %d (the test geometry moved)", d, want)
	}

	if inCombat(t, snap, me.EntityID) {
		t.Error("evader landed behind a rock wall and the bubble followed")
	}
}
