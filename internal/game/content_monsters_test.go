package game_test

// content_monsters_test.go (#266): the content-expansion kinds through the
// LIVE combat pipeline (the damage_types_test.go equivalence style) — the
// board's first RESISTANCE monsters (skeleton, wraith), the first ice
// attacker (frost wisp) and its Fire vulnerability, and the Frostward Charm's
// Ice resist (#267), which rides here because it needs an ice source to
// prove. Each resistance/vulnerability carries its negative control, so a
// card that forgot its condition fails even as its headline passes.

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
)

// TestGoblinDealsSharp: the goblin's Rusty Shiv is a plain sharp attack (no
// cards) — a bare fighter takes its full damage through the live pipeline,
// proving the new kind engages and its natural weapon resolves.
func TestGoblinDealsSharp(t *testing.T) {
	t.Parallel()

	if got, want := gearDamageTaken(t, "goblin", ""), game.MonsterDamageForTest("goblin"); got != want {
		t.Errorf("bare fighter lost %d HP to a goblin, want %d (its Rusty Shiv)", got, want)
	}
}

// TestSkeletonResistsSharp: blades glance off bone — a sharp swing lands for
// half on a skeleton vs a ghoul (which resists neither physical type). The
// iron sword the fighter keeps is also sharp, so both hits halve.
func TestSkeletonResistsSharp(t *testing.T) {
	t.Parallel()

	if got, want := damageDealtToKind(t, "skeleton", "dagger", 1),
		damageDealtToKind(t, "ghoul", "dagger", 1)/2; got != want {
		t.Errorf("sharp swing vs skeleton dealt %d, want %d (half — bone resists sharp)", got, want)
	}
}

// TestSkeletonResistIsTypeGated: the skeleton's Sharp resist is inert against
// Blunt — a 2H blunt maul (which replaces the sharp kit) lands its full 9,
// the negative control for the resist above.
func TestSkeletonResistIsTypeGated(t *testing.T) {
	t.Parallel()

	if got, want := damageDealtToKind(t, "skeleton", "ironhead-greatmaul", 1), 9; got != want {
		t.Errorf("blunt swing vs skeleton dealt %d, want %d (unresisted — the resist is sharp-only)", got, want)
	}
}

// TestFrostWispFearsFire: the ice mirror of "trolls fear fire" — the same
// fire staff lands for +50% on a frost wisp vs a ghoul (no fire vulnerability).
func TestFrostWispFearsFire(t *testing.T) {
	t.Parallel()

	const atRange = 2 // non-adjacent: keeps the staff's adjacency card out of it

	neutral := damageDealtToKind(t, "ghoul", "ember-staff", atRange)
	if got, want := damageDealtToKind(t, "frost-wisp", "ember-staff", atRange), neutral*3/2; got != want {
		t.Errorf("ember staff vs frost wisp dealt %d, want %d (fire vulnerability +50%%)", got, want)
	}
}

// TestFrostWispDealsIceCharmResists: the Frost Wisp is the board's first ice
// attacker (Frost Touch, ice); the Frostward Charm (#267) halves it. Proven
// together because the charm's Ice resist has no live tell without an ice
// source.
func TestFrostWispDealsIceCharmResists(t *testing.T) {
	t.Parallel()

	bare := gearDamageTaken(t, "frost-wisp", "")
	if got, want := bare, game.MonsterDamageForTest("frost-wisp"); got != want {
		t.Fatalf("bare fighter lost %d HP to a frost wisp, want %d (ice claws)", got, want)
	}

	if got, want := gearDamageTaken(t, "frost-wisp", "frostward-charm"), bare/2; got != want {
		t.Errorf("ice-charmed fighter lost %d HP, want %d (half)", got, want)
	}
}

// TestFrostwardCharmIsTypeGated: the charm's Ice resist is inert against a
// wolf's Sharp fangs — the negative control for the resist above.
func TestFrostwardCharmIsTypeGated(t *testing.T) {
	t.Parallel()

	if got, want := gearDamageTaken(t, "wolf", "frostward-charm"), gearDamageTaken(t, "wolf", ""); got != want {
		t.Errorf("ice-charmed fighter lost %d HP to SHARP fangs, want %d (unchanged)", got, want)
	}
}

// TestWraithResistsPhysicalFearsHoly: the frontier elite — mundane physical
// weapons barely bite (Sharp and Blunt both ×0.5) but Holy lands +50%, so the
// Sharp/Blunt kit is the wrong tool. Each leg carries its own neutral.
func TestWraithResistsPhysicalFearsHoly(t *testing.T) {
	t.Parallel()

	// Sharp halved (the iron sword + dagger are both sharp) vs the ghoul,
	// which resists neither physical type.
	if got, want := damageDealtToKind(t, "wraith", "dagger", 1),
		damageDealtToKind(t, "ghoul", "dagger", 1)/2; got != want {
		t.Errorf("sharp swing vs wraith dealt %d, want %d (half)", got, want)
	}

	// Blunt halved: the 2H maul replaces the kit, so only its blunt swing
	// lands — 9 → 4 on the wraith, full 9 on the ghoul.
	if got, want := damageDealtToKind(t, "wraith", "ironhead-greatmaul", 1),
		damageDealtToKind(t, "ghoul", "ironhead-greatmaul", 1)/2; got != want {
		t.Errorf("blunt swing vs wraith dealt %d, want %d (half)", got, want)
	}

	// Holy +50%: the 2H Wyrmslayer replaces the kit; the troll (no holy vuln,
	// no physical resist) is the neutral.
	neutralHoly := damageDealtToKind(t, "troll", "wyrmslayer-greatsword", 1)
	if got, want := damageDealtToKind(t, "wraith", "wyrmslayer-greatsword", 1), neutralHoly*3/2; got != want {
		t.Errorf("holy swing vs wraith dealt %d, want %d (+50%% holy vulnerability)", got, want)
	}
}
