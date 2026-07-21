package integration_test

// inventory_test.go: inventory-slots task 4 — the full inventory loop over
// real HTTP (drop, walk-free pickup accept, the exact backpack-full
// rejection, drink) and the restart round-trip with equipped + stacked
// state. The starting state is a HAND-CRAFTED v4 snapshot JSON (bumped from
// v3 by the gear keystone, #55/#56 — main-hand replaces melee-weapon as the
// equipped-map key) restored into the server before it starts — which both
// engineers an exact inventory (equipped sword, a 3-potion stack, a full
// backpack, a ground item) without any package-internal test bridge, and
// pins the disk format from outside internal/game: if a DTO json tag
// drifts, this file fails.

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// Instance ids used by the crafted snapshot — all below invSnapshotNextID.
const (
	invPlayerID     = int64(1)
	invSwordID      = int64(10)
	invPotionID     = int64(11)
	invHammer1ID    = int64(12)
	invHammer2ID    = int64(13)
	invHammer3ID    = int64(14)
	invGroundFangID = int64(20)

	invToken            = "itest-inventory-token"
	invSnapshotNextID   = 100
	invWarhammerDef     = "iron-warhammer"
	invHealingPotionDef = "healing-potion"
)

// invFighterMaxHP is a level-1 fighter's max HP; the crafted snapshot starts
// the player 10 below it so a potion's heal is visible.
const invFighterMaxHP = protocol.FighterMaxHP

// craftInventorySnapshot builds a current-version snapshot JSON literal
// (bump the version below alongside snapshotVersion): one fighter at
// the origin (the worldgen-forced clearing) with an equipped iron sword, a
// FULL backpack — a 3-potion stack plus three warhammers — and one
// venom-fang lying on the player's own hex.
func craftInventorySnapshot() []byte {
	type inst struct {
		ID    int64  `json:"id"`
		DefID string `json:"defId"`
	}

	type entry struct {
		Item  inst `json:"item"`
		Count int  `json:"count"`
	}

	snapshot := map[string]any{
		// re-derived for snapshotVersion 9 (#271: an entity's active timed
		// effects joined the entity DTO; this fixture carries none, so only the
		// version moves). The loader REJECTS a version mismatch by design, so a
		// crafted fixture has to move with the version — it is not a value to
		// weaken.
		"version":      9,
		"worldSeed":    persistSeed,
		"worldRadius":  persistRadius,
		"turn":         5,
		"nextId":       invSnapshotNextID,
		"nextBubbleId": 0,
		"nextPartyId":  0,
		"entities": []map[string]any{{
			"id": invPlayerID, "hex": protocol.Hex{Q: 0, R: 0}, "token": invToken,
			"kind": protocol.EntityPlayer, "monsterKind": "", "name": "packrat", "partyId": 0,
			"class": protocol.ClassFighter, "species": protocol.SpeciesDwarf,
			"hp": invFighterMaxHP - 10, "maxHp": invFighterMaxHP, "xp": 0,
			"equipped": map[string]inst{
				protocol.SlotMainHand: {ID: invSwordID, DefID: "iron-sword"},
			},
			"backpack": []entry{
				{Item: inst{ID: invPotionID, DefID: invHealingPotionDef}, Count: 3},
				{Item: inst{ID: invHammer1ID, DefID: invWarhammerDef}, Count: 1},
				{Item: inst{ID: invHammer2ID, DefID: invWarhammerDef}, Count: 1},
				{Item: inst{ID: invHammer3ID, DefID: invWarhammerDef}, Count: 1},
			},
		}},
		"groundItems": []map[string]any{{
			"hex":    protocol.Hex{Q: 0, R: 0},
			"stacks": []entry{{Item: inst{ID: invGroundFangID, DefID: "venom-fang"}, Count: 1}},
		}},
		"quests":  []any{},
		"archive": map[string]any{},
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		panic(err)
	}

	return data
}

// postInventoryIntent posts an inventory-action intent and returns the
// response (callers assert the status they expect).
func postInventoryIntent(
	t *testing.T, ts *httptest.Server, me protocol.JoinResponse, kind string, itemID, groundItemID int64,
) *http.Response {
	t.Helper()

	return postJSON(t, ts, "/api/intent", protocol.IntentRequest{
		EntityID: me.EntityID, Token: me.Token, Kind: kind, ItemID: itemID, GroundItemID: groundItemID,
	})
}

// waitForBundle reads turn frames until pred passes one, or fails at the
// deadline.
func waitForBundle(
	t *testing.T, reader *bufio.Reader, what string, pred func(protocol.TurnEvent) bool,
) protocol.TurnEvent {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)

	var last protocol.TurnEvent

	for time.Now().Before(deadline) {
		last = decodeTurnFrame(t, reader)
		if pred(last) {
			return last
		}
	}

	t.Fatalf("no turn bundle satisfied %q before the deadline; last bundle: %+v", what, last)

	return last
}

// itemOf finds an ItemView by instance id on the crafted player entity in a
// bundle (every caller inspects invPlayerID — the fixed entity id from the
// crafted snapshot).
func itemOf(bundle protocol.TurnEvent, itemID int64) (protocol.ItemView, bool) {
	e, ok := entityOf(bundle, invPlayerID)
	if !ok {
		return protocol.ItemView{}, false
	}

	for _, it := range e.Items {
		if it.ID == itemID {
			return it, true
		}
	}

	return protocol.ItemView{}, false
}

// TestInventoryLoopOverHTTP drives the whole task-2 action set over real
// HTTP against a crafted starting inventory: the wire shape (types, stack
// count), the exact backpack-full pickup rejection, drink (heal + stack
// decrement), drop freeing an entry, the then-accepted pickup, and finally
// the restart round-trip with the equipped + stacked state intact.
//
//nolint:paralleltest // serial by design (#22): tick loop must not be CPU-starved by parallel siblings.
func TestInventoryLoopOverHTTP(t *testing.T) {
	pwA := newPersistWorld(t)
	if err := pwA.world.RestoreState(craftInventorySnapshot()); err != nil {
		t.Fatalf("RestoreState(crafted snapshot): %v", err)
	}

	tsA := pwA.serve(t)

	me := join(t, tsA, invToken)
	if got, want := me.EntityID, invPlayerID; got != want {
		t.Fatalf("rejoined EntityID = %d, want %d (live reclaim of the restored entity)", got, want)
	}

	reader := bufio.NewReader(get(t, tsA, "/api/events").Body)

	// The crafted inventory rides the wire: equipped sword (type main-hand —
	// an equipped weapon's Type is the hand it occupies, not the generic
	// "weapon" taxonomy string, since the gear keystone's dual-wield model),
	// a count-3 potion stack (type consumable), and the ground venom-fang
	// with its type.
	first := waitForBundle(t, reader, "restored inventory visible", func(b protocol.TurnEvent) bool {
		_, ok := itemOf(b, invSwordID)

		return ok
	})

	sword, _ := itemOf(first, invSwordID)
	if !sword.Equipped || sword.Type != protocol.SlotMainHand {
		t.Errorf("sword view = %+v, want equipped in main-hand", sword)
	}

	potion, ok := itemOf(first, invPotionID)
	if !ok || potion.Count != 3 || potion.Type != protocol.ItemTypeConsumable {
		t.Errorf("potion view = %+v (ok=%v), want a count-3 consumable stack", potion, ok)
	}

	if got, want := len(first.GroundItems), 1; got != want {
		t.Fatalf("len(GroundItems) = %d, want %d", got, want)
	}

	if got, want := first.GroundItems[0].Type, protocol.ItemTypeWeapon; got != want {
		t.Errorf("ground venom-fang Type = %q, want %q", got, want)
	}

	// REJECT: the backpack is full and venom-fang is gear (no merge) — the
	// pickup 422s with the exact client-facing feedback line.
	resp := postInventoryIntent(t, tsA, me, protocol.IntentPickup, 0, invGroundFangID)
	if got, want := resp.StatusCode, http.StatusUnprocessableEntity; got != want {
		t.Fatalf("full-backpack pickup status = %d, want %d", got, want)
	}

	var apiErr protocol.ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode 422 body: %v", err)
	}

	if got, want := apiErr.Error, "backpack full — drop something first"; got != want {
		t.Errorf("422 error = %q, want %q", got, want)
	}

	// DRINK: heal +5 (from 10 below max) and the stack decrements to 2.
	if got, want := postInventoryIntent(t, tsA, me, protocol.IntentDrink, invPotionID, 0).StatusCode,
		http.StatusAccepted; got != want {
		t.Fatalf("drink status = %d, want %d", got, want)
	}

	afterDrink := waitForBundle(t, reader, "drink applied", func(b protocol.TurnEvent) bool {
		p, ok := itemOf(b, invPotionID)

		return ok && p.Count == 2
	})

	e, _ := entityOf(afterDrink, invPlayerID)
	if got, want := e.HP, invFighterMaxHP-10+5; got < want {
		// >= not ==: out-of-combat regen (+1/turn) may add on top while the
		// bundle stream ticks.
		t.Errorf("HP after drink = %d, want >= %d", got, want)
	}

	// DROP a warhammer: an entry frees and the hammer joins the ground.
	if got, want := postInventoryIntent(t, tsA, me, protocol.IntentDrop, invHammer1ID, 0).StatusCode,
		http.StatusAccepted; got != want {
		t.Fatalf("drop status = %d, want %d", got, want)
	}

	waitForBundle(t, reader, "dropped hammer on the ground", func(b protocol.TurnEvent) bool {
		for _, g := range b.GroundItems {
			if g.ID == invHammer1ID {
				return true
			}
		}

		return false
	})

	// ACCEPT: the same pickup now succeeds into the freed entry.
	if got, want := postInventoryIntent(t, tsA, me, protocol.IntentPickup, 0, invGroundFangID).StatusCode,
		http.StatusAccepted; got != want {
		t.Fatalf("pickup-after-drop status = %d, want %d", got, want)
	}

	preRestart := waitForBundle(t, reader, "venom-fang picked up", func(b protocol.TurnEvent) bool {
		fang, ok := itemOf(b, invGroundFangID)

		return ok && !fang.Equipped
	})

	assertRestartRoundTrip(t, pwA, preRestart)
}

// assertRestartRoundTrip is TestInventoryLoopOverHTTP's final phase: server
// B restores server A's marshaled state; the same token reclaims the same
// character with the equipped + stacked inventory intact (ids and counts,
// not just names).
func assertRestartRoundTrip(t *testing.T, pwA *persistWorld, preRestart protocol.TurnEvent) {
	t.Helper()

	data, err := pwA.world.MarshalState()
	if err != nil {
		t.Fatalf("MarshalState: %v", err)
	}

	pwB := newPersistWorld(t)
	if err := pwB.world.RestoreState(data); err != nil {
		t.Fatalf("RestoreState(server B): %v", err)
	}

	tsB := pwB.serve(t)

	back := join(t, tsB, invToken)
	if got, want := back.EntityID, invPlayerID; got != want {
		t.Fatalf("server B rejoin EntityID = %d, want %d", got, want)
	}

	readerB := bufio.NewReader(get(t, tsB, "/api/events").Body)
	postSnap := waitForBundle(t, readerB, "server B inventory visible", func(b protocol.TurnEvent) bool {
		_, ok := itemOf(b, invSwordID)

		return ok
	})

	if sword, ok := itemOf(postSnap, invSwordID); !ok || !sword.Equipped ||
		sword.Type != protocol.SlotMainHand {
		t.Errorf("server B sword view = %+v (ok=%v), want equipped in main-hand", sword, ok)
	}

	if potion, ok := itemOf(postSnap, invPotionID); !ok || potion.Count != 2 {
		t.Errorf("server B potion view = %+v (ok=%v), want the count-2 stack", potion, ok)
	}

	if fang, ok := itemOf(postSnap, invGroundFangID); !ok || fang.Equipped {
		t.Errorf("server B venom-fang view = %+v (ok=%v), want owned unequipped", fang, ok)
	}

	if _, ok := itemOf(postSnap, invHammer1ID); ok {
		t.Error("server B still shows the dropped hammer as owned")
	}

	foundHammer := false

	for _, g := range postSnap.GroundItems {
		if g.ID == invHammer1ID {
			foundHammer = true
		}
	}

	if !foundHammer {
		t.Error("server B ground items missing the dropped hammer")
	}

	// Sanity: the pre-restart snapshot and server B agree on the player's
	// item count.
	preMe, _ := entityOf(preRestart, invPlayerID)
	postMe, _ := entityOf(postSnap, invPlayerID)

	if got, want := len(postMe.Items), len(preMe.Items); got != want {
		t.Errorf("server B item count = %d, want %d (same as pre-restart)", got, want)
	}
}

// TestCraftedSnapshotVersionGate: the crafted snapshot with its version
// field rewritten to 1 (the pre-inventory shape's version) is refused by
// RestoreState — the world starts fresh instead of guessing at a migration
// (the app layer then preserves the rejected file aside; that wiring is
// cmd/rogue/app's own tested behavior).
func TestCraftedSnapshotVersionGate(t *testing.T) {
	t.Parallel()

	var raw map[string]any
	if err := json.Unmarshal(craftInventorySnapshot(), &raw); err != nil {
		t.Fatalf("unmarshal crafted snapshot: %v", err)
	}

	raw["version"] = 1

	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}

	pw := newPersistWorld(t)

	err = pw.world.RestoreState(data)
	if err == nil {
		t.Fatal("RestoreState(v1 snapshot) = nil, want a version-mismatch error")
	}

	if got, want := err.Error(), "version"; !strings.Contains(got, want) {
		t.Errorf("err = %q, should contain %q", got, want)
	}
}
