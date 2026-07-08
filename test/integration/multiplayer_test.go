package integration_test

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestSimultaneousResolutionMovesBothEntities: two joined clients each submit a
// step, and one turn bundle carries BOTH entities at their new positions —
// proving intents from multiple clients resolve together in a single turn.
func TestSimultaneousResolutionMovesBothEntities(t *testing.T) {
	t.Parallel()

	ts := startServer(t, 20*time.Millisecond, time.Hour)

	a := join(t, ts, "")

	b := join(t, ts, "")
	if a.EntityID == b.EntityID {
		t.Fatal("two joins must yield distinct entities")
	}

	var worldMap protocol.MapResponse
	if err := json.NewDecoder(get(t, ts, "/api/map").Body).Decode(&worldMap); err != nil {
		t.Fatalf("decode map: %v", err)
	}

	walkable := make(map[protocol.Hex]bool)

	for _, tile := range worldMap.Tiles {
		if tile.Terrain == protocol.TerrainGrass || tile.Terrain == protocol.TerrainForest {
			walkable[tile.Hex] = true
		}
	}

	targetA := walkableNeighborOf(t, a.Hex, walkable)
	targetB := walkableNeighborOf(t, b.Hex, walkable)

	postIntent(t, ts, a, targetA)
	postIntent(t, ts, b, targetB)

	events := get(t, ts, "/api/events")
	reader := bufio.NewReader(events.Body)
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		frames := readFrames(t, reader, 1)

		var bundle protocol.TurnEvent
		if err := json.Unmarshal([]byte(frames[0].data), &bundle); err != nil {
			t.Fatalf("unmarshal bundle %q: %v", frames[0].data, err)
		}

		if hexOf(bundle, a.EntityID) == targetA && hexOf(bundle, b.EntityID) == targetB {
			return // both moved in one bundle
		}
	}

	t.Fatal("both entities never reached their targets in one bundle")
}

func postIntent(t *testing.T, ts *httptest.Server, me protocol.JoinResponse, target protocol.Hex) {
	t.Helper()

	intent := protocol.IntentRequest{EntityID: me.EntityID, Token: me.Token, Target: target}
	if resp := postJSON(t, ts, "/api/intent", intent); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("intent status = %d, want 202", resp.StatusCode)
	}
}

func hexOf(bundle protocol.TurnEvent, id int64) protocol.Hex {
	for _, e := range bundle.Entities {
		if e.ID == id {
			return e.Hex
		}
	}

	return protocol.Hex{Q: -999, R: -999}
}

func walkableNeighborOf(t *testing.T, from protocol.Hex, walkable map[protocol.Hex]bool) protocol.Hex {
	t.Helper()

	for _, n := range neighborsOf(from) {
		if walkable[n] {
			return n
		}
	}

	t.Fatalf("no walkable neighbor of %v", from)

	return protocol.Hex{}
}
