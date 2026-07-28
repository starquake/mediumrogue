package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// evade_test.go (#161): the first ACTIVE skill, end to end.

const (
	skillEvadeID = "evade"
	// skillSurvivalistID is Evade's prerequisite, and is also seeded by
	// wire_nil_test.go's board — shared so goconst stays quiet.
	skillSurvivalistID = "survivalist"
	// Shared across game_test specs (archive/snapshot/skills-wire) so the
	// literals don't trip goconst.
	skillCombatTrainingID = "combat-training"
	skillWeakSpotID       = "weak-spot"
)

// evadeReady seeds a player who has learned Evade and can use it.
func evadeReady(t *testing.T, w *game.World) (int64, string) {
	t.Helper()

	resp, err := w.Join("", "evader", protocol.ClassRogue, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.SetSkillStateForTest(resp.EntityID, []string{skillSurvivalistID, skillEvadeID}, 0, 1)

	return resp.EntityID, resp.Token
}

// TestEvadeMovesThePlayerAndStartsItsCooldown: the whole point, through the
// real intent path rather than by poking fields.
func TestEvadeMovesThePlayerAndStartsItsCooldown(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id, token := evadeReady(t, w)

	w.SetHexForTest(id, protocol.Hex{Q: 0, R: 0})

	target := walkableHexAtDistance(t, w, protocol.Hex{Q: 0, R: 0}, 2, 3)
	clearSightLine(t, w, protocol.Hex{Q: 0, R: 0}, target)

	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
		SkillID: skillEvadeID, Target: target,
	}); err != nil {
		t.Fatalf("SubmitIntent use-skill: %v", err)
	}

	snap := step(t, w)

	if got, want := entityHexIn(t, snap, id), target; got != want {
		t.Errorf("player at %v after evade, want %v", got, want)
	}

	if got := w.ActiveReadyTurnForTest(id, skillEvadeID); got == 0 {
		t.Error("evade did not start its cooldown")
	}
}

// TestEvadeIsRejectedOnCooldown: the cost is real. A second trigger before the
// ready turn is refused at submit time, not silently dropped later.
func TestEvadeIsRejectedOnCooldown(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id, token := evadeReady(t, w)

	w.SetHexForTest(id, protocol.Hex{Q: 0, R: 0})

	target := walkableHexAtDistance(t, w, protocol.Hex{Q: 0, R: 0}, 2, 3)
	clearSightLine(t, w, protocol.Hex{Q: 0, R: 0}, target)

	req := protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
		SkillID: skillEvadeID, Target: target,
	}

	if err := w.SubmitIntent(req); err != nil {
		t.Fatalf("first evade: %v", err)
	}

	step(t, w)

	back := walkableHexAtDistance(t, w, target, 1, 2)
	clearSightLine(t, w, target, back)
	req.Target = back

	if got, want := w.SubmitIntent(req), game.ErrSkillOnCooldown; got == nil {
		t.Fatalf("second evade = nil, want %v", want)
	}
}

// TestUseSkillRejectsAnUnlearnedSkill: an active you have not learned is not a
// thing you can trigger, near-sightedness notwithstanding.
//
// Uses Second Wind rather than Evade since #322: evade is universal, so being
// unlearned is no longer a reason to refuse it — the gate it once demonstrated
// still has to hold for everything else.
func TestUseSkillRejectsAnUnlearnedSkill(t *testing.T) {
	t.Parallel()

	w := newWorld()

	resp, err := w.Join("", "nobody", protocol.ClassRogue, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	if got := w.SubmitIntent(protocol.IntentRequest{
		EntityID: resp.EntityID, Token: resp.Token, Kind: protocol.IntentUseSkill,
		SkillID: skillSecondWindID, Target: resp.Hex,
	}); got == nil {
		t.Fatal("unlearned second wind was accepted")
	}
}

// TestEvadeRejectsAPassiveSkill: use-skill names an ACTIVE. A passive has no
// trigger, and accepting one would make the category meaningless.
func TestEvadeRejectsAPassiveSkill(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id, token := evadeReady(t, w)

	if got, want := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
		SkillID: skillSurvivalistID, Target: protocol.Hex{Q: 0, R: 0},
	}), game.ErrSkillNotActive; got == nil {
		t.Fatalf("passive skill accepted as an active, want %v", want)
	}
}

// TestEvadeRejectsAnOutOfRangeTarget: range is 3 hexes.
func TestEvadeRejectsAnOutOfRangeTarget(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id, token := evadeReady(t, w)

	w.SetHexForTest(id, protocol.Hex{Q: 0, R: 0})

	far := walkableHexAtDistance(t, w, protocol.Hex{Q: 0, R: 0}, 5, 6)

	if got, want := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
		SkillID: skillEvadeID, Target: far,
	}), game.ErrOutOfRange; got == nil {
		t.Fatalf("out-of-range evade accepted, want %v", want)
	}
}

// TestEvadeRejectsAMonsterHeldTarget (#196): evade used to ignore occupancy,
// so a player could teleport onto a melee monster's hex — an opposing
// co-occupancy where the monster's Pathfind(from==to) is empty and it can
// never attack, i.e. a permanent safe spot. An occupied destination is now
// refused at submit like every other invalid evade.
func TestEvadeRejectsAMonsterHeldTarget(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id, token := evadeReady(t, w)

	w.SetHexForTest(id, protocol.Hex{Q: 0, R: 0})

	target := walkableHexAtDistance(t, w, protocol.Hex{Q: 0, R: 0}, 2, 3)
	clearSightLine(t, w, protocol.Hex{Q: 0, R: 0}, target)
	w.PlaceMonsterForTest(target)

	if got, want := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
		SkillID: skillEvadeID, Target: target,
	}), game.ErrHexOccupied; got == nil {
		t.Fatalf("evade onto a monster's hex accepted, want %v", want)
	}
}

// TestEvadeRejectsAStackCappedTarget (#196): evade onto a hex already holding
// protocol.StackCap friendly entities would breach the per-hex cap every
// ordinary mover respects. Refused at submit.
func TestEvadeRejectsAStackCappedTarget(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id, token := evadeReady(t, w)

	w.SetHexForTest(id, protocol.Hex{Q: 0, R: 0})

	target := walkableHexAtDistance(t, w, protocol.Hex{Q: 0, R: 0}, 2, 3)
	clearSightLine(t, w, protocol.Hex{Q: 0, R: 0}, target)

	for range protocol.StackCap {
		w.PlaceEntityForTest(target)
	}

	if got, want := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
		SkillID: skillEvadeID, Target: target,
	}), game.ErrHexOccupied; got == nil {
		t.Fatalf("evade onto a StackCap-full hex accepted, want %v", want)
	}
}

// TestEvadeDropsAnActiveWhoseHexFilledThisTurn (#196): the submit check reads
// occupancy as it stands in the intent window, but the board shifts at
// resolution — another evade the same turn can take the last slot on a hex.
// resolveActivesLocked re-checks against the evolving board and DROPS the
// second lander (no move, no cooldown) rather than breaching StackCap. Two
// players evade onto a hex already holding StackCap-1 friendlies: the lower-id
// caster lands (filling the cap), the higher-id caster is turned away.
func TestEvadeDropsAnActiveWhoseHexFilledThisTurn(t *testing.T) {
	t.Parallel()

	w := newWorld()

	origin := protocol.Hex{Q: 0, R: 0}
	target := walkableHexAtDistance(t, w, origin, 2, 3)

	// Fill the target to one below the cap with static players.
	for range protocol.StackCap - 1 {
		w.PlaceEntityForTest(target)
	}

	evadeFrom := func(from protocol.Hex) int64 {
		t.Helper()

		id, token := w.PlaceEntityForTest(from)
		w.SetSkillStateForTest(id, []string{skillSurvivalistID, skillEvadeID}, 0, 1)
		clearSightLine(t, w, from, target)

		if err := w.SubmitIntent(protocol.IntentRequest{
			EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
			SkillID: skillEvadeID, Target: target,
		}); err != nil {
			t.Fatalf("SubmitIntent evade from %v: %v", from, err)
		}

		return id
	}

	first := evadeFrom(walkableHexAtDistance(t, w, target, 2, 2))
	second := evadeFrom(walkableHexAtDistance(t, w, target, 3, 3))

	snap := step(t, w)

	occ := 0

	for _, e := range snap.Entities {
		if e.Hex == target {
			occ++
		}
	}

	if got, want := occ, protocol.StackCap; got != want {
		t.Errorf("target occupancy after two evades = %d, want %d (StackCap not breached)", got, want)
	}

	if got, want := entityHexIn(t, snap, first), target; got != want {
		t.Errorf("first (lower-id) evader at %v, want it to have landed at %v", got, want)
	}

	if got := entityHexIn(t, snap, second); got == target {
		t.Error("second (higher-id) evader landed on a now-full hex, breaching StackCap")
	}
}

// TestEvadeCooldownSurvivesASnapshotRoundTrip: the reason snapshotVersion went
// to 7. Without persistence a server restart is a free cooldown reset — which
// a player would find, and use.
func TestEvadeCooldownSurvivesASnapshotRoundTrip(t *testing.T) {
	t.Parallel()

	w := newWorld()
	id, token := evadeReady(t, w)

	w.SetHexForTest(id, protocol.Hex{Q: 0, R: 0})

	target := walkableHexAtDistance(t, w, protocol.Hex{Q: 0, R: 0}, 2, 3)
	clearSightLine(t, w, protocol.Hex{Q: 0, R: 0}, target)

	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
		SkillID: skillEvadeID, Target: target,
	}); err != nil {
		t.Fatalf("SubmitIntent: %v", err)
	}

	step(t, w)

	want := w.ActiveReadyTurnForTest(id, skillEvadeID)
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

	if got := restored.ActiveReadyTurnForTest(id, skillEvadeID); got != want {
		t.Errorf("cooldown after restore = %d, want %d — a restart must not be a free reset", got, want)
	}
}

// TestEvadeNeedsNoLearning pins the #322 change: evade is a MECHANIC, so a
// brand-new player who has spent no skill points can use it. Before it became
// universal this returned ErrSkillNotLearned.
func TestEvadeNeedsNoLearning(t *testing.T) {
	t.Parallel()

	w := newWorld()

	origin := protocol.Hex{Q: 0, R: 0}
	id, token := w.PlaceEntityForTest(origin)

	target := protocol.Hex{Q: 2, R: 0}
	clearLine(w, origin, protocol.Hex{Q: 1, R: 0}, target)

	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentUseSkill,
		SkillID: skillEvadeID, Target: target,
	}); err != nil {
		t.Fatalf("evade by an unlearned player = %v, want nil", err)
	}
}

// TestEvadeIsNotInTheSkillPanel: a universal mechanic has nothing to learn, so
// it must not occupy a row in the panel about what you can BECOME. Read off the
// turn bundle rather than the registry — the panel is built from what the wire
// carries, so that is where its absence has to hold.
//
//nolint:paralleltest // drives a shared world.
func TestEvadeIsNotInTheSkillPanel(t *testing.T) {
	w := newWorld()

	me, err := w.Join("", "evader", protocol.ClassRogue, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	// Points to spend and a learned prerequisite: the state most likely to
	// surface a learnable row if evade were still one.
	w.SetSkillStateForTest(me.EntityID, []string{skillSurvivalistID}, 5, 3)

	for _, e := range w.SnapshotFor(me.Token).Entities {
		if e.ID != me.EntityID {
			continue
		}

		for _, v := range e.Skills {
			if got := v.ID; got == skillEvadeID {
				t.Errorf("skill panel offers %q, want it absent — it is universal", got)
			}
		}
	}
}
