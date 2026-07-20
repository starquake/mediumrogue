package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/starquake/mediumrogue/internal/chat"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// handleChat validates a chat POST, runs any "/command", and publishes the
// resulting line to the broker. The sender's name and position come from the
// token (server-authoritative — the client cannot set them), so /here can't
// be spoofed.
//
// A per-token rate limit (#199, one line per Deps.ChatMinInterval) sits right
// after authentication: every authenticated chat POST counts — plain lines
// and "/commands" alike, since both cost handling and most commands broadcast
// — while unauthenticated spam never touches the limiter's memory. Over-rate
// lines get 429 + Retry-After; the client surfaces the error body as a local
// system line.
func handleChat(deps Deps) http.Handler {
	limiter := newPerKeyLimiter(deps.ChatMinInterval)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req protocol.ChatRequest
		if !decodeJSON(w, r, deps.Logger, &req) {
			return
		}

		name, senderHex, ok := deps.World.SenderFor(req.Token)
		if !ok {
			respondError(w, deps.Logger, http.StatusUnauthorized, "unknown or not-joined token")

			return
		}

		if !limiter.allow(req.Token) {
			w.Header().Set("Retry-After", retryAfterSeconds(deps.ChatMinInterval))
			respondError(w, deps.Logger, http.StatusTooManyRequests, "you're sending messages too fast")

			return
		}

		text := strings.TrimSpace(req.Text)
		if text == "" {
			respondError(w, deps.Logger, http.StatusUnprocessableEntity, "empty message")

			return
		}

		if utf8.RuneCountInString(text) > protocol.MaxChatLen {
			respondError(w, deps.Logger, http.StatusUnprocessableEntity, "message too long")

			return
		}

		sender, broadcast := name, text

		if strings.HasPrefix(text, "/") {
			sender, broadcast, ok = routeChatCommand(w, deps, req.Token, name, senderHex, text)
			if !ok {
				return
			}
		}

		deps.Chat.Publish(sender, broadcast)
		w.WriteHeader(http.StatusAccepted)
	})
}

// systemSender labels chat announcements the server generates itself (party
// invite/accept/leave), as opposed to a line a player typed.
const systemSender = protocol.SystemSender

// helpText is the one-line control + command summary the /help verb replies
// with (#203), and what an unknown command now points at instead of a bare
// error. Self-only (a 422 the client renders as a system line), so it never
// spams the shared channel. Keep in step with client/src/input/keys.ts.
const helpText = "controls — move QWE/ASD · wait Space · panels I·C inventory, K skills · " +
	"chat /help /here /quest <id> · party /invite <name> /accept /leave"

// questIDArg parses the numeric id argument shared by /quest and /abandon,
// responding with a usage error and reporting false if it is missing or
// malformed (#203 extraction — previously copy-pasted per verb).
func questIDArg(w http.ResponseWriter, deps Deps, rest, verb string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
	if err != nil {
		respondError(w, deps.Logger, http.StatusUnprocessableEntity, "usage: "+verb+" <id>")

		return 0, false
	}

	return id, true
}

// routeChatCommand runs a "/command" and returns the sender label to publish
// under (systemSender for party ops, the player's own name otherwise) and the
// text to broadcast. Party verbs (invite/accept/leave) go straight to World;
// every other command runs through chat.RunCommand under the player's own
// name. On failure it writes the error response itself and returns ok=false.
func routeChatCommand(
	w http.ResponseWriter, deps Deps, token string, name string, senderHex protocol.Hex, text string,
) (string, string, bool) {
	verb, rest := cutVerb(text)

	var (
		sender string
		out    string
		err    error
	)

	switch verb {
	case "invite":
		sender = systemSender
		out, err = deps.World.PartyInvite(token, rest)
	case "accept":
		sender = systemSender
		out, err = deps.World.PartyAccept(token)
	case "leave":
		sender = systemSender
		out, err = deps.World.PartyLeave(token)
	case "quest":
		sender = systemSender

		id, ok := questIDArg(w, deps, rest, "/quest")
		if !ok {
			return "", "", false
		}

		out, err = deps.World.QuestTake(token, id)
	case "help":
		// Self-only reply: a 422 the client shows as a system line in the
		// typer's own log (chat/store.ts), never broadcast.
		respondError(w, deps.Logger, http.StatusUnprocessableEntity, helpText)

		return "", "", false
	case "abandon":
		// A player can hold several quests, so /abandon names which (like /quest).
		sender = systemSender

		id, ok := questIDArg(w, deps, rest, "/abandon")
		if !ok {
			return "", "", false
		}

		out, err = deps.World.QuestAbandon(token, id)
	default:
		sender = name
		out, err = chat.RunCommand(text, chat.Sender{Name: name, Hex: senderHex})
	}

	if err != nil {
		msg := err.Error()
		if errors.Is(err, chat.ErrUnknownCommand) {
			msg += " — try /help" // #203: point at the help, keep the echoed verb
		}

		respondError(w, deps.Logger, http.StatusUnprocessableEntity, msg)

		return "", "", false
	}

	return sender, out, true
}

// cutVerb splits a "/verb rest…" chat command into a lower-cased verb and the
// trimmed remainder (the remainder may contain spaces, e.g. a multi-word name).
func cutVerb(text string) (string, string) {
	body := strings.TrimSpace(strings.TrimPrefix(text, "/"))
	if i := strings.IndexAny(body, " \t"); i >= 0 {
		return strings.ToLower(body[:i]), strings.TrimSpace(body[i+1:])
	}

	return strings.ToLower(body), ""
}
