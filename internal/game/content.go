package game

import "github.com/starquake/mediumrogue/internal/protocol"

// content.go is the content registry: the pipeline's rule cards land here.
// Species passives are the first rule set (numbers stay protocol constants:
// they're tuning knobs).
//
//nolint:gochecknoglobals // fixed rule-card tables, effectively const.
var (
	humanCards = []ruleCard{
		{event: evEarnXP, then: effect{kind: effMulPct, n: percentBase + protocol.HumanXPBonusPercent}},
	}
	elfCards = []ruleCard{
		{event: evDealDamage, when: []condition{{kind: condChance, n: protocol.ElfCritChancePercent}},
			then: effect{kind: effMulPct, n: percentBase * protocol.ElfCritMultiplier}},
	}
	dwarfCards = []ruleCard{
		{event: evTakeDamage, then: effect{kind: effAdd, n: -protocol.DwarfDamageReduction}},
	}
)

// speciesCards returns a species' passive rule cards (nil for monsters'
// empty species).
func speciesCards(species string) []ruleCard {
	switch species {
	case protocol.SpeciesHuman:
		return humanCards
	case protocol.SpeciesElf:
		return elfCards
	case protocol.SpeciesDwarf:
		return dwarfCards
	default:
		return nil
	}
}

// itemDefs is the item content registry (milestone 6b.4): class defaults
// first (dropWeight 0 — the "live balance" numbers carried forward from the
// old protocol weapon constants), then the starter drop set (dropWeight > 0,
// each a situational spike over its class's default, balanced per the design
// doc's table). Order is registry order — dropTable below preserves it. Every
// number here is authored content data (the design doc's table), not a
// tunable knob, hence the blanket mnd suppression — unlike speciesCards
// above, which reads protocol constants because those percentages ARE tuning
// knobs shared with other content.
//
//nolint:gochecknoglobals,mnd // fixed content registry, effectively const; validated at init (mustValidateContent).
var itemDefs = []*itemDef{
	// Class defaults.
	{id: idIronSword, name: "Iron Sword", slot: protocol.ItemSlotClose, class: protocol.ClassFighter, damage: 4},
	{id: idDagger, name: "Dagger", slot: protocol.ItemSlotClose, class: protocol.ClassRogue, damage: 7},
	{
		id: idShortbow, name: "Shortbow", slot: protocol.ItemSlotRanged, class: protocol.ClassRogue,
		damage: 6, rangeHex: 4,
	},
	{id: idOakStaff, name: "Oak Staff", slot: protocol.ItemSlotClose, class: protocol.ClassMage, damage: 2},
	{
		id: idEmberFocus, name: "Ember Focus", slot: protocol.ItemSlotRanged, class: protocol.ClassMage,
		damage: 4, rangeHex: 4, aoeRadius: 1,
	},

	// Starter drop set.
	{
		id: "butchers-cleaver", name: "Butcher's Cleaver", slot: protocol.ItemSlotClose, class: protocol.ClassFighter,
		damage: 3, desc: "+3 damage vs targets below half HP", dropWeight: 4,
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condTargetHPBelowPct, n: 50}}, then: effect{kind: effAdd, n: 3}},
		},
	},
	{
		id: "iron-warhammer", name: "Iron Warhammer", slot: protocol.ItemSlotClose, class: protocol.ClassFighter,
		damage: 6, desc: "a flat upgrade over the iron sword — rare", dropWeight: 1,
	},
	{
		id: "venom-fang", name: "Venom Fang", slot: protocol.ItemSlotClose, class: protocol.ClassRogue,
		damage: 5, desc: "+4 damage vs targets at full HP", dropWeight: 4,
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condTargetHPFull}}, then: effect{kind: effAdd, n: 4}},
		},
	},
	{
		id: "pack-bow", name: "Pack Bow", slot: protocol.ItemSlotRanged, class: protocol.ClassRogue,
		damage: 5, rangeHex: 4, desc: "+3 damage while an ally shares the bubble", dropWeight: 4,
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condAllyInBubble}}, then: effect{kind: effAdd, n: 3}},
		},
	},
	{
		id: "ember-staff", name: "Ember Staff", slot: protocol.ItemSlotRanged, class: protocol.ClassMage,
		damage: 3, rangeHex: 4, aoeRadius: 1, desc: "double damage vs adjacent targets", dropWeight: 4,
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condTargetAdjacent}}, then: effect{kind: effMulPct, n: 200}},
		},
	},

	// First designer batch (docs/rule-based-content-design.md's card format;
	// review in the first-gear correspondence). Authored by the group's
	// content designer — ids/names/numbers are his cards, transcribed.
	{
		id: "ancient-dwarven-mattock", name: "Ancient Dwarven Mattock", slot: protocol.ItemSlotClose,
		class:  protocol.ClassFighter,
		damage: 4, desc: "+3 damage in a dwarf's hands", dropWeight: 4,
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condAttackerSpecies, s: protocol.SpeciesDwarf}},
				then: effect{kind: effAdd, n: 3}},
		},
	},
	{
		id: "war-mage-staff", name: "Staff of the War Mage", slot: protocol.ItemSlotRanged,
		class:  protocol.ClassMage,
		damage: 3, rangeHex: 4, aoeRadius: 1, desc: "double damage vs targets below 6 HP", dropWeight: 4,
		rules: []ruleCard{
			// Flat threshold BY DESIGN, not percent: a mop-up AoE that ends the
			// boring tail of a fight, and never scales into a boss-killer.
			{event: evDealDamage, when: []condition{{kind: condTargetHPBelowFlat, n: 6}},
				then: effect{kind: effMulPct, n: 200}},
		},
	},
}

// itemDefByID and dropTable are lookup tables derived from itemDefs at
// package init: itemDefByID for O(1) resolution by id (equip, gear-panel
// wire lookups), dropTable for the weighted ground-drop roll (every def with
// dropWeight > 0, in registry order — determinism for the seeded pick).
//
//nolint:gochecknoglobals // derived lookup tables, built once at init from itemDefs (see the func init below).
var (
	itemDefByID map[string]*itemDef
	dropTable   []*itemDef
)

// init builds the derived lookup tables and validates the registry
// (mustValidateContent panics on a content bug), exactly once, before any
// World can exist.
//
//nolint:gochecknoinits // one-time content indexing/validation; see the doc comment above.
func init() {
	itemDefByID = make(map[string]*itemDef, len(itemDefs))
	for _, def := range itemDefs {
		itemDefByID[def.id] = def
	}

	for _, def := range itemDefs {
		if def.dropWeight > 0 {
			dropTable = append(dropTable, def)
		}
	}

	mustValidateContent()
}
