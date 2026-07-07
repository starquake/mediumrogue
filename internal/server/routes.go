package server

import (
	"net/http"

	"github.com/starquake/medium-rogue/internal/web"
)

// addRoutes registers every route. Kept as one function while the surface is
// small; split into per-surface addXRoutes functions (the topbanana pattern)
// as the API grows.
func addRoutes(mux *http.ServeMux, deps Deps) {
	// Liveness probe: cheap, unauthenticated, side-effect free.
	mux.Handle("GET /healthz", handleHealthz())

	// The world event stream. Turn bundles, and later chat + world events,
	// flow to every connected client over this single SSE connection.
	mux.Handle("GET /api/events", handleEvents(deps))

	// The embedded client bundle, served at the root. Registered last so the
	// more specific patterns above win.
	mux.Handle("/", web.Handler())
}

func handleHealthz() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
}
