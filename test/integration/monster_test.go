package integration_test

import (
	"bufio"
	"encoding/json"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
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
// The monster is seeded nine hexes from the origin (where the player spawns)
// — outside CombatRadius so it hunts and steps in the WORLD domain (no bubble
// to freeze it in place) for a few world ticks before it ever reaches combat
// range, but within MonsterAggroRadius (#36) so a WORLD-domain monster
// actually notices the player and moves at all. Its hex changes on the very
// first world tick, and the world-domain runway leaves a window to observe a
// step before the eventual freeze, regardless of the seed or connect latency.
// The test is not parallel so its tick loop is not starved by sibling servers
// under a loaded runner (#22).
//
//nolint:paralleltest // serial by design (#22): tick loop must not be CPU-starved by parallel siblings.
func TestMonsterHuntsPlayer(t *testing.T) {
	ts := startServerWithMonstersAt(t, 20*time.Millisecond, protocol.Hex{Q: -9, R: 0})

	join(t, ts, "")

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
