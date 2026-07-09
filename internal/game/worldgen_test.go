package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

const testSeed = 0xC0FFEE

func TestGenerateMapIsDeterministic(t *testing.T) {
	t.Parallel()

	a := game.GenerateMap(testSeed, 24)
	b := game.GenerateMap(testSeed, 24)

	if got, want := len(a.Tiles), len(b.Tiles); got != want {
		t.Fatalf("tile count differs across calls: %d vs %d", got, want)
	}

	for i := range a.Tiles {
		if got, want := a.Tiles[i], b.Tiles[i]; got != want {
			t.Fatalf("tile %d differs: %+v vs %+v", i, got, want)
		}
	}
}

func TestGenerateMapShape(t *testing.T) {
	t.Parallel()

	const radius = 24

	m := game.GenerateMap(testSeed, radius)

	if got, want := m.Radius, radius; got != want {
		t.Errorf("Radius = %d, want %d", got, want)
	}

	if got, want := len(m.Tiles), game.TileCountForTest(radius); got != want {
		t.Errorf("len(Tiles) = %d, want %d", got, want)
	}

	origin := protocol.Hex{Q: 0, R: 0}
	valid := map[protocol.Terrain]bool{
		protocol.TerrainGrass: true, protocol.TerrainForest: true,
		protocol.TerrainWater: true, protocol.TerrainRock: true,
	}
	counts := map[protocol.Terrain]int{}
	seen := map[protocol.Hex]bool{}

	var terrainAtOrigin protocol.Terrain

	for _, tile := range m.Tiles {
		if seen[tile.Hex] {
			t.Fatalf("duplicate tile %v", tile.Hex)
		}

		seen[tile.Hex] = true

		if !valid[tile.Terrain] {
			t.Fatalf("tile %v has unknown terrain %q", tile.Hex, tile.Terrain)
		}

		if game.HexDistance(origin, tile.Hex) > radius {
			t.Fatalf("tile %v outside radius %d", tile.Hex, radius)
		}

		// The rim is impassable rock.
		if game.HexDistance(origin, tile.Hex) == radius && tile.Terrain != protocol.TerrainRock {
			t.Fatalf("rim tile %v is %q, want rock", tile.Hex, tile.Terrain)
		}

		if tile.Hex == origin {
			terrainAtOrigin = tile.Terrain
		}

		counts[tile.Terrain]++
	}

	// Origin sits in the forced walkable clearing.
	if terrainAtOrigin != protocol.TerrainGrass && terrainAtOrigin != protocol.TerrainForest {
		t.Errorf("origin terrain = %q, want walkable", terrainAtOrigin)
	}

	// A real world has all four terrains.
	for _, terr := range []protocol.Terrain{
		protocol.TerrainGrass, protocol.TerrainForest, protocol.TerrainWater, protocol.TerrainRock,
	} {
		if counts[terr] == 0 {
			t.Errorf("map has no %s tiles", terr)
		}
	}
}

func TestGenerateMapDiffersBySeed(t *testing.T) {
	t.Parallel()

	a := game.GenerateMap(1, 24)
	b := game.GenerateMap(2, 24)

	diff := 0

	for i := range a.Tiles {
		if a.Tiles[i].Terrain != b.Tiles[i].Terrain {
			diff++
		}
	}

	if diff == 0 {
		t.Fatal("seed 1 and seed 2 produced identical terrain")
	}
}

func TestReachableSpawnRegionIsLarge(t *testing.T) {
	t.Parallel()

	// The origin must be walkable and sit in a large connected region so
	// spawns never strand a player on an island or in water.
	for _, seed := range []uint64{testSeed, 1, 42} {
		m := game.GenerateMap(seed, 24)
		reach := game.ReachableWalkableForTest(m)

		if !reach[protocol.Hex{Q: 0, R: 0}] {
			t.Fatalf("seed %d: origin not in the walkable region", seed)
		}
		// A healthy landmass — well above 15 players + monsters. tileCount(24)=1801.
		if got, wantMin := len(reach), 200; got < wantMin {
			t.Errorf("seed %d: reachable region = %d tiles, want >= %d", seed, got, wantMin)
		}
	}
}
