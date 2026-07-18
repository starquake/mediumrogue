package integration_test

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
	"github.com/starquake/mediumrogue/internal/server"
)

// TestMonstersAppearInTurnBundle proves MONSTER_COUNT wiring end to end: a
// server started with monsters spawned exposes them on the wire as
// protocol.EntityMonster with positive HP, alongside a joined player.
func TestMonstersAppearInTurnBundle(t *testing.T) {
	t.Parallel()

	ts := startServerWithMonsters(t, time.Hour, time.Hour, 3)

	me := join(t, ts, "")

	events := get(t, ts, "/api/events")
	reader := bufio.NewReader(events.Body)

	frames := readFrames(t, reader, 1)

	var bundle protocol.TurnEvent
	if err := json.Unmarshal([]byte(frames[0].data), &bundle); err != nil {
		t.Fatalf("unmarshal bundle %q: %v", frames[0].data, err)
	}

	monsters := 0

	for _, e := range bundle.Entities {
		if e.Kind != protocol.EntityMonster {
			continue
		}

		monsters++

		if got, want := e.HP, 0; got <= want {
			t.Errorf("monster %d HP = %d, want > 0", e.ID, got)
		}
	}

	if got, want := monsters, 3; got != want {
		t.Errorf("monster count in bundle = %d, want %d", got, want)
	}

	sawPlayer := false

	for _, e := range bundle.Entities {
		if e.ID == me.EntityID && e.Kind == protocol.EntityPlayer {
			sawPlayer = true
		}
	}

	if !sawPlayer {
		t.Error("joined player not present in turn bundle")
	}
}

// TestMonsterHuntsPlayer proves the hunting behavior is actually wired up
// over real HTTP: once a player joins, a monster's hex changes across
// successive turn bundles as it paths toward the player.
//
// The player spawn hex is random since #36 (no longer always the origin), so
// this test joins FIRST over HTTP to learn where it actually landed, then
// seeds the monster nine hexes out from THAT hex via a direct world
// reference — outside CombatRadius so it hunts and steps in the WORLD domain
// (no bubble to freeze it in place) for a few world ticks before it ever
// reaches combat range, but within MonsterAggroRadius (#36) so a
// WORLD-domain monster actually notices the player and moves at all. It
// tries a small spread of directions in case one lands on water/rock. Its
// hex changes on the very first world tick, and the world-domain runway
// leaves a window to observe a step before the eventual freeze, regardless
// of the seed or connect latency. The test is not parallel so its tick loop
// is not starved by sibling servers under a loaded runner (#22).
//
//nolint:paralleltest // serial by design (#22): tick loop must not be CPU-starved by parallel siblings.
func TestMonsterHuntsPlayer(t *testing.T) {
	ticks := hub.New()
	world := game.NewWorld(20*time.Millisecond, time.Minute, 5*time.Millisecond, testDisconnectGrace, 0xC0FFEE, 12, ticks)

	chatBroker := newAnnouncingChatBroker(world)
	go world.Run(t.Context())

	handler := server.New(server.Deps{
		Logger: slog.New(slog.DiscardHandler), World: world, Ticks: ticks, Chat: chatBroker,
		HeartbeatInterval: time.Hour,
	})

	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	me := join(t, ts, "")

	// Spawn the monster where it can actually SEE the player (#95/#153): line
	// of sight now gates aggro, so a monster that cannot see anyone stands
	// still and this test's premise evaporates. The old fixed 9-hex offsets
	// were tuned for distance-only aggro (9 <= the wolf's reach of 10), which
	// post-LOS leaves ONE hex of slack — and a single forest hex on the line
	// costs ForestSightCost (2), so any trees in the way blocked the notice
	// and no monster ever moved. That left this test ~50% red on a real
	// generated map (3 of 6 local runs on main, 2026-07-18).
	//
	// So: require an ALL-GRASS line between the two. That is stricter than the
	// engine's own rule (water is transparent, forest merely costs), so it
	// cannot drift with it and duplicates no sight arithmetic. Distances stay
	// above CombatRadius so the pair never bubbles, which would freeze the
	// monster into attacking instead of stepping.
	terrain := make(map[protocol.Hex]protocol.Terrain)
	for _, tile := range world.Map().Tiles {
		terrain[tile.Hex] = tile.Terrain
	}

	grassBetween := func(a, b protocol.Hex) bool {
		line := game.HexLine(a, b)
		for _, h := range line[1 : len(line)-1] {
			if terrain[h] != protocol.TerrainGrass {
				return false
			}
		}

		return true
	}

	dirs := []protocol.Hex{{Q: -1}, {Q: 1}, {R: -1}, {R: 1}, {Q: -1, R: 1}, {Q: 1, R: -1}}

	placed := false

	for dist := protocol.CombatRadius + 1; dist <= 9 && !placed; dist++ {
		for _, d := range dirs {
			h := protocol.Hex{Q: me.Hex.Q + d.Q*dist, R: me.Hex.R + d.R*dist}
			if grassBetween(me.Hex, h) && world.SpawnMonsterAt(h) {
				placed = true

				break
			}
		}
	}

	if !placed {
		t.Fatal("no spawnable hex 7-9 from the player with a clear grass line " +
			"(water/rock/forest/StackCap in every direction) — widen the search")
	}

	events := get(t, ts, "/api/events")
	reader := bufio.NewReader(events.Body)

	firstFrame := readFrames(t, reader, 1)

	var first protocol.TurnEvent
	if err := json.Unmarshal([]byte(firstFrame[0].data), &first); err != nil {
		t.Fatalf("unmarshal bundle %q: %v", firstFrame[0].data, err)
	}

	startHex := monsterHexes(first)
	if len(startHex) == 0 {
		t.Fatal("no monsters present in first bundle")
	}

	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		frames := readFrames(t, reader, 1)

		var bundle protocol.TurnEvent
		if err := json.Unmarshal([]byte(frames[0].data), &bundle); err != nil {
			t.Fatalf("unmarshal bundle %q: %v", frames[0].data, err)
		}

		later := monsterHexes(bundle)

		for id, start := range startHex {
			if now, ok := later[id]; ok && now != start {
				return // at least one monster moved — hunting is wired up
			}
		}
	}

	t.Fatal("no monster ever changed hex across successive turn bundles")
}

// monsterHexes maps monster entity ID to its current hex.
func monsterHexes(bundle protocol.TurnEvent) map[int64]protocol.Hex {
	hexes := make(map[int64]protocol.Hex)

	for _, e := range bundle.Entities {
		if e.Kind == protocol.EntityMonster {
			hexes[e.ID] = e.Hex
		}
	}

	return hexes
}
