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
// origin clearing is otherwise crowded with monsters — only one clearing hex
// (the origin itself) is left unoccupied, and every join must land there,
// never on one of the monster hexes.
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
// monster placed just outside the origin clearing leaves PART of the
// clearing within its CombatRadius and part outside it — every join must
// land on the safe side when a safe hex exists at all.
func TestSpawnHexLockedPrefersOutsideMonsterCombatRadiusWhenPossible(t *testing.T) {
	t.Parallel()

	w := newWorld()

	// Five hexes out along one axis: outside the radius-2 clearing itself,
	// but close enough that the NEAR side of the clearing (distance <=5+2=7...
	// concretely, hexes on the monster's side) falls within CombatRadius (6)
	// while the FAR side (opposite the monster) does not.
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

// TestSpawnHexLockedFallsBackWhenClearingFull (#36a): if every hex in the
// origin clearing is at StackCap, spawnHexLocked must fall back beyond the
// clearing (spawnHexSpiralLocked) rather than erroring — a join must still
// succeed.
func TestSpawnHexLockedFallsBackWhenClearingFull(t *testing.T) {
	t.Parallel()

	w := newWorld()

	origin := protocol.Hex{Q: 0, R: 0}

	for _, h := range hexesWithinDistance(origin, 2) {
		if !isWalkable(w, h) {
			continue
		}

		for range protocol.StackCap {
			w.PlaceMonsterForTest(h)
		}
	}

	resp, err := w.Join("", "overflow", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("join with a full clearing: %v, want a successful fallback spawn", err)
	}

	if got, want := game.HexDistance(resp.Hex, origin), 2; got <= want {
		t.Errorf("fallback spawn landed at %v (distance %d from origin), want it to have escaped the full clearing",
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

// newSmallWorld builds a radius-3 world — small enough that CombatRadius (6)
// spans the ENTIRE map from any hex, so a living occupant anywhere makes
// every other hex "too close" to it — used to force the guard-empty fallback
// path in SpawnMonsters.
func newSmallWorld() *game.World {
	return game.NewWorld(time.Hour, testCombatPatience, testBubblePoll, testDisconnectGrace, 0xC0FFEE, 3, hub.New())
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
