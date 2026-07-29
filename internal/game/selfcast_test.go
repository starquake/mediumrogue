package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// selfcast_test.go (#300): the self-effect active kind, end to end.
//
// Evade proved a targeted active works. These prove the OTHER half of the
// descriptor: a kind with no target at all, whose whole behaviour is a call
// into the timed-effect machinery the potions already use.

const (
	skillSecondWindID = "second-wind"
	skillBulwarkID    = "bulwark"
	// skillHardyID is Bulwark's prerequisite (Survival tree).
	skillHardyID = "hardy"
)

// selfCaster seeds a player who has learned one of the self-cast actives, plus
// the prerequisites it hangs behind.
func selfCaster(t *testing.T, w *game.World, learned ...string) (int64, string) {
	t.Helper()

	resp, err := w.Join("", "caster", protocol.ClassRogue, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.SetSkillStateForTest(resp.EntityID, learned, 0, 1)

	return resp.EntityID, resp.Token
}

// TestSecondWindNeedsNoTargetAndAppliesRegen: the point of the whole slice. A
// self-cast is submitted with NO target hex — the zero value — and must be
// accepted anyway, because its kind says it has nothing to aim at.
func TestSecondWindNeedsNoTargetAndAppliesRegen(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id, token := selfCaster(t, w, skillSurvivalistID, skillSecondWindID)

	w.SetHexForTest(id, protocol.Hex{Q: 0, R: 0})

	// No Target field: a self-cast that demanded one would be a worse version
	// of pressing the button.
	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
		SkillID: skillSecondWindID,
	}); err != nil {
		t.Fatalf("SubmitIntent use-skill: %v", err)
	}

	step(t, w)

	mag, turns, ok := w.EffectForTest(id, effectRegen)
	if !ok {
		t.Fatal("second wind applied no regeneration")
	}

	if got, want := mag, 3; got != want {
		t.Errorf("regen magnitude = %d, want %d", got, want)
	}

	if got, want := turns, 3; got != want {
		t.Errorf("regen turns = %d, want %d", got, want)
	}

	if got := w.ActiveReadyTurnForTest(id, skillSecondWindID); got == 0 {
		t.Error("second wind did not start its cooldown")
	}
}

// TestSecondWindLeavesTheCasterWhereTheyStand: a self-cast shares the
// reposition kind's resolution loop, and the submitted target for one is the
// ZERO hex. If the loop ever stopped branching on kind, this is the failure it
// would produce — a heal that teleports you to the world origin.
func TestSecondWindLeavesTheCasterWhereTheyStand(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id, token := selfCaster(t, w, skillSurvivalistID, skillSecondWindID)

	start := walkableHexAtDistance(t, w, protocol.Hex{Q: 0, R: 0}, 3, 4)
	w.SetHexForTest(id, start)

	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
		SkillID: skillSecondWindID,
	}); err != nil {
		t.Fatalf("SubmitIntent use-skill: %v", err)
	}

	snap := step(t, w)

	if got, want := entityHexIn(t, snap, id), start; got != want {
		t.Errorf("caster at %v after a self-cast, want %v — it must not move", got, want)
	}
}

// TestBulwarkAppliesWardAndSurvivesACleanse: Ward is beneficial, so the
// antidote rule must leave it alone. That behaviour is INHERITED from the
// effect row rather than written for the skill, which is exactly why it is
// worth pinning — nothing in skills.go mentions cleansing.
func TestBulwarkAppliesWardAndSurvivesACleanse(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id, token := selfCaster(t, w, skillSurvivalistID, skillHardyID, skillBulwarkID)

	w.SetHexForTest(id, protocol.Hex{Q: 0, R: 0})

	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
		SkillID: skillBulwarkID,
	}); err != nil {
		t.Fatalf("SubmitIntent use-skill: %v", err)
	}

	step(t, w)

	mag, turns, ok := w.EffectForTest(id, effectWard)
	if !ok {
		t.Fatal("bulwark applied no ward")
	}

	// The Warding Tonic's own numbers, deliberately: a skill that outperformed
	// the consumable would make the consumable dead content.
	if got, want := mag, 75; got != want {
		t.Errorf("ward magnitude = %d, want %d (percentBase-25)", got, want)
	}

	if got, want := turns, 4; got != want {
		t.Errorf("ward turns = %d, want %d", got, want)
	}

	// A real cleanse, through the real drink path — the same one that strips a
	// poison. A ward is not a problem to be cured.
	stackID := w.GrantItemForTest(id, "antivenom")
	if err := w.SubmitIntent(intentFor(id, token, protocol.IntentDrink, stackID)); err != nil {
		t.Fatalf("SubmitIntent drink antivenom: %v", err)
	}

	if _, _, ok := w.EffectForTest(id, effectWard); !ok {
		t.Error("a cleanse stripped the ward — cleansing is harmful-only")
	}
}

// TestSelfCastIsRejectedOnCooldown: the cost is real for a self-cast too, and
// it is refused at submit time rather than dropped silently at resolution.
func TestSelfCastIsRejectedOnCooldown(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id, token := selfCaster(t, w, skillSurvivalistID, skillSecondWindID)

	w.SetHexForTest(id, protocol.Hex{Q: 0, R: 0})

	use := protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
		SkillID: skillSecondWindID,
	}

	if err := w.SubmitIntent(use); err != nil {
		t.Fatalf("SubmitIntent use-skill: %v", err)
	}

	step(t, w)

	if err := w.SubmitIntent(use); err == nil {
		t.Fatal("a second self-cast inside the cooldown was accepted")
	}
}

// --- Expose: the entity-aimed kind (#300) ----------------------------------

const (
	skillExposeID = "expose"
	// effectVulnerable is Ward's mirror (#300) — an ABOVE-100 take-damage
	// multiplier, marked harmful so a cleanse strips it.
	effectVulnerable = "vulnerable"
)

// TestExposeAppliesVulnerableToTheNamedVictim: the entity aim, end to end. The
// intent names a victim by id — no hex anywhere — and the debuff lands on that
// entity, not on the caster.
func TestExposeAppliesVulnerableToTheNamedVictim(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id, token := selfCaster(t, w, skillCombatTrainingID, skillWeakSpotID, skillExposeID)

	w.SetHexForTest(id, protocol.Hex{Q: 0, R: 0})

	victimHex := walkableHexAtDistance(t, w, protocol.Hex{Q: 0, R: 0}, 2, 3)
	clearSightLine(t, w, protocol.Hex{Q: 0, R: 0}, victimHex)
	victim := w.PlaceMonsterForTest(victimHex)

	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
		SkillID: skillExposeID, TargetEntityID: victim,
	}); err != nil {
		t.Fatalf("SubmitIntent use-skill: %v", err)
	}

	step(t, w)

	mag, turns, ok := w.EffectForTest(victim, effectVulnerable)
	if !ok {
		t.Fatal("expose applied no vulnerable effect to the victim")
	}

	if got, want := mag, 120; got != want {
		t.Errorf("vulnerable magnitude = %d, want %d (percentBase+20)", got, want)
	}

	if got, want := turns, 3; got != want {
		t.Errorf("vulnerable turns = %d, want %d", got, want)
	}

	// The caster is not the target. A kind that applied to whoever cast it
	// would pass every assertion above if the victim happened to be the caster.
	if _, _, ok := w.EffectForTest(id, effectVulnerable); ok {
		t.Error("expose debuffed its own caster")
	}
}

// TestExposeIsRefusedOnAnAlly: a debuff is a weapon. Landing one on a friend is
// not a mis-click to be forgiven — the action should not exist, so it is
// refused at submit time with the same error a friendly-fire attack gets.
func TestExposeIsRefusedOnAnAlly(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id, token := selfCaster(t, w, skillCombatTrainingID, skillWeakSpotID, skillExposeID)

	w.SetHexForTest(id, protocol.Hex{Q: 0, R: 0})

	ally, err := w.Join("", "friend", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	allyHex := walkableHexAtDistance(t, w, protocol.Hex{Q: 0, R: 0}, 2, 3)
	clearSightLine(t, w, protocol.Hex{Q: 0, R: 0}, allyHex)
	w.SetHexForTest(ally.EntityID, allyHex)

	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
		SkillID: skillExposeID, TargetEntityID: ally.EntityID,
	}); err == nil {
		t.Fatal("expose on an ally was accepted")
	}
}

// TestExposeIsRefusedOutOfRange: the same reach gate a shot gets. Without it a
// debuff would be a free action against anything visible, which is a longer
// reach than any weapon in the game.
func TestExposeIsRefusedOutOfRange(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id, token := selfCaster(t, w, skillCombatTrainingID, skillWeakSpotID, skillExposeID)

	w.SetHexForTest(id, protocol.Hex{Q: 0, R: 0})

	// Expose reaches 4; place the victim beyond it.
	farHex := walkableHexAtDistance(t, w, protocol.Hex{Q: 0, R: 0}, 5, 6)
	clearSightLine(t, w, protocol.Hex{Q: 0, R: 0}, farHex)
	victim := w.PlaceMonsterForTest(farHex)

	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
		SkillID: skillExposeID, TargetEntityID: victim,
	}); err == nil {
		t.Fatal("expose beyond its range was accepted")
	}
}

// TestVulnerableIsCleansedAsHarmful: the counterplay, and it is INHERITED —
// nothing in skills.go mentions cleansing. Vulnerable is marked harmful on its
// effect row, and that one field is the whole of the behaviour.
func TestVulnerableIsCleansedAsHarmful(t *testing.T) {
	t.Parallel()

	w := newWorld()

	me, err := w.Join("", "cured", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.ApplyEffectForTest(me.EntityID, effectVulnerable, 120, 3)

	stackID := w.GrantItemForTest(me.EntityID, "antivenom")
	if err := w.SubmitIntent(intentFor(me.EntityID, me.Token, protocol.IntentDrink, stackID)); err != nil {
		t.Fatalf("SubmitIntent drink antivenom: %v", err)
	}

	if _, _, ok := w.EffectForTest(me.EntityID, effectVulnerable); ok {
		t.Error("antivenom did not clear vulnerable — it is marked harmful")
	}
}
