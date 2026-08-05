package bot_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/bot"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// testMaxHP is a fighter's starting pool — every case here scales its HP
// against the same maximum, so the quaff threshold reads directly.
const testMaxHP = 30

func player(id int64, name string, q, hp int) protocol.Entity {
	return protocol.Entity{
		ID: id, Name: name, Kind: protocol.EntityPlayer,
		Hex: protocol.Hex{Q: q}, HP: hp, MaxHP: testMaxHP,
	}
}

func monster(id int64, q int) protocol.Entity {
	return protocol.Entity{
		ID: id, Kind: protocol.EntityMonster,
		Hex: protocol.Hex{Q: q}, HP: 10, MaxHP: 10,
	}
}

// TestDecideAlwaysActsInABubble is the one that matters for #371. A bot that
// stays silent inside a bubble burns COMBAT_PATIENCE for every other member,
// so it is worse than no bot: the party waits out the timeout every turn.
func TestDecideAlwaysActsInABubble(t *testing.T) {
	t.Parallel()

	me := player(1, "botty", 0, 30)
	bundle := protocol.TurnEvent{
		Entities: []protocol.Entity{me},
		Bubbles:  []protocol.BubbleView{{ID: 7, MemberIDs: []int64{1}}},
	}

	got, ok := bot.Decide(bot.Config{}, me, bundle)
	if !ok {
		t.Fatal("no intent inside a bubble — the party would wait out the patience timeout")
	}

	// Nothing to fight, nobody to follow: waiting is a move onto your own hex.
	if got, want := got.Kind, protocol.IntentMove; got != want {
		t.Errorf("intent kind = %q, want %q", got, want)
	}

	if got, want := got.Target, me.Hex; got != want {
		t.Errorf("wait target = %v, want own hex %v", got, want)
	}
}

// TestDecideDoesNotQuaffOnCooldown pins the fix for a live-run bug: a hurt bot
// re-sent a quaff every turn, ate a 422 for each one on cooldown, and stopped
// fighting because the quaff branch wins the priority order. The world reports
// readiness; the bot has to read it.
func TestDecideDoesNotQuaffOnCooldown(t *testing.T) {
	t.Parallel()

	me := player(1, "botty", 0, 12)
	me.HealthPotionReadyIn = 3
	bundle := protocol.TurnEvent{Entities: []protocol.Entity{me, monster(2, 1)}}

	got, ok := bot.Decide(bot.Config{}, me, bundle)
	if !ok {
		t.Fatal("no intent at all")
	}

	if got.Kind == protocol.IntentQuaffHealth {
		t.Error("quaffed while the draught was on cooldown — a guaranteed 422")
	}

	if got, want := got.Kind, protocol.IntentAttack; got != want {
		t.Errorf("intent kind = %q, want %q — it should fight while it cannot drink", got, want)
	}
}

func TestDecideDrinksWhenHurt(t *testing.T) {
	t.Parallel()

	// 12/30 is below the 50% threshold, and a monster is adjacent — health
	// still wins, because a corpse deals no damage.
	me := player(1, "botty", 0, 12)
	bundle := protocol.TurnEvent{Entities: []protocol.Entity{me, monster(2, 1)}}

	got, ok := bot.Decide(bot.Config{}, me, bundle)
	if !ok {
		t.Fatal("no intent while hurt")
	}

	if got, want := got.Kind, protocol.IntentQuaffHealth; got != want {
		t.Errorf("intent kind = %q, want %q", got, want)
	}
}

func TestDecideAttacksAdjacentAndClosesOtherwise(t *testing.T) {
	t.Parallel()

	me := player(1, "botty", 0, 30)

	adjacent := protocol.TurnEvent{Entities: []protocol.Entity{me, monster(2, 1)}}
	if got, _ := bot.Decide(bot.Config{}, me, adjacent); got.Kind != protocol.IntentAttack {
		t.Errorf("adjacent monster: kind = %q, want %q", got.Kind, protocol.IntentAttack)
	} else if got.TargetEntityID != 2 {
		t.Errorf("attack target id = %d, want 2", got.TargetEntityID)
	}

	// Out of reach: close the gap rather than eat a 422 every turn.
	far := protocol.TurnEvent{Entities: []protocol.Entity{me, monster(2, 4)}}
	if got, _ := bot.Decide(bot.Config{}, me, far); got.Kind != protocol.IntentMove {
		t.Errorf("distant monster: kind = %q, want %q", got.Kind, protocol.IntentMove)
	}
}

// TestDecidePrefersTheNearestMonster pins that "nearest" is by hex distance,
// not bundle order — entities arrive sorted by id, which is not proximity.
func TestDecidePrefersTheNearestMonster(t *testing.T) {
	t.Parallel()

	me := player(1, "botty", 0, 30)
	bundle := protocol.TurnEvent{Entities: []protocol.Entity{
		me, monster(2, 5), monster(3, 1),
	}}

	got, _ := bot.Decide(bot.Config{}, me, bundle)
	if got, want := got.TargetEntityID, int64(3); got != want {
		t.Errorf("attacked entity %d, want %d (the nearer one, listed second)", got, want)
	}
}

func TestDecideFollowsOnlyWhenTooFar(t *testing.T) {
	t.Parallel()

	me := player(1, "botty", 0, 30)
	cfg := bot.Config{FollowName: "leader"}

	// Within FollowDistance: hold position rather than crowd them.
	near := protocol.TurnEvent{Entities: []protocol.Entity{me, player(2, "leader", 2, 30)}}
	if _, ok := bot.Decide(cfg, me, near); ok {
		t.Error("followed a leader already within FollowDistance")
	}

	far := protocol.TurnEvent{Entities: []protocol.Entity{me, player(2, "leader", 6, 30)}}

	got, ok := bot.Decide(cfg, me, far)
	if !ok {
		t.Fatal("did not follow a distant leader")
	}

	if got, want := got.Target, (protocol.Hex{Q: 6, R: 0}); got != want {
		t.Errorf("follow target = %v, want %v", got, want)
	}
}

// TestDecideNeverAttacksPlayers: a dumb bot that swings at people is a hazard
// to the person testing with it.
func TestDecideNeverAttacksPlayers(t *testing.T) {
	t.Parallel()

	me := player(1, "botty", 0, 30)
	bundle := protocol.TurnEvent{Entities: []protocol.Entity{me, player(2, "victim", 1, 30)}}

	if got, ok := bot.Decide(bot.Config{}, me, bundle); ok && got.Kind == protocol.IntentAttack {
		t.Error("attacked another player")
	}
}
