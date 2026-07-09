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

// PlaceEntityForTest injects a player entity at a specific hex and returns its
// id and bearer token, so conflict and AI tests can build exact board states
// instead of depending on spawn geometry. The player is a level-1 Fighter (the
// Join default), so its HP matches a plainly-joined player.
func (w *World) PlaceEntityForTest(hex protocol.Hex) (int64, string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.nextID++
	token := fmt.Sprintf("test-token-%d", w.nextID)
	maxHP := maxHPFor(protocol.ClassFighter, 1)
	e := &entity{
		id: w.nextID, hex: hex, token: token,
		kind: protocol.EntityPlayer, class: protocol.ClassFighter, hp: maxHP, maxHP: maxHP,
	}
	w.entities[e.id] = e
	w.byToken[token] = e

	return e.id, token
}

// PlaceMonsterForTest injects a monster entity at a specific hex and returns
// its id, so AI tests can build exact monster/player geometries without
// depending on SpawnMonsters' random placement.
func (w *World) PlaceMonsterForTest(hex protocol.Hex) int64 {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.nextID++
	w.entities[w.nextID] = &entity{
		id: w.nextID, hex: hex,
		kind: protocol.EntityMonster, hp: protocol.MonsterMaxHP, maxHP: protocol.MonsterMaxHP,
	}

	return w.nextID
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
	w.resolveDeathsLocked(members)

	w.turn++
}

// MaxHPForTest exposes the class/level max-HP helper so a test can assert the
// scaling curve directly, independent of a live entity.
func MaxHPForTest(class string, level int) int { return maxHPFor(class, level) }

// CloseWeaponDamageForTest exposes a class's default close-weapon damage at a
// given level (the melee/bump path Tasks 3/4 will read).
func CloseWeaponDamageForTest(class string, level int) int {
	return weaponDamage(closeWeapon(class), level)
}

// RangedWeaponForTest exposes a class's default ranged weapon. It returns, in
// order, the level-scaled damage, range in hexes, AoE radius, and whether the
// class has a ranged attack at all (false for Fighter and any classless entity).
func RangedWeaponForTest(class string, level int) (int, int, int, bool) {
	w, ok := rangedWeapon(class)

	return weaponDamage(w, level), w.rangeHex, w.aoeRadius, ok
}
