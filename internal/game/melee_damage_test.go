package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestBumpDamageUsesClassCloseWeapon: a player's melee bump deals its class
// close-weapon damage, not a flat constant. A Fighter's bump hits for the sword
// (4) and a Rogue's for the dagger (7) — different numbers off the same board —
// each dropping the monster's HP by exactly that weapon's level-1 damage.
func TestBumpDamageUsesClassCloseWeapon(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		class string
		want  int
	}{
		{"fighter sword", protocol.ClassFighter, game.ItemDamageForTest("iron-sword")},
		{"rogue dagger", protocol.ClassRogue, game.ItemDamageForTest("dagger")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld()
			w.SetSeedForTest(20)

			center := protocol.Hex{Q: 0, R: 0}
			if !isWalkable(w, center) {
				t.Skip("origin is not walkable on this map")
			}

			monsterHex := walkableNeighbor(t, w, center)

			pid, _ := w.PlaceEntityForTest(center)
			w.SetClassForTest(pid, tc.class)

			monsterID := w.PlaceMonsterForTest(monsterHex)

			// Bump the monster: a move onto its hex lands as a melee attack. The
			// monster has no path set, so it does not retaliate — isolating the
			// attacker's damage.
			w.SetPathForTest(pid, []protocol.Hex{monsterHex})
			w.ResolveCombatOnlyForTest()

			monster, ok := entityOfSnap(w.Snapshot(), monsterID)
			if !ok {
				t.Fatalf("monster %d missing after a single %s bump", monsterID, tc.class)
			}

			if got, want := monster.HP, protocol.MonsterMaxHP-tc.want; got != want {
				t.Errorf("monster HP after %s bump = %d, want %d (drop by the class close weapon)", tc.class, got, want)
			}
		})
	}
}

// TestAttackDamageDoesNotScaleWithLevel: DamagePerLevel is cut (#60, roadmap
// XP3) — a level-5 attacker's sword hit equals a level-1's. Two identical
// Fighters, one at level 1 (xp 0) and one at level 5 (xp
// XPCurveBase*(5-1)^2 == 1600), each bump an identical monster off identical
// boards with identical seeds: the damage dealt must be equal, and must equal
// the level-1 base (the pinned combat number from the old
// DamagePerLevel-scaled test — re-derived: DamagePerLevel cut (fast-lane
// T3)).
func TestAttackDamageDoesNotScaleWithLevel(t *testing.T) {
	t.Parallel()

	bump := func(t *testing.T, xp int) int {
		t.Helper()

		w := newWorld()
		w.SetSeedForTest(21)

		center := protocol.Hex{Q: 0, R: 0}
		if !isWalkable(w, center) {
			t.Skip("origin is not walkable on this map")
		}

		monsterHex := walkableNeighbor(t, w, center)

		pid, _ := w.PlaceEntityForTest(center) // level-1 Fighter by default
		w.SetXPForTest(pid, xp)

		monsterID := w.PlaceMonsterForTest(monsterHex)

		w.SetPathForTest(pid, []protocol.Hex{monsterHex})
		w.ResolveCombatOnlyForTest()

		monster, ok := entityOfSnap(w.Snapshot(), monsterID)
		if !ok {
			t.Fatalf("monster %d missing after a single bump", monsterID)
		}

		return protocol.MonsterMaxHP - monster.HP
	}

	level1Dealt := bump(t, 0)
	level5Dealt := bump(t, 4*protocol.XPCurveBase) // 1600: levelFor(1600) == 5

	if got, want := level5Dealt, level1Dealt; got != want {
		t.Errorf("level-5 Fighter bump damage = %d, want %d (equal to level-1's — level must not scale damage)", got, want)
	}

	// re-derived: DamagePerLevel cut (fast-lane T3) — the old test asserted
	// base + 2*DamagePerLevel for a level-3 attacker; damage is now always
	// exactly the weapon's base regardless of level.
	if got, want := level1Dealt, game.ItemDamageForTest("iron-sword"); got != want {
		t.Errorf("bump damage = %d, want %d (iron-sword base, level-free)", got, want)
	}
}

// TestMonsterBumpDamageUnchanged: a monster's melee is its kind's own claws
// damage (wolf here — the default spawn kind, carrying the old flat number
// forward unchanged) — classes changed only the player side of the bump. A
// wolf bumping a Fighter drops the Fighter by exactly wolf's claws damage.
func TestMonsterBumpDamageUnchanged(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(22)

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	monsterHex := walkableNeighbor(t, w, center)

	pid, _ := w.PlaceEntityForTest(center) // level-1 Fighter, FighterMaxHP
	monsterID := w.PlaceMonsterForTest(monsterHex)

	// Monster bumps the player; the player has no path, so it does not hit back.
	w.SetPathForTest(monsterID, []protocol.Hex{center})
	w.ResolveCombatOnlyForTest()

	player, ok := entityOfSnap(w.Snapshot(), pid)
	if !ok {
		t.Fatalf("player %d missing after a monster bump", pid)
	}

	if got, want := player.HP, protocol.FighterMaxHP-game.MonsterDamageForTest("wolf"); got != want {
		t.Errorf("player HP after monster bump = %d, want %d (monster melee flat, unchanged)", got, want)
	}
}
