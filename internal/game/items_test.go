package game //nolint:testpackage // white-box: needs unexported item-registry internals; see rules_test.go's file doc.

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestClassDefaultDamageMatchesLiveBalance pins the exact numbers the gear
// keystone rebalance (§4, "1H ≈ ½ 2H") leaves class defaults at: iron-sword
// 4, dagger 4, shortbow 4 rng4, oak-wand 2, ember-focus 3 rng4 aoe1.
func TestClassDefaultDamageMatchesLiveBalance(t *testing.T) {
	t.Parallel()

	cases := []struct {
		id                          string
		damage, rangeHex, aoeRadius int
	}{
		{idIronSword, 4, 0, 0},
		{idDagger, 4, 0, 0},   // re-derived: gear keystone rebalance (7 -> 4)
		{idShortbow, 4, 4, 0}, // re-derived: gear keystone rebalance (6 -> 4)
		{idOakWand, 2, 0, 0},
		{idEmberFocus, 3, 4, 1}, // re-derived: gear keystone rebalance (4 -> 3)
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

// TestClassDefaultTypesAndTags pins the gear keystone's re-typing of the
// class defaults: every one is the single ItemTypeWeapon, distinguished by
// tag — sword/dagger/oak-wand melee, shortbow ranged, ember-focus magic.
func TestClassDefaultTypesAndTags(t *testing.T) {
	t.Parallel()

	cases := []struct {
		id  string
		tag string
	}{
		{idIronSword, protocol.WeaponTagMelee},
		{idDagger, protocol.WeaponTagMelee},
		{idShortbow, protocol.WeaponTagRanged},
		{idOakWand, protocol.WeaponTagMelee},
		{idEmberFocus, protocol.WeaponTagMagic},
	}

	for _, tc := range cases {
		def, ok := itemDefByID[tc.id]
		if !ok {
			t.Fatalf("registry missing default item %q", tc.id)
		}

		if got, want := def.itemType, protocol.ItemTypeWeapon; got != want {
			t.Errorf("%s itemType = %q, want %q", tc.id, got, want)
		}

		if !def.hasTag(tc.tag) {
			t.Errorf("%s tags = %v, want to include %q", tc.id, def.tags, tc.tag)
		}
	}
}

// TestSlotForType: an armor/jewelry type's slot key IS the type string
// itself; a consumable and a weapon both have no slotForType result — a
// weapon's slot is a hand chosen at equip time (weaponTargetSlot).
func TestSlotForType(t *testing.T) {
	t.Parallel()

	armorTypes := []string{
		protocol.ItemTypeHelmet, protocol.ItemTypeChest, protocol.ItemTypeGloves,
		protocol.ItemTypeBoots, protocol.ItemTypeRing, protocol.ItemTypeAmulet,
	}

	for _, typ := range armorTypes {
		if got, want := slotForType(typ), typ; got != want {
			t.Errorf("slotForType(%q) = %q, want %q", typ, got, want)
		}
	}

	if got, want := slotForType(protocol.ItemTypeConsumable), ""; got != want {
		t.Errorf("slotForType(consumable) = %q, want %q (no slot)", got, want)
	}

	if got, want := slotForType(protocol.ItemTypeWeapon), ""; got != want {
		t.Errorf("slotForType(weapon) = %q, want %q (hand chosen at equip time)", got, want)
	}
}

// TestWeaponTargetSlot exercises the placement matrix directly: main if
// free, else off if free, else main (swap fallback) — and a two-handed
// weapon always targets main regardless of current occupancy.
func TestWeaponTargetSlot(t *testing.T) {
	t.Parallel()

	oneH := itemDefByID[idIronSword]
	twoH := itemDefByID[idWyrmslayerGreatsword]

	t.Run("empty hands: main", func(t *testing.T) {
		t.Parallel()

		e := &entity{kind: protocol.EntityPlayer, class: protocol.ClassFighter}

		if got, want := weaponTargetSlot(e, oneH), protocol.SlotMainHand; got != want {
			t.Errorf("weaponTargetSlot(empty) = %q, want %q", got, want)
		}
	})

	t.Run("main full: off", func(t *testing.T) {
		t.Parallel()

		e := &entity{
			kind: protocol.EntityPlayer, class: protocol.ClassFighter,
			equipped: map[string]itemInstance{protocol.SlotMainHand: {id: 1, defID: idIronSword}},
		}

		if got, want := weaponTargetSlot(e, oneH), protocol.SlotOffHand; got != want {
			t.Errorf("weaponTargetSlot(main full) = %q, want %q", got, want)
		}
	})

	t.Run("both full: main (swap)", func(t *testing.T) {
		t.Parallel()

		e := &entity{
			kind: protocol.EntityPlayer, class: protocol.ClassFighter,
			equipped: map[string]itemInstance{
				protocol.SlotMainHand: {id: 1, defID: idIronSword},
				protocol.SlotOffHand:  {id: 2, defID: idDagger},
			},
		}

		if got, want := weaponTargetSlot(e, oneH), protocol.SlotMainHand; got != want {
			t.Errorf("weaponTargetSlot(both full) = %q, want %q", got, want)
		}
	})

	t.Run("two-handed always targets main", func(t *testing.T) {
		t.Parallel()

		e := &entity{
			kind: protocol.EntityPlayer, class: protocol.ClassFighter,
			equipped: map[string]itemInstance{protocol.SlotOffHand: {id: 2, defID: idDagger}},
		}

		if got, want := weaponTargetSlot(e, twoH), protocol.SlotMainHand; got != want {
			t.Errorf("weaponTargetSlot(2H) = %q, want %q", got, want)
		}
	})
}

// inBackpackForTest reports whether instID sits in e's backpack (a small
// local helper — several tests below check where a displaced weapon landed).
func inBackpackForTest(e *entity, instID int64) bool {
	for _, be := range e.backpack {
		if !be.empty() && be.inst.id == instID {
			return true
		}
	}

	return false
}

// TestWeaponPlacementMatrix: equip A -> main (empty hands); equip B -> off
// (main taken); equip C -> main, swapping A out to the backpack (both hands
// taken) — the full auto-placement matrix, exercised through
// equipWeaponLocked (the primitive toggleEquip's weapon-aware counterpart).
func TestWeaponPlacementMatrix(t *testing.T) {
	t.Parallel()

	aDef, bDef, cDef := itemDefByID[idIronSword], itemDefByID[idDagger], itemDefByID[idButchersCleaver]
	a := itemInstance{id: 1, defID: aDef.id}
	b := itemInstance{id: 2, defID: bDef.id}
	c := itemInstance{id: 3, defID: cDef.id}

	e := &entity{kind: protocol.EntityPlayer, class: protocol.ClassFighter}
	e.backpack[0] = backpackEntry{inst: a, count: 1}
	e.backpack[1] = backpackEntry{inst: b, count: 1}
	e.backpack[2] = backpackEntry{inst: c, count: 1}

	if err := e.equipWeaponLocked(a, aDef); err != nil {
		t.Fatalf("equip A: %v", err)
	}

	if got, want := e.equipped[protocol.SlotMainHand].id, a.id; got != want {
		t.Errorf("after A: main = %d, want %d", got, want)
	}

	if err := e.equipWeaponLocked(b, bDef); err != nil {
		t.Fatalf("equip B: %v", err)
	}

	if got, want := e.equipped[protocol.SlotOffHand].id, b.id; got != want {
		t.Errorf("after B: off = %d, want %d", got, want)
	}

	if err := e.equipWeaponLocked(c, cDef); err != nil {
		t.Fatalf("equip C: %v", err)
	}

	if got, want := e.equipped[protocol.SlotMainHand].id, c.id; got != want {
		t.Errorf("after C: main = %d, want %d (swap fallback)", got, want)
	}

	if got, want := e.equipped[protocol.SlotOffHand].id, b.id; got != want {
		t.Errorf("after C: off = %d, want unchanged %d", got, want)
	}

	if !inBackpackForTest(e, a.id) {
		t.Error("A not found in the backpack after being swapped out by C")
	}
}

// TestTwoHandedLocksOffHand covers the two-handed eviction rules: equipping
// a 2H evicts the off-hand; equipping ANY weapon while a 2H sits in main
// evicts it first; either eviction politely fails (state untouched) if the
// backpack has nowhere for the displaced item to land.
func TestTwoHandedLocksOffHand(t *testing.T) {
	t.Parallel()

	aDef, bDef, wDef := itemDefByID[idIronSword], itemDefByID[idDagger], itemDefByID[idWyrmslayerGreatsword]
	a := itemInstance{id: 1, defID: aDef.id}
	b := itemInstance{id: 2, defID: bDef.id}
	w := itemInstance{id: 3, defID: wDef.id}

	// newFixture returns a fresh entity with A equipped main, B equipped
	// off, and W sitting in the backpack — every subtest starts here.
	newFixture := func(t *testing.T) *entity {
		t.Helper()

		e := &entity{kind: protocol.EntityPlayer, class: protocol.ClassFighter}
		e.backpack[0] = backpackEntry{inst: a, count: 1}
		e.backpack[1] = backpackEntry{inst: b, count: 1}
		e.backpack[2] = backpackEntry{inst: w, count: 1}

		if err := e.equipWeaponLocked(a, aDef); err != nil {
			t.Fatalf("fixture equip A: %v", err)
		}

		if err := e.equipWeaponLocked(b, bDef); err != nil {
			t.Fatalf("fixture equip B: %v", err)
		}

		return e
	}

	t.Run("2H evicts off-hand", func(t *testing.T) {
		t.Parallel()

		e := newFixture(t)

		if err := e.equipWeaponLocked(w, wDef); err != nil {
			t.Fatalf("equip W: %v", err)
		}

		if got, want := e.equipped[protocol.SlotMainHand].id, w.id; got != want {
			t.Errorf("main = %d, want the 2H %d", got, want)
		}

		if _, ok := e.equipped[protocol.SlotOffHand]; ok {
			t.Error("off-hand still occupied, want empty (2H locks it)")
		}

		if !inBackpackForTest(e, a.id) || !inBackpackForTest(e, b.id) {
			t.Errorf("A/B in backpack = (%v, %v), want (true, true)",
				inBackpackForTest(e, a.id), inBackpackForTest(e, b.id))
		}
	})

	t.Run("politely fails when the eviction has nowhere to land", func(t *testing.T) {
		t.Parallel()

		e := newFixture(t) // main=A, off=B, W in backpack[2]; 0,1,3 free -> fill them

		for i := range e.backpack {
			if e.backpack[i].empty() {
				e.backpack[i] = backpackEntry{inst: itemInstance{id: int64(100 + i), defID: idIronWarhammer}, count: 1}
			}
		}

		preMain, preOff, prePack := e.equipped[protocol.SlotMainHand], e.equipped[protocol.SlotOffHand], e.backpack

		if got, want := e.equipWeaponLocked(w, wDef), ErrBackpackFull; !errors.Is(got, want) {
			t.Errorf("err = %v, want %v", got, want)
		}

		if e.equipped[protocol.SlotMainHand] != preMain || e.equipped[protocol.SlotOffHand] != preOff {
			t.Error("equipped state changed despite the polite failure")
		}

		if e.backpack != prePack {
			t.Error("backpack changed despite the polite failure")
		}
	})

	t.Run("equipping any weapon while a 2H is held evicts it, off stays empty", func(t *testing.T) {
		t.Parallel()

		e := newFixture(t)

		if err := e.equipWeaponLocked(w, wDef); err != nil {
			t.Fatalf("equip W: %v", err)
		}

		if err := e.equipWeaponLocked(a, aDef); err != nil {
			t.Fatalf("equip A while W held: %v", err)
		}

		if got, want := e.equipped[protocol.SlotMainHand].id, a.id; got != want {
			t.Errorf("main = %d, want A %d", got, want)
		}

		if _, ok := e.equipped[protocol.SlotOffHand]; ok {
			t.Error("off-hand occupied, want empty")
		}

		if !inBackpackForTest(e, w.id) {
			t.Error("W not found in the backpack after being evicted by A")
		}
	})
}

// TestGatesGone: equipValidate no longer takes (or needs) a class at all —
// every item is equippable by every class. A mage equips the iron sword (a
// melee weapon) and leather armor over the real intent path; a rogue
// equipping a magic weapon (the mage's own default) also succeeds.
func TestGatesGone(t *testing.T) {
	t.Parallel()

	w := testItemsWorld()

	mage, err := w.Join("", "tester", protocol.ClassMage, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	swordInst := w.GrantItemForTest(mage.EntityID, idIronSword)
	armorInst := w.GrantItemForTest(mage.EntityID, idLeatherArmor)

	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: mage.EntityID, Token: mage.Token, Kind: protocol.IntentEquip, ItemID: swordInst,
	}); err != nil {
		t.Errorf("mage equip iron sword (melee weapon) = %v, want nil (gates dropped, #56)", err)
	}

	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: mage.EntityID, Token: mage.Token, Kind: protocol.IntentEquip, ItemID: armorInst,
	}); err != nil {
		t.Errorf("mage equip leather armor = %v, want nil", err)
	}

	// equipValidate itself carries no class parameter anymore — ErrWrongClass
	// is unreachable from it for any (class, item) pair; assert directly that
	// a magic weapon (the mage's own default) is equipValidate-nil too, the
	// same as it would be for a rogue.
	if err := equipValidate(itemDefByID[idEmberFocus]); err != nil {
		t.Errorf("equipValidate(ember-focus, a magic weapon) = %v, want nil", err)
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

	if got := e.equippedDefIn(protocol.SlotMainHand); got != nil {
		t.Errorf("equippedDefIn(main-hand) on an empty slot = %v, want nil", got)
	}

	if got := e.equippedDefIn(protocol.SlotOffHand); got != nil {
		t.Errorf("equippedDefIn(off-hand) on an empty slot = %v, want nil", got)
	}
}

// TestEquippedDefInReturnsOwnedInstance: a filled slot resolves to the owned
// instance's def, by id.
func TestEquippedDefInReturnsOwnedInstance(t *testing.T) {
	t.Parallel()

	e := &entity{
		kind: protocol.EntityPlayer, class: protocol.ClassRogue,
		equipped: map[string]itemInstance{protocol.SlotMainHand: {id: 7, defID: idDagger}},
	}

	got := e.equippedDefIn(protocol.SlotMainHand)
	if got == nil || got.id != idDagger {
		t.Errorf("equippedDefIn(main-hand) = %v, want the dagger def", got)
	}
}

// TestCloseDefForFallsBackToFists: a bare player (both hands empty) strikes
// with fists, not a zero-value weapon — for every class.
func TestCloseDefForFallsBackToFists(t *testing.T) {
	t.Parallel()

	for _, class := range []string{protocol.ClassFighter, protocol.ClassRogue, protocol.ClassMage, ""} {
		e := &entity{kind: protocol.EntityPlayer, class: class}

		if got := closeDefFor(e); got != fistsDef {
			t.Errorf("closeDefFor(bare %q player) = %v, want fistsDef", class, got)
		}
	}
}

// TestCloseDefForMonsterIsClaws: a monster (which owns no items) always strikes
// with its kind's own claws profile, regardless of its slot bookkeeping —
// the exact same *itemDef pointer every time (monsters.go's
// buildMonsterIndex builds it once per kind, not fresh per call).
func TestCloseDefForMonsterIsClaws(t *testing.T) {
	t.Parallel()

	e := &entity{kind: protocol.EntityMonster, monsterKind: idKindWolf}

	got := closeDefFor(e)
	if got != monsterDefByID[idKindWolf].weaponDef {
		t.Errorf("closeDefFor(wolf) = %v, want the wolf kind's named weapon", got)
	}

	if got, want := got.damage, monsterDefByID[idKindWolf].weaponDef.damage; got != want {
		t.Errorf("closeDefFor(wolf).damage = %d, want %d", got, want)
	}
}

// TestCloseDefForEquippedWeapon: a held melee-tagged weapon wins over the
// fists fallback, in either hand.
func TestCloseDefForEquippedWeapon(t *testing.T) {
	t.Parallel()

	rogue := &entity{
		kind: protocol.EntityPlayer, class: protocol.ClassRogue,
		equipped: map[string]itemInstance{protocol.SlotMainHand: {id: 1, defID: idDagger}},
	}

	if got := closeDefFor(rogue); got == nil || got.id != idDagger {
		t.Errorf("closeDefFor(equipped dagger) = %v, want the dagger def", got)
	}

	// oak-wand is melee-tagged (a "wand bonk") even in the off-hand.
	mage := &entity{
		kind: protocol.EntityPlayer, class: protocol.ClassMage,
		equipped: map[string]itemInstance{protocol.SlotOffHand: {id: 2, defID: idOakWand}},
	}

	if got := closeDefFor(mage); got == nil || got.id != idOakWand {
		t.Errorf("closeDefFor(mage with wand) = %v, want the oak-wand def (wand bonk)", got)
	}
}

// TestMagicOnlyWeaponNeverMelees: a mage holding only a magic-tagged weapon
// (no melee-tagged item in either hand) strikes with FISTS — a magic weapon
// never melees — but still has a ranged attack via rangedDefFor.
func TestMagicOnlyWeaponNeverMelees(t *testing.T) {
	t.Parallel()

	mage := &entity{
		kind: protocol.EntityPlayer, class: protocol.ClassMage,
		equipped: map[string]itemInstance{protocol.SlotMainHand: {id: 3, defID: idEmberFocus}},
	}

	if got := closeDefFor(mage); got != fistsDef {
		t.Errorf("closeDefFor(mage with magic weapon only) = %v, want fistsDef (magic never melees)", got)
	}

	if got := rangedDefFor(mage); got == nil || got.id != idEmberFocus {
		t.Errorf("rangedDefFor(mage with ember-focus) = %v, want the ember-focus def", got)
	}
}

// TestRangedDefForEmptyIsNil: empty hands report nil — the "no ranged
// weapon" signal queueAttackLocked/resolveRangedLocked read, with no
// fallback (unlike close).
func TestRangedDefForEmptyIsNil(t *testing.T) {
	t.Parallel()

	e := &entity{kind: protocol.EntityPlayer, class: protocol.ClassRogue}

	if got := rangedDefFor(e); got != nil {
		t.Errorf("rangedDefFor(empty hands) = %v, want nil", got)
	}
}

// TestFighterHasNoRangedAttack: a fighter's class default (iron sword) is
// melee-tagged only, so a fully-equipped fighter still has NO ranged
// weapon — preserving the pre-keystone "fighter has no ranged attack"
// combat contract via tags instead of a class-shaped empty slot.
func TestFighterHasNoRangedAttack(t *testing.T) {
	t.Parallel()

	fighter := &entity{
		kind: protocol.EntityPlayer, class: protocol.ClassFighter,
		equipped: map[string]itemInstance{protocol.SlotMainHand: {id: 1, defID: idIronSword}},
	}

	if got := rangedDefFor(fighter); got != nil {
		t.Errorf("rangedDefFor(fighter) = %v, want nil (iron sword is melee-only)", got)
	}

	sword := itemDefByID[idIronSword]
	if sword.hasTag(protocol.WeaponTagRanged) || sword.hasTag(protocol.WeaponTagMagic) {
		t.Errorf("iron-sword tags = %v, must not carry a ranged/magic tag this slice", sword.tags)
	}
}

// TestRangedDefForEquippedWeapon: a held ranged-tagged item resolves by id.
func TestRangedDefForEquippedWeapon(t *testing.T) {
	t.Parallel()

	e := &entity{
		kind: protocol.EntityPlayer, class: protocol.ClassRogue,
		equipped: map[string]itemInstance{protocol.SlotOffHand: {id: 2, defID: idShortbow}},
	}

	if got := rangedDefFor(e); got == nil || got.id != idShortbow {
		t.Errorf("rangedDefFor(equipped shortbow) = %v, want the shortbow def", got)
	}
}

// TestHeldWeaponsMainThenOff: heldWeapons returns occupied hands in fixed
// main-then-off order, skipping an empty hand.
func TestHeldWeaponsMainThenOff(t *testing.T) {
	t.Parallel()

	e := &entity{
		kind: protocol.EntityPlayer, class: protocol.ClassRogue,
		equipped: map[string]itemInstance{
			protocol.SlotMainHand: {id: 1, defID: idDagger},
			protocol.SlotOffHand:  {id: 2, defID: idShortbow},
		},
	}

	held := e.heldWeapons()
	if got, want := len(held), 2; got != want {
		t.Fatalf("heldWeapons() len = %d, want %d", got, want)
	}

	if got, want := held[0].id, idDagger; got != want {
		t.Errorf("heldWeapons()[0] = %q, want main-hand's %q", got, want)
	}

	if got, want := held[1].id, idShortbow; got != want {
		t.Errorf("heldWeapons()[1] = %q, want off-hand's %q", got, want)
	}

	bare := &entity{kind: protocol.EntityPlayer, class: protocol.ClassFighter}
	if got := bare.heldWeapons(); len(got) != 0 {
		t.Errorf("heldWeapons(bare) = %v, want empty", got)
	}
}

// TestItemViewOfWeaponSlotDistinguishesHands pins the wire fix behind the
// gear keystone's client re-key (K1 review finding): itemViewOf must set an
// EQUIPPED weapon's ItemView.Type to the hand it occupies (SlotMainHand/
// SlotOffHand), not the generic ItemTypeWeapon taxonomy string every weapon
// def shares — otherwise two dual-wielded weapons collide under one wire
// "type" and a client keying its equipped map by Type can only ever show one
// of them. A backpack (unequipped) weapon keeps the generic type, since it
// has no hand yet (weaponTargetSlot decides one at equip time).
func TestItemViewOfWeaponSlotDistinguishesHands(t *testing.T) {
	t.Parallel()

	dagger := itemInstance{id: 1, defID: idDagger}
	bow := itemInstance{id: 2, defID: idShortbow}
	spareSword := itemInstance{id: 3, defID: idIronSword}

	e := &entity{
		kind: protocol.EntityPlayer, class: protocol.ClassRogue,
		equipped: map[string]itemInstance{
			protocol.SlotMainHand: dagger,
			protocol.SlotOffHand:  bow,
		},
	}
	e.backpack[0] = backpackEntry{inst: spareSword, count: 1}

	views := itemViewsLocked(e)
	if got, want := len(views), 3; got != want {
		t.Fatalf("len(itemViewsLocked) = %d, want %d", got, want)
	}

	byID := make(map[int64]protocol.ItemView, len(views))
	for _, v := range views {
		byID[v.ID] = v
	}

	if got, want := byID[dagger.id].Type, protocol.SlotMainHand; got != want {
		t.Errorf("main-hand dagger Type = %q, want %q", got, want)
	}

	if got, want := byID[bow.id].Type, protocol.SlotOffHand; got != want {
		t.Errorf("off-hand shortbow Type = %q, want %q", got, want)
	}

	if got, want := byID[spareSword.id].Type, protocol.ItemTypeWeapon; got != want {
		t.Errorf("backpack spare sword Type = %q, want the generic %q (no hand assigned yet)", got, want)
	}

	if !byID[dagger.id].Equipped || !byID[bow.id].Equipped || byID[spareSword.id].Equipped {
		t.Errorf("Equipped flags = dagger:%v bow:%v spareSword:%v, want true/true/false",
			byID[dagger.id].Equipped, byID[bow.id].Equipped, byID[spareSword.id].Equipped)
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
		equipped: map[string]itemInstance{protocol.SlotMainHand: sword},
	}
	e.backpack[2] = backpackEntry{inst: hammer, count: 1}

	e.toggleEquip(hammer, protocol.SlotMainHand)

	if got, want := e.equipped[protocol.SlotMainHand].id, hammer.id; got != want {
		t.Errorf("main-hand slot = instance %d, want the warhammer %d", got, want)
	}

	if got, want := e.backpack[2].inst.id, sword.id; got != want {
		t.Errorf("backpack[2] = instance %d, want the displaced sword %d", got, want)
	}

	// Now unequip the hammer (toggle on the already-equipped instance): it
	// needs a free entry, and 0/1/3 are free — it lands in the first one.
	e.toggleEquip(hammer, protocol.SlotMainHand)

	if got := e.equipped[protocol.SlotMainHand].id; got != 0 {
		t.Errorf("main-hand slot after unequip = instance %d, want empty", got)
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
		equipped: map[string]itemInstance{protocol.SlotMainHand: sword},
	}

	for i := range e.backpack {
		e.backpack[i] = backpackEntry{inst: itemInstance{id: int64(10 + i), defID: idIronWarhammer}, count: 1}
	}

	e.toggleEquip(sword, protocol.SlotMainHand)

	if got, want := e.equipped[protocol.SlotMainHand].id, sword.id; got != want {
		t.Errorf("main-hand slot = instance %d, want the sword %d still equipped (backpack full)", got, want)
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
	return NewWorld(WorldConfig{
		Interval:        time.Hour,
		CombatPatience:  time.Minute,
		BubblePoll:      time.Millisecond,
		DisconnectGrace: time.Hour,
		WorldSeed:       0xC0FFEE,
		Radius:          12,
		Ticks:           hub.New(),
	})
}

// TestJoinRogueOwnsDaggerAndShortbowEquipped: Join grants and equips a fresh
// player's class defaults through the same placement path a player equip
// uses (weaponTargetSlot/toggleEquip) — a Rogue ends up with dagger in
// main-hand and shortbow in off-hand, backpack fully free.
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

	mainDef := e.equippedDefIn(protocol.SlotMainHand)
	if mainDef == nil || mainDef.id != idDagger {
		t.Errorf("rogue main-hand slot = %v, want the dagger def", mainDef)
	}

	offDef := e.equippedDefIn(protocol.SlotOffHand)
	if offDef == nil || offDef.id != idShortbow {
		t.Errorf("rogue off-hand slot = %v, want the shortbow def", offDef)
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
// off-hand stays empty.
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

	if got := e.equippedDefIn(protocol.SlotMainHand); got == nil || got.id != idIronSword {
		t.Errorf("fighter main-hand slot = %v, want the iron-sword def", got)
	}

	if got := e.equippedDefIn(protocol.SlotOffHand); got != nil {
		t.Errorf("fighter off-hand slot = %v, want nil (no ranged default)", got)
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
		{id: "dup", itemType: protocol.ItemTypeWeapon, tags: []string{protocol.WeaponTagMelee}},
		{id: "dup", itemType: protocol.ItemTypeWeapon, tags: []string{protocol.WeaponTagMelee}},
	})
}

// TestValidateItemDefsPanicsOnUnknownType: an itemType outside the taxonomy's
// 8 must fail at load.
func TestValidateItemDefsPanicsOnUnknownType(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("validateItemDefs did not panic on an unknown item type")
		}
	}()

	validateItemDefs([]*itemDef{{id: "x", itemType: "waist"}})
}

// TestValidateItemDefsPanicsOnWeaponTagShape covers every invalid weapon tag
// shape validateWeaponTags rejects: no tags at all, an unknown tag, a
// duplicate tag, and a magic-tagged weapon with no range.
func TestValidateItemDefsPanicsOnWeaponTagShape(t *testing.T) {
	t.Parallel()

	t.Run("no tags", func(t *testing.T) {
		t.Parallel()

		defer func() {
			if r := recover(); r == nil {
				t.Error("validateItemDefs did not panic on a weapon with no tags")
			}
		}()

		validateItemDefs([]*itemDef{{id: "x", itemType: protocol.ItemTypeWeapon}})
	})

	t.Run("unknown tag", func(t *testing.T) {
		t.Parallel()

		defer func() {
			if r := recover(); r == nil {
				t.Error("validateItemDefs did not panic on a weapon with an unknown tag")
			}
		}()

		validateItemDefs([]*itemDef{{id: "x", itemType: protocol.ItemTypeWeapon, tags: []string{"poison"}}})
	})

	t.Run("duplicate tag", func(t *testing.T) {
		t.Parallel()

		defer func() {
			if r := recover(); r == nil {
				t.Error("validateItemDefs did not panic on a weapon with a duplicate tag")
			}
		}()

		validateItemDefs([]*itemDef{{
			id: "x", itemType: protocol.ItemTypeWeapon,
			tags: []string{protocol.WeaponTagMelee, protocol.WeaponTagMelee},
		}})
	})

	t.Run("magic weapon with no range", func(t *testing.T) {
		t.Parallel()

		defer func() {
			if r := recover(); r == nil {
				t.Error("validateItemDefs did not panic on a magic weapon with rangeHex 0")
			}
		}()

		validateItemDefs([]*itemDef{{id: "x", itemType: protocol.ItemTypeWeapon, tags: []string{protocol.WeaponTagMagic}}})
	})
}

// TestValidateItemDefsPanicsOnNonWeaponTagsOrTwoHanded: tags and twoHanded
// are weapon-only fields — a content bug setting either on armor/jewelry
// must fail at load.
func TestValidateItemDefsPanicsOnNonWeaponTagsOrTwoHanded(t *testing.T) {
	t.Parallel()

	t.Run("tags on armor", func(t *testing.T) {
		t.Parallel()

		defer func() {
			if r := recover(); r == nil {
				t.Error("validateItemDefs did not panic on a non-weapon item with tags set")
			}
		}()

		validateItemDefs([]*itemDef{{id: "x", itemType: protocol.ItemTypeChest, tags: []string{protocol.WeaponTagMelee}}})
	})

	t.Run("twoHanded on armor", func(t *testing.T) {
		t.Parallel()

		defer func() {
			if r := recover(); r == nil {
				t.Error("validateItemDefs did not panic on a non-weapon item with twoHanded set")
			}
		}()

		validateItemDefs([]*itemDef{{id: "x", itemType: protocol.ItemTypeChest, twoHanded: true}})
	})
}

// TestValidateItemDefsPanicsOnNonWeaponCombatStats: damage, rangeHex, and
// aoeRadius are weapon-only fields — only a weapon fires as a hit, so a
// copy-paste shield or armor def carrying one is an authoring mistake that
// must fail at load (#90; a shield's −N lives in its rule card, not a damage
// field).
func TestValidateItemDefsPanicsOnNonWeaponCombatStats(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		def  *itemDef
	}{
		{"damage on a shield", &itemDef{id: "x", itemType: protocol.ItemTypeShield, damage: 3}},
		{"rangeHex on armor", &itemDef{id: "x", itemType: protocol.ItemTypeChest, rangeHex: 2}},
		{"aoeRadius on a shield", &itemDef{id: "x", itemType: protocol.ItemTypeShield, aoeRadius: 1}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			defer func() {
				if r := recover(); r == nil {
					t.Errorf("validateItemDefs did not panic on %s", tc.name)
				}
			}()

			validateItemDefs([]*itemDef{tc.def})
		})
	}
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

		validateItemDefs([]*itemDef{{id: "x", itemType: protocol.ItemTypeHelmet, heal: 3}})
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
				id: "x", itemType: protocol.ItemTypeWeapon, tags: []string{protocol.WeaponTagMelee},
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
		id: "cloak-of-shadows", itemType: protocol.ItemTypeChest,
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
		id: "long-bow", itemType: protocol.ItemTypeWeapon, tags: []string{protocol.WeaponTagRanged},
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

	// re-derived: staves 2H, wands 1H (keystone amendment) — damage 3 -> 6.
	if got, want := staff.damage, 6; got != want {
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

	// Gear keystone re-typing: the war-mage staff is ItemTypeWeapon tagged
	// magic (it is the mage's ranged-ish AoE caster) — the spec's §4 table.
	if got, want := staff.itemType, protocol.ItemTypeWeapon; got != want {
		t.Errorf("war-mage-staff itemType = %q, want %q", got, want)
	}

	if !staff.hasTag(protocol.WeaponTagMagic) {
		t.Errorf("war-mage-staff tags = %v, want to include %q", staff.tags, protocol.WeaponTagMagic)
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
		id: "bad", name: "Bad", itemType: protocol.ItemTypeWeapon, tags: []string{protocol.WeaponTagMelee},
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
		id: "bad", name: "Bad", itemType: protocol.ItemTypeWeapon, tags: []string{protocol.WeaponTagMelee},
		rules: []ruleCard{{
			event: evDealDamage,
			when:  []condition{{kind: condTargetKind, s: "griffin"}},
			then:  effect{kind: effMulPct, n: 150},
		}},
	}})
}

// TestWyrmslayerGreatswordPinned pins the first designer card's full intent
// (milestone 6c), retagged/rebalanced as the gear keystone's first
// two-handed weapon (#55/#56 §4): melee-tagged, two-handed, damage 9, ×1.5
// vs dragons via condTargetKind, and a dragon-only drop (present in
// dragon's table, absent from every other kind's).
func TestWyrmslayerGreatswordPinned(t *testing.T) {
	t.Parallel()

	sword, ok := itemDefByID[idWyrmslayerGreatsword]
	if !ok {
		t.Fatal("wyrmslayer-greatsword not registered")
	}

	if !sword.hasTag(protocol.WeaponTagMelee) {
		t.Errorf("sword tags = %v, want to include %q", sword.tags, protocol.WeaponTagMelee)
	}

	if got, want := sword.twoHanded, true; got != want {
		t.Errorf("sword twoHanded = %v, want %v (the keystone's first 2H weapon)", got, want)
	}

	// re-derived: gear keystone rebalance (damage 4 -> 9, the §4 "1H ≈ ½ 2H"
	// anchor — a 2H roughly doubles a 1H's damage).
	if got, want := sword.damage, 9; got != want {
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
// content's cards (task 3, re-typed by the gear keystone): leather-armor
// (chest, take-damage −1), headband-of-learning (helmet, earn-XP ×1.05),
// and healing-potion (consumable, heal 5, no rules) — plus the potion's
// low-weight presence in the rat and wolf drop tables (recovery layer 2).
// Class gates are gone (#55/#56): neither armor card restricts wearability
// anymore.
func TestStarterInventoryContentPinned(t *testing.T) {
	t.Parallel()

	armor, ok := itemDefByID[idLeatherArmor]
	if !ok {
		t.Fatal("leather-armor not registered")
	}

	if got, want := armor.itemType, protocol.ItemTypeChest; got != want {
		t.Errorf("armor itemType = %q, want %q", got, want)
	}

	if got, want := len(armor.rules), 1; got != want {
		t.Fatalf("armor rules = %d, want %d", got, want)
	}

	if got, want := armor.rules[0].event, evTakeDamage; got != want {
		t.Errorf("armor rule event = %q, want %q", got, want)
	}

	// re-derived: mitigation went PERCENTAGE (#154) — leather is ×0.9, not −1.
	if got, want := armor.rules[0].then, (effect{kind: effMulPct, n: percentBase - 10}); got != want {
		t.Errorf("armor rule effect = %+v, want %+v", got, want)
	}

	band, ok := itemDefByID[idHeadbandOfLearning]
	if !ok {
		t.Fatal("headband-of-learning not registered")
	}

	if got, want := band.itemType, protocol.ItemTypeHelmet; got != want {
		t.Errorf("headband itemType = %q, want %q", got, want)
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

	// The Kin Archer (#179) carries the same low-weight potion as the rat;
	// the Goblin (#266) also carries it at weight 1 (the other expansion
	// kinds carry the Minor Salve / Greater Draught instead).
	wantTables := map[string]int{idKindRat: 1, idKindWolf: 2, idKindArcher: 1, idKindGoblin: 1}

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

// TestValidateItemDefsPanicsOnDamageTypeShape (#92): every weapon carries
// exactly one of the six damage types and no non-weapon carries any. An
// untyped weapon would silently dodge every resist and vulnerability card
// ever written — a content bug that would only surface as odd numbers
// mid-fight, so it fails at load instead.
func TestValidateItemDefsPanicsOnDamageTypeShape(t *testing.T) {
	t.Parallel()

	t.Run("weapon without a type", func(t *testing.T) {
		t.Parallel()

		defer func() {
			if r := recover(); r == nil {
				t.Error("validateItemDefs did not panic on a weapon with no damage type")
			}
		}()

		validateItemDefs([]*itemDef{{
			id: "x", itemType: protocol.ItemTypeWeapon,
			tags: []string{protocol.WeaponTagMelee}, damage: 3,
		}})
	})

	t.Run("weapon with an unknown type", func(t *testing.T) {
		t.Parallel()

		defer func() {
			if r := recover(); r == nil {
				t.Error("validateItemDefs did not panic on a weapon with an unknown damage type")
			}
		}()

		validateItemDefs([]*itemDef{{
			id: "x", itemType: protocol.ItemTypeWeapon, damageType: "psychic",
			tags: []string{protocol.WeaponTagMelee}, damage: 3,
		}})
	})

	t.Run("armor with a type", func(t *testing.T) {
		t.Parallel()

		defer func() {
			if r := recover(); r == nil {
				t.Error("validateItemDefs did not panic on a non-weapon carrying a damage type")
			}
		}()

		validateItemDefs([]*itemDef{{
			id: "x", itemType: protocol.ItemTypeChest, damageType: protocol.DamageTypeFire,
		}})
	})
}

// TestValidateRuleCardsPanicsOnUnknownDamageType (#92): a resist card naming
// a type that doesn't exist would silently never hold — the same fail-loud
// treatment condAttackerSpecies and condTargetKind already get.
func TestValidateRuleCardsPanicsOnUnknownDamageType(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("validateRuleCards did not panic on a card naming an unknown damage type")
		}
	}()

	validateRuleCards("x", []ruleCard{{
		event: evTakeDamage,
		when:  []condition{{kind: condDamageType, s: "psychic"}},
		then:  effect{kind: effMulPct, n: 50},
	}})
}

// TestEveryWeaponAndKindCarriesADamageType (#92) pins the spec's assignment
// table: every registered weapon has a valid type, every monster kind's claws
// carry their kind's type through the buildMonsterIndex seam, and the
// built-in fists fall back to blunt.
func TestEveryWeaponAndKindCarriesADamageType(t *testing.T) {
	t.Parallel()

	for _, def := range itemDefs {
		if def.isWeapon() && !validDamageType(def.damageType) {
			t.Errorf("weapon %s damage type = %q, want one of the six", def.id, def.damageType)
		}
	}

	if got, want := fistsDef.damageType, protocol.DamageTypeBlunt; got != want {
		t.Errorf("fists damage type = %q, want %q", got, want)
	}

	wantKinds := map[string]string{
		idKindRat:    protocol.DamageTypeSharp,
		idKindWolf:   protocol.DamageTypeSharp,
		idKindGhoul:  protocol.DamageTypeChaos,
		idKindTroll:  protocol.DamageTypeBlunt,
		idKindDragon: protocol.DamageTypeFire,
	}

	for id, want := range wantKinds {
		def, ok := monsterDefByID[id]
		if !ok {
			t.Fatalf("monster kind %s not registered", id)
		}

		if got := def.weaponDef.damageType; got != want {
			t.Errorf("%s claws damage type = %q, want %q", id, got, want)
		}
	}
}

// TestValidateRuleCardsPanicsOnUnknownWeaponTag (#124): a tag gate naming a
// tag that can't exist would silently never hold — the same fail-loud
// treatment attackerSpecies, targetKind and damageType already get.
func TestValidateRuleCardsPanicsOnUnknownWeaponTag(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("validateRuleCards did not panic on a card naming an unknown weapon tag")
		}
	}()

	validateRuleCards("x", []ruleCard{{
		event: evDealDamage,
		when:  []condition{{kind: condWeaponTagged, s: "polearm"}},
		then:  effect{kind: effMulPct, n: percentBase + 10},
	}})
}

// TestValidateRuleCardsAcceptsTheNewSkillConditions (#124): both kinds are
// known to the validator — the third of the three places that must agree.
func TestValidateRuleCardsAcceptsTheNewSkillConditions(t *testing.T) {
	t.Parallel()

	validateRuleCards("x", []ruleCard{
		{
			event: evDealDamage,
			when:  []condition{{kind: condWeaponTagged, s: protocol.WeaponTagMelee}},
			then:  effect{kind: effMulPct, n: percentBase + 10},
		},
		{
			event: evTakeDamage,
			when:  []condition{{kind: condShieldEquipped}},
			then:  effect{kind: effMulPct, n: protocol.GlanceDamagePercent},
		},
	})
}

// TestValidateItemDefsPanicsOnMixedNature (#171 task 2): an item's cards must
// point the same way as its type. This is what makes the stat line's sign
// convention readable — "−20% Damage" carries no "Taken" suffix because a
// weapon means dealt and worn kit means taken, and one mixed item would make
// every tooltip ambiguous at a glance.
func TestValidateItemDefsPanicsOnMixedNature(t *testing.T) {
	t.Parallel()

	t.Run("defensive card on a weapon", func(t *testing.T) {
		t.Parallel()

		defer func() {
			if r := recover(); r == nil {
				t.Error("validateItemDefs accepted a take-damage card on a weapon")
			}
		}()

		validateItemDefs([]*itemDef{{
			id: "x", itemType: protocol.ItemTypeWeapon, damageType: protocol.DamageTypeSharp,
			tags: []string{protocol.WeaponTagMelee}, damage: 3,
			rules: []ruleCard{{event: evTakeDamage, then: effect{kind: effMulPct, n: percentBase - 10}}},
		}})
	})

	t.Run("offensive card on armor", func(t *testing.T) {
		t.Parallel()

		defer func() {
			if r := recover(); r == nil {
				t.Error("validateItemDefs accepted a deal-damage card on a chest item")
			}
		}()

		validateItemDefs([]*itemDef{{
			id: "x", itemType: protocol.ItemTypeChest,
			rules: []ruleCard{{event: evDealDamage, then: effect{kind: effAdd, n: 3}}},
		}})
	})

	t.Run("utility cards are exempt on either nature", func(t *testing.T) {
		t.Parallel()

		// Iron Plate Armor's shape: a defensive card plus an aggro-range
		// drawback on the same worn item (#171 Q5).
		validateItemDefs([]*itemDef{{
			id: "x", itemType: protocol.ItemTypeChest,
			rules: []ruleCard{
				{event: evTakeDamage, then: effect{kind: effMulPct, n: percentBase - 20}},
				{event: evAggroRange, then: effect{kind: effMulPct, n: percentBase + 25}},
			},
		}})

		// And an XP card on a helmet (Headband of Learning).
		validateItemDefs([]*itemDef{{
			id: "y", itemType: protocol.ItemTypeHelmet,
			rules: []ruleCard{{event: evEarnXP, then: effect{kind: effMulPct, n: percentBase + 5}}},
		}})
	})
}

// TestOneBaseLayer_EveryDamageSourceIsAnItemDef (#175) pins the invariant that
// makes base stats safe to keep as plain fields instead of rule cards: there is
// exactly ONE base layer, and every damage number the combat path consumes
// comes out of an *itemDef.
//
// #175 asked whether damage/range/heal should become cards for uniformity. The
// answer was no, and the reason is this invariant rather than taste — the
// argument against base-as-fields is fragmentation (content types growing
// incompatible base layers), and we have none: players, bare fists and monsters
// all arrive at rollDamageLocked as an *itemDef read through itemDamage. If
// that ever stopped being true, base-as-fields would become the wrong call and
// #175 would deserve reopening. Nothing else pinned it, so this test does.
func TestOneBaseLayer_EveryDamageSourceIsAnItemDef(t *testing.T) {
	t.Parallel()

	// A monster's claws are a REAL itemDef compiled from the kind's shorthand
	// by buildMonsterIndex — not a parallel damage representation.
	for _, def := range monsterDefs {
		w := def.weaponDef
		if w == nil {
			t.Errorf("monster kind %s resolved no weapon def", def.id)

			continue
		}

		// #179: the kind's damage IS the registry weapon's — there is no
		// second copy to disagree with it any more.
		if got, want := itemDamage(w), itemDefByID[def.weapon].damage; got != want {
			t.Errorf("itemDamage(%s weapon) = %d, want the registry's %d", def.id, got, want)
		}

		if !w.isWeapon() {
			t.Errorf("%s weapon item type = %q, want a weapon", def.id, w.itemType)
		}

		if !w.monsterOnly {
			t.Errorf("%s names player-reachable weapon %s", def.id, def.weapon)
		}
	}

	// Every entity the melee path can be asked about yields non-empty
	// []*itemDef with a real base — a player holding a weapon, a bare player
	// (fists), and a monster (claws). No branch returns a bare number.
	cases := map[string]*entity{
		"armed player": {
			kind: protocol.EntityPlayer, class: protocol.ClassRogue,
			equipped: map[string]itemInstance{protocol.SlotMainHand: {id: 7, defID: idDagger}},
		},
		"bare player": {kind: protocol.EntityPlayer, class: protocol.ClassFighter},
		"monster":     {kind: protocol.EntityMonster, monsterKind: idKindWolf},
	}

	for name, e := range cases {
		defs := meleeDefsFor(e)
		if len(defs) == 0 {
			t.Errorf("meleeDefsFor(%s) is empty, want at least one base profile", name)

			continue
		}

		for _, def := range defs {
			if got := itemDamage(def); got <= 0 {
				t.Errorf("meleeDefsFor(%s) base %q damage = %d, want > 0", name, def.id, got)
			}
		}
	}
}

// TestMonsterOnlyWeaponsAreUnreachableByPlayers (#179): monster natural
// weapons live in the SAME registry as player gear — that is the point of the
// change — so the only thing keeping Dragon Jaws out of a player's main hand
// is this validation. Trusting the content author is not enough: a drop row is
// one line, and a 9-damage fire weapon in a starting inventory would not look
// like a mistake until someone used it.
func TestMonsterOnlyWeaponsAreUnreachableByPlayers(t *testing.T) {
	t.Parallel()

	for _, def := range monsterDefs {
		for _, d := range def.drops {
			item, ok := itemDefByID[d.defID]
			if !ok {
				t.Fatalf("kind %s drops unregistered item %s", def.id, d.defID)
			}

			if item.monsterOnly {
				t.Errorf("kind %s drops monster-only weapon %s", def.id, d.defID)
			}
		}
	}

	for _, class := range []string{protocol.ClassFighter, protocol.ClassRogue, protocol.ClassMage} {
		for _, id := range classDefaultIDs(class) {
			if def, ok := itemDefByID[id]; ok && def.monsterOnly {
				t.Errorf("class %s starts with monster-only weapon %s", class, id)
			}
		}
	}
}

// TestValidateMonsterOnlyItemsPanicsOnADroppableNaturalWeapon: the guard
// itself, exercised directly — the table above is green today, so without this
// the validation could be deleted and nothing would notice.
func TestValidateMonsterOnlyItemsPanicsOnADroppableNaturalWeapon(t *testing.T) {
	t.Parallel()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("validateMonsterOnlyItems did not panic on a droppable natural weapon")
		}

		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value = %T, want string", r)
		}

		if got, want := msg, "monster-only"; !strings.Contains(got, want) {
			t.Errorf("panic = %q, should mention %q", got, want)
		}
	}()

	validateMonsterOnlyItems(
		[]*monsterDef{{id: "x", drops: []drop{{defID: idClaws, weight: 1}}}},
		map[string]*itemDef{idClaws: {id: idClaws, monsterOnly: true}},
	)
}
