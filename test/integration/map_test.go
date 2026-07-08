package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

func TestMapEndpoint(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)

	resp := get(t, ts, "/api/map")
	if got, want := resp.StatusCode, http.StatusOK; got != want {
		t.Fatalf("status = %d, want 200", got)
	}

	if got, want := resp.Header.Get("Content-Type"), "application/json"; got != want {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	var m protocol.MapResponse
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("decode map: %v", err)
	}

	if got, want := m.Radius, game.MapRadius; got != want {
		t.Fatalf("radius = %d, want %d", got, want)
	}

	wantTiles := 3*game.MapRadius*(game.MapRadius+1) + 1
	if got, want := len(m.Tiles), wantTiles; got != want {
		t.Fatalf("len(tiles) = %d, want %d", got, want)
	}
}
