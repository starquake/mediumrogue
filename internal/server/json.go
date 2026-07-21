package server

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// maxJSONBodySize caps request bodies on the JSON API. Intents and joins are
// tiny; anything near this limit is abuse, not gameplay. (Topbanana's bound,
// same reasoning.)
const maxJSONBodySize = 64 * 1024

// bodyReadTimeout bounds how long a JSON POST body may take to arrive (#199) —
// a trickle defence. Applied per-request in decodeJSON, never server-wide, so
// SSE streams keep their long life.
const bodyReadTimeout = 10 * time.Second

// decodeJSON reads a single bounded JSON value into dst. On failure it
// writes the 4xx response itself and returns false.
func decodeJSON(w http.ResponseWriter, r *http.Request, logger *slog.Logger, dst any) bool {
	// Bound body-read TIME as well as size (#199): MaxBytesReader caps bytes,
	// not duration, so a trickled body could pin a goroutine indefinitely. A
	// per-request deadline set here (not a server-wide ReadTimeout) leaves the
	// long-lived SSE GET untouched — only these JSON POSTs get the wall.
	if err := http.NewResponseController(w).SetReadDeadline(time.Now().Add(bodyReadTimeout)); err != nil {
		// A connection that cannot take a deadline (rare/test transport) is
		// not a client error — proceed without the bound.
		logger.Debug("set read deadline", "err", err)
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodySize)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		// A body past the size cap is a size violation, not a syntax error:
		// report 413 so the client blames its payload size, not its encoding.
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			respondError(w, logger, http.StatusRequestEntityTooLarge, "request body too large")

			return false
		}

		respondError(w, logger, http.StatusBadRequest, "malformed JSON body")

		return false
	}

	// Reject a body with anything after the first JSON value (a second value or
	// trailing garbage): a well-formed prefix must not smuggle junk past the
	// decoder. A clean single value leaves exactly io.EOF here.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
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
