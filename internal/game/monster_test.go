package game_test

import (
	"slices"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// monsterHexes returns the hexes of every monster-kind entity in snap, in
// snapshot order (already sorted by ID).
func monsterHexes(snap protocol.TurnEvent) []protocol.Hex {
	var hexes []protocol.Hex

	for _, e := range snap.Entities {
		if e.Kind == protocol.EntityMonster {
			hexes = append(hexes, e.Hex)
		}
	}

	return hexes
}

// waterHex scans w's generated map for a TerrainWater tile and returns its
// hex, failing the test if the map has none — the generator's biome mix is
// tuned to always include water, so an empty scan means the map or tuning
// changed underneath this test.
func waterHex(t *testing.T, w *game.World) protocol.Hex {
	t.Helper()

	for _, tile := range w.Map().Tiles {
		if tile.Terrain == protocol.TerrainWater {
			return tile.Hex
		}
	}

	t.Fatal("generated map has no TerrainWater tile")

	return protocol.Hex{}
}

func sortedHexes(hexes []protocol.Hex) []protocol.Hex {
	out := slices.Clone(hexes)
	slices.SortFunc(out, func(a, b protocol.Hex) int {
		if a.Q != b.Q {
			return a.Q - b.Q
		}

		return a.R - b.R
	})

	return out
}

// TestSpawnMonstersPlacesWalkableMonsters: SpawnMonsters places the
// requested count on walkable hexes, each spawned at full HP for ITS OWN
// kind (ring-distributed placement since 6c means spawned kinds vary —
// rat/wolf/ghoul/troll/dragon each carry a different maxHP — so this
// asserts internal consistency (HP == MaxHP, both positive) rather than a
// single flat constant; TestSpawnMonstersDistributesAcrossRings and
// TestSpawnMonstersRingKindsAreValid (rings_test.go) pin the per-kind/
// per-ring distribution itself).
func TestSpawnMonstersPlacesWalkableMonsters(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SpawnMonsters(3)

	snap := w.Snapshot()

	var monsters []protocol.Entity

	for _, e := range snap.Entities {
		if e.Kind == protocol.EntityMonster {
			monsters = append(monsters, e)
		}
	}

	if got, want := len(monsters), 3; got != want {
		t.Fatalf("monster count = %d, want %d", got, want)
	}

	for _, m := range monsters {
		if m.MaxHP <= 0 {
			t.Errorf("monster %d MaxHP = %d, want > 0", m.ID, m.MaxHP)
		}

		if got, want := m.HP, m.MaxHP; got != want {
			t.Errorf("monster %d HP = %d, want %d (its own kind's full MaxHP)", m.ID, got, want)
		}

		if !isWalkable(w, m.Hex) {
			t.Errorf("monster %d at %v is not on a walkable hex", m.ID, m.Hex)
		}
	}
}

// TestSpawnMonsterAtPlacesAndRejects pins SpawnMonsterAt's contract: it spawns
// a full-HP monster on a walkable, sub-StackCap hex and reports true; it refuses
// a non-walkable hex and a hex already at StackCap, reporting false and leaving
// occupancy untouched.
func TestSpawnMonsterAtPlacesAndRejects(t *testing.T) {
	t.Parallel()

	walkable := protocol.Hex{Q: 1, R: 0}
	full := protocol.Hex{Q: -1, R: 0}

	w := newWorld()
	water := waterHex(t, w) // a real TerrainWater hex on the generated map, not walkable

	if !isWalkable(w, walkable) {
		t.Fatalf("fixture hex %v is not walkable; pick another", walkable)
	}

	if isWalkable(w, water) {
		t.Fatalf("fixture hex %v is walkable; expected water", water)
	}

	// Walkable, empty hex → spawns a full-HP monster.
	if got, want := w.SpawnMonsterAt(walkable), true; got != want {
		t.Fatalf("SpawnMonsterAt(%v) = %v, want %v", walkable, got, want)
	}

	snap := w.Snapshot()

	var spawned *protocol.Entity

	for i, e := range snap.Entities {
		if e.Kind == protocol.EntityMonster && e.Hex == walkable {
			spawned = &snap.Entities[i]
		}
	}

	if spawned == nil {
		t.Fatalf("no monster at %v after SpawnMonsterAt", walkable)
	}

	if got, want := spawned.HP, protocol.MonsterMaxHP; got != want {
		t.Errorf("spawned monster HP = %d, want %d", got, want)
	}

	if got, want := spawned.MaxHP, protocol.MonsterMaxHP; got != want {
		t.Errorf("spawned monster MaxHP = %d, want %d", got, want)
	}

	// Non-walkable hex → refused.
	if got, want := w.SpawnMonsterAt(water), false; got != want {
		t.Errorf("SpawnMonsterAt(water %v) = %v, want %v", water, got, want)
	}

	// Hex already at StackCap → refused, occupancy unchanged.
	for range protocol.StackCap {
		w.PlaceEntityForTest(full)
	}

	if got, want := w.SpawnMonsterAt(full), false; got != want {
		t.Errorf("SpawnMonsterAt(full %v) = %v, want %v", full, got, want)
	}

	if got, want := countAt(w.Snapshot(), full), protocol.StackCap; got != want {
		t.Errorf("occupancy at %v = %d, want unchanged StackCap %d", full, got, want)
	}
}

func TestSpawnMonstersIsReproducibleForSameSeed(t *testing.T) {
	t.Parallel()

	hexesForSeed := func(seed int64) []protocol.Hex {
		w := newWorld()
		w.SetSeedForTest(seed)
		w.SpawnMonsters(3)

		return sortedHexes(monsterHexes(w.Snapshot()))
	}

	a := hexesForSeed(42)
	again := hexesForSeed(42)

	if !slices.Equal(a, again) {
		t.Fatalf("same seed produced different monster hexes: %v vs %v", a, again)
	}

	// Guard against a placement that ignores the seed entirely (which would
	// make the reproducibility check above vacuous): a different seed should
	// produce a different set of monster hexes on this large a map.
	b := hexesForSeed(43)
	if slices.Equal(a, b) {
		t.Fatalf("seed 42 and seed 43 produced identical monster hexes %v; placement does not appear seeded", a)
	}
}

// twoStepsAway returns a walkable hex at hex-distance 2 from `from`, found via
// a walkable neighbor of a walkable neighbor (geometry-independent discovery,
// same pattern as TestIntentWalksMultiStepPath in world_test.go).
func twoStepsAway(t *testing.T, w *game.World, from protocol.Hex) protocol.Hex {
	t.Helper()

	n1 := walkableNeighbor(t, w, from)

	for _, n2 := range game.HexNeighbors(n1) {
		if n2 != from && game.HexDistance(from, n2) == 2 && isWalkable(w, n2) {
			return n2
		}
	}

	t.Skip("no distance-2 walkable hex found near spawn")

	return protocol.Hex{}
}

func TestMonsterAIApproachesNearestPlayer(t *testing.T) {
	t.Parallel()

	w := newWorld()

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	monsterHex := twoStepsAway(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	if got, want := game.HexDistance(monsterHex, me.Hex), 2; got != want {
		t.Fatalf("setup: monster distance to player = %d, want %d", got, want)
	}

	snap := step(t, w)

	playerHex := hexOfSnap(snap, me.EntityID)
	if got, want := playerHex, me.Hex; got != want {
		t.Fatalf("player moved without an intent: %v != %v", got, want)
	}

	if got, want := game.HexDistance(hexOfSnap(snap, monsterID), playerHex), 1; got != want {
		t.Fatalf("monster distance to player after resolve = %d, want 1 (approached by one hex)", got)
	}
}

// TestMonsterAIAttacksAdjacentSolePlayer: superseded by 6.3 Task 3 — a
// monster already adjacent to the sole player no longer holds position (6.2
// behaviour); it steps onto the player's hex, and the move phase converts
// that into a bump-to-attack. See TestMonsterAIAttacksAdjacentPlayer in
// combat_test.go for the HP/positioning assertions; this test keeps the 6.2
// coverage of the AI's targeting/path-length decision at range 1.
func TestMonsterAIAttacksAdjacentSolePlayer(t *testing.T) {
	t.Parallel()

	w := newWorld()

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	snap := step(t, w)

	gotHex := hexOfSnap(snap, monsterID)
	if got, want := gotHex, monsterHex; got != want {
		t.Fatalf("monster hex = %v, want unchanged %v (a bump does not move the attacker)", got, want)
	}

	if got, want := game.HexDistance(gotHex, me.Hex), 1; got != want {
		t.Fatalf("monster distance to player = %d, want 1 (still adjacent, did not chase past the bump)", got)
	}
}

func TestMonsterAIStepsTowardNearerOfTwoPlayers(t *testing.T) {
	t.Parallel()

	w := newWorld()

	monsterHex := protocol.Hex{Q: 0, R: 0}
	// nearHex sits 2 hexes from the monster along one direction; farHex sits 3
	// hexes away along a different direction, so approaching the nearer player
	// does not also approach the farther one — a decrease in distance to only
	// one of them unambiguously proves which player the monster targeted.
	nearHex := protocol.Hex{Q: -2, R: 2}
	farHex := protocol.Hex{Q: 0, R: -3}

	if !isWalkable(w, monsterHex) || !isWalkable(w, nearHex) || !isWalkable(w, farHex) {
		t.Skip("expected hexes are not walkable on this map")
	}

	monsterID := w.PlaceMonsterForTest(monsterHex)
	nearID, _ := w.PlaceEntityForTest(nearHex)
	farID, _ := w.PlaceEntityForTest(farHex)

	if got, want := game.HexDistance(monsterHex, nearHex), 2; got != want {
		t.Fatalf("setup: distance to near player = %d, want %d", got, want)
	}

	if got, want := game.HexDistance(monsterHex, farHex), 3; got != want {
		t.Fatalf("setup: distance to far player = %d, want %d", got, want)
	}

	snap := step(t, w)

	gotMonsterHex := hexOfSnap(snap, monsterID)

	// The unique hex adjacent to both the monster's start and the nearer
	// player: the only shortest-path first step toward nearHex.
	wantStep := protocol.Hex{Q: -1, R: 1}
	if got, want := gotMonsterHex, wantStep; got != want {
		t.Fatalf("monster stepped to %v, want %v (the step toward the nearer player)", got, want)
	}

	if got, want := game.HexDistance(gotMonsterHex, hexOfSnap(snap, nearID)), 1; got != want {
		t.Fatalf("distance to nearer player after resolve = %d, want 1 (approached)", got)
	}

	// Moved away from the farther player too — confirms the nearer target, not
	// a coincidental step that also happens to approach the farther one.
	if got, want := game.HexDistance(gotMonsterHex, hexOfSnap(snap, farID)), 4; got != want {
		t.Fatalf("distance to farther player = %d, want %d", got, want)
	}
}

// TestMonsterBeyondAggroRangeStandsStill (#36): a WORLD-domain monster more
// than MonsterAggroRadius from the only player never notices it — it stands
// still (no wander this slice) instead of hunting from arbitrarily far away.
func TestMonsterBeyondAggroRangeStandsStill(t *testing.T) {
	t.Parallel()

	w := newWorld()

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	// Comfortably beyond MonsterAggroRadius; the upper bound is generous
	// (map bounds, not a tight aggro-adjacent distance) since this test only
	// needs SOME hex outside range, not a specific one.
	farHex := walkableHexAtDistance(t, w, me.Hex, protocol.MonsterAggroRadius+1, protocol.MonsterAggroRadius*3)
	monsterID := w.PlaceMonsterForTest(farHex)

	snap := step(t, w)

	if got, want := hexOfSnap(snap, monsterID), farHex; got != want {
		t.Errorf("monster hex = %v, want unchanged %v (beyond MonsterAggroRadius, should stand still)", got, want)
	}
}

// TestMonsterWithinAggroRangeHunts (#36): a WORLD-domain monster at exactly
// MonsterAggroRadius (the inclusive boundary) still notices and approaches
// the player — only STRICTLY beyond the radius stands still.
func TestMonsterWithinAggroRangeHunts(t *testing.T) {
	t.Parallel()

	w := newWorld()

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	// A scattered spawn near the map edge can force the monster's single
	// approach step from exactly MonsterAggroRadius to detour around
	// generated terrain instead of strictly closing distance.
	pinToOrigin(w, &me)

	atRadius := walkableHexAtDistance(t, w, me.Hex, protocol.MonsterAggroRadius, protocol.MonsterAggroRadius)
	monsterID := w.PlaceMonsterForTest(atRadius)

	snap := step(t, w)

	beforeDist := game.HexDistance(atRadius, me.Hex)
	afterDist := game.HexDistance(hexOfSnap(snap, monsterID), me.Hex)

	if afterDist >= beforeDist {
		t.Errorf("monster distance to player went %d -> %d, want it to close in (within aggro range, at the boundary)",
			beforeDist, afterDist)
	}
}
