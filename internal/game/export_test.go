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

// PlaceEntityForTest injects an entity at a specific hex and returns its id and
// bearer token, so conflict tests can build exact board states instead of
// depending on spawn geometry.
func (w *World) PlaceEntityForTest(hex protocol.Hex) (int64, string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.nextID++
	token := fmt.Sprintf("test-token-%d", w.nextID)
	e := &entity{id: w.nextID, hex: hex, token: token}
	w.entities[e.id] = e
	w.byToken[token] = e

	return e.id, token
}
