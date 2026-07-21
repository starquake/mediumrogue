package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// effects_test.go: the timed-effect foundation (#271, slice 1). Black-box over
// the real World: application/refresh, the end-of-turn tick (DoT drain + regen
// heal — the two evEndOfTurn directions), the buff fold into deal-damage, and
// the two live on-hit proof consumers (the Serpent's poison bite, the Bloodrage
// Cleaver's self-buff). Ids are string literals, the black-box convention.

const (
	effectPoison = "poison"
	effectFrenzy = "frenzy"
	effectRegen  = "regen"
	effectWard   = "ward" // #271, slice 2: the timed defensive buff (take-damage mulPct).
)

// TestApplyTimedEffectRefreshesNotStacks pins the stacking decision (#271):
// a second application of the SAME effect def refreshes the existing one —
// overwriting magnitude and resetting the timer — rather than stacking a second
// copy. The ARPG rationale is in design-decisions.md: percentages ADD within a
// fold, so N copies would compound toward the runaway scaling the flat-power
// curve refuses; refresh keeps each source bounded.
func TestApplyTimedEffectRefreshesNotStacks(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id, _ := w.PlaceEntityForTest(protocol.Hex{Q: 1, R: 0})

	w.ApplyEffectForTest(id, effectPoison, -2, 3)
	w.ApplyEffectForTest(id, effectPoison, -5, 2) // same def again: refresh, not a second copy

	if got, want := w.EffectCountForTest(id), 1; got != want {
		t.Errorf("effect count after re-apply = %d, want %d (refresh, not stack)", got, want)
	}

	mag, turns, ok := w.EffectForTest(id, effectPoison)
	if !ok {
		t.Fatal("poison effect missing after re-apply")
	}

	if got, want := mag, -5; got != want {
		t.Errorf("refreshed magnitude = %d, want %d (latest application wins)", got, want)
	}

	if got, want := turns, 2; got != want {
		t.Errorf("refreshed turnsRemaining = %d, want %d (timer reset)", got, want)
	}
}

// TestTickDrainsAndExpiresDoT: a DoT (a negative effAdd at evEndOfTurn) drains
// its magnitude each end-of-turn tick for exactly turnsRemaining ticks, then
// expires. Two turns of -2 drains 4 total, and the third tick is a no-op.
func TestTickDrainsAndExpiresDoT(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id, _ := w.PlaceEntityForTest(protocol.Hex{Q: 1, R: 0})

	const start = 20

	w.SetHPForTest(id, start)
	w.ApplyEffectForTest(id, effectPoison, -2, 2)

	w.TickEffectsForTest()

	if got, want := w.HPForTest(id), start-2; got != want {
		t.Errorf("hp after first tick = %d, want %d (drained 2)", got, want)
	}

	if _, turns, _ := w.EffectForTest(id, effectPoison); turns != 1 {
		t.Errorf("turnsRemaining after first tick = %d, want 1", turns)
	}

	w.TickEffectsForTest()

	if got, want := w.HPForTest(id), start-4; got != want {
		t.Errorf("hp after second tick = %d, want %d (drained 2 more)", got, want)
	}

	if got, want := w.EffectCountForTest(id), 0; got != want {
		t.Errorf("effect count after expiry = %d, want %d (expired at 0 turns)", got, want)
	}

	w.TickEffectsForTest()

	if got, want := w.HPForTest(id), start-4; got != want {
		t.Errorf("hp after expiry tick = %d, want %d (no further drain)", got, want)
	}
}

// TestTickRegenHealsCappedAtMaxHP is the SECOND evEndOfTurn consumer, proving
// the event folds the HEAL direction too (a POSITIVE effAdd). The content
// trigger that applies a regen (a regen potion / regenerating monster) is a
// later #271 slice, so this white-box exercise is the documented second
// consumer of the new event (new-pipeline-kind's two-consumer gate).
func TestTickRegenHealsCappedAtMaxHP(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id, _ := w.PlaceEntityForTest(protocol.Hex{Q: 1, R: 0})

	maxHP := game.MaxHPForTest(protocol.ClassFighter, 1)
	w.SetHPForTest(id, maxHP-5)
	w.ApplyEffectForTest(id, effectRegen, 3, 5)

	w.TickEffectsForTest()

	if got, want := w.HPForTest(id), maxHP-2; got != want {
		t.Errorf("hp after regen tick = %d, want %d (healed 3)", got, want)
	}

	w.TickEffectsForTest() // would heal 3 more, but only 2 to max — no overheal

	if got, want := w.HPForTest(id), maxHP; got != want {
		t.Errorf("hp after second regen tick = %d, want %d (capped at max)", got, want)
	}
}

// TestActiveBuffFoldsIntoDealDamage proves a timed buff is nothing but a rule
// card that is active for N turns: a frenzy effect (a deal-damage mulPct) folds
// into the pipeline exactly like a gear card. The SAME iron-sword swing lands
// for half again as much while the buff is active — one variable, the effect.
func TestActiveBuffFoldsIntoDealDamage(t *testing.T) {
	t.Parallel()

	base := damageDealtByBuffedFighter(t, 0)    // percentBase*0 => no buff applied
	buffed := damageDealtByBuffedFighter(t, 50) // +50%

	if got, want := base, game.ItemDamageForTest("iron-sword"); got != want {
		t.Fatalf("unbuffed iron-sword dealt %d, want %d", got, want)
	}

	if got, want := buffed, base*3/2; got != want {
		t.Errorf("buffed iron-sword dealt %d, want %d (+50%% frenzy fold)", got, want)
	}
}

// damageDealtByBuffedFighter stages a fighter adjacent to a wolf, optionally
// applies a frenzy buff of +bonusPct, resolves one attack, and returns the
// damage dealt to the wolf. bonusPct 0 applies no buff.
func damageDealtByBuffedFighter(t *testing.T, bonusPct int) int {
	t.Helper()

	w := newWorld()

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	wolfHex := walkableNeighbor(t, w, me.Hex)
	wolfID := w.PlaceMonsterKindForTest(wolfHex, "wolf")
	w.SetHPForTest(wolfID, game.MonsterMaxHPForTest("wolf"))

	if bonusPct > 0 {
		// magnitude is the effMulPct n — percentBase + the bonus.
		w.ApplyEffectForTest(me.EntityID, effectFrenzy, 100+bonusPct, 5)
	}

	if err := w.SubmitIntent(entityAttackIntent(me.EntityID, me.Token, wolfID)); err != nil {
		t.Fatalf("SubmitIntent(attack): %v", err)
	}

	w.ResolveCombatOnlyForTest()

	return game.MonsterMaxHPForTest("wolf") - w.HPForTest(wolfID)
}

// TestSerpentBiteAppliesPoisonOnHit is the live DoT proof consumer: the Serpent
// (idKindSerpent) bites a player and the poison timed effect lands on the
// victim, ready to drain from next turn. Application is AFTER the tick, so the
// bite turn deals only the bite — the drain starts next turn, at full duration.
func TestSerpentBiteAppliesPoisonOnHit(t *testing.T) {
	t.Parallel()

	w := newWorld()

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	full := protocol.FighterMaxHP
	w.SetHPForTest(me.EntityID, full)

	serpentHex := walkableNeighbor(t, w, me.Hex)
	serpentID := w.PlaceMonsterKindForTest(serpentHex, "serpent")

	// Drive the serpent's bite by walking it into the player's hex (the
	// walk-into-melee path), mirroring TestMeleeMutualKill.
	w.SetPathForTest(serpentID, []protocol.Hex{me.Hex})
	w.ResolveCombatOnlyForTest()

	mag, turns, ok := w.EffectForTest(me.EntityID, effectPoison)
	if !ok {
		t.Fatal("serpent bite did not apply a poison effect to the player")
	}

	if got, want := mag, -2; got != want {
		t.Errorf("poison magnitude = %d, want %d", got, want)
	}

	if got, want := turns, 3; got != want {
		t.Errorf("poison turnsRemaining = %d, want %d (full duration, no tick on the bite turn)", got, want)
	}

	// The bite dealt its melee damage this turn; the poison has not drained yet.
	if got, want := w.HPForTest(me.EntityID), full-game.MonsterDamageForTest("serpent"); got != want {
		t.Errorf("hp after bite = %d, want %d (melee only; poison drains next turn)", got, want)
	}
}

// TestBloodrageCleaverSelfBuffsOnHit is the live BUFF proof consumer: a player
// wielding the Bloodrage Cleaver gains the frenzy self-buff on a landed hit
// (toSelf application), refreshed each swing — the ARPG "rage stacks" idiom as
// pure content.
func TestBloodrageCleaverSelfBuffsOnHit(t *testing.T) {
	t.Parallel()

	w := newWorld()

	origin := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, origin) {
		t.Skip("origin is not walkable on this map")
	}

	id, token := w.PlaceEntityForTest(origin)

	instID := w.GrantItemForTest(id, "bloodrage-cleaver")
	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentEquip, ItemID: instID,
	}); err != nil {
		t.Fatalf("SubmitIntent(equip): %v", err)
	}

	wolfHex := walkableNeighbor(t, w, origin)
	wolfID := w.PlaceMonsterKindForTest(wolfHex, "wolf")
	w.SetHPForTest(wolfID, game.MonsterMaxHPForTest("wolf"))

	if err := w.SubmitIntent(entityAttackIntent(id, token, wolfID)); err != nil {
		t.Fatalf("SubmitIntent(attack): %v", err)
	}

	w.ResolveCombatOnlyForTest()

	mag, turns, ok := w.EffectForTest(id, effectFrenzy)
	if !ok {
		t.Fatal("Bloodrage Cleaver hit did not self-apply a frenzy effect")
	}

	if got, want := mag, 115; got != want {
		t.Errorf("frenzy magnitude = %d, want %d (+15%% deal-damage)", got, want)
	}

	if got, want := turns, 2; got != want {
		t.Errorf("frenzy turnsRemaining = %d, want %d (full duration; applied after this turn's tick)", got, want)
	}
}

// TestHydraBiteSelfAppliesRegenOnHit is the live REGEN proof consumer (#271,
// slice 2): the Hydra's bite (idHydraFangs, toSelf) self-applies a regen effect,
// so the monster knits itself back together as it fights — the second real
// evEndOfTurn consumer the foundation left as a white-box test. Application is
// AFTER the tick (applyPendingOnHitLocked), so the bite turn only lands the
// effect; the heal starts next turn, at full duration — exactly mirroring the
// Serpent's poison timing but in the heal direction. A follow-up tick proves the
// heal actually restores HP.
func TestHydraBiteSelfAppliesRegenOnHit(t *testing.T) {
	t.Parallel()

	w := newWorld()

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.SetHPForTest(me.EntityID, protocol.FighterMaxHP)

	hydraHex := walkableNeighbor(t, w, me.Hex)
	hydraID := w.PlaceMonsterKindForTest(hydraHex, "hydra")

	const hurtBy = 10

	wounded := game.MonsterMaxHPForTest("hydra") - hurtBy
	w.SetHPForTest(hydraID, wounded)

	// Drive the hydra's bite by walking it into the player's hex (the
	// walk-into-melee path), mirroring TestSerpentBiteAppliesPoisonOnHit.
	w.SetPathForTest(hydraID, []protocol.Hex{me.Hex})
	w.ResolveCombatOnlyForTest()

	mag, turns, ok := w.EffectForTest(hydraID, effectRegen)
	if !ok {
		t.Fatal("hydra bite did not self-apply a regen effect")
	}

	if got, want := mag, 3; got != want {
		t.Errorf("regen magnitude = %d, want %d", got, want)
	}

	if got, want := turns, 3; got != want {
		t.Errorf("regen turnsRemaining = %d, want %d (full duration; applied after this turn's tick)", got, want)
	}

	// The bite dealt no self-heal yet (application is after the tick), so the
	// hydra is still exactly wounded.
	if got, want := w.HPForTest(hydraID), wounded; got != want {
		t.Errorf("hydra hp after its own bite = %d, want %d (regen heals next turn, not this one)", got, want)
	}

	// Next end-of-turn tick: the regen restores its magnitude.
	w.TickEffectsForTest()

	if got, want := w.HPForTest(hydraID), wounded+3; got != want {
		t.Errorf("hydra hp after regen tick = %d, want %d (healed 3)", got, want)
	}
}

// TestRespawnClearsEffects: a player killed by any means respawns as a fresh
// body — lingering poison/buff effects do not carry across the death.
func TestRespawnClearsEffects(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(4)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.ApplyEffectForTest(me.EntityID, effectPoison, -2, 3)

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)
	w.SetHPForTest(me.EntityID, game.MonsterDamageForTest("wolf")) // one hit is lethal

	w.SetPathForTest(monsterID, []protocol.Hex{me.Hex})
	w.ResolveCombatOnlyForTest()

	if got, want := w.EffectCountForTest(me.EntityID), 0; got != want {
		t.Errorf("effect count after respawn = %d, want %d (a respawn is a fresh body)", got, want)
	}
}

// TestSnapshotRoundTripsEffects: active timed effects survive a marshal/restore
// (snapshotVersion 9). A monster mid-poison stays poisoned across a restart.
func TestSnapshotRoundTripsEffects(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id := w.PlaceMonsterForTest(protocol.Hex{Q: 2, R: 0})
	w.ApplyEffectForTest(id, effectPoison, -2, 3)

	data, err := w.MarshalState()
	if err != nil {
		t.Fatalf("MarshalState: %v", err)
	}

	restored := newWorld()
	if err := restored.RestoreState(data); err != nil {
		t.Fatalf("RestoreState: %v", err)
	}

	mag, turns, ok := restored.EffectForTest(id, effectPoison)
	if !ok {
		t.Fatal("poison effect lost across snapshot round-trip")
	}

	if got, want := mag, -2; got != want {
		t.Errorf("restored magnitude = %d, want %d", got, want)
	}

	if got, want := turns, 3; got != want {
		t.Errorf("restored turnsRemaining = %d, want %d", got, want)
	}
}
