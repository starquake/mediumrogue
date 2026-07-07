package integration_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/starquake/medium-rogue/internal/protocol"
)

// postJSON posts body as JSON and registers response-body cleanup.
func postJSON(t *testing.T, ts *httptest.Server, path string, body any) *http.Response {
	t.Helper()

	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.URL+path, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}

	t.Cleanup(func() { _ = resp.Body.Close() })

	return resp
}

func join(t *testing.T, ts *httptest.Server, token string) protocol.JoinResponse {
	t.Helper()

	resp := postJSON(t, ts, "/api/join", protocol.JoinRequest{Token: token})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("join status = %d, want 200", resp.StatusCode)
	}

	var joined protocol.JoinResponse
	if err := json.NewDecoder(resp.Body).Decode(&joined); err != nil {
		t.Fatalf("decode join response: %v", err)
	}

	return joined
}

// TestTurnLoopMovesEntity drives the heart of the game over real HTTP: join,
// submit a step intent, and watch the SSE turn stream deliver the moved
// entity on a subsequent turn bundle.
func TestTurnLoopMovesEntity(t *testing.T) {
	t.Parallel()

	ts := startServer(t, 20*time.Millisecond, time.Hour)

	me := join(t, ts, "")

	// Find a walkable neighbor by reading the map like a real client.
	var worldMap protocol.MapResponse

	mapResp := get(t, ts, "/api/map")
	if err := json.NewDecoder(mapResp.Body).Decode(&worldMap); err != nil {
		t.Fatalf("decode map: %v", err)
	}

	walkable := make(map[protocol.Hex]bool)

	for _, tile := range worldMap.Tiles {
		if tile.Terrain == protocol.TerrainGrass || tile.Terrain == protocol.TerrainForest {
			walkable[tile.Hex] = true
		}
	}

	target := protocol.Hex{}
	found := false

	for _, n := range neighborsOf(me.Hex) {
		if walkable[n] {
			target, found = n, true

			break
		}
	}

	if !found {
		t.Fatalf("spawn %v has no walkable neighbor", me.Hex)
	}

	intent := protocol.IntentRequest{EntityID: me.EntityID, Token: me.Token, Target: target}
	if resp := postJSON(t, ts, "/api/intent", intent); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("intent status = %d, want 202", resp.StatusCode)
	}

	// Watch the stream until the entity stands on the target.
	events := get(t, ts, "/api/events")
	reader := bufio.NewReader(events.Body)
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		frames := readFrames(t, reader, 1, 5*time.Second)

		var bundle protocol.TurnEvent
		if err := json.Unmarshal([]byte(frames[0].data), &bundle); err != nil {
			t.Fatalf("unmarshal bundle %q: %v", frames[0].data, err)
		}

		for _, e := range bundle.Entities {
			if e.ID == me.EntityID && e.Hex == target {
				return // moved — the loop works end to end
			}
		}
	}

	t.Fatal("entity never reached the intent target via the turn stream")
}

func TestIntentRejectsBadToken(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)
	me := join(t, ts, "")

	intent := protocol.IntentRequest{EntityID: me.EntityID, Token: "forged", Target: me.Hex}
	if resp := postJSON(t, ts, "/api/intent", intent); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestJoinReclaimsEntityByToken(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)

	first := join(t, ts, "")
	again := join(t, ts, first.Token)

	if again.EntityID != first.EntityID {
		t.Fatalf("re-join minted a new entity: %d != %d", again.EntityID, first.EntityID)
	}
}

// neighborsOf mirrors the flat-top neighbor offsets. Duplicated from
// internal/game on purpose: an integration test asserting wire behavior
// should not silently co-move with the implementation's hex math.
func neighborsOf(h protocol.Hex) []protocol.Hex {
	return []protocol.Hex{
		{Q: h.Q, R: h.R - 1},
		{Q: h.Q + 1, R: h.R - 1},
		{Q: h.Q + 1, R: h.R},
		{Q: h.Q, R: h.R + 1},
		{Q: h.Q - 1, R: h.R + 1},
		{Q: h.Q - 1, R: h.R},
	}
}
