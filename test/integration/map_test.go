package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/starquake/medium-rogue/internal/game"
	"github.com/starquake/medium-rogue/internal/protocol"
)

func TestMapEndpoint(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)

	resp := get(t, ts, "/api/map")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	var m protocol.MapResponse
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("decode map: %v", err)
	}

	if m.Radius != game.MapRadius {
		t.Fatalf("radius = %d, want %d", m.Radius, game.MapRadius)
	}

	wantTiles := 3*game.MapRadius*(game.MapRadius+1) + 1
	if len(m.Tiles) != wantTiles {
		t.Fatalf("len(tiles) = %d, want %d", len(m.Tiles), wantTiles)
	}
}
