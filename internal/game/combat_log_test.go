package game_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestMonsterKillAnnouncedInChatSolo: a bubble turn that kills monsters
// announces a kill summary through the chat hook (the de facto combat log),
// naming the slain kind (oneHitKillBubble's default spawn is wolf). Uses the
// miss seed so no pickup announce muddies the capture. oneHitKillBubble
// places exactly one player ("hero"), so — playtest item 3 — the announce
// names the killer: "hero slew a wolf (+20 XP)", not the nameless
// everyone-in-the-fight wording (that stays for 2+ players — see
// TestMonsterKillAnnouncedInChatNamesNobodyForTwoPlayers).
func TestMonsterKillAnnouncedInChatSolo(t *testing.T) {
	t.Parallel()

	w := newWorld()

	var announced []string

	w.SetAnnounce(func(_, text string) { announced = append(announced, text) })

	oneHitKillBubble(t, w, killMissSeed)

	want := fmt.Sprintf("hero slew a wolf (+%d XP)", game.MonsterXPForTest("wolf"))
	if !slices.Contains(announced, want) {
		t.Errorf("announced = %v, want to contain %q", announced, want)
	}
}

// TestMonsterKillAnnouncedInChatNamesNobodyForTwoPlayers: with two or more
// players in the bubble at award time, the nameless "everyone in the fight"
// wording is unchanged (playtest item 3 only names a solo killer) — no kill
// credit exists for a shared fight.
func TestMonsterKillAnnouncedInChatNamesNobodyForTwoPlayers(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(killMissSeed)

	var announced []string

	w.SetAnnounce(func(_, text string) { announced = append(announced, text) })

	// Only idA/tokA (the attacker) and monsterID are needed; twoPlayerBubble's
	// second player (idB/tokB) and turn bundle (form) exist only to seed the
	// two-player bubble itself — kept and explicitly discarded rather than
	// blanked to stay within dogsled's 2-blank-identifier limit.
	idA, tokA, idB, tokB, monsterID, form := twoPlayerBubble(t, w)
	_, _ = idB, tokB
	_ = form

	w.SetHPForTest(monsterID, game.ItemDamageForTest("iron-sword"))

	if err := w.SubmitIntent(entityAttackIntent(idA, tokA, monsterID)); err != nil {
		t.Fatalf("SubmitIntent(melee): %v", err)
	}

	// ResolveTurnForTest is ungated (lock-in/patience are exercised
	// elsewhere) — it resolves this bubble-turn regardless of B's lock-in
	// state, so A's melee attack alone one-hit-kills the monster here.
	w.ResolveTurnForTest()

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
	step(t, w) // bubble turn: the monster strikes, the victim dies

	if !slices.Contains(announced, "victim died") {
		t.Errorf("announced = %v, want to contain %q", announced, "victim died")
	}
}
