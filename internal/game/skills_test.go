package game //nolint:testpackage // white-box: exercises unexported registry validation; see rules_test.go's file doc.

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// withSkillRegistry swaps in a synthetic registry (and its index) for one
// test, restoring the real one afterwards. The validators read skillDefByID
// for prerequisite lookups, so the index has to move with the table.
func withSkillRegistry(t *testing.T, defs []*skillDef) {
	t.Helper()

	oldDefs := skillDefs
	oldIndex := skillDefByID

	t.Cleanup(func() { skillDefs, skillDefByID = oldDefs, oldIndex })

	skillDefs = defs

	buildSkillIndex()
}

// TestValidateSkillDefsAcceptsAWellFormedTree: the happy path, including a
// same-tree prerequisite chain — the shape #124's v1 content will use.
//
//nolint:paralleltest // swaps the package-global skill registry; parallel siblings would race on it.
func TestValidateSkillDefsAcceptsAWellFormedTree(t *testing.T) {
	defs := []*skillDef{
		{id: "combat-training", name: "Combat Training", tree: treeClass, desc: "+10% melee damage", rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condWeaponTagged, s: protocol.WeaponTagMelee}},
				then: effect{kind: effMulPct, n: percentBase + 10}},
		}},
		{id: "weak-spot", name: "Weak Spot", tree: treeClass, prereqs: []string{"combat-training"}},
		{id: "scouting", name: "Scouting", tree: treeAdventure},
	}

	withSkillRegistry(t, defs)
	validateSkillDefs(defs) // must not panic
}

// TestValidateSkillDefsPanics covers every content bug the registry can
// express. Each case is a separate registry so one bad def can't mask
// another.
//
//nolint:paralleltest // swaps the package-global skill registry; parallel siblings would race on it.
func TestValidateSkillDefsPanics(t *testing.T) {
	tests := []struct {
		name string
		defs []*skillDef
	}{
		{
			name: "duplicate id",
			defs: []*skillDef{
				{id: "same", tree: treeClass},
				{id: "same", tree: treeAdventure},
			},
		},
		{
			name: "unknown tree",
			defs: []*skillDef{{id: "x", tree: "sorcery"}},
		},
		{
			name: "dangling prerequisite",
			defs: []*skillDef{{id: "x", tree: treeClass, prereqs: []string{"no-such-skill"}}},
		},
		{
			// #61 principle 5: one tree's progression may never gate another's.
			name: "cross-tree prerequisite",
			defs: []*skillDef{
				{id: "forager", tree: treeSurvival},
				{id: "wayfarer", tree: treeAdventure, prereqs: []string{"forager"}},
			},
		},
		{
			name: "self prerequisite",
			defs: []*skillDef{{id: "x", tree: treeClass, prereqs: []string{"x"}}},
		},
		{
			name: "prerequisite cycle",
			defs: []*skillDef{
				{id: "a", tree: treeClass, prereqs: []string{"b"}},
				{id: "b", tree: treeClass, prereqs: []string{"a"}},
			},
		},
		{
			name: "unknown card kind",
			defs: []*skillDef{{id: "x", tree: treeClass, rules: []ruleCard{
				{event: "on-sneeze", then: effect{kind: effAdd, n: 1}},
			}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { //nolint:paralleltest // same global-registry swap as the parent.
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("validateSkillDefs did not panic on %s", tt.name)
				}
			}()

			withSkillRegistry(t, tt.defs)
			validateSkillDefs(tt.defs)
		})
	}
}

// TestSkillPrereqsMayNameALaterEntry: authoring order is not a constraint —
// the prerequisite pass runs after every id is indexed.
//
//nolint:paralleltest // swaps the package-global skill registry; parallel siblings would race on it.
func TestSkillPrereqsMayNameALaterEntry(t *testing.T) {
	defs := []*skillDef{
		{id: "second", tree: treeClass, prereqs: []string{"first"}},
		{id: "first", tree: treeClass},
	}

	withSkillRegistry(t, defs)
	validateSkillDefs(defs) // must not panic
}

// TestShippedSkillRegistryValidates: whatever the real table holds, it passes
// its own validator — the guard that keeps task 5's content honest. Empty
// today (this task ships machinery only), which is itself worth asserting so
// the emptiness is deliberate rather than accidental.
//
//nolint:paralleltest // reads the package-global registry the swapping tests above mutate.
func TestShippedSkillRegistryValidates(t *testing.T) {
	validateSkillDefs(skillDefs)

	if got, want := len(skillDefs), 0; got != want {
		t.Logf("skill registry now holds %d defs (was empty at task 2) — expected once task 5 lands", got)
	}
}
