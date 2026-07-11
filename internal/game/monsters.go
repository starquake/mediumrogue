package game

import (
	"strconv"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// monsters.go: the monster-kind registry's types, per-entity helpers, and
// content validation (spec:
// docs/superpowers/specs/2026-07-10-m6c-monster-kinds-rings-design.md). The
// registry itself (monsterDefs) lives in content.go, next to itemDefs —
// this file holds the machinery, mirroring items.go's split.

// Monster-kind ids: named the same way as items.go's item id consts, and
// for the same reason — referenced from the registry (content.go), combat
// (world.go), and their pinning tests, so a typo is a compile error instead
// of a silent registry-lookup miss.
const (
	idKindRat    = "rat"
	idKindWolf   = "wolf"
	idKindGhoul  = "ghoul"
	idKindTroll  = "troll"
	idKindDragon = "dragon"
)

// defaultMonsterKindID is the kind SpawnMonsters/SpawnMonsterAt/
// PlaceMonsterForTest fall back to when no kind is named — wolf, which
// carries today's exact pre-6c numbers (10 HP, 3 damage, 20 XP, aggro 10,
// 30% drop) so every un-migrated call site keeps its old behavior verbatim.
const defaultMonsterKindID = idKindWolf

// drop is one weighted entry in a monsterDef's own loot table
// (monsterDef.drops): pickDropFrom draws defID with probability
// weight/sum(weights) — the monster-side loot model (6c moves loot
// authority off items entirely; itemDef no longer carries a dropWeight).
type drop struct {
	defID  string
	weight int
}

// monsterDef is one entry in the monster-kind content registry: a kind's
// fixed stats, its own weighted loot table, which difficulty rings it
// spawns in, and the (empty at launch) rule-card seam for future passives
// (armored, regenerating). Pure data, mirroring itemDef's shape.
type monsterDef struct {
	id, name, glyph string
	maxHP           int
	damage          int // the claws profile: closeDefFor's monster branch
	xp              int // per-kill award, folded through the shared earn-XP event
	// aggroRadius overrides protocol.MonsterAggroRadius for a WORLD-domain
	// monster of this kind; 0 means "use the default". Non-zero values must
	// be strictly greater than protocol.CombatRadius (validateMonsterDefs) —
	// the same invariant protocol.MonsterAggroRadius itself carries: a
	// monster must notice a player before it can close into a combat
	// bubble, or it sits frozen just outside its own aggro range forever.
	aggroRadius int
	dropChance  int // percent, this kind's own roll (protocol.DropChancePercent retired)
	drops       []drop
	rings       []int      // which difficulty rings (0..protocol.RingCount-1) this kind spawns in
	rules       []ruleCard // future kind passives; empty at launch

	// claws is the built-in close-slot profile closeDefFor returns for an
	// entity of this kind: damage + rules from the fields above, mirroring
	// fistsDef's shape but per-kind. Built once by buildMonsterIndex, not
	// part of the authored literal in content.go.
	claws *itemDef
}

// monsterDefs and monsterDefByID are the registry and its by-id lookup,
// built and validated once at package init (content.go's init, alongside
// the item registry).
//
//nolint:gochecknoglobals // derived lookup table, built once at init from monsterDefs (content.go).
var monsterDefByID map[string]*monsterDef

// buildMonsterIndex builds monsterDefByID and each def's claws profile from
// monsterDefs (content.go). Called once from content.go's init, before
// mustValidateContent.
func buildMonsterIndex() {
	monsterDefByID = make(map[string]*monsterDef, len(monsterDefs))

	for _, def := range monsterDefs {
		monsterDefByID[def.id] = def
		def.claws = &itemDef{id: "claws", name: "Claws", slot: protocol.ItemSlotClose, damage: def.damage, rules: def.rules}
	}
}

// kindOf resolves e's monster-kind def (content.go's monsterDefs), or nil
// for a player. A monster entity whose monsterKind names no registered kind
// also resolves to nil (map miss) — every production spawn path sets a
// real, registered kind, so this only guards a malformed test fixture.
func kindOf(e *entity) *monsterDef {
	if e.kind != protocol.EntityMonster {
		return nil
	}

	return monsterDefByID[e.monsterKind]
}

// validateMonsterDefs panics on a content bug in defs: a duplicate id, a
// drop referencing an unknown item, an aggroRadius that violates the
// CombatRadius invariant, a ring outside [0,protocol.RingCount), a ring left
// uncovered by any kind, or a rule card naming an unknown event/condition/
// effect kind. Split out from mustValidateContent so tests can exercise the
// failure paths on a small synthetic def set, mirroring validateItemDefs.
func validateMonsterDefs(defs []*monsterDef) {
	seen := make(map[string]bool, len(defs))
	covered := make(map[int]bool, protocol.RingCount)

	for _, def := range defs {
		if seen[def.id] {
			panic("game: duplicate monster id " + def.id)
		}

		seen[def.id] = true

		validateMonsterAggroRadius(def)
		validateMonsterDrops(def)
		validateMonsterRings(def, covered)
		validateRuleCards(def.id, def.rules)
	}

	for r := range protocol.RingCount {
		if !covered[r] {
			panic("game: no monster kind covers ring " + strconv.Itoa(r))
		}
	}
}

// validateMonsterAggroRadius panics if def.aggroRadius violates the
// CombatRadius invariant documented on monsterDef.aggroRadius: 0 (use the
// default) or strictly greater than protocol.CombatRadius.
func validateMonsterAggroRadius(def *monsterDef) {
	if def.aggroRadius != 0 && def.aggroRadius <= protocol.CombatRadius {
		panic("game: monster " + def.id + " aggroRadius must be 0 or > CombatRadius")
	}
}

// validateMonsterDrops panics if any of def's drops references an item id
// that is not in the registered itemDefByID.
func validateMonsterDrops(def *monsterDef) {
	for _, d := range def.drops {
		if _, ok := itemDefByID[d.defID]; !ok {
			panic("game: monster " + def.id + " drop references unknown item " + d.defID)
		}
	}
}

// validateMonsterRings panics if def names no ring at all, or a ring index
// outside [0,protocol.RingCount); marks every ring def spawns in as covered
// in the shared covered map (validateMonsterDefs' every-ring-has-a-kind check).
func validateMonsterRings(def *monsterDef, covered map[int]bool) {
	if len(def.rings) == 0 {
		panic("game: monster " + def.id + " has no rings")
	}

	for _, r := range def.rings {
		if r < 0 || r >= protocol.RingCount {
			panic("game: monster " + def.id + " has invalid ring " + strconv.Itoa(r))
		}

		covered[r] = true
	}
}
