// Command bot joins a running world as a headless player and reports the turns
// it sees (#369).
//
// It exists so one person can playtest a multiplayer game: party features, a
// five-player boss and bubble merging cannot be exercised alone. This slice is
// the plumbing — it joins and watches. Behaviour (follow, fight, drink) is #370,
// and running a whole party is #371.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/starquake/mediumrogue/internal/botclient"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// errStreamEnded is the stream dropping while the bot still wanted it — as
// opposed to Ctrl-C, which is a clean exit.
var errStreamEnded = errors.New("bot: event stream ended")

func main() {
	var (
		url     = flag.String("url", "http://localhost:8080", "world to join")
		name    = flag.String("name", "bot", "display name")
		class   = flag.String("class", protocol.ClassFighter, "fighter, rogue or mage")
		species = flag.String("species", protocol.SpeciesHuman, "human, elf or dwarf")
		token   = flag.String("token", "", "reclaim an existing character instead of creating one")
	)
	flag.Parse()

	if err := run(url, name, class, species, token); err != nil {
		slog.Error("bot: exiting", "err", err)
		os.Exit(1)
	}
}

func run(url, name, class, species, token *string) error {
	// Ctrl-C ends the stream cleanly rather than leaving an entity waiting out
	// the disconnect grace.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client, err := botclient.Join(ctx, *url, protocol.JoinRequest{
		Token: *token, Name: *name, Class: *class, Species: *species,
	})
	if err != nil {
		return fmt.Errorf("bot: join: %w", err)
	}

	me := client.Identity()
	slog.Info("bot: joined", "name", *name, "entity", me.EntityID, "hex", me.Hex,
		// The token is what makes this bot reclaimable across a restart, so it
		// is printed to be reused — a bot's identity is not a secret worth
		// protecting the way a player's is.
		"token", me.Token)

	turns, err := client.Turns(ctx)
	if err != nil {
		return fmt.Errorf("bot: stream: %w", err)
	}

	for bundle := range turns {
		slog.Info("bot: turn", "turn", bundle.Turn, "entities", len(bundle.Entities))
	}

	// The channel closes on cancellation or a dropped stream; only the second
	// is an error worth reporting.
	if ctx.Err() != nil {
		return nil
	}

	return errStreamEnded
}
