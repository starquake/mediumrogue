package game_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// These tests pin the input-window semantics decided for #99: intent
// acceptance has NO server-side cutoff, because it needs none — World.mu
// serializes SubmitIntent against every resolution pass (the control loop's
// pollTick and the in-SubmitIntent bubble lock-in resolution both hold the
// mutex end-to-end), so an intent that arrives while a turn is resolving is
// (a) accepted, (b) provably invisible to the already-resolving turn, and
// (c) queued whole for the next one. A hard rejection window would punish
// clients that submit during playback for zero integrity gain.

// combatEventHook is a slog.Handler that fires fn exactly once, on the first
// "combat" record it sees. Combat records are only emitted mid-resolution,
// while World.mu is held — so fn runs at a moment when a turn is provably in
// the middle of resolving.
type combatEventHook struct {
	once sync.Once
	fn   func()
}

func (*combatEventHook) Enabled(context.Context, slog.Level) bool { return true }

func (h *combatEventHook) Handle(_ context.Context, r slog.Record) error {
	if r.Message == "combat" {
		h.once.Do(h.fn)
	}

	return nil
}

func (h *combatEventHook) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *combatEventHook) WithGroup(string) slog.Handler      { return h }

// submitMoveDuringResolution arms a goroutine that submits a move intent for
// the entity the moment hookFired closes (i.e. mid-resolution), returning the
// channel its SubmitIntent error will arrive on. The submission blocks on
// World.mu until the resolving pass completes — the serialization under test.
func submitMoveDuringResolution(
	w *game.World, hookFired <-chan struct{}, resp protocol.JoinResponse, target protocol.Hex,
) <-chan error {
	errCh := make(chan error, 1)

	go func() {
		<-hookFired

		errCh <- w.SubmitIntent(protocol.IntentRequest{
			EntityID: resp.EntityID, Token: resp.Token,
			Kind: protocol.IntentMove, Target: target,
		})
	}()

	return errCh
}

// requireHookFired fails fast if no combat event fired during the pass — the
// guard that keeps a broken fixture from hanging on the late-intent channel.
func requireHookFired(t *testing.T, hookFired <-chan struct{}) {
	t.Helper()

	select {
	case <-hookFired:
	default:
		t.Fatal("resolution emitted no combat event; the mid-resolution hook never fired")
	}
}

// TestIntentDuringWorldResolutionAppliesToNextTurn: a move intent submitted
// at the exact moment a WORLD turn is resolving (hooked off the pass's own
// combat "move" log event, emitted with World.mu held) is accepted, does not
// deflect the resolving turn — the entity steps along its ORIGINAL path —
// and stands queued as the next turn's route.
func TestIntentDuringWorldResolutionAppliesToNextTurn(t *testing.T) {
	t.Parallel()

	w := newWorld()
	alice := joinNamed(t, w, testAliceName)
	pinToOrigin(w, &alice)

	firstTarget := walkableHexAtDistance(t, w, alice.Hex, 3, 3)
	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: alice.EntityID, Token: alice.Token,
		Kind: protocol.IntentMove, Target: firstTarget,
	}); err != nil {
		t.Fatalf("submit first move: %v", err)
	}

	firstPath := w.PathForTest(alice.EntityID)
	if len(firstPath) < 2 {
		t.Fatalf("first path has %d steps, want >= 2", len(firstPath))
	}

	lateTarget := walkableHexAtDistance(t, w, alice.Hex, 2, 2)

	hookFired := make(chan struct{})

	w.SetLogger(slog.New(&combatEventHook{fn: func() { close(hookFired) }}))
	lateErr := submitMoveDuringResolution(w, hookFired, alice, lateTarget)

	w.ResolveTurnForTest()

	requireHookFired(t, hookFired)

	if err := <-lateErr; err != nil {
		t.Fatalf("mid-resolution SubmitIntent = %v, want accepted (nil)", err)
	}

	// The resolving turn used the pre-existing intent: one step along the
	// ORIGINAL path, untouched by the late submission.
	if got, want := entityHex(t, w, alice.EntityID), firstPath[0]; got != want {
		t.Errorf("hex after resolving turn = %v, want %v (first step of the original path)", got, want)
	}

	// The late intent is queued whole for the NEXT turn.
	latePath := w.PathForTest(alice.EntityID)
	if len(latePath) == 0 {
		t.Fatal("late intent left no queued path")
	}

	if got, want := latePath[len(latePath)-1], lateTarget; got != want {
		t.Errorf("queued path ends at %v, want %v (the late intent's target)", got, want)
	}

	w.SetLogger(slog.New(slog.DiscardHandler))
	w.ResolveTurnForTest()

	if got, want := entityHex(t, w, alice.EntityID), latePath[0]; got != want {
		t.Errorf("hex after next turn = %v, want %v (first step of the late intent's path)", got, want)
	}
}

// TestIntentDuringBubbleResolutionQueuesForNextBubbleTurn is the combat-time
// half of the same pin. A solo player's lock-in resolves her bubble turn
// synchronously INSIDE her own SubmitIntent call (fresh bubble — the turn
// floor is open); a second intent racing in during that resolution pass is
// accepted, does not leak into the resolving turn (she attacks, she does not
// also move), and stands queued as her next bubble turn's action — the turn
// floor keeps the late lock-in from resolving a second turn immediately.
func TestIntentDuringBubbleResolutionQueuesForNextBubbleTurn(t *testing.T) {
	t.Parallel()

	w := newWorld()
	alice := joinNamed(t, w, testAliceName)
	pinToOrigin(w, &alice)

	neighbors := walkableNeighborsN(t, w, alice.Hex, 2)
	monsterID := w.PlaceMonsterForTest(neighbors[0])

	// Form the bubble: recompute runs at the end of a resolution pass.
	w.ResolveTurnForTest()

	if got, want := len(w.Snapshot().Bubbles), 1; got != want {
		t.Fatalf("bubbles after formation pass = %d, want %d", got, want)
	}

	lateTarget := neighbors[1] // free (the monster holds neighbors[0])

	hookFired := make(chan struct{})

	w.SetLogger(slog.New(&combatEventHook{fn: func() { close(hookFired) }}))
	lateErr := submitMoveDuringResolution(w, hookFired, alice, lateTarget)

	// The lock-in: solo bubble + open floor resolves the bubble turn inside
	// this call, with World.mu held throughout.
	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: alice.EntityID, Token: alice.Token,
		Kind: protocol.IntentAttack, TargetEntityID: monsterID,
	}); err != nil {
		t.Fatalf("submit attack lock-in: %v", err)
	}

	requireHookFired(t, hookFired)

	if err := <-lateErr; err != nil {
		t.Fatalf("mid-resolution SubmitIntent = %v, want accepted (nil)", err)
	}

	// The resolving bubble turn was the attack: the melee swing landed and
	// alice did not also take the late intent's step.
	if got, want := monsterHP(t, w, monsterID), game.MonsterMaxHPForTest("wolf"); got >= want {
		t.Errorf("monster HP after attack turn = %d, want < %d (the swing landed)", got, want)
	}

	if got, want := entityHex(t, w, alice.EntityID), alice.Hex; got != want {
		t.Errorf("hex after attack turn = %v, want %v (the late move must not leak into the pass)", got, want)
	}

	// The late move is queued as the NEXT bubble turn's action.
	latePath := w.PathForTest(alice.EntityID)
	if len(latePath) != 1 || latePath[0] != lateTarget {
		t.Errorf("queued path = %v, want exactly [%v] (the late intent, held for the next bubble turn)", latePath, lateTarget)
	}
}

// monsterHP reads an entity's current HP off the snapshot.
func monsterHP(t *testing.T, w *game.World, id int64) int {
	t.Helper()

	for _, e := range w.Snapshot().Entities {
		if e.ID == id {
			return e.HP
		}
	}

	t.Fatalf("entity %d not in snapshot", id)

	return 0
}
