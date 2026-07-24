package game

// balance_sim.go: the party-size simulation (#283 task 4) — N scripted bots
// in a real generated world with real monster AI, stepped synchronously for T
// turns, reporting the fun-proxy scorecard. Nothing in content scales with
// party count today, so the size curve this measures is the slice's headline
// unknown.
//
// The bot policy is deliberately simple (decision 4): attack what's in reach,
// walk toward the nearest known monster otherwise, drink a held potion when
// low. It approximates a player, so every scorecard number is "for this
// policy" — stated once here rather than hedged per metric.

import (
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// PartySimConfig scales RunPartySim. Zero values default to the issue's
// grid: sizes {1,2,3,5,10,15}, 3 seeds, 200 turns, radius 24, 40 monsters.
type PartySimConfig struct {
	BaseSeed uint64
	Sizes    []int
	Seeds    int
	Turns    int
	Radius   int
	Monsters int
}

const (
	per100             = 100
	defaultSimSeeds    = 3
	defaultSimTurns    = 200
	defaultSimRadius   = 24
	defaultSimMonsters = 40
	// drinkBelowFrac is the bot's potion threshold (decision 4: below 30%).
	drinkBelowFrac = 0.3
)

// SizeStats is one party size's scorecard, averaged over the configured
// seeds. All "per turn" rates are per PLAYER-turn, so sizes compare directly.
type SizeStats struct {
	Players int `json:"players"`
	// DeathsPer100 is player deaths per 100 player-turns.
	DeathsPer100 float64 `json:"deathsPer100"`
	// CloseCall is the mean per-fight HP low-water mark as a fraction of max
	// HP — the excitement proxy (near 0 = squeakers, near 1 = never scared).
	CloseCall float64 `json:"closeCall"`
	// CombatFrac is the fraction of player-turns spent inside a bubble.
	CombatFrac float64 `json:"combatFrac"`
	// XPPerTurn is mean XP gained per player-turn.
	XPPerTurn float64 `json:"xpPerTurn"`
	// Spread is the relative spread of XP across members at the end
	// ((max-min)/mean; 0 for a solo run) — "is player 12 just watching?".
	Spread float64 `json:"spread"`
}

// PartySimReport is one row per party size, ascending.
type PartySimReport struct {
	Sizes []SizeStats `json:"sizes"`
}

// RunPartySim runs the full size grid.
func RunPartySim(cfg PartySimConfig) PartySimReport {
	if len(cfg.Sizes) == 0 {
		cfg.Sizes = []int{1, 2, 3, 5, 10, 15}
	}

	if cfg.Seeds <= 0 {
		cfg.Seeds = defaultSimSeeds
	}

	if cfg.Turns <= 0 {
		cfg.Turns = defaultSimTurns
	}

	if cfg.Radius <= 0 {
		cfg.Radius = defaultSimRadius
	}

	if cfg.Monsters <= 0 {
		cfg.Monsters = defaultSimMonsters
	}

	var report PartySimReport

	for _, size := range cfg.Sizes {
		agg := SizeStats{Players: size}

		for s := range cfg.Seeds {
			seed := deriveSeed(cfg.BaseSeed, "sim", strconv.Itoa(size), strconv.Itoa(s))
			run := runPartyWorld(cfg, size, seed)
			agg.DeathsPer100 += run.DeathsPer100
			agg.CloseCall += run.CloseCall
			agg.CombatFrac += run.CombatFrac
			agg.XPPerTurn += run.XPPerTurn
			agg.Spread += run.Spread
		}

		n := float64(cfg.Seeds)
		agg.DeathsPer100 /= n
		agg.CloseCall /= n
		agg.CombatFrac /= n
		agg.XPPerTurn /= n
		agg.Spread /= n
		report.Sizes = append(report.Sizes, agg)
	}

	return report
}

// botState tracks one bot's per-fight low-water accounting across bubble
// episodes.
type botState struct {
	id        int64
	inFight   bool
	episodeLo float64
}

func runPartyWorld(cfg PartySimConfig, size int, seed uint64) SizeStats {
	w := NewWorld(WorldConfig{
		Interval:        time.Second,
		CombatPatience:  time.Second,
		BubblePoll:      time.Second,
		DisconnectGrace: time.Hour,
		WorldSeed:       seed,
		Radius:          cfg.Radius,
		Ticks:           hub.New(),
		// Decision 4's potion: every bot starts holding one Healing Potion,
		// the same knob production exposes as STARTER_CONSUMABLES.
		StarterConsumables: []string{idHealingPotion},
	})

	deaths := &deathLog{playerDeaths: make(map[int64]int)}
	w.SetLogger(slog.New(deaths))

	w.mu.Lock()
	//nolint:gosec // seed reinterpretation: any 64-bit value is a valid seed.
	w.seed = int64(seed)
	base := time.Unix(0, 0)
	w.now = func() time.Time { return base }
	w.mu.Unlock()

	w.SpawnMonsters(cfg.Monsters)

	classes := []string{protocol.ClassFighter, protocol.ClassRogue, protocol.ClassMage}
	bots := make([]*botState, 0, size)

	for i := range size {
		resp, err := w.Join(fmt.Sprintf("balance-sim-%d", i), fmt.Sprintf("bot%d", i),
			classes[i%len(classes)], protocol.SpeciesHuman)
		if err != nil {
			panic("game: RunPartySim join failed: " + err.Error())
		}

		bots = append(bots, &botState{id: resp.EntityID, episodeLo: 1})
	}

	stats := SizeStats{Players: size}
	obs := &simObserver{}

	for range cfg.Turns {
		w.mu.Lock()
		for _, b := range bots {
			botAct(w, b.id)
		}
		w.mu.Unlock()

		w.stepOnce()

		w.mu.Lock()
		obs.observeTurn(w, bots)
		w.mu.Unlock()
	}

	w.mu.Lock()
	obs.finish(w, bots, deaths, &stats, size)
	w.mu.Unlock()

	return stats
}

// simObserver accumulates the per-turn scorecard inputs. Split from
// runPartyWorld so each half stays readable: the run loop drives, the
// observer counts.
type simObserver struct {
	playerTurns, combatTurns int
	fightLows                []float64
}

// observeTurn scans every bot after one step. Caller holds w.mu.
func (o *simObserver) observeTurn(w *World, bots []*botState) {
	for _, b := range bots {
		e, ok := w.entities[b.id]
		if !ok {
			continue
		}

		o.playerTurns++

		if e.bubbleID != 0 {
			o.combatTurns++

			lo := float64(e.hp) / float64(e.maxHP)
			if !b.inFight || lo < b.episodeLo {
				b.episodeLo = lo
			}

			b.inFight = true
		} else if b.inFight {
			o.fightLows = append(o.fightLows, b.episodeLo)
			b.inFight = false
			b.episodeLo = 1
		}
	}
}

// finish folds the accumulated counts into the scorecard. Caller holds w.mu.
func (o *simObserver) finish(w *World, bots []*botState, deaths *deathLog, stats *SizeStats, size int) {
	var totalXP, totalDeaths int

	minXP, maxXP := math.MaxInt, 0

	for _, b := range bots {
		if b.inFight {
			o.fightLows = append(o.fightLows, b.episodeLo)
		}

		totalDeaths += deaths.playerDeaths[b.id]

		if e, ok := w.entities[b.id]; ok {
			totalXP += e.xp
			minXP = min(minXP, e.xp)
			maxXP = max(maxXP, e.xp)
		}
	}

	if o.playerTurns > 0 {
		stats.DeathsPer100 = per100 * float64(totalDeaths) / float64(o.playerTurns)
		stats.CombatFrac = float64(o.combatTurns) / float64(o.playerTurns)
		stats.XPPerTurn = float64(totalXP) / float64(o.playerTurns)
	}

	for _, lo := range o.fightLows {
		stats.CloseCall += lo
	}

	if len(o.fightLows) > 0 {
		stats.CloseCall /= float64(len(o.fightLows))
	}

	if size > 1 && totalXP > 0 {
		mean := float64(totalXP) / float64(size)
		stats.Spread = float64(maxXP-minXP) / mean
	}
}

// botAct queues one bot's action for this turn (decision 4's policy). Caller
// holds w.mu. Queue errors are deliberately ignored: an invalid action (dead
// target, blocked step) just means the bot stands this turn — the policy is
// an approximation, not an optimum.
func botAct(w *World, id int64) {
	e, ok := w.entities[id]
	if !ok {
		return
	}

	// Drink when low and holding a potion.
	if float64(e.hp) < drinkBelowFrac*float64(e.maxHP) {
		for _, entry := range e.backpack {
			if entry.count > 0 && entry.inst.defID == idHealingPotion {
				if err := w.queueDrinkLocked(e, entry.inst.id); err == nil {
					return
				}
			}
		}
	}

	target := nearestMonsterLocked(w, e)
	if target == nil {
		return
	}

	// Attack whatever reaches; the entity-target shape first, ground-target
	// for an AoE-only kit (the same fallback the duel loop uses).
	if err := w.queueAttackLocked(e, target.hex, target.id); err == nil {
		return
	}

	if err := w.queueAttackLocked(e, target.hex, 0); err == nil {
		return
	}

	botMove(w, e, target)
}

// botMove walks the bot toward target: single-step inside a bubble, a
// server-pathfound destination in the world domain (#116 trims the last hex
// of a hostile-held target). Caller holds w.mu.
func botMove(w *World, e, target *entity) {
	if e.bubbleID == 0 {
		_ = w.queueMoveLocked(e, target.hex)

		return
	}

	// In a bubble, movement is single-step: pick the neighbor closest to
	// the target (ties: HexNeighbors order, which is fixed).
	best := e.hex
	bestDist := HexDistance(e.hex, target.hex)

	for _, n := range HexNeighbors(e.hex) {
		if !w.spawnable[n] || w.occupiedForLocked(e, n) {
			continue
		}

		if d := HexDistance(n, target.hex); d < bestDist {
			best, bestDist = n, d
		}
	}

	if best != e.hex {
		_ = w.queueMoveLocked(e, best)
	}
}

// nearestMonsterLocked scans entities in id order (deterministic — never map
// order) for the closest living monster. Caller holds w.mu.
func nearestMonsterLocked(w *World, e *entity) *entity {
	ids := make([]int64, 0, len(w.entities))
	for id := range w.entities {
		ids = append(ids, id)
	}

	slices.Sort(ids)

	var best *entity

	bestDist := math.MaxInt

	for _, id := range ids {
		m := w.entities[id]
		if m.kind != protocol.EntityMonster || m.hp <= 0 {
			continue
		}

		if d := HexDistance(e.hex, m.hex); d < bestDist {
			best, bestDist = m, d
		}
	}

	return best
}
