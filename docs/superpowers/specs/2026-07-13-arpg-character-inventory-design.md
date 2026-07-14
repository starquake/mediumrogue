# ARPG character & inventory — the gear keystone (design)

*Implements roadmap **G1** (weapon type-tags), **G2** (generic hand slots),
and **G3** (drop class gear gates) — the #55/#56 decided model — plus the
ARPG panel rework, keybindings, and the "1H ≈ ½ 2H" weapon rebalance with the
game's first two-handed weapon. Mockup approved 2026-07-13 (two variants:
dual-wield hands; two-handed greyed off-hand) in the shipped panel's visual
language.*

## 1. The model — tags replace weapon types (G1)

The five weapon item-types (`melee-weapon`, `thrown-weapon`, `ranged-weapon`,
`staff`, `wand`) collapse into **one item type `weapon`** carrying:

- **`tags []string`** — subset of `melee`, `ranged`, `magic`; a weapon has
  **≥1 tag**. Tags decide which attacks fire the weapon (§3).
- **`twoHanded bool`** — 1H is the *absence* of the flag (decided, Q3).

`thrown-weapon` is **deleted** (thrown content is parked, Q1). Armor/jewelry
types rename: `head→helmet`, `body→chest`, `hands→gloves`, `feet→boots`;
`ring`, `amulet`, `consumable` unchanged. The taxonomy is now **8 types**:
`weapon, consumable, helmet, chest, gloves, boots, ring, amulet`.

**Init-time validation** (`mustValidateContent`, fail-loud as always):
- a `weapon` has ≥1 tag, all tags from the known set, no duplicates;
- only `weapon` may carry tags or `twoHanded`;
- a `magic`-tagged weapon has `rangeHex > 0` (magic is ranged AoE-capable);
- non-weapons keep today's rules (no damage/range on armor, `heal` only on
  consumables, etc.).

## 2. Slots & equip (G2 + G3)

**Eight equip slots:** `helmet, chest, gloves, boots, ring, amulet,
main-hand, off-hand`. Armor derives its slot from its type (as today).
Backpack unchanged (4 entries, consumable stacks ≤5).

**Weapon placement (auto):** equip a weapon → **main hand if free, else
off-hand if free, else swap with main hand** (swapped item to backpack —
fails politely when the backpack is full, same as today's swap rule).

**Two-handed:** occupies **main-hand and locks off-hand** (the approved
greyed "two-handed grip" state). Equipping a 2H while the off-hand is
occupied pushes the off-hand item to the backpack first (politely fails if
it can't). While a 2H is held, equipping any weapon swaps out the 2H.
Clicking an equipped slot unequips to backpack (as today).

**Class gates drop (G3):** the `wearableBy` field is **deleted** from
`itemDef`, its validation, and the equip check — anyone equips anything,
armor included. *Accepted consequence (decided on #56): until class skills
exist, any class can wield magic weapons and AoE; class identity temporarily
rests on base HP + starting kit. The identity mechanism moves to the Class
skill tree (Q10).*

**Starting kits (unchanged in effect):** fighter Iron Sword (main); rogue
Dagger (main) + Shortbow (off); mage Oak Wand (main) + Ember Focus (off).

## 3. Combat — the decided #55 resolution

An attack fires **every held weapon whose tags and range fit the attack,
each as its own hit** through the full pipeline (each hit folds its own
cards — under the additive fold each hit's percentages add within that hit):

- **Bump melee:** every held `melee`-tagged weapon hits the bumped target —
  dual-wield = two separate hits (procs, conditions, and future
  evasion/crit resolve per hit).
- **Ranged/AoE intent:** every held `ranged`- or `magic`-tagged weapon whose
  `rangeHex` reaches the target fires; `magic` weapons keep their AoE shape
  (`aoeRadius`), `ranged` stay single-target. No friendly fire, as today.
- **No fitting weapon → fists** (damage 1), exactly today's fallback.
- The client's range/AoE UX hint reads the best-reaching held weapon
  (max `rangeHex` across hands), replacing today's single-ranged-slot read.

## 4. Rebalance — "1H ≈ ½ 2H" + the first two-handed weapon

**Wyrmslayer Greatsword** becomes the 2H anchor: `twoHanded: true`,
**damage 4 → 9**, keeps ×1.5 vs dragons. 1H weapons re-pin (all values
below are the binding numbers):

| Weapon | dmg old → new | tags | notes |
|---|---|---|---|
| Iron Sword | 4 → **4** | melee | starter anchor |
| Dagger | 7 → **4** | melee | dual dagger+Misericorde = 8 ≈ 2H 9 |
| Iron Warhammer | 6 → **5** | melee | the 1H flat king |
| Butcher's Cleaver | 3 → **3** | melee | +3 vs <½HP intact |
| Venom Fang | 5 → **3** | melee | +4 vs full HP intact |
| Ancient Dwarven Mattock | 4 → **4** | melee | +3 dwarf intact |
| Misericorde | 6 → **4** | melee | 15%→×2 (EV 4.6) |
| Duelist's Saber | 5 → **4** | melee | 10%→×2 (EV 4.4) |
| Wyrmslayer Greatsword | 4 → **9** | melee, **2H** | ×1.5 vs dragons |
| Shortbow | 6 → **4** | ranged | 1H by design (dagger+bow lives) |
| Pack Bow | 5 → **3** | ranged | +3 ally intact |
| Oak Wand | 2 → **2** | melee | the mage's bonk; 1H (staff+wand lives) |
| Ember Focus | 4 → **3** | magic | AoE tax; range 4, AoE 1 |
| Ember Staff | 3 → **6** | magic, **2H** | ×2 adjacent intact |
| Staff of the War Mage | 3 → **6** | magic, **2H** | ×2 vs <6HP intact |

Monster stats unchanged. Fists stay 1. Rule cards unchanged except where
noted (none change).

**Amended 2026-07-13 post-review: wands 1H, staves 2H at doubled damage;
Oak Staff renamed Oak Wand — maintainer decision.**

## 5. Wire, snapshot, client

**Protocol** (`internal/protocol`, then `make protocol`):
- item-type constants: `ItemTypeWeapon` + renamed armor types; weapon-type
  constants deleted;
- new tag constants (`WeaponTagMelee/Ranged/Magic`) and slot-name constants
  (`SlotMainHand`, `SlotOffHand`, `SlotHelmet`, …);
- `ItemView` gains `Tags []string`, `TwoHanded bool`; its `Slot` field now
  carries the *slot name* (hand for weapons) instead of the item type.

**Snapshot:** equipped-map keys and item types change shape →
**`snapshotVersion` 3 → 4**. Per the established rule the loader rejects
old snapshots, preserves the file aside (`.rejected-<ts>`), and starts
fresh — **no migration**. Honest cost: dev/staging worlds reset at deploy
and characters are lost; announce in the group chat.

**Client** (the approved mockup, verbatim):
- Slot layout identical geometry to today's doll; labels **Helmet, Amulet,
  Gloves, Ring, Main Hand, Chest, Off-Hand, Boots**; greyed off-hand
  ("two-handed grip") while a 2H is held.
- **Keybindings: `C` and `I` both toggle the panel; `Esc` closes it.** All
  three are ignored while the chat input or the join-name input has focus.
  The HUD button stays.
- Header keyhint per the mockup (`C / I toggles · Esc closes`).
- `<Index>`, never `<For>`, for every turn-bundle-derived list (the
  recurring trap).
- `window.game`: `panelOpen` already exists; the equipped view becomes
  slot-keyed (`window.game.equipped["main-hand"]` etc.) — synced when real
  state changes.
- Stat tooltip's compare-vs-equipped uses the slot the hovered item would
  land in (weapons compare vs main hand, or the hand auto-placement would
  pick).

## 6. Testing

- **Unit:** tag/2H validation (each invalid shape panics at init);
  auto-placement matrix (empty hands / main full / both full / 2H cases,
  incl. push-to-backpack and polite failure); dual-hit resolution (two
  melee weapons = exactly two pipeline hits with independent chance rolls —
  seeded); ranged fires all in-range ranged/magic weapons; fists fallback;
  gates-gone (mage equips Iron Sword; anyone wears Leather Armor).
- **Integration:** equip-into-hands over real HTTP (both hands, 2H lock,
  unequip).
- **e2e:** `C`/`I`/`Esc` toggle (and ignored while chat focused); slots
  render with new labels; equip a backpack weapon into a hand; greyed
  off-hand with a 2H. De-race per the established discipline (poll
  `window.game`, confirm panel open before clicking).
- **Re-derivation:** the rebalance changes most pinned combat numbers —
  re-derive from §4's table (comment `// re-derived: gear keystone rebalance`),
  never weaken. Drop-table pins unaffected (no drop-table changes).

## 7. Docs (same PR)

FEATURES.md (taxonomy → 8 types + tags, slots table, weapons table §4-style
with new damages, keybindings, class-gates-gone note);
`rule-based-content-design.md` §4 (gear card template: tags + twoHanded
replace the weapon-type line; wearableBy line removed); roadmap G1/G2/G3 →
✅ done; STATUS session entry + the snapshot-reset deploy note. After merge:
#55/#56 get decision comments + description updates (GitHub-side).

## Out of scope

Shields (need the off-hand — **now unblocked**, #55 S3 next); thrown
weapons (Q1 parked); evasion%/crit-check events (DF2); skills. No drop-table
changes.

## Success criteria

`make check` + `make e2e` green; a rogue can hold Misericorde + Shortbow and
bump for one dagger-class hit while shooting only with the bow at range; a
fighter with the Wyrmslayer sees a greyed off-hand and hits for 9; a mage
wears Leather Armor; `C`, `I`, `Esc` work and never fire while typing in
chat; old snapshots are rejected-and-preserved, fresh world starts.
