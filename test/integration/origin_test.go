package integration_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// postJSONWithOrigin is postJSON plus an explicit Origin header, for the #97
// same-origin guard tests.
func postJSONWithOrigin(t *testing.T, ts *httptest.Server, path, origin string, body any) *http.Response {
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
	req.Header.Set("Origin", origin)

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}

	t.Cleanup(func() { _ = resp.Body.Close() })

	return resp
}

// TestCrossOriginPostRejected drives the #97 guard over real HTTP: a POST
// declaring a foreign Origin is rejected with 403 on every mutating route,
// before any body decoding or game-state lookup runs.
func TestCrossOriginPostRejected(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Minute, time.Minute)

	for _, path := range []string{"/api/join", "/api/intent", "/api/chat"} {
		resp := postJSONWithOrigin(t, ts, path, "https://evil.example", struct{}{})
		if got, want := resp.StatusCode, http.StatusForbidden; got != want {
			t.Errorf("cross-origin POST %s status = %d, want %d", path, got, want)
		}
	}
}

// TestSameOriginAndHeaderlessPostsStillWork is the guard's non-breakage half:
// a POST with a MATCHING Origin (what the served browser client sends) and a
// POST with no Origin at all (curl, this test suite) both still join fine.
func TestSameOriginAndHeaderlessPostsStillWork(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Minute, time.Minute)

	// Matching Origin: ts.URL is exactly this server's scheme://host:port.
	resp := postJSONWithOrigin(t, ts, "/api/join", ts.URL,
		protocol.JoinRequest{Name: testerName, Class: protocol.ClassFighter, Species: protocol.SpeciesHuman})
	if got, want := resp.StatusCode, http.StatusOK; got != want {
		t.Fatalf("same-origin POST /api/join status = %d, want %d", got, want)
	}

	var joined protocol.JoinResponse
	if err := json.NewDecoder(resp.Body).Decode(&joined); err != nil {
		t.Fatalf("decode join response: %v", err)
	}

	if joined.Token == "" {
		t.Error("same-origin join returned an empty token")
	}

	// No Origin header at all: the guard must not demand one.
	resp = postJSON(t, ts, "/api/join", protocol.JoinRequest{
		Token: joined.Token, Name: testerName, Class: protocol.ClassFighter, Species: protocol.SpeciesHuman,
	})
	if got, want := resp.StatusCode, http.StatusOK; got != want {
		t.Fatalf("header-less POST /api/join status = %d, want %d", got, want)
	}
}
