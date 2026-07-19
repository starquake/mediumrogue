package game

import (
	"slices"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// skills.go: the skill-system registry (#124). A skill is the same pure-data
// rule card every other content type uses — species passives, gear, monster
// kind passives — hung on a tree with prerequisites. Nothing here is a
// closure (the SQLite-serialization prerequisite), and nothing here edits a
// combat site: a skill's effect is entirely its cards.
//
// The three trees are the settled model (#61): Class is per-class, Adventure
// and Survival are shared. Principle 5 is load-enforced below — progression
// in one tree may NEVER gate progression in another, so a prerequisite that
// crosses trees is a content bug, not a design option.

// Tree ids. Kept as engine constants rather than protocol ones until the wire
// needs them (task 6): content and validation are the only readers today.
const (
	treeClass     = "class"
	treeAdventure = "adventure"
	treeSurvival  = "survival"
)

// Skill ids: named for the same reason item ids are (items.go) — the
// registry, the prerequisite links, and the pinning tests all reference
// them, so a typo is a compile error rather than a silent miss.
const (
	skillCombatTraining = "combat-training"
	skillWeakSpot       = "weak-spot"
	skillShieldWall     = "shield-wall"
	skillScouting       = "scouting"

	// Skills 2 (#57). Crusher/Kindler are damage-type lines (zero new
	// vocabulary); Survivalist/Hardy open the Survival tree, which shipped
	// empty in v1; Twin Fangs/Wand Chorus are condDualWielding's two riders.
	skillCrusher     = "crusher"
	skillKindler     = "kindler"
	skillSurvivalist = "survivalist"
	skillHardy       = "hardy"
	skillTwinFangs   = "twin-fangs"
	skillWandChorus  = "wand-chorus"
)

// skillDef is one entry in the skill registry: what it is, which tree it
// hangs on, what must be learned first, and the cards it contributes once
// learned. Mirrors itemDef's shape deliberately — the pipeline cannot tell
// the difference between a card from a sword and a card from a skill.
type skillDef struct {
	id, name string
	// tree is one of the tree* consts. A skill lives in exactly one tree
	// (#61 principle 9).
	tree string
	// prereqs are skill ids that must ALL be learned first. Same-tree only
	// (#61 principle 5, enforced in validateSkillDefs); empty means the skill
	// is available from the start of its tree. Never a level gate (#61
	// principle 12) — "be level 5" is deliberately not expressible.
	prereqs []string
	// rules are the cards this skill folds into the pipeline while learned.
	rules []ruleCard
	// flavor is the skill's authored lore line, and the only authored text on
	// a skillDef since #171 — its mechanical line is rendered from the cards
	// (statlines.go). Carries no numbers (validateFlavorHasNoStats).
	flavor string
}

// skillDefs is the skill registry — the v1 content batch (#124 task 5).
// Numbers are first-draft knobs; the shapes are what matter.
//
// Every card here uses vocabulary that already exists. Two conditions were
// added for this batch (weaponTagged, shieldEquipped, task 1); nothing else
// is new, which is the point — a skill is content, not machinery.
//
//nolint:gochecknoglobals,mnd // fixed content registry, effectively const; validated at init.
var skillDefs = []*skillDef{
	// --- Class tree -------------------------------------------------------
	{
		id: skillCombatTraining, name: "Combat Training", tree: treeClass,
		flavor: "Hours on the practice yard, and it finally shows.",
		rules: []ruleCard{
			// Scoped by weapon TAG rather than damage type (@starquake's
			// call, #124): a tag is how a weapon is USED, which is what
			// "with melee weapons" means — a blunt-scoped version would also
			// have caught a thrown mace.
			{event: evDealDamage, when: []condition{{kind: condWeaponTagged, s: protocol.WeaponTagMelee}},
				then: effect{kind: effMulPct, n: percentBase + 10}},
		},
	},
	{
		id: skillWeakSpot, name: "Weak Spot", tree: treeClass,
		prereqs: []string{skillCombatTraining},
		flavor:  "The first cut is the one you plan.",
		rules: []ruleCard{
			// Zero new vocabulary: condTargetHPFull has shipped since the
			// Venom Fang. The prereq on Combat Training is what proves
			// in-tree gating works end to end.
			{event: evDealDamage, when: []condition{{kind: condTargetHPFull}},
				then: effect{kind: effAdd, n: 4}},
		},
	},
	{
		id: skillShieldWall, name: "Shield Wall", tree: treeClass,
		flavor: "Set your feet. Let it come.",
		rules: []ruleCard{
			// A glance% bump, NOT flat mitigation (@starquake's call, #124;
			// and after #154 flat -N would be the only subtractive card left
			// besides the dwarf passive). This is the shipped Rogue-passive
			// shape: a chance-conditioned take-damage multiplier.
			//
			// DETERMINISM: this is the first v1 skill carrying a chance
			// condition, so it CONSUMES rng from the turn stream whenever a
			// shield-bearing entity is hit.
			{event: evTakeDamage, when: []condition{{kind: condChance, n: 15}, {kind: condShieldEquipped}},
				then: effect{kind: effMulPct, n: protocol.GlanceDamagePercent}},
		},
	},

	// --- Adventure tree ---------------------------------------------------
	{
		id: skillCrusher, name: "Crusher", tree: treeClass,
		flavor: "Nothing fancy. Just weight, arriving.",
		rules: []ruleCard{
			// Damage-type scoped, not weapon-category: the taxonomy has no
			// category axis (#57, design-decisions.md).
			//
			// Stacks with Combat Training on a blunt melee weapon: +20%, NOT
			// x1.21 — percentages sum within a fold.
			{event: evDealDamage, when: []condition{{kind: condDamageType, s: protocol.DamageTypeBlunt}},
				then: effect{kind: effMulPct, n: percentBase + 10}},
		},
	},
	{
		id: skillKindler, name: "Kindler", tree: treeClass,
		flavor: "It only ever needed an excuse.",
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condDamageType, s: protocol.DamageTypeFire}},
				then: effect{kind: effMulPct, n: percentBase + 10}},
		},
	},
	{
		id: skillTwinFangs, name: "Twin Fangs", tree: treeClass,
		flavor: "Two answers to every question.",
		rules: []ruleCard{
			// First of condDualWielding's two riders (#57).
			{event: evDealDamage, when: []condition{{kind: condDualWielding}},
				then: effect{kind: effMulPct, n: percentBase + 10}},
		},
	},
	{
		id: skillWandChorus, name: "Wand Chorus", tree: treeClass,
		prereqs: []string{skillTwinFangs},
		flavor:  "Each wand hears the other.",
		rules: []ruleCard{
			// Second rider. A bonus for dual-wielding, never a gate on it.
			{
				event: evDealDamage,
				when: []condition{
					{kind: condDualWielding},
					{kind: condDamageType, s: protocol.DamageTypeFire},
				},
				then: effect{kind: effMulPct, n: percentBase + 15},
			},
		},
	},

	// --- Survival tree ----------------------------------------------------
	// Defensive/attrition (settled #57; see design-decisions.md). Empty
	// before #57 — TestSurvivalTreeIsNotEmpty guards that.
	{
		id: skillSurvivalist, name: "Survivalist", tree: treeSurvival,
		flavor: "You have been colder, and hungrier, and here you are.",
		rules: []ruleCard{
			// A percentage, never flat -N (#154: subtractive mitigation
			// stacks into the >=1 clamp).
			{event: evTakeDamage, then: effect{kind: effMulPct, n: percentBase - 10}},
		},
	},
	{
		id: skillHardy, name: "Hardy", tree: treeSurvival,
		prereqs: []string{skillSurvivalist},
		flavor:  "The part of the fight where it counts.",
		rules: []ruleCard{
			// condTargetHPBelowPct reads the VICTIM, which in a take-damage
			// fold is the holder.
			{event: evTakeDamage, when: []condition{{kind: condTargetHPBelowPct, n: 40}},
				then: effect{kind: effMulPct, n: percentBase - 15}},
		},
	},

	{
		id: skillScouting, name: "Scouting", tree: treeAdventure,
		flavor: "You read the ground before you walk it.",
		rules: []ruleCard{
			// The second rider #88 promised for evAggroRange (Padded Boots
			// was the first). applyRules' >=1 clamp already guards the floor.
			{event: evAggroRange, then: effect{kind: effMulPct, n: percentBase - 20}},
		},
	},
}

// skillDefByID is the derived lookup, built once at init alongside the item
// and monster indexes.
//
//nolint:gochecknoglobals // derived lookup table, built once at init from skillDefs.
var skillDefByID map[string]*skillDef

// buildSkillIndex builds skillDefByID from skillDefs. Called from content.go's
// init before mustValidateContent, mirroring buildMonsterIndex.
func buildSkillIndex() {
	skillDefByID = make(map[string]*skillDef, len(skillDefs))
	for _, def := range skillDefs {
		skillDefByID[def.id] = def
	}
}

// validTree reports whether t is one of the three trees.
func validTree(t string) bool {
	switch t {
	case treeClass, treeAdventure, treeSurvival:
		return true
	default:
		return false
	}
}

// validateSkillDefs panics on any content bug the registry can express:
// duplicate id, unknown tree, a prerequisite naming a skill that doesn't
// exist, a CROSS-TREE prerequisite (#61 principle 5), a prerequisite cycle,
// or a rule card the pipeline doesn't know. Called from mustValidateContent,
// so every failure lands at process start.
func validateSkillDefs(defs []*skillDef) {
	seen := make(map[string]bool, len(defs))

	for _, def := range defs {
		if seen[def.id] {
			panic("game: duplicate skill id " + def.id)
		}

		seen[def.id] = true

		if !validTree(def.tree) {
			panic("game: skill " + def.id + " has unknown tree " + def.tree)
		}

		validateRuleCards("skill:"+def.id, def.rules)
		validateFlavorHasNoStats("skill "+def.id, def.flavor)
	}

	// Prereq checks run in a second pass so a skill may name one declared
	// later in the table — authoring order is not a constraint.
	for _, def := range defs {
		validateSkillPrereqs(def)
	}

	validateNoSkillPrereqCycle(defs)
}

// validateSkillPrereqs panics if def names a prerequisite that is unknown,
// itself, or in a different tree (#61 principle 5: one tree's progression may
// never block another's).
func validateSkillPrereqs(def *skillDef) {
	for _, id := range def.prereqs {
		if id == def.id {
			panic("game: skill " + def.id + " lists itself as a prerequisite")
		}

		req, ok := skillDefByID[id]
		if !ok {
			panic("game: skill " + def.id + " has unknown prerequisite " + id)
		}

		if req.tree != def.tree {
			panic("game: skill " + def.id + " (" + def.tree + ") has cross-tree prerequisite " +
				id + " (" + req.tree + ") — #61 principle 5")
		}
	}
}

// validateNoSkillPrereqCycle panics if the prerequisite graph contains a
// cycle, which would make every skill in it permanently unlearnable — a
// content bug that is invisible until a player tries.
func validateNoSkillPrereqCycle(defs []*skillDef) {
	const (
		unvisited = 0
		onStack   = 1
		done      = 2
	)

	state := make(map[string]int, len(defs))

	var visit func(id string)

	visit = func(id string) {
		switch state[id] {
		case onStack:
			panic("game: skill prerequisite cycle through " + id)
		case done:
			return
		}

		state[id] = onStack

		if def, ok := skillDefByID[id]; ok {
			for _, req := range def.prereqs {
				visit(req)
			}
		}

		state[id] = done
	}

	for _, def := range defs {
		visit(def.id)
	}
}

// skillCards returns the cards an entity's LEARNED skills contribute, in
// REGISTRY order — not learn order — so the fold is identical however the
// player got there. entity.learned is kept sorted for the same reason;
// between them, two players with the same skills always fold the same way.
//
// An id with no registry entry is skipped rather than panicking: a snapshot
// written before a skill was removed must not crash the world on load. The
// version check is the guard against real drift; this is just not making a
// bad day worse.
func skillCards(e *entity) []ruleCard {
	if len(e.learned) == 0 {
		return nil
	}

	var cards []ruleCard

	for _, def := range skillDefs {
		if slices.Contains(e.learned, def.id) {
			cards = append(cards, def.rules...)
		}
	}

	return cards
}

// learnableFor reports whether e can learn def right now: not already known,
// and every prerequisite learned. It does NOT check the point bank — the
// wire uses this to decide what to OFFER (near-sightedness), and an offer
// the player can't yet afford is still worth showing.
func learnableFor(e *entity, def *skillDef) bool {
	if slices.Contains(e.learned, def.id) {
		return false
	}

	for _, req := range def.prereqs {
		if !slices.Contains(e.learned, req) {
			return false
		}
	}

	return true
}

// learnSkillLocked spends one banked point on id. Free and immediate OUT of
// combat and rejected inside a bubble (#124 Decision 4): learning is a
// between-fights decision, so unlike equip/drop/drink it is never queued as a
// turn's action. Callers hold w.mu.
func (w *World) learnSkillLocked(e *entity, id string) error {
	if e.bubbleID != 0 {
		return ErrLearnInCombat
	}

	def, ok := skillDefByID[id]
	if !ok {
		return ErrNoSuchSkill
	}

	if slices.Contains(e.learned, id) {
		return ErrSkillAlreadyLearned
	}

	if !learnableFor(e, def) {
		return ErrSkillPrereqUnmet
	}

	if e.skillPoints < 1 {
		return ErrNoSkillPoints
	}

	e.skillPoints--
	// Insert in sorted position: skillCards folds in registry order, but
	// `learned` itself is kept sorted so two players who learned the same
	// skills in different orders are byte-identical on disk and on the wire.
	e.learned = append(e.learned, id)
	slices.Sort(e.learned)

	return nil
}

// skillViewsLocked renders the NEAR-SIGHTED skill list for e: everything
// learned, plus everything learnable right now — and nothing else. A locked
// skill never reaches the wire, so the client cannot leak the tree even by
// accident (#124 Q7, enforced server-side by design rather than by client
// discipline). Registry order, so the panel is stable between turns.
func skillViewsLocked(e *entity) []protocol.SkillView {
	if e.kind != protocol.EntityPlayer {
		return nil
	}

	views := make([]protocol.SkillView, 0, len(skillDefs))

	for _, def := range skillDefs {
		learned := slices.Contains(e.learned, def.id)
		if !learned && !learnableFor(e, def) {
			continue
		}

		views = append(views, protocol.SkillView{
			ID: def.id, Name: def.name, Tree: def.tree,
			Stats: statViewsFor(&itemDef{rules: def.rules}), Flavor: def.flavor, Learned: learned,
		})
	}

	return views
}
