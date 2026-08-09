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

func newPartyWorld(t *testing.T) *game.World {
	t.Helper()

	return game.NewWorld(game.WorldConfig{
		Interval:        time.Hour,
		CombatPatience:  time.Second,
		BubblePoll:      time.Millisecond,
		DisconnectGrace: time.Minute,
		WorldSeed:       0xC0FFEE,
		Radius:          12,
		Ticks:           hub.New(),
	})
}

func joinNamed(t *testing.T, w *game.World, name string) protocol.JoinResponse {
	t.Helper()

	resp, err := w.Join("", name, protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("join %s: %v", name, err)
	}

	return resp
}

// partyIDOf reads an entity's PartyID off the snapshot.
func partyIDOf(t *testing.T, w *game.World, id int64) int64 {
	t.Helper()

	for _, e := range w.Snapshot().Entities {
		if e.ID == id {
			return e.PartyID
		}
	}

	t.Fatalf("entity %d not in snapshot", id)

	return 0
}

func TestInviteAcceptFormsSharedParty(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)
	alice := joinNamed(t, w, "alice")
	bob := joinNamed(t, w, "bob")

	if _, err := w.PartyInvite(alice.Token, "bob"); err != nil {
		t.Fatalf("invite: %v", err)
	}

	if _, err := w.PartyAccept(bob.Token); err != nil {
		t.Fatalf("accept: %v", err)
	}

	pa, pb := partyIDOf(t, w, alice.EntityID), partyIDOf(t, w, bob.EntityID)
	if pa == 0 || pa != pb {
		t.Errorf("party ids: alice=%d bob=%d, want equal non-zero", pa, pb)
	}
}

func TestAcceptWithoutInvite(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)
	bob := joinNamed(t, w, "bob")

	if _, err := w.PartyAccept(bob.Token); !errors.Is(err, game.ErrNoPendingInvite) {
		t.Errorf("err = %v, want ErrNoPendingInvite", err)
	}
}

func TestInviteUnknownName(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)
	alice := joinNamed(t, w, "alice")

	if _, err := w.PartyInvite(alice.Token, "ghost"); !errors.Is(err, game.ErrTargetNotFound) {
		t.Errorf("err = %v, want ErrTargetNotFound", err)
	}
}

func TestInviteSelf(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)
	alice := joinNamed(t, w, "alice")

	if _, err := w.PartyInvite(alice.Token, "alice"); !errors.Is(err, game.ErrInviteSelf) {
		t.Errorf("err = %v, want ErrInviteSelf", err)
	}
}

func TestLeaveFromPairDissolves(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)
	alice := joinNamed(t, w, "alice")
	bob := joinNamed(t, w, "bob")
	mustInviteAccept(t, w, alice, bob, "bob")

	if _, err := w.PartyLeave(bob.Token); err != nil {
		t.Fatalf("leave: %v", err)
	}

	if got := partyIDOf(t, w, alice.EntityID); got != 0 {
		t.Errorf("alice party = %d after pair leave, want 0 (dissolved)", got)
	}

	if got := partyIDOf(t, w, bob.EntityID); got != 0 {
		t.Errorf("bob party = %d after leave, want 0", got)
	}
}

func TestLeaveFromTrioKeepsOthers(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)
	alice := joinNamed(t, w, "alice")
	bob := joinNamed(t, w, "bob")
	carol := joinNamed(t, w, "carol")
	mustInviteAccept(t, w, alice, bob, "bob")
	mustInviteAccept(t, w, alice, carol, "carol")

	if _, err := w.PartyLeave(bob.Token); err != nil {
		t.Fatalf("leave: %v", err)
	}

	pa, pc := partyIDOf(t, w, alice.EntityID), partyIDOf(t, w, carol.EntityID)
	if pa == 0 || pa != pc {
		t.Errorf("after bob leaves trio: alice=%d carol=%d, want equal non-zero", pa, pc)
	}

	if got := partyIDOf(t, w, bob.EntityID); got != 0 {
		t.Errorf("bob party = %d after leave, want 0", got)
	}
}

func TestLeaveWhenSolo(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)
	alice := joinNamed(t, w, "alice")

	if _, err := w.PartyLeave(alice.Token); !errors.Is(err, game.ErrNotInParty) {
		t.Errorf("err = %v, want ErrNotInParty", err)
	}
}

func mustInviteAccept(t *testing.T, w *game.World, inviter, invitee protocol.JoinResponse, inviteeName string) {
	t.Helper()

	if _, err := w.PartyInvite(inviter.Token, inviteeName); err != nil {
		t.Fatalf("invite %s: %v", inviteeName, err)
	}

	if _, err := w.PartyAccept(invitee.Token); err != nil {
		t.Fatalf("accept %s: %v", inviteeName, err)
	}
}

// TestDisconnectSweepDissolvesParty: sweeping one member of a pair past the
// disconnect grace dissolves the party — the survivor's PartyID returns to 0.
// Uses the timed-world harness from presence_test.go so the sweep can be
// driven deterministically.
func TestDisconnectSweepDissolvesParty(t *testing.T) {
	t.Parallel()

	w, clk := newTimedWorld(t)
	w.SetDisconnectGraceForTest(presenceGrace)

	alice := joinNamed(t, w, "alice")
	bob := joinNamed(t, w, "bob")

	w.StreamOpened(alice.Token)
	w.StreamOpened(bob.Token)

	mustInviteAccept(t, w, alice, bob, "bob")

	if pa, pb := partyIDOf(t, w, alice.EntityID), partyIDOf(t, w, bob.EntityID); pa == 0 || pa != pb {
		t.Fatalf("party ids before sweep: alice=%d bob=%d, want equal non-zero", pa, pb)
	}

	w.StreamClosed(bob.Token) // bob disconnects; alice stays connected
	clk.advance(presenceGrace + time.Second)

	if got, want := w.SweepForTest(clk.now()), true; got != want {
		t.Errorf("SweepForTest removed = %v, want %v", got, want)
	}

	if got := partyIDOf(t, w, alice.EntityID); got != 0 {
		t.Errorf("alice party = %d after bob's disconnect sweep, want 0 (dissolved)", got)
	}
}

// TestPendingInviteIsOwnOnly pins the #385 wire field: an invite reaches the
// bundle of the player who has to answer it, and nobody else's — not the
// inviter's, and not a viewer-less snapshot's.
//
// The own-only half is the part that rots silently. A field wired into the
// shared bundle instead of the per-viewer one would still make every test
// about the invitee pass, while quietly telling the whole world who has been
// asked to join whom.
func TestPendingInviteIsOwnOnly(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)
	alice := joinNamed(t, w, "alice")
	bob := joinNamed(t, w, "bob")

	if got := w.SnapshotFor(bob.Token).PendingInvite; got != nil {
		t.Fatalf("pending invite before any was sent: %+v", got)
	}

	if _, err := w.PartyInvite(alice.Token, "bob"); err != nil {
		t.Fatalf("invite: %v", err)
	}

	invite := w.SnapshotFor(bob.Token).PendingInvite
	if invite == nil {
		t.Fatal("invitee's bundle carries no pending invite")
	}

	if got, want := invite.InviterName, "alice"; got != want {
		t.Errorf("InviterName = %q, want %q", got, want)
	}

	if got, want := invite.InviterID, alice.EntityID; got != want {
		t.Errorf("InviterID = %d, want %d", got, want)
	}

	if got := w.SnapshotFor(alice.Token).PendingInvite; got != nil {
		t.Errorf("inviter's own bundle carries the invite: %+v", got)
	}

	if got := w.Snapshot().PendingInvite; got != nil {
		t.Errorf("viewer-less snapshot carries the invite: %+v", got)
	}
}

// TestPendingInviteCarriesInvitersParty pins the roster the prompt shows
// (#385, maintainer's call 2026-08-08): you are deciding whether to join a
// GROUP, so the panel names the group, not just whoever typed /invite.
//
// The empty case is the one worth pinning. A solo inviter has no party yet —
// accepting CREATES one — so Members is empty rather than a one-entry roster
// naming the inviter twice, once as the asker and once as a member.
func TestPendingInviteCarriesInvitersParty(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)
	alice := joinNamed(t, w, "alice")
	bob := joinNamed(t, w, "bob")
	carol := joinNamed(t, w, "carol")

	// Solo inviter: no party exists yet, so there is no roster to show.
	if _, err := w.PartyInvite(alice.Token, "bob"); err != nil {
		t.Fatalf("invite bob: %v", err)
	}

	invite := w.SnapshotFor(bob.Token).PendingInvite
	if invite == nil {
		t.Fatal("invitee's bundle carries no pending invite")
	}

	if got := len(invite.Members); got != 0 {
		t.Errorf("Members = %+v, want empty for a solo inviter", invite.Members)
	}

	// Bob accepts, so alice+bob are now a party of two.
	if _, err := w.PartyAccept(bob.Token); err != nil {
		t.Fatalf("accept: %v", err)
	}

	if _, err := w.PartyInvite(alice.Token, "carol"); err != nil {
		t.Fatalf("invite carol: %v", err)
	}

	invite = w.SnapshotFor(carol.Token).PendingInvite
	if invite == nil {
		t.Fatal("carol's bundle carries no pending invite")
	}

	names := make([]string, 0, len(invite.Members))
	for _, m := range invite.Members {
		names = append(names, m.Name)
	}

	// id-sorted, like the roster — alice joined first.
	if got, want := strings.Join(names, ","), "alice,bob"; got != want {
		t.Errorf("Members = %q, want %q", got, want)
	}
}

// TestPendingInviteClearsOnAccept: the prompt has to go away by itself. The
// server already deletes the pending invite in PartyAccept, so this pins that
// the WIRE follows the state rather than needing its own clearing step.
func TestPendingInviteClearsOnAccept(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)
	alice := joinNamed(t, w, "alice")
	bob := joinNamed(t, w, "bob")

	if _, err := w.PartyInvite(alice.Token, "bob"); err != nil {
		t.Fatalf("invite: %v", err)
	}

	if _, err := w.PartyAccept(bob.Token); err != nil {
		t.Fatalf("accept: %v", err)
	}

	if got := w.SnapshotFor(bob.Token).PendingInvite; got != nil {
		t.Errorf("invite still pending after accepting it: %+v", got)
	}
}

// TestPartyDeclineClearsTheInvite: declining is a real server-side answer, not
// a client-side dismissal (maintainer's call 2026-08-08). The pending invite
// is gone afterwards — the prompt clears, and a later /accept has nothing to
// accept.
func TestPartyDeclineClearsTheInvite(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)
	alice := joinNamed(t, w, "alice")
	bob := joinNamed(t, w, "bob")

	if _, err := w.PartyInvite(alice.Token, "bob"); err != nil {
		t.Fatalf("invite: %v", err)
	}

	line, recipient, err := w.PartyDecline(bob.Token)
	if err != nil {
		t.Fatalf("decline: %v", err)
	}

	// The line goes to the INVITER, not the world: a broadcast decline is
	// socially expensive in a fifteen-friend group.
	if got, want := recipient, alice.EntityID; got != want {
		t.Errorf("recipient = %d, want alice's id %d", got, want)
	}

	if got, want := line, "bob"; !strings.Contains(got, want) {
		t.Errorf("line = %q, should name the decliner %q", got, want)
	}

	if got := w.SnapshotFor(bob.Token).PendingInvite; got != nil {
		t.Errorf("invite still pending after decline: %+v", got)
	}

	if _, err := w.PartyAccept(bob.Token); !errors.Is(err, game.ErrNoPendingInvite) {
		t.Errorf("accept after decline: err = %v, want ErrNoPendingInvite", err)
	}
}

// TestPartyDeclineWithNothingPending: /decline out of the blue is a 422, the
// same shape /accept already has.
func TestPartyDeclineWithNothingPending(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)
	joinNamed(t, w, "alice")
	bob := joinNamed(t, w, "bob")

	if _, _, err := w.PartyDecline(bob.Token); !errors.Is(err, game.ErrNoPendingInvite) {
		t.Errorf("err = %v, want ErrNoPendingInvite", err)
	}
}

// TestPartyDeclineDoesNotBlockReInviting pins the maintainer's call that a
// decline carries no cooldown (2026-08-08): fifteen friends, so invite spam is
// a social problem rather than one the server should model.
func TestPartyDeclineDoesNotBlockReInviting(t *testing.T) {
	t.Parallel()

	w := newPartyWorld(t)
	alice := joinNamed(t, w, "alice")
	bob := joinNamed(t, w, "bob")

	if _, err := w.PartyInvite(alice.Token, "bob"); err != nil {
		t.Fatalf("invite: %v", err)
	}

	if _, _, err := w.PartyDecline(bob.Token); err != nil {
		t.Fatalf("decline: %v", err)
	}

	if _, err := w.PartyInvite(alice.Token, "bob"); err != nil {
		t.Fatalf("re-invite immediately after a decline: %v", err)
	}

	if got := w.SnapshotFor(bob.Token).PendingInvite; got == nil {
		t.Error("re-invite after decline did not reach bob's bundle")
	}
}
