package game

import "github.com/starquake/mediumrogue/internal/protocol"

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
// wearability (unlike the other four appliers it re-validates fully, since
// queueEquipLocked shares it with the pending path) and swaps the item
// into its slot through the backpack (toggleEquip — naming an already-
// equipped item unequips it instead, the playtest batch 2 toggle). Callers
// hold w.mu.
func (*World) equipItemLocked(e *entity, itemID int64) error {
	inst, ok := e.itemByID(itemID)
	if !ok {
		return ErrItemNotOwned
	}

	def := itemDefByID[inst.defID]
	if !canEquip(e.class, def) {
		return ErrWrongClass
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

	slot := slotForType(def.itemType)
	if cur, ok := e.equipped[slot]; !ok || cur.id != inst.id {
		return ErrItemNotEquipped
	}

	if e.freeBackpackIndex() < 0 {
		return ErrBackpackFull
	}

	e.toggleEquip(inst, slot) // toggle on the equipped instance = unequip into a free entry

	return nil
}

// dropItemLocked removes an owned item from the player — an equipped item
// clears its slot; a backpack entry frees (a consumable stack leaves whole) —
// and lands it on the player's own hex as ground item(s). A stack of N
// becomes N single ground instances (the representative instance keeps its
// id; the extras mint fresh ones), so the ground model stays "one instance
// per item" and a later pickup re-merges them one at a time under the
// stack-cap rule. Callers hold w.mu.
func (w *World) dropItemLocked(e *entity, itemID int64) error {
	inst, def, err := w.ownedDefLocked(e, itemID)
	if err != nil {
		return err
	}

	count := 1

	if slot := slotForType(def.itemType); slot != "" {
		if cur, ok := e.equipped[slot]; ok && cur.id == inst.id {
			delete(e.equipped, slot)
			w.groundItemsAddLocked(e.hex, inst, count)
			w.logDropLocked(e, inst, count)

			return nil
		}
	}

	idx := e.findBackpackIndex(inst.id)
	if idx < 0 {
		// itemByID found it, so it is equipped-or-backpack; not equipped in
		// its own slot and not in the backpack cannot happen — defensive.
		return ErrItemNotOwned
	}

	count = e.backpack[idx].count
	e.backpack[idx] = backpackEntry{}
	w.groundItemsAddLocked(e.hex, inst, count)
	w.logDropLocked(e, inst, count)

	return nil
}

// groundItemsAddLocked lands count copies of inst's def on hex: the
// representative instance itself first, then count-1 freshly minted
// instances (ids from the shared nextID sequence). Callers hold w.mu.
func (w *World) groundItemsAddLocked(hex protocol.Hex, inst itemInstance, count int) {
	w.groundItems[hex] = append(w.groundItems[hex], inst)

	for range count - 1 {
		w.nextID++
		w.groundItems[hex] = append(w.groundItems[hex], itemInstance{id: w.nextID, defID: inst.defID})
	}
}

// logDropLocked emits the drop combat-log event. Callers hold w.mu.
func (w *World) logDropLocked(e *entity, inst itemInstance, count int) {
	w.logger.Info(combatLogMsg, "event", combatEventDrop, "id", e.id, "item", inst.defID, "count", count, "at", e.hex)
}

// pickupGroundLocked picks one ground item off the player's own hex, in the
// spec's priority order: merge into a mergeable consumable stack, else a
// free backpack entry, else ErrBackpackFull ("backpack full — drop something
// first", which the client surfaces as feedback). Items never auto-equip,
// even into an empty matching slot. The ground item must lie exactly on
// e.hex — a pickup at range is ErrNoSuchGroundItem, same as a stale id.
// Callers hold w.mu.
func (w *World) pickupGroundLocked(e *entity, groundItemID int64) error {
	items := w.groundItems[e.hex]

	idx := -1

	for i, it := range items {
		if it.id == groundItemID {
			idx = i

			break
		}
	}

	if idx < 0 {
		return ErrNoSuchGroundItem
	}

	it := items[idx]
	if !w.takeItemLocked(e, it) {
		return ErrBackpackFull
	}

	items = append(items[:idx], items[idx+1:]...)
	if len(items) == 0 {
		delete(w.groundItems, e.hex)
	} else {
		w.groundItems[e.hex] = items
	}

	w.announce("system", e.name+" picked up "+itemDefByID[it.defID].name)
	w.logger.Info(combatLogMsg, "event", combatEventPickup, "id", e.id, "item", it.defID)

	return nil
}

// drinkItemLocked drinks one unit of an owned consumable stack: heals the
// def's heal clamped to max HP and decrements the stack; an emptied stack
// frees its backpack entry. Only a consumable drinks (gear is
// ErrNotDrinkable). Callers hold w.mu.
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

	e.backpack[idx].count--
	if e.backpack[idx].count <= 0 {
		e.backpack[idx] = backpackEntry{}
	}

	w.logger.Info(combatLogMsg, "event", combatEventDrink, "id", e.id, "item", inst.defID, "hp", e.hp)

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
