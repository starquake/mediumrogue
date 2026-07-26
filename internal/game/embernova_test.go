package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// embernova_test.go (#300): the area-damage active kind, end to end.
//
// This is the kind that resolves in the ATTACK phase rather than the move
// phase — it needs the turn's rng and shared damage map, and it must land
// against pre-move positions like every other hit. The point of routing it
// through resolveAoELocked is that faction filtering, the damage pipeline and
// the buffered on-hit rider arrive without being written, so that is what
// these pin.

const (
	skillEmberNovaID = "ember-nova"
	skillKindlerID   = "kindler"
)

// Ember Nova's authored payload (skills.go); duplicated here so a tuning change
// fails these pins loudly and on purpose.
const (
	novaRange     = 4
	novaDamage    = 5
	novaBurnMag   = -2
	novaBurnTurns = 2
)

// novaIntent builds a "use skill" IntentRequest aimed at a hex.
func novaIntent(id int64, token string, target protocol.Hex) protocol.IntentRequest {
	return protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
		SkillID: skillEmberNovaID, Target: target,
	}
}

// TestEmberNovaBlastsAndBurns: the whole point. Damage lands this turn, the
// burning rider is buffered and first bites next turn — the same contract a
// thrown flask has, because it is the same code.
func TestEmberNovaBlastsAndBurns(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	origin := protocol.Hex{Q: 0, R: 0}
	monsterHex := protocol.Hex{Q: 3, R: 0}
	clearLine(w, origin, protocol.Hex{Q: 1, R: 0}, protocol.Hex{Q: 2, R: 0}, monsterHex)

	id, token := w.PlaceEntityForTest(origin)
	w.SetSkillStateForTest(id, []string{skillCombatTrainingID, skillKindlerID, skillEmberNovaID}, 0, 1)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	if err := w.SubmitIntent(novaIntent(id, token, monsterHex)); err != nil {
		t.Fatalf("SubmitIntent(use-skill): %v", err)
	}

	w.ResolveCombatOnlyForTest()

	// Kindler's +10% fire folds in through the ordinary pipeline: 5 × 1.10 = 5
	// (integer), so the pin is the base — what matters is that it is NOT more
	// than a fire-skilled caster's own card allows, and not zero.
	hp := w.HPForTest(monsterID)
	if got, want := hp, protocol.MonsterMaxHP; got >= want {
		t.Errorf("monster HP after nova = %d, want < %d", got, want)
	}

	if got, want := hp, protocol.MonsterMaxHP-novaDamage; got > want {
		t.Errorf("monster HP after nova = %d, want <= %d (at least the base blast)", got, want)
	}

	mag, turns, ok := w.EffectForTest(monsterID, burningEffectID)
	if !ok {
		t.Fatal("monster has no burning effect after the nova")
	}

	if got, want := mag, novaBurnMag; got != want {
		t.Errorf("burning magnitude = %d, want %d", got, want)
	}

	// Full duration, undrained: the rider is buffered past this turn's tick.
	if got, want := turns, novaBurnTurns; got != want {
		t.Errorf("burning turns = %d, want %d (buffered, bites next turn)", got, want)
	}

	if got := w.ActiveReadyTurnForTest(id, skillEmberNovaID); got == 0 {
		t.Error("ember nova did not start its cooldown")
	}
}

// TestEmberNovaHitsEveryHostileInTheBlastAndNoAlly: faction filtering and the
// blast radius, both inherited from resolveAoELocked. An AoE that could hit a
// friend would be a different game (#300 Q1, satisfied by construction).
func TestEmberNovaHitsEveryHostileInTheBlastAndNoAlly(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	origin := protocol.Hex{Q: 0, R: 0}
	centre := protocol.Hex{Q: 3, R: 0}
	// The blast centre plus two of its neighbours, all inside aoeRadius 1.
	near := protocol.Hex{Q: 4, R: 0}
	alsoNear := protocol.Hex{Q: 3, R: 1}
	clearLine(w, origin, protocol.Hex{Q: 1, R: 0}, protocol.Hex{Q: 2, R: 0}, centre, near, alsoNear)

	id, token := w.PlaceEntityForTest(origin)
	w.SetSkillStateForTest(id, []string{skillCombatTrainingID, skillKindlerID, skillEmberNovaID}, 0, 1)

	victims := []int64{
		w.PlaceMonsterForTest(centre),
		w.PlaceMonsterForTest(near),
		w.PlaceMonsterForTest(alsoNear),
	}

	// An ally standing in the same blast. Placed after the monsters so the
	// blast genuinely covers them both.
	allyHex := protocol.Hex{Q: 2, R: 0}
	allyID, _ := w.PlaceEntityForTest(allyHex)
	allyBefore := w.HPForTest(allyID)

	if err := w.SubmitIntent(novaIntent(id, token, centre)); err != nil {
		t.Fatalf("SubmitIntent(use-skill): %v", err)
	}

	w.ResolveCombatOnlyForTest()

	for _, m := range victims {
		if got, want := w.HPForTest(m), protocol.MonsterMaxHP; got >= want {
			t.Errorf("blast victim %d HP = %d, want < %d (AoE always hits)", m, got, want)
		}
	}

	if got, want := w.HPForTest(allyID), allyBefore; got != want {
		t.Errorf("ally HP after nearby nova = %d, want %d (no friendly fire)", got, want)
	}
}

// TestEmberNovaIsRefusedOutOfRange: the reach gate. Without it a blast would
// out-range every weapon in the game.
func TestEmberNovaIsRefusedOutOfRange(t *testing.T) {
	t.Parallel()

	w := newWorld()

	origin := protocol.Hex{Q: 0, R: 0}
	id, token := w.PlaceEntityForTest(origin)
	w.SetSkillStateForTest(id, []string{skillCombatTrainingID, skillKindlerID, skillEmberNovaID}, 0, 1)

	far := walkableHexAtDistance(t, w, origin, novaRange+1, novaRange+2)
	clearSightLine(t, w, origin, far)

	if err := w.SubmitIntent(novaIntent(id, token, far)); err == nil {
		t.Fatal("a nova beyond its range was accepted")
	}
}
