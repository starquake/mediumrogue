package game_test

import (
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// archiveGrace is the short removal grace these tests drive by hand, well
// under any clock step they take.
const archiveGrace = 5 * time.Second

// TestSweepArchivesThenJoinRestores: a swept player's XP, gear, and identity
// come back on a rejoin with the same token — new spawn hex, full level-scaled
// HP, everything else as left. The archive entry is consumed (gone) once
// restored.
func TestSweepArchivesThenJoinRestores(t *testing.T) {
	t.Parallel()

	w, clk := newTimedWorld(t)
	w.SetDisconnectGraceForTest(archiveGrace)

	me, err := w.Join("", "tester", protocol.ClassRogue, protocol.SpeciesElf)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	// Earn some XP (levels the character up) and grant a second close-slot
	// item so gear-beyond-defaults also round-trips.
	w.SetXPForTest(me.EntityID, 3*protocol.XPCurveBase+10)

	extraItem := w.GrantItemForTest(me.EntityID, "butchers-cleaver")

	wantClose, wantRanged := w.EquippedSlotsForTest(me.EntityID)

	// Disconnect and sweep past grace.
	w.StreamOpened(me.Token)
	w.StreamClosed(me.Token)
	clk.advance(archiveGrace + time.Second)

	if got, want := w.SweepForTest(clk.now()), true; got != want {
		t.Fatalf("SweepForTest removed = %v, want %v", got, want)
	}

	if _, ok := entityOfSnap(w.Snapshot(), me.EntityID); ok {
		t.Fatalf("player %d still present after sweep", me.EntityID)
	}

	if got, want := w.ArchivedForTest(me.Token), true; got != want {
		t.Fatalf("ArchivedForTest after sweep = %v, want %v", got, want)
	}

	// Rejoin with the same token: name/class/species are ignored (archived
	// identity wins), matching Join's reclaim contract.
	back, err := w.Join(me.Token, "ignored", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("restoring Join: %v", err)
	}

	if got, want := back.Token, me.Token; got != want {
		t.Errorf("restored token = %q, want %q (same token)", got, want)
	}

	if got, want := w.ArchivedForTest(me.Token), false; got != want {
		t.Errorf("ArchivedForTest after restore = %v, want %v (entry consumed)", got, want)
	}

	restored, ok := entityOfSnap(w.Snapshot(), back.EntityID)
	if !ok {
		t.Fatalf("restored entity %d not present in snapshot", back.EntityID)
	}

	if got, want := restored.Name, "tester"; got != want {
		t.Errorf("restored Name = %q, want %q", got, want)
	}

	if got, want := restored.Class, protocol.ClassRogue; got != want {
		t.Errorf("restored Class = %q, want %q", got, want)
	}

	if got, want := restored.Species, protocol.SpeciesElf; got != want {
		t.Errorf("restored Species = %q, want %q", got, want)
	}

	if got, want := restored.XP, 3*protocol.XPCurveBase+10; got != want {
		t.Errorf("restored XP = %d, want %d", got, want)
	}

	// 310 XP under the quadratic curve: level 2's floor is XPCurveBase*1^2=100,
	// level 3's is XPCurveBase*2^2=400, so 310 lands in level 2 (was level 4
	// under the old flat curve, 1+310/100).
	// re-derived for XPCurveBase quadratic curve (fast-lane T1)
	wantLevel := 2
	if got, want := restored.Level, wantLevel; got != want {
		t.Errorf("restored Level = %d, want %d", got, want)
	}

	wantMaxHP := game.MaxHPForTest(protocol.ClassRogue, wantLevel)
	if got, want := restored.MaxHP, wantMaxHP; got != want {
		t.Errorf("restored MaxHP = %d, want %d (level-scaled)", got, want)
	}

	if got, want := restored.HP, wantMaxHP; got != want {
		t.Errorf("restored HP = %d, want %d (full)", got, want)
	}

	gotClose, gotRanged := w.EquippedSlotsForTest(back.EntityID)
	if gotClose != wantClose || gotRanged != wantRanged {
		t.Errorf("restored equipped slots = (%d, %d), want (%d, %d)", gotClose, gotRanged, wantClose, wantRanged)
	}

	foundExtra := false

	for _, it := range restored.Items {
		if it.ID == extraItem {
			foundExtra = true
		}
	}

	if !foundExtra {
		t.Errorf("restored items missing granted-but-unequipped instance %d", extraItem)
	}

	if got, want := len(restored.Items), 3; got != want {
		t.Errorf("restored item count = %d, want %d (2 class defaults + 1 granted)", got, want)
	}
}

// TestJoinUnknownTokenUnaffectedByArchive: a token that was never archived
// (and is not live) always mints a brand-new character — the archive lookup
// is a no-op for it, and name/class/species validation still applies.
func TestJoinUnknownTokenUnaffectedByArchive(t *testing.T) {
	t.Parallel()

	w, _ := newTimedWorld(t)

	resp, err := w.Join("never-seen-token", "fresh", protocol.ClassMage, protocol.SpeciesDwarf)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	if got, want := w.ArchivedForTest("never-seen-token"), false; got != want {
		t.Errorf("ArchivedForTest(never-seen-token) = %v, want %v", got, want)
	}

	e, ok := entityOfSnap(w.Snapshot(), resp.EntityID)
	if !ok {
		t.Fatalf("joined entity %d not present in snapshot", resp.EntityID)
	}

	if got, want := e.Class, protocol.ClassMage; got != want {
		t.Errorf("new entity Class = %q, want %q (own token, not restored)", got, want)
	}

	// An unknown token in the request is discarded — Join mints its own
	// random token for a genuinely new character, exactly as before archiving
	// existed.
	if got, notWant := resp.Token, "never-seen-token"; got == notWant {
		t.Errorf("new entity token = %q, want a freshly minted token, not the unknown one supplied", got)
	}
}

// TestArchiveDoesNotCoverPartyOrQuest: sweeping a partied player with an
// active quest dissolves the party and returns the quest to the board — the
// archive holds only progression (identity/XP/gear), never social state —
// and a restore comes back partyless with no active quest, matching the
// spec's "progression persists, social state does not" rule.
func TestArchiveDoesNotCoverPartyOrQuest(t *testing.T) {
	t.Parallel()

	w, clk := newTimedWorld(t)
	w.SetDisconnectGraceForTest(archiveGrace)

	a, err := w.Join("", "alice", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join a: %v", err)
	}

	b, err := w.Join("", "bob", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join b: %v", err)
	}

	w.StreamOpened(a.Token)
	w.StreamOpened(b.Token)

	if _, err := w.QuestTake(a.Token, 1); err != nil {
		t.Fatalf("QuestTake: %v", err)
	}

	if _, err := w.PartyInvite(a.Token, "bob"); err != nil {
		t.Fatalf("PartyInvite: %v", err)
	}

	if _, err := w.PartyAccept(b.Token); err != nil {
		t.Fatalf("PartyAccept: %v", err)
	}

	// Disconnect and sweep alice only.
	w.StreamClosed(a.Token)
	clk.advance(archiveGrace + time.Second)
	w.SweepForTest(clk.now())

	back, err := w.Join(a.Token, "ignored", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("restoring Join: %v", err)
	}

	restored, ok := entityOfSnap(w.Snapshot(), back.EntityID)
	if !ok {
		t.Fatalf("restored entity %d not present in snapshot", back.EntityID)
	}

	if got, want := restored.PartyID, int64(0); got != want {
		t.Errorf("restored PartyID = %d, want %d (party dissolved, not archived)", got, want)
	}

	snap := w.Snapshot()

	var q1 protocol.QuestView

	for _, q := range snap.Quests {
		if q.ID == 1 {
			q1 = q
		}
	}

	if got, want := q1.State, protocol.QuestAvailable; got != want {
		t.Errorf("quest #1 state after sweep+restore = %q, want %q (returned to the board)", got, want)
	}

	if got, want := q1.HolderEntityID, int64(0); got != want {
		t.Errorf("quest #1 HolderEntityID = %d, want %d", got, want)
	}

	if got, want := q1.HolderPartyID, int64(0); got != want {
		t.Errorf("quest #1 HolderPartyID = %d, want %d", got, want)
	}
}

// TestArchivePreservesSkillState (#192): a swept player's learned skills,
// unspent bank, and high-water mark survive a rejoin. Before #192 the archive
// record had no skill fields, so restore rebuilt the entity with none — a
// silent full respec, and worse, the next XP event re-paid every level's points
// because pointsGrantedLevel came back 0 with xp intact. This is the coverage
// archive_test.go lacked because it predated #124.
func TestArchivePreservesSkillState(t *testing.T) {
	t.Parallel()

	w, clk := newTimedWorld(t)
	w.SetDisconnectGraceForTest(archiveGrace)

	me, err := w.Join("", "skiller", protocol.ClassFighter, protocol.SpeciesElf)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	// A level-3-ish character with two learned skills, a spent bank, and the
	// high-water mark already paid up to level 3.
	w.SetXPForTest(me.EntityID, 3*protocol.XPCurveBase+10)
	w.SetSkillStateForTest(me.EntityID, []string{skillCombatTrainingID, skillWeakSpotID}, 1, 3)

	wantLearned, wantPoints, wantGranted := w.SkillStateForTest(me.EntityID)

	w.StreamOpened(me.Token)
	w.StreamClosed(me.Token)
	clk.advance(archiveGrace + time.Second)

	if !w.SweepForTest(clk.now()) {
		t.Fatal("sweep did not remove the disconnected player")
	}

	back, err := w.Join(me.Token, "ignored", protocol.ClassRogue, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("restoring Join: %v", err)
	}

	gotLearned, gotPoints, gotGranted := w.SkillStateForTest(back.EntityID)

	if got, want := len(gotLearned), len(wantLearned); got != want {
		t.Fatalf("restored learned = %v, want %v", gotLearned, wantLearned)
	}

	for i := range wantLearned {
		if gotLearned[i] != wantLearned[i] {
			t.Errorf("restored learned[%d] = %q, want %q", i, gotLearned[i], wantLearned[i])
		}
	}

	if got, want := gotPoints, wantPoints; got != want {
		t.Errorf("restored skillPoints = %d, want %d", got, want)
	}

	// The high-water mark must survive: if it came back 0, the next XP event
	// would re-pay every level — the refund bug.
	if got, want := gotGranted, wantGranted; got != want {
		t.Errorf("restored pointsGrantedLevel = %d, want %d (0 would re-pay all levels)", got, want)
	}
}
