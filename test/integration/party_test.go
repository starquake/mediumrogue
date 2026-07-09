package integration_test

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// entityInBundle opens a fresh /api/events connection (a joined client always
// gets the current snapshot as its first frame — see events.go's writeTurn
// called before the stream's select loop) and returns the entity with id, so
// each call observes party-state mutations that happened after any earlier
// connection was opened. Reusing a single long-lived stream would work only
// for the first lookup: with the frozen clock these tests use, no second turn
// frame ever arrives on it, so a second read would just time out.
func entityInBundle(t *testing.T, ts *httptest.Server, id int64) protocol.Entity {
	t.Helper()

	resp := get(t, ts, "/api/events")
	reader := bufio.NewReader(resp.Body)

	frame, ok := readFrameWithin(t, reader, frameReadTimeout)
	if !ok {
		t.Fatal("no turn frame arrived on fresh /api/events connection")
	}

	if got, want := frame.event, protocol.EventTurn; got != want {
		t.Fatalf("event = %q, want %q", got, want)
	}

	var bundle protocol.TurnEvent
	if err := json.Unmarshal([]byte(frame.data), &bundle); err != nil {
		t.Fatalf("unmarshal turn frame %q: %v", frame.data, err)
	}

	for _, e := range bundle.Entities {
		if e.ID == id {
			return e
		}
	}

	t.Fatalf("entity %d not found in turn bundle", id)

	return protocol.Entity{}
}

// readSystemChat scans bob's stream for the next `event: chat` frame within
// frameReadTimeout, skipping every other frame along the way. Local to this
// file (rather than reusing chat_test.go's readChatWithin) so that helper's
// timeout parameter isn't given a fourth always-identical call site.
func readSystemChat(t *testing.T, r *bufio.Reader) protocol.ChatMessage {
	t.Helper()

	deadline := time.Now().Add(frameReadTimeout)

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatal("no chat frame arrived before timeout")
		}

		frame, ok := readFrameWithin(t, r, remaining)
		if !ok {
			t.Fatal("no chat frame arrived before timeout")
		}

		if frame.event != protocol.EventChat {
			continue
		}

		var msg protocol.ChatMessage
		if err := json.Unmarshal([]byte(frame.data), &msg); err != nil {
			t.Fatalf("unmarshal chat frame %q: %v", frame.data, err)
		}

		return msg
	}
}

// formParty joins alice and bob under distinct names, has alice invite bob
// over bob's stream, and has bob accept — returning both join responses once
// the "joined" system announcement has landed. Shared setup for the leave and
// invite/accept tests below.
func formParty(t *testing.T, ts *httptest.Server) (protocol.JoinResponse, protocol.JoinResponse) {
	t.Helper()

	alice := joinNamed(t, ts, "alice")
	bob := joinNamed(t, ts, "bob")

	stream := get(t, ts, "/api/events?token="+bob.Token)
	reader := bufio.NewReader(stream.Body)

	resp := postJSON(t, ts, "/api/chat", protocol.ChatRequest{Token: alice.Token, Text: "/invite bob"})
	if got, want := resp.StatusCode, http.StatusAccepted; got != want {
		t.Fatalf("/invite status = %d, want %d", got, want)
	}

	invited := readSystemChat(t, reader)

	if got, want := invited.Sender, "system"; got != want {
		t.Errorf("invite announcement sender = %q, want %q", got, want)
	}

	if got, want := invited.Text, "invited"; !strings.Contains(got, want) {
		t.Errorf("invite announcement text = %q, should contain %q", got, want)
	}

	resp = postJSON(t, ts, "/api/chat", protocol.ChatRequest{Token: bob.Token, Text: "/accept"})
	if got, want := resp.StatusCode, http.StatusAccepted; got != want {
		t.Fatalf("/accept status = %d, want %d", got, want)
	}

	joined := readSystemChat(t, reader)

	if got, want := joined.Sender, "system"; got != want {
		t.Errorf("accept announcement sender = %q, want %q", got, want)
	}

	if got, want := joined.Text, "joined"; !strings.Contains(got, want) {
		t.Errorf("accept announcement text = %q, should contain %q", got, want)
	}

	return alice, bob
}

// TestPartyInviteAcceptSharesPartyID: alice invites bob, bob accepts, and both
// land on the same non-zero PartyID in a fresh turn bundle.
func TestPartyInviteAcceptSharesPartyID(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)

	alice, bob := formParty(t, ts)

	aliceEntity := entityInBundle(t, ts, alice.EntityID)
	bobEntity := entityInBundle(t, ts, bob.EntityID)

	if got, want := aliceEntity.PartyID, bobEntity.PartyID; got != want {
		t.Errorf("alice.PartyID = %d, bob.PartyID = %d, want equal", got, want)
	}

	if aliceEntity.PartyID == 0 {
		t.Error("PartyID = 0, want non-zero after accept")
	}
}

// TestPartyLeaveClearsPartyID: once a pair has formed, leaving drops the
// leaver's PartyID back to 0.
func TestPartyLeaveClearsPartyID(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)

	_, bob := formParty(t, ts)

	resp := postJSON(t, ts, "/api/chat", protocol.ChatRequest{Token: bob.Token, Text: "/leave"})
	if got, want := resp.StatusCode, http.StatusAccepted; got != want {
		t.Fatalf("/leave status = %d, want %d", got, want)
	}

	bobEntity := entityInBundle(t, ts, bob.EntityID)
	if got, want := bobEntity.PartyID, int64(0); got != want {
		t.Errorf("bob.PartyID after leave = %d, want %d", got, want)
	}
}

// TestInviteUnknownNameRejected: inviting a name nobody is playing under is a
// 422, not a party mutation.
func TestInviteUnknownNameRejected(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)

	alice := joinNamed(t, ts, "alice")

	resp := postJSON(t, ts, "/api/chat", protocol.ChatRequest{Token: alice.Token, Text: "/invite ghost"})
	if got, want := resp.StatusCode, http.StatusUnprocessableEntity; got != want {
		t.Errorf("/invite ghost status = %d, want %d", got, want)
	}
}
