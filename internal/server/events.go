package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
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
// A named `heartbeat` event goes out every HeartbeatInterval on a fixed
// cadence (regardless of turns) so proxies don't reap the connection and the
// client's liveness watchdog stays fed even when a frozen combat clock stops
// turn frames.
func handleEvents(deps Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)

			return
		}

		// Identify the player behind this stream and mark presence. A token is
		// present once the client has joined; a not-yet-joined watcher (empty
		// token) is skipped — StreamOpened/Closed are no-ops for it anyway, so
		// this just avoids a needless lock. The defer sits right after the open
		// so StreamClosed fires on every return path (normal, context cancel, or
		// a write error), dropping the stream count and starting the grace.
		// The token also selects the VIEWER for own-only bundle fields
		// (skills and the point bank, #124 task 7): resolved once here, not
		// per turn. A token-less watcher gets the viewer-less bundle.
		token := r.URL.Query().Get("token")
		if token != "" {
			deps.World.StreamOpened(token)
			defer deps.World.StreamClosed(token)
		}

		// Subscribe BEFORE any byte (headers included) reaches the client, so
		// "the GET response arrived" guarantees this stream is registered with
		// both the tick hub and the chat broker. The tick hub coalesces (a tick
		// means "fetch latest", so a late subscription never loses a turn), but
		// chat is ephemeral fan-out: a message published between the header
		// flush and Chat.Subscribe would be silently dropped — the #220 flake,
		// where a client POSTed a chat line the instant its peer's stream
		// opened and the peer's subscription lost the race.
		ticks, unsubscribe := deps.Ticks.Subscribe()
		defer unsubscribe()

		chatCh, unsubscribeChat := deps.Chat.Subscribe()
		defer unsubscribeChat()

		h := w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		// Establish the stream on the wire immediately. Without this, Go's
		// server buffers the header block until the first body write, so a
		// reconnecting client whose Last-Event-ID already matches the current
		// turn (writeTurn below sends nothing) would see its connection hang
		// with no bytes at all until the next turn or heartbeat.
		flusher.Flush()

		// Seed the watermark from Last-Event-ID so a reconnecting client is not
		// re-sent a turn it already has. A fresh client (no header) or a
		// malformed value defaults to -1 → current snapshot sent immediately.
		lastSent := parseLastEventID(r)
		lastSent = writeTurn(w, deps, flusher, lastSent, token)

		heartbeat := time.NewTicker(deps.HeartbeatInterval)
		defer heartbeat.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticks:
				lastSent = writeTurn(w, deps, flusher, lastSent, token)
			case msg := <-chatCh:
				if !writeChat(w, deps, flusher, msg) {
					return
				}
			case <-heartbeat.C:
				// Named keep-alive: fires on a fixed cadence regardless of turns,
				// so the client's liveness watchdog stays fed even when a frozen
				// combat clock stops turn frames. No id: — a heartbeat is not a
				// turn and must not advance Last-Event-ID.
				if _, err := fmt.Fprintf(w, "event: %s\ndata: {}\n\n", protocol.EventHeartbeat); err != nil {
					return
				}

				flusher.Flush()
			}
		}
	})
}

// parseLastEventID reads the SSE reconnection header the browser's EventSource
// sends automatically: the turn number the client last saw, used as the initial
// "already sent" watermark. Missing or malformed values yield -1, which makes
// the handler behave like a fresh connection (send the current snapshot).
func parseLastEventID(r *http.Request) int64 {
	id, err := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64)
	if err != nil {
		return -1
	}

	return id
}

// writeTurn emits the latest turn bundle as an SSE frame, unless that turn
// was already sent (a coalesced wake-up with no new turn is a no-op).
// Returns the turn number now on the wire.
func writeTurn(w http.ResponseWriter, deps Deps, flusher http.Flusher, lastSent int64, viewerToken string) int64 {
	snapshot := deps.World.SnapshotFor(viewerToken)

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

// writeChat emits one chat message as an EventChat frame. No id: — chat must
// not advance Last-Event-ID. Returns false if the connection is gone.
func writeChat(w http.ResponseWriter, deps Deps, flusher http.Flusher, msg protocol.ChatMessage) bool {
	payload, err := json.Marshal(msg)
	if err != nil {
		deps.Logger.Error("marshal chat", "err", err)

		return true
	}

	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", protocol.EventChat, payload); err != nil {
		return false
	}

	flusher.Flush()

	return true
}
