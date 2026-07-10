// Package game (white-box): exercises unexported rule-pipeline internals
// (ruleCard, applyRules, speciesCards) directly — there is no exported
// surface for these, by design (see rules.go).
//
//nolint:testpackage // see file doc above
package game

import (
	mrand "math/rand/v2"
	"testing"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// testRNG returns a deterministic rng for condition tests.
//
//nolint:gosec // deterministic test RNG, not security-sensitive.
func testRNG(seed uint64) *mrand.Rand { return mrand.New(mrand.NewPCG(seed, 0)) }

func TestApplyRulesFoldOrder(t *testing.T) {
	t.Parallel()

	// adds sum first, then multipliers, then the event clamp: (10+3-1)*200/100 = 24.
	cards := []ruleCard{
		{event: evDealDamage, then: effect{kind: effAdd, n: 3}},
		{event: evDealDamage, then: effect{kind: effMulPct, n: 200}},
		{event: evDealDamage, then: effect{kind: effAdd, n: -1}},
		{event: evTakeDamage, then: effect{kind: effAdd, n: 100}}, // wrong event: ignored
	}
	if got, want := applyRules(evDealDamage, 10, cards, ruleCtx{}), 24; got != want {
		t.Errorf("applyRules = %d, want %d", got, want)
	}
}

func TestApplyRulesTakeDamageFloorsAtOne(t *testing.T) {
	t.Parallel()

	cards := []ruleCard{{event: evTakeDamage, then: effect{kind: effAdd, n: -10}}}
	if got, want := applyRules(evTakeDamage, 2, cards, ruleCtx{}), 1; got != want {
		t.Errorf("applyRules take-damage = %d, want floor %d", got, want)
	}
}

func TestSpeciesCardsMatchOldPassives(t *testing.T) {
	t.Parallel()

	// Dwarf: flat reduction, floored at 1 — the old applyDR numbers.
	dwarf := speciesCards(protocol.SpeciesDwarf)
	if got, want := applyRules(evTakeDamage, 5, dwarf, ruleCtx{}), 5-protocol.DwarfDamageReduction; got != want {
		t.Errorf("dwarf take-damage(5) = %d, want %d", got, want)
	}

	if got, want := applyRules(evTakeDamage, 1, dwarf, ruleCtx{}), 1; got != want {
		t.Errorf("dwarf take-damage(1) = %d, want %d", got, want)
	}

	// Human: the old award*(100+bonus)/100.
	human := speciesCards(protocol.SpeciesHuman)
	if got, want := applyRules(evEarnXP, protocol.MonsterXP, human, ruleCtx{}),
		protocol.MonsterXP*(percentBase+protocol.HumanXPBonusPercent)/percentBase; got != want {
		t.Errorf("human earn-xp = %d, want %d", got, want)
	}

	// Elf: with a chance card, both branches must be reachable across seeds.
	elf := speciesCards(protocol.SpeciesElf)
	crit, plain := false, false

	for seed := range uint64(100) {
		got := applyRules(evDealDamage, 4, elf, ruleCtx{rng: testRNG(seed)})
		switch got {
		case 4:
			plain = true
		case 4 * protocol.ElfCritMultiplier:
			crit = true
		default:
			t.Fatalf("elf deal-damage = %d, want 4 or %d", got, 4*protocol.ElfCritMultiplier)
		}
	}

	if !crit || !plain {
		t.Errorf("elf chance card: crit seen %v, plain seen %v, want both", crit, plain)
	}
}

func TestApplyRulesConditions(t *testing.T) {
	t.Parallel()

	halfDead := &entity{hp: 4, maxHP: 10}
	fresh := &entity{hp: 10, maxHP: 10}
	card := func(c condition) []ruleCard {
		return []ruleCard{{event: evDealDamage, when: []condition{c}, then: effect{kind: effAdd, n: 3}}}
	}

	belowPct := card(condition{kind: condTargetHPBelowPct, n: 50})
	if got, want := applyRules(evDealDamage, 1, belowPct, ruleCtx{victim: halfDead}), 4; got != want {
		t.Errorf("targetHPBelowPct(50) vs 4/10 = %d, want %d", got, want)
	}

	if got, want := applyRules(evDealDamage, 1, belowPct, ruleCtx{victim: fresh}), 1; got != want {
		t.Errorf("targetHPBelowPct(50) vs 10/10 = %d, want %d", got, want)
	}

	full := card(condition{kind: condTargetHPFull})
	if got, want := applyRules(evDealDamage, 1, full, ruleCtx{victim: fresh}), 4; got != want {
		t.Errorf("targetHPFull vs 10/10 = %d, want %d", got, want)
	}

	adj := ruleCtx{attacker: &entity{hex: protocol.Hex{Q: 0, R: 0}}, victim: &entity{hex: protocol.Hex{Q: 1, R: 0}}}

	adjacent := card(condition{kind: condTargetAdjacent})
	if got, want := applyRules(evDealDamage, 1, adjacent, adj), 4; got != want {
		t.Errorf("targetAdjacent at distance 1 = %d, want %d", got, want)
	}

	ally := card(condition{kind: condAllyInBubble})
	if got, want := applyRules(evDealDamage, 1, ally, ruleCtx{allyInBubble: true}), 4; got != want {
		t.Errorf("allyInBubble = %d, want %d", got, want)
	}
}
