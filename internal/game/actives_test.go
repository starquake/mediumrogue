package game //nolint:testpackage // white-box: exercises the unexported skill registry.

import (
	"strings"
	"testing"
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
