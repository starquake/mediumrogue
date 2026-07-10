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

		if got, want := def.dropWeight, 0; got != want {
			t.Errorf("%s dropWeight = %d, want %d (class defaults never drop)", tc.id, got, want)
		}
	}
}

// TestClassDefaultIDsAreRegistered: every id classDefaultIDs names for a
// playable class must exist in the registry — the exact bug
// mustValidateContent guards against at process start.
func TestClassDefaultIDsAreRegistered(t *testing.T) {
	t.Parallel()

	for _, class := range []string{protocol.ClassFighter, protocol.ClassRogue, protocol.ClassMage} {
		ids := classDefaultIDs(class)
		if len(ids) == 0 {
			t.Errorf("class %q has no default items", class)
		}

		for _, id := range ids {
			if _, ok := itemDefByID[id]; !ok {
				t.Errorf("class %q default %q is not a registered item", class, id)
			}
		}
	}
}

// TestItemDamageScalesWithLevel: an item's damage grows by DamagePerLevel per
// level above 1 — the single source both melee and ranged read.
func TestItemDamageScalesWithLevel(t *testing.T) {
	t.Parallel()

	const level = 3

	def := itemDefByID[idDagger]

	if got, want := itemDamage(def, level), def.damage+protocol.DamagePerLevel*(level-1); got != want {
		t.Errorf("itemDamage(dagger, %d) = %d, want %d", level, got, want)
	}

	if got, floor := itemDamage(def, level), def.damage; got <= floor {
		t.Errorf("itemDamage(dagger, %d) = %d, want > level-1 base %d", level, got, floor)
	}
}

// TestEquippedDefEmptySlotIsNil: a slot with no instance id (0) reports nil,
// not a zero-value def.
func TestEquippedDefEmptySlotIsNil(t *testing.T) {
	t.Parallel()

	e := &entity{kind: protocol.EntityPlayer}

	if got := e.equippedDef(protocol.ItemSlotClose); got != nil {
		t.Errorf("equippedDef(close) on an empty slot = %v, want nil", got)
	}

	if got := e.equippedDef(protocol.ItemSlotRanged); got != nil {
		t.Errorf("equippedDef(ranged) on an empty slot = %v, want nil", got)
	}
}

// TestEquippedDefReturnsOwnedInstance: a filled slot resolves to the owned
// instance's def, by id.
func TestEquippedDefReturnsOwnedInstance(t *testing.T) {
	t.Parallel()

	e := &entity{
		kind:      protocol.EntityPlayer,
		items:     []itemInstance{{id: 7, defID: idIronSword}},
		closeSlot: 7,
	}

	got := e.equippedDef(protocol.ItemSlotClose)
	if got == nil || got.id != idIronSword {
		t.Errorf("equippedDef(close) = %v, want the iron-sword def", got)
	}
}

// TestCloseDefForFallsBackToFists: a bare player (empty close slot) bumps
// with fists, not a zero-value weapon.
func TestCloseDefForFallsBackToFists(t *testing.T) {
	t.Parallel()

	e := &entity{kind: protocol.EntityPlayer}

	if got := closeDefFor(e); got != fistsDef {
		t.Errorf("closeDefFor(bare player) = %v, want fistsDef", got)
	}
}

// TestCloseDefForMonsterIsClaws: a monster (which owns no items) always bumps
// with claws, regardless of its close slot bookkeeping.
func TestCloseDefForMonsterIsClaws(t *testing.T) {
	t.Parallel()

	e := &entity{kind: protocol.EntityMonster}

	if got := closeDefFor(e); got != monsterClawsDef {
		t.Errorf("closeDefFor(monster) = %v, want monsterClawsDef", got)
	}
}

// TestCloseDefForEquippedWeapon: an equipped close-slot item wins over the
// fists fallback.
func TestCloseDefForEquippedWeapon(t *testing.T) {
	t.Parallel()

	e := &entity{
		kind:      protocol.EntityPlayer,
		items:     []itemInstance{{id: 1, defID: idDagger}},
		closeSlot: 1,
	}

	if got := closeDefFor(e); got == nil || got.id != idDagger {
		t.Errorf("closeDefFor(equipped dagger) = %v, want the dagger def", got)
	}
}

// TestRangedDefForEmptyIsNil: an empty ranged slot (Fighter default, or any
// unarmed entity) reports nil — the "no ranged weapon" signal
// queueAttackLocked/resolveRangedLocked read, with no fallback (unlike close).
func TestRangedDefForEmptyIsNil(t *testing.T) {
	t.Parallel()

	e := &entity{kind: protocol.EntityPlayer}

	if got := rangedDefFor(e); got != nil {
		t.Errorf("rangedDefFor(no ranged slot) = %v, want nil", got)
	}
}

// TestRangedDefForEquippedWeapon: an equipped ranged-slot item resolves by id.
func TestRangedDefForEquippedWeapon(t *testing.T) {
	t.Parallel()

	e := &entity{
		kind:       protocol.EntityPlayer,
		items:      []itemInstance{{id: 2, defID: idShortbow}},
		rangedSlot: 2,
	}

	if got := rangedDefFor(e); got == nil || got.id != idShortbow {
		t.Errorf("rangedDefFor(equipped shortbow) = %v, want the shortbow def", got)
	}
}

// testItemsWorld builds a minimal World for the Join-level test below,
// mirroring world_test.go's newWorld (unavailable here: that helper lives in
// the black-box game_test package).
func testItemsWorld() *World {
	return NewWorld(time.Hour, time.Minute, time.Millisecond, time.Hour, 0xC0FFEE, 12, hub.New())
}

// TestJoinRogueOwnsDaggerAndShortbowEquipped: Join grants and equips a fresh
// player's class defaults — a Rogue ends up owning exactly dagger + shortbow,
// both slots filled with those instances.
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

	if got, want := len(e.items), 2; got != want {
		t.Fatalf("joined rogue owns %d items, want %d (dagger + shortbow)", got, want)
	}

	closeDef := e.equippedDef(protocol.ItemSlotClose)
	if closeDef == nil || closeDef.id != idDagger {
		t.Errorf("rogue close slot = %v, want the dagger def", closeDef)
	}

	rangedDef := e.equippedDef(protocol.ItemSlotRanged)
	if rangedDef == nil || rangedDef.id != idShortbow {
		t.Errorf("rogue ranged slot = %v, want the shortbow def", rangedDef)
	}

	for _, it := range e.items {
		if it.id == 0 {
			t.Errorf("item instance %+v has a zero id", it)
		}
	}
}

// TestJoinFighterOwnsOnlyIronSword: a Fighter has no ranged default — its
// ranged slot stays empty.
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

	if got, want := len(e.items), 1; got != want {
		t.Fatalf("joined fighter owns %d items, want %d (iron-sword only)", got, want)
	}

	if got := e.equippedDef(protocol.ItemSlotClose); got == nil || got.id != idIronSword {
		t.Errorf("fighter close slot = %v, want the iron-sword def", got)
	}

	if got := e.equippedDef(protocol.ItemSlotRanged); got != nil {
		t.Errorf("fighter ranged slot = %v, want nil (no ranged default)", got)
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
		{id: "dup", slot: protocol.ItemSlotClose},
		{id: "dup", slot: protocol.ItemSlotClose},
	})
}

// TestValidateItemDefsPanicsOnUnknownSlot.
func TestValidateItemDefsPanicsOnUnknownSlot(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("validateItemDefs did not panic on an unknown slot")
		}
	}()

	validateItemDefs([]*itemDef{{id: "x", slot: "waist"}})
}

// TestValidateItemDefsPanicsOnUnknownClass.
func TestValidateItemDefsPanicsOnUnknownClass(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("validateItemDefs did not panic on an unknown class")
		}
	}()

	validateItemDefs([]*itemDef{{id: "x", slot: protocol.ItemSlotClose, class: "necromancer"}})
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

			validateItemDefs([]*itemDef{{id: "x", slot: protocol.ItemSlotClose, rules: []ruleCard{tc.card}}})
		})
	}
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

	validateMaxReach([]*itemDef{{id: "long-bow", slot: protocol.ItemSlotRanged, rangeHex: protocol.CombatRadius + 1}})
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
