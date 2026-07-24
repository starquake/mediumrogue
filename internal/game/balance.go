package game

// balance.go: the balance-measurement harness (#283) — seeded, headless duels
// driven through the REAL resolution code, never a re-derived formula. The
// repo's precedent is statlines.go: derived, never authored, so the
// measurement and the mechanics can never disagree. A duel is a tiny world,
// one player and one monster placed adjacent, real bubble turns stepped
// synchronously (stepOnce, the same driver ResolveTurnForTest uses) until one
// side dies — so on-hit riders, DoTs, regen, lifesteal, and summons all count.
//
// Determinism: each duel derives its own seed (deriveSeed) for both worldgen
// and the resolution rng, and the injected clock never moves — the whole
// matrix is reproducible to the digit. The harness only OBSERVES (hp deltas,
// the structured combat log); it never rolls on shared rng paths.
//
// Player deaths are detected by consuming the structured combat event stream
// (a slog.Handler on the "combat" category) — the same stream the analytics
// milestone will read. Monster deaths delete the entity (resolveDeathsLocked),
// so absence from w.entities is the win signal.

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"sort"
	"strconv"
	"time"

	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// duelWorldRadius is deliberately tiny: a duel needs two adjacent walkable
// hexes, not a world. Worldgen cost is per-duel, so small keeps the matrix
// fast.
const duelWorldRadius = 6

// DuelConfig describes one seeded duel. Zero-value fields fall back to
// defaults in RunDuel (level 1, human, 60-turn bound).
type DuelConfig struct {
	// Seed drives worldgen AND the resolution rng for this duel.
	Seed uint64
	// Class is the player's class (protocol.ClassFighter/Rogue/Mage).
	Class string
	// Species defaults to human — combat-neutral (its passive is earn-xp
	// side), so the baseline matrix measures class+kit, not species luck.
	Species string
	// Level places the player on the XP curve (xp = XPCurveBase*(L-1)^2).
	Level int
	// MonsterKind is the content.go monster-kind id. Panics if unregistered —
	// the registry's fail-loud rule.
	MonsterKind string
	// ExtraItems are item def ids equipped OVER the class-default kit before
	// the duel (the delta mode's one-variable change). A weapon replaces the
	// main hand; armor/jewelry takes its slot.
	ExtraItems []string
	// Passives are extra learned skill ids (delta mode for passive skills).
	Passives []string
	// MaxTurns bounds a stalemate (troll regen vs. low DPS); hitting it is a
	// Draw, not an error.
	MaxTurns int
}

// DuelResult is one duel's outcome, entirely from observed resolution state.
type DuelResult struct {
	PlayerWon  bool
	MonsterWon bool
	// Draw: MaxTurns elapsed with both sides alive.
	Draw  bool
	Turns int
	// HPLeft of each side when the duel ended (loser's is 0; on a draw both
	// are live values).
	PlayerHPLeft  int
	MonsterHPLeft int
	PlayerMaxHP   int
	MonsterMaxHP  int
	// Damage totals are hp-delta accounting per side per turn — attribution
	// is unambiguous in a 1v1, and deltas count what hit records alone miss
	// (DoT ticks), netting out self-heals (lifesteal, regen).
	DamageByPlayer  int
	DamageByMonster int
}

// deathLog consumes the structured combat stream and records player deaths.
// It is the analytics-seed pattern (CLAUDE.md): combat events are a
// filterable stream, and the harness is its first machine consumer.
type deathLog struct {
	playerDeaths map[int64]bool
}

func (*deathLog) Enabled(context.Context, slog.Level) bool { return true }

func (d *deathLog) Handle(_ context.Context, r slog.Record) error {
	if r.Message != combatLogMsg {
		return nil
	}

	var event, kind string

	var id int64

	r.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "event":
			event = a.Value.String()
		case "kind":
			kind = a.Value.String()
		case "id":
			id = a.Value.Int64()
		}

		return true
	})

	if event == combatEventDeath && kind == protocol.EntityPlayer {
		d.playerDeaths[id] = true
	}

	return nil
}

func (d *deathLog) WithAttrs([]slog.Attr) slog.Handler { return d }
func (d *deathLog) WithGroup(string) slog.Handler      { return d }

// deriveSeed folds a cell's identity into a per-duel seed, so every duel in a
// matrix is independently reproducible and no two cells share an rng stream.
func deriveSeed(base uint64, parts ...string) uint64 {
	h := fnv.New64a()

	_, _ = fmt.Fprintf(h, "%d", base)

	for _, p := range parts {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(p))
	}

	return h.Sum64()
}

// duelSite returns a deterministic pair of adjacent walkable hexes: the
// map-derived spawnable set is SORTED before scanning (the standing
// determinism rule), and the first adjacent pair wins.
func duelSite(w *World) (protocol.Hex, protocol.Hex) {
	hexes := make([]protocol.Hex, 0, len(w.spawnable))
	for h := range w.spawnable {
		hexes = append(hexes, h)
	}

	sort.Slice(hexes, func(i, j int) bool {
		if hexes[i].Q != hexes[j].Q {
			return hexes[i].Q < hexes[j].Q
		}

		return hexes[i].R < hexes[j].R
	})

	set := make(map[protocol.Hex]bool, len(hexes))
	for _, h := range hexes {
		set[h] = true
	}

	for _, h := range hexes {
		for _, n := range HexNeighbors(h) {
			if set[n] {
				return h, n
			}
		}
	}

	panic("game: balance duelSite found no adjacent walkable pair — radius too small or seed degenerate")
}

// placeDuelPlayer injects the configured player at hex: class-default kit
// (grantDefaultsLocked, mirroring Join), the requested level via the XP curve,
// extra items and passives applied last.
func (w *World) placeDuelPlayer(hex protocol.Hex, cfg DuelConfig) *entity {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.nextID++
	e := &entity{
		id: w.nextID, hex: hex, token: fmt.Sprintf("balance-%d", w.nextID),
		kind: protocol.EntityPlayer, class: cfg.Class, species: cfg.Species,
		streams: 1, disconnectedAt: w.now(),
	}
	w.entities[e.id] = e
	w.byToken[e.token] = e
	w.grantDefaultsLocked(e)

	e.xp = protocol.XPCurveBase * (cfg.Level - 1) * (cfg.Level - 1)
	syncMaxHPLocked(e)
	e.hp = e.maxHP

	for _, defID := range cfg.ExtraItems {
		def, ok := itemDefByID[defID]
		if !ok {
			panic("game: balance ExtraItems unknown item def " + defID)
		}

		w.nextID++
		e.equipped[slotForItem(def)] = itemInstance{id: w.nextID, defID: defID}
	}

	e.learned = append(e.learned, cfg.Passives...)

	sort.Strings(e.learned) // the entity invariant: learned stays sorted

	return e
}

// slotForItem maps a def onto the equip slot the delta mode overwrites: a
// weapon takes the main hand; everything else has exactly one armor/jewelry
// slot (slotForType).
func slotForItem(def *itemDef) string {
	if def.itemType == protocol.ItemTypeWeapon {
		return protocol.SlotMainHand
	}

	return slotForType(def.itemType)
}

// RunDuel runs one seeded duel to the death (or the MaxTurns draw bound) and
// reports what actually happened.
func RunDuel(cfg DuelConfig) DuelResult {
	if cfg.Species == "" {
		cfg.Species = protocol.SpeciesHuman
	}

	if cfg.Level <= 0 {
		cfg.Level = 1
	}

	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = 60
	}

	kind, ok := monsterDefByID[cfg.MonsterKind]
	if !ok {
		panic("game: RunDuel unknown monster kind " + cfg.MonsterKind)
	}

	w := NewWorld(WorldConfig{
		Interval:        time.Second,
		CombatPatience:  time.Second,
		BubblePoll:      time.Second,
		DisconnectGrace: time.Hour,
		WorldSeed:       cfg.Seed,
		Radius:          duelWorldRadius,
		Ticks:           hub.New(),
	})

	deaths := &deathLog{playerDeaths: make(map[int64]bool)}
	w.SetLogger(slog.New(deaths))

	// Pin the resolution rng (NewWorld draws it from crypto/rand) and freeze
	// the clock — stepOnce is ungated, so time never needs to advance.
	w.mu.Lock()
	//nolint:gosec // seed reinterpretation: any 64-bit value is a valid seed.
	w.seed = int64(cfg.Seed)
	base := time.Unix(0, 0)
	w.now = func() time.Time { return base }
	w.mu.Unlock()

	site, foe := duelSite(w)
	player := w.placeDuelPlayer(site, cfg)

	w.mu.Lock()
	w.nextID++
	monsterID := w.nextID
	w.entities[monsterID] = newMonsterEntity(monsterID, foe, kind)
	monster := w.entities[monsterID]
	w.mu.Unlock()

	return runDuelLoop(w, deaths, player, monster, monsterID, cfg.MaxTurns)
}

// runDuelLoop steps the placed board until a death or the draw bound,
// accounting damage by hp deltas per side per turn.
func runDuelLoop(w *World, deaths *deathLog, player, monster *entity, monsterID int64, maxTurns int) DuelResult {
	res := DuelResult{PlayerMaxHP: player.maxHP, MonsterMaxHP: monster.maxHP}

	for t := 1; t <= maxTurns; t++ {
		w.mu.Lock()
		// Entity-targeted first (melee and single-target ranged); an AoE-only
		// kit (the mage) rejects that shape, so fall back to ground-targeted.
		// A queue error mid-duel (target just died) is fine — the outcome
		// check below ends the loop.
		if err := w.queueAttackLocked(player, monster.hex, monsterID); err != nil {
			_ = w.queueAttackLocked(player, monster.hex, 0)
		}

		pBefore, mBefore := player.hp, monster.hp
		w.mu.Unlock()

		w.stepOnce()

		res.Turns = t

		w.mu.Lock()
		_, monsterAlive := w.entities[monsterID]
		pAfter := player.hp
		mAfter := monster.hp

		if !monsterAlive {
			mAfter = 0
		}

		if d := mBefore - mAfter; d > 0 {
			res.DamageByPlayer += d
		}

		if d := pBefore - pAfter; d > 0 {
			res.DamageByMonster += d
		}

		playerDied := deaths.playerDeaths[player.id]
		w.mu.Unlock()

		if playerDied {
			// The engine already respawned the entity at full HP; the duel's
			// record is the death itself.
			res.MonsterWon = true
			res.PlayerHPLeft = 0
			res.MonsterHPLeft = mAfter

			return res
		}

		if !monsterAlive {
			res.PlayerWon = true
			res.PlayerHPLeft = pAfter
			res.MonsterHPLeft = 0

			return res
		}
	}

	res.Draw = true
	res.PlayerHPLeft = player.hp
	res.MonsterHPLeft = monster.hp

	return res
}

// CellStats is one matchup cell: a class (with default kit, plus any delta
// override) against a monster kind at a level, aggregated over Duels runs.
type CellStats struct {
	Class string `json:"class"`
	Kind  string `json:"kind"`
	Level int    `json:"level"`

	Duels       int `json:"duels"`
	PlayerWins  int `json:"playerWins"`
	MonsterWins int `json:"monsterWins"`
	Draws       int `json:"draws"`

	MeanTurns float64 `json:"meanTurns"`
	// WinnerHPFrac is the mean fraction of max HP the winning side kept —
	// the "how close was it" margin (1.0 = untouched, near 0 = a squeaker).
	WinnerHPFrac float64 `json:"winnerHpFrac"`

	// DPS observed per side (total damage / total turns, across all duels),
	// and the projected turns-to-kill each direction derived from it:
	// TTKPlayer = monster max HP / player DPS (how long the player needs),
	// TTKMonster = player max HP / monster DPS. Projection, not observation:
	// a duel stops at the first death, so the winner's own TTK is observed
	// and the loser's is extrapolated from its realized damage rate.
	DPSPlayer  float64 `json:"dpsPlayer"`
	DPSMonster float64 `json:"dpsMonster"`
	TTKPlayer  float64 `json:"ttkPlayer"`
	TTKMonster float64 `json:"ttkMonster"`

	// Threat = TTKPlayer / TTKMonster. 1.0 = an even race; above 1 the
	// monster kills faster than the player (dangerous); below 1 the player
	// outpaces it. 0 when either side never dealt damage (see Draws).
	Threat float64 `json:"threat"`
}

// MatrixConfig scales RunDuelMatrix. Zero values default: all classes, the
// full monster registry in registry order, levels {1}, 200 duels, 60 turns.
type MatrixConfig struct {
	BaseSeed uint64
	Duels    int
	Levels   []int
	Classes  []string
	Kinds    []string
	MaxTurns int
	// ExtraItems/Passives apply to EVERY cell (the delta mode re-runs the
	// matrix with one change and diffs against a baseline report).
	ExtraItems []string
	Passives   []string
}

// MatrixReport is the full grid, cells ordered class-major, then kind in
// registry order, then level ascending — a stable order for diffing.
type MatrixReport struct {
	Cells []CellStats `json:"cells"`
}

// RunDuelMatrix runs the class x kind x level grid.
func RunDuelMatrix(cfg MatrixConfig) MatrixReport {
	if cfg.Duels <= 0 {
		cfg.Duels = 200
	}

	if len(cfg.Levels) == 0 {
		cfg.Levels = []int{1}
	}

	if len(cfg.Classes) == 0 {
		cfg.Classes = []string{protocol.ClassFighter, protocol.ClassRogue, protocol.ClassMage}
	}

	if len(cfg.Kinds) == 0 {
		for _, def := range monsterDefs {
			cfg.Kinds = append(cfg.Kinds, def.id)
		}
	}

	var report MatrixReport

	for _, class := range cfg.Classes {
		for _, kind := range cfg.Kinds {
			for _, level := range cfg.Levels {
				report.Cells = append(report.Cells, runCell(cfg, class, kind, level))
			}
		}
	}

	return report
}

func runCell(cfg MatrixConfig, class, kind string, level int) CellStats {
	cell := CellStats{Class: class, Kind: kind, Level: level, Duels: cfg.Duels}

	var totalTurns, dmgPlayer, dmgMonster int

	var winnerFracSum float64

	var pMaxHP, mMaxHP int

	for i := range cfg.Duels {
		seed := deriveSeed(cfg.BaseSeed, class, kind, strconv.Itoa(level), strconv.Itoa(i))
		r := RunDuel(DuelConfig{
			Seed: seed, Class: class, Level: level, MonsterKind: kind,
			ExtraItems: cfg.ExtraItems, Passives: cfg.Passives, MaxTurns: cfg.MaxTurns,
		})

		totalTurns += r.Turns
		dmgPlayer += r.DamageByPlayer
		dmgMonster += r.DamageByMonster
		pMaxHP, mMaxHP = r.PlayerMaxHP, r.MonsterMaxHP

		switch {
		case r.PlayerWon:
			cell.PlayerWins++
			winnerFracSum += float64(r.PlayerHPLeft) / float64(r.PlayerMaxHP)
		case r.MonsterWon:
			cell.MonsterWins++
			winnerFracSum += float64(r.MonsterHPLeft) / float64(r.MonsterMaxHP)
		default:
			cell.Draws++
		}
	}

	cell.MeanTurns = float64(totalTurns) / float64(cfg.Duels)

	if decided := cell.PlayerWins + cell.MonsterWins; decided > 0 {
		cell.WinnerHPFrac = winnerFracSum / float64(decided)
	}

	if totalTurns > 0 {
		cell.DPSPlayer = float64(dmgPlayer) / float64(totalTurns)
		cell.DPSMonster = float64(dmgMonster) / float64(totalTurns)
	}

	if cell.DPSPlayer > 0 {
		cell.TTKPlayer = float64(mMaxHP) / cell.DPSPlayer
	}

	if cell.DPSMonster > 0 {
		cell.TTKMonster = float64(pMaxHP) / cell.DPSMonster
	}

	if cell.TTKPlayer > 0 && cell.TTKMonster > 0 {
		cell.Threat = cell.TTKPlayer / cell.TTKMonster
	}

	return cell
}
