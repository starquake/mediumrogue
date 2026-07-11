package game

import (
	"slices"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// items.go: the gear system's types, per-entity helpers, and content
// validation. Spec: docs/superpowers/specs/2026-07-11-inventory-slots-design.md
// (task 1: taxonomy + entity storage model), superseding the flat two-slot
// (close/ranged) model from milestone 6b.4's
// docs/superpowers/specs/2026-07-10-m6b.4-gear-pipeline-design.md. The
// registry itself (itemDefs, and the class-default/drop tables built from
// it) lives in content.go, next to speciesCards — items.go holds the
// machinery, content.go holds the data.

// itemDef is one entry in the content registry: an item's fixed stats, its
// type (which determines its slot — slotForType), its wearability
// restriction, and the rule cards it feeds into the pipeline when equipped.
// Pure data (the design doc's §7 SQLite prerequisite) — mirrors ruleCard.
type itemDef struct {
	id, name, desc string
	// itemType is one of the protocol.ItemType* consts — the taxonomy's 12
	// types. It determines the item's slot (slotForType(itemType)): each type
	// fits exactly one slot, except consumable, which has no slot (backpack
	// stack only).
	itemType string
	// wearableBy is the set of classes that may equip/wear this item. Empty
	// means "any class" — the spec's default for armor/jewelry (the six
	// gearSlotTypes). A weapon-type item (one of the five weaponSlotTypes)
	// MUST declare an explicit, non-empty wearableBy (validateItemDefs
	// enforces this) — "any" would be meaningless for a weapon, since a
	// class's weapon slots are shape-restricted anyway (weaponSlotsFor).
	// Ignored for a consumable (no wearability gate — drink is not an equip).
	wearableBy []string
	damage     int
	rangeHex   int // 0 = melee/adjacent (weapon-type items only)
	aoeRadius  int // 0 = single target
	// heal is a consumable's HP restore on drink (clamped to maxHP);
	// validateItemDefs requires heal > 0 for a consumable and 0 for any gear
	// def — drinking is an action (task 2), not a combat pipeline event.
	heal  int
	rules []ruleCard
}

// itemInstance is one entity-owned copy of a def: a stable id (minted from
// the world's shared entity-id sequence — instance ids are unique across the
// whole world, entities and items alike, see grantDefaultsLocked) plus which
// def it is.
type itemInstance struct {
	id    int64
	defID string
}

// backpackEntry is one of an entity's protocol.BackpackSize backpack slots:
// either a single gear instance (count 1) or a consumable stack (count 1..
// protocol.ItemStackCap, identical defs merge, never split — see
// stackIndexFor). The zero value (inst.id == 0, count == 0) is a free entry.
type backpackEntry struct {
	inst  itemInstance
	count int
}

// empty reports whether this backpack entry holds nothing.
func (be backpackEntry) empty() bool { return be.count == 0 }

// fistsDef is the built-in close-slot fallback for an empty-handed player:
// not in the registry (no instance id, never owned, equipped, or dropped),
// just the profile closeDefFor returns when a player's melee-ish weapon slot
// (melee-weapon for fighter/rogue, staff for mage) is empty. A monster's
// equivalent is per-kind (monsterDef.claws, monsters.go) — built from that
// kind's own damage/rules, not a single shared fallback.
//
//nolint:gochecknoglobals // built-in fallback def, effectively const (never mutated).
var fistsDef = &itemDef{
	id: "fists", name: "Fists", itemType: protocol.ItemTypeMeleeWeapon, damage: protocol.FistsDamage,
}

// weaponSlotTypes is every item type that fills one of the five weapon-type
// slots (a class-shaped slot, per weaponSlotsFor) — used by isWeaponType and
// validateItemDefs.
//
//nolint:gochecknoglobals // fixed taxonomy table, effectively const.
var weaponSlotTypes = map[string]bool{
	protocol.ItemTypeMeleeWeapon:  true,
	protocol.ItemTypeThrownWeapon: true,
	protocol.ItemTypeRangedWeapon: true,
	protocol.ItemTypeStaff:        true,
	protocol.ItemTypeWand:         true,
}

// gearSlotTypes is every item type that fills one of the six universal
// (non-class-shaped) body slots.
//
//nolint:gochecknoglobals // fixed taxonomy table, effectively const.
var gearSlotTypes = map[string]bool{
	protocol.ItemTypeHead:   true,
	protocol.ItemTypeBody:   true,
	protocol.ItemTypeHands:  true,
	protocol.ItemTypeRing:   true,
	protocol.ItemTypeAmulet: true,
	protocol.ItemTypeFeet:   true,
}

// isWeaponType reports whether t is one of the five class-shaped weapon
// types.
func isWeaponType(t string) bool { return weaponSlotTypes[t] }

// isGearType reports whether t is one of the six universal body-slot types.
func isGearType(t string) bool { return gearSlotTypes[t] }

// validItemType reports whether t is one of the taxonomy's 12 known types.
func validItemType(t string) bool {
	return weaponSlotTypes[t] || gearSlotTypes[t] || t == protocol.ItemTypeConsumable
}

// slotForType returns the equip-slot key for item type t: t itself for
// every type except consumable, which has no slot (a consumable never
// equips — it lives in the backpack as a stack; empty string is never a
// valid map key produced by any equip path, so it also safely means "no
// slot" wherever slotForType's result is used as an equipped-map key).
func slotForType(t string) string {
	if t == protocol.ItemTypeConsumable {
		return ""
	}

	return t
}

// weaponSlotsFor returns the two item types that fill class's weapon slots:
// index 0 is the "close" analog (the type closeDefFor reads), index 1 is
// the "ranged" analog (the type rangedDefFor reads) — fighter = melee-weapon
// + thrown-weapon (thrown ships empty — no thrown content exists yet, so a
// fighter has no ranged attack), rogue = melee-weapon + ranged-weapon,
// mage = staff + wand (a staff can melee-bonk; a wand never melees). An
// unknown/empty class (a monster or malformed fixture) returns a zero pair,
// matching the old classDefaultIDs fallback comment.
func weaponSlotsFor(class string) [2]string {
	switch class {
	case protocol.ClassFighter:
		return [2]string{protocol.ItemTypeMeleeWeapon, protocol.ItemTypeThrownWeapon}
	case protocol.ClassRogue:
		return [2]string{protocol.ItemTypeMeleeWeapon, protocol.ItemTypeRangedWeapon}
	case protocol.ClassMage:
		return [2]string{protocol.ItemTypeStaff, protocol.ItemTypeWand}
	default:
		return [2]string{}
	}
}

// classHasWeaponSlot reports whether itemType is one of class's two
// class-shaped weapon slots (weaponSlotsFor).
func classHasWeaponSlot(class, itemType string) bool {
	slots := weaponSlotsFor(class)

	return itemType == slots[0] || itemType == slots[1]
}

// wearableByClass reports whether class may wear/wield def per its
// wearableBy set: empty means any class (the spec's armor/jewelry default).
func wearableByClass(def *itemDef, class string) bool {
	if len(def.wearableBy) == 0 {
		return true
	}

	return slices.Contains(def.wearableBy, class)
}

// canEquip reports whether class may equip def at all: wearability
// (wearableByClass) plus, for a weapon-type item, the class's own weapon-slot
// shape (classHasWeaponSlot) — a fighter can never equip a wand even if some
// future card marked it wearableBy-any, because "wand" is not one of the
// fighter's two weapon-slot types. A consumable is never equippable (it has
// no slot; drink, not equip, task 2, is its action) — canEquip always false
// for one, matching slotForType's "no slot" contract.
func canEquip(class string, def *itemDef) bool {
	if def.itemType == protocol.ItemTypeConsumable {
		return false
	}

	if !wearableByClass(def, class) {
		return false
	}

	if isWeaponType(def.itemType) {
		return classHasWeaponSlot(class, def.itemType)
	}

	return isGearType(def.itemType)
}

// canonicalSlotOrder is every non-consumable item type in a fixed order —
// used wherever equipped items must fold deterministically (equippedRuleCards
// below), since e.equipped is a map and Go map iteration order is
// unspecified.
//
//nolint:gochecknoglobals // fixed enumeration order, effectively const.
var canonicalSlotOrder = []string{
	protocol.ItemTypeMeleeWeapon, protocol.ItemTypeThrownWeapon, protocol.ItemTypeRangedWeapon,
	protocol.ItemTypeStaff, protocol.ItemTypeWand,
	protocol.ItemTypeHead, protocol.ItemTypeBody, protocol.ItemTypeHands,
	protocol.ItemTypeRing, protocol.ItemTypeAmulet, protocol.ItemTypeFeet,
}

// itemByID looks up an entity's owned item instance by id — across both its
// equipped slots and its backpack entries (an entity's items live in exactly
// one of those two places; there is no separate flat owned list anymore) —
// so an equip/unequip/drop intent can resolve its target without needing to
// know in advance where the item currently lives.
func (e *entity) itemByID(id int64) (itemInstance, bool) {
	for _, inst := range e.equipped {
		if inst.id == id {
			return inst, true
		}
	}

	for _, be := range e.backpack {
		if !be.empty() && be.inst.id == id {
			return be.inst, true
		}
	}

	return itemInstance{}, false
}

// equippedDefIn returns the def currently filling slot, or nil for an empty
// slot or a slot the entity has never equipped into (a nil/uninitialized
// equipped map reads as empty, so this is always safe to call on a
// zero-value entity, e.g. a monster, which never equips).
func (e *entity) equippedDefIn(slot string) *itemDef {
	inst, ok := e.equipped[slot]
	if !ok || inst.id == 0 {
		return nil
	}

	return itemDefByID[inst.defID]
}

// freeBackpackIndex returns the index of the first free (empty) backpack
// entry, or -1 if the backpack (protocol.BackpackSize entries) is full.
func (e *entity) freeBackpackIndex() int {
	for i, be := range e.backpack {
		if be.empty() {
			return i
		}
	}

	return -1
}

// findBackpackIndex returns the index of the backpack entry holding the
// gear instance with this id, or -1 if none (including if id names a
// consumable stack's CURRENT representative instance rather than the
// stack's own identity — see stackIndexFor for the stack-merge path).
func (e *entity) findBackpackIndex(instID int64) int {
	for i, be := range e.backpack {
		if !be.empty() && be.inst.id == instID {
			return i
		}
	}

	return -1
}

// stackIndexFor returns the index of a backpack entry that is a mergeable
// consumable stack of defID — same def, count below protocol.ItemStackCap —
// or -1 if none exists (a fresh stack needs a free entry instead;
// freeBackpackIndex). Stacks never split, so this is pickup's first
// priority (merge > free entry > reject, see the spec's pickup flow).
func (e *entity) stackIndexFor(defID string) int {
	if itemDefByID[defID].itemType != protocol.ItemTypeConsumable {
		return -1
	}

	for i, be := range e.backpack {
		if !be.empty() && be.inst.defID == defID && be.count < protocol.ItemStackCap {
			return i
		}
	}

	return -1
}

// equippedRuleCards returns the rule cards of every item currently equipped
// by e, in canonicalSlotOrder — deterministic regardless of e.equipped's map
// iteration order. Used by rollDamageLocked's victim-side fold: a hit lands
// on the whole entity, not just whichever slot happens to be attacking, so
// e.g. leather armor's take-damage card applies even though the hit came
// from a bump, not the armor "acting".
func equippedRuleCards(e *entity) []ruleCard {
	var cards []ruleCard

	for _, slot := range canonicalSlotOrder {
		if def := e.equippedDefIn(slot); def != nil {
			cards = append(cards, def.rules...)
		}
	}

	return cards
}

// toggleEquip is the shared swap primitive for both the free (outside-bubble)
// equip path and the queued (inside-bubble) pending-equip path
// (applyPendingEquip). inst is the item instance to equip, already known to
// be owned and class/wearability-valid (queueEquipLocked's job); slot is its
// itemType-derived slot key.
//
// If inst is ALREADY the slot's occupant, this is really an unequip: it
// moves inst back into a free backpack entry, clearing the slot — unless the
// backpack is full, in which case it's a no-op (the item stays equipped;
// task 2 gives unequip its own intent with a proper rejection error, but
// task 1 preserves the pre-existing "equip-intent-as-toggle" contract this
// entity model must remain internally consistent with: every owned item
// lives in equipped or backpack, never nowhere).
//
// Otherwise inst is currently a backpack entry: it is removed from there,
// equipped into slot, and whatever was previously equipped there (if
// anything) is swapped back into that now-free backpack index — the spec's
// "swaps the displaced item back into that entry". Callers must already
// hold e's entity (no locking here; the caller — world.go — holds w.mu).
func (e *entity) toggleEquip(inst itemInstance, slot string) {
	if e.equipped == nil {
		e.equipped = make(map[string]itemInstance)
	}

	if cur, ok := e.equipped[slot]; ok && cur.id == inst.id {
		idx := e.freeBackpackIndex()
		if idx < 0 {
			return // backpack full: can't unequip, leave it equipped (see doc comment)
		}

		e.backpack[idx] = backpackEntry{inst: inst, count: 1}
		delete(e.equipped, slot)

		return
	}

	idx := e.findBackpackIndex(inst.id)
	displaced, hadDisplaced := e.equipped[slot]
	e.equipped[slot] = inst

	if idx < 0 {
		// inst wasn't in the backpack (e.g. it was already equipped in a
		// DIFFERENT slot — not a real caller path, but harmless).
		return
	}

	if hadDisplaced && displaced.id != 0 {
		e.backpack[idx] = backpackEntry{inst: displaced, count: 1}
	} else {
		e.backpack[idx] = backpackEntry{}
	}
}

// closeDefFor is the def an entity bumps with: its equipped melee-ish
// weapon-slot item (weaponSlotsFor(e.class)[0] — melee-weapon for
// fighter/rogue, staff for mage), or fists for an empty slot (a bare/unarmed
// player), or its kind's claws profile for a monster (which owns no items
// and never equips — checked first, so a monster never falls through to the
// fists case). Panics if a monster entity's monsterKind names no registered
// kind — every production spawn path sets a real one; this only guards a
// malformed fixture.
func closeDefFor(e *entity) *itemDef {
	if e.kind == protocol.EntityMonster {
		k := kindOf(e)
		if k == nil {
			panic("game: closeDefFor monster entity has no registered kind")
		}

		return k.claws
	}

	slots := weaponSlotsFor(e.class)
	if def := e.equippedDefIn(slots[0]); def != nil {
		return def
	}

	return fistsDef
}

// rangedDefFor is the def an entity shoots with: its equipped ranged-ish
// weapon-slot item (weaponSlotsFor(e.class)[1] — thrown-weapon for fighter,
// ranged-weapon for rogue, wand for mage), or nil if that slot is empty — no
// ranged weapon at all. A fighter's thrown-weapon slot has no registered
// content yet (the spec's "fighter thrown slot ships empty"), so this always
// returns nil for a fighter today — exactly the pre-inventory-slots
// contract (a fighter has no ranged attack), reproduced via an always-empty
// slot instead of a hardcoded class check.
func rangedDefFor(e *entity) *itemDef {
	slots := weaponSlotsFor(e.class)

	return e.equippedDefIn(slots[1])
}

// itemDamage is the single source of truth for an item's damage at a given
// level: the def's base plus DamagePerLevel for each level above 1. Used by
// both the melee and ranged combat paths.
func itemDamage(def *itemDef, level int) int {
	return def.damage + protocol.DamagePerLevel*(level-1)
}

// Class-default item ids: shared between the registry (content.go), the
// class → starting-items mapping below, and their pinning tests, named so a
// typo is a compile error instead of a silent registry-lookup miss (and so
// goconst does not flag the same literal repeated across all three).
const (
	idIronSword  = "iron-sword"
	idDagger     = "dagger"
	idShortbow   = "shortbow"
	idOakStaff   = "oak-staff"
	idEmberFocus = "ember-focus"
)

// Starter-drop-set item ids: named the same way as the class-default ids
// above, and for the same reason — referenced from both the item registry
// (content.go) and, since 6c, per-kind monster loot tables (also
// content.go) and their pinning tests, so a typo is a compile error.
const (
	idButchersCleaver       = "butchers-cleaver"
	idIronWarhammer         = "iron-warhammer"
	idVenomFang             = "venom-fang"
	idPackBow               = "pack-bow"
	idEmberStaff            = "ember-staff"
	idAncientDwarvenMattock = "ancient-dwarven-mattock"
	idWarMageStaff          = "war-mage-staff"
	// idWyrmslayerGreatsword is the dragon-only drop (6c) — named here so
	// content.go's item literal and monsterDefs' dragon drop table (both
	// content.go) and the pinning test can't drift on a typo.
	idWyrmslayerGreatsword = "wyrmslayer-greatsword"
)

// classDefaultIDs returns the item def ids a class starts with at Join: one
// close-ish weapon, plus a ranged-ish weapon for Rogue and Mage (Fighter has
// none — its thrown-weapon slot ships empty). An empty or unknown class
// returns nil — Join's validClass check means this only guards non-player
// entities and test fixtures, mirroring class.go's baseMaxHP fallback
// comment.
func classDefaultIDs(class string) []string {
	switch class {
	case protocol.ClassFighter:
		return []string{idIronSword}
	case protocol.ClassRogue:
		return []string{idDagger, idShortbow}
	case protocol.ClassMage:
		return []string{idOakStaff, idEmberFocus}
	default:
		return nil
	}
}

// grantDefaultsLocked creates, owns, and equips a fresh player's class-default
// items: one itemInstance per classDefaultIDs id, instance ids minted from
// the world's shared id sequence, equipped straight into the def's
// itemType-derived slot (no bare "owned but unequipped" starting state — the
// backpack starts with all protocol.BackpackSize entries free). Callers hold
// w.mu and must call this after the entity is stored in w.entities —
// instance ids share nextID with entity ids, so ordering only matters for id
// uniqueness, not entity lookup.
func (w *World) grantDefaultsLocked(e *entity) {
	if e.equipped == nil {
		e.equipped = make(map[string]itemInstance)
	}

	for _, defID := range classDefaultIDs(e.class) {
		def, ok := itemDefByID[defID]
		if !ok {
			// mustValidateContent (run once at package init, before any World
			// exists) already guarantees every classDefaultIDs id is registered,
			// so this is unreachable under the current registry — panic rather
			// than silently joining a player short a starting item, matching this
			// file's fail-loud-on-a-content-bug philosophy (validateItemDefs et
			// al.) instead of degrading quietly if that guarantee is ever broken
			// (e.g. a future class added to classDefaultIDs but not to
			// mustValidateContent's class loop).
			panic("game: class default " + defID + " is not a registered item")
		}

		w.nextID++

		inst := itemInstance{id: w.nextID, defID: defID}
		e.equipped[def.itemType] = inst
	}
}

// validateItemDefs panics on a content bug in defs: a duplicate id, an
// unknown item type, an unknown wearableBy class, a weapon-type item with no
// (or an impossible) wearableBy, a heal value on the wrong kind of def, or a
// rule card referencing an unknown event/condition/effect kind — so bad
// content data fails at load, not mid-combat. Split out from
// mustValidateContent so tests can exercise the failure paths on a small
// synthetic def set instead of only the real registry.
func validateItemDefs(defs []*itemDef) {
	seen := make(map[string]bool, len(defs))

	for _, def := range defs {
		if seen[def.id] {
			panic("game: duplicate item id " + def.id)
		}

		seen[def.id] = true

		validateItemType(def)
		validateItemHeal(def)
		validateRuleCards(def.id, def.rules)
	}
}

// validateItemType panics if def's itemType is unknown, or if a weapon-type
// def's wearableBy is empty or names a class with no matching weapon slot
// (classHasWeaponSlot) — a weapon no class could ever equip is a
// content-authoring mistake, not a runtime condition. Split out of
// validateItemDefs to keep its cognitive complexity under the linter's
// threshold.
func validateItemType(def *itemDef) {
	if !validItemType(def.itemType) {
		panic("game: item " + def.id + " has unknown item type " + def.itemType)
	}

	for _, c := range def.wearableBy {
		if !validClass(c) {
			panic("game: item " + def.id + " wearableBy names unknown class " + c)
		}
	}

	if !isWeaponType(def.itemType) {
		return
	}

	if len(def.wearableBy) == 0 {
		panic("game: weapon " + def.id + " must declare an explicit wearableBy (a weapon is never wearableBy-any)")
	}

	for _, c := range def.wearableBy {
		if !classHasWeaponSlot(c, def.itemType) {
			panic("game: weapon " + def.id + " wearableBy " + c + " has no " + def.itemType + " weapon slot")
		}
	}
}

// validateItemHeal panics if a consumable's heal is not positive, or if any
// non-consumable def sets heal at all — heal is a consumable def field
// (drink, task 2, is an action), never a combat pipeline event, so it must
// never leak onto gear.
func validateItemHeal(def *itemDef) {
	if def.itemType == protocol.ItemTypeConsumable {
		if def.heal <= 0 {
			panic("game: consumable " + def.id + " must have heal > 0")
		}

		return
	}

	if def.heal != 0 {
		panic("game: gear item " + def.id + " must not set heal (consumables only)")
	}
}

// validateRuleCards panics if any card in cards names an event, condition, or
// effect kind the pipeline (rules.go) does not know — owner names the
// item/context in the panic message. The three switches below must be kept in
// sync with rules.go's own event/condition/effect const blocks and
// conditionHolds/applyRules switches — see the cross-reference comment on
// rules.go's evDealDamage const block.
//
// It also rejects an evEarnXP card carrying a condChance condition: the
// kill-XP fold (resolveBubbleTurnLocked's award loop) calls applyRules with a
// bare ruleCtx{} — no rng — because earn-XP has never needed one before. A
// chance condition on such a card would nil-deref conditionHolds' ctx.rng the
// first time it actually rolled, mid-combat. Fail at load instead; lift this
// once earn-XP folds thread a real rng.
func validateRuleCards(owner string, cards []ruleCard) {
	for _, c := range cards {
		switch c.event {
		case evDealDamage, evTakeDamage, evEarnXP, evAggroRange:
		default:
			panic("game: " + owner + " rule card has unknown event " + c.event)
		}

		for _, cond := range c.when {
			validateRuleCondition(owner, c.event, cond)
		}

		switch c.then.kind {
		case effAdd, effMulPct:
		default:
			panic("game: " + owner + " rule card has unknown effect " + c.then.kind)
		}
	}
}

// validateRuleCondition validates one condition of one card (event is the
// owning card's event, for the earn-xp/chance cross-check) — split out of
// validateRuleCards to keep its cognitive complexity under the linter's
// threshold.
func validateRuleCondition(owner, event string, cond condition) {
	switch cond.kind {
	case condChance, condTargetHPBelowPct, condTargetHPBelowFlat,
		condTargetHPFull, condAllyInBubble, condTargetAdjacent:
	case condAttackerSpecies:
		// A species gate on a species that can't exist would silently
		// never hold — a content typo, caught at load.
		if !validSpecies(cond.s) {
			panic("game: " + owner + " attackerSpecies rule card names unknown species " + cond.s)
		}
	case condTargetKind:
		// A kind gate on an unregistered monster id would silently never
		// hold — a content typo, caught at load. monsterDefByID is already
		// built (buildMonsterIndex runs before mustValidateContent —
		// content.go's init) by the time any item's rule cards validate.
		if _, ok := monsterDefByID[cond.s]; !ok {
			panic("game: " + owner + " targetKind rule card names unknown monster kind " + cond.s)
		}
	default:
		panic("game: " + owner + " rule card has unknown condition " + cond.kind)
	}

	if event == evEarnXP && cond.kind == condChance {
		panic("game: " + owner + " earn-xp rule card has a chance condition (earn-xp folds run without rng)")
	}
}

// validateMaxReach panics if any def's rangeHex+aoeRadius exceeds
// protocol.CombatRadius — the invariant queueAttackLocked's doc comment
// depends on: any hex a ranged attack can reach must already be inside the
// shooter's combat bubble, or a monster could be ranged-killed in the WORLD
// domain (where resolveWorldTurnLocked awards no kill-XP). Enforced at
// content load so a future longer-reach item fails the build, not a fight.
func validateMaxReach(defs []*itemDef) {
	for _, def := range defs {
		if reach := def.rangeHex + def.aoeRadius; reach > protocol.CombatRadius {
			panic("game: item " + def.id + " reach exceeds CombatRadius")
		}
	}
}

// mustValidateContent validates the whole live registry: every def
// (validateItemDefs, validateMaxReach) plus every class default id
// (classDefaultIDs) actually naming a registered item that class can equip.
// Called once from content.go's init, so a content bug fails at process
// start.
func mustValidateContent() {
	validateItemDefs(itemDefs)
	validateMaxReach(itemDefs)
	validateMonsterDefs(monsterDefs)

	for _, class := range []string{protocol.ClassFighter, protocol.ClassRogue, protocol.ClassMage} {
		for _, id := range classDefaultIDs(class) {
			def, ok := itemDefByID[id]
			if !ok {
				panic("game: class default " + id + " for " + class + " is not a registered item")
			}

			if !canEquip(class, def) {
				panic("game: class default " + id + " for " + class + " is not equippable by that class")
			}
		}
	}
}
