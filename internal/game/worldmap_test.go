package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

func TestStaticMapShape(t *testing.T) {
	t.Parallel()

	m := game.StaticMap()

	wantTiles := 3*game.MapRadius*(game.MapRadius+1) + 1
	if got, want := len(m.Tiles), wantTiles; got != want {
		t.Fatalf("len(Tiles) = %d, want %d (hexagon of radius %d)", got, want, game.MapRadius)
	}

	if got, want := m.Radius, game.MapRadius; got != want {
		t.Fatalf("Radius = %d, want %d", got, want)
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

	if got, want := len(first.Tiles), len(second.Tiles); got != want {
		t.Fatalf("tile counts differ: %d vs %d", got, want)
	}

	for i := range first.Tiles {
		if got, want := first.Tiles[i], second.Tiles[i]; got != want {
			t.Fatalf("tile %d differs between calls: %+v vs %+v", i, got, want)
		}
	}
}
