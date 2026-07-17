package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// allWalkable is an open plane: every hex is walkable.
func allWalkable(protocol.Hex) bool { return true }

func TestPathfindStraightLine(t *testing.T) {
	t.Parallel()

	from := protocol.Hex{Q: 0, R: 0}
	to := protocol.Hex{Q: 3, R: 0}

	path := game.Pathfind(from, to, allWalkable)

	if got, want := len(path), 3; got != want {
		t.Fatalf("path length = %d, want 3 (%v)", got, path)
	}

	if got, want := path[len(path)-1], to; got != want {
		t.Fatalf("path does not end at destination: %v", path)
	}

	// Every step is adjacent to the previous position. Report every offending
	// step (t.Errorf, not t.Fatalf) so one bad step does not mask the rest.
	prev := from
	for _, step := range path {
		if got, want := game.HexDistance(prev, step), 1; got != want {
			t.Errorf("non-adjacent step %v after %v", step, prev)
		}

		prev = step
	}
}

func TestPathfindFromEqualsTo(t *testing.T) {
	t.Parallel()

	h := protocol.Hex{Q: 1, R: -2}
	path := game.Pathfind(h, h, allWalkable)

	if path == nil || len(path) != 0 {
		t.Fatalf("from==to must return an empty non-nil path, got %v", path)
	}
}

func TestPathfindUnwalkableDestinationIsNil(t *testing.T) {
	t.Parallel()

	to := protocol.Hex{Q: 2, R: 0}
	walkable := func(h protocol.Hex) bool { return h != to }

	if path := game.Pathfind(protocol.Hex{}, to, walkable); path != nil {
		t.Fatalf("unwalkable destination must be nil, got %v", path)
	}
}

func TestPathfindRoutesAroundAWall(t *testing.T) {
	t.Parallel()

	// A vertical wall at q==1 for r in [-2..2]. from (0,0) to (2,0) cannot go
	// straight through q==1 and must detour around an end of the wall.
	wall := map[protocol.Hex]bool{
		{Q: 1, R: -2}: true, {Q: 1, R: -1}: true, {Q: 1, R: 0}: true,
		{Q: 1, R: 1}: true, {Q: 1, R: 2}: true,
	}
	walkable := func(h protocol.Hex) bool { return !wall[h] }

	path := game.Pathfind(protocol.Hex{Q: 0, R: 0}, protocol.Hex{Q: 2, R: 0}, walkable)
	if path == nil {
		t.Fatal("expected a detour path, got nil")
	}

	for _, step := range path {
		if wall[step] {
			t.Errorf("path walked through the wall at %v", step)
		}
	}

	if got, want := path[len(path)-1], (protocol.Hex{Q: 2, R: 0}); got != want {
		t.Fatalf("path does not reach destination: %v", path)
	}
}

func TestPathfindUnreachableIsNil(t *testing.T) {
	t.Parallel()

	// (0,0) is fully walled in by an impassable ring; any outside target is
	// unreachable.
	ring := map[protocol.Hex]bool{}
	for _, n := range game.HexNeighbors(protocol.Hex{Q: 0, R: 0}) {
		ring[n] = true
	}

	walkable := func(h protocol.Hex) bool { return !ring[h] }

	if path := game.Pathfind(protocol.Hex{Q: 0, R: 0}, protocol.Hex{Q: 5, R: 0}, walkable); path != nil {
		t.Fatalf("unreachable destination must be nil, got %v", path)
	}
}

// TestPathfindReachesAnExemptGoal pins the idiom #96's re-path is built on: the
// predicate rejects OCCUPIED hexes but exempts the goal itself. Pathfind returns
// nil whenever !walkable(to), so a goal that is itself occupied — a full stack, a
// monster that wandered onto it — would otherwise kill every detour outright.
func TestPathfindReachesAnExemptGoal(t *testing.T) {
	t.Parallel()

	from := protocol.Hex{Q: 0, R: 0}
	goal := protocol.Hex{Q: 1, R: -2}
	// The goal and one of its two shortest routes ({0,-1}) are both occupied;
	// only the route through {1,-1} is open.
	occupied := map[protocol.Hex]bool{{Q: 0, R: -1}: true, goal: true}
	walkable := func(h protocol.Hex) bool { return h == goal || !occupied[h] }

	path := game.Pathfind(from, goal, walkable)
	if path == nil {
		t.Fatal("expected a path to the exempt goal, got nil")
	}

	if got, want := path[len(path)-1], goal; got != want {
		t.Errorf("path end = %v, want the goal %v", got, want)
	}

	for _, step := range path {
		if step != goal && occupied[step] {
			t.Errorf("path walked through occupied hex %v", step)
		}
	}
}
