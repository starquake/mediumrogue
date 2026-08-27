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

// mudSeeds is the spread the generation guards run over. Fixed rather than
// random: a flaky world-generation test would be unfalsifiable, and these
// numbers are the ones the tuning below was measured against.
var mudSeeds = []uint64{0xC0FFEE, 1, 2, 42, 7, 1234, 99999, 31337, 555, 8080}

// mudStats counts mud against walkable land, and per difficulty ring, for one
// generated world.
func mudStats(seed uint64, radius int) (mudPct float64, perRing map[int]int) {
	m := game.GenerateMap(seed, radius)
	perRing = map[int]int{}

	land, mud := 0, 0

	for _, tile := range m.Tiles {
		if tile.Terrain != protocol.TerrainWater && tile.Terrain != protocol.TerrainRock {
			land++
		}

		if tile.Terrain == protocol.TerrainMud {
			mud++
			perRing[game.RingOfForTest(tile.Hex, radius)]++
		}
	}

	return 100 * float64(mud) / float64(land), perRing
}

// TestMudCoverageIsOccasional pins how MUCH mud generation produces, so a
// later tweak to mudLevel cannot silently flood the world or erase mud
// altogether. The decision (#437) was "occasional patches" — measured at
// ~9% of walkable land on this seed spread, against forest's ~27%.
//
// Bands, not exact values: mud varies a lot seed to seed (5%–15% across
// these ten), which is the terrain following the water rather than a bug.
// From #436 this knob also sets how many skeletons the world has, so moving
// it is a balance change and should have to update this test deliberately.
func TestMudCoverageIsOccasional(t *testing.T) {
	t.Parallel()

	const (
		radius     = 24
		perSeedMin = 1.0
		perSeedMax = 30.0
		meanMin    = 6.0
		meanMax    = 13.0
	)

	sum := 0.0

	for _, seed := range mudSeeds {
		pct, _ := mudStats(seed, radius)
		sum += pct

		if pct < perSeedMin || pct > perSeedMax {
			t.Errorf("seed %d: mud = %.2f%% of land, want within [%.1f, %.1f]", seed, pct, perSeedMin, perSeedMax)
		}
	}

	if mean := sum / float64(len(mudSeeds)); mean < meanMin || mean > meanMax {
		t.Errorf("mean mud across %d seeds = %.2f%% of land, want within [%.1f, %.1f] — "+
			"retune mudLevel, or update this band deliberately if the change is intended",
			len(mudSeeds), mean, meanMin, meanMax)
	}
}

// TestMudNeverGeneratesInTheHomeClearing: terrainAt returns from the clearing
// before it samples any noise, so the spawn circle is always plain grass. It
// matters beyond tidiness — #436 buries ambushers in mud, and the hexes a
// player spawns on must not be able to hide one.
func TestMudNeverGeneratesInTheHomeClearing(t *testing.T) {
	t.Parallel()

	const radius = 24

	origin := protocol.Hex{Q: 0, R: 0}

	for _, seed := range mudSeeds {
		for _, tile := range game.GenerateMap(seed, radius).Tiles {
			if game.HexDistance(origin, tile.Hex) <= game.ClearingRadiusForTest() &&
				tile.Terrain == protocol.TerrainMud {
				t.Errorf("seed %d: mud at %v, inside the home clearing", seed, tile.Hex)
			}
		}
	}
}

// TestMudCoversEverySkeletonRing is #437's guard for #436.
//
// #436 makes skeletons spawn ONLY in mud, and the Skeleton's rings are {1, 2}.
// Mud follows the water, which the seed decides — so a world whose low ground
// all sits in ring 0 would silently have no skeletons in ring 1. Nothing would
// fail; the kind would just quietly not be there.
//
// WHAT THIS PROVES, EXACTLY: that the tuned mudLevel puts mud in rings 1 and 2
// for these ten seeds. It is a regression guard on the tuning, and it is NOT a
// guarantee about an arbitrary world. Measured over 500 seeds while tuning,
// roughly 1 in 200–500 still comes up with an empty ring 1, and raising
// mudLevel far enough to erase that (past 0.50) makes mud stop reading as
// occasional patches without ever reaching zero risk. That residual is a real
// consequence of deriving terrain from noise and is #436's to handle — see the
// issue thread; do not "fix" it by widening the band here.
//
// #436 should REPLACE this with the same assertion driven by the mud-only
// kind's own rings field, so retuning the skeleton cannot outdate the guard.
func TestMudCoversEverySkeletonRing(t *testing.T) {
	t.Parallel()

	const radius = 24

	// Hardcoded here because no kind is mud-only until #436 lands; that is
	// the ticket that makes this readable from the registry instead.
	wantRings := []int{1, 2}

	for _, seed := range mudSeeds {
		_, perRing := mudStats(seed, radius)

		for _, ring := range wantRings {
			if got, want := perRing[ring] > 0, true; got != want {
				t.Errorf("seed %d: ring %d has no mud (counts %v) — a mud-only kind would be silently absent there",
					seed, ring, perRing)
			}
		}
	}
}
