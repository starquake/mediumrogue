package game //nolint:testpackage // white-box: exercises the unexported graph generator; see sight_test.go.

import (
	"os"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/hub"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// worldgen_graph_test.go (#458 experiment): the graph generator's own tests.
//
// White-box and opting explicitly back into the graph generator, because
// TestMain pins the rest of this package to the noise world — see its comment
// for why. These call generateGraphMap directly rather than going through
// GenerateMap, so they are unaffected by that pin.

// graphWalkable returns the walkable set of a generated graph world.
func graphWalkable(m protocol.MapResponse) map[protocol.Hex]bool {
	walk := make(map[protocol.Hex]bool, len(m.Tiles))

	for _, t := range m.Tiles {
		if terrainWalkable(t.Terrain) {
			walk[t.Hex] = true
		}
	}

	return walk
}

// TestGraphWorldIsFullyConnected is the property the whole approach rests on.
// Threshold-tuning noise was rejected because squeezing it to 40% walkable
// collapsed connectivity to 0.1% — the origin stranded on an island. A
// spanning tree rooted at the origin is connected BY CONSTRUCTION, and this is
// what pins that claim.
func TestGraphWorldIsFullyConnected(t *testing.T) {
	t.Parallel()

	for _, radius := range []int{24, 60, 120} {
		m := generateGraphMap(testGraphSeed, radius)

		walk := graphWalkable(m)
		reach := reachableWalkable(m)

		if got, want := len(reach), len(walk); got != want {
			t.Errorf("radius %d: %d of %d walkable hexes reachable from the origin, want all",
				radius, got, want)
		}
	}
}

// TestGraphWorldLeavesNegativeSpace: the point of the generator. The noise
// world is 82.8% walkable with no impassable terrain doing structural work.
func TestGraphWorldLeavesNegativeSpace(t *testing.T) {
	t.Parallel()

	const (
		radius  = 120
		maxWalk = 0.70 // today's noise world is 0.83 — anything near it is not structure
	)

	m := generateGraphMap(testGraphSeed, radius)
	if got := float64(len(graphWalkable(m))) / float64(len(m.Tiles)); got > maxWalk {
		t.Errorf("walkable = %.1f%%, want <= %.0f%% — impassable terrain must shape movement",
			got*100, maxWalk*100)
	}
}

// TestGraphNodeCountsScaleWithArea pins the bug that made the generator work at
// exactly one world size: node counts were absolute, so blobs sized for radius
// 120 swamped a radius-24 map and produced 91.4% walkable — MORE open than the
// world being replaced.
//
// Small worlds are exempt by construction and that is honest: a radius-10 world
// is 19 hexes across and cannot hold structure whose features are 9-21 hexes
// wide. It is also the e2e world, which wants open predictable space.
func TestGraphNodeCountsScaleWithArea(t *testing.T) {
	t.Parallel()

	const maxWalk = 0.70

	for _, radius := range []int{20, 24, 60} {
		m := generateGraphMap(testGraphSeed, radius)
		if got := float64(len(graphWalkable(m))) / float64(len(m.Tiles)); got > maxWalk {
			t.Errorf("radius %d: walkable = %.1f%%, want <= %.0f%% — counts must scale with ring area",
				radius, got*100, maxWalk*100)
		}
	}
}

// TestGraphWorldSatisfiesTheBootGuard: ValidateSpawnTerrainCoverage (#436/#438)
// refuses to start a world whose rings lack the terrain a confined kind spawns
// on. A graph world carves a fraction of the map, so a biome merely uncommon
// world-wide can be absent from a whole ring — which is why graphTerrainAt's
// thresholds are more generous than the noise generator's. Without this the
// server would not boot at all.
func TestGraphWorldSatisfiesTheBootGuard(t *testing.T) {
	t.Parallel()

	for _, def := range monsterDefs {
		if def.spawnTerrain == "" {
			continue
		}

		m := generateGraphMap(testGraphSeed, 120)
		found := make(map[int]bool)

		for _, tile := range m.Tiles {
			if tile.Terrain == def.spawnTerrain {
				found[ringOf(tile.Hex, 120)] = true
			}
		}

		for _, r := range def.rings {
			if !found[r] {
				t.Errorf("kind %q spawns on %q in ring %d, which a graph world has none of",
					def.id, def.spawnTerrain, r)
			}
		}
	}
}

// TestGraphTuningComesFromEnv: every knob is an env var so a deployed world can
// be retuned by restarting the container rather than rebuilding the image.
func TestGraphTuningComesFromEnv(t *testing.T) {
	t.Setenv("WORLDGEN_LOOPS", "99")

	if got, want := graphTuningFromEnv().loops, 99; got != want {
		t.Errorf("loops = %d, want %d", got, want)
	}

	if err := os.Unsetenv("WORLDGEN_LOOPS"); err != nil {
		t.Fatal(err)
	}

	if got, want := graphTuningFromEnv().loops, defLoops; got != want {
		t.Errorf("loops without env = %d, want the default %d", got, want)
	}
}

const (
	testGraphSeed     = 42
	testGraphInterval = time.Hour
)

func testGraphHub() *hub.Hub { return hub.New() }

// TestGraphWorldKeepsTheSanctuaryClear: no monster spawns within
// protocol.SanctuaryRadius of the origin, which is where every player arrives.
//
// Worth pinning on THIS generator specifically. tooCloseToSanctuaryLocked is
// only a preference: spawnCandidatesByRingLocked drops both spawn guards
// entirely when no walkable hex satisfies them, so a world whose walkable space
// is scarce or badly placed could legitimately spawn monsters on the doorstep.
// A graph world carves a fraction of the map, so that fallback is far closer to
// reach than it is with the noise generator.
//
// Not parallel: it selects the graph generator through the environment, which
// TestMain otherwise pins to noise.
func TestGraphWorldKeepsTheSanctuaryClear(t *testing.T) {
	t.Setenv("WORLDGEN", "graph")

	const monsters = 1000 // the deployed MONSTER_COUNT

	// Every size that actually runs. Small worlds matter MORE here: the
	// fallback drops both spawn guards when no walkable hex satisfies them, and
	// scarce walkable space is what brings that within reach.
	for _, radius := range []int{10, 24, 120} {
		assertSanctuaryClear(t, radius, monsters)
	}
}

func assertSanctuaryClear(t *testing.T, radius, monsters int) {
	t.Helper()

	w := NewWorld(WorldConfig{
		Interval:        testGraphInterval,
		CombatPatience:  testGraphInterval,
		BubblePoll:      testGraphInterval,
		DisconnectGrace: testGraphInterval,
		WorldSeed:       testGraphSeed,
		Radius:          radius,
		Ticks:           testGraphHub(),
	})
	w.SpawnMonsters(monsters)

	origin := protocol.Hex{Q: 0, R: 0}
	spawned, inside := 0, 0

	w.mu.Lock()
	for _, e := range w.entities {
		if e.kind != protocol.EntityMonster {
			continue
		}

		spawned++

		if HexDistance(origin, e.hex) <= protocol.SanctuaryRadius {
			inside++

			t.Errorf("radius %d: monster %d spawned at %v, %d hexes from the origin — inside the sanctuary (%d)",
				radius, e.id, e.hex, HexDistance(origin, e.hex), protocol.SanctuaryRadius)
		}
	}
	w.mu.Unlock()

	if spawned == 0 {
		t.Fatalf("radius %d: no monsters spawned; the assertion is vacuous", radius)
	}

	t.Logf("radius %d: %d monsters spawned, %d inside the sanctuary", radius, spawned, inside)
}
