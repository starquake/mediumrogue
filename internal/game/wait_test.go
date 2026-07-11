package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestOwnHexMoveIsWaitAndLocksInBubble: a move intent targeting my own hex
// (the client's SPACE-key wait, item 11) is an ordinary move intent —
// Pathfind(from == to) yields an empty path, so it's a no-op walk — and
// still counts as this bubble-turn's lock-in exactly like any other intent.
// Two players both "waiting" this way resolves the bubble turn without
// either of them moving.
func TestOwnHexMoveIsWaitAndLocksInBubble(t *testing.T) {
	t.Parallel()

	w := newWorld()
	idA, tokA, idB, tokB, _, form := twoPlayerBubble(t, w)

	hexA0 := hexOfSnap(form, idA)
	hexB0 := hexOfSnap(form, idB)

	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: idA, Token: tokA, Kind: protocol.IntentMove, Target: hexA0,
	}); err != nil {
		t.Fatalf("SubmitIntent A (wait): %v", err)
	}

	waiting := w.Snapshot().Bubbles[0].WaitingForIDs
	if len(waiting) != 1 || waiting[0] != idB {
		t.Fatalf("WaitingForIDs = %v, want only %d (an own-hex move must count as A's lock-in)", waiting, idB)
	}

	turnBeforeB := w.Snapshot().Turn

	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: idB, Token: tokB, Kind: protocol.IntentMove, Target: hexB0,
	}); err != nil {
		t.Fatalf("SubmitIntent B (wait): %v", err)
	}

	resolved := w.Snapshot()

	if got := resolved.Turn; got == turnBeforeB {
		t.Fatalf("Turn = %d, want it to have advanced once both players \"waited\"", got)
	}

	if got, want := hexOfSnap(resolved, idA), hexA0; got != want {
		t.Errorf("A's hex = %v, want unchanged %v (a wait never moves)", got, want)
	}

	if got, want := hexOfSnap(resolved, idB), hexB0; got != want {
		t.Errorf("B's hex = %v, want unchanged %v (a wait never moves)", got, want)
	}
}
