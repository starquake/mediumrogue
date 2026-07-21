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
	// ChatMinInterval is the per-player minimum gap between chat POSTs
	// (#199); over-rate lines are 429ed. Zero (the tests' default — every
	// existing harness builds Deps without it) disables the limit.
	ChatMinInterval time.Duration
	// JoinMinInterval is the refill rate of the global new-character join
	// bucket (burst protocol.MaxPlayers); over-rate joins are 429ed.
	// Reclaims/restores are exempt. Zero disables the limit.
	JoinMinInterval time.Duration
	// SSEMaxStreams caps concurrent SSE event streams globally (#199);
	// over-cap connects are 503ed with Retry-After. Zero disables the cap.
	SSEMaxStreams int
	// TrustProxyIP turns on the per-IP SSE stream cap by trusting the
	// X-Forwarded-For header (#199). False (the default, and every test
	// harness) means no per-IP cap and XFF is never read — RemoteAddr behind a
	// proxy is the shared proxy IP, so a per-IP cap on it would be one bucket
	// for everyone. Enable only where the app port is reachable exclusively via
	// the trusted proxy.
	TrustProxyIP bool
	// PerIPSSEStreams is the per-IP concurrent SSE stream cap enforced when
	// TrustProxyIP is on (#199); the 6th stream from one IP is rejected on top
	// of the still-applied global cap. Zero disables just the per-IP layer.
	PerIPSSEStreams int
}

// New returns the root HTTP handler.
func New(deps Deps) http.Handler {
	mux := http.NewServeMux()
	addRoutes(mux, deps)

	// The origin guard sits inside securityHeaders so its 403s carry the
	// baseline headers too.
	return securityHeaders(requireSameOriginPosts(deps.Logger, mux))
}
