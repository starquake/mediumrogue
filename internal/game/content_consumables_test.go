package game_test

// content_consumables_test.go (#268): the heal ladder through the live drink
// action (inventory.go) — each rung restores its authored amount, and the
// Full Restorative clamps to max HP. Mirrors the shipped Healing Potion's
// drink coverage (inventory_actions_test.go).

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// healedFromDown joins a fighter, drops it downBy HP below its max, grants +
// drinks one unit of the named consumable, and returns the HP it recovered.
// downBy is chosen per rung to leave room for the whole heal (so nothing
// clamps) except where a clamp is the thing under test.
func healedFromDown(t *testing.T, defID string, downBy int) int {
	t.Helper()

	w := newWorld()

	me, err := w.Join("", "drinker", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	stackID := w.GrantItemForTest(me.EntityID, defID)

	maxHP := game.MaxHPForTest(protocol.ClassFighter, 1)
	start := maxHP - downBy
	w.SetHPForTest(me.EntityID, start)

	if err := w.SubmitIntent(intentFor(me.EntityID, me.Token, protocol.IntentDrink, stackID)); err != nil {
		t.Fatalf("SubmitIntent drink %s: %v", defID, err)
	}

	e, ok := entityOfSnap(w.Snapshot(), me.EntityID)
	if !ok {
		t.Fatal("player missing from snapshot")
	}

	return e.HP - start
}

// TestHealLadderAmounts: each rung restores its authored heal amount through
// the live drink action.
func TestHealLadderAmounts(t *testing.T) {
	t.Parallel()

	if got, want := healedFromDown(t, "minor-salve", 5), 3; got != want {
		t.Errorf("minor salve healed %d, want %d", got, want)
	}

	if got, want := healedFromDown(t, "greater-draught", 12), 10; got != want {
		t.Errorf("greater draught healed %d, want %d", got, want)
	}
}

// TestFullRestorativeClampsToMax: the ladder's top rung heals to full — its
// heal (999) clamps to maxHP, so from downBy below full it recovers exactly
// downBy, never over.
func TestFullRestorativeClampsToMax(t *testing.T) {
	t.Parallel()

	const downBy = 20 // deeper than any single heal amount, still < FighterMaxHP (30)

	if got, want := healedFromDown(t, "full-restorative", downBy), downBy; got != want {
		t.Errorf("full restorative healed %d, want %d (clamped to max HP)", got, want)
	}
}
