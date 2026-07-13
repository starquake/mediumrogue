package game_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// Bubble-clock values for tests: a long patience keeps the AFK fallback out of
// the way (tests that exercise it override via SetCombatPatienceForTest) and a
// fast poll keeps any Run loop ticking promptly.
const (
	testCombatPatience = time.Minute
	testBubblePoll     = time.Millisecond
	// testDisconnectGrace is long enough that no existing test's entity is
	// swept mid-run; disconnect-sweep behavior gets its own short-grace tests.
	testDisconnectGrace = time.Hour
)

// Shared two-character names for the identity-swap regression tests
// (goconst: keeping these as one constant each, rather than repeated
// literals, is what it takes to compare two distinct named players across
// several assertions each).
const (
	testAliceName = "alice"
	testBobName   = "bob"
)

func newWorld() *game.World {
	return game.NewWorld(time.Hour, testCombatPatience, testBubblePoll, testDisconnectGrace, 0xC0FFEE, 12, hub.New())
}

// pinToOrigin moves a freshly joined player to the origin hex and syncs the
// local JoinResponse to match. spawnHexLocked scatters joins across the whole
// sanctuary (re-derived: sanctuary scatter (fast-lane T5, Q9)), so a join is
// no longer guaranteed to land at {0,0} — but many tests build their board
// geometry against real generated terrain at fixed map coordinates and need
// the player exactly at the origin.
func pinToOrigin(w *game.World, resp *protocol.JoinResponse) {
	origin := protocol.Hex{Q: 0, R: 0}
	w.SetHexForTest(resp.EntityID, origin)
	resp.Hex = origin
}

// step drives one turn without running the ticker goroutine.
func step(t *testing.T, w *game.World) protocol.TurnEvent {
	t.Helper()
	w.ResolveTurnForTest()

	return w.Snapshot()
}

// TestNewWorldMintsDistinctWorldIDs is the fresh-boot half of item 4's
// (playtest feedback batch 3) world-reset signal: two independently
// constructed worlds (no snapshot involved — the same scenario as two
// separate `go run` boots, or a restart with SNAPSHOT_PATH unset) get
// different, non-empty worldIDs, and Snapshot() actually carries it on the
// wire. The restore-keeps-the-same-worldID half is
// TestSnapshotRoundTrip (snapshot_test.go).
func TestNewWorldMintsDistinctWorldIDs(t *testing.T) {
	t.Parallel()

	a := newWorld()
	b := newWorld()

	if got := a.WorldIDForTest(); got == "" {
		t.Error("worldID is empty")
	}

	if got, want := b.WorldIDForTest(), a.WorldIDForTest(); got == want {
		t.Errorf("two independently constructed worlds minted the same worldID %q", got)
	}

	if got, want := a.Snapshot().WorldID, a.WorldIDForTest(); got != want {
		t.Errorf("Snapshot().WorldID = %q, want %q (World.worldID)", got, want)
	}
}

func TestJoinCreatesEntityOnWalkableHex(t *testing.T) {
	t.Parallel()

	w := newWorld()

	resp, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	if resp.EntityID == 0 || resp.Token == "" {
		t.Fatalf("Join returned zero identity: %+v", resp)
	}

	snap := w.Snapshot()
	if got, want := len(snap.Entities), 1; got != want {
		t.Fatalf("entities = %d, want 1", got)
	}

	if got, want := snap.Entities[0].Hex, resp.Hex; got != want {
		t.Fatalf("snapshot hex %v != join hex %v", got, want)
	}
}

func TestJoinWithKnownTokenReturnsSameEntity(t *testing.T) {
	t.Parallel()

	w := newWorld()

	first, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	// Name, class, and species are ignored on a reclaim (known token) — empty
	// values here must still succeed and return the existing entity.
	again, err := w.Join(first.Token, "", "", "")
	if err != nil {
		t.Fatalf("re-Join: %v", err)
	}

	if got, want := again.EntityID, first.EntityID; got != want {
		t.Fatalf("re-join created a new entity: %d != %d", got, want)
	}

	if got, want := len(w.Snapshot().Entities), 1; got != want {
		t.Fatal("re-join must not create a second entity")
	}
}

func TestJoinWithUnknownTokenCreatesNewEntity(t *testing.T) {
	t.Parallel()

	w := newWorld()

	resp, err := w.Join("stale-token-from-before-a-restart", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	if resp.Token == "stale-token-from-before-a-restart" {
		t.Fatal("server must mint a fresh token, not adopt the stale one")
	}
}

func TestJoinRejectsEmptyName(t *testing.T) {
	t.Parallel()

	w := newWorld()

	_, err := w.Join("", "   ", protocol.ClassFighter, protocol.SpeciesHuman)
	if got, want := err, game.ErrInvalidName; !errors.Is(got, want) {
		t.Errorf("err = %v, want %v", got, want)
	}
}

// TestJoinRejectsOversizeName covers validName's upper-bound branch (only the
// empty/whitespace branch was previously exercised): a name longer than
// protocol.MaxNameLen runes is rejected, one exactly at the cap is kept.
func TestJoinRejectsOversizeName(t *testing.T) {
	t.Parallel()

	w := newWorld()

	over := strings.Repeat("a", protocol.MaxNameLen+1)
	if _, err := w.Join("", over, protocol.ClassFighter, protocol.SpeciesHuman); !errors.Is(err, game.ErrInvalidName) {
		t.Errorf("Join(name of %d runes) err = %v, want ErrInvalidName", protocol.MaxNameLen+1, err)
	}

	atCap := strings.Repeat("a", protocol.MaxNameLen)
	if _, err := w.Join("", atCap, protocol.ClassFighter, protocol.SpeciesHuman); err != nil {
		t.Errorf("Join(name of exactly %d runes) err = %v, want nil", protocol.MaxNameLen, err)
	}
}

// TestJoinIdentityNeverCrossesTokens pins the server-side invariant behind
// item 2's investigation (playtest feedback batch 3, "players swapped
// identities"): a live-token reclaim ALWAYS returns the entity that owns the
// exact token in the request — never a different, unrelated live entity's —
// no matter how many other characters are live at the same time. The
// client-side defect this batch found and fixed (net/session.ts's reclaim()
// used to re-read a possibly-clobbered token from localStorage instead of
// the caller's own known token) could only ever manifest if this
// server-side mapping were unreliable; it isn't, but the seam is real
// enough to deserve a permanent regression test — see the client-side fix's
// doc comments for the full
// story. Mirrors the reclaim contract the client actually uses: empty name/
// class/species alongside a known token.
func TestJoinIdentityNeverCrossesTokens(t *testing.T) {
	t.Parallel()

	w := newWorld()

	alice, err := w.Join("", testAliceName, protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("join alice: %v", err)
	}

	bob, err := w.Join("", testBobName, protocol.ClassRogue, protocol.SpeciesElf)
	if err != nil {
		t.Fatalf("join bob: %v", err)
	}

	if alice.Token == bob.Token || alice.EntityID == bob.EntityID {
		t.Fatalf("alice and bob must be distinct: %+v vs %+v", alice, bob)
	}

	// Reclaim alice's token — must always come back as alice, never bob, no
	// matter that bob joined more recently / has a higher entity id.
	reclaimedAlice, err := w.Join(alice.Token, "", "", "")
	if err != nil {
		t.Fatalf("reclaim alice: %v", err)
	}

	if got, want := reclaimedAlice.EntityID, alice.EntityID; got != want {
		t.Errorf("reclaim(alice.Token).EntityID = %d, want %d (alice)", got, want)
	}

	if got, want := reclaimedAlice.Token, alice.Token; got != want {
		t.Errorf("reclaim(alice.Token).Token = %q, want %q (alice's own)", got, want)
	}

	aliceEntity, ok := entityOfSnap(w.Snapshot(), reclaimedAlice.EntityID)
	if !ok {
		t.Fatalf("reclaimed alice entity %d not in snapshot", reclaimedAlice.EntityID)
	}

	if got, want := aliceEntity.Name, testAliceName; got != want {
		t.Errorf("reclaimed entity Name = %q, want %q — identity swap", got, want)
	}

	// Same check in the other direction — reclaiming bob's token must never
	// hand back alice's record.
	reclaimedBob, err := w.Join(bob.Token, "", "", "")
	if err != nil {
		t.Fatalf("reclaim bob: %v", err)
	}

	if got, want := reclaimedBob.EntityID, bob.EntityID; got != want {
		t.Errorf("reclaim(bob.Token).EntityID = %d, want %d (bob)", got, want)
	}

	bobEntity, ok := entityOfSnap(w.Snapshot(), reclaimedBob.EntityID)
	if !ok {
		t.Fatalf("reclaimed bob entity %d not in snapshot", reclaimedBob.EntityID)
	}

	if got, want := bobEntity.Name, testBobName; got != want {
		t.Errorf("reclaimed entity Name = %q, want %q — identity swap", got, want)
	}

	if got, want := bobEntity.Class, protocol.ClassRogue; got != want {
		t.Errorf("reclaimed bob Class = %q, want %q — identity swap", got, want)
	}
}

// TestJoinArchivedRestoreNeverCrossesTokens is the archived-restore half of
// TestJoinIdentityNeverCrossesTokens: two independently swept/archived
// characters must each restore under their own token only, even though both
// archive entries exist at once and restoreArchivedLocked mints entity ids
// from the same shared counter.
func TestJoinArchivedRestoreNeverCrossesTokens(t *testing.T) {
	t.Parallel()

	w, clk := newTimedWorld(t)
	w.SetDisconnectGraceForTest(archiveGrace)

	alice, err := w.Join("", testAliceName, protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("join alice: %v", err)
	}

	bob, err := w.Join("", testBobName, protocol.ClassRogue, protocol.SpeciesElf)
	if err != nil {
		t.Fatalf("join bob: %v", err)
	}

	// Both go quiet and get swept/archived together.
	w.StreamOpened(alice.Token)
	w.StreamClosed(alice.Token)
	w.StreamOpened(bob.Token)
	w.StreamClosed(bob.Token)
	clk.advance(archiveGrace + time.Second)

	if got, want := w.SweepForTest(clk.now()), true; got != want {
		t.Fatalf("SweepForTest removed = %v, want %v", got, want)
	}

	if !w.ArchivedForTest(alice.Token) || !w.ArchivedForTest(bob.Token) {
		t.Fatalf("both tokens must be archived after sweep")
	}

	// Restore bob FIRST (opposite order from how they joined), then alice —
	// each must come back as itself.
	restoredBob, err := w.Join(bob.Token, "", "", "")
	if err != nil {
		t.Fatalf("restore bob: %v", err)
	}

	bobEntity, ok := entityOfSnap(w.Snapshot(), restoredBob.EntityID)
	if !ok {
		t.Fatalf("restored bob entity %d not in snapshot", restoredBob.EntityID)
	}

	if got, want := bobEntity.Name, testBobName; got != want {
		t.Errorf("restored Name = %q, want %q — identity swap", got, want)
	}

	restoredAlice, err := w.Join(alice.Token, "", "", "")
	if err != nil {
		t.Fatalf("restore alice: %v", err)
	}

	if got, want := restoredAlice.Token, alice.Token; got != want {
		t.Errorf("restored alice Token = %q, want %q (own token)", got, want)
	}

	aliceEntity, ok := entityOfSnap(w.Snapshot(), restoredAlice.EntityID)
	if !ok {
		t.Fatalf("restored alice entity %d not in snapshot", restoredAlice.EntityID)
	}

	if got, want := aliceEntity.Name, testAliceName; got != want {
		t.Errorf("restored Name = %q, want %q — identity swap", got, want)
	}

	if got, want := aliceEntity.Class, protocol.ClassFighter; got != want {
		t.Errorf("restored alice Class = %q, want %q — identity swap", got, want)
	}
}

func TestSenderForReturnsNameAndHex(t *testing.T) {
	t.Parallel()

	w := newWorld()

	resp, err := w.Join("", "alice", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("join: %v", err)
	}

	name, hexPos, ok := w.SenderFor(resp.Token)
	if !ok {
		t.Fatal("SenderFor(valid token) ok = false")
	}

	if got, want := name, "alice"; got != want {
		t.Errorf("name = %q, want %q", got, want)
	}

	if got, want := hexPos, resp.Hex; got != want {
		t.Errorf("hex = %v, want %v", got, want)
	}

	if _, _, ok := w.SenderFor("nope"); ok {
		t.Error("SenderFor(unknown) ok = true, want false")
	}
}

func TestIntentMovesEntityOnResolve(t *testing.T) {
	t.Parallel()

	w := newWorld()
	me, _ := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)

	target := walkableNeighbor(t, w, me.Hex)

	req := protocol.IntentRequest{Kind: protocol.IntentMove, EntityID: me.EntityID, Token: me.Token, Target: target}
	if err := w.SubmitIntent(req); err != nil {
		t.Fatalf("SubmitIntent: %v", err)
	}

	// Not moved yet: intents apply at the tick, never immediately.
	if got, want := w.Snapshot().Entities[0].Hex, me.Hex; got != want {
		t.Fatalf("entity moved before resolve: %v", got)
	}

	snap := step(t, w)
	if got, want := snap.Entities[0].Hex, target; got != want {
		t.Fatalf("after resolve: hex = %v, want %v", got, want)
	}

	if got, want := snap.Turn, int64(1); got != want {
		t.Fatalf("turn = %d, want 1", got)
	}
}

func TestLatestIntentWins(t *testing.T) {
	t.Parallel()

	w := newWorld()
	me, _ := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)

	first := walkableNeighbor(t, w, me.Hex)

	var second protocol.Hex

	for _, n := range game.HexNeighbors(me.Hex) {
		if n != first && submitOK(w, me, n) {
			second = n

			break
		}
	}

	if second == (protocol.Hex{}) {
		t.Skip("spawn has only one walkable neighbor; cannot exercise latest-wins here")
	}

	// second was submitted last (submitOK submits), so it must win.
	snap := step(t, w)
	if got, want := snap.Entities[0].Hex, second; got != want {
		t.Fatalf("hex = %v, want the latest intent target %v", got, want)
	}
}

func TestIntentValidation(t *testing.T) {
	t.Parallel()

	w := newWorld()
	me, _ := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	target := walkableNeighbor(t, w, me.Hex)

	cases := []struct {
		name string
		req  protocol.IntentRequest
		want error
	}{
		{
			"bad token",
			protocol.IntentRequest{Kind: protocol.IntentMove, EntityID: me.EntityID, Token: "wrong", Target: target},
			game.ErrUnauthorized,
		},
		{
			"unknown entity",
			protocol.IntentRequest{Kind: protocol.IntentMove, EntityID: 999, Token: me.Token, Target: target},
			game.ErrUnauthorized,
		},
		{
			"empty kind",
			protocol.IntentRequest{EntityID: me.EntityID, Token: me.Token, Target: target},
			game.ErrInvalidIntentKind,
		},
		{
			"unknown kind",
			protocol.IntentRequest{Kind: "teleport", EntityID: me.EntityID, Token: me.Token, Target: target},
			game.ErrInvalidIntentKind,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got, want := w.SubmitIntent(tc.req), tc.want; !errors.Is(got, want) {
				t.Fatalf("err = %v, want %v", got, want)
			}
		})
	}
}

func TestIntentRejectsUnwalkableTarget(t *testing.T) {
	t.Parallel()

	w := newWorld()
	me, _ := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)

	// Find an adjacent unwalkable hex if the spawn has one; otherwise walk a
	// probe entity to the lake shore... which milestone 3 cannot do without
	// pathfinding, so settle for the direct check against the map.
	for _, n := range game.HexNeighbors(me.Hex) {
		if !isWalkable(w, n) {
			req := protocol.IntentRequest{Kind: protocol.IntentMove, EntityID: me.EntityID, Token: me.Token, Target: n}
			if got, want := w.SubmitIntent(req), game.ErrNotWalkable; !errors.Is(got, want) {
				t.Fatalf("err = %v, want ErrNotWalkable", got)
			}

			return
		}
	}

	t.Skip("spawn has no unwalkable neighbor on this map")
}

// walkableNeighbor returns the first neighbor a fresh entity can step to.
func walkableNeighbor(t *testing.T, w *game.World, from protocol.Hex) protocol.Hex {
	t.Helper()

	for _, n := range game.HexNeighbors(from) {
		if isWalkable(w, n) {
			return n
		}
	}

	t.Fatalf("no walkable neighbor around %v", from)

	return protocol.Hex{}
}

func isWalkable(w *game.World, h protocol.Hex) bool {
	for _, tile := range w.Map().Tiles {
		if tile.Hex == h {
			return tile.Terrain == protocol.TerrainGrass || tile.Terrain == protocol.TerrainForest
		}
	}

	return false
}

// terrainOf scans w's map for h's tile and returns its terrain, failing the
// test if h is off the map entirely.
func terrainOf(t *testing.T, w *game.World, h protocol.Hex) protocol.Terrain {
	t.Helper()

	for _, tile := range w.Map().Tiles {
		if tile.Hex == h {
			return tile.Terrain
		}
	}

	t.Fatalf("hex %v is off the generated map", h)

	return ""
}

// TestSpawnsLandInWalkableRegion: many joins on a large generated map must all
// land on grass or forest — spawnHexLocked restricts to w.spawnable (the
// origin-reachable walkable region), so a spawn can never land on an isolated
// walkable pocket the origin can't reach, nor on water/rock.
func TestSpawnsLandInWalkableRegion(t *testing.T) {
	t.Parallel()

	w := game.NewWorld(
		time.Millisecond, testCombatPatience, testBubblePoll, testDisconnectGrace, 0xC0FFEE, 24, hub.New(),
	)

	// Many joins must all land on reachable, walkable tiles.
	for i := range 30 {
		e, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
		if err != nil {
			t.Fatalf("join %d: %v", i, err)
		}

		if terr := terrainOf(t, w, e.Hex); terr != protocol.TerrainGrass && terr != protocol.TerrainForest {
			t.Errorf("join %d landed on %q at %v, want grass or forest", i, terr, e.Hex)
		}
	}
}

func submitOK(w *game.World, me protocol.JoinResponse, target protocol.Hex) bool {
	req := protocol.IntentRequest{Kind: protocol.IntentMove, EntityID: me.EntityID, Token: me.Token, Target: target}

	return w.SubmitIntent(req) == nil
}

func TestIntentWalksMultiStepPath(t *testing.T) {
	t.Parallel()

	w := newWorld()
	me, _ := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)

	// A destination two hexes away: a walkable neighbor of a walkable neighbor
	// that sits at distance 2 from spawn (geometry-independent discovery).
	n1 := walkableNeighbor(t, w, me.Hex)

	var dest protocol.Hex

	for _, n2 := range game.HexNeighbors(n1) {
		if n2 != me.Hex && game.HexDistance(me.Hex, n2) == 2 && isWalkable(w, n2) {
			dest = n2

			break
		}
	}

	if dest == (protocol.Hex{}) {
		t.Skip("spawn has no reachable distance-2 hex on this map")
	}

	req := protocol.IntentRequest{Kind: protocol.IntentMove, EntityID: me.EntityID, Token: me.Token, Target: dest}
	if err := w.SubmitIntent(req); err != nil {
		t.Fatalf("SubmitIntent: %v", err)
	}

	// One hex per turn: after the first turn the entity is adjacent to spawn,
	// not yet at the destination.
	snap := step(t, w)
	mid := snap.Entities[0].Hex

	if got, want := game.HexDistance(me.Hex, mid), 1; got != want {
		t.Fatalf("after turn 1: hex %v is not one step from spawn %v", mid, me.Hex)
	}

	if mid == dest {
		t.Fatal("reached a distance-2 destination in a single turn")
	}

	// The second turn arrives.
	snap = step(t, w)
	if got, want := snap.Entities[0].Hex, dest; got != want {
		t.Fatalf("after turn 2: hex = %v, want destination %v", got, want)
	}
}

func TestSnapshotCarriesInterval(t *testing.T) {
	t.Parallel()

	w := game.NewWorld(
		250*time.Millisecond, testCombatPatience, testBubblePoll, testDisconnectGrace, 0xC0FFEE, 12, hub.New(),
	)
	if got, want := w.Snapshot().IntervalMs, int64(250); got != want {
		t.Fatalf("IntervalMs = %d, want 250", got)
	}
}

// TestFriendlyStackingConverges: two entities on neighbouring hexes both step
// onto one shared hex in a single turn and stack — friendly stacking under the
// StackCap, resolved simultaneously.
func TestFriendlyStackingConverges(t *testing.T) {
	t.Parallel()

	w := newWorld()

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	ns := game.HexNeighbors(center)

	idA, tokA := w.PlaceEntityForTest(ns[0])
	idB, tokB := w.PlaceEntityForTest(ns[1])

	mustSubmit(t, w, idA, tokA, center)
	mustSubmit(t, w, idB, tokB, center)

	w.ResolveTurnForTest()

	snap := w.Snapshot()
	if got, want := hexOfSnap(snap, idA), center; got != want {
		t.Errorf("A at %v, want center %v", got, want)
	}

	if got, want := hexOfSnap(snap, idB), center; got != want {
		t.Errorf("B at %v, want center %v", got, want)
	}

	if got, want := countAt(snap, center), 2; got != want {
		t.Errorf("center occupancy = %d, want 2", got)
	}
}

// TestStackCapBlocksOverflow: a hex already full at StackCap does not admit one
// more mover — the overflow entity stays put and the hex still holds exactly
// StackCap. Asserts the invariant, not which entity won a tie-break.
func TestStackCapBlocksOverflow(t *testing.T) {
	t.Parallel()

	w := newWorld()

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	ns := game.HexNeighbors(center)

	for range protocol.StackCap {
		w.PlaceEntityForTest(center)
	}

	sixth, tok := w.PlaceEntityForTest(ns[0])
	mustSubmit(t, w, sixth, tok, center)

	w.ResolveTurnForTest()

	snap := w.Snapshot()
	if got := hexOfSnap(snap, sixth); got == center {
		t.Errorf("overflow entity entered a full hex; want it blocked at %v", ns[0])
	}

	if got, want := countAt(snap, center), protocol.StackCap; got != want {
		t.Errorf("center occupancy = %d, want StackCap %d", got, want)
	}
}

func mustSubmit(t *testing.T, w *game.World, id int64, token string, target protocol.Hex) {
	t.Helper()

	req := protocol.IntentRequest{Kind: protocol.IntentMove, EntityID: id, Token: token, Target: target}
	if err := w.SubmitIntent(req); err != nil {
		t.Fatalf("SubmitIntent(%d -> %v): %v", id, target, err)
	}
}

func hexOfSnap(snap protocol.TurnEvent, id int64) protocol.Hex {
	for _, e := range snap.Entities {
		if e.ID == id {
			return e.Hex
		}
	}

	return protocol.Hex{Q: -999, R: -999}
}

func countAt(snap protocol.TurnEvent, hex protocol.Hex) int {
	n := 0

	for _, e := range snap.Entities {
		if e.Hex == hex {
			n++
		}
	}

	return n
}

// placeSixMoversIntoCenter places one entity on each of the six neighbours of
// the origin, each queued to step onto the origin. With StackCap = 5 exactly
// one loses the seeded overflow tie-break. Returns the entity ids in placement
// (ascending-id) order.
func placeSixMoversIntoCenter(t *testing.T, w *game.World, center protocol.Hex) []int64 {
	t.Helper()

	ids := make([]int64, 0, 6)

	for _, n := range game.HexNeighbors(center) {
		id, tok := w.PlaceEntityForTest(n)
		mustSubmit(t, w, id, tok, center)
		ids = append(ids, id)
	}

	return ids
}

// blockedMover returns the id of the entity NOT on center after one resolve, or
// 0 if none (should not happen in a 6-into-5 overflow).
func blockedMover(snap protocol.TurnEvent, ids []int64, center protocol.Hex) int64 {
	for _, id := range ids {
		if hexOfSnap(snap, id) != center {
			return id
		}
	}

	return 0
}

func TestPhasedOverflowAdmitsStackCapBlocksOne(t *testing.T) {
	t.Parallel()

	w := newWorld()

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	ids := placeSixMoversIntoCenter(t, w, center)
	w.ResolveTurnForTest()
	snap := w.Snapshot()

	if got, want := countAt(snap, center), protocol.StackCap; got != want {
		t.Fatalf("center occupancy = %d, want StackCap %d", got, want)
	}

	if got := blockedMover(snap, ids, center); got == 0 {
		t.Fatal("expected exactly one blocked mover, found none")
	}
}

func TestPhasedTieBreakIsReproducible(t *testing.T) {
	t.Parallel()

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(newWorld(), center) {
		t.Skip("origin is not walkable on this map")
	}

	blockedFor := func(seed int64) int64 {
		w := newWorld()
		w.SetSeedForTest(seed)
		ids := placeSixMoversIntoCenter(t, w, center)
		w.ResolveTurnForTest()

		return blockedMover(w.Snapshot(), ids, center)
	}

	// Same seed + same board → same tie-break outcome.
	if got, want := blockedFor(42), blockedFor(42); got != want {
		t.Fatalf("same seed produced different blocked entities: %d vs %d", got, want)
	}
}

func TestPhasedTieBreakIsNotIDFavoritism(t *testing.T) {
	t.Parallel()

	// The old placeholder always blocked the highest id (low ids moved first and
	// filled the hex). Under the seeded shuffle the loser must depend on the
	// seed — assert at least one seed (of 20) blocks a non-max id. Deterministic
	// (fixed seeds), not flaky: P(all 20 block the max) = (1/6)^20 ≈ 0.
	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(newWorld(), center) {
		t.Skip("origin is not walkable on this map")
	}

	var maxID int64

	blockedNonMax := false

	for seed := range int64(20) {
		w := newWorld()
		w.SetSeedForTest(seed)
		ids := placeSixMoversIntoCenter(t, w, center)
		maxID = ids[len(ids)-1] // PlaceEntityForTest assigns ascending ids

		w.ResolveTurnForTest()

		if b := blockedMover(w.Snapshot(), ids, center); b != 0 && b != maxID {
			blockedNonMax = true

			break
		}
	}

	if !blockedNonMax {
		t.Fatalf("across 20 seeds the blocked mover was always the max id %d — no seeded tie-break", maxID)
	}
}
