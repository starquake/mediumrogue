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

	// Within FollowDistance: halt rather than crowd them. Halting is an
	// explicit move onto our own hex, NOT the absence of an intent — the
	// earlier version of this test asserted "no intent" and read that as
	// "holds position", which is not what happens: the route to the leader's
	// hex survives and walks the bot onto them. Five bots following one leader
	// all ended up stacked on their leader's hex (live run, 2026-08-06).
	near := protocol.TurnEvent{Entities: []protocol.Entity{me, player(2, "leader", 2, 30)}}

	halt, ok := bot.Decide(cfg, me, near)
	if !ok {
		t.Fatal("issued no intent near the leader, so an existing route keeps walking")
	}

	if got, want := halt.Target, me.Hex; got != want {
		t.Errorf("halt target = %v, want own hex %v", got, want)
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

// TestDecideIgnoresMonstersFarFromTheLeader is #408: a -follow bot chased
// whatever it could see and never came back.
//
// The drift is one-way and self-reinforcing, which is what makes it worse than
// it sounds: chasing one monster brings the next into view, and once the bot
// passes the leader's interest radius the leader is not in its bundle at all,
// so findLeader cannot even see them. A live 5-bot party ended 34 hexes from
// the sanctuary, stacked together, permanently lost.
func TestDecideIgnoresMonstersFarFromTheLeader(t *testing.T) {
	t.Parallel()

	me := player(1, "botty", 0, testMaxHP)
	cfg := bot.Config{FollowName: "leader"}

	// The leader is far; a monster sits right next to the bot but nowhere near
	// the leader. Following wins — the party goes where the leader goes.
	leaderFar := player(2, "leader", 12, testMaxHP)
	bundle := protocol.TurnEvent{Entities: []protocol.Entity{me, leaderFar, monster(3, 1)}}

	got, ok := bot.Decide(cfg, me, bundle)
	if !ok {
		t.Fatal("issued no intent")
	}

	if got.Kind == protocol.IntentAttack {
		t.Errorf("attacked a monster %d hexes from the leader; want to follow instead",
			protocol.CombatRadius+6)
	}

	if want := leaderFar.Hex; got.Target != want {
		t.Errorf("target = %v, want the leader's hex %v", got.Target, want)
	}
}

// TestDecideFightsMonstersNearTheLeader is the other half: the leash must not
// turn the party into passengers. Anything within CombatRadius of the leader
// could form a bubble with them, so it is the party's fight.
func TestDecideFightsMonstersNearTheLeader(t *testing.T) {
	t.Parallel()

	me := player(1, "botty", 0, testMaxHP)
	cfg := bot.Config{FollowName: "leader"}

	// Leader at 2, monster adjacent to the bot and well inside CombatRadius of
	// the leader: swing.
	leader := player(2, "leader", 2, testMaxHP)
	bundle := protocol.TurnEvent{Entities: []protocol.Entity{me, leader, monster(3, 1)}}

	got, ok := bot.Decide(cfg, me, bundle)
	if !ok {
		t.Fatal("issued no intent")
	}

	if got, want := got.Kind, protocol.IntentAttack; got != want {
		t.Errorf("kind = %q, want %q — a monster beside the leader is the party's fight", got, want)
	}
}

// TestDecideWithoutALeaderStillHunts: the leash is a FOLLOW behaviour. A bot
// with no -follow (a roamer, or a punchbag) must be unchanged by #408.
func TestDecideWithoutALeaderStillHunts(t *testing.T) {
	t.Parallel()

	me := player(1, "botty", 0, testMaxHP)
	bundle := protocol.TurnEvent{Entities: []protocol.Entity{me, monster(3, 1)}}

	got, ok := bot.Decide(bot.Config{}, me, bundle)
	if !ok {
		t.Fatal("issued no intent")
	}

	if got, want := got.Kind, protocol.IntentAttack; got != want {
		t.Errorf("kind = %q, want %q — a leaderless bot still hunts", got, want)
	}
}
