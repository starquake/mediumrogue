package game_test

import (
	"errors"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// recall_test.go (#271, slice 5): the scroll of recall — the targeted-consumable
// action path, end to end through the real resolution pipeline. The thrown
// flask that shared this path was removed in #352.

const (
	scrollOfRecallID = "scroll-of-recall"
	healingPotionID  = "healing-potion"
	// burningEffectID is read by embernova_test.go — the nova's DoT is the same
	// registered effect the removed flask used (#352).
	burningEffectID = "burning"
)

// recallIntent builds a "recall" IntentRequest: ItemID names the scroll; no
// target (recall teleports to a server-chosen safe hex).
func recallIntent(id int64, token string, itemID int64) protocol.IntentRequest {
	return protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentRecall, ItemID: itemID,
	}
}

// backpackCount sums the units of defID across an entity's backpack.
func backpackCount(w *game.World, id int64, defID string) int {
	total := 0

	for _, e := range w.BackpackForTest(id) {
		if e.DefID == defID {
			total += e.Count
		}
	}

	return total
}

// TestRecallTeleportsToSanctuaryAndSpendsTheScroll: a recall teleports the
// user from the field back into the shared sanctuary and consumes the scroll.
func TestRecallTeleportsToSanctuaryAndSpendsTheScroll(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	origin := protocol.Hex{Q: 0, R: 0}
	fieldHex := protocol.Hex{Q: 9, R: 0} // well beyond SanctuaryRadius (5)

	id, token := w.PlaceEntityForTest(fieldHex)
	scroll := w.GrantItemForTest(id, scrollOfRecallID)

	if err := w.SubmitIntent(recallIntent(id, token, scroll)); err != nil {
		t.Fatalf("SubmitIntent(recall): %v", err)
	}

	w.ResolveCombatOnlyForTest()

	dest := hexOfSnap(w.Snapshot(), id)
	if dest == fieldHex {
		t.Fatalf("player did not move on recall (still at %v)", dest)
	}

	if got := game.HexDistance(origin, dest); got > protocol.SanctuaryRadius {
		t.Errorf("recall landed at %v, distance %d from origin, want <= %d (the sanctuary)",
			dest, got, protocol.SanctuaryRadius)
	}

	if got := backpackCount(w, id, scrollOfRecallID); got != 0 {
		t.Errorf("scroll count after recall = %d, want 0 (consumed)", got)
	}
}

// TestStarterConsumablesGrantedAtJoin: the config-gated starter kit (#271)
// lands each configured consumable in a new player's backpack, so the client
// feature is deterministically usable in e2e without a monster drop.
func TestStarterConsumablesGrantedAtJoin(t *testing.T) {
	t.Parallel()

	w := game.NewWorld(game.WorldConfig{
		Interval:           time.Hour,
		CombatPatience:     time.Second,
		BubblePoll:         time.Millisecond,
		DisconnectGrace:    time.Minute,
		WorldSeed:          0xC0FFEE,
		Radius:             12,
		Ticks:              hub.New(),
		StarterConsumables: []string{scrollOfRecallID},
	})

	resp, err := w.Join("", "kitted", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	if got := backpackCount(w, resp.EntityID, scrollOfRecallID); got != 1 {
		t.Errorf("scroll count at join = %d, want 1", got)
	}
}

// TestRecallNonRecallItemRejected: only a recall scroll recalls
// (ErrNotRecallable).
func TestRecallNonRecallItemRejected(t *testing.T) {
	t.Parallel()

	w := newWorld()

	id, token := w.PlaceEntityForTest(protocol.Hex{Q: 0, R: 0})
	// Any consumable that is not a scroll; the flask this used to name was
	// removed in #352.
	potion := w.GrantItemForTest(id, healingPotionID)

	if got, want := w.SubmitIntent(recallIntent(id, token, potion)), game.ErrNotRecallable; !errors.Is(got, want) {
		t.Errorf("recall of a non-recall item err = %v, want %v", got, want)
	}
}
