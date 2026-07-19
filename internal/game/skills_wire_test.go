package game_test

// skills_wire_test.go (#124 task 7): what the SKILL state looks like from the
// outside — own-only visibility on the turn bundle. Black-box on purpose: it
// asserts the contract a client actually receives, using the same string ids
// the wire carries rather than the engine's unexported constants.

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestSnapshotForIsOwnOnly (#124 task 7 / Q9): a player's skills and point
// bank reach that player's bundle and nobody else's. This is the assertion
// that makes "own-only" true rather than aspirational — the client never has
// to be trusted to hide another player's build, because it never receives it.
//
//nolint:paralleltest // drives a shared world.
func TestSnapshotForIsOwnOnly(t *testing.T) {
	w := newWorld()

	alice, err := w.Join("", "alice", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join alice: %v", err)
	}

	bob, err := w.Join("", "bob", protocol.ClassRogue, protocol.SpeciesElf)
	if err != nil {
		t.Fatalf("Join bob: %v", err)
	}

	w.SetSkillStateForTest(alice.EntityID, []string{"combat-training"}, 3, 2)
	w.SetSkillStateForTest(bob.EntityID, []string{"scouting"}, 5, 3)

	find := func(snap protocol.TurnEvent, id int64) protocol.Entity {
		for _, e := range snap.Entities {
			if e.ID == id {
				return e
			}
		}

		t.Fatalf("entity %d missing from bundle", id)

		return protocol.Entity{}
	}

	// Alice's own bundle: her state is present, Bob's is not.
	forAlice := w.SnapshotFor(alice.Token)

	if got := find(forAlice, alice.EntityID); got.SkillPoints != 3 || len(got.Skills) == 0 {
		t.Errorf("alice's own row = %d points / %d skills, want 3 and non-empty", got.SkillPoints, len(got.Skills))
	}

	if got := find(forAlice, bob.EntityID); got.SkillPoints != 0 || len(got.Skills) != 0 {
		t.Errorf("bob's row in ALICE's bundle = %d points / %d skills, want 0 and empty (own-only)",
			got.SkillPoints, len(got.Skills))
	}

	// And symmetrically for Bob.
	forBob := w.SnapshotFor(bob.Token)

	if got := find(forBob, bob.EntityID); got.SkillPoints != 5 {
		t.Errorf("bob's own row = %d points, want 5", got.SkillPoints)
	}

	if got := find(forBob, alice.EntityID); got.SkillPoints != 0 || len(got.Skills) != 0 {
		t.Errorf("alice's row in BOB's bundle = %d points / %d skills, want 0 and empty",
			got.SkillPoints, len(got.Skills))
	}

	// A viewer-less bundle (tests, a token-less watcher) carries nobody's.
	anon := w.Snapshot()
	if got := find(anon, alice.EntityID); got.SkillPoints != 0 || len(got.Skills) != 0 {
		t.Errorf("alice's row in a VIEWERLESS bundle = %d points / %d skills, want 0 and empty",
			got.SkillPoints, len(got.Skills))
	}
}
