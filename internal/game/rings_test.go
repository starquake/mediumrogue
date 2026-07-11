package game_test

import (
	"slices"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestRingOfAtRealRadius pins the spec's exact table at WORLD_RADIUS 24
// (config's default): ring 0 = distance 0-7 (home), ring 1 = 8-15, ring 2 =
// 16-24 (frontier).
func TestRingOfAtRealRadius(t *testing.T) {
	t.Parallel()

	cases := []struct {
		dist int
		want int
	}{
		{0, 0}, {7, 0}, // ring 0: the boundary hex still counts as home
		{8, 1}, {15, 1}, // ring 1
		{16, 2}, {24, 2}, // ring 2: the boundary hex (map rim) still frontier
	}

	for _, tc := range cases {
		h := protocol.Hex{Q: tc.dist, R: 0}
		if got, want := game.RingOfForTest(h, 24), tc.want; got != want {
			t.Errorf("ringOf(dist=%d, radius=24) = %d, want %d", tc.dist, got, want)
		}
	}
}

// TestRingOfAtTinyRadius pins the band math at a small radius (4) — "works
// at test sizes": bands narrow but every ring still gets covered somewhere
// on the map at this size, and the math never panics or leaves [0,RingCount).
func TestRingOfAtTinyRadius(t *testing.T) {
	t.Parallel()

	cases := []struct {
		dist int
		want int
	}{
		{0, 0}, {1, 0},
		{2, 1},
		{3, 2}, {4, 2},
	}

	for _, tc := range cases {
		h := protocol.Hex{Q: tc.dist, R: 0}
		if got, want := game.RingOfForTest(h, 4), tc.want; got != want {
			t.Errorf("ringOf(dist=%d, radius=4) = %d, want %d", tc.dist, got, want)
		}
	}
}

// TestRingOfNeverOutOfRange fuzzes a spread of (distance, worldRadius)
// pairs — including the smallest legal radius (1, config-enforced minimum)
// — and asserts ringOf never panics and always returns a valid ring index.
func TestRingOfNeverOutOfRange(t *testing.T) {
	t.Parallel()

	for radius := 1; radius <= 30; radius++ {
		for dist := 0; dist <= radius; dist++ {
			h := protocol.Hex{Q: dist, R: 0}

			got := game.RingOfForTest(h, radius)
			if got < 0 || got >= protocol.RingCount {
				t.Errorf("ringOf(dist=%d, radius=%d) = %d, want [0,%d)", dist, radius, got, protocol.RingCount)
			}
		}
	}
}

// TestSanctuaryZoneHasNoHostileSpawn: on a normal-sized map (plenty of
// walkable area outside the sanctuary), no monster SpawnMonsters places
// lands within protocol.SanctuaryRadius of the origin.
func TestSanctuaryZoneHasNoHostileSpawn(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SpawnMonsters(30)

	origin := protocol.Hex{Q: 0, R: 0}

	for _, e := range w.Snapshot().Entities {
		if e.Kind != protocol.EntityMonster {
			continue
		}

		if got, want := game.HexDistance(origin, e.Hex), protocol.SanctuaryRadius; got <= want {
			t.Errorf("monster %d spawned at %v, %d hexes from the origin — want > SanctuaryRadius (%d)",
				e.ID, e.Hex, got, want)
		}
	}
}

// TestSanctuaryGuardFallsBackOnTinyMap: mirrors
// TestSpawnMonstersFallsBackWhenNoHexClearsThePlayer (spawn_test.go) for the
// sanctuary guard specifically — on newSmallWorld (radius 3, entirely
// inside SanctuaryRadius 5), SpawnMonsters must still place monsters
// (dropping the sanctuary guard) instead of placing none.
func TestSanctuaryGuardFallsBackOnTinyMap(t *testing.T) {
	t.Parallel()

	w := newSmallWorld()
	w.SpawnMonsters(3)

	monsters := 0

	for _, e := range w.Snapshot().Entities {
		if e.Kind == protocol.EntityMonster {
			monsters++
		}
	}

	if got, want := monsters, 3; got != want {
		t.Errorf("monsters placed on a fully-sanctuary tiny map = %d, want %d (fallback should still place them)",
			got, want)
	}
}

// TestSpawnMonstersRingKindsAreValid: every monster SpawnMonsters places is
// a kind actually registered for the ring its own hex falls into
// (content.go's monsterDefs' own rings field) — proves the ring-aware kind
// pick, not just the ring-aware hex pick.
func TestSpawnMonstersRingKindsAreValid(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(7)
	w.SpawnMonsters(40)

	for _, e := range w.Snapshot().Entities {
		if e.Kind != protocol.EntityMonster {
			continue
		}

		kind := w.MonsterKindForTest(e.ID)

		ring := game.RingOfForTest(e.Hex, 12) // newWorld's radius
		rings := game.MonsterRingsForTest(kind)

		if !slices.Contains(rings, ring) {
			t.Errorf("monster %d (%s) at %v is in ring %d, but %s only spawns in rings %v",
				e.ID, kind, e.Hex, ring, kind, rings)
		}
	}
}

// TestSpawnMonstersDistributesAcrossRings: a large-enough spawn call
// actually reaches more than one ring — proves the ring-weighted
// distribution isn't accidentally collapsing everything into a single
// ring. (newWorld's radius 12 splits into ring 0 = dist 0-3, ring 1 = 4-7,
// ring 2 = 8-11ish — see TestRingOfNeverOutOfRange for the general math;
// ring 0 is entirely inside SanctuaryRadius 5, so only rings 1 and 2 are
// ever populated on this test map, which is the assertion below.)
func TestSpawnMonstersDistributesAcrossRings(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(99)
	w.SpawnMonsters(60)

	seen := make(map[int]bool)

	for _, e := range w.Snapshot().Entities {
		if e.Kind != protocol.EntityMonster {
			continue
		}

		seen[game.RingOfForTest(e.Hex, 12)] = true
	}

	if len(seen) < 2 {
		t.Errorf("60 monsters landed in only %d distinct ring(s) (%v), want at least 2 — "+
			"distribution looks collapsed onto one ring", len(seen), seen)
	}
}

// TestSpawnMonstersDragonCapped: however many monsters are placed, dragons
// never exceed protocol.DragonCount for the whole world.
func TestSpawnMonstersDragonCapped(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(123)
	w.SpawnMonsters(200)

	dragons := 0

	for _, e := range w.Snapshot().Entities {
		if e.Kind == protocol.EntityMonster && w.MonsterKindForTest(e.ID) == "dragon" {
			dragons++
		}
	}

	if got, want := dragons, protocol.DragonCount; got > want {
		t.Errorf("dragons placed = %d, want <= %d (protocol.DragonCount)", got, want)
	}
}

// TestDragonCapIsPerWorldNotPerCall: the DragonCount cap counts dragons
// already alive in the world — one pre-seeded via SpawnMonsterKindAt plus a
// large SpawnMonsters call (and a second SpawnMonsters call on top) must
// never exceed the cap, since the future continuous/density spawner will
// call SpawnMonsters repeatedly mid-run.
func TestDragonCapIsPerWorldNotPerCall(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(123) // the seed TestSpawnMonstersDragonCapped proves places a dragon at n=200

	// Pre-seed one dragon directly, as a test or a future spawner might.
	origin := protocol.Hex{Q: 0, R: 0}

	placed := false

	for _, h := range hexesWithinDistance(origin, 3) {
		if isWalkable(w, h) && w.SpawnMonsterKindAt(h, "dragon") {
			placed = true

			break
		}
	}

	if !placed {
		t.Fatal("could not pre-seed a dragon near the origin")
	}

	w.SpawnMonsters(200)
	w.SpawnMonsters(200) // a second call must also see the cap already met

	dragons := 0

	for _, e := range w.Snapshot().Entities {
		if e.Kind == protocol.EntityMonster && w.MonsterKindForTest(e.ID) == "dragon" {
			dragons++
		}
	}

	if got, want := dragons, protocol.DragonCount; got > want {
		t.Errorf("dragons in the world = %d, want <= %d (per-WORLD cap, across calls)", got, want)
	}
}

// TestSpawnMonstersRingPlacementReproducibleForSameSeed mirrors
// TestSpawnMonstersIsReproducibleForSameSeed (monster_test.go) at the
// per-kind level: the same seed reproduces the exact same (hex, kind) pairs.
func TestSpawnMonstersRingPlacementReproducibleForSameSeed(t *testing.T) {
	t.Parallel()

	kindsForSeed := func(seed int64) map[protocol.Hex]string {
		w := newWorld()
		w.SetSeedForTest(seed)
		w.SpawnMonsters(20)

		out := make(map[protocol.Hex]string)

		for _, e := range w.Snapshot().Entities {
			if e.Kind == protocol.EntityMonster {
				out[e.Hex] = w.MonsterKindForTest(e.ID)
			}
		}

		return out
	}

	a := kindsForSeed(55)
	again := kindsForSeed(55)

	if len(a) != len(again) {
		t.Fatalf("same seed produced different monster counts: %d vs %d", len(a), len(again))
	}

	for hex, kind := range a {
		if got, want := again[hex], kind; got != want {
			t.Errorf("hex %v: kind %q on the second run, want %q (same seed 55)", hex, got, want)
		}
	}
}
