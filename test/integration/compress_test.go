package integration_test

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// getEncoded fetches path with an explicit Accept-Encoding, which also stops
// Go's transport from transparently decompressing — so the caller sees the
// bytes that actually crossed the wire, and Content-Encoding survives on the
// response. (The transport only auto-decodes the gzip IT asked for.)
func getEncoded(t *testing.T, ts *httptest.Server, path, encoding string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL+path, nil)
	if err != nil {
		t.Fatalf("build request for %s: %v", path, err)
	}

	req.Header.Set("Accept-Encoding", encoding)

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}

	t.Cleanup(func() { _ = resp.Body.Close() })

	return resp
}

// TestMapIsGzippedOnTheWire pins #288 for the largest single payload: at the
// deployed WORLD_RADIUS=120 the map is 1.89 MB uncompressed and ~119 KB
// gzipped. The harness world is small, but the same middleware decides it, so
// this asserts the mechanism (header, decodability, real size reduction)
// rather than a size that would drift with the test radius.
func TestMapIsGzippedOnTheWire(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)

	resp := getEncoded(t, ts, "/api/map", "gzip")
	if got, want := resp.StatusCode, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}

	if got, want := resp.Header.Get("Content-Encoding"), "gzip"; got != want {
		t.Fatalf("Content-Encoding = %q, want %q", got, want)
	}

	compressed := readAllBody(t, resp)

	zr, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("gzip.NewReader over /api/map: %v", err)
	}
	defer func() { _ = zr.Close() }()

	var m protocol.MapResponse
	if err := json.NewDecoder(zr).Decode(&m); err != nil {
		t.Fatalf("decode gzipped map: %v", err)
	}

	if got, want := m.Radius, testWorldRadius; got != want {
		t.Fatalf("radius = %d, want %d", got, want)
	}

	wantTiles := 3*testWorldRadius*(testWorldRadius+1) + 1
	if got, want := len(m.Tiles), wantTiles; got != want {
		t.Fatalf("len(tiles) = %d, want %d", got, want)
	}

	identity := readAllBody(t, getEncoded(t, ts, "/api/map", "identity"))
	if got, want := len(compressed), len(identity); got >= want {
		t.Errorf("gzipped map = %d bytes, want fewer than identity %d", got, want)
	}
}

// TestMapIsNotGzippedForIdentityClients: a client that does not offer gzip
// still gets a plain body, not an undecodable one.
func TestMapIsNotGzippedForIdentityClients(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)

	resp := getEncoded(t, ts, "/api/map", "identity")
	if got := resp.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty", got)
	}

	var m protocol.MapResponse
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("decode identity map: %v", err)
	}
}

// TestTurnFramesArriveOnAGzippedStream is the end-to-end guard against the
// failure mode that makes this middleware dangerous: turn bundles sitting in
// the compressor's window instead of reaching the client.
//
// A stalled stream does not fail loudly — the connection stays open and the
// game simply freezes — so the assertion is a real deadline on a real socket
// with a real gzip decoder in between. The turn interval is short and the
// deadline is generous: this must fail because compression stalled the stream,
// never because the box was busy.
func TestTurnFramesArriveOnAGzippedStream(t *testing.T) {
	t.Parallel()

	ts := startServer(t, 50*time.Millisecond, time.Hour)

	// The deadline rides on the request context rather than a racing timer:
	// decodeTurnFrame reports failures with t.Fatalf, which must run on the
	// test goroutine. If compression stalls the stream, the deadline fires,
	// the body read fails, and that surfaces as a normal test failure here.
	// Generous on purpose — this must fail because the stream stalled, never
	// because the box was busy.
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/events", nil)
	if err != nil {
		t.Fatalf("build events request: %v", err)
	}

	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /api/events: %v", err)
	}

	t.Cleanup(func() { _ = resp.Body.Close() })

	if got, want := resp.Header.Get("Content-Encoding"), "gzip"; got != want {
		t.Fatalf("Content-Encoding = %q, want %q — the stream was not compressed", got, want)
	}

	// gzip.NewReader consumes the header, which only returns once the server
	// has flushed real bytes: already a check that the stream is not stalled.
	zr, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader over the SSE stream: %v", err)
	}
	defer func() { _ = zr.Close() }()

	if bundle := decodeTurnFrame(t, bufio.NewReader(zr)); bundle.IntervalMs <= 0 {
		t.Errorf("bundle.IntervalMs = %d, want positive", bundle.IntervalMs)
	}
}
