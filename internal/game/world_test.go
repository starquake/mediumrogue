package game_test

import (
	"errors"
	"testing"
	"time"

	"github.com/starquake/medium-rogue/internal/game"
	"github.com/starquake/medium-rogue/internal/hub"
	"github.com/starquake/medium-rogue/internal/protocol"
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
	if len(snap.Entities) != 1 {
		t.Fatalf("entities = %d, want 1", len(snap.Entities))
	}

	if snap.Entities[0].Hex != resp.Hex {
		t.Fatalf("snapshot hex %v != join hex %v", snap.Entities[0].Hex, resp.Hex)
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

	if again.EntityID != first.EntityID {
		t.Fatalf("re-join created a new entity: %d != %d", again.EntityID, first.EntityID)
	}

	if len(w.Snapshot().Entities) != 1 {
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
	if got := w.Snapshot().Entities[0].Hex; got != me.Hex {
		t.Fatalf("entity moved before resolve: %v", got)
	}

	snap := step(t, w)
	if got := snap.Entities[0].Hex; got != target {
		t.Fatalf("after resolve: hex = %v, want %v", got, target)
	}

	if snap.Turn != 1 {
		t.Fatalf("turn = %d, want 1", snap.Turn)
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
	if got := snap.Entities[0].Hex; got != second {
		t.Fatalf("hex = %v, want the latest intent target %v", got, second)
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
		{
			"not adjacent",
			protocol.IntentRequest{EntityID: me.EntityID, Token: me.Token, Target: protocol.Hex{Q: me.Hex.Q + 5, R: me.Hex.R}},
			game.ErrNotAdjacent,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if err := w.SubmitIntent(tc.req); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
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
			if !errors.Is(err, game.ErrNotWalkable) {
				t.Fatalf("err = %v, want ErrNotWalkable", err)
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
