package game

import (
	"sort"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// guide.go (#156) exposes the designer content guide's DERIVABLE half: the
// card vocabulary, the calibration numbers, and every registered item and
// monster kind with its stat lines. cmd/contentguide renders it into the
// guide's HTML; the guide's PROSE (the coupling tell, the drift cases, the
// checklist) is hand-written in that template, because it is argument rather
// than data and regenerating it would lose the reasoning.
//
// The point of generating it is that a guide which cites live numbers goes
// stale silently, and a designer trusts it anyway — the 2026-07-18 PDF was
// wrong twice within a day (the damageType rename, then #154's percentage
// mitigation). A regenerated guide cannot drift from the registries.
//
// **Stat lines come from statlines.go, never re-derived here.** A second
// renderer would be free to disagree with the tooltips the player actually
// sees, which is the same class of bug this file exists to prevent, one level
// up: the guide would be internally consistent and still describe a game
// nobody is playing.

// GuideVocabEntry is one card-vocabulary term: an event, condition or effect
// kind, its parameter shape, and what it means. Kind and Param are derived;
// Description is authored (see guideDescriptions).
type GuideVocabEntry struct {
	Kind        string
	Param       string
	Description string
}

// GuideStat is one rendered stat line, exactly as a player sees it in a
// tooltip — statlines.go's output, not a parallel rendering.
type GuideStat struct {
	Text     string
	Drawback bool
}

// GuideItem is one registered item as the guide shows it.
type GuideItem struct {
	ID, Name   string
	Type       string
	DamageType string
	Tags       []string
	TwoHanded  bool
	Damage     int
	RangeHex   int
	AoERadius  int
	Heal       int
	Stats      []GuideStat
	Flavor     string
}

// GuideMonster is one registered monster kind as the guide shows it. Stats are
// the kind's own cards rendered through the same statlines path as an item's —
// a troll's fire vulnerability reads the way a resistance on armour reads.
type GuideMonster struct {
	ID, Name string
	MaxHP    int
	// Weapon is the kind's natural weapon (#179): its name, damage, reach and
	// type all come from a real registry item now, so the guide shows what the
	// kind actually swings rather than a copy of its numbers.
	Weapon      string
	Damage      int
	RangeHex    int
	DamageType  string
	XP          int
	AggroRadius int
	DropChance  int
	Stats       []GuideStat
}

// GuideNumber is one calibration anchor — a live constant a designer needs in
// order to pitch a new number against the existing ones.
type GuideNumber struct {
	Name        string
	Value       int
	Description string
}

// Guide is everything cmd/contentguide injects into the guide template.
type Guide struct {
	Events      []GuideVocabEntry
	Conditions  []GuideVocabEntry
	Effects     []GuideVocabEntry
	DamageTypes []string
	WeaponTags  []string
	Items       []GuideItem
	Monsters    []GuideMonster
	Numbers     []GuideNumber
}

// guideDescriptions authors one line per vocabulary term. It is the FOURTH
// place that must agree about the card vocabulary — the const block and
// conditionHolds in rules.go and validateRuleCondition in items.go are the
// other three (their cross-reference comments name this one).
//
// Two checks keep it honest rather than trusting the author: every key here
// must be accepted by the real validator (validateGuideVocabulary, run at
// package init — a renamed or deleted kind panics at process start), and
// every kind any registered card actually uses must appear here
// (TestGuideDocumentsEveryVocabularyKindInUse) — so a new condition reaches
// the guide the first time content uses it, rather than being silently
// missing from the document designers write cards against.
//
//nolint:gochecknoglobals // authored vocabulary table, effectively const; validated at init.
var guideDescriptions = map[string]GuideVocabEntry{
	// Events — the moments a value folds through the pipeline.
	evDealDamage: {
		Param:       "—",
		Description: "A hit's damage, attacker side. Cards here belong on weapons.",
	},
	evTakeDamage: {
		Param:       "—",
		Description: "A hit's damage, victim side — resistances and vulnerabilities. Cards here belong on worn kit.",
	},
	evEarnXP: {
		Param:       "—",
		Description: "An XP award, before it lands. Folds without rng, so chance conditions are rejected.",
	},
	evAggroRange: {
		Param:       "—",
		Description: "How far a monster notices you from. Clamped to ≥1, so noticeability can never reach zero.",
	},
	evEndOfTurn: {
		Param: "—",
		Description: "A per-turn HP delta from an entity's active timed effects (a DoT drains, a regen restores). " +
			"Base 0, no rng, applied once each turn — heals clamp to max HP, drains can be lethal.",
	},

	// Conditions — when a card fires. Decoupled by design: a condition asks
	// about ONE side's state, never attacker-versus-defender.
	condChance: {
		Param: "n = percent",
		Description: "Fires n% of the time. This is where crit and glance live — a percentage, never a roll against " +
			"the other side.",
	},
	condTargetHPBelowPct: {
		Param:       "n = percent of maxHP",
		Description: "The victim is below n% of its own max HP. Scales with the target.",
	},
	condTargetHPBelowFlat: {
		Param: "n = hit points",
		Description: "The victim is below n absolute HP. Deliberately does NOT scale: a mop-up rule stays a mop-up " +
			"rule against a boss.",
	},
	condTargetHPFull: {
		Param:       "—",
		Description: "The victim is at full HP — opener flavour.",
	},
	condAllyInBubble: {
		Param:       "—",
		Description: "Another friendly is in this combat bubble.",
	},
	condTargetAdjacent: {
		Param:       "—",
		Description: "The victim is in an adjacent hex.",
	},
	condAttackerSpecies: {
		Param:       "s = species",
		Description: "Who SWINGS is of that species — gear a class can use but that sings in one species' hands.",
	},
	condTargetKind: {
		Param:       "s = monster kind",
		Description: "The victim is a monster of that registered kind. Never holds against a player.",
	},
	condDamageType: {
		Param: "s = damage type",
		Description: "The type of the hit being folded: what is LANDING on you in a take-damage fold, what you are " +
			"SWINGING in a deal-damage one.",
	},
	condWeaponTagged: {
		Param: "s = weapon tag",
		Description: "The weapon being swung carries that tag. A tag is how a weapon is USED; a damage type is what " +
			"it DEALS.",
	},
	condDualWielding: {
		Param: "—",
		Description: "The ATTACKER holds a weapon in both hands. A two-handed weapon is NOT " +
			"dual-wielding — it fills both slots but is one weapon.",
	},
	condShieldEquipped: {
		Param:       "—",
		Description: "The DEFENDER holds a shield in its off-hand. Defender-side is a requirement, not a convention.",
	},

	// Effects — what a card does to the value.
	effAdd: {
		Param:       "n (may be negative)",
		Description: "Adds n. Every add in a fold sums first, before any percentage applies.",
	},
	effMulPct: {
		Param:       "n = percent (200 = double)",
		Description: "Scales by n%. Percentages sum within one fold and apply once — never compounding pairwise.",
	},
}

// vocabFor builds one vocabulary entry, panicking if the kind is undocumented.
// Callers pass kinds straight from the const blocks, so a miss is a build-time
// content bug, never a runtime surprise.
func vocabFor(kind string) GuideVocabEntry {
	e, ok := guideDescriptions[kind]
	if !ok {
		panic("game: guide has no description for vocabulary kind " + kind)
	}

	e.Kind = kind

	return e
}

// guideEvents, guideConditions and guideEffects are the display order of the
// vocabulary tables — authored so the guide reads in a sensible order rather
// than alphabetically. Every entry is looked up through vocabFor, so a term
// listed here but undocumented panics at init.
//
//nolint:gochecknoglobals // authored display order, effectively const.
var (
	guideEvents     = []string{evDealDamage, evTakeDamage, evEarnXP, evAggroRange, evEndOfTurn}
	guideConditions = []string{
		condChance, condDamageType, condWeaponTagged, condTargetKind, condAttackerSpecies,
		condShieldEquipped, condDualWielding, condTargetAdjacent, condAllyInBubble,
		condTargetHPFull, condTargetHPBelowPct, condTargetHPBelowFlat,
	}
	guideEffects = []string{effAdd, effMulPct}
)

// validateGuideVocabulary panics if the guide documents a term the real
// validator no longer accepts — the check that catches a rename or a removal
// at process start instead of in a designer's PDF. Called by
// mustValidateContent (content.go).
func validateGuideVocabulary() {
	for _, kind := range guideEvents {
		vocabFor(kind)
	}

	for _, kind := range guideConditions {
		vocabFor(kind)

		// The real validator is the authority on what a condition may be;
		// exercising it here means a kind that stops being valid cannot
		// survive in the guide.
		validateRuleCondition("guide", evDealDamage, sampleConditionFor(kind))
	}

	for _, kind := range guideEffects {
		vocabFor(kind)
	}
}

// sampleConditionFor builds a minimal VALID condition of the given kind, for
// validateGuideVocabulary to feed the real validator. Parameterised kinds get
// a real registry value, so the sample exercises the same lookups content does.
func sampleConditionFor(kind string) condition {
	switch kind {
	case condAttackerSpecies:
		return condition{kind: kind, s: protocol.SpeciesHuman}
	case condDamageType:
		return condition{kind: kind, s: protocol.DamageTypeBlunt}
	case condWeaponTagged:
		return condition{kind: kind, s: protocol.WeaponTagMelee}
	case condTargetKind:
		return condition{kind: kind, s: idKindWolf}
	default:
		return condition{kind: kind, n: 1}
	}
}

// guideStatsFor renders a def's stat lines through statlines.go — the SAME
// path that fills a tooltip, never a second rendering.
func guideStatsFor(def *itemDef) []GuideStat {
	views := statViewsFor(def)

	out := make([]GuideStat, 0, len(views))
	for _, v := range views {
		out = append(out, GuideStat{Text: v.Text, Drawback: v.Drawback})
	}

	return out
}

// GuideData assembles the guide's derivable half from the live registries.
// Sorted by id throughout so regenerating produces a stable diff — a guide
// that reorders itself every run is unreviewable.
func GuideData() Guide {
	g := Guide{
		DamageTypes: []string{
			protocol.DamageTypeBlunt, protocol.DamageTypeSharp, protocol.DamageTypeFire,
			protocol.DamageTypeIce, protocol.DamageTypeHoly, protocol.DamageTypeChaos,
		},
		WeaponTags: []string{protocol.WeaponTagMelee, protocol.WeaponTagRanged, protocol.WeaponTagMagic},
		Numbers:    guideNumbers(),
	}

	for _, kind := range guideEvents {
		g.Events = append(g.Events, vocabFor(kind))
	}

	for _, kind := range guideConditions {
		g.Conditions = append(g.Conditions, vocabFor(kind))
	}

	for _, kind := range guideEffects {
		g.Effects = append(g.Effects, vocabFor(kind))
	}

	for _, def := range itemDefs {
		// Monster natural weapons are shown in the monster table, where a
		// designer looks for them — not mixed into the gear a player can hold.
		if def.monsterOnly {
			continue
		}

		g.Items = append(g.Items, GuideItem{
			ID: def.id, Name: def.name, Type: def.itemType, DamageType: def.damageType,
			Tags: wireTags(def), TwoHanded: def.twoHanded,
			Damage: def.damage, RangeHex: def.rangeHex, AoERadius: def.aoeRadius, Heal: def.heal,
			Stats: guideStatsFor(def), Flavor: def.flavor,
		})
	}

	for _, def := range monsterDefs {
		g.Monsters = append(g.Monsters, GuideMonster{
			ID: def.id, Name: def.name, MaxHP: def.maxHP,
			Weapon: def.weaponDef.name, Damage: def.weaponDef.damage,
			RangeHex: def.weaponDef.rangeHex, DamageType: def.weaponDef.damageType,
			XP: def.xp, AggroRadius: aggroRadiusOf(def),
			DropChance: def.dropChance, Stats: guideStatsFor(&itemDef{rules: def.rules}),
		})
	}

	sort.Slice(g.Items, func(i, j int) bool { return g.Items[i].ID < g.Items[j].ID })
	sort.Slice(g.Monsters, func(i, j int) bool { return g.Monsters[i].ID < g.Monsters[j].ID })

	return g
}

// aggroRadiusOf reports the radius a kind actually notices from: its own
// override, or the global default when it sets none. The guide shows the
// EFFECTIVE number, because a designer pitching a new kind compares against
// what monsters really do, not against a zero meaning "default".
func aggroRadiusOf(def *monsterDef) int {
	if def.aggroRadius == 0 {
		return protocol.MonsterAggroRadius
	}

	return def.aggroRadius
}

// guideNumbers is the calibration set: the anchors a designer pitches a new
// number against. Values come from the live constants, never from prose.
func guideNumbers() []GuideNumber {
	return []GuideNumber{
		{
			Name: "Fists damage", Value: protocol.FistsDamage,
			Description: "An unarmed player's hit — the floor every weapon is measured against.",
		},
		{
			Name: "Glance damage", Value: protocol.GlanceDamagePercent,
			Description: "What a glance leaves of a hit, as a percent. A glance HALVES; it never negates.",
		},
		{
			Name: "Combat radius", Value: protocol.CombatRadius,
			Description: "A combat bubble's reach in hexes. A weapon's range + AoE can never exceed it.",
		},
		{
			Name: "Default aggro radius", Value: protocol.MonsterAggroRadius,
			Description: "How far a monster notices from unless its kind overrides it. " +
				"Always greater than the combat radius.",
		},
		{
			Name: "Leash multiplier", Value: protocol.MonsterLeashMultiplier,
			Description: "How far past its aggro radius a monster chases before walking home.",
		},
		{
			Name: "Item stack cap", Value: protocol.ItemStackCap,
			Description: "Consumables per backpack stack.",
		},
		{
			Name: "Forest sight cost", Value: protocol.ForestSightCost,
			Description: "What one forest hex costs a line of sight.",
		},
		{
			Name: "Percent base", Value: percentBase,
			Description: "100. A mulPct of 200 doubles; of 50 halves.",
		},
	}
}
