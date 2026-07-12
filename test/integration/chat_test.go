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

// joinNamed is join plus an explicit display name, for chat tests that need
// two distinguishable senders (join/joinClass always use testerName).
func joinNamed(t *testing.T, ts *httptest.Server, name string) protocol.JoinResponse {
	t.Helper()

	resp := postJSON(t, ts, "/api/join",
		protocol.JoinRequest{Name: name, Class: protocol.ClassFighter, Species: protocol.SpeciesHuman})
	if got, want := resp.StatusCode, http.StatusOK; got != want {
		t.Fatalf("join(name=%q) status = %d, want %d", name, got, want)
	}

	var joined protocol.JoinResponse
	if err := json.NewDecoder(resp.Body).Decode(&joined); err != nil {
		t.Fatalf("decode join response: %v", err)
	}

	return joined
}

// readChatWithin scans the SSE stream for the next `event: chat` frame within
// timeout, skipping every other frame (turn, heartbeat) along the way — the
// stream always emits a turn frame immediately on connect.
func readChatWithin(t *testing.T, r *bufio.Reader, timeout time.Duration) (protocol.ChatMessage, bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return protocol.ChatMessage{}, false
		}

		frame, ok := readFrameWithin(t, r, remaining)
		if !ok {
			return protocol.ChatMessage{}, false
		}

		if frame.event != protocol.EventChat {
			continue
		}

		var msg protocol.ChatMessage
		if err := json.Unmarshal([]byte(frame.data), &msg); err != nil {
			t.Fatalf("unmarshal chat frame %q: %v", frame.data, err)
		}

		return msg, true
	}
}

// TestChatFansOutToOtherClient: alice POSTs a line; bob's own event stream
// (a separate connection, not alice's) receives it as an EventChat frame.
func TestChatFansOutToOtherClient(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)

	alice := joinNamed(t, ts, "alice")
	bob := joinNamed(t, ts, "bob")

	stream := get(t, ts, "/api/events?token="+bob.Token)
	reader := bufio.NewReader(stream.Body)

	resp := postJSON(t, ts, "/api/chat", protocol.ChatRequest{Token: alice.Token, Text: "hello"})
	if got, want := resp.StatusCode, http.StatusAccepted; got != want {
		t.Fatalf("chat status = %d, want %d", got, want)
	}

	msg, ok := readChatWithin(t, reader, frameReadTimeout)
	if !ok {
		t.Fatal("bob's stream never received a chat frame")
	}

	if got, want := msg.Sender, "alice"; got != want {
		t.Errorf("sender = %q, want %q", got, want)
	}

	if got, want := msg.Text, "hello"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

// TestHereCommandSharesLocation: alice POSTs "/here"; bob receives a message
// whose text contains alice's server-authoritative coordinates.
func TestHereCommandSharesLocation(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)

	alice := joinNamed(t, ts, "alice")
	bob := joinNamed(t, ts, "bob")

	stream := get(t, ts, "/api/events?token="+bob.Token)
	reader := bufio.NewReader(stream.Body)

	resp := postJSON(t, ts, "/api/chat", protocol.ChatRequest{Token: alice.Token, Text: "/here"})
	if got, want := resp.StatusCode, http.StatusAccepted; got != want {
		t.Fatalf("chat status = %d, want %d", got, want)
	}

	msg, ok := readChatWithin(t, reader, frameReadTimeout)
	if !ok {
		t.Fatal("bob's stream never received a chat frame")
	}

	if got, want := msg.Text, fmt.Sprintf("(%d, %d)", alice.Hex.Q, alice.Hex.R); !strings.Contains(got, want) {
		t.Errorf("text = %q, should contain %q", got, want)
	}
}

// TestChatRejectsEmptyAndOversizeAndBadToken covers every 4xx path of
// handleChat: whitespace-only text, over-MaxChatLen text, an unknown token,
// and an unrecognized "/command".
func TestChatRejectsEmptyAndOversizeAndBadToken(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)

	alice := joinNamed(t, ts, "alice")

	cases := []struct {
		name string
		req  protocol.ChatRequest
		want int
	}{
		{"empty", protocol.ChatRequest{Token: alice.Token, Text: "   "}, http.StatusUnprocessableEntity},
		{
			"oversize",
			protocol.ChatRequest{Token: alice.Token, Text: strings.Repeat("x", protocol.MaxChatLen+1)},
			http.StatusUnprocessableEntity,
		},
		{"bad token", protocol.ChatRequest{Token: "nope", Text: "hi"}, http.StatusUnauthorized},
		{"bogus command", protocol.ChatRequest{Token: alice.Token, Text: "/bogus"}, http.StatusUnprocessableEntity},
	}

	for _, tc := range cases {
		resp := postJSON(t, ts, "/api/chat", tc.req)
		if got, want := resp.StatusCode, tc.want; got != want {
			t.Errorf("case %s: status = %d, want %d", tc.name, got, want)
		}
	}
}

// TestChatCapCountsRunesNotBytes pins that MaxChatLen is a RUNE cap, not a byte
// cap: "\u00e9" is 1 rune / 2 bytes, so MaxChatLen of them (2xMaxChatLen bytes)
// must still be accepted, and one rune over rejected. The oversize case above
// used ASCII only, where rune and byte counts coincide.
func TestChatCapCountsRunesNotBytes(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)
	alice := joinNamed(t, ts, "alice")

	atCap := protocol.ChatRequest{Token: alice.Token, Text: strings.Repeat("\u00e9", protocol.MaxChatLen)}
	if got, want := postJSON(t, ts, "/api/chat", atCap).StatusCode, http.StatusAccepted; got != want {
		t.Errorf("MaxChatLen multibyte runes: status = %d, want %d (rune cap, not byte cap)", got, want)
	}

	overCap := protocol.ChatRequest{Token: alice.Token, Text: strings.Repeat("\u00e9", protocol.MaxChatLen+1)}
	if got, want := postJSON(t, ts, "/api/chat", overCap).StatusCode, http.StatusUnprocessableEntity; got != want {
		t.Errorf("MaxChatLen+1 multibyte runes: status = %d, want %d", got, want)
	}
}

// TestJoinRequiresName: an empty display name is rejected at join.
func TestJoinRequiresName(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)

	resp := postJSON(t, ts, "/api/join",
		protocol.JoinRequest{Name: "", Class: protocol.ClassFighter, Species: protocol.SpeciesHuman})
	if got, want := resp.StatusCode, http.StatusUnprocessableEntity; got != want {
		t.Errorf("join(name=\"\") status = %d, want %d", got, want)
	}
}
