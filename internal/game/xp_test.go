package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestFreshPlayerHasZeroXPLevelOne: a newly joined player carries XP 0 and the
// derived Level 1 on the wire.
func TestFreshPlayerHasZeroXPLevelOne(t *testing.T) {
	t.Parallel()

	w := newWorld()

	me, err := w.Join("")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	player, ok := entityOfSnap(w.Snapshot(), me.EntityID)
	if !ok {
		t.Fatalf("player %d missing from snapshot", me.EntityID)
	}

	if got, want := player.XP, 0; got != want {
		t.Errorf("fresh player XP = %d, want %d", got, want)
	}

	if got, want := player.Level, 1; got != want {
		t.Errorf("fresh player Level = %d, want %d", got, want)
	}
}

// TestKillGrantsXP: a player who bumps a one-hit monster to death is awarded the
// full MonsterXP; the derived Level reflects the new total.
func TestKillGrantsXP(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(10)

	me, err := w.Join("")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)
	w.SetHPForTest(monsterID, protocol.PlayerAttackDamage) // one bump is lethal

	if !submitOK(w, me, monsterHex) {
		t.Fatalf("SubmitIntent onto the monster's hex failed")
	}

	snap := step(t, w)

	if _, ok := entityOfSnap(snap, monsterID); ok {
		t.Fatalf("monster %d should have been killed by the bump", monsterID)
	}

	player, ok := entityOfSnap(snap, me.EntityID)
	if !ok {
		t.Fatalf("killer %d missing from snapshot", me.EntityID)
	}

	if got, want := player.XP, protocol.MonsterXP; got != want {
		t.Errorf("killer XP = %d, want %d (full MonsterXP)", got, want)
	}

	// 20 XP is still level 1 (XPPerLevel=100); the level-up crossing is exercised
	// separately in TestKillCrossingLevelBoundaryLevelsUp.
	if got, want := player.Level, 1; got != want {
		t.Errorf("killer Level = %d, want %d", got, want)
	}
}

// TestSharedXPIsFullNotSplit: two players in one fight both kill a single
// monster; each is awarded the FULL MonsterXP, not a divided share — helping
// always pays, with no last-hit competition.
func TestSharedXPIsFullNotSplit(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(11)

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	ns := game.HexNeighbors(center)

	idA, _ := w.PlaceEntityForTest(ns[0])
	idB, _ := w.PlaceEntityForTest(ns[1])

	monsterID := w.PlaceMonsterForTest(center)
	w.SetHPForTest(monsterID, protocol.PlayerAttackDamage) // dies to a single hit

	// Both players bump the monster's hex on the same turn; the monster has no
	// path set, so it never strikes back and both attackers survive to be paid.
	w.SetPathForTest(idA, []protocol.Hex{center})
	w.SetPathForTest(idB, []protocol.Hex{center})

	w.ResolveCombatOnlyForTest()

	if _, ok := entityOfSnap(w.Snapshot(), monsterID); ok {
		t.Fatalf("monster %d should have died to the shared bumps", monsterID)
	}

	if got, want := w.XPForTest(idA), protocol.MonsterXP; got != want {
		t.Errorf("player A XP = %d, want the full %d (not split)", got, want)
	}

	if got, want := w.XPForTest(idB), protocol.MonsterXP; got != want {
		t.Errorf("player B XP = %d, want the full %d (not split)", got, want)
	}
}

// TestKillCrossingLevelBoundaryLevelsUp: a player one kill short of the next
// level crosses XPPerLevel on the kill and their derived Level increments.
func TestKillCrossingLevelBoundaryLevelsUp(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(12)

	me, err := w.Join("")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	// One MonsterXP below the level-2 boundary: still level 1 before the kill.
	w.SetXPForTest(me.EntityID, protocol.XPPerLevel-protocol.MonsterXP)

	before, ok := entityOfSnap(w.Snapshot(), me.EntityID)
	if !ok {
		t.Fatalf("player %d missing before the kill", me.EntityID)
	}

	if got, want := before.Level, 1; got != want {
		t.Fatalf("pre-kill Level = %d, want %d", got, want)
	}

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)
	w.SetHPForTest(monsterID, protocol.PlayerAttackDamage)

	if !submitOK(w, me, monsterHex) {
		t.Fatalf("SubmitIntent onto the monster's hex failed")
	}

	snap := step(t, w)

	player, ok := entityOfSnap(snap, me.EntityID)
	if !ok {
		t.Fatalf("player %d missing after the kill", me.EntityID)
	}

	if got, want := player.XP, protocol.XPPerLevel; got != want {
		t.Errorf("post-kill XP = %d, want %d (exactly the boundary)", got, want)
	}

	if got, want := player.Level, 2; got != want {
		t.Errorf("post-kill Level = %d, want %d (leveled up)", got, want)
	}
}

// TestDeathFloorsXPKeepsLevel: a dying player falls back to the start of the
// level they were in — keeping the level, losing only within-level progress.
func TestDeathFloorsXPKeepsLevel(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(13)

	me, err := w.Join("")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	// Mid level 2: 150 XP → level 2, floor 100.
	w.SetXPForTest(me.EntityID, 150)

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	// The monster strikes the player dead; the player has no intent, so no
	// monster dies this turn (pure death-floor, no kill award).
	w.SetHPForTest(me.EntityID, protocol.MonsterAttackDamage) // exactly lethal
	w.SetPathForTest(monsterID, []protocol.Hex{me.Hex})
	w.ResolveCombatOnlyForTest()

	snap := w.Snapshot()

	player, ok := entityOfSnap(snap, me.EntityID)
	if !ok {
		t.Fatalf("player %d should respawn, not vanish", me.EntityID)
	}

	if got, want := player.XP, 100; got != want {
		t.Errorf("respawned XP = %d, want %d (floored to the level-2 start)", got, want)
	}

	if got, want := player.Level, 2; got != want {
		t.Errorf("respawned Level = %d, want %d (unchanged by death)", got, want)
	}
}

// TestPlayerDyingSameTurnAsMonsterGetsNoKillXP: when a player dies on the very
// turn a monster also dies (a mutual kill), the dead player is NOT a surviving
// member and is awarded nothing — the XP award is credited before the death
// loop but only to living players. Seeding the player at 190 XP makes this
// observable: a correct floor lands at 100 (level-2 start), whereas a buggy
// award-then-floor would reach 200 (level-3 start).
func TestPlayerDyingSameTurnAsMonsterGetsNoKillXP(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(14)

	me, err := w.Join("")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.SetXPForTest(me.EntityID, 190) // level 2, floor 100

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	// One hit each is lethal in both directions: a mutual kill.
	w.SetHPForTest(monsterID, protocol.PlayerAttackDamage)
	w.SetHPForTest(me.EntityID, protocol.MonsterAttackDamage)

	if !submitOK(w, me, monsterHex) {
		t.Fatalf("SubmitIntent onto the monster's hex failed")
	}

	w.SetPathForTest(monsterID, []protocol.Hex{me.Hex})
	w.ResolveCombatOnlyForTest()

	snap := w.Snapshot()

	if _, ok := entityOfSnap(snap, monsterID); ok {
		t.Fatalf("monster %d should have died in the mutual kill", monsterID)
	}

	player, ok := entityOfSnap(snap, me.EntityID)
	if !ok {
		t.Fatalf("player %d should respawn, not vanish", me.EntityID)
	}

	if got, want := player.XP, 100; got != want {
		t.Errorf("respawned XP = %d, want %d (no kill award; floored from 190, not 210)", got, want)
	}

	if got, want := player.Level, 2; got != want {
		t.Errorf("respawned Level = %d, want %d", got, want)
	}
}
