//nolint:testpackage // white-box: needs unexported item-registry internals (itemDefByID, ruleCard fields).
package game

import (
	"slices"
	"testing"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestCritWeaponsRegistered pins the two crit% weapons' registry shape: each
// is a single-card WHEN deal-damage IF chance N THEN mulPct 200 (the
// elf-crit card pattern, content.go:14-17, applied to an ITEM instead of a
// species).
func TestCritWeaponsRegistered(t *testing.T) {
	t.Parallel()

	cases := []struct {
		id     string
		damage int
		chance int
	}{
		// re-derived: gear keystone rebalance (misericorde 6 -> 4, duelist's
		// saber 5 -> 4).
		{idMisericorde, 4, 15},
		{idDuelistsSaber, 4, 10},
	}
	for _, c := range cases {
		def := itemDefByID[c.id]
		if def == nil {
			t.Fatalf("%s not registered", c.id)
		}

		if got, want := def.damage, c.damage; got != want {
			t.Errorf("%s damage = %d, want %d", c.id, got, want)
		}

		if got, want := len(def.rules), 1; got != want {
			t.Fatalf("%s rules = %d, want %d", c.id, got, want)
		}

		card := def.rules[0]
		if got, want := card.when[0].n, c.chance; got != want {
			t.Errorf("%s crit chance = %d, want %d", c.id, got, want)
		}

		if got, want := card.then.n, 200; got != want {
			t.Errorf("%s crit multiplier = %d, want %d (x2)", c.id, got, want)
		}
	}
}

// TestKeystoneRetagAndRebalance pins the gear keystone spec's §4 binding
// table verbatim: every weapon's damage, tags, and twoHanded — the model
// swap's single source of truth for the whole registry's retag/rebalance.
func TestKeystoneRetagAndRebalance(t *testing.T) {
	t.Parallel()

	cases := []struct {
		id     string
		damage int
		tags   []string
		twoH   bool
	}{
		{idIronSword, 4, []string{protocol.WeaponTagMelee}, false},
		{idDagger, 4, []string{protocol.WeaponTagMelee}, false},
		{idIronWarhammer, 5, []string{protocol.WeaponTagMelee}, false},
		{idButchersCleaver, 3, []string{protocol.WeaponTagMelee}, false},
		{idVenomFang, 3, []string{protocol.WeaponTagMelee}, false},
		{idAncientDwarvenMattock, 4, []string{protocol.WeaponTagMelee}, false},
		{idMisericorde, 4, []string{protocol.WeaponTagMelee}, false},
		{idDuelistsSaber, 4, []string{protocol.WeaponTagMelee}, false},
		{idWyrmslayerGreatsword, 9, []string{protocol.WeaponTagMelee}, true},
		{idShortbow, 4, []string{protocol.WeaponTagRanged}, false},
		{idPackBow, 3, []string{protocol.WeaponTagRanged}, false},
		{idOakStaff, 2, []string{protocol.WeaponTagMelee}, false},
		{idEmberFocus, 3, []string{protocol.WeaponTagMagic}, false},
		{idEmberStaff, 3, []string{protocol.WeaponTagMagic}, false},
		{idWarMageStaff, 3, []string{protocol.WeaponTagMagic}, false},
	}

	for _, c := range cases {
		def := itemDefByID[c.id]
		if def == nil {
			t.Fatalf("%s not registered", c.id)
		}

		if got, want := def.itemType, protocol.ItemTypeWeapon; got != want {
			t.Errorf("%s itemType = %q, want %q", c.id, got, want)
		}

		if got, want := def.damage, c.damage; got != want {
			t.Errorf("%s damage = %d, want %d", c.id, got, want)
		}

		if got, want := def.tags, c.tags; !slices.Equal(got, want) {
			t.Errorf("%s tags = %v, want %v", c.id, got, want)
		}

		if got, want := def.twoHanded, c.twoH; got != want {
			t.Errorf("%s twoHanded = %v, want %v", c.id, got, want)
		}
	}
}
