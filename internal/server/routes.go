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

	// The static world map, fetched once at client startup. Terrain never
	// changes mid-game, so it stays off the SSE stream.
	mux.Handle("GET /api/map", handleMap(deps))

	// Identity + input: join claims (or reclaims) an entity; intent queues a
	// step for the next turn. Both are small bounded JSON POSTs.
	mux.Handle("POST /api/join", handleJoin(deps))
	mux.Handle("POST /api/intent", handleIntent(deps))

	// The embedded client bundle, served at the root. Registered last so the
	// more specific patterns above win.
	mux.Handle("/", web.Handler())
}

// handleMap serves the world map — immutable, so re-served from memory.
func handleMap(deps Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respondJSON(w, deps.Logger, deps.World.Map())
	})
}

func handleHealthz() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
}
