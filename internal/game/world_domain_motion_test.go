package game_test

import (
	"slices"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// walkableHexAtDistance scans the world's origin-reachable walkable region
// (the same set spawnHexLocked and Pathfind can actually reach) for a hex
// whose distance from `from` falls in [minDist, maxDist], returning the
// lowest by (Q, R) among matches for a fully deterministic, reproducible pick
// (the region is a map — iterating it directly would return an arbitrary
// match, varying run to run for the same fixed-seed world).
func walkableHexAtDistance(t *testing.T, w *game.World, from protocol.Hex, minDist, maxDist int) protocol.Hex {
	t.Helper()

	var candidates []protocol.Hex

	for h := range game.ReachableWalkableForTest(w.Map()) {
		if d := game.HexDistance(from, h); d >= minDist && d <= maxDist {
			candidates = append(candidates, h)
		}
	}

	if len(candidates) == 0 {
		t.Fatalf("no reachable walkable hex within distance [%d,%d] of %v", minDist, maxDist, from)
	}

	slices.SortFunc(candidates, func(a, b protocol.Hex) int {
		if a.Q != b.Q {
			return a.Q - b.Q
		}

		return a.R - b.R
	})

	return candidates[0]
}

// entityHex scans the snapshot for an entity's current hex.
func entityHex(t *testing.T, w *game.World, id int64) protocol.Hex {
	t.Helper()

	for _, e := range w.Snapshot().Entities {
		if e.ID == id {
			return e.Hex
		}
	}

	t.Fatalf("entity %d not in snapshot", id)

	return protocol.Hex{}
}

// TestWorldMonstersKeepMovingWhileAllPlayersAreBubbled: the design's headline
// promise (§5, "the rest of the world keeps running") — when the only player
// is frozen inside a combat bubble, monsters OUTSIDE the bubble must keep
// hunting in world time (approaching until the bubble recompute absorbs them
// as walk-in reinforcements), not stand still because their domain currently
// holds no player. Regression for the live report "any movement outside the
// combat bubble stops".
func TestWorldMonstersKeepMovingWhileAllPlayersAreBubbled(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)
	alice := joinNamed(t, w, "alice")

	// A scattered spawn near the map edge can force the far monster's
	// approach to detour around generated terrain instead of closing
	// distance in a single step below.
	pinToOrigin(w, &alice)

	// A monster adjacent to alice forms her bubble.
	nearHex := walkableNeighborsN(t, w, alice.Hex, 1)[0]
	w.PlaceMonsterForTest(nearHex)

	// A far monster: within MonsterAggroRadius (#36 — a WORLD monster only
	// hunts a player within ITS aggro range; the whole point of this test is
	// that the world keeps running, which now requires the monster to have
	// noticed alice in the first place) but with enough of a buffer over
	// CombatRadius (+2, not +1) that a single approach step during the
	// forming turn below can't accidentally close it into alice's bubble
	// before the "starts OUTSIDE the bubble" precondition is even checked.
	farHex := walkableHexAtDistance(t, w, alice.Hex, protocol.CombatRadius+2, protocol.MonsterAggroRadius)
	farID := w.PlaceMonsterForTest(farHex)

	// Forming turn: alice + the near monster bubble up; the far monster stays
	// in the world domain.
	step(t, w)

	snap := w.Snapshot()
	for _, e := range snap.Entities {
		if e.ID == alice.EntityID && !e.InCombat {
			t.Fatal("precondition: alice should be frozen in a combat bubble")
		}

		if e.ID == farID && e.InCombat {
			t.Fatal("precondition: the far monster must start OUTSIDE the bubble")
		}
	}

	// World turns keep resolving while alice's bubble waits on her intent —
	// the far monster must keep approaching, not freeze.
	before := entityHex(t, w, farID)
	beforeDist := game.HexDistance(before, alice.Hex)

	step(t, w)

	after := entityHex(t, w, farID)
	afterDist := game.HexDistance(after, alice.Hex)

	if after == before {
		t.Fatalf("far monster froze at %v while all players were bubbled — the world must keep running", before)
	}

	if afterDist >= beforeDist {
		t.Errorf("far monster distance to alice went %d -> %d, want it to close in", beforeDist, afterDist)
	}
}
