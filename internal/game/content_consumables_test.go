package game_test

// content_consumables_test.go (#268): consumables through the live drink
// action (inventory.go).
//
// The heal-ladder half is GONE with #410, which deleted every consumable that
// had a `heal` value. itemDef.heal and the drink action's healing branch are
// still there, but no registered content exercises them, so there is nothing
// left to assert — a test naming a deleted item id would simply not compile.
// What remains is the buff/cure half, which is unaffected.

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// drinkGranted joins a fighter, grants + drinks one unit of the named
// consumable through the live drink action (free and immediate outside a
// bubble), and returns the join response — so a buff/antidote test can then
// assert the drinker's timed effects.
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
