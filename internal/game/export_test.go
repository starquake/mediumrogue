package game

import (
	"fmt"
	mrand "math/rand/v2"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// ResolveTurnForTest drives one turn resolution synchronously, so tests can
// step the world without running the ticker goroutine.
func (w *World) ResolveTurnForTest() {
	w.resolveTurn()
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
// instead of depending on spawn geometry.
func (w *World) PlaceEntityForTest(hex protocol.Hex) (int64, string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.nextID++
	token := fmt.Sprintf("test-token-%d", w.nextID)
	e := &entity{
		id: w.nextID, hex: hex, token: token,
		kind: protocol.EntityPlayer, hp: protocol.PlayerMaxHP, maxHP: protocol.PlayerMaxHP,
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
// without the monster-AI think phase, mirroring resolveTurn minus
// thinkMonstersLocked. It exists so combat tests can pin an exact monster
// path via SetPathForTest (simulating a monster-initiated bump — attack or
// retreat) without the AI recomputing and overriding it on the very same
// turn.
func (w *World) ResolveCombatOnlyForTest() {
	w.mu.Lock()
	defer w.mu.Unlock()

	//nolint:gosec // deterministic per-turn combat RNG, not security-sensitive; test-only, mirrors resolveTurn.
	rng := mrand.New(mrand.NewPCG(uint64(w.seed), uint64(w.turn)))

	byHex := make(map[protocol.Hex][]*entity, len(w.entities))
	for _, e := range w.entities {
		byHex[e.hex] = append(byHex[e.hex], e)
	}

	attacks := w.moveAndBumpLocked(rng, byHex)
	w.attackLocked(rng, byHex, attacks)
	w.resolveDeathsLocked()

	w.turn++
}
