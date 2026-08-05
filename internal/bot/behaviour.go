// Package bot decides what a headless player does with a turn (#370).
//
// Kept separate from botclient, which is pure plumbing: one decision function,
// no I/O, so it is testable without a server and so #371 can run several bots
// over one brain.
//
// This is the DUMB tier, deliberately (decided 2026-08-05). Balance-grade
// behaviour — skills, focus fire, kiting, retreat — is a later ticket, because
// AI-driven balance measures the AI, and blocking solo playtesting on that
// problem is the wrong trade.
package bot

import (
	"slices"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// QuaffBelowPercent is the health fraction at which the bot drinks. The draught
// is free and cooldown-gated (#322), so there is no resource to husband — the
// only cost of drinking early is the cooldown, and the only cost of drinking
// late is dying.
const QuaffBelowPercent = 50

// percentBase turns a fraction into a percentage. Named because revive's
// add-constant flags a bare 100, and it is genuinely a unit conversion.
const percentBase = 100

// FollowDistance is how close the bot trails its leader. Two hexes rather than
// adjacent: a bot standing on top of you is in the way, and a bot that must
// close every time you step is always a turn behind.
const FollowDistance = 2

// Config is one bot's standing orders.
type Config struct {
	// FollowName is the player to trail out of combat. Empty means hold
	// position — useful for a bot placed as a punchbag.
	FollowName string
}

// Decide returns the intent this bot submits for the turn, and whether it has
// one at all.
//
// It is a pure function of the bundle: same bundle, same answer. That makes it
// testable without a world, and it is why the loop in cmd/bot is trivial.
//
// IN A BUBBLE IT ALWAYS RETURNS AN INTENT. A bot that fails to act does not
// merely idle — it burns the bubble's COMBAT_PATIENCE for every other member,
// so a silent bot is worse for the party than no bot at all. When there is
// nothing better to do it waits, which is a move onto its own hex.
func Decide(cfg Config, me protocol.Entity, bundle protocol.TurnEvent) (protocol.IntentRequest, bool) {
	// HealthPotionReadyIn is checked, not just the HP fraction. Without it a
	// hurt bot re-sends a quaff every turn, eats a 422 for every one on
	// cooldown, and — because this branch wins the priority order — stops
	// fighting entirely while it does. Found in a live run: 20 quaff intents
	// against 13 attacks, 14 of them refused.
	if me.MaxHP > 0 && me.HP*percentBase/me.MaxHP < QuaffBelowPercent && me.HealthPotionReadyIn == 0 {
		return protocol.IntentRequest{Kind: protocol.IntentQuaffHealth}, true
	}

	if target, ok := nearestHostile(me, bundle); ok {
		// Adjacent: swing. Otherwise close the gap — the server rejects an
		// out-of-reach attack, and eating a 422 every turn is not a plan.
		if distance(me.Hex, target.Hex) == 1 {
			return protocol.IntentRequest{
				Kind: protocol.IntentAttack, Target: target.Hex, TargetEntityID: target.ID,
			}, true
		}

		return protocol.IntentRequest{Kind: protocol.IntentMove, Target: target.Hex}, true
	}

	if leader, ok := findLeader(cfg.FollowName, me, bundle); ok &&
		distance(me.Hex, leader.Hex) > FollowDistance {
		return protocol.IntentRequest{Kind: protocol.IntentMove, Target: leader.Hex}, true
	}

	// Nothing to do. Inside a bubble that still has to be SAID, or the turn
	// stalls on this bot's patience.
	if inBubble(me, bundle) {
		return protocol.IntentRequest{Kind: protocol.IntentMove, Target: me.Hex}, true
	}

	return protocol.IntentRequest{}, false
}

// nearestHostile finds the closest monster. Only monsters: a dumb bot never
// attacks a player, which would make it a hazard to the person testing with it.
func nearestHostile(me protocol.Entity, bundle protocol.TurnEvent) (protocol.Entity, bool) {
	var (
		best  protocol.Entity
		found bool
	)

	for _, e := range bundle.Entities {
		if e.Kind != protocol.EntityMonster || e.HP <= 0 {
			continue
		}

		if !found || distance(me.Hex, e.Hex) < distance(me.Hex, best.Hex) {
			best, found = e, true
		}
	}

	return best, found
}

func findLeader(name string, me protocol.Entity, bundle protocol.TurnEvent) (protocol.Entity, bool) {
	if name == "" {
		return protocol.Entity{}, false
	}

	for _, e := range bundle.Entities {
		if e.ID != me.ID && e.Kind == protocol.EntityPlayer && e.Name == name {
			return e, true
		}
	}

	return protocol.Entity{}, false
}

func inBubble(me protocol.Entity, bundle protocol.TurnEvent) bool {
	for _, b := range bundle.Bubbles {
		if slices.Contains(b.MemberIDs, me.ID) {
			return true
		}
	}

	return me.InCombat
}

// distance is hex cube distance over axial coordinates — the same formula
// game.HexDistance uses. Reimplemented rather than imported so a bot does not
// pull in the whole simulation to subtract two numbers.
func distance(a, b protocol.Hex) int {
	dq, dr := a.Q-b.Q, a.R-b.R
	ds := -dq - dr

	return (abs(dq) + abs(dr) + abs(ds)) / 2
}

func abs(n int) int {
	if n < 0 {
		return -n
	}

	return n
}
