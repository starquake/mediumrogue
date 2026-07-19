package game //nolint:testpackage // white-box: exercises unexported registry validation; see rules_test.go's file doc.

import (
	"errors"
	"strings"
	"testing"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// newWorldForSkillTest builds a bare world for the learn-intent tests, which
// need a *World receiver but none of its clock or map machinery.
func newWorldForSkillTest(t *testing.T) *World {
	t.Helper()

	return &World{}
}

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
		{id: "combat-training", name: "Combat Training", tree: treeClass, rules: []ruleCard{
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

// TestGrantSkillPointsIsIdempotentAcrossLevels (#124): the point bank is paid
// per level CROSSED, using a persisted high-water mark rather than a level-up
// event — because the engine has none (level is derived from xp via levelFor).
// The three cases below are the ones that would silently double-pay without
// it: calling twice on the same xp, a multi-level jump, and re-earning xp
// lost to death (levelFloorXP).
//
//nolint:paralleltest // mutates entity state shared with the registry-swapping tests above.
func TestGrantSkillPointsIsIdempotentAcrossLevels(t *testing.T) {
	e := &entity{kind: protocol.EntityPlayer, species: protocol.SpeciesElf}

	// Level 1 at zero xp: the starting level is not "crossed", so it pays.
	grantSkillPointsLocked(e)

	afterFirst := e.skillPoints

	// Calling again on identical xp must pay nothing.
	grantSkillPointsLocked(e)

	if got, want := e.skillPoints, afterFirst; got != want {
		t.Errorf("second grant on unchanged xp = %d points, want %d (idempotent)", got, want)
	}

	// A jump straight to level 4 pays for every level crossed, not just one.
	e.xp = xpFloorFor(4)
	grantSkillPointsLocked(e)

	if got, want := e.skillPoints, afterFirst+3*protocol.SkillPointsPerLevel; got != want {
		t.Errorf("after jumping to level 4 = %d points, want %d (3 levels × %d)",
			got, want, protocol.SkillPointsPerLevel)
	}

	banked := e.skillPoints

	// Death floors xp to the level's floor; re-earning it must grant nothing.
	e.xp = levelFloorXP(e.xp)
	grantSkillPointsLocked(e)

	e.xp = xpFloorFor(4)
	grantSkillPointsLocked(e)

	if got, want := e.skillPoints, banked; got != want {
		t.Errorf("after dying and re-earning the same level = %d points, want %d (no double pay)", got, want)
	}
}

// TestHumanEarnsTheBonusSkillPoint (#124/#123): the Human perk is a per-level
// BANK grant, not a rule card — a species check in grantSkillPointsLocked,
// because a bank grant is not a fold over a combat value.
//
//nolint:paralleltest // mutates entity state; see above.
func TestHumanEarnsTheBonusSkillPoint(t *testing.T) {
	grant := func(species string) int {
		e := &entity{kind: protocol.EntityPlayer, species: species}
		e.xp = xpFloorFor(3)
		grantSkillPointsLocked(e)

		return e.skillPoints
	}

	human, elf := grant(protocol.SpeciesHuman), grant(protocol.SpeciesElf)
	if got, want := human-elf, 3*protocol.HumanBonusSkillPoints; got != want {
		t.Errorf("human earned %d more points than an elf over 3 levels, want %d", got, want)
	}
}

// TestMonstersEarnNoSkillPoints (#124): the grant is player-only; a monster
// carrying xp must not bank anything.
//
//nolint:paralleltest // mutates entity state; see above.
func TestMonstersEarnNoSkillPoints(t *testing.T) {
	m := &entity{kind: protocol.EntityMonster, xp: 10_000}
	grantSkillPointsLocked(m)

	if got, want := m.skillPoints, 0; got != want {
		t.Errorf("monster banked %d skill points, want %d", got, want)
	}
}

// TestSkillCardsFoldInRegistryOrder (#124 task 4): the fold must not depend on
// the order a player learned things in. Two entities with the same skills
// learned in opposite orders produce identical card sequences — the property
// that lets learned/skillCards feed a seeded pipeline at all.
//
//nolint:paralleltest // swaps the package-global skill registry.
func TestSkillCardsFoldInRegistryOrder(t *testing.T) {
	const (
		alpha = "alpha"
		beta  = "beta"
	)

	first := ruleCard{event: evDealDamage, then: effect{kind: effAdd, n: 1}}
	second := ruleCard{event: evDealDamage, then: effect{kind: effAdd, n: 2}}

	withSkillRegistry(t, []*skillDef{
		{id: alpha, tree: treeClass, rules: []ruleCard{first}},
		{id: beta, tree: treeClass, rules: []ruleCard{second}},
	})

	forward := skillCards(&entity{learned: []string{alpha, beta}})
	reversed := skillCards(&entity{learned: []string{beta, alpha}})

	if got, want := len(forward), 2; got != want {
		t.Fatalf("skillCards returned %d cards, want %d", got, want)
	}

	// ruleCard holds a []condition, so it is not comparable with == ; these
	// fixtures differ only in their effect, which is.
	if forward[0].then != first.then || forward[1].then != second.then {
		t.Errorf("skillCards = %+v, want registry order (alpha then beta)", forward)
	}

	if forward[0].then != reversed[0].then || forward[1].then != reversed[1].then {
		t.Errorf("learn order changed the fold: %+v vs %+v", forward, reversed)
	}
}

// TestSkillCardsSkipsUnknownIDs (#124): a learned id with no registry entry —
// a snapshot written before a skill was removed — is skipped, not a panic.
//
//nolint:paralleltest // swaps the package-global skill registry.
func TestSkillCardsSkipsUnknownIDs(t *testing.T) {
	card := ruleCard{event: evDealDamage, then: effect{kind: effAdd, n: 5}}

	withSkillRegistry(t, []*skillDef{{id: "extant", tree: treeClass, rules: []ruleCard{card}}})

	got := skillCards(&entity{learned: []string{"deleted-last-year", "extant"}})
	if len(got) != 1 || got[0].then != card.then {
		t.Errorf("skillCards with an unknown id = %+v, want just the extant card", got)
	}
}

// TestSkillLessEntityFoldsExactlyAsBefore (#124 task 4) is the regression
// guard for every pinned seed in the repo: an entity with no skills must
// contribute NO cards, so the concat is byte-identical to the pre-skills
// fold. nil, not an empty slice — slices.Concat treats them the same, but a
// nil says "there was nothing here" rather than "here is nothing".
func TestSkillLessEntityFoldsExactlyAsBefore(t *testing.T) {
	t.Parallel()

	if got := skillCards(&entity{}); got != nil {
		t.Errorf("skillCards for an entity with no skills = %+v, want nil", got)
	}
}

// TestV1SkillContentFoldsThroughThePipeline (#124 task 5) measures each
// shipped skill through applyRules rather than asserting its card literal —
// a card that validated but folded to nothing would pass a shape test and
// fail here.
func TestV1SkillContentFoldsThroughThePipeline(t *testing.T) {
	t.Parallel()

	melee := &itemDef{id: "sword", itemType: protocol.ItemTypeWeapon, tags: []string{protocol.WeaponTagMelee}}
	bow := &itemDef{id: "bow", itemType: protocol.ItemTypeWeapon, tags: []string{protocol.WeaponTagRanged}}

	cardsOf := func(id string) []ruleCard {
		def, ok := skillDefByID[id]
		if !ok {
			t.Fatalf("skill %s is not registered", id)
		}

		return def.rules
	}

	t.Run("combat training scopes by weapon tag", func(t *testing.T) {
		t.Parallel()

		cards := cardsOf(skillCombatTraining)

		if got, want := applyRules(evDealDamage, 10, cards, ruleCtx{weapon: melee}), 11; got != want {
			t.Errorf("melee swing of 10 = %d, want %d", got, want)
		}

		if got, want := applyRules(evDealDamage, 10, cards, ruleCtx{weapon: bow}), 10; got != want {
			t.Errorf("bow shot of 10 = %d, want %d (melee-only)", got, want)
		}
	})

	t.Run("weak spot only against a full-health target", func(t *testing.T) {
		t.Parallel()

		cards := cardsOf(skillWeakSpot)
		full := &entity{hp: 10, maxHP: 10}
		hurt := &entity{hp: 4, maxHP: 10}

		if got, want := applyRules(evDealDamage, 5, cards, ruleCtx{victim: full}), 9; got != want {
			t.Errorf("hit on a full-health target = %d, want %d", got, want)
		}

		if got, want := applyRules(evDealDamage, 5, cards, ruleCtx{victim: hurt}), 5; got != want {
			t.Errorf("hit on a wounded target = %d, want %d (inert)", got, want)
		}
	})

	t.Run("scouting shrinks the notice radius", func(t *testing.T) {
		t.Parallel()

		cards := cardsOf(skillScouting)
		if got, want := applyRules(evAggroRange, 10, cards, ruleCtx{}), 8; got != want {
			t.Errorf("aggro radius 10 with scouting = %d, want %d", got, want)
		}
	})
}

// TestShieldWallNeedsBothTheShieldAndTheRoll (#124 task 5): the only v1 skill
// with two conditions, and the first that consumes rng. Both halves have to
// hold — a shield with an unlucky roll and a lucky roll without a shield must
// each leave the hit whole.
//
//nolint:paralleltest // drives a seeded rng whose consumption order matters.
func TestShieldWallNeedsBothTheShieldAndTheRoll(t *testing.T) {
	cards := skillDefByID[skillShieldWall].rules

	shielded := &entity{equipped: map[string]itemInstance{
		protocol.SlotOffHand: {id: 1, defID: idWoodenBuckler},
	}}
	bare := &entity{equipped: map[string]itemInstance{}}

	// A 15% card fires on a roll below 15; testRNG(seed) is deterministic, so
	// scan seeds until one rolls each way rather than pinning a magic seed.
	glanced, whole := false, false

	for seed := uint64(1); seed <= 40 && (!glanced || !whole); seed++ {
		got := applyRules(evTakeDamage, 10, cards, ruleCtx{victim: shielded, rng: testRNG(seed)})
		switch got {
		case 5:
			glanced = true
		case 10:
			whole = true
		default:
			t.Fatalf("shielded hit of 10 = %d, want 5 (glance) or 10 (no proc)", got)
		}
	}

	if !glanced || !whole {
		t.Fatalf("scan found glanced=%v whole=%v over 40 seeds — expected both", glanced, whole)
	}

	// No shield: the card can never fire, whatever the roll.
	for seed := uint64(1); seed <= 40; seed++ {
		if got, want := applyRules(evTakeDamage, 10, cards, ruleCtx{victim: bare, rng: testRNG(seed)}), 10; got != want {
			t.Fatalf("unshielded hit of 10 at seed %d = %d, want %d", seed, got, want)
		}
	}
}

// TestLearnSkillRejections (#124 task 6) covers all five 422 paths. Each is a
// well-formed request the world says no to — the handler maps every one to
// 422 rather than 500.
//
//nolint:paralleltest // mutates entity state.
func TestLearnSkillRejections(t *testing.T) {
	w := newWorldForSkillTest(t)

	tests := []struct {
		name    string
		setup   func(e *entity)
		skillID string
		want    error
	}{
		{
			name:    "unknown skill",
			setup:   func(e *entity) { e.skillPoints = 1 },
			skillID: "telekinesis",
			want:    ErrNoSuchSkill,
		},
		{
			name:    "already learned",
			setup:   func(e *entity) { e.skillPoints = 1; e.learned = []string{skillCombatTraining} },
			skillID: skillCombatTraining,
			want:    ErrSkillAlreadyLearned,
		},
		{
			name:    "prerequisite unmet",
			setup:   func(e *entity) { e.skillPoints = 1 },
			skillID: skillWeakSpot, // gated behind Combat Training
			want:    ErrSkillPrereqUnmet,
		},
		{
			name:    "empty bank",
			setup:   func(e *entity) { e.skillPoints = 0 },
			skillID: skillCombatTraining,
			want:    ErrNoSkillPoints,
		},
		{
			name:    "in combat",
			setup:   func(e *entity) { e.skillPoints = 1; e.bubbleID = 7 },
			skillID: skillCombatTraining,
			want:    ErrLearnInCombat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { //nolint:paralleltest // shares the world above.
			e := &entity{kind: protocol.EntityPlayer, equipped: map[string]itemInstance{}}
			tt.setup(e)

			if got, want := w.learnSkillLocked(e, tt.skillID), tt.want; !errors.Is(got, want) {
				t.Errorf("learnSkillLocked = %v, want %v", got, want)
			}
		})
	}
}

// TestLearnSkillSpendsAPointAndUnlocksTheNext (#124 task 6): the happy path,
// and the prerequisite opening up as a result — the whole point of a tree.
//
//nolint:paralleltest // mutates entity state.
func TestLearnSkillSpendsAPointAndUnlocksTheNext(t *testing.T) {
	w := newWorldForSkillTest(t)
	e := &entity{kind: protocol.EntityPlayer, skillPoints: 2, equipped: map[string]itemInstance{}}

	if err := w.learnSkillLocked(e, skillCombatTraining); err != nil {
		t.Fatalf("learn combat-training: %v", err)
	}

	if got, want := e.skillPoints, 1; got != want {
		t.Errorf("bank after learning = %d, want %d", got, want)
	}

	// Weak Spot was prereq-locked a moment ago; now it isn't.
	if err := w.learnSkillLocked(e, skillWeakSpot); err != nil {
		t.Fatalf("learn weak-spot after its prereq: %v", err)
	}

	if got, want := strings.Join(e.learned, ","), skillCombatTraining+","+skillWeakSpot; got != want {
		t.Errorf("learned = %q, want %q (sorted)", got, want)
	}
}

// TestSkillViewsAreNearSighted (#124 Q7): the wire carries learned skills and
// currently-learnable ones — never a locked skill. This is enforced
// server-side precisely so the client cannot leak the tree by accident.
//
//nolint:paralleltest // mutates entity state.
func TestSkillViewsAreNearSighted(t *testing.T) {
	fresh := &entity{kind: protocol.EntityPlayer}

	ids := func(views []protocol.SkillView) string {
		out := make([]string, 0, len(views))
		for _, v := range views {
			out = append(out, v.ID)
		}

		return strings.Join(out, ",")
	}

	got := ids(skillViewsLocked(fresh))
	if strings.Contains(got, skillWeakSpot) {
		t.Errorf("a fresh player's skill views = %q — must NOT include the prereq-locked %s", got, skillWeakSpot)
	}

	if !strings.Contains(got, skillCombatTraining) || !strings.Contains(got, skillScouting) {
		t.Errorf("a fresh player's skill views = %q, want the unlocked roots", got)
	}

	// Learning the root reveals exactly one more.
	trained := &entity{kind: protocol.EntityPlayer, learned: []string{skillCombatTraining}}
	if !strings.Contains(ids(skillViewsLocked(trained)), skillWeakSpot) {
		t.Errorf("after learning %s, views = %q — want %s revealed",
			skillCombatTraining, ids(skillViewsLocked(trained)), skillWeakSpot)
	}

	// Monsters have no skills on the wire — as an EMPTY list, never nil.
	// Re-derived: nil marshals to JSON null while the generated client type
	// says it is an array, which is the crash class wire_nil_test.go guards
	// (#167, and the waitingForIds crash on development 2026-07-19).
	if got := skillViewsLocked(&entity{kind: protocol.EntityMonster}); got == nil {
		t.Error("monster skill views = nil, want an empty slice — nil marshals to null and crashes the client")
	} else if len(got) != 0 {
		t.Errorf("monster skill views = %+v, want empty", got)
	}
}
