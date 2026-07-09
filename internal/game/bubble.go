package game

import (
	"slices"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// bubble is a set of entities frozen in local combat time, resolving on its own
// action-gated clock (see resolveBubbleLocked, Task 3). Rebuilt each recompute;
// id is carried across recomputes by membership overlap so gating state
// (ready/deadline) stays stable while members join, leave, or bubbles merge.
type bubble struct {
	id       int64
	members  map[int64]struct{} // entity ids
	ready    map[int64]struct{} // player ids locked in this bubble-turn (Task 3)
	deadline time.Time          // patience timeout for the current bubble-turn (Task 3)
}

// recomputeBubblesLocked rebuilds w.bubbles as a pure function of entity
// positions: connected components under player-anchored CombatRadius adjacency
// (an edge needs a player endpoint — see connectedComponents) that contain at
// least one opposing (player vs monster) pair become bubbles. Each entity's
// bubbleID is set (0 = world domain). Bubble ids carry across recomputes by max
// membership overlap so a bubble's gating state survives a member joining,
// leaving, or two bubbles merging. A freshly formed bubble (no deadline carried
// over) starts its patience clock at now + combatPatience. Callers hold w.mu.
func (w *World) recomputeBubblesLocked(now time.Time) {
	var comps [][]*entity

	for _, comp := range connectedComponents(w.entitiesSlice()) {
		if hasOpposingPair(comp) {
			comps = append(comps, comp)

			continue
		}

		for _, e := range comp {
			e.bubbleID = 0
		}
	}

	ids := w.assignBubbleIDsLocked(comps)
	next := make(map[int64]*bubble, len(comps))

	for i, comp := range comps {
		b := w.carryBubbleLocked(ids[i])
		b.members = make(map[int64]struct{}, len(comp))

		for _, e := range comp {
			e.bubbleID = b.id
			b.members[e.id] = struct{}{}
		}

		// A fresh bubble has no patience clock yet; start it. A carried bubble
		// keeps its running deadline so waiting time is not reset by a member
		// merely moving, joining, or leaving mid-wait.
		if b.deadline.IsZero() {
			b.deadline = now.Add(w.combatPatience)
		}

		next[b.id] = b
	}

	w.bubbles = next
}

// entitiesSlice returns every entity sorted by id, so component grouping and
// bubble-id assignment are deterministic. Callers hold w.mu.
func (w *World) entitiesSlice() []*entity {
	out := make([]*entity, 0, len(w.entities))
	for _, e := range w.entities {
		out = append(out, e)
	}

	slices.SortFunc(out, func(a, b *entity) int { return int(a.id - b.id) })

	return out
}

// connectedComponents groups entities into maximal sets connected by
// player-anchored CombatRadius adjacency: an edge iff HexDistance <=
// CombatRadius AND at least one endpoint is a player. Only players extend a
// bubble's reach — monsters attach to players (a player↔monster edge) but never
// chain the bubble through each other, so an enemy walking in joins the fight
// without enlarging the frozen (combat) area, while reinforcing players still
// grow it. Input must be sorted by id; each component preserves that order, and
// components come out ordered by their lowest member id, so downstream id
// assignment is stable.
//
// Dropping monster↔monster edges introduces one harmless boundary case: a
// wandering monster within CombatRadius of a *bubble monster* but far from every
// bubble player stays world-domain, so two same-faction monsters can momentarily
// co-locate across the world/bubble boundary. That is inert — monsters don't
// fight monsters, and player↔monster domain scoping is unaffected because any
// monster adjacent to a bubble player is still linked in via a player↔monster
// edge.
func connectedComponents(ents []*entity) [][]*entity {
	uf := newUnionFind(len(ents))

	for i := range ents {
		for j := i + 1; j < len(ents); j++ {
			// Only players extend a bubble's reach: require a player endpoint, so
			// monster↔monster proximity never links two entities.
			if HexDistance(ents[i].hex, ents[j].hex) <= protocol.CombatRadius &&
				(ents[i].kind == protocol.EntityPlayer || ents[j].kind == protocol.EntityPlayer) {
				uf.union(i, j)
			}
		}
	}

	groups := make(map[int][]*entity)

	for i, e := range ents {
		root := uf.find(i)
		groups[root] = append(groups[root], e)
	}

	comps := make([][]*entity, 0, len(groups))
	for _, g := range groups {
		comps = append(comps, g)
	}

	slices.SortFunc(comps, func(a, b []*entity) int { return int(a[0].id - b[0].id) })

	return comps
}

// hasOpposingPair reports whether comp contains at least one player and one
// monster — the condition that turns a proximity cluster into a combat bubble.
func hasOpposingPair(comp []*entity) bool {
	var hasPlayer, hasMonster bool

	for _, e := range comp {
		switch e.kind {
		case protocol.EntityPlayer:
			hasPlayer = true
		case protocol.EntityMonster:
			hasMonster = true
		}
	}

	return hasPlayer && hasMonster
}

// bubbleMatch is a candidate carry-over of a previous bubble's id onto a new
// component, ranked by how many members they share.
type bubbleMatch struct {
	comp    int
	old     int64
	overlap int
}

// assignBubbleIDsLocked chooses a stable id for each bubble component. A
// component inherits the id of the previous bubble it shares the most members
// with (so ready/deadline survive joins, leaves, and merges); each previous id
// is claimed by at most one component, and unmatched components mint a fresh id
// from nextBubbleID. Returns ids parallel to comps. Callers hold w.mu.
func (w *World) assignBubbleIDsLocked(comps [][]*entity) []int64 {
	ids := make([]int64, len(comps))
	matches := make([]bubbleMatch, 0)

	for ci, comp := range comps {
		for oldID, b := range w.bubbles {
			if ov := overlapCount(comp, b); ov > 0 {
				matches = append(matches, bubbleMatch{comp: ci, old: oldID, overlap: ov})
			}
		}
	}

	slices.SortFunc(matches, compareBubbleMatch)

	claimed := make(map[int64]bool, len(w.bubbles))

	for _, m := range matches {
		if ids[m.comp] != 0 || claimed[m.old] {
			continue
		}

		ids[m.comp] = m.old
		claimed[m.old] = true
	}

	for i := range ids {
		if ids[i] == 0 {
			w.nextBubbleID++
			ids[i] = w.nextBubbleID
		}
	}

	return ids
}

// compareBubbleMatch ranks matches by descending overlap, then by component and
// previous id, so id carry-over does not depend on map iteration order.
func compareBubbleMatch(a, b bubbleMatch) int {
	if a.overlap != b.overlap {
		return b.overlap - a.overlap
	}

	if a.comp != b.comp {
		return a.comp - b.comp
	}

	return int(a.old - b.old)
}

// overlapCount is the number of a component's members that belonged to bubble b.
func overlapCount(comp []*entity, b *bubble) int {
	n := 0

	for _, e := range comp {
		if _, ok := b.members[e.id]; ok {
			n++
		}
	}

	return n
}

// carryBubbleLocked returns the previous bubble with id (preserving its ready
// and deadline gating state) or a fresh one; the caller refills members.
// Reads the pre-recompute w.bubbles, so an inherited id keeps its old state.
// Callers hold w.mu.
func (w *World) carryBubbleLocked(id int64) *bubble {
	if b, ok := w.bubbles[id]; ok {
		return b
	}

	return &bubble{id: id, members: map[int64]struct{}{}, ready: map[int64]struct{}{}}
}

// bubbleViewLocked renders a bubble for the wire: member ids sorted, the player
// members not yet locked in (WaitingForIDs), and patience remaining relative to
// now. Callers hold w.mu.
func (w *World) bubbleViewLocked(b *bubble, now time.Time) protocol.BubbleView {
	memberIDs := make([]int64, 0, len(b.members))

	var waiting []int64

	for id := range b.members {
		memberIDs = append(memberIDs, id)

		e, ok := w.entities[id]
		if !ok || e.kind != protocol.EntityPlayer {
			continue
		}

		if _, ready := b.ready[id]; !ready {
			waiting = append(waiting, id)
		}
	}

	slices.Sort(memberIDs)
	slices.Sort(waiting)

	return protocol.BubbleView{
		ID: b.id, MemberIDs: memberIDs, WaitingForIDs: waiting,
		PatienceRemainingMs: patienceRemainingMs(b.deadline, now),
	}
}

// patienceRemainingMs is the time left, at now, until a bubble's patience
// deadline. It is 0 whenever the deadline is unset or already passed.
func patienceRemainingMs(deadline, now time.Time) int64 {
	if deadline.IsZero() {
		return 0
	}

	return max(0, deadline.Sub(now).Milliseconds())
}

// unionFind is a tiny disjoint-set over entity indices, used to find combat
// connected components.
type unionFind struct{ parent []int }

func newUnionFind(n int) *unionFind {
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}

	return &unionFind{parent: parent}
}

func (u *unionFind) find(x int) int {
	for u.parent[x] != x {
		u.parent[x] = u.parent[u.parent[x]] // path halving
		x = u.parent[x]
	}

	return x
}

func (u *unionFind) union(a, b int) {
	ra, rb := u.find(a), u.find(b)
	if ra != rb {
		u.parent[ra] = rb
	}
}
