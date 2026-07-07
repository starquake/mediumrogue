package server

import "net/http"

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
