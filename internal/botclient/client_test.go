package botclient_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/botclient"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// client_test.go (#430): a dropped event stream must not end the bot.
//
// The bug this covers was invisible from the outside — the process exited 0 and
// the only trace was one ERROR line per party member — so the test drives the
// exact shape that produced it: a server that hands out a stream, cuts it, and
// hands out another. Before the fix the channel closed on the first cut.

// turnFrame is one SSE turn frame carrying the given turn number.
func turnFrame(t *testing.T, turn int64) string {
	t.Helper()

	body, err := json.Marshal(protocol.TurnEvent{Turn: turn})
	if err != nil {
		t.Fatalf("marshal turn: %v", err)
	}

	return fmt.Sprintf("id: %d\nevent: %s\ndata: %s\n\n", turn, protocol.EventTurn, body)
}

// droppingServer serves /api/join and an /api/events that sends one turn frame
// and then CLOSES — the deploy-restart shape from #430. Each connection sends a
// turn number one higher than the last, so the test can tell a genuine
// reconnect from a replay of the first stream.
func droppingServer(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()

	var opens atomic.Int64

	mux := http.NewServeMux()
	mux.HandleFunc("/api/join", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(protocol.JoinResponse{EntityID: 7, Token: "tok"}); err != nil {
			t.Errorf("encode join: %v", err)
		}
	})
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, _ *http.Request) {
		n := opens.Add(1)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		if _, err := w.Write([]byte(turnFrame(t, n))); err != nil {
			return
		}

		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Returning closes the connection: the stream drops mid-session,
		// exactly as it does when development redeploys underneath a bot.
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv, &opens
}

// TestEventsReconnectsAfterTheStreamDrops is the regression: two turn bundles
// arrive across two separate connections, on one uninterrupted channel.
func TestEventsReconnectsAfterTheStreamDrops(t *testing.T) {
	t.Parallel()

	srv, opens := droppingServer(t)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	client, err := botclient.Join(ctx, srv.URL, protocol.JoinRequest{Name: "b", Class: protocol.ClassFighter})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	events, err := client.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}

	var got []int64

	for ev := range events {
		if ev.Turn == nil {
			continue
		}

		got = append(got, ev.Turn.Turn)

		// Two bundles from two connections is the whole proof; stop before the
		// backoff makes the test slow.
		if len(got) == 2 {
			cancel()

			break
		}
	}

	if len(got) < 2 {
		t.Fatalf("received %v, want two bundles across a reconnect (the stream ended for good)", got)
	}

	if got[0] == got[1] {
		t.Errorf("both bundles were turn %d, want distinct turns from distinct connections", got[0])
	}

	if n := opens.Load(); n < 2 {
		t.Errorf("/api/events was opened %d time(s), want >= 2", n)
	}
}

// TestEventsClosesOnCancel pins the other half of the contract: the channel
// closes on cancellation and ONLY on cancellation. cmd/bot reads exactly this
// to tell Ctrl-C from a drop, and a reconnect loop that ignored ctx would hang
// a bot forever instead of stopping it.
func TestEventsClosesOnCancel(t *testing.T) {
	t.Parallel()

	srv, _ := droppingServer(t)

	ctx, cancel := context.WithCancel(t.Context())

	client, err := botclient.Join(ctx, srv.URL, protocol.JoinRequest{Name: "b", Class: protocol.ClassFighter})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	events, err := client.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}

	cancel()

	done := make(chan struct{})

	go func() {
		defer close(done)

		for range events { //nolint:revive // draining until close is the assertion.
		}
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("event channel did not close after cancellation")
	}
}

// TestEventsFailsLoudlyOnTheFirstOpen: a bot pointed at nothing must stop at
// startup rather than retry forever. Only a stream that once worked reconnects
// — a typo'd URL should not become an infinite quiet loop.
func TestEventsFailsLoudlyOnTheFirstOpen(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/join", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(protocol.JoinResponse{EntityID: 7, Token: "tok"}); err != nil {
			t.Errorf("encode join: %v", err)
		}
	})
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := botclient.Join(t.Context(), srv.URL, protocol.JoinRequest{Name: "b", Class: protocol.ClassFighter})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	if _, err := client.Events(t.Context()); err == nil {
		t.Error("Events returned no error on a refused first open, want one")
	}
}
