package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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

	// A rejected input spends no budget: this over-long paste 422s, and the
	// corrected line right after must still be alice's free first line.
	resp := postJSON(t, ts, "/api/chat",
		protocol.ChatRequest{Token: alice.Token, Text: strings.Repeat("x", protocol.MaxChatLen+1)})
	if got, want := resp.StatusCode, http.StatusUnprocessableEntity; got != want {
		t.Fatalf("over-long chat status = %d, want %d", got, want)
	}

	resp = postJSON(t, ts, "/api/chat", protocol.ChatRequest{Token: alice.Token, Text: "first"})
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

// TestPerIPSSECapOffIgnoresForwardedFor: with TrustProxyIP off (the default),
// there is NO per-IP cap and X-Forwarded-For is ignored — many streams from
// one IP all connect, and only the GLOBAL cap bites. Opens 6 streams from a
// single XFF IP (a per-IP wall of 2, set but inert, would have blocked the
// 3rd); all succeed, then the 7th trips the global cap of 6.
func TestPerIPSSECapOffIgnoresForwardedFor(t *testing.T) {
	t.Parallel()

	ts := startServerWithProxyLimits(t, 6, false, 2)

	const ip = "203.0.113.1"

	for i := range 6 {
		stream := getWithXFF(t, ts, ip)
		if got, want := stream.StatusCode, http.StatusOK; got != want {
			t.Fatalf("stream #%d from one IP status = %d, want %d (per-IP cap must be off)", i+1, got, want)
		}
	}

	over := getWithXFF(t, ts, ip)
	if got, want := over.StatusCode, http.StatusServiceUnavailable; got != want {
		t.Fatalf("7th stream status = %d, want %d (global cap should bite, not per-IP)", got, want)
	}
}

// TestPerIPSSECapRejectsOverCapPerIP: with TrustProxyIP on and the global cap
// off, the per-IP layer is what bites. The wall is 2 here (the default is 5 —
// see config; a tight wall keeps the test fast, mirroring the global-cap test
// above): the 3rd concurrent stream from one XFF IP is 503 + Retry-After, a
// different XFF IP has an independent budget, and a freed slot is reusable.
func TestPerIPSSECapRejectsOverCapPerIP(t *testing.T) {
	t.Parallel()

	ts := startServerWithProxyLimits(t, 0, true, 2)

	const ipA, ipB = "203.0.113.1", "203.0.113.2"

	streams := make([]*http.Response, 0, 2)

	for i := range 2 {
		stream := getWithXFF(t, ts, ipA)
		if got, want := stream.StatusCode, http.StatusOK; got != want {
			t.Fatalf("ipA stream #%d status = %d, want %d", i+1, got, want)
		}

		streams = append(streams, stream)
	}

	over := getWithXFF(t, ts, ipA)
	if got, want := over.StatusCode, http.StatusServiceUnavailable; got != want {
		t.Fatalf("over per-IP-cap stream status = %d, want %d", got, want)
	}

	if got := over.Header.Get("Retry-After"); got == "" {
		t.Error("over per-IP-cap 503 has no Retry-After header, want one")
	}

	var errBody protocol.ErrorResponse
	if err := json.NewDecoder(over.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode 503 body: %v", err)
	}

	if got := errBody.Error; got == "" {
		t.Error(`503 body error = "", want the standard error shape`)
	}

	// A different IP is an independent budget — full for ipA, empty of nothing
	// for ipB.
	other := getWithXFF(t, ts, ipB)
	if got, want := other.StatusCode, http.StatusOK; got != want {
		t.Fatalf("different-IP stream status = %d, want %d (independent per-IP budget)", got, want)
	}

	// Free one of ipA's slots: closing an open stream tears the connection down,
	// cancelling the handler context and releasing the per-IP slot. The release
	// races the next connect, so poll until ipA can connect again.
	_ = streams[0].Body.Close()

	deadline := time.Now().Add(5 * time.Second)

	for {
		resp := getWithXFF(t, ts, ipA)
		if resp.StatusCode == http.StatusOK {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("no per-IP slot freed within 5s of closing one; last status = %d", resp.StatusCode)
		}

		time.Sleep(10 * time.Millisecond)
	}
}
