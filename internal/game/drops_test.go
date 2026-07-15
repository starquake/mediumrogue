package game_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// Pinned seeds for the one-hit-kill drop roll (rng.IntN(100) < the slain
// KIND's own dropChance — wolf's 30%, the default spawn kind, since loot
// moved monster-side in 6c — drawn from the same PCG stream as the rest of
// a bubble-turn resolution: move-shuffle first, then the bump's damage
// roll, then the drop roll in resolveDeathsLocked). Found by probing
// killSeedDrops' exact scenario: seed 0 misses (no ground item), seed 4
// hits. WHICH def the hit yields depends on the whole of wolf's own drop
// table (the weighted pick walks it — content.go's monsterDefs), so these
// two constants are re-derived whenever that table changes — the tests
// prove the drop→pickup cycle, not any particular item. Current values:
// re-derived after the first designer batch (mattock + war-mage staff)
// widened the table; 6c kept wolf's table byte-identical to that pre-6c
// global dropTable precisely so these seeds survive.
const (
	killMissSeed        = 0
	killDropSeed        = 4
	killDropSeedDefID   = "venom-fang"
	killDropSeedDefName = "Venom Fang"
)

// oneHitKillBubble joins a named level-1 Fighter (iron sword, the Join
// default), relocates it next to a monster whose HP is set to die to
// exactly one bump, forms the bubble, then bumps — killing the monster
// inside the fight (so the shared kill-XP/drop path runs). A real Join
// (rather than PlaceEntityForTest) gives the player an actual display name,
// so the announce-text tests can assert the exact "NAME picked up ITEM"
// wording. Returns the player's join response (id + token — the pickup
// tests submit real intents) plus the post-kill snapshot.
func oneHitKillBubble(t *testing.T, w *game.World, seed int64) (protocol.JoinResponse, protocol.TurnEvent) {
	t.Helper()

	w.SetSeedForTest(seed)

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	monsterHex := walkableNeighbor(t, w, center)

	me := joinNamed(t, w, "hero")
	playerID := me.EntityID
	w.SetHexForTest(playerID, center)

	monsterID := w.PlaceMonsterForTest(monsterHex)
	w.SetHPForTest(monsterID, game.ItemDamageForTest("iron-sword"))

	step(t, w) // idle turn: forms the bubble around the player and monster

	w.SetPathForTest(playerID, []protocol.Hex{monsterHex})
	snap := step(t, w) // bump-kills the monster inside the bubble

	if _, ok := entityOfSnap(snap, monsterID); ok {
		t.Fatalf("monster %d should have died to the one-hit-kill bump", monsterID)
	}

	return me, snap
}

// TestPickDropCoversWolfsWholeTable: pickDropFrom, run over wolf's own drop
// table (content.go's monsterDefs) over a fixed seed range, returns every
// entry in it at least once — loot authority is monster-side since 6c
// (items no longer carry any drop weight of their own), so this asserts
// against the live per-kind table, not a hand-duplicated literal list.
func TestPickDropCoversWolfsWholeTable(t *testing.T) {
	t.Parallel()

	want := game.DropTableIDsForTest("wolf")
	if len(want) == 0 {
		t.Fatalf("wolf's drop table is empty — nothing to distribute")
	}

	seen := make(map[string]bool, len(want))

	const seedRange = 200

	for seed := range uint64(seedRange) {
		id := game.PickDropForTest("wolf", seed)
		if id == "" {
			t.Fatalf("PickDropForTest(wolf, %d) = \"\" (empty draw)", seed)
		}

		seen[id] = true
	}

	for _, id := range want {
		if !seen[id] {
			t.Errorf("wolf drop table id %q never drawn over %d seeds", id, seedRange)
		}
	}
}

// TestKillDropVisibleInSnapshot: a monster killed in a bubble leaves a ground
// item visible in Snapshot().GroundItems when its seed rolls a drop
// (killDropSeed); the killer does not auto-loot it the same turn (a bump
// attacker stays in place — see collectBumpsLocked — so it never lands on
// the corpse hex this turn).
func TestKillDropVisibleInSnapshot(t *testing.T) {
	t.Parallel()

	w := newWorld()
	me, snap := oneHitKillBubble(t, w, killDropSeed)
	playerID := me.EntityID

	if got, want := len(snap.GroundItems), 1; got != want {
		t.Fatalf("len(GroundItems) = %d, want %d", got, want)
	}

	ground := snap.GroundItems[0]
	if got, want := ground.DefID, killDropSeedDefID; got != want {
		t.Errorf("GroundItemView.DefID = %q, want %q", got, want)
	}

	if got, want := ground.Name, killDropSeedDefName; got != want {
		t.Errorf("GroundItemView.Name = %q, want %q", got, want)
	}

	playerHex := hexOfSnap(snap, playerID) // player did not move onto the corpse

	if got, want := ground.Hex, playerHex; got == want {
		t.Fatalf("dropped item landed on the killer's own hex %v — bump attacker should not have moved", got)
	}

	player, ok := entityOfSnap(snap, playerID)
	if !ok {
		t.Fatalf("player %d missing from snapshot", playerID)
	}

	if got, want := len(player.Items), 1; got != want { // just the starting iron sword
		t.Errorf("killer Items = %d, want %d (no same-turn auto-loot)", got, want)
	}
}

// TestKillMissDropsNothing: the miss-seed control for TestKillDropVisibleInSnapshot
// — the same one-hit-kill scenario at killMissSeed leaves no ground item.
func TestKillMissDropsNothing(t *testing.T) {
	t.Parallel()

	w := newWorld()
	_, snap := oneHitKillBubble(t, w, killMissSeed)

	if got, want := len(snap.GroundItems), 0; got != want {
		t.Fatalf("len(GroundItems) = %d, want %d (miss-seed control)", got, want)
	}
}

// TestKillDropPickedUpViaIntent: the item dropped on a kill is collected by
// walking onto its hex and submitting an explicit pickup INTENT (the
// inventory-slots milestone removed walk-over auto-pickup). Confirms the
// full drop -> walk -> pickup-intent cycle: Items grows (unequipped, into
// the backpack), GroundItems empties, and the pickup announces.
func TestKillDropPickedUpViaIntent(t *testing.T) {
	t.Parallel()

	w := newWorld()

	var announced []string

	w.SetAnnounce(func(_, text string) { announced = append(announced, text) })

	me, snap := oneHitKillBubble(t, w, killDropSeed)
	playerID := me.EntityID

	corpseHex := snap.GroundItems[0].Hex
	itemID := snap.GroundItems[0].ID

	w.SetPathForTest(playerID, []protocol.Hex{corpseHex})
	walked := step(t, w)

	// Walking onto the item no longer grants it: it is still on the ground.
	if got, want := len(walked.GroundItems), 1; got != want {
		t.Fatalf("len(GroundItems) after walk-on = %d, want %d (no auto-pickup)", got, want)
	}

	// The bubble dissolved with the kill, so the pickup intent is free and
	// immediate.
	err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: playerID, Token: me.Token, Kind: protocol.IntentPickup, GroundItemID: itemID,
	})
	if err != nil {
		t.Fatalf("SubmitIntent pickup: %v", err)
	}

	resolved := w.Snapshot()

	if got, want := len(resolved.GroundItems), 0; got != want {
		t.Fatalf("len(GroundItems) after pickup = %d, want %d", got, want)
	}

	player, ok := entityOfSnap(resolved, playerID)
	if !ok {
		t.Fatalf("player %d missing from snapshot", playerID)
	}

	if got, want := len(player.Items), 2; got != want { // iron sword + the picked-up drop
		t.Fatalf("player Items = %d, want %d", got, want)
	}

	idx := slices.IndexFunc(player.Items, func(it protocol.ItemView) bool { return it.ID == itemID })
	if idx == -1 {
		t.Fatalf("picked-up instance %d not found in player.Items: %+v", itemID, player.Items)
	}

	if got, want := player.Items[idx].DefID, killDropSeedDefID; got != want {
		t.Errorf("picked-up ItemView.DefID = %q, want %q", got, want)
	}

	if got, want := player.Items[idx].Equipped, false; got != want {
		t.Errorf("picked-up ItemView.Equipped = %v, want %v (owned, not auto-equipped)", got, want)
	}

	// Two lines in order: the kill summary from the turn the monster died —
	// oneHitKillBubble's solo "hero" (playtest item 3 names a solo killer)
	// — then the pickup announce from the accepted pickup intent.
	wantMsg := []string{
		fmt.Sprintf("hero slew a wolf (+%d XP)", game.MonsterXPForTest("wolf")),
		"hero picked up " + killDropSeedDefName,
	}
	if !slices.Equal(announced, wantMsg) {
		t.Errorf("announced = %v, want %v", announced, wantMsg)
	}
}

// TestWalkOverDoesNotAutoPickup: a player walking onto a ground item no
// longer auto-collects it (the inventory-slots milestone removed walk-over
// auto-pickup) — the item stays on the ground until an explicit pickup
// intent names it, and THAT grants it and announces "NAME picked up ITEM".
func TestWalkOverDoesNotAutoPickup(t *testing.T) {
	t.Parallel()

	w := newWorld()

	alice := joinNamed(t, w, "alice")

	target := walkableNeighbor(t, w, alice.Hex)
	itemID := w.GroundItemForTest(target, "venom-fang")

	var announced []string

	w.SetAnnounce(func(_, text string) { announced = append(announced, text) })

	w.SetPathForTest(alice.EntityID, []protocol.Hex{target})
	snap := step(t, w)

	if got, want := hexOfSnap(snap, alice.EntityID), target; got != want {
		t.Fatalf("alice's hex = %v, want %v (did not walk onto the item)", got, want)
	}

	if got, want := len(snap.GroundItems), 1; got != want {
		t.Fatalf("len(GroundItems) after walk-on = %d, want %d (no auto-pickup)", got, want)
	}

	if got, want := len(announced), 0; got != want {
		t.Fatalf("announced = %v, want none (walking over must not collect)", announced)
	}

	err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: alice.EntityID, Token: alice.Token, Kind: protocol.IntentPickup, GroundItemID: itemID,
	})
	if err != nil {
		t.Fatalf("SubmitIntent pickup: %v", err)
	}

	snap = w.Snapshot()

	if got, want := len(snap.GroundItems), 0; got != want {
		t.Fatalf("len(GroundItems) after pickup intent = %d, want %d", got, want)
	}

	player, ok := entityOfSnap(snap, alice.EntityID)
	if !ok {
		t.Fatalf("alice missing from snapshot")
	}

	idx := slices.IndexFunc(player.Items, func(it protocol.ItemView) bool { return it.ID == itemID })
	if idx == -1 {
		t.Fatalf("picked-up instance %d not found in alice.Items: %+v", itemID, player.Items)
	}

	if got, want := player.Items[idx].Name, "Venom Fang"; got != want {
		t.Errorf("picked-up ItemView.Name = %q, want %q", got, want)
	}

	if got, want := announced, []string{"alice picked up Venom Fang"}; !slices.Equal(got, want) {
		t.Errorf("announced = %v, want %v", got, want)
	}
}

// TestContestedPickupFirstIntentWins: two players on the same hex both go
// for the same ground item — the first accepted pickup intent takes it, and
// the second is rejected with ErrNoSuchGroundItem (the item is gone), the
// intent-flow analog of the old walk-over lowest-id tie-break.
func TestContestedPickupFirstIntentWins(t *testing.T) {
	t.Parallel()

	w := newWorld()

	target := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, target) {
		t.Skip("origin is not walkable on this map")
	}

	idA, tokenA := w.PlaceEntityForTest(target)
	idB, tokenB := w.PlaceEntityForTest(target)

	itemID := w.GroundItemForTest(target, "pack-bow")

	err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: idA, Token: tokenA, Kind: protocol.IntentPickup, GroundItemID: itemID,
	})
	if err != nil {
		t.Fatalf("SubmitIntent pickup (A): %v", err)
	}

	if got, want := w.SubmitIntent(protocol.IntentRequest{
		EntityID: idB, Token: tokenB, Kind: protocol.IntentPickup, GroundItemID: itemID,
	}), game.ErrNoSuchGroundItem; !errors.Is(got, want) {
		t.Errorf("second pickup err = %v, want %v", got, want)
	}

	snap := w.Snapshot()

	if got, want := len(snap.GroundItems), 0; got != want {
		t.Fatalf("len(GroundItems) = %d, want %d", got, want)
	}

	a, aOK := entityOfSnap(snap, idA)
	b, bOK := entityOfSnap(snap, idB)

	if !aOK || !bOK {
		t.Fatalf("both players must still be present: aOK=%v bOK=%v", aOK, bOK)
	}

	if idx := slices.IndexFunc(a.Items, func(it protocol.ItemView) bool { return it.ID == itemID }); idx == -1 {
		t.Errorf("first picker (A) did not collect the item: %+v", a.Items)
	}

	if got, want := len(b.Items), 1; got != want { // unchanged: just the starting iron sword
		t.Errorf("loser (B) Items = %d, want %d (must not have collected)", got, want)
	}
}
