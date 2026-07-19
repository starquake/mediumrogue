package game

import (
	"strconv"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// statlines.go: rendering a def's numbers as ARPG stat lines (#171) — "−50%
// Chaos Damage", "+10% Melee Damage", "Damage 4" — instead of the authored
// prose that used to restate each rule card by hand.
//
// Derived, never authored: a hand-written line that repeats its own card is a
// drift surface, and this file exists so the tooltip and the card can never
// disagree. Authored text is now flavor ONLY, and carries no numbers.
//
// SIGN CONVENTION (@starquake, #171): a damage stat is read against the
// ITEM'S NATURE — a weapon's number is damage dealt, a wearable's is damage
// taken — so neither needs a "Taken" suffix. Utility stats (XP, Aggro Range)
// are exempt: they name their own subject and their sign is literal.

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
// then one line per rule card, in registry order.
func statLinesFor(def *itemDef) []statLine {
	out := baseStatLines(def)

	for _, c := range def.rules {
		out = append(out, cardStatLine(c))
	}

	return out
}

// cardStatLine renders one rule card: [chance prefix] amount subject [suffix].
func cardStatLine(c ruleCard) statLine {
	text := amountText(c.then) + " " + subjectText(c)

	if prefix := chancePrefix(c.when); prefix != "" {
		text = prefix + " " + text
	}

	if suffix := suffixText(c.when); suffix != "" {
		text += " " + suffix
	}

	return statLine{text: text, drawback: isDrawback(c)}
}

// amountText renders the effect's magnitude: "+3", "−1", "×2", "+10%", "−50%".
// A mulPct is shown as a DELTA from 100 (−50% rather than ×0.5) because
// percent effects add within a fold — deltas are what actually stack, so the
// number a player sees is the number that combines.
func amountText(e effect) string {
	if e.kind == effAdd {
		if e.n < 0 {
			return "−" + strconv.Itoa(-e.n)
		}

		return "+" + strconv.Itoa(e.n)
	}

	// A clean doubling reads better as ×2 than as +100%.
	if e.n == 2*percentBase {
		return "×2"
	}

	delta := e.n - percentBase
	if delta < 0 {
		return "−" + strconv.Itoa(-delta) + "%"
	}

	return "+" + strconv.Itoa(delta) + "%"
}

// subjectText names WHAT the card changes: the event's noun, narrowed by any
// condition that qualifies the noun itself (a damage type, a weapon tag).
func subjectText(c ruleCard) string {
	noun := "Damage"

	switch c.event {
	case evEarnXP:
		noun = "XP"
	case evAggroRange:
		noun = "Aggro Range"
	}

	for _, cond := range c.when {
		switch cond.kind {
		case condDamageType:
			noun = titleWord(cond.s) + " " + noun
		case condWeaponTagged:
			noun = titleWord(cond.s) + " " + noun
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
func isDrawback(c ruleCard) bool {
	worse := increases(c.then)

	switch c.event {
	case evTakeDamage, evAggroRange:
		return worse
	case evDealDamage, evEarnXP:
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
