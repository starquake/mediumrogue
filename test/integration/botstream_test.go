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
