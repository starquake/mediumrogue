# Inventory System — Slots, Backpack, Drop & Pickup: Design

*User-designed (the Vitruvian paper-doll mockup is the layout authority)
and scoped 2026-07-11: full systems + starter content. Replaces today's
model — 2 weapon slots, unbounded inventory, silent auto-pickup — with 8
typed gear slots, a 4-slot backpack, consumable stacks, drop, and
prompt-based pickup. Builds after playtest batch 3 lands (it touches the
same gear panel).*

## Item taxonomy (12 types — the user's list, verbatim)

`melee-weapon, thrown-weapon, ranged-weapon, staff, wand, consumable,
head, body, hands, ring, amulet, feet`

- `itemDef` gains `itemType`; the **slot is derived from the type** (each
  gear type fits exactly one slot; consumables have no slot — they live in
  the backpack).
- **Weapon slots are class-shaped** (the recorded weapon-slot direction):
  fighter = melee + thrown · rogue = melee + ranged · mage = staff + wand.
  Existing defs re-typed: swords/daggers/cleaver/warhammer/mattock/
  wyrmslayer → melee-weapon; shortbow/pack-bow → ranged-weapon; oak-staff
  → staff; ember-focus/ember-staff/war-mage-staff → wand. No thrown
  content exists yet — the fighter's thrown slot ships empty. Staff can
  melee-bonk; wand never melees (combat weapon resolution reads the class
  shape).
- **Characters remain strictly single-class** (no multi-classing —
  ever). What changes is the ITEM side: an item's wearability restriction
  may now list SEVERAL classes (`wearableBy` set, or "any") — e.g.
  Leather Armor is wearable by fighter OR rogue, per its card; a dagger
  stays rogue-only. Armor and jewelry default to "any" unless the card
  says otherwise.

## Entity model

- `equipped map[slot]instanceID` — 8 slots: head, hands, amulet, body,
  ring, feet + the two class-shaped weapon slots. One item per slot, only
  matching types fit.
- `backpack` — exactly **4 entries**. An entry is a gear instance OR a
  consumable **stack** `{defID, count ≤ 5}` (identical defs merge on
  pickup; stacks never split; drinking decrements, an empty stack frees
  the entry).
- Equipping moves an item from a backpack entry into its slot (and swaps
  the displaced item back into that entry); unequip requires a free
  backpack entry.

## Actions (all: free outside a bubble, your WHOLE turn inside — the
established equip rule extends to everything)

- **equip / unequip** — slot-aware as above.
- **drop** `{itemID | stack}` — the item (or whole stack) leaves the
  player and becomes a ground item at their hex.
- **pickup** `{groundItemID}` — see flow below.
- **drink** `{stack}` — consumable use: apply the def's `heal` (clamped to
  maxHP), decrement the stack.

## Pickup flow (replaces auto-pickup)

Walking onto a hex with ground items no longer auto-grants. The client
prompts per item — **name + type**, yes/no (`GroundItemView` gains
`type`); multiple items queue one prompt at a time. "Yes" sends the pickup
intent; the server re-validates a free home in priority order: matching
empty gear slot? → equip-less pickup into backpack first (items never
auto-equip), so really: consumable stack merge → free backpack entry →
otherwise **reject with a clear error** the client surfaces as feedback
("backpack full — drop something first"). "No" dismisses that item until
the player leaves and re-enters the hex. Monster loot and player drops
behave identically.

## Starter content (making the systems real)

| Item | Type | Rule / effect | Source |
|---|---|---|---|
| `leather-armor` | body (fighter+rogue) | take-damage −1 (floor 1) | the designer's card |
| `headband-of-learning` | head (any) | earn-XP ×1.05 | the designer's card |
| `healing-potion` | consumable | drink: +5 HP; stacks to 5 | recovery layer 2 begins |

Potions enter rat/wolf drop tables at low weight. The content guide gains
the armor/consumable vocabulary (new slots as card fields; `heal` is a
consumable def field, not a pipeline event — drinking is an action, not a
combat-value fold).

## Wire & client

- `ItemView` += `type`, `count`; equipped state becomes slot-keyed;
  `GroundItemView` += `type`. New/extended intents: equip, unequip, drop,
  pickup, drink (kind + itemID/groundItemID). `make protocol`.
- **Paper-doll panel** replaces the gear list: hex slots arranged on the
  user's Vitruvian layout (Head top; Hands left; Ring right; Amulet
  center-upper; Body center; Feet bottom; the two weapon slots flanking
  left/right of Body), 4 backpack cells beneath, per-item drop
  affordance, stack counts on consumables. Pickup prompt = a small DOM
  dialog. **Mockup-first**: HTML mockup approved by the user before the
  client task builds.
- `window.game`: equipped map, backpack, pendingPickupPrompt — kept in
  sync per the testability rule.

## Persistence

Snapshot shape changes (equipped map, backpack, stacks) →
`snapshotVersion` bump. Existing worlds are preserved-aside + fresh on
upgrade (pre-launch; no-backward-compat rule). Archive records migrate to
the new shape (they persist the same fields).

## Out of scope

Thrown-weapon content and wand↔staff interactions (the weapon-slot
content slice), trading, item destruction/durability, backpack upgrades,
scrolls (the type system admits them; none authored), auto-sort, drag &
drop UI (click-based v1).

## Tests

Unit: type→slot derivation + registry validation (every def's type/slot/
classes coherent; consumables have heal>0, gear never); equip/unequip
swap-through-backpack; stack merge/decrement/free; pickup gating (merge >
free entry > reject) with exact error; drop→ground→re-pickup identity;
drink heal clamp + turn rules in bubble. Integration: the full
drop/walk/prompt/accept/reject/full loop over HTTP; potion drink.
e2e: paper-doll renders equipped state; pickup prompt appears on
walk-over, yes grants / no dismisses; backpack-full feedback visible;
stack count renders. Snapshot round-trip with the new shapes.

## Risks

- Widest entity-model change since 6b.4: the equip/combat seam
  (closeDefFor/rangedDefFor by class shape) must keep every existing
  combat test's semantics — melee/ranged behavior is unchanged, only the
  storage model moves.
- The prompt is new UX in bubbles: it must never block the turn clock
  (patience keeps running) and must survive bundle refreshes.
- 4 backpack entries is tight by design (drop decisions are gameplay) —
  expect tuning feedback; the size is a constant, trivially changed.
