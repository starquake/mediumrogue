package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// Guardrails (#283 decision 5): COARSE bounds at the extremes, proposed from
// the first real report (2026-07-24: max L1 threat 10.29 mage/hydra; every
// class beat the rat 100%; starter-kind threats all <= 0.86; solo sim 5.83
// deaths/100 turns vs 0.09 at 15 players). They move only when content moves
// — a failure here means a content change crossed a line the maintainer set,
// not that a number wobbled. Tight per-cell bands are deliberately NOT
// asserted (report-first; tuning churn is the failure mode to avoid).

func TestGuardrailEveryClassBeatsTheRat(t *testing.T) {
	t.Parallel()

	rep := game.RunDuelMatrix(game.MatrixConfig{
		BaseSeed: 283, Duels: 20, Levels: []int{1}, Kinds: []string{kindRat},
	})

	for _, c := range rep.Cells {
		// >= 95%: the weakest kind must stay a safe first fight for every
		// class (observed: 100% across the board).
		if got, want := c.PlayerWins, c.Duels*19/20; got < want {
			t.Errorf("%s vs rat L1 wins = %d/%d, want >= %d", c.Class, got, c.Duels, want)
		}
	}
}

func TestGuardrailStarterKindsStayMild(t *testing.T) {
	t.Parallel()

	rep := game.RunDuelMatrix(game.MatrixConfig{
		BaseSeed: 283, Duels: 20, Levels: []int{1},
		Kinds: []string{kindRat, kindWolf, "goblin"},
	})

	for _, c := range rep.Cells {
		// Starter kinds must never project as outright deadly for a fresh
		// character (observed max: 0.86 mage/wolf; 2.0 leaves content
		// headroom while catching a runaway).
		if got, want := c.Threat, 2.0; got >= want {
			t.Errorf("%s vs %s L1 threat = %.2f, want < %.1f", c.Class, c.Kind, got, want)
		}
	}
}

func TestGuardrailThreatCeiling(t *testing.T) {
	t.Parallel()

	rep := game.RunDuelMatrix(game.MatrixConfig{
		BaseSeed: 283, Duels: 10, Levels: []int{1},
	})

	for _, c := range rep.Cells {
		// The scariest 1v1 in the game stays bounded (observed max: 10.29
		// mage/hydra). A threat beyond 15 means a kind became effectively
		// unfightable solo — flag it, whatever the tier intent.
		if got, want := c.Threat, 15.0; got >= want {
			t.Errorf("%s vs %s L1 threat = %.2f, want < %.1f", c.Class, c.Kind, got, want)
		}
	}
}

func TestGuardrailSoloIsDangerousAndPartiesAreSafer(t *testing.T) {
	t.Parallel()

	rep := game.RunPartySim(game.PartySimConfig{
		BaseSeed: 283, Sizes: []int{1, 15}, Seeds: 1, Turns: 100,
	})

	solo, party := rep.Sizes[0], rep.Sizes[1]

	// The boring floor: a solo player who never risks death has no game
	// (observed: 5.83/100 turns).
	if got, want := solo.DeathsPer100, 0.0; got <= want {
		t.Errorf("solo DeathsPer100 = %.2f, want > %.1f (solo play should carry real risk)", got, want)
	}

	// The direction of safety: more players must not be MORE lethal per
	// player-turn (observed: 0.09 vs 5.83 — a steep drop).
	if got, want := party.DeathsPer100, solo.DeathsPer100; got > want {
		t.Errorf("15-player DeathsPer100 = %.2f, want <= solo's %.2f", got, want)
	}

	if got, want := protocol.MaxPlayers, 15; got < want {
		t.Fatalf("MaxPlayers = %d, want >= %d (the sim's largest size must be joinable)", got, want)
	}
}
