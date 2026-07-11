package game_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// slogEvents parses buf's newline-delimited JSON log lines into a slice of
// attribute maps, so a test can assert on the "event" attribute without
// coupling to slog's text-encoding layout.
func slogEvents(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()

	var out []map[string]any

	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}

		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("unmarshal log line %q: %v", line, err)
		}

		out = append(out, m)
	}

	return out
}

func eventsOfKind(events []map[string]any, kind string) []map[string]any {
	var out []map[string]any

	for _, e := range events {
		if e["msg"] == "combat" && e["event"] == kind {
			out = append(out, e)
		}
	}

	return out
}

// TestCombatSlogMove: a plain step onto an empty walkable hex logs a "move"
// event carrying id/kind/from/to (item 1: the milestone-12 analytics seed).
func TestCombatSlogMove(t *testing.T) {
	t.Parallel()

	w := newWorld()

	var buf bytes.Buffer

	w.SetLogger(slog.New(slog.NewJSONHandler(&buf, nil)))

	from := protocol.Hex{Q: 0, R: 0}
	to := walkableNeighbor(t, w, from)

	if !isWalkable(w, from) {
		t.Skip("origin is not walkable on this map")
	}

	id, _ := w.PlaceEntityForTest(from)
	w.SetPathForTest(id, []protocol.Hex{to})

	w.ResolveCombatOnlyForTest()

	events := slogEvents(t, &buf)

	moves := eventsOfKind(events, "move")
	if len(moves) == 0 {
		t.Fatalf("no move events logged; events = %v", events)
	}

	if got, want := moves[0]["id"], float64(id); got != want {
		t.Errorf("move event id = %v, want %v", got, want)
	}
}

// TestCombatSlogAttackDeathXP: a one-hit-kill bump inside a bubble emits the
// attack → death → xp_award sequence on the injected logger, each
// filterable by the "event" attribute.
func TestCombatSlogAttackDeathXP(t *testing.T) {
	t.Parallel()

	w := newWorld()

	var buf bytes.Buffer

	w.SetLogger(slog.New(slog.NewJSONHandler(&buf, nil)))

	oneHitKillBubble(t, w, killMissSeed)

	events := slogEvents(t, &buf)

	attacks := eventsOfKind(events, "attack")
	if len(attacks) == 0 {
		t.Fatalf("no attack events logged; events = %v", events)
	}

	if _, ok := attacks[0]["weapon"]; !ok {
		t.Errorf("attack event missing weapon attr: %v", attacks[0])
	}

	if _, ok := attacks[0]["dealt"]; !ok {
		t.Errorf("attack event missing dealt attr: %v", attacks[0])
	}

	deaths := eventsOfKind(events, "death")
	if len(deaths) == 0 {
		t.Fatalf("no death events logged; events = %v", events)
	}

	if got, want := deaths[0]["kind"], string(protocol.EntityMonster); got != want {
		t.Errorf("death event kind = %v, want %v", got, want)
	}

	if got := eventsOfKind(events, "xp_award"); len(got) == 0 {
		t.Errorf("no xp_award events logged; events = %v", events)
	}
}

// TestCombatSlogRangedFizzleOutOfRange: a bow shot whose target has moved out
// of range by resolution time logs a "fizzle" event (reason
// out_of_range) instead of an "attack" event.
func TestCombatSlogRangedFizzleOutOfRange(t *testing.T) {
	t.Parallel()

	w := newWorld()

	var buf bytes.Buffer

	w.SetLogger(slog.New(slog.NewJSONHandler(&buf, nil)))

	center := protocol.Hex{Q: 0, R: 0}
	far := protocol.Hex{Q: game.ItemRangeForTest("shortbow") + 5, R: 0}

	shooterID, _ := w.PlaceEntityForTest(center)
	w.SetClassForTest(shooterID, protocol.ClassRogue)

	w.PlaceMonsterForTest(far)

	// The attack target hex is out of the shortbow's range from the shooter
	// — SetAttackTargetForTest bypasses queueAttackLocked's submit-time
	// ErrOutOfRange rejection (a real intent could never reach this state
	// today; see resolveRangedLocked's doc comment on the resolution-time
	// re-check being defensive), exercising the fizzle path directly.
	w.SetAttackTargetForTest(shooterID, far)

	w.ResolveCombatOnlyForTest()

	events := slogEvents(t, &buf)

	fizzles := eventsOfKind(events, "fizzle")
	if len(fizzles) == 0 {
		t.Fatalf("no fizzle events logged; events = %v", events)
	}

	found := false

	for _, f := range fizzles {
		if f["reason"] == "out_of_range" {
			found = true
		}
	}

	if !found {
		t.Errorf("fizzle events = %v, want one with reason out_of_range", fizzles)
	}
}
