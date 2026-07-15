# Shields: Design

*Status: draft 2026-07-15 (design Q&A with the maintainer). Tracks issue #90
(S4 of #55 — the last unshipped slice of the gear keystone). Implementation
follows a separate plan after spec review (standard milestone-slice pause).*

## Goal

Ship shields v1 per #55's decided model: **a shield is a flat
`take-damage −N` rule card on today's pipeline** — no new event, condition,
or effect kind. A shield occupies the **off-hand**, trading dual-wield's
second hit (~half your melee output) for defence; a two-handed weapon still
locks it out.

Decisions (2026-07-15 Q&A):

1. **Off-hand only.** A shield always equips into `off-hand` (swapping out
   the current occupant like any slot equip). No dual-shield turtle, no
   shield-in-main — #90's wording over #55's looser "fits a hand". Richer
   placement can revisit with #69's block/evasion work if ever needed.
2. **Two shields, two tiers**: a common **−1** and a rare **−2**, both pure
   `take-damage` `add` cards. The −2 stacks with Leather Armor (−1) and the
   dwarf passive (−1), but `applyRules`' event-level clamp keeps every landed
   hit ≥ 1 — no new floor logic.
3. **Drop-only.** No class-default kit changes: Join balance and the pinned
   class-kit tests stay untouched; a fighter picks one up in play.

## Taxonomy — a 9th item type (`internal/protocol`)

`ItemTypeShield = "shield"` joins the taxonomy. A shield is **not a weapon**
(no `tags`, no `twoHanded`, no damage — it never fires as a hit) and not
armor (its slot is a hand, not a type-named slot). `make protocol`
regenerates `client/src/protocol.gen.ts` in the same commit.

## Server (`internal/game/items.go`, `inventory.go`)

### Placement

- `validItemType`: accept `shield`.
- `slotForType(shield)` → `protocol.SlotOffHand`. That one line buys the
  whole existing non-weapon equip path: `toggleEquip`'s swap-to-backpack,
  the unequip toggle, `currentSlotOf`, `canonicalSlotOrder` folding (off-hand
  is already enumerated), and wire rendering (`itemViewOf` reports an
  equipped item's Type as its slot, so an equipped shield arrives as
  `off-hand` — exactly how held weapons already disambiguate hands).
- **Two-handed interaction, both directions:**
  - *Equipping a shield while a two-hander holds main*: evict the two-hander
    to the backpack first — room-checked **before any state change**
    (`ErrBackpackFull`, entity untouched), mirroring `equipWeaponLocked`'s
    polite-failure rule. This is a new small branch in `equipItemLocked`
    (the only place the plain non-weapon path differs for shields). Note a
    two-hander in main implies an empty off-hand (existing invariant), so
    eviction is the only prerequisite.
  - *Equipping a two-handed weapon while a shield holds off-hand*: already
    handled — `equipWeaponLocked` evicts **any** off-hand occupant.
  - *Equipping a one-hander while a shield holds off-hand*:
    `weaponTargetSlot` falls through to a main-hand swap — the shield stays
    put. Already correct; gains test coverage, not code.

### Combat

**No pipeline changes.** The shield's card folds victim-side via
`rollDamageLocked` → `victimGearCards` → `equippedRuleCards` (species → class
→ gear order unchanged). The card carries no `chance` condition, so **rng
consumption does not move** — no seeded combat expectations shift.

### Validation (fail-loud, one tightening)

Existing checks already forbid `tags`/`twoHanded`/`heal` on a shield
(non-weapon rules). Add the missing symmetric check: **only a weapon def may
set `damage`/`rangeHex`/`aoeRadius`** — a general `validateItemDefs`
tightening (all current non-weapon defs already comply), so a copy-paste
shield def carrying a damage number panics at process start, not never.

## Content (`internal/game/content.go`)

Two defs (ids as consts per house convention, `idWoodenBuckler`,
`idIronKiteShield`):

| Item | Type | Card | Desc | Drops (appended LAST) |
|---|---|---|---|---|
| **Wooden Buckler** | shield | `take-damage` add −1 | "block 1 damage from every hit" | rat (weight 1), wolf (weight 4) |
| **Iron Kite Shield** | shield | `take-damage` add −2 | "block 2 damage from every hit" | troll (weight 4), dragon (weight 1) |

Flavor lines to be authored in the designer's voice at implementation time
(both items get one). Tier logic: −1 lands early (rat/wolf rings) where
monsters hit for 1–3; −2 is frontier loot (troll 6, dragon 9) where a flat
−2 is meaningful but the ≥1 clamp and rarity keep it honest. The ghoul table
is deliberately untouched (its identity is venom-fang/misericorde).

## Client (`client/src/gear/store.ts`)

- `targetSlotFor`: a `shield`-typed backpack item maps to `SlotOffHand`
  (today it would return the raw type string, which matches no slot key, so
  the hover tooltip's compare-against-equipped silently shows nothing).
- Everything else is free: an equipped shield's wire Type is `off-hand`
  (server slot rule), so the slot-keyed equipped map just works;
  `typeLabel("shield")` reads fine on pickup rows; the stat-less-gear
  tooltip path (Leather Armor precedent) renders desc + flavor; the
  two-handed grey-out already blocks off-hand clicks while locked, and
  equipping a shield from the backpack in that state round-trips through
  the server's eviction rule.
- No `window.game` shape changes, no new client state, no e2e surface
  change.

## Snapshots / persistence

No state-shape change — `equipped` is already a slot→instance map and
instances store only `defID`. **No `snapshotVersion` bump.**

## Determinism / test-migration notes (the plan owns details)

- Combat rng: untouched (no chance condition on shield cards).
- **Drop tables**: appending entries changes each table's total weight, so
  the same seed can draw a different item — `drops_test.go`'s pinned
  `killDropSeed`/`killMissSeed` (and any integration pin that fights a wolf
  for loot) may need **re-hunting/re-deriving, never weakening**, per the
  house protocol. The wolf 200-seed coverage test must draw the buckler
  (weight 4 of ~28 — comfortably covered).
- New unit coverage: shield lands off-hand; unequip toggle; equip-shield
  evicts a main-hand two-hander (and rejects `ErrBackpackFull` untouched
  when full); equipping a two-hander evicts the shield; a one-hander swaps
  main and leaves the shield; damage fold −N with the ≥1 clamp; −2 + Leather
  Armor + dwarf stacking arithmetic; validation panics for a damage-bearing
  shield def.

## Docs (implementation PR, per the FEATURES rule)

- `FEATURES.md`: taxonomy bullet 8→9 types; slots bullet (off-hand accepts a
  shield; placement + eviction rules); a Shields bullet under Gear
  (the trade: your off-hand's ~half damage for −N); the non-weapon items
  list + the rat/wolf/troll/dragon drop-table notes.
- `docs/design-decisions.md`: dated decided entry (off-hand only, two tiers,
  drop-only; richer block/evasion still deferred to #69, shield skills to
  #57).
- `docs/content-authoring.md`: shields as an authorable card shape
  (take-damage add −N on a `shield`-type def), if the doc enumerates types.

## Scope

- **This spec + plan commit:** documents only (draft PR, standard pause).
- **Implementation (after review, same PR):** `internal/protocol` (one
  const + regen), `internal/game` (placement branch, validation tightening,
  two content defs, four drop-table rows), `client/src/gear/store.ts` (one
  mapping), tests, docs.
- **Out of scope:** active block/evasion (#69), shield skills (#57), class
  starting-kit changes, dual-shield placement, monster shields.
