package game_test

import (
	"errors"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// snapshotClassAndMaxHP joins a player with the given class and returns the
// Class and MaxHP the wire reports for it.
func snapshotClassAndMaxHP(t *testing.T, w *game.World, class string) (string, int) {
	t.Helper()

	me, err := w.Join("", class)
	if err != nil {
		t.Fatalf("Join(%q): %v", class, err)
	}

	e, ok := entityOfSnap(w.Snapshot(), me.EntityID)
	if !ok {
		t.Fatalf("joined entity %d missing from snapshot", me.EntityID)
	}

	return e.Class, e.MaxHP
}

// TestJoinClassSetsWireClassAndMaxHP: joining with a valid class reports that
// class on the wire with its per-class MaxHP.
func TestJoinClassSetsWireClassAndMaxHP(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		joinClass string
		wantClass string
		wantMaxHP int
	}{
		{"fighter", protocol.ClassFighter, protocol.ClassFighter, protocol.FighterMaxHP},
		{"rogue", protocol.ClassRogue, protocol.ClassRogue, protocol.RogueMaxHP},
		{"mage", protocol.ClassMage, protocol.ClassMage, protocol.MageMaxHP},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld()

			gotClass, gotMaxHP := snapshotClassAndMaxHP(t, w, tc.joinClass)

			if got, want := gotClass, tc.wantClass; got != want {
				t.Errorf("joined Class = %q, want %q", got, want)
			}

			if got, want := gotMaxHP, tc.wantMaxHP; got != want {
				t.Errorf("joined MaxHP = %d, want %d", got, want)
			}
		})
	}
}

// TestJoinRejectsInvalidClass: a new entity (no known token) must supply a
// valid class — empty or unknown is rejected with ErrInvalidClass, not
// defaulted.
func TestJoinRejectsInvalidClass(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		class string
	}{
		{"empty", ""},
		{"unknown", "necromancer"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld()

			_, err := w.Join("", tc.class)
			if got, want := err, game.ErrInvalidClass; !errors.Is(got, want) {
				t.Fatalf("Join(%q) err = %v, want %v", tc.class, got, want)
			}
		})
	}
}

// TestFighterIsTankierThanRogueAndMage: at level 1 a Fighter has strictly more
// MaxHP than a Rogue or a Mage — the class identity the design pins.
func TestFighterIsTankierThanRogueAndMage(t *testing.T) {
	t.Parallel()

	w := newWorld()

	_, fighter := snapshotClassAndMaxHP(t, w, protocol.ClassFighter)
	_, rogue := snapshotClassAndMaxHP(t, w, protocol.ClassRogue)
	_, mage := snapshotClassAndMaxHP(t, w, protocol.ClassMage)

	if fighter <= rogue || fighter <= mage {
		t.Errorf("fighter MaxHP = %d, want > rogue %d and > mage %d", fighter, rogue, mage)
	}
}

// TestMaxHPForScalesWithLevel: MaxHP grows monotonically with level for every
// class, adding HPPerLevel per level above 1.
func TestMaxHPForScalesWithLevel(t *testing.T) {
	t.Parallel()

	classes := []string{protocol.ClassFighter, protocol.ClassRogue, protocol.ClassMage}

	for _, class := range classes {
		t.Run(class, func(t *testing.T) {
			t.Parallel()

			for level := 1; level <= 5; level++ {
				got := game.MaxHPForTest(class, level)

				if want := game.MaxHPForTest(class, 1) + protocol.HPPerLevel*(level-1); got != want {
					t.Errorf("MaxHPForTest(%q, %d) = %d, want %d", class, level, got, want)
				}

				if level > 1 {
					if prev := game.MaxHPForTest(class, level-1); got <= prev {
						t.Errorf("MaxHPForTest(%q, %d) = %d, want > level %d (%d)", class, level, got, level-1, prev)
					}
				}
			}
		})
	}
}

// TestRespawnScalesMaxHPToLevel: a leveled player that dies respawns with a
// MaxHP scaled to its (retained) level, via the maxHPFor recompute in the
// respawn path — not the flat level-1 bar it joined with.
func TestRespawnScalesMaxHPToLevel(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(7)

	me, err := w.Join("", protocol.ClassFighter)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	// One level of XP: levelFor(XPPerLevel) == 2. Death floors XP to this level's
	// start, so the respawn keeps level 2.
	w.SetXPForTest(me.EntityID, protocol.XPPerLevel)
	w.SetHPForTest(me.EntityID, 0)

	snap := step(t, w) // world resolution respawns the dead player

	e, ok := entityOfSnap(snap, me.EntityID)
	if !ok {
		t.Fatalf("player %d should respawn, not vanish", me.EntityID)
	}

	want := game.MaxHPForTest(protocol.ClassFighter, 2)

	if got, want := e.MaxHP, want; got != want {
		t.Errorf("respawned level-2 fighter MaxHP = %d, want %d", got, want)
	}

	if got, want := e.HP, want; got != want {
		t.Errorf("respawned fighter HP = %d, want %d (full bar)", got, want)
	}

	if got, want := e.Level, 2; got != want {
		t.Errorf("respawned fighter Level = %d, want %d", got, want)
	}
}

// TestCloseWeaponByClass: each class's default close (bump) weapon deals its
// distinct damage, and an unknown/empty class falls back to fists.
func TestCloseWeaponByClass(t *testing.T) {
	t.Parallel()

	cases := []struct {
		class      string
		wantDamage int
	}{
		{protocol.ClassFighter, protocol.SwordDamage},
		{protocol.ClassRogue, protocol.DaggerDamage},
		{protocol.ClassMage, protocol.StaffBonkDamage},
		{"", protocol.FistsDamage},
		{"necromancer", protocol.FistsDamage},
	}

	for _, tc := range cases {
		if got, want := game.CloseWeaponDamageForTest(tc.class, 1), tc.wantDamage; got != want {
			t.Errorf("CloseWeaponDamageForTest(%q, 1) = %d, want %d", tc.class, got, want)
		}
	}
}

// TestWeaponDamageScalesWithLevel: close-weapon damage grows by DamagePerLevel
// per level above 1.
func TestWeaponDamageScalesWithLevel(t *testing.T) {
	t.Parallel()

	const level = 3

	got := game.CloseWeaponDamageForTest(protocol.ClassFighter, level)

	if want := protocol.SwordDamage + protocol.DamagePerLevel*(level-1); got != want {
		t.Errorf("CloseWeaponDamageForTest(fighter, %d) = %d, want %d", level, got, want)
	}
}

// TestRangedWeaponByClass: Rogue has a bow (single-target, ranged), Mage has AoE
// magic, and Fighter (and any other class) has no ranged weapon.
func TestRangedWeaponByClass(t *testing.T) {
	t.Parallel()

	t.Run("rogue bow", func(t *testing.T) {
		t.Parallel()

		dmg, rangeHex, aoe, ok := game.RangedWeaponForTest(protocol.ClassRogue, 1)
		if !ok {
			t.Fatal("rogue should have a ranged weapon")
		}

		if got, want := dmg, protocol.BowDamage; got != want {
			t.Errorf("rogue bow damage = %d, want %d", got, want)
		}

		if got, want := rangeHex, protocol.BowRange; got != want {
			t.Errorf("rogue bow range = %d, want %d", got, want)
		}

		if got, want := aoe, 0; got != want {
			t.Errorf("rogue bow aoeRadius = %d, want %d (single target)", got, want)
		}
	})

	t.Run("mage magic", func(t *testing.T) {
		t.Parallel()

		dmg, rangeHex, aoe, ok := game.RangedWeaponForTest(protocol.ClassMage, 1)
		if !ok {
			t.Fatal("mage should have a ranged weapon")
		}

		if got, want := dmg, protocol.StaffMagicDamage; got != want {
			t.Errorf("mage magic damage = %d, want %d", got, want)
		}

		if got, want := rangeHex, protocol.MageRange; got != want {
			t.Errorf("mage magic range = %d, want %d", got, want)
		}

		if got, want := aoe, protocol.MageAoERadius; got != want {
			t.Errorf("mage magic aoeRadius = %d, want %d", got, want)
		}
	})

	t.Run("fighter has no ranged", func(t *testing.T) {
		t.Parallel()

		if _, _, _, ok := game.RangedWeaponForTest(protocol.ClassFighter, 1); ok {
			t.Error("fighter should have no ranged weapon")
		}
	})
}
