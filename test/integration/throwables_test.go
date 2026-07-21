package integration_test

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
	"github.com/starquake/mediumrogue/internal/server"
)

// throwables_test.go (#271): the targeted-consumable intents over real HTTP —
// throw and recall reach the handler, resolve or reject, and every rejection
// is a 422 (never a 500). The starter-consumables harness hands the joined
// player a flask and a scroll deterministically.

const (
	itFlaskOfFireDef    = "flask-of-fire"
	itScrollOfRecallDef = "scroll-of-recall"
)

// startServerWithStarterConsumables boots the handler tree over a world that
// grants the given consumable ids into every new player's backpack (#271, the
// STARTER_CONSUMABLES knob), so a joined player has a flask and a scroll to
// throw and recall with — no monster drop needed.
func startServerWithStarterConsumables(t *testing.T, ids ...string) *httptest.Server {
	t.Helper()

	ticks := hub.New()

	world := game.NewWorld(game.WorldConfig{
		Interval:           time.Hour,
		CombatPatience:     time.Minute,
		BubblePoll:         5 * time.Millisecond,
		DisconnectGrace:    testDisconnectGrace,
		WorldSeed:          0xC0FFEE,
		Radius:             12,
		Ticks:              ticks,
		StarterConsumables: ids,
	})

	return serveWorld(t, world, ticks, server.Deps{HeartbeatInterval: time.Hour})
}

// ownedItemID returns the instance id of the player's own backpack item with
// the given def, or 0.
func ownedItemID(bundle protocol.TurnEvent, entityID int64, defID string) int64 {
	e, ok := entityOf(bundle, entityID)
	if !ok {
		return 0
	}

	for _, it := range e.Items {
		if it.DefID == defID {
			return it.ID
		}
	}

	return 0
}

// TestThrowAndRecallOverHTTP drives the new targeted-consumable intents through
// the real handler: a flask thrown at a hex and a recall are accepted (202),
// while a bogus item id and a wrong-consumable id are rejected as 422 (not the
// 500 an unmapped sentinel would produce).
func TestThrowAndRecallOverHTTP(t *testing.T) {
	t.Parallel()

	ts := startServerWithStarterConsumables(t, itFlaskOfFireDef, itScrollOfRecallDef)
	me := join(t, ts, "")

	reader := bufio.NewReader(get(t, ts, "/api/events?token="+me.Token).Body)

	var flaskID, scrollID int64

	waitForBundle(t, reader, "starter consumables visible", func(b protocol.TurnEvent) bool {
		flaskID = ownedItemID(b, me.EntityID, itFlaskOfFireDef)
		scrollID = ownedItemID(b, me.EntityID, itScrollOfRecallDef)

		return flaskID != 0 && scrollID != 0
	})

	// A throw aimed at the player's own hex (in range, self is always visible)
	// is accepted — no enemy in the blast, but the ACTION is valid.
	if got, want := postJSON(t, ts, "/api/intent", protocol.IntentRequest{
		EntityID: me.EntityID, Token: me.Token, Kind: protocol.IntentThrow, ItemID: flaskID, Target: me.Hex,
	}).StatusCode, http.StatusAccepted; got != want {
		t.Errorf("throw status = %d, want %d", got, want)
	}

	// A recall (no target) is accepted.
	if got, want := postJSON(t, ts, "/api/intent", protocol.IntentRequest{
		EntityID: me.EntityID, Token: me.Token, Kind: protocol.IntentRecall, ItemID: scrollID,
	}).StatusCode, http.StatusAccepted; got != want {
		t.Errorf("recall status = %d, want %d", got, want)
	}

	// A throw of an item the player does not own is a 422, not a 500.
	if got, want := postJSON(t, ts, "/api/intent", protocol.IntentRequest{
		EntityID: me.EntityID, Token: me.Token, Kind: protocol.IntentThrow, ItemID: 999999, Target: me.Hex,
	}).StatusCode, http.StatusUnprocessableEntity; got != want {
		t.Errorf("throw of unowned item status = %d, want %d", got, want)
	}

	// A recall naming the flask (not a recall scroll) is a 422.
	if got, want := postJSON(t, ts, "/api/intent", protocol.IntentRequest{
		EntityID: me.EntityID, Token: me.Token, Kind: protocol.IntentRecall, ItemID: flaskID,
	}).StatusCode, http.StatusUnprocessableEntity; got != want {
		t.Errorf("recall of a non-recall item status = %d, want %d", got, want)
	}
}
