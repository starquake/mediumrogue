package game_test

// starter_content_test.go: task 3 of the inventory-slots milestone — the
// starter armor/consumable content works through the LIVE pipeline
// (equivalence tests mirroring the species-card style): leather-armor's
// take-damage −1 (with applyRules' floor of 1), headband-of-learning's
// earn-XP ×1.05 stacking with the species fold, and the potion riding the
// rat/wolf drop tables (pinned white-box in monsters_test.go).

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// bumpDamageTaken places a fighter at origin (optionally wearing
// leather-armor), a monster of the given kind adjacent, resolves one turn
// (the adjacent monster bumps the nearest player), and returns how much HP
// the player lost to that bump.
func bumpDamageTaken(t *testing.T, kind string, wearArmor bool) int {
	t.Helper()

	w := newWorld()

	target := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, target) {
		t.Skip("origin is not walkable on this map")
	}

	id, token := w.PlaceEntityForTest(target)

	if wearArmor {
		instID := w.GrantItemForTest(id, "leather-armor")
		if err := w.SubmitIntent(protocol.IntentRequest{
			EntityID: id, Token: token, Kind: protocol.IntentEquip, ItemID: instID,
		}); err != nil {
			t.Fatalf("SubmitIntent equip leather-armor: %v", err)
		}

		if got, want := w.EquippedInSlotForTest(id, protocol.ItemTypeChest), instID; got != want {
			t.Fatalf("body slot = %d, want %d (armor equipped)", got, want)
		}
	}

	w.PlaceMonsterKindForTest(walkableNeighbor(t, w, target), kind)

	before := game.MaxHPForTest(protocol.ClassFighter, 1)
	snap := step(t, w)

	e, ok := entityOfSnap(snap, id)
	if !ok {
		t.Fatal("player missing from snapshot")
	}

	return before - e.HP
}

// TestLeatherArmorReducesDamageThroughLivePipeline: a wolf bump against a
// leather-armored fighter lands for exactly one less than against a bare
// one — the card's take-damage −1 folded at the live combat site, not a
// special case.
func TestLeatherArmorReducesDamageThroughLivePipeline(t *testing.T) {
	t.Parallel()

	bare := bumpDamageTaken(t, "wolf", false)
	if got, want := bare, game.MonsterDamageForTest("wolf"); got != want {
		t.Fatalf("bare fighter lost %d HP to a wolf bump, want %d", got, want)
	}

	armored := bumpDamageTaken(t, "wolf", true)
	if got, want := armored, bare-1; got != want {
		t.Errorf("armored fighter lost %d HP, want %d (take-damage -1)", got, want)
	}
}

// TestLeatherArmorFloorsAtOne: a rat bump (1 damage) against leather armor
// still lands for 1 — applyRules' take-damage clamp (a landed hit always
// costs at least 1), the card's "floor 1".
func TestLeatherArmorFloorsAtOne(t *testing.T) {
	t.Parallel()

	if got, want := game.MonsterDamageForTest("rat"), 1; got != want {
		t.Fatalf("rat damage = %d, want %d (this test's floor premise)", got, want)
	}

	if got, want := bumpDamageTaken(t, "rat", true), 1; got != want {
		t.Errorf("armored fighter lost %d HP to a rat bump, want %d (floor 1)", got, want)
	}
}

// TestHeadbandBoostsXPThroughLivePipeline: a kill award folds the equipped
// headband's earn-XP ×1.05 on top of the species passive (human ×1.5 here) —
// gear XP cards run at the same live award site as species cards.
func TestHeadbandBoostsXPThroughLivePipeline(t *testing.T) {
	t.Parallel()

	w := newWorld()

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	me := joinNamed(t, w, "scholar") // human fighter
	w.SetHexForTest(me.EntityID, center)

	instID := w.GrantItemForTest(me.EntityID, "headband-of-learning")
	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: me.EntityID, Token: me.Token, Kind: protocol.IntentEquip, ItemID: instID,
	}); err != nil {
		t.Fatalf("SubmitIntent equip headband: %v", err)
	}

	monsterHex := walkableNeighbor(t, w, center)
	monsterID := w.PlaceMonsterForTest(monsterHex)
	w.SetHPForTest(monsterID, 1)

	step(t, w) // form the bubble

	w.SetPathForTest(me.EntityID, []protocol.Hex{monsterHex})
	snap := step(t, w) // bump-kill inside the bubble

	if _, ok := entityOfSnap(snap, monsterID); ok {
		t.Fatal("monster should have died to the bump")
	}

	// Fold order (applyRules): no adds, then multipliers in card order —
	// species cards first, then equipped gear (earnXPCards): base * 1.5
	// (human), then * 1.05 (headband), integer math at each step.
	base := game.MonsterXPForTest("wolf")
	afterHuman := base * (100 + protocol.HumanXPBonusPercent) / 100
	want := afterHuman * 105 / 100

	e, ok := entityOfSnap(snap, me.EntityID)
	if !ok {
		t.Fatal("player missing from snapshot")
	}

	if got := e.XP; got != want {
		t.Errorf("XP after headband kill = %d, want %d (human x1.5 then headband x1.05)", got, want)
	}
}

// TestLeatherArmorEquipsForEveryClass: class gates are gone (gear keystone,
// #55/#56) — leather-armor, once a fighter/rogue-only wearability card, now
// equips through the real intent path for every class, including mage.
func TestLeatherArmorEquipsForEveryClass(t *testing.T) {
	t.Parallel()

	for _, class := range []string{protocol.ClassFighter, protocol.ClassRogue, protocol.ClassMage} {
		w := newWorld()

		me, err := w.Join("", "tester", class, protocol.SpeciesHuman)
		if err != nil {
			t.Fatalf("Join %s: %v", class, err)
		}

		instID := w.GrantItemForTest(me.EntityID, "leather-armor")

		if err := w.SubmitIntent(protocol.IntentRequest{
			EntityID: me.EntityID, Token: me.Token, Kind: protocol.IntentEquip, ItemID: instID,
		}); err != nil {
			t.Errorf("%s equip leather-armor = %v, want nil (gates dropped, #56)", class, err)
		}
	}
}
