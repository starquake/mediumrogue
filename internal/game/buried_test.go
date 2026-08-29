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

// TestBuriedMonsterIsAbsentFromTheWire reads the RAW turn bundle, not the
// client's view of it. That is the whole point: a buried monster is omitted
// server-side, so nothing a client — or anyone reading the SSE stream —
// receives can reveal it. A "sent but flagged hidden" design would pass a
// client-side assertion and fail this one.
func TestBuriedMonsterIsAbsentFromTheWire(t *testing.T) {
	t.Parallel()

	w := newBuriedWorld(t, 0xC0FFEE)

	join, err := w.Join("", "alice", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	// Right next to the player, so only burial can be keeping it off the wire
	// — not the interest cull, not line of sight.
	at := w.EntityHexForTest(join.EntityID)
	id := w.PlaceMonsterKindForTest(protocol.Hex{Q: at.Q + 1, R: at.R}, "skeleton")

	if got, want := entityOnWire(w.SnapshotFor(join.Token), id), true; got != want {
		t.Fatalf("unburied monster on the wire = %v, want %v (the test's own premise)", got, want)
	}

	w.SetBuriedForTest(id, true)

	if got, want := entityOnWire(w.SnapshotFor(join.Token), id), false; got != want {
		t.Errorf("buried monster on the wire = %v, want %v — it must be omitted entirely", got, want)
	}
}

// TestBuriedMonsterNeitherFormsNorJoinsABubble: dormancy's combat half. A
// buried monster adjacent to a player must not drag them into a fight.
func TestBuriedMonsterNeitherFormsNorJoinsABubble(t *testing.T) {
	t.Parallel()

	w := newBuriedWorld(t, 0xC0FFEE)

	join, err := w.Join("", "alice", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	at := w.EntityHexForTest(join.EntityID)
	id := w.PlaceMonsterKindForTest(protocol.Hex{Q: at.Q + 1, R: at.R}, "skeleton")
	w.SetBuriedForTest(id, true)
	w.ResolveTurnForTest()

	if got, want := inCombatOnWire(w.SnapshotFor(join.Token), join.EntityID), false; got != want {
		t.Errorf("player in combat with a buried monster = %v, want %v", got, want)
	}
}

// entityOnWire reports whether an entity id appears in a rendered bundle.
func entityOnWire(ev protocol.TurnEvent, id int64) bool {
	for _, e := range ev.Entities {
		if e.ID == id {
			return true
		}
	}

	return false
}

// inCombatOnWire reads a player's own InCombat flag out of a rendered bundle,
// which is how a client learns it — no test-only accessor needed.
func inCombatOnWire(ev protocol.TurnEvent, id int64) bool {
	for _, e := range ev.Entities {
		if e.ID == id {
			return e.InCombat
		}
	}

	return false
}
