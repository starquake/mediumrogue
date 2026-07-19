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

// intentErrorStatus maps a SubmitIntent error to its HTTP status, reporting
// false for an error it does not recognize (which handleIntent surfaces as a
// 500). Split out of the handler so a test can drive every domain sentinel
// through it directly — Deps.World is a concrete *game.World, so there is no
// stub that could make SubmitIntent return an arbitrary error.
//
// The shape of the mapping: 401 for a bad token; **422 for every "your intent
// no longer applies" rejection** — routine, client-caused conditions, not
// server faults. The world moves between a player's click and their POST, so
// a victim that died or stopped being hostile in that gap is as ordinary as a
// blocked path (#133: those two attack sentinels were missed when
// entity-targeted attacks landed and fell through to the 500 default, turning
// a normal race into an internal error — and, incidentally, flaking the
// suite).
func intentErrorStatus(err error) (int, bool) {
	switch {
	case err == nil:
		return http.StatusAccepted, true
	case errors.Is(err, game.ErrUnauthorized):
		return http.StatusUnauthorized, true
	case errors.Is(err, game.ErrNotWalkable), errors.Is(err, game.ErrNoPath),
		errors.Is(err, game.ErrNoRangedWeapon), errors.Is(err, game.ErrOutOfRange),
		errors.Is(err, game.ErrAttackTargetNotFound), errors.Is(err, game.ErrAttackTargetNotHostile),
		errors.Is(err, game.ErrInvalidIntentKind), errors.Is(err, game.ErrItemNotOwned),
		errors.Is(err, game.ErrBackpackFull),
		errors.Is(err, game.ErrItemNotEquipped), errors.Is(err, game.ErrNotDrinkable),
		errors.Is(err, game.ErrNotEquippable), errors.Is(err, game.ErrNoSuchGroundItem),
		errors.Is(err, game.ErrNoSuchSkill), errors.Is(err, game.ErrSkillAlreadyLearned),
		errors.Is(err, game.ErrSkillPrereqUnmet), errors.Is(err, game.ErrNoSkillPoints),
		errors.Is(err, game.ErrLearnInCombat),
		// Use-skill rejections (#161): a learned-but-cooling active, a
		// passive named as an active, an unlearned skill, a destination
		// behind a wall — all well-formed requests the world says no to.
		errors.Is(err, game.ErrSkillNotActive), errors.Is(err, game.ErrSkillNotLearned),
		errors.Is(err, game.ErrSkillOnCooldown), errors.Is(err, game.ErrNoLineOfSight):
		return http.StatusUnprocessableEntity, true
	default:
		return http.StatusInternalServerError, false
	}
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

		status, known := intentErrorStatus(err)
		switch {
		case err == nil:
			w.WriteHeader(http.StatusAccepted)
		case known:
			respondError(w, deps.Logger, status, err.Error())
		default:
			// An unmapped error is a genuine server fault — a domain sentinel
			// landing here instead means someone added one without mapping it
			// (#133), which TestEveryIntentSentinelIsMapped now catches.
			deps.Logger.Error("submit intent", "err", err)
			respondError(w, deps.Logger, http.StatusInternalServerError, "internal error")
		}
	})
}
