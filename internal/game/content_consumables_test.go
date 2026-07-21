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

// drinkGranted joins a fighter, grants + drinks one unit of the named
// consumable through the live drink action (free and immediate outside a
// bubble), and returns the join response — so a buff/antidote test can then
// assert the drinker's timed effects. Mirrors healedFromDown without the HP
// wrangling.
func drinkGranted(t *testing.T, w *game.World, defID string) protocol.JoinResponse {
	t.Helper()

	me, err := w.Join("", "drinker", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	stackID := w.GrantItemForTest(me.EntityID, defID)
	if err := w.SubmitIntent(intentFor(me.EntityID, me.Token, protocol.IntentDrink, stackID)); err != nil {
		t.Fatalf("SubmitIntent drink %s: %v", defID, err)
	}

	return me
}

// TestDraughtOfFuryBuffsOnDrink: the offensive buff potion (#271, slice 2)
// applies a timed frenzy (+25% deal-damage) self-buff to the drinker — the
// drink counterpart of the Bloodrage Cleaver's on-hit self-buff, proving the
// appliesEffect rider reaches the live drink action.
func TestDraughtOfFuryBuffsOnDrink(t *testing.T) {
	t.Parallel()

	w := newWorld()
	me := drinkGranted(t, w, "draught-of-fury")

	mag, turns, ok := w.EffectForTest(me.EntityID, effectFrenzy)
	if !ok {
		t.Fatal("draught of fury did not apply a frenzy buff on drink")
	}

	if got, want := mag, 125; got != want {
		t.Errorf("frenzy magnitude = %d, want %d (+25%% deal-damage)", got, want)
	}

	if got, want := turns, 4; got != want {
		t.Errorf("frenzy turnsRemaining = %d, want %d", got, want)
	}
}

// TestWardingTonicWardsOnDrink: the defensive buff potion (#271, slice 2)
// applies a timed ward (a take-damage mulPct, +25% damage resistance) to the
// drinker — the reuse of the existing evTakeDamage event as pure content.
func TestWardingTonicWardsOnDrink(t *testing.T) {
	t.Parallel()

	w := newWorld()
	me := drinkGranted(t, w, "warding-tonic")

	mag, turns, ok := w.EffectForTest(me.EntityID, effectWard)
	if !ok {
		t.Fatal("warding tonic did not apply a ward buff on drink")
	}

	if got, want := mag, 75; got != want {
		t.Errorf("ward magnitude = %d, want %d (percentBase-25 => -25%% damage taken)", got, want)
	}

	if got, want := turns, 4; got != want {
		t.Errorf("ward turnsRemaining = %d, want %d", got, want)
	}
}

// TestAntivenomClearsPoisonNotBuffs: the antidote (#271, slice 2) strips every
// HARMFUL timed effect (the Serpent's poison) on drink while leaving beneficial
// effects (a frenzy buff) intact — the documented "cleanse harmful only"
// decision. Pins BOTH halves: a cleanse that cleared everything would pass the
// poison-gone assertion but fail the buff-survives one.
func TestAntivenomClearsPoisonNotBuffs(t *testing.T) {
	t.Parallel()

	w := newWorld()

	me, err := w.Join("", "drinker", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.ApplyEffectForTest(me.EntityID, effectPoison, -2, 3)  // harmful DoT
	w.ApplyEffectForTest(me.EntityID, effectFrenzy, 125, 4) // beneficial buff

	stackID := w.GrantItemForTest(me.EntityID, "antivenom")
	if err := w.SubmitIntent(intentFor(me.EntityID, me.Token, protocol.IntentDrink, stackID)); err != nil {
		t.Fatalf("SubmitIntent drink antivenom: %v", err)
	}

	if _, _, ok := w.EffectForTest(me.EntityID, effectPoison); ok {
		t.Error("antivenom did not clear the harmful poison effect")
	}

	if _, _, ok := w.EffectForTest(me.EntityID, effectFrenzy); !ok {
		t.Error("antivenom wrongly cleared a beneficial buff (cleanse is harmful-only)")
	}

	if got, want := w.EffectCountForTest(me.EntityID), 1; got != want {
		t.Errorf("effect count after cleanse = %d, want %d (only the buff remains)", got, want)
	}
}
