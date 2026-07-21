package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// offensive_gear_test.go: the lifesteal + crit-jewelry slice (#271), black-box
// over the real World. It proves the two mechanisms through the LIVE combat
// pipeline (not a white-box fold): the Vampiric Blade heals its wielder when a
// hit lands, respects the max-HP clamp, and never rescues an attacker from a
// mutual kill; and the Ring of Precision's crit% card actually reaches the
// attacker's deal-damage roll (attackerGearCards) — the wiring gap that would
// otherwise make offensive jewelry a silent no-op.

// equipVampiricBlade grants and equips the Vampiric Blade on a placed entity,
// applied in the same resolution the caller then drives (equip is free outside
// a bubble). The entity keeps its default iron sword too, so it dual-wields —
// which is the point of the per-weapon lifesteal assertion below.
func equipVampiricBlade(t *testing.T, w *game.World, id int64, token string) {
	t.Helper()

	inst := w.GrantItemForTest(id, "vampiric-blade")
	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentEquip, ItemID: inst,
	}); err != nil {
		t.Fatalf("SubmitIntent(equip vampiric-blade): %v", err)
	}
}

// TestVampiricBladeHealsWielderOnHit is the live lifesteal proof: a wielder
// below max HP heals for 25% of the damage the Vampiric Blade deals. The
// wielder dual-wields its default iron sword (4) and the Vampiric Blade (4), so
// the wolf loses 8 — but ONLY the blade's hit carries the lifesteal rider, so
// the heal is 25% of 4 = 1, not 25% of 8. Lifesteal is per-weapon (the rider is
// on the card, folded per-hit), never a global cut of the turn's damage.
func TestVampiricBladeHealsWielderOnHit(t *testing.T) {
	t.Parallel()

	w := newWorld()

	origin := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, origin) {
		t.Skip("origin is not walkable on this map")
	}

	id, token := w.PlaceEntityForTest(origin)
	equipVampiricBlade(t, w, id, token)

	maxHP := game.MaxHPForTest(protocol.ClassFighter, 1)

	const below = 10

	w.SetHPForTest(id, maxHP-below)

	wolfHex := walkableNeighbor(t, w, origin)
	wolfID := w.PlaceMonsterKindForTest(wolfHex, "wolf")
	w.SetHPForTest(wolfID, game.MonsterMaxHPForTest("wolf"))

	if err := w.SubmitIntent(entityAttackIntent(id, token, wolfID)); err != nil {
		t.Fatalf("SubmitIntent(attack): %v", err)
	}

	w.ResolveCombatOnlyForTest()

	// The blade deals 4; 25% lifesteal heals exactly 1.
	if got, want := w.HPForTest(id), maxHP-below+1; got != want {
		t.Errorf("wielder HP after lifesteal hit = %d, want %d (healed 25%% of the blade's 4)", got, want)
	}
}

// TestVampiricBladeLifestealClampsAtMaxHP: a full-HP wielder does not overheal —
// the leech is clamped to max HP, exactly as the end-of-turn regen tick is.
func TestVampiricBladeLifestealClampsAtMaxHP(t *testing.T) {
	t.Parallel()

	w := newWorld()

	origin := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, origin) {
		t.Skip("origin is not walkable on this map")
	}

	id, token := w.PlaceEntityForTest(origin)
	equipVampiricBlade(t, w, id, token)

	maxHP := game.MaxHPForTest(protocol.ClassFighter, 1)
	w.SetHPForTest(id, maxHP)

	wolfHex := walkableNeighbor(t, w, origin)
	wolfID := w.PlaceMonsterKindForTest(wolfHex, "wolf")
	w.SetHPForTest(wolfID, game.MonsterMaxHPForTest("wolf"))

	if err := w.SubmitIntent(entityAttackIntent(id, token, wolfID)); err != nil {
		t.Fatalf("SubmitIntent(attack): %v", err)
	}

	w.ResolveCombatOnlyForTest()

	if got, want := w.HPForTest(id), maxHP; got != want {
		t.Errorf("full-HP wielder HP after lifesteal hit = %d, want %d (no overheal past max)", got, want)
	}
}

// TestLifestealDoesNotRescueFromMutualKill pins the simultaneity guarantee: the
// leech lands with the turn's damage, not before it, so an attacker whom the
// same turn's damage kills does NOT heal back. The wielder (HP set to the wolf's
// exact damage) both strikes the wolf and is struck for lethal in one turn; it
// dies and respawns at full HP. A heal-before-death bug would instead leave it
// clinging at 1 HP. The wolf ends at 2 (it took both the iron sword's 4 and the
// blade's 4), proving the wielder's hits landed — so a live leech source existed
// and was correctly withheld from a corpse.
func TestLifestealDoesNotRescueFromMutualKill(t *testing.T) {
	t.Parallel()

	w := newWorld()

	origin := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, origin) {
		t.Skip("origin is not walkable on this map")
	}

	id, token := w.PlaceEntityForTest(origin)
	equipVampiricBlade(t, w, id, token)

	wolfDamage := game.MonsterDamageForTest("wolf")
	w.SetHPForTest(id, wolfDamage) // one wolf hit is exactly lethal

	wolfHex := walkableNeighbor(t, w, origin)
	wolfID := w.PlaceMonsterKindForTest(wolfHex, "wolf")
	w.SetHPForTest(wolfID, game.MonsterMaxHPForTest("wolf"))

	if err := w.SubmitIntent(entityAttackIntent(id, token, wolfID)); err != nil {
		t.Fatalf("SubmitIntent(attack): %v", err)
	}

	w.SetPathForTest(wolfID, []protocol.Hex{origin}) // wolf walks into melee, striking back

	w.ResolveCombatOnlyForTest()

	// Died and respawned as a fresh full-HP body — lifesteal did not keep it at 1.
	if got, want := w.HPForTest(id), game.MaxHPForTest(protocol.ClassFighter, 1); got != want {
		t.Errorf("wielder HP after mutual kill = %d, want %d (respawned; leech must not rescue a corpse)", got, want)
	}

	// The wielder's hits landed (8 dealt), so the leech source was real.
	if got, want := w.HPForTest(wolfID), game.MonsterMaxHPForTest("wolf")-8; got != want {
		t.Errorf("wolf HP after mutual kill = %d, want %d (took iron sword 4 + blade 4)", got, want)
	}
}

// Seeds pinned so a human Fighter's iron-sword swing WITH the Ring of Precision
// equipped does / does not proc the ring's 10% crit card — found by scanning
// seeds 0-39 during implementation (the hits_test.go critProcSeed pattern). If a
// change reorders rng consumption these move: re-derive by re-scanning, never by
// weakening the crit/amount assertions.
const (
	ringCritSeed = 20 // verified: iron-sword swing (base 4) resolves loss=8 (crit)
	ringMissSeed = 0  // verified: iron-sword swing (base 4) resolves loss=4 (plain hit)
)

// ringScenario drives one human Fighter melee swing into a wolf with the Ring of
// Precision equipped, under seed, and returns the recorded hit plus the wolf's
// HP loss. The Fighter keeps only its default iron sword (4), so a plain hit is
// 4 and a crit is 8 — the ring's crit% card reaching the deal-damage roll
// (attackerGearCards) is the whole thing under test.
func ringScenario(t *testing.T, seed int64) (protocol.HitView, int) {
	t.Helper()

	w := newWorld()
	w.SetSeedForTest(seed)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	inst := w.GrantItemForTest(me.EntityID, "ring-of-precision")
	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: me.EntityID, Token: me.Token, Kind: protocol.IntentEquip, ItemID: inst,
	}); err != nil {
		t.Fatalf("SubmitIntent(equip ring): %v", err)
	}

	wolfHex := walkableNeighbor(t, w, me.Hex)
	wolfID := w.PlaceMonsterKindForTest(wolfHex, "wolf")
	w.SetHPForTest(wolfID, game.MonsterMaxHPForTest("wolf"))
	w.SetAttackTargetEntityForTest(me.EntityID, wolfID)
	w.ResolveCombatOnlyForTest()

	return hitOn(t, w.Snapshot(), wolfID), game.MonsterMaxHPForTest("wolf") - w.HPForTest(wolfID)
}

// TestRingOfPrecisionCritReachesDealDamage is the crit-jewelry proof: the ring's
// deal-damage crit card is folded into the attacker's roll (attackerGearCards),
// so on a crit seed the swing doubles and the hit is flagged a crit, and on a
// miss seed it is a plain hit. Without the attacker-side jewelry fold the ring
// would be a silent no-op — a plain 4 every time, whatever the seed.
func TestRingOfPrecisionCritReachesDealDamage(t *testing.T) {
	t.Parallel()

	plain, plainLoss := ringScenario(t, ringMissSeed)
	if plain.Crit {
		t.Errorf("miss-seed hit flagged crit, want a plain hit")
	}

	if got, want := plainLoss, 4; got != want {
		t.Errorf("plain swing loss = %d, want %d (base iron-sword damage)", got, want)
	}

	crit, critLoss := ringScenario(t, ringCritSeed)
	if !crit.Crit {
		t.Errorf("crit-seed hit not flagged crit, want a crit (the ring's card fired)")
	}

	if got, want := critLoss, plainLoss*2; got != want {
		t.Errorf("crit swing loss = %d, want %d (ring doubles the hit)", got, want)
	}
}
