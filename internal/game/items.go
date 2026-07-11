package game

import (
	mrand "math/rand/v2"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// items.go: the gear system's types, per-entity helpers, and content
// validation (spec: docs/superpowers/specs/2026-07-10-m6b.4-gear-pipeline-design.md).
// The registry itself (itemDefs, and the class-default/drop tables built from
// it) lives in content.go, next to speciesCards — items.go holds the
// machinery, content.go holds the data.

// itemDef is one entry in the content registry: an item's fixed stats, its
// class/slot restriction, and the rule cards it feeds into the pipeline when
// equipped. Pure data (the design doc's §7 SQLite prerequisite) — mirrors
// ruleCard. class is empty for a built-in (fists/claws), never for a
// registry entry (mustValidateContent enforces this indirectly via
// classDefaultIDs).
type itemDef struct {
	id, name, desc string
	slot           string // protocol.ItemSlotClose | protocol.ItemSlotRanged
	class          string // protocol.ClassFighter | ClassRogue | ClassMage
	damage         int
	rangeHex       int // 0 = melee/adjacent (close-slot items only)
	aoeRadius      int // 0 = single target
	rules          []ruleCard
	dropWeight     int // 0 = never drops (class defaults); >0 = weight in dropTable
}

// itemInstance is one entity-owned copy of a def: a stable id (minted from
// the world's shared entity-id sequence — instance ids are unique across the
// whole world, entities and items alike, see grantDefaultsLocked) plus which
// def it is.
type itemInstance struct {
	id    int64
	defID string
}

// fistsDef and monsterClawsDef are built-in close-slot fallbacks: not in the
// registry (no instance ids, never owned, equipped, or dropped), just the
// profile closeDefFor returns when there is nothing better — an empty close
// slot for a player, or a monster (which owns no items at all).
//
//nolint:gochecknoglobals // built-in fallback defs, effectively const (never mutated).
var (
	fistsDef        = &itemDef{id: "fists", name: "Fists", slot: protocol.ItemSlotClose, damage: protocol.FistsDamage}
	monsterClawsDef = &itemDef{
		id: "claws", name: "Claws", slot: protocol.ItemSlotClose, damage: protocol.MonsterAttackDamage,
	}
)

// itemByID looks up an entity's owned item instance by id, so an equip
// intent (Task 4) can resolve its target without scanning items twice.
func (e *entity) itemByID(id int64) (itemInstance, bool) {
	for _, it := range e.items {
		if it.id == id {
			return it, true
		}
	}

	return itemInstance{}, false
}

// equippedDef returns the def currently filling slot (protocol.ItemSlotClose
// or ItemSlotRanged), or nil for an empty slot or an unknown slot string. It
// does NOT fall back to fists/claws — an empty ranged slot is a real "no
// ranged weapon" state, not a fallback candidate; that distinction is
// closeDefFor/rangedDefFor's job.
func (e *entity) equippedDef(slot string) *itemDef {
	var instID int64

	switch slot {
	case protocol.ItemSlotClose:
		instID = e.closeSlot
	case protocol.ItemSlotRanged:
		instID = e.rangedSlot
	default:
		return nil
	}

	if instID == 0 {
		return nil
	}

	it, ok := e.itemByID(instID)
	if !ok {
		return nil
	}

	return itemDefByID[it.defID]
}

// closeDefFor is the def an entity bumps with: its equipped close-slot item,
// or fists for an empty slot (a bare/unarmed player), or claws for a monster
// (which owns no items and never equips — checked first, so a monster never
// falls through to the fists case).
func closeDefFor(e *entity) *itemDef {
	if e.kind == protocol.EntityMonster {
		return monsterClawsDef
	}

	if def := e.equippedDef(protocol.ItemSlotClose); def != nil {
		return def
	}

	return fistsDef
}

// rangedDefFor is the def an entity shoots with: its equipped ranged-slot
// item, or nil if the slot is empty — no ranged weapon at all (the Fighter
// default, an unarmed/classless entity, or any monster).
func rangedDefFor(e *entity) *itemDef {
	return e.equippedDef(protocol.ItemSlotRanged)
}

// itemDamage is the single source of truth for an item's damage at a given
// level: the def's base plus DamagePerLevel for each level above 1. Used by
// both the melee and ranged combat paths.
func itemDamage(def *itemDef, level int) int {
	return def.damage + protocol.DamagePerLevel*(level-1)
}

// pickDrop draws one def from dropTable, weighted by dropWeight — a
// class-default (dropWeight 0) is never in dropTable, so it can never be
// returned. Returns nil only if dropTable is empty (no content registers any
// drops at all); the live registry always has entries, so dropLootLocked's
// nil check is a defensive no-op today. Consumes exactly one rng draw.
func pickDrop(rng *mrand.Rand) *itemDef {
	total := 0
	for _, def := range dropTable {
		total += def.dropWeight
	}

	if total == 0 {
		return nil
	}

	roll := rng.IntN(total)

	for _, def := range dropTable {
		if roll < def.dropWeight {
			return def
		}

		roll -= def.dropWeight
	}

	// Unreachable: roll is drawn from [0,total) and the loop above consumes
	// exactly total weight across dropTable's defs, so it always returns
	// before falling through.
	panic("game: pickDrop weight accounting bug")
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
)

// classDefaultIDs returns the item def ids a class starts with at Join: one
// close weapon, plus a ranged weapon for Rogue and Mage (Fighter has none).
// An empty or unknown class returns nil — Join's validClass check means this
// only guards non-player entities and test fixtures, mirroring class.go's
// baseMaxHP fallback comment.
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
// the world's shared id sequence, equipped straight into the def's slot (no
// bare "owned but unequipped" starting state). Callers hold w.mu and must
// call this after the entity is stored in w.entities — instance ids share
// nextID with entity ids, so ordering only matters for id uniqueness, not
// entity lookup.
func (w *World) grantDefaultsLocked(e *entity) {
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
		e.items = append(e.items, inst)

		switch def.slot {
		case protocol.ItemSlotClose:
			e.closeSlot = inst.id
		case protocol.ItemSlotRanged:
			e.rangedSlot = inst.id
		}
	}
}

// validateItemDefs panics on a content bug in defs: a duplicate id, an
// unknown slot, an unknown class, or a rule card referencing an unknown
// event/condition/effect kind — so bad content data fails at load, not
// mid-combat. Split out from mustValidateContent so tests can exercise the
// failure paths on a small synthetic def set instead of only the real
// registry.
func validateItemDefs(defs []*itemDef) {
	seen := make(map[string]bool, len(defs))

	for _, def := range defs {
		if seen[def.id] {
			panic("game: duplicate item id " + def.id)
		}

		seen[def.id] = true

		if def.slot != protocol.ItemSlotClose && def.slot != protocol.ItemSlotRanged {
			panic("game: item " + def.id + " has unknown slot " + def.slot)
		}

		if def.class != "" && !validClass(def.class) {
			panic("game: item " + def.id + " has unknown class " + def.class)
		}

		validateRuleCards(def.id, def.rules)
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
			switch cond.kind {
			case condChance, condTargetHPBelowPct, condTargetHPBelowFlat,
				condTargetHPFull, condAllyInBubble, condTargetAdjacent:
			case condAttackerSpecies:
				// A species gate on a species that can't exist would silently
				// never hold — a content typo, caught at load.
				if !validSpecies(cond.s) {
					panic("game: " + owner + " attackerSpecies rule card names unknown species " + cond.s)
				}
			default:
				panic("game: " + owner + " rule card has unknown condition " + cond.kind)
			}

			if c.event == evEarnXP && cond.kind == condChance {
				panic("game: " + owner + " earn-xp rule card has a chance condition (earn-xp folds run without rng)")
			}
		}

		switch c.then.kind {
		case effAdd, effMulPct:
		default:
			panic("game: " + owner + " rule card has unknown effect " + c.then.kind)
		}
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
// (classDefaultIDs) actually naming a registered item. Called once from
// content.go's init, so a content bug fails at process start.
func mustValidateContent() {
	validateItemDefs(itemDefs)
	validateMaxReach(itemDefs)
	validateMonsterDefs(monsterDefs)

	for _, class := range []string{protocol.ClassFighter, protocol.ClassRogue, protocol.ClassMage} {
		for _, id := range classDefaultIDs(class) {
			if _, ok := itemDefByID[id]; !ok {
				panic("game: class default " + id + " for " + class + " is not a registered item")
			}
		}
	}
}
