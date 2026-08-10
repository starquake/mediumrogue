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
// that into a melee attack. See TestMonsterAIAttacksAdjacentPlayer in
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
		t.Fatalf("monster hex = %v, want unchanged %v (a melee attack does not move the attacker)", got, want)
	}

	if got, want := game.HexDistance(gotHex, me.Hex), 1; got != want {
		t.Fatalf("monster distance to player = %d, want 1 (still adjacent, did not chase past the bump)", got)
	}
}

func TestMonsterAIStepsTowardNearerOfTwoPlayers(t *testing.T) {
	t.Parallel()

	w := newWorld()

	// SEARCHED, not hardcoded (#413). This test used three fixed coordinates
	// and skipped when they were not all walkable — and because the world seed
	// is pinned, that skip fired on EVERY run since the test was written, so
	// monster targeting had no coverage at all while the test reported green.
	// The map offers ~21,900 triples of this shape; the fixed three simply were
	// not one of them.
	monsterHex, nearHex, farHex := aggroTriple(t, w)

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

	// Asserted as a PROPERTY, not a fixed destination hex (#413). The old
	// version expected a literal {-1, 1}, which only held for the hardcoded
	// setup this test no longer uses — and the property is the actual contract
	// anyway: closed on the nearer player, did not close on the farther one.
	if got, want := game.HexDistance(gotMonsterHex, hexOfSnap(snap, nearID)), 1; got != want {
		t.Fatalf("distance to nearer player after resolve = %d, want %d (approached)", got, want)
	}

	// Did NOT approach the farther player — confirms the nearer target, rather
	// than a coincidental step that closed on both. The triple is chosen so the
	// two are more than 3 apart, which makes these mutually exclusive.
	if got, want := game.HexDistance(gotMonsterHex, hexOfSnap(snap, farID)),
		game.HexDistance(monsterHex, farHex); got < want {
		t.Fatalf("distance to farther player = %d, want >= %d (must not have approached)", got, want)
	}
}

// TestMonsterBeyondAggroRangeDoesNotHunt (#36): a WORLD-domain monster more
// than MonsterAggroRadius from the only player never notices it — it stands
// still (no wander this slice) instead of hunting from arbitrarily far away.
func TestMonsterBeyondAggroRangeDoesNotHunt(t *testing.T) {
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
	w.SetMonsterHomeForTest(monsterID, farHex)

	// "Stands still" WAS the observable proof of "does not hunt", and #366 ended
	// that: an unaggroed monster now drifts near home. So assert the EXACT
	// invariant the drift guarantees instead.
	//
	// thinkWanderLocked refuses to drift while a player is within
	// MonsterAggroRadius+1, and one step closes at most one hex — so a drifting
	// monster can approach to that boundary and never past it. A hunter, at one
	// hex a turn, would be on top of the player long before turn 20.
	//
	// A margin like "no more than N hexes closer than it started" was tried and
	// is FLAKY: over 20 turns a random walk drifts several hexes in one
	// direction often enough to fail roughly one run in two.
	const closest = protocol.MonsterAggroRadius + 1

	for range 20 {
		step(t, w)

		if got := game.HexDistance(hexOfSnap(w.Snapshot(), monsterID), me.Hex); got < closest {
			t.Fatalf("monster reached %d hexes from the player, want never nearer than %d "+
				"(drift must never bring a player into aggro range)", got, closest)
		}
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
	clearSightLine(t, w, me.Hex, atRadius) // this test varies DISTANCE, not terrain (#95)
	monsterID := w.PlaceMonsterForTest(atRadius)

	snap := step(t, w)

	beforeDist := game.HexDistance(atRadius, me.Hex)
	afterDist := game.HexDistance(hexOfSnap(snap, monsterID), me.Hex)

	if afterDist >= beforeDist {
		t.Errorf("monster distance to player went %d -> %d, want it to close in (within aggro range, at the boundary)",
			beforeDist, afterDist)
	}
}

// noticedByWolfAt reports whether a wolf placed dist hexes from a fighter at
// origin notices that fighter and steps toward them within one turn — the
// live evAggroRange fold (#88). gearID, when non-empty, is a registry def the
// fighter equips first (free equip: the player is well outside any bubble at
// these distances). "Noticed" is asserted as "the wolf moved at all", not as
// a strictly closing distance: generated terrain can force an approach step
// to detour sideways, but an UNAWARE world monster never steps at all.
func noticedByWolfAt(t *testing.T, dist int, gearID string) bool {
	t.Helper()

	w := newWorld()

	origin := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, origin) {
		t.Skip("origin is not walkable on this map")
	}

	id, token := w.PlaceEntityForTest(origin)

	if gearID != "" {
		instID := w.GrantItemForTest(id, gearID)
		if err := w.SubmitIntent(protocol.IntentRequest{
			EntityID: id, Token: token, Kind: protocol.IntentEquip, ItemID: instID,
		}); err != nil {
			t.Fatalf("SubmitIntent equip %s: %v", gearID, err)
		}
	}

	at := walkableHexAtDistance(t, w, origin, dist, dist)
	clearSightLine(t, w, origin, at) // hold terrain constant: this test varies GEAR
	monsterID := w.PlaceMonsterKindForTest(at, "wolf")

	return hexOfSnap(step(t, w), monsterID) != at
}

// TestNoticeabilityGearMovesTheAggroBoundary (#88): the same wolf, at the same
// distance 8, notices a bare fighter (its radius is protocol.MonsterAggroRadius
// = 10) but NOT one in Padded Boots (×0.75 → 7). One distance, one variable —
// the gear — proving the fold reaches the live AI notice check and not just
// applyRules.
func TestNoticeabilityGearMovesTheAggroBoundary(t *testing.T) {
	t.Parallel()

	const dist = 8 // inside a bare fighter's 10, outside a booted one's 7

	if got, want := protocol.MonsterAggroRadius, 10; got != want {
		t.Fatalf("MonsterAggroRadius = %d, want %d (this test's boundary premise)", got, want)
	}

	if got, want := noticedByWolfAt(t, dist, ""), true; got != want {
		t.Errorf("bare fighter noticed at %d hexes = %v, want %v (inside the wolf's %d)",
			dist, got, want, protocol.MonsterAggroRadius)
	}

	if got, want := noticedByWolfAt(t, dist, paddedBootsID), false; got != want {
		t.Errorf("booted fighter noticed at %d hexes = %v, want %v (boots shrink the wolf's reach to 7)",
			dist, got, want)
	}
}

// TestIronPlateArmorWidensTheAggroBoundary (#88): the tradeoff's cost half —
// the plate (×1.25 → 12) gets its wearer noticed at 11 hexes, where a bare
// fighter is still invisible to the same wolf.
func TestIronPlateArmorWidensTheAggroBoundary(t *testing.T) {
	t.Parallel()

	const dist = 11 // outside a bare fighter's 10, inside a plated one's 12

	if got, want := noticedByWolfAt(t, dist, ""), false; got != want {
		t.Errorf("bare fighter noticed at %d hexes = %v, want %v (beyond the wolf's %d)",
			dist, got, want, protocol.MonsterAggroRadius)
	}

	if got, want := noticedByWolfAt(t, dist, ironPlateArmorID), true; got != want {
		t.Errorf("plated fighter noticed at %d hexes = %v, want %v (plate widens the wolf's reach to 12)",
			dist, got, want)
	}
}

// aggroTriple finds a monster hex with one player hex 2 away and another 3
// away, all walkable, with the two players far enough apart that closing on
// one does not also close on the other — so a decrease in distance to exactly
// one of them proves which the monster targeted.
//
// It also requires a walkable step from the monster TOWARD the near player, or
// the monster could be pinned by terrain and the test would read a blocked
// move as a targeting failure.
//
// Deterministic: w.Map().Tiles is a slice in a stable order, so the same seed
// yields the same triple every run. Fatal, never Skip — a radius-12 map that
// cannot produce this shape is itself worth failing over (#413).
func aggroTriple(t *testing.T, w *game.World) (monster, near, far protocol.Hex) {
	t.Helper()

	tiles := w.Map().Tiles

	for _, m := range tiles {
		if !isWalkable(w, m.Hex) || !stepsTowardExists(w, m.Hex) {
			continue
		}

		for _, n := range tiles {
			if game.HexDistance(m.Hex, n.Hex) != 2 || !isWalkable(w, n.Hex) {
				continue
			}

			for _, f := range tiles {
				if game.HexDistance(m.Hex, f.Hex) != 3 || !isWalkable(w, f.Hex) {
					continue
				}

				if game.HexDistance(n.Hex, f.Hex) <= 3 {
					continue
				}

				return m.Hex, n.Hex, f.Hex
			}
		}
	}

	t.Fatal("no walkable monster/near/far triple on this map — the shape this test needs does not exist")

	return protocol.Hex{}, protocol.Hex{}, protocol.Hex{}
}

// stepsTowardExists reports whether from has at least one walkable neighbour,
// so a monster placed there can move at all.
func stepsTowardExists(w *game.World, from protocol.Hex) bool {
	for _, n := range game.HexNeighbors(from) {
		if isWalkable(w, n) {
			return true
		}
	}

	return false
}
