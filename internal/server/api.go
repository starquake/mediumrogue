package server

import (
	"errors"
	"net/http"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// handleJoin mints or returns the caller's entity. Idempotent for a client
// that re-sends its stored token; a stale token (server restarted) quietly
// becomes a fresh entity.
func handleJoin(deps Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req protocol.JoinRequest
		if !decodeJSON(w, r, deps.Logger, &req) {
			return
		}

		resp, err := deps.World.Join(req.Token)
		if err != nil {
			deps.Logger.Error("join", "err", err)
			respondError(w, deps.Logger, http.StatusServiceUnavailable, "no room in the world")

			return
		}

		respondJSON(w, deps.Logger, resp)
	})
}

// handleIntent queues a step for the next turn. The response only
// acknowledges queueing — movement itself arrives via the turn bundle, which
// is the single source of truth for entity positions.
func handleIntent(deps Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req protocol.IntentRequest
		if !decodeJSON(w, r, deps.Logger, &req) {
			return
		}

		err := deps.World.SubmitIntent(req)
		switch {
		case errors.Is(err, game.ErrUnauthorized):
			respondError(w, deps.Logger, http.StatusUnauthorized, err.Error())
		case errors.Is(err, game.ErrNotAdjacent), errors.Is(err, game.ErrNotWalkable):
			respondError(w, deps.Logger, http.StatusUnprocessableEntity, err.Error())
		case err != nil:
			deps.Logger.Error("submit intent", "err", err)
			respondError(w, deps.Logger, http.StatusInternalServerError, "internal error")
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	})
}
