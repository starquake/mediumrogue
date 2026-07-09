package integration_test

import (
	"bufio"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// entityOf returns the entity with id from bundle, if present.
func entityOf(bundle protocol.TurnEvent, id int64) (protocol.Entity, bool) {
	for _, e := range bundle.Entities {
		if e.ID == id {
			return e, true
		}
	}

	return protocol.Entity{}, false
}

// bubbleWithMember returns the bubble in bundle whose MemberIDs include id, if
// any — mirrors how the client picks "my" bubble out of the shared broadcast.
func bubbleWithMember(bundle protocol.TurnEvent, id int64) (protocol.BubbleView, bool) {
	for _, b := range bundle.Bubbles {
		if slices.Contains(b.MemberIDs, id) {
			return b, true
		}
	}

	return protocol.BubbleView{}, false
}

// decodeBundle reads and unmarshals the next SSE turn frame.
func decodeBundle(t *testing.T, r *bufio.Reader) protocol.TurnEvent {
	t.Helper()

	frames := readFrames(t, r, 1)

	var bundle protocol.TurnEvent
	if err := json.Unmarshal([]byte(frames[0].data), &bundle); err != nil {
		t.Fatalf("unmarshal bundle %q: %v", frames[0].data, err)
	}

	return bundle
}

// TestCombatBubbleFreezesOverHTTP exercises milestone 6.4's headline behavior
// over real HTTP/SSE: a joined player next to the nearest monster (re-targeted
// every bundle, same rationale as TestCombatOverHTTP — monsters hunt back)
// lands inside a combat time bubble (InCombat + a Bubbles entry naming it).
// From that point the player submits NO further intents, yet several more world
// turns resolve — proving the world clock and the bubble's local clock are
// independent: every bubble member's hex must hold exactly (the freeze) while
// the world Turn counter keeps climbing underneath it (the world does not wait).
//
// A short TurnInterval/BubblePoll keeps the suite fast; a CombatPatience far
// longer than the whole observation window keeps the AFK fallback from firing
// mid-assertion and masking the freeze as a timeout resolution instead.
//
// The monster is seeded one hex from the origin (where the player spawns), so
// the bubble forms within a tick or two regardless of the seed — deterministic
// and robust even under a CPU-starved runner (#22). The test is not parallel so
// its tick loop is not starved by sibling servers.
//
//nolint:paralleltest // serial by design (#22): tick loop must not be CPU-starved by parallel siblings.
func TestCombatBubbleFreezesOverHTTP(t *testing.T) {
	const (
		turnInterval   = 20 * time.Millisecond
		bubblePoll     = 5 * time.Millisecond
		combatPatience = 5 * time.Second
	)

	ts := startServerWithBubbleTuningAt(
		t, turnInterval, combatPatience, bubblePoll, protocol.Hex{Q: 1, R: 0},
	)

	me := join(t, ts, "")

	events := get(t, ts, "/api/events")
	reader := bufio.NewReader(events.Body)

	// Phase 1: chase the nearest monster until the player is swept into a
	// combat bubble.
	var (
		bubble       protocol.BubbleView
		freezeBundle protocol.TurnEvent
		entered      bool
	)

	formDeadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(formDeadline) {
		bundle := decodeBundle(t, reader)

		myEntity, ok := entityOf(bundle, me.EntityID)
		if !ok {
			t.Fatal("joined player missing from turn bundle")
		}

		if b, found := bubbleWithMember(bundle, me.EntityID); found {
			bubble, freezeBundle, entered = b, bundle, true

			break
		}

		if target, found := nearestMonster(bundle, myEntity.Hex); found {
			postIntent(t, ts, me, target)
		}
	}

	if !entered {
		t.Fatalf("player never entered a combat bubble before deadline")
	}

	if meEntity, _ := entityOf(freezeBundle, me.EntityID); !meEntity.InCombat {
		t.Fatalf("player InCombat = false in the bundle that formed the bubble, want true")
	}

	// Snapshot every bubble member's hex at the instant of formation.
	frozenHexes := make(map[int64]protocol.Hex, len(bubble.MemberIDs))

	for _, id := range bubble.MemberIDs {
		e, ok := entityOf(freezeBundle, id)
		if !ok {
			t.Fatalf("bubble member %d missing from the forming bundle", id)
		}

		frozenHexes[id] = e.Hex
	}

	freezeTurn := freezeBundle.Turn

	// Phase 2: the headline assertion. No more intents from the player. Read
	// several further bundles and require, on every single one: the world
	// turn strictly ahead of the last (the world clock keeps running), the
	// same bubble still present and unchanged in membership (no spurious
	// re-formation), and every member's hex exactly as frozen (local time did
	// not advance for anyone inside the bubble).
	const bundlesToObserve = 8

	lastTurn := freezeTurn

	for i := range bundlesToObserve {
		bundle := decodeBundle(t, reader)

		if got, want := bundle.Turn, lastTurn; got <= want {
			t.Fatalf("bundle %d: world turn = %d, want > %d (the world clock must keep running)", i, got, want)
		}

		lastTurn = bundle.Turn

		b, found := bubbleWithMember(bundle, me.EntityID)
		if !found {
			t.Fatalf("bundle %d (turn %d): player fell out of every bubble while frozen", i, bundle.Turn)
		}

		if got, want := b.ID, bubble.ID; got != want {
			t.Fatalf("bundle %d: bubble id = %d, want unchanged %d (freeze must not spawn a new bubble)", i, got, want)
		}

		for id, wantHex := range frozenHexes {
			e, ok := entityOf(bundle, id)
			if !ok {
				t.Fatalf("bundle %d: frozen member %d disappeared from the snapshot", i, id)
			}

			if got := e.Hex; got != wantHex {
				t.Fatalf("bundle %d: frozen member %d hex = %v, want unchanged %v (local time must be frozen)",
					i, id, got, wantHex)
			}
		}
	}

	// Phase 3 (best-effort but included): a second player walking straight at
	// the still-frozen bubble's location lands in the SAME bubble — join by
	// proximity, not fixed at formation.
	other := join(t, ts, "")
	if other.EntityID == me.EntityID {
		t.Fatal("second join must yield a distinct entity")
	}

	// One intent is enough: the server pathfinds a route and walks it one hex
	// per turn on its own from here.
	postIntent(t, ts, other, frozenHexes[me.EntityID])

	joinDeadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(joinDeadline) {
		bundle := decodeBundle(t, reader)

		b, found := bubbleWithMember(bundle, me.EntityID)
		if !found {
			continue // the original bubble is allowed to resolve/AFK-timeout by now
		}

		if slices.Contains(b.MemberIDs, other.EntityID) {
			return // the second joiner landed in the first player's bubble
		}
	}

	t.Fatalf("second player never joined the first player's combat bubble before deadline")
}
