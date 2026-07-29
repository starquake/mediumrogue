package game_test

// energy_test.go (#322 slice 2): the action currency — what a pool starts at,
// what an active costs, what happens when you cannot afford one, and the regen
// that keeps a long fight playable.

import (
	"errors"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestEnergyPoolIsTheInverseOfTheHPCurve: the squishy classes live on their
// actives, so they carry more energy. Pins the relationship rather than the
// three numbers, so a retune that keeps the shape does not fail here.
//
//nolint:paralleltest // drives a shared world.
func TestEnergyPoolIsTheInverseOfTheHPCurve(t *testing.T) {
	w := newWorld()

	pools := map[string]int{}

	for _, c := range []string{protocol.ClassFighter, protocol.ClassRogue, protocol.ClassMage} {
		me, err := w.Join("", "e-"+c, c, protocol.SpeciesHuman)
		if err != nil {
			t.Fatalf("Join %s: %v", c, err)
		}

		for _, e := range w.SnapshotFor(me.Token).Entities {
			if e.ID == me.EntityID {
				pools[c] = e.MaxEnergy
			}
		}
	}

	if got, want := pools[protocol.ClassMage] > pools[protocol.ClassRogue], true; got != want {
		t.Errorf("mage pool %d vs rogue %d: want the mage larger", pools[protocol.ClassMage], pools[protocol.ClassRogue])
	}

	if got, want := pools[protocol.ClassRogue] > pools[protocol.ClassFighter], true; got != want {
		t.Errorf("rogue pool %d vs fighter %d: want the rogue larger",
			pools[protocol.ClassRogue], pools[protocol.ClassFighter])
	}
}

// TestActiveSpendsEnergyAndADryCasterIsRefused: the cost is real, and running
// out is a refusal rather than a free cast.
func TestActiveSpendsEnergyAndADryCasterIsRefused(t *testing.T) {
	t.Parallel()

	w := newWorld()

	origin := protocol.Hex{Q: 0, R: 0}
	id, token := w.PlaceEntityForTest(origin)
	w.SetSkillStateForTest(id, []string{skillSurvivalistID, skillSecondWindID}, 0, 1)

	before := w.EnergyForTest(id)

	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill, SkillID: skillSecondWindID,
	}); err != nil {
		t.Fatalf("second wind with a full pool: %v", err)
	}

	w.ResolveTurnForTest()

	if got := w.EnergyForTest(id); got >= before {
		t.Errorf("energy after casting = %d, want less than %d", got, before)
	}

	// Now empty the pool: the same cast must be refused, not discounted.
	w.SetEnergyForTest(id, 0)
	w.SetActiveReadyForTest(id, skillSecondWindID)

	if got, want := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill, SkillID: skillSecondWindID,
	}), game.ErrNotEnoughEnergy; !errors.Is(got, want) {
		t.Errorf("cast with an empty pool = %v, want %v", got, want)
	}
}

// TestEvadeCostsNoEnergy: the panic button stays affordable. An escape you
// cannot pay for is not an escape, so evade is priced at zero deliberately.
func TestEvadeCostsNoEnergy(t *testing.T) {
	t.Parallel()

	w := newWorld()

	origin := protocol.Hex{Q: 0, R: 0}
	id, token := w.PlaceEntityForTest(origin)
	w.SetEnergyForTest(id, 0)

	target := protocol.Hex{Q: 1, R: 0}
	clearLine(w, origin, target)

	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
		SkillID: skillEvadeID, Target: target,
	}); err != nil {
		t.Fatalf("evade on an empty pool = %v, want nil", err)
	}
}

// TestEnergyRegeneratesEveryTurn pins the recovery that makes a long fight
// playable — and #322 decision 11's half of it that HP does not share out of
// combat: both pools tick, but energy is the one sized to be spent.
func TestEnergyRegeneratesEveryTurn(t *testing.T) {
	t.Parallel()

	w := newWorld()

	id, _ := w.PlaceEntityForTest(protocol.Hex{Q: 0, R: 0})
	w.SetEnergyForTest(id, 0)

	w.ResolveTurnForTest()

	if got, want := w.EnergyForTest(id), protocol.EnergyRegenPerTurn; got != want {
		t.Errorf("energy after one turn from empty = %d, want %d", got, want)
	}
}
