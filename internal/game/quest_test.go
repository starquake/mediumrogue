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

// questKindKill mirrors the unexported game.questKindKill string constant —
// this external test package can't reference it directly, and it recurs
// often enough here to warrant a name instead of a repeated literal.
const questKindKill = "kill"

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
		case questKindKill:
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

// TestQuestTakeMultiplePersonalQuestsAndErrors: item 14, playtest batch 2 —
// a player may hold MULTIPLE personal quests concurrently (amending 8.3's
// one-slot rule), so a second personal take no longer errors. /abandon now
// names the quest explicitly (ambiguous otherwise, with several active).
func TestQuestTakeMultiplePersonalQuestsAndErrors(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)
	alice := joinNamed(t, w, "alice")
	q1, q2 := firstAvailableQuests(t, w, 2)

	if _, err := w.QuestTake(alice.Token, 999); !errors.Is(err, game.ErrQuestNotFound) {
		t.Errorf("take 999: err = %v, want ErrQuestNotFound", err)
	}

	if _, err := w.QuestTake(alice.Token, q1); err != nil {
		t.Fatalf("take q1: %v", err)
	}

	if _, err := w.QuestTake(alice.Token, q2); err != nil {
		t.Fatalf("take q2 (second concurrent personal quest): %v", err)
	}

	bob := joinNamed(t, w, "bob")
	if _, err := w.QuestTake(bob.Token, q1); !errors.Is(err, game.ErrQuestTaken) {
		t.Errorf("take taken: err = %v, want ErrQuestTaken", err)
	}

	if _, err := w.QuestAbandon(bob.Token, q2); !errors.Is(err, game.ErrNoActiveQuest) {
		t.Errorf("abandon not-held: err = %v, want ErrNoActiveQuest", err)
	}

	if _, err := w.QuestAbandon(alice.Token, q1); err != nil {
		t.Fatalf("abandon q1: %v", err)
	}

	if got, want := questByID(t, w, q1).State, protocol.QuestAvailable; got != want {
		t.Errorf("q1 state after abandon = %q, want %q", got, want)
	}

	if got, want := questByID(t, w, q1).Progress, 0; got != want {
		t.Errorf("q1 progress after abandon = %d, want %d (reset)", got, want)
	}

	// q2 is untouched by abandoning q1 — alice still holds it.
	if got, want := questByID(t, w, q2).State, protocol.QuestTaken; got != want {
		t.Errorf("q2 state after abandoning q1 = %q, want %q (untouched)", got, want)
	}

	if got, want := questByID(t, w, q2).HolderEntityID, alice.EntityID; got != want {
		t.Errorf("q2 holder after abandoning q1 = %d, want alice %d (untouched)", got, want)
	}
}

// TestTwoConcurrentPersonalQuestsProgressAndPayIndependently: item 14,
// playtest batch 2 — alice takes a kill quest AND a reach quest at once.
// Killing one monster progresses the kill quest without touching the reach
// quest; reaching the goal completes and pays the reach quest without
// touching the kill quest's progress; finishing the kill quest FIRST
// (chosen so the test never needs to return to a monster after alice
// leaves the bubble — the monster's own hunting AI would otherwise wander
// it away from its spawn hex, a real race the other ordering hit) pays its
// own reward, then completing the reach quest afterward pays its own
// reward too, on top of (not instead of) the kill reward already paid.
func TestTwoConcurrentPersonalQuestsProgressAndPayIndependently(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)
	alice := joinNamed(t, w, "alice")
	startHex := alice.Hex

	killID, targetN := killQuest(t, w, 2)
	reachID, goal := reachQuest(t, w)

	if _, err := w.QuestTake(alice.Token, killID); err != nil {
		t.Fatalf("take kill quest: %v", err)
	}

	if _, err := w.QuestTake(alice.Token, reachID); err != nil {
		t.Fatalf("take reach quest (second concurrent personal quest): %v", err)
	}

	hexes := walkableNeighborsN(t, w, startHex, targetN)
	for _, h := range hexes {
		monsterID := w.PlaceMonsterForTest(h)
		w.SetHPForTest(monsterID, game.ItemDamageForTest("iron-sword", 1)) // one bump is lethal
	}

	step(t, w) // forming turn: the monsters chase into the bubble

	// Progress the kill quest by one kill; the reach quest must be untouched.
	w.SetPathForTest(alice.EntityID, []protocol.Hex{hexes[0]})
	step(t, w)

	if got, want := questByID(t, w, killID).Progress, 1; got != want {
		t.Fatalf("kill quest progress = %d, want %d", got, want)
	}

	if got, want := questByID(t, w, reachID).State, protocol.QuestTaken; got != want {
		t.Fatalf("reach quest state after an unrelated kill = %q, want %q (untouched)", got, want)
	}

	if got, want := questByID(t, w, reachID).Progress, 0; got != want {
		t.Fatalf("reach quest progress after an unrelated kill = %d, want %d (untouched)", got, want)
	}

	// Finish the kill quest with the second monster (still waiting, alice
	// never left the bubble) — completes and pays it. The reach quest must
	// still be untouched.
	w.SetPathForTest(alice.EntityID, []protocol.Hex{hexes[1]})
	step(t, w)

	if got, want := questByID(t, w, killID).State, protocol.QuestCompleted; got != want {
		t.Fatalf("kill quest state after final kill = %q, want %q", got, want)
	}

	if got, want := questByID(t, w, reachID).State, protocol.QuestTaken; got != want {
		t.Errorf("reach quest state after kill quest completes = %q, want %q (untouched)", got, want)
	}

	if got, want := questByID(t, w, reachID).Progress, 0; got != want {
		t.Errorf("reach quest progress after kill quest completes = %d, want %d (untouched)", got, want)
	}

	xpAfterKill := w.XPForTest(alice.EntityID)

	// Now complete the reach quest too, by teleporting to its goal — its own
	// completion pays its own reward, on top of (not instead of) the kill
	// reward already paid above. Clear the leftover queued path first: a
	// bump-turned-attack never consumes it (the mover stays put), and
	// moveAndBumpLocked doesn't re-validate a queued path's adjacency
	// against SetHexForTest's raw teleport — an unconsumed path would
	// otherwise silently walk her back toward hexes[1] on the next
	// resolution instead of landing on goal.
	w.SetPathForTest(alice.EntityID, nil)
	w.SetHexForTest(alice.EntityID, goal)
	step(t, w)

	if got, want := questByID(t, w, reachID).State, protocol.QuestCompleted; got != want {
		t.Fatalf("reach quest state after reaching goal = %q, want %q", got, want)
	}

	if got := w.XPForTest(alice.EntityID); got <= xpAfterKill {
		t.Errorf("XP after finishing the reach quest = %d, want it to have grown past %d "+
			"(its own reward, paid independently of the kill reward already paid)", got, xpAfterKill)
	}
}

// TestQuestTakeCompletedReturnsDistinctError: taking a COMPLETED quest reports
// ErrQuestCompleted, not the generic ErrQuestTaken (which now means "taken but
// still in progress") — a player asking to help with a finished quest gets a
// clearer message than "someone already grabbed that".
func TestQuestTakeCompletedReturnsDistinctError(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)

	qID, goal := reachQuest(t, w)

	_, token := w.PlaceEntityForTest(goal)
	if _, err := w.QuestTake(token, qID); err != nil {
		t.Fatalf("take: %v", err)
	}

	w.ResolveTurnForTest()

	if got, want := questByID(t, w, qID).State, protocol.QuestCompleted; got != want {
		t.Fatalf("state after reaching the goal = %q, want %q", got, want)
	}

	bob := joinNamed(t, w, "bob")
	if _, err := w.QuestTake(bob.Token, qID); !errors.Is(err, game.ErrQuestCompleted) {
		t.Errorf("take completed: err = %v, want ErrQuestCompleted", err)
	}
}

// TestPartyJoinKeepsPersonalQuest: item 14, playtest batch 2 — joining a
// party no longer abandons the accepter's personal quest (amends 8.3's
// join-abandons-it rule). Bob keeps progressing q1 on his own, entirely
// independent of whatever quest the party itself takes; the PARTY's own
// slot still caps at one, though — a member's take fails once the party
// already holds a quest, exactly as before.
func TestPartyJoinKeepsPersonalQuest(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)
	alice := joinNamed(t, w, "alice")
	bob := joinNamed(t, w, "bob")
	q1, q2 := firstAvailableQuests(t, w, 2)

	// bob takes a personal quest, then joins alice's party.
	if _, err := w.QuestTake(bob.Token, q1); err != nil {
		t.Fatalf("bob take: %v", err)
	}

	mustInviteAccept(t, w, alice, bob, "bob")

	if got, want := questByID(t, w, q1).State, protocol.QuestTaken; got != want {
		t.Errorf("bob's quest after joining a party = %q, want %q (kept, not abandoned)", got, want)
	}

	if got, want := questByID(t, w, q1).HolderEntityID, bob.EntityID; got != want {
		t.Errorf("bob's quest holder after joining a party = %d, want bob %d (still personal)", got, want)
	}

	// a member takes for the party -> HolderPartyID set, distinct from bob's
	// still-personal q1.
	if _, err := w.QuestTake(alice.Token, q2); err != nil {
		t.Fatalf("party take: %v", err)
	}

	qv := questByID(t, w, q2)
	if qv.HolderPartyID == 0 || qv.HolderEntityID != 0 {
		t.Errorf("party quest holder = entity %d party %d, want party-only", qv.HolderEntityID, qv.HolderPartyID)
	}

	// The party's OWN slot is still capped at one — either member's take now
	// fails, even though bob personally could otherwise hold more.
	q3 := nthAvailableQuest(t, w, 0)
	if _, err := w.QuestTake(bob.Token, q3); !errors.Is(err, game.ErrQuestSlotFull) {
		t.Errorf("member take with party quest: err = %v, want ErrQuestSlotFull", err)
	}
}

// TestFormingPartyPromotesInviterQuest: alice (solo) takes a KILL quest and
// progresses it by actually killing a monster — proving Progress is a real,
// non-zero value before promotion, not just its zero-value default. She then
// invites bob and bob accepts. Rather than abandoning alice's PERSONAL quest
// (the existing rule for the ACCEPTER's quest, exercised by
// TestPartyTakeAndJoinAbandonsPersonalQuest), forming the party PROMOTES it —
// the party forms around whatever alice had already pitched. Progress carries
// over UNCHANGED (a buggy promotion that reset it to 0 would be caught here),
// and the one-slot invariant now covers both members: neither can take a
// second quest.
func TestFormingPartyPromotesInviterQuest(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)
	alice := joinNamed(t, w, "alice")
	bob := joinNamed(t, w, "bob")

	// Pick a KILL quest explicitly — firstAvailableQuests may return reach
	// quests, and this test needs one it can progress with a kill.
	var q1 int64

	for _, q := range w.Snapshot().Quests {
		if q.Kind == questKindKill {
			q1 = q.ID

			break
		}
	}

	if q1 == 0 {
		t.Fatalf("no kill quest on the board")
	}

	if _, err := w.QuestTake(alice.Token, q1); err != nil {
		t.Fatalf("alice take: %v", err)
	}

	if got, want := questByID(t, w, q1).HolderEntityID, alice.EntityID; got != want {
		t.Fatalf("quest holder entity before party forms = %d, want alice %d", got, want)
	}

	// Alice progresses the quest SOLO, before any party exists: one monster,
	// one kill (targetN is always >= 2, so this leaves the quest in progress
	// rather than completing it out from under the test).
	hexes := walkableNeighborsN(t, w, alice.Hex, 1)
	monsterID := w.PlaceMonsterForTest(hexes[0])
	w.SetHPForTest(monsterID, game.ItemDamageForTest("iron-sword", 1)) // one bump is lethal

	step(t, w) // forming turn: the monster chases into the bubble

	w.SetPathForTest(alice.EntityID, []protocol.Hex{hexes[0]})
	step(t, w) // the kill

	progressed := questByID(t, w, q1).Progress
	if progressed == 0 {
		t.Fatalf("progress after solo kill = 0, want > 0 (test setup did not land a kill)")
	}

	// A second available quest, distinct from q1, for the full-slot check below.
	var q2 int64

	for _, q := range w.Snapshot().Quests {
		if q.State == protocol.QuestAvailable && q.ID != q1 {
			q2 = q.ID

			break
		}
	}

	if q2 == 0 {
		t.Fatalf("need a second available quest distinct from q1")
	}

	mustInviteAccept(t, w, alice, bob, "bob")

	pa, pb := partyIDOf(t, w, alice.EntityID), partyIDOf(t, w, bob.EntityID)
	if pa == 0 || pa != pb {
		t.Fatalf("party ids: alice=%d bob=%d, want equal non-zero", pa, pb)
	}

	qv := questByID(t, w, q1)
	if got, want := qv.HolderPartyID, pa; got != want {
		t.Errorf("quest HolderPartyID = %d, want the new party id %d", got, want)
	}

	if got, want := qv.HolderEntityID, int64(0); got != want {
		t.Errorf("quest HolderEntityID = %d, want %d (promoted off the inviter)", got, want)
	}

	if got, want := qv.Progress, progressed; got != want {
		t.Errorf("progress after promotion = %d, want %d (carried over unchanged from the solo kill)", got, want)
	}

	if got, want := qv.State, protocol.QuestTaken; got != want {
		t.Errorf("state after promotion = %q, want %q", got, want)
	}

	// Both slots are now full — the promoted quest is the whole party's.
	if _, err := w.QuestTake(alice.Token, q2); !errors.Is(err, game.ErrQuestSlotFull) {
		t.Errorf("alice take another: err = %v, want ErrQuestSlotFull", err)
	}

	if _, err := w.QuestTake(bob.Token, q2); !errors.Is(err, game.ErrQuestSlotFull) {
		t.Errorf("bob take another: err = %v, want ErrQuestSlotFull", err)
	}
}

// TestPromotedQuestPaysWholeParty: alice takes a reach quest solo, then forms
// a party with bob — promoting it. Bob (who never touched the quest before
// the promotion) then stands on the goal alone; completion still pays BOTH
// party members the full reward. Both join as dwarf/elf (not human) so
// neither's XP gets the human bonus, keeping the expected delta exact.
func TestPromotedQuestPaysWholeParty(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)

	alice, err := w.Join("", "alice", protocol.ClassFighter, protocol.SpeciesDwarf)
	if err != nil {
		t.Fatalf("join alice: %v", err)
	}

	bob, err := w.Join("", "bob", protocol.ClassFighter, protocol.SpeciesElf)
	if err != nil {
		t.Fatalf("join bob: %v", err)
	}

	qID, goal := reachQuest(t, w)

	if _, err := w.QuestTake(alice.Token, qID); err != nil {
		t.Fatalf("take: %v", err)
	}

	mustInviteAccept(t, w, alice, bob, "bob")

	if got, want := questByID(t, w, qID).HolderPartyID, partyIDOf(t, w, alice.EntityID); got == 0 || got != want {
		t.Fatalf("quest HolderPartyID = %d, want alice's party %d", got, want)
	}

	w.SetHexForTest(bob.EntityID, goal)
	w.ResolveTurnForTest()

	qv := questByID(t, w, qID)
	if got, want := qv.State, protocol.QuestCompleted; got != want {
		t.Fatalf("state after bob reaches the goal = %q, want %q", got, want)
	}

	if got, want := w.XPForTest(alice.EntityID), qv.RewardXP; got != want {
		t.Errorf("alice XP = %d, want %d (full reward, no human bonus)", got, want)
	}

	if got, want := w.XPForTest(bob.EntityID), qv.RewardXP; got != want {
		t.Errorf("bob XP = %d, want %d (full reward, no human bonus)", got, want)
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
		w.SetHPForTest(monsterID, game.ItemDamageForTest("iron-sword", 1)) // one bump is lethal
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

	wantPerKill := game.MonsterXPForTest("wolf") * (100 + protocol.HumanXPBonusPercent) / 100

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
	w.SetHPForTest(monster0, game.ItemDamageForTest("iron-sword", 1))

	step(t, w) // forming turn

	w.SetPathForTest(alice.EntityID, []protocol.Hex{hexes[0]})
	step(t, w) // one kill short of done

	if got, want := questByID(t, w, qID).Progress, 1; got != want {
		t.Fatalf("progress before bob joins = %d, want %d", got, want)
	}

	// bob joins the party — pinned onto alice's hex (spawnHexLocked is
	// randomized among the origin clearing since #36, so a fresh join is no
	// longer guaranteed to land where alice did) so he lands in the existing
	// bubble on the settle turn below, never having fought.
	bob := joinNamed(t, w, "bob")
	w.SetHexForTest(bob.EntityID, alice.Hex)
	mustInviteAccept(t, w, alice, bob, "bob")

	monster1 := w.PlaceMonsterForTest(hexes[1])
	w.SetHPForTest(monster1, game.ItemDamageForTest("iron-sword", 1))

	step(t, w) // settle turn: bob and monster1 join the existing bubble

	w.SetPathForTest(alice.EntityID, []protocol.Hex{hexes[1]})
	step(t, w) // final kill: completes the quest

	qv := questByID(t, w, qID)
	if got, want := qv.State, protocol.QuestCompleted; got != want {
		t.Fatalf("state after final kill = %q, want %q", got, want)
	}

	wantPerKill := game.MonsterXPForTest("wolf") * (100 + protocol.HumanXPBonusPercent) / 100
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

// TestSweepReturnsAllPersonalQuests: a personal-quest holder swept for
// disconnect past the grace returns ALL of their quests to the board (via
// abandonPersonalQuestLocked in the sweep's gone-loop) — item 14, playtest
// batch 2: since a player can hold several concurrently, the old
// single-quest version of this test would have missed a regression that
// only returned the first one, permanently stranding the rest as "taken" by
// a ghost entity.
func TestSweepReturnsAllPersonalQuests(t *testing.T) {
	t.Parallel()

	w, clk := newTimedWorld(t)
	w.SetDisconnectGraceForTest(presenceGrace)

	alice := joinNamed(t, w, "alice")
	w.StreamOpened(alice.Token)

	q1, q2 := firstAvailableQuests(t, w, 2)
	if _, err := w.QuestTake(alice.Token, q1); err != nil {
		t.Fatalf("take q1: %v", err)
	}

	if _, err := w.QuestTake(alice.Token, q2); err != nil {
		t.Fatalf("take q2 (second concurrent personal quest): %v", err)
	}

	w.StreamClosed(alice.Token)
	clk.advance(presenceGrace + time.Second)

	if got, want := w.SweepForTest(clk.now()), true; got != want {
		t.Fatalf("SweepForTest removed = %v, want %v", got, want)
	}

	if got, want := questByID(t, w, q1).State, protocol.QuestAvailable; got != want {
		t.Errorf("q1 after sweeping its holder = %q, want %q", got, want)
	}

	if got, want := questByID(t, w, q2).State, protocol.QuestAvailable; got != want {
		t.Errorf("q2 after sweeping its holder = %q, want %q (both must return, not just one)", got, want)
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
//
//nolint:unparam // want is always 2 today; kept for the self-documenting call shape.
func killQuest(t *testing.T, w *game.World, want int) (int64, int) {
	t.Helper()

	var fallbackID int64

	fallbackN := 0

	for _, q := range w.Snapshot().Quests {
		if q.Kind != questKindKill {
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

// TestQuestBoardTinyWorldDoesNotPanic guards the reach-goal fallback: a world
// too small for any hex at the preferred quest distance must still boot (the
// generator falls back to nearer reachable hexes rather than panicking).
func TestQuestBoardTinyWorldDoesNotPanic(t *testing.T) {
	t.Parallel()

	w := game.NewWorld(time.Hour, time.Second, time.Millisecond, time.Minute, 0xC0FFEE, 2, hub.New())

	quests := w.Snapshot().Quests
	if got, want := len(quests), 6; got != want {
		t.Fatalf("tiny-world board size = %d, want %d", got, want)
	}
}

// TestKillQuestTickAnnouncesProgress: a mid-quest kill announces "N down, M to
// go" in chat (feedback where players look mid-fight); the FINAL kill
// announces completion only — no redundant tick line.
func TestKillQuestTickAnnouncesProgress(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)
	alice := joinNamed(t, w, "alice")

	var announced []string

	w.SetAnnounce(func(_, text string) { announced = append(announced, text) })

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
		w.SetHPForTest(monsterID, game.ItemDamageForTest("iron-sword", 1)) // one bump is lethal
	}

	step(t, w) // forming turn

	// First kill: a progress announcement, no completion yet.
	w.SetPathForTest(alice.EntityID, []protocol.Hex{hexes[0]})
	step(t, w)

	if got, want := lastAnnouncement(announced), "Cull the pack: 1 down, 1 to go"; got != want {
		t.Errorf("tick announcement = %q, want %q", got, want)
	}

	// Final kill: completion announcement, and NOT another tick line.
	w.SetPathForTest(alice.EntityID, []protocol.Hex{hexes[1]})
	step(t, w)

	last := lastAnnouncement(announced)
	if got, want := last, "Quest complete"; !strings.Contains(got, want) {
		t.Errorf("final announcement = %q, should contain %q", got, want)
	}

	for _, a := range announced {
		if strings.Contains(a, "2 down") {
			t.Errorf("final kill also announced a tick line %q — completion should replace it", a)
		}
	}
}

func lastAnnouncement(announced []string) string {
	if len(announced) == 0 {
		return ""
	}

	return announced[len(announced)-1]
}
