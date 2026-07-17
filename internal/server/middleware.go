package server

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

// securityHeaders sets the baseline response headers on every route. The CSP
// is strict because the client is fully self-contained: all script, style,
// and asset requests come from our own origin, and the only streaming
// connection is the same-origin SSE stream.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; "+
				"connect-src 'self'; frame-ancestors 'none'")

		next.ServeHTTP(w, r)
	})
}

// requireSameOriginPosts rejects cross-origin POSTs with 403 (#97). Every
// mutating route is a POST, so guarding the method guards the whole write
// surface; GETs (the SSE stream, the map, the embedded client) pass
// untouched.
//
// This is defense-in-depth, not the auth boundary: auth is a bearer token in
// the request body (no ambient credentials for a cross-site form to ride),
// so a request with NO provenance headers at all — curl, the Go integration
// tests, some same-origin fetches — stays allowed. Only a request that
// positively declares another origin (a browser-supplied Origin or
// Sec-Fetch-Site header) is turned away. The served origin is not configured
// anywhere, so "same origin" is derived from the request's own Host header.
func requireSameOriginPosts(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && !originAllowed(r) {
			logger.Warn("cross-origin POST rejected", "path", r.URL.Path,
				"origin", r.Header.Get("Origin"), "sec_fetch_site", r.Header.Get("Sec-Fetch-Site"))
			respondError(w, logger, http.StatusForbidden, "cross-origin request rejected")

			return
		}

		next.ServeHTTP(w, r)
	})
}

// originAllowed reports whether r may proceed: it carries no evidence of
// coming from another origin. Deliberately NOT "is same origin" — a
// header-less curl request is allowed here while proving nothing about its
// origin, so this is never a basis for an auth decision. Both browser
// provenance signals must clear:
//
//   - Sec-Fetch-Site: "same-origin" and "none" (direct/user-initiated) pass;
//     "cross-site" AND "same-site" are rejected — nothing legitimate POSTs
//     here from a sibling subdomain, so the stricter read costs nothing.
//     Absent (non-browser clients, older browsers) passes.
//   - Origin: absent passes (see requireSameOriginPosts); present, its host
//     must equal the request Host exactly (case-insensitive, port included —
//     browsers omit default ports in both headers consistently). "null" and
//     malformed values have no host and are rejected.
//
// Two accepted limitations of deriving the origin from Host (both fine for
// defense-in-depth over token-in-body auth, both need a *configured* origin
// to close): the scheme is not compared — the server sits behind a
// TLS-terminating proxy and cannot know its own public scheme — so this is
// strictly a same-HOST check; and a DNS-rebinding page is self-consistent
// (its Host, Origin, and Sec-Fetch-Site all agree) and passes.
func originAllowed(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
	default:
		return false
	}

	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}

	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}

	return strings.EqualFold(u.Host, r.Host)
}
