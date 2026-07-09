package integration_test

import (
	"bufio"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestXPRisesOnMonsterKillOverHTTP exercises milestone 6b.1's headline
// behavior over real HTTP/SSE: a joined player drives the nearest monster
// (re-targeted every bundle, since monsters hunt back and can shift the board)
// until it lands a killing blow. It asserts the player's own XP, as carried on
// the wire, rises by at least protocol.MonsterXP, and that Level tracks the
// server's flat curve (1 + xp/XPPerLevel) at every observation — not just the
// final one.
//
// A player starts at XP 0 / Level 1 and one kill awards the full
// protocol.MonsterXP, so "xp reaches >= MonsterXP" is a clean, one-directional
// signal: it cannot happen without at least one kill, and it is robust to
// exactly which monster dies or how many turns the fight takes.
//
// The monster is seeded one hex from the origin (where the player spawns), so
// the fight is a 1–3 turn bubble brawl resolved on the player's own lock-ins
// (each intent POST resolves a bubble-turn synchronously) rather than a long
// crypto-random chase gated on the background tick loop — deterministic and
// robust even under a CPU-starved runner (#22). The test is not parallel so its
// tick loop is not starved by sibling servers.
//
//nolint:paralleltest // serial by design (#22): tick loop must not be CPU-starved by parallel siblings.
func TestXPRisesOnMonsterKillOverHTTP(t *testing.T) {
	ts := startServerWithMonstersAt(t, 15*time.Millisecond, protocol.Hex{Q: 1, R: 0})

	me := join(t, ts, "")

	events := get(t, ts, "/api/events")
	reader := bufio.NewReader(events.Body)

	first := decodeBundle(t, reader)

	myEntity, ok := entityOf(first, me.EntityID)
	if !ok {
		t.Fatal("joined player missing from first turn bundle")
	}

	if got, want := myEntity.XP, 0; got != want {
		t.Fatalf("fresh player XP = %d, want %d", got, want)
	}

	if got, want := myEntity.Level, 1; got != want {
		t.Fatalf("fresh player Level = %d, want %d", got, want)
	}

	deadline := time.Now().Add(10 * time.Second)

	var lastXP, lastLevel int

	for time.Now().Before(deadline) {
		bundle := decodeBundle(t, reader)

		myEntity, ok := entityOf(bundle, me.EntityID)
		if !ok {
			t.Fatal("joined player missing from turn bundle")
		}

		lastXP, lastLevel = myEntity.XP, myEntity.Level

		// The server's flat leveling curve must hold on every single
		// observation, not just at the end.
		if got, want := myEntity.Level, 1+myEntity.XP/protocol.XPPerLevel; got != want {
			t.Fatalf("player Level = %d, want %d (xp=%d, XPPerLevel=%d)",
				got, want, myEntity.XP, protocol.XPPerLevel)
		}

		if myEntity.XP >= protocol.MonsterXP {
			return // a kill landed and the reward reached the player over the wire
		}

		if target, found := nearestMonster(bundle, myEntity.Hex); found {
			postIntent(t, ts, me, target)
		}
	}

	t.Fatalf("player XP never reached MonsterXP (%d) before deadline: last xp=%d level=%d",
		protocol.MonsterXP, lastXP, lastLevel)
}
