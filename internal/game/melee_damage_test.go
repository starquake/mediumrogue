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
		{"fighter sword", protocol.ClassFighter, game.ItemDamageForTest("iron-sword", 1)},
		{"rogue dagger", protocol.ClassRogue, game.ItemDamageForTest("dagger", 1)},
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

// TestBumpDamageScalesWithLevel: a higher-level player bumps for more — the
// class close weapon gains DamagePerLevel for each level above 1. A level-3
// Fighter's sword deals its iron-sword base + 2*DamagePerLevel, strictly above the
// level-1 sword.
func TestBumpDamageScalesWithLevel(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(21)

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	monsterHex := walkableNeighbor(t, w, center)

	pid, _ := w.PlaceEntityForTest(center) // level-1 Fighter
	// Quadratic curve: level 3's floor is XPCurveBase*(3-1)^2 = 400, i.e. two
	// levels above 1 (was 200 = 2*XPPerLevel under the old flat curve).
	// re-derived for XPCurveBase quadratic curve (fast-lane T1)
	w.SetXPForTest(pid, 4*protocol.XPCurveBase)

	monsterID := w.PlaceMonsterForTest(monsterHex)

	w.SetPathForTest(pid, []protocol.Hex{monsterHex})
	w.ResolveCombatOnlyForTest()

	monster, ok := entityOfSnap(w.Snapshot(), monsterID)
	if !ok {
		t.Fatalf("monster %d missing after a single bump", monsterID)
	}

	dealt := protocol.MonsterMaxHP - monster.HP

	if got, want := dealt, game.ItemDamageForTest("iron-sword", 1)+2*protocol.DamagePerLevel; got != want {
		t.Errorf("level-3 Fighter bump damage = %d, want %d (sword + DamagePerLevel per level)", got, want)
	}

	if got, floor := dealt, game.ItemDamageForTest("iron-sword", 1); got <= floor {
		t.Errorf("level-3 bump damage = %d, want > level-1 sword %d (level must raise it)", got, floor)
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
