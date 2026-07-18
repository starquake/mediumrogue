package game //nolint:testpackage // white-box: exercises unexported sight geometry; see rules_test.go's file doc.

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// flatTerrain returns a terrain lookup where every named hex has the given
// terrain and everything else is grass — enough to place one obstacle on or
// beside a line.
func flatTerrain(kind protocol.Terrain, at ...protocol.Hex) func(protocol.Hex) protocol.Terrain {
	set := make(map[protocol.Hex]bool, len(at))
	for _, h := range at {
		set[h] = true
	}

	return func(h protocol.Hex) protocol.Terrain {
		if set[h] {
			return kind
		}

		return protocol.TerrainGrass
	}
}

// TestHexLineEndpointsAndLength: a line contains distance+1 hexes, starts at a
// and ends at b — the invariant every sight test rests on.
func TestHexLineEndpointsAndLength(t *testing.T) {
	t.Parallel()

	a := protocol.Hex{Q: 0, R: 0}

	for _, b := range []protocol.Hex{
		{Q: 0, R: 0}, {Q: 3, R: 0}, {Q: 0, R: 4}, {Q: -5, R: 2}, {Q: 2, R: -6},
	} {
		line := HexLine(a, b)

		if got, want := len(line), HexDistance(a, b)+1; got != want {
			t.Errorf("len(HexLine(%v, %v)) = %d, want %d", a, b, got, want)
		}

		if got, want := line[0], a; got != want {
			t.Errorf("HexLine(%v, %v) starts at %v, want %v", a, b, got, want)
		}

		if got, want := line[len(line)-1], b; got != want {
			t.Errorf("HexLine(%v, %v) ends at %v, want %v", a, b, got, want)
		}
	}
}

// TestHexLineStepsAreAdjacent: consecutive hexes on a line are neighbours —
// a line with a gap would let a ray skip straight through a rock wall.
func TestHexLineStepsAreAdjacent(t *testing.T) {
	t.Parallel()

	line := HexLine(protocol.Hex{Q: -4, R: 2}, protocol.Hex{Q: 3, R: -1})
	for i := 1; i < len(line); i++ {
		if got, want := HexDistance(line[i-1], line[i]), 1; got != want {
			t.Errorf("step %d: %v -> %v is %d hexes, want %d", i, line[i-1], line[i], got, want)
		}
	}
}

// TestSightOverOpenGroundReachesTheRadius: with nothing in the way, sight is
// exactly the distance test it replaces — blocked only beyond the radius.
func TestSightOverOpenGroundReachesTheRadius(t *testing.T) {
	t.Parallel()

	open := flatTerrain(protocol.TerrainRock) // nothing named: all grass
	a := protocol.Hex{Q: 0, R: 0}

	atRadius := protocol.Hex{Q: protocol.CombatRadius, R: 0}
	if got, want := sightBlocked(a, atRadius, protocol.CombatRadius, open), false; got != want {
		t.Errorf("sight at exactly the radius blocked = %v, want %v", got, want)
	}

	beyond := protocol.Hex{Q: protocol.CombatRadius + 1, R: 0}
	if got, want := sightBlocked(a, beyond, protocol.CombatRadius, open), true; got != want {
		t.Errorf("sight beyond the radius blocked = %v, want %v", got, want)
	}
}

// TestRockOnTheLineBlocksButBesideItDoesNot: the hard block, and the check
// that it is the LINE that matters and not mere proximity.
func TestRockOnTheLineBlocksButBesideItDoesNot(t *testing.T) {
	t.Parallel()

	a, b := protocol.Hex{Q: 0, R: 0}, protocol.Hex{Q: 4, R: 0}
	onLine := protocol.Hex{Q: 2, R: 0}
	beside := protocol.Hex{Q: 2, R: -2}

	if got, want := sightBlocked(a, b, protocol.CombatRadius, flatTerrain(protocol.TerrainRock, onLine)),
		true; got != want {
		t.Errorf("rock on the line blocks = %v, want %v", got, want)
	}

	if got, want := sightBlocked(a, b, protocol.CombatRadius, flatTerrain(protocol.TerrainRock, beside)),
		false; got != want {
		t.Errorf("rock beside the line blocks = %v, want %v", got, want)
	}
}

// TestForestSoftensRatherThanBlocks (#95): forest costs range instead of
// ending the ray — one belt is see-through at short distance and opaque near
// the radius, which is the whole point of "softened".
func TestForestSoftensRatherThanBlocks(t *testing.T) {
	t.Parallel()

	a := protocol.Hex{Q: 0, R: 0}
	trees := flatTerrain(protocol.TerrainForest, protocol.Hex{Q: 1, R: 0}, protocol.Hex{Q: 2, R: 0})

	// Distance 3 through two forest hexes: 3 + 2×2 = 7 > 6, blocked.
	if got, want := sightBlocked(a, protocol.Hex{Q: 3, R: 0}, protocol.CombatRadius, trees), true; got != want {
		t.Errorf("two forest hexes at distance 3 blocked = %v, want %v (3 + 2*%d > %d)",
			got, want, protocol.ForestSightCost, protocol.CombatRadius)
	}

	// The same two hexes at a generous radius are see-through: forest never
	// hard-blocks, it only costs.
	if got, want := sightBlocked(a, protocol.Hex{Q: 3, R: 0}, protocol.CombatRadius*2, trees), false; got != want {
		t.Errorf("two forest hexes at double radius blocked = %v, want %v (softened, never a hard block)", got, want)
	}
}

// TestEndpointTerrainIsIgnored (#95): standing IN forest or against a rock
// face never hides you from something adjacent — only what lies strictly
// between two entities counts.
func TestEndpointTerrainIsIgnored(t *testing.T) {
	t.Parallel()

	a, b := protocol.Hex{Q: 0, R: 0}, protocol.Hex{Q: 1, R: 0}

	for _, kind := range []protocol.Terrain{protocol.TerrainRock, protocol.TerrainForest} {
		if got, want := sightBlocked(a, b, protocol.CombatRadius, flatTerrain(kind, a, b)), false; got != want {
			t.Errorf("adjacent pair both standing on %s blocked = %v, want %v", kind, got, want)
		}
	}
}

// TestSightIsSymmetric (#95): the maintainer's Q4 — we never want "it sees
// you but you don't see it". Asserted over a spread of pairs and obstacles
// rather than one hand-picked case.
func TestSightIsSymmetric(t *testing.T) {
	t.Parallel()

	obstacles := flatTerrain(protocol.TerrainRock, protocol.Hex{Q: 1, R: -1}, protocol.Hex{Q: -2, R: 1})
	trees := flatTerrain(protocol.TerrainForest, protocol.Hex{Q: 0, R: 1}, protocol.Hex{Q: 2, R: -1})

	for _, terrain := range []func(protocol.Hex) protocol.Terrain{obstacles, trees} {
		for q := -4; q <= 4; q++ {
			for r := -4; r <= 4; r++ {
				a, b := protocol.Hex{Q: 0, R: 0}, protocol.Hex{Q: q, R: r}

				ab := sightBlocked(a, b, protocol.CombatRadius, terrain)
				if ba := sightBlocked(b, a, protocol.CombatRadius, terrain); ab != ba {
					t.Errorf("sightBlocked(%v,%v) = %v but reversed = %v — must be symmetric", a, b, ab, ba)
				}
			}
		}
	}
}

// TestWaterIsTransparent (#95): water is unwalkable but you can see across
// it — walkableLocked is deliberately not the sight predicate.
func TestWaterIsTransparent(t *testing.T) {
	t.Parallel()

	a, b := protocol.Hex{Q: 0, R: 0}, protocol.Hex{Q: 4, R: 0}
	lake := flatTerrain(protocol.TerrainWater,
		protocol.Hex{Q: 1, R: 0}, protocol.Hex{Q: 2, R: 0}, protocol.Hex{Q: 3, R: 0})

	if got, want := sightBlocked(a, b, protocol.CombatRadius, lake), false; got != want {
		t.Errorf("sight across water blocked = %v, want %v (unwalkable but transparent)", got, want)
	}
}
