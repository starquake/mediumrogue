package game

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
	// desc is the authored mechanical line ("+10% damage with melee
	// weapons"); flavor is the optional lore line. Same split as itemDef.
	// flavor has no reader until the panel renders it (task 9) and no writer
	// until content lands (task 5) — declared here so the shape is settled
	// before either is written.
	desc   string
	flavor string //nolint:unused // authored in task 5, rendered in task 9; see the comment above.
}

// skillDefs is the registry. EMPTY until task 5 — this task ships the
// machinery and its load-time panics, so a content bug fails at process
// start rather than mid-fight.
//
//nolint:gochecknoglobals // fixed content registry, effectively const; validated at init.
var skillDefs []*skillDef

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
