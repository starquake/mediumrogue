package game_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestMonsterKillAnnouncedInChat: a bubble turn that kills monsters announces
// a kill summary through the chat hook (the de facto combat log), naming the
// slain kind (oneHitKillBubble's default spawn is wolf). Uses the miss seed
// so no pickup announce muddies the capture; the summary quotes the
// per-player base XP (species bonuses are per-player, so the base is the only
// honest shared number — and no kill credit exists by design, so nobody is
// named).
func TestMonsterKillAnnouncedInChat(t *testing.T) {
	t.Parallel()

	w := newWorld()

	var announced []string

	w.SetAnnounce(func(_, text string) { announced = append(announced, text) })

	oneHitKillBubble(t, w, killMissSeed)

	want := fmt.Sprintf("a wolf was slain (+%d XP to everyone in the fight)", game.MonsterXPForTest("wolf"))
	if !slices.Contains(announced, want) {
		t.Errorf("announced = %v, want to contain %q", announced, want)
	}
}

// TestPlayerDeathAnnouncedInChat: a player death announces "NAME died" — the
// one combat event that previously happened with zero textual feedback (the
// entity just reappears at a spawn hex).
func TestPlayerDeathAnnouncedInChat(t *testing.T) {
	t.Parallel()

	w := newWorld()

	var announced []string

	w.SetAnnounce(func(_, text string) { announced = append(announced, text) })

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	victim := joinNamed(t, w, "victim")
	w.SetHexForTest(victim.EntityID, center)
	w.PlaceMonsterForTest(walkableNeighbor(t, w, center))
	// One claw swipe (3, before the victim's take-damage floor of ≥1) must
	// kill: any positive damage beats 1 HP.
	w.SetHPForTest(victim.EntityID, 1)

	step(t, w) // bubble forms around victim + monster
	step(t, w) // bubble turn: the monster bumps, the victim dies

	if !slices.Contains(announced, "victim died") {
		t.Errorf("announced = %v, want to contain %q", announced, "victim died")
	}
}
