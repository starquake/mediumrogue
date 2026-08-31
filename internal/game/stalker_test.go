package game_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// stalker_test.go (#438): the Woodwose advances only while no player can see
// it. The geometry is PINNED in every case here — player, monster, and the
// ground between them — for the reason #436's emergence test learned the hard
// way: Join spawns the player at a random hex, and a test that measures
// MOVEMENT against that is really measuring whichever terrain happened to be
// in the way.

func newStalkerWorld(t *testing.T) *game.World {
	t.Helper()

	return game.NewWorld(game.WorldConfig{
		Interval:        time.Hour,
		CombatPatience:  testCombatPatience,
		BubblePoll:      testBubblePoll,
		DisconnectGrace: testDisconnectGrace,
		WorldSeed:       testSeed,
		Radius:          24,
		Ticks:           hub.New(),
	})
}

// stalkerLane places a player at the origin and a Woodwose `distance` hexes
// east, with clear grass between them, then optionally turns the hexes named
// by `forestAt` into trees. Returns the monster's id and its starting hex.
//
// The two sight budgets are what every case here turns on, so they are worth
// stating once: a player sees a monster at CombatRadius (6), while the monster
// aggroes at its OWN reach (aggroRadius 8, via sightBlockedLocked). Both pay
// ForestSightCost (2) per intervening tree, so a belt of trees shifts the
// whole "aggroed but unseen" band closer rather than removing it.
func stalkerLane(t *testing.T, w *game.World, distance int, forestAt ...int) (int64, protocol.Hex) {
	t.Helper()

	join, err := w.Join("", "alice", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	player := protocol.Hex{Q: 0, R: 0}

	w.SetTerrainForTest(player, protocol.TerrainGrass)

	for q := 1; q <= distance; q++ {
		w.SetTerrainForTest(protocol.Hex{Q: q, R: 0}, protocol.TerrainGrass)
	}

	for _, q := range forestAt {
		w.SetTerrainForTest(protocol.Hex{Q: q, R: 0}, protocol.TerrainForest)
	}

	w.SetHexForTest(join.EntityID, player)

	start := protocol.Hex{Q: distance, R: 0}

	return w.PlaceMonsterKindForTest(start, "woodwose"), start
}

// TestStalkerHoldsStillWhileSeen: the identity. On open grass at exactly
// CombatRadius the player can see it, so it must not gain ground. Pair this
// with TestForestLetsAStalkerCloseFurther, which is the SAME distance with one
// tree added.
func TestStalkerHoldsStillWhileSeen(t *testing.T) {
	t.Parallel()

	w := newStalkerWorld(t)
	id, start := stalkerLane(t, w, protocol.CombatRadius)

	w.ResolveTurnForTest()

	if got, want := w.EntityHexForTest(id), start; got != want {
		t.Errorf("moved while watched: %v -> %v, want it held still", start, got)
	}
}

// TestStalkerAdvancesWhileUnseen: beyond sight it closes like any monster.
// 8 hexes is inside the Woodwose's aggroRadius (8) and outside CombatRadius
// (6), which is the only band where "aggroed but unseen" exists at all.
func TestStalkerAdvancesWhileUnseen(t *testing.T) {
	t.Parallel()

	w := newStalkerWorld(t)
	id, start := stalkerLane(t, w, 8)

	w.ResolveTurnForTest()

	player := protocol.Hex{Q: 0, R: 0}
	if got, want := game.HexDistance(w.EntityHexForTest(id), player), game.HexDistance(start, player); got >= want {
		t.Errorf("distance after a turn unseen = %d, want < %d (it should close when nobody is looking)", got, want)
	}
}

// TestForestLetsAStalkerCloseFurther is the point of the whole design: forest
// is the only terrain that already carried a combat rule, and this turns it
// into a decision.
//
// The same 6 hexes as TestStalkerHoldsStillWhileSeen, with ONE tree added
// between. That single hex costs ForestSightCost (2) against both budgets:
// the player's sight becomes 6+2 = 8 > CombatRadius, so they lose it — while
// the Woodwose's own reach is 8, so 6+2 = 8 still aggroes. It closes.
//
// One tree, not a belt: at four the cost is 8 and BOTH budgets fail, so the
// creature is blinded too and simply wanders. That was the first draft of this
// test, and it failed for exactly that reason — worth stating, because "more
// forest is more stalking" is the intuitive and wrong reading.
func TestForestLetsAStalkerCloseFurther(t *testing.T) {
	t.Parallel()

	w := newStalkerWorld(t)
	id, start := stalkerLane(t, w, protocol.CombatRadius, 3)

	w.ResolveTurnForTest()

	player := protocol.Hex{Q: 0, R: 0}
	if got, want := game.HexDistance(w.EntityHexForTest(id), player), game.HexDistance(start, player); got >= want {
		t.Errorf("distance after a turn behind one tree = %d, want < %d — trees are what let it close", got, want)
	}
}

// TestOnlyOptedInKindsStalk: movesOnlyUnseen is a def property, so exactly one
// kind has it today and nothing else changed behaviour.
func TestOnlyOptedInKindsStalk(t *testing.T) {
	t.Parallel()

	for _, kind := range game.MonsterKindIDsForTest() {
		stalks := game.KindMovesOnlyUnseenForTest(kind)
		if want := kind == "woodwose"; stalks != want {
			t.Errorf("kind %q movesOnlyUnseen = %v, want %v", kind, stalks, want)
		}
	}
}

// TestSpawnTerrainCoverageGuardNamesTheKind: #436's guard, generalised. Seed
// 472 has no ring-1 mud, so the skeleton is the one it catches — a PIN,
// re-derived (never weakened) whenever terrain generation is retuned. Seed 17
// held this role until the graph generator (#458) landed and gave it ring-1
// mud; 472 is the new smallest seed that fails, measured by scanning 1..8000
// at radius 24, where 27 seeds fail on the skeleton and 65 on the woodwose.
func TestSpawnTerrainCoverageGuardNamesTheKind(t *testing.T) {
	t.Parallel()

	w := game.NewWorld(game.WorldConfig{
		Interval:        time.Hour,
		CombatPatience:  testCombatPatience,
		BubblePoll:      testBubblePoll,
		DisconnectGrace: testDisconnectGrace,
		WorldSeed:       472,
		Radius:          24,
		Ticks:           hub.New(),
	})

	err := w.ValidateSpawnTerrainCoverage()
	if got, want := err, game.ErrRingLacksSpawnTerrain; !errors.Is(got, want) {
		t.Fatalf("err = %v, want %v", got, want)
	}

	for _, want := range []string{"skeleton", "mud", "ring 1", "seed 472", "WORLD_SEED"} {
		if got := err.Error(); !strings.Contains(got, want) {
			t.Errorf("err.Error() = %q, should contain %q", got, want)
		}
	}
}
