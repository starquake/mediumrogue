package server

import (
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
func handleChat(deps Deps) http.Handler {
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
const systemSender = "system"

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

		id, perr := strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
		if perr != nil {
			respondError(w, deps.Logger, http.StatusUnprocessableEntity, "usage: /quest <id>")

			return "", "", false
		}

		out, err = deps.World.QuestTake(token, id)
	case "abandon":
		sender = systemSender

		// Item 14, playtest batch 2: a player can hold several personal
		// quests at once, so /abandon must name which one (mirrors /quest).
		id, perr := strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
		if perr != nil {
			respondError(w, deps.Logger, http.StatusUnprocessableEntity, "usage: /abandon <id>")

			return "", "", false
		}

		out, err = deps.World.QuestAbandon(token, id)
	default:
		sender = name
		out, err = chat.RunCommand(text, chat.Sender{Name: name, Hex: senderHex})
	}

	if err != nil {
		respondError(w, deps.Logger, http.StatusUnprocessableEntity, err.Error())

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
