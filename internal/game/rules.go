package game

import (
	mrand "math/rand/v2"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// The modifier pipeline (spec: docs/superpowers/specs/2026-07-10-m6b.4-gear-pipeline-design.md).
// Combat exposes events; species and gear carry rule cards — pure serializable
// data, never closures — that transform a value at an event. Adding an effect
// means adding a card in content.go, not editing a combat site.

// percentBase is the denominator for all percent knobs: a "percent out of 100".
const percentBase = 100

// Events: the moments a card can hook. deal-damage runs attacker-side per
// (attacker, victim) pair; take-damage runs victim-side on the result;
// earn-XP runs on each player's kill award; aggro-range runs PLAYER-side (see
// aggroRadiusForLocked, world.go): it folds a player's own noticeability cards
// over protocol.MonsterAggroRadius, so a future sneaky/loud item can shrink or
// grow the distance at which a WORLD-domain monster picks that player up.
// ctx.attacker is the player being evaluated (mirroring rollDamageLocked's
// convention that ctx.attacker is whichever entity's own cards are running),
// so e.g. condAttackerSpecies gates on the player's species. Live content
// since #88: Padded Boots and Iron Plate Armor. Gear-only by design — no
// species card feeds this event.
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
	evAggroRange = "aggro-range"
)

// Condition kinds. chance consumes the turn rng (deterministic: cards are
// evaluated in stable order). The target* conditions read the victim; ally
// presence is precomputed into the ctx by the caller (it needs world state).
// See the evDealDamage const block above: keep in sync with
// items.go's validateRuleCards.
const (
	condChance           = "chance"           // n = percent
	condTargetHPBelowPct = "targetHPBelowPct" // n = percent of maxHP
	// condTargetHPBelowFlat compares ABSOLUTE hp (n = hit points),
	// deliberately not scaling with the target's maxHP: an execute/mop-up
	// rule stays a mop-up rule against big monsters, where a percent
	// threshold would quietly become a boss-killer. Designer decision from
	// the first gear batch (Staff of the War Mage).
	condTargetHPBelowFlat = "targetHPBelowFlat"
	condTargetHPFull      = "targetHPFull"
	condAllyInBubble      = "allyInBubble"
	condTargetAdjacent    = "targetAdjacent"
	// condAttackerSpecies (s = species) gates on who SWINGS the weapon —
	// gear a whole class can use but that sings in one species' hands
	// (Ancient Dwarven Mattock).
	condAttackerSpecies = "attackerSpecies"
	// condTargetKind (s = a monster-kind registry id, content.go's
	// monsterDefs) gates on the VICTIM being a monster of that specific
	// kind — a boss-specific spike (the Wyrmslayer Greatsword vs dragons).
	// Never holds for a player victim (kindOf returns nil).
	condTargetKind = "targetKind"
	// condDamageType (s = a protocol.DamageType* value) gates on the damage
	// type of the hit being folded, and works in BOTH directions off the
	// same ruleCtx field (#92, renamed from condIncomingType in #155):
	//   - in a take-damage fold it is the type LANDING on you — every
	//     resistance and vulnerability card ever written;
	//   - in a deal-damage fold it is the type of the weapon YOU are
	//     swinging (rollDamageLocked builds one ctx per hit from the firing
	//     weapon) — "+10% blunt damage", the ARPG idiom for weapon-flavoured
	//     passives (#124's Combat Training, #57's Crusher).
	// The old name said "incoming", which is accurate on defence and exactly
	// backwards on offence; the rename happened before offensive cards
	// became common, not after.
	// DECOUPLED, never a roll: it asks what type is landing, not whether
	// attacker beats defender.
	condDamageType = "damageType"
	// condWeaponTagged (s = a protocol.WeaponTag* value) gates on the weapon
	// being SWUNG carrying that tag — "+10% with melee weapons" (#124's
	// Combat Training, the maintainer's chosen scope over damage type). It
	// reads ctx.weapon, which only rollDamageLocked fills; every other fold
	// leaves it nil and the condition fails closed.
	//
	// Deliberately distinct from condDamageType: a tag is how a weapon is
	// USED (which attack fires it), a damage type is what it DEALS. A mace
	// and a fire staff are both "not melee-tagged vs melee-tagged" questions
	// that no damage type can answer.
	condWeaponTagged = "weaponTagged"
	// condShieldEquipped (no parameter) gates on the DEFENDER holding a
	// shield in its off-hand — #124's Shield Wall. Defender-side is not a
	// convention here but a requirement: in rollDamageLocked the victim's
	// own cards fold under a ctx whose .attacker is still the SWINGER, so a
	// take-damage card asking "do I have a shield" must read ctx.victim.
	// (condAttackerSpecies is the mirror case — a deal-damage card, so it
	// reads ctx.attacker.) An attacker-side "while holding a shield" card
	// would need its own kind; nothing wants one.
	//
	// Narrow by the maintainer's call (#124 O2) rather than a general
	// "equippedType" condition; #57's shield-skill backlog is its rider
	// queue, so it is not a one-off.
	condShieldEquipped = "shieldEquipped"
	// condDualWielding (no parameter) gates on the ATTACKER holding a weapon
	// in BOTH hands — #57's rogue and mage lines. Attacker-side, mirroring
	// condAttackerSpecies: it is a deal-damage condition, and in
	// rollDamageLocked a victim's own cards fold under a ctx whose .attacker
	// is still the swinger, so reading the wrong side is silent and wrong.
	//
	// A TWO-HANDED weapon is NOT dual-wielding, even though it occupies both
	// hand slots: it is one weapon. heldWeapons returns one entry for it, so
	// counting weapons rather than filled slots gets this right by
	// construction (TestDualWieldingDoesNotHoldForATwoHander).
	condDualWielding = "dualWielding"
)

// dualWieldHandCount is how many weapons "dual-wielding" means. Named rather
// than inline so the two-handed case reads correctly: it is a count of
// WEAPONS, not of occupied hand slots.
const dualWieldHandCount = 2

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
	// s is the string parameter for kinds that need one (condAttackerSpecies:
	// a protocol.Species* value). Empty for numeric/parameterless kinds.
	s string
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
	attacker *entity
	victim   *entity
	// damageType is the protocol.DamageType* of the attack being folded
	// (#92) — threaded from the firing weapon by rollDamageLocked and read
	// by condDamageType. Empty for folds with no attack in flight
	// (earn-XP, aggro-range), where a condDamageType card simply never
	// holds.
	damageType string
	// weapon is the itemDef being SWUNG in this fold (#124) — read by
	// condWeaponTagged. Only rollDamageLocked fills it; every other fold
	// leaves it nil, where a weapon-tag card simply never holds. Kept as the
	// def (not just its tags) so a future condition can read damage, range,
	// or two-handedness without another ctx field.
	weapon       *itemDef
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
	case condTargetHPBelowPct, condTargetHPBelowFlat, condTargetHPFull:
		return targetHPConditionHolds(c, ctx)
	case condAttackerSpecies:
		return ctx.attacker != nil && ctx.attacker.species == c.s
	case condAllyInBubble:
		return ctx.allyInBubble
	case condTargetAdjacent:
		return ctx.attacker != nil && ctx.victim != nil && HexDistance(ctx.attacker.hex, ctx.victim.hex) == 1
	case condTargetKind:
		return targetKindHolds(ctx, c.s)
	case condDamageType:
		return ctx.damageType == c.s
	case condWeaponTagged:
		return ctx.weapon != nil && ctx.weapon.hasTag(c.s)
	case condShieldEquipped, condDualWielding:
		return equipmentConditionHolds(c, ctx)
	default:
		return false // unknown condition never holds — content bugs fail closed
	}
}

// targetHPConditionHolds groups the three victim-HP conditions
// (condTargetHPBelowPct, condTargetHPBelowFlat, condTargetHPFull) — split
// out of conditionHolds' switch to keep its cyclomatic complexity under the
// linter's threshold.
func targetHPConditionHolds(c condition, ctx ruleCtx) bool {
	if ctx.victim == nil {
		return false
	}

	switch c.kind {
	case condTargetHPBelowPct:
		return ctx.victim.hp*percentBase < ctx.victim.maxHP*c.n
	case condTargetHPBelowFlat:
		return ctx.victim.hp < c.n
	case condTargetHPFull:
		return ctx.victim.hp >= ctx.victim.maxHP
	default:
		return false
	}
}

// shieldEquippedHolds is condShieldEquipped's body: the VICTIM — the entity
// taking the hit, whose take-damage cards are folding — holds a shield in its
// off-hand. See the const's doc comment for why this is the victim and not
// "whoever's cards are running".
func shieldEquippedHolds(ctx ruleCtx) bool {
	if ctx.victim == nil {
		return false
	}

	def := ctx.victim.equippedDefIn(protocol.SlotOffHand)

	return def != nil && def.itemType == protocol.ItemTypeShield
}

// equipmentConditionHolds groups the two hand-slot conditions
// (condShieldEquipped, condDualWielding) — split out of conditionHolds' switch
// to keep its cyclomatic complexity under the linter's threshold, exactly as
// targetHPConditionHolds groups the three victim-HP ones.
//
// They read OPPOSITE sides on purpose: a shield is defender-side (a
// take-damage card asking "do I have a shield"), dual-wielding is
// attacker-side (a deal-damage card asking "am I holding two"). The grouping
// is about the switch's size, never about them being symmetric.
func equipmentConditionHolds(c condition, ctx ruleCtx) bool {
	if c.kind == condShieldEquipped {
		return shieldEquippedHolds(ctx)
	}

	return dualWieldingHolds(ctx)
}

// dualWieldingHolds is condDualWielding's body: the ATTACKER holds two
// weapons. Counts weapons via heldWeapons rather than checking that both hand
// slots are occupied, which is what makes a two-handed weapon (one weapon,
// both slots) correctly fail.
func dualWieldingHolds(ctx ruleCtx) bool {
	if ctx.attacker == nil {
		return false
	}

	return len(ctx.attacker.heldWeapons()) >= dualWieldHandCount
}

// targetKindHolds is condTargetKind's condition body, split out of
// conditionHolds' switch to keep its cyclomatic complexity under the
// linter's threshold. Holds iff ctx.victim is a monster whose kind id is s.
func targetKindHolds(ctx ruleCtx, s string) bool {
	if ctx.victim == nil {
		return false
	}

	k := kindOf(ctx.victim)

	return k != nil && k.id == s
}

// ruleTrace reports which chance-conditioned multiplier cards fired during
// one applyRules fold — the crit/glance combat moments (#114). boostFired is
// a chance-conditioned mulPct > 100 firing (a crit when the fold is
// deal-damage: elf passive, Misericorde, Duelist's Saber); reduceFired a
// chance-conditioned mulPct < 100 firing (a glance when the fold is
// take-damage: the Rogue passive). The trace records only the raw fold fact;
// mapping to crit/glance semantics — which fold the flag came from — is
// rollDamageLocked's job (world.go). Deterministic effects (an unconditional
// mulPct, a targetKind gate) never set a flag: a moment is a chance roll
// landing, not a rule doing its steady job. Purely observational: tracing
// changes no arithmetic and consumes no rng.
type ruleTrace struct {
	boostFired  bool
	reduceFired bool
}

// noteMul records a fired mulPct card into the trace: only chance-conditioned
// multipliers count (see ruleTrace's doc — deterministic rules are not
// moments).
func (t *ruleTrace) noteMul(c ruleCard) {
	if !hasChanceCondition(c.when) {
		return
	}

	if c.then.n > percentBase {
		t.boostFired = true
	}

	if c.then.n < percentBase {
		t.reduceFired = true
	}
}

// applyRules folds base through every card matching event whose conditions
// hold: adds sum first, then multiplier deltas sum and apply once (percent
// fold is additive, not compounding — #61 principle 14), then the
// event-level clamp (a landed hit always costs ≥1; a noticeability radius
// stays ≥1; XP never goes negative).
func applyRules(event string, base int, cards []ruleCard, ctx ruleCtx) int {
	v, _ := applyRulesTraced(event, base, cards, ctx)

	return v
}

// applyRulesTraced is applyRules plus a ruleTrace of the chance-conditioned
// multiplier cards that fired (see ruleTrace). Card evaluation order — and
// therefore rng consumption — is IDENTICAL to the untraced fold: determinism
// is load-bearing, and this function must never move a seeded test.
func applyRulesTraced(event string, base int, cards []ruleCard, ctx ruleCtx) (int, ruleTrace) {
	add := 0

	var (
		muls  []int
		trace ruleTrace
	)

	for _, c := range cards {
		if c.event != event || !holds(c.when, ctx) {
			continue
		}

		switch c.then.kind {
		case effAdd:
			add += c.then.n
		case effMulPct:
			muls = append(muls, c.then.n)
			trace.noteMul(c)
		}
	}

	v := base + add

	// Percentages ADD within one event's fold (#61 principle 14, roadmap
	// Q8): sum the deltas and apply once — a single integer truncation,
	// trivially order-independent. Stages still compose across events
	// (deal-damage -> take-damage -> future crit-check), so cross-stage
	// effects remain true multipliers.
	if len(muls) > 0 {
		delta := 0
		for _, m := range muls {
			delta += m - percentBase
		}

		v = max(v*(percentBase+delta)/percentBase, 0)
	}

	switch event {
	case evTakeDamage:
		if v < 1 {
			v = 1
		}
	case evAggroRange:
		// A noticeability radius stays ≥1 (#88): shipped cards are
		// multiplicative and cannot go negative, but a future negative-`add`
		// card could fold it to 0 — a monster that never notices anyone.
		if v < 1 {
			v = 1
		}
	case evEarnXP:
		if v < 0 {
			v = 0
		}
	}

	return v, trace
}

// hasChanceCondition reports whether when carries a condChance condition —
// the marker that a card is a per-hit gamble (crit%/glance%) rather than a
// deterministic rule.
func hasChanceCondition(when []condition) bool {
	for _, c := range when {
		if c.kind == condChance {
			return true
		}
	}

	return false
}
