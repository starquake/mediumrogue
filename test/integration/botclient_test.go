package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/botclient"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// botclient_test.go (#369): the headless client over REAL HTTP.
//
// Tested here rather than with a stub server because the whole point of the
// decision to run bots over the wire is that they exercise the same path a
// browser does — same-origin POST enforcement, SSE framing, the join contract.
// A mock would prove none of it.
//
//nolint:paralleltest // serial by design: drives a live world clock.
func TestBotClientJoinsAndReadsTurns(t *testing.T) {
	ts := startServer(t, 50*time.Millisecond, time.Hour)

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	client, err := botclient.Join(ctx, ts.URL, protocol.JoinRequest{
		Name: "botty", Class: protocol.ClassFighter, Species: protocol.SpeciesHuman,
	})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	me := client.Identity()
	if me.EntityID == 0 {
		t.Error("joined with entity id 0")
	}

	if me.Token == "" {
		t.Error("joined with an empty token — nothing to reclaim with")
	}

	turns, err := client.Turns(ctx)
	if err != nil {
		t.Fatalf("Turns: %v", err)
	}

	// One bundle proves join + stream + framing. Heartbeats must NOT arrive
	// here: they are consumed inside the client, and a bot has nothing to do
	// with one.
	select {
	case bundle, ok := <-turns:
		if !ok {
			t.Fatal("turn stream closed before delivering a bundle")
		}

		if bundle.Turn < 0 {
			t.Errorf("bundle turn = %d, want >= 0", bundle.Turn)
		}
	case <-ctx.Done():
		t.Fatal("no turn bundle before the deadline")
	}
}

// TestBotClientReclaimsItsCharacter pins the property that makes a bot
// restartable: the token round-trips, exactly as a browser's stored identity
// does. Without it every restart leaves an abandoned entity waiting out the
// disconnect grace.
//
//nolint:paralleltest // serial by design: drives a live world clock.
func TestBotClientReclaimsItsCharacter(t *testing.T) {
	ts := startServer(t, 50*time.Millisecond, time.Hour)

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()

	first, err := botclient.Join(ctx, ts.URL, protocol.JoinRequest{
		Name: "botty", Class: protocol.ClassRogue, Species: protocol.SpeciesElf,
	})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	again, err := botclient.Join(ctx, ts.URL, protocol.JoinRequest{Token: first.Identity().Token})
	if err != nil {
		t.Fatalf("reclaim Join: %v", err)
	}

	if got, want := again.Identity().EntityID, first.Identity().EntityID; got != want {
		t.Errorf("reclaimed entity id = %d, want %d — a new character was created", got, want)
	}
}
