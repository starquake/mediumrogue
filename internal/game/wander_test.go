package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// wander_test.go (#366): an idle monster drifts near home instead of standing
// still forever.

// TestIdleMonsterWandersNearHome is the feature: with no player anywhere, a
// monster still moves — and never further from home than its leash allows, so
// #102's leash keeps meaning what it meant.
func TestIdleMonsterWandersNearHome(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(7)

	home := protocol.Hex{Q: 0, R: 0}
	id := w.PlaceMonsterForTest(home)
	w.SetMonsterHomeForTest(id, home)

	start := w.EntityHexForTest(id)

	moved := false

	// Wander is one-in-N per turn, so a single tick proves nothing either way.
	for range 40 {
		w.ResolveTurnForTest()

		at := w.EntityHexForTest(id)
		if at != start {
			moved = true
		}

		if got := game.HexDistance(at, home); got > 12 {
			t.Fatalf("wandered to distance %d from home — the leash is not holding", got)
		}
	}

	if !moved {
		t.Error("an idle monster never moved in 40 turns")
	}
}

// TestWanderIsDeterministic pins the property the whole design rests on: the
// wander stream is seeded, so the same world replays identically. If this ever
// fails, wandering has started drawing from something unseeded and the sim is
// no longer reproducible.
func TestWanderIsDeterministic(t *testing.T) {
	t.Parallel()

	track := func() []protocol.Hex {
		w := newWorld()
		w.SetSeedForTest(99)

		home := protocol.Hex{Q: 0, R: 0}
		id := w.PlaceMonsterForTest(home)
		w.SetMonsterHomeForTest(id, home)

		path := make([]protocol.Hex, 0, 25)
		for range 25 {
			w.ResolveTurnForTest()
			path = append(path, w.EntityHexForTest(id))
		}

		return path
	}

	first, second := track(), track()
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("turn %d: %v then %v — wander is not reproducible from the seed", i, first[i], second[i])
		}
	}
}
