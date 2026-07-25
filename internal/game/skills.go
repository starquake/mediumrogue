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

	// Actives (#161). Blink is the first — the category exists so the second
	// one is content rather than another special case.
	skillBlink = "blink"

	// Actives 2 (#300). Both are self-casts over rows the potions already
	// use, which is the proof the descriptor was worth having: neither
	// needed a line of resolution code of its own.
	skillSecondWind = "second-wind"
	skillBulwark    = "bulwark"
	skillExpose     = "expose"
)

// activeDef is a skill's triggerable half (#161). A skill is passive (rules,
// no active) or active (an active, no rules) — never both; validateSkillDefs
// rejects the mix.
//
// Cooldowns count TURNS, whichever clock is ticking. Bubble turns run slower
// in wall-clock than world turns, and that dilation is the bubble's point, so
// a turn-denominated cooldown rides it instead of fighting it (#161).
type activeDef struct {
	// kind names WHAT this active does. Without it every new active would be
	// another hardcoded branch at resolution, and the category would become a
	// switch statement pretending to be a system (#300). Each kind routes to
	// machinery that already exists for consumables and throwables, so a new
	// active is content rather than engine work.
	kind string
	// cooldownTurns is how many turns must pass before it can fire again.
	// Must be > 0: a zero cooldown is a skill with no cost.
	cooldownTurns int
	// rangeHex is the furthest target hex, capped at protocol.CombatRadius so
	// an active cannot leave a bubble from anywhere inside it by accident.
	// ZERO for a self-cast kind, which has no target to range-check.
	rangeHex int
	// effect is the timed effect this active applies, for the kinds that apply
	// one (activeAppliesEffect); nil otherwise. It is the SAME appliedEffect a
	// potion carries, handed to the same applyTimedEffectLocked — a skill and a
	// drink are two triggers on one mechanism, not two mechanisms.
	effect *appliedEffect
}

// The active kinds. Each names an existing resolution path rather than a new
// mechanism — see #300's wildfire-gate note: these pass only BECAUSE the
// behaviour already exists for potions and thrown flasks.
const (
	// activeReposition moves the caster to the target hex (Blink, #161).
	activeReposition = "reposition"
	// activeSelfEffect applies the def's timed effect to the CASTER (#300) —
	// the same call a potion makes, with a cooldown instead of an inventory
	// slot as its cost.
	activeSelfEffect = "self-effect"
	// activeTargetEffect applies the def's timed effect to a named hostile
	// entity (#300) — a debuff, aimed the way a ranged attack is aimed.
	activeTargetEffect = "target-effect"
)

// activeAim names what a kind is POINTED at. The three modes need three
// different submit-time validations and three different client flows, and a
// bool cannot say which — a self-cast and an entity-targeted debuff both fail
// "needs a hex" for entirely different reasons.
type activeAim int

const (
	// aimSelf is a self-cast: no target at all. Validating it against a hex it
	// never supplies would reject every cast.
	aimSelf activeAim = iota
	// aimHex is pointed at a destination hex (Blink) — range, walkability and
	// line of sight.
	aimHex
	// aimEntity is pointed at another entity (Expose) — range, hostility and
	// line of sight, exactly as a ranged attack is.
	aimEntity
)

// aimFor reports a kind's targeting mode. Kept as one function so submit-time
// validation, resolution and the wire can never disagree about it.
func aimFor(kind string) activeAim {
	switch kind {
	case activeReposition:
		return aimHex
	case activeTargetEffect:
		return aimEntity
	default:
		return aimSelf
	}
}

// activeAppliesEffect reports whether a kind uses activeDef.effect. Kinds that
// do not must leave it nil — a def carrying a payload nothing reads is a
// content bug that would otherwise look like a working skill.
func activeAppliesEffect(kind string) bool {
	switch kind {
	case activeSelfEffect, activeTargetEffect:
		return true
	default:
		return false
	}
}

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
	// Empty for an active, whose behaviour is its trigger rather than a fold.
	rules []ruleCard
	// active is non-nil for a triggerable skill (#161). Mutually exclusive
	// with rules.
	active *activeDef
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
		// Expose (#300): the first entity-aimed active, and Ward's mirror —
		// a take-damage effMulPct ABOVE percentBase instead of below. Marked
		// harmful on the row, which is the whole of its counterplay: a monster
		// that could cleanse would shrug it off, and nothing here says so.
		//
		// A party skill by shape: the debuff helps whoever hits the target
		// next, which is usually not the caster.
		id: skillExpose, name: "Expose", tree: treeClass,
		prereqs: []string{skillWeakSpot},
		flavor:  "You point. Everyone else understands.",
		active: &activeDef{
			kind: activeTargetEffect, cooldownTurns: 5, rangeHex: 4,
			effect: &appliedEffect{effectID: idEffectVulnerable, magnitude: percentBase + 20, turns: 3},
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
		// The first active (#161). Survival tree, 1 point, 3 hexes, 3-turn
		// cooldown — maintainer's defaults.
		//
		// Destination needs line of sight as well as range: it does NOT pass
		// through walls, deliberately unlike the classic ARPG blink, so cover
		// stays real.
		id: skillBlink, name: "Blink", tree: treeSurvival,
		prereqs: []string{skillSurvivalist},
		flavor:  "Here, then not.",
		active:  &activeDef{kind: activeReposition, cooldownTurns: 3, rangeHex: 3},
	},

	{
		// Second Wind (#300): the Healing Draught's regen row, triggered by a
		// cooldown instead of by spending an item. Deliberately the SAME
		// numbers as the potion — a skill that outheals the consumable would
		// make the consumable dead content, and the point of the slice is that
		// actives cost no new vocabulary.
		//
		// Longer cooldown than Blink: a heal that returns every third turn is
		// attrition removed rather than managed.
		id: skillSecondWind, name: "Second Wind", tree: treeSurvival,
		prereqs: []string{skillSurvivalist},
		flavor:  "You find the breath you had put down somewhere.",
		active: &activeDef{
			kind: activeSelfEffect, cooldownTurns: 6,
			effect: &appliedEffect{effectID: idEffectRegen, magnitude: 3, turns: 3},
		},
	},
	{
		// Bulwark (#300): the Warding Tonic's row. Sits behind Hardy rather
		// than beside Blink so the Survival tree has actual depth — this is
		// the tree's strongest defensive button and should cost the walk.
		id: skillBulwark, name: "Bulwark", tree: treeSurvival,
		prereqs: []string{skillHardy},
		flavor:  "Set, and stay set.",
		active: &activeDef{
			kind: activeSelfEffect, cooldownTurns: 6,
			effect: &appliedEffect{effectID: idEffectWard, magnitude: percentBase - 25, turns: 4},
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
		validateSkillActive(def)
	}

	// Prereq checks run in a second pass so a skill may name one declared
	// later in the table — authoring order is not a constraint.
	for _, def := range defs {
		validateSkillPrereqs(def)
	}

	validateNoSkillPrereqCycle(defs)
}

// validateSkillActive panics if an active skill is malformed (#161): carrying
// rule cards as well as a trigger, a cooldown that never costs anything, or a
// range that could clear a combat bubble from anywhere inside it.
func validateSkillActive(def *skillDef) {
	if def.active == nil {
		return
	}

	if len(def.rules) > 0 {
		panic("game: active skill " + def.id + " also carries rule cards")
	}

	if def.active.cooldownTurns <= 0 {
		panic("game: active skill " + def.id + " has no cooldown")
	}

	if !activeKnownKind(def.active.kind) {
		panic("game: active skill " + def.id + " has unknown kind " + def.active.kind)
	}

	validateActiveRange(def)
	validateActiveEffect(def)
}

// validateActiveRange panics if an active's range disagrees with its aim: a
// kind that points at something needs a usable one, a self-cast must NOT carry
// one, or the def is lying about what it does and the next reader will believe
// it.
func validateActiveRange(def *skillDef) {
	if aimFor(def.active.kind) == aimSelf {
		if def.active.rangeHex != 0 {
			panic("game: self-cast active skill " + def.id + " must not carry a range")
		}

		return
	}

	if def.active.rangeHex <= 0 || def.active.rangeHex > protocol.CombatRadius {
		panic("game: active skill " + def.id + " range is outside 1..CombatRadius")
	}
}

// validateActiveEffect panics if an active's timed-effect payload disagrees
// with its kind, or names instance values applyTimedEffectLocked cannot use.
// The checks mirror validateItemAppliesEffect deliberately: the same
// appliedEffect reaching the same applier deserves the same guards whatever
// pulled the trigger.
func validateActiveEffect(def *skillDef) {
	ae := def.active.effect

	if !activeAppliesEffect(def.active.kind) {
		if ae != nil {
			panic("game: active skill " + def.id + " carries an effect its kind " +
				def.active.kind + " never applies")
		}

		return
	}

	if ae == nil {
		panic("game: active skill " + def.id + " kind " + def.active.kind + " needs an effect")
	}

	if _, ok := effectDefByID[ae.effectID]; !ok {
		panic("game: active skill " + def.id + " names unknown effect " + ae.effectID)
	}

	if ae.turns <= 0 {
		panic("game: active skill " + def.id + " effect " + ae.effectID + " must have turns > 0")
	}

	// toSelf is an on-hit router (attacker vs victim); an active's KIND already
	// says who is affected, so a set flag means the author expected it to be
	// read and it never will be.
	if ae.toSelf {
		panic("game: active skill " + def.id + " must not set toSelf (the kind decides who is affected)")
	}
}

// activeKnownKind reports whether kind is one this build can resolve. Content
// naming an unknown kind panics at init like every other content error, rather
// than failing silently mid-combat.
func activeKnownKind(kind string) bool {
	switch kind {
	case activeReposition, activeSelfEffect, activeTargetEffect:
		return true
	default:
		return false
	}
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

// useSkillLocked validates an active-skill trigger and queues it as this
// turn's action (#161). Callers hold w.mu.
//
// The destination needs range, walkability AND line of sight: an active does
// not pass through walls, so cover stays real.
func (w *World) useSkillLocked(e *entity, id string, target protocol.Hex, targetEntityID int64) error {
	def, ok := skillDefByID[id]
	if !ok {
		return ErrNoSuchSkill
	}

	if def.active == nil {
		return ErrSkillNotActive
	}

	if !slices.Contains(e.learned, id) {
		return ErrSkillNotLearned
	}

	if w.turn < e.activeReadyTurn[id] {
		return ErrSkillOnCooldown
	}

	switch aimFor(def.active.kind) {
	case aimSelf:
		// Nothing to validate — checking range, walkability and line of sight
		// against a hex it never supplies would refuse every cast.
		return w.commitActiveLocked(e, id, nil, 0)
	case aimEntity:
		return w.useEntityAimedSkillLocked(e, def, targetEntityID)
	case aimHex:
	}

	if HexDistance(e.hex, target) > def.active.rangeHex {
		return ErrOutOfRange
	}

	if !w.walkableLocked(target) {
		return ErrNotWalkable
	}

	if w.sightBlockedLocked(e.hex, target, def.active.rangeHex) {
		return ErrNoLineOfSight
	}

	// #196: a blink onto an opposing-held or StackCap-full hex is refused here
	// for the same reason an ordinary mover is turned away — occupancy is not
	// negotiable. resolveActivesLocked re-checks against the evolving board for
	// the rare hex that fills between this window and resolution.
	if w.occupiedForLocked(e, target) {
		return ErrHexOccupied
	}

	return w.commitActiveLocked(e, id, &target, 0)
}

// useEntityAimedSkillLocked validates an entity-aimed active (#300, Expose).
//
// The gates are queueAttackLocked's entity branch, deliberately: a debuff you
// can land is a debuff you could have shot, so the same hostility, reach and
// line-of-sight rules apply and terrain stays real cover. Callers hold w.mu.
func (w *World) useEntityAimedSkillLocked(e *entity, def *skillDef, targetEntityID int64) error {
	victim, ok := w.entities[targetEntityID]
	if !ok || victim.hp <= 0 {
		return ErrAttackTargetNotFound
	}

	// Hostile-only. A debuff is a weapon; landing one on an ally is not a
	// mis-click to be forgiven, it is an action that should not exist.
	if !opposing(e, victim) {
		return ErrAttackTargetNotHostile
	}

	if HexDistance(e.hex, victim.hex) > def.active.rangeHex {
		return ErrOutOfRange
	}

	if !w.seesLocked(e.hex, victim.hex) {
		return ErrNoLineOfSight
	}

	return w.commitActiveLocked(e, def.id, nil, targetEntityID)
}

// commitActiveLocked queues an active as this turn's action.
//
// It displaces any queued move, attack or throw — the latest intent in the
// window wins, exactly as elsewhere. Exactly one of `target`/`targetEntityID`
// is set, or neither for a self-cast — the one place an active legitimately has
// nowhere to point. Callers hold w.mu.
func (w *World) commitActiveLocked(e *entity, id string, target *protocol.Hex, targetEntityID int64) error {
	e.path = nil
	e.attackTarget = nil
	e.attackTargetEntity = 0
	e.throwItem, e.throwTarget, e.recallItem = 0, nil, 0 // #271
	e.activeSkill = id
	e.activeTarget = target
	e.activeTargetEntity = targetEntityID

	return nil
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

	if e.skillPoints < protocol.SkillPointCost {
		return ErrNoSkillPoints
	}

	e.skillPoints -= protocol.SkillPointCost
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
func skillViewsLocked(e *entity, turn int64) []protocol.SkillView {
	// Empty, never nil: a nil slice marshals to JSON null and the generated
	// client type says it is an array. See wire_nil_test.go.
	if e.kind != protocol.EntityPlayer {
		return []protocol.SkillView{}
	}

	views := make([]protocol.SkillView, 0, len(skillDefs))

	for _, def := range skillDefs {
		learned := slices.Contains(e.learned, def.id)
		if !learned && !learnableFor(e, def) {
			continue
		}

		view := protocol.SkillView{
			ID: def.id, Name: def.name, Tree: def.tree,
			Stats: statViewsFor(&itemDef{rules: def.rules}), Flavor: def.flavor, Learned: learned,
		}

		if def.active != nil {
			view.Active = true
			view.CooldownTurns = def.active.cooldownTurns
			view.RangeHex = def.active.rangeHex

			if ready := e.activeReadyTurn[def.id]; ready > turn {
				view.TurnsUntilReady = int(ready - turn)
			}
		}

		views = append(views, view)
	}

	return views
}
