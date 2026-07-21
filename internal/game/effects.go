package game

import (
	"cmp"
	"slices"
)

// effects.go: the timed / lingering effect foundation (#271, slice 1 — the
// keystone the buff-potion / DoT-poison / regen / summoner mechanics stack on
// later).
//
// An entity carries a list of ACTIVE TIMED EFFECTS — pure data,
// {defID, magnitude, turnsRemaining}, never a Go closure (the same SQLite-
// serialization prerequisite a ruleCard has). The modifier pipeline FOLDS an
// active effect at its event (a damage buff at deal-damage, a DoT/regen at the
// new end-of-turn event), and an end-of-turn tick (tickEffectsLocked, driven
// from resolveCombatLocked) advances every effect's counter, applies the
// per-turn ones, and expires those that reach zero.
//
// The design rule, stated once: a timed effect is "a rule card that is active
// for N turns", NOT a special case bolted onto a combat site. An effect
// contributes its card to the fold ONLY while it is in the entity's list, so
// the pipeline needs no "while effect active" condition — presence in the list
// IS the condition. That is why this slice adds exactly ONE new pipeline kind
// (the evEndOfTurn event, rules.go), reusing effAdd/effMulPct for the folds.
//
// ARPG, not TTRPG (docs/game-identity.md): an effect is a percentage/int
// modifier plus a turn counter, folded by the pipeline — never a status with a
// save, never a coupled roll. Duration is a plain turn count; there is no
// saving throw to shorten it. See design-decisions.md for the framing.

// timedEffect is one active effect on an entity: pure data, serialized in the
// snapshot (snapshot.go's timedEffectDTO). defID names an effectDef in the
// registry (effectDefByID, content.go). magnitude is the value that fills the
// synthesized card's effect n — a SIGNED effAdd amount (negative = a DoT
// draining HP each turn, positive = a regen restoring it) or an effMulPct
// percent (percentBase+N = a +N% buff). turnsRemaining counts down one per
// end-of-turn tick; the effect expires (is removed) when it reaches zero.
type timedEffect struct {
	defID          string
	magnitude      int
	turnsRemaining int
}

// effectDef is the registry description of one effect KIND (content.go's
// effectDefs table). It is the STRUCTURAL half of a timed effect — which
// pipeline event the effect folds at, any gating conditions, and which effect
// verb it uses — while the per-application magnitude and duration live on the
// timedEffect INSTANCE. Splitting it this way lets one def back several
// strengths (a weak vs a strong poison) without a def per number, and keeps
// every field pure data.
type effectDef struct {
	id, name string
	// event is the pipeline event this effect folds at: evDealDamage for a
	// damage buff, evEndOfTurn for a per-turn drain/heal. Validated against the
	// same event switch content cards use (validateEffectDefs).
	event string
	// when are conditions gating the fold, usually empty — an active effect
	// already gates itself by being present in the list. Kept so a future
	// conditional effect (a DoT that only bites a full-HP target, say) is
	// content, not machinery.
	when []condition
	// effect is the effect verb (effAdd / effMulPct) whose n the instance
	// magnitude fills.
	effect string
}

// effectDefByID is the id→def lookup built from effectDefs (content.go) at
// package init by buildEffectIndex, before mustValidateContent — the same
// build-then-validate shape itemDefByID / monsterDefByID use.
//
//nolint:gochecknoglobals // derived lookup table, built once at init from effectDefs (content.go).
var effectDefByID map[string]*effectDef

// buildEffectIndex builds effectDefByID from effectDefs (content.go). Called
// once from content.go's init, before mustValidateContent — so an item's onHit
// or a monster claws' onHit can resolve an effect id at validation time.
func buildEffectIndex() {
	effectDefByID = make(map[string]*effectDef, len(effectDefs))
	for _, def := range effectDefs {
		effectDefByID[def.id] = def
	}
}

// validateEffectDefs panics if any effect def is malformed (#271): a duplicate
// or empty id, or a card SHAPE the pipeline would reject — an unknown event or
// effect verb, or an illegal condition. It validates by synthesizing the exact
// ruleCard the effect contributes (a magnitude of 1) and running it through the
// SAME validateRuleCards the content cards use, so an effect def can never name
// vocabulary the fold sites do not implement. Called from mustValidateContent
// (content.go).
func validateEffectDefs(defs []*effectDef) {
	seen := make(map[string]bool, len(defs))

	for _, def := range defs {
		if def.id == "" || def.name == "" {
			panic("game: effect def has empty id or name")
		}

		if seen[def.id] {
			panic("game: duplicate effect id " + def.id)
		}

		seen[def.id] = true

		// The card an INSTANCE of this def contributes must be legal for the
		// pipeline: event, conditions, and effect verb all validated as content
		// cards are (magnitude stands in as 1 — the shape is what is checked).
		validateRuleCards("effect:"+def.id, []ruleCard{
			{event: def.event, when: def.when, then: effect{kind: def.effect, n: 1}},
		})
	}
}

// card synthesizes the ruleCard a timedEffect contributes to a fold: pure data
// assembled from the def's structure and the instance's magnitude. It is the
// bridge that makes "a timed effect" and "a rule card" the same thing to the
// pipeline — applyRules cannot tell a synthesized effect card from an authored
// gear card, which is the whole point.
func (te timedEffect) card() ruleCard {
	def := effectDefByID[te.defID]

	return ruleCard{event: def.event, when: def.when, then: effect{kind: def.effect, n: te.magnitude}}
}

// activeEffectCards returns the rule cards an entity's active timed effects
// contribute, in the effects slice's order (kept sorted by defID at apply time
// — applyTimedEffectLocked — so the fold order is deterministic and does not
// leak map iteration order into the pipeline). Nil when the entity has no
// effects, so the common case appends nothing and cannot move a seeded pin.
//
// Every fold site filters by its own event, so one call serves all events: a
// deal-damage fold picks up only the buff cards, an end-of-turn fold only the
// DoT/regen cards. Effect cards carry no chance condition, so they consume no
// rng — appending them never shifts an existing seeded expectation.
func activeEffectCards(e *entity) []ruleCard {
	if len(e.effects) == 0 {
		return nil
	}

	cards := make([]ruleCard, 0, len(e.effects))
	for _, te := range e.effects {
		cards = append(cards, te.card())
	}

	return cards
}

// applyTimedEffectLocked applies (or REFRESHES) a timed effect on e. Stacking
// model (design decision, #271): a second application of the SAME effect def
// REFRESHES the existing one — it overwrites the magnitude and resets the timer
// — rather than stacking a second independent copy.
//
// The ARPG rationale (docs/design-decisions.md): an effect is a bounded
// percentage/int modifier, and the pipeline ADDS percentages within a fold, so
// N stacked copies would compound toward the runaway vertical scaling the
// flat-power-curve identity explicitly refuses (game-identity.md). Refresh
// keeps each source's contribution bounded to one modifier. Different-def
// effects still coexist (a poison and a buff at once) — refresh is per-def.
//
// The list is kept SORTED by defID so the fold order (activeEffectCards) is
// deterministic. A non-positive turns is a no-op — an effect with no duration
// is nothing. Callers hold w.mu.
func applyTimedEffectLocked(e *entity, defID string, magnitude, turns int) {
	if turns <= 0 {
		return
	}

	for i := range e.effects {
		if e.effects[i].defID == defID {
			e.effects[i].magnitude = magnitude
			e.effects[i].turnsRemaining = turns

			return
		}
	}

	e.effects = append(e.effects, timedEffect{defID: defID, magnitude: magnitude, turnsRemaining: turns})
	slices.SortFunc(e.effects, func(a, b timedEffect) int {
		return cmp.Compare(a.defID, b.defID)
	})
}

// tickEffectsLocked is the end-of-turn tick: for every member it folds the
// per-turn (evEndOfTurn) effects into a single HP delta and applies it, then
// decrements every effect's timer and expires those that reach zero. Driven
// once per turn resolution from resolveCombatLocked, AFTER attacks and moves
// and BEFORE resolveDeathsLocked — so a DoT that drops an entity to 0 is
// reaped by the same death pass that reaps a melee kill (a DoT kill in a bubble
// therefore awards XP and rolls loot exactly like any other), and a heal is
// clamped to maxHP.
//
// Deterministic and rng-FREE by construction: evEndOfTurn folds with no rng
// (like earn-xp — validateRuleCondition rejects a chance condition on it), the
// per-turn amount is a flat magnitude, and members arrive id-sorted. It
// therefore consumes nothing from the turn's rng stream and cannot shift a
// seeded pin — an entity with no effects is skipped entirely.
//
// An effect ticks on EVERY turn it is active, INCLUDING the turn it was applied
// (on-hit application happens earlier in resolveCombatLocked): the deal-damage
// fold for this turn's hits has already resolved, so a fresh buff first bites
// next turn, while a fresh DoT — whose event has not yet folded this turn —
// bites now. Each is correct for its own event's place in the resolution order.
// Callers hold w.mu.
func (w *World) tickEffectsLocked(members []*entity) {
	for _, e := range members {
		if len(e.effects) == 0 {
			continue
		}

		// A member already dead this turn (a melee kill) is reaped by
		// resolveDeathsLocked next; do not heal it back above zero or drain a
		// corpse.
		if e.hp > 0 {
			if delta := applyRules(evEndOfTurn, 0, activeEffectCards(e), ruleCtx{victim: e}); delta != 0 {
				e.hp = min(e.hp+delta, e.maxHP)
			}
		}

		for i := range e.effects {
			e.effects[i].turnsRemaining--
		}

		e.effects = slices.DeleteFunc(e.effects, func(te timedEffect) bool {
			return te.turnsRemaining <= 0
		})

		// Drop back to the nil zero value when empty so the snapshot and the
		// "no effects" fast paths stay clean (an empty non-nil slice would
		// round-trip as nil anyway).
		if len(e.effects) == 0 {
			e.effects = nil
		}
	}
}

// appliedEffect is a pure-data on-hit rider carried by a weapon def (itemDef's
// onHit, items.go): when a melee hit with that weapon lands, the effect is
// applied to either the attacker (toSelf) or the victim. It is what makes a
// poison monster and a self-buff weapon CONTENT — a registry row — rather than
// two edits at the combat site. Deliberately NOT a pipeline effect kind: an
// applyRules effect must stay a pure int fold (it runs in preview and trace
// paths with no world to mutate), so APPLYING an effect is a separate,
// side-effecting step at the combat site, keyed off this data.
type appliedEffect struct {
	// effectID names an effectDef in the registry (effectDefByID).
	effectID string
	// magnitude and turns are the instance values handed to
	// applyTimedEffectLocked (magnitude signed as the effect's fold value —
	// negative for a DoT, percentBase+N for a buff).
	magnitude int
	turns     int
	// toSelf routes the effect to the ATTACKER (a self-buff-on-hit like a rage
	// weapon) rather than the VICTIM (a poison-on-hit). The default (false) is
	// victim-side, the poison case.
	toSelf bool
}

// pendingEffectApply is one on-hit effect application collected during the
// melee attack phase (attackLocked) and applied AFTER the turn's damage map
// resolves — so a self-buff never folds into the same turn's later hits
// (dual-wield) and only bites next turn, deterministically.
type pendingEffectApply struct {
	target *entity
	ae     appliedEffect
}

// collectOnHitEffects appends weapon's on-hit effects (if any) to acc, routing
// each to the attacker (toSelf) or the victim. A weapon with no onHit riders —
// every weapon in the game today except the two #271 proof consumers — appends
// nothing, so this is a no-op on the hot path.
func collectOnHitEffects(acc []pendingEffectApply, attacker, victim *entity, weapon *itemDef) []pendingEffectApply {
	for _, ae := range weapon.onHit {
		target := victim
		if ae.toSelf {
			target = attacker
		}

		acc = append(acc, pendingEffectApply{target: target, ae: ae})
	}

	return acc
}
