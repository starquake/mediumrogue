package integration_test

import (
	"bufio"
	"encoding/json"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
	"github.com/starquake/mediumrogue/internal/server"
)

// TestNecromancerRaisesAddsInCombat drives the summoner end to end over the
// REAL bubble path (not the ForTest combat bridge): a Necromancer placed within
// combat range of a joined player bubbles with them, and — after its wind-up
// cooldown counts down over successive bubble turns — raises a Risen add that
// appears on the wire as a fresh EntityMonster with monsterKind "risen". Proves
// the in-combat spawn hook fires from resolveBubbleTurnLocked, not just the
// unit bridge.
func TestNecromancerRaisesAddsInCombat(t *testing.T) {
	t.Parallel()

	ticks := hub.New()
	world := game.NewWorld(game.WorldConfig{
		Interval:        20 * time.Millisecond,
		CombatPatience:  30 * time.Millisecond, // bubble turns auto-resolve fast (player never locks in)
		BubblePoll:      5 * time.Millisecond,
		DisconnectGrace: testDisconnectGrace,
		WorldSeed:       0xC0FFEE,
		Radius:          12,
		Ticks:           ticks,
	})

	ts := serveWorld(t, world, ticks, server.Deps{HeartbeatInterval: time.Hour})

	me := join(t, ts, "")

	// Place the Necromancer within CombatRadius on an all-grass line, so mutual
	// LOS forms a bubble immediately (the same clear-line discipline
	// TestMonsterHuntsPlayer uses, so a generated map's trees can't silently
	// block the notice and evaporate the test's premise).
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

	for dist := 2; dist <= protocol.CombatRadius && !placed; dist++ {
		for _, d := range dirs {
			h := protocol.Hex{Q: me.Hex.Q + d.Q*dist, R: me.Hex.R + d.R*dist}
			if grassBetween(me.Hex, h) && world.SpawnMonsterKindAt(h, "necromancer") {
				placed = true

				break
			}
		}
	}

	if !placed {
		t.Fatal("no spawnable grass-line hex within CombatRadius for the Necromancer — widen the search")
	}

	events := get(t, ts, "/api/events")
	reader := bufio.NewReader(events.Body)

	deadline := time.Now().Add(6 * time.Second)

	for time.Now().Before(deadline) {
		frames := readFrames(t, reader, 1)

		var bundle protocol.TurnEvent
		if err := json.Unmarshal([]byte(frames[0].data), &bundle); err != nil {
			t.Fatalf("unmarshal bundle %q: %v", frames[0].data, err)
		}

		for _, e := range bundle.Entities {
			if e.Kind == protocol.EntityMonster && e.MonsterKind == "risen" && e.HP > 0 {
				return // the Necromancer raised an add over the real bubble path
			}
		}
	}

	t.Fatal("no Risen add ever appeared on the wire — the in-combat summon hook did not fire")
}
