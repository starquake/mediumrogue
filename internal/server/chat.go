package server

import (
	"net/http"
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

		broadcast := text
		if strings.HasPrefix(text, "/") {
			out, err := chat.RunCommand(text, chat.Sender{Name: name, Hex: senderHex})
			if err != nil {
				respondError(w, deps.Logger, http.StatusUnprocessableEntity, err.Error())

				return
			}

			broadcast = out
		}

		deps.Chat.Publish(name, broadcast)
		w.WriteHeader(http.StatusAccepted)
	})
}
