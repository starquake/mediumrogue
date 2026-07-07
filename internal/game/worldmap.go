package game

import "github.com/starquake/mediumrogue/internal/protocol"

// MapRadius is the static world's hex radius. Milestone 7 replaces this
// hand-shaped map with procedural generation.
const MapRadius = 12

// lakeRadius carves an impassable water lake around lakeCenter.
const lakeRadius = 3

// lakeCenter is offset from the origin so spawns near the center stay dry.
// Immutable map-shape constant; Go has no const structs, and its coordinates
// are arbitrary map dressing.
//
//nolint:gochecknoglobals,mnd
var lakeCenter = protocol.Hex{Q: 5, R: -2}

// StaticMap builds the deterministic milestone-2 world: a hexagon of radius
// MapRadius, ringed by impassable rock, with a water lake, scattered forest,
// and grass everywhere else. Same output on every call — no RNG state.
func StaticMap() protocol.MapResponse {
	tiles := make([]protocol.Tile, 0, tileCount(MapRadius))
	origin := protocol.Hex{Q: 0, R: 0}

	for q := -MapRadius; q <= MapRadius; q++ {
		for r := -MapRadius; r <= MapRadius; r++ {
			h := protocol.Hex{Q: q, R: r}
			if HexDistance(origin, h) > MapRadius {
				continue
			}

			tiles = append(tiles, protocol.Tile{Hex: h, Terrain: terrainAt(h)})
		}
	}

	return protocol.MapResponse{Radius: MapRadius, Tiles: tiles}
}

// terrainAt shapes the world: rock rim, one lake, hash-scattered forest
// (~1 in 5), grass otherwise.
func terrainAt(h protocol.Hex) protocol.Terrain {
	switch {
	case HexDistance(protocol.Hex{Q: 0, R: 0}, h) == MapRadius:
		return protocol.TerrainRock
	case HexDistance(lakeCenter, h) <= lakeRadius:
		return protocol.TerrainWater
	case hexHash(h)%5 == 0:
		return protocol.TerrainForest
	default:
		return protocol.TerrainGrass
	}
}

// hexHash is a tiny deterministic integer hash over axial coordinates —
// just enough scatter to make the forest look organic. Not a security or
// distribution-quality hash: the murmur3-style constants and shifts below
// are magic on purpose, and the int→uint32 truncation is part of the mix.
func hexHash(h protocol.Hex) uint32 {
	//nolint:gosec,mnd // see above: truncating conversion and magic mixing constants are the point.
	x := uint32(h.Q)*0x9E3779B1 ^ uint32(h.R)*0x85EBCA77
	x ^= x >> 13 //nolint:mnd
	x *= 0xC2B2AE35
	x ^= x >> 16 //nolint:mnd

	return x
}

// tileCount is the number of hexes in a hexagon of the given radius:
// 3r(r+1)+1 (centered hexagonal number).
func tileCount(radius int) int {
	return 3*radius*(radius+1) + 1
}
