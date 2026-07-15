package integration_test

import (
	"bufio"
	"math"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// levelForQuadraticCurve mirrors internal/game's unexported levelFor for
// this wire-level assertion: the 1-based level for a cumulative XP total is
// 1 + isqrt(xp/XPCurveBase). Integer sqrt (not math.Sqrt alone) avoids
// float mis-rounding at perfect squares, matching the server's own math.
func levelForQuadraticCurve(xp int) int {
	n := xp / protocol.XPCurveBase

	s := int(math.Sqrt(float64(n)))
	for s > 0 && s*s > n {
		s--
	}

	for (s+1)*(s+1) <= n {
		s++
	}

	return 1 + s
}

// TestXPRisesOnMonsterKillOverHTTP exercises milestone 6b.1's headline
// behavior over real HTTP/SSE: a joined player drives the nearest monster
// (re-targeted every bundle, since monsters hunt back and can shift the board)
// until it lands a killing blow. It asserts the player's own XP, as carried on
// the wire, rises by at least wolfKillXP, and that Level tracks the
// server's quadratic curve (1 + isqrt(xp/XPCurveBase)) at every observation —
// not just the final one.
//
// A player starts at XP 0 / Level 1 and one kill of the seeded (default
// wolf) monster awards the full wolfKillXP, so "xp reaches >= wolfKillXP" is
// a clean, one-directional signal: it cannot happen without at least one
// kill, and it is robust to how many turns the fight takes.
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
	ts := startServerWithMonstersAt(t, protocol.Hex{Q: 1, R: 0})

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

		// The server's quadratic leveling curve must hold on every single
		// observation, not just at the end.
		if got, want := myEntity.Level, levelForQuadraticCurve(myEntity.XP); got != want {
			t.Fatalf("player Level = %d, want %d (xp=%d, XPCurveBase=%d)",
				got, want, myEntity.XP, protocol.XPCurveBase)
		}

		if myEntity.XP >= wolfKillXP {
			return // a kill landed and the reward reached the player over the wire
		}

		if id, target, found := nearestMonsterID(bundle, myEntity.Hex); found {
			if hexDistance(myEntity.Hex, target) == 1 {
				postEntityAttackIntent(t, ts, me, id)
			} else {
				postIntent(t, ts, me, target)
			}
		}
	}

	t.Fatalf("player XP never reached wolfKillXP (%d) before deadline: last xp=%d level=%d",
		wolfKillXP, lastXP, lastLevel)
}
