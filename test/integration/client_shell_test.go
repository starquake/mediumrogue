package integration_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestClientShell asserts the root path serves something sensible in both
// worlds: the built client bundle (index.html) when present in the embed, or
// the explicit "not built" hint when absent. Either way it must not 404 —
// a blank page with no explanation is the one unacceptable outcome.
func TestClientShell(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)

	resp := get(t, ts, "/")

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		if got, want := strings.ToLower(string(body)), "<!doctype html"; !strings.Contains(got, want) {
			t.Fatalf("GET / returned 200 but not an HTML document: %.100q", string(body))
		}
	case http.StatusServiceUnavailable:
		if got, want := string(body), "make client"; !strings.Contains(got, want) {
			t.Fatalf("GET / returned 503 without the build hint: %.100q", got)
		}
	default:
		t.Fatalf("GET / status = %d, want 200 (built) or 503 (not built)", resp.StatusCode)
	}
}
