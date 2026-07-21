package game

import (
	"fmt"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// inventory.go: the five inventory ACTIONS (equip, unequip, drop, pickup,
// drink) — task 2 of the inventory-slots milestone (spec:
// docs/superpowers/specs/2026-07-11-inventory-slots-design.md). Every action
// follows one shared rule, extended from 6b.4's equip: outside a combat
// bubble it applies immediately and costs nothing; inside a bubble it
// becomes the player's committed action for that turn (clearing any queued
// move/attack — the latest intent in the window wins), applied by
// resolveCombatLocked's pending pass. The storage model these act on
// (equipped map + backpack) lives in items.go; the intent plumbing
// (SubmitIntent) in world.go.

// pendingItemAction is an inventory action queued inside a combat bubble as
// the entity's committed action for the turn: which action (a
// protocol.Intent* inventory kind) and its target — an owned item instance
// id, or a ground item instance id for a pickup. The zero value means "no
// pending action".
type pendingItemAction struct {
	kind string
	id   int64
}

// commitItemActionLocked routes a validated inventory action per the shared
// free-outside/turn-inside rule: outside a bubble it applies now (apply is
// the action's own immediate application; its error propagates to the
// intent's HTTP response); inside one it records the pending action and
// clears any queued move/attack — you act on your inventory, you don't also
// move or shoot this turn. Callers hold w.mu.
func (w *World) commitItemActionLocked(e *entity, kind string, id int64, apply func() error) error {
	if e.bubbleID == 0 {
		e.pending = pendingItemAction{}

		return apply()
	}

	e.pending = pendingItemAction{kind: kind, id: id}
	e.path = nil
	e.attackTarget = nil
	e.attackTargetEntity = 0

	return nil
}

// applyPendingItemLocked applies and clears e's queued inventory action, if
// any. Shared by the resolution pass in resolveCombatLocked (the action is
// the bubble-turn's action) and by the bubble-dissolve branch of
// recomputeBubblesLocked (the bubble vanished before its turn resolved — the
// player is back in world time, where inventory actions are free and
// immediate, so the queued action applies now instead of leaking into a
// later world turn). A pending action that fails its re-validation at apply
// time (the ground item was taken by a lower-id player this same pass, the
// backpack filled, ...) fizzles with a log line rather than erroring — there
// is no HTTP response to carry an error at resolution time. Callers hold
// w.mu.
func (w *World) applyPendingItemLocked(e *entity) {
	p := e.pending
	if p.kind == "" {
		return
	}

	e.pending = pendingItemAction{}

	var err error

	switch p.kind {
	case protocol.IntentEquip:
		err = w.equipItemLocked(e, p.id)
	case protocol.IntentUnequip:
		err = w.unequipItemLocked(e, p.id)
	case protocol.IntentDrop:
		err = w.dropItemLocked(e, p.id)
	case protocol.IntentPickup:
		err = w.pickupGroundLocked(e, p.id)
	case protocol.IntentDrink:
		err = w.drinkItemLocked(e, p.id)
	}

	if err != nil {
		w.logger.Info(combatLogMsg, "event", combatEventFizzle, "reason", "pending_item_action",
			"kind", p.kind, "id", e.id, "item", p.id, "err", err.Error())
	}
}

// equipItemLocked applies an equip toggle NOW: validates ownership and
// equippability (unlike the other four appliers it re-validates fully, since
// queueEquipLocked shares it with the pending path) and swaps the item into
// its slot through the backpack — armor/jewelry via toggleEquip directly
// (its slot is fixed by itemType); a weapon via equipWeaponLocked (its slot
// is a hand chosen at equip time, plus the two-handed eviction rules).
// Naming an already-equipped item unequips it instead (the playtest batch 2
// toggle). Callers hold w.mu.
func (*World) equipItemLocked(e *entity, itemID int64) error {
	inst, ok := e.itemByID(itemID)
	if !ok {
		return ErrItemNotOwned
	}

	def := itemDefByID[inst.defID]
	if err := equipValidate(def); err != nil {
		return err
	}

	if def.isWeapon() {
		return e.equipWeaponLocked(inst, def)
	}

	// A shield equips into the off-hand (slotForType), but a two-handed
	// weapon in main LOCKS that slot — equipping the shield evicts the
	// two-hander to the backpack first, room-checked BEFORE any state
	// change (mirroring equipWeaponLocked's polite failure). A two-hander
	// in main implies an empty off-hand (equipWeaponLocked's invariant), so
	// eviction is the only prerequisite. heldSlotOf == "" is exactly "not
	// currently equipped" for a shield (off-hand is its only possible
	// slot), so a toggle-OFF of an equipped shield never fires this. #90.
	if def.itemType == protocol.ItemTypeShield && heldSlotOf(e, inst.id) == "" {
		if main := e.equippedDefIn(protocol.SlotMainHand); main != nil && main.twoHanded {
			if e.freeBackpackIndex() < 0 {
				return ErrBackpackFull
			}

			e.toggleEquip(e.equipped[protocol.SlotMainHand], protocol.SlotMainHand)
		}
	}

	// A toggle-OFF (already equipped) is an unequip: it needs a free
	// backpack entry, and the intent path should say so rather than no-op.
	slot := slotForType(def.itemType)
	if cur, isCur := e.equipped[slot]; isCur && cur.id == inst.id && e.freeBackpackIndex() < 0 {
		return ErrBackpackFull
	}

	e.toggleEquip(inst, slot)

	return nil
}

// unequipItemLocked moves an equipped item back into a free backpack entry.
// Callers hold w.mu.
func (w *World) unequipItemLocked(e *entity, itemID int64) error {
	inst, def, err := w.ownedDefLocked(e, itemID)
	if err != nil {
		return err
	}

	slot := currentSlotOf(e, inst, def)
	if slot == "" {
		return ErrItemNotEquipped
	}

	if e.freeBackpackIndex() < 0 {
		return ErrBackpackFull
	}

	e.toggleEquip(inst, slot) // toggle on the equipped instance = unequip into a free entry

	return nil
}

// dropItemLocked removes an owned item from the player — an equipped item
// clears its slot; a backpack entry frees (a consumable stack leaves WHOLE) —
// and lands it on the player's own hex as a single ground stack carrying its
// count (gear/single items are count 1). Two separate drops of the same
// consumable onto the same hex stay two ground stacks; a pickup re-merges
// them into the backpack under the stack-cap rule. Callers hold w.mu.
func (w *World) dropItemLocked(e *entity, itemID int64) error {
	inst, def, err := w.ownedDefLocked(e, itemID)
	if err != nil {
		return err
	}

	if slot := currentSlotOf(e, inst, def); slot != "" {
		delete(e.equipped, slot)
		w.groundItemsAddLocked(e.hex, groundStack{inst: inst, count: 1})
		w.logDropLocked(e, inst, 1)

		return nil
	}

	idx := e.findBackpackIndex(inst.id)
	if idx < 0 {
		// itemByID found it, so it is equipped-or-backpack; not equipped in
		// its own slot and not in the backpack cannot happen — defensive.
		return ErrItemNotOwned
	}

	count := e.backpack[idx].count
	e.backpack[idx] = backpackEntry{}
	w.groundItemsAddLocked(e.hex, groundStack{inst: inst, count: count})
	w.logDropLocked(e, inst, count)

	return nil
}

// groundItemsAddLocked lands one ground stack on hex (dropped whole; no
// splitting). Callers hold w.mu.
func (w *World) groundItemsAddLocked(hex protocol.Hex, gs groundStack) {
	w.groundItems[hex] = append(w.groundItems[hex], gs)
}

// logDropLocked emits the drop combat-log event. Callers hold w.mu.
func (w *World) logDropLocked(e *entity, inst itemInstance, count int) {
	w.logger.Info(combatLogMsg, "event", combatEventDrop, "id", e.id, "item", inst.defID, "count", count, "at", e.hex)
}

// findGroundStackLocked returns the ground stack with the given representative
// id on hex, and its index, or (nil, -1). Callers hold w.mu.
func (w *World) findGroundStackLocked(hex protocol.Hex, groundItemID int64) (*groundStack, int) {
	stacks := w.groundItems[hex]
	for i := range stacks {
		if stacks[i].inst.id == groundItemID {
			return &stacks[i], i
		}
	}

	return nil, -1
}

// hasRoomForLocked reports whether e's backpack has room for at least one unit
// of defID: a mergeable consumable stack that isn't at the cap, or a free
// entry. Callers hold w.mu.
func (e *entity) hasRoomForLocked(defID string) bool {
	return e.stackIndexFor(defID) >= 0 || e.freeBackpackIndex() >= 0
}

// pickupGroundLocked picks one WHOLE ground stack off the player's own hex
// into the backpack, taking what fits and leaving any remainder (a smaller
// stack) on the ground. Units land in the spec's priority order: top up a
// matching consumable stack first (to the cap), then fresh backpack entries
// (each up to the cap); gear (count 1) simply takes a free entry. Items never
// auto-equip. If NOTHING fits (a full matching stack and no free entry) the
// pickup is ErrBackpackFull ("backpack full — drop something first"). The
// stack must lie exactly on e.hex — a pickup at range is ErrNoSuchGroundItem,
// same as a stale id. Callers hold w.mu.
func (w *World) pickupGroundLocked(e *entity, groundItemID int64) error {
	gs, idx := w.findGroundStackLocked(e.hex, groundItemID)
	if gs == nil {
		return ErrNoSuchGroundItem
	}

	taken := e.takeStackLocked(gs.inst, gs.count)
	if taken == 0 {
		return ErrBackpackFull
	}

	remaining := gs.count - taken
	if remaining > 0 {
		// Partial pickup: the leftover stays on the ground as a smaller stack.
		w.groundItems[e.hex][idx].count = remaining
	} else {
		stacks := w.groundItems[e.hex]
		stacks = append(stacks[:idx], stacks[idx+1:]...)

		if len(stacks) == 0 {
			delete(w.groundItems, e.hex)
		} else {
			w.groundItems[e.hex] = stacks
		}
	}

	w.announce("system", pickupAnnounce(e.name, itemDefByID[gs.inst.defID].name, taken))
	w.logger.Info(combatLogMsg, "event", combatEventPickup, "id", e.id, "item", gs.inst.defID, "count", taken)

	return nil
}

// takeStackLocked absorbs up to count units of inst's def into e's backpack,
// respecting the per-stack cap: it tops up a matching consumable stack first,
// then — since a single ground stack is always <= ItemStackCap — puts whatever
// remains into one free entry (which keeps the ground stack's representative
// id). Returns how many units were actually taken (0 if nothing fit). Callers
// hold w.mu.
func (e *entity) takeStackLocked(inst itemInstance, count int) int {
	remaining := count

	if idx := e.stackIndexFor(inst.defID); idx >= 0 {
		add := min(remaining, protocol.ItemStackCap-e.backpack[idx].count)
		e.backpack[idx].count += add
		remaining -= add
	}

	if remaining > 0 {
		if idx := e.freeBackpackIndex(); idx >= 0 {
			add := min(remaining, protocol.ItemStackCap)
			e.backpack[idx] = backpackEntry{inst: inst, count: add}
			remaining -= add
		}
	}

	return count - remaining
}

// pickupAnnounce renders the pickup chat line: "NAME picked up ITEM" for a
// single unit, "NAME picked up ITEM ×N" for a multi-unit stack.
func pickupAnnounce(playerName, itemName string, count int) string {
	if count > 1 {
		return fmt.Sprintf("%s picked up %s ×%d", playerName, itemName, count)
	}

	return playerName + " picked up " + itemName
}

// drinkItemLocked drinks one unit of an owned consumable stack: it heals the
// def's heal (clamped to max HP), then applies the def's timed-effect payload
// (#271, slice 2) — cleansing harmful effects (cleansesHarmful) and applying
// any self-buffs (appliesEffect) to the drinker — and decrements the stack; an
// emptied stack frees its backpack entry. Only a consumable drinks (gear is
// ErrNotDrinkable). Callers hold w.mu.
//
// The payload is pure-data riders on the def, the drink counterpart of a
// weapon's onHit — no combat-site special case. Unlike onHit (buffered and
// applied AFTER the end-of-turn tick so a fresh effect first bites next turn), a
// drink applies its effect NOW: a Warding Tonic must turn aside the incoming
// blow the very turn it is drunk, and a drink is already the player's whole turn
// inside a combat bubble.
func (w *World) drinkItemLocked(e *entity, itemID int64) error {
	inst, def, err := w.ownedDefLocked(e, itemID)
	if err != nil {
		return err
	}

	if def.itemType != protocol.ItemTypeConsumable {
		return ErrNotDrinkable
	}

	idx := e.findBackpackIndex(inst.id)
	if idx < 0 {
		return ErrItemNotOwned // consumables only live in the backpack; defensive
	}

	e.hp = min(e.hp+def.heal, e.maxHP)

	cleared := 0
	if def.cleansesHarmful {
		cleared = clearHarmfulEffectsLocked(e)
	}

	for _, ae := range def.appliesEffect {
		applyTimedEffectLocked(e, ae.effectID, ae.magnitude, ae.turns)
	}

	e.backpack[idx].count--
	if e.backpack[idx].count <= 0 {
		e.backpack[idx] = backpackEntry{}
	}

	w.logger.Info(combatLogMsg, "event", combatEventDrink, "id", e.id, "item", inst.defID,
		"hp", e.hp, "cleared", cleared, "buffs", len(def.appliesEffect))

	return nil
}

// ownedDefLocked resolves an owned item instance and its def, or
// ErrItemNotOwned. Callers hold w.mu.
func (*World) ownedDefLocked(e *entity, itemID int64) (itemInstance, *itemDef, error) {
	inst, ok := e.itemByID(itemID)
	if !ok {
		return itemInstance{}, nil, ErrItemNotOwned
	}

	return inst, itemDefByID[inst.defID], nil
}
