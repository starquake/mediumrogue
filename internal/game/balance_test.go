package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// The balance harness (#283) measures through the real resolution code, so
// these tests pin two properties the reports depend on: byte-determinism
// (same config, same numbers — the whole point of a seeded harness) and
// sanity (a level-1 fighter beats a rat, the weakest kind, nearly always —
// if THIS moves, either content moved deliberately or the harness broke).

func TestRunDuelIsDeterministic(t *testing.T) {
	t.Parallel()

	cfg := game.DuelConfig{
		Seed: 42, Class: protocol.ClassFighter, Level: 1, MonsterKind: kindRat,
	}

	if got, want := game.RunDuel(cfg), game.RunDuel(cfg); got != want {
		t.Errorf("RunDuel twice = %+v, want %+v (same seed must reproduce exactly)", got, want)
	}
}

func TestFighterBeatsRatAtLevelOne(t *testing.T) {
	t.Parallel()

	const duels = 20

	wins := 0

	for i := range duels {
		r := game.RunDuel(game.DuelConfig{

			Seed: uint64(1000 + i), Class: protocol.ClassFighter, Level: 1, MonsterKind: kindRat,
		})
		if r.PlayerWon {
			wins++
		}

		if got, want := r.Turns, 0; got <= want {
			t.Fatalf("duel %d Turns = %d, want > %d (a duel that never stepped measured nothing)", i, got, want)
		}
	}

	// Sanity, not a tuning band (those are the guardrail tests): the weakest
	// kind against the sturdiest starter class should hardly ever win.
	if got, want := wins, duels*9/10; got < want {
		t.Errorf("fighter vs rat wins = %d/%d, want >= %d", got, duels, want)
	}
}

func TestRunDuelMatrixShapeAndDeterminism(t *testing.T) {
	t.Parallel()

	cfg := game.MatrixConfig{
		BaseSeed: 7,
		Duels:    3,
		Classes:  []string{protocol.ClassFighter},
		Kinds:    []string{kindRat, "wolf"},
		Levels:   []int{1},
	}

	a := game.RunDuelMatrix(cfg)
	b := game.RunDuelMatrix(cfg)

	if got, want := len(a.Cells), 2; got != want {
		t.Fatalf("len(Cells) = %d, want %d", got, want)
	}

	for i := range a.Cells {
		if got, want := a.Cells[i], b.Cells[i]; got != want {
			t.Errorf("cell %d differs across identical runs:\n got %+v\nwant %+v", i, got, want)
		}
	}

	for _, c := range a.Cells {
		if got, want := c.PlayerWins+c.MonsterWins+c.Draws, c.Duels; got != want {
			t.Errorf("cell %s/%s outcomes = %d, want %d (every duel ends exactly one way)", c.Class, c.Kind, got, want)
		}
	}
}
