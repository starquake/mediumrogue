package game

import mrand "math/rand/v2"

// The modifier pipeline (spec: docs/superpowers/specs/2026-07-10-m6b.4-gear-pipeline-design.md).
// Combat exposes events; species and gear carry rule cards — pure serializable
// data, never closures — that transform a value at an event. Adding an effect
// means adding a card in content.go, not editing a combat site.

// percentBase is the denominator for all percent knobs: a "percent out of 100".
const percentBase = 100

// Events: the moments a card can hook. deal-damage runs attacker-side per
// (attacker, victim) pair; take-damage runs victim-side on the result;
// earn-XP runs on each player's kill award.
//
// Adding a new event/condition/effect kind here also means adding it to
// items.go's validateRuleCards switches (event/condition/effect) and, for a
// condition, conditionHolds' switch below — three places that must agree so
// mustValidateContent's load-time check and this file's runtime evaluation
// never silently diverge (a kind valid to one but not the other either panics
// spuriously at load, or validates cleanly and then no-ops forever at
// runtime — conditionHolds fails closed via its default case, but
// applyRules' effect switch has no default at all).
const (
	evDealDamage = "deal-damage"
	evTakeDamage = "take-damage"
	evEarnXP     = "earn-xp"
)

// Condition kinds. chance consumes the turn rng (deterministic: cards are
// evaluated in stable order). The target* conditions read the victim; ally
// presence is precomputed into the ctx by the caller (it needs world state).
// See the evDealDamage const block above: keep in sync with
// items.go's validateRuleCards.
const (
	condChance           = "chance"           // n = percent
	condTargetHPBelowPct = "targetHPBelowPct" // n = percent of maxHP
	condTargetHPFull     = "targetHPFull"
	condAllyInBubble     = "allyInBubble"
	condTargetAdjacent   = "targetAdjacent"
)

// Effect kinds. All adds apply before all multipliers (fold phases), so card
// order can never change arithmetic within a phase. See the evDealDamage
// const block above: keep in sync with items.go's validateRuleCards.
const (
	effAdd    = "add"    // n may be negative
	effMulPct = "mulPct" // n = percent (200 = double)
)

type condition struct {
	kind string
	n    int
}

type effect struct {
	kind string
	n    int
}

// ruleCard is one when/if/then rule: pure data (the §7 SQLite prerequisite).
type ruleCard struct {
	event string
	when  []condition // all must hold; empty = always
	then  effect
}

// ruleCtx carries the facts conditions read. victim is the entity being hit
// (deal-damage and take-damage alike); attacker the one hitting. rng is only
// consumed by chance conditions. allyInBubble is precomputed by the caller.
type ruleCtx struct {
	attacker     *entity
	victim       *entity
	allyInBubble bool
	rng          *mrand.Rand
}

// holds reports whether every condition in when holds under ctx.
func holds(when []condition, ctx ruleCtx) bool {
	for _, c := range when {
		if !conditionHolds(c, ctx) {
			return false
		}
	}

	return true
}

// conditionHolds reports whether a single condition holds under ctx.
func conditionHolds(c condition, ctx ruleCtx) bool {
	switch c.kind {
	case condChance:
		return ctx.rng.IntN(percentBase) < c.n
	case condTargetHPBelowPct:
		return ctx.victim != nil && ctx.victim.hp*percentBase < ctx.victim.maxHP*c.n
	case condTargetHPFull:
		return ctx.victim != nil && ctx.victim.hp >= ctx.victim.maxHP
	case condAllyInBubble:
		return ctx.allyInBubble
	case condTargetAdjacent:
		return ctx.attacker != nil && ctx.victim != nil && HexDistance(ctx.attacker.hex, ctx.victim.hex) == 1
	default:
		return false // unknown condition never holds — content bugs fail closed
	}
}

// applyRules folds base through every card matching event whose conditions
// hold: adds sum first, then multipliers apply in card order, then the
// event-level clamp (a landed hit always costs ≥1; XP never goes negative).
func applyRules(event string, base int, cards []ruleCard, ctx ruleCtx) int {
	add := 0

	var muls []int

	for _, c := range cards {
		if c.event != event || !holds(c.when, ctx) {
			continue
		}

		switch c.then.kind {
		case effAdd:
			add += c.then.n
		case effMulPct:
			muls = append(muls, c.then.n)
		}
	}

	v := base + add
	for _, m := range muls {
		v = v * m / percentBase
	}

	switch event {
	case evTakeDamage:
		if v < 1 {
			v = 1
		}
	case evEarnXP:
		if v < 0 {
			v = 0
		}
	}

	return v
}
