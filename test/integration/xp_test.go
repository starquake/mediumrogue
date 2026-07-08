package integration_test

import (
	"bufio"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestXPRisesOnMonsterKillOverHTTP exercises milestone 6b.1's headline
// behavior over real HTTP/SSE: a joined player chases whichever monster is
// nearest (re-targeted every bundle — same rationale as TestCombatOverHTTP,
// since monster spawn hexes are seeded from crypto/rand and monsters hunt the
// player back) until it lands a killing blow. It asserts the player's own
// XP, as carried on the wire, rises by at least protocol.MonsterXP, and that
// Level tracks the server's flat curve (1 + xp/XPPerLevel) at every
// observation — not just the final one.
//
// A player starts at XP 0 / Level 1 and one kill awards the full
// protocol.MonsterXP, so "xp reaches >= MonsterXP" is a clean, one-directional
// signal: it cannot happen without at least one kill, and it is robust to
// exactly which monster dies or how many turns the chase takes. A generous
// turn budget (fast clock, long deadline) and a loud t.Fatalf keep this from
// being flaky if the initial spawn distance is large; see task-3-report.md
// for the multi-run results.
func TestXPRisesOnMonsterKillOverHTTP(t *testing.T) {
	t.Parallel()

	const monsterCount = 8

	ts := startServerWithMonsters(t, 15*time.Millisecond, time.Hour, monsterCount)

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
