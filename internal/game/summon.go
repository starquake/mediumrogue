package game

import (
	mrand "math/rand/v2"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// summon.go: the in-combat summon hook (#271, slice) — the mechanism a SUMMONER
// monster (a Necromancer, content.go) uses to raise weak adds mid-fight. Like
// the on-hit effect rider (effects.go's appliedEffect), a summon is a
// pure-data monsterDef property plus a SIDE-EFFECTING hook, deliberately NOT a
// pipeline effect kind: applyRules folds a pure int and mutates nothing, but
// spawning creates entities, so it lives at the resolution site keyed off data,
// not in the fold.
//
// The design rule, stated once: a summon is content — a summonSpec row on a
// kind — never a special case at a combat site. tickSummonsLocked drives it
// from resolveCombatLocked at the same end-of-turn point the timed-effect tick
// runs, and it is BOUNDED two ways so it can never runaway-spawn: a per-summoner
// living-minion cap (summonSpec.maxLiving) and a per-turn window (everyTurns).
//
// ARPG, not TTRPG (docs/game-identity.md): summoning is a deterministic spawn
// behavior — no summon check, no save, no roll to resist. The only randomness
// is WHICH free adjacent hex an add lands on, drawn from the same per-turn
// seeded PCG the rest of resolution uses.

// summonSpec is a summoner kind's pure-data description of what it raises and
// how fast (monsterDef.summon). Validated at package init (validateMonsterSummon):
// a malformed spec — an unregistered minion kind, a non-positive cadence or cap
// — panics at process start, never mid-fight.
type summonSpec struct {
	// minionKind is the monster-kind id (content.go's monsterDefs) the summoner
	// raises. Resolved and checked against monsterDefByID at init.
	minionKind string
	// everyTurns is the number of IN-COMBAT turns between summon windows: the
	// cooldown reloaded onto the summoner each time a window opens. Must be > 0.
	everyTurns int
	// maxLiving caps how many of THIS summoner's minions may be alive at once —
	// the runaway-spawn guard. A summoner at its cap opens its window and raises
	// nothing until one of its adds dies. Must be > 0.
	maxLiving int
	// count is how many minions one open window tries to raise, clamped down to
	// the room left under maxLiving and to the number of free adjacent hexes.
	// Must be >= 1.
	count int
}

// validateMonsterSummon panics if def carries a malformed summon spec (#271):
// an unregistered minion kind, or a non-positive cadence/cap/count. Called from
// validateMonsterDefs, after buildMonsterIndex has populated monsterDefByID, so
// a minionKind naming a kind defined later in the registry still resolves.
func validateMonsterSummon(def *monsterDef) {
	sp := def.summon
	if sp == nil {
		return
	}

	if _, ok := monsterDefByID[sp.minionKind]; !ok {
		panic("game: summoner " + def.id + " names unregistered minion kind " + sp.minionKind)
	}

	if sp.everyTurns <= 0 {
		panic("game: summoner " + def.id + " summon.everyTurns must be > 0")
	}

	if sp.maxLiving <= 0 {
		panic("game: summoner " + def.id + " summon.maxLiving must be > 0")
	}

	if sp.count <= 0 {
		panic("game: summoner " + def.id + " summon.count must be >= 1")
	}
}

// tickSummonsLocked is the end-of-turn summon hook: for every summoner in the
// resolving set it advances the summon cooldown and, when a window opens, raises
// its adds. Driven from resolveCombatLocked AFTER resolveDeathsLocked, so a
// summoner slain this turn (hp <= 0) raises nothing and its rng draw lands after
// every existing consumer — the reason adding this hook shifts no pinned seed.
//
// worldDomain gates it to IN-COMBAT only (a bubble turn, worldDomain false):
// summoning is a fight behavior, the same way a bubble monster chases
// unconditionally while a world-domain one obeys aggro range. members arrives
// id-sorted (bubbleMembersLocked), so multiple summoners process — and consume
// rng — in a deterministic order. Callers hold w.mu.
func (w *World) tickSummonsLocked(rng *mrand.Rand, members []*entity, worldDomain bool) {
	if worldDomain {
		return
	}

	for _, e := range members {
		k := kindOf(e)
		if k == nil || k.summon == nil || e.hp <= 0 {
			continue
		}

		if e.summonCooldown > 0 {
			e.summonCooldown--

			continue
		}

		// Window open: reload the cooldown REGARDLESS of whether anything was
		// raised (capped, or no free hex), so the summon rate stays bounded to
		// one window per everyTurns even while the summoner sits at its cap.
		e.summonCooldown = k.summon.everyTurns
		w.raiseMinionsLocked(rng, e, k.summon)
	}
}

// raiseMinionsLocked raises up to spec.count minions for summoner, respecting
// the living-minion cap (spec.maxLiving) and placing each on a FREE adjacent hex
// — the same walkability + occupancy rule an ordinary mover obeys
// (occupiedForLocked: no opposing occupant, under StackCap), so an add can never
// land on a blocked, occupied-by-a-player, or full hex (#196). The candidate
// hexes come from HexNeighbors' fixed six-slot order (not a map), so they need
// no sort; the seeded rng only shuffles WHICH free hexes get used. Callers hold
// w.mu.
func (w *World) raiseMinionsLocked(rng *mrand.Rand, summoner *entity, spec *summonSpec) {
	room := spec.maxLiving - w.livingMinionsLocked(summoner.id)
	if room <= 0 {
		return
	}

	want := min(spec.count, room)

	free := w.freeSummonHexesLocked(summoner)
	if len(free) == 0 {
		return
	}

	// Shuffle the free-hex candidates with the turn rng so an add's placement
	// varies deterministically instead of always taking the first neighbor
	// slot. The candidate slice is fixed-order (HexNeighbors), so the only rng
	// this hook consumes is the shuffle — deterministic for a pinned seed.
	rng.Shuffle(len(free), func(i, j int) { free[i], free[j] = free[j], free[i] })

	minionDef := monsterDefByID[spec.minionKind]
	n := min(want, len(free))

	for i := range n {
		w.nextID++
		m := newMonsterEntity(w.nextID, free[i], minionDef)
		m.summonerID = summoner.id
		// The add starts in the world domain (bubbleID 0); recomputeBubblesLocked
		// — the single authority on membership — folds it into the fight at the
		// START of next tick (before any resolution), since it spawns adjacent to
		// the bubbled summoner. Setting bubbleID by hand here would duplicate that
		// authority and leave the id out of the bubble's own member map.
		w.entities[w.nextID] = m

		w.logger.Info(combatLogMsg, "event", combatEventSummon,
			"summoner", summoner.id, "minion", m.id, "kind", minionDef.id, "at", m.hex)
	}
}

// livingMinionsLocked counts the entities currently alive that were raised by
// the summoner with id summonerID — the summonSpec.maxLiving cap's denominator.
// A count over an unordered map, order-independent and rng-free. Callers hold
// w.mu.
func (w *World) livingMinionsLocked(summonerID int64) int {
	n := 0

	for _, e := range w.entities {
		if e.summonerID == summonerID && e.hp > 0 {
			n++
		}
	}

	return n
}

// freeSummonHexesLocked returns summoner's adjacent hexes that a fresh minion
// may spawn on: walkable, and not blocked for the summoner's own faction
// (occupiedForLocked — no opposing occupant, under StackCap). Returned in
// HexNeighbors' fixed order (deterministic); the caller shuffles with the turn
// rng. Callers hold w.mu.
func (w *World) freeSummonHexesLocked(summoner *entity) []protocol.Hex {
	var free []protocol.Hex

	for _, h := range HexNeighbors(summoner.hex) {
		if w.walkableLocked(h) && !w.occupiedForLocked(summoner, h) {
			free = append(free, h)
		}
	}

	return free
}
