package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// maxJSONBodySize caps request bodies on the JSON API. Intents and joins are
// tiny; anything near this limit is abuse, not gameplay. (Topbanana's bound,
// same reasoning.)
const maxJSONBodySize = 64 * 1024

// decodeJSON reads a single bounded JSON value into dst. On failure it
// writes the 4xx response itself and returns false.
func decodeJSON(w http.ResponseWriter, r *http.Request, logger *slog.Logger, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		respondError(w, logger, http.StatusBadRequest, "malformed JSON body")

		return false
	}

	return true
}

// respondJSON writes v as a JSON response. On marshal failure it logs and
// sends a bare 500 — by then no body has been written, so the status is
// still ours to set.
func respondJSON(w http.ResponseWriter, logger *slog.Logger, v any) {
	payload, err := json.Marshal(v)
	if err != nil {
		logger.Error("marshal response", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(payload)
}

// respondError writes the uniform JSON error shape with the given status.
func respondError(w http.ResponseWriter, logger *slog.Logger, status int, msg string) {
	payload, err := json.Marshal(protocol.ErrorResponse{Error: msg})
	if err != nil {
		logger.Error("marshal error response", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}
