package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// sseRetryAfterSeconds is the Retry-After hint on an over-cap 503: long
// enough to space a retry storm out, short enough that a freed slot is
// picked up promptly.
const sseRetryAfterSeconds = "5"

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
//
// A GLOBAL cap on concurrent streams (#199, Deps.SSEMaxStreams) bounds the
// per-tick cost every open stream pays (a SnapshotFor under the world lock
// plus a marshal): over-cap connects get an immediate 503 with Retry-After —
// an EventSource treats it as a failed connect and retries — instead of
// silently degrading turn resolution for everyone.
//
// An OPT-IN per-IP cap (#199, Deps.TrustProxyIP + Deps.PerIPSSEStreams) adds a
// fairness layer on top: with a trusted reverse proxy, one client IP can't hog
// every global slot. It is off by default because it hinges on trusting the
// X-Forwarded-For header, which is only safe where the app port is reachable
// exclusively via the proxy (see clientIP). When on, the per-IP rejection uses
// the SAME 503 + Retry-After as the global cap — an EventSource reacts to both
// identically (retry later), and one status keeps the SSE reject surface
// uniform; the semantics ("stream cap full, come back") are the same, just
// scoped per IP.
func handleEvents(deps Deps) http.Handler {
	gate := newStreamGate(deps.SSEMaxStreams)

	// Per-IP gate only when the proxy header is trusted; nil otherwise, so a
	// default deployment never reads X-Forwarded-For at all.
	var perIP *perKeyStreamGate
	if deps.TrustProxyIP {
		perIP = newPerKeyStreamGate(deps.PerIPSSEStreams)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !gate.acquire() {
			w.Header().Set("Retry-After", sseRetryAfterSeconds)
			respondError(w, deps.Logger, http.StatusServiceUnavailable, "too many open event streams")

			return
		}
		defer gate.release()

		// Per-IP cap sits after the global one, so the global slot released by
		// the defer above is returned if this rejects.
		if perIP != nil {
			ip := clientIP(r)
			if !perIP.acquire(ip) {
				w.Header().Set("Retry-After", sseRetryAfterSeconds)
				respondError(w, deps.Logger, http.StatusServiceUnavailable, "too many open event streams from your address")

				return
			}
			defer perIP.release(ip)
		}

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

		streamEvents(r.Context(), w, deps, flusher, token, lastSent, ticks, chatCh)
	})
}

// streamEvents is handleEvents' pump: it forwards turn ticks, chat messages,
// and heartbeats onto the established stream until ctx (the request context)
// ends or a write fails.
func streamEvents(
	ctx context.Context, w http.ResponseWriter, deps Deps, flusher http.Flusher,
	token string, lastSent int64, ticks <-chan struct{}, chatCh <-chan protocol.ChatMessage,
) {
	heartbeat := time.NewTicker(deps.HeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
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
}

// clientIP derives the caller's address for the per-IP SSE cap, and is only
// reached when TrustProxyIP is on (a proxy is assumed in front). It trusts the
// X-Forwarded-For header the sole proxy (SWAG) sets, taking the LAST entry:
// SWAG runs nginx's $proxy_add_x_forwarded_for, which APPENDS the peer that
// connected to it — the real client — to whatever XFF the client sent. So the
// rightmost entry is the one SWAG itself added and the only trustworthy one;
// every earlier entry is client-supplied and spoofable.
//
// If XFF is absent or empty (proxy misconfigured, or a direct hit that
// shouldn't happen when the flag is set correctly), fall back to RemoteAddr's
// host. Behind a proxy that resolves to the shared proxy IP — one bucket for
// everyone, a stricter shared cap, still safe rather than open; direct it is
// the real peer.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if last := strings.TrimSpace(parts[len(parts)-1]); last != "" {
			return last
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
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
	// Cheap gate before the expensive per-viewer bundle: a coalesced no-op wake
	// (or a reconnect already at the current watermark) skips SnapshotFor and
	// its marshal entirely (#209). The turn can only advance between this read
	// and SnapshotFor, so a bundle we do build is never staler than this check.
	if deps.World.Turn() == lastSent {
		return lastSent
	}

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
