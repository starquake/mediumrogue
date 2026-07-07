package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

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
