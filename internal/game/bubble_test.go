package game_test

import (
	"slices"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// inCombat looks up an entity in snap and returns its InCombat flag, failing
// the test if the entity is absent.
func inCombat(t *testing.T, snap protocol.TurnEvent, id int64) bool {
	t.Helper()

	e, ok := entityOfSnap(snap, id)
	if !ok {
		t.Fatalf("entity %d missing from snapshot", id)
	}

	return e.InCombat
}

// walkableNeighborsN returns the first n distinct walkable neighbors of from,
// so a test can place several entities on distinct adjacent hexes.
func walkableNeighborsN(t *testing.T, w *game.World, from protocol.Hex, n int) []protocol.Hex {
	t.Helper()

	var out []protocol.Hex

	for _, nb := range game.HexNeighbors(from) {
		if isWalkable(w, nb) {
			out = append(out, nb)

			if len(out) == n {
				return out
			}
		}
	}

	t.Fatalf("need %d walkable neighbors around %v, found %d", n, from, len(out))

	return nil
}

// mustWalkable asserts h is walkable, so far-hex geometry the tests depend on
// fails loudly if the static map ever changes underneath them.
func mustWalkable(t *testing.T, w *game.World, h protocol.Hex) protocol.Hex {
	t.Helper()

	if !isWalkable(w, h) {
		t.Fatalf("hex %v is not walkable on the static map", h)
	}

	return h
}

// TestBubbleFormsOnOpposingProximity: a player and a monster within
// CombatRadius fall into one combat bubble — both read InCombat, and the
// snapshot carries a single bubble listing both as members.
func TestBubbleFormsOnOpposingProximity(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	me, err := w.Join("")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	monsterID := w.PlaceMonsterForTest(walkableNeighbor(t, w, me.Hex))

	snap := step(t, w)

	if !inCombat(t, snap, me.EntityID) {
		t.Errorf("player InCombat = false, want true")
	}

	if !inCombat(t, snap, monsterID) {
		t.Errorf("monster InCombat = false, want true")
	}

	if got, want := len(snap.Bubbles), 1; got != want {
		t.Fatalf("bubble count = %d, want %d", got, want)
	}

	if got, want := snap.Bubbles[0].MemberIDs, []int64{me.EntityID, monsterID}; !slices.Equal(got, want) {
		t.Errorf("bubble members = %v, want %v", got, want)
	}
}

// TestBubbleAbsorbsFriendlyInRange: a second player within CombatRadius of a
// bubble member is pulled into the same bubble, even though it forms no
// opposing pair of its own.
func TestBubbleAbsorbsFriendlyInRange(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	me, err := w.Join("")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	spots := walkableNeighborsN(t, w, me.Hex, 2)
	monsterID := w.PlaceMonsterForTest(spots[0])
	friendID, _ := w.PlaceEntityForTest(spots[1])

	snap := step(t, w)

	for _, id := range []int64{me.EntityID, monsterID, friendID} {
		if !inCombat(t, snap, id) {
			t.Errorf("entity %d InCombat = false, want true", id)
		}
	}

	if got, want := len(snap.Bubbles), 1; got != want {
		t.Fatalf("bubble count = %d, want %d", got, want)
	}

	want := []int64{me.EntityID, monsterID, friendID}
	slices.Sort(want)

	if got := snap.Bubbles[0].MemberIDs; !slices.Equal(got, want) {
		t.Errorf("bubble members = %v, want %v", got, want)
	}
}

// TestBubbleKeepsIDAcrossRecomputes: a bubble that stays alive across several
// world turns keeps the same id every recompute. recomputeBubblesLocked rebuilds
// bubbles from scratch each turn, carrying the id forward by membership overlap;
// Task 3's action-gating (ready/deadline) hangs off that id, so a regression that
// minted a fresh id per recompute would silently reset the bubble's clock.
func TestBubbleKeepsIDAcrossRecomputes(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	me, err := w.Join("")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.PlaceMonsterForTest(walkableNeighbor(t, w, me.Hex))

	// Buffer the anchor's HP far above maxHP so the monster's per-turn chip damage
	// never kills (and respawns) it — a respawn would relocate the player and
	// dissolve the bubble, ending the run we want to observe.
	w.SetHPForTest(me.EntityID, 100000)

	snap := step(t, w)

	if got, want := len(snap.Bubbles), 1; got != want {
		t.Fatalf("bubble count = %d, want %d", got, want)
	}

	firstID := snap.Bubbles[0].ID
	if firstID == 0 {
		t.Fatalf("first bubble id = 0, want a real (non-world) id")
	}

	// Same physical bubble across several more recomputes: id must not change.
	for turn := range 3 {
		snap = step(t, w)

		if got, want := len(snap.Bubbles), 1; got != want {
			t.Fatalf("turn %d: bubble count = %d, want %d", turn, got, want)
		}

		if got, want := snap.Bubbles[0].ID, firstID; got != want {
			t.Errorf("turn %d: bubble id = %d, want %d (id must persist across recomputes)", turn, got, want)
		}
	}
}

// TestBubbleKeepsIDWhenAbsorbingFriendly: pulling a new friendly member into an
// existing bubble must not remint its id. The larger component still shares most
// members with the old bubble, so it inherits that id — preserving any gating
// state the bubble already held.
func TestBubbleKeepsIDWhenAbsorbingFriendly(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	me, err := w.Join("")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	spots := walkableNeighborsN(t, w, me.Hex, 2)
	w.PlaceMonsterForTest(spots[0])

	// Keep the anchor alive so the bubble survives to the absorb turn.
	w.SetHPForTest(me.EntityID, 100000)

	snap := step(t, w)

	if got, want := len(snap.Bubbles), 1; got != want {
		t.Fatalf("bubble count = %d, want %d", got, want)
	}

	if got, want := len(snap.Bubbles[0].MemberIDs), 2; got != want {
		t.Fatalf("bubble members = %d, want %d before the absorb", got, want)
	}

	beforeID := snap.Bubbles[0].ID
	if beforeID == 0 {
		t.Fatalf("bubble id = 0, want a real (non-world) id")
	}

	// Drop a second friendly one hex from the anchor: the next recompute folds it
	// into the same connected component.
	w.PlaceEntityForTest(spots[1])

	snap = step(t, w)

	if got, want := len(snap.Bubbles), 1; got != want {
		t.Fatalf("bubble count = %d, want %d after the absorb", got, want)
	}

	if got, want := len(snap.Bubbles[0].MemberIDs), 3; got != want {
		t.Fatalf("bubble members = %d, want %d (friendly must be absorbed)", got, want)
	}

	if got, want := snap.Bubbles[0].ID, beforeID; got != want {
		t.Errorf("bubble id = %d, want %d (absorbing a member must not remint the id)", got, want)
	}
}

// TestOutOfRangeStaysWorldDomain: a monster well beyond CombatRadius never
// enters combat — a single step of its chase does not close the gap, so both
// stay world-domain with no bubble on the wire.
func TestOutOfRangeStaysWorldDomain(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	me, err := w.Join("")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	// (0,9) is dist 9 from the origin spawn; one chase step leaves it at 8 > 6.
	monsterID := w.PlaceMonsterForTest(mustWalkable(t, w, protocol.Hex{Q: 0, R: 9}))

	snap := step(t, w)

	if inCombat(t, snap, me.EntityID) {
		t.Errorf("player InCombat = true, want false")
	}

	if inCombat(t, snap, monsterID) {
		t.Errorf("monster InCombat = true, want false")
	}

	if got, want := len(snap.Bubbles), 0; got != want {
		t.Errorf("bubble count = %d, want %d", got, want)
	}
}

// TestBubblesMergeWhenClustersOverlap: two opposing pairs sitting within
// CombatRadius of each other resolve to a single connected bubble containing
// all four entities, not two.
func TestBubblesMergeWhenClustersOverlap(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	me, err := w.Join("")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	m1ID := w.PlaceMonsterForTest(walkableNeighbor(t, w, me.Hex))

	// A second cluster 4 hexes west of spawn — within CombatRadius of the first,
	// so the two opposing pairs merge into one bubble.
	bHex := mustWalkable(t, w, protocol.Hex{Q: -4, R: 0})
	bID, _ := w.PlaceEntityForTest(bHex)
	m2ID := w.PlaceMonsterForTest(walkableNeighbor(t, w, bHex))

	snap := step(t, w)

	for _, id := range []int64{me.EntityID, m1ID, bID, m2ID} {
		if !inCombat(t, snap, id) {
			t.Errorf("entity %d InCombat = false, want true", id)
		}
	}

	if got, want := len(snap.Bubbles), 1; got != want {
		t.Fatalf("bubble count = %d, want %d (clusters within range must merge)", got, want)
	}

	want := []int64{me.EntityID, m1ID, bID, m2ID}
	slices.Sort(want)

	if got := snap.Bubbles[0].MemberIDs; !slices.Equal(got, want) {
		t.Errorf("merged bubble members = %v, want %v", got, want)
	}
}

// TestMonstersDoNotExtendBubbleReach: only players extend a bubble's reach. A
// bubble forms from player P and adjacent monster M1. A second monster M2 sits
// within CombatRadius of M1 but more than CombatRadius from P (the only player),
// so with monster↔monster edges dropped it is never linked in — it stays
// world-domain while P and M1 hold the bubble. Under the old any-member-extends
// rule the M1↔M2 edge would have pulled M2 into the frozen area.
func TestMonstersDoNotExtendBubbleReach(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	me, err := w.Join("")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	// Keep the anchor well-fed so a chip hit never respawns (and relocates) it.
	w.SetHPForTest(me.EntityID, 100000)

	// West axis, clear of the lake around {5,-2} (mirrors TestPlayersExtendBubbleReach;
	// the east axis threads water at ~{2,0}..{6,0} and would detour the chase). P at
	// the origin, M1 one hex west (the seed bubble). M2 starts at {-8,0}; its lone
	// chase step toward its nearest (only) player P lands it on {-7,0}: distance 6
	// from M1 (a dropped monster↔monster edge) but 7 from P, so it must stay
	// world-domain. The post-resolve distances are asserted below so the geometry
	// self-verifies and can't silently rot if the map or chase logic changes.
	m1Hex := mustWalkable(t, w, protocol.Hex{Q: -1, R: 0})
	m1ID := w.PlaceMonsterForTest(m1Hex)
	m2ID := w.PlaceMonsterForTest(mustWalkable(t, w, protocol.Hex{Q: -8, R: 0}))

	snap := step(t, w)

	// Verify the actual resolved geometry: M2 landed within CombatRadius of the
	// bubble monster M1 (the dropped edge) but beyond CombatRadius of player P.
	// If either fails the test no longer exercises the monster↔monster edge.
	m2Hex := hexOfSnap(snap, m2ID)
	if got, want := game.HexDistance(m2Hex, m1Hex), protocol.CombatRadius; got > want {
		t.Fatalf("HexDistance(M2, M1) = %d, want <= %d (M2 must be in reach of the bubble monster)", got, want)
	}

	if got, want := game.HexDistance(m2Hex, me.Hex), protocol.CombatRadius; got <= want {
		t.Fatalf("HexDistance(M2, P) = %d, want > %d (M2 must be out of reach of every player)", got, want)
	}

	if !inCombat(t, snap, me.EntityID) {
		t.Errorf("player InCombat = false, want true")
	}

	if !inCombat(t, snap, m1ID) {
		t.Errorf("bubble monster M1 InCombat = false, want true")
	}

	if inCombat(t, snap, m2ID) {
		t.Errorf("far monster M2 InCombat = true, want false (monsters must not extend the bubble)")
	}

	if got, want := len(snap.Bubbles), 1; got != want {
		t.Fatalf("bubble count = %d, want %d", got, want)
	}

	want := []int64{me.EntityID, m1ID}
	slices.Sort(want)

	if got := snap.Bubbles[0].MemberIDs; !slices.Equal(got, want) {
		t.Errorf("bubble members = %v, want %v (M2 must not be a member)", got, want)
	}
}

// TestPlayersExtendBubbleReach: the positive counterpart — players still chain
// the bubble outward. P1 + adjacent monster M1 form a bubble. A second player P2
// stands within CombatRadius of P1 (a player↔player edge), and monster M2 sits
// within CombatRadius of P2 but more than CombatRadius from P1 and M1. P2's
// player↔monster edge pulls M2 into the frozen area, proving a reinforcing
// player extends the reach to enemies no existing member could reach.
func TestPlayersExtendBubbleReach(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	p1, err := w.Join("")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.SetHPForTest(p1.EntityID, 100000)

	// West axis, clear of the lake around {5,-2}. P1 at origin, M1 one hex west
	// (the seed bubble). P2 at {-5,0} — distance 5 from P1, so a player↔player edge
	// folds it in. M2 starts at {-11,0}; its chase step toward its nearest player
	// (P2) lands it on {-10,0}: distance 5 from P2 but 10 from P1 and 9 from M1, so
	// only P2's reach can pull it in.
	m1ID := w.PlaceMonsterForTest(mustWalkable(t, w, protocol.Hex{Q: -1, R: 0}))
	p2ID, _ := w.PlaceEntityForTest(mustWalkable(t, w, protocol.Hex{Q: -5, R: 0}))
	m2ID := w.PlaceMonsterForTest(mustWalkable(t, w, protocol.Hex{Q: -11, R: 0}))

	snap := step(t, w)

	for _, id := range []int64{p1.EntityID, m1ID, p2ID, m2ID} {
		if !inCombat(t, snap, id) {
			t.Errorf("entity %d InCombat = false, want true", id)
		}
	}

	if got, want := len(snap.Bubbles), 1; got != want {
		t.Fatalf("bubble count = %d, want %d (a reinforcing player must extend the reach)", got, want)
	}

	want := []int64{p1.EntityID, m1ID, p2ID, m2ID}
	slices.Sort(want)

	if got := snap.Bubbles[0].MemberIDs; !slices.Equal(got, want) {
		t.Errorf("bubble members = %v, want %v (P2 must pull M2 in)", got, want)
	}
}

// TestBubbleDissolvesWhenMonsterDies: killing the only monster drops the
// component below an opposing pair, so the surviving player returns to the
// world domain and the bubble disappears.
func TestBubbleDissolvesWhenMonsterDies(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	me, err := w.Join("")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	monsterID := w.PlaceMonsterForTest(walkableNeighbor(t, w, me.Hex))

	// Precondition: the bubble exists before we remove the monster.
	if snap := step(t, w); !inCombat(t, snap, me.EntityID) {
		t.Fatalf("player InCombat = false before the kill, want true")
	}

	// Drop the monster to 0 HP; the next turn's death phase removes it.
	w.SetHPForTest(monsterID, 0)

	snap := step(t, w)

	if _, ok := entityOfSnap(snap, monsterID); ok {
		t.Fatalf("monster %d still present after a lethal turn", monsterID)
	}

	if inCombat(t, snap, me.EntityID) {
		t.Errorf("player InCombat = true after the monster died, want false")
	}

	if got, want := len(snap.Bubbles), 0; got != want {
		t.Errorf("bubble count = %d, want %d", got, want)
	}
}

// TestBubbleMemberEscapesWhenLeavingRange: a friendly member that walks beyond
// CombatRadius of everyone in the bubble drops back to the world domain, while
// the opposing pair it left behind keeps its bubble.
func TestBubbleMemberEscapesWhenLeavingRange(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	me, err := w.Join("")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	spots := walkableNeighborsN(t, w, me.Hex, 2)
	monsterID := w.PlaceMonsterForTest(spots[0])
	friendID, friendToken := w.PlaceEntityForTest(spots[1])

	// Keep the anchor player alive across the escape walk: the monster chases
	// (and bumps) the lowest-id player every turn, and only InCombat is asserted
	// here, not HP.
	w.SetHPForTest(me.EntityID, 1000)

	if snap := step(t, w); !inCombat(t, snap, friendID) {
		t.Fatalf("friend InCombat = false before escaping, want true")
	}

	// Send the friend far west (dist 9 from spawn), well outside CombatRadius of
	// the anchor pair near the origin.
	far := mustWalkable(t, w, protocol.Hex{Q: -9, R: 0})
	if !submitOK(w, protocol.JoinResponse{EntityID: friendID, Token: friendToken}, far) {
		t.Fatalf("SubmitIntent for the friend's escape route failed")
	}

	var snap protocol.TurnEvent

	escaped := false

	for range 25 {
		snap = step(t, w)
		if !inCombat(t, snap, friendID) {
			escaped = true

			break
		}
	}

	if !escaped {
		t.Fatalf("friend never left the bubble after walking out of range")
	}

	if !inCombat(t, snap, me.EntityID) {
		t.Errorf("anchor player InCombat = false, want true (its bubble must persist)")
	}

	if !inCombat(t, snap, monsterID) {
		t.Errorf("monster InCombat = false, want true (its bubble must persist)")
	}
}
