package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// Server-hardening tests (#199). These are the only tests that ENABLE the
// rate limits — every other harness leaves them at zero (disabled), so
// rapid-fire suites like the chat fan-out tests are untouched. Limits here
// are set to time.Hour walls: the first request spends the budget and the
// second must be rejected, deterministically, with no sleeps.

// TestChatRateLimitRejectsSecondLine: with CHAT_MIN_INTERVAL-style throttling
// on, a player's second line inside the window is 429 (Retry-After set, JSON
// error body), while another player's budget is untouched.
func TestChatRateLimitRejectsSecondLine(t *testing.T) {
	t.Parallel()

	ts := startServerWithLimits(t, time.Hour, 0, 0)

	alice := joinNamed(t, ts, "alice")
	bob := joinNamed(t, ts, "bob")

	resp := postJSON(t, ts, "/api/chat", protocol.ChatRequest{Token: alice.Token, Text: "first"})
	if got, want := resp.StatusCode, http.StatusAccepted; got != want {
		t.Fatalf("first chat status = %d, want %d", got, want)
	}

	resp = postJSON(t, ts, "/api/chat", protocol.ChatRequest{Token: alice.Token, Text: "second"})
	if got, want := resp.StatusCode, http.StatusTooManyRequests; got != want {
		t.Fatalf("second chat status = %d, want %d", got, want)
	}

	if got := resp.Header.Get("Retry-After"); got == "" {
		t.Error("second chat has no Retry-After header, want one")
	}

	var errBody protocol.ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode 429 body: %v", err)
	}

	if got := errBody.Error; got == "" {
		t.Error(`429 body error = "", want the standard error shape`)
	}

	// The limit is per token: bob still has his budget.
	resp = postJSON(t, ts, "/api/chat", protocol.ChatRequest{Token: bob.Token, Text: "hi"})
	if got, want := resp.StatusCode, http.StatusAccepted; got != want {
		t.Errorf("other player's chat status = %d, want %d", got, want)
	}
}

// TestJoinRateLimitAfterBurst: the join bucket admits a full
// protocol.MaxPlayers burst (a whole friend group at once), then throttles
// the next NEW join with 429 — not the cap's 503, proving the limiter fires
// first — while a returning token (a reclaim) stays exempt.
func TestJoinRateLimitAfterBurst(t *testing.T) {
	t.Parallel()

	ts := startServerWithLimits(t, 0, time.Hour, 0)

	first := joinNamed(t, ts, "p0")
	for i := 1; i < protocol.MaxPlayers; i++ {
		joinNamed(t, ts, fmt.Sprintf("p%d", i))
	}

	resp := postJSON(t, ts, "/api/join",
		protocol.JoinRequest{Name: "straggler", Class: protocol.ClassFighter, Species: protocol.SpeciesHuman})
	if got, want := resp.StatusCode, http.StatusTooManyRequests; got != want {
		t.Fatalf("join past burst status = %d, want %d", got, want)
	}

	if got := resp.Header.Get("Retry-After"); got == "" {
		t.Error("throttled join has no Retry-After header, want one")
	}

	// A reclaim re-sends a known token: exempt from the bucket (a returning
	// player mints no new state), so it succeeds even with the bucket empty.
	resp = postJSON(t, ts, "/api/join", protocol.JoinRequest{Token: first.Token})
	if got, want := resp.StatusCode, http.StatusOK; got != want {
		t.Errorf("reclaim join status = %d, want %d", got, want)
	}

	var reclaimed protocol.JoinResponse
	if err := json.NewDecoder(resp.Body).Decode(&reclaimed); err != nil {
		t.Fatalf("decode reclaim response: %v", err)
	}

	if got, want := reclaimed.EntityID, first.EntityID; got != want {
		t.Errorf("reclaim EntityID = %d, want %d (the same character back)", got, want)
	}
}

// TestSSEStreamCapRejectsOverCap: with a global cap of 2, the third
// concurrent /api/events connect is refused with 503 + Retry-After, and a
// freed slot is reusable — the gate releases when a stream ends.
func TestSSEStreamCapRejectsOverCap(t *testing.T) {
	t.Parallel()

	ts := startServerWithLimits(t, 0, 0, 2)

	streams := make([]*http.Response, 0, 2)

	for i := range 2 {
		stream := get(t, ts, "/api/events")
		if got, want := stream.StatusCode, http.StatusOK; got != want {
			t.Fatalf("stream #%d status = %d, want %d", i+1, got, want)
		}

		streams = append(streams, stream)
	}

	over := get(t, ts, "/api/events")
	if got, want := over.StatusCode, http.StatusServiceUnavailable; got != want {
		t.Fatalf("over-cap stream status = %d, want %d", got, want)
	}

	if got := over.Header.Get("Retry-After"); got == "" {
		t.Error("over-cap 503 has no Retry-After header, want one")
	}

	var errBody protocol.ErrorResponse
	if err := json.NewDecoder(over.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode 503 body: %v", err)
	}

	if got := errBody.Error; got == "" {
		t.Error(`503 body error = "", want the standard error shape`)
	}

	// Free a slot: closing an OPEN stream's body tears its connection down,
	// which cancels the handler's request context and releases the gate. The
	// release races the next connect, so poll until a slot opens.
	_ = streams[0].Body.Close()

	deadline := time.Now().Add(5 * time.Second)

	for {
		resp := get(t, ts, "/api/events")
		if resp.StatusCode == http.StatusOK {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("no stream slot freed within 5s of closing one; last status = %d", resp.StatusCode)
		}

		time.Sleep(10 * time.Millisecond)
	}
}
