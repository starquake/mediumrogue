package game_test

import (
	"slices"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// wedgeBoard is the geometry every re-path test needs: `blocked` and `open` are
// CONSECUTIVE walkable neighbours of `origin`, and `dest` is their shared far
// corner — walkable, adjacent to both, and at distance 2 from origin. So
// origin→dest has exactly two shortest routes, [blocked, dest] and [open, dest].
// Standing something on `blocked` therefore leaves a same-length detour through
// `open`: any stall is the walker's own doing, never a lack of alternatives.
type wedgeBoard struct {
	origin  protocol.Hex
	blocked protocol.Hex
	open    protocol.Hex
	dest    protocol.Hex
}

// originWedge builds a wedgeBoard at the origin, skipping the test when the
// generated map has no such wedge there.
func originWedge(t *testing.T, w *game.World) wedgeBoard {
	t.Helper()

	origin := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, origin) {
		t.Skip("origin is not walkable on this map")
	}

	ns := game.HexNeighbors(origin)
	for i := range ns {
		x, y := ns[i], ns[(i+1)%len(ns)]
		corner := protocol.Hex{Q: x.Q + y.Q - origin.Q, R: x.R + y.R - origin.R}

		if isWalkable(w, x) && isWalkable(w, y) && isWalkable(w, corner) {
			return wedgeBoard{origin: origin, blocked: x, open: y, dest: corner}
		}
	}

	t.Skipf("no walkable wedge around %v on this map", origin)

	return wedgeBoard{}
}

// TestBlockedPlayerWalkDetoursAroundHostile: a queued walk whose next step a
// monster has wandered onto re-routes around it and still advances THIS turn
// (#96) — it no longer stalls at its current hex.
func TestBlockedPlayerWalkDetoursAroundHostile(t *testing.T) {
	t.Parallel()

	w := newWorld()
	b := originWedge(t, w)

	pid, _ := w.PlaceEntityForTest(b.origin)
	w.PlaceMonsterForTest(b.blocked)
	w.SetPathForTest(pid, []protocol.Hex{b.blocked, b.dest})

	w.ResolveCombatOnlyForTest()

	if got, want := hexOfSnap(w.Snapshot(), pid), b.open; got != want {
		t.Errorf("blocked walker hex = %v, want the detour step %v", got, want)
	}

	if got, want := w.PathForTest(pid), []protocol.Hex{b.dest}; !slices.Equal(got, want) {
		t.Errorf("path = %v, want %v (the re-route, first step consumed)", got, want)
	}
}

// TestBlockedPlayerWalkDetoursAroundFullStack: the other half of the block rule
// (world.go's blockedFor) — a same-faction hex at StackCap — also earns a
// detour, not a stall.
func TestBlockedPlayerWalkDetoursAroundFullStack(t *testing.T) {
	t.Parallel()

	w := newWorld()
	b := originWedge(t, w)

	for range protocol.StackCap {
		w.PlaceEntityForTest(b.blocked)
	}

	pid, _ := w.PlaceEntityForTest(b.origin)
	w.SetPathForTest(pid, []protocol.Hex{b.blocked, b.dest})

	w.ResolveCombatOnlyForTest()

	if got, want := hexOfSnap(w.Snapshot(), pid), b.open; got != want {
		t.Errorf("blocked walker hex = %v, want the detour step %v", got, want)
	}

	if got, want := w.PathForTest(pid), []protocol.Hex{b.dest}; !slices.Equal(got, want) {
		t.Errorf("path = %v, want %v (the re-route, first step consumed)", got, want)
	}
}

// TestBlockedMonsterWalkNeverDetours pins the player-only rule (#96 decision 1):
// a monster waits with its path retained, exactly as before.
//
// Monsters re-path from a retained goal every turn in thinkMonstersLocked, and
// their wait-on-block is load-bearing — it is how a standing intent becomes next
// turn's melee attack (collectMeleeAttacksLocked). The blocker here is a full
// FRIENDLY stack rather than a player, which keeps the test on the move phase: a
// player on the blocked hex would convert the step into a melee attack, and the
// monster would never reach movePhaseLocked's block at all.
func TestBlockedMonsterWalkNeverDetours(t *testing.T) {
	t.Parallel()

	w := newWorld()
	b := originWedge(t, w)

	for range protocol.StackCap {
		w.PlaceMonsterForTest(b.blocked)
	}

	mid := w.PlaceMonsterForTest(b.origin)
	path := []protocol.Hex{b.blocked, b.dest}
	w.SetPathForTest(mid, path)

	w.ResolveCombatOnlyForTest()

	if got, want := hexOfSnap(w.Snapshot(), mid), b.origin; got != want {
		t.Errorf("blocked monster hex = %v, want %v — monsters never detour", got, want)
	}

	if got, want := w.PathForTest(mid), path; !slices.Equal(got, want) {
		t.Errorf("path = %v, want %v retained", got, want)
	}
}

// TestBlockedPlayerWalkWaitsWhenGoalIsUnreachable: with every neighbour of the
// destination hostile-held there is no detour at all, so the walker falls back
// to the pre-#96 behaviour — wait, path retained.
func TestBlockedPlayerWalkWaitsWhenGoalIsUnreachable(t *testing.T) {
	t.Parallel()

	w := newWorld()
	b := originWedge(t, w)

	pid, _ := w.PlaceEntityForTest(b.origin)
	for _, n := range game.HexNeighbors(b.dest) {
		w.PlaceMonsterForTest(n)
	}

	path := []protocol.Hex{b.blocked, b.dest}
	w.SetPathForTest(pid, path)

	w.ResolveCombatOnlyForTest()

	if got, want := hexOfSnap(w.Snapshot(), pid), b.origin; got != want {
		t.Errorf("walker hex = %v, want %v — no detour exists, so it waits", got, want)
	}

	if got, want := w.PathForTest(pid), path; !slices.Equal(got, want) {
		t.Errorf("path = %v, want %v retained", got, want)
	}
}

// TestBlockedPlayerWalkWaitsWhenDetourExceedsSlack pins the slack guard
// (protocol.RepathDetourSlack): a re-route more than the slack longer than the
// route it replaces is refused, and the walker waits as it did before #96.
//
// The realistic trigger is a terrain chokepoint — a hostile parked on a land
// bridge — which this test cannot build: NewWorld derives terrain from
// seed+radius and nothing injects it, so a chokepoint would mean seed-hunting a
// generated map. movePhaseLocked reads only path[0] and path[len-1], so a short
// route pointed at a far goal pins the same arithmetic head-on: two steps
// remaining, a blocked next step, and a goal whose real route is far longer than
// two + the slack.
func TestBlockedPlayerWalkWaitsWhenDetourExceedsSlack(t *testing.T) {
	t.Parallel()

	w := newWorld()
	b := originWedge(t, w)

	terrain := func(h protocol.Hex) bool { return isWalkable(w, h) }

	var far protocol.Hex

	found := false

	for _, tile := range w.Map().Tiles {
		if !isWalkable(w, tile.Hex) || game.HexDistance(b.origin, tile.Hex) < 8 {
			continue
		}

		if len(game.Pathfind(b.origin, tile.Hex, terrain)) > 2+protocol.RepathDetourSlack {
			far, found = tile.Hex, true

			break
		}
	}

	if !found {
		t.Skip("no goal far enough to exceed the slack on this map")
	}

	pid, _ := w.PlaceEntityForTest(b.origin)
	w.PlaceMonsterForTest(b.blocked)

	path := []protocol.Hex{b.blocked, far}
	w.SetPathForTest(pid, path)

	w.ResolveCombatOnlyForTest()

	if got, want := hexOfSnap(w.Snapshot(), pid), b.origin; got != want {
		t.Errorf("walker hex = %v, want %v — the only detour exceeds the slack", got, want)
	}

	if got, want := w.PathForTest(pid), path; !slices.Equal(got, want) {
		t.Errorf("path = %v, want %v retained", got, want)
	}
}
