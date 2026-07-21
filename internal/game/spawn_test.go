package game_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// hexesWithinDistance returns every hex whose distance from origin is <= maxDist,
// via the same double-loop scan spawnHexLocked's pre-#36 spiral used — a small,
// deterministic, geometry-independent way for a test to enumerate a bounded
// region without depending on the unexported clearingRadius constant.
func hexesWithinDistance(origin protocol.Hex, maxDist int) []protocol.Hex {
	var out []protocol.Hex

	for q := -maxDist; q <= maxDist; q++ {
		for r := -maxDist; r <= maxDist; r++ {
			h := protocol.Hex{Q: origin.Q + q, R: origin.R + r}
			if game.HexDistance(origin, h) <= maxDist {
				out = append(out, h)
			}
		}
	}

	return out
}

// TestSpawnHexLockedNeverOccupiesALivingMonster (#36a): a player join must
// never land on a hex a living monster already occupies, even when the
// area around the origin is crowded with monsters — every hex within the old
// radius-2 clearing except the origin itself holds one, and every join must
// avoid all of them (landing at the origin or on the sanctuary's outer,
// monster-free hexes).
func TestSpawnHexLockedNeverOccupiesALivingMonster(t *testing.T) {
	t.Parallel()

	w := newWorld()

	origin := protocol.Hex{Q: 0, R: 0}

	occupied := make(map[protocol.Hex]bool)

	for _, h := range hexesWithinDistance(origin, 2) {
		if h == origin || !isWalkable(w, h) {
			continue // leave the origin itself free; skip any non-walkable hex
		}

		w.PlaceMonsterForTest(h)
		occupied[h] = true
	}

	for i := range 5 {
		resp, err := w.Join("", fmt.Sprintf("p%d", i), protocol.ClassFighter, protocol.SpeciesHuman)
		if err != nil {
			t.Fatalf("join %d: %v", i, err)
		}

		if occupied[resp.Hex] {
			t.Errorf("join %d spawned at %v, which a living monster occupies", i, resp.Hex)
		}
	}
}

// TestSpawnHexLockedPrefersOutsideMonsterCombatRadiusWhenPossible (#36a): a
// monster near the sanctuary's edge leaves PART of the spawn area within its
// CombatRadius and part outside it — every join must land on the safe side
// when a safe hex exists at all.
func TestSpawnHexLockedPrefersOutsideMonsterCombatRadiusWhenPossible(t *testing.T) {
	t.Parallel()

	w := newWorld()

	// Five hexes out along one axis, at the sanctuary's boundary: the NEAR
	// side of the spawn area (hexes on the monster's side) falls within
	// CombatRadius (6) while the FAR side (opposite the monster, e.g.
	// distance 7+ hexes like {-2,0}) does not.
	monsterHex := protocol.Hex{Q: 5, R: 0}
	if !isWalkable(w, monsterHex) {
		t.Skip("fixture hex is not walkable on this map")
	}

	w.PlaceMonsterForTest(monsterHex)

	for i := range 10 {
		resp, err := w.Join("", fmt.Sprintf("p%d", i), protocol.ClassFighter, protocol.SpeciesHuman)
		if err != nil {
			t.Fatalf("join %d: %v", i, err)
		}

		if got, want := game.HexDistance(resp.Hex, monsterHex), protocol.CombatRadius; got <= want {
			t.Errorf("join %d spawned at %v, %d hexes from the monster at %v — want > CombatRadius (%d): "+
				"a safe hex exists on the far side of the clearing", i, resp.Hex, got, monsterHex, want)
		}
	}
}

// TestSpawnHexLockedFallsBackWhenSanctuaryFull (#36a): if every hex in the
// sanctuary is at StackCap, spawnHexLocked must fall back beyond it
// (spawnHexSpiralLocked) rather than erroring — a join must still succeed.
// Saturates the full protocol.SanctuaryRadius (re-derived: sanctuary scatter
// (fast-lane T5, Q9)) — capping only the old radius-2 clearing would leave
// unoccupied tier candidates at distance 3-5 and never reach the spiral
// fallback this test exists to exercise.
func TestSpawnHexLockedFallsBackWhenSanctuaryFull(t *testing.T) {
	t.Parallel()

	w := newWorld()

	origin := protocol.Hex{Q: 0, R: 0}

	for _, h := range hexesWithinDistance(origin, protocol.SanctuaryRadius) {
		if !isWalkable(w, h) {
			continue
		}

		for range protocol.StackCap {
			w.PlaceMonsterForTest(h)
		}
	}

	resp, err := w.Join("", "overflow", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("join with a full sanctuary: %v, want a successful fallback spawn", err)
	}

	if got, want := game.HexDistance(resp.Hex, origin), protocol.SanctuaryRadius; got <= want {
		t.Errorf("fallback spawn landed at %v (distance %d from origin), want it to have escaped the full sanctuary",
			resp.Hex, got)
	}
}

// TestSpawnHexLockedRandomDistribution (#36c): two joins on a fresh world (no
// crowding, no monsters) land on different hexes for a fixed seed known to
// exercise it — spawns no longer pile deterministically onto the same tile.
func TestSpawnHexLockedRandomDistribution(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(42)

	a, err := w.Join("", "a", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("join a: %v", err)
	}

	b, err := w.Join("", "b", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("join b: %v", err)
	}

	if got := b.Hex; got == a.Hex {
		t.Errorf("two joins on a fresh world both landed at %v, want the random clearing pick to differ for seed 42", got)
	}
}

// TestSpawnScattersAcrossSanctuary (Q9, fast-lane #36 follow-up): a
// default-gen world with no monsters near the origin must (a) keep every
// spawn within protocol.SanctuaryRadius of the origin, and (b) actually use
// the widened area — across a dozen joins on a fixed seed, at least one must
// land beyond the old clearingRadius (2). Determinism: replaying the same
// seed on a fresh world reproduces the exact same spawn sequence.
func TestSpawnScattersAcrossSanctuary(t *testing.T) {
	t.Parallel()

	origin := protocol.Hex{Q: 0, R: 0}

	const joinCount = 12

	spawnSequence := func(seed int64) []protocol.Hex {
		t.Helper()

		w := newWorld()
		w.SetSeedForTest(seed)

		hexes := make([]protocol.Hex, 0, joinCount)

		for i := range joinCount {
			resp, err := w.Join("", fmt.Sprintf("p%d", i), protocol.ClassFighter, protocol.SpeciesHuman)
			if err != nil {
				t.Fatalf("join %d: %v", i, err)
			}

			hexes = append(hexes, resp.Hex)
		}

		return hexes
	}

	const seed = 7

	first := spawnSequence(seed)

	sawBeyondClearing := false

	for i, h := range first {
		if got, want := game.HexDistance(origin, h), protocol.SanctuaryRadius; got > want {
			t.Errorf("spawn %d at distance %d, want <= %d (SanctuaryRadius)", i, got, want)
		}

		if game.HexDistance(origin, h) > 2 {
			sawBeyondClearing = true
		}
	}

	if !sawBeyondClearing {
		t.Error("no spawn landed beyond the old clearingRadius (2) — scatter not widened to the sanctuary")
	}

	second := spawnSequence(seed)

	if got, want := len(second), len(first); got != want {
		t.Fatalf("replay produced %d spawns, want %d", got, want)
	}

	for i, h := range second {
		if got, want := h, first[i]; got != want {
			t.Errorf("replay spawn %d = %v, want %v (same seed must reproduce the same sequence)", i, got, want)
		}
	}
}

// newSmallWorld builds a radius-3 world — small enough that CombatRadius (6)
// spans the ENTIRE map from any hex, so a living occupant anywhere makes
// every other hex "too close" to it — used to force the guard-empty fallback
// path in SpawnMonsters.
func newSmallWorld() *game.World {
	return game.NewWorld(game.WorldConfig{
		Interval:        time.Hour,
		CombatPatience:  testCombatPatience,
		BubblePoll:      testBubblePoll,
		DisconnectGrace: testDisconnectGrace,
		WorldSeed:       0xC0FFEE,
		Radius:          3,
		Ticks:           hub.New(),
	})
}

// TestSpawnMonstersAvoidsLivingPlayerWhenPossible (#36b): on a normal-sized
// map, monsters spawned after a player has joined must land outside the
// player's CombatRadius when room elsewhere allows it.
func TestSpawnMonstersAvoidsLivingPlayerWhenPossible(t *testing.T) {
	t.Parallel()

	w := newWorld()

	playerID, _ := w.PlaceEntityForTest(protocol.Hex{Q: 0, R: 0})

	w.SpawnMonsters(20)

	snap := w.Snapshot()

	playerHex := entityHex(t, w, playerID)

	for _, e := range snap.Entities {
		if e.Kind != protocol.EntityMonster {
			continue
		}

		if got, want := game.HexDistance(e.Hex, playerHex), protocol.CombatRadius; got <= want {
			t.Errorf("monster %d spawned at %v, %d hexes from the player at %v — want > CombatRadius (%d)",
				e.ID, e.Hex, got, playerHex, want)
		}
	}
}

// TestSpawnMonstersFallsBackWhenNoHexClearsThePlayer (#36b): on a tiny map
// where CombatRadius spans the whole reachable region, EVERY hex is too
// close to the sole player — SpawnMonsters must still place monsters
// (falling back to walkable+capacity, ignoring the guard) instead of placing
// none.
func TestSpawnMonstersFallsBackWhenNoHexClearsThePlayer(t *testing.T) {
	t.Parallel()

	w := newSmallWorld()

	w.PlaceEntityForTest(protocol.Hex{Q: 0, R: 0})

	w.SpawnMonsters(3)

	monsters := 0

	for _, e := range w.Snapshot().Entities {
		if e.Kind == protocol.EntityMonster {
			monsters++
		}
	}

	if got, want := monsters, 3; got != want {
		t.Errorf("monsters placed on a fully-guarded tiny map = %d, want %d (fallback should still place them)", got, want)
	}
}
