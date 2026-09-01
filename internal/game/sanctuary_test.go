package game_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// sanctuary_test.go (#460): joining a fresh world must not put you inside
// combat range of a monster.
//
// The assertion is deliberately about the PLAYER, not about the origin. An
// earlier test elsewhere asserted "no monster within SanctuaryRadius of the
// origin" — which was true, and passed, while joining still started a fight,
// because players spawn anywhere within the sanctuary rather than at its
// centre. Testing a proxy for the property instead of the property is what let
// this ship.

func newSanctuaryWorld(t *testing.T, radius int) *game.World {
	t.Helper()

	return game.NewWorld(game.WorldConfig{
		Interval:        time.Hour,
		CombatPatience:  time.Hour,
		BubblePoll:      time.Hour,
		DisconnectGrace: time.Hour,
		WorldSeed:       testSeed,
		Radius:          radius,
		Ticks:           hub.New(),
	})
}

// TestJoiningPlayerStartsOutOfCombatRange is the regression test for #460.
//
// The arithmetic that caused it: monsters were excluded within SanctuaryRadius
// (5) of the origin, players spawn anywhere within that same radius, and combat
// starts at CombatRadius (6) — so a monster at distance 6 sat legally outside
// the sanctuary and one hex from a player at its edge. spawnHexLocked prefers
// clear hexes but falls through to a tier that ignores the check when none
// qualify, which at a realistic MONSTER_COUNT was every time.
//
// Measured before the fix at radius 120 / 1000 monsters: 25 of 25 joins landed
// within CombatRadius, the closest one hex away.
func TestJoiningPlayerStartsOutOfCombatRange(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ radius, monsters int }{
		{radius: 120, monsters: 1000}, // the deployed configuration
		{radius: 24, monsters: 200},
	} {
		w := newSanctuaryWorld(t, tc.radius)
		w.SpawnMonsters(tc.monsters)

		for i := range 25 {
			join, err := w.Join("", fmt.Sprintf("p%d", i), protocol.ClassFighter, protocol.SpeciesHuman)
			if err != nil {
				t.Fatalf("radius %d: join %d: %v", tc.radius, i, err)
			}

			got, ok := w.NearestMonsterDistanceForTest(join.EntityID)
			if ok && got <= protocol.CombatRadius {
				t.Errorf("radius %d: player %d joined %d hexes from a monster, want more than CombatRadius (%d)",
					tc.radius, i, got, protocol.CombatRadius)
			}
		}
	}
}

// TestJoiningRunningWorldStartsOutOfCombatRange is the SECOND half of #460,
// and the half the first fix missed. @starquake, 2026-09-01, after the
// spawn-time guard had landed: "It happens way less often but I still have it
// happen once."
//
// The test above joins a world the instant it is populated, so it only ever
// exercised the SPAWN-time guard. Nothing kept the sanctuary clear afterwards:
// idle drift had no sanctuary bound, so monsters wandered in and stayed.
// Measured on the deployed shape (radius 120, 1000 monsters, seed 42) before
// the fix — 0 monsters inside the exclusion at turn 0, 2 by turn 100, 5 by
// turn 300, which is only ~20 minutes at the 4s turn. A server that has been
// up for hours accumulates them, which is why this reproduced for a player and
// not for the suite.
//
// So: run the world FIRST, then join. Same property, aged world.
func TestJoiningRunningWorldStartsOutOfCombatRange(t *testing.T) {
	t.Parallel()

	const turns = 400

	w := newSanctuaryWorld(t, 24)
	w.SpawnMonsters(200)

	for range turns {
		w.ResolveTurnForTest()
	}

	for i := range 25 {
		join, err := w.Join("", fmt.Sprintf("late%d", i), protocol.ClassFighter, protocol.SpeciesHuman)
		if err != nil {
			t.Fatalf("join %d: %v", i, err)
		}

		got, ok := w.NearestMonsterDistanceForTest(join.EntityID)
		if ok && got <= protocol.CombatRadius {
			t.Errorf("after %d turns, player %d joined %d hexes from a monster, want more than CombatRadius (%d)",
				turns, i, got, protocol.CombatRadius)
		}
	}
}
