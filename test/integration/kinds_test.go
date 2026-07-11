package integration_test

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
	"github.com/starquake/mediumrogue/internal/server"
)

// trollKillXP is troll's kill XP (internal/game/content.go's monsterDefs) —
// mirrors wolfKillXP (testmain_test.go) for a non-default kind, proving the
// award is genuinely per-kind and not just wolf's flat, well-tested number.
const trollKillXP = 60

// wantTrollKillAnnounce is killSummary's exact text for a single slain
// troll (internal/game/world.go) — the kind-naming combat log line 6c adds.
const wantTrollKillAnnounce = "a troll was slain (+60 XP to everyone in the fight)"

// startServerWithKindAt boots the handler tree with a single monster of a
// caller-chosen KIND (not just the default wolf) at hex, via
// World.SpawnMonsterKindAt — the production API milestone 6c added for
// placing a specific registered kind (mirrors testmain_test.go's
// startServerWithMonstersAt, built on the kind-blind SpawnMonsterAt). Fails
// the test if the hex refuses the spawn (not walkable or at StackCap).
func startServerWithKindAt(t *testing.T, kind string, hex protocol.Hex) *httptest.Server {
	t.Helper()

	ticks := hub.New()

	world := game.NewWorld(15*time.Millisecond, time.Minute, 5*time.Millisecond, testDisconnectGrace, 0xC0FFEE, 12, ticks)

	if !world.SpawnMonsterKindAt(hex, kind) {
		t.Fatalf("SpawnMonsterKindAt(%v, %q) = false, want a monster (not walkable or over StackCap)", hex, kind)
	}

	chatBroker := newAnnouncingChatBroker(world)
	go world.Run(t.Context())

	handler := server.New(server.Deps{
		Logger: slog.New(slog.DiscardHandler), World: world, Ticks: ticks, Chat: chatBroker,
		HeartbeatInterval: time.Hour,
	})

	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	return ts
}

// decodeAnyFrame reads one SSE frame and, if it's a turn or a chat frame,
// decodes it into the matching return value (exactly one of the two is
// non-nil); any other named frame decodes to both nil.
func decodeAnyFrame(t *testing.T, r *bufio.Reader) (*protocol.TurnEvent, *protocol.ChatMessage) {
	t.Helper()

	frame := readFrames(t, r, 1)[0]

	switch frame.event {
	case protocol.EventTurn:
		var bundle protocol.TurnEvent
		if err := json.Unmarshal([]byte(frame.data), &bundle); err != nil {
			t.Fatalf("unmarshal bundle %q: %v", frame.data, err)
		}

		return &bundle, nil
	case protocol.EventChat:
		var msg protocol.ChatMessage
		if err := json.Unmarshal([]byte(frame.data), &msg); err != nil {
			t.Fatalf("unmarshal chat %q: %v", frame.data, err)
		}

		return nil, &msg
	default:
		return nil, nil
	}
}

// TestKillSpecificKindOverHTTP proves the whole 6c per-kind combat pipeline
// over real HTTP/SSE, mirroring gear_test.go's harness: kill a seeded TROLL
// (not the default wolf) and see BOTH its own XP (60, not wolf's 20) land
// on the killer AND the exact kind-naming combat-log announce arrive over
// chat.
//
//nolint:paralleltest // serial by design (#22): tick loop must not be CPU-starved by parallel siblings.
func TestKillSpecificKindOverHTTP(t *testing.T) {
	ts := startServerWithKindAt(t, "troll", protocol.Hex{Q: 1, R: 0})

	me := join(t, ts, "")

	events := get(t, ts, "/api/events")
	reader := bufio.NewReader(events.Body)

	var (
		announced   []string
		gotAnnounce bool
		gotXP       bool
	)

	deadline := time.Now().Add(15 * time.Second)

	for time.Now().Before(deadline) && (!gotAnnounce || !gotXP) {
		bundle, chatMsg := decodeAnyFrame(t, reader)

		if chatMsg != nil {
			announced = append(announced, chatMsg.Text)

			if chatMsg.Text == wantTrollKillAnnounce {
				gotAnnounce = true
			}

			continue
		}

		if bundle == nil {
			continue
		}

		myEntity, ok := entityOf(*bundle, me.EntityID)
		if !ok {
			t.Fatal("joined player missing from turn bundle")
		}

		if myEntity.XP >= trollKillXP {
			gotXP = true
		}

		if target, found := nearestMonster(*bundle, myEntity.Hex); found {
			postIntent(t, ts, me, target)
		}
	}

	if !gotXP {
		t.Errorf("player XP never reached trollKillXP (%d) before the deadline", trollKillXP)
	}

	if !gotAnnounce {
		t.Errorf("announce %q never arrived before the deadline; saw %v", wantTrollKillAnnounce, announced)
	}

	if !slices.Contains(announced, wantTrollKillAnnounce) {
		t.Errorf("final announced = %v, want to contain %q", announced, wantTrollKillAnnounce)
	}
}
