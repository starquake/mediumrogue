package game_test

import (
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestBubbleTurnFloorGatesSoloSpam: a solo player locked in a bubble cannot
// resolve turns faster than the world's configured turn interval (playtest
// item 5 — no solo action-spam), even though a solo bubble's lock-in would
// otherwise resolve instantly every SubmitIntent call. newTimedWorld's
// interval is 1s (fakeClock-driven), so the floor here is 1s — proving the
// gate scales with the configured interval, not a hardcoded constant.
func TestBubbleTurnFloorGatesSoloSpam(t *testing.T) {
	t.Parallel()

	w, clk := newTimedWorld(t)
	me, _, form := formBubble(t, w, clk)

	ownHex := hexOfSnap(form, me.EntityID)
	waitIntent := protocol.IntentRequest{
		EntityID: me.EntityID, Token: me.Token, Kind: protocol.IntentMove, Target: ownHex,
	}

	// The bubble's first turn has never resolved (lastResolvedAt zero) — the
	// floor does not gate it, so this lock-in resolves immediately.
	if err := w.SubmitIntent(waitIntent); err != nil {
		t.Fatalf("SubmitIntent (first lock-in): %v", err)
	}

	turnAfterFirst := w.Snapshot().Turn

	// Spam more lock-ins WITHOUT advancing the clock: the floor blocks every
	// one of them from resolving again, even though this solo bubble would
	// otherwise have every player ready on each submission.
	for range 5 {
		if err := w.SubmitIntent(waitIntent); err != nil {
			t.Fatalf("SubmitIntent (spam): %v", err)
		}

		if got := w.Snapshot().Turn; got != turnAfterFirst {
			t.Fatalf("Turn = %d after a spammed lock-in with no clock advance, want unchanged %d", got, turnAfterFirst)
		}
	}

	// Advance the clock past the floor (1s, this world's configured
	// interval) and poll: the already-ready bubble now resolves.
	clk.advance(time.Second)

	if !w.PollTickForTest() {
		t.Fatalf("poll did not resolve once the turn floor elapsed")
	}

	if got := w.Snapshot().Turn; got == turnAfterFirst {
		t.Errorf("Turn = %d, want it to have advanced past %d once the floor elapsed", got, turnAfterFirst)
	}
}

// TestBubbleTurnFloorMultiPlayerUnaffectedBeyondFloor: a multi-player bubble
// behaves exactly as before item 5 as long as it is genuinely waiting on a
// straggler's lock-in (the common case — real players rarely both act
// within the same turn interval): the first turn resolves the instant both
// lock in, with no floor delay (never resolved before, so nothing to floor
// against). Only a SECOND round of lock-ins arriving before the floor has
// elapsed is held back — proving the floor gates a genuinely fast
// multi-player bubble the same way it gates a solo one, not just solo play.
func TestBubbleTurnFloorMultiPlayerUnaffectedBeyondFloor(t *testing.T) {
	t.Parallel()

	w, clk := newTimedWorld(t)

	center := protocol.Hex{Q: 0, R: 0}
	ns := game.HexNeighbors(center)

	idA, tokA := w.PlaceEntityForTest(ns[0])
	idB, tokB := w.PlaceEntityForTest(ns[1])
	w.PlaceMonsterForTest(center)

	clk.advance(time.Second)

	if !w.PollTickForTest() {
		t.Fatalf("world tick did not resolve on the forming poll")
	}

	snap := w.Snapshot()
	if got, want := len(snap.Bubbles), 1; got != want {
		t.Fatalf("bubble count after forming poll = %d, want %d", got, want)
	}

	hexA := hexOfSnap(snap, idA)
	hexB := hexOfSnap(snap, idB)

	lockIn := func(id int64, tok string, hex protocol.Hex) {
		t.Helper()

		if err := w.SubmitIntent(protocol.IntentRequest{
			EntityID: id, Token: tok, Kind: protocol.IntentMove, Target: hex,
		}); err != nil {
			t.Fatalf("SubmitIntent %d: %v", id, err)
		}
	}

	// Both lock in: first turn ever for this bubble, so the floor does not
	// gate it — resolves the instant B (the second) locks in.
	lockIn(idA, tokA, hexA)

	turnBeforeB := w.Snapshot().Turn

	lockIn(idB, tokB, hexB)

	turnAfterFirst := w.Snapshot().Turn
	if turnAfterFirst == turnBeforeB {
		t.Fatalf("Turn = %d, want it to have advanced once both players locked in (first turn, unfloored)", turnAfterFirst)
	}

	// A SECOND round of lock-ins, still with no clock advance: the floor now
	// applies (this bubble HAS a previous resolution), so even both players
	// being ready again does not resolve it early.
	lockIn(idA, tokA, hexA)
	lockIn(idB, tokB, hexB)

	if got := w.Snapshot().Turn; got != turnAfterFirst {
		t.Fatalf("Turn = %d after a second round of lock-ins with no clock advance, want unchanged %d",
			got, turnAfterFirst)
	}

	// Advance past the floor and poll: the already-ready bubble resolves.
	clk.advance(time.Second)

	if !w.PollTickForTest() {
		t.Fatalf("poll did not resolve once the turn floor elapsed")
	}

	if got := w.Snapshot().Turn; got == turnAfterFirst {
		t.Errorf("Turn = %d, want it to have advanced past %d once the floor elapsed", got, turnAfterFirst)
	}
}
