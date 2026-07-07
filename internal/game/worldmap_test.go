package game_test

import (
	"testing"

	"github.com/starquake/medium-rogue/internal/game"
	"github.com/starquake/medium-rogue/internal/protocol"
)

func TestStaticMapShape(t *testing.T) {
	t.Parallel()

	m := game.StaticMap()

	wantTiles := 3*game.MapRadius*(game.MapRadius+1) + 1
	if len(m.Tiles) != wantTiles {
		t.Fatalf("len(Tiles) = %d, want %d (hexagon of radius %d)", len(m.Tiles), wantTiles, game.MapRadius)
	}

	if m.Radius != game.MapRadius {
		t.Fatalf("Radius = %d, want %d", m.Radius, game.MapRadius)
	}

	origin := protocol.Hex{Q: 0, R: 0}
	valid := map[protocol.Terrain]bool{
		protocol.TerrainGrass:  true,
		protocol.TerrainForest: true,
		protocol.TerrainWater:  true,
		protocol.TerrainRock:   true,
	}
	seen := make(map[protocol.Hex]bool, len(m.Tiles))
	counts := make(map[protocol.Terrain]int)

	for _, tile := range m.Tiles {
		if d := game.HexDistance(origin, tile.Hex); d > game.MapRadius {
			t.Fatalf("tile %v outside radius: distance %d", tile.Hex, d)
		}

		if seen[tile.Hex] {
			t.Fatalf("duplicate tile at %v", tile.Hex)
		}

		seen[tile.Hex] = true

		if !valid[tile.Terrain] {
			t.Fatalf("tile %v has unknown terrain %q", tile.Hex, tile.Terrain)
		}

		counts[tile.Terrain]++

		// The rim is the wall of the world: every edge tile must be rock.
		if game.HexDistance(origin, tile.Hex) == game.MapRadius && tile.Terrain != protocol.TerrainRock {
			t.Fatalf("rim tile %v is %q, want rock", tile.Hex, tile.Terrain)
		}
	}

	// The map must actually contain some of everything.
	for _, terrain := range []protocol.Terrain{
		protocol.TerrainGrass, protocol.TerrainForest, protocol.TerrainWater, protocol.TerrainRock,
	} {
		if counts[terrain] == 0 {
			t.Errorf("map contains no %s tiles", terrain)
		}
	}
}

func TestStaticMapIsDeterministic(t *testing.T) {
	t.Parallel()

	first := game.StaticMap()
	second := game.StaticMap()

	if len(first.Tiles) != len(second.Tiles) {
		t.Fatalf("tile counts differ: %d vs %d", len(first.Tiles), len(second.Tiles))
	}

	for i := range first.Tiles {
		if first.Tiles[i] != second.Tiles[i] {
			t.Fatalf("tile %d differs between calls: %+v vs %+v", i, first.Tiles[i], second.Tiles[i])
		}
	}
}
