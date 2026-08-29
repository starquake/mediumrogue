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

// newBuriedWorld builds a full-size world on the shared testSeed (radius 24,
// the deployed value) so
// the ring bands and mud coverage match production — burial is a spawn rule
// that depends on both, and a small map collapses toward ring 0.
func newBuriedWorld(t *testing.T) *game.World {
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

	w := newBuriedWorld(t)
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

	w := newBuriedWorld(t)

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

// TestBuriedMonsterNeitherFormsNorJoinsABubble: dormancy's combat half.
//
// Placed at 5 hexes — deliberately INSIDE CombatRadius (6), so an ordinary
// monster there would form a bubble, and OUTSIDE BuriedRevealRadius (4), so
// the reveal pass leaves it alone. That gap is the only distance at which
// dormancy is observable in isolation; adjacent would just be testing the
// reveal.
func TestBuriedMonsterNeitherFormsNorJoinsABubble(t *testing.T) {
	t.Parallel()

	w := newBuriedWorld(t)

	join, err := w.Join("", "alice", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	const dormantDistance = protocol.BuriedRevealRadius + 1 // 5: past the reveal, inside CombatRadius

	at := w.EntityHexForTest(join.EntityID)
	id := w.PlaceMonsterKindForTest(protocol.Hex{Q: at.Q + dormantDistance, R: at.R}, "skeleton")
	w.SetBuriedForTest(id, true)
	w.ResolveTurnForTest()

	if got, want := w.BuriedForTest(id), true; got != want {
		t.Fatalf("still buried at %d hexes = %v, want %v (the test's own premise)", dormantDistance, got, want)
	}

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

// TestBuriedRevealsAtExactlyTheRadiusAndNotBeyond pins both sides of the
// boundary. Only the far case can fail silently — a reveal that never fires
// looks exactly like a world with no ambushers in it.
func TestBuriedRevealsAtExactlyTheRadiusAndNotBeyond(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		distance   int
		wantBuried bool
	}{
		{"inside the radius", protocol.BuriedRevealRadius - 1, false},
		{"exactly at the radius", protocol.BuriedRevealRadius, false},
		{"one hex beyond", protocol.BuriedRevealRadius + 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := newBuriedWorld(t)

			join, err := w.Join("", "alice", protocol.ClassFighter, protocol.SpeciesHuman)
			if err != nil {
				t.Fatalf("Join: %v", err)
			}

			at := w.EntityHexForTest(join.EntityID)
			id := w.PlaceMonsterKindForTest(protocol.Hex{Q: at.Q + tc.distance, R: at.R}, "skeleton")
			w.SetBuriedForTest(id, true)

			w.ResolveTurnForTest()

			if got, want := w.BuriedForTest(id), tc.wantBuried; got != want {
				t.Errorf("buried at distance %d = %v, want %v", tc.distance, got, want)
			}
		})
	}
}

// TestEmergingMonsterCannotActOnTheRevealTurnButCanOnTheNext is the
// counterplay: the crawl-out is a real beat you get to react to. A monster
// that emerged and swung in the same turn would be damage you could not have
// avoided.
func TestEmergingMonsterCannotActOnTheRevealTurnButCanOnTheNext(t *testing.T) {
	t.Parallel()

	w := newBuriedWorld(t)

	join, err := w.Join("", "alice", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	at := w.EntityHexForTest(join.EntityID)
	id := w.PlaceMonsterKindForTest(protocol.Hex{Q: at.Q + protocol.BuriedRevealRadius, R: at.R}, "skeleton")
	w.SetBuriedForTest(id, true)

	before := w.EntityHexForTest(id)

	// The reveal turn: it comes out, and is visible — but frozen mid-crawl.
	w.ResolveTurnForTest()

	if got, want := w.BuriedForTest(id), false; got != want {
		t.Fatalf("buried after the reveal turn = %v, want %v", got, want)
	}

	if got, want := w.EntityHexForTest(id), before; got != want {
		t.Errorf("moved on its own emergence turn: %v -> %v, want it rooted while it climbs out", before, got)
	}

	// The next turn: it is a normal skeleton and closes on the player.
	w.ResolveTurnForTest()

	wasDist := game.HexDistance(before, at)
	if got := game.HexDistance(w.EntityHexForTest(id), at); got >= wasDist {
		t.Errorf("distance to player after the turn AFTER emerging = %d, want < %d (it should act now)", got, wasDist)
	}
}

// TestBuriedNeverRebuiles: once out, out. A monster that could re-bury when
// you walked away would turn every retreat into a reset.
func TestBuriedNeverReburies(t *testing.T) {
	t.Parallel()

	w := newBuriedWorld(t)

	join, err := w.Join("", "alice", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	at := w.EntityHexForTest(join.EntityID)
	id := w.PlaceMonsterKindForTest(protocol.Hex{Q: at.Q + protocol.BuriedRevealRadius, R: at.R}, "skeleton")
	w.SetBuriedForTest(id, true)
	w.ResolveTurnForTest()

	// Teleport it far beyond the reveal radius and keep the clock running.
	w.SetHexForTest(id, protocol.Hex{Q: at.Q + protocol.InterestRadius - 1, R: at.R})

	for range 3 {
		w.ResolveTurnForTest()
	}

	if got, want := w.BuriedForTest(id), false; got != want {
		t.Errorf("re-buried after walking away = %v, want %v — burial is a spawn state, never restored", got, want)
	}
}

// TestBuriedKindCoverageGuard is #436's answer to a risk #437 proved cannot be
// tuned away: mud follows the water, the seed decides where the water goes, so
// roughly 1 world in 200-500 leaves a ring with no mud — and therefore no
// skeletons in it, silently.
//
// seed 17 is one of those worlds, found while measuring #437's tuning across
// 500 seeds. It is a PIN: if mud generation is ever retuned, this seed may
// start passing, and the fix is to RE-DERIVE a failing seed, never to weaken
// the assertion — a guard that no longer has anything to catch is not a guard.
func TestBuriedKindCoverageGuard(t *testing.T) {
	t.Parallel()

	t.Run("a world with mud in every skeleton ring boots", func(t *testing.T) {
		t.Parallel()

		if err := newBuriedWorld(t).ValidateBuriedKindCoverage(); err != nil {
			t.Errorf("ValidateBuriedKindCoverage() on the standard test seed = %v, want nil", err)
		}
	})

	t.Run("a world missing mud in a skeleton ring refuses to boot", func(t *testing.T) {
		t.Parallel()

		w := game.NewWorld(game.WorldConfig{
			Interval:        time.Hour,
			CombatPatience:  testCombatPatience,
			BubblePoll:      testBubblePoll,
			DisconnectGrace: testDisconnectGrace,
			WorldSeed:       17,
			Radius:          24,
			Ticks:           hub.New(),
		})

		err := w.ValidateBuriedKindCoverage()
		if got, want := err, game.ErrRingHasNoMud; !errors.Is(got, want) {
			t.Fatalf("err = %v, want %v", got, want)
		}

		// The message has to be actionable: whoever hits this is an operator
		// with a bad seed, not a developer with a stack trace.
		for _, want := range []string{"skeleton", "ring 1", "seed 17", "WORLD_SEED"} {
			if got := err.Error(); !strings.Contains(got, want) {
				t.Errorf("err.Error() = %q, should contain %q", got, want)
			}
		}
	})
}
