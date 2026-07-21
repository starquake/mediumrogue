package game

import (
	"slices"

	mrand "math/rand/v2"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// throwables.go: the targeted-consumable ACTION path (#271, slice 5 — the
// throwable flask and the scroll of recall). Both are consumables used through
// a NEW intent rather than drunk: a throw is a targeted combat action (hurl a
// flask at a hex, blast everything opposing in the radius), a recall is a
// self-teleport to safety.
//
// Design: neither uses the free-outside/turn-inside inventory rule
// (commitItemActionLocked). Like an attack or a Blink, they are combat actions
// resolved IN the turn pipeline against the evolving board, never applied
// instantly — a throw needs the shared damage map and the turn rng, a recall
// reuses Blink's teleport. They are the entity's whole turn (they clear any
// queued move/attack), and the flask/scroll is consumed at RESOLUTION, not at
// submit, so a later intent in the same window cancels the throw and keeps the
// item (latest intent wins). ARPG, not TTRPG (docs/game-identity.md): a throw's
// blast ALWAYS hits every entity in radius — no to-hit roll — and is
// range- and line-of-sight-gated exactly as a ranged attack is (#195).

// queueThrowLocked validates a throw intent and queues it as this turn's action
// (#271). The item must be an owned throwable consumable, the aim hex within
// the flask's throw range, and there must be line of sight to it (the same #195
// gate a ranged attack obeys, so a wall shields a target instead of leaving it
// a through-wall loophole). The flask is NOT consumed here — resolveThrowsLocked
// consumes it, so a later intent this window cancels the throw and keeps it.
// Like an attack it clears any queued move/attack/skill/recall. Callers hold
// w.mu.
func (w *World) queueThrowLocked(e *entity, itemID int64, target protocol.Hex) error {
	inst, ok := e.itemByID(itemID)
	if !ok {
		return ErrItemNotOwned
	}

	def := itemDefByID[inst.defID]
	if !def.isThrowable() {
		return ErrNotThrowable
	}

	if HexDistance(e.hex, target) > def.throw.rangeHex {
		return ErrOutOfRange
	}

	// seesLocked returns true for an adjacent/self target (endpoints are never
	// occluded), so a point-blank throw needs no distance exemption.
	if !w.seesLocked(e.hex, target) {
		return ErrNoLineOfSight
	}

	t := target
	e.throwItem = itemID
	e.throwTarget = &t
	e.clearQueuedActionLocked(clearKeepThrow)

	return nil
}

// queueRecallLocked validates a recall intent and queues it as this turn's
// action (#271). The item must be an owned recall consumable (a scroll of
// recall); recall targets the USER, so there is no aim hex, range, or
// line-of-sight check. The scroll is consumed at resolution
// (resolveRecallsLocked), not here — a later intent cancels the recall and
// keeps the scroll. Callers hold w.mu.
func (w *World) queueRecallLocked(e *entity, itemID int64) error {
	inst, ok := e.itemByID(itemID)
	if !ok {
		return ErrItemNotOwned
	}

	if !itemDefByID[inst.defID].recall {
		return ErrNotRecallable
	}

	e.recallItem = itemID
	e.clearQueuedActionLocked(clearKeepRecall)

	return nil
}

// clearKeep* select which queued action clearQueuedActionLocked PRESERVES: a
// queue function sets its own action, then clears every other one so the latest
// intent in the window is the only one that resolves.
type clearKeep int

const (
	clearKeepThrow clearKeep = iota
	clearKeepRecall
)

// clearQueuedActionLocked zeroes every OTHER mutually-exclusive turn action on
// e (path, ranged/melee attack, pending inventory action, active skill), so a
// throw or recall is the entity's whole turn — mirroring how queueMoveLocked
// and useSkillLocked displace one another (latest intent wins). It preserves
// exactly the action named by keep. Callers hold w.mu.
func (e *entity) clearQueuedActionLocked(keep clearKeep) {
	e.path = nil
	e.attackTarget = nil
	e.attackTargetEntity = 0
	e.pending = pendingItemAction{}
	e.activeSkill = ""
	e.activeTarget = nil

	if keep != clearKeepThrow {
		e.throwItem = 0
		e.throwTarget = nil
	}

	if keep != clearKeepRecall {
		e.recallItem = 0
	}
}

// resolveThrowsLocked resolves every queued throw this pass, folding each
// flask's blast into the shared damage map so it lands SIMULTANEOUSLY with the
// turn's melee and ranged hits (#104 — against pre-move positions). Throwers
// are drawn from byHex and processed in id order so the seeded per-victim rolls
// are reproducible regardless of map iteration order, exactly like
// resolveRangedLocked. A thrower that died earlier this phase drops its throw
// (no blast, no consume) — a queued action never fires from a corpse.
//
// The flask is CONSUMED here (consumeBackpackUnitLocked); a blast reuses
// resolveAoELocked with a def synthesized from the throw payload, so the flask
// damages every opposing-faction entity in radius through the full pipeline
// (AoE always hits; resistances/vulnerabilities of the throw's damage type
// apply) and its on-land timed effect rides the synthesized def's onHit — the
// same buffered path (pendingOnHit) a poison weapon uses, so a fresh DoT first
// bites next turn. Callers hold w.mu.
func (w *World) resolveThrowsLocked(rng *mrand.Rand, byHex map[protocol.Hex][]*entity, damage map[int64]int) {
	var throwers []*entity

	for _, occs := range byHex {
		for _, e := range occs {
			if e.throwItem != 0 && e.throwTarget != nil {
				throwers = append(throwers, e)
			}
		}
	}

	slices.SortFunc(throwers, byEntityID)

	for _, e := range throwers {
		itemID, target := e.throwItem, *e.throwTarget
		e.throwItem, e.throwTarget = 0, nil // consumed, whether it blasts or fizzles

		if e.hp <= 0 {
			continue
		}

		inst, ok := e.itemByID(itemID)
		if !ok || !itemDefByID[inst.defID].isThrowable() {
			w.logger.Info(combatLogMsg, "event", combatEventFizzle, "reason", "throw_item_gone", "id", e.id)

			continue
		}

		if !w.consumeBackpackUnitLocked(e, itemID) {
			w.logger.Info(combatLogMsg, "event", combatEventFizzle, "reason", "throw_item_gone", "id", e.id)

			continue
		}

		def := itemDefByID[inst.defID]
		t := def.throw
		// A pure-data weapon synthesized from the throw payload: the blast folds
		// through rollDamageLocked as any hit does (damage type resistances, the
		// thrower's own damage-type skills), and the on-land effect rides onHit so
		// it is buffered and first bites next turn.
		wpn := &itemDef{id: def.id, name: def.name, damageType: t.damageType, onHit: t.onLand}

		w.logger.Info(combatLogMsg, "event", combatEventThrow, "id", e.id, "item", def.id,
			"target", target, "radius", t.aoeRadius)

		w.resolveAoELocked(rng, byHex, e, wpn, target, t.aoeRadius, t.damage, damage)
	}
}

// resolveRecallsLocked resolves every queued recall this pass, teleporting each
// user to a safe hex in the shared sanctuary (spawnHexLocked — the same guarded
// placement a join/respawn uses, so the destination is walkable, has room, and
// is not on or beside a monster). It runs in the move phase alongside the Blink
// teleport (resolveActivesLocked), reusing that mechanism: remove the user from
// its old hex on the evolving board, place it on the destination, clear its
// route.
//
// Recall is "blink to home" (#271): the destination is server-chosen (the
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
			w.logger.Info(combatLogMsg, "event", combatEventFizzle, "reason", "recall_no_dest", "id", e.id)

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

		w.logger.Info(combatLogMsg, "event", combatEventRecall, "id", e.id, "item", inst.defID,
			"from", from, "to", dest)
	}
}
