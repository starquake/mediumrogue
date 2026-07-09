package integration_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// openStream opens GET /api/events?token=<token> over its own cancelable
// context and returns a reader over the SSE body plus a cancel func that drops
// the connection — the way a browser tab closing kills its EventSource. The
// server sees r.Context().Done() and fires StreamClosed, starting the grace.
// The context and body are also cleaned up at test end, so an un-cancelled
// stream simply lives (present) until the test finishes.
func openStream(t *testing.T, ts *httptest.Server, token string) (*bufio.Reader, func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	path := "/api/events"
	if token != "" {
		path += "?token=" + url.QueryEscape(token)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+path, nil)
	if err != nil {
		t.Fatalf("build events request: %v", err)
	}

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}

	t.Cleanup(func() { _ = resp.Body.Close() })

	return bufio.NewReader(resp.Body), cancel
}

// waitForPresence reads bundles from r until entity id appears (a real
// baseline: the observer's first snapshot predates a later join, so a caller
// must confirm presence before asserting a later absence). Fails at deadline.
func waitForPresence(t *testing.T, r *bufio.Reader, id int64, deadline time.Time) {
	t.Helper()

	for time.Now().Before(deadline) {
		if _, ok := entityOf(decodeBundle(t, r), id); ok {
			return
		}
	}

	t.Fatalf("entity %d never appeared in the observer bundles", id)
}

// waitForAbsence reads bundles from r until entity id is gone. Fails at deadline.
func waitForAbsence(t *testing.T, r *bufio.Reader, id int64, deadline time.Time) {
	t.Helper()

	for time.Now().Before(deadline) {
		if _, ok := entityOf(decodeBundle(t, r), id); !ok {
			return
		}
	}

	t.Fatalf("entity %d still present in the observer bundles after the grace", id)
}

// TestDisconnectRemovesEntityAfterGrace is the load-bearing proof: a player
// whose only event stream closes has its entity swept from the world after the
// disconnect grace, and a SECOND connected client (the observer) sees it
// vanish from the turn bundles. The observer's own stream stays open the whole
// test, so the observer is never itself swept.
func TestDisconnectRemovesEntityAfterGrace(t *testing.T) {
	t.Parallel()

	grace := 300 * time.Millisecond
	ts := startServerWithGrace(t, 20*time.Millisecond, time.Hour, grace)

	// A persistent observer: its stream is held open (never cancelled) so it
	// stays present and its bundles are the ground truth for who is in the world.
	observer := join(t, ts, "")
	obs, _ := openStream(t, ts, observer.Token)

	// The player under test: join, open its own stream, confirm it is present
	// in its own first bundle, then drop the stream (a disconnect).
	player := join(t, ts, "")

	playerStream, closePlayer := openStream(t, ts, player.Token)
	if _, ok := entityOf(decodeBundle(t, playerStream), player.EntityID); !ok {
		t.Fatalf("player %d absent from its own first bundle", player.EntityID)
	}

	// Baseline: confirm the observer actually sees the player before dropping it
	// (the observer's first snapshot predates the join, so absence would
	// otherwise be trivially true — a false pass).
	waitForPresence(t, obs, player.EntityID, time.Now().Add(5*time.Second))

	closePlayer()

	// Now the disconnected player must vanish from the observer's bundles. A
	// generous deadline keeps this robust under a CPU-starved -race runner; the
	// sweep should actually fire within grace + one bubblePoll.
	waitForAbsence(t, obs, player.EntityID, time.Now().Add(5*time.Second))
}

// TestReconnectWithinGraceKeepsEntity proves the M5 reconnect model survives:
// a player drops its stream and REOPENS one with the SAME token before the
// grace elapses. The reopened stream refreshes presence, so the sweep skips the
// entity and the observer keeps seeing it across more than one grace period.
func TestReconnectWithinGraceKeepsEntity(t *testing.T) {
	t.Parallel()

	grace := 400 * time.Millisecond
	ts := startServerWithGrace(t, 20*time.Millisecond, time.Hour, grace)

	observer := join(t, ts, "")
	obs, _ := openStream(t, ts, observer.Token)

	player := join(t, ts, "")

	playerStream, closePlayer := openStream(t, ts, player.Token)
	if _, ok := entityOf(decodeBundle(t, playerStream), player.EntityID); !ok {
		t.Fatalf("player %d absent from its own first bundle", player.EntityID)
	}

	// Baseline: the observer sees the player before anything is dropped.
	waitForPresence(t, obs, player.EntityID, time.Now().Add(5*time.Second))

	// Disconnect, then immediately reconnect with the same token — well within
	// the grace. openStream holds the new stream open until test end, so
	// presence stays refreshed (streams == 1) and the sweep never fires.
	closePlayer()
	openStream(t, ts, player.Token)

	// Across more than one grace period the player must stay present in the
	// observer's bundles: the reconnect saved it.
	deadline := time.Now().Add(3 * grace)
	for time.Now().Before(deadline) {
		if _, ok := entityOf(decodeBundle(t, obs), player.EntityID); !ok {
			t.Fatalf("reconnected player %d was swept despite a live stream", player.EntityID)
		}
	}
}
