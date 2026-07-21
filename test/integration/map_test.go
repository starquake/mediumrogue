package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// testWorldRadius is the hex radius startServer's harness boots the world
// with (see startServerWithBubbleTuning's game.NewWorld call).
const testWorldRadius = 12

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

	if got, want := m.Radius, testWorldRadius; got != want {
		t.Fatalf("radius = %d, want %d", got, want)
	}

	wantTiles := 3*testWorldRadius*(testWorldRadius+1) + 1
	if got, want := len(m.Tiles), wantTiles; got != want {
		t.Fatalf("len(tiles) = %d, want %d", got, want)
	}
}

// TestMapEndpointServesCachedBytes: the map is marshalled once and served from
// cache (#209), so two requests return byte-for-byte identical bodies. A stable
// payload is the whole point of the cache — this pins that the optimization is
// behaviour-preserving.
func TestMapEndpointServesCachedBytes(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)

	first := readAllBody(t, get(t, ts, "/api/map"))
	second := readAllBody(t, get(t, ts, "/api/map"))

	if !bytes.Equal(first, second) {
		t.Fatalf("two /api/map responses differ:\nfirst  = %s\nsecond = %s", first, second)
	}

	// And the cached bytes must still be a valid MapResponse, not some stale or
	// truncated blob.
	var m protocol.MapResponse
	if err := json.Unmarshal(first, &m); err != nil {
		t.Fatalf("cached map body is not valid MapResponse: %v", err)
	}

	if got, want := m.Radius, testWorldRadius; got != want {
		t.Fatalf("cached map radius = %d, want %d", got, want)
	}
}

// readAllBody drains and returns a response body.
func readAllBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	return body
}

// TestMapIsGeneratedAndWalkableNearOrigin exercises the seeded procedural
// generator end-to-end over real HTTP: the served map has a walkable origin
// (the forced clearing, so a joined player can move immediately), a rock rim
// (spot-checked at a couple of distance==radius hexes), and more than one
// distinct terrain (proof it's real generated variety, not a flat fill).
//
// This deliberately stops short of asserting all four biomes are present —
// that's only guaranteed at radius 24 (see
// internal/game.TestGenerateMapShape); at the test harness's radius 12 a
// smaller sample of the same noise field may not roll every terrain.
//
// Seed determinism (same seed -> identical map, different seed -> different
// map) is NOT re-tested here: the integration harness
// (startServer/startServerWithBubbleTuning) constructs game.NewWorld directly
// with a hardcoded seed/radius and doesn't thread a per-server WORLD_SEED, so
// a genuine two-server same-seed/different-seed integration test isn't
// possible without faking the harness. That property is already covered at
// the unit layer by internal/game.TestGenerateMapIsDeterministic and
// TestGenerateMapDiffersBySeed.
func TestMapIsGeneratedAndWalkableNearOrigin(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)

	resp := get(t, ts, "/api/map")
	if got, want := resp.StatusCode, http.StatusOK; got != want {
		t.Fatalf("status = %d, want 200", got)
	}

	var m protocol.MapResponse
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("decode map: %v", err)
	}

	origin := protocol.Hex{Q: 0, R: 0}

	originTerrain, ok := terrainAtHex(m, origin)
	if !ok {
		t.Fatalf("origin %v missing from tiles", origin)
	}

	if got := originTerrain; got != protocol.TerrainGrass && got != protocol.TerrainForest {
		t.Errorf("origin terrain = %q, want grass or forest (walkable)", got)
	}

	// Spot-check a couple of rim hexes (distance == radius from origin): the
	// generator forces the rim to impassable rock.
	rimHexes := []protocol.Hex{
		{Q: testWorldRadius, R: 0},
		{Q: 0, R: testWorldRadius},
		{Q: -testWorldRadius, R: testWorldRadius},
	}
	for _, h := range rimHexes {
		terr, ok := terrainAtHex(m, h)
		if !ok {
			t.Fatalf("rim hex %v missing from tiles", h)
		}

		if got, want := terr, protocol.TerrainRock; got != want {
			t.Errorf("rim hex %v terrain = %q, want %q", h, got, want)
		}
	}

	// Real generated variety: more than one distinct terrain among the tiles
	// (guards against a degenerate flat-fill map slipping past the endpoint).
	distinct := map[protocol.Terrain]bool{}
	for _, tile := range m.Tiles {
		distinct[tile.Terrain] = true
	}

	if got, wantMin := len(distinct), 2; got < wantMin {
		t.Errorf("distinct terrains = %d, want >= %d (tiles: %+v)", got, wantMin, distinct)
	}
}

// terrainAtHex scans m.Tiles for h and reports its terrain, or ok=false if h
// isn't present.
func terrainAtHex(m protocol.MapResponse, h protocol.Hex) (protocol.Terrain, bool) {
	for _, tile := range m.Tiles {
		if tile.Hex == h {
			return tile.Terrain, true
		}
	}

	return "", false
}
