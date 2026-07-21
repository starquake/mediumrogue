package server_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/chat"
	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
	"github.com/starquake/mediumrogue/internal/server"
)

// TestEventsTokenTracksPresence proves the SSE handler identifies its stream by
// the ?token= query param and hooks presence: an open stream (StreamOpened)
// keeps a player out of the disconnect sweep, and closing the stream — here by
// cancelling the request context, one of the return paths the deferred
// StreamClosed must cover — starts the removal grace so the player is swept.
//
// Player B, who never opens a stream, is a live control: it must be swept after
// the grace, confirming the sweep is actually running so player A's survival is
// attributable to StreamOpened, not to a dormant sweep.
func TestEventsTokenTracksPresence(t *testing.T) {
	t.Parallel()

	const grace = 80 * time.Millisecond

	ticks := hub.New()
	// Fast poll + short grace so the real-time disconnect sweep fires within the
	// test; long combat patience keeps AFK resolution out of the way.
	world := game.NewWorld(game.WorldConfig{
		Interval:        20 * time.Millisecond,
		CombatPatience:  time.Minute,
		BubblePoll:      5 * time.Millisecond,
		DisconnectGrace: grace,
		WorldSeed:       0xC0FFEE,
		Radius:          12,
		Ticks:           ticks,
	})

	chatBroker := chat.NewBroker()

	world.SetAnnounce(func(sender, text string) { chatBroker.Publish(sender, text) })

	go world.Run(t.Context())

	handler := server.New(server.Deps{
		Logger:            slog.New(slog.DiscardHandler),
		World:             world,
		Ticks:             ticks,
		Chat:              chatBroker,
		HeartbeatInterval: time.Hour,
	})
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	playerA := joinTest(t, ts)
	playerB := joinTest(t, ts)

	// Open a stream for A (StreamOpened runs before the first turn frame, so
	// reading one frame proves presence is registered); B opens nothing.
	streamCtx, stopStream := context.WithCancel(t.Context())
	defer stopStream()

	openStreamTest(streamCtx, t, ts, playerA.Token)

	// Past the grace: A's open stream protects it; B, never streamed, is swept.
	// This asserts the sweep is live, so A's survival is the stream's doing.
	waitEntity(t, world, playerA.EntityID, true, 3*grace, "player A with an open stream must survive the disconnect grace")
	waitEntity(t, world, playerB.EntityID, false, 3*grace, "player B with no stream must be swept after the grace")

	// Cancelling the request context returns the handler, firing the deferred
	// StreamClosed, which stamps disconnectedAt and starts A's removal grace.
	stopStream()
	waitEntity(t, world, playerA.EntityID, false, 3*grace, "player A must be swept after its stream closes")
}

// joinTest joins a fresh player over real HTTP and returns its assigned token
// and entity id.
func joinTest(t *testing.T, ts *httptest.Server) protocol.JoinResponse {
	t.Helper()

	body, err := json.Marshal(
		protocol.JoinRequest{Name: "tester", Class: protocol.ClassFighter, Species: protocol.SpeciesHuman})
	if err != nil {
		t.Fatalf("marshal join request: %v", err)
	}

	url := ts.URL + "/api/join"

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build join request: %v", err)
	}

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("POST /api/join: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if got, want := resp.StatusCode, http.StatusOK; got != want {
		t.Fatalf("join status = %d, want %d", got, want)
	}

	var joined protocol.JoinResponse
	if err := json.NewDecoder(resp.Body).Decode(&joined); err != nil {
		t.Fatalf("decode join response: %v", err)
	}

	return joined
}

// openStreamTest opens GET /api/events?token=… under ctx and blocks until the
// first turn frame arrives, which proves the handler ran StreamOpened (it runs
// before the first writeTurn). The stream stays open until ctx is cancelled.
func openStreamTest(ctx context.Context, t *testing.T, ts *httptest.Server, token string) {
	t.Helper()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/events?token="+token, nil)
	if err != nil {
		t.Fatalf("build events request: %v", err)
	}

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /api/events: %v", err)
	}

	t.Cleanup(func() { _ = resp.Body.Close() })

	if got, want := resp.StatusCode, http.StatusOK; got != want {
		t.Fatalf("events status = %d, want %d", got, want)
	}

	// Read until the first data frame lands — by then StreamOpened has run.
	reader := bufio.NewReader(resp.Body)
	done := make(chan struct{})

	go func() {
		defer close(done)

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}

			if strings.HasPrefix(line, "data: ") {
				return
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("no turn frame arrived on the token stream")
	}
}

// waitEntity polls the world snapshot until entity id is present (want=true) or
// absent (want=false), failing with msg if the deadline passes first.
func waitEntity(t *testing.T, world *game.World, id int64, want bool, timeout time.Duration, msg string) {
	t.Helper()

	deadline := time.After(timeout)

	for {
		if entityPresent(world.Snapshot(), id) == want {
			return
		}

		select {
		case <-deadline:
			t.Fatalf("%s (entity %d present=%v, want present=%v)", msg, id, !want, want)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// entityPresent reports whether the snapshot contains an entity with this id.
func entityPresent(snap protocol.TurnEvent, id int64) bool {
	for _, e := range snap.Entities {
		if e.ID == id {
			return true
		}
	}

	return false
}
