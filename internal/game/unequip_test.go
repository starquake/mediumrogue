package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestUnequipOutsideBubbleTogglesOff: an equip intent naming an item ALREADY
// in its slot unequips it instead of re-equipping — free and immediate
// outside a bubble, mirroring the equip-on rules (item 2).
func TestUnequipOutsideBubbleTogglesOff(t *testing.T) {
	t.Parallel()

	w := newWorld()

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	closeInst, _ := w.EquippedSlotsForTest(me.EntityID)
	if closeInst == 0 {
		t.Fatalf("fighter join default left the close slot empty")
	}

	// Equip the already-equipped item: toggles it OFF.
	if err := w.SubmitIntent(equipIntent(me.EntityID, me.Token, closeInst)); err != nil {
		t.Fatalf("SubmitIntent equip (toggle off): %v", err)
	}

	if got, _ := w.EquippedSlotsForTest(me.EntityID); got != 0 {
		t.Errorf("close slot = %d, want 0 (unequipped)", got)
	}

	// Toggling the same item again re-equips it (round-trip).
	if err := w.SubmitIntent(equipIntent(me.EntityID, me.Token, closeInst)); err != nil {
		t.Fatalf("SubmitIntent equip (toggle on): %v", err)
	}

	if closeAfter, _ := w.EquippedSlotsForTest(me.EntityID); closeAfter != closeInst {
		t.Errorf("close slot = %d, want %d (re-equipped)", closeAfter, closeInst)
	}
}

// TestUnequipInBubbleQueuesTurnConsumption: inside a combat bubble,
// unequipping is the player's action for the turn just like equipping — it
// marks the player ready and only flips the slot once the bubble-turn
// resolves.
func TestUnequipInBubbleQueuesTurnConsumption(t *testing.T) {
	t.Parallel()

	w := newWorld()
	idA, tokA, idB, tokB, _, _ := twoPlayerBubble(t, w)

	closeInst, _ := w.EquippedSlotsForTest(idA)
	if closeInst == 0 {
		t.Fatalf("fighter join default left A's close slot empty")
	}

	if err := w.SubmitIntent(equipIntent(idA, tokA, closeInst)); err != nil {
		t.Fatalf("SubmitIntent equip (toggle off): %v", err)
	}

	if got, _ := w.EquippedSlotsForTest(idA); got != closeInst {
		t.Fatalf("close slot flipped before the bubble-turn resolved (got %d)", got)
	}

	waiting := w.Snapshot().Bubbles[0].WaitingForIDs
	if len(waiting) != 1 || waiting[0] != idB {
		t.Fatalf("WaitingForIDs = %v, want only %d (unequip must mark A ready)", waiting, idB)
	}

	// B locks in with a no-op move onto its own hex -> bubble resolves.
	bHex := entityHexByID(t, w, idB)
	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: idB, Token: tokB, Kind: protocol.IntentMove, Target: bHex,
	}); err != nil {
		t.Fatalf("SubmitIntent B move: %v", err)
	}

	if got, _ := w.EquippedSlotsForTest(idA); got != 0 {
		t.Errorf("close slot after resolution = %d, want 0 (unequipped)", got)
	}
}

// TestFistsFallbackDamageAfterUnequip: once a player's close slot is
// unequipped, closeDefFor falls back to fists — a melee attack deals exactly
// protocol.FistsDamage (before the take-damage pipeline), not the weapon's
// damage.
func TestFistsFallbackDamageAfterUnequip(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	center := protocol.Hex{Q: 0, R: 0}
	monsterHex := protocol.Hex{Q: 1, R: 0}

	playerID, token := w.PlaceEntityForTest(center)
	w.SetClassForTest(playerID, protocol.ClassFighter)

	closeInst, _ := w.EquippedSlotsForTest(playerID)
	if closeInst == 0 {
		t.Fatalf("fighter default left the close slot empty")
	}

	if err := w.SubmitIntent(equipIntent(playerID, token, closeInst)); err != nil {
		t.Fatalf("SubmitIntent equip (toggle off): %v", err)
	}

	if got, _ := w.EquippedSlotsForTest(playerID); got != 0 {
		t.Fatalf("close slot = %d, want 0 (unequipped)", got)
	}

	monsterID := w.PlaceMonsterForTest(monsterHex)
	monsterMaxHP := game.MonsterMaxHPForTest(w.MonsterKindForTest(monsterID))

	w.SetPathForTest(playerID, []protocol.Hex{monsterHex})
	w.ResolveCombatOnlyForTest()

	snap := w.Snapshot()

	wantHP := monsterMaxHP - protocol.FistsDamage
	if got := entityHP(t, snap, monsterID); got != wantHP {
		t.Errorf("monster HP = %d, want %d (fists fallback damage after unequip)", got, wantHP)
	}
}

// entityHexByID returns id's current hex from a fresh snapshot, so a bubble
// test can lock in a no-op move without hardcoding the pre-formation hex.
func entityHexByID(t *testing.T, w *game.World, id int64) protocol.Hex {
	t.Helper()

	return hexOfSnap(w.Snapshot(), id)
}
