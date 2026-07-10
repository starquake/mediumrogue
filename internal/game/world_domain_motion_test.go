package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// reachKind mirrors the unexported game-package quest-kind literal (goconst:
// keep the literal out of this file; quest_test.go owns the other uses).
const reachKind = "reach"

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

	// A monster adjacent to alice forms her bubble.
	nearHex := walkableNeighborsN(t, w, alice.Hex, 1)[0]
	w.PlaceMonsterForTest(nearHex)

	// A far monster: use a reach-quest goal — guaranteed walkable and
	// reachable, ≥8 from the origin. Pick the farthest from alice and require
	// it strictly outside her CombatRadius so it starts world-domain.
	var farHex protocol.Hex

	bestDist := 0

	for _, q := range w.Snapshot().Quests {
		if q.Kind != reachKind {
			continue
		}

		if d := game.HexDistance(alice.Hex, q.GoalHex); d > bestDist {
			farHex, bestDist = q.GoalHex, d
		}
	}

	if bestDist <= protocol.CombatRadius {
		t.Fatalf("no reach goal outside CombatRadius of alice (best %d) — pick another placement", bestDist)
	}

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
