package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// terrain_mud_test.go (#437): mud is the fifth terrain, and adding a terrain
// is a "these must all agree" change — walkable, spawnable, transparent to
// sight, and generated. The sight half lives in sight_test.go, which is
// white-box; everything reachable from outside the package is here.

// TestMudIsWalkable pins the rule the two Go call sites share: mud is ground
// you can stand on, mechanically identical to grass. Only its look and, from
// #436, what buries in it differ.
func TestMudIsWalkable(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		terrain protocol.Terrain
		want    bool
	}{
		{protocol.TerrainGrass, true},
		{protocol.TerrainForest, true},
		{protocol.TerrainMud, true},
		{protocol.TerrainWater, false},
		{protocol.TerrainRock, false},
	} {
		if got, want := game.TerrainWalkableForTest(tc.terrain), tc.want; got != want {
			t.Errorf("walkable(%q) = %v, want %v", tc.terrain, got, want)
		}
	}
}

// TestMudIsReachableAndSpawnable: reachableWalkable is both the connectivity
// BFS and the spawn-candidate filter, so mud joining it is what lets #436's
// skeletons place on a bog at all. A synthetic map rather than a generated
// one, so this rule holds independently of how much mud generation produces.
func TestMudIsReachableAndSpawnable(t *testing.T) {
	t.Parallel()

	// A three-hex spur east of the origin: grass, mud, grass. If mud were
	// unwalkable the BFS would stop at it and never reach the far hex.
	near := protocol.Hex{Q: 1, R: 0}
	far := protocol.Hex{Q: 2, R: 0}
	m := protocol.MapResponse{Radius: 2, Tiles: []protocol.Tile{
		{Hex: protocol.Hex{Q: 0, R: 0}, Terrain: protocol.TerrainGrass},
		{Hex: near, Terrain: protocol.TerrainMud},
		{Hex: far, Terrain: protocol.TerrainGrass},
	}}

	reach := game.ReachableWalkableForTest(m)

	if got, want := reach[near], true; got != want {
		t.Errorf("mud hex %v reachable = %v, want %v", near, got, want)
	}

	if got, want := reach[far], true; got != want {
		t.Errorf("hex %v beyond the mud reachable = %v, want %v — mud must not dam the BFS", far, got, want)
	}
}
