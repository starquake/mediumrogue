package integration_test

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
	"github.com/starquake/mediumrogue/internal/server"
)

// serveWorld is the world-boot tail every integration harness shares: wire an
// announcing chat broker (before world.Run — SetAnnounce's contract), start the
// control loop, front it with the real handler tree over httptest, and register
// shutdown via t.Cleanup. The caller builds and configures world (and ticks) —
// including any monster placement, which must happen before the loop starts —
// and passes the per-harness Deps knobs it cares about (HeartbeatInterval, the
// #199 hardening limits); serveWorld fills in the Logger/World/Ticks/Chat that
// never vary. The caller keeps its own world reference, so tests that place
// monsters or read state after connecting still can.
func serveWorld(t *testing.T, world *game.World, ticks *hub.Hub, deps server.Deps) *httptest.Server {
	t.Helper()

	chatBroker := newAnnouncingChatBroker(world)
	go world.Run(t.Context())

	deps.Logger = slog.New(slog.DiscardHandler)
	deps.World = world
	deps.Ticks = ticks
	deps.Chat = chatBroker

	ts := httptest.NewServer(server.New(deps))
	t.Cleanup(ts.Close)

	return ts
}

// joinWith POSTs req to /api/join, asserts a 200, and decodes the response —
// the shared core the named join helpers (join/joinClass/joinNamed/joinAs/
// joinSpecies) wrap, each supplying its own JoinRequest.
func joinWith(t *testing.T, ts *httptest.Server, req protocol.JoinRequest) protocol.JoinResponse {
	t.Helper()

	resp := postJSON(t, ts, "/api/join", req)
	if got, want := resp.StatusCode, http.StatusOK; got != want {
		t.Fatalf("join(%+v) status = %d, want %d", req, got, want)
	}

	var joined protocol.JoinResponse
	if err := json.NewDecoder(resp.Body).Decode(&joined); err != nil {
		t.Fatalf("decode join response: %v", err)
	}

	return joined
}

// decodeTurnFrame reads and unmarshals the next SSE TURN frame. The stream
// interleaves chat announces (kill summaries, deaths, pickups) and named
// heartbeats with turn bundles — anything that isn't a turn frame is skipped,
// not mis-decoded as an empty bundle.
func decodeTurnFrame(t *testing.T, r *bufio.Reader) protocol.TurnEvent {
	t.Helper()

	for {
		frames := readFrames(t, r, 1)
		if frames[0].event != protocol.EventTurn {
			continue
		}

		var bundle protocol.TurnEvent
		if err := json.Unmarshal([]byte(frames[0].data), &bundle); err != nil {
			t.Fatalf("unmarshal bundle %q: %v", frames[0].data, err)
		}

		return bundle
	}
}

// awaitTurnFrame reads turn bundles until one satisfies cond, and fails
// naming what it was waiting for.
//
// Use it instead of asserting on the bundle that happens to follow a POST.
// The intent POST and the SSE stream are independent connections, so bundles
// generated BEFORE an intent was accepted can already be sitting in this
// reader's buffer — "the next frame I read" is not "the first frame after my
// POST". Nothing ever guaranteed otherwise; it was merely masked while
// bundles were large enough that each read blocked for a fresh one. #288's
// response compression shrank them to the point where several arrive in a
// single read, so the assumption now fails a few percent of runs.
//
// Bounded by a frame count rather than a timeout: the world clock is what
// produces frames, so a stuck world stops the stream and readFrames' own
// deadline reports it — counting frames keeps this from silently becoming a
// sleep on a slow runner.
func awaitTurnFrame(t *testing.T, r *bufio.Reader, waitingFor string, cond func(protocol.TurnEvent) bool) {
	t.Helper()

	// Comfortably more turns than any state change here needs, while still
	// failing fast enough to be a useful signal.
	const maxFrames = 40

	for range maxFrames {
		if cond(decodeTurnFrame(t, r)) {
			return
		}
	}

	t.Fatalf("no turn bundle showed %s within %d frames", waitingFor, maxFrames)
}
