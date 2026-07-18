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
// type (which determines its slot — slotForType/weaponTargetSlot), and the
// rule cards it feeds into the pipeline when equipped. Pure data (the design
// doc's §7 SQLite prerequisite) — mirrors ruleCard.
type itemDef struct {
	id, name, desc string
	// flavor is the card's authored lore ("Fantasy") line, shown as flavor
	// text in the inventory tooltip — separate from desc's mechanical effect
	// line, and never gameplay-affecting. Empty for items without lore.
	flavor string
	// itemType is one of the protocol.ItemType* consts — the taxonomy's 9
	// types (one weapon type plus consumable plus shield plus the six
	// armor/jewelry types). It determines the item's slot
	// (slotForType/weaponTargetSlot): each armor/jewelry type fits exactly
	// one slot, a shield fits the off-hand (#90), a weapon fits a hand
	// chosen at equip time, and a consumable has no slot (backpack stack
	// only).
	itemType string
	// tags (weapon-type items only) name which attacks fire this weapon:
	// protocol.WeaponTagMelee/Ranged/Magic. ≥1 tag required for a weapon,
	// none allowed on anything else (validateItemDefs).
	tags []string
	// twoHanded (weapons only): occupies main-hand AND locks off-hand.
	twoHanded bool
	damage    int
	rangeHex  int // 0 = melee/adjacent (weapon-type items only)
	aoeRadius int // 0 = single target
	// heal is a consumable's HP restore on drink (clamped to maxHP);
	// validateItemDefs requires heal > 0 for a consumable and 0 for any gear
	// def — drinking is an action (task 2), not a combat pipeline event.
	heal  int
	rules []ruleCard
}

// hasTag reports whether this def's weapon tags include tag (always false
// for a non-weapon def, whose tags are always empty).
func (d *itemDef) hasTag(tag string) bool { return slices.Contains(d.tags, tag) }

// isWeapon reports whether this def is the one weapon item type — anyone may
// equip it (gates dropped, #56); its tags determine which attacks fire it.
func (d *itemDef) isWeapon() bool { return d.itemType == protocol.ItemTypeWeapon }

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

// groundStack is one item lying on the map: a gear instance (count 1) or a
// whole consumable stack (count 1..protocol.ItemStackCap). Dropping a backpack
// stack lands it here WHOLE as one groundStack (it is not split into N
// instances); a monster loot drop is always count 1. The representative
// instance's id is the stable id a pickup intent names.
type groundStack struct {
	inst  itemInstance
	count int
}

// fistsDef is the built-in main-hand fallback for an empty-handed player: not
// in the registry (no instance id, never owned, equipped, or dropped), just
// the profile closeDefFor returns when an entity holds no melee-tagged
// weapon. A monster's equivalent is per-kind (monsterDef.claws, monsters.go)
// — built from that kind's own damage/rules, not a single shared fallback.
//
//nolint:gochecknoglobals // built-in fallback def, effectively const (never mutated).
var fistsDef = &itemDef{
	id: "fists", name: "Fists", itemType: protocol.ItemTypeWeapon,
	tags: []string{protocol.WeaponTagMelee}, damage: protocol.FistsDamage,
}

// validItemType reports whether t is one of the taxonomy's 9 known types.
func validItemType(t string) bool {
	switch t {
	case protocol.ItemTypeWeapon, protocol.ItemTypeConsumable,
		protocol.ItemTypeHelmet, protocol.ItemTypeChest, protocol.ItemTypeGloves,
		protocol.ItemTypeBoots, protocol.ItemTypeRing, protocol.ItemTypeAmulet,
		protocol.ItemTypeShield:
		return true
	default:
		return false
	}
}

// slotForType returns the equip slot for a NON-WEAPON item type (armor
// slots equal their type; a shield is the one non-weapon whose slot is a
// HAND — the off-hand, #90), "" for consumable (no slot), and "" for
// weapon — a weapon's slot is a hand chosen at equip time
// (weaponTargetSlot).
func slotForType(t string) string {
	switch t {
	case protocol.ItemTypeConsumable, protocol.ItemTypeWeapon:
		return ""
	case protocol.ItemTypeShield:
		return protocol.SlotOffHand
	default:
		return t
	}
}

// weaponTargetSlot picks the hand an equipped weapon lands in: main if
// free, else off if free, else main (swap). A two-handed weapon always
// targets main (the equip path clears/locks off before calling this).
func weaponTargetSlot(e *entity, def *itemDef) string {
	if def.twoHanded {
		return protocol.SlotMainHand
	}

	if e.equippedDefIn(protocol.SlotMainHand) == nil {
		return protocol.SlotMainHand
	}

	if e.equippedDefIn(protocol.SlotOffHand) == nil {
		return protocol.SlotOffHand
	}

	return protocol.SlotMainHand
}

// heldSlotOf returns the hand slot (main or off) currently holding the item
// instance instID, or "" if it is not held in either hand.
func heldSlotOf(e *entity, instID int64) string {
	for _, slot := range [2]string{protocol.SlotMainHand, protocol.SlotOffHand} {
		if cur, ok := e.equipped[slot]; ok && cur.id == instID {
			return slot
		}
	}

	return ""
}

// currentSlotOf returns the slot inst currently occupies, or "" if it is not
// currently equipped at all (e.g. a backpack-resident item): heldSlotOf for a
// weapon (either hand), or the fixed type-derived slot for armor/jewelry,
// checked against actual occupancy (slotForType alone can't tell "equipped"
// from "sitting in the backpack while a DIFFERENT instance fills the slot").
func currentSlotOf(e *entity, inst itemInstance, def *itemDef) string {
	if def.isWeapon() {
		return heldSlotOf(e, inst.id)
	}

	slot := slotForType(def.itemType)
	if cur, ok := e.equipped[slot]; ok && cur.id == inst.id {
		return slot
	}

	return ""
}

// equipValidate: nil if def can be equipped at all — the only failure left
// is a consumable (no slot; drink is its action). Class gates dropped
// (gear keystone, #56): anyone equips anything.
func equipValidate(def *itemDef) error {
	if def.itemType == protocol.ItemTypeConsumable {
		return ErrNotEquippable
	}

	return nil
}

// canonicalSlotOrder is every equip slot in a fixed order — used wherever
// equipped items must fold deterministically (equippedRuleCards below),
// since e.equipped is a map and Go map iteration order is unspecified. The
// two hands come first (heldWeapons relies on this same main-then-off
// order), followed by the six armor/jewelry slots.
//
//nolint:gochecknoglobals // fixed enumeration order, effectively const.
var canonicalSlotOrder = []string{
	protocol.SlotMainHand, protocol.SlotOffHand,
	protocol.SlotHelmet, protocol.SlotChest, protocol.SlotGloves, protocol.SlotBoots,
	protocol.SlotRing, protocol.SlotAmulet,
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
// from a melee attack, not the armor "acting".
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
// (applyPendingEquip), and for kit granting (grantDefaultsLocked) — one
// placement path for every caller. inst is the item instance to equip,
// already known to be owned and equippable (queueEquipLocked's job); slot is
// its target slot (slotForType for armor/jewelry, weaponTargetSlot for a
// weapon).
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

// equipWeaponLocked applies a weapon equip toggle (the weapon-aware
// counterpart of a bare toggleEquip call, since a weapon's slot is a hand
// chosen at equip time, not fixed by itemType): naming an already-held
// weapon (in either hand) unequips it; otherwise inst is placed by
// weaponTargetSlot. A two-handed weapon can never share hands with anything
// else: equipping one evicts a current off-hand occupant first; equipping
// ANY weapon while a two-handed weapon sits in main evicts it first (a
// non-two-handed equip never otherwise touches the other hand). Both
// evictions land in the backpack via the same toggleEquip swap-to-backpack
// path unequip already uses — and are checked for room BEFORE any state
// changes, so a doomed equip fails politely with the entity untouched, never
// half-applied. Callers hold w.mu.
func (e *entity) equipWeaponLocked(inst itemInstance, def *itemDef) error {
	if slot := heldSlotOf(e, inst.id); slot != "" {
		if e.freeBackpackIndex() < 0 {
			return ErrBackpackFull
		}

		e.toggleEquip(inst, slot)

		return nil
	}

	var evictSlot string

	switch {
	case def.twoHanded && e.equippedDefIn(protocol.SlotOffHand) != nil:
		evictSlot = protocol.SlotOffHand
	case !def.twoHanded:
		if main := e.equippedDefIn(protocol.SlotMainHand); main != nil && main.twoHanded {
			evictSlot = protocol.SlotMainHand
		}
	}

	if evictSlot != "" {
		if e.freeBackpackIndex() < 0 {
			return ErrBackpackFull // politely fail before any state change
		}

		e.toggleEquip(e.equipped[evictSlot], evictSlot)
	}

	e.toggleEquip(inst, weaponTargetSlot(e, def))

	return nil
}

// closeDefFor is the def an entity's melee attack would highlight (the client's range
// hint) or falls back to when only one hit matters (unequipped-mid-turn
// guards): meleeDefsFor(e)'s first entry — its kind's claws for a monster, its
// first melee-tagged held weapon in hand order, or fists for a bare/unarmed
// player. A real melee attack resolves EVERY entry of meleeDefsFor, not just this one
// (attackLocked) — this shim is for non-combat callers only.
func closeDefFor(e *entity) *itemDef {
	return meleeDefsFor(e)[0]
}

// meleeDefsFor returns every melee hit an entity's melee attack delivers this turn, in
// hand order (heldWeapons — main before off): a monster's kind claws profile
// (single — monsters own no items and never equip; checked first, so a
// monster never falls through to the fists case), else every melee-tagged
// held weapon, else fists for a bare/unarmed player. Never empty. Panics if a
// monster entity's monsterKind names no registered kind — every production
// spawn path sets a real one; this only guards a malformed fixture.
func meleeDefsFor(e *entity) []*itemDef {
	if e.kind == protocol.EntityMonster {
		k := kindOf(e)
		if k == nil {
			panic("game: meleeDefsFor monster entity has no registered kind")
		}

		return []*itemDef{k.claws}
	}

	var out []*itemDef

	for _, def := range e.heldWeapons() {
		if def.hasTag(protocol.WeaponTagMelee) {
			out = append(out, def)
		}
	}

	if len(out) == 0 {
		return []*itemDef{fistsDef}
	}

	return out
}

// rangedDefFor is the LONGEST-range ranged/magic-tagged held weapon
// (rangedDefsFor(e, 0) — every rangeHex is >= 0, so dist 0 never filters any
// held ranged/magic weapon out, only feeds them all to the reduction below),
// or nil if none held at all — used only to gate "does this entity have any
// ranged attack whatsoever" (ErrNoRangedWeapon, the mid-turn-unequip fizzle
// check), independent of any particular shot's distance. A real shot resolves
// EVERY def rangedDefsFor(e, dist) returns, not just this one.
func rangedDefFor(e *entity) *itemDef {
	var best *itemDef

	for _, def := range rangedDefsFor(e, 0) {
		if best == nil || def.rangeHex > best.rangeHex {
			best = def
		}
	}

	return best
}

// rangedDefsFor returns every ranged/magic-tagged held weapon that reaches
// dist hexes, in hand order (heldWeapons — main before off): each fires as
// its own hit this turn (a bow's single-target shot, a magic weapon's own
// AoE). Empty if the entity holds no such weapon, or none reaches dist.
func rangedDefsFor(e *entity, dist int) []*itemDef {
	var out []*itemDef

	for _, def := range e.heldWeapons() {
		if !def.hasTag(protocol.WeaponTagRanged) && !def.hasTag(protocol.WeaponTagMagic) {
			continue
		}

		if def.rangeHex >= dist {
			out = append(out, def)
		}
	}

	return out
}

// heldWeapons returns e's equipped hand weapons, main then off (fixed
// order — deterministic fold, and "main hits first" once dual-wield damage
// resolution lands, task 2). Skips an empty hand.
func (e *entity) heldWeapons() []*itemDef {
	var out []*itemDef

	for _, slot := range [2]string{protocol.SlotMainHand, protocol.SlotOffHand} {
		if def := e.equippedDefIn(slot); def != nil {
			out = append(out, def)
		}
	}

	return out
}

// itemDamage is the single source of truth for an item's damage: the def's
// base — levels do not scale damage (#60, roadmap XP3: no raw-stat scaling;
// levels give HP and, later, skill points). Used by both the melee and
// ranged combat paths.
func itemDamage(def *itemDef) int {
	return def.damage
}

// Class-default item ids: shared between the registry (content.go), the
// class → starting-items mapping below, and their pinning tests, named so a
// typo is a compile error instead of a silent registry-lookup miss (and so
// goconst does not flag the same literal repeated across all three).
const (
	idIronSword  = "iron-sword"
	idDagger     = "dagger"
	idShortbow   = "shortbow"
	idOakWand    = "oak-wand"
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

// Inventory-slots starter-content ids (armor/consumable vocabulary): the
// potion lands with task 2 (the drink/stack machinery needs a real
// registered consumable); the armor/headband cards and the rat/wolf drop
// table entries land with task 3.
const (
	idLeatherArmor       = "leather-armor"
	idHeadbandOfLearning = "headband-of-learning"
	idHealingPotion      = "healing-potion"
)

// Crit%-weapon ids (fast-lane batch task 6, #69 Q5): the first weapons to
// carry a per-hit crit-chance card (the elf-crit card pattern applied to an
// item instead of a species), so a typo in content.go's registry entries or
// the wolf/ghoul drop tables is a compile error instead of a silent miss.
const (
	idMisericorde   = "misericorde"
	idDuelistsSaber = "duelists-saber"
)

// Noticeability item ids (#88, the last Gear 1 slice): the first gear to
// carry an evAggroRange card — how far away a WORLD-domain monster picks the
// wearer up. Named here for the same reason as every id block above: the
// registry entry (content.go), the rat/wolf/troll/dragon drop tables (also
// content.go) and their pinning tests can't drift on a typo.
const (
	idPaddedBoots    = "padded-boots"
	idIronPlateArmor = "iron-plate-armor"
)

// Shield ids (#90, S4 of #55): the first shield-type items — referenced
// from the registry, the rat/wolf/troll/dragon drop tables (both
// content.go), and their pinning tests.
const (
	idWoodenBuckler  = "wooden-buckler"
	idIronKiteShield = "iron-kite-shield"
)

// classDefaultIDs returns the item def ids a class starts with at Join: one
// melee-tagged weapon, plus a ranged/magic-tagged weapon for Rogue and Mage
// (Fighter has none — no ranged/magic-tagged default, so its off-hand starts
// empty). An empty or unknown class returns nil — Join's validClass check
// means this only guards non-player entities and test fixtures, mirroring
// class.go's baseMaxHP fallback comment.
func classDefaultIDs(class string) []string {
	switch class {
	case protocol.ClassFighter:
		return []string{idIronSword}
	case protocol.ClassRogue:
		return []string{idDagger, idShortbow}
	case protocol.ClassMage:
		return []string{idOakWand, idEmberFocus}
	default:
		return nil
	}
}

// grantDefaultsLocked creates, owns, and equips a fresh player's class-default
// items: one itemInstance per classDefaultIDs id, instance ids minted from
// the world's shared id sequence, placed through the SAME toggleEquip
// primitive a player's own equip intent uses (weaponTargetSlot picks the
// hand — fighter's iron sword lands main; rogue's dagger lands main, then
// its shortbow lands off since main is now taken; mage's oak wand/ember
// focus the same way) — no bare "owned but unequipped" starting state (the
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

		slot := slotForType(def.itemType)
		if def.isWeapon() {
			slot = weaponTargetSlot(e, def)
		}

		e.toggleEquip(inst, slot)
	}
}

// validateItemDefs panics on a content bug in defs: a duplicate id, an
// unknown item type, an invalid weapon tag shape, a heal value on the wrong
// kind of def, or a rule card referencing an unknown event/condition/effect
// kind — so bad content data fails at load, not mid-combat. Split out from
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
		validateItemCombatStats(def)
		validateRuleCards(def.id, def.rules)
	}
}

// validateItemType panics if def's itemType is unknown, if tags/twoHanded
// are set on a non-weapon, or if a weapon's tag shape is invalid
// (validateWeaponTags) — content-authoring mistakes, not runtime conditions.
// Split out of validateItemDefs to keep its cognitive complexity under the
// linter's threshold.
func validateItemType(def *itemDef) {
	if !validItemType(def.itemType) {
		panic("game: item " + def.id + " has unknown item type " + def.itemType)
	}

	if !def.isWeapon() {
		if len(def.tags) != 0 {
			panic("game: non-weapon item " + def.id + " must not set tags")
		}

		if def.twoHanded {
			panic("game: non-weapon item " + def.id + " must not set twoHanded")
		}

		return
	}

	validateWeaponTags(def)
}

// validateWeaponTags panics if a weapon def declares no tags, an unknown or
// duplicate tag, or a magic-tagged weapon with no range (a magic attack is
// always ranged — rangeHex 0 would make it unreachable by the ranged combat
// path). Split out of validateItemType for the same reason.
func validateWeaponTags(def *itemDef) {
	if len(def.tags) == 0 {
		panic("game: weapon " + def.id + " must declare at least one tag")
	}

	seen := make(map[string]bool, len(def.tags))

	for _, tag := range def.tags {
		switch tag {
		case protocol.WeaponTagMelee, protocol.WeaponTagRanged, protocol.WeaponTagMagic:
		default:
			panic("game: weapon " + def.id + " has unknown tag " + tag)
		}

		if seen[tag] {
			panic("game: weapon " + def.id + " has duplicate tag " + tag)
		}

		seen[tag] = true
	}

	if def.hasTag(protocol.WeaponTagMagic) && def.rangeHex <= 0 {
		panic("game: magic weapon " + def.id + " must have rangeHex > 0")
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

// validateItemCombatStats panics if a non-weapon def sets damage, rangeHex,
// or aoeRadius — only a weapon fires as a hit, so combat stats on a shield
// or armor def are authoring mistakes (a shield's −N lives in its rule card,
// not a damage field). #90.
func validateItemCombatStats(def *itemDef) {
	if def.isWeapon() {
		return
	}

	if def.damage != 0 || def.rangeHex != 0 || def.aoeRadius != 0 {
		panic("game: non-weapon item " + def.id + " must not set damage, rangeHex, or aoeRadius")
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
// (classDefaultIDs) actually naming a registered, equippable item — class
// gates are gone (#56), so equippability no longer depends on the class.
// Called once from content.go's init, so a content bug fails at process
// start.
func mustValidateContent() {
	validateItemDefs(itemDefs)
	validateMaxReach(itemDefs)
	validateMonsterDefs(monsterDefs)

	// Class passives ride the same card vocabulary as items/monsters —
	// validate them at init so a bad kind panics at process start, not
	// mid-combat (speciesCards predate this check and stay grandfathered;
	// extend this list when a card is added there).
	validateRuleCards("class:rogue", rogueGlanceCards)

	for _, class := range []string{protocol.ClassFighter, protocol.ClassRogue, protocol.ClassMage} {
		for _, id := range classDefaultIDs(class) {
			def, ok := itemDefByID[id]
			if !ok {
				panic("game: class default " + id + " for " + class + " is not a registered item")
			}

			if equipValidate(def) != nil {
				panic("game: class default " + id + " for " + class + " is not equippable")
			}
		}
	}
}
