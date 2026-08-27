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
	default:
		// ds is the largest component: rs is recomputed, and is already set.
	}

	return protocol.Hex{Q: rq, R: rr}
}

func round(f float64) int {
	if f < 0 {
		return -int(-f + 0.5) // The 0.5 is rounding itself.
	}

	return int(f + 0.5) // The 0.5 is rounding itself.
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
	// Draw the ray in a canonical endpoint order so the answer is identical
	// whichever entity is a and which is b — the symmetry this file's doc
	// comment promises and TestSightIsSymmetric guards (#95 Q4: never "it sees
	// you but you don't see it"). HexLine nudges only its SECOND endpoint by an
	// epsilon to break edge-aligned ties, which makes the raw ray
	// direction-dependent: a and b swapped can traverse a different set of
	// in-between hexes, so a rock (or forest) between them blocks one direction
	// and not the other. Callers pick the direction freely — the aggro path
	// raycasts monster→player, ranged attacks attacker→target, bubble formation
	// lower-id→higher-id — so without a canonical order the same two hexes can
	// disagree about visibility depending on who asks. Ordering the endpoints
	// here keeps HexLine's tie-break deterministic while making the outcome an
	// intrinsic property of the unordered pair.
	if compareHexQR(a, b) > 0 {
		a, b = b, a
	}

	rock, forestCost := rayObstacles(a, b, terrainAt)
	if rock {
		return true
	}

	return HexDistance(a, b)+forestCost > radius
}

// rayObstacles walks the hexes STRICTLY BETWEEN a and b and reports what the
// ray met: whether a rock wall stands in it, and what the forest it crossed
// costs. Split out because two rules read the same ray and must not drift:
// ordinary sight spends the forest cost against a budget, while evade (#322)
// only asks whether a wall is in the way — a wall stays real cover, but woods
// no longer refuse the escape they were most needed for (#313).
//
// Endpoints are assumed canonically ordered by the caller.
// Returns (a wall stands in the ray, the forest cost the ray crossed).
func rayObstacles(a, b protocol.Hex, terrainAt func(protocol.Hex) protocol.Terrain) (bool, int) {
	forestCost := 0

	line := HexLine(a, b)
	// A line of at most two hexes is the two endpoints: nothing in between.
	if len(line) <= 2 {
		return false, 0 // adjacent or same hex: nothing strictly between them
	}

	for _, h := range line[1 : len(line)-1] {
		switch terrainAt(h) {
		case protocol.TerrainRock:
			return true, 0
		case protocol.TerrainForest:
			forestCost += protocol.ForestSightCost
		case protocol.TerrainGrass, protocol.TerrainWater, protocol.TerrainMud:
			// Grass is open; water is unwalkable but transparent; mud is a
			// bog, which is open ground — you see it, not what is in it.
		default:
			// Unreachable: every protocol terrain is listed above, and the
			// exhaustive linter fails the build if a new one is not.
			panic("game: sight has no rule for terrain " + terrainAt(h))
		}
	}

	return false, forestCost
}

// seesLocked reports whether an entity at a can spot one at b: within
// CombatRadius is the caller's business (the cheap pre-filter), this is the
// terrain half. Callers hold w.mu.
func (w *World) seesLocked(a, b protocol.Hex) bool {
	return !sightBlocked(a, b, protocol.CombatRadius, func(h protocol.Hex) protocol.Terrain { return w.terrain[h] })
}

// sightBlockedLocked is sightBlocked against this world's terrain, at an
// explicit radius — the aggro path passes the monster's own effective reach
// rather than CombatRadius (#95). Callers hold w.mu.
func (w *World) sightBlockedLocked(a, b protocol.Hex, radius int) bool {
	return sightBlocked(a, b, radius, func(h protocol.Hex) protocol.Terrain { return w.terrain[h] })
}
