package game

import (
	mrand "math/rand/v2"
	"slices"
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
// (armored, regenerating). Pure data, mirroring itemDef's shape. A kind's
// LOOKS (dot color + glyph letter) live client-side in entities.ts's
// KIND_STYLE, keyed by this id — presentation is client-owned by
// convention, same as CLASS_GLYPH for player classes.
type monsterDef struct {
	id, name string
	maxHP    int
	damage   int // the claws profile: closeDefFor's monster branch
	xp       int // per-kill award, folded through the shared earn-XP event
	// aggroRadius overrides protocol.MonsterAggroRadius for a WORLD-domain
	// monster of this kind; 0 means "use the default". Non-zero values must
	// be strictly greater than protocol.CombatRadius (validateMonsterDefs) —
	// the same invariant protocol.MonsterAggroRadius itself carries: a
	// monster must notice a player before it can close into a combat
	// bubble, or it sits frozen just outside its own aggro range forever.
	aggroRadius int
	// leashRadius overrides this kind's leash radius (#102 — how far from
	// its home hex a WORLD-domain monster of this kind will chase before it
	// drops the target and walks back home); 0 means "use the default",
	// protocol.MonsterLeashMultiplier × the kind's base aggro radius
	// (leashRadiusFor, world.go). Non-zero values must be strictly greater
	// than the kind's base aggro radius (validateMonsterLeashRadius): a
	// leash inside the aggro radius would drop a chase the moment it starts.
	leashRadius int
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
		def.claws = &itemDef{
			id: "claws", name: "Claws", itemType: protocol.ItemTypeWeapon,
			tags: []string{protocol.WeaponTagMelee}, damage: def.damage, rules: def.rules,
		}
	}
}

// newMonsterEntity builds a monster entity of kind k at hex h with id.
// EVERY monster spawn path goes through it (SpawnMonsters,
// SpawnMonsterKindAt, PlaceMonsterKindForTest) so a new one cannot silently
// forget a field: a monster whose homeHex were left at the zero value would
// take the origin for home and leash-walk the whole map back to it (#102).
// The caller owns id allocation (w.nextID) and inserting into w.entities.
func newMonsterEntity(id int64, h protocol.Hex, k *monsterDef) *entity {
	return &entity{
		id: id, hex: h, homeHex: h,
		kind: protocol.EntityMonster, monsterKind: k.id, hp: k.maxHP, maxHP: k.maxHP,
	}
}

// defAggroRadius returns def's effective base aggro radius: its own
// aggroRadius override if non-zero, else the shared
// protocol.MonsterAggroRadius default. A nil def (a player, or a malformed
// fixture whose monsterKind names no registered kind) also takes the
// default. It is the ONE place the "0 means the default" rule lives —
// baseAggroRadiusFor (the runtime, entity-level caller) and
// validateMonsterLeashRadius (the init-time content check) both go through
// it, so the validator can never check a stale formula the runtime no
// longer uses.
func defAggroRadius(def *monsterDef) int {
	if def != nil && def.aggroRadius != 0 {
		return def.aggroRadius
	}

	return protocol.MonsterAggroRadius
}

// defLeashRadius returns def's effective leash radius (#102): its own
// leashRadius override if non-zero, else protocol.MonsterLeashMultiplier ×
// defAggroRadius(def). Nil takes the derived default, mirroring
// defAggroRadius. Same single-source-of-truth role as defAggroRadius:
// leashRadiusFor is its entity-level wrapper.
func defLeashRadius(def *monsterDef) int {
	if def != nil && def.leashRadius != 0 {
		return def.leashRadius
	}

	return protocol.MonsterLeashMultiplier * defAggroRadius(def)
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

// pickDropFrom draws one defID from table, weighted by weight — mirrors the
// pre-6c pickDrop's algorithm but reads a monsterDef's own table instead of
// the (retired) global dropTable. Returns "" if table is empty or every
// weight is zero. Consumes exactly one rng draw.
func pickDropFrom(rng *mrand.Rand, table []drop) string {
	total := 0
	for _, d := range table {
		total += d.weight
	}

	if total == 0 {
		return ""
	}

	roll := rng.IntN(total)

	for _, d := range table {
		if roll < d.weight {
			return d.defID
		}

		roll -= d.weight
	}

	// Unreachable: see the pre-6c pickDrop's identical comment — roll is
	// drawn from [0,total) and the loop consumes exactly total weight.
	panic("game: pickDropFrom weight accounting bug")
}

// kindsPerRing returns, for each ring 0..protocol.RingCount-1, the
// id-sorted (for determinism) list of monster-kind ids registered for it
// (monsterDefs' own rings field) — SpawnMonsters' uniform-among-the-ring's-
// kinds pick draws from this. Built fresh per call since SpawnMonsters is
// not hot-path (a handful of calls at server startup, or in tests).
func kindsPerRing() [][]string {
	out := make([][]string, protocol.RingCount)

	for _, def := range monsterDefs {
		for _, r := range def.rings {
			out[r] = append(out[r], def.id)
		}
	}

	for r := range out {
		slices.Sort(out[r])
	}

	return out
}

// excludeKind returns kinds with every occurrence of id removed (order
// preserved) — SpawnMonsters' dragon-cap check: once protocol.DragonCount
// dragons are placed, dragon drops out of ring 2's candidate kind list for
// the rest of that call.
func excludeKind(kinds []string, id string) []string {
	out := make([]string, 0, len(kinds))

	for _, k := range kinds {
		if k != id {
			out = append(out, k)
		}
	}

	return out
}

// weightedRingPick draws a ring index weighted by weights, mirroring
// pickDropFrom's algorithm. ok is false iff every weight is zero (every
// ring is out of placement weight — SpawnMonsters stops placing).
func weightedRingPick(rng *mrand.Rand, weights []int) (int, bool) {
	total := 0
	for _, w := range weights {
		total += w
	}

	if total == 0 {
		return 0, false
	}

	roll := rng.IntN(total)

	for i, w := range weights {
		if roll < w {
			return i, true
		}

		roll -= w
	}

	// Unreachable: see pickDropFrom's identical comment — roll is drawn
	// from [0,total) and the loop consumes exactly total weight.
	panic("game: weightedRingPick weight accounting bug")
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
		validateMonsterLeashRadius(def)
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

// validateMonsterLeashRadius panics if def.leashRadius violates the
// invariant documented on monsterDef.leashRadius: 0 (use the default,
// protocol.MonsterLeashMultiplier × the base aggro radius) or strictly
// greater than the kind's base aggro radius (def.aggroRadius if set, else
// protocol.MonsterAggroRadius) — a leash at or inside the aggro radius
// would drop every chase the moment it starts.
func validateMonsterLeashRadius(def *monsterDef) {
	if def.leashRadius == 0 {
		return
	}

	if def.leashRadius <= defAggroRadius(def) {
		panic("game: monster " + def.id + " leashRadius must be 0 or > its aggro radius")
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
