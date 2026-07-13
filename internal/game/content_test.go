//nolint:testpackage // white-box: needs unexported item-registry internals (itemDefByID, ruleCard fields).
package game

import "testing"

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
		{idMisericorde, 6, 15},
		{idDuelistsSaber, 5, 10},
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
