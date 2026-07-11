package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// Pinned seeds for the elf crit roll (rng.IntN(100) < ElfCritChancePercent).
// Found by probing the exact draw order of each scenario: a crit seed forces the
// roll under the threshold, a miss seed forces it over. Different scenarios draw
// the rng differently (melee vs bow — the bow site now draws the victim-pick
// roll before the crit-chance roll, pipeline order per rollDamageLocked), so
// each has its own pair.
const (
	meleeCritSeed = 1 // elf melee bump crits at this seed
	meleeMissSeed = 0 // elf melee bump misses (base damage) at this seed
	bowCritSeed   = 1 // elf bow shot crits at this seed
	bowMissSeed   = 0 // elf bow shot misses (base damage) at this seed
)

// TestHumanKillXPBonus: on the same shared kill, a Human is paid
// MonsterXP * (100+HumanXPBonusPercent)/100 (i.e. 1.5x at +50%) while a
// non-Human (Dwarf here) earns the flat MonsterXP. Both survive the fight, so
// both are credited — isolating the species multiplier as the only difference.
func TestHumanKillXPBonus(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(11)

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	ns := game.HexNeighbors(center)

	human, _ := w.PlaceEntityForTest(ns[0])
	w.SetSpeciesForTest(human, protocol.SpeciesHuman)

	dwarf, _ := w.PlaceEntityForTest(ns[1])
	w.SetSpeciesForTest(dwarf, protocol.SpeciesDwarf)

	monsterID := w.PlaceMonsterForTest(center)
	w.SetHPForTest(monsterID, game.ItemDamageForTest("iron-sword", 1)) // dies to a single hit

	// One world resolution forms the bubble around the two idle players and the
	// monster; the kill then lands inside the bubble (kill XP is only earned in a
	// real fight).
	step(t, w)

	w.SetPathForTest(human, []protocol.Hex{center})
	w.SetPathForTest(dwarf, []protocol.Hex{center})
	step(t, w)

	if _, ok := entityOfSnap(w.Snapshot(), monsterID); ok {
		t.Fatalf("monster %d should have died to the shared bumps", monsterID)
	}

	wantHuman := game.MonsterXPForTest("wolf") * (100 + protocol.HumanXPBonusPercent) / 100
	if got, want := w.XPForTest(human), wantHuman; got != want {
		t.Errorf("Human XP = %d, want %d (MonsterXP +%d%%)", got, want, protocol.HumanXPBonusPercent)
	}

	if got, want := w.XPForTest(dwarf), game.MonsterXPForTest("wolf"); got != want {
		t.Errorf("non-Human (Dwarf) XP = %d, want the flat %d", got, want)
	}
}

// elfBumpDamage places an elf of the given class at the origin, bumps a plain
// (species-less) monster at a neighbour, and returns the damage the monster
// took. The seed pins the elf crit roll.
func elfBumpDamage(t *testing.T, seed int64) int {
	t.Helper()

	w := newWorld()
	w.SetSeedForTest(seed)

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	monsterHex := walkableNeighbor(t, w, center)

	pid, _ := w.PlaceEntityForTest(center) // level-1 Fighter (sword)
	w.SetSpeciesForTest(pid, protocol.SpeciesElf)

	monsterID := w.PlaceMonsterForTest(monsterHex)

	w.SetPathForTest(pid, []protocol.Hex{monsterHex})
	w.ResolveCombatOnlyForTest()

	monster, ok := entityOfSnap(w.Snapshot(), monsterID)
	if !ok {
		t.Fatalf("monster %d missing after an elf bump (unexpected kill)", monsterID)
	}

	return protocol.MonsterMaxHP - monster.HP
}

// TestElfCritMelee: an elf's melee bump deals ElfCritMultiplier x base on a crit
// and exactly the base on a miss — proving both branches of the crit roll.
func TestElfCritMelee(t *testing.T) {
	t.Parallel()

	swordDamage := game.ItemDamageForTest("iron-sword", 1)

	t.Run("crit", func(t *testing.T) {
		t.Parallel()

		if got, want := elfBumpDamage(t, meleeCritSeed), protocol.ElfCritMultiplier*swordDamage; got != want {
			t.Errorf("elf crit bump = %d, want %d (%dx sword)", got, want, protocol.ElfCritMultiplier)
		}
	})

	t.Run("miss", func(t *testing.T) {
		t.Parallel()

		if got, want := elfBumpDamage(t, meleeMissSeed), swordDamage; got != want {
			t.Errorf("elf non-crit bump = %d, want %d (base sword)", got, want)
		}
	})
}

// elfBowDamage places an elf Rogue at the origin, shoots a plain (species-less)
// monster three hexes away (given a fat HP pool so a crit does not kill it), and
// returns the damage the monster took. The seed pins the elf crit roll.
func elfBowDamage(t *testing.T, seed int64) int {
	t.Helper()

	w := newWorld()
	w.SetSeedForTest(seed)

	rogueHex := protocol.Hex{Q: 0, R: 0}
	monsterHex := protocol.Hex{Q: 3, R: 0} // distance 3 <= shortbow range (4), not adjacent

	rogueID, token := w.PlaceEntityForTest(rogueHex)
	w.SetClassForTest(rogueID, protocol.ClassRogue)
	w.SetSpeciesForTest(rogueID, protocol.SpeciesElf)

	const fatHP = 100

	monsterID := w.PlaceMonsterForTest(monsterHex)
	w.SetHPForTest(monsterID, fatHP) // survives even a crit, so HP is readable

	if err := w.SubmitIntent(attackIntent(rogueID, token, monsterHex)); err != nil {
		t.Fatalf("SubmitIntent(attack): %v", err)
	}

	w.ResolveCombatOnlyForTest()

	monster, ok := entityOfSnap(w.Snapshot(), monsterID)
	if !ok {
		t.Fatalf("monster %d missing after an elf bow shot", monsterID)
	}

	return fatHP - monster.HP
}

// TestElfCritBow: an elf's bow shot deals ElfCritMultiplier x base on a crit and
// exactly the base on a miss — the same passive on the ranged path.
func TestElfCritBow(t *testing.T) {
	t.Parallel()

	bowDamage := game.ItemDamageForTest("shortbow", 1)

	t.Run("crit", func(t *testing.T) {
		t.Parallel()

		if got, want := elfBowDamage(t, bowCritSeed), protocol.ElfCritMultiplier*bowDamage; got != want {
			t.Errorf("elf crit bow = %d, want %d (%dx bow)", got, want, protocol.ElfCritMultiplier)
		}
	})

	t.Run("miss", func(t *testing.T) {
		t.Parallel()

		if got, want := elfBowDamage(t, bowMissSeed), bowDamage; got != want {
			t.Errorf("elf non-crit bow = %d, want %d (base bow)", got, want)
		}
	})
}

// TestDwarfDamageReductionBump: a Dwarf player struck by a monster's melee takes
// MonsterAttackDamage - DwarfDamageReduction. DR is a victim-side passive, so it
// applies to a dwarf being hit (the attacker here is a species-less monster).
func TestDwarfDamageReductionBump(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(30)

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	monsterHex := walkableNeighbor(t, w, center)

	pid, _ := w.PlaceEntityForTest(center) // level-1 Fighter, FighterMaxHP
	w.SetSpeciesForTest(pid, protocol.SpeciesDwarf)

	monsterID := w.PlaceMonsterForTest(monsterHex)

	// Monster bumps the dwarf; the dwarf has no path, so it does not hit back.
	w.SetPathForTest(monsterID, []protocol.Hex{center})
	w.ResolveCombatOnlyForTest()

	player, ok := entityOfSnap(w.Snapshot(), pid)
	if !ok {
		t.Fatalf("dwarf %d missing after a monster bump", pid)
	}

	wantHP := protocol.FighterMaxHP - (game.MonsterDamageForTest("wolf") - protocol.DwarfDamageReduction)
	if got, want := player.HP, wantHP; got != want {
		t.Errorf("dwarf HP after monster bump = %d, want %d (hit reduced by %d)",
			got, want, protocol.DwarfDamageReduction)
	}
}

// TestDwarfDamageReductionRanged: a ranged hit landing on a Dwarf is reduced by
// DwarfDamageReduction too — the passive is at the damage site, not the melee
// path. No monster carries a ranged weapon and players do not friendly-fire, so
// the dwarf victim here is a dwarf-species monster (via the test bridge) shot by
// a (non-elf) rogue: it exercises applyDR on the bow path directly.
func TestDwarfDamageReductionRanged(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(31)

	rogueHex := protocol.Hex{Q: 0, R: 0}
	monsterHex := protocol.Hex{Q: 3, R: 0}

	rogueID, token := w.PlaceEntityForTest(rogueHex)
	w.SetClassForTest(rogueID, protocol.ClassRogue)
	w.SetSpeciesForTest(rogueID, protocol.SpeciesHuman) // non-elf: no crit in play

	monsterID := w.PlaceMonsterForTest(monsterHex)
	w.SetHPForTest(monsterID, 100) // survives so HP is readable
	w.SetSpeciesForTest(monsterID, protocol.SpeciesDwarf)

	if err := w.SubmitIntent(attackIntent(rogueID, token, monsterHex)); err != nil {
		t.Fatalf("SubmitIntent(attack): %v", err)
	}

	w.ResolveCombatOnlyForTest()

	monster, ok := entityOfSnap(w.Snapshot(), monsterID)
	if !ok {
		t.Fatalf("dwarf monster %d missing after a bow shot", monsterID)
	}

	if got, want := 100-monster.HP, game.ItemDamageForTest("shortbow", 1)-protocol.DwarfDamageReduction; got != want {
		t.Errorf("dwarf ranged hit = %d, want %d (bow reduced by %d)",
			got, want, protocol.DwarfDamageReduction)
	}
}

// TestDwarfDamageReductionFloor: DR never zeroes a hit — a 1-damage fists bump
// (base FistsDamage minus DwarfDamageReduction would be 0) still lands for 1.
func TestDwarfDamageReductionFloor(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(32)

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	monsterHex := walkableNeighbor(t, w, center)

	pid, _ := w.PlaceEntityForTest(center)
	w.SetClassForTest(pid, "") // unarmed: closeDefFor falls back to fists (1 damage)

	monsterID := w.PlaceMonsterForTest(monsterHex)
	w.SetSpeciesForTest(monsterID, protocol.SpeciesDwarf)

	w.SetPathForTest(pid, []protocol.Hex{monsterHex})
	w.ResolveCombatOnlyForTest()

	monster, ok := entityOfSnap(w.Snapshot(), monsterID)
	if !ok {
		t.Fatalf("dwarf monster %d missing after a fists bump", monsterID)
	}

	if got, want := protocol.MonsterMaxHP-monster.HP, 1; got != want {
		t.Errorf("dwarf floored hit = %d, want %d (FistsDamage %d - DR %d, floored at 1)",
			got, want, protocol.FistsDamage, protocol.DwarfDamageReduction)
	}
}

// TestElfCritThenDwarfDR: when an elf crits a dwarf, the crit multiplies the
// base first and DR is subtracted from that product — ElfCritMultiplier*base -
// DwarfDamageReduction. A wrong order (DR before crit) would give
// (base-DR)*ElfCritMultiplier, a different number, so the exact assertion pins
// the ordering. The dwarf victim is a dwarf-species monster (via the bridge) so
// an elf player can actually strike it.
func TestElfCritThenDwarfDR(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(meleeCritSeed) // forces the elf crit

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	monsterHex := walkableNeighbor(t, w, center)

	pid, _ := w.PlaceEntityForTest(center) // level-1 Fighter (sword)
	w.SetSpeciesForTest(pid, protocol.SpeciesElf)

	monsterID := w.PlaceMonsterForTest(monsterHex)
	w.SetSpeciesForTest(monsterID, protocol.SpeciesDwarf)

	w.SetPathForTest(pid, []protocol.Hex{monsterHex})
	w.ResolveCombatOnlyForTest()

	monster, ok := entityOfSnap(w.Snapshot(), monsterID)
	if !ok {
		t.Fatalf("dwarf monster %d missing after an elf crit bump", monsterID)
	}

	wantDealt := protocol.ElfCritMultiplier*game.ItemDamageForTest("iron-sword", 1) - protocol.DwarfDamageReduction
	if got := protocol.MonsterMaxHP - monster.HP; got != wantDealt {
		t.Errorf("elf-crits-dwarf dealt = %d, want %d (crit THEN DR)", got, wantDealt)
	}

	// Guard against the reversed order producing the same number by accident.
	reversed := (game.ItemDamageForTest("iron-sword", 1) - protocol.DwarfDamageReduction) * protocol.ElfCritMultiplier
	if reversed == wantDealt {
		t.Fatalf("test cannot distinguish ordering: crit-then-DR (%d) == DR-then-crit (%d)", wantDealt, reversed)
	}
}

// TestNonElfNeverCrits: a non-elf attacker never crits, whatever the seed — its
// melee bump deals exactly the base weapon damage across a sweep of seeds, so a
// Human/Dwarf/species-less attacker is byte-for-byte the pre-species behaviour.
func TestNonElfNeverCrits(t *testing.T) {
	t.Parallel()

	species := []struct {
		name string
		val  string
	}{
		{"human", protocol.SpeciesHuman},
		{"dwarf", protocol.SpeciesDwarf},
		{"none", ""},
	}

	for _, sp := range species {
		t.Run(sp.name, func(t *testing.T) {
			t.Parallel()

			for seed := range int64(20) {
				w := newWorld()
				w.SetSeedForTest(seed)

				center := protocol.Hex{Q: 0, R: 0}
				if !isWalkable(w, center) {
					t.Skip("origin is not walkable on this map")
				}

				monsterHex := walkableNeighbor(t, w, center)

				pid, _ := w.PlaceEntityForTest(center) // Fighter (sword)
				w.SetSpeciesForTest(pid, sp.val)

				monsterID := w.PlaceMonsterForTest(monsterHex)

				w.SetPathForTest(pid, []protocol.Hex{monsterHex})
				w.ResolveCombatOnlyForTest()

				monster, ok := entityOfSnap(w.Snapshot(), monsterID)
				if !ok {
					t.Fatalf("monster %d missing (seed %d, %s)", monsterID, seed, sp.name)
				}

				if got, want := protocol.MonsterMaxHP-monster.HP, game.ItemDamageForTest("iron-sword", 1); got != want {
					t.Fatalf("%s bump at seed %d = %d, want %d (no crit for non-elf)",
						sp.name, seed, got, want)
				}
			}
		})
	}
}
