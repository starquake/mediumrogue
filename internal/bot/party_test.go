package bot_test

import (
	"fmt"
	"testing"

	"github.com/starquake/mediumrogue/internal/bot"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestInviteAddressesMe pins the REAL announcement's shape. The bot has to
// string-match it, because an invite is broadcast only as chat and no bundle
// field carries it — so if party.go's wording changes, this fails loudly here
// rather than silently in a playtest when no bot joins.
//
// The format string mirrors game/party.go's PartyInvite exactly.
func TestInviteAddressesMe(t *testing.T) {
	t.Parallel()

	announce := func(inviter, target string) protocol.ChatMessage {
		return protocol.ChatMessage{
			Sender: protocol.SystemSender,
			Text:   fmt.Sprintf("%s invited %s to a party — %s: /accept", inviter, target, target),
		}
	}

	tests := []struct {
		name string
		msg  protocol.ChatMessage
		want bool
	}{
		{name: "addressed to me", msg: announce("starquake", "botty"), want: true},
		{name: "addressed to someone else", msg: announce("starquake", "otherbot")},
		{
			name: "a player merely typing the sentence",
			msg:  protocol.ChatMessage{Sender: "trickster", Text: "starquake invited botty to a party — botty: /accept"},
		},
		{name: "ordinary system line", msg: protocol.ChatMessage{Sender: protocol.SystemSender, Text: "botty was slain"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got, want := bot.InviteAddressesMe(tt.msg, "botty"), tt.want; got != want {
				t.Errorf("InviteAddressesMe(%q) = %v, want %v", tt.msg.Text, got, want)
			}
		})
	}
}

// A bot with no name cannot be addressed, and must not accept every invite it
// sees — which a naive "contains /accept" check would do.
func TestInviteNeedsAName(t *testing.T) {
	t.Parallel()

	msg := protocol.ChatMessage{
		Sender: protocol.SystemSender,
		Text:   "starquake invited botty to a party — botty: /accept",
	}

	if bot.InviteAddressesMe(msg, "") {
		t.Error("a nameless bot accepted an invite addressed to someone else")
	}
}
