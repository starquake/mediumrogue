package game_test

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestKilledEntityDoesNotMove (#104): an entity killed in the attack phase
// does not get its move — the spec's death-timing consequence. A 1-HP
// monster with a queued retreat path dies to the melee attack (resolved first) and
// is removed; no "move" combat event is ever logged for it.
func TestKilledEntityDoesNotMove(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(3)

	var buf bytes.Buffer

	w.SetLogger(slog.New(slog.NewJSONHandler(&buf, nil)))

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)
	w.SetHPForTest(monsterID, 1)

	var (
		escapeHex protocol.Hex
		found     bool
	)

	for _, n := range game.HexNeighbors(monsterHex) {
		if n != me.Hex && isWalkable(w, n) {
			escapeHex = n
			found = true

			break
		}
	}

	if !found {
		t.Skip("no free walkable escape hex around the monster on this map")
	}

	if err := w.SubmitIntent(entityAttackIntent(me.EntityID, me.Token, monsterID)); err != nil {
		t.Fatalf("SubmitIntent(melee): %v", err)
	}

	w.SetPathForTest(monsterID, []protocol.Hex{escapeHex})
	w.ResolveCombatOnlyForTest()

	if _, ok := entityOfSnap(w.Snapshot(), monsterID); ok {
		t.Fatalf("monster %d should be dead and removed", monsterID)
	}

	for _, ev := range eventsOfKind(slogEvents(t, &buf), "move") {
		if id, ok := ev["id"].(float64); ok && int64(id) == monsterID {
			t.Errorf("killed monster %d logged a move event %v; the dead never move", monsterID, ev)
		}
	}
}
