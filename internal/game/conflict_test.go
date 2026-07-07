package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestFriendlyStackingConverges: two entities on neighbouring hexes both step
// onto one shared hex in a single turn and stack — friendly stacking under the
// StackCap, resolved simultaneously.
func TestFriendlyStackingConverges(t *testing.T) {
	t.Parallel()

	w := newWorld()

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	ns := game.HexNeighbors(center)

	idA, tokA := w.PlaceEntityForTest(ns[0])
	idB, tokB := w.PlaceEntityForTest(ns[1])

	mustSubmit(t, w, idA, tokA, center)
	mustSubmit(t, w, idB, tokB, center)

	w.ResolveTurnForTest()

	snap := w.Snapshot()
	if got := hexOfSnap(snap, idA); got != center {
		t.Errorf("A at %v, want center %v", got, center)
	}

	if got := hexOfSnap(snap, idB); got != center {
		t.Errorf("B at %v, want center %v", got, center)
	}

	if n := countAt(snap, center); n != 2 {
		t.Errorf("center occupancy = %d, want 2", n)
	}
}

// TestStackCapBlocksOverflow: a hex already full at StackCap does not admit one
// more mover — the overflow entity stays put and the hex still holds exactly
// StackCap. Asserts the invariant, not which entity won a tie-break.
func TestStackCapBlocksOverflow(t *testing.T) {
	t.Parallel()

	w := newWorld()

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	ns := game.HexNeighbors(center)

	for range protocol.StackCap {
		w.PlaceEntityForTest(center)
	}

	sixth, tok := w.PlaceEntityForTest(ns[0])
	mustSubmit(t, w, sixth, tok, center)

	w.ResolveTurnForTest()

	snap := w.Snapshot()
	if got := hexOfSnap(snap, sixth); got == center {
		t.Errorf("overflow entity entered a full hex; want it blocked at %v", ns[0])
	}

	if n := countAt(snap, center); n != protocol.StackCap {
		t.Errorf("center occupancy = %d, want StackCap %d", n, protocol.StackCap)
	}
}

func mustSubmit(t *testing.T, w *game.World, id int64, token string, target protocol.Hex) {
	t.Helper()

	if err := w.SubmitIntent(protocol.IntentRequest{EntityID: id, Token: token, Target: target}); err != nil {
		t.Fatalf("SubmitIntent(%d -> %v): %v", id, target, err)
	}
}

func hexOfSnap(snap protocol.TurnEvent, id int64) protocol.Hex {
	for _, e := range snap.Entities {
		if e.ID == id {
			return e.Hex
		}
	}

	return protocol.Hex{Q: -999, R: -999}
}

func countAt(snap protocol.TurnEvent, hex protocol.Hex) int {
	n := 0

	for _, e := range snap.Entities {
		if e.Hex == hex {
			n++
		}
	}

	return n
}
