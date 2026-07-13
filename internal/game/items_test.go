package game //nolint:testpackage // white-box: needs unexported item-registry internals; see rules_test.go's file doc.

import (
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestClassDefaultDamageMatchesLiveBalance pins the exact numbers the spec
// carries forward from the old protocol weapon constants (content.go's
// registry is now the single source): iron-sword 4, dagger 7, shortbow 6
// rng4, oak-staff 2, ember-focus 4 rng4 aoe1.
func TestClassDefaultDamageMatchesLiveBalance(t *testing.T) {
	t.Parallel()

	cases := []struct {
		id                          string
		damage, rangeHex, aoeRadius int
	}{
		{idIronSword, 4, 0, 0},
		{idDagger, 7, 0, 0},
		{idShortbow, 6, 4, 0},
		{idOakStaff, 2, 0, 0},
		{idEmberFocus, 4, 4, 1},
	}

	for _, tc := range cases {
		def, ok := itemDefByID[tc.id]
		if !ok {
			t.Fatalf("registry missing default item %q", tc.id)
		}

		if got, want := def.damage, tc.damage; got != want {
			t.Errorf("%s damage = %d, want %d", tc.id, got, want)
		}

		if got, want := def.rangeHex, tc.rangeHex; got != want {
			t.Errorf("%s rangeHex = %d, want %d", tc.id, got, want)
		}

		if got, want := def.aoeRadius, tc.aoeRadius; got != want {
			t.Errorf("%s aoeRadius = %d, want %d", tc.id, got, want)
		}
	}
}

// TestClassDefaultTypesMatchTaxonomy pins the spec's re-typing of the class
// defaults: sword/dagger → melee-weapon, shortbow → ranged-weapon, oak-staff
// → staff, ember-focus → wand.
func TestClassDefaultTypesMatchTaxonomy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		id, itemType string
	}{
		{idIronSword, protocol.ItemTypeMeleeWeapon},
		{idDagger, protocol.ItemTypeMeleeWeapon},
		{idShortbow, protocol.ItemTypeRangedWeapon},
		{idOakStaff, protocol.ItemTypeStaff},
		{idEmberFocus, protocol.ItemTypeWand},
	}

	for _, tc := range cases {
		def, ok := itemDefByID[tc.id]
		if !ok {
			t.Fatalf("registry missing default item %q", tc.id)
		}

		if got, want := def.itemType, tc.itemType; got != want {
			t.Errorf("%s itemType = %q, want %q", tc.id, got, want)
		}
	}
}

// TestSlotForType: the slot is derived from the type — the slot key IS the
// type string for every gear type; a consumable has no slot at all.
func TestSlotForType(t *testing.T) {
	t.Parallel()

	for _, typ := range canonicalSlotOrder {
		if got, want := slotForType(typ), typ; got != want {
			t.Errorf("slotForType(%q) = %q, want %q", typ, got, want)
		}
	}

	if got, want := slotForType(protocol.ItemTypeConsumable), ""; got != want {
		t.Errorf("slotForType(consumable) = %q, want %q (no slot)", got, want)
	}
}

// TestWeaponSlotsForClassShape pins the recorded weapon-slot direction:
// fighter = melee + thrown, rogue = melee + ranged, mage = staff + wand;
// index 0 is the melee-ish (bump) slot, index 1 the ranged-ish one.
func TestWeaponSlotsForClassShape(t *testing.T) {
	t.Parallel()

	cases := []struct {
		class          string
		closeIsh, rIsh string
	}{
		{protocol.ClassFighter, protocol.ItemTypeMeleeWeapon, protocol.ItemTypeThrownWeapon},
		{protocol.ClassRogue, protocol.ItemTypeMeleeWeapon, protocol.ItemTypeRangedWeapon},
		{protocol.ClassMage, protocol.ItemTypeStaff, protocol.ItemTypeWand},
	}

	for _, tc := range cases {
		slots := weaponSlotsFor(tc.class)
		if got, want := slots[0], tc.closeIsh; got != want {
			t.Errorf("weaponSlotsFor(%s)[0] = %q, want %q", tc.class, got, want)
		}

		if got, want := slots[1], tc.rIsh; got != want {
			t.Errorf("weaponSlotsFor(%s)[1] = %q, want %q", tc.class, got, want)
		}
	}

	if got, want := weaponSlotsFor(""), [2]string{}; got != want {
		t.Errorf("weaponSlotsFor(\"\") = %v, want a zero pair", got)
	}
}

// TestCanEquipWearability: characters stay single-class; the ITEM side may
// list several wearers. A multi-class card (leather-armor style) equips on
// each listed class and rejects the rest; an empty wearableBy means any
// class; a weapon additionally needs the class to have that weapon slot; a
// consumable is never equippable.
func TestCanEquipWearability(t *testing.T) {
	t.Parallel()

	armor := &itemDef{
		id: "test-armor", itemType: protocol.ItemTypeBody,
		wearableBy: []string{protocol.ClassFighter, protocol.ClassRogue},
	}

	if !canEquip(protocol.ClassFighter, armor) || !canEquip(protocol.ClassRogue, armor) {
		t.Error("fighter+rogue armor must be equippable by both listed classes")
	}

	if canEquip(protocol.ClassMage, armor) {
		t.Error("fighter+rogue armor must not be equippable by a mage")
	}

	anyRing := &itemDef{id: "test-ring", itemType: protocol.ItemTypeRing}
	for _, class := range []string{protocol.ClassFighter, protocol.ClassRogue, protocol.ClassMage} {
		if !canEquip(class, anyRing) {
			t.Errorf("empty wearableBy (any) ring must be equippable by %s", class)
		}
	}

	// A dagger stays rogue-only; and even a hypothetical any-class wand is
	// shape-blocked for classes without a wand slot.
	if canEquip(protocol.ClassFighter, itemDefByID[idDagger]) {
		t.Error("a rogue-only dagger must not be equippable by a fighter")
	}

	if !canEquip(protocol.ClassRogue, itemDefByID[idDagger]) {
		t.Error("a dagger must be equippable by a rogue")
	}

	potion := &itemDef{id: "test-potion", itemType: protocol.ItemTypeConsumable, heal: 5}
	if canEquip(protocol.ClassFighter, potion) {
		t.Error("a consumable must never be equippable")
	}
}

// TestItemDamageIsLevelFree: an item's damage is exactly its def's base —
// levels do not scale damage (#60, roadmap XP3: DamagePerLevel cut).
func TestItemDamageIsLevelFree(t *testing.T) {
	t.Parallel()

	def := itemDefByID[idIronSword]

	if got, want := itemDamage(def), def.damage; got != want {
		t.Errorf("itemDamage = %d, want base %d", got, want)
	}
}

// TestEquippedDefInEmptySlotIsNil: a slot with no instance reports nil, not
// a zero-value def — including on an entity whose equipped map was never
// initialized (a monster/zero-value fixture).
func TestEquippedDefInEmptySlotIsNil(t *testing.T) {
	t.Parallel()

	e := &entity{kind: protocol.EntityPlayer, class: protocol.ClassRogue}

	if got := e.equippedDefIn(protocol.ItemTypeMeleeWeapon); got != nil {
		t.Errorf("equippedDefIn(melee-weapon) on an empty slot = %v, want nil", got)
	}

	if got := e.equippedDefIn(protocol.ItemTypeRangedWeapon); got != nil {
		t.Errorf("equippedDefIn(ranged-weapon) on an empty slot = %v, want nil", got)
	}
}

// TestEquippedDefInReturnsOwnedInstance: a filled slot resolves to the owned
// instance's def, by id.
func TestEquippedDefInReturnsOwnedInstance(t *testing.T) {
	t.Parallel()

	e := &entity{
		kind: protocol.EntityPlayer, class: protocol.ClassRogue,
		equipped: map[string]itemInstance{protocol.ItemTypeMeleeWeapon: {id: 7, defID: idDagger}},
	}

	got := e.equippedDefIn(protocol.ItemTypeMeleeWeapon)
	if got == nil || got.id != idDagger {
		t.Errorf("equippedDefIn(melee-weapon) = %v, want the dagger def", got)
	}
}

// TestCloseDefForFallsBackToFists: a bare player (empty melee-ish slot) bumps
// with fists, not a zero-value weapon — for every class (a mage's melee-ish
// slot is staff; empty staff slot = fists bonk fallback).
func TestCloseDefForFallsBackToFists(t *testing.T) {
	t.Parallel()

	for _, class := range []string{protocol.ClassFighter, protocol.ClassRogue, protocol.ClassMage, ""} {
		e := &entity{kind: protocol.EntityPlayer, class: class}

		if got := closeDefFor(e); got != fistsDef {
			t.Errorf("closeDefFor(bare %q player) = %v, want fistsDef", class, got)
		}
	}
}

// TestCloseDefForMonsterIsClaws: a monster (which owns no items) always bumps
// with its kind's own claws profile, regardless of its slot bookkeeping —
// the exact same *itemDef pointer every time (monsters.go's
// buildMonsterIndex builds it once per kind, not fresh per call).
func TestCloseDefForMonsterIsClaws(t *testing.T) {
	t.Parallel()

	e := &entity{kind: protocol.EntityMonster, monsterKind: idKindWolf}

	got := closeDefFor(e)
	if got != monsterDefByID[idKindWolf].claws {
		t.Errorf("closeDefFor(wolf) = %v, want the wolf kind's claws profile", got)
	}

	if got, want := got.damage, monsterDefByID[idKindWolf].damage; got != want {
		t.Errorf("closeDefFor(wolf).damage = %d, want %d", got, want)
	}
}

// TestCloseDefForEquippedWeapon: an equipped melee-ish item wins over the
// fists fallback — and a mage's melee-ish slot is its STAFF (staff bonk).
func TestCloseDefForEquippedWeapon(t *testing.T) {
	t.Parallel()

	rogue := &entity{
		kind: protocol.EntityPlayer, class: protocol.ClassRogue,
		equipped: map[string]itemInstance{protocol.ItemTypeMeleeWeapon: {id: 1, defID: idDagger}},
	}

	if got := closeDefFor(rogue); got == nil || got.id != idDagger {
		t.Errorf("closeDefFor(equipped dagger) = %v, want the dagger def", got)
	}

	mage := &entity{
		kind: protocol.EntityPlayer, class: protocol.ClassMage,
		equipped: map[string]itemInstance{protocol.ItemTypeStaff: {id: 2, defID: idOakStaff}},
	}

	if got := closeDefFor(mage); got == nil || got.id != idOakStaff {
		t.Errorf("closeDefFor(mage with staff) = %v, want the oak-staff def (staff bonk)", got)
	}
}

// TestWandNeverMelees: a mage with only its wand equipped (staff slot empty)
// bumps with FISTS — the wand fills the ranged-ish slot and never
// contributes to melee (the spec: "wand never melees").
func TestWandNeverMelees(t *testing.T) {
	t.Parallel()

	mage := &entity{
		kind: protocol.EntityPlayer, class: protocol.ClassMage,
		equipped: map[string]itemInstance{protocol.ItemTypeWand: {id: 3, defID: idEmberFocus}},
	}

	if got := closeDefFor(mage); got != fistsDef {
		t.Errorf("closeDefFor(mage with wand only) = %v, want fistsDef (a wand never melees)", got)
	}

	if got := rangedDefFor(mage); got == nil || got.id != idEmberFocus {
		t.Errorf("rangedDefFor(mage with wand) = %v, want the ember-focus def", got)
	}
}

// TestRangedDefForEmptyIsNil: an empty ranged-ish slot reports nil — the "no
// ranged weapon" signal queueAttackLocked/resolveRangedLocked read, with no
// fallback (unlike close).
func TestRangedDefForEmptyIsNil(t *testing.T) {
	t.Parallel()

	e := &entity{kind: protocol.EntityPlayer, class: protocol.ClassRogue}

	if got := rangedDefFor(e); got != nil {
		t.Errorf("rangedDefFor(no ranged slot) = %v, want nil", got)
	}
}

// TestFighterThrownSlotShipsEmpty: a fighter's ranged-ish slot is
// thrown-weapon, and no thrown content exists — so a fully-equipped fighter
// (melee slot filled) still has NO ranged weapon, preserving the pre-slots
// "fighter has no ranged attack" combat contract via an empty slot instead
// of a class check. Also pins that the registry really has no thrown content
// yet (the day one lands, the fighter gains ranged and this test is updated
// deliberately).
func TestFighterThrownSlotShipsEmpty(t *testing.T) {
	t.Parallel()

	fighter := &entity{
		kind: protocol.EntityPlayer, class: protocol.ClassFighter,
		equipped: map[string]itemInstance{protocol.ItemTypeMeleeWeapon: {id: 1, defID: idIronSword}},
	}

	if got := rangedDefFor(fighter); got != nil {
		t.Errorf("rangedDefFor(fighter) = %v, want nil (thrown slot ships empty)", got)
	}

	for _, def := range itemDefs {
		if got, notWant := def.itemType, protocol.ItemTypeThrownWeapon; got == notWant {
			t.Errorf("registry item %s is thrown-weapon; no thrown content should exist this slice", def.id)
		}
	}
}

// TestRangedDefForEquippedWeapon: an equipped ranged-ish item resolves by id.
func TestRangedDefForEquippedWeapon(t *testing.T) {
	t.Parallel()

	e := &entity{
		kind: protocol.EntityPlayer, class: protocol.ClassRogue,
		equipped: map[string]itemInstance{protocol.ItemTypeRangedWeapon: {id: 2, defID: idShortbow}},
	}

	if got := rangedDefFor(e); got == nil || got.id != idShortbow {
		t.Errorf("rangedDefFor(equipped shortbow) = %v, want the shortbow def", got)
	}
}

// TestToggleEquipFromBackpackSwapsThroughEntry: equipping from a backpack
// entry moves the item into its slot and the displaced occupant back into
// that same entry — the spec's swap rule. An equip into an EMPTY slot frees
// the entry.
func TestToggleEquipFromBackpackSwapsThroughEntry(t *testing.T) {
	t.Parallel()

	sword := itemInstance{id: 1, defID: idIronSword}
	hammer := itemInstance{id: 2, defID: idIronWarhammer}

	e := &entity{
		kind: protocol.EntityPlayer, class: protocol.ClassFighter,
		equipped: map[string]itemInstance{protocol.ItemTypeMeleeWeapon: sword},
	}
	e.backpack[2] = backpackEntry{inst: hammer, count: 1}

	e.toggleEquip(hammer, protocol.ItemTypeMeleeWeapon)

	if got, want := e.equipped[protocol.ItemTypeMeleeWeapon].id, hammer.id; got != want {
		t.Errorf("melee slot = instance %d, want the warhammer %d", got, want)
	}

	if got, want := e.backpack[2].inst.id, sword.id; got != want {
		t.Errorf("backpack[2] = instance %d, want the displaced sword %d", got, want)
	}

	// Now unequip the hammer (toggle on the already-equipped instance): it
	// needs a free entry, and 0/1/3 are free — it lands in the first one.
	e.toggleEquip(hammer, protocol.ItemTypeMeleeWeapon)

	if got := e.equipped[protocol.ItemTypeMeleeWeapon].id; got != 0 {
		t.Errorf("melee slot after unequip = instance %d, want empty", got)
	}

	if got, want := e.backpack[0].inst.id, hammer.id; got != want {
		t.Errorf("backpack[0] = instance %d, want the unequipped warhammer %d", got, want)
	}
}

// TestToggleEquipUnequipNeedsFreeEntry: unequipping with a full backpack is
// refused (the item stays equipped) — an owned item must always live in
// equipped or backpack, never nowhere.
func TestToggleEquipUnequipNeedsFreeEntry(t *testing.T) {
	t.Parallel()

	sword := itemInstance{id: 9, defID: idIronSword}

	e := &entity{
		kind: protocol.EntityPlayer, class: protocol.ClassFighter,
		equipped: map[string]itemInstance{protocol.ItemTypeMeleeWeapon: sword},
	}

	for i := range e.backpack {
		e.backpack[i] = backpackEntry{inst: itemInstance{id: int64(10 + i), defID: idIronWarhammer}, count: 1}
	}

	e.toggleEquip(sword, protocol.ItemTypeMeleeWeapon)

	if got, want := e.equipped[protocol.ItemTypeMeleeWeapon].id, sword.id; got != want {
		t.Errorf("melee slot = instance %d, want the sword %d still equipped (backpack full)", got, want)
	}
}

// TestStackAndFreeEntryHelpers: stackIndexFor only matches a same-def
// consumable stack below the cap; freeBackpackIndex finds the first empty
// entry; gear never stacks.
func TestStackAndFreeEntryHelpers(t *testing.T) {
	t.Parallel()

	// A synthetic consumable def, registered just for this test's lookup
	// needs, is not possible without mutating the global registry — use a
	// gear def to prove gear never stacks, and rely on task 3's potion tests
	// for a real registry consumable. Here: freeBackpackIndex ordering.
	e := &entity{kind: protocol.EntityPlayer, class: protocol.ClassFighter}

	if got, want := e.freeBackpackIndex(), 0; got != want {
		t.Errorf("freeBackpackIndex(empty) = %d, want %d", got, want)
	}

	e.backpack[0] = backpackEntry{inst: itemInstance{id: 1, defID: idIronWarhammer}, count: 1}

	if got, want := e.freeBackpackIndex(), 1; got != want {
		t.Errorf("freeBackpackIndex(entry 0 used) = %d, want %d", got, want)
	}

	if got, want := e.stackIndexFor(idIronWarhammer), -1; got != want {
		t.Errorf("stackIndexFor(gear) = %d, want %d (gear never stacks)", got, want)
	}

	for i := range e.backpack {
		e.backpack[i] = backpackEntry{inst: itemInstance{id: int64(1 + i), defID: idIronWarhammer}, count: 1}
	}

	if got, want := e.freeBackpackIndex(), -1; got != want {
		t.Errorf("freeBackpackIndex(full) = %d, want %d", got, want)
	}
}

// testItemsWorld builds a minimal World for the Join-level test below,
// mirroring world_test.go's newWorld (unavailable here: that helper lives in
// the black-box game_test package).
func testItemsWorld() *World {
	return NewWorld(time.Hour, time.Minute, time.Millisecond, time.Hour, 0xC0FFEE, 12, hub.New())
}

// TestJoinRogueOwnsDaggerAndShortbowEquipped: Join grants and equips a fresh
// player's class defaults into the class-shaped weapon slots — a Rogue ends
// up with dagger in melee-weapon and shortbow in ranged-weapon, backpack
// fully free.
func TestJoinRogueOwnsDaggerAndShortbowEquipped(t *testing.T) {
	t.Parallel()

	w := testItemsWorld()

	resp, err := w.Join("", "tester", protocol.ClassRogue, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.mu.Lock()
	e := w.entities[resp.EntityID]
	w.mu.Unlock()

	if got, want := len(e.equipped), 2; got != want {
		t.Fatalf("joined rogue has %d equipped items, want %d (dagger + shortbow)", got, want)
	}

	closeDef := e.equippedDefIn(protocol.ItemTypeMeleeWeapon)
	if closeDef == nil || closeDef.id != idDagger {
		t.Errorf("rogue melee-weapon slot = %v, want the dagger def", closeDef)
	}

	rangedDef := e.equippedDefIn(protocol.ItemTypeRangedWeapon)
	if rangedDef == nil || rangedDef.id != idShortbow {
		t.Errorf("rogue ranged-weapon slot = %v, want the shortbow def", rangedDef)
	}

	for slot, inst := range e.equipped {
		if inst.id == 0 {
			t.Errorf("equipped[%s] = %+v has a zero instance id", slot, inst)
		}
	}

	if got, want := e.freeBackpackIndex(), 0; got != want {
		t.Errorf("fresh rogue freeBackpackIndex = %d, want %d (backpack starts empty)", got, want)
	}
}

// TestJoinFighterOwnsOnlyIronSword: a Fighter has no ranged default — its
// thrown-weapon slot stays empty.
func TestJoinFighterOwnsOnlyIronSword(t *testing.T) {
	t.Parallel()

	w := testItemsWorld()

	resp, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.mu.Lock()
	e := w.entities[resp.EntityID]
	w.mu.Unlock()

	if got, want := len(e.equipped), 1; got != want {
		t.Fatalf("joined fighter has %d equipped items, want %d (iron-sword only)", got, want)
	}

	if got := e.equippedDefIn(protocol.ItemTypeMeleeWeapon); got == nil || got.id != idIronSword {
		t.Errorf("fighter melee-weapon slot = %v, want the iron-sword def", got)
	}

	if got := e.equippedDefIn(protocol.ItemTypeThrownWeapon); got != nil {
		t.Errorf("fighter thrown-weapon slot = %v, want nil (no thrown content)", got)
	}
}

// TestValidateItemDefsPanicsOnDuplicateID: a content bug (two defs sharing an
// id) must fail loudly at load, not silently shadow one item.
func TestValidateItemDefsPanicsOnDuplicateID(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("validateItemDefs did not panic on a duplicate id")
		}
	}()

	validateItemDefs([]*itemDef{
		{id: "dup", itemType: protocol.ItemTypeMeleeWeapon, wearableBy: []string{protocol.ClassFighter}},
		{id: "dup", itemType: protocol.ItemTypeMeleeWeapon, wearableBy: []string{protocol.ClassFighter}},
	})
}

// TestValidateItemDefsPanicsOnUnknownType: an itemType outside the taxonomy's
// 12 must fail at load.
func TestValidateItemDefsPanicsOnUnknownType(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("validateItemDefs did not panic on an unknown item type")
		}
	}()

	validateItemDefs([]*itemDef{{id: "x", itemType: "waist"}})
}

// TestValidateItemDefsPanicsOnUnknownWearableByClass.
func TestValidateItemDefsPanicsOnUnknownWearableByClass(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("validateItemDefs did not panic on an unknown wearableBy class")
		}
	}()

	validateItemDefs([]*itemDef{{id: "x", itemType: protocol.ItemTypeBody, wearableBy: []string{"necromancer"}}})
}

// TestValidateItemDefsPanicsOnWearableByAnyWeapon: a weapon must declare an
// explicit wearableBy — and each listed class must actually have that weapon
// slot in its class shape.
func TestValidateItemDefsPanicsOnWearableByAnyWeapon(t *testing.T) {
	t.Parallel()

	t.Run("empty wearableBy", func(t *testing.T) {
		t.Parallel()

		defer func() {
			if r := recover(); r == nil {
				t.Error("validateItemDefs did not panic on a weapon with no wearableBy")
			}
		}()

		validateItemDefs([]*itemDef{{id: "x", itemType: protocol.ItemTypeMeleeWeapon}})
	})

	t.Run("class without the slot", func(t *testing.T) {
		t.Parallel()

		defer func() {
			if r := recover(); r == nil {
				t.Error("validateItemDefs did not panic on a wand wearable by a fighter")
			}
		}()

		validateItemDefs([]*itemDef{{id: "x", itemType: protocol.ItemTypeWand, wearableBy: []string{protocol.ClassFighter}}})
	})
}

// TestValidateItemDefsHealRules: a consumable must have heal > 0; gear must
// never set heal.
func TestValidateItemDefsHealRules(t *testing.T) {
	t.Parallel()

	t.Run("consumable without heal", func(t *testing.T) {
		t.Parallel()

		defer func() {
			if r := recover(); r == nil {
				t.Error("validateItemDefs did not panic on a heal-less consumable")
			}
		}()

		validateItemDefs([]*itemDef{{id: "x", itemType: protocol.ItemTypeConsumable}})
	})

	t.Run("gear with heal", func(t *testing.T) {
		t.Parallel()

		defer func() {
			if r := recover(); r == nil {
				t.Error("validateItemDefs did not panic on a healing hat")
			}
		}()

		validateItemDefs([]*itemDef{{id: "x", itemType: protocol.ItemTypeHead, heal: 3}})
	})

	t.Run("valid consumable", func(t *testing.T) {
		t.Parallel()

		validateItemDefs([]*itemDef{{id: "x", itemType: protocol.ItemTypeConsumable, heal: 5}}) // must not panic
	})
}

// TestValidateItemDefsPanicsOnUnknownRuleKinds: a rule card referencing an
// unknown event, condition, or effect kind must fail at load — the same
// fail-closed guarantee the pipeline gives at runtime (rules.go's
// conditionHolds default case), just moved earlier.
func TestValidateItemDefsPanicsOnUnknownRuleKinds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		card ruleCard
	}{
		{"event", ruleCard{event: "on-kill", then: effect{kind: effAdd, n: 1}}},
		{"condition", ruleCard{event: evDealDamage, when: []condition{{kind: "onFire"}}, then: effect{kind: effAdd, n: 1}}},
		{"effect", ruleCard{event: evDealDamage, then: effect{kind: "setDamage", n: 1}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			defer func() {
				if r := recover(); r == nil {
					t.Errorf("validateItemDefs did not panic on an unknown %s kind", tc.name)
				}
			}()

			validateItemDefs([]*itemDef{{
				id: "x", itemType: protocol.ItemTypeMeleeWeapon, wearableBy: []string{protocol.ClassFighter},
				rules: []ruleCard{tc.card},
			}})
		})
	}
}

// TestValidateItemDefsAcceptsAggroRangeEvent (#36): validateRuleCards must
// accept evAggroRange as a known event — a future sneaky/loud item's rule
// card should validate cleanly at load, not panic on an "unknown event" it
// actually knows about.
func TestValidateItemDefsAcceptsAggroRangeEvent(t *testing.T) {
	t.Parallel()

	validateItemDefs([]*itemDef{{
		id: "cloak-of-shadows", itemType: protocol.ItemTypeBody,
		rules: []ruleCard{
			{event: evAggroRange, then: effect{kind: effAdd, n: -3}},
		},
	}})
}

// TestValidateItemDefsPanicsOnEarnXPWithChanceCondition: earn-XP folds run
// with a nil rng (ruleCtx{} — see resolveBubbleTurnLocked's kill-XP award),
// so a chance condition on an earn-XP card would nil-deref conditionHolds'
// ctx.rng.IntN call the first time such a card actually rolled. Content that
// shape must fail loudly at load, not the first time a player gets a kill.
func TestValidateItemDefsPanicsOnEarnXPWithChanceCondition(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("validateItemDefs did not panic on an earn-xp card with a chance condition")
		}
	}()

	validateItemDefs([]*itemDef{{
		id: "lucky-charm", itemType: protocol.ItemTypeAmulet,
		rules: []ruleCard{
			{event: evEarnXP, when: []condition{{kind: condChance, n: 50}}, then: effect{kind: effMulPct, n: 200}},
		},
	}})
}

// TestValidateMaxReachPanicsBeyondCombatRadius: the INVARIANT queueAttackLocked
// depends on — every ranged def's rangeHex+aoeRadius must stay within
// CombatRadius, or a ranged kill could land in the WORLD domain (no bubble,
// no kill-XP) — is enforced at load, not discovered mid-combat.
func TestValidateMaxReachPanicsBeyondCombatRadius(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("validateMaxReach did not panic on a reach beyond CombatRadius")
		}
	}()

	validateMaxReach([]*itemDef{{
		id: "long-bow", itemType: protocol.ItemTypeRangedWeapon, wearableBy: []string{protocol.ClassRogue},
		rangeHex: protocol.CombatRadius + 1,
	}})
}

// TestValidateMaxReachAcceptsRealRegistry: the real registry must stay within
// the invariant (documents the current numbers so a future content change
// that pushes reach beyond CombatRadius fails this test, not production).
func TestValidateMaxReachAcceptsRealRegistry(t *testing.T) {
	t.Parallel()

	validateMaxReach(itemDefs) // must not panic
}

// TestRealRegistryValidatesCleanly re-runs the exact validation init() already
// performed against the live registry — a belt-and-suspenders check that a
// future content edit failing only under a second load still fails the suite.
func TestRealRegistryValidatesCleanly(t *testing.T) {
	t.Parallel()

	mustValidateContent()
}

// TestFirstGearCardsPinned pins the two designer cards from the first gear
// batch (first-gear review v2): the Mattock's species gate and the War Mage
// Staff's flat-threshold execute — id, base stats, weight, and rule shape.
func TestFirstGearCardsPinned(t *testing.T) {
	t.Parallel()

	mattock, ok := itemDefByID["ancient-dwarven-mattock"]
	if !ok {
		t.Fatal("ancient-dwarven-mattock not registered")
	}

	if got, want := mattock.damage, 4; got != want {
		t.Errorf("mattock damage = %d, want %d", got, want)
	}

	// Loot authority moved monster-side in 6c: the mattock's drop weight now
	// lives in wolf's own table (its weight there is unchanged, 4 — see
	// TestWolfCarriesTodaysExactNumbers, monsters_test.go), not on the item
	// itself.
	if got, want := len(mattock.rules), 1; got != want {
		t.Fatalf("mattock rules = %d, want %d", got, want)
	}

	if got, want := mattock.rules[0].when[0].kind, condAttackerSpecies; got != want {
		t.Errorf("mattock condition = %q, want %q", got, want)
	}

	staff, ok := itemDefByID["war-mage-staff"]
	if !ok {
		t.Fatal("war-mage-staff not registered")
	}

	if got, want := staff.damage, 3; got != want {
		t.Errorf("staff damage = %d, want %d", got, want)
	}

	if got, want := staff.rangeHex, 4; got != want {
		t.Errorf("staff rangeHex = %d, want %d", got, want)
	}

	if got, want := staff.aoeRadius, 1; got != want {
		t.Errorf("staff aoeRadius = %d, want %d", got, want)
	}

	if got, want := staff.rules[0].when[0].kind, condTargetHPBelowFlat; got != want {
		t.Errorf("staff condition = %q, want %q", got, want)
	}

	if got, want := staff.rules[0].then.n, 200; got != want {
		t.Errorf("staff effect = x%d pct, want %d", got, want)
	}

	// Re-typed by the inventory-slots milestone: the war-mage staff is a
	// WAND (it is the mage's ranged-ish AoE caster, and a mage's staff slot
	// is the melee-bonk slot) — the spec's re-typing table.
	if got, want := staff.itemType, protocol.ItemTypeWand; got != want {
		t.Errorf("war-mage-staff itemType = %q, want %q", got, want)
	}
}

// TestValidateItemDefsPanicsOnBadAttackerSpecies: a species-gated card whose
// species string is not one of the three playable species is a content bug —
// it would silently never hold. Fail at load.
func TestValidateItemDefsPanicsOnBadAttackerSpecies(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("validateItemDefs did not panic on an unknown attackerSpecies value")
		}
	}()

	validateItemDefs([]*itemDef{{
		id: "bad", name: "Bad", itemType: protocol.ItemTypeMeleeWeapon, wearableBy: []string{protocol.ClassFighter},
		rules: []ruleCard{{
			event: evDealDamage,
			when:  []condition{{kind: condAttackerSpecies, s: "gnome"}},
			then:  effect{kind: effAdd, n: 1},
		}},
	}})
}

// TestValidateItemDefsPanicsOnUnknownTargetKind: a targetKind gate naming an
// unregistered monster id is a content bug — it would silently never hold.
func TestValidateItemDefsPanicsOnUnknownTargetKind(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("validateItemDefs did not panic on an unknown targetKind value")
		}
	}()

	validateItemDefs([]*itemDef{{
		id: "bad", name: "Bad", itemType: protocol.ItemTypeMeleeWeapon, wearableBy: []string{protocol.ClassFighter},
		rules: []ruleCard{{
			event: evDealDamage,
			when:  []condition{{kind: condTargetKind, s: "griffin"}},
			then:  effect{kind: effMulPct, n: 150},
		}},
	}})
}

// TestWyrmslayerGreatswordPinned pins the first designer card's full intent
// (milestone 6c, previously blocked on monster kinds existing): fighter-only
// melee-weapon, damage 4, ×1.5 vs dragons via condTargetKind, and a
// dragon-only drop (present in dragon's table, absent from every other
// kind's).
func TestWyrmslayerGreatswordPinned(t *testing.T) {
	t.Parallel()

	sword, ok := itemDefByID[idWyrmslayerGreatsword]
	if !ok {
		t.Fatal("wyrmslayer-greatsword not registered")
	}

	if got, want := len(sword.wearableBy), 1; got != want {
		t.Fatalf("sword wearableBy lists %d classes, want %d", got, want)
	}

	if got, want := sword.wearableBy[0], protocol.ClassFighter; got != want {
		t.Errorf("sword wearableBy = %q, want %q", got, want)
	}

	if got, want := sword.damage, 4; got != want {
		t.Errorf("sword damage = %d, want %d", got, want)
	}

	if got, want := len(sword.rules), 1; got != want {
		t.Fatalf("sword rules = %d, want %d", got, want)
	}

	rule := sword.rules[0]
	if got, want := rule.when[0].kind, condTargetKind; got != want {
		t.Errorf("sword condition = %q, want %q", got, want)
	}

	if got, want := rule.when[0].s, idKindDragon; got != want {
		t.Errorf("sword condition target = %q, want %q", got, want)
	}

	if got, want := rule.then.n, 150; got != want {
		t.Errorf("sword effect = x%d pct, want %d", got, want)
	}

	for _, def := range monsterDefs {
		present := false

		for _, d := range def.drops {
			if d.defID == idWyrmslayerGreatsword {
				present = true
			}
		}

		if def.id == idKindDragon && !present {
			t.Errorf("dragon's drops must include %s", idWyrmslayerGreatsword)
		}

		if def.id != idKindDragon && present {
			t.Errorf("%s's drops must NOT include the dragon-only %s", def.id, idWyrmslayerGreatsword)
		}
	}
}

// TestStarterInventoryContentPinned pins the inventory-slots starter
// content's cards (task 3): leather-armor (body, fighter OR rogue — the
// first multi-class wearability card — take-damage −1), headband-of-learning
// (head, any class, earn-XP ×1.05), and healing-potion (consumable, heal 5,
// no rules), plus the potion's low-weight presence in the rat and wolf drop
// tables (recovery layer 2).
func TestStarterInventoryContentPinned(t *testing.T) {
	t.Parallel()

	armor, ok := itemDefByID[idLeatherArmor]
	if !ok {
		t.Fatal("leather-armor not registered")
	}

	if got, want := armor.itemType, protocol.ItemTypeBody; got != want {
		t.Errorf("armor itemType = %q, want %q", got, want)
	}

	wantWear := []string{protocol.ClassFighter, protocol.ClassRogue}
	if got, want := len(armor.wearableBy), len(wantWear); got != want {
		t.Fatalf("armor wearableBy lists %d classes, want %d", got, want)
	}

	for i, c := range wantWear {
		if got, want := armor.wearableBy[i], c; got != want {
			t.Errorf("armor wearableBy[%d] = %q, want %q", i, got, want)
		}
	}

	if got, want := len(armor.rules), 1; got != want {
		t.Fatalf("armor rules = %d, want %d", got, want)
	}

	if got, want := armor.rules[0].event, evTakeDamage; got != want {
		t.Errorf("armor rule event = %q, want %q", got, want)
	}

	if got, want := armor.rules[0].then, (effect{kind: effAdd, n: -1}); got != want {
		t.Errorf("armor rule effect = %+v, want %+v", got, want)
	}

	band, ok := itemDefByID[idHeadbandOfLearning]
	if !ok {
		t.Fatal("headband-of-learning not registered")
	}

	if got, want := band.itemType, protocol.ItemTypeHead; got != want {
		t.Errorf("headband itemType = %q, want %q", got, want)
	}

	if got, want := len(band.wearableBy), 0; got != want {
		t.Errorf("headband wearableBy lists %d classes, want %d (any)", got, want)
	}

	if got, want := len(band.rules), 1; got != want {
		t.Fatalf("headband rules = %d, want %d", got, want)
	}

	if got, want := band.rules[0].event, evEarnXP; got != want {
		t.Errorf("headband rule event = %q, want %q", got, want)
	}

	if got, want := band.rules[0].then, (effect{kind: effMulPct, n: 105}); got != want {
		t.Errorf("headband rule effect = %+v, want %+v", got, want)
	}

	potion, ok := itemDefByID[idHealingPotion]
	if !ok {
		t.Fatal("healing-potion not registered")
	}

	if got, want := potion.itemType, protocol.ItemTypeConsumable; got != want {
		t.Errorf("potion itemType = %q, want %q", got, want)
	}

	if got, want := potion.heal, 5; got != want {
		t.Errorf("potion heal = %d, want %d", got, want)
	}

	if got, want := len(potion.rules), 0; got != want {
		t.Errorf("potion rules = %d, want %d (drinking is an action, not a pipeline event)", got, want)
	}

	wantTables := map[string]int{idKindRat: 1, idKindWolf: 2}

	for _, def := range monsterDefs {
		weight := 0

		for _, d := range def.drops {
			if d.defID == idHealingPotion {
				weight = d.weight
			}
		}

		if got, want := weight, wantTables[def.id]; got != want {
			t.Errorf("%s potion drop weight = %d, want %d", def.id, got, want)
		}
	}
}
