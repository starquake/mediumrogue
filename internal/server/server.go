// Package server wires the HTTP surface: routes, middleware, and the SSE
// turn stream. The layering follows topbanana's internal/server: New builds
// the handler tree, routes.go registers per-surface route groups, and
// middleware.go holds the cross-cutting wrappers.
package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/starquake/mediumrogue/internal/chat"
	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
)

// Deps bundles what the HTTP layer needs from the rest of the app, so it
// travels as one argument and grows without widening every signature.
type Deps struct {
	Logger *slog.Logger
	World  *game.World
	Ticks  *hub.Hub
	// Chat fans chat messages to every connected SSE stream.
	Chat *chat.Broker
	// HeartbeatInterval is how often the SSE handlers emit a comment frame on
	// an otherwise idle stream. Threaded through Deps (not read from config
	// here) so tests can shrink it to milliseconds.
	HeartbeatInterval time.Duration
}

// New returns the root HTTP handler.
func New(deps Deps) http.Handler {
	mux := http.NewServeMux()
	addRoutes(mux, deps)

	return securityHeaders(mux)
}
