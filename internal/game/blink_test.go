package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// blink_test.go (#161): the first ACTIVE skill, end to end.

const skillBlinkID = "blink"

// blinkReady seeds a player who has learned Blink and can use it.
func blinkReady(t *testing.T, w *game.World) (int64, string) {
	t.Helper()

	resp, err := w.Join("", "blinker", protocol.ClassRogue, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.SetSkillStateForTest(resp.EntityID, []string{"survivalist", skillBlinkID}, 0, 1)

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
		SkillID: "survivalist", Target: protocol.Hex{Q: 0, R: 0},
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
