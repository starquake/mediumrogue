package game_test

import (
	"errors"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// Bubble-clock values for tests: a long patience keeps the AFK fallback out of
// the way (tests that exercise it override via SetCombatPatienceForTest) and a
// fast poll keeps any Run loop ticking promptly.
const (
	testCombatPatience = time.Minute
	testBubblePoll     = time.Millisecond
)

func newWorld() *game.World {
	return game.NewWorld(time.Hour, testCombatPatience, testBubblePoll, hub.New())
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

	w := game.NewWorld(250*time.Millisecond, testCombatPatience, testBubblePoll, hub.New())
	if got, want := w.Snapshot().IntervalMs, int64(250); got != want {
		t.Fatalf("IntervalMs = %d, want 250", got)
	}
}

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
	if got, want := hexOfSnap(snap, idA), center; got != want {
		t.Errorf("A at %v, want center %v", got, want)
	}

	if got, want := hexOfSnap(snap, idB), center; got != want {
		t.Errorf("B at %v, want center %v", got, want)
	}

	if got, want := countAt(snap, center), 2; got != want {
		t.Errorf("center occupancy = %d, want 2", got)
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

	if got, want := countAt(snap, center), protocol.StackCap; got != want {
		t.Errorf("center occupancy = %d, want StackCap %d", got, want)
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

// placeSixMoversIntoCenter places one entity on each of the six neighbours of
// the origin, each queued to step onto the origin. With StackCap = 5 exactly
// one loses the seeded overflow tie-break. Returns the entity ids in placement
// (ascending-id) order.
func placeSixMoversIntoCenter(t *testing.T, w *game.World, center protocol.Hex) []int64 {
	t.Helper()

	ids := make([]int64, 0, 6)

	for _, n := range game.HexNeighbors(center) {
		id, tok := w.PlaceEntityForTest(n)
		mustSubmit(t, w, id, tok, center)
		ids = append(ids, id)
	}

	return ids
}

// blockedMover returns the id of the entity NOT on center after one resolve, or
// 0 if none (should not happen in a 6-into-5 overflow).
func blockedMover(snap protocol.TurnEvent, ids []int64, center protocol.Hex) int64 {
	for _, id := range ids {
		if hexOfSnap(snap, id) != center {
			return id
		}
	}

	return 0
}

func TestPhasedOverflowAdmitsStackCapBlocksOne(t *testing.T) {
	t.Parallel()

	w := newWorld()

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	ids := placeSixMoversIntoCenter(t, w, center)
	w.ResolveTurnForTest()
	snap := w.Snapshot()

	if got, want := countAt(snap, center), protocol.StackCap; got != want {
		t.Fatalf("center occupancy = %d, want StackCap %d", got, want)
	}

	if got := blockedMover(snap, ids, center); got == 0 {
		t.Fatal("expected exactly one blocked mover, found none")
	}
}

func TestPhasedTieBreakIsReproducible(t *testing.T) {
	t.Parallel()

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(newWorld(), center) {
		t.Skip("origin is not walkable on this map")
	}

	blockedFor := func(seed int64) int64 {
		w := newWorld()
		w.SetSeedForTest(seed)
		ids := placeSixMoversIntoCenter(t, w, center)
		w.ResolveTurnForTest()

		return blockedMover(w.Snapshot(), ids, center)
	}

	// Same seed + same board → same tie-break outcome.
	if got, want := blockedFor(42), blockedFor(42); got != want {
		t.Fatalf("same seed produced different blocked entities: %d vs %d", got, want)
	}
}

func TestPhasedTieBreakIsNotIDFavoritism(t *testing.T) {
	t.Parallel()

	// The old placeholder always blocked the highest id (low ids moved first and
	// filled the hex). Under the seeded shuffle the loser must depend on the
	// seed — assert at least one seed (of 20) blocks a non-max id. Deterministic
	// (fixed seeds), not flaky: P(all 20 block the max) = (1/6)^20 ≈ 0.
	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(newWorld(), center) {
		t.Skip("origin is not walkable on this map")
	}

	var maxID int64

	blockedNonMax := false

	for seed := range int64(20) {
		w := newWorld()
		w.SetSeedForTest(seed)
		ids := placeSixMoversIntoCenter(t, w, center)
		maxID = ids[len(ids)-1] // PlaceEntityForTest assigns ascending ids

		w.ResolveTurnForTest()

		if b := blockedMover(w.Snapshot(), ids, center); b != 0 && b != maxID {
			blockedNonMax = true

			break
		}
	}

	if !blockedNonMax {
		t.Fatalf("across 20 seeds the blocked mover was always the max id %d — no seeded tie-break", maxID)
	}
}
