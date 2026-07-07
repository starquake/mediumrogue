package game

// ResolveTurnForTest drives one turn resolution synchronously, so tests can
// step the world without running the ticker goroutine.
func (w *World) ResolveTurnForTest() {
	w.resolveTurn()
}
