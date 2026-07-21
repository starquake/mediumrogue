package integration_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// postRaw posts an arbitrary body (bytes, not JSON-marshalled) to path with the
// application/json content type, so a test can exercise decodeJSON with an
// oversized or malformed payload. Registers response-body cleanup.
func postRaw(t *testing.T, ts *httptest.Server, path string, body []byte) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.URL+path, bytes.NewReader(body))
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

// TestOversizedBodyIs413: a JSON body past the size cap is 413 Payload Too
// Large — a size violation, not a syntax error, so a 400 "malformed JSON" would
// mislead a client into blaming its own encoding (#209). The body is
// well-formed JSON (a single huge string value) so only the size trips it.
func TestOversizedBodyIs413(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)

	// A valid JSON object whose token value alone blows past the 64 KiB cap.
	body := []byte(`{"token":"` + strings.Repeat("x", 70*1024) + `"}`)

	resp := postRaw(t, ts, "/api/intent", body)
	if got, want := resp.StatusCode, http.StatusRequestEntityTooLarge; got != want {
		t.Fatalf("oversized body status = %d, want %d", got, want)
	}

	var errBody protocol.ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode 413 body: %v", err)
	}

	if got := errBody.Error; got == "" {
		t.Error(`413 body error = "", want the standard error shape`)
	}
}

// TestTrailingGarbageIsRejected: a body with a second JSON value (or any
// non-whitespace) after the first is malformed and must be 400, not silently
// accepted on the strength of its valid prefix (#209). Uses a well-formed first
// value so the ONLY fault is the trailing garbage.
func TestTrailingGarbageIsRejected(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)

	body := []byte(`{"kind":"move","entityId":1,"token":"nope"}{"extra":true}`)

	resp := postRaw(t, ts, "/api/intent", body)
	if got, want := resp.StatusCode, http.StatusBadRequest; got != want {
		t.Fatalf("trailing-garbage status = %d, want %d", got, want)
	}
}
