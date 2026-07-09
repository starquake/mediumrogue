package game_test

import (
	"errors"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

func TestQuestBoardIsDeterministicAndWellFormed(t *testing.T) {
	t.Parallel()

	a := newPartyWorld(t) // same seed 0xC0FFEE, radius 12
	b := newPartyWorld(t)

	qa, qb := a.Snapshot().Quests, b.Snapshot().Quests
	if got, want := len(qa), 6; got != want {
		t.Fatalf("board size = %d, want %d", got, want)
	}

	for i := range qa {
		if got, want := qa[i], qb[i]; got != want {
			t.Fatalf("quest %d differs across same-seed worlds: %+v vs %+v", i, got, want)
		}
	}

	kills, reaches := 0, 0
	origin := protocol.Hex{Q: 0, R: 0}

	for _, q := range qa {
		if got, want := q.State, protocol.QuestAvailable; got != want {
			t.Errorf("quest %d state = %q, want %q", q.ID, got, want)
		}

		switch q.Kind {
		case "kill":
			kills++

			if q.TargetN < 2 || q.TargetN > 4 {
				t.Errorf("kill quest %d targetN = %d, want 2..4", q.ID, q.TargetN)
			}
		case "reach":
			reaches++

			if d := game.HexDistance(origin, q.GoalHex); d < 8 {
				t.Errorf("reach quest %d goal %v is %d from origin, want >= 8", q.ID, q.GoalHex, d)
			}
		default:
			t.Errorf("quest %d unknown kind %q", q.ID, q.Kind)
		}

		if q.RewardXP <= 0 {
			t.Errorf("quest %d rewardXP = %d, want > 0", q.ID, q.RewardXP)
		}
	}

	if kills != 3 || reaches != 3 {
		t.Errorf("board = %d kill + %d reach, want 3 + 3", kills, reaches)
	}
}

func TestQuestTakeOneSlotAndErrors(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)
	alice := joinNamed(t, w, "alice")
	q1, q2 := firstAvailableQuests(t, w, 2)

	if _, err := w.QuestTake(alice.Token, 999); !errors.Is(err, game.ErrQuestNotFound) {
		t.Errorf("take 999: err = %v, want ErrQuestNotFound", err)
	}

	if _, err := w.QuestTake(alice.Token, q1); err != nil {
		t.Fatalf("take: %v", err)
	}

	if _, err := w.QuestTake(alice.Token, q2); !errors.Is(err, game.ErrQuestSlotFull) {
		t.Errorf("second take: err = %v, want ErrQuestSlotFull", err)
	}

	bob := joinNamed(t, w, "bob")
	if _, err := w.QuestTake(bob.Token, q1); !errors.Is(err, game.ErrQuestTaken) {
		t.Errorf("take taken: err = %v, want ErrQuestTaken", err)
	}

	if _, err := w.QuestAbandon(bob.Token); !errors.Is(err, game.ErrNoActiveQuest) {
		t.Errorf("abandon none: err = %v, want ErrNoActiveQuest", err)
	}

	if _, err := w.QuestAbandon(alice.Token); err != nil {
		t.Fatalf("abandon: %v", err)
	}

	if got, want := questByID(t, w, q1).State, protocol.QuestAvailable; got != want {
		t.Errorf("state after abandon = %q, want %q", got, want)
	}

	if got, want := questByID(t, w, q1).Progress, 0; got != want {
		t.Errorf("progress after abandon = %d, want %d (reset)", got, want)
	}
}

func TestPartyTakeAndJoinAbandonsPersonalQuest(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)
	alice := joinNamed(t, w, "alice")
	bob := joinNamed(t, w, "bob")
	q1, q2 := firstAvailableQuests(t, w, 2)

	// bob takes a personal quest, then joins alice's party -> auto-abandoned.
	if _, err := w.QuestTake(bob.Token, q1); err != nil {
		t.Fatalf("bob take: %v", err)
	}

	mustInviteAccept(t, w, alice, bob, "bob")

	if got, want := questByID(t, w, q1).State, protocol.QuestAvailable; got != want {
		t.Errorf("bob's quest after joining a party = %q, want %q (auto-abandoned)", got, want)
	}

	// a member takes for the party -> HolderPartyID set.
	if _, err := w.QuestTake(alice.Token, q2); err != nil {
		t.Fatalf("party take: %v", err)
	}

	qv := questByID(t, w, q2)
	if qv.HolderPartyID == 0 || qv.HolderEntityID != 0 {
		t.Errorf("party quest holder = entity %d party %d, want party-only", qv.HolderEntityID, qv.HolderPartyID)
	}

	// the OTHER member's slot is full too (shared).
	q3 := nthAvailableQuest(t, w, 0)
	if _, err := w.QuestTake(bob.Token, q3); !errors.Is(err, game.ErrQuestSlotFull) {
		t.Errorf("member take with party quest: err = %v, want ErrQuestSlotFull", err)
	}
}

func TestPartyDissolveReturnsQuest(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)
	alice := joinNamed(t, w, "alice")
	bob := joinNamed(t, w, "bob")
	mustInviteAccept(t, w, alice, bob, "bob")

	q, _ := firstAvailableQuests(t, w, 2)
	if _, err := w.QuestTake(alice.Token, q); err != nil {
		t.Fatalf("take: %v", err)
	}

	if _, err := w.PartyLeave(bob.Token); err != nil { // pair -> dissolve
		t.Fatalf("leave: %v", err)
	}

	if got, want := questByID(t, w, q).State, protocol.QuestAvailable; got != want {
		t.Errorf("quest after dissolve = %q, want %q", got, want)
	}
}

// TestKillQuestTicksOncePerPartyAndCompletes: a pair takes a kill-2 quest and
// fights two adjacent one-hit-kill monsters inside a shared combat bubble.
// The first kill ticks the party's shared quest exactly once (Progress == 1,
// not 2 — a regression that ticked once per member, rather than once per
// distinct quest, would double-count here since both alice and bob hold the
// same party quest). The second kill completes it and pays the RewardXP (with
// the Human XP bonus) to BOTH members, on top of the per-kill award both
// already earned merely by surviving inside the bubble.
func TestKillQuestTicksOncePerPartyAndCompletes(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)
	alice := joinNamed(t, w, "alice")
	bob := joinNamed(t, w, "bob")
	mustInviteAccept(t, w, alice, bob, "bob")

	qID, targetN := killQuest(t, w, 2)

	if got, want := targetN, 2; got != want {
		t.Fatalf("test assumes a kill-2 quest; got targetN = %d", got)
	}

	if _, err := w.QuestTake(alice.Token, qID); err != nil {
		t.Fatalf("take: %v", err)
	}

	hexes := walkableNeighborsN(t, w, alice.Hex, targetN)

	for _, h := range hexes {
		monsterID := w.PlaceMonsterForTest(h)
		w.SetHPForTest(monsterID, protocol.SwordDamage) // one bump is lethal
	}

	// Forming turn: both idle, the monsters chase into the shared bubble.
	step(t, w)

	// First kill: progress must land at 1, not 2 (one tick for the party).
	w.SetPathForTest(alice.EntityID, []protocol.Hex{hexes[0]})
	step(t, w)

	if got, want := questByID(t, w, qID).Progress, 1; got != want {
		t.Fatalf("progress after first kill = %d, want %d (ticked once for the party)", got, want)
	}

	if got, want := questByID(t, w, qID).State, protocol.QuestTaken; got != want {
		t.Fatalf("state after first kill = %q, want %q", got, want)
	}

	wantPerKill := protocol.MonsterXP * (100 + protocol.HumanXPBonusPercent) / 100

	if got, want := w.XPForTest(alice.EntityID), wantPerKill; got != want {
		t.Errorf("alice XP after first kill = %d, want %d", got, want)
	}

	if got, want := w.XPForTest(bob.EntityID), wantPerKill; got != want {
		t.Errorf("bob XP after first kill = %d, want %d", got, want)
	}

	// Second (final) kill: completes the quest and pays the reward on top.
	w.SetPathForTest(alice.EntityID, []protocol.Hex{hexes[1]})
	step(t, w)

	qv := questByID(t, w, qID)
	if got, want := qv.State, protocol.QuestCompleted; got != want {
		t.Fatalf("state after final kill = %q, want %q", got, want)
	}

	wantReward := qv.RewardXP * (100 + protocol.HumanXPBonusPercent) / 100
	wantTotal := 2*wantPerKill + wantReward

	if got, want := w.XPForTest(alice.EntityID), wantTotal; got != want {
		t.Errorf("alice total XP = %d, want %d (2 kills + reward)", got, want)
	}

	if got, want := w.XPForTest(bob.EntityID), wantTotal; got != want {
		t.Errorf("bob total XP = %d, want %d (2 kills + reward)", got, want)
	}
}

// TestLateJoinerPaidInFull: alice and carol progress a kill-2 quest to one
// kill short of done; bob then joins the party (never having fought so far)
// and stands in for the final kill. Completion pays every CURRENT holder in
// full — bob's XP includes the whole RewardXP, not a prorated share.
func TestLateJoinerPaidInFull(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)
	alice := joinNamed(t, w, "alice")
	carol := joinNamed(t, w, "carol")
	mustInviteAccept(t, w, alice, carol, "carol")

	qID, targetN := killQuest(t, w, 2)

	if got, want := targetN, 2; got != want {
		t.Fatalf("test assumes a kill-2 quest; got targetN = %d", got)
	}

	if _, err := w.QuestTake(alice.Token, qID); err != nil {
		t.Fatalf("take: %v", err)
	}

	hexes := walkableNeighborsN(t, w, alice.Hex, targetN)

	monster0 := w.PlaceMonsterForTest(hexes[0])
	w.SetHPForTest(monster0, protocol.SwordDamage)

	step(t, w) // forming turn

	w.SetPathForTest(alice.EntityID, []protocol.Hex{hexes[0]})
	step(t, w) // one kill short of done

	if got, want := questByID(t, w, qID).Progress, 1; got != want {
		t.Fatalf("progress before bob joins = %d, want %d", got, want)
	}

	// bob joins the party — same spawn hex, never having fought.
	bob := joinNamed(t, w, "bob")
	mustInviteAccept(t, w, alice, bob, "bob")

	monster1 := w.PlaceMonsterForTest(hexes[1])
	w.SetHPForTest(monster1, protocol.SwordDamage)

	step(t, w) // settle turn: bob and monster1 join the existing bubble

	w.SetPathForTest(alice.EntityID, []protocol.Hex{hexes[1]})
	step(t, w) // final kill: completes the quest

	qv := questByID(t, w, qID)
	if got, want := qv.State, protocol.QuestCompleted; got != want {
		t.Fatalf("state after final kill = %q, want %q", got, want)
	}

	wantPerKill := protocol.MonsterXP * (100 + protocol.HumanXPBonusPercent) / 100
	wantReward := qv.RewardXP * (100 + protocol.HumanXPBonusPercent) / 100
	wantBob := wantPerKill + wantReward // bob only stood for the final kill

	if got, want := w.XPForTest(bob.EntityID), wantBob; got != want {
		t.Errorf("late joiner bob XP = %d, want %d (kill + full reward)", got, want)
	}
}

// TestReachQuestCompletes: taking a reach quest with the holder already
// standing on GoalHex completes it on the next world turn and pays the flat
// reach reward (no species bonus — PlaceEntityForTest leaves species unset).
func TestReachQuestCompletes(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)

	qID, goal := reachQuest(t, w)

	id, token := w.PlaceEntityForTest(goal)
	if _, err := w.QuestTake(token, qID); err != nil {
		t.Fatalf("take: %v", err)
	}

	w.ResolveTurnForTest()

	qv := questByID(t, w, qID)
	if got, want := qv.State, protocol.QuestCompleted; got != want {
		t.Errorf("state after reaching the goal = %q, want %q", got, want)
	}

	if got, want := w.XPForTest(id), qv.RewardXP; got != want {
		t.Errorf("XP after reach completion = %d, want %d (flat reward, no species bonus)", got, want)
	}
}

// TestSweepReturnsPersonalQuest: a personal-quest holder swept for disconnect
// past the grace returns their quest to the board (via
// abandonPersonalQuestLocked in the sweep's gone-loop).
func TestSweepReturnsPersonalQuest(t *testing.T) {
	t.Parallel()

	w, clk := newTimedWorld(t)
	w.SetDisconnectGraceForTest(presenceGrace)

	alice := joinNamed(t, w, "alice")
	w.StreamOpened(alice.Token)

	q, _ := firstAvailableQuests(t, w, 2)
	if _, err := w.QuestTake(alice.Token, q); err != nil {
		t.Fatalf("take: %v", err)
	}

	w.StreamClosed(alice.Token)
	clk.advance(presenceGrace + time.Second)

	if got, want := w.SweepForTest(clk.now()), true; got != want {
		t.Fatalf("SweepForTest removed = %v, want %v", got, want)
	}

	if got, want := questByID(t, w, q).State, protocol.QuestAvailable; got != want {
		t.Errorf("quest after sweeping its holder = %q, want %q", got, want)
	}
}

// firstAvailableQuests returns the ids of the first two Available quests on
// the board, in board (id) order. n is always 2 at every call site — kept as
// a parameter (rather than hardcoded) so a call site reads as "give me N
// quests", matching nthAvailableQuest's shape.
//
//nolint:unparam // n is always 2 today; kept for the self-documenting call shape.
func firstAvailableQuests(t *testing.T, w *game.World, n int) (int64, int64) {
	t.Helper()

	if n != 2 {
		t.Fatalf("firstAvailableQuests only supports n=2, got %d", n)
	}

	var ids []int64

	for _, q := range w.Snapshot().Quests {
		if q.State == protocol.QuestAvailable {
			ids = append(ids, q.ID)
			if len(ids) == n {
				break
			}
		}
	}

	if len(ids) < n {
		t.Fatalf("only %d available quests on the board, want %d", len(ids), n)
	}

	return ids[0], ids[1]
}

// nthAvailableQuest returns the id of the idx-th (0-based) currently
// Available quest, in board order.
func nthAvailableQuest(t *testing.T, w *game.World, idx int) int64 {
	t.Helper()

	i := 0

	for _, q := range w.Snapshot().Quests {
		if q.State != protocol.QuestAvailable {
			continue
		}

		if i == idx {
			return q.ID
		}

		i++
	}

	t.Fatalf("no available quest at index %d", idx)

	return 0
}

// questByID returns the current QuestView for id, failing the test if it is
// not on the board.
func questByID(t *testing.T, w *game.World, id int64) protocol.QuestView {
	t.Helper()

	for _, q := range w.Snapshot().Quests {
		if q.ID == id {
			return q
		}
	}

	t.Fatalf("quest %d not found on the board", id)

	return protocol.QuestView{}
}

// killQuest returns the id and targetN of the first "kill" quest whose
// TargetN equals want, or — if none matches — the first kill quest found at
// all (so a caller can fall back to spawning exactly its targetN monsters).
func killQuest(t *testing.T, w *game.World, want int) (int64, int) {
	t.Helper()

	var fallbackID int64

	fallbackN := 0

	for _, q := range w.Snapshot().Quests {
		if q.Kind != "kill" {
			continue
		}

		if fallbackID == 0 {
			fallbackID, fallbackN = q.ID, q.TargetN
		}

		if q.TargetN == want {
			return q.ID, q.TargetN
		}
	}

	if fallbackID == 0 {
		t.Fatalf("no kill quest on the board")
	}

	return fallbackID, fallbackN
}

// reachQuest returns the id and goal hex of the first "reach" quest on the
// board.
func reachQuest(t *testing.T, w *game.World) (int64, protocol.Hex) {
	t.Helper()

	for _, q := range w.Snapshot().Quests {
		if q.Kind == "reach" {
			return q.ID, q.GoalHex
		}
	}

	t.Fatalf("no reach quest on the board")

	return 0, protocol.Hex{}
}
