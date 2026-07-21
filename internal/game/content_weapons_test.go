package game_test

// content_weapons_test.go (#267): the content-expansion weapons & gear — a
// martial Fire weapon, the players' first heavy 2H blunt, a reach bow, and
// the Blunt-resist gloves — proven through the LIVE combat pipeline (the
// damage_types_test.go / starter_content_test.go equivalence style), plus a
// registry pin for the bow's reach, which has no fuller live tell. The
// Frostward Charm's Ice resist needs an Ice attacker to prove, so its live
// test rides the Frost Wisp (content_monsters_test.go, #266).

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// gearDamageTaken places a fighter at origin (wearing gearID equipped into
// its own slot, or nothing if it is ""), a monster of the given kind
// adjacent, resolves one turn, and returns the HP the player lost to that
// melee attack. The slot-agnostic sibling of starter_content_test.go's
// chest-only meleeDamageTaken — the equip intent auto-routes armor/jewelry
// to its type's slot (slotForType).
func gearDamageTaken(t *testing.T, kind, gearID string) int {
	t.Helper()

	w := newWorld()

	origin := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, origin) {
		t.Skip("origin is not walkable on this map")
	}

	id, token := w.PlaceEntityForTest(origin)

	if gearID != "" {
		instID := w.GrantItemForTest(id, gearID)
		if err := w.SubmitIntent(equipIntent(id, token, instID)); err != nil {
			t.Fatalf("SubmitIntent equip %s: %v", gearID, err)
		}
	}

	w.PlaceMonsterKindForTest(walkableNeighbor(t, w, origin), kind)

	before := game.MaxHPForTest(protocol.ClassFighter, 1)

	return before - entityHP(t, step(t, w), id)
}

// TestEmberBrandDealsFireTrollFears: the Ember Brand is a real Fire weapon —
// Fire is no longer magic-only. The same swing lands harder on the
// fire-vulnerable troll than on the ghoul (which fears Holy, not Fire), the
// difference being the brand's 4 Fire damage taking the troll's +50%.
func TestEmberBrandDealsFireTrollFears(t *testing.T) {
	t.Parallel()

	// A 1H weapon joins the fighter's iron sword (both hands swing), so both
	// calls include the sword's Sharp 4; the victim's card is the only
	// variable. The troll takes +2 = the brand's 4 Fire damage ×1.5 − 4.
	onGhoul := damageDealtToKind(t, "ghoul", "ember-brand", 1)
	if got, want := damageDealtToKind(t, "troll", "ember-brand", 1), onGhoul+2; got != want {
		t.Errorf("ember brand vs troll dealt %d, want %d (its Fire +50%% on a fire-fearing troll)", got, want)
	}
}

// TestIronheadGreatmaulIsHeavyTwoHander: the 2H maul replaces the fighter's
// whole kit (both hands) and lands its full 9 Blunt in one swing — the
// players' first heavy 2H blunt. Blunt, so no ghoul vulnerability rides along.
func TestIronheadGreatmaulIsHeavyTwoHander(t *testing.T) {
	t.Parallel()

	if got, want := damageDealtToKind(t, "ghoul", "ironhead-greatmaul", 1), 9; got != want {
		t.Errorf("ironhead greatmaul vs ghoul dealt %d, want %d (2H replaces the kit; blunt, no vuln)", got, want)
	}
}

// TestLongbowOutreachesShortbow: the Longbow fires at 5 hexes — one past the
// Shortbow's 4 — for its 3 damage. {5,0} is the clear-LOS distance-5 hex the
// ranged tests use.
func TestLongbowOutreachesShortbow(t *testing.T) {
	t.Parallel()

	w := newWorld()

	origin := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, origin) {
		t.Skip("origin is not walkable on this map")
	}

	id, token := w.PlaceEntityForTest(origin)

	instID := w.GrantItemForTest(id, "longbow")
	if err := w.SubmitIntent(equipIntent(id, token, instID)); err != nil {
		t.Fatalf("SubmitIntent equip longbow: %v", err)
	}

	monsterHex := protocol.Hex{Q: 5, R: 0}
	monsterID := w.PlaceMonsterKindForTest(monsterHex, "ghoul")

	if err := w.SubmitIntent(entityAttackIntent(id, token, monsterID)); err != nil {
		t.Fatalf("SubmitIntent attack: %v", err)
	}

	got := game.MonsterMaxHPForTest("ghoul") - entityHP(t, step(t, w), monsterID)
	if want := 3; got != want {
		t.Errorf("longbow at 5 hexes dealt %d, want %d (out-ranges the shortbow's 4)", got, want)
	}
}

// TestLongbowReachPinned: the reach (6, the max validateMaxReach allows) has
// no fuller live tell than TestLongbowOutreachesShortbow, so pin the registry
// stats here — the reach-for-damage tradeoff is 3 damage at 6 hexes.
func TestLongbowReachPinned(t *testing.T) {
	t.Parallel()

	if got, want := game.ItemDamageForTest("longbow"), 3; got != want {
		t.Errorf("longbow damage = %d, want %d", got, want)
	}

	if got, want := game.ItemRangeForTest("longbow"), 6; got != want {
		t.Errorf("longbow rangeHex = %d, want %d", got, want)
	}
}

// TestIronboundGauntletsHalveBlunt: a troll's Blunt maul lands for half
// against the Ironbound Gauntlets — the Blunt mirror of the Warded Gambeson's
// Sharp resist, folded at the live take-damage site.
func TestIronboundGauntletsHalveBlunt(t *testing.T) {
	t.Parallel()

	bare := gearDamageTaken(t, "troll", "")
	if got, want := bare, game.MonsterDamageForTest("troll"); got != want {
		t.Fatalf("bare fighter lost %d HP to a troll maul, want %d", got, want)
	}

	if got, want := gearDamageTaken(t, "troll", "ironbound-gauntlets"), bare/2; got != want {
		t.Errorf("blunt-resisted fighter lost %d HP, want %d (half)", got, want)
	}
}

// TestIronboundGauntletsAreTypeGated: the Blunt resist is inert against a
// wolf's Sharp fangs — the negative control every resist card needs, so a
// card that forgot its condition would fail here even as the halving test
// passes.
func TestIronboundGauntletsAreTypeGated(t *testing.T) {
	t.Parallel()

	if got, want := gearDamageTaken(t, "wolf", "ironbound-gauntlets"), gearDamageTaken(t, "wolf", ""); got != want {
		t.Errorf("blunt-resisted fighter lost %d HP to SHARP fangs, want %d (unchanged)", got, want)
	}
}
