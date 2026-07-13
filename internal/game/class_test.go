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

	me, err := w.Join("", "tester", class, protocol.SpeciesHuman)
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

			_, err := w.Join("", "tester", tc.class, protocol.SpeciesHuman)
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
// class, adding the front-loaded curve's cumulative bonus (levelHPBonus) per
// level above 1.
func TestMaxHPForScalesWithLevel(t *testing.T) {
	t.Parallel()

	classes := []string{protocol.ClassFighter, protocol.ClassRogue, protocol.ClassMage}

	for _, class := range classes {
		t.Run(class, func(t *testing.T) {
			t.Parallel()

			// re-derived for front-loaded HP curve (fast-lane T2)
			for level := 1; level <= 5; level++ {
				got := game.MaxHPForTest(class, level)

				if want := game.MaxHPForTest(class, 1) + game.LevelHPBonusForTest(level); got != want {
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

// TestLevelHPBonusFrontLoaded pins the front-loaded HP curve's cumulative
// bonus directly against the spec table: gains fall 8,7,6,...,1 then floor at
// +1 per level forever (#60, roadmap XP2).
func TestLevelHPBonusFrontLoaded(t *testing.T) {
	t.Parallel()

	cases := []struct {
		level int
		want  int
	}{
		{1, 0}, {2, 8}, {3, 15}, {5, 26}, {9, 36}, {10, 37}, {12, 39},
	}
	for _, c := range cases {
		if got, want := game.LevelHPBonusForTest(c.level), c.want; got != want {
			t.Errorf("levelHPBonus(%d) = %d, want %d", c.level, got, want)
		}
	}
}

// TestMaxHPForUsesCurve pins maxHPFor's use of the front-loaded curve against
// the spec table's fighter row (base 30).
func TestMaxHPForUsesCurve(t *testing.T) {
	t.Parallel()

	// Fighter base 30: spec table pins L5 = 56, L10 = 67.
	if got, want := game.MaxHPForTest(protocol.ClassFighter, 5), 56; got != want {
		t.Errorf("maxHPFor(fighter, 5) = %d, want %d", got, want)
	}

	if got, want := game.MaxHPForTest(protocol.ClassFighter, 10), 67; got != want {
		t.Errorf("maxHPFor(fighter, 10) = %d, want %d", got, want)
	}
}

// TestRespawnScalesMaxHPToLevel: a leveled player that dies respawns with a
// MaxHP scaled to its (retained) level, via the maxHPFor recompute in the
// respawn path — not the flat level-1 bar it joined with.
func TestRespawnScalesMaxHPToLevel(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(7)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	// One level of XP: levelFor(XPCurveBase) == 2 (the level-2 floor,
	// XPCurveBase*(2-1)^2, is XPCurveBase itself). Death floors XP to this
	// level's start, so the respawn keeps level 2.
	w.SetXPForTest(me.EntityID, protocol.XPCurveBase)
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

// TestRangedWeaponByClass: Rogue has a bow (single-target, ranged), Mage has AoE
// magic, and Fighter (and any other class) has no ranged weapon. (Close-weapon
// defaults and level scaling are pinned directly against the registry in
// items_test.go's TestClassDefaultDamageMatchesLiveBalance /
// TestItemDamageScalesWithLevel — the closeWeapon/rangedWeapon helpers this
// file used to exercise were replaced by the item registry in 6b.4.)
func TestRangedWeaponByClass(t *testing.T) {
	t.Parallel()

	t.Run("rogue bow", func(t *testing.T) {
		t.Parallel()

		dmg, rangeHex, aoe, ok := game.RangedWeaponForTest(protocol.ClassRogue)
		if !ok {
			t.Fatal("rogue should have a ranged weapon")
		}

		if got, want := dmg, game.ItemDamageForTest("shortbow"); got != want {
			t.Errorf("rogue bow damage = %d, want %d", got, want)
		}

		if got, want := rangeHex, game.ItemRangeForTest("shortbow"); got != want {
			t.Errorf("rogue bow range = %d, want %d", got, want)
		}

		if got, want := aoe, 0; got != want {
			t.Errorf("rogue bow aoeRadius = %d, want %d (single target)", got, want)
		}
	})

	t.Run("mage magic", func(t *testing.T) {
		t.Parallel()

		dmg, rangeHex, aoe, ok := game.RangedWeaponForTest(protocol.ClassMage)
		if !ok {
			t.Fatal("mage should have a ranged weapon")
		}

		if got, want := dmg, game.ItemDamageForTest("ember-focus"); got != want {
			t.Errorf("mage magic damage = %d, want %d", got, want)
		}

		if got, want := rangeHex, game.ItemRangeForTest("ember-focus"); got != want {
			t.Errorf("mage magic range = %d, want %d", got, want)
		}

		if got, want := aoe, game.ItemAoERadiusForTest("ember-focus"); got != want {
			t.Errorf("mage magic aoeRadius = %d, want %d", got, want)
		}
	})

	t.Run("fighter has no ranged", func(t *testing.T) {
		t.Parallel()

		if _, _, _, ok := game.RangedWeaponForTest(protocol.ClassFighter); ok {
			t.Error("fighter should have no ranged weapon")
		}
	})
}
