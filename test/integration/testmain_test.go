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
	"github.com/starquake/mediumrogue/internal/server"
)

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

	world := game.NewWorld(turnInterval, combatPatience, bubblePoll, ticks)

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
