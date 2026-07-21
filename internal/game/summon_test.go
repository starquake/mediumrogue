package game_test

import (
	"slices"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// summon_test.go: the in-combat summon hook (#271) — the Necromancer summoner
// and the Risen adds it raises (summon.go). Black-box over the real World: the
// wind-up cooldown, the window cadence, the living-minion cap, occupancy/StackCap
// safety at the spawn hex, determinism, and the snapshot round-trip of the new
// per-entity state. The kind ids are game-package-internal consts, so these
// tests name them as string literals (as effects_test.go names "serpent").

const (
	kindNecromancer = "necromancer"
	kindRisen       = "risen"
)

// openTerrain sets center and all six of its neighbors to walkable grass, so a
// summon test has a fully-known, fully-free neighborhood to place adds on.
func openTerrain(w *game.World, center protocol.Hex) {
	w.SetTerrainForTest(center, protocol.TerrainGrass)

	for _, h := range game.HexNeighbors(center) {
		w.SetTerrainForTest(h, protocol.TerrainGrass)
	}
}

// TestNecromancerRaisesAddWhenWindowOpens is the live proof: with its summon
// window open, a Necromancer raises exactly one Risen on a free adjacent hex,
// stamped with the summoner's id, and reloads its cooldown to everyTurns (3).
func TestNecromancerRaisesAddWhenWindowOpens(t *testing.T) {
	t.Parallel()

	w := newWorld()

	center := protocol.Hex{Q: 0, R: 0}
	openTerrain(w, center)

	necroID := w.PlaceMonsterKindForTest(center, kindNecromancer)
	w.SetSummonCooldownForTest(necroID, 0) // window open now

	w.ResolveCombatOnlyForTest()

	minions := w.MinionsOfForTest(necroID)
	if got, want := len(minions), 1; got != want {
		t.Fatalf("minions raised = %d, want %d", got, want)
	}

	if got, want := w.MonsterKindForTest(minions[0]), kindRisen; got != want {
		t.Errorf("minion kind = %q, want %q", got, want)
	}

	if got, want := w.SummonerIDForTest(minions[0]), necroID; got != want {
		t.Errorf("minion summonerID = %d, want %d", got, want)
	}

	neighbors := game.HexNeighbors(center)
	if got := w.EntityHexForTest(minions[0]); !slices.Contains(neighbors[:], got) {
		t.Errorf("minion at %v, want one of the summoner's neighbors %v", got, neighbors)
	}

	if got, want := w.SummonCooldownForTest(necroID), 3; got != want {
		t.Errorf("cooldown after summon = %d, want %d (reloaded to everyTurns)", got, want)
	}
}

// TestSummonWindUpDelaysFirstAdd: a freshly-spawned Necromancer starts on a
// full cooldown (everyTurns=3), so the first add appears only on the 4th
// in-combat turn — the player gets a wind-up window, not an instant swarm.
func TestSummonWindUpDelaysFirstAdd(t *testing.T) {
	t.Parallel()

	w := newWorld()

	center := protocol.Hex{Q: 0, R: 0}
	openTerrain(w, center)

	necroID := w.PlaceMonsterKindForTest(center, kindNecromancer)

	// Turns 1..3: cooldown counts 3→2→1→0, nothing raised yet.
	for turn := 1; turn <= 3; turn++ {
		w.ResolveCombatOnlyForTest()

		if got := len(w.MinionsOfForTest(necroID)); got != 0 {
			t.Fatalf("after %d turns, minions = %d, want 0 (still winding up)", turn, got)
		}
	}

	// Turn 4: the window opens and the first add is raised.
	w.ResolveCombatOnlyForTest()

	if got, want := len(w.MinionsOfForTest(necroID)), 1; got != want {
		t.Errorf("after the wind-up, minions = %d, want %d", got, want)
	}
}

// TestSummonCapsLivingMinions: however often the window opens, a summoner never
// keeps more than summonSpec.maxLiving (3) adds alive at once — the
// runaway-spawn guard.
func TestSummonCapsLivingMinions(t *testing.T) {
	t.Parallel()

	w := newWorld()

	center := protocol.Hex{Q: 0, R: 0}
	openTerrain(w, center)

	necroID := w.PlaceMonsterKindForTest(center, kindNecromancer)

	// Force a window every turn (cooldown 0 each time) for well past the cap.
	for range 6 {
		w.SetSummonCooldownForTest(necroID, 0)
		w.ResolveCombatOnlyForTest()

		if got := len(w.MinionsOfForTest(necroID)); got > 3 {
			t.Fatalf("living minions = %d, want <= 3 (maxLiving cap)", got)
		}
	}

	if got, want := len(w.MinionsOfForTest(necroID)), 3; got != want {
		t.Errorf("living minions at steady state = %d, want %d (the cap)", got, want)
	}
}

// TestSummonRefillsWhenAMinionDies: the cap is over LIVING minions, so killing
// one frees room for the next window to raise a replacement — a steady-state
// pressure, not a one-time burst.
func TestSummonRefillsWhenAMinionDies(t *testing.T) {
	t.Parallel()

	w := newWorld()

	center := protocol.Hex{Q: 0, R: 0}
	openTerrain(w, center)

	necroID := w.PlaceMonsterKindForTest(center, kindNecromancer)

	// Fill to the cap.
	for range 3 {
		w.SetSummonCooldownForTest(necroID, 0)
		w.ResolveCombatOnlyForTest()
	}

	minions := w.MinionsOfForTest(necroID)
	if got, want := len(minions), 3; got != want {
		t.Fatalf("minions before kill = %d, want %d", got, want)
	}

	// Kill one; the next open window must refill back to the cap.
	w.SetHPForTest(minions[0], 0)
	w.SetSummonCooldownForTest(necroID, 0)
	w.ResolveCombatOnlyForTest()

	if got, want := len(w.MinionsOfForTest(necroID)), 3; got != want {
		t.Errorf("living minions after a death + window = %d, want %d (refilled)", got, want)
	}
}

// TestSummonRespectsOccupancyAndStackCap: an add never lands on a blocked hex
// (#196). With every neighbor either non-walkable rock or a walkable hex filled
// to StackCap, there is no free hex, so an open window raises nothing — proving
// the spawn reuses the mover's walkability + occupancy rule rather than dropping
// a minion onto a blocked tile.
func TestSummonRespectsOccupancyAndStackCap(t *testing.T) {
	t.Parallel()

	w := newWorld()

	center := protocol.Hex{Q: 0, R: 0}
	openTerrain(w, center)

	neighbors := game.HexNeighbors(center)

	// Rock out all but the first neighbor; fill the first (still grass) to
	// StackCap with friendly monsters so occupancy blocks it too.
	for _, h := range neighbors[1:] {
		w.SetTerrainForTest(h, protocol.TerrainRock)
	}

	for range protocol.StackCap {
		w.PlaceMonsterKindForTest(neighbors[0], "rat")
	}

	necroID := w.PlaceMonsterKindForTest(center, kindNecromancer)
	w.SetSummonCooldownForTest(necroID, 0)

	w.ResolveCombatOnlyForTest()

	if got := len(w.MinionsOfForTest(necroID)); got != 0 {
		t.Errorf("minions raised = %d, want 0 (no free adjacent hex — occupancy/StackCap safe)", got)
	}

	// The window was still consumed (cooldown reloaded), so it can't busy-loop
	// trying every turn until a hex frees.
	if got, want := w.SummonCooldownForTest(necroID), 3; got != want {
		t.Errorf("cooldown after a no-room window = %d, want %d (still reloaded)", got, want)
	}
}

// TestSummonLandsOnTheOneFreeHex: with five of six neighbors rocked out, the add
// must land on the single walkable free neighbor — proving the walkability
// filter, not just that it lands somewhere.
func TestSummonLandsOnTheOneFreeHex(t *testing.T) {
	t.Parallel()

	w := newWorld()

	center := protocol.Hex{Q: 0, R: 0}
	openTerrain(w, center)

	neighbors := game.HexNeighbors(center)
	free := neighbors[3]

	for i, h := range neighbors {
		if i == 3 {
			continue
		}

		w.SetTerrainForTest(h, protocol.TerrainRock)
	}

	necroID := w.PlaceMonsterKindForTest(center, kindNecromancer)
	w.SetSummonCooldownForTest(necroID, 0)

	w.ResolveCombatOnlyForTest()

	minions := w.MinionsOfForTest(necroID)
	if got, want := len(minions), 1; got != want {
		t.Fatalf("minions raised = %d, want %d", got, want)
	}

	if got, want := w.EntityHexForTest(minions[0]), free; got != want {
		t.Errorf("minion hex = %v, want the one free neighbor %v", got, want)
	}
}

// TestSummonPlacementIsDeterministic: the same seed and setup reproduce the
// exact same add placement — the spawn's only randomness (which free hex) rides
// the per-turn seeded PCG.
func TestSummonPlacementIsDeterministic(t *testing.T) {
	t.Parallel()

	placeOne := func() (int64, protocol.Hex) {
		w := newWorld()
		w.SetSeedForTest(42)

		center := protocol.Hex{Q: 0, R: 0}
		openTerrain(w, center)

		necroID := w.PlaceMonsterKindForTest(center, kindNecromancer)
		w.SetSummonCooldownForTest(necroID, 0)
		w.ResolveCombatOnlyForTest()

		minions := w.MinionsOfForTest(necroID)
		if len(minions) != 1 {
			t.Fatalf("expected exactly one minion, got %d", len(minions))
		}

		return minions[0], w.EntityHexForTest(minions[0])
	}

	id1, hex1 := placeOne()
	id2, hex2 := placeOne()

	if id1 != id2 {
		t.Errorf("minion id differs across identical runs: %d vs %d", id1, id2)
	}

	if hex1 != hex2 {
		t.Errorf("minion hex differs across identical runs: %v vs %v (non-deterministic)", hex1, hex2)
	}
}

// TestSummonStateSurvivesSnapshot: a summoner's cooldown and a minion's
// summonerID are multi-turn state (snapshotVersion 10) — a restart must keep
// both, or a reloaded fight hands a free summon and its adds escape the cap.
func TestSummonStateSurvivesSnapshot(t *testing.T) {
	t.Parallel()

	w, _ := newSnapshotWorld(t)

	center := protocol.Hex{Q: 0, R: 0}
	openTerrain(w, center)

	necroID := w.PlaceMonsterKindForTest(center, kindNecromancer)
	w.SetSummonCooldownForTest(necroID, 0)
	w.ResolveCombatOnlyForTest()

	minions := w.MinionsOfForTest(necroID)
	if len(minions) != 1 {
		t.Fatalf("expected one minion before snapshot, got %d", len(minions))
	}

	minionID := minions[0]

	wantCooldown := w.SummonCooldownForTest(necroID)

	data, err := w.MarshalState()
	if err != nil {
		t.Fatalf("MarshalState: %v", err)
	}

	w2, _ := newSnapshotWorld(t)
	if err := w2.RestoreState(data); err != nil {
		t.Fatalf("RestoreState: %v", err)
	}

	if got, want := w2.SummonCooldownForTest(necroID), wantCooldown; got != want {
		t.Errorf("restored summoner cooldown = %d, want %d", got, want)
	}

	if got, want := w2.SummonerIDForTest(minionID), necroID; got != want {
		t.Errorf("restored minion summonerID = %d, want %d", got, want)
	}
}
