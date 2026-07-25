package game //nolint:testpackage // white-box: exercises the unexported skill registry.

import (
	"strings"
	"testing"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// actives_test.go (#161): active skills as a CATEGORY.
//
// Everything in #124 is passive — cards that fold onto a value at an event.
// An active is triggered, so it carries an effect and a cooldown instead of
// cards. The category exists so the second active is content rather than a
// second special case.

// TestValidateSkillDefsPanicsOnAnActiveWithCards: an active's behaviour is its
// trigger, not a fold. Carrying both would mean two mechanisms in one entry,
// and the pipeline would silently apply the cards forever.
func TestValidateSkillDefsPanicsOnAnActiveWithCards(t *testing.T) {
	t.Parallel()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("validateSkillDefs did not panic on an active carrying rule cards")
		}

		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value = %T, want string", r)
		}

		if got, want := msg, "active"; !strings.Contains(got, want) {
			t.Errorf("panic = %q, should mention %q", got, want)
		}
	}()

	validateSkillDefs([]*skillDef{{
		id: "x", tree: treeSurvival, active: &activeDef{cooldownTurns: 3, rangeHex: 3},
		rules: []ruleCard{{event: evDealDamage, then: effect{kind: effMulPct, n: percentBase}}},
	}})
}

// TestValidateSkillDefsPanicsOnAnActiveWithoutACooldown: a cooldown of zero is
// a skill with no cost, usable every turn. That is a content bug, not a design
// choice, so it fails at load rather than in play.
func TestValidateSkillDefsPanicsOnAnActiveWithoutACooldown(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("validateSkillDefs did not panic on an active with no cooldown")
		}
	}()

	validateSkillDefs([]*skillDef{{
		id: "x", tree: treeSurvival, active: &activeDef{cooldownTurns: 0, rangeHex: 3},
	}})
}

// TestValidateSkillDefsPanicsOnAnActiveOutOfReach: an active whose range
// exceeds the combat radius could take a player out of a bubble in one jump
// from anywhere inside it, which is a decision, not an accident.
func TestValidateSkillDefsPanicsOnAnActiveOutOfReach(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("validateSkillDefs did not panic on an over-long active range")
		}
	}()

	validateSkillDefs([]*skillDef{{
		id: "x", tree: treeSurvival, active: &activeDef{cooldownTurns: 3, rangeHex: 99},
	}})
}

// TestTeleportIsRegisteredAsAnActive: the first content on the new category.
func TestTeleportIsRegisteredAsAnActive(t *testing.T) {
	t.Parallel()

	def, ok := skillDefByID[skillBlink]
	if !ok {
		t.Fatal("blink is not registered")
	}

	if def.active == nil {
		t.Fatal("blink is not an active")
	}

	if got, want := def.active.cooldownTurns, 3; got != want {
		t.Errorf("blink cooldown = %d turns, want %d", got, want)
	}

	if got, want := def.active.rangeHex, 3; got != want {
		t.Errorf("blink range = %d hexes, want %d", got, want)
	}

	if len(def.rules) != 0 {
		t.Errorf("blink carries %d rule cards, want 0 — an active's behaviour is its trigger", len(def.rules))
	}
}

// --- The self-effect kind (#300) ------------------------------------------

// TestValidateSkillDefsPanicsOnASelfEffectWithoutAnEffect: a kind whose entire
// behaviour is its payload, shipped without one, would be a learnable skill
// that does nothing when triggered — the silent failure the registry exists to
// make impossible.
func TestValidateSkillDefsPanicsOnASelfEffectWithoutAnEffect(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("validateSkillDefs did not panic on a self-effect active with no effect")
		}
	}()

	validateSkillDefs([]*skillDef{{
		id: "x", tree: treeSurvival, active: &activeDef{kind: activeSelfEffect, cooldownTurns: 3},
	}})
}

// TestValidateSkillDefsPanicsOnAnEffectTheKindIgnores: the mirror. A reposition
// carrying a regen row reads as a heal-and-teleport and is not one; nothing
// would ever apply it.
func TestValidateSkillDefsPanicsOnAnEffectTheKindIgnores(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("validateSkillDefs did not panic on an effect the kind never applies")
		}
	}()

	validateSkillDefs([]*skillDef{{
		id: "x", tree: treeSurvival, active: &activeDef{
			kind: activeReposition, cooldownTurns: 3, rangeHex: 3,
			effect: &appliedEffect{effectID: idEffectRegen, magnitude: 3, turns: 3},
		},
	}})
}

// TestValidateSkillDefsPanicsOnASelfEffectNamingAnUnknownRow: the same guard
// every other effect-carrying content type gets (validateItemAppliesEffect) —
// a typo'd id must fail at load, not resolve to nothing mid-fight.
func TestValidateSkillDefsPanicsOnASelfEffectNamingAnUnknownRow(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("validateSkillDefs did not panic on an unknown effect id")
		}
	}()

	validateSkillDefs([]*skillDef{{
		id: "x", tree: treeSurvival, active: &activeDef{
			kind: activeSelfEffect, cooldownTurns: 3,
			effect: &appliedEffect{effectID: "no-such-effect", magnitude: 3, turns: 3},
		},
	}})
}

// TestValidateSkillDefsPanicsOnASelfEffectWithNoDuration: applyTimedEffectLocked
// treats turns <= 0 as a no-op, so a zero-duration payload is a skill that
// visibly fires and changes nothing.
func TestValidateSkillDefsPanicsOnASelfEffectWithNoDuration(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("validateSkillDefs did not panic on a zero-duration effect")
		}
	}()

	validateSkillDefs([]*skillDef{{
		id: "x", tree: treeSurvival, active: &activeDef{
			kind: activeSelfEffect, cooldownTurns: 3,
			effect: &appliedEffect{effectID: idEffectRegen, magnitude: 3, turns: 0},
		},
	}})
}

// TestSelfCastActivesAreRegistered: the two content rows, and the fact that
// neither carries a range — the descriptor's promise that a self-cast has
// nothing to aim at.
func TestSelfCastActivesAreRegistered(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		id, effectID string
		magnitude    int
	}{
		{skillSecondWind, idEffectRegen, 3},
		{skillBulwark, idEffectWard, percentBase - 25},
	} {
		def, ok := skillDefByID[tc.id]
		if !ok {
			t.Fatalf("%s is not registered", tc.id)
		}

		if def.active == nil {
			t.Fatalf("%s is not an active", tc.id)
		}

		if got, want := aimFor(def.active.kind), aimSelf; got != want {
			t.Errorf("%s aim = %d, want %d (self-cast)", tc.id, got, want)
		}

		if got, want := def.active.rangeHex, 0; got != want {
			t.Errorf("%s range = %d, want %d — a self-cast has nothing to aim at", tc.id, got, want)
		}

		if def.active.effect == nil {
			t.Fatalf("%s carries no effect", tc.id)
		}

		if got, want := def.active.effect.effectID, tc.effectID; got != want {
			t.Errorf("%s effect = %q, want %q", tc.id, got, want)
		}

		if got, want := def.active.effect.magnitude, tc.magnitude; got != want {
			t.Errorf("%s magnitude = %d, want %d", tc.id, got, want)
		}
	}
}

// --- The area-damage kind (#300) ------------------------------------------

// TestValidateSkillDefsPanicsOnABlastBeyondTheBubble: the same reach invariant
// every item obeys (validateMaxReach). A blast whose furthest hex sits outside
// the caster's combat bubble could kill a monster in the WORLD domain, which
// awards no kill-XP — a silent theft of progression, not a crash.
func TestValidateSkillDefsPanicsOnABlastBeyondTheBubble(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("validateSkillDefs did not panic on a blast reaching past CombatRadius")
		}
	}()

	validateSkillDefs([]*skillDef{{
		id: "x", tree: treeSurvival, active: &activeDef{
			kind: activeAreaDamage, cooldownTurns: 3,
			rangeHex: protocol.CombatRadius, aoeRadius: 1,
			damage: 5, damageType: protocol.DamageTypeFire,
		},
	}})
}

// TestValidateSkillDefsPanicsOnABlastThatDoesNothing: neither damage nor a
// rider is a cooldown spent on nothing — a learnable skill that visibly fires
// and changes no state.
func TestValidateSkillDefsPanicsOnABlastThatDoesNothing(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("validateSkillDefs did not panic on a blast with no damage and no effect")
		}
	}()

	validateSkillDefs([]*skillDef{{
		id: "x", tree: treeSurvival, active: &activeDef{
			kind: activeAreaDamage, cooldownTurns: 3, rangeHex: 3,
			aoeRadius: 1, damage: 0, damageType: protocol.DamageTypeFire,
		},
	}})
}

// TestValidateSkillDefsPanicsOnABlastPayloadTheKindIgnores: the mirror of the
// effect check. A blink carrying a blast radius reads as an explosive teleport
// and is not one.
func TestValidateSkillDefsPanicsOnABlastPayloadTheKindIgnores(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("validateSkillDefs did not panic on a blast payload the kind never fires")
		}
	}()

	validateSkillDefs([]*skillDef{{
		id: "x", tree: treeSurvival, active: &activeDef{
			kind: activeReposition, cooldownTurns: 3, rangeHex: 3, aoeRadius: 2,
		},
	}})
}

// TestEmberNovaResolvesInTheAttackPhase: the kind's defining property, asserted
// on the descriptor rather than only through a world. A blast that drifted to
// the move phase would land against POST-move positions, so walking away would
// dodge it while a flask at the same hex still connected.
func TestEmberNovaResolvesInTheAttackPhase(t *testing.T) {
	t.Parallel()

	def, ok := skillDefByID[skillEmberNova]
	if !ok {
		t.Fatal("ember nova is not registered")
	}

	if !activeResolvesInAttackPhase(def.active.kind) {
		t.Error("ember nova does not resolve in the attack phase")
	}

	if got, want := aimFor(def.active.kind), aimHex; got != want {
		t.Errorf("ember nova aim = %d, want %d (hex)", got, want)
	}

	// The reach invariant, on the shipped content and not just the validator.
	if got, want := def.active.rangeHex+def.active.aoeRadius, protocol.CombatRadius; got > want {
		t.Errorf("ember nova reach = %d, want <= %d", got, want)
	}
}
