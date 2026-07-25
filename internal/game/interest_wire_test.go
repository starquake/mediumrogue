package game_test

// interest_wire_test.go (#289): what a turn bundle contains once it is culled
// to the viewer's interest radius. Black-box on purpose — this is the contract
// a client receives, and "the client never learns about a monster it cannot
// see" is only true if the server never sends it.

import (
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// interestTestRadius is comfortably wider than protocol.InterestRadius so a
// test can place entities on BOTH sides of the boundary. newWorld's radius 12
// cannot — everything in it is inside the interest radius.
const interestTestRadius = 40

func newWideWorld() *game.World {
	return game.NewWorld(game.WorldConfig{
		Interval:        time.Hour,
		CombatPatience:  testCombatPatience,
		BubblePoll:      testBubblePoll,
		DisconnectGrace: testDisconnectGrace,
		WorldSeed:       0xC0FFEE,
		Radius:          interestTestRadius,
		Ticks:           hub.New(),
	})
}

// eastOf returns the hex exactly d hexes from the origin along +q, so a test
// can name a distance directly.
func eastOf(d int) protocol.Hex { return protocol.Hex{Q: d, R: 0} }

func hasEntity(snap protocol.TurnEvent, id int64) bool {
	for _, e := range snap.Entities {
		if e.ID == id {
			return true
		}
	}

	return false
}

func hasGroundItem(snap protocol.TurnEvent, id int64) bool {
	for _, g := range snap.GroundItems {
		if g.ID == id {
			return true
		}
	}

	return false
}

// TestSnapshotForCullsEntitiesBeyondInterestRadius is the headline contract,
// asserted at the boundary itself: exactly at the radius an entity is still
// sent, one hex further it is gone. Testing the boundary rather than "near"
// and "far" is what pins the comparison as inclusive (<=), so an off-by-one
// cannot pass.
func TestSnapshotForCullsEntitiesBeyondInterestRadius(t *testing.T) {
	t.Parallel()

	w := newWideWorld()

	_, token := w.PlaceEntityForTest(protocol.Hex{})

	atEdge := w.PlaceMonsterForTest(eastOf(protocol.InterestRadius))
	justOutside := w.PlaceMonsterForTest(eastOf(protocol.InterestRadius + 1))

	snap := w.SnapshotFor(token)

	if got, want := hasEntity(snap, atEdge), true; got != want {
		t.Errorf("monster at exactly %d hexes present = %v, want %v (the radius is inclusive)",
			protocol.InterestRadius, got, want)
	}

	if got, want := hasEntity(snap, justOutside), false; got != want {
		t.Errorf("monster at %d hexes present = %v, want %v (beyond the radius)",
			protocol.InterestRadius+1, got, want)
	}
}

// TestSnapshotForAlwaysKeepsTheViewer: the viewer's own row is unconditional.
// It is trivially inside its own radius today, but the client cannot render
// anything without `mine`, so this pins it against a future change of centre.
func TestSnapshotForAlwaysKeepsTheViewer(t *testing.T) {
	t.Parallel()

	w := newWideWorld()

	far := eastOf(interestTestRadius - 2)

	id, token := w.PlaceEntityForTest(far)

	if got, want := hasEntity(w.SnapshotFor(token), id), true; got != want {
		t.Errorf("viewer's own entity present in its own bundle = %v, want %v (at %v)", got, want, far)
	}
}

// TestSnapshotForCullsGroundItems: loot follows the same rule as entities —
// you should not learn what is lying on the floor 30 hexes away.
func TestSnapshotForCullsGroundItems(t *testing.T) {
	t.Parallel()

	w := newWideWorld()

	_, token := w.PlaceEntityForTest(protocol.Hex{})

	near := w.GroundItemForTest(eastOf(protocol.InterestRadius-1), "leather-armor")
	far := w.GroundItemForTest(eastOf(protocol.InterestRadius+1), "leather-armor")

	snap := w.SnapshotFor(token)

	if got, want := hasGroundItem(snap, near), true; got != want {
		t.Errorf("ground item inside the radius present = %v, want %v", got, want)
	}

	if got, want := hasGroundItem(snap, far), false; got != want {
		t.Errorf("ground item beyond the radius present = %v, want %v", got, want)
	}
}

// TestSnapshotForWatcherCullsAroundOrigin: a token-less watcher (the
// viewer-less bundle handleEvents allows) has no position to cull around, so
// it is culled around the world origin. The alternative — sending it
// everything — would quietly reinstate the full-world bundle this slice
// exists to remove, on a path nothing authenticates.
func TestSnapshotForWatcherCullsAroundOrigin(t *testing.T) {
	t.Parallel()

	w := newWideWorld()

	near := w.PlaceMonsterForTest(eastOf(protocol.InterestRadius - 1))
	far := w.PlaceMonsterForTest(eastOf(protocol.InterestRadius + 1))

	snap := w.SnapshotFor("")

	if got, want := hasEntity(snap, near), true; got != want {
		t.Errorf("watcher bundle: monster near origin present = %v, want %v", got, want)
	}

	if got, want := hasEntity(snap, far), false; got != want {
		t.Errorf("watcher bundle: monster beyond the radius present = %v, want %v", got, want)
	}
}
