package game_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
)

const (
	snapTestSeed   = 0xC0FFEE
	snapTestRadius = 12
)

// newSnapshotWorld builds a world with the fixed seed/radius the snapshot
// tests share, a fast interval, and a fake clock so tests control time.
func newSnapshotWorld(t *testing.T) (*game.World, *fakeClock) {
	t.Helper()

	w := game.NewWorld(
		time.Second, testCombatPatience, testBubblePoll, testDisconnectGrace,
		snapTestSeed, snapTestRadius, hub.New(),
	)
	clk := &fakeClock{t: time.Unix(2_000_000, 0)}
	w.SetNowForTest(clk.now)
	w.StartClockForTest()
	w.SetSeedForTest(1)

	return w, clk
}

// TestSnapshotRoundTrip: marshaling a world with a player (XP, gear beyond
// class defaults), a monster, a ground item, a taken/progressed quest, and an
// archived character, then restoring into a fresh same-seed/radius world
// reproduces every one of those exactly.
func TestSnapshotRoundTrip(t *testing.T) {
	t.Parallel()

	w, clk := newSnapshotWorld(t)

	alice, err := w.Join("", testAliceName, protocol.ClassRogue, protocol.SpeciesElf)
	if err != nil {
		t.Fatalf("Join alice: %v", err)
	}

	// Keep alice out of the disconnect sweep below (which shrinks the grace
	// to catch bob): an open stream, unlike bob's, keeps her live.
	w.StreamOpened(alice.Token)

	w.SetXPForTest(alice.EntityID, 2*protocol.XPCurveBase+15)

	extraItem := w.GrantItemForTest(alice.EntityID, "butchers-cleaver")
	wantClose, wantRanged := w.EquippedSlotsForTest(alice.EntityID)

	monsterID := w.PlaceMonsterKindForTest(protocol.Hex{Q: 3, R: -2}, "wolf")

	groundHex := protocol.Hex{Q: 1, R: 1}
	groundInst := w.GroundItemForTest(groundHex, "dagger")

	if _, err := w.QuestTake(alice.Token, 1); err != nil {
		t.Fatalf("QuestTake: %v", err)
	}

	// Archive a second character (bob) via the sweep, so the archive map
	// itself round-trips too.
	bob, err := w.Join("", "bob", protocol.ClassFighter, protocol.SpeciesDwarf)
	if err != nil {
		t.Fatalf("Join bob: %v", err)
	}

	w.StreamOpened(bob.Token)
	w.StreamClosed(bob.Token)
	w.SetDisconnectGraceForTest(time.Second)
	clk.advance(2 * time.Second)
	w.SweepForTest(clk.now())

	// Advance the turn a few times so turn monotonicity has something to
	// pin (see TestSnapshotRestoreTurnMonotonic below for the full check).
	w.ResolveTurnForTest()
	w.ResolveTurnForTest()

	beforeTurn := w.Snapshot().Turn

	beforeAlice, ok := entityOfSnap(w.Snapshot(), alice.EntityID)
	if !ok {
		t.Fatalf("alice missing from pre-marshal snapshot")
	}

	data, err := w.MarshalState()
	if err != nil {
		t.Fatalf("MarshalState: %v", err)
	}

	w2, _ := newSnapshotWorld(t)

	if got, want := w2.WorldIDForTest(), w.WorldIDForTest(); got == want {
		t.Fatalf("w2's freshly-minted worldID already equals w's before restoring — test fixture bug")
	}

	if err := w2.RestoreState(data); err != nil {
		t.Fatalf("RestoreState: %v", err)
	}

	// item 4 (playtest feedback batch 3): a restored world is the SAME
	// world — it keeps w's original worldID, not a freshly-minted one.
	if got, want := w2.WorldIDForTest(), w.WorldIDForTest(); got != want {
		t.Errorf("restored worldID = %q, want %q (w's own, unchanged)", got, want)
	}

	if got := w2.Snapshot().WorldID; got == "" {
		t.Error("restored TurnEvent.WorldID is empty")
	}

	if got, want := w2.Snapshot().Turn, beforeTurn; got != want {
		t.Errorf("restored Turn = %d, want %d", got, want)
	}

	checkRestoredAlice(t, w2, alice, beforeAlice, extraItem, wantClose, wantRanged)
	checkRestoredMonsterAndGround(t, w2, monsterID, groundInst, groundHex)
	checkRestoredQuestAndArchive(t, w2, alice.EntityID, bob.Token)
}

// checkRestoredAlice asserts the restored player's identity, progression,
// HP/MaxHP, equipped slots, and owned items — split out of
// TestSnapshotRoundTrip to keep it under the complexity linter's threshold.
func checkRestoredAlice(
	t *testing.T, w2 *game.World, alice protocol.JoinResponse, beforeAlice protocol.Entity,
	extraItem, wantClose, wantRanged int64,
) {
	t.Helper()

	restored, ok := entityOfSnap(w2.Snapshot(), alice.EntityID)
	if !ok {
		t.Fatalf("restored alice (id %d) missing from snapshot", alice.EntityID)
	}

	if got, want := restored.Hex, alice.Hex; got != want {
		t.Errorf("restored alice Hex = %v, want %v", got, want)
	}

	if got, want := restored.Name, testAliceName; got != want {
		t.Errorf("restored alice Name = %q, want %q", got, want)
	}

	if got, want := restored.Class, protocol.ClassRogue; got != want {
		t.Errorf("restored alice Class = %q, want %q", got, want)
	}

	if got, want := restored.Species, protocol.SpeciesElf; got != want {
		t.Errorf("restored alice Species = %q, want %q", got, want)
	}

	if got, want := restored.XP, 2*protocol.XPCurveBase+15; got != want {
		t.Errorf("restored alice XP = %d, want %d", got, want)
	}

	// HP is exactly the marshaled value (never healed by a restore) — alice
	// never took damage, so it's still her join-time full bar.
	if got, want := restored.HP, beforeAlice.HP; got != want {
		t.Errorf("restored alice HP = %d, want %d (exactly the marshaled value)", got, want)
	}

	// MaxHP is RECOMPUTED at restore (fast-lane T2), not the marshaled value:
	// alice's XP (2*XPCurveBase+15) was set directly via SetXPForTest, which
	// bypasses the earn-xp sync, so her pre-marshal MaxHP is still the stale
	// level-1 bar — restore must resolve it to her actual level (2).
	// re-derived for front-loaded HP curve (fast-lane T2)
	if got, want := restored.MaxHP, game.MaxHPForTest(protocol.ClassRogue, 2); got != want {
		t.Errorf("restored alice MaxHP = %d, want %d (recomputed from class+XP)", got, want)
	}

	gotClose, gotRanged := w2.EquippedSlotsForTest(alice.EntityID)
	if gotClose != wantClose || gotRanged != wantRanged {
		t.Errorf("restored alice equipped slots = (%d, %d), want (%d, %d)", gotClose, gotRanged, wantClose, wantRanged)
	}

	for _, it := range restored.Items {
		if it.ID == extraItem {
			return
		}
	}

	t.Errorf("restored alice items missing granted instance %d", extraItem)
}

// checkRestoredMonsterAndGround asserts the restored monster's kind/HP and
// the restored ground item — split out of TestSnapshotRoundTrip.
func checkRestoredMonsterAndGround(
	t *testing.T, w2 *game.World, monsterID, groundInst int64, groundHex protocol.Hex,
) {
	t.Helper()

	restoredMonster, ok := entityOfSnap(w2.Snapshot(), monsterID)
	if !ok {
		t.Fatalf("restored monster (id %d) missing from snapshot", monsterID)
	}

	if got, want := w2.MonsterKindForTest(monsterID), "wolf"; got != want {
		t.Errorf("restored monster kind = %q, want %q", got, want)
	}

	if got, want := restoredMonster.HP, game.MonsterMaxHPForTest("wolf"); got != want {
		t.Errorf("restored monster HP = %d, want %d", got, want)
	}

	for _, gi := range w2.Snapshot().GroundItems {
		if gi.ID == groundInst && gi.Hex == groundHex && gi.DefID == "dagger" {
			return
		}
	}

	t.Errorf("restored ground items missing instance %d (dagger at %v)", groundInst, groundHex)
}

// checkRestoredQuestAndArchive asserts quest #1's taken state/holder and
// that bob's archive entry rode the snapshot — split out of
// TestSnapshotRoundTrip.
func checkRestoredQuestAndArchive(t *testing.T, w2 *game.World, aliceID int64, bobToken string) {
	t.Helper()

	var q1 protocol.QuestView

	for _, q := range w2.Snapshot().Quests {
		if q.ID == 1 {
			q1 = q
		}
	}

	if got, want := q1.State, protocol.QuestTaken; got != want {
		t.Errorf("restored quest #1 state = %q, want %q", got, want)
	}

	if got, want := q1.HolderEntityID, aliceID; got != want {
		t.Errorf("restored quest #1 HolderEntityID = %d, want %d", got, want)
	}

	if got, want := w2.ArchivedForTest(bobToken), true; got != want {
		t.Errorf("ArchivedForTest(bob) after restore = %v, want %v (archive round-trips)", got, want)
	}
}

// TestSnapshotTransientsZeroed: path, attackTarget, pending, and
// bubbleID (InCombat) are all zeroed on a restored entity, even when they
// were non-zero at marshal time — none of them ride the snapshot.
func TestSnapshotTransientsZeroed(t *testing.T) {
	t.Parallel()

	w, _ := newSnapshotWorld(t)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.SetPathForTest(me.EntityID, []protocol.Hex{{Q: 1, R: 0}, {Q: 2, R: 0}})
	w.SetAttackTargetForTest(me.EntityID, protocol.Hex{Q: 5, R: 5})
	w.SetPendingEquipForTest(me.EntityID, 999)
	w.SetBubbleIDForTest(me.EntityID, 42)

	data, err := w.MarshalState()
	if err != nil {
		t.Fatalf("MarshalState: %v", err)
	}

	w2, _ := newSnapshotWorld(t)
	if err := w2.RestoreState(data); err != nil {
		t.Fatalf("RestoreState: %v", err)
	}

	if got := w2.PathForTest(me.EntityID); got != nil {
		t.Errorf("restored Path = %v, want nil", got)
	}

	if got, want := w2.HasAttackTargetForTest(me.EntityID), false; got != want {
		t.Errorf("restored HasAttackTarget = %v, want %v", got, want)
	}

	if got, want := w2.PendingEquipForTest(me.EntityID), int64(0); got != want {
		t.Errorf("restored PendingEquip = %d, want %d", got, want)
	}

	restored, ok := entityOfSnap(w2.Snapshot(), me.EntityID)
	if !ok {
		t.Fatalf("restored entity %d missing from snapshot", me.EntityID)
	}

	if got, want := restored.InCombat, false; got != want {
		t.Errorf("restored InCombat (bubbleID != 0) = %v, want %v", got, want)
	}
}

// TestSnapshotRestoredPlayerGraceStartsAtLoad pins the spec's risk directly:
// a restored player's disconnect-grace clock starts at LOAD time, not the
// pre-shutdown disconnectedAt, so it survives a full grace after restart
// before the sweep archives it again.
func TestSnapshotRestoredPlayerGraceStartsAtLoad(t *testing.T) {
	t.Parallel()

	w, clk := newSnapshotWorld(t)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	// Simulate a long-lived, still-connected player at shutdown: its
	// disconnectedAt is old (join time), streams are open.
	w.StreamOpened(me.Token)
	clk.advance(365 * 24 * time.Hour)

	data, err := w.MarshalState()
	if err != nil {
		t.Fatalf("MarshalState: %v", err)
	}

	w2, clk2 := newSnapshotWorld(t)

	loadTime := clk2.now()

	if err := w2.RestoreState(data); err != nil {
		t.Fatalf("RestoreState: %v", err)
	}

	got, ok := w2.DisconnectedAtForTest(me.Token)
	if !ok {
		t.Fatalf("DisconnectedAtForTest: restored entity not found")
	}

	if !got.Equal(loadTime) {
		t.Errorf("restored disconnectedAt = %v, want load time %v (not the ancient pre-shutdown value)", got, loadTime)
	}

	if got, want := w2.StreamsForTest(me.Token), 0; got != want {
		t.Errorf("restored streams = %d, want %d", got, want)
	}

	grace := 5 * time.Second
	w2.SetDisconnectGraceForTest(grace)

	// Within one grace of LOAD time: not swept, even though the entity's
	// pre-shutdown state was disconnected for a year.
	clk2.advance(grace - time.Second)

	if got, want := w2.SweepForTest(clk2.now()), false; got != want {
		t.Errorf("SweepForTest within grace-from-load = %v, want %v (grace must restart at load)", got, want)
	}

	// Past one full grace from LOAD time: swept (and archived).
	clk2.advance(2 * time.Second)

	if got, want := w2.SweepForTest(clk2.now()), true; got != want {
		t.Errorf("SweepForTest past grace-from-load = %v, want %v", got, want)
	}

	if got, want := w2.ArchivedForTest(me.Token), true; got != want {
		t.Errorf("ArchivedForTest after grace-from-load sweep = %v, want %v", got, want)
	}
}

// TestSnapshotRestoreTurnMonotonic: the turn counter continues climbing from
// where the snapshot left off, not resetting to zero — so SSE ids (which
// ride the turn number) stay monotonic across a restart.
func TestSnapshotRestoreTurnMonotonic(t *testing.T) {
	t.Parallel()

	w, _ := newSnapshotWorld(t)

	for range 5 {
		w.ResolveTurnForTest()
	}

	beforeTurn := w.Snapshot().Turn
	if beforeTurn == 0 {
		t.Fatalf("test setup: turn is still 0 after 5 resolutions")
	}

	data, err := w.MarshalState()
	if err != nil {
		t.Fatalf("MarshalState: %v", err)
	}

	w2, _ := newSnapshotWorld(t)
	if err := w2.RestoreState(data); err != nil {
		t.Fatalf("RestoreState: %v", err)
	}

	w2.ResolveTurnForTest()

	if got, want := w2.Snapshot().Turn, beforeTurn+1; got != want {
		t.Errorf("turn after restore + one resolution = %d, want %d (continuing, not restarting)", got, want)
	}
}

// TestSnapshotMismatchGates: a version, world-seed, or world-radius mismatch
// between the snapshot and the target world makes RestoreState error and
// leave the target world untouched — never a migration, never a panic.
func TestSnapshotMismatchGates(t *testing.T) {
	t.Parallel()

	t.Run("seed mismatch", func(t *testing.T) {
		t.Parallel()

		w, _ := newSnapshotWorld(t)

		data, err := w.MarshalState()
		if err != nil {
			t.Fatalf("MarshalState: %v", err)
		}

		other := game.NewWorld(
			time.Second, testCombatPatience, testBubblePoll, testDisconnectGrace,
			snapTestSeed+1, snapTestRadius, hub.New(),
		)

		err = other.RestoreState(data)
		if err == nil {
			t.Fatalf("RestoreState with mismatched seed: got nil error, want a mismatch error")
		}

		if got, want := err.Error(), "does not match this world"; !strings.Contains(got, want) {
			t.Errorf("err.Error() = %q, should contain %q", got, want)
		}
	})

	t.Run("radius mismatch", func(t *testing.T) {
		t.Parallel()

		w, _ := newSnapshotWorld(t)

		data, err := w.MarshalState()
		if err != nil {
			t.Fatalf("MarshalState: %v", err)
		}

		other := game.NewWorld(
			time.Second, testCombatPatience, testBubblePoll, testDisconnectGrace,
			snapTestSeed, snapTestRadius+1, hub.New(),
		)

		err = other.RestoreState(data)
		if err == nil {
			t.Fatalf("RestoreState with mismatched radius: got nil error, want a mismatch error")
		}

		if got, want := err.Error(), "does not match this world"; !strings.Contains(got, want) {
			t.Errorf("err.Error() = %q, should contain %q", got, want)
		}
	})

	t.Run("garbage data", func(t *testing.T) {
		t.Parallel()

		other := game.NewWorld(
			time.Second, testCombatPatience, testBubblePoll, testDisconnectGrace,
			snapTestSeed, snapTestRadius, hub.New(),
		)

		err := other.RestoreState([]byte("not json"))
		if err == nil {
			t.Fatalf("RestoreState with garbage data: got nil error, want a decode error")
		}
	})
}

// TestSnapshotRoundTripInventoryShapes: the v3 disk shape (the
// inventory-slots milestone) round-trips the slot-keyed equipped map, the
// backpack with a consumable STACK (count > 1), and an archived character
// carrying the same shapes — ids, defs, entry indices, and counts all exact.
func TestSnapshotRoundTripInventoryShapes(t *testing.T) {
	t.Parallel()

	w, clk := newSnapshotWorld(t)

	// Live player: armor equipped in the body slot, a 2-potion stack and a
	// spare hammer in the backpack.
	me, err := w.Join("", "packrat", protocol.ClassFighter, protocol.SpeciesDwarf)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	// A live stream keeps packrat out of the sweep that archives sleeper.
	w.StreamOpened(me.Token)

	armorID := w.GrantItemForTest(me.EntityID, "leather-armor")
	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: me.EntityID, Token: me.Token, Kind: protocol.IntentEquip, ItemID: armorID,
	}); err != nil {
		t.Fatalf("equip armor: %v", err)
	}

	stackID := w.GrantItemForTest(me.EntityID, "healing-potion")
	w.GrantItemForTest(me.EntityID, "healing-potion")
	hammerID := w.GrantItemForTest(me.EntityID, "iron-warhammer")

	// Archived character with the same shapes: a second player, swept.
	gone, err := w.Join("", "sleeper", protocol.ClassRogue, protocol.SpeciesElf)
	if err != nil {
		t.Fatalf("Join sleeper: %v", err)
	}

	w.GrantItemForTest(gone.EntityID, "healing-potion")
	w.GrantItemForTest(gone.EntityID, "healing-potion")
	w.GrantItemForTest(gone.EntityID, "healing-potion")
	clk.advance(testDisconnectGrace + time.Second)

	if !w.SweepForTest(clk.now()) {
		t.Fatal("sweep did not archive the disconnected sleeper")
	}

	data, err := w.MarshalState()
	if err != nil {
		t.Fatalf("MarshalState: %v", err)
	}

	w2, _ := newSnapshotWorld(t)
	if err := w2.RestoreState(data); err != nil {
		t.Fatalf("RestoreState: %v", err)
	}

	// The live player's equipped map: same instance ids in the same slots.
	if got, want := w2.EquippedInSlotForTest(me.EntityID, protocol.ItemTypeChest), armorID; got != want {
		t.Errorf("restored body slot = %d, want %d", got, want)
	}

	closeInst, _ := w.EquippedSlotsForTest(me.EntityID)
	if got, want := firstOf(w2.EquippedSlotsForTest(me.EntityID)), closeInst; got != want {
		t.Errorf("restored melee slot = %d, want %d", got, want)
	}

	// The backpack: same entries at the same indices, stack count intact.
	pre, post := w.BackpackForTest(me.EntityID), w2.BackpackForTest(me.EntityID)
	if post != pre {
		t.Errorf("restored backpack = %+v, want %+v", post, pre)
	}

	if got, want := post[0].DefID, "healing-potion"; got != want {
		t.Errorf("restored backpack[0] = %q, want %q", got, want)
	}

	if got, want := post[0].Count, 2; got != want {
		t.Errorf("restored stack count = %d, want %d", got, want)
	}

	// The archived character restores through Join with its stack intact.
	back, err := w2.Join(gone.Token, "", "", "")
	if err != nil {
		t.Fatalf("Join restored sleeper: %v", err)
	}

	sleeperPack := w2.BackpackForTest(back.EntityID)
	if got, want := sleeperPack[0].DefID, "healing-potion"; got != want {
		t.Errorf("restored sleeper backpack[0] = %q, want %q", got, want)
	}

	if got, want := sleeperPack[0].Count, 3; got != want {
		t.Errorf("restored sleeper stack count = %d, want %d", got, want)
	}

	// Uniqueness guard: the restored world's id counter is past every
	// restored instance id, so a fresh grant can never collide.
	fresh := w2.GrantItemForTest(me.EntityID, "venom-fang")
	if fresh == 0 {
		t.Fatal("GrantItemForTest on the restored world failed (backpack full?)")
	}

	for _, taken := range []int64{armorID, stackID, hammerID} {
		if fresh == taken {
			t.Errorf("fresh instance id %d collides with a restored one", fresh)
		}
	}
}

// TestRestoreRecomputesDerivedHP: a stored entity whose maxHP/hp were written
// under a stale/older curve must come back recalibrated on load — maxHP is
// recomputed from (class, levelFor(xp)) and hp is clamped to it, so a curve
// change (XP1/XP2) reaches existing characters without a snapshot-version
// bump (fast-lane T2). The stored 66/66 pair is deliberately stale — neither
// the new curve's answer (45) nor a value a live game state would ever
// produce — so the test can't pass by accident if recompute silently no-ops.
func TestRestoreRecomputesDerivedHP(t *testing.T) {
	t.Parallel()

	w, _ := newSnapshotWorld(t)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.SetXPForTest(me.EntityID, 400) // level 3 under the quadratic curve (T1)

	data, err := w.MarshalState()
	if err != nil {
		t.Fatalf("MarshalState: %v", err)
	}

	// Craft: rewrite the marshaled entity's hp/maxHp to stale values (as if
	// written by an older curve) directly in the JSON, since no ForTest
	// helper sets maxHP independent of the sync path.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal snapshot for craft: %v", err)
	}

	entities, ok := raw["entities"].([]any)
	if !ok {
		t.Fatalf("snapshot entities: got %T, want []any", raw["entities"])
	}

	found := false

	for _, ent := range entities {
		m, ok := ent.(map[string]any)
		if !ok {
			continue
		}

		if id, _ := m["id"].(float64); int64(id) == me.EntityID {
			m["hp"] = 66
			m["maxHp"] = 66
			found = true
		}
	}

	if !found {
		t.Fatalf("crafted entity %d not found in snapshot", me.EntityID)
	}

	crafted, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal crafted snapshot: %v", err)
	}

	w2, _ := newSnapshotWorld(t)
	if err := w2.RestoreState(crafted); err != nil {
		t.Fatalf("RestoreState: %v", err)
	}

	e, ok := entityOfSnap(w2.Snapshot(), me.EntityID)
	if !ok {
		t.Fatalf("restored entity %d missing from snapshot", me.EntityID)
	}

	if got, want := e.MaxHP, 45; got != want { // fighter base 30 + levelHPBonus(3)=15 (8+7)
		t.Errorf("restored MaxHP = %d, want %d (recomputed from class+XP, not the stale stored 66)", got, want)
	}

	if got, want := e.HP, 45; got != want {
		t.Errorf("restored HP = %d, want %d (clamped down from the stale stored 66)", got, want)
	}
}

// TestSnapshotRoundTripSkillState (#124 task 3): all three skill fields
// survive a save/load. PointsGrantedLevel is the one that matters most — it
// is what stops a reload from re-paying every level the player has ever
// earned, and it is invisible in play, so only a test catches its loss.
func TestSnapshotRoundTripSkillState(t *testing.T) {
	t.Parallel()

	w, _ := newSnapshotWorld(t)

	me, err := w.Join("", "scholar", protocol.ClassMage, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.StreamOpened(me.Token)

	learned := []string{skillCombatTrainingID, skillWeakSpotID}
	w.SetSkillStateForTest(me.EntityID, learned, 4, 3)

	data, err := w.MarshalState()
	if err != nil {
		t.Fatalf("MarshalState: %v", err)
	}

	restored, _ := newSnapshotWorld(t)
	if err := restored.RestoreState(data); err != nil {
		t.Fatalf("RestoreState: %v", err)
	}

	gotLearned, gotPoints, gotGranted := restored.SkillStateForTest(me.EntityID)

	if got, want := strings.Join(gotLearned, ","), strings.Join(learned, ","); got != want {
		t.Errorf("learned skills = %q, want %q", got, want)
	}

	if got, want := gotPoints, 4; got != want {
		t.Errorf("skill points = %d, want %d", got, want)
	}

	if got, want := gotGranted, 3; got != want {
		t.Errorf("pointsGrantedLevel = %d, want %d (else a reload re-pays every level)", got, want)
	}
}
