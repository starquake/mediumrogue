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
// first (the "live balance" numbers carried forward from the old protocol
// weapon constants), then the starter drop set (each a situational spike
// over its class's default, balanced per the design doc's table). Loot
// authority moved monster-side in 6c — an item no longer carries its own
// drop weight; monsterDefs' own tables (below) name these ids and weights
// instead. Every number here is authored content data (the design doc's
// table), not a tunable knob, hence the blanket mnd suppression — unlike
// speciesCards above, which reads protocol constants because those
// percentages ARE tuning knobs shared with other content.
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
		id: idButchersCleaver, name: "Butcher's Cleaver", slot: protocol.ItemSlotClose, class: protocol.ClassFighter,
		damage: 3, desc: "+3 damage vs targets below half HP",
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condTargetHPBelowPct, n: 50}}, then: effect{kind: effAdd, n: 3}},
		},
	},
	{
		id: idIronWarhammer, name: "Iron Warhammer", slot: protocol.ItemSlotClose, class: protocol.ClassFighter,
		damage: 6, desc: "a flat upgrade over the iron sword — rare",
	},
	{
		id: idVenomFang, name: "Venom Fang", slot: protocol.ItemSlotClose, class: protocol.ClassRogue,
		damage: 5, desc: "+4 damage vs targets at full HP",
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condTargetHPFull}}, then: effect{kind: effAdd, n: 4}},
		},
	},
	{
		id: idPackBow, name: "Pack Bow", slot: protocol.ItemSlotRanged, class: protocol.ClassRogue,
		damage: 5, rangeHex: 4, desc: "+3 damage while an ally shares the bubble",
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condAllyInBubble}}, then: effect{kind: effAdd, n: 3}},
		},
	},
	{
		id: idEmberStaff, name: "Ember Staff", slot: protocol.ItemSlotRanged, class: protocol.ClassMage,
		damage: 3, rangeHex: 4, aoeRadius: 1, desc: "double damage vs adjacent targets",
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condTargetAdjacent}}, then: effect{kind: effMulPct, n: 200}},
		},
	},

	// First designer batch (docs/rule-based-content-design.md's card format;
	// review in the first-gear correspondence). Authored by the group's
	// content designer — ids/names/numbers are his cards, transcribed.
	{
		id: idAncientDwarvenMattock, name: "Ancient Dwarven Mattock", slot: protocol.ItemSlotClose,
		class:  protocol.ClassFighter,
		damage: 4, desc: "+3 damage in a dwarf's hands",
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condAttackerSpecies, s: protocol.SpeciesDwarf}},
				then: effect{kind: effAdd, n: 3}},
		},
	},
	{
		id: idWarMageStaff, name: "Staff of the War Mage", slot: protocol.ItemSlotRanged,
		class:  protocol.ClassMage,
		damage: 3, rangeHex: 4, aoeRadius: 1, desc: "double damage vs targets below 6 HP",
		rules: []ruleCard{
			// Flat threshold BY DESIGN, not percent: a mop-up AoE that ends the
			// boring tail of a fight, and never scales into a boss-killer.
			{event: evDealDamage, when: []condition{{kind: condTargetHPBelowFlat, n: 6}},
				then: effect{kind: effMulPct, n: 200}},
		},
	},

	// Wyrmslayer Greatsword (milestone 6c): the first designer card's full
	// intent, previously blocked on monster kinds existing to gate a
	// per-species-style condition against. Dragon-only drop (dragon's own
	// table, below).
	{
		id: idWyrmslayerGreatsword, name: "Wyrmslayer Greatsword", slot: protocol.ItemSlotClose,
		class:  protocol.ClassFighter,
		damage: 4, desc: "×1.5 damage vs dragons",
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condTargetKind, s: idKindDragon}},
				then: effect{kind: effMulPct, n: 150}},
		},
	},
}

// itemDefByID is the lookup table derived from itemDefs at package init:
// O(1) resolution by id (equip, gear-panel wire lookups, monster loot-table
// validation). The weighted ground-drop roll is monster-side since 6c
// (monsterDef.drops, monsters.go) — items no longer carry their own weight.
//
//nolint:gochecknoglobals // derived lookup table, built once at init from itemDefs (see the func init below).
var itemDefByID map[string]*itemDef

// monsterDefs is the monster-kind content registry (milestone 6c): the
// spec's launch table (docs/superpowers/specs/2026-07-10-m6c-monster-kinds-rings-design.md).
// wolf carries today's EXACT pre-6c numbers verbatim (10 HP, 3 damage, 20
// XP, aggro 10, 30% drop, the current starter drop set in its original
// registry order/weights) so existing balance and most seeded tests survive
// unchanged. Every other kind's numbers are first-draft knobs, tuned
// against wolf and the player stats table in the spec. Order is registry
// order.
//
//nolint:gochecknoglobals,mnd // fixed content registry, effectively const; validated at init (mustValidateContent).
var monsterDefs = []*monsterDef{
	{
		id: idKindRat, name: "Rat", glyph: "r",
		// aggroRadius is CombatRadius+1, not the spec table's flat 6 — 6 fails
		// the validation invariant this file shares with protocol.MonsterAggroRadius
		// (a monster must notice a player before it can close into a combat
		// bubble, or it sits frozen just outside its own aggro range forever).
		maxHP: 4, damage: 1, xp: 8, aggroRadius: protocol.CombatRadius + 1, dropChance: 10,
		drops: []drop{{defID: idButchersCleaver, weight: 1}},
		rings: []int{0, 1},
	},
	{
		id: idKindWolf, name: "Wolf", glyph: "w",
		maxHP: 10, damage: 3, xp: 20, aggroRadius: protocol.MonsterAggroRadius, dropChance: 30,
		// The current starter drop set, same order/weights as the pre-6c
		// global dropTable — pins killDropSeed/killMissSeed (drops_test.go).
		drops: []drop{
			{defID: idButchersCleaver, weight: 4},
			{defID: idIronWarhammer, weight: 1},
			{defID: idVenomFang, weight: 4},
			{defID: idPackBow, weight: 4},
			{defID: idEmberStaff, weight: 4},
			{defID: idAncientDwarvenMattock, weight: 4},
			{defID: idWarMageStaff, weight: 4},
		},
		rings: []int{1},
	},
	{
		id: idKindGhoul, name: "Ghoul", glyph: "g",
		maxHP: 16, damage: 4, xp: 35, aggroRadius: 8, dropChance: 35,
		// The starter set with venom-fang weighted up (a ghoul's signature drop).
		drops: []drop{
			{defID: idButchersCleaver, weight: 4},
			{defID: idIronWarhammer, weight: 1},
			{defID: idVenomFang, weight: 8},
			{defID: idPackBow, weight: 4},
			{defID: idEmberStaff, weight: 4},
			{defID: idAncientDwarvenMattock, weight: 4},
			{defID: idWarMageStaff, weight: 4},
		},
		rings: []int{1, 2},
	},
	{
		id: idKindTroll, name: "Troll", glyph: "T",
		maxHP: 30, damage: 6, xp: 60, aggroRadius: 8, dropChance: 50,
		// The starter set with the warhammer/pack-bow/war-mage-staff weighted
		// up — a troll's frontier-tier signature drops.
		drops: []drop{
			{defID: idButchersCleaver, weight: 4},
			{defID: idIronWarhammer, weight: 4},
			{defID: idVenomFang, weight: 4},
			{defID: idPackBow, weight: 8},
			{defID: idEmberStaff, weight: 4},
			{defID: idAncientDwarvenMattock, weight: 4},
			{defID: idWarMageStaff, weight: 8},
		},
		rings: []int{2},
	},
	{
		id: idKindDragon, name: "Dragon", glyph: "D",
		maxHP: 60, damage: 9, xp: 150, aggroRadius: 12, dropChance: 100,
		// The Wyrmslayer Greatsword (weight 2 — the headline drop, roughly as
		// likely as the whole rest of the rare pool combined) plus a small
		// rare pool.
		drops: []drop{
			{defID: idWyrmslayerGreatsword, weight: 2},
			{defID: idIronWarhammer, weight: 1},
			{defID: idWarMageStaff, weight: 1},
		},
		rings: []int{2}, // rare: capped at protocol.DragonCount per world by the ring spawner (6c Task 3)
	},
}

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

	buildMonsterIndex()

	mustValidateContent()
}
