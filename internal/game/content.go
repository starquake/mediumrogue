package game

import "github.com/starquake/mediumrogue/internal/protocol"

// content.go is the content registry: the pipeline's rule cards land here.
// Species passives are the first rule set (numbers stay protocol constants:
// they're tuning knobs).
//
//nolint:gochecknoglobals // fixed rule-card tables, effectively const.
var (
	// humanCards is deliberately EMPTY since #124 task 8: the Human perk is
	// no longer an XP multiplier but a +1 skill point per level, granted in
	// grantSkillPointsLocked (world.go). A per-level BANK grant is not a fold
	// over a combat value, so it is a species check rather than a rule card —
	// and inventing an evLevelUp event for a single rider would trip the
	// no-mechanic-wildfire gate.
	//
	// The old +50% XP was invisible in play: levels grant HP only, so it
	// bought "reach the same HP slightly sooner" while the elf and dwarf
	// perks paid off in every fight (#123).
	humanCards = []ruleCard{}
	elfCards   = []ruleCard{
		{event: evDealDamage, when: []condition{{kind: condChance, n: protocol.ElfCritChancePercent}},
			then: effect{kind: effMulPct, n: percentBase * protocol.ElfCritMultiplier}},
	}
	dwarfCards = []ruleCard{
		{event: evTakeDamage, then: effect{kind: effAdd, n: -protocol.DwarfDamageReduction}},
	}
	rogueGlanceCards = []ruleCard{
		{event: evTakeDamage, when: []condition{{kind: condChance, n: protocol.RogueGlanceChancePercent}},
			then: effect{kind: effMulPct, n: protocol.GlanceDamagePercent}},
	}
)

// Timed-effect def ids (#271, slice 1). An effect def is the STRUCTURAL half
// of a timed effect (which event it folds at, which verb) — the per-application
// magnitude and duration live on the instance (effects.go). Named here for the
// usual reason: registry, on-hit riders (itemDefs.onHit), and their tests can't
// drift on a typo.
const (
	idEffectPoison = "poison"
	idEffectFrenzy = "frenzy"
	idEffectRegen  = "regen"
	// idEffectWard is the timed defensive buff (#271, slice 2): a take-damage
	// mulPct folded on the victim side, applied by the Warding Tonic on drink.
	// It reuses the existing evTakeDamage event and effMulPct verb — pure
	// content, no new pipeline kind.
	idEffectWard = "ward"
)

// effectDefs is the timed-effect content registry (#271). Three defs, two of
// which back the slice's proof consumers:
//   - poison: a DoT — a negative effAdd folded at end-of-turn (the Serpent's
//     bite applies it, idVenomSting.onHit). The first evEndOfTurn consumer.
//   - frenzy: a self-buff — a deal-damage mulPct (idBloodrageCleaver.onHit
//     applies it to the attacker). Exercises the fold-into-combat path.
//   - regen: a heal-over-turn — a POSITIVE effAdd folded at end-of-turn. The
//     SECOND evEndOfTurn consumer (its heal direction), proving the event folds
//     both ways. Slice 2 gives it a live trigger: the Hydra's bite self-applies
//     it (idHydraFangs.onHit, toSelf), so the regenerating monster is real
//     content, not only a white-box test.
//   - ward: a timed DEFENSIVE buff (#271, slice 2) — a take-damage mulPct folded
//     victim-side, applied by the Warding Tonic on drink. Reuses evTakeDamage +
//     effMulPct, so it is content, not a new pipeline kind.
//
// poison is the only HARMFUL def (a DoT that drains you); the buffs (frenzy,
// ward) and regen are beneficial, so the cleanse path (Antivenom,
// clearHarmfulEffectsLocked) strips the poison and leaves them.
//
// magnitude and duration are per-INSTANCE (set where the effect is applied), so
// the defs carry no numbers — nothing here to tune, hence no mnd suppression.
//
//nolint:gochecknoglobals // fixed effect-def registry, effectively const; validated at init.
var effectDefs = []*effectDef{
	{id: idEffectPoison, name: "Poison", event: evEndOfTurn, effect: effAdd, harmful: true},
	{id: idEffectRegen, name: "Regeneration", event: evEndOfTurn, effect: effAdd},
	{id: idEffectFrenzy, name: "Frenzy", event: evDealDamage, effect: effMulPct},
	{id: idEffectWard, name: "Ward", event: evTakeDamage, effect: effMulPct},
}

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

// classCards returns a class's passive rule cards (nil for other classes
// and for monsters' empty class). The Rogue's glance% (#91) is the first
// class passive: the take-damage mirror of the elf-crit card — a
// chance-conditioned mulPct, pure content, no new pipeline event (the
// 2026-07-15 spec's point). Folded victim-side by rollDamageLocked, right
// after speciesCards.
func classCards(class string) []ruleCard {
	if class == protocol.ClassRogue {
		return rogueGlanceCards
	}

	return nil
}

// itemDefs is the item content registry: class defaults first, then the
// starter drop set (each a situational spike over its class's default),
// then the designer batches. Loot authority is monster-side (6c) — an item
// no longer carries its own drop weight; monsterDefs' own tables (below)
// name these ids and weights instead. Every weapon carries tags (which
// attacks fire it) and twoHanded — the gear keystone's taxonomy (#55/#56);
// class gates are gone, so wearableBy no longer exists at all. Every number
// here is authored content data (the design doc's table, rebalanced per the
// keystone spec §4's "1H ≈ ½ 2H" pass), not a tunable knob, hence the
// blanket mnd suppression — unlike speciesCards above, which reads protocol
// constants because those percentages ARE tuning knobs shared with other
// content.
//
//nolint:gochecknoglobals,mnd // fixed content registry, effectively const; validated at init (mustValidateContent).
var itemDefs = []*itemDef{
	// Class defaults.
	{
		id: idIronSword, name: "Iron Sword", itemType: protocol.ItemTypeWeapon,
		tags: []string{protocol.WeaponTagMelee}, damage: 4,
		damageType: protocol.DamageTypeSharp,
	},
	{
		// re-derived: gear keystone rebalance (damage 7 -> 4, §4's "1H ≈ ½ 2H"
		// pass — the dagger no longer out-damages the fighter's sword).
		id: idDagger, name: "Dagger", itemType: protocol.ItemTypeWeapon,
		tags: []string{protocol.WeaponTagMelee}, damage: 4,
		damageType: protocol.DamageTypeSharp,
	},
	{
		// re-derived: gear keystone rebalance (damage 6 -> 4).
		id: idShortbow, name: "Shortbow", itemType: protocol.ItemTypeWeapon,
		tags: []string{protocol.WeaponTagRanged}, damage: 4, rangeHex: 4,
		damageType: protocol.DamageTypeSharp,
	},
	{
		id: idOakWand, name: "Oak Wand", itemType: protocol.ItemTypeWeapon,
		tags: []string{protocol.WeaponTagMelee}, damage: 2,
		damageType: protocol.DamageTypeBlunt,
	},
	{
		// re-derived: gear keystone rebalance (damage 4 -> 3).
		id: idEmberFocus, name: "Ember Focus", itemType: protocol.ItemTypeWeapon,
		tags: []string{protocol.WeaponTagMagic}, damage: 3, rangeHex: 4, aoeRadius: 1,
		damageType: protocol.DamageTypeFire,
	},

	// Monster natural weapons (#179). A kind NAMES one of these instead of
	// carrying a private copy of a weapon's fields, so the base layer is
	// shared with player gear all the way down (see design-decisions.md's
	// one-base-layer entry). They carry NO rule cards on purpose: a kind's
	// own cards fold alongside the weapon's, and keeping these empty leaves
	// the fold sequence byte-identical to the pre-#179 order, so every
	// pinned seed survives.
	//
	// monsterOnly keeps them unreachable by players — validated, not trusted.
	{
		id: idClaws, name: "Claws", itemType: protocol.ItemTypeWeapon,
		tags: []string{protocol.WeaponTagMelee}, damage: 1,
		damageType: protocol.DamageTypeSharp, monsterOnly: true,
	},
	{
		id: idFangs, name: "Fangs", itemType: protocol.ItemTypeWeapon,
		tags: []string{protocol.WeaponTagMelee}, damage: 3,
		damageType: protocol.DamageTypeSharp, monsterOnly: true,
	},
	{
		id: idTalons, name: "Talons", itemType: protocol.ItemTypeWeapon,
		tags: []string{protocol.WeaponTagMelee}, damage: 4,
		damageType: protocol.DamageTypeChaos, monsterOnly: true,
	},
	{
		id: idMaul, name: "Maul", itemType: protocol.ItemTypeWeapon,
		tags: []string{protocol.WeaponTagMelee}, damage: 6,
		damageType: protocol.DamageTypeBlunt, monsterOnly: true,
	},
	{
		id: idDragonJaws, name: "Dragon Jaws", itemType: protocol.ItemTypeWeapon,
		tags: []string{protocol.WeaponTagMelee}, damage: 9,
		damageType: protocol.DamageTypeFire, monsterOnly: true,
	},
	{
		// The whole point of #179: a monster weapon with reach, which the
		// old copied-fields shorthand could not express at all.
		id: idHunterBow, name: "Hunter's Bow", itemType: protocol.ItemTypeWeapon,
		tags: []string{protocol.WeaponTagRanged}, damage: 3, rangeHex: 3,
		damageType: protocol.DamageTypeSharp, monsterOnly: true,
	},

	// Content-expansion natural weapons (#266): the new kinds' claws. Like
	// every monster weapon they carry NO rule cards — a kind's own cards
	// (a skeleton's sharp resistance) belong to the kind, not to a weapon
	// other kinds may share.
	{
		id: idRustyShiv, name: "Rusty Shiv", itemType: protocol.ItemTypeWeapon,
		tags: []string{protocol.WeaponTagMelee}, damage: 2,
		damageType: protocol.DamageTypeSharp, monsterOnly: true,
	},
	{
		id: idBoneClub, name: "Bone Club", itemType: protocol.ItemTypeWeapon,
		tags: []string{protocol.WeaponTagMelee}, damage: 3,
		damageType: protocol.DamageTypeBlunt, monsterOnly: true,
	},
	{
		id: idFrostTouch, name: "Frost Touch", itemType: protocol.ItemTypeWeapon,
		tags: []string{protocol.WeaponTagMelee}, damage: 4,
		damageType: protocol.DamageTypeIce, monsterOnly: true,
	},

	// Starter drop set.
	{
		id: idButchersCleaver, name: "Butcher's Cleaver", itemType: protocol.ItemTypeWeapon,
		tags:       []string{protocol.WeaponTagMelee},
		damageType: protocol.DamageTypeSharp,
		damage:     3,
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condTargetHPBelowPct, n: 50}}, then: effect{kind: effAdd, n: 3}},
		},
		flavor: "Made for carcasses. It has never much minded the difference.",
	},
	{
		// re-derived: gear keystone rebalance (damage 6 -> 5).
		id: idIronWarhammer, name: "Iron Warhammer", itemType: protocol.ItemTypeWeapon,
		tags:       []string{protocol.WeaponTagMelee},
		damageType: protocol.DamageTypeBlunt,
		damage:     5,
		flavor:     "Not clever. It has never needed to be.",
	},
	{
		// re-derived: gear keystone rebalance (damage 5 -> 3).
		id: idVenomFang, name: "Venom Fang", itemType: protocol.ItemTypeWeapon,
		tags:       []string{protocol.WeaponTagMelee},
		damageType: protocol.DamageTypeSharp,
		damage:     3,
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condTargetHPFull}}, then: effect{kind: effAdd, n: 4}},
		},
		flavor: "The venom went stale a long time ago. The point did not.",
	},
	{
		// re-derived: gear keystone rebalance (damage 5 -> 3).
		id: idPackBow, name: "Pack Bow", itemType: protocol.ItemTypeWeapon,
		tags:       []string{protocol.WeaponTagRanged},
		damageType: protocol.DamageTypeSharp,
		damage:     3, rangeHex: 4,
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condAllyInBubble}}, then: effect{kind: effAdd, n: 3}},
		},
		flavor: "Wolf-gut string. It sings better in company.",
	},
	{
		// re-derived: staves 2H, wands 1H (keystone amendment) — damage 3 -> 6.
		id: idEmberStaff, name: "Ember Staff", itemType: protocol.ItemTypeWeapon,
		tags: []string{protocol.WeaponTagMagic}, twoHanded: true,
		damageType: protocol.DamageTypeFire,
		damage:     6, rangeHex: 4, aoeRadius: 1,
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condTargetAdjacent}}, then: effect{kind: effMulPct, n: 200}},
		},
		flavor: "Close work — the kind that singes your own eyebrows.",
	},

	// First designer batch (docs/content-authoring.md's card format;
	// review in the first-gear correspondence). Authored by the group's
	// content designer — ids/names/numbers are his cards, transcribed.
	{
		id: idAncientDwarvenMattock, name: "Ancient Dwarven Mattock", itemType: protocol.ItemTypeWeapon,
		tags:       []string{protocol.WeaponTagMelee},
		damageType: protocol.DamageTypeBlunt,
		damage:     4,
		flavor:     "This ancient mattock still holds a razor-sharp edge.",
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condAttackerSpecies, s: protocol.SpeciesDwarf}},
				then: effect{kind: effAdd, n: 3}},
		},
	},
	{
		// re-derived: staves 2H, wands 1H (keystone amendment) — damage 3 -> 6.
		id: idWarMageStaff, name: "Staff of the War Mage", itemType: protocol.ItemTypeWeapon,
		tags: []string{protocol.WeaponTagMagic}, twoHanded: true,
		damageType: protocol.DamageTypeFire,
		damage:     6, rangeHex: 4, aoeRadius: 1,
		flavor: "Tuned to eliminate the weakest enemies.",
		rules: []ruleCard{
			// Flat threshold BY DESIGN, not percent: a mop-up AoE that ends the
			// boring tail of a fight, and never scales into a boss-killer.
			{event: evDealDamage, when: []condition{{kind: condTargetHPBelowFlat, n: 6}},
				then: effect{kind: effMulPct, n: 200}},
		},
	},

	// Wyrmslayer Greatsword (milestone 6c, retagged/rebalanced as the gear
	// keystone's first two-handed weapon, #55/#56 §4): the first designer
	// card's full intent, previously blocked on monster kinds existing to
	// gate a per-species-style condition against. Dragon-only drop (dragon's
	// own table, below).
	{
		// re-derived: gear keystone rebalance (damage 4 -> 9, twoHanded — the
		// keystone spec's "1H ≈ ½ 2H" anchor: a 2H roughly doubles a 1H).
		id: idWyrmslayerGreatsword, name: "Wyrmslayer Greatsword", itemType: protocol.ItemTypeWeapon,
		tags: []string{protocol.WeaponTagMelee}, twoHanded: true,
		damageType: protocol.DamageTypeHoly,
		damage:     9,
		flavor:     "Forged by a legendary hero to slay the evil dragon Werdmullerix.",
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
		heal:   5,
		flavor: "Tastes like a barn floor. Nobody drinks it for the taste.",
	},

	// Inventory-slots starter armor (task 3, the designer's cards): the first
	// non-weapon gear. Class gates are gone (gear keystone, #55/#56) — every
	// class may equip both.
	{
		id: idLeatherArmor, name: "Leather Armor", itemType: protocol.ItemTypeChest,
		flavor: "Supple leather that lets you dodge out of harm's way.",
		rules: []ruleCard{
			// Mitigation is PERCENTAGE, not flat (#154): a percent is still a
			// percent against a dragon, where a flat −N stacked into the ≥1
			// clamp and stopped meaning anything. Percent deltas ADD within
			// one fold (#61 principle 14), so pieces combine predictably.
			{event: evTakeDamage, then: effect{kind: effMulPct, n: percentBase - 10}},
		},
	},
	{
		id: idHeadbandOfLearning, name: "Headband of Learning", itemType: protocol.ItemTypeHelmet,
		flavor: "Stimulates your tiny little brain, for faster learning.",
		rules: []ruleCard{
			{event: evEarnXP, then: effect{kind: effMulPct, n: percentBase + 5}},
		},
	},

	// Shields (#90, S4 of #55): the last gear-keystone slice. A shield is
	// pure defence — a flat take-damage card, no damage of its own
	// (validateItemCombatStats) — and occupies the off-hand (slotForType),
	// trading dual-wield's second hit for the reduction. Richer
	// block/evasion waits on #69; shield skills on #57. Drop-only: no class
	// starting kit changes. applyRules' event-level clamp keeps every
	// landed hit ≥ 1, so the −2 stacking with leather armor and the dwarf
	// passive never zeroes a hit.
	{
		id: idWoodenBuckler, name: "Wooden Buckler", itemType: protocol.ItemTypeShield,
		flavor: "A barrel lid with delusions of grandeur. It holds.",
		rules: []ruleCard{
			{event: evTakeDamage, then: effect{kind: effMulPct, n: percentBase - 10}},
		},
	},
	{
		id: idIronKiteShield, name: "Iron Kite Shield", itemType: protocol.ItemTypeShield,
		flavor: "Iron-bound and man-high — the wall the front rank hides behind.",
		rules: []ruleCard{
			{event: evTakeDamage, then: effect{kind: effMulPct, n: percentBase - 20}},
		},
	},

	// Crit%-weapons (fast-lane batch task 6, #69 Q5): the first weapons
	// carrying a per-hit crit-chance card — the elf-crit card pattern
	// (elfCards, above) applied to an ITEM instead of a species passive.
	{
		// re-derived: gear keystone rebalance (damage 6 -> 4).
		id: idMisericorde, name: "Misericorde", itemType: protocol.ItemTypeWeapon,
		tags:       []string{protocol.WeaponTagMelee},
		damageType: protocol.DamageTypeSharp,
		damage:     4,
		flavor:     "A blade thin enough to find the gap between any two plates.",
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condChance, n: 15}},
				then: effect{kind: effMulPct, n: percentBase + 100}},
		},
	},
	{
		// re-derived: gear keystone rebalance (damage 5 -> 4).
		id: idDuelistsSaber, name: "Duelist's Saber", itemType: protocol.ItemTypeWeapon,
		tags:       []string{protocol.WeaponTagMelee},
		damageType: protocol.DamageTypeSharp,
		damage:     4,
		flavor:     "Its balance rewards patience; its edge rewards timing.",
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condChance, n: 10}},
				then: effect{kind: effMulPct, n: percentBase + 100}},
		},
	},

	// Noticeability gear (#88, the last Gear 1 slice): the first content to
	// use evAggroRange — the player-side fold over protocol.MonsterAggroRadius
	// that decides how far off a WORLD-domain monster notices its wearer
	// (aggroRadiusForLocked, world.go). Noticeability is deliberately
	// GEAR-ONLY, not a species trait: it is a choice you make in the
	// inventory, not one you're born into. Multiplicative, so it scales with
	// each monster kind's own radius instead of flattening them — over wolf's
	// 10 the boots read 7 and the plate 12; applyRules' event-level clamp
	// keeps any radius ≥1.
	{
		id: idPaddedBoots, name: "Padded Boots", itemType: protocol.ItemTypeBoots,
		flavor: "Rag-wrapped soles. You hear yourself think, and nothing hears you.",
		rules: []ruleCard{
			{event: evAggroRange, then: effect{kind: effMulPct, n: percentBase - 25}},
		},
	},
	{
		// The game's first TRADEOFF item: a strict upgrade on Leather Armor's
		// mitigation (−2 vs −1) bought with a real cost — you are noticed 25%
		// sooner. Gear that is only ever better makes the inventory a sorting
		// exercise; a cost makes it a decision.
		id: idIronPlateArmor, name: "Iron Plate Armor", itemType: protocol.ItemTypeChest,
		flavor: "It turns blades. It also announces you, one clank at a time.",
		rules: []ruleCard{
			{event: evTakeDamage, then: effect{kind: effMulPct, n: percentBase - 20}},
			{event: evAggroRange, then: effect{kind: effMulPct, n: percentBase + 25}},
		},
	},
	// Damage-type content wave (#92, DT1): types must be FELT on day one, so
	// the wave ships one resist armor per FAMILY (physical / elemental /
	// metaphysical) plus a weapon for each type that had no representative.
	// A resist is an ordinary take-damage card gated on the incoming type
	// (condDamageType) — no resist subsystem, no new machinery.
	//
	// Numbers below are first-draft knobs anchored on the shipped armor
	// ladder (Leather -1, Iron Plate -2 flat): a resist is a HALVING, but
	// only against one of six types, so it is situational where flat
	// mitigation is always-on. Designer rewording and retuning welcome.
	{
		// The parked P2 designer card, unparked (#92).
		id: idInfernalChainMail, name: "Infernal Chain Mail", itemType: protocol.ItemTypeChest,
		flavor: "Forged in a place where fire was the weather.",
		rules: []ruleCard{
			{event: evTakeDamage, when: []condition{{kind: condDamageType, s: protocol.DamageTypeFire}},
				then: effect{kind: effMulPct, n: 50}},
		},
	},
	{
		// Physical-family resist: sharp only. Blunt is deliberately NOT
		// covered — a single card that answered both physical types would be
		// strictly better than either elemental resist, since almost every
		// early monster is sharp or blunt.
		id: idWardedGambeson, name: "Warded Gambeson", itemType: protocol.ItemTypeChest,
		flavor: "Layered linen, quilted thick. Blades slide; hammers do not care.",
		rules: []ruleCard{
			{event: evTakeDamage, when: []condition{{kind: condDamageType, s: protocol.DamageTypeSharp}},
				then: effect{kind: effMulPct, n: 50}},
		},
	},
	{
		// Metaphysical-family resist: chaos only, the ghoul-tier answer.
		id: idPilgrimsMantle, name: "Pilgrim's Mantle", itemType: protocol.ItemTypeChest,
		flavor: "Worn thin by a road no map admits to.",
		rules: []ruleCard{
			{event: evTakeDamage, when: []condition{{kind: condDamageType, s: protocol.DamageTypeChaos}},
				then: effect{kind: effMulPct, n: 50}},
		},
	},
	{
		// Ice had no representative at all (#92's assignment table). Damage
		// sits at the shipped 1H melee anchor (4), so the type is the whole
		// point of the weapon rather than a stat upgrade riding along.
		id: idFrostbrand, name: "Frostbrand", itemType: protocol.ItemTypeWeapon,
		tags:       []string{protocol.WeaponTagMelee},
		damageType: protocol.DamageTypeIce,
		damage:     4,
		flavor:     "The scabbard frosts over between fights.",
	},
	{
		// Holy had exactly one representative, the dragon-only Wyrmslayer —
		// so the type was unreachable for most of the game. A 1H blunt-tier
		// mace at the same anchor makes it obtainable.
		id: idConsecratedMace, name: "Consecrated Mace", itemType: protocol.ItemTypeWeapon,
		tags:       []string{protocol.WeaponTagMelee},
		damageType: protocol.DamageTypeHoly,
		damage:     4,
		flavor:     "Every dent in it was somebody's bad night.",
	},

	// Content-expansion weapons & gear (#267): fills the gaps the shipped
	// arsenal left — Fire was magic-only, players had no heavy 2H blunt,
	// ranged was two bows at the same reach, and the gloves and amulet slots
	// (and Blunt/Ice resist) were empty. Every one is existing-vocabulary
	// content: a typed weapon (type is the point, no card), or a single-type
	// resist card on worn kit. Damage anchors follow the keystone's
	// "1H ≈ ½ 2H" (1H melee 4, 2H 9, bow ~3).
	{
		// Takes Fire off the mage's exclusive list so a fighter/rogue can
		// answer a fire-vulnerable troll (or the new Frost Wisp). Type is the
		// whole point — no card, same design as Frostbrand/Consecrated Mace.
		id: idEmberBrand, name: "Ember Brand", itemType: protocol.ItemTypeWeapon,
		tags:       []string{protocol.WeaponTagMelee},
		damageType: protocol.DamageTypeFire,
		damage:     4,
		flavor:     "The blade keeps its own warmth, even sheathed in a snowbank.",
	},
	{
		// The players' first heavy 2H blunt (Maul is monsterOnly; the
		// Warhammer is 1H). Damage at the 2H anchor; the cost is the 2H lock
		// (no shield, no dual-wield). Blunt's heavy hitter — the answer to the
		// sharp-resistant Skeleton.
		id: idIronheadGreatmaul, name: "Ironhead Greatmaul", itemType: protocol.ItemTypeWeapon,
		tags: []string{protocol.WeaponTagMelee}, twoHanded: true,
		damageType: protocol.DamageTypeBlunt,
		damage:     9,
		flavor:     "Two hands, one purpose, and nothing left standing after.",
	},
	{
		// The reach-for-damage tradeoff: +2 hexes over the Shortbow bought
		// with −1 damage. rangeHex is the max the reach invariant allows
		// (rangeHex+aoeRadius ≤ CombatRadius, validateMaxReach) — a decision
		// on a hex grid, not a strict upgrade.
		id: idLongbow, name: "Longbow", itemType: protocol.ItemTypeWeapon,
		tags:       []string{protocol.WeaponTagRanged},
		damageType: protocol.DamageTypeSharp,
		damage:     3, rangeHex: 6,
		flavor: "Draws slow, reaches far. Patience is the whole weapon.",
	},
	{
		// First content in the gloves slot, and the Blunt mirror of the Warded
		// Gambeson's Sharp resist — a single type halved, so situational, never
		// the "both physical" card the design refuses. Renders as
		// "+50% Blunt Resistance".
		id: idIronboundGauntlets, name: "Ironbound Gauntlets", itemType: protocol.ItemTypeGloves,
		flavor: "Iron over the knuckles. A maul lands like a friendly handshake.",
		rules: []ruleCard{
			{event: evTakeDamage, when: []condition{{kind: condDamageType, s: protocol.DamageTypeBlunt}},
				then: effect{kind: effMulPct, n: 50}},
		},
	},
	{
		// First content in the amulet slot, and the only resist for Ice — the
		// one type with no resist armor at all. Pairs directly with the Frost
		// Wisp. Renders as "+50% Ice Resistance".
		id: idFrostwardCharm, name: "Frostward Charm", itemType: protocol.ItemTypeAmulet,
		flavor: "Cold to the touch, and everything colder just slides off you.",
		rules: []ruleCard{
			{event: evTakeDamage, when: []condition{{kind: condDamageType, s: protocol.DamageTypeIce}},
				then: effect{kind: effMulPct, n: 50}},
		},
	},

	// Content-expansion consumables (#268): the heal ladder, extending the
	// shipped Healing Potion (5). heal is applied by the drink action
	// (inventory.go), clamped to maxHP; each stacks to protocol.ItemStackCap.
	{
		// Below the Healing Potion: the cheap bandage you top up with
		// mid-explore. Common, home/rat-tier.
		id: idMinorSalve, name: "Minor Salve", itemType: protocol.ItemTypeConsumable,
		heal:   3,
		flavor: "Boiled root and candle grease. It works, mostly.",
	},
	{
		// Above the Healing Potion: a real "save it for the boss" heal,
		// frontier-tier — meaningful because death drops you to the start of
		// your XP level.
		id: idGreaterDraught, name: "Greater Draught", itemType: protocol.ItemTypeConsumable,
		heal:   10,
		flavor: "Thick as tar and twice as bitter. You feel it knit you back.",
	},
	{
		// The ladder's top rung: a very rare full heal (clamped to maxHP on
		// drink). Flagged optional in the proposal for the flat power curve,
		// so it is gated behind the dragon's rare pool (#269) — a once-a-run
		// relief, not a staple.
		id: idFullRestorative, name: "Full Restorative", itemType: protocol.ItemTypeConsumable,
		heal:   999,
		flavor: "One swallow and the road behind you might as well not have happened.",
	},

	// Timed-effect foundation proof weapons (#271, slice 1). Both carry an
	// onHit rider (effects.go) instead of a rule card: the effect they apply is
	// a lingering, turn-counted modifier, not an instant fold. The Bloodrage
	// Cleaver is the player-side proof (a self-buff-on-hit — pure ARPG "rage
	// stacks", refresh-not-stack per the design); the Venom Sting is the
	// Serpent's monsterOnly bite (a DoT on the victim). Full buff-potion / DoT
	// content is a later #271 slice — these two exist to prove the mechanism.
	{
		// A rage weapon: each landed hit refreshes a short "+damage for a couple
		// turns" self-buff, so sustained aggression hits harder than opening.
		// Damage at the 1H melee anchor (4) — the buff is the point, not raw
		// stats. Drops off the Serpent (its own table, below).
		id: idBloodrageCleaver, name: "Bloodrage Cleaver", itemType: protocol.ItemTypeWeapon,
		tags:       []string{protocol.WeaponTagMelee},
		damageType: protocol.DamageTypeSharp,
		damage:     4,
		flavor:     "The longer the fight, the better it likes you.",
		//nolint:mnd // authored content: +15% deal-damage buff for 2 turns, refreshed on hit.
		onHit: []appliedEffect{
			{effectID: idEffectFrenzy, magnitude: percentBase + 15, turns: 2, toSelf: true},
		},
	},
	{
		// The Serpent's bite: a small poison that ticks each end-of-turn for a
		// few turns, refreshed on every hit — so staying adjacent to a serpent
		// bleeds you steadily even between its swings. monsterOnly, like every
		// natural weapon.
		id: idVenomSting, name: "Venom Sting", itemType: protocol.ItemTypeWeapon,
		tags:       []string{protocol.WeaponTagMelee},
		damageType: protocol.DamageTypeSharp,
		damage:     2, monsterOnly: true,
		//nolint:mnd // authored content: -2 HP/turn (a negative effAdd) for 3 turns, refreshed on hit.
		onHit: []appliedEffect{
			{effectID: idEffectPoison, magnitude: -2, turns: 3},
		},
	},

	// Timed-effect CONTENT (#271, slice 2) — appended LAST so no earlier
	// registry entry moves, mirroring the drop-table protocol.

	// The Hydra's bite (#271, slice 2): the live REGEN consumer. Its onHit
	// self-applies a regen effect (toSelf), so the Hydra heals +3 HP/turn for a
	// few turns after each bite — flat, fixed regen, NOT damage-proportional
	// lifesteal (that is a later #271 slice's new pipeline kind). monsterOnly,
	// like every natural weapon. Damage at the mid-tier melee anchor; the regen
	// is the gimmick, not the raw hit.
	{
		id: idHydraFangs, name: "Hydra Fangs", itemType: protocol.ItemTypeWeapon,
		tags:       []string{protocol.WeaponTagMelee},
		damageType: protocol.DamageTypeSharp,
		damage:     4, monsterOnly: true,
		//nolint:mnd // authored content: +3 HP/turn (a positive effAdd) for 3 turns, refreshed on hit.
		onHit: []appliedEffect{
			{effectID: idEffectRegen, magnitude: 3, turns: 3, toSelf: true},
		},
	},

	// Buff potions (#271, slice 2): consumables that apply a timed self-buff on
	// DRINK (appliesEffect — the drink counterpart of a weapon's onHit rider).
	// A drink is the player's whole turn inside a combat bubble, so a buff is a
	// deliberate tempo trade, not a free power spike. Both heal 0 — a pure buff,
	// exercising the "a consumable need not heal" payload path.
	{
		// A short, strong offensive buff: swing harder for a few turns after
		// drinking. frenzy is the deal-damage mulPct the Bloodrage Cleaver also
		// applies — the potion is a second, drink-triggered source of it.
		id: idDraughtOfFury, name: "Draught of Fury", itemType: protocol.ItemTypeConsumable,
		flavor: "Bottled aggression. It does not ask what you intend to hit.",
		//nolint:mnd // authored content: +25% deal-damage (frenzy) for 4 turns on drink.
		appliesEffect: []appliedEffect{
			{effectID: idEffectFrenzy, magnitude: percentBase + 25, turns: 4},
		},
	},
	{
		// The defensive mirror: take less damage for a few turns. ward is a
		// take-damage mulPct (percentBase-25 => −25% damage taken), folded
		// victim-side, so it protects against the incoming hit the turn it is
		// drunk — a real defensive tempo play (drinking is the whole turn).
		id: idWardingTonic, name: "Warding Tonic", itemType: protocol.ItemTypeConsumable,
		flavor: "A skin of bitter draught that turns the next blow aside.",
		//nolint:mnd // authored content: +25% damage resistance (ward) for 4 turns on drink.
		appliesEffect: []appliedEffect{
			{effectID: idEffectWard, magnitude: percentBase - 25, turns: 4},
		},
	},
	{
		// Antivenom (#271, slice 2): the cleanse. On drink it strips every
		// HARMFUL timed effect (the Serpent's poison) and nothing else — your
		// own buffs survive (clearHarmfulEffectsLocked; the "harmful only"
		// decision is documented there and in design-decisions.md). It carries
		// no heal: its whole value is the cure.
		id: idAntivenom, name: "Antivenom", itemType: protocol.ItemTypeConsumable,
		flavor:          "Milked from the very fangs that make it necessary.",
		cleansesHarmful: true,
	},

	// Offensive-gear slice (#271, on the timed-effect foundation): the lifesteal
	// weapon and the first offensive jewelry.
	{
		// The lifesteal proof consumer (effLifesteal): every hit heals the
		// wielder for 25% of the damage it deals (rollDamageLocked reads the
		// deal-damage trace; the heal is clamped to max HP and lands with the
		// turn's damage). Damage at the 1H melee anchor (4) — the leech is the
		// point, not raw stats — so sustained fighting outlasts a plain blade
		// without out-damaging one. Drops off the Wraith (a life-draining elite,
		// its own table below). A deal-damage card, hence weapon-legal by
		// validateItemNature.
		id: idVampiricBlade, name: "Vampiric Blade", itemType: protocol.ItemTypeWeapon,
		tags:       []string{protocol.WeaponTagMelee},
		damageType: protocol.DamageTypeSharp,
		damage:     4,
		flavor:     "It drinks first, and asks nothing in return.",
		rules: []ruleCard{
			{event: evDealDamage, then: effect{kind: effLifesteal, n: 25}},
		},
	},
	{
		// The first OFFENSIVE jewelry (#271): a per-hit crit% ring — the elf-crit
		// card pattern (elfCards) applied to a ring instead of a weapon, made
		// legal by the deliberate validateItemNature relaxation for jewelry
		// (rings/amulets). Its crit applies to EVERY attack the wearer lands
		// (attackerGearCards folds it into every hit's deal-damage roll), main-
		// hand, off-hand, or ranged — the point of an affix ring over a single
		// crit weapon. Renders as "10% chance ×2 Damage". Drops off the Ghoul
		// (the assassin/precision tier that already carries the Misericorde).
		id: idRingOfPrecision, name: "Ring of Precision", itemType: protocol.ItemTypeRing,
		flavor: "A steadying weight on the hand that already knows where to strike.",
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
		maxHP: 4, weapon: idClaws, xp: 8, aggroRadius: protocol.CombatRadius + 1, dropChance: 10,
		drops: []drop{
			{defID: idButchersCleaver, weight: 1},
			// Low-weight potion (inventory-slots task 3): recovery layer 2.
			{defID: idHealingPotion, weight: 1},
			// Wooden Buckler (#90): appended LAST so every earlier entry keeps
			// its cumulative-weight position. Rare here (weight 1) — the wolf
			// table is its common source.
			{defID: idWoodenBuckler, weight: 1},
			// Padded Boots (#88): appended LAST so every earlier entry keeps
			// its cumulative-weight position. Rare here (weight 1) — the wolf
			// table is the common source, so the first pair of boots reads as
			// a wolf-country find.
			{defID: idPaddedBoots, weight: 1},
			// Content expansion (#269 table B): the Minor Salve rides the
			// rat table as the low-tier recovery ladder's home rung. Appended
			// LAST so every earlier entry keeps its cumulative-weight position.
			{defID: idMinorSalve, weight: 2},
		},
		rings: []int{0, 1},
	},
	{
		id: idKindWolf, name: "Wolf",
		maxHP: 10, weapon: idFangs, xp: 20, aggroRadius: protocol.MonsterAggroRadius, dropChance: 30,
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
			// Wooden Buckler (#90): appended LAST so every earlier entry keeps
			// its cumulative-weight position (killDropSeed/killMissSeed
			// re-derived if the new total weight moves them).
			{defID: idWoodenBuckler, weight: 4},
			// Padded Boots (#88): appended LAST for the same reason — common
			// here (weight 4), so noticeability gear is reachable early
			// (killDropSeed/killMissSeed re-derived if the new total weight
			// moves them).
			{defID: idPaddedBoots, weight: 4},
			// Damage-type wave (#92): appended LAST for the same reason. The
			// Warded Gambeson (sharp resist) belongs on the sharp-clawed
			// kind a player meets first.
			{defID: idWardedGambeson, weight: 3},
			// Content expansion (#269 table B): the Longbow (the shooter's
			// kind carries the reach upgrade), a rare Frostward Charm (the
			// Frost Wisp is its common source), and the Minor Salve recovery
			// rung. Appended LAST so every earlier entry keeps its
			// cumulative-weight position — but the new total weight DOES move
			// drops_test.go's killDropSeed pick, which is re-derived there
			// (the drop def, not the seed: seed 0 stays a hit, seed 3 a miss).
			{defID: idLongbow, weight: 3},
			{defID: idFrostwardCharm, weight: 1},
			{defID: idMinorSalve, weight: 2},
		},
		rings: []int{1},
	},
	{
		id: idKindGhoul, name: "Ghoul",
		maxHP: 16, weapon: idTalons, xp: 35, aggroRadius: 8, dropChance: 35,
		// Opposition as an AUTHORING CONVENTION (#92), not machinery: a
		// Chaos-aligned monster is written with a Holy vulnerability, and
		// nothing in the engine knows the two are paired. +50% from Holy —
		// the Wyrmslayer Greatsword was forged for exactly this.
		rules: []ruleCard{
			{event: evTakeDamage, when: []condition{{kind: condDamageType, s: protocol.DamageTypeHoly}},
				then: effect{kind: effMulPct, n: percentBase + 50}},
		},
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
			// Damage-type wave (#92): appended LAST so every earlier entry
			// keeps its cumulative-weight position. The Pilgrim's Mantle
			// (chaos resist) and the Consecrated Mace answer this kind
			// specifically — the ghoul is where a player first WANTS a type.
			{defID: idPilgrimsMantle, weight: 3},
			{defID: idConsecratedMace, weight: 3},
			// Offensive jewelry (#271): the Ring of Precision (a crit% ring) on
			// the ghoul, the same assassin/precision tier that carries the
			// Misericorde. Appended LAST so every earlier entry keeps its
			// cumulative-weight position (this kind is not seed-pinned by
			// drops_test.go — only wolf is).
			{defID: idRingOfPrecision, weight: 2},
		},
		rings: []int{1, 2},
	},
	{
		id: idKindTroll, name: "Troll",
		maxHP: 30, weapon: idMaul, xp: 60, aggroRadius: 8, dropChance: 50,
		// "Trolls fear fire" — the identity the whole damage-type arc was
		// pitched on (#92). +50% from Fire.
		rules: []ruleCard{
			{event: evTakeDamage, when: []condition{{kind: condDamageType, s: protocol.DamageTypeFire}},
				then: effect{kind: effMulPct, n: percentBase + 50}},
		},
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
			// Iron Kite Shield (#90): appended LAST so every earlier entry
			// keeps its cumulative-weight position — frontier loot, common on
			// the troll.
			{defID: idIronKiteShield, weight: 4},
			// Iron Plate Armor (#88): appended LAST so every earlier entry
			// keeps its cumulative-weight position — frontier loot, common on
			// the troll, the tier where taking 2 less per hit is worth being
			// noticed sooner.
			{defID: idIronPlateArmor, weight: 4},
			// Damage-type wave (#92): appended LAST. The Frostbrand is the
			// game's only Ice weapon, frontier-tier loot.
			{defID: idFrostbrand, weight: 3},
			// Content expansion (#269 table B): the troll is the frontier
			// heavy-weapon tier — it carries the martial fire Ember Brand,
			// the 2H blunt Ironhead Greatmaul, the blunt-resist Ironbound
			// Gauntlets (the blunt attacker drops its own counter), and the
			// Greater Draught's save-for-the-boss heal. Appended LAST so every
			// earlier entry keeps its cumulative-weight position.
			{defID: idEmberBrand, weight: 3},
			{defID: idIronheadGreatmaul, weight: 3},
			{defID: idIronboundGauntlets, weight: 3},
			{defID: idGreaterDraught, weight: 1},
		},
		rings: []int{2},
	},
	{
		// The first monster that attacks WITHOUT closing (#179). It exists to
		// prove the point of that change: reach is now a property of the
		// weapon a kind names, so this kind is a content row, not an engine
		// change.
		//
		// Calibration (approved 2026-07-19): fragile relative to a ghoul (12
		// vs 16 HP) and the wolf's damage exactly — reach is the upgrade,
		// damage is not. Its bow reaches 3, under the player Shortbow's 4, so
		// player gear still out-ranges it. XP above a wolf's because closing
		// the distance is the cost.
		//
		// It shoots at point-blank rather than backing off: everything moves
		// one hex per turn, so a kiting monster would simply be uncatchable.
		id: idKindArcher, name: "Kin Archer",
		maxHP: 12, weapon: idHunterBow, xp: 30, aggroRadius: 8, dropChance: 30,
		// The starter set with the pack bow weighted up — a shooter's
		// signature drop. Appended in the same order as every other kind's so
		// the seeded drop pins keep their cumulative positions.
		drops: []drop{
			{defID: idButchersCleaver, weight: 4},
			{defID: idPackBow, weight: 8},
			{defID: idVenomFang, weight: 4},
			{defID: idHealingPotion, weight: 1},
			// Content expansion (#269 table B): the shooter's kind is the
			// common source of the reach-for-damage Longbow. Appended LAST.
			{defID: idLongbow, weight: 4},
		},
		rings: []int{1, 2},
	},
	{
		id: idKindDragon, name: "Dragon",
		maxHP: 60, weapon: idDragonJaws, xp: 150, aggroRadius: 12, dropChance: 100,
		// The Wyrmslayer Greatsword (weight 2 — the headline drop, roughly as
		// likely as the whole rest of the rare pool combined) plus a small
		// rare pool.
		drops: []drop{
			{defID: idWyrmslayerGreatsword, weight: 2},
			{defID: idIronWarhammer, weight: 1},
			{defID: idWarMageStaff, weight: 1},
			// Iron Kite Shield (#90): appended LAST so every earlier entry
			// keeps its cumulative-weight position — rare here (weight 1),
			// the troll table is its common source.
			{defID: idIronKiteShield, weight: 1},
			// Iron Plate Armor (#88): appended LAST — rare here (weight 1),
			// the troll table is its common source.
			{defID: idIronPlateArmor, weight: 1},
			// Damage-type wave (#92): appended LAST. Infernal Chain Mail —
			// fire resistance, dropped by the one kind whose claws are fire.
			{defID: idInfernalChainMail, weight: 2},
			// Content expansion (#269 table B + a judgement call): the Ember
			// Brand rides the fire-breathing dragon as rare frontier loot (its
			// common source is the troll). The Full Restorative — the heal
			// ladder's optional top rung — had no drop routing in #269, so it
			// is placed here as a very-rare once-a-run relief behind the
			// rarest encounter. Appended LAST.
			{defID: idEmberBrand, weight: 1},
			{defID: idFullRestorative, weight: 1},
		},
		rings: []int{2}, // rare: capped at protocol.DragonCount per world by the ring spawner (6c Task 3)
	},

	// Content-expansion kinds (#266): the bestiary's first RESISTANCE enemies
	// (before this, every monster card was a vulnerability — nothing ever
	// resisted a type, so a player only had to avoid the wrong armor, never
	// bring the right weapon), an ice attacker (Ice landed on nobody), a
	// second home-ring face, and a frontier elite bridging Troll (30) and
	// Dragon (60). Appended LAST in registry order — the spawn kind-pick
	// draws from an id-sorted per-ring list (kindsPerRing), so registry order
	// is not seed-bearing. Each names a monsterOnly natural weapon and owns
	// its loot table (#269 table A: its signature "answer" item weighted up,
	// so killing it teaches the counter it enables).
	{
		// A weak trash mob for the sanctuary approaches — the Rat's only
		// ring-0 company. Sharp, so the early game stays physical. No cards.
		id: idKindGoblin, name: "Goblin",
		maxHP: 6, weapon: idRustyShiv, xp: 12, aggroRadius: protocol.CombatRadius + 1, dropChance: 15,
		drops: []drop{
			{defID: idButchersCleaver, weight: 2},
			{defID: idMinorSalve, weight: 2},
			{defID: idHealingPotion, weight: 1},
		},
		rings: []int{0, 1},
	},
	{
		// The first kind that RESISTS a type: blades glance off bone (sharp
		// ×0.5), so most of the (mostly-Sharp) arsenal underperforms and the
		// 2H blunt Greatmaul becomes the answer. Its own claws are blunt.
		id: idKindSkeleton, name: "Skeleton",
		maxHP: 14, weapon: idBoneClub, xp: 30, aggroRadius: 8, dropChance: 35,
		rules: []ruleCard{
			{event: evTakeDamage, when: []condition{{kind: condDamageType, s: protocol.DamageTypeSharp}},
				then: effect{kind: effMulPct, n: 50}},
		},
		drops: []drop{
			{defID: idIronheadGreatmaul, weight: 3}, // the blunt it's weak to
			{defID: idIronboundGauntlets, weight: 2},
			{defID: idIronWarhammer, weight: 2},
		},
		rings: []int{1, 2},
	},
	{
		// The missing ice attacker (Frost Touch, ice), Fire-vulnerable — the
		// ice mirror of "trolls fear fire". Makes Ice land on the player,
		// gives the Frostward Charm a reason to exist, and rewards the fire
		// arsenal.
		id: idKindFrostWisp, name: "Frost Wisp",
		maxHP: 14, weapon: idFrostTouch, xp: 32, aggroRadius: 8, dropChance: 35,
		rules: []ruleCard{
			{event: evTakeDamage, when: []condition{{kind: condDamageType, s: protocol.DamageTypeFire}},
				then: effect{kind: effMulPct, n: percentBase + 50}},
		},
		drops: []drop{
			{defID: idFrostwardCharm, weight: 3}, // ice answers
			{defID: idFrostbrand, weight: 3},
			{defID: idMinorSalve, weight: 2},
		},
		rings: []int{1, 2},
	},
	{
		// A frontier elite between Troll (30) and Dragon (60): mundane
		// physical weapons barely bite (sharp ×0.5, blunt ×0.5) but it takes
		// +50% from Holy — the first enemy that DEMANDS elemental or Holy
		// damage, where your Sharp/Blunt kit is the wrong tool. Two
		// physical-resist cards on a MONSTER make it harder by design (the
		// "don't stack a both-physical resist" caution is about player armor).
		// Chaos-aligned like the ghoul, one tier up (reuses idTalons).
		id: idKindWraith, name: "Wraith",
		maxHP: 26, weapon: idTalons, xp: 70, aggroRadius: 8, dropChance: 45,
		rules: []ruleCard{
			{event: evTakeDamage, when: []condition{{kind: condDamageType, s: protocol.DamageTypeSharp}},
				then: effect{kind: effMulPct, n: 50}},
			{event: evTakeDamage, when: []condition{{kind: condDamageType, s: protocol.DamageTypeBlunt}},
				then: effect{kind: effMulPct, n: 50}},
			{event: evTakeDamage, when: []condition{{kind: condDamageType, s: protocol.DamageTypeHoly}},
				then: effect{kind: effMulPct, n: percentBase + 50}},
		},
		drops: []drop{
			{defID: idConsecratedMace, weight: 3}, // the Holy it's weak to
			{defID: idPilgrimsMantle, weight: 3},
			{defID: idGreaterDraught, weight: 1},
			// Offensive-gear slice (#271): the Vampiric Blade (a lifesteal
			// weapon) on the life-draining wraith — thematic and frontier-elite
			// tier. Appended LAST so every earlier entry keeps its
			// cumulative-weight position (this kind is not seed-pinned).
			{defID: idVampiricBlade, weight: 2},
		},
		rings: []int{2},
	},

	// Timed-effect foundation proof enemy (#271, slice 1): the first monster
	// whose attack applies a LINGERING effect rather than only instant damage.
	// Its bite (idVenomSting) poisons the victim — a small HP drain each
	// end-of-turn for a few turns, refreshed on every hit — so it teaches the
	// DoT foundation in play. Calibrated near the Goblin/Rat home tier: weak in
	// raw HP and hit, the poison is the whole threat. Drops the Bloodrage
	// Cleaver (the buff-weapon proof) so both halves of the mechanism are
	// reachable from one encounter. Its own table is NEW, so no existing seeded
	// drop pin moves.
	{
		id: idKindSerpent, name: "Serpent",
		maxHP: 8, weapon: idVenomSting, xp: 16, aggroRadius: 8, dropChance: 30,
		drops: []drop{
			{defID: idBloodrageCleaver, weight: 2},
			{defID: idMinorSalve, weight: 2},
			// Antivenom (#271, slice 2): the poison monster drops its own cure —
			// killing a Serpent teaches the counter to its bite. Appended LAST so
			// every earlier entry keeps its cumulative-weight position (the Serpent
			// table is not seed-pinned, but the protocol holds regardless).
			{defID: idAntivenom, weight: 2},
		},
		rings: []int{1},
	},

	// Timed-effect foundation proof REGEN enemy (#271, slice 2): the live
	// evEndOfTurn heal consumer. Its bite (idHydraFangs) self-applies a regen
	// effect, so the Hydra knits itself back together as it fights — a frontier
	// gimmick that punishes low, drawn-out damage and rewards bursting it down.
	// A mid-frontier elite (below the Wraith's 26 HP and the Dragon's 60): the
	// regen, not raw stats, is the threat. Its own NEW table carries the buff
	// potions (Draught of Fury, Warding Tonic) plus a Greater Draught, so a
	// tough fight funds the tools that make the next one easier. New table, so
	// no existing seeded drop pin moves.
	{
		id: idKindHydra, name: "Hydra",
		maxHP: 24, weapon: idHydraFangs, xp: 55, aggroRadius: 8, dropChance: 45,
		drops: []drop{
			{defID: idDraughtOfFury, weight: 3},
			{defID: idWardingTonic, weight: 3},
			{defID: idGreaterDraught, weight: 1},
		},
		rings: []int{2},
	},

	// In-combat-spawn foundation proof pair (#271): the Necromancer is the
	// first SUMMONER — while bubbled it raises weak Risen adds on nearby free
	// hexes (summon.go's tickSummonsLocked, an end-of-turn hook), bounded by a
	// per-summoner living cap and a per-turn cooldown so it can never
	// runaway-spawn. A frontier elite (ring 2): its own melee (a bone club, the
	// skeleton's weapon) is modest — the swarm is the threat. The Risen is the
	// weak add it raises, and also a plain wild frontier trash mob so the kind
	// isn't summon-only. Both tables are NEW, so no existing seeded drop pin
	// moves. Appended LAST in registry order — the spawn kind-pick draws from an
	// id-sorted per-ring list (kindsPerRing), so registry order is not
	// seed-bearing.
	{
		// The weak add. Very low HP/hit/XP: it exists to pressure with numbers,
		// not damage, and to be cleared cheaply. Reuses idClaws (the rat's weak
		// sharp weapon, monsterOnly). Rare, tiny loot so a maintained swarm is
		// not an item fountain — the living cap + cooldown already bound the
		// spawn rate to a trickle.
		id: idKindRisen, name: "Risen",
		maxHP: 4, weapon: idClaws, xp: 5, aggroRadius: protocol.CombatRadius + 1, dropChance: 5,
		drops: []drop{
			{defID: idMinorSalve, weight: 1},
		},
		rings: []int{2},
	},
	{
		id: idKindNecromancer, name: "Necromancer",
		maxHP: 24, weapon: idBoneClub, xp: 65, aggroRadius: 8, dropChance: 45,
		// Raise one Risen every 3 in-combat turns, up to 3 alive at once. The
		// cap (maxLiving) is the runaway guard; everyTurns paces it; the wind-up
		// (summonCooldown starts full, newMonsterEntity) gives the player a
		// window before the first add. Pure data — the behavior is summon.go's
		// hook, not a combat-site edit.
		summon: &summonSpec{minionKind: idKindRisen, everyTurns: 3, maxLiving: 3, count: 1},
		drops: []drop{
			{defID: idGreaterDraught, weight: 1},
			{defID: idMinorSalve, weight: 2},
		},
		rings: []int{2},
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

	buildEffectIndex()
	buildMonsterIndex()
	buildSkillIndex()

	mustValidateContent()
}
