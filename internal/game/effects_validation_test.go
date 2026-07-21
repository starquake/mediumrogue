package game //nolint:testpackage // white-box: exercises the unexported validators; see actives_test.go.

import (
	"strings"
	"testing"
)

// effects_validation_test.go (#271): the load-time, fail-loud half of the
// timed-effect foundation — an invalid effect def or on-hit rider must panic at
// process start, never no-op mid-fight. Mirrors actives_test.go's
// recover()-and-check-the-message shape.

// wantPanicContaining runs fn and fails the test unless it panics with a string
// message containing want.
func wantPanicContaining(t *testing.T, want string, fn func()) {
	t.Helper()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected a panic mentioning %q, got none", want)
		}

		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value = %T, want string", r)
		}

		if got := msg; !strings.Contains(got, want) {
			t.Errorf("panic = %q, should mention %q", got, want)
		}
	}()

	fn()
}

// TestLiveEffectDefsAreValid: the shipped registry passes its own validator, so
// this test also guards the shape of every effect def we add.
func TestLiveEffectDefsAreValid(t *testing.T) {
	t.Parallel()

	validateEffectDefs(effectDefs) // must not panic
}

// TestValidateEffectDefsPanicsOnUnknownEvent: an effect def that folds at an
// event the pipeline does not implement panics — the synthesized card is run
// through the same validateRuleCards content uses, so unknown vocabulary can
// never slip in as an effect.
func TestValidateEffectDefsPanicsOnUnknownEvent(t *testing.T) {
	t.Parallel()

	wantPanicContaining(t, "unknown event", func() {
		validateEffectDefs([]*effectDef{{id: "x", name: "X", event: "no-such-event", effect: effAdd}})
	})
}

// TestValidateEffectDefsPanicsOnUnknownEffectVerb: likewise for the effect verb.
func TestValidateEffectDefsPanicsOnUnknownEffectVerb(t *testing.T) {
	t.Parallel()

	wantPanicContaining(t, "unknown effect", func() {
		validateEffectDefs([]*effectDef{{id: "x", name: "X", event: evEndOfTurn, effect: "no-such-verb"}})
	})
}

// TestValidateEffectDefsPanicsOnDuplicateID: two defs with the same id would
// make effectDefByID lookups ambiguous — caught at load.
func TestValidateEffectDefsPanicsOnDuplicateID(t *testing.T) {
	t.Parallel()

	wantPanicContaining(t, "duplicate effect id", func() {
		validateEffectDefs([]*effectDef{
			{id: "same-id", name: "A", event: evEndOfTurn, effect: effAdd},
			{id: "same-id", name: "B", event: evDealDamage, effect: effMulPct},
		})
	})
}

// TestValidateRuleConditionRejectsChanceOnEndOfTurn pins the no-rng guard: the
// end-of-turn fold (tickEffectsLocked) builds a bare ruleCtx with no rng, so a
// chance condition on an evEndOfTurn card would nil-deref conditionHolds'
// ctx.rng mid-turn — reject it at load, exactly as earn-xp does.
func TestValidateRuleConditionRejectsChanceOnEndOfTurn(t *testing.T) {
	t.Parallel()

	wantPanicContaining(t, "without rng", func() {
		validateRuleCondition("test", evEndOfTurn, condition{kind: condChance, n: 50})
	})
}

// TestValidateItemOnHitPanicsOnUnknownEffect: an onHit rider that names an
// unregistered effect would silently no-op in play — caught at load.
func TestValidateItemOnHitPanicsOnUnknownEffect(t *testing.T) {
	t.Parallel()

	wantPanicContaining(t, "unknown effect", func() {
		validateItemOnHit(&itemDef{
			id: "x", itemType: "weapon",
			onHit: []appliedEffect{{effectID: "no-such-effect", magnitude: -1, turns: 2}},
		})
	})
}

// TestValidateItemOnHitPanicsOnNonWeapon: onHit fires on a melee hit, so it is a
// weapon-only rider — armor carrying one is an authoring mistake.
func TestValidateItemOnHitPanicsOnNonWeapon(t *testing.T) {
	t.Parallel()

	wantPanicContaining(t, "must not set onHit", func() {
		validateItemOnHit(&itemDef{
			id: "x", itemType: "chest",
			onHit: []appliedEffect{{effectID: idEffectPoison, magnitude: -1, turns: 2}},
		})
	})
}

// TestValidateItemOnHitPanicsOnNonPositiveTurns: a zero-turn effect is nothing —
// applyTimedEffectLocked would drop it — so it fails at load rather than
// shipping a weapon whose rider silently never applies.
func TestValidateItemOnHitPanicsOnNonPositiveTurns(t *testing.T) {
	t.Parallel()

	wantPanicContaining(t, "turns > 0", func() {
		validateItemOnHit(&itemDef{
			id: "x", itemType: "weapon",
			onHit: []appliedEffect{{effectID: idEffectPoison, magnitude: -1, turns: 0}},
		})
	})
}
