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

	"github.com/starquake/mediumrogue/internal/bot"
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
		follow  = flag.String("follow", "", "player name to trail when out of combat")
	)
	flag.Parse()

	if err := run(url, name, class, species, token, follow); err != nil {
		slog.Error("bot: exiting", "err", err)
		os.Exit(1)
	}
}

func run(url, name, class, species, token, follow *string) error {
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

	cfg := bot.Config{FollowName: *follow}

	for bundle := range turns {
		// The bot only ever acts on ITSELF as the world reports it — never on
		// remembered state. A bundle is the whole truth for that turn.
		self, ok := entityByID(bundle, me.EntityID)
		if !ok {
			continue // dead or out of our own interest radius; nothing to decide with
		}

		intent, act := bot.Decide(cfg, self, bundle)
		if !act {
			continue
		}

		intent.EntityID, intent.Token = me.EntityID, me.Token
		if err := client.Submit(ctx, intent); err != nil {
			// A refusal is normal — the world saying no to a well-formed
			// intent (a hex that filled, a cooldown). Log and take the next
			// turn rather than dying on it.
			slog.Warn("bot: intent refused", "kind", intent.Kind, "err", err)

			continue
		}

		slog.Info("bot: acted", "turn", bundle.Turn, "kind", intent.Kind, "hp", self.HP)
	}

	// The channel closes on cancellation or a dropped stream; only the second
	// is an error worth reporting.
	if ctx.Err() != nil {
		return nil
	}

	return errStreamEnded
}

func entityByID(bundle protocol.TurnEvent, id int64) (protocol.Entity, bool) {
	for _, e := range bundle.Entities {
		if e.ID == id {
			return e, true
		}
	}

	return protocol.Entity{}, false
}
