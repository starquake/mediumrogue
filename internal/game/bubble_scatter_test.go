package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// bubble_scatter_test.go (#412): overlap is fine while travelling and banned
// inside a combat bubble.
//
// The pair of tests that matter are the first two, and they must be read
// together: the SAME stack of players on the SAME hex separates or does not,
// decided by nothing but whether a monster is standing nearby. That is the
// whole mechanic.

// partySize is the blob these tests build: protocol.StackCap players, the
// largest stack the travelling rule allows, so the scatter is exercised at its
// widest.
const partySize = protocol.StackCap

// stackAll puts every id on hex h, so a test can build the blob the mechanic
// is about without relying on movement to produce it.
func stackAll(t *testing.T, w *game.World, h protocol.Hex, ids ...int64) {
	t.Helper()

	for _, id := range ids {
		w.SetHexForTest(id, h)
	}
}

// distinctHexes returns how many different hexes the given entities occupy.
func distinctHexes(t *testing.T, w *game.World, ids ...int64) int {
	t.Helper()

	seen := make(map[protocol.Hex]bool, len(ids))
	for _, id := range ids {
		seen[w.EntityHexForTest(id)] = true
	}

	return len(seen)
}

// TestBubbleScattersStackedPlayers: five players standing on one hex when a
// fight starts end up on five hexes. The headline behaviour.
func TestBubbleScattersStackedPlayers(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	ids := make([]int64, 0, partySize)
	ids = append(ids, me.EntityID)

	for range partySize - 1 {
		id, _ := w.PlaceEntityForTest(me.Hex)
		ids = append(ids, id)
	}

	stackAll(t, w, me.Hex, ids...)

	if got, want := distinctHexes(t, w, ids...), 1; got != want {
		t.Fatalf("before the fight: distinct hexes = %d, want %d (the test did not build a stack)", got, want)
	}

	w.PlaceMonsterForTest(walkableNeighbor(t, w, me.Hex))
	step(t, w)

	if got, want := distinctHexes(t, w, ids...), len(ids); got != want {
		t.Errorf("in a bubble: distinct hexes = %d, want %d", got, want)
	}
}

// TestTravellingPlayersStillStack is the other half of the same rule, and the
// reason the cap is a function of the mover rather than a constant: with no
// monster in sight the identical stack is left alone, because travel is where
// the blob earns its keep.
func TestTravellingPlayersStillStack(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	ids := make([]int64, 0, partySize)
	ids = append(ids, me.EntityID)

	for range partySize - 1 {
		id, _ := w.PlaceEntityForTest(me.Hex)
		ids = append(ids, id)
	}

	stackAll(t, w, me.Hex, ids...)
	step(t, w) // no monster: no bubble forms

	if got, want := distinctHexes(t, w, ids...), 1; got != want {
		t.Errorf("out of combat: distinct hexes = %d, want %d (the stack was broken up while travelling)", got, want)
	}
}

// TestScatterIsDeterministic: two worlds built and driven identically scatter
// to identical hexes. Scatter moves entities, so a scatter that varied run to
// run would desync replays and shift every pinned seed downstream of it —
// which is why the placement walks a fixed direction order and draws no
// randomness at all.
func TestScatterIsDeterministic(t *testing.T) {
	t.Parallel()

	run := func() []protocol.Hex {
		w := newWorld()
		w.SetSeedForTest(1)

		me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
		if err != nil {
			t.Fatalf("Join: %v", err)
		}

		ids := make([]int64, 0, partySize)
		ids = append(ids, me.EntityID)

		for range partySize - 1 {
			id, _ := w.PlaceEntityForTest(me.Hex)
			ids = append(ids, id)
		}

		stackAll(t, w, me.Hex, ids...)
		w.PlaceMonsterForTest(walkableNeighbor(t, w, me.Hex))
		step(t, w)

		out := make([]protocol.Hex, 0, len(ids))
		for _, id := range ids {
			out = append(out, w.EntityHexForTest(id))
		}

		return out
	}

	first, second := run(), run()

	for i := range first {
		if got, want := first[i], second[i]; got != want {
			t.Errorf("member %d scattered to %v on the first run and %v on the second", i, got, want)
		}
	}
}

// TestScatterToleratesNowhereToGo: walled in, the stack stays stacked rather
// than erroring or leaving the bubble unformable. The decided answer to "what
// if there is no room" (@starquake, 2026-08-10) — a temporary blob is a far
// better failure than a fight that cannot start, and it is self-correcting,
// since the next recompute tries again from a board that has moved.
func TestScatterToleratesNowhereToGo(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	// Wall off everything within CombatRadius except the players' own hex and
	// the one the monster needs to stand on, so no tier can find a home.
	monsterHex := walkableNeighbor(t, w, me.Hex)

	for _, tile := range w.Map().Tiles {
		if tile.Hex == me.Hex || tile.Hex == monsterHex {
			continue
		}

		if game.HexDistance(me.Hex, tile.Hex) <= protocol.CombatRadius {
			w.SetTerrainForTest(tile.Hex, protocol.TerrainWater)
		}
	}

	friendID, _ := w.PlaceEntityForTest(me.Hex)
	stackAll(t, w, me.Hex, me.EntityID, friendID)
	w.PlaceMonsterForTest(monsterHex)

	step(t, w)

	if got, want := distinctHexes(t, w, me.EntityID, friendID), 1; got != want {
		t.Errorf("distinct hexes = %d, want %d (a walled-in stack must be tolerated, not forced apart)", got, want)
	}
}

// TestWalkInIsAdmittedThenScattered: reinforcement arriving at an ongoing
// fight is never refused at the door. The mover is still in the world domain
// when it steps, so its cap is the travelling one and the step is legal; the
// recompute that follows separates it. Refusing the step instead would have
// silently broken walk-in reinforcement, which game-identity.md names as an
// intended core experience.
func TestWalkInIsAdmittedThenScattered(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.PlaceMonsterForTest(walkableNeighbor(t, w, me.Hex))
	step(t, w) // the fight is under way, with me in it

	// The latecomer arrives on the defender's own hex — the arrival the
	// in-bubble cap would refuse if it keyed on the hex instead of the mover.
	lateID, _ := w.PlaceEntityForTest(me.Hex)
	w.SetHexForTest(lateID, w.EntityHexForTest(me.EntityID))

	step(t, w)

	if got, want := distinctHexes(t, w, me.EntityID, lateID), 2; got != want {
		t.Errorf("distinct hexes = %d, want %d (the walk-in was not separated)", got, want)
	}
}
