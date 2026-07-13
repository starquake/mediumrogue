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
	level5Dealt := bump(t, 16*protocol.XPCurveBase) // 1600: levelFor(1600) == 5

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

// Pinned seeds for the Duelist's Saber's own 10% crit-chance card (condChance
// n=10), found the same way misericordeCritSeed/misericordeMissSeed were
// (species_test.go): a fresh RNG stream's single crit-chance draw, scanned
// 0-39 — seed 0 rolls 67 (>=10, no proc), seed 1 rolls 8 (<10, proc). In the
// dual-wield bump below, the dagger hit consumes NO rng (no chance card on
// it), so the ONE draw the saber's own card consumes is the stream's SECOND
// draw overall — the first is the attack phase's victim pick
// (rng.IntN(len(victims)), always 1 candidate here, but IntN still advances
// the generator).
// re-derived: dual-wield per-hit resolution
const (
	saberCritSeed = 1 // Duelist's Saber procs (double damage) at this seed
	saberMissSeed = 0 // Duelist's Saber does not proc (base damage) at this seed
)

// dualWieldBumpDamage places a human (non-elf, no species crit) player
// wielding the Dagger (main hand) and the Duelist's Saber (off hand) at the
// origin, bumps a fat-HP monster at a neighbour so it survives even a double
// crit, and returns the total damage dealt across BOTH hits — isolating the
// saber's own crit card as the only source of a per-hit multiplier in play.
func dualWieldBumpDamage(t *testing.T, seed int64) int {
	t.Helper()

	w := newWorld()
	w.SetSeedForTest(seed)

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	monsterHex := walkableNeighbor(t, w, center)

	pid, tok := w.PlaceEntityForTest(center)
	w.SetClassForTest(pid, "")                      // clear class defaults: both hands start empty
	w.SetSpeciesForTest(pid, protocol.SpeciesHuman) // no species crit in play

	daggerID := w.GrantItemForTest(pid, "dagger")
	if err := w.SubmitIntent(equipIntent(pid, tok, daggerID)); err != nil {
		t.Fatalf("SubmitIntent(equip dagger): %v", err)
	}

	saberID := w.GrantItemForTest(pid, "duelists-saber")
	if err := w.SubmitIntent(equipIntent(pid, tok, saberID)); err != nil {
		t.Fatalf("SubmitIntent(equip Duelist's Saber): %v", err)
	}

	const fatHP = 100

	monsterID := w.PlaceMonsterForTest(monsterHex)
	w.SetHPForTest(monsterID, fatHP) // survives even a double crit, so HP is readable

	w.SetPathForTest(pid, []protocol.Hex{monsterHex})
	w.ResolveCombatOnlyForTest()

	monster, ok := entityOfSnap(w.Snapshot(), monsterID)
	if !ok {
		t.Fatalf("monster %d missing after a dual-wield bump (unexpected kill)", monsterID)
	}

	return fatHP - monster.HP
}

// TestDualWieldTwoMeleeHits: a bump with two melee-tagged weapons held lands
// TWO independent pipeline hits against the same victim, not one shared roll
// — the dagger's flat 4 plus the saber's own 4 (or 8 on its 10% crit).
func TestDualWieldTwoMeleeHits(t *testing.T) {
	t.Parallel()

	const (
		daggerDamage = 4
		saberDamage  = 4
	)

	t.Run("no proc", func(t *testing.T) {
		t.Parallel()

		if got, want := dualWieldBumpDamage(t, saberMissSeed), daggerDamage+saberDamage; got != want {
			t.Errorf("dual-wield bump = %d, want %d (dagger %d + saber base %d)", got, want, daggerDamage, saberDamage)
		}
	})

	t.Run("saber procs", func(t *testing.T) {
		t.Parallel()

		if got, want := dualWieldBumpDamage(t, saberCritSeed), daggerDamage+2*saberDamage; got != want {
			t.Errorf("dual-wield bump = %d, want %d (dagger %d + saber crit %d)", got, want, daggerDamage, 2*saberDamage)
		}
	})
}

// TestSingleWeaponSingleHit: a single held melee weapon (no phantom second
// hit from an empty off hand) and bare/empty hands (fists) each land exactly
// ONE hit — meleeDefsFor must not pad a single-weapon or unarmed attacker's
// hit count.
func TestSingleWeaponSingleHit(t *testing.T) {
	t.Parallel()

	t.Run("main hand only", func(t *testing.T) {
		t.Parallel()

		w := newWorld()
		w.SetSeedForTest(20)

		center := protocol.Hex{Q: 0, R: 0}
		if !isWalkable(w, center) {
			t.Skip("origin is not walkable on this map")
		}

		monsterHex := walkableNeighbor(t, w, center)

		pid, tok := w.PlaceEntityForTest(center)
		w.SetClassForTest(pid, "") // both hands start empty
		w.SetSpeciesForTest(pid, protocol.SpeciesHuman)

		daggerID := w.GrantItemForTest(pid, "dagger")
		if err := w.SubmitIntent(equipIntent(pid, tok, daggerID)); err != nil {
			t.Fatalf("SubmitIntent(equip dagger): %v", err)
		}

		monsterID := w.PlaceMonsterForTest(monsterHex)

		w.SetPathForTest(pid, []protocol.Hex{monsterHex})
		w.ResolveCombatOnlyForTest()

		monster, ok := entityOfSnap(w.Snapshot(), monsterID)
		if !ok {
			t.Fatalf("monster %d missing after a single dagger bump", monsterID)
		}

		if got, want := protocol.MonsterMaxHP-monster.HP, game.ItemDamageForTest("dagger"); got != want {
			t.Errorf("bump damage = %d, want %d (exactly one dagger hit, no phantom off-hand hit)", got, want)
		}
	})

	t.Run("empty hands", func(t *testing.T) {
		t.Parallel()

		w := newWorld()
		w.SetSeedForTest(20)

		center := protocol.Hex{Q: 0, R: 0}
		if !isWalkable(w, center) {
			t.Skip("origin is not walkable on this map")
		}

		monsterHex := walkableNeighbor(t, w, center)

		pid, _ := w.PlaceEntityForTest(center)
		w.SetClassForTest(pid, "") // both hands empty: closeDefFor/meleeDefsFor falls back to fists

		monsterID := w.PlaceMonsterForTest(monsterHex)

		w.SetPathForTest(pid, []protocol.Hex{monsterHex})
		w.ResolveCombatOnlyForTest()

		monster, ok := entityOfSnap(w.Snapshot(), monsterID)
		if !ok {
			t.Fatalf("monster %d missing after a single fists bump", monsterID)
		}

		if got, want := protocol.MonsterMaxHP-monster.HP, protocol.FistsDamage; got != want {
			t.Errorf("bump damage = %d, want %d (exactly one fists hit)", got, want)
		}
	})
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
