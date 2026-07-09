package game

import (
	"errors"
	"fmt"
	"strings"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// Party command failures. The HTTP layer maps all of these to 422.
var (
	ErrTargetNotFound  = errors.New("no such player")
	ErrInviteSelf      = errors.New("cannot invite yourself")
	ErrNoPendingInvite = errors.New("no pending party invite")
	ErrInviteExpired   = errors.New("that invite is no longer valid")
	ErrAlreadyInParty  = errors.New("already in that party")
	ErrNotInParty      = errors.New("not in a party")
	ErrPartyNotJoined  = errors.New("not joined")
)

// PartyInvite records a pending invite from the token holder to the nearest
// player named targetName. Returns the chat announcement to broadcast.
func (w *World) PartyInvite(token, targetName string) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	inviter, ok := w.byToken[token]
	if !ok || token == "" {
		return "", ErrPartyNotJoined
	}

	targetName = strings.TrimSpace(targetName)

	target := w.nearestPlayerByNameLocked(targetName, inviter)
	if target == nil {
		if targetName == inviter.name {
			return "", ErrInviteSelf
		}

		return "", ErrTargetNotFound
	}

	w.pendingInvites[target.id] = inviter.id

	return fmt.Sprintf("%s invited %s to a party — %s: /accept", inviter.name, target.name, target.name), nil
}

// PartyAccept joins the token holder to the party of whoever invited them.
func (w *World) PartyAccept(token string) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	accepter, ok := w.byToken[token]
	if !ok || token == "" {
		return "", ErrPartyNotJoined
	}

	inviterID, ok := w.pendingInvites[accepter.id]
	if !ok {
		return "", ErrNoPendingInvite
	}

	delete(w.pendingInvites, accepter.id)

	inviter, ok := w.entities[inviterID]
	if !ok {
		return "", ErrInviteExpired
	}

	if accepter.partyID != 0 && accepter.partyID == inviter.partyID {
		return "", ErrAlreadyInParty
	}

	w.leavePartyLocked(accepter)
	w.abandonPersonalQuestLocked(accepter)

	if inviter.partyID == 0 {
		w.nextPartyID++
		inviter.partyID = w.nextPartyID
	}

	accepter.partyID = inviter.partyID

	return fmt.Sprintf("%s joined %s's party", accepter.name, inviter.name), nil
}

// PartyLeave removes the token holder from their party (dissolving it if it
// drops below two members).
func (w *World) PartyLeave(token string) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	e, ok := w.byToken[token]
	if !ok || token == "" {
		return "", ErrPartyNotJoined
	}

	if e.partyID == 0 {
		return "", ErrNotInParty
	}

	name := e.name
	w.leavePartyLocked(e)

	return name + " left the party", nil
}

// minPartySize is the fewest members a party can have before it dissolves —
// a "party" of one is just a player, so leavePartyLocked clears the last
// member's id too.
const minPartySize = 2

// leavePartyLocked clears e's party and dissolves the remainder if it now has
// fewer than two members. Callers hold w.mu. Also used by the disconnect sweep.
func (w *World) leavePartyLocked(e *entity) {
	pid := e.partyID
	if pid == 0 {
		return
	}

	e.partyID = 0

	var members []*entity

	for _, o := range w.entities {
		if o.partyID == pid {
			members = append(members, o)
		}
	}

	if len(members) < minPartySize {
		for _, o := range members {
			o.partyID = 0
		}

		w.returnPartyQuestLocked(pid)
	}
}

// nearestPlayerByNameLocked returns the player named name closest to inviter
// (excluding inviter itself), lowest-id on a distance tie, or nil if none.
func (w *World) nearestPlayerByNameLocked(name string, inviter *entity) *entity {
	var best *entity

	bestDist := 0

	for _, o := range w.entities {
		if o.kind != protocol.EntityPlayer || o.id == inviter.id || o.name != name {
			continue
		}

		d := HexDistance(inviter.hex, o.hex)
		if best == nil || d < bestDist || (d == bestDist && o.id < best.id) {
			best = o
			bestDist = d
		}
	}

	return best
}
