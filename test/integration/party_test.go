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

// systemSenderName is the Sender on a server-generated chat announcement
// (party ops, quest board events) — mirrors internal/server's unexported
// systemSender constant, which this black-box package cannot reference.
const systemSenderName = "system"

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

// readSystemChat is readChatWithin with the timeout treated as fatal, for the
// party and quest tests, where a missing announcement is never a legitimate
// outcome worth branching on.
//
// It used to duplicate readChatWithin's whole scan loop to avoid giving that
// helper's timeout parameter another always-identical call site. #385 removed
// the parameter, so the duplication had nothing left to buy and this delegates.
func readSystemChat(t *testing.T, r *bufio.Reader) protocol.ChatMessage {
	t.Helper()

	msg, ok := readChatWithin(t, r)
	if !ok {
		t.Fatal("no chat frame arrived before timeout")
	}

	return msg
}

// chatPost POSTs text to /api/chat as token and returns the status code — the
// party verbs are chat commands, so every one of these tests speaks over the
// same route a player's client does.
func chatPost(t *testing.T, ts *httptest.Server, token, text string) int {
	t.Helper()

	return postJSON(t, ts, "/api/chat", protocol.ChatRequest{Token: token, Text: text}).StatusCode
}

// formParty joins alice and bob under distinct names, has alice invite bob
// over bob's stream, and has bob accept — returning both join responses once
// the "joined" system announcement has landed. Shared setup for the leave and
// invite/accept tests below.
func formParty(t *testing.T, ts *httptest.Server) (leader, member protocol.JoinResponse) {
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

	if got, want := invited.Sender, systemSenderName; got != want {
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

	if got, want := joined.Sender, systemSenderName; got != want {
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

// errBody decodes a 4xx JSON error body (protocol.ErrorResponse) to its message.
func errBody(t *testing.T, resp *http.Response) string {
	t.Helper()

	var er protocol.ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		t.Fatalf("decode error body: %v", err)
	}

	return er.Error
}

// TestLeaveWhenNotInPartyRejected pins ErrNotInParty over HTTP: /leave with no
// party is a 422 carrying the sentinel text (previously unit-covered only).
func TestLeaveWhenNotInPartyRejected(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)
	alice := joinNamed(t, ts, "alice")

	resp := postJSON(t, ts, "/api/chat", protocol.ChatRequest{Token: alice.Token, Text: "/leave"})
	if got, want := resp.StatusCode, http.StatusUnprocessableEntity; got != want {
		t.Fatalf("/leave (no party) status = %d, want %d", got, want)
	}

	if got, want := errBody(t, resp), "not in a party"; !strings.Contains(got, want) {
		t.Errorf("error = %q, should contain %q", got, want)
	}
}

// TestUppercaseVerbRoutes: verbs are case-insensitive (cutVerb lowercases), so
// /LEAVE reaches the leave handler exactly like /leave — proven by the
// ErrNotInParty message coming back rather than the unknown-command path.
func TestUppercaseVerbRoutes(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)
	alice := joinNamed(t, ts, "alice")

	resp := postJSON(t, ts, "/api/chat", protocol.ChatRequest{Token: alice.Token, Text: "/LEAVE"})
	if got, want := resp.StatusCode, http.StatusUnprocessableEntity; got != want {
		t.Fatalf("/LEAVE status = %d, want %d", got, want)
	}

	if got, want := errBody(t, resp), "not in a party"; !strings.Contains(got, want) {
		t.Errorf("/LEAVE error = %q, should contain %q (case-insensitive routing)", got, want)
	}
}

// TestAcceptWhenAlreadyInPartyRejected pins ErrAlreadyInParty over HTTP: once
// alice and bob share a party, a re-invite + re-accept is a 422.
func TestAcceptWhenAlreadyInPartyRejected(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)
	alice, bob := formParty(t, ts)

	if got, want := postJSON(t, ts, "/api/chat",
		protocol.ChatRequest{Token: alice.Token, Text: "/invite bob"}).StatusCode, http.StatusAccepted; got != want {
		t.Fatalf("re-invite status = %d, want %d", got, want)
	}

	resp := postJSON(t, ts, "/api/chat", protocol.ChatRequest{Token: bob.Token, Text: "/accept"})
	if got, want := resp.StatusCode, http.StatusUnprocessableEntity; got != want {
		t.Fatalf("re-accept status = %d, want %d", got, want)
	}

	if got, want := errBody(t, resp), "already in that party"; !strings.Contains(got, want) {
		t.Errorf("error = %q, should contain %q", got, want)
	}
}

// TestPendingInviteRidesTheBundle drives #385's wire field over real HTTP: the
// invitee's own stream carries the pending invite, the inviter's does not, and
// accepting clears it.
//
// The unit tests already pin the same thing against the World directly. This
// exists because the field is only useful if it survives the per-viewer bundle
// build and the SSE encoding — a field the handler tree drops is a field the
// client never sees, and no world-level test can tell you that.
func TestPendingInviteRidesTheBundle(t *testing.T) {
	t.Parallel()

	ts := startServer(t, 20*time.Millisecond, time.Hour)

	alice := joinNamed(t, ts, "alice")
	bob := joinNamed(t, ts, "bob")

	bobStream := get(t, ts, "/api/events?token="+bob.Token)
	bobReader := bufio.NewReader(bobStream.Body)

	aliceStream := get(t, ts, "/api/events?token="+alice.Token)
	aliceReader := bufio.NewReader(aliceStream.Body)

	resp := postJSON(t, ts, "/api/chat", protocol.ChatRequest{Token: alice.Token, Text: "/invite bob"})
	if got, want := resp.StatusCode, http.StatusAccepted; got != want {
		t.Fatalf("/invite status = %d, want %d", got, want)
	}

	// awaitTurnFrame, not "the next frame": the POST and the stream are
	// independent connections, so bundles built before the invite landed can
	// already be buffered here (see its doc).
	var invitedTurn int64

	awaitTurnFrame(t, bobReader, "bob's bundle to carry the pending invite", func(b protocol.TurnEvent) bool {
		if b.PendingInvite == nil || b.PendingInvite.InviterName != "alice" {
			return false
		}

		invitedTurn = b.Turn

		return true
	})

	// The negative has to be read from a bundle built at or after the turn bob
	// saw it on. Simply reading alice's next frame would pass just as happily
	// on a bundle rendered BEFORE the invite existed — proving nothing, and
	// passing for the wrong reason on every run.
	for {
		aliceBundle := decodeTurnFrame(t, aliceReader)
		if aliceBundle.Turn < invitedTurn {
			continue
		}

		if got := aliceBundle.PendingInvite; got != nil {
			t.Errorf("inviter's own bundle at turn %d carries the invite: %+v", aliceBundle.Turn, got)
		}

		break
	}

	resp = postJSON(t, ts, "/api/chat", protocol.ChatRequest{Token: bob.Token, Text: "/accept"})
	if got, want := resp.StatusCode, http.StatusAccepted; got != want {
		t.Fatalf("/accept status = %d, want %d", got, want)
	}

	awaitTurnFrame(t, bobReader, "the pending invite to clear once accepted", func(b protocol.TurnEvent) bool {
		return b.PendingInvite == nil
	})
}

// TestDeclineOverHTTPReachesOnlyTheInviter is the whole #385 decline path over
// real HTTP: bob declines, alice (who asked) is told, and carol — a bystander
// on the same server — is not.
//
// Carol is the point of the test. A decline that leaks is not a crash; it is a
// social failure nobody would notice in a unit test, so the bystander is
// asserted on explicitly rather than assumed.
func TestDeclineOverHTTPReachesOnlyTheInviter(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)

	alice := joinNamed(t, ts, "alice")
	bob := joinNamed(t, ts, "bob")
	carol := joinNamed(t, ts, "carol")

	aliceReader := bufio.NewReader(get(t, ts, "/api/events?token="+alice.Token).Body)
	carolReader := bufio.NewReader(get(t, ts, "/api/events?token="+carol.Token).Body)

	if got, want := chatPost(t, ts, alice.Token, "/invite bob"), http.StatusAccepted; got != want {
		t.Fatalf("/invite status = %d, want %d", got, want)
	}

	if got, want := chatPost(t, ts, bob.Token, "/decline"), http.StatusAccepted; got != want {
		t.Fatalf("/decline status = %d, want %d", got, want)
	}

	// A later global line gives carol's stream something legitimate to deliver,
	// so "carol did not get the decline" is a real assertion rather than a
	// timeout that passes on a slow machine.
	if got, want := chatPost(t, ts, carol.Token, "still here"), http.StatusAccepted; got != want {
		t.Fatalf("plain chat status = %d, want %d", got, want)
	}

	// Alice sees the invite broadcast, then the decline addressed to her.
	readSystemChat(t, aliceReader)

	decline := readSystemChat(t, aliceReader)

	if got, want := decline.Text, "bob declined"; !strings.Contains(got, want) {
		t.Errorf("alice's second line = %q, should contain %q", got, want)
	}

	if got, want := decline.Recipient, alice.EntityID; got != want {
		t.Errorf("Recipient = %d, want alice's id %d", got, want)
	}

	// Carol saw the invite broadcast; her NEXT line must be the plain one, not
	// the decline that was published between them.
	readSystemChat(t, carolReader)

	if got, want := readSystemChat(t, carolReader).Text, "still here"; got != want {
		t.Errorf("carol's second line = %q, want %q — the decline leaked to a bystander", got, want)
	}
}

// TestDeclineWithNothingPendingIs422: the same shape /accept already has.
func TestDeclineWithNothingPendingIs422(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)
	bob := joinNamed(t, ts, "bob")

	if got, want := chatPost(t, ts, bob.Token, "/decline"), http.StatusUnprocessableEntity; got != want {
		t.Errorf("/decline with no invite: status = %d, want %d", got, want)
	}
}
