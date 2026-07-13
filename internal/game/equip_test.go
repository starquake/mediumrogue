package game_test

import (
	"errors"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// equipIntent builds an "equip" IntentRequest for itemID, so the equip tests
// read as one line at the call site (mirrors ranged_test.go's attackIntent).
func equipIntent(id int64, token string, itemID int64) protocol.IntentRequest {
	return protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentEquip, ItemID: itemID,
	}
}

// TestEquipOutsideBubbleAppliesImmediately: outside a combat bubble, an equip
// intent is free — the slot flips synchronously, before any turn resolves.
func TestEquipOutsideBubbleAppliesImmediately(t *testing.T) {
	t.Parallel()

	w := newWorld()

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	instID := w.GrantItemForTest(me.EntityID, "iron-warhammer")

	if err := w.SubmitIntent(equipIntent(me.EntityID, me.Token, instID)); err != nil {
		t.Fatalf("SubmitIntent equip: %v", err)
	}

	// The fighter's main-hand already holds its class default (iron sword),
	// so the warhammer lands in off-hand (weaponTargetSlot's placement
	// matrix).
	_, offInst := w.EquippedSlotsForTest(me.EntityID)
	if got, want := offInst, instID; got != want {
		t.Errorf("off-hand slot = %d, want %d (equip outside a bubble must be immediate)", got, want)
	}
}

// TestEquipUnownedItemRejected: an item id the entity does not own — even a
// registered, valid def id — is rejected with ErrItemNotOwned.
func TestEquipUnownedItemRejected(t *testing.T) {
	t.Parallel()

	w := newWorld()

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	const unownedInstID = 999_999_999

	got := w.SubmitIntent(equipIntent(me.EntityID, me.Token, unownedInstID))
	if want := game.ErrItemNotOwned; !errors.Is(got, want) {
		t.Errorf("err = %v, want %v", got, want)
	}
}

// TestEquipIntentUnknownKindStillRejected: adding the "equip" intent kind must
// not loosen the default-case rejection for any other unknown Kind.
func TestEquipIntentUnknownKindStillRejected(t *testing.T) {
	t.Parallel()

	w := newWorld()

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	req := protocol.IntentRequest{EntityID: me.EntityID, Token: me.Token, Kind: "teleport"}

	if got, want := w.SubmitIntent(req), game.ErrInvalidIntentKind; !errors.Is(got, want) {
		t.Errorf("err = %v, want %v", got, want)
	}
}

// twoPlayerBubble places two Fighter players adjacent to a monster and runs
// one world turn so the bubble forms around all three (mirrors
// xp_test.go's TestSharedXPIsFullNotSplit setup). It returns both players'
// identities and the post-formation snapshot.
func twoPlayerBubble(t *testing.T, w *game.World) (int64, string, int64, string, int64, protocol.TurnEvent) {
	t.Helper()

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	ns := game.HexNeighbors(center)

	idA, tokA := w.PlaceEntityForTest(ns[0])
	idB, tokB := w.PlaceEntityForTest(ns[1])
	monsterID := w.PlaceMonsterForTest(center)

	snap := step(t, w)

	if !inCombat(t, snap, idA) || !inCombat(t, snap, idB) {
		t.Fatalf("players did not form a shared bubble around the monster")
	}

	return idA, tokA, idB, tokB, monsterID, snap
}

// TestEquipInBubbleQueuesClearsPathAppliesAfterTurn: inside a combat bubble,
// equip is the player's action for the turn — it does not flip the slot
// immediately, it clears an already-queued move, and it marks the player
// ready (so the OTHER player's lock-in, not a clock, resolves the bubble). The
// slot flips only once the bubble-turn actually resolves, and the equipping
// player does not move (its queued path was discarded in favor of the swap).
func TestEquipInBubbleQueuesClearsPathAppliesAfterTurn(t *testing.T) {
	t.Parallel()

	w := newWorld()
	idA, tokA, idB, tokB, _, form := twoPlayerBubble(t, w)

	hexA0 := hexOfSnap(form, idA)
	target := walkableNeighbor(t, w, hexA0)

	instID := w.GrantItemForTest(idA, "iron-warhammer")

	// Queue a move for A; B has not acted, so the bubble must not resolve yet.
	moveReq := protocol.IntentRequest{EntityID: idA, Token: tokA, Kind: protocol.IntentMove, Target: target}
	if err := w.SubmitIntent(moveReq); err != nil {
		t.Fatalf("SubmitIntent move: %v", err)
	}

	// Now equip: this must clear the queued move and NOT flip the slot yet
	// (B still has not locked in).
	if err := w.SubmitIntent(equipIntent(idA, tokA, instID)); err != nil {
		t.Fatalf("SubmitIntent equip: %v", err)
	}

	if closeInst, _ := w.EquippedSlotsForTest(idA); closeInst == instID {
		t.Fatalf("close slot flipped before the bubble-turn resolved")
	}

	waiting := w.Snapshot().Bubbles[0].WaitingForIDs
	if len(waiting) != 1 || waiting[0] != idB {
		t.Fatalf("WaitingForIDs = %v, want only %d (equip must mark A ready)", waiting, idB)
	}

	// B locks in (a no-op move onto its own hex is enough) -> bubble resolves.
	hexB0 := hexOfSnap(form, idB)
	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: idB, Token: tokB, Kind: protocol.IntentMove, Target: hexB0,
	}); err != nil {
		t.Fatalf("SubmitIntent B move: %v", err)
	}

	resolved := w.Snapshot()

	// A's main-hand already holds its class default (iron sword), so the
	// warhammer lands in off-hand (weaponTargetSlot's placement matrix).
	if _, offInst := w.EquippedSlotsForTest(idA); offInst != instID {
		t.Errorf("off-hand slot after resolution = %d, want %d", offInst, instID)
	}

	if got, want := hexOfSnap(resolved, idA), hexA0; got != want {
		t.Errorf("A's hex = %v, want unchanged %v (equip must have replaced the queued move)", got, want)
	}
}

// TestEquipInBubbleClearsPendingRangedAttack: equip queued after a ranged
// attack intent discards the attack — the equipping player does not also
// shoot this turn.
func TestEquipInBubbleClearsPendingRangedAttack(t *testing.T) {
	t.Parallel()

	w := newWorld()
	idA, tokA, idB, tokB, monsterID, form := twoPlayerBubble(t, w)
	w.SetClassForTest(idA, protocol.ClassRogue) // grants dagger + shortbow

	monsterHex := hexOfSnap(form, monsterID)
	monsterHP := entityHP(t, form, monsterID)

	instID := w.GrantItemForTest(idA, "venom-fang") // a Rogue close-slot item

	// Queue a ranged attack for A.
	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: idA, Token: tokA, Kind: protocol.IntentAttack, Target: monsterHex,
	}); err != nil {
		t.Fatalf("SubmitIntent attack: %v", err)
	}

	// Equip must clear that queued attack.
	if err := w.SubmitIntent(equipIntent(idA, tokA, instID)); err != nil {
		t.Fatalf("SubmitIntent equip: %v", err)
	}

	// B locks in without engaging the monster (steps onto its own hex).
	hexB0 := hexOfSnap(form, idB)
	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: idB, Token: tokB, Kind: protocol.IntentMove, Target: hexB0,
	}); err != nil {
		t.Fatalf("SubmitIntent B move: %v", err)
	}

	resolved := w.Snapshot()

	if closeInst, _ := w.EquippedSlotsForTest(idA); closeInst != instID {
		t.Errorf("close slot after resolution = %d, want %d", closeInst, instID)
	}

	if got, want := entityHP(t, resolved, monsterID), monsterHP; got != want {
		t.Errorf("monster HP = %d, want unchanged %d (A's queued ranged attack must have been discarded)", got, want)
	}
}

// TestEquipThenMoveInBubbleClearsPendingEquip: submitting a move AFTER an
// equip within the same input window must replace the queued equip too — the
// swap is the player's whole turn, so a later move/attack intent must discard
// it exactly as an equip discards a queued move/attack (the reverse ordering
// tested above). Without clearing pendingEquip, both the swap and the move
// would land the same turn.
func TestEquipThenMoveInBubbleClearsPendingEquip(t *testing.T) {
	t.Parallel()

	w := newWorld()
	idA, tokA, idB, tokB, _, form := twoPlayerBubble(t, w)

	hexA0 := hexOfSnap(form, idA)
	target := walkableNeighbor(t, w, hexA0)

	instID := w.GrantItemForTest(idA, "iron-warhammer")

	// Queue the equip first.
	if err := w.SubmitIntent(equipIntent(idA, tokA, instID)); err != nil {
		t.Fatalf("SubmitIntent equip: %v", err)
	}

	// Now move: this must clear the queued equip and become A's action instead.
	moveReq := protocol.IntentRequest{EntityID: idA, Token: tokA, Kind: protocol.IntentMove, Target: target}
	if err := w.SubmitIntent(moveReq); err != nil {
		t.Fatalf("SubmitIntent move: %v", err)
	}

	if got, want := w.PendingEquipForTest(idA), int64(0); got != want {
		t.Fatalf("pendingEquip after move queued = %d, want cleared (%d)", got, want)
	}

	// B locks in -> bubble resolves.
	hexB0 := hexOfSnap(form, idB)
	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: idB, Token: tokB, Kind: protocol.IntentMove, Target: hexB0,
	}); err != nil {
		t.Fatalf("SubmitIntent B move: %v", err)
	}

	resolved := w.Snapshot()

	if closeInst, _ := w.EquippedSlotsForTest(idA); closeInst == instID {
		t.Errorf("close slot after resolution = %d, want unchanged (equip replaced by the move)", closeInst)
	}

	if got, want := hexOfSnap(resolved, idA), target; got != want {
		t.Errorf("A's hex = %v, want %v (the queued move must have resolved, not been dropped)", got, want)
	}
}

// TestEquipThenAttackInBubbleClearsPendingEquip: submitting a ranged attack
// AFTER an equip within the same input window must replace the queued equip —
// the mirror of TestEquipThenMoveInBubbleClearsPendingEquip for attack.
func TestEquipThenAttackInBubbleClearsPendingEquip(t *testing.T) {
	t.Parallel()

	w := newWorld()
	idA, tokA, idB, tokB, monsterID, form := twoPlayerBubble(t, w)
	w.SetClassForTest(idA, protocol.ClassRogue) // grants dagger + shortbow

	monsterHex := hexOfSnap(form, monsterID)
	monsterHP := entityHP(t, form, monsterID)

	instID := w.GrantItemForTest(idA, "venom-fang") // a Rogue close-slot item

	// Queue the equip first.
	if err := w.SubmitIntent(equipIntent(idA, tokA, instID)); err != nil {
		t.Fatalf("SubmitIntent equip: %v", err)
	}

	// Now attack: this must clear the queued equip and become A's action instead.
	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: idA, Token: tokA, Kind: protocol.IntentAttack, Target: monsterHex,
	}); err != nil {
		t.Fatalf("SubmitIntent attack: %v", err)
	}

	if got, want := w.PendingEquipForTest(idA), int64(0); got != want {
		t.Fatalf("pendingEquip after attack queued = %d, want cleared (%d)", got, want)
	}

	// B locks in without engaging the monster -> bubble resolves.
	hexB0 := hexOfSnap(form, idB)
	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: idB, Token: tokB, Kind: protocol.IntentMove, Target: hexB0,
	}); err != nil {
		t.Fatalf("SubmitIntent B move: %v", err)
	}

	resolved := w.Snapshot()

	if closeInst, _ := w.EquippedSlotsForTest(idA); closeInst == instID {
		t.Errorf("close slot after resolution = %d, want unchanged (equip replaced by the attack)", closeInst)
	}

	if got, want := entityHP(t, resolved, monsterID), monsterHP; got >= want {
		t.Errorf("monster HP = %d, want less than %d (A's ranged attack must have resolved, not been dropped)", got, want)
	}
}

// TestImmediateEquipClearsStalePendingEquip: a pendingEquip left over from a
// dissolved bubble (e.g. the other member fled or died before the swap
// resolved) must not resurrect on the entity's next resolution — a later,
// free (out-of-bubble) equip of a different item must win outright, not get
// silently reverted to the stale queued item by the very next combat pass.
func TestImmediateEquipClearsStalePendingEquip(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id, tok := w.PlaceEntityForTest(protocol.Hex{Q: 0, R: 0})

	staleInst := w.GrantItemForTest(id, "iron-warhammer")
	w.SetPendingEquipForTest(id, staleInst)

	newInst := w.GrantItemForTest(id, "butchers-cleaver")

	if err := w.SubmitIntent(equipIntent(id, tok, newInst)); err != nil {
		t.Fatalf("SubmitIntent equip: %v", err)
	}

	// main-hand already holds the class default (iron sword), so the cleaver
	// lands in off-hand (weaponTargetSlot's placement matrix).
	if _, offInst := w.EquippedSlotsForTest(id); offInst != newInst {
		t.Fatalf("off-hand slot after immediate equip = %d, want %d", offInst, newInst)
	}

	// A full world-domain resolution (not ResolveCombatOnlyForTest, which
	// skips the pendingEquip-apply pass entirely) is what would silently
	// re-apply a stale pendingEquip left uncleared by the immediate-equip path.
	step(t, w)

	if _, offInst := w.EquippedSlotsForTest(id); offInst != newInst {
		t.Errorf("off-hand slot after resolution = %d, want %d (stale pendingEquip must not revert it)", offInst, newInst)
	}
}

// TestDeathClearsPendingEquip: a pending equip does not survive a death — and
// separately, gear already equipped (items owned + slots filled) is untouched
// by death/respawn, matching entity.items' doc comment ("gear survives
// death").
func TestDeathClearsPendingEquip(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id, _ := w.PlaceEntityForTest(protocol.Hex{Q: 0, R: 0})

	preCloseInst, preRangedInst := w.EquippedSlotsForTest(id)

	instID := w.GrantItemForTest(id, "iron-warhammer")
	w.SetPendingEquipForTest(id, instID)
	w.SetHPForTest(id, 0)

	w.ResolveCombatOnlyForTest()

	if got, want := w.PendingEquipForTest(id), int64(0); got != want {
		t.Errorf("pendingEquip after death = %d, want cleared (%d)", got, want)
	}

	// Gear survives death: the stale pending equip must not have been applied
	// either, and the pre-death equipped slots must be exactly what they were.
	closeInst, rangedInst := w.EquippedSlotsForTest(id)
	if got, want := closeInst, preCloseInst; got != want {
		t.Errorf("close slot after death = %d, want unchanged %d", got, want)
	}

	if got, want := rangedInst, preRangedInst; got != want {
		t.Errorf("ranged slot after death = %d, want unchanged %d", got, want)
	}
}

// chainBubbleHexes hunts the generated map for a player-anchored chain: a
// walkable anchor a, a walkable b at exactly CombatRadius from a, and a
// walkable m at exactly CombatRadius from b but beyond CombatRadius of a. A
// bubble over {a, b, m} then depends on the player at b (players anchor
// edges; a↔m is out of range), so removing b dissolves it around a.
func chainBubbleHexes(t *testing.T, w *game.World) (protocol.Hex, protocol.Hex, protocol.Hex) {
	t.Helper()

	tiles := w.Map().Tiles
	for _, ta := range tiles {
		if !isWalkable(w, ta.Hex) {
			continue
		}

		for _, tb := range tiles {
			if !isWalkable(w, tb.Hex) || game.HexDistance(ta.Hex, tb.Hex) != protocol.CombatRadius {
				continue
			}

			for _, tm := range tiles {
				if isWalkable(w, tm.Hex) &&
					game.HexDistance(tb.Hex, tm.Hex) == protocol.CombatRadius &&
					game.HexDistance(ta.Hex, tm.Hex) > protocol.CombatRadius+1 {
					return ta.Hex, tb.Hex, tm.Hex
				}
			}
		}
	}

	t.Skip("no player-anchored chain topology on this map")

	return protocol.Hex{}, protocol.Hex{}, protocol.Hex{}
}

// TestBubbleDissolveAppliesPendingEquip: a swap queued inside a bubble that
// dissolves before its turn resolves applies AT dissolve time — back in world
// time equips are free, so the queued intent must neither vanish nor leak
// into a later world-turn resolution as a silent late swap.
func TestBubbleDissolveAppliesPendingEquip(t *testing.T) {
	t.Parallel()

	w := newWorld()
	hexA, hexB, hexM := chainBubbleHexes(t, w)

	idA, tokA := w.PlaceEntityForTest(hexA)
	idB, tokB := w.PlaceEntityForTest(hexB)
	w.PlaceMonsterForTest(hexM)

	form := step(t, w)
	if !inCombat(t, form, idA) || !inCombat(t, form, idB) {
		t.Fatalf("chain bubble did not form (A: %v, B: %v)", inCombat(t, form, idA), inCombat(t, form, idB))
	}

	// A queues a swap inside the bubble; B never locks in, so it stays pending.
	instID := w.GrantItemForTest(idA, "iron-warhammer")
	if err := w.SubmitIntent(equipIntent(idA, tokA, instID)); err != nil {
		t.Fatalf("SubmitIntent equip: %v", err)
	}

	if got, want := w.PendingEquipForTest(idA), instID; got != want {
		t.Fatalf("pendingEquip = %d, want queued %d", got, want)
	}

	// B disconnects and is swept: the bubble loses its anchoring player and
	// dissolves around A without ever resolving its turn.
	w.StreamClosed(tokB)
	w.SetDisconnectGraceForTest(0)

	if !w.SweepForTest(time.Now().Add(time.Minute)) {
		t.Fatalf("sweep removed nobody; expected B to be swept")
	}

	if inCombat(t, w.Snapshot(), idA) {
		t.Fatalf("A still in combat after sweep; expected the bubble to dissolve")
	}

	// The queued swap applied at dissolve and is no longer pending. A's
	// main-hand already holds its class default (iron sword), so the
	// warhammer lands in off-hand (weaponTargetSlot's placement matrix).
	if _, offInst := w.EquippedSlotsForTest(idA); offInst != instID {
		t.Errorf("off-hand slot after dissolve = %d, want %d (queued equip must apply at dissolve)", offInst, instID)
	}

	if got, want := w.PendingEquipForTest(idA), int64(0); got != want {
		t.Errorf("pendingEquip after dissolve = %d, want %d", got, want)
	}
}
