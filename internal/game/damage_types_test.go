package game_test

// damage_types_test.go (#92, DT1): the damage-type system through the LIVE
// pipeline — the opposition-convention vulnerability cards on monster kinds
// (a Chaos ghoul fears Holy, a troll fears Fire), measured as real HP lost in
// a real turn rather than as a white-box fold.

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// damageDealtToKind equips weaponID on a player at origin, places a monster
// of the given kind dist hexes away, fires one attack at it, and returns the
// HP that monster lost. dist 1 is a melee swing; anything further needs a
// ranged/magic weapon whose reach covers it.
func damageDealtToKind(t *testing.T, kind, weaponID string, dist int) int {
	t.Helper()

	w := newWorld()

	origin := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, origin) {
		t.Skip("origin is not walkable on this map")
	}

	id, token := w.PlaceEntityForTest(origin)

	instID := w.GrantItemForTest(id, weaponID)
	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentEquip, ItemID: instID,
	}); err != nil {
		t.Fatalf("SubmitIntent equip %s: %v", weaponID, err)
	}

	at := walkableHexAtDistance(t, w, origin, dist, dist)
	monsterID := w.PlaceMonsterKindForTest(at, kind)

	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentAttack, TargetEntityID: monsterID,
	}); err != nil {
		t.Fatalf("SubmitIntent attack: %v", err)
	}

	return game.MonsterMaxHPForTest(kind) - entityHP(t, step(t, w), monsterID)
}

// TestTrollFearsFire (#92): the identity the whole damage-type arc was
// pitched on. The SAME fire weapon lands for half again as much on a troll as
// on a ghoul, which carries no fire vulnerability — one variable, the
// victim's card.
func TestTrollFearsFire(t *testing.T) {
	t.Parallel()

	const atRange = 2 // not adjacent: keeps the staff's adjacency card out of it

	neutral := damageDealtToKind(t, "ghoul", "ember-staff", atRange)
	if got, want := neutral, 6; got != want {
		t.Fatalf("ember staff vs ghoul dealt %d, want %d (its base damage, no vulnerability)", got, want)
	}

	if got, want := damageDealtToKind(t, "troll", "ember-staff", atRange), neutral*3/2; got != want {
		t.Errorf("ember staff vs troll dealt %d, want %d (fire vulnerability +50%%)", got, want)
	}
}

// TestGhoulFearsHoly (#92): the opposition convention in the other direction
// — a Chaos-aligned monster takes extra from Holy, and the Wyrmslayer
// Greatsword (the game's only Holy weapon) is what proves it. Nothing in the
// engine knows Holy and Chaos are paired; it is two cards someone wrote.
func TestGhoulFearsHoly(t *testing.T) {
	t.Parallel()

	const adjacent = 1 // the Wyrmslayer is a melee two-hander

	neutral := damageDealtToKind(t, "troll", "wyrmslayer-greatsword", adjacent)
	if got, want := neutral, 9; got != want {
		t.Fatalf("wyrmslayer vs troll dealt %d, want %d (its base damage, no vulnerability)", got, want)
	}

	if got, want := damageDealtToKind(t, "ghoul", "wyrmslayer-greatsword", adjacent), neutral*3/2; got != want {
		t.Errorf("wyrmslayer vs ghoul dealt %d, want %d (holy vulnerability +50%%)", got, want)
	}
}

// TestVulnerabilityIsTypeGatedNotUniversal (#92): the troll's card is a FIRE
// vulnerability, not a damage bonus. The same BLUNT swing lands for exactly
// the same amount on the fire-fearing troll as on the ghoul, which fears
// Holy — so neither card fires, and a card that simply forgot its condition
// would fail here even though it would sail through the two tests above.
func TestVulnerabilityIsTypeGatedNotUniversal(t *testing.T) {
	t.Parallel()

	const adjacent = 1

	// A one-handed weapon joins the fighter kit's iron sword rather than
	// replacing it, so both hands swing; the total is the same either way,
	// and the victim's card stays the only variable between the two calls.
	onGhoul := damageDealtToKind(t, "ghoul", "iron-warhammer", adjacent)
	if got, want := damageDealtToKind(t, "troll", "iron-warhammer", adjacent), onGhoul; got != want {
		t.Errorf("blunt swing vs troll dealt %d, want %d (same as vs ghoul — neither card is a blunt card)", got, want)
	}
}
