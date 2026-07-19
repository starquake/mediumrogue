package game //nolint:testpackage // white-box: exercises unexported conditions; see rules_test.go.

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// dualwield_test.go (#57): condDualWielding — the batch's one new condition.
//
// The case worth writing down is the two-hander: it occupies BOTH hand slots
// but is ONE weapon, so a naive "both slots filled" reading calls it
// dual-wielding. It isn't, and a reader skimming the implementation would very
// plausibly get that wrong — which is why it gets a test rather than a comment.

// TestDualWieldingHoldsWithTwoOneHandedWeapons: the ordinary yes-case.
func TestDualWieldingHoldsWithTwoOneHandedWeapons(t *testing.T) {
	t.Parallel()

	e := &entity{
		kind: protocol.EntityPlayer, class: protocol.ClassRogue,
		equipped: map[string]itemInstance{
			protocol.SlotMainHand: {id: 1, defID: idDagger},
			protocol.SlotOffHand:  {id: 2, defID: idDagger},
		},
	}

	if !dualWieldingHolds(ruleCtx{attacker: e}) {
		t.Error("dualWielding did not hold for two one-handed weapons")
	}
}

// TestDualWieldingDoesNotHoldForATwoHander: THE trap. A two-handed weapon
// fills main-hand and locks off-hand, so both slots read as occupied — but it
// is a single weapon, and "dual-wielding" means two.
func TestDualWieldingDoesNotHoldForATwoHander(t *testing.T) {
	t.Parallel()

	var twoHanded string

	for _, def := range itemDefs {
		if def.twoHanded {
			twoHanded = def.id

			break
		}
	}

	if twoHanded == "" {
		t.Skip("no two-handed weapon in the registry")
	}

	e := &entity{
		kind: protocol.EntityPlayer, class: protocol.ClassFighter,
		equipped: map[string]itemInstance{protocol.SlotMainHand: {id: 1, defID: twoHanded}},
	}

	if dualWieldingHolds(ruleCtx{attacker: e}) {
		t.Errorf("dualWielding held for two-handed %s — one weapon is not two", twoHanded)
	}
}

// TestDualWieldingDoesNotHoldWithOneHandFull: the plain no-case.
func TestDualWieldingDoesNotHoldWithOneHandFull(t *testing.T) {
	t.Parallel()

	e := &entity{
		kind: protocol.EntityPlayer, class: protocol.ClassRogue,
		equipped: map[string]itemInstance{protocol.SlotMainHand: {id: 1, defID: idDagger}},
	}

	if dualWieldingHolds(ruleCtx{attacker: e}) {
		t.Error("dualWielding held with only one hand full")
	}
}

// TestDualWieldingReadsTheATTACKER: it is a deal-damage condition, so it asks
// about the swinger. The mirror of TestCondShieldEquippedReadsTheVICTIM — in
// rollDamageLocked the victim's own cards fold under a ctx whose .attacker is
// still the swinger, so reading the wrong side is a real and silent bug.
func TestDualWieldingReadsTheATTACKER(t *testing.T) {
	t.Parallel()

	dualWielder := &entity{
		kind: protocol.EntityPlayer, class: protocol.ClassRogue,
		equipped: map[string]itemInstance{
			protocol.SlotMainHand: {id: 1, defID: idDagger},
			protocol.SlotOffHand:  {id: 2, defID: idDagger},
		},
	}
	bare := &entity{kind: protocol.EntityPlayer, class: protocol.ClassFighter}

	if dualWieldingHolds(ruleCtx{attacker: bare, victim: dualWielder}) {
		t.Error("dualWielding read the victim's hands, not the attacker's")
	}

	if !dualWieldingHolds(ruleCtx{attacker: dualWielder, victim: bare}) {
		t.Error("dualWielding did not read the attacker's hands")
	}
}

// TestValidateRuleCardsAcceptsDualWielding: the validator knows the kind —
// one of the four places that must agree (#156 added the guide as the fourth).
func TestValidateRuleCardsAcceptsDualWielding(t *testing.T) {
	t.Parallel()

	validateRuleCards("x", []ruleCard{{
		event: evDealDamage,
		when:  []condition{{kind: condDualWielding}},
		then:  effect{kind: effMulPct, n: percentBase + 10},
	}})
}

// TestSurvivalTreeIsNotEmpty (#57): the hole this batch exists to close.
//
// Three trees are principle 1 of #61, and Survival shipped in v1 with no
// entries at all — a player spending points there had nothing to spend them
// on. Asserting it by NAME rather than counting skills, so a future refactor
// that empties it fails here loudly instead of quietly restoring the hole.
func TestSurvivalTreeIsNotEmpty(t *testing.T) {
	t.Parallel()

	for _, tree := range []string{treeClass, treeAdventure, treeSurvival} {
		n := 0

		for _, def := range skillDefs {
			if def.tree == tree {
				n++
			}
		}

		if n == 0 {
			t.Errorf("tree %q has no skills — a player cannot spend points in it", tree)
		}
	}
}

// TestCrusherScalesBluntNotSharp: the damage-type line does what its name says,
// through the real fold rather than by inspecting the card.
func TestCrusherScalesBluntNotSharp(t *testing.T) {
	t.Parallel()

	cards := skillDefByID[skillCrusher].rules

	blunt := ruleCtx{damageType: protocol.DamageTypeBlunt}
	if got, want := applyRules(evDealDamage, 10, cards, blunt), 11; got != want {
		t.Errorf("crusher on a blunt hit = %d, want %d", got, want)
	}

	sharp := ruleCtx{damageType: protocol.DamageTypeSharp}
	if got, want := applyRules(evDealDamage, 10, cards, sharp), 10; got != want {
		t.Errorf("crusher on a sharp hit = %d, want %d (unchanged)", got, want)
	}
}

// TestCrusherAndCombatTrainingSUMRatherThanCompound: the overlap flagged to the
// maintainer before build. Two +10% cards in one fold are +20%, never x1.21 —
// percentages sum within a fold and apply once. Pinning it because "stacking"
// is exactly where a reader assumes multiplication.
func TestCrusherAndCombatTrainingSUMRatherThanCompound(t *testing.T) {
	t.Parallel()

	cards := append(
		append([]ruleCard{}, skillDefByID[skillCombatTraining].rules...),
		skillDefByID[skillCrusher].rules...,
	)

	ctx := ruleCtx{damageType: protocol.DamageTypeBlunt, weapon: &itemDef{
		id: "x", itemType: protocol.ItemTypeWeapon, tags: []string{protocol.WeaponTagMelee},
	}}

	// 10 * (1 + 0.10 + 0.10) = 12, NOT 10 * 1.1 * 1.1 = 12.1 -> 12 by luck.
	// Using 100 makes the two readings differ (120 vs 121).
	if got, want := applyRules(evDealDamage, 100, cards, ctx), 120; got != want {
		t.Errorf("crusher + combat training on blunt melee = %d, want %d (sum, not compound)", got, want)
	}
}
