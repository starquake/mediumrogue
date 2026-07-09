// Package integration_test spins up the real handler tree over real HTTP
// (httptest) and exercises it the way a browser would — the topbanana
// test/integration pattern. Fast intervals keep the suite in milliseconds.
package integration_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
	"github.com/starquake/mediumrogue/internal/server"
)

// testDisconnectGrace keeps the disconnect sweep comfortably out of the way of
// existing integration tests — no entity is swept mid-test. Milestone 6.4
// Task 5 adds sweep tests that thread a short grace explicitly.
const testDisconnectGrace = time.Hour

// startServer boots the full handler tree with a fast clock and returns the
// test server. Everything shuts down via t.Cleanup / t.Context. No monsters
// are spawned, so existing tests that assert on entity counts/behavior are
// unaffected — see startServerWithMonsters for milestone 6.2 tests.
func startServer(t *testing.T, turnInterval, heartbeatInterval time.Duration) *httptest.Server {
	t.Helper()

	return startServerWithMonsters(t, turnInterval, heartbeatInterval, 0)
}

// startServerWithMonsters is startServer plus n monsters spawned before the
// clock starts running. A fast poll (shorter than the smallest turn interval
// any test uses) keeps the control loop ticking promptly; a long patience
// keeps the AFK fallback out of the way so combat resolves on lock-ins. Tests
// that need to observe the freeze window itself (milestone 6.4) want a
// shorter patience and use startServerWithBubbleTuning instead.
func startServerWithMonsters(
	t *testing.T, turnInterval, heartbeatInterval time.Duration, monsterCount int,
) *httptest.Server {
	t.Helper()

	return startServerWithBubbleTuning(t, turnInterval, heartbeatInterval, monsterCount, time.Minute, 5*time.Millisecond)
}

// startServerWithBubbleTuning is startServerWithMonsters plus explicit combat-
// bubble patience/poll knobs, for tests that need the freeze window to stay
// open for a controlled span (long enough to poll several turn bundles
// without the AFK patience timeout auto-resolving the bubble out from under
// the assertion).
func startServerWithBubbleTuning(
	t *testing.T, turnInterval, heartbeatInterval time.Duration, monsterCount int,
	combatPatience, bubblePoll time.Duration,
) *httptest.Server {
	t.Helper()

	ticks := hub.New()

	world := game.NewWorld(turnInterval, combatPatience, bubblePoll, testDisconnectGrace, ticks)

	world.SpawnMonsters(monsterCount)
	go world.Run(t.Context())

	handler := server.New(server.Deps{
		Logger:            slog.New(slog.DiscardHandler),
		World:             world,
		Ticks:             ticks,
		HeartbeatInterval: heartbeatInterval,
	})

	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	return ts
}

// startServerWithGrace boots the handler tree with an explicit (short)
// disconnectGrace so the disconnect sweep fires within a test — the rest of the
// suite keeps the long testDisconnectGrace default and is never swept
// mid-test. No monsters are spawned; a fast bubblePoll keeps the sweep (which
// rides the control loop) checking promptly, and a long combatPatience stays
// out of the way. Used by disconnect_test.go.
func startServerWithGrace(
	t *testing.T, turnInterval, heartbeatInterval, disconnectGrace time.Duration,
) *httptest.Server {
	t.Helper()

	ticks := hub.New()

	world := game.NewWorld(turnInterval, time.Minute, 5*time.Millisecond, disconnectGrace, ticks)

	go world.Run(t.Context())

	handler := server.New(server.Deps{
		Logger:            slog.New(slog.DiscardHandler),
		World:             world,
		Ticks:             ticks,
		HeartbeatInterval: heartbeatInterval,
	})

	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	return ts
}

// startServerWithMonstersAt is startServerWithMonsters but places monsters at
// caller-chosen hexes (via world.SpawnMonsterAt) instead of random world-seeded
// positions. A chase test that seeds a monster at a KNOWN hex a couple steps
// from the origin (where players spawn) gets a short, deterministic chase — one
// that no longer depends on how far crypto/rand scattered the monster, and so
// resolves in a handful of turns even when the tick loop is CPU-starved (the #22
// chase-timing flake). Uses the same long patience / fast poll as
// startServerWithMonsters. The heartbeat is pinned far out (time.Hour): these
// timing-sensitive tests assert on turn bundles, never on heartbeat comments.
func startServerWithMonstersAt(
	t *testing.T, turnInterval time.Duration, hexes ...protocol.Hex,
) *httptest.Server {
	t.Helper()

	return startServerWithBubbleTuningAt(t, turnInterval, time.Minute, 5*time.Millisecond, hexes...)
}

// startServerWithBubbleTuningAt is startServerWithBubbleTuning but places
// monsters at caller-chosen hexes (see startServerWithMonstersAt) rather than a
// random count, for freeze-window tests that also need explicit combat-bubble
// patience/poll knobs. Fails the test if any hex is not walkable or is already
// at StackCap, so a bad fixture hex surfaces loudly instead of silently
// spawning nothing. The heartbeat is pinned far out (time.Hour) — see
// startServerWithMonstersAt.
func startServerWithBubbleTuningAt(
	t *testing.T, turnInterval, combatPatience, bubblePoll time.Duration,
	hexes ...protocol.Hex,
) *httptest.Server {
	t.Helper()

	ticks := hub.New()

	world := game.NewWorld(turnInterval, combatPatience, bubblePoll, testDisconnectGrace, ticks)

	for _, h := range hexes {
		if !world.SpawnMonsterAt(h) {
			t.Fatalf("SpawnMonsterAt(%v) = false, want a monster (not walkable or over StackCap)", h)
		}
	}

	go world.Run(t.Context())

	handler := server.New(server.Deps{
		Logger:            slog.New(slog.DiscardHandler),
		World:             world,
		Ticks:             ticks,
		HeartbeatInterval: time.Hour,
	})

	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	return ts
}

// get issues a GET against the test server and registers body cleanup. The
// request context is the test's, so an open SSE stream dies with the test.
func get(t *testing.T, ts *httptest.Server, path string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL+path, nil)
	if err != nil {
		t.Fatalf("build request for %s: %v", path, err)
	}

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}

	t.Cleanup(func() { _ = resp.Body.Close() })

	return resp
}
