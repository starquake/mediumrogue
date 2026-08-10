//nolint:testpackage // white-box: blockedFor and stackCapFor are unexported, and the rule cannot be pinned from outside (see below).
package game

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// stackcap_test.go (#412): the occupancy cap is a property of the MOVER, not of
// the hex — protocol.StackCap while travelling, 1 inside a combat bubble.
//
// White-box on purpose. The observable half of this rule is covered
// behaviourally in bubble_scatter_test.go, but the cap itself cannot be pinned
// that way: scatter runs on every recompute, so a test that let a bubbled
// entity step onto an ally would find them on separate hexes afterwards
// regardless of whether the step was ever refused — it would pass with the cap
// deleted. Testing the predicate directly is the layer that can actually tell
// the two apart.

func stackTestEntity(id int64, kind string, bubbleID int64) *entity {
	return &entity{id: id, kind: kind, bubbleID: bubbleID}
}

// TestStackCapForIsPerMover pins the rule itself: the same entity carries a
// different cap depending only on whether it is in a fight.
func TestStackCapForIsPerMover(t *testing.T) {
	t.Parallel()

	traveller := stackTestEntity(1, protocol.EntityPlayer, 0)
	if got, want := stackCapFor(traveller), protocol.StackCap; got != want {
		t.Errorf("stackCapFor(travelling) = %d, want %d", got, want)
	}

	fighter := stackTestEntity(2, protocol.EntityPlayer, 7)
	if got, want := stackCapFor(fighter), 1; got != want {
		t.Errorf("stackCapFor(in a bubble) = %d, want %d", got, want)
	}
}

// TestBlockedForAppliesTheMoversCap: one occupied hex, two movers, opposite
// answers. The travelling mover may pile on; the fighting one may not.
func TestBlockedForAppliesTheMoversCap(t *testing.T) {
	t.Parallel()

	h := protocol.Hex{Q: 3, R: -1}
	byHex := map[protocol.Hex][]*entity{
		h: {stackTestEntity(10, protocol.EntityPlayer, 7)},
	}

	if got, want := blockedFor(stackTestEntity(1, protocol.EntityPlayer, 0), byHex, h), false; got != want {
		t.Errorf("blockedFor(travelling, 1 occupant) = %v, want %v", got, want)
	}

	if got, want := blockedFor(stackTestEntity(2, protocol.EntityPlayer, 7), byHex, h), true; got != want {
		t.Errorf("blockedFor(in a bubble, 1 occupant) = %v, want %v", got, want)
	}
}

// TestBlockedForStillFillsToStackCapOutOfCombat: the travelling cap is
// unchanged by #412 — four allies still admit a fifth, five still refuse a
// sixth. The anti-deathball lever this rule leaves alone.
func TestBlockedForStillFillsToStackCapOutOfCombat(t *testing.T) {
	t.Parallel()

	h := protocol.Hex{Q: 0, R: 0}
	mover := stackTestEntity(99, protocol.EntityPlayer, 0)

	occs := make([]*entity, 0, protocol.StackCap)

	for i := range protocol.StackCap {
		if got, want := blockedFor(mover, map[protocol.Hex][]*entity{h: occs}, h), false; got != want {
			t.Errorf("with %d occupants: blockedFor = %v, want %v", i, got, want)
		}

		occs = append(occs, stackTestEntity(int64(i), protocol.EntityPlayer, 0))
	}

	if got, want := blockedFor(mover, map[protocol.Hex][]*entity{h: occs}, h), true; got != want {
		t.Errorf("at StackCap: blockedFor = %v, want %v", got, want)
	}
}

// TestBlockedForOpposingIgnoresTheCap: factions never share a hex at any cap,
// in or out of a bubble. #412 narrows the same-faction limit and must not
// loosen this one.
func TestBlockedForOpposingIgnoresTheCap(t *testing.T) {
	t.Parallel()

	h := protocol.Hex{Q: -2, R: 2}
	byHex := map[protocol.Hex][]*entity{
		h: {stackTestEntity(10, protocol.EntityMonster, 0)},
	}

	for _, bubbleID := range []int64{0, 7} {
		mover := stackTestEntity(1, protocol.EntityPlayer, bubbleID)
		if got, want := blockedFor(mover, byHex, h), true; got != want {
			t.Errorf("bubbleID %d: blockedFor(opposing occupant) = %v, want %v", bubbleID, got, want)
		}
	}
}
