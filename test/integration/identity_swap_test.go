package integration_test

// identity_swap_test.go: item 2, playtest feedback batch 3 — the bug
// investigation ("players swapped somehow; I logged out and one other
// player became one"). This proves the seam explicitly named in the
// investigation over real HTTP: two live players, a disconnect-grace sweep
// on one of them, then both rejoining — the server must NEVER hand either
// player's rejoin the OTHER player's identity, regardless of sweep timing or
// rejoin order. The root cause turned out to be client-side (net/session.ts
// re-derived the token to reclaim from localStorage, which two browser tabs
// on one origin share — see that file's fix), but this test rules the
// server out for good and guards the exact multi-client sequence the
// playtest report described.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// joinAs is join/joinClass plus an explicit display name, so a multi-player
// test can tell players apart by the Name that rides every turn bundle.
func joinAs(t *testing.T, ts *httptest.Server, token, name, class string) protocol.JoinResponse {
	t.Helper()

	resp := postJSON(t, ts, "/api/join",
		protocol.JoinRequest{Token: token, Name: name, Class: class, Species: protocol.SpeciesHuman})
	if got, want := resp.StatusCode, http.StatusOK; got != want {
		t.Fatalf("join(%q) status = %d, want 200", name, got)
	}

	var joined protocol.JoinResponse
	if err := json.NewDecoder(resp.Body).Decode(&joined); err != nil {
		t.Fatalf("decode join(%q) response: %v", name, err)
	}

	return joined
}

func TestTwoClientsSweepBetweenNeverSwapIdentities(t *testing.T) {
	t.Parallel()

	grace := 300 * time.Millisecond
	ts := startServerWithGrace(t, 20*time.Millisecond, time.Hour, grace)

	alice := joinAs(t, ts, "", "alice", protocol.ClassFighter)
	bob := joinAs(t, ts, "", "bob", protocol.ClassRogue)

	if alice.Token == bob.Token || alice.EntityID == bob.EntityID {
		t.Fatalf("alice and bob must be distinct: %+v vs %+v", alice, bob)
	}

	// A persistent observer, held open for the whole test, is the ground
	// truth for who is present and what they're named.
	observer := joinAs(t, ts, "", "observer", protocol.ClassFighter)
	obs, _ := openStream(t, ts, observer.Token)

	aliceStream, closeAlice := openStream(t, ts, alice.Token)
	if _, ok := entityOf(decodeBundle(t, aliceStream), alice.EntityID); !ok {
		t.Fatalf("alice %d absent from its own first bundle", alice.EntityID)
	}

	bobStream, closeBob := openStream(t, ts, bob.Token)
	if _, ok := entityOf(decodeBundle(t, bobStream), bob.EntityID); !ok {
		t.Fatalf("bob %d absent from its own first bundle", bob.EntityID)
	}

	waitForPresence(t, obs, alice.EntityID, time.Now().Add(5*time.Second))
	waitForPresence(t, obs, bob.EntityID, time.Now().Add(5*time.Second))

	// Only alice goes quiet — bob's stream stays open the whole time, so bob
	// must never be touched by alice's sweep/rejoin.
	closeAlice()
	waitForAbsence(t, obs, alice.EntityID, time.Now().Add(5*time.Second))

	// Bob must still be exactly bob: present, same entity id, same name.
	bundle := decodeBundle(t, obs)

	bobStill, ok := entityOf(bundle, bob.EntityID)
	if !ok {
		t.Fatalf("bob %d vanished from the world during alice's sweep", bob.EntityID)
	}

	if got, want := bobStill.Name, "bob"; got != want {
		t.Fatalf("bob's entity Name = %q, want %q after alice's sweep — identity swap", got, want)
	}

	// Alice rejoins with her OWN token (exactly what a correct client always
	// sends — see net/session.ts's reclaim doc). The archived-restore branch
	// must hand her back HER OWN record: a new entity id, but her own name
	// and class, never bob's. Name/class are deliberately garbage here to
	// prove they're ignored on a restore.
	aliceRejoin := joinAs(t, ts, alice.Token, "ignored", protocol.ClassMage)

	if got, want := aliceRejoin.Token, alice.Token; got != want {
		t.Fatalf("alice rejoin Token = %q, want %q (same token, restore contract)", got, want)
	}

	if aliceRejoin.EntityID == bob.EntityID {
		t.Fatalf("alice's restored entity id collided with bob's live entity id")
	}

	waitForPresence(t, obs, aliceRejoin.EntityID, time.Now().Add(5*time.Second))

	bundle = decodeBundle(t, obs)

	restoredAlice, ok := entityOf(bundle, aliceRejoin.EntityID)
	if !ok {
		t.Fatalf("alice's restored entity %d never appeared", aliceRejoin.EntityID)
	}

	if got, want := restoredAlice.Name, "alice"; got != want {
		t.Fatalf("restored alice Name = %q, want %q — identity swap", got, want)
	}

	if got, want := restoredAlice.Class, protocol.ClassFighter; got != want {
		t.Fatalf("restored alice Class = %q, want %q — identity swap", got, want)
	}

	// Bob — who was never swept — must STILL be exactly bob after alice's
	// whole sweep-and-restore cycle interleaved with his own live session.
	bobStillEntity, ok := entityOf(bundle, bob.EntityID)
	if !ok {
		t.Fatalf("bob %d vanished after alice's restore", bob.EntityID)
	}

	if got, want := bobStillEntity.Name, "bob"; got != want {
		t.Fatalf("bob's entity Name = %q, want %q after alice's restore — identity swap", got, want)
	}

	if got, want := bobStillEntity.Class, protocol.ClassRogue; got != want {
		t.Fatalf("bob's entity Class = %q, want %q after alice's restore — identity swap", got, want)
	}

	// Finally, bob reclaiming his own (never-dead) token must also come back
	// as bob, unaffected by anything alice did meanwhile.
	bobReclaim := joinAs(t, ts, bob.Token, "ignored", protocol.ClassMage)
	if got, want := bobReclaim.EntityID, bob.EntityID; got != want {
		t.Fatalf("bob reclaim EntityID = %d, want %d (same live entity)", got, want)
	}

	closeBob()
}
