package game_test

// quaff_test.go (#322 slice 3): the two always-available draughts — what they
// restore, what stops them, and the independence of their cooldowns.

import (
	"errors"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

func TestQuaffHealthRestoresAndCools(t *testing.T) {
	t.Parallel()

	w := newWorld()

	id, token := w.PlaceEntityForTest(protocol.Hex{Q: 0, R: 0})
	w.SetHPForTest(id, 1)

	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentQuaffHealth,
	}); err != nil {
		t.Fatalf("quaff health: %v", err)
	}

	if got := w.HPForTest(id); got <= 1 {
		t.Errorf("HP after a draught = %d, want more than 1", got)
	}

	// Immediately again: the cooldown is the entire price, so it must bite.
	if got, want := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentQuaffHealth,
	}), game.ErrPotionOnCooldown; !errors.Is(got, want) {
		t.Errorf("second draught = %v, want %v", got, want)
	}
}

// TestQuaffCooldownsAreIndependent: draining one pool must not lock the other,
// or a fight that costs energy would also cost you your heal.
func TestQuaffCooldownsAreIndependent(t *testing.T) {
	t.Parallel()

	w := newWorld()

	id, token := w.PlaceEntityForTest(protocol.Hex{Q: 0, R: 0})
	w.SetEnergyForTest(id, 0)

	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentQuaffEnergy,
	}); err != nil {
		t.Fatalf("quaff energy: %v", err)
	}

	// Health is untouched by the energy draught's cooldown.
	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentQuaffHealth,
	}); err != nil {
		t.Errorf("health draught after an energy one = %v, want nil", err)
	}
}

// TestQuaffRestoresAShareOfTheMaximum pins the percentage rather than a flat
// number: pools grow with level, and a flat heal would quietly stop mattering.
func TestQuaffRestoresAShareOfTheMaximum(t *testing.T) {
	t.Parallel()

	w := newWorld()

	id, token := w.PlaceEntityForTest(protocol.Hex{Q: 0, R: 0})
	w.SetEnergyForTest(id, 0)

	maxEnergy := w.MaxEnergyForTest(id)

	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentQuaffEnergy,
	}); err != nil {
		t.Fatalf("quaff energy: %v", err)
	}

	if got, want := w.EnergyForTest(id), maxEnergy*protocol.PotionRestorePercent/100; got != want {
		t.Errorf("energy after a draught from empty = %d, want %d", got, want)
	}
}
