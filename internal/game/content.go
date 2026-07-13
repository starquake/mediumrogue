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
	{
		id: idIronSword, name: "Iron Sword", itemType: protocol.ItemTypeMeleeWeapon,
		wearableBy: []string{protocol.ClassFighter}, damage: 4,
	},
	{
		id: idDagger, name: "Dagger", itemType: protocol.ItemTypeMeleeWeapon,
		wearableBy: []string{protocol.ClassRogue}, damage: 7,
	},
	{
		id: idShortbow, name: "Shortbow", itemType: protocol.ItemTypeRangedWeapon,
		wearableBy: []string{protocol.ClassRogue}, damage: 6, rangeHex: 4,
	},
	{
		id: idOakStaff, name: "Oak Staff", itemType: protocol.ItemTypeStaff,
		wearableBy: []string{protocol.ClassMage}, damage: 2,
	},
	{
		id: idEmberFocus, name: "Ember Focus", itemType: protocol.ItemTypeWand,
		wearableBy: []string{protocol.ClassMage}, damage: 4, rangeHex: 4, aoeRadius: 1,
	},

	// Starter drop set.
	{
		id: idButchersCleaver, name: "Butcher's Cleaver", itemType: protocol.ItemTypeMeleeWeapon,
		wearableBy: []string{protocol.ClassFighter},
		damage:     3, desc: "+3 damage vs targets below half HP",
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condTargetHPBelowPct, n: 50}}, then: effect{kind: effAdd, n: 3}},
		},
	},
	{
		id: idIronWarhammer, name: "Iron Warhammer", itemType: protocol.ItemTypeMeleeWeapon,
		wearableBy: []string{protocol.ClassFighter},
		damage:     6, desc: "a flat upgrade over the iron sword — rare",
	},
	{
		id: idVenomFang, name: "Venom Fang", itemType: protocol.ItemTypeMeleeWeapon,
		wearableBy: []string{protocol.ClassRogue},
		damage:     5, desc: "+4 damage vs targets at full HP",
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condTargetHPFull}}, then: effect{kind: effAdd, n: 4}},
		},
	},
	{
		id: idPackBow, name: "Pack Bow", itemType: protocol.ItemTypeRangedWeapon,
		wearableBy: []string{protocol.ClassRogue},
		damage:     5, rangeHex: 4, desc: "+3 damage while an ally shares the bubble",
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condAllyInBubble}}, then: effect{kind: effAdd, n: 3}},
		},
	},
	{
		id: idEmberStaff, name: "Ember Staff", itemType: protocol.ItemTypeWand,
		wearableBy: []string{protocol.ClassMage},
		damage:     3, rangeHex: 4, aoeRadius: 1, desc: "double damage vs adjacent targets",
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condTargetAdjacent}}, then: effect{kind: effMulPct, n: 200}},
		},
	},

	// First designer batch (docs/rule-based-content-design.md's card format;
	// review in the first-gear correspondence). Authored by the group's
	// content designer — ids/names/numbers are his cards, transcribed.
	{
		id: idAncientDwarvenMattock, name: "Ancient Dwarven Mattock", itemType: protocol.ItemTypeMeleeWeapon,
		wearableBy: []string{protocol.ClassFighter},
		damage:     4, desc: "+3 damage in a dwarf's hands",
		flavor: "This ancient mattock still holds a razor-sharp edge.",
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condAttackerSpecies, s: protocol.SpeciesDwarf}},
				then: effect{kind: effAdd, n: 3}},
		},
	},
	{
		id: idWarMageStaff, name: "Staff of the War Mage", itemType: protocol.ItemTypeWand,
		wearableBy: []string{protocol.ClassMage},
		damage:     3, rangeHex: 4, aoeRadius: 1, desc: "double damage vs targets below 6 HP",
		flavor: "Tuned to eliminate the weakest enemies.",
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
		id: idWyrmslayerGreatsword, name: "Wyrmslayer Greatsword", itemType: protocol.ItemTypeMeleeWeapon,
		wearableBy: []string{protocol.ClassFighter},
		damage:     4, desc: "×1.5 damage vs dragons",
		flavor: "Forged by a legendary hero to slay the evil dragon Werdmullerix.",
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condTargetKind, s: idKindDragon}},
				then: effect{kind: effMulPct, n: 150}},
		},
	},

	// Healing Potion (inventory-slots task 2): the first consumable — heal is
	// a def field consumed by the drink ACTION (inventory.go), not a combat
	// pipeline event. Registered with the action machinery (its stack/drink
	// tests need a real consumable); it rides the rat/wolf drop tables at low
	// weight (task 3 — recovery layer 2 begins).
	{
		id: idHealingPotion, name: "Healing Potion", itemType: protocol.ItemTypeConsumable,
		heal: 5, desc: "drink: +5 HP; stacks to 5",
	},

	// Inventory-slots starter armor (task 3, the designer's cards): the first
	// non-weapon gear — leather-armor is the first multi-class WEARABILITY
	// card (fighter OR rogue; characters stay single-class), and
	// headband-of-learning defaults to "any" (empty wearableBy, the
	// armor/jewelry rule).
	{
		id: idLeatherArmor, name: "Leather Armor", itemType: protocol.ItemTypeBody,
		wearableBy: []string{protocol.ClassFighter, protocol.ClassRogue},
		desc:       "take a little less from every hit",
		flavor:     "Supple leather that lets you dodge out of harm's way.",
		rules: []ruleCard{
			// take-damage −1; applyRules' event-level clamp keeps every landed
			// hit ≥1 (the card's "floor 1").
			{event: evTakeDamage, then: effect{kind: effAdd, n: -1}},
		},
	},
	{
		id: idHeadbandOfLearning, name: "Headband of Learning", itemType: protocol.ItemTypeHead,
		desc:   "earn 5% more XP",
		flavor: "Stimulates your tiny little brain, for faster learning.",
		rules: []ruleCard{
			{event: evEarnXP, then: effect{kind: effMulPct, n: percentBase + 5}},
		},
	},

	// Crit%-weapons (fast-lane batch task 6, #69 Q5): the first weapons
	// carrying a per-hit crit-chance card — the elf-crit card pattern
	// (elfCards, above) applied to an ITEM instead of a species passive.
	{
		id: idMisericorde, name: "Misericorde", itemType: protocol.ItemTypeMeleeWeapon,
		wearableBy: []string{protocol.ClassRogue},
		damage:     6, desc: "15% chance to strike true for double damage",
		flavor: "A blade thin enough to find the gap between any two plates.",
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condChance, n: 15}},
				then: effect{kind: effMulPct, n: percentBase + 100}},
		},
	},
	{
		id: idDuelistsSaber, name: "Duelist's Saber", itemType: protocol.ItemTypeMeleeWeapon,
		wearableBy: []string{protocol.ClassFighter},
		damage:     5, desc: "10% chance to land a perfect riposte for double",
		flavor: "Its balance rewards patience; its edge rewards timing.",
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condChance, n: 10}},
				then: effect{kind: effMulPct, n: percentBase + 100}},
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
		id: idKindRat, name: "Rat",
		// aggroRadius is CombatRadius+1, not the spec table's flat 6 — 6 fails
		// the validation invariant this file shares with protocol.MonsterAggroRadius
		// (a monster must notice a player before it can close into a combat
		// bubble, or it sits frozen just outside its own aggro range forever).
		maxHP: 4, damage: 1, xp: 8, aggroRadius: protocol.CombatRadius + 1, dropChance: 10,
		drops: []drop{
			{defID: idButchersCleaver, weight: 1},
			// Low-weight potion (inventory-slots task 3): recovery layer 2.
			{defID: idHealingPotion, weight: 1},
		},
		rings: []int{0, 1},
	},
	{
		id: idKindWolf, name: "Wolf",
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
			// Low-weight potion (inventory-slots task 3): appended LAST so the
			// pre-existing entries keep their cumulative-weight positions and
			// the pinned killDropSeed/killMissSeed (drops_test.go) survive
			// where possible.
			{defID: idHealingPotion, weight: 2},
			// Duelist's Saber (fast-lane batch task 6, #69 Q5): appended LAST
			// for the same reason — every earlier entry keeps its
			// cumulative-weight position, so killDropSeed/killMissSeed
			// (drops_test.go) survive unchanged.
			{defID: idDuelistsSaber, weight: 4},
		},
		rings: []int{1},
	},
	{
		id: idKindGhoul, name: "Ghoul",
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
			// Misericorde (fast-lane batch task 6, #69 Q5): appended LAST so
			// every earlier entry keeps its cumulative-weight position (this
			// kind is not pinned by drops_test.go today, but the rule holds
			// regardless).
			{defID: idMisericorde, weight: 4},
		},
		rings: []int{1, 2},
	},
	{
		id: idKindTroll, name: "Troll",
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
		id: idKindDragon, name: "Dragon",
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
