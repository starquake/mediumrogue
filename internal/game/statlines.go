package game

import (
	"strconv"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// doublePercent is 200% — a clean doubling, which reads better as "×2" than
// as "+100%", and is also the mirror point when inverting a take-damage
// multiplier into a resistance (×0.5 taken == +50% resisted).
const doublePercent = 2 * percentBase

// statlines.go: rendering a def's numbers as ARPG stat lines (#171) — "−50%
// Chaos Damage", "+10% Melee Damage", "Damage 4" — instead of the authored
// prose that used to restate each rule card by hand.
//
// Derived, never authored: a hand-written line that repeats its own card is a
// drift surface, and this file exists so the tooltip and the card can never
// disagree. Authored text is now flavor ONLY, and carries no numbers.
//
// VOCABULARY (@starquake, #171, revised 2026-07-19): defensive stats read as
// RESISTANCE, offensive ones as DAMAGE. "+50% Chaos Resistance", not "−50%
// Chaos Damage".
//
// Resistance carries its own direction, so the reader never has to infer
// anything from which slot the item occupies — and it removes a
// double-negative: "+50% Chaos Resistance" is plainly a benefit, where "−50%
// Chaos Damage" only reads as one after working out that less damage taken is
// good. A future cursed item falls out for free as "−50% Fire Resistance".
//
// Sign still does not mean good — "+25% Aggro Range" is a drawback — so the
// drawback flag below stays. Utility stats (XP, Aggro Range) name their own
// subject and their sign is literal.

// statLine is one rendered stat. Drawback marks a stat that makes the wearer
// WORSE — Iron Plate Armor's +25% Aggro Range is the shipped example — so the
// client can style it apart from a benefit (#171 Q6). Sign alone cannot carry
// that: +25% Aggro Range is bad while +5% XP is good.
type statLine struct {
	text     string
	drawback bool
}

// baseStatLines renders the numbers that are NOT rule cards: a weapon's
// damage/reach and a consumable's heal. They are the pipeline's INPUT rather
// than modifiers within it (see #175), so they have no card to derive from —
// but a tooltip that omitted them would be worse than the prose it replaced.
func baseStatLines(def *itemDef) []statLine {
	var out []statLine

	if def.damage != 0 {
		out = append(out, statLine{text: "Damage " + strconv.Itoa(def.damage)})
	}

	if def.rangeHex != 0 {
		out = append(out, statLine{text: "Range " + strconv.Itoa(def.rangeHex)})
	}

	if def.aoeRadius != 0 {
		out = append(out, statLine{text: "AoE " + strconv.Itoa(def.aoeRadius)})
	}

	if def.heal != 0 {
		out = append(out, statLine{text: "+" + strconv.Itoa(def.heal) + " HP"})
		out = append(out, statLine{text: "Stacks to " + strconv.Itoa(protocol.ItemStackCap)})
	}

	return out
}

// statLinesFor renders every stat a def contributes: its base numbers first,
// then one line per rule card, then a consumable's timed-effect payload — all
// in registry order.
func statLinesFor(def *itemDef) []statLine {
	out := baseStatLines(def)

	for _, c := range def.rules {
		out = append(out, cardStatLine(c))
	}

	out = append(out, consumableEffectStatLines(def)...)

	return out
}

func consumableEffectStatLines(def *itemDef) []statLine {
	var out []statLine

	for _, ae := range def.appliesEffect {
		line := cardStatLine(timedEffect{defID: ae.effectID, magnitude: ae.magnitude}.card())
		line.text += " " + turnsText(ae.turns)
		out = append(out, line)
	}

	if def.cleansesHarmful {
		out = append(out, statLine{text: "Cures harmful effects"})
	}

	return out
}

// turnsText renders a timed effect's duration: "for 1 turn" / "for N turns".
func turnsText(turns int) string {
	if turns == 1 {
		return "for 1 turn"
	}

	return "for " + strconv.Itoa(turns) + " turns"
}

// cardStatLine renders one rule card: [chance prefix] amount subject [suffix].
func cardStatLine(c ruleCard) statLine {
	// Lifesteal is a rider, not a value transform (#271): it names its own
	// subject and its sign is always a benefit, so it renders on its own path
	// rather than through amountText/subjectText (which would read its percent
	// as a damage multiplier and mislabel it "−75% Damage").
	if c.then.kind == effLifesteal {
		return lifestealStatLine(c)
	}

	text := amountText(c.then, c.event) + " " + subjectText(c)

	if prefix := chancePrefix(c.when); prefix != "" {
		text = prefix + " " + text
	}

	if suffix := suffixText(c.when); suffix != "" {
		text += " " + suffix
	}

	return statLine{text: text, drawback: isDrawback(c)}
}

// lifestealStatLine renders an effLifesteal card as "+N% Lifesteal" (the ARPG
// leech affix, always a benefit), keeping any chance prefix a gated lifesteal
// card would carry. Never a drawback: a positive leech only helps the wielder.
func lifestealStatLine(c ruleCard) statLine {
	text := "+" + strconv.Itoa(c.then.n) + "% Lifesteal"

	if prefix := chancePrefix(c.when); prefix != "" {
		text = prefix + " " + text
	}

	return statLine{text: text, drawback: false}
}

// amountText renders the effect's magnitude: "+3", "−1", "×2", "+10%", "−50%".
// A mulPct is shown as a DELTA from 100 (+50% rather than ×1.5) because
// percent effects add within a fold — deltas are what actually stack, so the
// number a player sees is the number that combines.
//
// take-damage INVERTS: the stat is named Resistance, so a card multiplying
// incoming damage by 0.5 reads "+50% Resistance", and a flat −1 to damage
// taken reads "+1 Resistance".
func amountText(e effect, event string) string {
	n := e.n
	if event == evTakeDamage {
		if e.kind == effAdd {
			n = -n
		} else {
			n = doublePercent - n
		}
	}

	if e.kind == effAdd {
		if n < 0 {
			return "−" + strconv.Itoa(-n)
		}

		return "+" + strconv.Itoa(n)
	}

	// A clean doubling reads better as ×2 than as +100%.
	if n == doublePercent && event != evTakeDamage {
		return "×2"
	}

	delta := n - percentBase
	if delta < 0 {
		return "−" + strconv.Itoa(-delta) + "%"
	}

	return "+" + strconv.Itoa(delta) + "%"
}

// subjectText names WHAT the card changes: the event's noun, narrowed by any
// condition that qualifies the noun itself (a damage type, a weapon tag).
//
// A take-damage card is named RESISTANCE rather than damage, which is why
// amountText inverts its sign: a card that multiplies incoming damage by 0.5
// is +50% resistance, not −50% damage.
func subjectText(c ruleCard) string {
	noun := "Damage"

	switch c.event {
	case evTakeDamage:
		// "Damage Resistance" rather than a bare "Resistance": untyped
		// mitigation resists everything, and the noun should say what.
		noun = "Damage Resistance"
	case evEarnXP:
		noun = "XP"
	case evAggroRange:
		noun = "Aggro Range"
	case evEndOfTurn, evRegen:
		// Both are an HP delta per turn, not damage dealt — a timed effect's
		// tick (#271) and passive recovery (#397). A player does not care which
		// fold produced it, so they share a noun.
		//
		// Falling through to "Damage" is the hazard, and it has bitten twice:
		// it shipped for end-of-turn in #271, where a fire flask's DoT rider
		// read as "−3 Damage" (a damage REDUCTION) for something that drains
		// HP; and evRegen would have rendered the Mender's Locket's +1 as
		// "+1 Damage", naming a stat the item does not touch.
		noun = "HP per turn"
	default:
		// Events without a noun contribute no stat line.
	}

	for _, cond := range c.when {
		switch cond.kind {
		case condDamageType:
			// A type REPLACES the generic noun rather than stacking with it:
			// "Chaos Resistance", not "Chaos Damage Resistance".
			if c.event == evTakeDamage {
				noun = titleWord(cond.s) + " Resistance"
			} else {
				noun = titleWord(cond.s) + " " + noun
			}
		case condWeaponTagged:
			noun = titleWord(cond.s) + " " + noun
		default:
			// Conditions that do not qualify the noun leave it as-is.
		}
	}

	return noun
}

// chancePrefix renders a chance gate — "15% chance" — which reads better in
// front of the amount than trailing behind it.
func chancePrefix(when []condition) string {
	for _, cond := range when {
		if cond.kind == condChance {
			return strconv.Itoa(cond.n) + "% chance"
		}
	}

	return ""
}

// suffixText renders the conditions that qualify WHEN a card applies rather
// than what it applies to. Unknown kinds render nothing rather than guessing:
// a missing qualifier is a smaller lie than a wrong one.
func suffixText(when []condition) string {
	for _, cond := range when {
		switch cond.kind {
		case condTargetHPFull:
			return "vs Full HP"
		case condTargetHPBelowPct:
			return "vs Below " + strconv.Itoa(cond.n) + "% HP"
		case condTargetHPBelowFlat:
			return "vs Below " + strconv.Itoa(cond.n) + " HP"
		case condTargetAdjacent:
			return "vs Adjacent"
		case condAllyInBubble:
			return "with an Ally"
		case condAttackerSpecies:
			return "(" + titleWord(cond.s) + ")"
		case condTargetKind:
			return "vs " + kindDisplayName(cond.s)
		case condShieldEquipped:
			return "with a Shield"
		}
	}

	return ""
}

// isDrawback reports whether a card makes its holder worse off. Written as an
// explicit per-event table rather than a sign-flipping expression: "is this
// good?" depends on the event as well as the direction, and the clever
// version is unreadable six months later.
//
//	take-damage  more is worse  (you take more)
//	deal-damage  less is worse  (you deal less)
//	earn-xp      less is worse
//	aggro-range  more is worse  (noticed sooner)
//	regen        less is worse  (slower recovery)
func isDrawback(c ruleCard) bool {
	worse := increases(c.then)

	switch c.event {
	case evTakeDamage, evAggroRange:
		return worse
	case evDealDamage, evEarnXP, evRegen:
		return !worse && changes(c.then)
	}

	return false
}

// increases reports whether an effect raises the value it folds onto.
func increases(e effect) bool {
	if e.kind == effAdd {
		return e.n > 0
	}

	return e.n > percentBase
}

// changes reports whether an effect moves the value at all — a no-op card is
// neither a benefit nor a drawback.
func changes(e effect) bool {
	if e.kind == effAdd {
		return e.n != 0
	}

	return e.n != percentBase
}

// titleWord upper-cases the first letter of a registry token ("chaos" ->
// "Chaos", "melee" -> "Melee") for display.
func titleWord(s string) string {
	if s == "" {
		return s
	}

	b := []byte(s)
	if b[0] >= 'a' && b[0] <= 'z' {
		b[0] -= 'a' - 'A'
	}

	return string(b)
}

// kindDisplayName renders a monster kind id for a stat line, plural because
// the line reads as a class of enemy ("vs Dragons"). Falls back to the raw id
// if the kind is not registered — validateRuleCondition already rejects that
// at load, so this is belt-and-braces for a card built in a test.
func kindDisplayName(id string) string {
	if def, ok := monsterDefByID[id]; ok {
		return def.name + "s"
	}

	return titleWord(id) + "s"
}

// statViewsFor renders a def's stat lines for the wire. Always non-nil: the
// generated TS type is a non-optional StatView[], and a nil slice marshals to
// null — the exact shape that froze the client in #167.
// statViewsForCards renders a bare list of rule cards, for callers that have
// cards and nothing else — a skill, a monster kind. They used to fabricate an
// `&itemDef{rules: ...}` to reach statViewsFor, which meant knowing that every
// other field of itemDef would render to nothing (#353).
func statViewsForCards(cards []ruleCard) []protocol.StatView {
	out := make([]protocol.StatView, 0, len(cards))

	for _, c := range cards {
		l := cardStatLine(c)
		out = append(out, protocol.StatView{Text: l.text, Drawback: l.drawback})
	}

	return out
}

func statViewsFor(def *itemDef) []protocol.StatView {
	lines := statLinesFor(def)
	out := make([]protocol.StatView, 0, len(lines))

	for _, l := range lines {
		out = append(out, protocol.StatView{Text: l.text, Drawback: l.drawback})
	}

	return out
}

// activeStatLines renders an ACTIVE skill's descriptor as stat lines (#300).
//
// The same problem buff potions had (consumableEffectStatLines, above): an
// active carries no rule cards — its behaviour is its trigger — so without this
// its panel entry shows a flavor line and nothing else. A player reading "Here,
// then not." learns neither that Evade moves them three hexes nor that it costs
// a cooldown.
//
// Derived, never authored, for the reason the whole file exists: an authored
// line restating the descriptor is a drift surface, and validateFlavorHasNoStats
// already forbids putting numbers in the flavor. Every line below reads its
// numbers from the same activeDef that resolution reads.
func activeStatLines(a *activeDef) []statLine {
	if a == nil {
		return nil
	}

	var out []statLine

	switch a.kind {
	case activeReposition:
		out = append(out, statLine{text: "Teleport Range " + strconv.Itoa(a.rangeHex)})
	case activeSelfEffect:
		out = append(out, activeEffectLine(a.effect, "on yourself"))
	case activeTargetEffect:
		out = append(out, statLine{text: "Range " + strconv.Itoa(a.rangeHex)})
		out = append(out, activeEffectLine(a.effect, "on the target"))
	case activeAreaDamage:
		if a.damage != 0 {
			out = append(out, statLine{text: "Blast Damage " + strconv.Itoa(a.damage) + " " + titleWord(a.damageType)})
		}

		out = append(out, statLine{text: "Range " + strconv.Itoa(a.rangeHex)})

		if a.aoeRadius != 0 {
			out = append(out, statLine{text: "Blast " + strconv.Itoa(a.aoeRadius)})
		}

		if a.effect != nil {
			out = append(out, activeEffectLine(a.effect, "on each victim"))
		}
	default:
		// An active whose kind describes no stat line contributes none.
	}

	return append(out, statLine{text: "Cooldown " + strconv.Itoa(a.cooldownTurns) + " " + turnPlural(a.cooldownTurns)})
}

// activeEffectLine renders one applied timed effect through the SAME card
// renderer a gear card uses, suffixed with its duration and who receives it.
//
// The drawback flag is deliberately cleared. It answers "does this make its
// HOLDER worse", which is the right question for a potion you drink and the
// wrong one for a debuff you throw: Expose's Vulnerable renders as a drawback
// because it worsens whoever holds it, and whoever holds it is the enemy. The
// "on the target" suffix carries that instead.
func activeEffectLine(ae *appliedEffect, who string) statLine {
	if ae == nil {
		return statLine{text: who}
	}

	line := cardStatLine(timedEffect{defID: ae.effectID, magnitude: ae.magnitude}.card())
	line.text += " " + turnsText(ae.turns) + ", " + who
	line.drawback = false

	return line
}

// turnPlural is turnsText's bare counterpart, for a count that is not a
// duration ("Cooldown 3 turns" rather than "for 3 turns").
func turnPlural(n int) string {
	if n == 1 {
		return "turn"
	}

	return "turns"
}

// activeStatViews is statViewsFor's counterpart for an active skill.
func activeStatViews(a *activeDef) []protocol.StatView {
	lines := activeStatLines(a)
	out := make([]protocol.StatView, 0, len(lines))

	for _, l := range lines {
		out = append(out, protocol.StatView{Text: l.text, Drawback: l.drawback})
	}

	return out
}
