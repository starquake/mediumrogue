package integration_test

import (
	"io"
	"net/http"
	"testing"
	"time"
)

func TestHealthz(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)

	resp := get(t, ts, "/healthz")
	if got, want := resp.StatusCode, http.StatusOK; got != want {
		t.Fatalf("status = %d, want 200", got)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if got, want := string(body), "ok\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)

	resp := get(t, ts, "/healthz")

	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "same-origin",
	} {
		if got := resp.Header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}

	if resp.Header.Get("Content-Security-Policy") == "" {
		t.Error("Content-Security-Policy header missing")
	}
}
