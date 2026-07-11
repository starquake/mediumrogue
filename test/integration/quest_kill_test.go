package integration_test

import (
	"bufio"
	"encoding/json"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestKillQuestTicksOverHTTP reproduces the user's live flow end-to-end: join,
// take a KILL quest via /quest <id>, bump a monster to death, and watch the
// quest's Progress on the wire. Reported broken in live play ("the monster
// count in the quest doesn't really go down") despite green unit tests.
//
//nolint:paralleltest // deterministic monster placement (see #22): private server, no parallel siblings.
func TestKillQuestTicksOverHTTP(t *testing.T) {
	ts := startServerWithMonstersAt(t, protocol.Hex{Q: 1, R: 0})

	me := join(t, ts, "")

	events := get(t, ts, "/api/events")
	reader := bufio.NewReader(events.Body)

	// decodeTurn skips non-turn frames (the /quest chat announcement rides the
	// same stream) — decodeBundle would mis-decode a chat frame as an empty
	// TurnEvent.
	decodeTurn := func() protocol.TurnEvent {
		for {
			frames := readFrames(t, reader, 1)
			if frames[0].event != protocol.EventTurn {
				continue
			}

			var bundle protocol.TurnEvent
			if err := json.Unmarshal([]byte(frames[0].data), &bundle); err != nil {
				t.Fatalf("unmarshal bundle %q: %v", frames[0].data, err)
			}

			return bundle
		}
	}

	first := decodeTurn()

	// Take the first available KILL quest.
	var questID int64

	for _, q := range first.Quests {
		if q.Kind == "kill" && q.State == protocol.QuestAvailable {
			questID = q.ID

			break
		}
	}

	if questID == 0 {
		t.Fatal("no available kill quest on the board")
	}

	resp := postJSON(t, ts, "/api/chat", protocol.ChatRequest{Token: me.Token, Text: "/quest 1"})
	if got, want := resp.StatusCode, 202; got != want {
		t.Fatalf("take quest status = %d, want %d", got, want)
	}

	// Fight the monster to death, watching quest progress the whole time.
	deadline := time.Now().Add(10 * time.Second)

	lastProgress := -1

	for time.Now().Before(deadline) {
		bundle := decodeTurn()

		myEntity, ok := entityOf(bundle, me.EntityID)
		if !ok {
			t.Fatal("joined player missing from turn bundle")
		}

		for _, q := range bundle.Quests {
			if q.ID == questID {
				lastProgress = q.Progress
			}
		}

		if lastProgress > 0 {
			return // the kill quest ticked over the wire — works
		}

		if myEntity.XP >= protocol.MonsterXP && lastProgress == 0 {
			// The kill LANDED (XP arrived) but the quest never ticked: give it a
			// few more bundles to arrive, then fail loudly.
			for range 5 {
				bundle = decodeTurn()
				for _, q := range bundle.Quests {
					if q.ID == questID && q.Progress > 0 {
						return
					}
				}
			}

			t.Fatalf("kill landed (xp=%d) but quest %d progress still 0 — kill quests don't tick over HTTP",
				myEntity.XP, questID)
		}

		if target, found := nearestMonster(bundle, myEntity.Hex); found {
			postIntent(t, ts, me, target)
		}
	}

	t.Fatalf("no kill landed before deadline (last progress=%d)", lastProgress)
}
