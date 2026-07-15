package integration_test

import (
	"bufio"
	"encoding/json"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// hexDistance mirrors the cube-coordinate distance used server-side.
// Duplicated on purpose (see neighborsOf in multiplayer_test.go): an
// integration test asserting wire behavior should not silently co-move with
// the implementation's hex math.
func hexDistance(a, b protocol.Hex) int {
	dq, dr := a.Q-b.Q, a.R-b.R
	ds := -dq - dr

	abs := func(n int) int {
		if n < 0 {
			return -n
		}

		return n
	}

	return (abs(dq) + abs(dr) + abs(ds)) / 2
}

// nearestMonster returns the hex of the alive monster in bundle closest to
// from (by hex distance), and whether any monster is present.
func nearestMonster(bundle protocol.TurnEvent, from protocol.Hex) (protocol.Hex, bool) {
	var (
		best     protocol.Hex
		bestDist int
		found    bool
	)

	for _, e := range bundle.Entities {
		if e.Kind != protocol.EntityMonster {
			continue
		}

		d := hexDistance(from, e.Hex)
		if !found || d < bestDist {
			best, bestDist, found = e.Hex, d, true
		}
	}

	return best, found
}

// TestCombatOverHTTP exercises milestone 6.3 combat over real HTTP/SSE: a
// joined player is driven turn by turn toward whichever monster is nearest
// (recomputed from the latest bundle every turn, since monsters hunt the player
// too — a fixed target could go stale as the board evolves). It asserts two
// independent TRENDS rather than exact hexes or HP values:
//
//  1. some monster's HP falls below its starting value, or it disappears
//     from the snapshot entirely (killed) — the player's melee attack lands.
//  2. the player's own HP drops below max at some point — a monster's
//     hunting AI closes distance and strikes back, unprompted by the test.
//
// The monster is seeded one hex from the origin (where the player spawns), so
// both combat directions land within a couple of bubble-turns resolved on the
// player's own lock-ins, rather than after a long crypto-random chase gated on
// the background tick loop — deterministic and robust even under a CPU-starved
// runner (#22). The test is not parallel so its tick loop is not starved by
// sibling servers.
//
//nolint:paralleltest // serial by design (#22): tick loop must not be CPU-starved by parallel siblings.
func TestCombatOverHTTP(t *testing.T) {
	ts := startServerWithMonstersAt(t, protocol.Hex{Q: 1, R: 0})

	me := join(t, ts, "")

	events := get(t, ts, "/api/events")
	reader := bufio.NewReader(events.Body)

	firstFrame := readFrames(t, reader, 1)

	var first protocol.TurnEvent
	if err := json.Unmarshal([]byte(firstFrame[0].data), &first); err != nil {
		t.Fatalf("unmarshal bundle %q: %v", firstFrame[0].data, err)
	}

	startHP := make(map[int64]int)

	for _, e := range first.Entities {
		if e.Kind == protocol.EntityMonster {
			startHP[e.ID] = e.HP
		}
	}

	if len(startHP) == 0 {
		t.Fatal("no monsters present in first bundle")
	}

	var monsterDamaged, playerDamaged bool

	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) {
		frames := readFrames(t, reader, 1)

		var bundle protocol.TurnEvent
		if err := json.Unmarshal([]byte(frames[0].data), &bundle); err != nil {
			t.Fatalf("unmarshal bundle %q: %v", frames[0].data, err)
		}

		myHex := hexOf(bundle, me.EntityID)
		if myHex == (protocol.Hex{Q: -999, R: -999}) {
			t.Fatal("joined player missing from turn bundle")
		}

		// Chase whichever monster is nearest right now — a fixed target can
		// go stale since monsters hunt the player back.
		if target, found := nearestMonster(bundle, myHex); found {
			postIntent(t, ts, me, target)
		}

		seenNow := make(map[int64]bool, len(startHP))

		for _, e := range bundle.Entities {
			if e.Kind != protocol.EntityMonster {
				continue
			}

			seenNow[e.ID] = true

			if start, tracked := startHP[e.ID]; tracked && e.HP < start {
				monsterDamaged = true
			}
		}

		for id := range startHP {
			if !seenNow[id] {
				monsterDamaged = true // killed and removed from the snapshot
			}
		}

		for _, e := range bundle.Entities {
			// The joined player is a Fighter by default (empty class in join);
			// compare against its reported max, not the retired flat PlayerMaxHP.
			if e.ID == me.EntityID && e.HP < e.MaxHP {
				playerDamaged = true
			}
		}

		if monsterDamaged && playerDamaged {
			return // both combat directions proven end to end
		}
	}

	t.Fatalf(
		"combat trends not observed before deadline: monsterDamaged=%v playerDamaged=%v",
		monsterDamaged, playerDamaged,
	)
}
