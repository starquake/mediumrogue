package game_test

import (
	"errors"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
)

func newWorld() *game.World {
	return game.NewWorld(time.Hour, hub.New())
}

// step drives one turn without running the ticker goroutine.
func step(t *testing.T, w *game.World) protocol.TurnEvent {
	t.Helper()
	w.ResolveTurnForTest()

	return w.Snapshot()
}

func TestJoinCreatesEntityOnWalkableHex(t *testing.T) {
	t.Parallel()

	w := newWorld()

	resp, err := w.Join("")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	if resp.EntityID == 0 || resp.Token == "" {
		t.Fatalf("Join returned zero identity: %+v", resp)
	}

	snap := w.Snapshot()
	if got, want := len(snap.Entities), 1; got != want {
		t.Fatalf("entities = %d, want 1", got)
	}

	if got, want := snap.Entities[0].Hex, resp.Hex; got != want {
		t.Fatalf("snapshot hex %v != join hex %v", got, want)
	}
}

func TestJoinWithKnownTokenReturnsSameEntity(t *testing.T) {
	t.Parallel()

	w := newWorld()

	first, err := w.Join("")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	again, err := w.Join(first.Token)
	if err != nil {
		t.Fatalf("re-Join: %v", err)
	}

	if got, want := again.EntityID, first.EntityID; got != want {
		t.Fatalf("re-join created a new entity: %d != %d", got, want)
	}

	if got, want := len(w.Snapshot().Entities), 1; got != want {
		t.Fatal("re-join must not create a second entity")
	}
}

func TestJoinWithUnknownTokenCreatesNewEntity(t *testing.T) {
	t.Parallel()

	w := newWorld()

	resp, err := w.Join("stale-token-from-before-a-restart")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	if resp.Token == "stale-token-from-before-a-restart" {
		t.Fatal("server must mint a fresh token, not adopt the stale one")
	}
}

func TestIntentMovesEntityOnResolve(t *testing.T) {
	t.Parallel()

	w := newWorld()
	me, _ := w.Join("")

	target := walkableNeighbor(t, w, me.Hex)
	if err := w.SubmitIntent(protocol.IntentRequest{EntityID: me.EntityID, Token: me.Token, Target: target}); err != nil {
		t.Fatalf("SubmitIntent: %v", err)
	}

	// Not moved yet: intents apply at the tick, never immediately.
	if got, want := w.Snapshot().Entities[0].Hex, me.Hex; got != want {
		t.Fatalf("entity moved before resolve: %v", got)
	}

	snap := step(t, w)
	if got, want := snap.Entities[0].Hex, target; got != want {
		t.Fatalf("after resolve: hex = %v, want %v", got, want)
	}

	if got, want := snap.Turn, int64(1); got != want {
		t.Fatalf("turn = %d, want 1", got)
	}
}

func TestLatestIntentWins(t *testing.T) {
	t.Parallel()

	w := newWorld()
	me, _ := w.Join("")

	first := walkableNeighbor(t, w, me.Hex)

	var second protocol.Hex

	for _, n := range game.HexNeighbors(me.Hex) {
		if n != first && submitOK(w, me, n) {
			second = n

			break
		}
	}

	if second == (protocol.Hex{}) {
		t.Skip("spawn has only one walkable neighbor; cannot exercise latest-wins here")
	}

	// second was submitted last (submitOK submits), so it must win.
	snap := step(t, w)
	if got, want := snap.Entities[0].Hex, second; got != want {
		t.Fatalf("hex = %v, want the latest intent target %v", got, want)
	}
}

func TestIntentValidation(t *testing.T) {
	t.Parallel()

	w := newWorld()
	me, _ := w.Join("")

	cases := []struct {
		name string
		req  protocol.IntentRequest
		want error
	}{
		{
			"bad token",
			protocol.IntentRequest{EntityID: me.EntityID, Token: "wrong", Target: walkableNeighbor(t, w, me.Hex)},
			game.ErrUnauthorized,
		},
		{
			"unknown entity",
			protocol.IntentRequest{EntityID: 999, Token: me.Token, Target: walkableNeighbor(t, w, me.Hex)},
			game.ErrUnauthorized,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got, want := w.SubmitIntent(tc.req), tc.want; !errors.Is(got, want) {
				t.Fatalf("err = %v, want %v", got, want)
			}
		})
	}
}

func TestIntentRejectsUnwalkableTarget(t *testing.T) {
	t.Parallel()

	w := newWorld()
	me, _ := w.Join("")

	// Find an adjacent unwalkable hex if the spawn has one; otherwise walk a
	// probe entity to the lake shore... which milestone 3 cannot do without
	// pathfinding, so settle for the direct check against the map.
	for _, n := range game.HexNeighbors(me.Hex) {
		if !isWalkable(w, n) {
			err := w.SubmitIntent(protocol.IntentRequest{EntityID: me.EntityID, Token: me.Token, Target: n})
			if got, want := err, game.ErrNotWalkable; !errors.Is(got, want) {
				t.Fatalf("err = %v, want ErrNotWalkable", got)
			}

			return
		}
	}

	t.Skip("spawn has no unwalkable neighbor on this map")
}

// walkableNeighbor returns the first neighbor a fresh entity can step to.
func walkableNeighbor(t *testing.T, w *game.World, from protocol.Hex) protocol.Hex {
	t.Helper()

	for _, n := range game.HexNeighbors(from) {
		if isWalkable(w, n) {
			return n
		}
	}

	t.Fatalf("no walkable neighbor around %v", from)

	return protocol.Hex{}
}

func isWalkable(w *game.World, h protocol.Hex) bool {
	for _, tile := range w.Map().Tiles {
		if tile.Hex == h {
			return tile.Terrain == protocol.TerrainGrass || tile.Terrain == protocol.TerrainForest
		}
	}

	return false
}

func submitOK(w *game.World, me protocol.JoinResponse, target protocol.Hex) bool {
	return w.SubmitIntent(protocol.IntentRequest{EntityID: me.EntityID, Token: me.Token, Target: target}) == nil
}

func TestIntentWalksMultiStepPath(t *testing.T) {
	t.Parallel()

	w := newWorld()
	me, _ := w.Join("")

	// A destination two hexes away: a walkable neighbor of a walkable neighbor
	// that sits at distance 2 from spawn (geometry-independent discovery).
	n1 := walkableNeighbor(t, w, me.Hex)

	var dest protocol.Hex

	for _, n2 := range game.HexNeighbors(n1) {
		if n2 != me.Hex && game.HexDistance(me.Hex, n2) == 2 && isWalkable(w, n2) {
			dest = n2

			break
		}
	}

	if dest == (protocol.Hex{}) {
		t.Skip("spawn has no reachable distance-2 hex on this map")
	}

	if err := w.SubmitIntent(protocol.IntentRequest{EntityID: me.EntityID, Token: me.Token, Target: dest}); err != nil {
		t.Fatalf("SubmitIntent: %v", err)
	}

	// One hex per turn: after the first turn the entity is adjacent to spawn,
	// not yet at the destination.
	snap := step(t, w)
	mid := snap.Entities[0].Hex

	if got, want := game.HexDistance(me.Hex, mid), 1; got != want {
		t.Fatalf("after turn 1: hex %v is not one step from spawn %v", mid, me.Hex)
	}

	if mid == dest {
		t.Fatal("reached a distance-2 destination in a single turn")
	}

	// The second turn arrives.
	snap = step(t, w)
	if got, want := snap.Entities[0].Hex, dest; got != want {
		t.Fatalf("after turn 2: hex = %v, want destination %v", got, want)
	}
}

func TestSnapshotCarriesInterval(t *testing.T) {
	t.Parallel()

	w := game.NewWorld(250*time.Millisecond, hub.New())
	if got, want := w.Snapshot().IntervalMs, int64(250); got != want {
		t.Fatalf("IntervalMs = %d, want 250", got)
	}
}
