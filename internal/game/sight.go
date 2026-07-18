package game

import "github.com/starquake/mediumrogue/internal/protocol"

// sight.go: line-of-sight geometry (#95). Terrain blocks spotting, so
// breaking line of sight is a real way to avoid or escape a fight — the
// design has always wanted this; pure distance was the shipped placeholder
// (decided 2026-07-14, settled 2026-07-18).
//
// The rule, settled with the maintainer:
//
//   - ROCK hard-blocks. A single rock hex anywhere strictly between two
//     entities ends the ray.
//   - FOREST softens: each forest hex strictly between them costs
//     protocol.ForestSightCost hexes of effective range.
//   - WATER is unwalkable but TRANSPARENT — walkableLocked is deliberately
//     NOT the predicate here.
//   - ENDPOINTS never count, so adjacent entities always see each other and
//     standing in forest never hides you from something already next to you.
//   - SYMMETRIC by construction (one ray, and the cost is a sum), so there is
//     never "it sees you but you don't see it".

// HexLine returns every hex the straight line from a to b passes through,
// endpoints included, in order — Red Blob's cube lerp plus cube rounding.
// A zero-length line is the single hex a.
func HexLine(a, b protocol.Hex) []protocol.Hex {
	n := HexDistance(a, b)
	if n == 0 {
		return []protocol.Hex{a}
	}

	// Nudge one endpoint by a hair so a line running exactly along a hex
	// edge lands on one side consistently instead of ping-ponging between
	// two equally-near hexes — the standard fix from the same guide.
	const epsilon = 1e-6

	line := make([]protocol.Hex, 0, n+1)

	aq, ar := float64(a.Q), float64(a.R)
	bq, br := float64(b.Q)+epsilon, float64(b.R)+epsilon

	for i := range n + 1 {
		t := float64(i) / float64(n)
		line = append(line, cubeRound(aq+(bq-aq)*t, ar+(br-ar)*t))
	}

	return line
}

// cubeRound rounds fractional axial coordinates to the nearest hex: round all
// three cube coordinates, then fix up whichever moved furthest so they still
// sum to zero.
func cubeRound(q, r float64) protocol.Hex {
	s := -q - r

	rq, rr, rs := round(q), round(r), round(s)
	dq, dr, ds := absF(float64(rq)-q), absF(float64(rr)-r), absF(float64(rs)-s)

	switch {
	case dq > dr && dq > ds:
		rq = -rr - rs
	case dr > ds:
		rr = -rq - rs
	}

	return protocol.Hex{Q: rq, R: rr}
}

func round(f float64) int {
	if f < 0 {
		return -int(-f + 0.5) //nolint:mnd // the 0.5 is rounding itself.
	}

	return int(f + 0.5) //nolint:mnd // the 0.5 is rounding itself.
}

func absF(f float64) float64 {
	if f < 0 {
		return -f
	}

	return f
}

// sightBlocked reports whether terrain blocks the line of sight from a to b
// at the given effective radius. terrainAt returns the terrain of a hex (the
// zero Terrain for an off-map hex, which blocks nothing — off-map hexes are
// not obstacles, they are absence). See this file's doc comment for the rule.
func sightBlocked(a, b protocol.Hex, radius int, terrainAt func(protocol.Hex) protocol.Terrain) bool {
	line := HexLine(a, b)
	if len(line) <= 2 { //nolint:mnd // a line of at most two hexes is the two endpoints: nothing in between.
		return false // adjacent or same hex: nothing strictly between them
	}

	cost := HexDistance(a, b)

	for _, h := range line[1 : len(line)-1] {
		switch terrainAt(h) {
		case protocol.TerrainRock:
			return true
		case protocol.TerrainForest:
			cost += protocol.ForestSightCost
		case protocol.TerrainGrass, protocol.TerrainWater:
			// Grass is open; water is unwalkable but transparent.
		}
	}

	return cost > radius
}
