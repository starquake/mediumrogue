package game_test

import (
	"errors"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// refused_route_test.go (#343): a REFUSED action must not leave the previous
// auto-walk route queued.
//
// commitActiveLocked clears e.path — that is the decided "latest intent wins"
// rule — but it only runs once validation has passed. Every refusal returns
// before it, so the route survived and the player kept walking the old way
// while being told their action failed.
func TestARefusedActiveCancelsTheStandingRoute(t *testing.T) {
	t.Parallel()

	w := newWorld()

	origin := protocol.Hex{Q: 0, R: 0}
	id, token := w.PlaceEntityForTest(origin)
	w.SetSkillStateForTest(id, []string{skillSurvivalistID, skillEvadeID}, 0, 1)

	route := []protocol.Hex{{Q: 1, R: 0}, {Q: 2, R: 0}, {Q: 3, R: 0}}
	w.SetPathForTest(id, route)

	// Far outside EvadeRangeHex, so this is refused at submit.
	tooFar := protocol.Hex{Q: 0, R: 20}

	err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
		SkillID: skillEvadeID, Target: tooFar,
	})
	if got, want := err, game.ErrOutOfRange; !errors.Is(got, want) {
		t.Fatalf("SubmitIntent err = %v, want %v", got, want)
	}

	if got := w.PathForTest(id); len(got) != 0 {
		t.Errorf("a refused evade left %d hexes of route queued (%v) — the player "+
			"is told the action failed and then walks the old way anyway", len(got), got)
	}
}

// TestARefusedAttackCancelsTheStandingRoute is the same rule on the other
// targeted intent — a stale or out-of-range attack target is the #130/#133 422
// this repo already has form for.
func TestARefusedAttackCancelsTheStandingRoute(t *testing.T) {
	t.Parallel()

	w := newWorld()

	origin := protocol.Hex{Q: 0, R: 0}
	id, token := w.PlaceEntityForTest(origin)

	route := []protocol.Hex{{Q: 1, R: 0}, {Q: 2, R: 0}}
	w.SetPathForTest(id, route)

	// No such entity: refused at submit.
	err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentAttack,
		Target: protocol.Hex{Q: 1, R: 0}, TargetEntityID: 999999,
	})
	if err == nil {
		t.Fatal("attack on a non-existent target was accepted")
	}

	if got := w.PathForTest(id); len(got) != 0 {
		t.Errorf("a refused attack left %d hexes of route queued (%v)", len(got), got)
	}
}

// TestAnIncidentalIntentKeepsTheStandingRoute pins the OTHER half deliberately.
// Drinking or swapping kit while travelling must not strand you: only intents
// that redirect what you are DOING cancel the walk. Without this, the fix above
// would drift into "any intent stops you", which is a different and worse rule.
func TestAnIncidentalIntentKeepsTheStandingRoute(t *testing.T) {
	t.Parallel()

	w := newWorld()

	origin := protocol.Hex{Q: 0, R: 0}
	id, token := w.PlaceEntityForTest(origin)

	route := []protocol.Hex{{Q: 1, R: 0}, {Q: 2, R: 0}}
	w.SetPathForTest(id, route)

	// Refused (no such item), and incidental either way.
	_ = w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentEquip, ItemID: 999999,
	})

	if got, want := len(w.PathForTest(id)), len(route); got != want {
		t.Errorf("an equip intent cancelled the walk: %d hexes left, want %d", got, want)
	}
}
