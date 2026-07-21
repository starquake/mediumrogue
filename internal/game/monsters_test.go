//nolint:testpackage // white-box: needs unexported monster-registry internals; see rules_test.go's file doc.
package game

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestMonsterRegistryHasTheExpectedKinds pins the registry's ids — a content
// mistake that drops or renames a kind fails here first. Grew by one in #179 (the
// Kin Archer, the first ranged kind) and by four in #266 (the goblin,
// skeleton, frost wisp and wraith); re-derived deliberately, since adding a
// kind is exactly what this test is meant to notice.
func TestMonsterRegistryHasTheExpectedKinds(t *testing.T) {
	t.Parallel()

	want := map[string]bool{
		idKindRat: true, idKindWolf: true, idKindGhoul: true,
		idKindTroll: true, idKindDragon: true, idKindArcher: true,
		idKindGoblin: true, idKindSkeleton: true, idKindFrostWisp: true, idKindWraith: true,
	}

	if got, want := len(monsterDefs), len(want); got != want {
		t.Fatalf("len(monsterDefs) = %d, want %d", got, want)
	}

	for _, def := range monsterDefs {
		if !want[def.id] {
			t.Errorf("unexpected monster id %q in registry", def.id)
		}
	}

	for id := range want {
		if _, ok := monsterDefByID[id]; !ok {
			t.Errorf("monsterDefByID missing %q", id)
		}
	}
}

// TestWolfCarriesTodaysExactNumbers pins the load-bearing compatibility
// contract: wolf's stats match the pre-6c flat constants exactly (10 HP, 3
// damage, 20 XP, aggro 10, 30% drop chance, the exact pre-6c starter drop
// table in its original order/weights) — see drops_test.go's
// killDropSeed/killMissSeed, which depend on this order.
func TestWolfCarriesTodaysExactNumbers(t *testing.T) {
	t.Parallel()

	wolf, ok := monsterDefByID[idKindWolf]
	if !ok {
		t.Fatal("wolf not registered")
	}

	if got, want := wolf.maxHP, 10; got != want {
		t.Errorf("wolf.maxHP = %d, want %d", got, want)
	}

	if got, want := wolf.weaponDef.damage, 3; got != want {
		t.Errorf("wolf weapon damage = %d, want %d", got, want)
	}

	if got, want := wolf.weapon, idFangs; got != want {
		t.Errorf("wolf weapon = %q, want %q", got, want)
	}

	if got, want := wolf.xp, 20; got != want {
		t.Errorf("wolf.xp = %d, want %d", got, want)
	}

	if got, want := wolf.aggroRadius, protocol.MonsterAggroRadius; got != want {
		t.Errorf("wolf.aggroRadius = %d, want %d", got, want)
	}

	if got, want := wolf.dropChance, 30; got != want {
		t.Errorf("wolf.dropChance = %d, want %d", got, want)
	}

	wantDrops := []drop{
		{defID: idButchersCleaver, weight: 4},
		{defID: idIronWarhammer, weight: 1},
		{defID: idVenomFang, weight: 4},
		{defID: idPackBow, weight: 4},
		{defID: idEmberStaff, weight: 4},
		{defID: idAncientDwarvenMattock, weight: 4},
		{defID: idWarMageStaff, weight: 4},
		// Appended by the inventory-slots milestone (task 3): the low-weight
		// healing potion — recovery layer 2. Appended LAST so the pre-existing
		// entries keep their cumulative-weight positions.
		{defID: idHealingPotion, weight: 2},
		// Appended by the fast-lane batch (task 6, #69 Q5): the Duelist's
		// Saber, wolf's crit% signature drop. Appended LAST for the same
		// reason — every earlier entry keeps its cumulative-weight position.
		{defID: idDuelistsSaber, weight: 4},
		// Appended by shields v1 (#90): the Wooden Buckler, the wolf table
		// being its common source. Appended LAST for the same reason.
		{defID: idWoodenBuckler, weight: 4},
		// Appended by noticeability gear (#88): the Padded Boots, common on
		// the wolf so the reach-shrinking option is reachable early. Appended
		// LAST for the same reason.
		{defID: idPaddedBoots, weight: 4},
		// Appended by the damage-type wave (#92): the Warded Gambeson, the
		// sharp resist, on the sharp-clawed kind a player meets first.
		// Appended LAST for the same reason.
		{defID: idWardedGambeson, weight: 3},
	}

	if got, want := len(wolf.drops), len(wantDrops); got != want {
		t.Fatalf("len(wolf.drops) = %d, want %d", got, want)
	}

	for i, d := range wantDrops {
		if got, want := wolf.drops[i], d; got != want {
			t.Errorf("wolf.drops[%d] = %+v, want %+v", i, got, want)
		}
	}
}

// TestKindOfPlayerIsNil: kindOf never resolves a def for a player entity,
// regardless of what its (always-empty in production) monsterKind holds.
func TestKindOfPlayerIsNil(t *testing.T) {
	t.Parallel()

	p := &entity{kind: protocol.EntityPlayer}
	if got := kindOf(p); got != nil {
		t.Errorf("kindOf(player) = %v, want nil", got)
	}
}

// TestKindOfMonsterResolvesRegisteredKind: a monster entity with a
// registered monsterKind resolves to that exact def.
func TestKindOfMonsterResolvesRegisteredKind(t *testing.T) {
	t.Parallel()

	m := &entity{kind: protocol.EntityMonster, monsterKind: idKindTroll}

	got := kindOf(m)
	if got == nil || got.id != idKindTroll {
		t.Errorf("kindOf(troll entity) = %v, want the troll def", got)
	}
}

// TestKindOfMonsterUnknownKindIsNil: a monster entity naming an unregistered
// kind (a malformed fixture — never produced by a real spawn path) fails
// closed to nil rather than a random registry entry.
func TestKindOfMonsterUnknownKindIsNil(t *testing.T) {
	t.Parallel()

	m := &entity{kind: protocol.EntityMonster, monsterKind: "griffin"}
	if got := kindOf(m); got != nil {
		t.Errorf("kindOf(unregistered kind) = %v, want nil", got)
	}
}

// TestValidateMonsterDefsPanicsOnDuplicateID.
func TestValidateMonsterDefsPanicsOnDuplicateID(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("validateMonsterDefs did not panic on a duplicate id")
		}
	}()

	validateMonsterDefs([]*monsterDef{
		{id: "sameid", weapon: idFangs, rings: []int{0}},
		{id: "sameid", weapon: idFangs, rings: []int{1, 2}},
	})
}

// TestValidateMonsterDefsPanicsOnMissingDamageType (#92): a kind's claws are
// a weapon like any other — an untyped one would dodge every resistance and
// vulnerability card ever written.
func TestValidateMonsterDefsPanicsOnMissingDamageType(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("validateMonsterDefs did not panic on a kind with no damage type")
		}
	}()

	validateMonsterDefs([]*monsterDef{
		{id: "x", rings: []int{0, 1, 2}},
	})
}

// TestValidateMonsterDefsPanicsOnUnknownDropItem.
func TestValidateMonsterDefsPanicsOnUnknownDropItem(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("validateMonsterDefs did not panic on a drop referencing an unknown item")
		}
	}()

	validateMonsterDefs([]*monsterDef{
		{id: "x",
			weapon: idFangs,
			rings:  []int{0, 1, 2}, drops: []drop{{defID: "no-such-item", weight: 1}}},
	})
}

// TestValidateMonsterDefsPanicsOnRingWithNoKind: every ring in
// [0,protocol.RingCount) must be covered by at least one kind.
func TestValidateMonsterDefsPanicsOnRingWithNoKind(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("validateMonsterDefs did not panic on an uncovered ring")
		}
	}()

	validateMonsterDefs([]*monsterDef{
		{id: "x", weapon: idFangs, rings: []int{0}},
	})
}

// TestValidateMonsterDefsPanicsOnInvalidRingIndex.
func TestValidateMonsterDefsPanicsOnInvalidRingIndex(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("validateMonsterDefs did not panic on an out-of-range ring index")
		}
	}()

	validateMonsterDefs([]*monsterDef{
		{id: "x", weapon: idFangs, rings: []int{protocol.RingCount}},
	})
}

// TestValidateMonsterDefsPanicsOnAggroRadiusInForbiddenRange: a non-zero
// aggroRadius between 1 and CombatRadius (inclusive) violates the same
// invariant protocol.MonsterAggroRadius itself carries.
func TestValidateMonsterDefsPanicsOnAggroRadiusInForbiddenRange(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("validateMonsterDefs did not panic on an aggroRadius inside (0, CombatRadius]")
		}
	}()

	validateMonsterDefs([]*monsterDef{
		{id: "x", weapon: idFangs, rings: []int{0, 1, 2}, aggroRadius: protocol.CombatRadius - 1},
	})
}

// TestValidateMonsterDefsAcceptsZeroAggroRadius: 0 means "use the default"
// and must not panic.
func TestValidateMonsterDefsAcceptsZeroAggroRadius(t *testing.T) {
	t.Parallel()

	validateMonsterDefs([]*monsterDef{
		{id: "x", weapon: idFangs, rings: []int{0, 1, 2}, aggroRadius: 0},
	})
}

// TestValidateMonsterDefsPanicsOnLeashInsideAggro (#102): a non-zero
// leashRadius at or inside the kind's base aggro radius would drop every
// chase the moment it starts — a content bug, caught at init.
func TestValidateMonsterDefsPanicsOnLeashInsideAggro(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("validateMonsterDefs did not panic on a leashRadius <= the aggro radius")
		}
	}()

	validateMonsterDefs([]*monsterDef{
		{id: "x",
			weapon: idFangs,
			rings:  []int{0, 1, 2}, aggroRadius: protocol.CombatRadius + 2, leashRadius: protocol.CombatRadius + 2},
	})
}

// TestValidateMonsterDefsPanicsOnLeashInsideDefaultAggro (#102): with no
// per-kind aggroRadius the leash override is checked against the shared
// protocol.MonsterAggroRadius default.
func TestValidateMonsterDefsPanicsOnLeashInsideDefaultAggro(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("validateMonsterDefs did not panic on a leashRadius <= MonsterAggroRadius with default aggro")
		}
	}()

	validateMonsterDefs([]*monsterDef{
		{id: "x", weapon: idFangs, rings: []int{0, 1, 2}, leashRadius: protocol.MonsterAggroRadius},
	})
}

// TestValidateMonsterDefsAcceptsZeroAndValidLeashRadius (#102): 0 means "use
// the default multiplier" and a value beyond the aggro radius is a real
// override; neither may panic.
func TestValidateMonsterDefsAcceptsZeroAndValidLeashRadius(t *testing.T) {
	t.Parallel()

	validateMonsterDefs([]*monsterDef{
		{id: "x", weapon: idFangs, rings: []int{0, 1, 2}, leashRadius: 0},
		{id: "y",
			weapon: idFangs,
			rings:  []int{0, 1, 2}, aggroRadius: protocol.CombatRadius + 2, leashRadius: protocol.CombatRadius + 3},
	})
}

// TestValidateMonsterDefsPanicsOnUnknownRuleEvent: the kind rules seam
// reuses items.go's validateRuleCards, so an unknown event must still fail
// at load.
func TestValidateMonsterDefsPanicsOnUnknownRuleEvent(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("validateMonsterDefs did not panic on a rule card with an unknown event")
		}
	}()

	validateMonsterDefs([]*monsterDef{
		{
			id: "x", rings: []int{0, 1, 2},
			rules: []ruleCard{{event: "on-roar", then: effect{kind: effAdd, n: 1}}},
		},
	})
}

// TestRealMonsterRegistryValidatesCleanly re-runs the exact validation
// init() already performed against the live registry — belt-and-suspenders,
// mirroring TestRealRegistryValidatesCleanly (items_test.go).
func TestRealMonsterRegistryValidatesCleanly(t *testing.T) {
	t.Parallel()

	validateMonsterDefs(monsterDefs)
}

// TestEveryRingHasAKind: every ring 0..RingCount-1 has at least one kind
// that spawns in it, over the REAL registry — the spec's "legibility"
// requirement (every ring must have something to fight), not just a
// synthetic validation-panic test.
func TestEveryRingHasAKind(t *testing.T) {
	t.Parallel()

	covered := make(map[int]bool, protocol.RingCount)

	for _, def := range monsterDefs {
		for _, r := range def.rings {
			covered[r] = true
		}
	}

	for r := range protocol.RingCount {
		if !covered[r] {
			t.Errorf("ring %d has no monster kind", r)
		}
	}
}
