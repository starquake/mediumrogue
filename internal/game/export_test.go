package game

import (
	"fmt"
	mrand "math/rand/v2"
	"slices"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// ResolveTurnForTest drives one full turn synchronously, so tests can step the
// world without running the control-loop goroutine: it resolves the world
// domain, then every combat bubble that already existed before that world
// resolution (ungated — patience and lock-in are exercised via the dedicated
// clock bridges). Resolving only pre-existing bubbles means a bubble that forms
// during this same world resolution does not also act this step, so a single
// step is exactly one action for every entity — the invariant the milestone
// 6.1–6.3 tests were written against.
func (w *World) ResolveTurnForTest() {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := w.now()

	// Capture the pre-existing bubbles and their members before resolving the
	// world domain, so a bubble that forms during this same world resolution
	// does not also act this step, and defer the single recompute to the end
	// (mirroring pollTick's one-action-per-entity guarantee).
	ids := make([]int64, 0, len(w.bubbles))
	for id := range w.bubbles {
		ids = append(ids, id)
	}

	slices.Sort(ids)

	pre := make([]bubbleTurn, 0, len(ids))
	for _, id := range ids {
		b := w.bubbles[id]
		pre = append(pre, bubbleTurn{bubble: b, members: w.bubbleMembersLocked(b)})
	}

	w.resolveWorldTurnLocked(w.domainMembersLocked())

	for _, bt := range pre {
		w.resolveBubbleTurnLocked(bt.bubble, bt.members, now)
	}

	w.recomputeBubblesLocked(now)
}

// PollTickForTest runs one control-loop pass at the injected clock (see
// SetNowForTest) and reports whether anything resolved, so tests can drive the
// two-clock gating deterministically without real time.
func (w *World) PollTickForTest() bool {
	return w.pollTick(w.now())
}

// SetNowForTest replaces the world clock. Call before starting any goroutine
// that reads it (the production path never mutates it, so this is test-only).
func (w *World) SetNowForTest(now func() time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.now = now
}

// StartClockForTest seeds the world-tick accounting to "now", the baseline the
// Run loop establishes at startup, so the first world tick fires one interval
// later under the injected clock.
func (w *World) StartClockForTest() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.lastWorldTick = w.now()
}

// SetCombatPatienceForTest overrides the bubble patience timeout so a test can
// force a timeout in a few clock steps instead of the 60 s default.
func (w *World) SetCombatPatienceForTest(d time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.combatPatience = d
}

// SetBubblePollForTest overrides the control-loop poll cadence so the -race
// concurrency test can drive many passes per millisecond.
func (w *World) SetBubblePollForTest(d time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.bubblePoll = d
}

// SetSeedForTest pins the world's tie-break RNG seed so a test can assert exact,
// reproducible move-resolution outcomes.
func (w *World) SetSeedForTest(seed int64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.seed = seed
}

// WorldIDForTest exposes w's minted/restored worldID (item 4, playtest
// feedback batch 3) for snapshot round-trip and reset-signal tests.
func (w *World) WorldIDForTest() string {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.worldID
}

// PlaceEntityForTest injects a player entity at a specific hex and returns its
// id and bearer token, so conflict and AI tests can build exact board states
// instead of depending on spawn geometry. The player is a level-1 Fighter (the
// Join default), so its HP matches a plainly-joined player. It also grants
// and equips the class's default items (mirroring Join), so a placed
// Fighter bumps for iron-sword damage exactly like a joined one.
func (w *World) PlaceEntityForTest(hex protocol.Hex) (int64, string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.nextID++
	token := fmt.Sprintf("test-token-%d", w.nextID)
	maxHP := maxHPFor(protocol.ClassFighter, 1)
	e := &entity{
		id: w.nextID, hex: hex, token: token,
		kind: protocol.EntityPlayer, class: protocol.ClassFighter, hp: maxHP, maxHP: maxHP,
		// A placed player stands in for a live, connected one: give it an open
		// stream so the disconnect sweep never removes it out from under a test.
		streams: 1, disconnectedAt: w.now(),
	}
	w.entities[e.id] = e
	w.byToken[token] = e
	w.grantDefaultsLocked(e)

	return e.id, token
}

// PlaceMonsterForTest injects a monster entity at a specific hex and returns
// its id, so AI tests can build exact monster/player geometries without
// depending on SpawnMonsters' random placement.
func (w *World) PlaceMonsterForTest(hex protocol.Hex) int64 {
	return w.PlaceMonsterKindForTest(hex, defaultMonsterKindID)
}

// PlaceMonsterKindForTest is PlaceMonsterForTest for a caller-chosen monster
// kind (content.go's monsterDefs id), so a test can build an exact board
// state with a non-default kind (e.g. a dragon, for the Wyrmslayer pin).
// Panics if kind is not registered.
func (w *World) PlaceMonsterKindForTest(hex protocol.Hex, kind string) int64 {
	w.mu.Lock()
	defer w.mu.Unlock()

	k, ok := monsterDefByID[kind]
	if !ok {
		panic("game: PlaceMonsterKindForTest unknown monster kind " + kind)
	}

	w.nextID++
	w.entities[w.nextID] = &entity{
		id: w.nextID, hex: hex,
		kind: protocol.EntityMonster, monsterKind: k.id, hp: k.maxHP, maxHP: k.maxHP,
	}

	return w.nextID
}

// MonsterMaxHPForTest, MonsterDamageForTest, MonsterXPForTest,
// MonsterDropChanceForTest, and MonsterAggroRadiusForTest expose a
// registered monster kind's stats by id, so black-box tests can assert
// combat numbers without duplicating the registry (content.go) inline —
// mirrors ItemDamageForTest et al. Panics if kind is not registered.
func MonsterMaxHPForTest(kind string) int       { return monsterDefByID[kind].maxHP }
func MonsterDamageForTest(kind string) int      { return monsterDefByID[kind].damage }
func MonsterXPForTest(kind string) int          { return monsterDefByID[kind].xp }
func MonsterDropChanceForTest(kind string) int  { return monsterDefByID[kind].dropChance }
func MonsterAggroRadiusForTest(kind string) int { return monsterDefByID[kind].aggroRadius }

// SetHexForTest overwrites an entity's position directly, so a quest test can
// place an already-joined party member onto a reach quest's goal without
// grinding out a multi-turn path.
func (w *World) SetHexForTest(id int64, hex protocol.Hex) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if e, ok := w.entities[id]; ok {
		e.hex = hex
	}
}

// SetHPForTest overwrites an entity's HP directly, so tests can drive exact
// lethal-threshold scenarios (mutual kills, respawns) without grinding out
// many turns of combat.
func (w *World) SetHPForTest(id int64, hp int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if e, ok := w.entities[id]; ok {
		e.hp = hp
	}
}

// SetBubbleIDForTest overwrites an entity's bubbleID directly, so a regen test
// can pin an entity into (or out of) the world domain without needing a live
// opposing-pair bubble (and the combat that would come with one) to form
// naturally. Note: any subsequent recomputeBubblesLocked (e.g. the one at the
// end of ResolveTurnForTest) recalculates bubbleID from real positions, so this
// override only holds for the resolution pass it's set before.
func (w *World) SetBubbleIDForTest(id int64, bubbleID int64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if e, ok := w.entities[id]; ok {
		e.bubbleID = bubbleID
	}
}

// RegenTickForTest runs one passive-regen pass (regenPlayersLocked) over
// every current world-domain entity (bubbleID == 0), without resolving combat,
// deaths/respawns, or advancing the turn — so a regen test can isolate the
// passive-heal rule (plan §9) from the death-respawn and combat side effects a
// full ResolveTurnForTest step would also trigger.
func (w *World) RegenTickForTest() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.regenPlayersLocked(w.domainMembersLocked())
}

// XPForTest returns an entity's current cumulative XP, so tests can assert kill
// awards and death floors without going through Snapshot.
func (w *World) XPForTest(id int64) int {
	w.mu.Lock()
	defer w.mu.Unlock()

	if e, ok := w.entities[id]; ok {
		return e.xp
	}

	return 0
}

// SetXPForTest overwrites an entity's cumulative XP directly, so tests can place
// a player near a level boundary without grinding out kills.
func (w *World) SetXPForTest(id int64, xp int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if e, ok := w.entities[id]; ok {
		e.xp = xp
	}
}

// SetClassForTest overwrites a player entity's class directly, resyncs its max
// HP, and resets its inventory to the new class's default items (discarding
// whatever it held before — an empty class grants nothing, leaving both
// slots empty, so closeDefFor falls back to fists), so a melee test can pit
// different classes' close weapons against the same board without going
// through Join.
func (w *World) SetClassForTest(id int64, class string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if e, ok := w.entities[id]; ok {
		e.class = class
		e.equipped = nil
		e.backpack = [protocol.BackpackSize]backpackEntry{}
		w.grantDefaultsLocked(e)
		syncMaxHPLocked(e)
	}
}

// SetSpeciesForTest overwrites a player entity's species directly, so a combat
// test can drive a species passive (human XP, elf crit, dwarf DR) on an exact
// board without going through Join.
func (w *World) SetSpeciesForTest(id int64, species string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if e, ok := w.entities[id]; ok {
		e.species = species
	}
}

// PathForTest returns an entity's queued path (nil if idle), so a
// snapshot-restore test can assert this transient field is zeroed on a
// restored entity without depending on its side effects during resolution.
func (w *World) PathForTest(id int64) []protocol.Hex {
	w.mu.Lock()
	defer w.mu.Unlock()

	if e, ok := w.entities[id]; ok {
		return e.path
	}

	return nil
}

// SetAttackTargetForTest overwrites an entity's pending ranged-attack target
// directly, so a snapshot-restore test can engineer a non-zero attackTarget
// (normally only set mid-turn by an "attack" intent) and assert it is zeroed
// on a restored entity.
func (w *World) SetAttackTargetForTest(id int64, target protocol.Hex) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if e, ok := w.entities[id]; ok {
		e.attackTarget = &target
	}
}

// HasAttackTargetForTest reports whether an entity currently has a pending
// ranged-attack target queued — ground- OR entity-targeted (item 7) — so a
// test can assert the field's presence or absence without exposing the
// target hex or entity id itself.
func (w *World) HasAttackTargetForTest(id int64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	if e, ok := w.entities[id]; ok {
		return e.attackTarget != nil || e.attackTargetEntity != 0
	}

	return false
}

// SetAttackTargetEntityForTest overwrites an entity's pending single-target
// ranged-attack VICTIM directly (item 7, playtest batch 2), bypassing
// queueAttackLocked's submit-time validation — so a resolution test can
// engineer an entity-targeted shot's exact starting state (e.g. a target
// about to sidestep beyond range) the same way SetAttackTargetForTest does
// for the ground-targeted hex path.
func (w *World) SetAttackTargetEntityForTest(id, targetEntityID int64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if e, ok := w.entities[id]; ok {
		e.attackTargetEntity = targetEntityID
		e.attackTarget = nil
	}
}

// SetPathForTest overwrites an entity's queued path directly. A monster's path
// is normally computed fresh by thinkMonstersLocked every turn (which holds a
// monster in place whenever it's adjacent to a player — attacking is
// milestone 6.3 Task 3, not this one) — this bridge lets a combat test drive
// a monster's move (attack or retreat) directly, for use with
// ResolveCombatOnlyForTest, which does not call thinkMonstersLocked and so
// will not immediately overwrite it.
func (w *World) SetPathForTest(id int64, path []protocol.Hex) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if e, ok := w.entities[id]; ok {
		e.path = path
	}
}

// ResolveCombatOnlyForTest runs the move/bump/attack/death phases of a turn
// over all entities without the monster-AI think phase, mirroring
// resolveCombatLocked minus thinkMonstersLocked. It exists so combat tests can pin an exact monster
// path via SetPathForTest (simulating a monster-initiated bump — attack or
// retreat) without the AI recomputing and overriding it on the very same
// turn.
func (w *World) ResolveCombatOnlyForTest() {
	w.mu.Lock()
	defer w.mu.Unlock()

	//nolint:gosec // deterministic per-turn combat RNG, not security-sensitive; test-only, mirrors resolveCombatLocked.
	rng := mrand.New(mrand.NewPCG(uint64(w.seed), uint64(w.turn)))

	members := make([]*entity, 0, len(w.entities))
	for _, e := range w.entities {
		members = append(members, e)
	}

	slices.SortFunc(members, func(a, b *entity) int { return int(a.id - b.id) })

	byHex := make(map[protocol.Hex][]*entity, len(members))
	for _, e := range members {
		byHex[e.hex] = append(byHex[e.hex], e)
	}

	attacks := w.moveAndBumpLocked(rng, byHex, members)
	w.attackLocked(rng, byHex, attacks)
	w.resolveDeathsLocked(rng, members)

	w.turn++
}

// SetDisconnectGraceForTest overrides the disconnect grace so a presence test can
// drive the sweep after a short, hand-advanced interval instead of the default.
func (w *World) SetDisconnectGraceForTest(d time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.disconnectGrace = d
}

// SweepForTest runs the disconnect sweep at now and reports whether it removed
// any entity, so a test can drive removal without the control-loop goroutine.
func (w *World) SweepForTest(now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.sweepDisconnectedLocked(now)
}

// ArchivedForTest reports whether World.archive currently holds a character
// record for token, so a test can assert the sweep→archive→restore lifecycle
// (archived after sweep, consumed after a restoring Join) without re-deriving
// it indirectly from Join's side effects alone.
func (w *World) ArchivedForTest(token string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	_, ok := w.archive[token]

	return ok
}

// StreamsForTest returns the live stream count for the entity with token, or -1
// if no entity has that token, so a presence test can assert the bookkeeping and
// distinguish "zero streams" from "swept away".
func (w *World) StreamsForTest(token string) int {
	w.mu.Lock()
	defer w.mu.Unlock()

	if e, ok := w.byToken[token]; ok {
		return e.streams
	}

	return -1
}

// DisconnectedAtForTest returns the entity's removal-grace clock start and
// whether an entity with token exists, so a test can assert the stamp.
func (w *World) DisconnectedAtForTest(token string) (time.Time, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if e, ok := w.byToken[token]; ok {
		return e.disconnectedAt, true
	}

	return time.Time{}, false
}

// MaxHPForTest exposes the class/level max-HP helper so a test can assert the
// scaling curve directly, independent of a live entity.
func MaxHPForTest(class string, level int) int { return maxHPFor(class, level) }

// ItemDamageForTest exposes a registry item's level-scaled damage by id, so a
// black-box test can assert exact combat numbers without duplicating the
// registry (content.go) inline.
func ItemDamageForTest(id string, level int) int {
	return itemDamage(itemDefByID[id], level)
}

// ItemRangeForTest exposes a registry item's rangeHex by id.
func ItemRangeForTest(id string) int { return itemDefByID[id].rangeHex }

// ItemAoERadiusForTest exposes a registry item's aoeRadius by id.
func ItemAoERadiusForTest(id string) int { return itemDefByID[id].aoeRadius }

// RangedWeaponForTest exposes a class's default ranged item. It returns, in
// order, the level-scaled damage, range in hexes, AoE radius, and whether the
// class has a ranged default at all (false for Fighter and any classless
// entity).
func RangedWeaponForTest(class string, level int) (int, int, int, bool) {
	rangedSlot := weaponSlotsFor(class)[1]

	for _, id := range classDefaultIDs(class) {
		if def := itemDefByID[id]; def.itemType == rangedSlot {
			return itemDamage(def, level), def.rangeHex, def.aoeRadius, true
		}
	}

	return 0, 0, 0, false
}

// GrantItemForTest mints and grants (but does not equip) an item instance of
// defID to the entity — into its first free backpack entry (or merged into a
// mergeable consumable stack) — returning the new instance id (for a stack
// merge, the stack's existing representative id), so an equip test can
// engineer an owned item beyond a class's Join defaults without going
// through Join/SetClassForTest. Returns 0 for an unknown entity or a full
// backpack (a test-fixture bug, surfaced as an id no assert can match).
func (w *World) GrantItemForTest(entityID int64, defID string) int64 {
	w.mu.Lock()
	defer w.mu.Unlock()

	e, ok := w.entities[entityID]
	if !ok {
		return 0
	}

	if idx := e.stackIndexFor(defID); idx >= 0 {
		e.backpack[idx].count++

		return e.backpack[idx].inst.id
	}

	idx := e.freeBackpackIndex()
	if idx < 0 {
		return 0
	}

	w.nextID++
	inst := itemInstance{id: w.nextID, defID: defID}
	e.backpack[idx] = backpackEntry{inst: inst, count: 1}

	return inst.id
}

// EquippedSlotsForTest returns an entity's equipped close-ish and ranged-ish
// weapon-slot item instance ids (0 = empty), re-derived through the
// class-shaped weapon slots (weaponSlotsFor), so pre-inventory equip tests
// keep their close/ranged assertions across the storage change.
func (w *World) EquippedSlotsForTest(id int64) (int64, int64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if e, ok := w.entities[id]; ok {
		slots := weaponSlotsFor(e.class)

		return e.equipped[slots[0]].id, e.equipped[slots[1]].id
	}

	return 0, 0
}

// EquippedInSlotForTest returns the instance id equipped in a named typed
// slot (slot keys are itemType strings; 0 = empty), so an inventory-slots
// test can assert armor/jewelry equips beyond the two weapon slots.
func (w *World) EquippedInSlotForTest(id int64, slot string) int64 {
	w.mu.Lock()
	defer w.mu.Unlock()

	if e, ok := w.entities[id]; ok {
		return e.equipped[slot].id
	}

	return 0
}

// BackpackForTest returns an entity's backpack as (defID, count) pairs by
// entry index ("" / 0 = free entry), so a test can assert exact backpack
// layout — stacks, swap-through-backpack placement, free entries.
func (w *World) BackpackForTest(id int64) [protocol.BackpackSize]struct {
	DefID string
	Count int
} {
	w.mu.Lock()
	defer w.mu.Unlock()

	var out [protocol.BackpackSize]struct {
		DefID string
		Count int
	}

	if e, ok := w.entities[id]; ok {
		for i, be := range e.backpack {
			if !be.empty() {
				out[i].DefID = be.inst.defID
				out[i].Count = be.count
			}
		}
	}

	return out
}

// SetPendingEquipForTest overwrites an entity's queued equip directly, so a
// death test can engineer a pending equip surviving into a death/respawn
// without depending on multi-player bubble timing (a solo bubble's equip
// resolves — and clears pendingEquip — synchronously within the SubmitIntent
// call that completes its lock-in).
func (w *World) SetPendingEquipForTest(id, itemID int64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if e, ok := w.entities[id]; ok {
		e.pending = pendingItemAction{kind: protocol.IntentEquip, id: itemID}
	}
}

// PendingEquipForTest returns an entity's queued equip item id (0 = none), so
// a test can assert it was cleared.
func (w *World) PendingEquipForTest(id int64) int64 {
	w.mu.Lock()
	defer w.mu.Unlock()

	if e, ok := w.entities[id]; ok && e.pending.kind == protocol.IntentEquip {
		return e.pending.id
	}

	return 0
}

// ReachableWalkableForTest exposes reachableWalkable — the origin-connected
// walkable BFS — so a black-box test can assert on the spawnable region.
func ReachableWalkableForTest(m protocol.MapResponse) map[protocol.Hex]bool {
	return reachableWalkable(m)
}

// TileCountForTest exposes tileCount, the closed-form hexagon tile count, so
// a black-box test can assert map size without duplicating the formula.
func TileCountForTest(radius int) int {
	return tileCount(radius)
}

// PickDropForTest exposes pickDropFrom over a named monster kind's own drop
// table, seeded from a single uint64 (stream 0), so a content test can
// enumerate the weighted-drop distribution over a fixed seed range without
// depending on any World. Returns "" if kind is unregistered, its table is
// empty, or every weight is zero (pickDropFrom's defensive case).
func PickDropForTest(kind string, seed uint64) string {
	k, ok := monsterDefByID[kind]
	if !ok {
		return ""
	}

	//nolint:gosec // deterministic test-only seed, not security-sensitive; reproducibility is required.
	rng := mrand.New(mrand.NewPCG(seed, 0))

	return pickDropFrom(rng, k.drops)
}

// DropTableIDsForTest returns every def id in a named monster kind's own
// drops table (registry order, duplicates included when a kind lists the
// same item at more than one weight bucket — none do today), so a content
// test can assert pickDropFrom's output set against the live registry
// instead of a hand-duplicated literal list.
func DropTableIDsForTest(kind string) []string {
	dropTable := monsterDefByID[kind].drops

	ids := make([]string, len(dropTable))
	for i, d := range dropTable {
		ids[i] = d.defID
	}

	return ids
}

// GroundItemForTest drops a fresh item instance of defID directly onto hex,
// bypassing the death-roll (dropLootLocked) entirely, so a pickup test can
// engineer an exact ground-item board state without seed-hunting a kill's
// drop roll. Returns the new instance id.
func (w *World) GroundItemForTest(hex protocol.Hex, defID string) int64 {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.nextID++
	inst := itemInstance{id: w.nextID, defID: defID}
	w.groundItems[hex] = append(w.groundItems[hex], inst)

	return inst.id
}

// RingOfForTest exposes ringOf (worldgen.go), so a black-box test can pin
// the ring-band math at various world radii without depending on any World.
func RingOfForTest(h protocol.Hex, worldRadius int) int {
	return ringOf(h, worldRadius)
}

// MonsterKindForTest returns the monster-kind registry id of the entity
// with id (empty for a player, or an unknown id), so a ring/spawn test can
// assert on WHICH kind landed where without waiting for Entity.MonsterKind
// to ride the wire (6c Task 4).
func (w *World) MonsterKindForTest(id int64) string {
	w.mu.Lock()
	defer w.mu.Unlock()

	if e, ok := w.entities[id]; ok {
		return e.monsterKind
	}

	return ""
}

// MonsterRingsForTest returns the rings a registered monster kind spawns
// in, so a ring/spawn test can check "this kind is valid for the ring its
// hex fell into" without duplicating the registry inline. Panics if kind
// is not registered.
func MonsterRingsForTest(kind string) []int {
	return monsterDefByID[kind].rings
}

// KillSummaryForTest exposes killSummary over a list of monster-kind ids
// (resolved through monsterDefByID, in the given order — killSummary's
// grouping is order-sensitive, see its doc comment), so a black-box test
// can assert the exact chat/combat-log announce text without duplicating
// the pipeline inline. Panics if any id is unregistered.
func KillSummaryForTest(kindIDs ...string) string {
	slain := make([]*monsterDef, len(kindIDs))

	for i, id := range kindIDs {
		k, ok := monsterDefByID[id]
		if !ok {
			panic("game: KillSummaryForTest unknown monster kind " + id)
		}

		slain[i] = k
	}

	return killSummary(slain)
}

// KillSoloSummaryForTest exposes killSoloSummary the same way
// KillSummaryForTest exposes killSummary (playtest item 3's named-solo-
// killer wording), so a black-box test can pin the exact text — including a
// mixed-kind solo kill — without duplicating the pipeline inline. Panics if
// any id is unregistered.
func KillSoloSummaryForTest(playerName string, kindIDs ...string) string {
	slain := make([]*monsterDef, len(kindIDs))

	for i, id := range kindIDs {
		k, ok := monsterDefByID[id]
		if !ok {
			panic("game: KillSoloSummaryForTest unknown monster kind " + id)
		}

		slain[i] = k
	}

	return killSoloSummary(playerName, slain)
}
