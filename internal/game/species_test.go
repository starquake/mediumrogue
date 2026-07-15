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
//
// re-derived: #116 melee-as-attack-intent — elfMeleeDamage's attacker now
// submits an entity-targeted attack intent instead of walking onto the
// monster's hex, so its melee resolves via resolveEntityTargetedLocked (a
// named victim id) instead of the old move-conversion path
// (collectMeleeAttacksLocked/attackLocked), which always drew
// rng.IntN(len(victims)) — even for a single-candidate hex — before the
// crit-chance roll. With that draw gone, the elf's crit-chance card becomes
// the melee attack's FIRST rng draw rather than its second, which redraws a
// different PCG output for the same seed. bowCritSeed/bowMissSeed are
// UNCHANGED: elfBowDamage still submits a ground-targeted ranged attack
// intent (untouched by this migration), so its draw order is the same as
// before. Re-hunted meleeCritSeed by scanning seeds 0-39 through the
// migrated elfMeleeDamage helper for a dealt value != the sword base (4):
// seed 0 (meleeMissSeed, unchanged) still misses; seed 4 is the first seed
// in the scanned range that crits.
const (
	meleeCritSeed = 4 // elf melee attack crits at this seed
	meleeMissSeed = 0 // elf melee attack misses (base damage) at this seed
	bowCritSeed   = 1 // elf bow shot crits at this seed
	bowMissSeed   = 0 // elf bow shot misses (base damage) at this seed
)

// TestHumanKillXPBonus: on the same shared kill, a Human is paid
// killXP * (100+HumanXPBonusPercent)/100 (i.e. 1.5x at +50%, over the slain
// kind's own XP — wolf's here) while a non-Human (Dwarf here) earns the
// unmodified kill XP. Both survive the fight, so both are credited —
// isolating the species multiplier as the only difference.
func TestHumanKillXPBonus(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(11)

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	ns := game.HexNeighbors(center)

	human, humanTok := w.PlaceEntityForTest(ns[0])
	w.SetSpeciesForTest(human, protocol.SpeciesHuman)

	dwarf, dwarfTok := w.PlaceEntityForTest(ns[1])
	w.SetSpeciesForTest(dwarf, protocol.SpeciesDwarf)

	monsterID := w.PlaceMonsterForTest(center)
	w.SetHPForTest(monsterID, game.ItemDamageForTest("iron-sword")) // dies to a single hit

	// One world resolution forms the bubble around the two idle players and the
	// monster; the kill then lands inside the bubble (kill XP is only earned in a
	// real fight).
	step(t, w)

	if err := w.SubmitIntent(entityAttackIntent(human, humanTok, monsterID)); err != nil {
		t.Fatalf("SubmitIntent(melee, human): %v", err)
	}

	if err := w.SubmitIntent(entityAttackIntent(dwarf, dwarfTok, monsterID)); err != nil {
		t.Fatalf("SubmitIntent(melee, dwarf): %v", err)
	}

	step(t, w)

	if _, ok := entityOfSnap(w.Snapshot(), monsterID); ok {
		t.Fatalf("monster %d should have died to the shared melee attacks", monsterID)
	}

	wantHuman := game.MonsterXPForTest("wolf") * (100 + protocol.HumanXPBonusPercent) / 100
	if got, want := w.XPForTest(human), wantHuman; got != want {
		t.Errorf("Human XP = %d, want %d (wolf's kill XP +%d%%)", got, want, protocol.HumanXPBonusPercent)
	}

	if got, want := w.XPForTest(dwarf), game.MonsterXPForTest("wolf"); got != want {
		t.Errorf("non-Human (Dwarf) XP = %d, want the flat %d", got, want)
	}
}

// elfMeleeDamage places an elf of the given class at the origin, melee-attacks
// a plain (species-less) monster at a neighbour, and returns the damage the
// monster took. The seed pins the elf crit roll.
func elfMeleeDamage(t *testing.T, seed int64) int {
	t.Helper()

	w := newWorld()
	w.SetSeedForTest(seed)

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	monsterHex := walkableNeighbor(t, w, center)

	pid, tok := w.PlaceEntityForTest(center) // level-1 Fighter (sword)
	w.SetSpeciesForTest(pid, protocol.SpeciesElf)

	monsterID := w.PlaceMonsterForTest(monsterHex)

	if err := w.SubmitIntent(entityAttackIntent(pid, tok, monsterID)); err != nil {
		t.Fatalf("SubmitIntent(melee): %v", err)
	}

	w.ResolveCombatOnlyForTest()

	monster, ok := entityOfSnap(w.Snapshot(), monsterID)
	if !ok {
		t.Fatalf("monster %d missing after an elf melee attack (unexpected kill)", monsterID)
	}

	return protocol.MonsterMaxHP - monster.HP
}

// TestElfCritMelee: an elf's melee attack deals ElfCritMultiplier x base on a
// crit and exactly the base on a miss — proving both branches of the crit
// roll.
func TestElfCritMelee(t *testing.T) {
	t.Parallel()

	swordDamage := game.ItemDamageForTest("iron-sword")

	t.Run("crit", func(t *testing.T) {
		t.Parallel()

		if got, want := elfMeleeDamage(t, meleeCritSeed), protocol.ElfCritMultiplier*swordDamage; got != want {
			t.Errorf("elf crit melee attack = %d, want %d (%dx sword)", got, want, protocol.ElfCritMultiplier)
		}
	})

	t.Run("miss", func(t *testing.T) {
		t.Parallel()

		if got, want := elfMeleeDamage(t, meleeMissSeed), swordDamage; got != want {
			t.Errorf("elf non-crit melee attack = %d, want %d (base sword)", got, want)
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

	bowDamage := game.ItemDamageForTest("shortbow")

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

// TestDwarfDamageReductionMelee: a Dwarf player struck by a monster's melee
// takes the monster's claws damage (wolf's, here) - DwarfDamageReduction. DR
// is a victim-side passive, so it applies to a dwarf being hit (the attacker
// here is a species-less monster).
func TestDwarfDamageReductionMelee(t *testing.T) {
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

	// Monster strikes the dwarf; the dwarf has no path, so it does not hit back.
	w.SetPathForTest(monsterID, []protocol.Hex{center})
	w.ResolveCombatOnlyForTest()

	player, ok := entityOfSnap(w.Snapshot(), pid)
	if !ok {
		t.Fatalf("dwarf %d missing after a monster melee attack", pid)
	}

	wantHP := protocol.FighterMaxHP - (game.MonsterDamageForTest("wolf") - protocol.DwarfDamageReduction)
	if got, want := player.HP, wantHP; got != want {
		t.Errorf("dwarf HP after monster melee attack = %d, want %d (hit reduced by %d)",
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

	if got, want := 100-monster.HP, game.ItemDamageForTest("shortbow")-protocol.DwarfDamageReduction; got != want {
		t.Errorf("dwarf ranged hit = %d, want %d (bow reduced by %d)",
			got, want, protocol.DwarfDamageReduction)
	}
}

// TestDwarfDamageReductionFloor: DR never zeroes a hit — a 1-damage fists melee
// attack (base FistsDamage minus DwarfDamageReduction would be 0) still lands
// for 1.
func TestDwarfDamageReductionFloor(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(32)

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	monsterHex := walkableNeighbor(t, w, center)

	pid, tok := w.PlaceEntityForTest(center)
	w.SetClassForTest(pid, "") // unarmed: closeDefFor falls back to fists (1 damage)

	monsterID := w.PlaceMonsterForTest(monsterHex)
	w.SetSpeciesForTest(monsterID, protocol.SpeciesDwarf)

	if err := w.SubmitIntent(entityAttackIntent(pid, tok, monsterID)); err != nil {
		t.Fatalf("SubmitIntent(melee): %v", err)
	}

	w.ResolveCombatOnlyForTest()

	monster, ok := entityOfSnap(w.Snapshot(), monsterID)
	if !ok {
		t.Fatalf("dwarf monster %d missing after a fists melee attack", monsterID)
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

	pid, tok := w.PlaceEntityForTest(center) // level-1 Fighter (sword)
	w.SetSpeciesForTest(pid, protocol.SpeciesElf)

	monsterID := w.PlaceMonsterForTest(monsterHex)
	w.SetSpeciesForTest(monsterID, protocol.SpeciesDwarf)

	if err := w.SubmitIntent(entityAttackIntent(pid, tok, monsterID)); err != nil {
		t.Fatalf("SubmitIntent(melee): %v", err)
	}

	w.ResolveCombatOnlyForTest()

	monster, ok := entityOfSnap(w.Snapshot(), monsterID)
	if !ok {
		t.Fatalf("dwarf monster %d missing after an elf crit melee attack", monsterID)
	}

	wantDealt := protocol.ElfCritMultiplier*game.ItemDamageForTest("iron-sword") - protocol.DwarfDamageReduction
	if got := protocol.MonsterMaxHP - monster.HP; got != wantDealt {
		t.Errorf("elf-crits-dwarf dealt = %d, want %d (crit THEN DR)", got, wantDealt)
	}

	// Guard against the reversed order producing the same number by accident.
	reversed := (game.ItemDamageForTest("iron-sword") - protocol.DwarfDamageReduction) * protocol.ElfCritMultiplier
	if reversed == wantDealt {
		t.Fatalf("test cannot distinguish ordering: crit-then-DR (%d) == DR-then-crit (%d)", wantDealt, reversed)
	}
}

// TestNonElfNeverCrits: a non-elf attacker never crits, whatever the seed — its
// melee attack deals exactly the base weapon damage across a sweep of seeds, so
// a Human/Dwarf/species-less attacker is byte-for-byte the pre-species behaviour.
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

				pid, tok := w.PlaceEntityForTest(center) // Fighter (sword)
				w.SetSpeciesForTest(pid, sp.val)

				monsterID := w.PlaceMonsterForTest(monsterHex)

				if err := w.SubmitIntent(entityAttackIntent(pid, tok, monsterID)); err != nil {
					t.Fatalf("SubmitIntent(melee): %v", err)
				}

				w.ResolveCombatOnlyForTest()

				monster, ok := entityOfSnap(w.Snapshot(), monsterID)
				if !ok {
					t.Fatalf("monster %d missing (seed %d, %s)", monsterID, seed, sp.name)
				}

				if got, want := protocol.MonsterMaxHP-monster.HP, game.ItemDamageForTest("iron-sword"); got != want {
					t.Fatalf("%s melee attack at seed %d = %d, want %d (no crit for non-elf)",
						sp.name, seed, got, want)
				}
			}
		})
	}
}

// Pinned seeds for the Misericorde's own crit roll (rng.IntN(100) < 15, the
// item card's chance — distinct from ElfCritChancePercent's roll). Found the
// same way meleeCritSeed/meleeMissSeed were: scanning seeds 0-39 with a
// human Rogue (no species crit in play) wielding the Misericorde against a
// fat-HP monster and printing dealt damage per seed — seed 0 misses (dealt
// base 4, gear keystone rebalance), seed 4 procs (dealt 8, the x2). They
// happen to equal meleeCritSeed/meleeMissSeed because both scenarios are a
// single first melee attack on a fresh RNG stream, drawing the same one
// chance roll at the same pipeline position — a coincidence of this test's
// setup, not a rule.
//
// re-derived: #116 melee-as-attack-intent — misericordeMeleeDamage's
// attacker now submits an entity-targeted attack intent instead of walking
// onto the monster's hex, for the same reason and with the same
// stream-shift as meleeCritSeed above (the move-conversion path's
// rng.IntN(len(victims)) victim-pick draw, always taken even for a single
// candidate, no longer precedes the weapon's own crit-chance roll). Re-hunted
// by scanning seeds 0-39 through the migrated misericordeMeleeDamage helper:
// seed 0 (misericordeMissSeed, unchanged) still misses; seed 4 is the first
// seed in the scanned range that procs — and it coincidentally still equals
// the re-derived meleeCritSeed, for the same reason the pair coincided
// before (both are a single first melee attack on a fresh stream).
const (
	misericordeCritSeed = 4 // Misericorde procs (double damage) at this seed
	misericordeMissSeed = 0 // Misericorde does not proc (base damage) at this seed
)

// misericordeMeleeDamage places a human (non-elf) Rogue wielding the
// Misericorde at the origin, melee-attacks a fat-HP monster at a neighbour so
// it survives even a crit, and returns the damage dealt — isolating the
// weapon's own crit card as the only source of a multiplier in play.
func misericordeMeleeDamage(t *testing.T, seed int64) int {
	t.Helper()

	w := newWorld()
	w.SetSeedForTest(seed)

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	monsterHex := walkableNeighbor(t, w, center)

	pid, tok := w.PlaceEntityForTest(center)
	w.SetClassForTest(pid, protocol.ClassRogue) // class is irrelevant now (gates dropped, #56)
	w.SetSpeciesForTest(pid, protocol.SpeciesHuman)

	instID := w.GrantItemForTest(pid, "misericorde")
	if err := w.SubmitIntent(equipIntent(pid, tok, instID)); err != nil {
		t.Fatalf("SubmitIntent(equip Misericorde): %v", err)
	}

	const fatHP = 100

	monsterID := w.PlaceMonsterForTest(monsterHex)
	w.SetHPForTest(monsterID, fatHP) // survives even a crit, so HP is readable

	if err := w.SubmitIntent(entityAttackIntent(pid, tok, monsterID)); err != nil {
		t.Fatalf("SubmitIntent(melee): %v", err)
	}

	w.ResolveCombatOnlyForTest()

	monster, ok := entityOfSnap(w.Snapshot(), monsterID)
	if !ok {
		t.Fatalf("monster %d missing after a Misericorde melee attack (unexpected kill)", monsterID)
	}

	return fatHP - monster.HP
}

// TestMisericordeCritProcsSeeded: the Misericorde's 15% crit card deals
// exactly double its 4 base damage (8) on a proc and exactly its base (4)
// on a miss — re-derived: gear keystone rebalance (base 6 -> 4).
func TestMisericordeCritProcsSeeded(t *testing.T) {
	t.Parallel()

	const misericordeDamage = 4

	t.Run("proc", func(t *testing.T) {
		t.Parallel()

		if got, want := misericordeMeleeDamage(t, misericordeCritSeed), 2*misericordeDamage; got != want {
			t.Errorf("Misericorde proc melee attack = %d, want %d (2x base %d)", got, want, misericordeDamage)
		}
	})

	t.Run("no proc", func(t *testing.T) {
		t.Parallel()

		if got, want := misericordeMeleeDamage(t, misericordeMissSeed), misericordeDamage; got != want {
			t.Errorf("Misericorde non-proc melee attack = %d, want %d (base)", got, want)
		}
	})
}
