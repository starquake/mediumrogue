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

		resp, err := deps.World.Join(req.Token, req.Name, req.Class, req.Species)
		switch {
		case errors.Is(err, game.ErrInvalidClass), errors.Is(err, game.ErrInvalidSpecies),
			errors.Is(err, game.ErrInvalidName):
			respondError(w, deps.Logger, http.StatusUnprocessableEntity, err.Error())
		case err != nil:
			deps.Logger.Error("join", "err", err)
			respondError(w, deps.Logger, http.StatusServiceUnavailable, "no room in the world")
		default:
			respondJSON(w, deps.Logger, resp)
		}
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
		case errors.Is(err, game.ErrNotWalkable), errors.Is(err, game.ErrNoPath),
			errors.Is(err, game.ErrNoRangedWeapon), errors.Is(err, game.ErrOutOfRange),
			errors.Is(err, game.ErrInvalidIntentKind), errors.Is(err, game.ErrItemNotOwned),
			errors.Is(err, game.ErrBackpackFull),
			errors.Is(err, game.ErrItemNotEquipped), errors.Is(err, game.ErrNotDrinkable),
			errors.Is(err, game.ErrNotEquippable), errors.Is(err, game.ErrNoSuchGroundItem):
			respondError(w, deps.Logger, http.StatusUnprocessableEntity, err.Error())
		case err != nil:
			deps.Logger.Error("submit intent", "err", err)
			respondError(w, deps.Logger, http.StatusInternalServerError, "internal error")
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	})
}
