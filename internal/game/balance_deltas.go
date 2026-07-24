package game

// balance_deltas.go: the one-variable re-run (#283 task 3). Each equippable
// item (and each passive skill) is measured by re-running the duel grid with
// exactly that change over the class-default kit and diffing threat against
// the baseline — a mechanical item power score, from the real fold, no
// authored tier numbers. Outliers vs. their tier peers become visible without
// a playtest.

import (
	"fmt"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// DeltaConfig scales RunDeltas. Zero values default like MatrixConfig, except
// Duels (deltas re-run the grid once per item, so the default is smaller) and
// Levels (level 1 — the tier where a single item swing matters most).
type DeltaConfig struct {
	BaseSeed uint64
	Duels    int
	Levels   []int
	Classes  []string
	Kinds    []string
	MaxTurns int
}

// DeltaRow is one item's (or passive's) mean threat delta for one class and
// level, averaged over every monster kind. Negative = the fights got safer
// for the player with the item equipped.
type DeltaRow struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"` // "weapon", "chest", ..., or "passive"
	Class string `json:"class"`
	Level int    `json:"level"`
	// ThreatDelta is mean(threat with change) - mean(threat baseline) across
	// kinds. Cells where either run never dealt damage both ways are skipped.
	ThreatDelta float64 `json:"threatDelta"`
}

// DeltaReport rows are ordered item-major (registry order, then passives in
// registry order), then class, then level — stable for diffing.
type DeltaReport struct {
	Rows []DeltaRow `json:"rows"`
}

const defaultDeltaDuels = 50

// RunDeltas measures every equippable item and passive skill against the
// baseline grid.
func RunDeltas(cfg DeltaConfig) DeltaReport {
	if cfg.Duels <= 0 {
		cfg.Duels = defaultDeltaDuels
	}

	if len(cfg.Levels) == 0 {
		cfg.Levels = []int{1}
	}

	if len(cfg.Classes) == 0 {
		cfg.Classes = []string{protocol.ClassFighter, protocol.ClassRogue, protocol.ClassMage}
	}

	base := matrixFor(cfg, nil, nil)

	var report DeltaReport

	// itemDefs is the registry slice — already ordered, the determinism rule.
	for _, def := range itemDefs {
		if !equippable(def) {
			continue
		}

		mod := matrixFor(cfg, []string{def.id}, nil)
		report.Rows = append(report.Rows, deltaRows(def.id, def.itemType, cfg, base, mod)...)
	}

	for _, def := range skillDefs {
		if def.active != nil {
			// Actives are out of scope (#283 decision 3): measuring one would
			// measure the bot's trigger policy, not the skill.
			continue
		}

		mod := matrixFor(cfg, nil, []string{def.id})
		report.Rows = append(report.Rows, deltaRows(def.id, "passive", cfg, base, mod)...)
	}

	return report
}

// equippable: anything a PLAYER can wear — weapons and wearables, minus the
// monster-only natural weapons (#179's claws/fangs; a row for gear no player
// can hold measures nothing). Consumables have no slot to measure.
func equippable(def *itemDef) bool {
	if def.monsterOnly {
		return false
	}

	return def.itemType == protocol.ItemTypeWeapon || slotForType(def.itemType) != ""
}

func matrixFor(cfg DeltaConfig, items, passives []string) MatrixReport {
	return RunDuelMatrix(MatrixConfig{
		BaseSeed: cfg.BaseSeed, Duels: cfg.Duels, Levels: cfg.Levels,
		Classes: cfg.Classes, Kinds: cfg.Kinds, MaxTurns: cfg.MaxTurns,
		ExtraItems: items, Passives: passives,
	})
}

// deltaRows folds two same-shaped matrix reports into per-class/level mean
// threat deltas. Cell order is identical by construction (same config, same
// loops), so cells pair by index.
func deltaRows(id, kind string, cfg DeltaConfig, base, mod MatrixReport) []DeltaRow {
	type key struct {
		class string
		level int
	}

	sums := make(map[key]float64)
	counts := make(map[key]int)

	for i := range base.Cells {
		b, m := base.Cells[i], mod.Cells[i]
		if b.Class != m.Class || b.Kind != m.Kind || b.Level != m.Level {
			panic(fmt.Sprintf("game: delta cell mismatch at %d: %s/%s vs %s/%s", i, b.Class, b.Kind, m.Class, m.Kind))
		}

		// A cell where either run never exchanged damage both ways has no
		// threat to diff — skip it rather than fabricate a zero.
		if b.Threat == 0 || m.Threat == 0 {
			continue
		}

		k := key{b.Class, b.Level}
		sums[k] += m.Threat - b.Threat
		counts[k]++
	}

	var rows []DeltaRow

	// Deterministic row order: config order, never map order.
	for _, class := range cfg.Classes {
		for _, level := range cfg.Levels {
			k := key{class, level}
			if counts[k] == 0 {
				continue
			}

			rows = append(rows, DeltaRow{
				ID: id, Kind: kind, Class: class, Level: level,
				ThreatDelta: sums[k] / float64(counts[k]),
			})
		}
	}

	return rows
}
