package game_test

import (
	"slices"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// leash_test.go: the monster home tile + leash (#102). All geometry below is
// built from the rat kind, whose small aggro radius (CombatRadius+1) gives
// the smallest leash in the registry — the only default leash a fixed-seed
// generated map can comfortably stage displacements beyond.

// newLeashWorld builds a world like newWorld but with a larger radius (20),
// so a beyond-leash displacement (rat leash = MonsterLeashMultiplier ×
// (CombatRadius+1) = 14 hexes) plus a margin fits inside the walkable map —
// a radius-12 map's origin-anchored geometry tops out around 12 hexes.
func newLeashWorld(t *testing.T) *game.World {
	t.Helper()

	return game.NewWorld(time.Hour, testCombatPatience, testBubblePoll, testDisconnectGrace, 0xC0FFEE, 20, hub.New())
}

// ratLeash returns the rat kind's effective leash radius, derived from the
// registry (never duplicated inline) via the LeashRadiusForTest bridge.
func ratLeash() int {
	return game.LeashRadiusForTest("rat")
}

// walkableHexAwayFrom picks the lowest-(Q,R) reachable walkable hex at
// exactly dist hexes from `from` that lies directly AWAY from `avoid`
// (HexDistance(avoid, h) == HexDistance(avoid, from) + dist), so a first
// step toward the returned hex can never also be a step toward `avoid` —
// the disambiguation the ignore-players assertions need.
func walkableHexAwayFrom(t *testing.T, w *game.World, from, avoid protocol.Hex, dist int) protocol.Hex {
	t.Helper()

	var candidates []protocol.Hex

	for h := range game.ReachableWalkableForTest(w.Map()) {
		if game.HexDistance(from, h) == dist && game.HexDistance(avoid, h) == game.HexDistance(avoid, from)+dist {
			candidates = append(candidates, h)
		}
	}

	if len(candidates) == 0 {
		t.Fatalf("no reachable walkable hex at distance %d from %v directly away from %v", dist, from, avoid)
	}

	slices.SortFunc(candidates, func(a, b protocol.Hex) int {
		if a.Q != b.Q {
			return a.Q - b.Q
		}

		return a.R - b.R
	})

	return candidates[0]
}

// TestMonsterBeyondLeashWalksHome (#102): a WORLD-domain monster farther
// from its home hex than its leash radius stops standing around (the
// pre-#102 no-target behavior) and paths back home, flagged as returning.
func TestMonsterBeyondLeashWalksHome(t *testing.T) {
	t.Parallel()

	w := newLeashWorld(t)

	me := joinNamed(t, w, "tester")
	pinToOrigin(w, &me)

	// The rat sits outside its own aggro radius of the player, so the ONLY
	// behavior change under test is stand-still → walk-home.
	ratHex := walkableHexAtDistance(t, w, me.Hex, game.MonsterAggroRadiusForTest("rat")+1, ratLeash()-1)
	ratID := w.PlaceMonsterKindForTest(ratHex, "rat")

	home := walkableHexAtDistance(t, w, ratHex, ratLeash()+1, ratLeash()+2)
	w.SetMonsterHomeForTest(ratID, home)

	if got, want := w.MonsterReturningForTest(ratID), false; got != want {
		t.Fatalf("precondition: MonsterReturning = %v, want %v", got, want)
	}

	step(t, w)

	if got, want := w.MonsterReturningForTest(ratID), true; got != want {
		t.Errorf("MonsterReturning after a beyond-leash turn = %v, want %v", got, want)
	}

	after := entityHex(t, w, ratID)
	if after == ratHex {
		t.Fatalf("rat stood still at %v, want a step toward home %v", ratHex, home)
	}

	if got, want := game.HexDistance(after, home), game.HexDistance(ratHex, home)-1; got != want {
		t.Errorf("distance to home after one turn = %d, want %d (one step closer)", got, want)
	}
}

// TestReturningMonsterIgnoresPlayers (#102): a monster walking home does not
// re-aggro mid-return — once it is back INSIDE its leash range (so the leash
// trigger no longer explains its behavior) with a player inside its aggro
// radius, it keeps stepping home rather than chasing.
//
// The returning state is EARNED through a real beyond-leash turn rather than
// injected, so the flag-flip and the ignore-players rule are covered by one
// unbroken sequence: the rat starts exactly one hex beyond its leash, so its
// first real leash step carries it back inside leash range — exactly the
// hysteresis case (inside range, still returning) this test is about.
func TestReturningMonsterIgnoresPlayers(t *testing.T) {
	t.Parallel()

	w := newLeashWorld(t)

	me := joinNamed(t, w, "tester")
	pinToOrigin(w, &me)

	aggro := game.MonsterAggroRadiusForTest("rat")

	// The rat starts outside its own aggro radius of the player, so turn 1
	// is a pure leash trip with no chase in the picture.
	ratHex := walkableHexAtDistance(t, w, me.Hex, aggro+1, ratLeash()-1)
	ratID := w.PlaceMonsterKindForTest(ratHex, "rat")

	// Home exactly one hex beyond the leash: one step home lands the rat
	// back inside leash range.
	home := walkableHexAtDistance(t, w, ratHex, ratLeash()+1, ratLeash()+1)
	w.SetMonsterHomeForTest(ratID, home)

	// Turn 1: the real beyond-leash trip — the rat earns its returning flag.
	step(t, w)

	if got, want := w.MonsterReturningForTest(ratID), true; got != want {
		t.Fatalf("MonsterReturning after the beyond-leash turn = %v, want %v", got, want)
	}

	midHex := entityHex(t, w, ratID)

	// Precondition for the rule under test: the rat is now back INSIDE its
	// leash range, so anything it does next is the returning flag's doing,
	// not the leash trigger's.
	if got, want := game.HexDistance(midHex, home), ratLeash(); got > want {
		t.Fatalf("distance to home after the first step = %d, want <= %d (back inside leash range)", got, want)
	}

	// Put the player exactly at the rat's aggro boundary, directly AWAY from
	// home: outside CombatRadius (no bubble), but close enough that a normal
	// think pass would chase — and a step toward home is provably not a step
	// toward the player.
	lure := walkableHexAwayFrom(t, w, midHex, home, aggro)
	w.SetHexForTest(me.EntityID, lure)

	// Turn 2: inside leash range, player in aggro range, still returning.
	step(t, w)

	after := entityHex(t, w, ratID)
	if after == midHex {
		t.Fatalf("returning rat stood still at %v, want a step toward home %v", midHex, home)
	}

	if got, want := game.HexDistance(after, home), game.HexDistance(midHex, home); got >= want {
		t.Errorf("distance to home after the lure turn = %d, want < %d (still walking home)", got, want)
	}

	// A chase step would have closed to aggro-1; ignoring the player means
	// never getting nearer than the lure distance.
	if got, want := game.HexDistance(after, lure), aggro; got < want {
		t.Errorf("returning rat approached the player: distance %d, want >= %d", got, want)
	}

	if got, want := w.MonsterReturningForTest(ratID), true; got != want {
		t.Errorf("MonsterReturning mid-return = %v, want %v (clears only on arrival)", got, want)
	}
}

// TestArrivedMonsterReaggroesNextThink (#102 edge choice): arrival clears
// the returning flag on the next think pass, and that SAME pass runs the
// normal aggro check — a player camping just outside the home hex is
// noticed immediately, not one turn late.
func TestArrivedMonsterReaggroesNextThink(t *testing.T) {
	t.Parallel()

	w := newLeashWorld(t)

	me := joinNamed(t, w, "tester")
	pinToOrigin(w, &me)

	aggro := game.MonsterAggroRadiusForTest("rat")

	// Home at exactly the rat's aggro radius from the player: outside
	// CombatRadius (no bubble on arrival), inside aggro (re-aggro applies).
	home := walkableHexAtDistance(t, w, me.Hex, aggro, aggro)

	// The rat starts one step from home, no closer to the player than home
	// is (a neighbor inside CombatRadius would bubble up instead).
	var start protocol.Hex

	found := false

	for _, n := range game.HexNeighbors(home) {
		if isWalkable(w, n) && game.HexDistance(n, me.Hex) >= aggro {
			start, found = n, true

			break
		}
	}

	if !found {
		t.Fatalf("no walkable neighbor of home %v at distance >= %d from the player", home, aggro)
	}

	// Re-aggro needs SIGHT as well as range since #95, and by the re-aggro
	// turn the rat is standing on HOME (it walks there on turn 1) — so that
	// is the line that has to be clear. This test is about the leash flag
	// clearing, not terrain.
	clearSightLine(t, w, me.Hex, home)
	ratID := w.PlaceMonsterKindForTest(start, "rat")
	w.SetMonsterHomeForTest(ratID, home)
	w.SetMonsterReturningForTest(ratID, true)

	// Turn 1: the final step home. The flag clears on the NEXT think pass,
	// so it must still be set right after the arrival move.
	step(t, w)

	if got, want := entityHex(t, w, ratID), home; got != want {
		t.Fatalf("rat hex after the arrival turn = %v, want home %v", got, want)
	}

	if got, want := w.MonsterReturningForTest(ratID), true; got != want {
		t.Errorf("MonsterReturning right after the arrival move = %v, want %v (clears on the next think)", got, want)
	}

	// Turn 2: think detects arrival, clears the flag, and the same pass
	// re-runs the aggro check — the player at exactly aggro range is
	// noticed, so the rat steps toward it.
	step(t, w)

	if got, want := w.MonsterReturningForTest(ratID), false; got != want {
		t.Errorf("MonsterReturning after the post-arrival think = %v, want %v", got, want)
	}

	if got, want := game.HexDistance(entityHex(t, w, ratID), me.Hex), aggro; got >= want {
		t.Errorf("distance to player after re-aggro turn = %d, want < %d (chasing again)", got, want)
	}
}

// TestReturningMonsterArrivesAdjacentWhenHomeFull (#102 edge choice): a home
// hex at StackCap counts as arrived from an adjacent hex — the flag clears
// instead of leaving the monster waiting one hex away, passive forever.
func TestReturningMonsterArrivesAdjacentWhenHomeFull(t *testing.T) {
	t.Parallel()

	w := newLeashWorld(t)

	home := walkableHexAtDistance(t, w, protocol.Hex{}, 4, 8)
	for range protocol.StackCap {
		w.PlaceMonsterKindForTest(home, "wolf")
	}

	start := walkableNeighbor(t, w, home)
	ratID := w.PlaceMonsterKindForTest(start, "rat")
	w.SetMonsterHomeForTest(ratID, home)
	w.SetMonsterReturningForTest(ratID, true)

	step(t, w)

	if got, want := w.MonsterReturningForTest(ratID), false; got != want {
		t.Errorf("MonsterReturning adjacent to a full home = %v, want %v (adjacent-and-full counts as arrived)", got, want)
	}

	if got, want := entityHex(t, w, ratID), start; got != want {
		t.Errorf("rat hex = %v, want %v (no room on home, stays adjacent)", got, want)
	}
}

// TestBubbleMonsterIgnoresLeashAndResumesReturnAfter (#102): the leash binds
// WORLD-domain monsters only. A bubbled monster far beyond its leash keeps
// fighting unconditionally; once its bubble dissolves it re-enters the world
// domain, where the leash check applies from there — it drops the chase and
// walks home.
func TestBubbleMonsterIgnoresLeashAndResumesReturnAfter(t *testing.T) {
	t.Parallel()

	w := newLeashWorld(t)

	me := joinNamed(t, w, "tester")
	pinToOrigin(w, &me)

	ratHex := walkableNeighbor(t, w, me.Hex)
	ratID := w.PlaceMonsterKindForTest(ratHex, "rat")

	hpOf := func() int {
		e, ok := entityOfSnap(w.Snapshot(), me.EntityID)
		if !ok {
			t.Fatalf("player %d not in snapshot", me.EntityID)
		}

		return e.HP
	}

	hp0 := hpOf()

	// Turn 1: the adjacent rat attacks (world-domain melee conversion) and
	// the end-of-turn recompute forms the bubble.
	step(t, w)

	if hp1 := hpOf(); hp1 >= hp0 {
		t.Fatalf("player HP %d -> %d over the forming turn, want a hit landed", hp0, hp1)
	}

	// Displace the rat's home far beyond its leash. Being bubbled, it must
	// keep fighting anyway.
	home := walkableHexAtDistance(t, w, ratHex, ratLeash()+3, ratLeash()+4)
	w.SetMonsterHomeForTest(ratID, home)

	hp1 := hpOf()

	step(t, w)

	if hp2 := hpOf(); hp2 >= hp1 {
		t.Errorf("player HP %d -> %d over a bubble turn, want the beyond-leash rat to keep attacking", hp1, hp2)
	}

	if got, want := w.MonsterReturningForTest(ratID), false; got != want {
		t.Errorf("MonsterReturning inside a bubble = %v, want %v (bubbles ignore the leash)", got, want)
	}

	// Teleport the player far away: the next turn's end recompute dissolves
	// the bubble (no opposing pair within CombatRadius), returning the rat
	// to the world domain.
	aggro := game.MonsterAggroRadiusForTest("rat")
	away := walkableHexAtDistance(t, w, ratHex, aggro+4, aggro+6)
	w.SetHexForTest(me.EntityID, away)
	step(t, w)

	// World-domain turn: the leash check applies from here — the rat flips
	// to returning and steps toward home.
	before := entityHex(t, w, ratID)

	step(t, w)

	if got, want := w.MonsterReturningForTest(ratID), true; got != want {
		t.Errorf("MonsterReturning after the bubble dissolved = %v, want %v (leash applies in the world domain)", got, want)
	}

	after := entityHex(t, w, ratID)
	if got, want := game.HexDistance(after, home), game.HexDistance(before, home); got >= want {
		t.Errorf("distance to home after the post-bubble turn = %d, want < %d (walking home)", got, want)
	}
}

// TestSpawnPathsStampHome (#102): every monster spawn path stamps the
// entity's home tile to its spawn hex — the seeded ring spawner, the
// fixed-hex spawner, and the test bridge.
func TestSpawnPathsStampHome(t *testing.T) {
	t.Parallel()

	w := newLeashWorld(t)

	w.SpawnMonsters(8)

	monsters := 0

	for _, e := range w.Snapshot().Entities {
		if e.Kind != protocol.EntityMonster {
			continue
		}

		monsters++

		if got, want := w.MonsterHomeForTest(e.ID), e.Hex; got != want {
			t.Errorf("SpawnMonsters: monster %d home = %v, want its spawn hex %v", e.ID, got, want)
		}
	}

	if monsters == 0 {
		t.Fatal("SpawnMonsters placed no monsters")
	}

	at := walkableHexAtDistance(t, w, protocol.Hex{}, 3, 6)
	if !w.SpawnMonsterKindAt(at, "wolf") {
		t.Fatalf("SpawnMonsterKindAt(%v) refused", at)
	}

	for _, e := range w.Snapshot().Entities {
		if e.Kind == protocol.EntityMonster && e.Hex == at {
			if got, want := w.MonsterHomeForTest(e.ID), at; got != want {
				t.Errorf("SpawnMonsterKindAt: home = %v, want %v", got, want)
			}
		}
	}

	placed := walkableHexAtDistance(t, w, protocol.Hex{}, 7, 9)
	placedID := w.PlaceMonsterKindForTest(placed, "rat")

	if got, want := w.MonsterHomeForTest(placedID), placed; got != want {
		t.Errorf("PlaceMonsterKindForTest: home = %v, want %v", got, want)
	}
}

// TestSnapshotRoundTripLeashState (#102): a monster's home tile and
// returning flag are multi-turn behavioral state, not per-turn transients —
// both survive a marshal/restore round trip.
func TestSnapshotRoundTripLeashState(t *testing.T) {
	t.Parallel()

	w, _ := newSnapshotWorld(t)

	ratID := w.PlaceMonsterKindForTest(protocol.Hex{Q: 3, R: -2}, "rat")
	home := protocol.Hex{Q: -4, R: 1}
	w.SetMonsterHomeForTest(ratID, home)
	w.SetMonsterReturningForTest(ratID, true)

	data, err := w.MarshalState()
	if err != nil {
		t.Fatalf("MarshalState: %v", err)
	}

	w2, _ := newSnapshotWorld(t)
	if err := w2.RestoreState(data); err != nil {
		t.Fatalf("RestoreState: %v", err)
	}

	if got, want := w2.MonsterHomeForTest(ratID), home; got != want {
		t.Errorf("restored monster home = %v, want %v", got, want)
	}

	if got, want := w2.MonsterReturningForTest(ratID), true; got != want {
		t.Errorf("restored MonsterReturning = %v, want %v", got, want)
	}
}
