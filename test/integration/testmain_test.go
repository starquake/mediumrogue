// Package integration_test spins up the real handler tree over real HTTP
// (httptest) and exercises it the way a browser would — the topbanana
// test/integration pattern. Fast intervals keep the suite in milliseconds.
package integration_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/chat"
	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
	"github.com/starquake/mediumrogue/internal/server"
)

// testDisconnectGrace keeps the disconnect sweep comfortably out of the way of
// existing integration tests — no entity is swept mid-test. Milestone 6.4
// Task 5 adds sweep tests that thread a short grace explicitly.
const testDisconnectGrace = time.Hour

// wolfKillXP is wolf's kill XP (internal/game/content.go's monsterDefs) —
// the default spawn kind (SpawnMonsters/SpawnMonsterAt), and the exact old
// flat protocol.MonsterXP value (deleted in 6c — monster kind numbers are
// now game-package content data, not importable here) it carries forward
// unchanged.
const wolfKillXP = 20

// newAnnouncingChatBroker builds a chat broker and wires it as world's
// announce hook for in-resolution quest/party events, mirroring app.go's
// production wiring (chat.NewBroker + World.SetAnnounce). Callers must call
// this BEFORE world.Run: SetAnnounce's contract requires installing the hook
// before the clock starts, so no early resolution's announcement can be lost
// to the default no-op.
func newAnnouncingChatBroker(world *game.World) *chat.Broker {
	broker := chat.NewBroker()

	world.SetAnnounce(func(sender, text string) { broker.Publish(sender, text) })

	return broker
}

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

	ticks, world := newWorldWithMonstersAboveGround(
		t, turnInterval, combatPatience, bubblePoll, monsterCount,
	)

	return serveWorld(t, world, ticks, server.Deps{HeartbeatInterval: heartbeatInterval})
}

// monsterSpawnAttempts bounds newWorldWithMonstersAboveGround's re-roll. A
// single attempt yields an all-above-ground fixture ~98.4% of the time, so the
// chance of exhausting this many is vanishingly small (~1.6%^50); it exists so
// a content change that made burial the common case fails loudly here instead
// of hanging.
const monsterSpawnAttempts = 50

// newWorldWithMonstersAboveGround builds the integration fixture world and
// spawns monsterCount monsters that are all ABOVE GROUND, so a test may assert
// an exact monster count on the wire.
//
// The re-roll is the fix for #516. SpawnMonsters draws placement and kind from
// w.seed — the per-World tie-break seed minted from crypto/rand in NewWorld,
// NOT the pinned WorldSeed, which seeds only the map and quests — so monster
// kinds differ on every construction even with WorldSeed pinned. Every
// skeleton spawns buried (#436), and a buried monster is deliberately never
// put on the wire (entityViewsLocked), while revealBuriedLocked only runs in
// the turn pass, which these harnesses pin far out. So ~1.6% of fixtures put
// n monsters in the world and fewer than n in a turn bundle, which is exactly
// how TestMonstersAppearInTurnBundle flaked.
//
// Re-rolling the whole World is what re-draws the spawn: the tie-break seed is
// minted per construction. The map itself does not move — that comes from the
// pinned WorldSeed — so only monster kinds and hexes are redrawn.
//
// Burial is visible through the public API alone: SnapshotFor already excludes
// buried monsters, so counting monsters in the world's own snapshot says
// whether this draw is usable. monsterCount == 0 (startServer, and so every
// other integration test) matches on the first attempt and is unaffected.
func newWorldWithMonstersAboveGround(
	t *testing.T, turnInterval, combatPatience, bubblePoll time.Duration, monsterCount int,
) (*hub.Hub, *game.World) {
	t.Helper()

	for range monsterSpawnAttempts {
		ticks := hub.New()

		world := game.NewWorld(game.WorldConfig{
			Interval:        turnInterval,
			CombatPatience:  combatPatience,
			BubblePoll:      bubblePoll,
			DisconnectGrace: testDisconnectGrace,
			WorldSeed:       0xC0FFEE,
			Radius:          12,
			Ticks:           ticks,
		})

		world.SpawnMonsters(monsterCount)

		if monstersOnWire(world) == monsterCount {
			return ticks, world
		}
	}

	t.Fatalf(
		"no fixture world in %d attempts spawned %d monsters all above ground"+
			" (a buried kind has become the common spawn?)",
		monsterSpawnAttempts, monsterCount,
	)

	return nil, nil
}

// monstersOnWire counts the monsters w would put in a turn bundle — which
// excludes the buried (#436), the whole point of the count. The token-less
// viewer centres the InterestRadius cull on the origin; every hex of the
// radius-12 fixture map is well inside it, so the cull drops nothing here.
func monstersOnWire(w *game.World) int {
	monsters := 0

	for _, e := range w.SnapshotFor("").Entities {
		if e.Kind == protocol.EntityMonster {
			monsters++
		}
	}

	return monsters
}

// startServerWithLimits boots the handler tree with the #199 hardening knobs
// set — the ONLY harness that enables them: every other harness builds Deps
// without the limit fields (zero = disabled), so the rest of the suite can
// chat and join as fast as it likes. A long turn interval keeps the clock out
// of the way; these tests assert on HTTP statuses, not turns.
func startServerWithLimits(
	t *testing.T, chatMinInterval, joinMinInterval time.Duration, sseMaxStreams int,
) *httptest.Server {
	t.Helper()

	ticks := hub.New()

	world := game.NewWorld(game.WorldConfig{
		Interval:        time.Hour,
		CombatPatience:  time.Minute,
		BubblePoll:      5 * time.Millisecond,
		DisconnectGrace: testDisconnectGrace,
		WorldSeed:       0xC0FFEE,
		Radius:          12,
		Ticks:           ticks,
	})

	return serveWorld(t, world, ticks, server.Deps{
		HeartbeatInterval: time.Hour,
		ChatMinInterval:   chatMinInterval,
		JoinMinInterval:   joinMinInterval,
		SSEMaxStreams:     sseMaxStreams,
	})
}

// startServerWithProxyLimits boots the handler tree with the SSE stream caps
// set for the per-IP tests (#199): the global cap plus the opt-in per-IP layer
// (TrustProxyIP + PerIPSSEStreams). Every other harness leaves TrustProxyIP
// false, so X-Forwarded-For is never read elsewhere. A long turn interval and
// heartbeat keep both clocks out of the way — these tests assert on connect
// statuses, not turns.
func startServerWithProxyLimits(
	t *testing.T, sseMaxStreams int, trustProxyIP bool, perIPSSEStreams int,
) *httptest.Server {
	t.Helper()

	ticks := hub.New()

	world := game.NewWorld(game.WorldConfig{
		Interval:        time.Hour,
		CombatPatience:  time.Minute,
		BubblePoll:      5 * time.Millisecond,
		DisconnectGrace: testDisconnectGrace,
		WorldSeed:       0xC0FFEE,
		Radius:          12,
		Ticks:           ticks,
	})

	return serveWorld(t, world, ticks, server.Deps{
		HeartbeatInterval: time.Hour,
		SSEMaxStreams:     sseMaxStreams,
		TrustProxyIP:      trustProxyIP,
		PerIPSSEStreams:   perIPSSEStreams,
	})
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

	world := game.NewWorld(game.WorldConfig{
		Interval:        turnInterval,
		CombatPatience:  time.Minute,
		BubblePoll:      5 * time.Millisecond,
		DisconnectGrace: disconnectGrace,
		WorldSeed:       0xC0FFEE,
		Radius:          12,
		Ticks:           ticks,
	})

	return serveWorld(t, world, ticks, server.Deps{HeartbeatInterval: heartbeatInterval})
}

// startServerWithMonstersAt is startServerWithMonsters but places monsters at
// caller-chosen hexes (via world.SpawnMonsterAt) instead of random world-seeded
// positions. A chase test that seeds a monster at a KNOWN hex a couple steps
// from the origin (where players spawn) gets a short, deterministic chase — one
// that no longer depends on how far crypto/rand scattered the monster, and so
// resolves in a handful of turns even when the tick loop is CPU-starved (the #22
// chase-timing flake). Uses the same long patience / fast poll as
// startServerWithMonsters, and the standard 15ms turn interval every caller
// wants — a test that needs a different interval or wants monsters placed
// relative to a player's actual (random since #36) spawn hex builds its own
// world/server directly, as TestMageAoEDamagesMonsters and
// TestMonsterHuntsPlayer do. The heartbeat is pinned far out (time.Hour):
// these timing-sensitive tests assert on turn bundles, never on heartbeat
// comments.
func startServerWithMonstersAt(t *testing.T, hexes ...protocol.Hex) *httptest.Server {
	t.Helper()

	return startServerWithBubbleTuningAt(t, 15*time.Millisecond, time.Minute, 5*time.Millisecond, hexes...)
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

	world := game.NewWorld(game.WorldConfig{
		Interval:        turnInterval,
		CombatPatience:  combatPatience,
		BubblePoll:      bubblePoll,
		DisconnectGrace: testDisconnectGrace,
		WorldSeed:       0xC0FFEE,
		Radius:          12,
		Ticks:           ticks,
	})

	for _, h := range hexes {
		if !world.SpawnMonsterAt(h) {
			t.Fatalf("SpawnMonsterAt(%v) = false, want a monster (not walkable or over StackCap)", h)
		}
	}

	return serveWorld(t, world, ticks, server.Deps{HeartbeatInterval: time.Hour})
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

// getWithXFF opens an /api/events stream with an X-Forwarded-For header, the
// way a client reaches the server through the trusted reverse proxy — the
// input the per-IP SSE cap keys on (#199).
func getWithXFF(t *testing.T, ts *httptest.Server, xff string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL+"/api/events", nil)
	if err != nil {
		t.Fatalf("build /api/events request: %v", err)
	}

	req.Header.Set("X-Forwarded-For", xff)

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /api/events: %v", err)
	}

	t.Cleanup(func() { _ = resp.Body.Close() })

	return resp
}
