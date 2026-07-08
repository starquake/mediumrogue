package game

import (
	"fmt"

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
