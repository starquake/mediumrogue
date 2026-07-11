package integration_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// questReachKind mirrors internal/game's unexported "reach" quest-kind
// literal — protocol.QuestView.Kind is a plain wire string ("kill"/"reach"),
// not an exported constant, so the wire contract is the only source of truth
// here.
const questReachKind = "reach"

// questInBundle opens a fresh /api/events connection (same rationale as
// party_test.go's entityInBundle: the frozen clock these tests use never
// emits a second frame on an existing stream) and returns the quest with id.
func questInBundle(t *testing.T, ts *httptest.Server, id int64) protocol.QuestView {
	t.Helper()

	bundle := decodeBundle(t, bufio.NewReader(get(t, ts, "/api/events").Body))

	q, ok := questByID(bundle, id)
	if !ok {
		t.Fatalf("quest %d not found in turn bundle", id)
	}

	return q
}

// questByID returns the quest with id from bundle, if present.
func questByID(bundle protocol.TurnEvent, id int64) (protocol.QuestView, bool) {
	for _, q := range bundle.Quests {
		if q.ID == id {
			return q, true
		}
	}

	return protocol.QuestView{}, false
}

// firstAvailableQuest returns the first available quest on the board.
func firstAvailableQuest(t *testing.T, bundle protocol.TurnEvent) protocol.QuestView {
	t.Helper()

	for _, q := range bundle.Quests {
		if q.State == protocol.QuestAvailable {
			return q
		}
	}

	t.Fatal("no available quest in turn bundle")

	return protocol.QuestView{}
}

// closestReachQuest returns the available reach quest whose GoalHex is
// nearest to from — the default harness world (radius 12) seeds reach goals
// at least 8 hexes out, so picking the closest keeps the walk in the test
// short.
func closestReachQuest(t *testing.T, bundle protocol.TurnEvent, from protocol.Hex) protocol.QuestView {
	t.Helper()

	var best protocol.QuestView

	bestDist := -1

	for _, q := range bundle.Quests {
		if q.Kind != questReachKind || q.State != protocol.QuestAvailable {
			continue
		}

		if d := hexDistance(from, q.GoalHex); bestDist == -1 || d < bestDist {
			best, bestDist = q, d
		}
	}

	if bestDist == -1 {
		t.Fatal("no available reach quest in turn bundle")
	}

	return best
}

// TestQuestBoardOnWire: a fresh world's very first turn bundle carries the
// whole seeded board (6 quests) and every one starts available.
func TestQuestBoardOnWire(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)

	bundle := decodeBundle(t, bufio.NewReader(get(t, ts, "/api/events").Body))

	if got, want := len(bundle.Quests), 6; got != want {
		t.Fatalf("len(bundle.Quests) = %d, want %d", got, want)
	}

	for _, q := range bundle.Quests {
		if got, want := q.State, protocol.QuestAvailable; got != want {
			t.Errorf("quest #%d (%s) state = %q, want %q", q.ID, q.Name, got, want)
		}
	}
}

// TestQuestTakeAndAbandonOverHTTP: /quest claims the first available board
// entry (202 + a "took quest" system announcement, holder = me on the wire),
// and /abandon returns it to the board with progress reset and no holder.
func TestQuestTakeAndAbandonOverHTTP(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)

	me := join(t, ts, "")

	stream := get(t, ts, "/api/events?token="+me.Token)
	reader := bufio.NewReader(stream.Body)

	bundle := decodeBundle(t, reader)
	q := firstAvailableQuest(t, bundle)

	resp := postJSON(t, ts, "/api/chat",
		protocol.ChatRequest{Token: me.Token, Text: fmt.Sprintf("/quest %d", q.ID)})
	if got, want := resp.StatusCode, http.StatusAccepted; got != want {
		t.Fatalf("/quest status = %d, want %d", got, want)
	}

	took := readSystemChat(t, reader)
	if got, want := took.Sender, systemSenderName; got != want {
		t.Errorf("take announcement sender = %q, want %q", got, want)
	}

	if got, want := took.Text, "took quest"; !strings.Contains(got, want) {
		t.Errorf("take announcement text = %q, should contain %q", got, want)
	}

	taken := questInBundle(t, ts, q.ID)
	if got, want := taken.State, protocol.QuestTaken; got != want {
		t.Errorf("state after take = %q, want %q", got, want)
	}

	if got, want := taken.HolderEntityID, me.EntityID; got != want {
		t.Errorf("HolderEntityID after take = %d, want %d", got, want)
	}

	resp = postJSON(t, ts, "/api/chat",
		protocol.ChatRequest{Token: me.Token, Text: fmt.Sprintf("/abandon %d", q.ID)})
	if got, want := resp.StatusCode, http.StatusAccepted; got != want {
		t.Fatalf("/abandon status = %d, want %d", got, want)
	}

	abandoned := readSystemChat(t, reader)
	if got, want := abandoned.Text, "abandoned quest"; !strings.Contains(got, want) {
		t.Errorf("abandon announcement text = %q, should contain %q", got, want)
	}

	back := questInBundle(t, ts, q.ID)
	if got, want := back.State, protocol.QuestAvailable; got != want {
		t.Errorf("state after abandon = %q, want %q", got, want)
	}

	if got, want := back.Progress, 0; got != want {
		t.Errorf("progress after abandon = %d, want %d", got, want)
	}

	if got, want := back.HolderEntityID, int64(0); got != want {
		t.Errorf("HolderEntityID after abandon = %d, want %d", got, want)
	}
}

// TestQuestErrorsOverHTTP: a nonexistent quest id, a non-numeric id, and
// abandoning with no active quest all reject as 422s, not 202s.
func TestQuestErrorsOverHTTP(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)

	me := join(t, ts, "")

	cases := []struct {
		name, text string
	}{
		{"unknown id", "/quest 999"},
		{"non-numeric id", "/quest abc"},
		{"abandon non-numeric id", "/abandon abc"},
		{"abandon not held", "/abandon 1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resp := postJSON(t, ts, "/api/chat", protocol.ChatRequest{Token: me.Token, Text: tc.text})
			if got, want := resp.StatusCode, http.StatusUnprocessableEntity; got != want {
				t.Errorf("%q status = %d, want %d", tc.text, got, want)
			}
		})
	}
}

// TestReachQuestCompletesWithXPOverHTTP exercises the headline reach-quest
// path over real HTTP/SSE: join as a dwarf (no human +XP% passive, so the
// reward is the exact RewardXP off the wire), take the closest reach quest,
// queue a single move intent at its GoalHex (the server pathfinds and walks
// one hex per resolved turn — see World.SubmitIntent), and poll the stream
// until the quest flips to completed, a "Quest complete" system announcement
// lands, and XP has risen by exactly RewardXP. The two signals (turn bundle
// state, chat announcement) can arrive on either side of the same wall-clock
// resolution, so both are tracked independently and only checked together
// once both have shown up — not gated on a single frame carrying both.
//
//nolint:paralleltest // serial by design (#22): tick loop must not be CPU-starved by parallel siblings.
func TestReachQuestCompletesWithXPOverHTTP(t *testing.T) {
	ts := startServer(t, 15*time.Millisecond, time.Hour)

	me := joinSpecies(t, ts, protocol.SpeciesDwarf)

	stream := get(t, ts, "/api/events?token="+me.Token)
	reader := bufio.NewReader(stream.Body)

	first := decodeBundle(t, reader)

	myEntity, ok := entityOf(first, me.EntityID)
	if !ok {
		t.Fatal("joined player missing from first turn bundle")
	}

	startXP := myEntity.XP

	goal := closestReachQuest(t, first, myEntity.Hex)

	resp := postJSON(t, ts, "/api/chat",
		protocol.ChatRequest{Token: me.Token, Text: fmt.Sprintf("/quest %d", goal.ID)})
	if got, want := resp.StatusCode, http.StatusAccepted; got != want {
		t.Fatalf("/quest status = %d, want %d", got, want)
	}

	took := readSystemChat(t, reader)
	if got, want := took.Text, "took quest"; !strings.Contains(got, want) {
		t.Errorf("take announcement text = %q, should contain %q", got, want)
	}

	postIntent(t, ts, me, goal.GoalHex)

	deadline := time.Now().Add(20 * time.Second)

	var (
		lastEntity           protocol.Entity
		lastQuest            protocol.QuestView
		sawComplete, sawChat bool
	)

	for time.Now().Before(deadline) && (!sawComplete || !sawChat) {
		frame, ok := readFrameWithin(t, reader, frameReadTimeout)
		if !ok {
			t.Fatal("no frame arrived before timeout")
		}

		switch frame.event {
		case protocol.EventChat:
			var msg protocol.ChatMessage
			if err := json.Unmarshal([]byte(frame.data), &msg); err != nil {
				t.Fatalf("unmarshal chat frame %q: %v", frame.data, err)
			}

			if strings.Contains(msg.Text, "Quest complete") {
				sawChat = true
			}
		case protocol.EventTurn:
			var bundle protocol.TurnEvent
			if err := json.Unmarshal([]byte(frame.data), &bundle); err != nil {
				t.Fatalf("unmarshal turn frame %q: %v", frame.data, err)
			}

			e, ok := entityOf(bundle, me.EntityID)
			if !ok {
				t.Fatal("joined player missing from turn bundle")
			}

			lastEntity = e

			q, ok := questByID(bundle, goal.ID)
			if !ok {
				t.Fatalf("quest %d missing from turn bundle", goal.ID)
			}

			lastQuest = q
			if q.State == protocol.QuestCompleted {
				sawComplete = true
			}
		}
	}

	if !sawComplete || !sawChat {
		t.Fatalf("reach quest never completed before deadline: last state=%q progress=%d chat seen=%v",
			lastQuest.State, lastQuest.Progress, sawChat)
	}

	if got, want := lastEntity.XP, startXP+goal.RewardXP; got != want {
		t.Errorf("XP after quest complete = %d, want %d (start %d + reward %d)",
			got, want, startXP, goal.RewardXP)
	}
}
