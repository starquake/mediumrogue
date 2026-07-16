package server_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/starquake/mediumrogue/internal/server"
)

// TestRequireSameOriginPosts pins the #97 cross-origin guard: a POST carrying
// browser-supplied provenance headers (Origin, Sec-Fetch-Site) that point at
// another origin is rejected with 403, while header-less requests (curl, Go
// tests, some same-origin fetches) and genuinely same-origin ones pass
// through. GETs are never guarded — every mutating route is a POST, and the
// SSE stream must stay reachable.
func TestRequireSameOriginPosts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		method       string
		origin       string // "" = header absent
		secFetchSite string // "" = header absent
		wantStatus   int
	}{
		{name: "post without provenance headers passes", method: http.MethodPost, wantStatus: http.StatusOK},
		{name: "post with matching origin passes", method: http.MethodPost,
			origin: "http://game.example", wantStatus: http.StatusOK},
		{name: "origin host compare is case-insensitive", method: http.MethodPost,
			origin: "http://GAME.example", wantStatus: http.StatusOK},
		{name: "cross-origin post rejected", method: http.MethodPost,
			origin: "https://evil.example", wantStatus: http.StatusForbidden},
		{name: "same host different port rejected", method: http.MethodPost,
			origin: "http://game.example:9999", wantStatus: http.StatusForbidden},
		{name: "null origin rejected", method: http.MethodPost,
			origin: "null", wantStatus: http.StatusForbidden},
		{name: "malformed origin rejected", method: http.MethodPost,
			origin: "http://[::1", wantStatus: http.StatusForbidden},
		{name: "sec-fetch-site same-origin passes", method: http.MethodPost,
			secFetchSite: "same-origin", wantStatus: http.StatusOK},
		{name: "sec-fetch-site none passes", method: http.MethodPost,
			secFetchSite: "none", wantStatus: http.StatusOK},
		{name: "sec-fetch-site cross-site rejected", method: http.MethodPost,
			secFetchSite: "cross-site", wantStatus: http.StatusForbidden},
		{name: "sec-fetch-site same-site rejected", method: http.MethodPost,
			secFetchSite: "same-site", wantStatus: http.StatusForbidden},
		{name: "cross-site rejected even with matching origin", method: http.MethodPost,
			origin: "http://game.example", secFetchSite: "cross-site", wantStatus: http.StatusForbidden},
		{name: "get with cross origin passes", method: http.MethodGet,
			origin: "https://evil.example", wantStatus: http.StatusOK},
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	guarded := server.RequireSameOriginPostsForTest(slog.New(slog.DiscardHandler), next)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), tt.method, "http://game.example/api/intent", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}

			if tt.secFetchSite != "" {
				req.Header.Set("Sec-Fetch-Site", tt.secFetchSite)
			}

			rec := httptest.NewRecorder()
			guarded.ServeHTTP(rec, req)

			if got, want := rec.Code, tt.wantStatus; got != want {
				t.Errorf("status = %d, want %d", got, want)
			}
		})
	}
}
