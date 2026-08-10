package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/botclient"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestBotStreamCarriesOwnOnlyFields pins the bug found in #370's first live run.
//
// botclient opened /api/events with no token, so the server handed it the
// VIEWER-LESS bundle and every own-only field — cooldowns, energy, skills —
// arrived zeroed. A bot reading HealthPotionReadyIn == 0 quaffs on cooldown
// forever: the first run sent 20 quaffs against 13 attacks, 14 of them refused.
//
// Asserted on Skills rather than a cooldown because it is non-empty from the
// first bundle, where a cooldown is only interesting after something is spent.
//
//nolint:paralleltest // serial by design: drives a live world clock.
func TestBotStreamCarriesOwnOnlyFields(t *testing.T) {
	ts := startServer(t, 50*time.Millisecond, time.Hour)

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	client, err := botclient.Join(ctx, ts.URL, protocol.JoinRequest{
		Name: "botty", Class: protocol.ClassFighter, Species: protocol.SpeciesHuman,
	})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	turns, err := client.Turns(ctx)
	if err != nil {
		t.Fatalf("Turns: %v", err)
	}

	me := client.Identity()

	for {
		select {
		case bundle, ok := <-turns:
			if !ok {
				t.Fatal("stream closed before a bundle carried the viewer's own fields")
			}

			for _, e := range bundle.Entities {
				if e.ID != me.EntityID {
					continue
				}

				if len(e.Skills) == 0 {
					t.Fatal("own-only fields are empty — the stream is anonymous, " +
						"so cooldowns read as zero and the bot acts on them")
				}

				return
			}
		case <-ctx.Done():
			t.Fatal("no bundle containing the bot before the deadline")
		}
	}
}

// TestBotStreamCarriesPendingInvite pins what the bot's party behaviour now
// RESTS ON (#385). It used to detect an invite by matching the broadcast
// sentence, and internal/bot's InviteAddressesMe + its unit test pinned that
// sentence's shape so a reword failed loudly. Both are deleted with the string
// matching, so this is what replaces them: the field the bot reads instead.
//
// Without it the bot's invite path is untested at every layer — cmd/bot has no
// tests of its own, and a bundle that silently stopped carrying PendingInvite
// would show up only as bots never joining a party in a playtest.
//
//nolint:paralleltest // serial by design: drives a live world clock.
func TestBotStreamCarriesPendingInvite(t *testing.T) {
	ts := startServer(t, 50*time.Millisecond, time.Hour)

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	// The inviter is an ordinary HTTP player; the invitee is a bot client, so
	// this exercises the same stream cmd/bot reads.
	alice := joinNamed(t, ts, "alice")

	client, err := botclient.Join(ctx, ts.URL, protocol.JoinRequest{
		Name: "botty", Class: protocol.ClassFighter, Species: protocol.SpeciesHuman,
	})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	turns, err := client.Turns(ctx)
	if err != nil {
		t.Fatalf("Turns: %v", err)
	}

	if got, want := chatPost(t, ts, alice.Token, "/invite botty"), 202; got != want {
		t.Fatalf("/invite status = %d, want %d", got, want)
	}

	for {
		select {
		case bundle, ok := <-turns:
			if !ok {
				t.Fatal("stream closed before a bundle carried the pending invite")
			}

			if bundle.PendingInvite == nil {
				continue
			}

			if got, want := bundle.PendingInvite.InviterName, "alice"; got != want {
				t.Errorf("InviterName = %q, want %q", got, want)
			}

			return
		case <-ctx.Done():
			t.Fatal("the bot's own stream never carried the invite it was sent")
		}
	}
}
