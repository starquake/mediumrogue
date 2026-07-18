package game_test

// sight_bubble_test.go (#95): line of sight decides who is in a combat
// bubble. Terrain is now an escape tool — duck behind a rock and the fight
// ends — so these drive the real turn loop rather than the geometry.

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// rockWalledPair places a player at origin and a monster two hexes away with
// the hex between them set to rock, and returns the world, the ids, and the
// two hexes. Both are well inside CombatRadius, so distance alone would
// bubble them — only sight keeps them apart.
// sightPair is one player and one monster with a wall between them.
type sightPair struct {
	w                   *game.World
	playerID, monsterID int64
	origin, beyond      protocol.Hex
	between             protocol.Hex
}

func rockWalledPair(t *testing.T) sightPair {
	t.Helper()

	w := newWorld()

	// Four hexes apart, wall at the midpoint: comfortably inside
	// CombatRadius, but far enough that a hunting monster's one step per turn
	// can't reach adjacency — where a wall is CORRECTLY irrelevant, since
	// nothing lies between two neighbours.
	origin := protocol.Hex{Q: 0, R: 0}
	between := protocol.Hex{Q: 2, R: 0}
	beyond := protocol.Hex{Q: 4, R: 0}

	for _, h := range []protocol.Hex{origin, {Q: 1, R: 0}, between, {Q: 3, R: 0}, beyond} {
		w.SetTerrainForTest(h, protocol.TerrainGrass)
	}

	playerID, _ := w.PlaceEntityForTest(origin)
	monsterID := w.PlaceMonsterKindForTest(beyond, "wolf")

	w.SetTerrainForTest(between, protocol.TerrainRock)

	return sightPair{w: w, playerID: playerID, monsterID: monsterID, origin: origin, beyond: beyond, between: between}
}

// TestRockWallKeepsAPairOutOfCombat (#95): two hexes apart — trivially inside
// CombatRadius — but a rock between them, so no bubble forms. Under the old
// distance-only rule this pair was always in combat.
func TestRockWallKeepsAPairOutOfCombat(t *testing.T) {
	t.Parallel()

	p := rockWalledPair(t)

	if got, want := game.HexDistance(p.origin, p.beyond), protocol.CombatRadius; got > want {
		t.Fatalf("test hexes are %d apart, want <= %d (the premise: distance alone would bubble them)", got, want)
	}

	snap := step(t, p.w)

	if got, want := inCombatOf(t, snap, p.playerID), false; got != want {
		t.Errorf("player inCombat = %v, want %v (rock between them)", got, want)
	}

	if got, want := inCombatOf(t, snap, p.monsterID), false; got != want {
		t.Errorf("monster inCombat = %v, want %v (rock between them)", got, want)
	}
}

// TestClearingTheRockFormsTheBubble (#95): the same pair, same hexes — remove
// the wall and they spot each other on the next turn. Pairs with the test
// above so the wall is proven to be the ONLY thing keeping them apart.
func TestClearingTheRockFormsTheBubble(t *testing.T) {
	t.Parallel()

	p := rockWalledPair(t)

	if got, want := inCombatOf(t, step(t, p.w), p.playerID), false; got != want {
		t.Fatalf("player inCombat before clearing = %v, want %v", got, want)
	}

	p.w.SetTerrainForTest(p.between, protocol.TerrainGrass)

	if got, want := inCombatOf(t, step(t, p.w), p.playerID), true; got != want {
		t.Errorf("player inCombat after clearing the rock = %v, want %v", got, want)
	}
}

// TestLosingSightDissolvesTheBubble (#95, the maintainer's Q3): ducking
// behind a rock ENDS a fight already in progress. There is no dissolve code
// path — recomputeBubblesLocked rebuilds components every tick, so an edge
// that stops passing simply doesn't re-form. That emergent behaviour is
// exactly why it deserves a test that names the decision.
func TestLosingSightDissolvesTheBubble(t *testing.T) {
	t.Parallel()

	p := rockWalledPair(t)

	p.w.SetTerrainForTest(p.between, protocol.TerrainGrass)

	if got, want := inCombatOf(t, step(t, p.w), p.playerID), true; got != want {
		t.Fatalf("player inCombat with clear ground = %v, want %v (the premise)", got, want)
	}

	// The monster closed in during that turn; put it back so the wall is
	// provably between them again and the test measures the sight rule rather
	// than one turn of pathfinding.
	p.w.SetHexForTest(p.monsterID, p.beyond)
	p.w.SetTerrainForTest(p.between, protocol.TerrainRock)

	if got, want := inCombatOf(t, step(t, p.w), p.playerID), false; got != want {
		t.Errorf("player inCombat after losing sight = %v, want %v (breaking LOS ends the fight)", got, want)
	}
}

// inCombatOf reads one entity's InCombat flag out of a turn bundle.
func inCombatOf(t *testing.T, snap protocol.TurnEvent, id int64) bool {
	t.Helper()

	e, ok := entityOfSnap(snap, id)
	if !ok {
		t.Fatalf("entity %d missing from snapshot", id)
	}

	return e.InCombat
}
