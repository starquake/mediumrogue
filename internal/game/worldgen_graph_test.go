package game //nolint:testpackage // white-box: exercises the unexported graph generator; see sight_test.go.

import (
	"fmt"
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

// TestJoiningPlayerIsNotInstantlyAttacked is the assertion that matters, and
// it replaces a weaker one that passed while the game was broken.
//
// The first version of this test asserted "no monster within SanctuaryRadius of
// the ORIGIN". That passed — and @starquake still got attacked the moment they
// joined, because it tested the wrong thing. Players spawn anywhere within the
// sanctuary, combat starts at CombatRadius, and a monster at distance 6 is
// legally outside the sanctuary while being one hex from a player at its edge.
//
// So this joins real players and asks the real question. Both generators are
// covered because the bug was in NEITHER: it reproduced identically on the
// noise world, 25 of 25 joins inside combat range.
//
//nolint:paralleltest // t.Setenv selects the generator and cannot be parallel.
func TestJoiningPlayerIsNotInstantlyAttacked(t *testing.T) {
	for _, mode := range []string{"graph", "noise"} {
		t.Setenv("WORLDGEN", mode)
		assertJoinsAreSafe(t, mode, 120, 1000)
		assertJoinsAreSafe(t, mode, 24, 200)
	}
}

func assertJoinsAreSafe(t *testing.T, mode string, radius, monsters int) {
	t.Helper()

	const joins = 25

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

	for i := range joins {
		join, err := w.Join("", fmt.Sprintf("p%d", i), protocol.ClassFighter, protocol.SpeciesHuman)
		if err != nil {
			t.Fatalf("%s radius %d: join %d: %v", mode, radius, i, err)
		}

		if d, ok := nearestMonsterDistance(w, join.EntityID); ok && d <= protocol.CombatRadius {
			t.Errorf("%s radius %d: player %d joined %d hexes from a monster, inside CombatRadius (%d)",
				mode, radius, i, d, protocol.CombatRadius)
		}
	}
}

// nearestMonsterDistance reports how far the nearest living monster is from an
// entity, and whether there was one at all.
func nearestMonsterDistance(w *World, id int64) (int, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	self, ok := w.entities[id]
	if !ok {
		return 0, false
	}

	nearest, found := 0, false

	for _, e := range w.entities {
		if e.kind != protocol.EntityMonster || e.hp <= 0 {
			continue
		}

		if d := HexDistance(self.hex, e.hex); !found || d < nearest {
			nearest, found = d, true
		}
	}

	return nearest, found
}
