package game

import (
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"slices"
	"strings"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// Quest command failures; the HTTP layer maps them to 422.
var (
	ErrQuestNotFound  = errors.New("no such quest")
	ErrQuestTaken     = errors.New("that quest is already taken")
	ErrQuestCompleted = errors.New("that quest is already completed")
	ErrQuestSlotFull  = errors.New("you already have an active quest — /abandon it first")
	ErrNoActiveQuest  = errors.New("no active quest")
)

const (
	questKindKill  = "kill"
	questKindReach = "reach"

	// Board shape: modest kill counts (monsters don't respawn) and reach goals
	// far enough from the spawn clearing to be a real trip.
	questCount               = 6
	questKillMin             = 2
	questKillMax             = 4
	questReachMinDist        = 8
	questKillRewardPerTarget = protocol.MonsterXP
	questReachRewardXP       = 20
)

// quest is one board entry. All access under w.mu.
type quest struct {
	id           int64
	name         string
	kind         string
	targetN      int
	goalHex      protocol.Hex
	rewardXP     int
	state        protocol.QuestState
	progress     int
	holderEntity int64
	holderParty  int64
}

//nolint:gochecknoglobals // fixed name templates, effectively const.
var (
	questKillNames  = []string{"Cull the pack", "Thin the horde", "Clear the road"}
	questReachNames = []string{"Scout the far shore", "Survey the frontier", "Plant the banner"}
)

// generateQuests builds the deterministic boot-time board: 3 kill + 3 reach.
// Draws only from a PCG seeded by the world seed; reach goals come from a
// SORTED slice of reachable hexes (map iteration order would break
// determinism) and sit at least questReachMinDist from the origin.
func generateQuests(seed uint64, m protocol.MapResponse) []*quest {
	// 0x9E3779B97F4A7C15 is a domain-separation salt for the quest stream.
	//nolint:gosec,mnd // deterministic seeded generation, not security-sensitive.
	rng := mrand.New(mrand.NewPCG(seed, 0x9E3779B97F4A7C15))

	// Candidate goals: reachable, walkable, far enough out — sorted for determinism.
	origin := protocol.Hex{Q: 0, R: 0}
	reach := reachableWalkable(m)

	var goals []protocol.Hex

	for h := range reach {
		if HexDistance(origin, h) >= questReachMinDist {
			goals = append(goals, h)
		}
	}

	// A tiny WORLD_RADIUS can leave no candidate at questReachMinDist; fall
	// back to every reachable non-origin hex so boot never panics on IntN(0).
	// A world too small even for that (radius 1) targets the origin itself.
	if len(goals) == 0 {
		for h := range reach {
			if h != origin {
				goals = append(goals, h)
			}
		}
	}

	if len(goals) == 0 {
		goals = append(goals, origin)
	}

	slices.SortFunc(goals, func(a, b protocol.Hex) int {
		if a.Q != b.Q {
			return a.Q - b.Q
		}

		return a.R - b.R
	})

	quests := make([]*quest, 0, questCount)

	for i, name := range questKillNames {
		n := questKillMin + rng.IntN(questKillMax-questKillMin+1)
		quests = append(quests, &quest{
			id: int64(i + 1), name: name, kind: questKindKill,
			targetN: n, rewardXP: n * questKillRewardPerTarget,
			state: protocol.QuestAvailable,
		})
	}

	for i, name := range questReachNames {
		goal := goals[rng.IntN(len(goals))]
		quests = append(quests, &quest{
			id: int64(len(questKillNames) + i + 1), name: name, kind: questKindReach,
			goalHex: goal, rewardXP: questReachRewardXP,
			state: protocol.QuestAvailable,
		})
	}

	return quests
}

// SetAnnounce installs the chat hook used for in-resolution quest events
// (completion, auto-abandon). Call before Run; defaults to a no-op.
func (w *World) SetAnnounce(fn func(sender, text string)) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.announce = fn
}

// QuestTake claims an available quest for the caller — for their party when
// they are in one, personally otherwise. One active quest per slot.
func (w *World) QuestTake(token string, questID int64) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	e, ok := w.byToken[token]
	if !ok || token == "" {
		return "", ErrPartyNotJoined
	}

	q := w.questByIDLocked(questID)
	if q == nil {
		return "", ErrQuestNotFound
	}

	if q.state == protocol.QuestCompleted {
		return "", ErrQuestCompleted
	}

	if q.state != protocol.QuestAvailable {
		return "", ErrQuestTaken
	}

	if w.activeQuestLocked(e) != nil {
		return "", ErrQuestSlotFull
	}

	q.state = protocol.QuestTaken

	if e.partyID != 0 {
		q.holderParty = e.partyID

		return fmt.Sprintf("%s's party took quest #%d: %s", e.name, q.id, q.name), nil
	}

	q.holderEntity = e.id

	return fmt.Sprintf("%s took quest #%d: %s", e.name, q.id, q.name), nil
}

// QuestAbandon returns the caller's active quest (personal or their party's)
// to the board with progress reset.
func (w *World) QuestAbandon(token string) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	e, ok := w.byToken[token]
	if !ok || token == "" {
		return "", ErrPartyNotJoined
	}

	q := w.activeQuestLocked(e)
	if q == nil {
		return "", ErrNoActiveQuest
	}

	w.resetQuestLocked(q)

	return fmt.Sprintf("%s abandoned quest #%d: %s", e.name, q.id, q.name), nil
}

// activeQuestLocked is e's taken quest: personal, or their party's.
func (w *World) activeQuestLocked(e *entity) *quest {
	for _, q := range w.quests {
		if q.state != protocol.QuestTaken {
			continue
		}

		if q.holderEntity == e.id || (e.partyID != 0 && q.holderParty == e.partyID) {
			return q
		}
	}

	return nil
}

func (w *World) questByIDLocked(id int64) *quest {
	for _, q := range w.quests {
		if q.id == id {
			return q
		}
	}

	return nil
}

// resetQuestLocked puts a quest back on the board (abandon/dissolve/sweep).
func (w *World) resetQuestLocked(q *quest) {
	q.state = protocol.QuestAvailable
	q.progress = 0
	q.holderEntity = 0
	q.holderParty = 0
}

// personalQuestLocked returns e's PERSONAL taken quest (holderEntity == e.id),
// or nil if e holds none. Shared lookup for abandonPersonalQuestLocked and
// promotePersonalQuestLocked, which differ only in what they do with the
// match.
func (w *World) personalQuestLocked(e *entity) *quest {
	for _, q := range w.quests {
		if q.state == protocol.QuestTaken && q.holderEntity == e.id {
			return q
		}
	}

	return nil
}

// abandonPersonalQuestLocked returns e's PERSONAL quest to the board (used
// when e joins a party — the slot becomes the party's agenda — and by the
// disconnect sweep). No-op without one. Announces.
func (w *World) abandonPersonalQuestLocked(e *entity) {
	q := w.personalQuestLocked(e)
	if q == nil {
		return
	}

	w.resetQuestLocked(q)
	w.announce("system", fmt.Sprintf("quest #%d (%s) returned to the board", q.id, q.name))
}

// promotePersonalQuestLocked converts e's PERSONAL quest into e's (freshly
// minted) party's quest, called from PartyAccept's mint-new-party branch: the
// party forms AROUND whatever quest the inviter had pitched, rather than
// abandoning it — invariant is that nobody in a party holds a personal
// quest. Progress carries over unchanged. No-op without one. Announces.
func (w *World) promotePersonalQuestLocked(e *entity) {
	q := w.personalQuestLocked(e)
	if q == nil {
		return
	}

	q.holderEntity = 0
	q.holderParty = e.partyID
	w.announce("system", fmt.Sprintf("quest #%d (%s) is now %s's party's quest", q.id, q.name, e.name))
}

// returnPartyQuestLocked returns a dissolved party's quest to the board.
func (w *World) returnPartyQuestLocked(partyID int64) {
	for _, q := range w.quests {
		if q.state == protocol.QuestTaken && q.holderParty == partyID {
			w.resetQuestLocked(q)
			w.announce("system", fmt.Sprintf("quest #%d (%s) returned to the board", q.id, q.name))

			return
		}
	}
}

// tickKillQuestsLocked advances every DISTINCT active kill quest held by a
// surviving player in the bubble — once per quest per turn (a party fight
// ticks its shared quest once). Completes any that reach their target.
func (w *World) tickKillQuestsLocked(members []*entity, killed int) {
	ticked := make(map[int64]bool)

	for _, e := range members {
		if e.kind != protocol.EntityPlayer || e.hp <= 0 {
			continue
		}

		q := w.activeQuestLocked(e)
		if q == nil || q.kind != questKindKill || ticked[q.id] {
			continue
		}

		ticked[q.id] = true

		q.progress = min(q.progress+killed, q.targetN)
		if q.progress >= q.targetN {
			w.completeQuestLocked(q)

			continue
		}

		// Progress feedback where players are actually looking mid-fight: the
		// chat stream. (Completion has its own announcement.)
		w.announce("system", fmt.Sprintf("%s: %d down, %d to go", q.name, q.progress, q.targetN-q.progress))
	}
}

// checkReachQuestsLocked completes any active reach quest one of whose
// holders stands on its goal hex. Called after movement in both domains.
func (w *World) checkReachQuestsLocked() {
	for _, q := range w.quests {
		if q.state != protocol.QuestTaken || q.kind != questKindReach {
			continue
		}

		for _, e := range w.entities {
			if e.kind != protocol.EntityPlayer || e.hex != q.goalHex {
				continue
			}

			if q.holderEntity == e.id || (q.holderParty != 0 && e.partyID == q.holderParty) {
				q.progress = 1
				w.completeQuestLocked(q)

				break
			}
		}
	}
}

// completeQuestLocked pays every current holder the full reward through the
// modifier pipeline (evEarnXP — same event the kill award uses, so the human
// +XP% passive and any future XP cards apply identically) and announces. The
// announce text prints the base rewardXP, not each holder's actual award,
// since holders can differ per-species (Human gets +HumanXPBonusPercent); the
// wording says so explicitly rather than implying it is everyone's exact take.
func (w *World) completeQuestLocked(q *quest) {
	q.state = protocol.QuestCompleted

	var names []string

	for _, e := range w.entities {
		if e.kind != protocol.EntityPlayer {
			continue
		}

		holder := q.holderEntity == e.id || (q.holderParty != 0 && e.partyID == q.holderParty)
		if !holder {
			continue
		}

		award := applyRules(evEarnXP, q.rewardXP, speciesCards(e.species), ruleCtx{})

		e.xp += award
		syncMaxHPLocked(e)
		names = append(names, e.name)
	}

	slices.Sort(names)
	msg := fmt.Sprintf("Quest complete: %s — %s each gain %d XP (species bonuses apply)",
		q.name, strings.Join(names, ", "), q.rewardXP)
	w.announce("system", msg)
}
