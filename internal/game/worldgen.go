package game

import (
	"math"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// Generation tuning. All thresholds are in [0,1) over the noise fields; tweak
// to shift the land/water/forest/mountain balance. noiseScale sets feature
// size (smaller = larger, smoother regions — a feature spans ~1/noiseScale
// hexes). clearingRadius forces a walkable circle at the origin so spawns
// always have room regardless of the noise there.
const (
	noiseScale     = 0.11
	waterLevel     = 0.30
	mountainLevel  = 0.78
	forestLevel    = 0.55
	clearingRadius = 2
	moistureSalt   = 0x1234_5678_9ABC_DEF0
)

// GenerateMap builds a deterministic procedural world of the given hex radius
// from seed: a rock-rimmed hexagon of coherent biomes (grass, forest, water,
// rock) derived from two value-noise fields (elevation, moisture), with a
// forced walkable clearing at the origin. Same (seed, radius) → identical map.
func GenerateMap(seed uint64, radius int) protocol.MapResponse {
	tiles := make([]protocol.Tile, 0, tileCount(radius))
	origin := protocol.Hex{Q: 0, R: 0}

	for q := -radius; q <= radius; q++ {
		for r := -radius; r <= radius; r++ {
			h := protocol.Hex{Q: q, R: r}
			if HexDistance(origin, h) > radius {
				continue
			}

			tiles = append(tiles, protocol.Tile{Hex: h, Terrain: terrainAt(seed, radius, h)})
		}
	}

	return protocol.MapResponse{Radius: radius, Tiles: tiles}
}

// terrainAt classifies one hex: the rim is rock; the origin clearing is grass;
// otherwise elevation carves water (low) and mountains (high), and within land
// moisture separates forest (moist) from grass (dry).
func terrainAt(seed uint64, radius int, h protocol.Hex) protocol.Terrain {
	origin := protocol.Hex{Q: 0, R: 0}
	switch {
	case HexDistance(origin, h) == radius:
		return protocol.TerrainRock
	case HexDistance(origin, h) <= clearingRadius:
		return protocol.TerrainGrass
	}

	// Sample the noise fields in a lightly sheared axial plane so regions are
	// spatially coherent across neighbouring hexes.
	fx := float64(h.Q) * noiseScale
	fy := (float64(h.R) + float64(h.Q)*0.5) * noiseScale

	elevation := fbm(seed, fx, fy)
	switch {
	case elevation < waterLevel:
		return protocol.TerrainWater
	case elevation > mountainLevel:
		return protocol.TerrainRock
	}

	moisture := fbm(seed^moistureSalt, fx, fy)
	if moisture > forestLevel {
		return protocol.TerrainForest
	}

	return protocol.TerrainGrass
}

// fbm sums two octaves of value noise weighted 2:1 (the "2" and "3" below)
// into [0,1) — enough for organic regions without the cost of more octaves.
//
//nolint:mnd // octave-weighting constants, explained above.
func fbm(seed uint64, x, y float64) float64 {
	return (noise2D(seed, x, y)*2 + noise2D(seed+1, x*2, y*2)) / 3
}

// noise2D is smoothstep-interpolated value noise over the unit integer lattice.
func noise2D(seed uint64, x, y float64) float64 {
	x0 := int(math.Floor(x))
	y0 := int(math.Floor(y))
	fx := smoothstep(x - float64(x0))
	fy := smoothstep(y - float64(y0))

	v00 := latticeValue(seed, x0, y0)
	v10 := latticeValue(seed, x0+1, y0)
	v01 := latticeValue(seed, x0, y0+1)
	v11 := latticeValue(seed, x0+1, y0+1)

	top := v00 + fx*(v10-v00)
	bot := v01 + fx*(v11-v01)

	return top + fy*(bot-top)
}

func smoothstep(t float64) float64 { return t * t * (3 - 2*t) } //nolint:mnd // Hermite 3t²−2t³.

// latticeValue hashes an integer lattice point + seed to a value in [0,1).
// SplitMix64-style mixing; the magic constants and truncations are the point.
//
//nolint:mnd,gosec // integer-hash mixing constants; wraparound conversions intentional.
func latticeValue(seed uint64, gx, gy int) float64 {
	h := seed
	h ^= uint64(uint32(gx)) * 0x9E3779B97F4A7C15
	h ^= uint64(uint32(gy)) * 0xC2B2AE3D27D4EB4F
	h ^= h >> 30
	h *= 0xBF58476D1CE4E5B9
	h ^= h >> 27
	h *= 0x94D049BB133111EB
	h ^= h >> 31

	return float64(h>>11) / float64(uint64(1)<<53)
}

// reachableWalkable returns the set of walkable hexes connected to the origin,
// via BFS over hex neighbours. Spawn placement restricts to this set so a
// player is never stranded on an island or across water.
func reachableWalkable(m protocol.MapResponse) map[protocol.Hex]bool {
	walkable := make(map[protocol.Hex]bool, len(m.Tiles))
	for _, t := range m.Tiles {
		if t.Terrain == protocol.TerrainGrass || t.Terrain == protocol.TerrainForest {
			walkable[t.Hex] = true
		}
	}

	origin := protocol.Hex{Q: 0, R: 0}

	reach := make(map[protocol.Hex]bool)
	if !walkable[origin] {
		return reach
	}

	queue := []protocol.Hex{origin}
	reach[origin] = true

	for len(queue) > 0 {
		h := queue[0]
		queue = queue[1:]

		for _, n := range HexNeighbors(h) {
			if walkable[n] && !reach[n] {
				reach[n] = true
				queue = append(queue, n)
			}
		}
	}

	return reach
}

// tileCount is the number of hexes in a hexagon of the given radius:
// 3r(r+1)+1 (centered hexagonal number).
func tileCount(radius int) int {
	return 3*radius*(radius+1) + 1
}
