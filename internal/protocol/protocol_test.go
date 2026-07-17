package protocol_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestItemAndGroundItemRoundTripOnWire is the protocol contract test for
// milestone 6b.4 Task 1: it proves ItemView (on an Entity) and
// GroundItemView (on a TurnEvent) survive a JSON encode/decode cycle intact
// — the same encode the server performs over SSE and the same decode shape
// the generated TS client relies on (protocol.gen.ts is a 1:1 mirror of
// these json tags, kept honest by `make protocol-check`). Game logic that
// actually populates Items/GroundItems from real gameplay lands in later
// tasks; this test exercises the wire format on its own.
func TestItemAndGroundItemRoundTripOnWire(t *testing.T) {
	t.Parallel()

	want := protocol.TurnEvent{
		Turn:       1,
		IntervalMs: 5000,
		Entities: []protocol.Entity{
			{
				ID:   1,
				Kind: protocol.EntityPlayer,
				Items: []protocol.ItemView{
					{
						ID:        7,
						DefID:     "sword",
						Name:      "Sword",
						Type:      protocol.ItemTypeWeapon,
						Tags:      []string{protocol.WeaponTagMelee},
						TwoHanded: false,
						Damage:    4,
						RangeHex:  1,
						AoERadius: 0,
						Desc:      "+3 vs targets below half HP",
						Equipped:  true,
						Count:     1,
					},
				},
			},
		},
		GroundItems: []protocol.GroundItemView{
			{
				ID:        9,
				Hex:       protocol.Hex{Q: 2, R: -1},
				DefID:     "bow",
				Name:      "Bow",
				Type:      protocol.ItemTypeWeapon,
				Tags:      []string{protocol.WeaponTagRanged},
				TwoHanded: true,
				Damage:    3,
				RangeHex:  4,
				AoERadius: 0,
				Desc:      "A ranged weapon.",
				Flavor:    "Yew and sinew.",
			},
		},
	}

	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal(TurnEvent) = %v, want nil error", err)
	}

	var got protocol.TurnEvent
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("json.Unmarshal(TurnEvent) = %v, want nil error", err)
	}

	if got, want := len(got.Entities), 1; got != want {
		t.Fatalf("len(got.Entities) = %d, want %d", got, want)
	}

	item := got.Entities[0]
	if got, want := len(item.Items), 1; got != want {
		t.Fatalf("len(got.Entities[0].Items) = %d, want %d", got, want)
	}

	gotItem, wantItem := item.Items[0], want.Entities[0].Items[0]
	if got, want := gotItem.ID, wantItem.ID; got != want {
		t.Errorf("ItemView.ID = %d, want %d", got, want)
	}

	if got, want := gotItem.DefID, wantItem.DefID; got != want {
		t.Errorf("ItemView.DefID = %q, want %q", got, want)
	}

	if got, want := gotItem.Name, wantItem.Name; got != want {
		t.Errorf("ItemView.Name = %q, want %q", got, want)
	}

	if got, want := gotItem.Type, wantItem.Type; got != want {
		t.Errorf("ItemView.Type = %q, want %q", got, want)
	}

	if got, want := len(gotItem.Tags), len(wantItem.Tags); got != want {
		t.Fatalf("len(ItemView.Tags) = %d, want %d", got, want)
	}

	if got, want := gotItem.Tags[0], wantItem.Tags[0]; got != want {
		t.Errorf("ItemView.Tags[0] = %q, want %q", got, want)
	}

	if got, want := gotItem.TwoHanded, wantItem.TwoHanded; got != want {
		t.Errorf("ItemView.TwoHanded = %v, want %v", got, want)
	}

	if got, want := gotItem.Count, wantItem.Count; got != want {
		t.Errorf("ItemView.Count = %d, want %d", got, want)
	}

	if got, want := gotItem.Damage, wantItem.Damage; got != want {
		t.Errorf("ItemView.Damage = %d, want %d", got, want)
	}

	if got, want := gotItem.RangeHex, wantItem.RangeHex; got != want {
		t.Errorf("ItemView.RangeHex = %d, want %d", got, want)
	}

	if got, want := gotItem.AoERadius, wantItem.AoERadius; got != want {
		t.Errorf("ItemView.AoERadius = %d, want %d", got, want)
	}

	if got, want := gotItem.Desc, wantItem.Desc; got != want {
		t.Errorf("ItemView.Desc = %q, want %q", got, want)
	}

	if got, want := gotItem.Equipped, wantItem.Equipped; got != want {
		t.Errorf("ItemView.Equipped = %v, want %v", got, want)
	}

	if got, want := len(got.GroundItems), 1; got != want {
		t.Fatalf("len(got.GroundItems) = %d, want %d", got, want)
	}

	gotGround, wantGround := got.GroundItems[0], want.GroundItems[0]
	if got, want := gotGround.ID, wantGround.ID; got != want {
		t.Errorf("GroundItemView.ID = %d, want %d", got, want)
	}

	if got, want := gotGround.Hex, wantGround.Hex; got != want {
		t.Errorf("GroundItemView.Hex = %+v, want %+v", got, want)
	}

	if got, want := gotGround.DefID, wantGround.DefID; got != want {
		t.Errorf("GroundItemView.DefID = %q, want %q", got, want)
	}

	if got, want := gotGround.Name, wantGround.Name; got != want {
		t.Errorf("GroundItemView.Name = %q, want %q", got, want)
	}

	if got, want := gotGround.Type, wantGround.Type; got != want {
		t.Errorf("GroundItemView.Type = %q, want %q", got, want)
	}

	// Detail fields (#139) survive the round-trip too.
	if got, want := gotGround.Damage, wantGround.Damage; got != want {
		t.Errorf("GroundItemView.Damage = %d, want %d", got, want)
	}

	if got, want := gotGround.RangeHex, wantGround.RangeHex; got != want {
		t.Errorf("GroundItemView.RangeHex = %d, want %d", got, want)
	}

	if got, want := gotGround.AoERadius, wantGround.AoERadius; got != want {
		t.Errorf("GroundItemView.AoERadius = %d, want %d", got, want)
	}

	if got, want := gotGround.TwoHanded, wantGround.TwoHanded; got != want {
		t.Errorf("GroundItemView.TwoHanded = %t, want %t", got, want)
	}

	if got, want := gotGround.Tags, wantGround.Tags; !slices.Equal(got, want) {
		t.Errorf("GroundItemView.Tags = %v, want %v", got, want)
	}

	if got, want := gotGround.Desc, wantGround.Desc; got != want {
		t.Errorf("GroundItemView.Desc = %q, want %q", got, want)
	}

	if got, want := gotGround.Flavor, wantGround.Flavor; got != want {
		t.Errorf("GroundItemView.Flavor = %q, want %q", got, want)
	}
}

// TestMonsterAggroRadiusExceedsCombatRadius pins the invariant documented on
// MonsterAggroRadius: a monster must notice a player before it can be close
// enough to bubble with them, or it would sit frozen just outside aggro
// range forever. protocol.go also carries a compile-time array-length guard
// for the same invariant; this test gives it a readable failure message.
func TestMonsterAggroRadiusExceedsCombatRadius(t *testing.T) {
	t.Parallel()

	if got, want := protocol.MonsterAggroRadius, protocol.CombatRadius; got <= want {
		t.Errorf("MonsterAggroRadius = %d, want > CombatRadius (%d)", got, want)
	}
}

// TestEntityMonsterKindRoundTripOnWire proves Entity.MonsterKind (milestone
// 6c) survives a JSON encode/decode cycle — a monster's kind registry id
// (empty for a player), alongside its kind-display Name.
func TestEntityMonsterKindRoundTripOnWire(t *testing.T) {
	t.Parallel()

	want := protocol.Entity{
		ID: 5, Kind: protocol.EntityMonster, Name: "Dragon", MonsterKind: "dragon", HP: 60, MaxHP: 60,
	}

	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal(Entity) = %v, want nil error", err)
	}

	var got protocol.Entity
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("json.Unmarshal(Entity) = %v, want nil error", err)
	}

	if got, want := got.Name, want.Name; got != want {
		t.Errorf("Entity.Name = %q, want %q", got, want)
	}

	if got, want := got.MonsterKind, want.MonsterKind; got != want {
		t.Errorf("Entity.MonsterKind = %q, want %q", got, want)
	}
}

// TestIntentRequestItemIDRoundTrip proves IntentEquip and IntentRequest's new
// ItemID field survive a JSON round trip — an equip intent names the item to
// equip, not a target hex.
func TestIntentRequestItemIDRoundTrip(t *testing.T) {
	t.Parallel()

	want := protocol.IntentRequest{
		EntityID: 1,
		Token:    "tok",
		Kind:     protocol.IntentEquip,
		ItemID:   7,
	}

	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal(IntentRequest) = %v, want nil error", err)
	}

	var got protocol.IntentRequest
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("json.Unmarshal(IntentRequest) = %v, want nil error", err)
	}

	if got, want := got.Kind, protocol.IntentEquip; got != want {
		t.Errorf("IntentRequest.Kind = %q, want %q", got, want)
	}

	if got, want := got.ItemID, want.ItemID; got != want {
		t.Errorf("IntentRequest.ItemID = %d, want %d", got, want)
	}
}

// TestHitViewRoundTripOnWire (#114) is the protocol contract test for the
// turn bundle's per-hit combat moments: a TurnEvent's Hits survive a JSON
// encode/decode cycle intact — the same encode the server performs over SSE
// and the same decode shape the generated TS client relies on.
func TestHitViewRoundTripOnWire(t *testing.T) {
	t.Parallel()

	want := protocol.TurnEvent{
		Turn: 7,
		Hits: []protocol.HitView{
			{Turn: 7, AttackerID: 1, VictimID: 2, Amount: 8, Crit: true},
			{Turn: 6, AttackerID: 2, VictimID: 1, Amount: 1, Glance: true},
		},
	}

	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal(TurnEvent) = %v, want nil error", err)
	}

	var got protocol.TurnEvent
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("json.Unmarshal(TurnEvent) = %v, want nil error", err)
	}

	if len(got.Hits) != len(want.Hits) {
		t.Fatalf("Hits length = %d, want %d", len(got.Hits), len(want.Hits))
	}

	for i := range want.Hits {
		if got, want := got.Hits[i], want.Hits[i]; got != want {
			t.Errorf("Hits[%d] = %+v, want %+v", i, got, want)
		}
	}
}
