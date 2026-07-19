package game //nolint:testpackage // white-box: bubbleViewOf is unexported.

import (
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestBubbleViewWaitingIsEmptyNotNilWhenEveryoneIsReady: the exact crash
// reported on development, 2026-07-19 —
// `can't access property "map", waitingForIds is null`.
//
// This is a WHITE-BOX test on the render function rather than a full board,
// because the state is transient end-to-end: submitting the last intent
// resolves the bubble turn and clears `ready` in the same call, so a
// black-box board almost never observes "everyone locked in, not yet
// resolved". The branch still ships to clients constantly — it is the render
// used every time a bundle goes out between lock-in and resolution.
func TestBubbleViewWaitingIsEmptyNotNilWhenEveryoneIsReady(t *testing.T) {
	t.Parallel()

	w := NewWorld(time.Hour, time.Minute, time.Second, time.Minute, 1, 4, hub.New())

	player := &entity{id: 1, kind: protocol.EntityPlayer, hp: 10, maxHP: 10}
	monster := &entity{id: 2, kind: protocol.EntityMonster, hp: 5, maxHP: 5, monsterKind: idKindWolf}
	w.entities[1] = player
	w.entities[2] = monster

	b := &bubble{
		id:      1,
		members: map[int64]struct{}{1: {}, 2: {}},
		ready:   map[int64]struct{}{1: {}}, // the player has locked in
	}

	view := w.bubbleViewLocked(b, w.now())

	if view.WaitingForIDs == nil {
		t.Error("WaitingForIDs is nil when everyone is ready — marshals to JSON null, " +
			"and the client's generated type says it is an array (see wire_nil_test.go)")
	}

	if got, want := len(view.WaitingForIDs), 0; got != want {
		t.Errorf("WaitingForIDs = %v, want empty", view.WaitingForIDs)
	}

	if view.MemberIDs == nil {
		t.Error("MemberIDs is nil")
	}
}
