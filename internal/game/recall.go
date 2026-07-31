package game

import (
	"slices"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// recall.go: the targeted-consumable ACTION path (#271, slice 5). The thrown
// flask was removed in #352 — its economy read TTRPG (a scarce, hoarded,
// single-use item) even though its resolution was ARPG-clean — leaving the
// scroll of recall, a consumable used through a NEW intent rather than drunk:
// a self-teleport to safety.
//
// Design: it does not use the free-outside/turn-inside inventory rule
// (commitItemActionLocked). Like an attack or an Evade, a recall is a combat
// action resolved IN the turn pipeline against the evolving board, never
// applied instantly — it reuses Evade's teleport. It is the entity's whole turn
// (it clears any queued move/attack), and the scroll is consumed at RESOLUTION,
// not at submit, so a later intent in the same window cancels the recall and
// keeps the scroll (latest intent wins).

// queueRecallLocked validates a recall intent and queues it as this turn's
// action (#271). The item must be an owned recall consumable (a scroll of
// recall); recall targets the USER, so there is no aim hex, range, or
// line-of-sight check. The scroll is consumed at resolution
// (resolveRecallsLocked), not here — a later intent cancels the recall and
// keeps the scroll. Callers hold w.mu.
func (*World) queueRecallLocked(e *entity, itemID int64) error {
	inst, ok := e.itemByID(itemID)
	if !ok {
		return ErrItemNotOwned
	}

	if !itemDefByID[inst.defID].recall {
		return ErrNotRecallable
	}

	e.clearQueuedActionLocked()
	e.recallItem = itemID

	return nil
}

// clearQueuedActionLocked zeroes every OTHER mutually-exclusive turn action on
// e (path, ranged/melee attack, pending inventory action, active skill), so a
// recall is the entity's whole turn — mirroring how queueMoveLocked and
// useSkillLocked displace one another (latest intent wins). recallItem is set
// by the caller AFTER this runs. Callers hold w.mu.
//
// It took a `keep` selector while the thrown flask shared it (#271); with the
// throw gone (#352) there is exactly one queued action to preserve, so the
// selector would only ever have one value. Callers hold w.mu.
func (e *entity) clearQueuedActionLocked() {
	e.path = nil
	e.attackTarget = nil
	e.attackTargetEntity = 0
	e.pending = pendingItemAction{}
	e.activeSkill = ""
	e.activeTarget = nil
}

// resolveRecallsLocked resolves every queued recall this pass, teleporting each
// user to a safe hex in the shared sanctuary (spawnHexLocked — the same guarded
// placement a join/respawn uses, so the destination is walkable, has room, and
// is not on or beside a monster). It runs in the move phase alongside the Evade
// teleport (resolveActivesLocked), reusing that mechanism: remove the user from
// its old hex on the evolving board, place it on the destination, clear its
// route.
//
// Recall is "evade to home" (#271): the destination is server-chosen (the
// sanctuary is every player's shared home until per-player beds land), NOT a
// client target, so there is no range/LOS check — a recall is meant to break
// contact from anywhere. Occupancy is still respected (#196): spawnHexLocked
// only returns a hex under StackCap, and a final blockedFor guard covers the
// rare hex that fills on the evolving board this same pass. The scroll is
// consumed only on a SUCCESSFUL recall — a destination that cannot be found or
// is blocked fizzles and keeps the scroll. A recaller that committed a melee
// attack or died this turn drops its recall. Callers hold w.mu.
func (w *World) resolveRecallsLocked(byHex map[protocol.Hex][]*entity, members []*entity, attacked map[int64]bool) {
	recallers := make([]*entity, 0, len(members))

	for _, e := range members {
		if e.recallItem != 0 {
			recallers = append(recallers, e)
		}
	}

	slices.SortFunc(recallers, byEntityID)

	for _, e := range recallers {
		itemID := e.recallItem
		e.recallItem = 0 // consumed, hit or dropped

		if attacked[e.id] || e.hp <= 0 {
			continue
		}

		inst, ok := e.itemByID(itemID)
		if !ok || !itemDefByID[inst.defID].recall {
			continue
		}

		dest, err := w.spawnHexLocked()
		if err != nil || blockedFor(e, byHex, dest) {
			// No safe hex, or the chosen one filled this pass: fizzle, keep the
			// scroll. spawnHexLocked failing at all is a saturated-world edge.
			w.logger.Info(combatLogMsg, logKeyEvent, combatEventFizzle, logKeyReason, "recall_no_dest", logKeyID, e.id)

			continue
		}

		if !w.consumeBackpackUnitLocked(e, itemID) {
			continue // stale id already consumed this pass — nothing to spend
		}

		from := e.hex
		byHex[from] = removeEntity(byHex[from], e)
		byHex[dest] = append(byHex[dest], e)
		e.hex = dest
		e.path = nil

		w.logger.Info(combatLogMsg, logKeyEvent, combatEventRecall, logKeyID, e.id, logKeyItem, inst.defID,
			"from", from, "to", dest)
	}
}
