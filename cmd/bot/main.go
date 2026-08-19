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
	"sync"
	"syscall"

	"github.com/starquake/mediumrogue/internal/bot"
	"github.com/starquake/mediumrogue/internal/botclient"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// errStreamEnded is the stream dropping while the bot still wanted it — as
// submitFallback handles a refused intent: the world saying no to something
// that was well-formed when it was chosen (#407).
//
// A refused ATTACK is the common case and it is not an error — the target died
// during the previous turn's resolution, and 422 is the correct answer (#130's
// fix; that path used to 500). What was wrong was the CONSEQUENCE: the bot
// submitted nothing at all that turn, so a rejected intent was indistinguishable
// from a silent bot as far as the bubble's patience clock was concerned. It now
// falls back to a wait, restoring Decide's always-act contract at the layer that
// actually talks to the server — Decide itself stays a pure function of the
// bundle, which is what makes it testable.
//
// Logged at DEBUG once handled, because a handled, expected race is not a
// warning; anything else refused stays a WARN.
func submitFallback(
	ctx context.Context, client *botclient.Client,
	me protocol.JoinResponse, self protocol.Entity,
	refused protocol.IntentRequest, err error,
) {
	wait, ok := bot.FallbackAfterRefusal(refused, self)
	if !ok {
		slog.Warn("bot: intent refused", "kind", refused.Kind, "err", err)

		return
	}

	slog.Debug("bot: attack refused, waiting instead", "kind", refused.Kind, "err", err)

	wait.EntityID, wait.Token = me.EntityID, me.Token
	if waitErr := client.Submit(ctx, wait); waitErr != nil {
		// The wait itself failing IS a surprise: a move onto your own hex is
		// always legal. Worth seeing.
		slog.Warn("bot: wait after refused attack also refused", "err", waitErr)
	}
}

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
		count   = flag.Int("count", 1, "how many bots to run in this process")
	)
	flag.Parse()

	// Ctrl-C ends every stream cleanly rather than leaving entities waiting out
	// the disconnect grace — which matters far more with five of them.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	err := run(ctx, url, name, class, species, token, follow, *count)

	// Released before Exit, which would skip a defer.
	stop()

	if err != nil {
		slog.Error("bot: exiting", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, url, name, class, species, token, follow *string, count int) error {
	if count > 1 {
		return runParty(ctx, url, name, species, follow, count)
	}

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

	// Events rather than Turns: chat is still read for everything else a bot
	// might react to. The party INVITE no longer needs it — since #385 the
	// bundle carries PendingInvite, so the bot answers a field instead of
	// pattern-matching an English sentence.
	events, err := client.Events(ctx)
	if err != nil {
		return fmt.Errorf("bot: stream: %w", err)
	}

	cfg := bot.Config{FollowName: *follow}

	for ev := range events {
		if ev.Chat != nil {
			continue
		}

		bundle := *ev.Turn

		acceptIfInvited(ctx, client, bundle, *name)

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
			submitFallback(ctx, client, me, self, intent, err)

			continue
		}

		slog.Info("bot: acted", "turn", bundle.Turn, "kind", intent.Kind, "hp", self.HP)
	}

	// Since #430 the channel closes on CANCELLATION ONLY — botclient reconnects
	// across drops itself. errStreamEnded is kept as an unreachable backstop:
	// a bot that stops for an unnameable reason is the failure #430 was about.
	if errors.Is(ctx.Err(), context.Canceled) {
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

// runParty runs several bots in one process, one goroutine each, so a single
// person can field a party (#371). Party features, bubble merging under real
// load and a five-player boss cannot be exercised alone otherwise.
//
// Each bot is a SEPARATE identity over its own connection — not one client
// pretending to be five — because the thing being tested is how the server
// handles several players, and a shortcut here would test the shortcut.
func runParty(ctx context.Context, url, name, species, follow *string, count int) error {
	// The roster a multi-bot run draws from, in order. Five identical fighters
	// test one shape of fight; a mixed party is what a real group looks like,
	// and it is what makes a boss's telegraph interesting.
	partyClasses := []string{
		protocol.ClassFighter, protocol.ClassRogue, protocol.ClassMage,
		protocol.ClassFighter, protocol.ClassRogue,
	}

	var wg sync.WaitGroup

	for i := range count {
		botName := fmt.Sprintf("%s%d", *name, i+1)
		botClass := partyClasses[i%len(partyClasses)]

		wg.Go(func() {
			// A bot that dies on a transport blip should not take the party
			// with it: log and let the others carry on.
			if err := run(ctx, url, &botName, &botClass, species, new(string), follow, 1); err != nil {
				slog.Error("bot: member stopped", "name", botName, "err", err)
			}
		})
	}

	wg.Wait()

	return nil
}

// acceptIfInvited joins a party when the bundle says this bot has been asked.
// Split out of run to keep it under the complexity limit, and because "did
// someone invite me" is a decision, not plumbing.
//
// Reads PendingInvite (#385). It used to match the broadcast sentence
// "<inviter> invited <target> to a party — <target>: /accept", which was the
// only way to know before the field existed and which would have broken
// silently on any reword. The field is own-only, so its mere presence means
// this bot is the one being asked — there is no name to compare.
//
// Answering every turn while an invite stands is harmless: the second /accept
// gets a 422 because the first already cleared it.
func acceptIfInvited(ctx context.Context, client *botclient.Client, bundle protocol.TurnEvent, name string) {
	if bundle.PendingInvite == nil {
		return
	}

	if err := client.Say(ctx, "/accept"); err != nil {
		slog.Warn("bot: could not accept invite", "name", name, "err", err)

		return
	}

	slog.Info("bot: joined a party", "name", name)
}
