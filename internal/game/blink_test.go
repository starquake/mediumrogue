package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// blink_test.go (#161): the first ACTIVE skill, end to end.

const (
	skillBlinkID = "blink"
	// skillSurvivalistID is Blink's prerequisite, and is also seeded by
	// wire_nil_test.go's board — shared so goconst stays quiet.
	skillSurvivalistID = "survivalist"
	// Shared across game_test specs (archive/snapshot/skills-wire) so the
	// literals don't trip goconst.
	skillCombatTrainingID = "combat-training"
	skillWeakSpotID       = "weak-spot"
)

// blinkReady seeds a player who has learned Blink and can use it.
func blinkReady(t *testing.T, w *game.World) (int64, string) {
	t.Helper()

	resp, err := w.Join("", "blinker", protocol.ClassRogue, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.SetSkillStateForTest(resp.EntityID, []string{skillSurvivalistID, skillBlinkID}, 0, 1)

	return resp.EntityID, resp.Token
}

// TestBlinkMovesThePlayerAndStartsItsCooldown: the whole point, through the
// real intent path rather than by poking fields.
func TestBlinkMovesThePlayerAndStartsItsCooldown(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id, token := blinkReady(t, w)

	w.SetHexForTest(id, protocol.Hex{Q: 0, R: 0})

	target := walkableHexAtDistance(t, w, protocol.Hex{Q: 0, R: 0}, 2, 3)
	clearSightLine(t, w, protocol.Hex{Q: 0, R: 0}, target)

	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
		SkillID: skillBlinkID, Target: target,
	}); err != nil {
		t.Fatalf("SubmitIntent use-skill: %v", err)
	}

	snap := step(t, w)

	if got, want := entityHexIn(t, snap, id), target; got != want {
		t.Errorf("player at %v after blink, want %v", got, want)
	}

	if got := w.ActiveReadyTurnForTest(id, skillBlinkID); got == 0 {
		t.Error("blink did not start its cooldown")
	}
}

// TestBlinkIsRejectedOnCooldown: the cost is real. A second trigger before the
// ready turn is refused at submit time, not silently dropped later.
func TestBlinkIsRejectedOnCooldown(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id, token := blinkReady(t, w)

	w.SetHexForTest(id, protocol.Hex{Q: 0, R: 0})

	target := walkableHexAtDistance(t, w, protocol.Hex{Q: 0, R: 0}, 2, 3)
	clearSightLine(t, w, protocol.Hex{Q: 0, R: 0}, target)

	req := protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
		SkillID: skillBlinkID, Target: target,
	}

	if err := w.SubmitIntent(req); err != nil {
		t.Fatalf("first blink: %v", err)
	}

	step(t, w)

	back := walkableHexAtDistance(t, w, target, 1, 2)
	clearSightLine(t, w, target, back)
	req.Target = back

	if got, want := w.SubmitIntent(req), game.ErrSkillOnCooldown; got == nil {
		t.Fatalf("second blink = nil, want %v", want)
	}
}

// TestBlinkRejectsAnUnlearnedSkill: an active you have not learned is not a
// thing you can trigger, near-sightedness notwithstanding.
func TestBlinkRejectsAnUnlearnedSkill(t *testing.T) {
	t.Parallel()

	w := newWorld()

	resp, err := w.Join("", "nobody", protocol.ClassRogue, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	if got := w.SubmitIntent(protocol.IntentRequest{
		EntityID: resp.EntityID, Token: resp.Token, Kind: protocol.IntentUseSkill,
		SkillID: skillBlinkID, Target: resp.Hex,
	}); got == nil {
		t.Fatal("unlearned blink was accepted")
	}
}

// TestBlinkRejectsAPassiveSkill: use-skill names an ACTIVE. A passive has no
// trigger, and accepting one would make the category meaningless.
func TestBlinkRejectsAPassiveSkill(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id, token := blinkReady(t, w)

	if got, want := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
		SkillID: skillSurvivalistID, Target: protocol.Hex{Q: 0, R: 0},
	}), game.ErrSkillNotActive; got == nil {
		t.Fatalf("passive skill accepted as an active, want %v", want)
	}
}

// TestBlinkRejectsAnOutOfRangeTarget: range is 3 hexes.
func TestBlinkRejectsAnOutOfRangeTarget(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id, token := blinkReady(t, w)

	w.SetHexForTest(id, protocol.Hex{Q: 0, R: 0})

	far := walkableHexAtDistance(t, w, protocol.Hex{Q: 0, R: 0}, 5, 6)

	if got, want := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
		SkillID: skillBlinkID, Target: far,
	}), game.ErrOutOfRange; got == nil {
		t.Fatalf("out-of-range blink accepted, want %v", want)
	}
}

// TestBlinkRejectsAMonsterHeldTarget (#196): blink used to ignore occupancy,
// so a player could teleport onto a melee monster's hex — an opposing
// co-occupancy where the monster's Pathfind(from==to) is empty and it can
// never attack, i.e. a permanent safe spot. An occupied destination is now
// refused at submit like every other invalid blink.
func TestBlinkRejectsAMonsterHeldTarget(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id, token := blinkReady(t, w)

	w.SetHexForTest(id, protocol.Hex{Q: 0, R: 0})

	target := walkableHexAtDistance(t, w, protocol.Hex{Q: 0, R: 0}, 2, 3)
	clearSightLine(t, w, protocol.Hex{Q: 0, R: 0}, target)
	w.PlaceMonsterForTest(target)

	if got, want := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
		SkillID: skillBlinkID, Target: target,
	}), game.ErrHexOccupied; got == nil {
		t.Fatalf("blink onto a monster's hex accepted, want %v", want)
	}
}

// TestBlinkRejectsAStackCappedTarget (#196): blink onto a hex already holding
// protocol.StackCap friendly entities would breach the per-hex cap every
// ordinary mover respects. Refused at submit.
func TestBlinkRejectsAStackCappedTarget(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id, token := blinkReady(t, w)

	w.SetHexForTest(id, protocol.Hex{Q: 0, R: 0})

	target := walkableHexAtDistance(t, w, protocol.Hex{Q: 0, R: 0}, 2, 3)
	clearSightLine(t, w, protocol.Hex{Q: 0, R: 0}, target)

	for range protocol.StackCap {
		w.PlaceEntityForTest(target)
	}

	if got, want := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
		SkillID: skillBlinkID, Target: target,
	}), game.ErrHexOccupied; got == nil {
		t.Fatalf("blink onto a StackCap-full hex accepted, want %v", want)
	}
}

// TestBlinkDropsAnActiveWhoseHexFilledThisTurn (#196): the submit check reads
// occupancy as it stands in the intent window, but the board shifts at
// resolution — another blink the same turn can take the last slot on a hex.
// resolveActivesLocked re-checks against the evolving board and DROPS the
// second lander (no move, no cooldown) rather than breaching StackCap. Two
// players blink onto a hex already holding StackCap-1 friendlies: the lower-id
// caster lands (filling the cap), the higher-id caster is turned away.
func TestBlinkDropsAnActiveWhoseHexFilledThisTurn(t *testing.T) {
	t.Parallel()

	w := newWorld()

	origin := protocol.Hex{Q: 0, R: 0}
	target := walkableHexAtDistance(t, w, origin, 2, 3)

	// Fill the target to one below the cap with static players.
	for range protocol.StackCap - 1 {
		w.PlaceEntityForTest(target)
	}

	blinkFrom := func(from protocol.Hex) int64 {
		t.Helper()

		id, token := w.PlaceEntityForTest(from)
		w.SetSkillStateForTest(id, []string{skillSurvivalistID, skillBlinkID}, 0, 1)
		clearSightLine(t, w, from, target)

		if err := w.SubmitIntent(protocol.IntentRequest{
			EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
			SkillID: skillBlinkID, Target: target,
		}); err != nil {
			t.Fatalf("SubmitIntent blink from %v: %v", from, err)
		}

		return id
	}

	first := blinkFrom(walkableHexAtDistance(t, w, target, 2, 2))
	second := blinkFrom(walkableHexAtDistance(t, w, target, 3, 3))

	snap := step(t, w)

	occ := 0

	for _, e := range snap.Entities {
		if e.Hex == target {
			occ++
		}
	}

	if got, want := occ, protocol.StackCap; got != want {
		t.Errorf("target occupancy after two blinks = %d, want %d (StackCap not breached)", got, want)
	}

	if got, want := entityHexIn(t, snap, first), target; got != want {
		t.Errorf("first (lower-id) blinker at %v, want it to have landed at %v", got, want)
	}

	if got := entityHexIn(t, snap, second); got == target {
		t.Error("second (higher-id) blinker landed on a now-full hex, breaching StackCap")
	}
}

// TestBlinkCooldownSurvivesASnapshotRoundTrip: the reason snapshotVersion went
// to 7. Without persistence a server restart is a free cooldown reset — which
// a player would find, and use.
func TestBlinkCooldownSurvivesASnapshotRoundTrip(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id, token := blinkReady(t, w)

	w.SetHexForTest(id, protocol.Hex{Q: 0, R: 0})

	target := walkableHexAtDistance(t, w, protocol.Hex{Q: 0, R: 0}, 2, 3)
	clearSightLine(t, w, protocol.Hex{Q: 0, R: 0}, target)

	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
		SkillID: skillBlinkID, Target: target,
	}); err != nil {
		t.Fatalf("SubmitIntent: %v", err)
	}

	step(t, w)

	want := w.ActiveReadyTurnForTest(id, skillBlinkID)
	if want == 0 {
		t.Fatal("cooldown never started, nothing to round-trip")
	}

	data, err := w.MarshalState()
	if err != nil {
		t.Fatalf("MarshalState: %v", err)
	}

	restored := newWorld()
	if err := restored.RestoreState(data); err != nil {
		t.Fatalf("RestoreState: %v", err)
	}

	if got := restored.ActiveReadyTurnForTest(id, skillBlinkID); got != want {
		t.Errorf("cooldown after restore = %d, want %d — a restart must not be a free reset", got, want)
	}
}
