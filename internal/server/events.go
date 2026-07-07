package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// handleEvents is the SSE world stream. Every connected client holds one of
// these open; the world clock publishes a tick per resolved turn and this
// handler re-reads the latest turn and emits it as an `event: turn` frame.
//
// The hub's coalescing semantics ("tick means fetch latest", never a delta)
// make a slow client harmless: it skips intermediate turns and paints the
// newest one. The SSE id is the turn number, so a reconnecting EventSource
// sends Last-Event-ID and (in a later milestone) the server can replay the
// missed turn bundles from its buffer.
//
// A comment frame goes out every HeartbeatInterval on an otherwise idle
// stream so proxies and load balancers don't reap the connection.
func handleEvents(deps Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)

			return
		}

		h := w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		ticks, unsubscribe := deps.Ticks.Subscribe()
		defer unsubscribe()

		// Send the current turn immediately so a fresh client paints without
		// waiting for the next tick.
		lastSent := int64(-1)
		lastSent = writeTurn(w, deps, flusher, lastSent)

		heartbeat := time.NewTicker(deps.HeartbeatInterval)
		defer heartbeat.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticks:
				lastSent = writeTurn(w, deps, flusher, lastSent)
				heartbeat.Reset(deps.HeartbeatInterval)
			case <-heartbeat.C:
				// Comment frame: keeps the connection warm, invisible to
				// EventSource listeners.
				if _, err := fmt.Fprint(w, ": hb\n\n"); err != nil {
					return
				}

				flusher.Flush()
			}
		}
	})
}

// writeTurn emits the latest turn bundle as an SSE frame, unless that turn
// was already sent (a coalesced wake-up with no new turn is a no-op).
// Returns the turn number now on the wire.
func writeTurn(w http.ResponseWriter, deps Deps, flusher http.Flusher, lastSent int64) int64 {
	snapshot := deps.World.Snapshot()

	turn := snapshot.Turn
	if turn == lastSent {
		return lastSent
	}

	payload, err := json.Marshal(snapshot)
	if err != nil {
		// TurnEvent is a fixed struct of marshalable fields; reaching this
		// means a programming error, not a runtime condition.
		deps.Logger.Error("marshal turn event", "err", err)

		return lastSent
	}

	if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", turn, protocol.EventTurn, payload); err != nil {
		return lastSent
	}

	flusher.Flush()

	return turn
}
