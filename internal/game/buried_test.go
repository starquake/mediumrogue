package game_test

import (
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// newBuriedWorld builds a full-size world (radius 24, the deployed value) so
// the ring bands and mud coverage match production — burial is a spawn rule
// that depends on both, and a small map collapses toward ring 0.
func newBuriedWorld(t *testing.T, seed uint64) *game.World {
	t.Helper()

	return game.NewWorld(game.WorldConfig{
		Interval:        time.Hour,
		CombatPatience:  testCombatPatience,
		BubblePoll:      testBubblePoll,
		DisconnectGrace: testDisconnectGrace,
		WorldSeed:       seed,
		Radius:          24,
		Ticks:           hub.New(),
	})
}

// buried_test.go (#436): skeletons spawn buried in mud and crawl out when a
// player gets close. The mechanic is a spawn state plus three rules that must
// hold together — dormant, off the wire, and one turn to emerge.

// TestBuriedIsOnlySetForOptedInKinds: burial is a monsterDef property, not a
// skeleton special case (the wildfire gate), and equally it must never appear
// on a kind that did not ask for it.
func TestBuriedIsOnlySetForOptedInKinds(t *testing.T) {
	t.Parallel()

	for _, kind := range game.MonsterKindIDsForTest() {
		buries := game.KindBuriesOnSpawnForTest(kind)
		if kind == "skeleton" && !buries {
			t.Error("skeleton should bury on spawn (#436)")
		}

		if kind != "skeleton" && buries {
			t.Errorf("kind %q buries on spawn, but only the skeleton opts in today", kind)
		}
	}
}

// TestBuriedMonsterSpawnsOnMudOnly: a burying kind is placed on mud and
// nowhere else, which is what makes "a stretch of empty ground is no longer
// proof that it is empty" true only where the ground is a bog.
func TestBuriedMonsterSpawnsOnMudOnly(t *testing.T) {
	t.Parallel()

	w := newBuriedWorld(t, 0xC0FFEE)
	w.SpawnMonsters(120)

	seen := 0

	for _, id := range w.MonsterIDsForTest() {
		if w.MonsterKindForTest(id) != "skeleton" {
			continue
		}

		seen++

		if got, want := w.TerrainAtForTest(w.EntityHexForTest(id)), protocol.TerrainMud; got != want {
			t.Errorf("skeleton %d spawned on %q, want %q — skeletons are a mud-only kind (#436)", id, got, want)
		}

		if got, want := w.BuriedForTest(id), true; got != want {
			t.Errorf("skeleton %d buried = %v, want %v — all skeletons spawn buried", id, got, want)
		}
	}

	if seen == 0 {
		t.Skip("no skeletons placed on this seed; nothing to assert")
	}
}
