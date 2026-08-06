package bot

import (
	"strings"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// InviteAddressesMe reports whether a chat line is a party invite aimed at this
// bot (#371).
//
// An invite is announced ONLY as chat — `party.go` broadcasts
// "<inviter> invited <target> to a party — <target>: /accept" and the turn
// bundle carries no pending-invite field. So matching the announcement is the
// only way a bot can know, short of a protocol change.
//
// That makes this string-matching, which is fragile by nature: reword the
// announcement and bots stop joining parties, silently. It is matched on the
// SYSTEM sender plus both the verb and the bot's own name, so an ordinary
// player cannot trigger it by typing the sentence in chat — and
// TestInviteAddressesMe pins the real announcement's shape so a reword fails
// loudly here instead of quietly in a playtest.
func InviteAddressesMe(msg protocol.ChatMessage, myName string) bool {
	if msg.Sender != protocol.SystemSender || myName == "" {
		return false
	}

	if !strings.Contains(msg.Text, "invited") || !strings.Contains(msg.Text, "/accept") {
		return false
	}

	// "invited <me> to a party" — the target, not the inviter. Matching the
	// name anywhere would make a bot accept invites sent to someone else.
	return strings.Contains(msg.Text, "invited "+myName+" ")
}
