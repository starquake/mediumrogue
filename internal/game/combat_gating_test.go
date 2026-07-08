package game_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// fakeClock is a hand-advanced clock for driving the two-clock control loop
// deterministically. Its now method value is handed to World.SetNowForTest.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.t = c.t.Add(d)
}

// newTimedWorld builds a world whose clock the test drives by hand: a 1 s world
// interval (so advancing the clock a second fires a world tick) and a clock
// baseline seeded like the Run loop does at startup.
func newTimedWorld(t *testing.T) (*game.World, *fakeClock) {
	t.Helper()

	w := game.NewWorld(time.Second, testCombatPatience, testBubblePoll, hub.New())
	clk := &fakeClock{t: time.Unix(1_000_000, 0)}
	w.SetNowForTest(clk.now)
	w.StartClockForTest()
	w.SetSeedForTest(1)

	return w, clk
}

// formBubble joins a player and drops a monster on an adjacent hex, then fires
// one world tick to form the combat bubble around them. It returns the player's
// join response and the monster id. After it returns the monster has bumped the
// player once (its think-then-attack on the forming tick), so callers read HP
// from the returned snapshot, not from full health.
func formBubble(t *testing.T, w *game.World, clk *fakeClock) (protocol.JoinResponse, int64, protocol.TurnEvent) {
	t.Helper()

	me, err := w.Join("")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	monsterID := w.PlaceMonsterForTest(walkableNeighbor(t, w, me.Hex))

	clk.advance(time.Second)

	if !w.PollTickForTest() {
		t.Fatalf("world tick did not resolve on the forming poll")
	}

	snap := w.Snapshot()
	if got, want := len(snap.Bubbles), 1; got != want {
		t.Fatalf("bubble count after forming poll = %d, want %d", got, want)
	}

	if !inCombat(t, snap, me.EntityID) {
		t.Fatalf("player InCombat = false after forming poll, want true")
	}

	return me, monsterID, snap
}

// TestBubbleFreezesWhileWorldTicks: a formed bubble does not advance while its
// player has not locked in, even as the world clock keeps ticking. The world
// turn counter climbs, but the bubble's members neither move nor take damage.
func TestBubbleFreezesWhileWorldTicks(t *testing.T) {
	t.Parallel()

	w, clk := newTimedWorld(t)
	w.SetCombatPatienceForTest(time.Hour) // never times out during this test

	me, monsterID, form := formBubble(t, w, clk)

	meHP := entityHP(t, form, me.EntityID)
	monsterHP := entityHP(t, form, monsterID)
	meHex := hexOfSnap(form, me.EntityID)
	monsterHex := hexOfSnap(form, monsterID)
	formTurn := form.Turn

	// Several world ticks pass with the player idle (never locking in).
	for range 3 {
		clk.advance(time.Second)
		w.PollTickForTest()
	}

	snap := w.Snapshot()

	if got := snap.Turn; got <= formTurn {
		t.Errorf("world turn = %d, want > %d (world must keep ticking)", got, formTurn)
	}

	if got, want := entityHP(t, snap, me.EntityID), meHP; got != want {
		t.Errorf("frozen player HP = %d, want unchanged %d", got, want)
	}

	if got, want := entityHP(t, snap, monsterID), monsterHP; got != want {
		t.Errorf("frozen monster HP = %d, want unchanged %d", got, want)
	}

	if got, want := hexOfSnap(snap, me.EntityID), meHex; got != want {
		t.Errorf("frozen player hex = %v, want unchanged %v", got, want)
	}

	if got, want := hexOfSnap(snap, monsterID), monsterHex; got != want {
		t.Errorf("frozen monster hex = %v, want unchanged %v", got, want)
	}

	if !inCombat(t, snap, me.EntityID) {
		t.Errorf("player InCombat = false, want true (bubble must persist)")
	}
}

// TestBubbleAdvancesOnLockIn: submitting an intent for the sole player of a
// bubble locks it in, so the bubble resolves immediately — a combat turn runs
// (the player bumps the monster, the monster bumps back) without any clock
// advance.
func TestBubbleAdvancesOnLockIn(t *testing.T) {
	t.Parallel()

	w, clk := newTimedWorld(t)
	w.SetCombatPatienceForTest(time.Hour)

	me, monsterID, form := formBubble(t, w, clk)

	meHP := entityHP(t, form, me.EntityID)
	monsterHP := entityHP(t, form, monsterID)
	monsterHex := hexOfSnap(form, monsterID)

	// Lock in with a bump onto the monster: all (one) players ready -> resolve now.
	if !submitOK(w, me, monsterHex) {
		t.Fatalf("SubmitIntent onto the monster's hex failed")
	}

	snap := w.Snapshot()

	if got, want := entityHP(t, snap, monsterID), monsterHP-protocol.PlayerAttackDamage; got != want {
		t.Errorf("monster HP = %d, want %d (lock-in must run the combat turn)", got, want)
	}

	if got, want := entityHP(t, snap, me.EntityID), meHP-protocol.MonsterAttackDamage; got != want {
		t.Errorf("player HP = %d, want %d (monster bumps back on the resolved turn)", got, want)
	}

	// Lock-in cleared: the player is waiting again for the next bubble-turn.
	if got, want := snap.Bubbles[0].WaitingForIDs, []int64{me.EntityID}; !slices.Equal(got, want) {
		t.Errorf("WaitingForIDs = %v, want %v (ready must clear after resolving)", got, want)
	}
}

// TestBubbleTimesOutWithUnreadyPlayer: with a short patience, advancing the
// clock past the deadline resolves the bubble even though the player never
// locked in — the monster gets its attack, but the AFK player does not move.
func TestBubbleTimesOutWithUnreadyPlayer(t *testing.T) {
	t.Parallel()

	w, clk := newTimedWorld(t)
	w.SetCombatPatienceForTest(10 * time.Second)

	me, monsterID, form := formBubble(t, w, clk)

	meHP := entityHP(t, form, me.EntityID)
	monsterHP := entityHP(t, form, monsterID)
	meHex := hexOfSnap(form, me.EntityID)

	// Before the deadline: still frozen.
	clk.advance(5 * time.Second)
	w.PollTickForTest()

	if got, want := entityHP(t, w.Snapshot(), me.EntityID), meHP; got != want {
		t.Fatalf("player HP before deadline = %d, want unchanged %d", got, want)
	}

	// Past the deadline: the bubble resolves with the player still unready. The
	// combat outcome (not pollTick's bool, which also reports the world tick)
	// is what proves the patience gate fired: the monster bit the AFK player,
	// who neither attacked back nor moved.
	clk.advance(6 * time.Second)
	w.PollTickForTest()

	snap := w.Snapshot()

	if got, want := entityHP(t, snap, me.EntityID), meHP-protocol.MonsterAttackDamage; got != want {
		t.Errorf("player HP after timeout = %d, want %d (monster still attacks)", got, want)
	}

	if got, want := entityHP(t, snap, monsterID), monsterHP; got != want {
		t.Errorf("monster HP after timeout = %d, want unchanged %d (AFK player never attacked)", got, want)
	}

	if got, want := hexOfSnap(snap, me.EntityID), meHex; got != want {
		t.Errorf("player hex after timeout = %v, want unchanged %v (unready player does not move)", got, want)
	}

	// The timeout restarted the patience deadline: a second poll at the same
	// instant must not resolve the bubble again (were the deadline not reset it
	// would still read as expired and the player would take a second hit).
	postTimeoutHP := entityHP(t, snap, me.EntityID)

	w.PollTickForTest()

	if got, want := entityHP(t, w.Snapshot(), me.EntityID), postTimeoutHP; got != want {
		t.Errorf("player HP = %d, want unchanged %d (timeout must restart the patience deadline)", got, want)
	}
}

// TestWorldDomainResolvesWhileBubbleFrozen: a world-domain entity keeps taking
// its turn every world tick even while a separate combat bubble sits frozen on
// its unready player.
func TestWorldDomainResolvesWhileBubbleFrozen(t *testing.T) {
	t.Parallel()

	w, clk := newTimedWorld(t)
	w.SetCombatPatienceForTest(time.Hour)

	me, monsterID, form := formBubble(t, w, clk)

	monsterHP := entityHP(t, form, monsterID)

	// A lone player far from the bubble (dist 9 from origin), well outside
	// CombatRadius, stays in the world domain.
	far := mustWalkable(t, w, protocol.Hex{Q: 0, R: 9})
	wandererID, tok := w.PlaceEntityForTest(far)

	step := walkableNeighbor(t, w, far)
	mustSubmit(t, w, wandererID, tok, step)

	clk.advance(time.Second)
	w.PollTickForTest()

	snap := w.Snapshot()

	if got, want := hexOfSnap(snap, wandererID), step; got != want {
		t.Errorf("world-domain wanderer hex = %v, want %v (it must resolve on the world tick)", got, want)
	}

	if inCombat(t, snap, wandererID) {
		t.Errorf("wanderer InCombat = true, want false (it is far from the bubble)")
	}

	// The frozen bubble did not advance: its monster took no new action.
	if got, want := entityHP(t, snap, monsterID), monsterHP; got != want {
		t.Errorf("frozen bubble monster HP = %d, want unchanged %d", got, want)
	}

	if !inCombat(t, snap, me.EntityID) {
		t.Errorf("bubble player InCombat = false, want true")
	}
}

// TestNoDoubleResolveAfterLockIn: after a lock-in resolves a bubble-turn, a poll
// pass at the same instant must not resolve it a second time. Lock-in clears the
// ready set and restarts the patience deadline, so the poll finds the bubble
// neither all-ready nor expired and leaves it alone.
func TestNoDoubleResolveAfterLockIn(t *testing.T) {
	t.Parallel()

	w, clk := newTimedWorld(t)
	w.SetCombatPatienceForTest(time.Hour)

	me, monsterID, form := formBubble(t, w, clk)

	monsterHex := hexOfSnap(form, monsterID)

	if !submitOK(w, me, monsterHex) {
		t.Fatalf("SubmitIntent onto the monster's hex failed")
	}

	afterLockIn := w.Snapshot()
	monsterHP := entityHP(t, afterLockIn, monsterID)
	meHP := entityHP(t, afterLockIn, me.EntityID)
	turn := afterLockIn.Turn

	// A poll at the same clock instant must be a no-op for this bubble.
	if got := w.PollTickForTest(); got {
		t.Errorf("poll after lock-in resolved something, want no-op")
	}

	snap := w.Snapshot()

	if got, want := entityHP(t, snap, monsterID), monsterHP; got != want {
		t.Errorf("monster HP = %d, want unchanged %d (no second resolution)", got, want)
	}

	if got, want := entityHP(t, snap, me.EntityID), meHP; got != want {
		t.Errorf("player HP = %d, want unchanged %d (no second resolution)", got, want)
	}

	if got, want := snap.Turn, turn; got != want {
		t.Errorf("turn = %d, want unchanged %d", got, want)
	}
}

// TestPatienceRemainingCountsDown: the wire's PatienceRemainingMs reflects the
// bubble's deadline relative to the injected clock, and shrinks as the clock
// advances toward the timeout.
func TestPatienceRemainingCountsDown(t *testing.T) {
	t.Parallel()

	w, clk := newTimedWorld(t)
	w.SetCombatPatienceForTest(30 * time.Second)

	formBubble(t, w, clk)

	first := w.Snapshot().Bubbles[0].PatienceRemainingMs
	if first <= 0 {
		t.Fatalf("PatienceRemainingMs = %d, want a positive countdown", first)
	}

	clk.advance(10 * time.Second)

	if got := w.Snapshot().Bubbles[0].PatienceRemainingMs; got >= first {
		t.Errorf("PatienceRemainingMs = %d, want less than %d after the clock advanced", got, first)
	}
}

// TestNoDoubleActionWalkingIntoExpiredBubble guards the subtle pass-ordering
// hazard: a world tick and a bubble timeout can fire in the same poll, and the
// world tick can walk a world-domain entity into that bubble. If the pass
// resolved the bubble over its post-merge membership it would move the walker a
// second time. Because the pass captures each resolution's members up front and
// recomputes only once at the end, the walker moves exactly one hex on the pass
// it joins the bubble.
func TestNoDoubleActionWalkingIntoExpiredBubble(t *testing.T) {
	t.Parallel()

	w, clk := newTimedWorld(t)
	w.SetCombatPatienceForTest(time.Nanosecond) // the bubble is always expired

	me, _, _ := formBubble(t, w, clk)

	// A world-domain walker heading straight for the bubble from outside range.
	start := mustWalkable(t, w, protocol.Hex{Q: 0, R: 9})
	wandererID, tok := w.PlaceEntityForTest(start)
	mustSubmit(t, w, wandererID, tok, me.Hex)

	prevInCombat := false

	for range 12 {
		before := hexOfSnap(w.Snapshot(), wandererID)

		clk.advance(time.Second)
		w.PollTickForTest()

		snap := w.Snapshot()
		after := hexOfSnap(snap, wandererID)
		nowInCombat := inCombat(t, snap, wandererID)

		if nowInCombat && !prevInCombat {
			if got, want := game.HexDistance(before, after), 1; got != want {
				t.Fatalf("walker moved %d hexes on the pass it joined the bubble, want 1 (double action)", got)
			}

			return
		}

		prevInCombat = nowInCombat
	}

	t.Fatalf("walker never joined the bubble within the step budget")
}

// TestRunLoopSurvivesConcurrentIntents drives the real control-loop goroutine
// with several concurrent submitters and readers, so the race detector can
// prove every resolution path serializes on the world mutex. It asserts only
// liveness (the world advances); the point is a clean -race run.
func TestRunLoopSurvivesConcurrentIntents(t *testing.T) {
	t.Parallel()

	w := game.NewWorld(2*time.Millisecond, testCombatPatience, testBubblePoll, hub.New())
	w.SetBubblePollForTest(time.Millisecond)

	me, err := w.Join("")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.PlaceMonsterForTest(walkableNeighbor(t, w, me.Hex))

	target := walkableNeighbor(t, w, me.Hex)

	ctx, cancel := context.WithCancel(t.Context())

	var wg sync.WaitGroup

	wg.Go(func() {
		w.Run(ctx)
	})

	for range 4 {
		wg.Go(func() {
			for range 200 {
				_ = w.SubmitIntent(protocol.IntentRequest{EntityID: me.EntityID, Token: me.Token, Target: target})
				_ = w.Snapshot()
			}
		})
	}

	// Let the loop turn over for a while, then stop.
	time.Sleep(100 * time.Millisecond)
	cancel()
	wg.Wait()

	if got := w.Snapshot().Turn; got <= 0 {
		t.Errorf("world turn = %d, want > 0 (the control loop must have advanced)", got)
	}
}

// entityHP returns an entity's HP from a snapshot, failing if it is absent.
func entityHP(t *testing.T, snap protocol.TurnEvent, id int64) int {
	t.Helper()

	e, ok := entityOfSnap(snap, id)
	if !ok {
		t.Fatalf("entity %d missing from snapshot", id)
	}

	return e.HP
}
