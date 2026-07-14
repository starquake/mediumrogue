# ARPG Character & Inventory (Gear Keystone) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Weapon tags replace weapon types, generic main/off-hand slots replace class-shaped slots, class gates drop, weapons rebalance around the first two-handed weapon, and the character panel becomes the approved ARPG mockup with C/I/Esc keys.

**Architecture:** One data-model swap in the Go core (`internal/protocol` taxonomy + `internal/game` items/equip/content, snapshot v4), then per-hit multi-weapon combat resolution on top, then the client panel + keybindings, then docs. Each task compiles and gates green on its own.

**Tech Stack:** Go (module root), tygo-generated TS protocol, SolidJS panel, Playwright e2e.

**Spec:** `docs/superpowers/specs/2026-07-13-arpg-character-inventory-design.md` — its §4 rebalance table and §2 equip rules are binding.

## Global Constraints

- **Branch `feat/arpg-inventory`** (exists, carries the spec). One PR; do NOT merge (maintainer's label is the gate).
- **Determinism:** seeded PCG only; sort map-derived slices before draws; **re-derive** pinned test values from the spec's §4 table with comment `// re-derived: gear keystone rebalance` — never weaken/delete. Drop tables are untouched in this slice.
- **Protocol:** constants live in `internal/protocol/protocol.go`; run `make protocol` after changes and commit the regenerated `client/src/protocol.gen.ts` in the same commit; never hand-edit it. Grep `client/e2e/*.spec.ts` too — e2e files import protocol constants and are typechecked by `make check`.
- **Snapshot:** `snapshotVersion` 3 → 4 (internal/game/snapshot.go:37). No migration — the loader already rejects, preserves aside, starts fresh.
- **Binding slot names:** `main-hand`, `off-hand`, `helmet`, `chest`, `gloves`, `boots`, `ring`, `amulet`. Binding tags: `melee`, `ranged`, `magic`. Binding types: `weapon`, `consumable`, `helmet`, `chest`, `gloves`, `boots`, `ring`, `amulet` (type = slot for armor).
- **Client rules:** `<Index>` never `<For>` for turn-bundle lists; every new client state syncs a `window.game` field; e2e de-race by polling `window.game`, never sleeps.
- **Test style:** `got, want` inline declarations (.claude/rules/go-style.md).
- Go may not be on PATH — use `make test` / `make check`, or `/usr/local/go/bin/go test` for focused runs.

## File Map

| File | Changes |
|---|---|
| `internal/protocol/protocol.go` | taxonomy consts, tag consts, slot consts, `ItemView.Tags/TwoHanded` (T1) |
| `internal/game/items.go` | `itemDef.tags/twoHanded` (drop `wearableBy`), placement + equip rules, validation, weapon pickers (T1), multi-weapon pickers (T2) |
| `internal/game/inventory.go` | equip/unequip paths (T1) |
| `internal/game/content.go` | retag + rebalance all 15 weapons, drop `wearableBy` everywhere (T1) |
| `internal/game/snapshot.go` | version 4 (T1) |
| `internal/game/world.go` | attack sites loop per weapon (T2), grant kits to hands (T1) |
| `client/src/gear/store.ts` + `CharacterPanel.tsx` | slot model, labels, greyed 2H (T3) |
| `client/src/main.ts` | keybindings, window.game, range hint (T3, T4) |
| `client/e2e/gear.spec.ts` (+new `panelkeys.spec.ts`) | panel + keys (T3, T4) |
| `docs/*` | FEATURES, content guide, roadmap, STATUS (T5) |

---

### Task 1: The model swap — taxonomy, tags, hands, rebalance, snapshot v4

**Files:**
- Modify: `internal/protocol/protocol.go:154-165` (types) + ItemView struct
- Modify: `internal/game/items.go` (itemDef :22-42, slotForType :137, weaponSlotsFor :153-164, classHasWeaponSlot/wearableByClass/canEquip/equipValidate :166-224, toggleEquip :354, closeDefFor/rangedDefFor :395-426, canonicalSlotOrder, validateItemDefs)
- Modify: `internal/game/inventory.go:97-140` (equip/unequip paths)
- Modify: `internal/game/content.go` (all itemDefs + class kits)
- Modify: `internal/game/snapshot.go:37`
- Modify: `internal/game/world.go` (kit granting — find with `grep -n grantDefaults internal/game/world.go`)
- Test: `internal/game/items_test.go`, `internal/game/content_test.go`, `internal/game/inventory_test.go`

**Interfaces:**
- Produces (later tasks consume): `protocol.ItemTypeWeapon`, `protocol.WeaponTagMelee/Ranged/Magic`, `protocol.SlotMainHand = "main-hand"`, `protocol.SlotOffHand = "off-hand"`, `protocol.SlotHelmet/Chest/Gloves/Boots/Ring/Amulet`; `itemDef.tags []string`, `itemDef.twoHanded bool`; `def.hasTag(tag string) bool`; `(e *entity) heldWeapons() []*itemDef` (sorted main-then-off, skips empty); placement `weaponTargetSlot(e *entity) string`; unchanged combat shims `closeDefFor(e) *itemDef` (first melee-tagged held weapon, else monster claws/fists) and `rangedDefFor(e) *itemDef` (longest-range ranged/magic-tagged held weapon, nil if none).

- [ ] **Step 1: Protocol constants + ItemView**

In `internal/protocol/protocol.go` replace the item-type block (:154-165):

```go
	// The item taxonomy (gear keystone, #55/#56): one weapon type carrying
	// tags, plus armor/jewelry types that each map 1:1 to an equip slot.
	ItemTypeWeapon     = "weapon"
	ItemTypeConsumable = "consumable"
	ItemTypeHelmet     = "helmet"
	ItemTypeChest      = "chest"
	ItemTypeGloves     = "gloves"
	ItemTypeBoots      = "boots"
	ItemTypeRing       = "ring"
	ItemTypeAmulet     = "amulet"

	// Weapon tags: which attacks fire the weapon (§3 of the keystone spec).
	WeaponTagMelee  = "melee"
	WeaponTagRanged = "ranged"
	WeaponTagMagic  = "magic"

	// Equip-slot names. Armor slots equal their item type; weapons go to a
	// hand (main first, then off; two-handed locks both).
	SlotMainHand = "main-hand"
	SlotOffHand  = "off-hand"
	SlotHelmet   = ItemTypeHelmet
	SlotChest    = ItemTypeChest
	SlotGloves   = ItemTypeGloves
	SlotBoots    = ItemTypeBoots
	SlotRing     = ItemTypeRing
	SlotAmulet   = ItemTypeAmulet
```

(Delete `ItemTypeMeleeWeapon/ThrownWeapon/RangedWeapon/Staff/Wand/Head/Body/Hands/Feet`.) In `ItemView` add `Tags []string` + `TwoHanded bool` (json tags matching the struct's existing style); its `Slot` doc comment now says "the equip slot this item occupies or would occupy (hand name for weapons)". Run `make protocol`.

- [ ] **Step 2: Failing model tests**

In `internal/game/items_test.go` (match the file's fixture style):

```go
func TestWeaponPlacementMatrix(t *testing.T) {
	t.Parallel()

	// Placement: main if free, else off if free, else swap main.
	// Craft an entity with the package's entity fixture (see existing
	// toggleEquip tests) and three 1H melee weapons A, B, C:
	// equip A -> main; equip B -> off; equip C -> main (A to backpack).
	// BINDING assertions: after each step, equipped[SlotMainHand] /
	// equipped[SlotOffHand] hold exactly the expected instance ids, and A
	// lands in the backpack on the third equip.
}

func TestTwoHandedLocksOffHand(t *testing.T) {
	t.Parallel()

	// Equip 1H A (main) + 1H B (off), then a two-handed W:
	// -> main = W, off EMPTY, A and B both in backpack.
	// Then with backpack full (fill remaining entries): equipping W again
	// after re-equipping A+B FAILS politely (ErrBackpackFull or the file's
	// existing error), state unchanged.
	// While W held: equip A -> W to backpack, A to main (off stays empty).
}

func TestGatesGone(t *testing.T) {
	t.Parallel()

	// A mage equips the Iron Sword (melee weapon) and Leather Armor: both
	// succeed. equipValidate returns nil for every (class, weapon) pair.
	// ErrWrongClass is no longer reachable from equipValidate — assert a
	// rogue equipping a magic weapon returns nil too.
}
```

Add to `internal/game/content_test.go`:

```go
func TestKeystoneRetagAndRebalance(t *testing.T) {
	t.Parallel()

	// The spec §4 binding table, verbatim: id -> damage, tags, twoHanded.
	cases := []struct {
		id     string
		damage int
		tags   []string
		twoH   bool
	}{
		{idIronSword, 4, []string{protocol.WeaponTagMelee}, false},
		{idDagger, 4, []string{protocol.WeaponTagMelee}, false},
		{idIronWarhammer, 5, []string{protocol.WeaponTagMelee}, false},
		{idButchersCleaver, 3, []string{protocol.WeaponTagMelee}, false},
		{idVenomFang, 3, []string{protocol.WeaponTagMelee}, false},
		{idAncientDwarvenMattock, 4, []string{protocol.WeaponTagMelee}, false},
		{idMisericorde, 4, []string{protocol.WeaponTagMelee}, false},
		{idDuelistsSaber, 4, []string{protocol.WeaponTagMelee}, false},
		{idWyrmslayerGreatsword, 9, []string{protocol.WeaponTagMelee}, true},
		{idShortbow, 4, []string{protocol.WeaponTagRanged}, false},
		{idPackBow, 3, []string{protocol.WeaponTagRanged}, false},
		{idOakStaff, 2, []string{protocol.WeaponTagMelee}, false},
		{idEmberFocus, 3, []string{protocol.WeaponTagMagic}, false},
		{idEmberStaff, 3, []string{protocol.WeaponTagMagic}, false},
		{idWarMageStaff, 3, []string{protocol.WeaponTagMagic}, false},
	}
	// For each: look up via the registry helper content_test.go already
	// uses, assert damage/tags/twoHanded with got, want.
}
```

- [ ] **Step 3: Run to verify failure**

Run: `make test`
Expected: compile errors (deleted constants) — that is the RED signal for a model swap.

- [ ] **Step 4: itemDef + validation**

In `internal/game/items.go`: replace `wearableBy []string` with:

```go
	// tags (weapon-type items only) name which attacks fire this weapon:
	// protocol.WeaponTagMelee/Ranged/Magic. ≥1 tag required for a weapon,
	// none allowed on anything else (validateItemDefs).
	tags []string
	// twoHanded (weapons only): occupies main-hand AND locks off-hand.
	twoHanded bool
```

Add `func (d *itemDef) hasTag(tag string) bool { return slices.Contains(d.tags, tag) }` and `func (d *itemDef) isWeapon() bool { return d.itemType == protocol.ItemTypeWeapon }`. Update `validateItemDefs` (in the same file, follow its existing error style): a weapon has ≥1 tag, all from the known set, no duplicates; tags/twoHanded forbidden on non-weapons; a `WeaponTagMagic` weapon has `rangeHex > 0`; delete every wearableBy rule. Delete `wearableByClass`, `classHasWeaponSlot`, `weaponSlotsFor`, `isWeaponType`/`isGearType` (replace uses with `isWeapon()` / a slot check). `equipValidate` becomes:

```go
// equipValidate: nil if def can be equipped at all — the only failure left
// is a consumable (no slot; drink is its action). Class gates dropped
// (gear keystone, #56): anyone equips anything.
func equipValidate(_ string, def *itemDef) error {
	if def.itemType == protocol.ItemTypeConsumable {
		return ErrNotEquippable
	}

	return nil
}
```

(Keep the `class` parameter position dropped or `_`-named per call sites; simplest is changing the signature to `equipValidate(def *itemDef) error` and fixing the few call sites — `grep -n equipValidate internal/game test/integration`.)

- [ ] **Step 5: Slots + placement**

Replace `slotForType` and add placement:

```go
// slotForType returns the equip slot for a NON-WEAPON item type (armor
// slots equal their type), "" for consumable (no slot), and "" for weapon —
// a weapon's slot is a hand chosen at equip time (weaponTargetSlot).
func slotForType(t string) string {
	switch t {
	case protocol.ItemTypeConsumable, protocol.ItemTypeWeapon:
		return ""
	default:
		return t
	}
}

// weaponTargetSlot picks the hand an equipped weapon lands in: main if
// free, else off if free, else main (swap). A two-handed weapon always
// targets main (toggleEquip clears/locks off).
func weaponTargetSlot(e *entity, def *itemDef) string {
	if def.twoHanded {
		return protocol.SlotMainHand
	}

	if e.equippedDefIn(protocol.SlotMainHand) == nil {
		return protocol.SlotMainHand
	}

	if e.equippedDefIn(protocol.SlotOffHand) == nil {
		return protocol.SlotOffHand
	}

	return protocol.SlotMainHand
}
```

`canonicalSlotOrder` becomes the eight slot names (main-hand, off-hand, helmet, chest, gloves, boots, ring, amulet — fixed order, comment why). Update `toggleEquip` (:354) and the `inventory.go` equip path: weapons route through `weaponTargetSlot`; equipping a 2H first unequips the off-hand to backpack (reusing the existing swap-to-backpack path — **politely fail before any state change if the backpack can't hold every displaced item**); while a 2H is in main, equipping any weapon swaps the 2H out; a non-2H equip never touches the other hand. Unequip is unchanged (slot → backpack).

- [ ] **Step 6: Combat pickers (single-hit shims — Task 2 makes them multi)**

```go
// closeDefFor: monster claws as before; else the first melee-tagged held
// weapon in canonical hand order, else fists.
func closeDefFor(e *entity) *itemDef {
	if e.kind == protocol.EntityMonster { /* unchanged claws block */ }

	for _, def := range e.heldWeapons() {
		if def.hasTag(protocol.WeaponTagMelee) {
			return def
		}
	}

	return fistsDef
}

// rangedDefFor: the longest-range ranged/magic-tagged held weapon, nil if
// none (fighter with sword only = no ranged attack, as before).
func rangedDefFor(e *entity) *itemDef {
	var best *itemDef

	for _, def := range e.heldWeapons() {
		if !def.hasTag(protocol.WeaponTagRanged) && !def.hasTag(protocol.WeaponTagMagic) {
			continue
		}

		if best == nil || def.rangeHex > best.rangeHex {
			best = def
		}
	}

	return best
}

// heldWeapons returns the equipped hand weapons, main then off (fixed
// order — deterministic fold and "main hits first" once dual lands).
func (e *entity) heldWeapons() []*itemDef {
	var out []*itemDef

	for _, slot := range [2]string{protocol.SlotMainHand, protocol.SlotOffHand} {
		if def := e.equippedDefIn(slot); def != nil {
			out = append(out, def)
		}
	}

	return out
}
```

- [ ] **Step 7: Content retag + rebalance + kits + snapshot**

`internal/game/content.go`: every weapon becomes `itemType: protocol.ItemTypeWeapon` with `tags:`/`twoHanded:` and the §4 damage (the Step-2 test table IS the checklist); armor/jewelry types rename (`ItemTypeBody`→`ItemTypeChest` on Leather Armor, `ItemTypeHead`→`ItemTypeHelmet` on the Headband); delete every `wearableBy:` line. Kit granting (world.go, `grantDefaultsLocked` or successor): fighter Iron Sword→main; rogue Dagger→main + Shortbow→off; mage Oak Staff→main + Ember Focus→off — route through `weaponTargetSlot`/toggleEquip so grants and player equips share one path. `internal/game/snapshot.go:37`: `snapshotVersion = 4` (comment: gear-keystone equipped-map keys + item types).

- [ ] **Step 8: Green + re-derive**

Run: `make test` then `make check`. Expect broad re-derivation: every pinned combat number using old damages re-derives from §4 (`// re-derived: gear keystone rebalance`); equip/inventory tests re-key to the new slots; integration `TestEquipOverHTTP`/`TestInventoryLoopOverHTTP` update (crafted snapshot version → 4, slot keys → hands). Client compiles because protocol.gen.ts regenerated and `client/src` still references old constants — fix `client/src/gear/store.ts`'s references minimally in THIS task only as far as compilation needs (label/UI work stays Task 3); same for `client/e2e` imports.
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal client
git commit -m "feat(gear): keystone model — weapon tags, main/off hands, gates dropped, rebalance + first 2H, snapshot v4 (#55, #56)"
```

### Task 2: Per-hit multi-weapon combat

**Files:**
- Modify: `internal/game/items.go` (pickers), `internal/game/world.go` attack sites (`grep -n "closeDefFor\|rangedDefFor" internal/game/world.go` — bump/attackLocked, resolveRangedLocked/bow, resolveAoE/entity-targeted)
- Test: `internal/game/melee_damage_test.go`, `internal/game/ranged_test.go`

**Interfaces:**
- Consumes: `heldWeapons()`, tags, Task 1 shims.
- Produces: `meleeDefsFor(e *entity) []*itemDef` (each melee-tagged held weapon; monster = claws single; fists single when empty), `rangedDefsFor(e *entity, dist int) []*itemDef` (each ranged/magic-tagged held weapon with `rangeHex >= dist`). `closeDefFor`/`rangedDefFor` remain for non-combat callers (UI range hint) — closeDefFor delegates to `meleeDefsFor(e)[0]`.

- [ ] **Step 1: Failing tests**

```go
func TestDualWieldTwoMeleeHits(t *testing.T) {
	// A player with Dagger (main) + Duelist's Saber (off) bumps a wolf:
	// the victim takes TWO pipeline hits that turn — total damage
	// 4 + 4 = 8 before take-damage cards (seed chosen so the saber's 10%
	// crit does NOT proc; also pin a seed where it DOES: 4 + 8 = 12).
	// Each hit folds independently. Use the seeded-combat scaffolding the
	// file's elf-crit/misericorde tests already use; document the seed scan.
}

func TestSingleWeaponSingleHit(t *testing.T) {
	// Same fixture, main hand only: exactly ONE hit (no phantom second).
	// And empty hands: exactly one fists hit for 1.
}

func TestRangedFiresAllInRange(t *testing.T) {
	// Shortbow (main, range 4) + Ember Focus (off, magic range 4 aoe 1):
	// a ranged intent at distance 3 fires BOTH — bow single-target hit +
	// focus AoE (its ring). At distance 5: neither (out of range, intent
	// rejected exactly as today).
}
```

- [ ] **Step 2: RED** — `make test`: helpers undefined / single-hit expectations fail.

- [ ] **Step 3: Implement**

```go
// meleeDefsFor: every melee hit this entity's bump delivers, in hand order.
// Monster = its claws (single). No melee-tagged weapon = fists (single).
func meleeDefsFor(e *entity) []*itemDef {
	if e.kind == protocol.EntityMonster { /* claws, as closeDefFor */ }

	var out []*itemDef

	for _, def := range e.heldWeapons() {
		if def.hasTag(protocol.WeaponTagMelee) {
			out = append(out, def)
		}
	}

	if len(out) == 0 {
		return []*itemDef{fistsDef}
	}

	return out
}

// rangedDefsFor: every ranged/magic weapon that reaches dist, hand order.
func rangedDefsFor(e *entity, dist int) []*itemDef {
	var out []*itemDef

	for _, def := range e.heldWeapons() {
		if !def.hasTag(protocol.WeaponTagRanged) && !def.hasTag(protocol.WeaponTagMagic) {
			continue
		}

		if def.rangeHex >= dist {
			out = append(out, def)
		}
	}

	return out
}
```

At each attack site, loop the returned defs and run the existing single-hit body per def (damage roll → pipeline → apply — the existing `rollDamageLocked(def, ...)` call per def; floating-damage/log events per hit, as separate hits). Magic defs keep their AoE application; ranged defs stay single-target. Intent validation ("is anything in range?") = `len(rangedDefsFor(e, dist)) > 0`. Keep rng consumption ordered: defs are already in fixed hand order.

- [ ] **Step 4: GREEN + full gate** — `make test`, re-derive any pin that assumed one hit (comment `// re-derived: dual-wield per-hit resolution`), then `make check`.

- [ ] **Step 5: Commit** — `git commit -m "feat(combat): attacks fire every fitting held weapon as its own hit (#55 dual-wield)"`

### Task 3: Client — the approved panel

**Files:**
- Modify: `client/src/gear/store.ts` (slot model), `client/src/gear/CharacterPanel.tsx` (labels/layout/greyed 2H), `client/index.html` (slot CSS class renames + `.hex.greyed`), `client/src/main.ts` (range-hint read, window.game.equipped)
- Test: `client/e2e/gear.spec.ts`

**Interfaces:**
- Consumes: `ItemView.Slot` (now the slot name incl. hands), `Tags`, `TwoHanded` from protocol.gen.ts.
- Produces: `equipped()` keyed by the eight slot names; `SLOT_LABELS` map; `window.game.equipped` (slot-keyed `{[slot]: itemName|null}`).

- [ ] **Step 1: Failing e2e**

Extend `client/e2e/gear.spec.ts` (existing spec style: real embedded binary, poll `window.game`):

```ts
test("panel shows ARPG slots and greys off-hand under a 2H", async ({ page }) => {
  // open panel via the HUD button (existing helper), assert the eight
  // slot labels render: HELMET, AMULET, GLOVES, RING, MAIN HAND, CHEST,
  // OFF-HAND, BOOTS. Poll window.game.equipped["main-hand"] for the
  // class-default weapon name after join.
});
```

(The greyed-2H e2e needs a 2H in inventory — no e2e drop hook exists; assert the greyed state via a unit-level store test instead and note it in the spec file comment. Do not invent a spawn hook.)

- [ ] **Step 2: Store + panel**

`store.ts`: `weaponSlots`/`slotLabel` replaced by:

```ts
export const SLOT_ORDER = ["helmet", "amulet", "gloves", "ring",
  "main-hand", "chest", "off-hand", "boots"] as const;
export const SLOT_LABELS: Record<string, string> = {
  helmet: "Helmet", amulet: "Amulet", gloves: "Gloves", ring: "Ring",
  "main-hand": "Main Hand", chest: "Chest", "off-hand": "Off-Hand",
  boots: "Boots",
};
// offHandLocked(): true when the main-hand item is TwoHanded.
```

`CharacterPanel.tsx`: render the eight hexes from `SLOT_ORDER` (positions = the existing CSS classes renamed: `head→helmet`, `hands→gloves`, `weap1→mainhand`, `body→chest`, `weap2→offhand`, `feet→boots`; `ring`/`amulet` stay); off-hand hex gets class `greyed` + text "two-handed grip" (per the approved mockup) when `offHandLocked()`; clicking it while locked is a no-op (`disabled`). `index.html`: rename the position classes, add `.hex.greyed { background:#26302a; } .hex.greyed::before { background:#0d120e; }` and a `.ghost` text style (mockup values). Tooltip compare: a hovered weapon compares vs the item in `weaponTargetSlot`-equivalent client logic (main if free/2H, else off if free, else main) — implement as a small `targetSlotFor(item)` in store.ts mirroring the server rule, with a unit test. `main.ts`: the ranged UX hint reads max `rangeHex`/`aoeRadius` across BOTH hands' ranged/magic items; `window.game.equipped` = slot-keyed names, synced where inventory syncs today.

- [ ] **Step 3: GREEN** — `npx vitest run` (store tests, if the client has unit tests — else the e2e), `make check`, `make e2e` locally once.

- [ ] **Step 4: Commit** — `git commit -m "feat(client): ARPG character panel — eight named slots, hands, greyed two-handed grip (approved mockup)"`

### Task 4: Keybindings C / I / Esc

**Files:**
- Modify: `client/src/main.ts` (global keydown — the file already has an `applyPanelOpen`/`toggleInventory` at :787-796)
- Test: new `client/e2e/panelkeys.spec.ts`

**Interfaces:** consumes `applyPanelOpen`/`panelOpen` from Task 3's state; produces nothing downstream.

- [ ] **Step 1: Failing e2e**

```ts
// panelkeys.spec.ts — same server fixture as gear.spec.ts:
test("C and I toggle the panel, Esc closes, chat focus suppresses", async ({ page }) => {
  // join; poll window.game.turn >= 1.
  // press "c" -> window.game.panelOpen === true
  // press "c" -> false; press "i" -> true; press "Escape" -> false
  // focus the chat input, press "i" -> panelOpen stays false
});
```

- [ ] **Step 2: Implement**

In `main.ts`, next to the existing movement-key handler (find with `grep -n "keydown" client/src/main.ts` — follow its focus-guard pattern if one exists):

```ts
document.addEventListener("keydown", (ev) => {
  const target = ev.target as HTMLElement | null;
  if (target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA")) {
    return; // typing in chat / join name — never steal keys
  }

  if (ev.key === "c" || ev.key === "C" || ev.key === "i" || ev.key === "I") {
    ev.preventDefault();
    toggleInventory();
  } else if (ev.key === "Escape" && panelOpen()) {
    applyPanelOpen(false);
  }
});
```

Add the header keyhint (`C / I toggles · Esc closes`) to the panel per the mockup. If movement keys already share a guard helper, reuse it instead of duplicating the INPUT check.

- [ ] **Step 3: GREEN** — `make e2e` (run the new spec with `--repeat-each=3 --workers=9` once to shake races), `make check`.

- [ ] **Step 4: Commit** — `git commit -m "feat(client): C/I toggle + Esc close for the character panel, chat-focus guarded"`

### Task 5: Docs sync + PR

**Files:** `docs/FEATURES.md`, `docs/rule-based-content-design.md` (§4 gear card template), `docs/design-roadmap.md`, `docs/STATUS.md`

- [ ] **Step 1: Sync** — FEATURES: taxonomy (8 types + 3 tags + twoHanded), slots table (the eight names), the full §4 weapons table with NEW damages/tags (values from content.go, never memory), keybindings row, "class gates dropped (#56)" note, snapshot-v4 reset note. Content guide §4: gear card template's `Type:`/`Wearable by:` lines become `Type: weapon (+ tags: melee/ranged/magic, two-handed?) / helmet / chest / gloves / boots / ring / amulet / consumable` and the wearableBy line is deleted (anyone equips anything); §4 prose about hard lanes ("there will never be a fighter bow") rewritten to the tags model. Roadmap: G1/G2/G3 → `✅ done (2026-07-13)`. STATUS: session block + **deploy note: snapshot v4 rejects old worlds — dev/staging reset, characters lost; announce in group chat**.
- [ ] **Step 2: Gate** — `make check`.
- [ ] **Step 3: Commit + PR**

```bash
git add docs && git commit -m "docs: FEATURES/guide/roadmap/STATUS sync for the gear keystone"
git push -u origin feat/arpg-inventory
gh pr create --base main --title "feat: ARPG character & inventory — weapon tags, hand slots, gates dropped, rebalance + first 2H (#55, #56)" --body "Implements docs/superpowers/specs/2026-07-13-arpg-character-inventory-design.md (G1+G2+G3). Snapshot v4 — worlds reset at deploy. Mockup-approved panel; C/I/Esc keys. Do not merge without the label."
```

Do **not** merge. After merge (maintainer's call), #55/#56 get decision comments + description updates — GitHub-side, not this plan.

---

## Self-review notes

- Spec coverage: §1→T1 (types/tags/validation), §2→T1 (slots/placement/2H/gates/kits), §3→T2, §4→T1 Step 2/7 (binding table as a test), §5→T1 (wire/snapshot) + T3/T4 (client), §6 tests distributed per task, §7→T5. No gaps.
- Type consistency: `heldWeapons()`/`hasTag`/`weaponTargetSlot` defined T1, consumed T2/T3 (client mirrors as `targetSlotFor`); slot constants defined T1 Step 1, used throughout; `meleeDefsFor`/`rangedDefsFor` defined T2 only where used.
- Deliberate delegations (not placeholders): test scaffolding/fixtures follow each file's existing helpers, with the binding assertion values stated; the 2H-greyed e2e is explicitly redirected to a store unit test because no e2e drop hook exists.
