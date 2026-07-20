package game_test

// ranged_domain_sight_test.go (#195): a ranged attack must obey the same
// line-of-sight and resolving-domain boundaries a combat bubble does. Before
// the fix, entity-targeted resolution fetched its victim from w.entities by id
// — so a shot could land through a wall (no bubble, target can't fight back)
// or reach a victim frozen in someone else's bubble.

import (
	"errors"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// clearLine sets every hex on the origin→beyond axis (inclusive) to grass so a
// test controls the sight line explicitly instead of depending on generated
// terrain.
func clearLine(w *game.World, hexes ...protocol.Hex) {
	for _, h := range hexes {
		w.SetTerrainForTest(h, protocol.TerrainGrass)
	}
}

// TestEntityTargetedShotThroughRockRejected (#195, scenario a): a rogue names a
// monster four hexes away — exactly the shortbow's range, so range alone
// passes — with a rock strictly between. The shot is rejected at submit with
// ErrNoLineOfSight; clearing the rock (the ONLY blocker) lets the identical
// shot through and land. Before the fix the through-rock submit was accepted
// and the monster took damage every world tick without ever fighting back —
// risk-free loot farming through walls.
func TestEntityTargetedShotThroughRockRejected(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	origin := protocol.Hex{Q: 0, R: 0}
	between := protocol.Hex{Q: 2, R: 0}
	monsterHex := protocol.Hex{Q: 4, R: 0} // distance 4 == shortbow range: range passes, only sight can reject

	clearLine(w, origin, protocol.Hex{Q: 1, R: 0}, between, protocol.Hex{Q: 3, R: 0}, monsterHex)

	rogueID, token := w.PlaceEntityForTest(origin)
	w.SetClassForTest(rogueID, protocol.ClassRogue)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	w.SetTerrainForTest(between, protocol.TerrainRock)

	got := w.SubmitIntent(entityAttackIntent(rogueID, token, monsterID))
	if want := game.ErrNoLineOfSight; !errors.Is(got, want) {
		t.Fatalf("through-rock shot err = %v, want %v", got, want)
	}

	// Control: remove the wall and the very same shot is accepted and lands,
	// proving the rock was the only thing keeping it out (mirrors the #95
	// rock-wall bubble pairing).
	w.SetTerrainForTest(between, protocol.TerrainGrass)

	if err := w.SubmitIntent(entityAttackIntent(rogueID, token, monsterID)); err != nil {
		t.Fatalf("clear-line shot rejected: %v", err)
	}

	w.ResolveCombatOnlyForTest()

	wantHP := protocol.MonsterMaxHP - rangedDamage(t, protocol.ClassRogue)
	if got := entityHP(t, w.Snapshot(), monsterID); got != wantHP {
		t.Errorf("monster HP after clear-line shot = %d, want %d (the shot lands once the rock is gone)", got, wantHP)
	}
}

// TestGroundTargetedShotThroughRockRejected (#195): the legacy hex-targeted
// ranged path is gated the same way — a rock between the shooter and the
// target hex rejects the shot at submit with ErrNoLineOfSight.
func TestGroundTargetedShotThroughRockRejected(t *testing.T) {
	t.Parallel()

	w := newWorld()

	origin := protocol.Hex{Q: 0, R: 0}
	between := protocol.Hex{Q: 2, R: 0}
	targetHex := protocol.Hex{Q: 4, R: 0}

	clearLine(w, origin, protocol.Hex{Q: 1, R: 0}, between, protocol.Hex{Q: 3, R: 0}, targetHex)

	rogueID, token := w.PlaceEntityForTest(origin)
	w.SetClassForTest(rogueID, protocol.ClassRogue)

	w.SetTerrainForTest(between, protocol.TerrainRock)

	got := w.SubmitIntent(attackIntent(rogueID, token, targetHex))
	if want := game.ErrNoLineOfSight; !errors.Is(got, want) {
		t.Fatalf("through-rock ground shot err = %v, want %v", got, want)
	}
}

// TestEntityTargetedShotAcrossDomainsFizzles (#195, scenario b): a shooter at
// world cadence names a monster with a CLEAR line of sight (so the submit-time
// sight gate passes) that is frozen in a DIFFERENT domain — pinned into another
// bubble. The world-domain resolution must not reach across the boundary and
// damage it; before the fix it did, leaving a corpse the other bubble's death
// loop never processes. The pending target is engineered directly (bypassing
// submit, whose reach check is not the property under test) exactly as the
// pre-move/out-of-range resolution tests do.
func TestEntityTargetedShotAcrossDomainsFizzles(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	origin := protocol.Hex{Q: 0, R: 0}
	monsterHex := protocol.Hex{Q: 3, R: 0} // in range, clear line of sight

	clearLine(w, origin, protocol.Hex{Q: 1, R: 0}, protocol.Hex{Q: 2, R: 0}, monsterHex)

	rogueID, _ := w.PlaceEntityForTest(origin) // stays world-domain (bubbleID 0)
	w.SetClassForTest(rogueID, protocol.ClassRogue)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	// Freeze the monster into a foreign bubble, so the world domain that
	// resolves the rogue's shot does not include it.
	const foreignBubbleID = 999
	w.SetBubbleIDForTest(monsterID, foreignBubbleID)
	w.SetAttackTargetEntityForTest(rogueID, monsterID)

	w.ResolveTurnForTest()

	if got, want := entityHP(t, w.Snapshot(), monsterID), protocol.MonsterMaxHP; got != want {
		t.Errorf("cross-domain victim HP = %d, want %d (the shot must not cross the domain boundary)", got, want)
	}
}
