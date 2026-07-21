package game_test

import (
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// presenceGrace is the short removal grace the presence tests drive by hand,
// well under any clock step they take.
const presenceGrace = 5 * time.Second

// TestJoinStartsGraceClock: a freshly joined player has no stream yet (streams
// 0) but its removal-grace clock is already running (disconnectedAt == join
// time), so a join that never opens a stream is eventually swept.
func TestJoinStartsGraceClock(t *testing.T) {
	t.Parallel()

	w, clk := newTimedWorld(t)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	if got, want := w.StreamsForTest(me.Token), 0; got != want {
		t.Errorf("streams after Join = %d, want %d", got, want)
	}

	got, ok := w.DisconnectedAtForTest(me.Token)
	if !ok {
		t.Fatalf("DisconnectedAtForTest: no entity for join token")
	}

	if want := clk.now(); !got.Equal(want) {
		t.Errorf("disconnectedAt after Join = %v, want join time %v", got, want)
	}
}

// TestStreamOpenClose: StreamOpened raises the count and keeps the entity out of
// the sweep; StreamClosed drops it to 0 and stamps disconnectedAt to the clock,
// starting the grace afresh.
func TestStreamOpenClose(t *testing.T) {
	t.Parallel()

	w, clk := newTimedWorld(t)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.StreamOpened(me.Token)

	if got, want := w.StreamsForTest(me.Token), 1; got != want {
		t.Errorf("streams after StreamOpened = %d, want %d", got, want)
	}

	clk.advance(3 * time.Second)
	w.StreamClosed(me.Token)

	if got, want := w.StreamsForTest(me.Token), 0; got != want {
		t.Errorf("streams after StreamClosed = %d, want %d", got, want)
	}

	got, ok := w.DisconnectedAtForTest(me.Token)
	if !ok {
		t.Fatalf("DisconnectedAtForTest: no entity for token")
	}

	if want := clk.now(); !got.Equal(want) {
		t.Errorf("disconnectedAt after StreamClosed = %v, want close time %v", got, want)
	}
}

// TestSubmitIntentRefreshesGrace: a player whose only event stream was reaped
// (streams == 0) but who keeps submitting intents refreshes the disconnect
// grace, so an actively-playing client is never swept mid-session (#209).
// Without the refresh the grace clock keeps running from the stream close and
// the still-clicking player is swept out from under them.
func TestSubmitIntentRefreshesGrace(t *testing.T) {
	t.Parallel()

	w, clk := newTimedWorld(t)
	w.SetDisconnectGraceForTest(presenceGrace)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.StreamOpened(me.Token)
	w.StreamClosed(me.Token) // disconnectedAt = t0, grace running

	// Keep playing: within the grace, submit a valid move. Proof of life must
	// push the grace clock forward to the intent time.
	clk.advance(presenceGrace - time.Second)

	target := walkableNeighbor(t, w, me.Hex)

	req := protocol.IntentRequest{Kind: protocol.IntentMove, EntityID: me.EntityID, Token: me.Token, Target: target}
	if err := w.SubmitIntent(req); err != nil {
		t.Fatalf("SubmitIntent: %v", err)
	}

	got, ok := w.DisconnectedAtForTest(me.Token)
	if !ok {
		t.Fatalf("DisconnectedAtForTest: no entity for token")
	}

	if want := clk.now(); !got.Equal(want) {
		t.Errorf("disconnectedAt after intent = %v, want intent time %v (grace not refreshed)", got, want)
	}

	// Past the ORIGINAL grace but within a fresh grace measured from the
	// intent: the still-playing player must survive the sweep.
	clk.advance(2 * time.Second)

	if got, want := w.SweepForTest(clk.now()), false; got != want {
		t.Errorf("SweepForTest removed = %v, want %v (intent refreshed the grace)", got, want)
	}

	if _, ok := entityOfSnap(w.Snapshot(), me.EntityID); !ok {
		t.Errorf("actively-playing player %d swept despite a recent intent", me.EntityID)
	}
}

// TestSweepRemovesPastGrace: a player with no open stream for longer than the
// grace is removed — gone from the snapshot and from byToken.
func TestSweepRemovesPastGrace(t *testing.T) {
	t.Parallel()

	w, clk := newTimedWorld(t)
	w.SetDisconnectGraceForTest(presenceGrace)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.StreamOpened(me.Token)
	w.StreamClosed(me.Token) // disconnectedAt = now

	clk.advance(presenceGrace + time.Second)

	if got, want := w.SweepForTest(clk.now()), true; got != want {
		t.Errorf("SweepForTest removed = %v, want %v", got, want)
	}

	if _, ok := entityOfSnap(w.Snapshot(), me.EntityID); ok {
		t.Errorf("player %d still present after sweep past grace", me.EntityID)
	}

	if got, want := w.StreamsForTest(me.Token), -1; got != want {
		t.Errorf("StreamsForTest after sweep = %d, want %d (gone from byToken)", got, want)
	}
}

// TestSweepKeepsWithinGrace: a player disconnected for less than the grace is
// left in place.
func TestSweepKeepsWithinGrace(t *testing.T) {
	t.Parallel()

	w, clk := newTimedWorld(t)
	w.SetDisconnectGraceForTest(presenceGrace)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.StreamOpened(me.Token)
	w.StreamClosed(me.Token)

	clk.advance(presenceGrace - time.Second)

	if got, want := w.SweepForTest(clk.now()), false; got != want {
		t.Errorf("SweepForTest removed = %v, want %v", got, want)
	}

	if _, ok := entityOfSnap(w.Snapshot(), me.EntityID); !ok {
		t.Errorf("player %d removed within grace, want kept", me.EntityID)
	}
}

// TestReconnectWithinGraceKeeps: a stream that closes and reopens before the
// grace elapses (an EventSource blip) leaves the player with an open stream, so
// a later sweep past the original grace does not remove it.
func TestReconnectWithinGraceKeeps(t *testing.T) {
	t.Parallel()

	w, clk := newTimedWorld(t)
	w.SetDisconnectGraceForTest(presenceGrace)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.StreamOpened(me.Token)
	w.StreamClosed(me.Token) // disconnectedAt = t0

	clk.advance(presenceGrace - time.Second) // within grace
	w.StreamOpened(me.Token)                 // reconnect

	if got, want := w.StreamsForTest(me.Token), 1; got != want {
		t.Errorf("streams after reconnect = %d, want %d", got, want)
	}

	clk.advance(2 * presenceGrace) // well past the original grace

	if got, want := w.SweepForTest(clk.now()), false; got != want {
		t.Errorf("SweepForTest removed = %v, want %v (reconnected)", got, want)
	}

	if _, ok := entityOfSnap(w.Snapshot(), me.EntityID); !ok {
		t.Errorf("reconnected player %d removed, want kept", me.EntityID)
	}
}

// TestTwoStreamsKeptUntilBothClose: two open streams (two tabs); closing one
// leaves streams at 1, so the player is not swept even past the grace.
func TestTwoStreamsKeptUntilBothClose(t *testing.T) {
	t.Parallel()

	w, clk := newTimedWorld(t)
	w.SetDisconnectGraceForTest(presenceGrace)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.StreamOpened(me.Token)
	w.StreamOpened(me.Token)
	w.StreamClosed(me.Token)

	if got, want := w.StreamsForTest(me.Token), 1; got != want {
		t.Errorf("streams after one of two closed = %d, want %d", got, want)
	}

	clk.advance(2 * presenceGrace)

	if got, want := w.SweepForTest(clk.now()), false; got != want {
		t.Errorf("SweepForTest removed = %v, want %v (one stream still open)", got, want)
	}

	if _, ok := entityOfSnap(w.Snapshot(), me.EntityID); !ok {
		t.Errorf("player %d with an open stream removed, want kept", me.EntityID)
	}
}

// TestMonsterNeverSwept: a monster has no token and no presence, so the sweep
// never targets it even far past the grace.
func TestMonsterNeverSwept(t *testing.T) {
	t.Parallel()

	w, clk := newTimedWorld(t)
	w.SetDisconnectGraceForTest(presenceGrace)

	monsterID := w.PlaceMonsterForTest(protocol.Hex{Q: 0, R: 0})

	clk.advance(100 * presenceGrace)

	if got, want := w.SweepForTest(clk.now()), false; got != want {
		t.Errorf("SweepForTest removed = %v, want %v (monster only)", got, want)
	}

	if _, ok := entityOfSnap(w.Snapshot(), monsterID); !ok {
		t.Errorf("monster %d swept, want kept", monsterID)
	}
}

// TestSweepDissolvesBubble: sweeping the sole player of a combat bubble
// recomputes bubbles, so the bubble dissolves and the surviving monster leaves
// combat.
func TestSweepDissolvesBubble(t *testing.T) {
	t.Parallel()

	w, clk := newTimedWorld(t)
	w.SetDisconnectGraceForTest(presenceGrace)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.StreamOpened(me.Token) // connected while fighting

	monsterID := w.PlaceMonsterForTest(walkableNeighbor(t, w, me.Hex))

	// Advance one interval and poll to form the bubble; streams == 1 keeps the
	// player out of the sweep on this pass.
	clk.advance(time.Second)

	if !w.PollTickForTest() {
		t.Fatalf("world tick did not resolve on the forming poll")
	}

	snap := w.Snapshot()
	if got, want := len(snap.Bubbles), 1; got != want {
		t.Fatalf("bubble count after forming = %d, want %d", got, want)
	}

	if !inCombat(t, snap, me.EntityID) {
		t.Fatalf("player InCombat = false after forming, want true")
	}

	// Player disconnects; advance past the grace and poll — the sweep removes the
	// player and recomputes, dissolving the bubble.
	w.StreamClosed(me.Token)
	clk.advance(presenceGrace + time.Second)

	if !w.PollTickForTest() {
		t.Fatalf("poll did not resolve on the sweeping pass")
	}

	snap = w.Snapshot()

	if _, ok := entityOfSnap(snap, me.EntityID); ok {
		t.Errorf("player %d still present after sweep, want removed", me.EntityID)
	}

	if got, want := len(snap.Bubbles), 0; got != want {
		t.Errorf("bubble count after sweeping the sole player = %d, want %d", got, want)
	}

	if inCombat(t, snap, monsterID) {
		t.Errorf("monster %d still InCombat after its bubble dissolved, want false", monsterID)
	}
}
