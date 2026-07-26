package game

// balance_composition.go: the party-COMPOSITION sweep (#299).
//
// The size sweep (balance_sim.go) answers "how does a party of N fare"; every
// party it builds is the same round-robin mix, so composition is neither an
// input nor an output there. This file adds the axis.
//
// The framing is the maintainer's (#299 Q2, 2026-07-26): the goal is to CHECK
// THAT NO COMPOSITION DOMINATES, not to crown one. If a single mix wins
// clearly, players copy it and the class choice stops being a choice — so a
// flat field is the good result, and this reports flatness rather than a
// winner.

import (
	"cmp"
	"math"
	"slices"
	"strconv"
)

// Ranking defaults, all overridable from the CLI.
const (
	// defaultCompositionSeeds is high because ranking needs tighter error bars
	// than describing did: #299's own measurements showed XPPerTurn and Spread
	// varying so widely at party >= 3 that a 3-seed ranking would be inventing
	// its winner (maintainer's answer to Q3: 20).
	defaultCompositionSeeds = 20
	// deathsCeiling is the tension ranking's constraint (Q1): above this a
	// party is not "tense", it is dying, and its low close-call number means
	// the opposite of what the ranking reads it as. Expressed per 100
	// player-turns like DeathsPer100 itself.
	//
	// 3.0 sits above every measured party of 2+ and below solo's ~6.5, so it
	// admits the whole interesting range while still excluding a genuine
	// bloodbath. It is a starting line to argue with, not a discovered truth.
	deathsCeiling = 3.0
	// dominanceMargin is how far a composition's close-call must sit from the
	// runner-up, in units of the field's own spread, before this tool will
	// call it a standout. Below it the honest answer is "nothing separates".
	dominanceMargin = 1.0
)

// MetricSpread is one metric across the seeds: its mean and the range that
// mean is hiding. Reporting the mean alone is what would let a ranking invent
// precision it does not have (#299 decision 3).
type MetricSpread struct {
	Mean float64 `json:"mean"`
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
}

func (m MetricSpread) width() float64 { return m.Max - m.Min }

// CompositionStats is one composition's scorecard: the party it describes,
// and every metric with its spread. Self-describing on purpose — a row lifted
// out of the table still says which party it is about.
type CompositionStats struct {
	Composition []string `json:"composition"`
	Players     int      `json:"players"`
	// Label is the composition rendered for a table cell — "2F/1R/2M".
	Label        string       `json:"label"`
	DeathsPer100 MetricSpread `json:"deathsPer100"`
	CloseCall    MetricSpread `json:"closeCall"`
	CombatFrac   MetricSpread `json:"combatFrac"`
	XPPerTurn    MetricSpread `json:"xpPerTurn"`
	Spread       MetricSpread `json:"spread"`
}

// CompositionReport is one party size's whole field, ranked.
type CompositionReport struct {
	Players int                `json:"players"`
	Rows    []CompositionStats `json:"rows"`
	// Verdict states whether anything separated from the field, in words,
	// because that is the question this tool was built to answer.
	Verdict string `json:"verdict"`
}

// compositionLabel renders a class multiset compactly: "2F/1R/2M". Counts in
// simClasses order so two equal parties always render identically.
func compositionLabel(composition []string) string {
	initials := map[string]string{"fighter": "F", "rogue": "R", "mage": "M"}

	out := ""

	for _, c := range simClasses {
		n := 0

		for _, got := range composition {
			if got == c {
				n++
			}
		}

		if n == 0 {
			continue
		}

		if out != "" {
			out += "/"
		}

		out += strconv.Itoa(n) + initials[c]
	}

	return out
}

// RunCompositionSweep runs every class multiset of the given size at Seeds
// seeds each and returns the field, ranked by tension (#299 Q1): most
// close-calls first, among parties whose deaths stay under the ceiling.
//
// Runtime is the honest cost — compositions × seeds worlds. A party of 5 at
// the default 20 seeds is 420 full simulations, which is minutes, not the
// seconds `make balance` takes. That is why this is its own mode.
func RunCompositionSweep(cfg PartySimConfig, size int) CompositionReport {
	cfg = withSimDefaults(cfg, defaultCompositionSeeds)

	report := CompositionReport{Players: size}

	for _, comp := range PartyCompositions(size) {
		report.Rows = append(report.Rows, runComposition(cfg, comp))
	}

	rankByTension(report.Rows)
	report.Verdict = verdictFor(report.Rows)

	return report
}

// runComposition runs one composition at every seed and folds the runs into a
// spread per metric.
func runComposition(cfg PartySimConfig, comp []string) CompositionStats {
	runs := make([]SizeStats, 0, cfg.Seeds)

	for s := range cfg.Seeds {
		// A distinct seed namespace from "sim": these runs are not comparable
		// with size-sweep runs (different classes join, so rng diverges), and
		// sharing the namespace would invite reading one against the other.
		seed := deriveSeed(cfg.BaseSeed, "composition", compositionLabel(comp), strconv.Itoa(s))
		runCfg := cfg
		runCfg.Composition = comp
		runs = append(runs, runPartyWorld(runCfg, len(comp), seed))
	}

	return CompositionStats{
		Composition:  slices.Clone(comp),
		Players:      len(comp),
		Label:        compositionLabel(comp),
		DeathsPer100: spreadOf(runs, func(r SizeStats) float64 { return r.DeathsPer100 }),
		CloseCall:    spreadOf(runs, func(r SizeStats) float64 { return r.CloseCall }),
		CombatFrac:   spreadOf(runs, func(r SizeStats) float64 { return r.CombatFrac }),
		XPPerTurn:    spreadOf(runs, func(r SizeStats) float64 { return r.XPPerTurn }),
		Spread:       spreadOf(runs, func(r SizeStats) float64 { return r.Spread }),
	}
}

// spreadOf folds one metric across runs into mean/min/max.
func spreadOf(runs []SizeStats, pick func(SizeStats) float64) MetricSpread {
	if len(runs) == 0 {
		return MetricSpread{}
	}

	out := MetricSpread{Min: math.Inf(1), Max: math.Inf(-1)}
	sum := 0.0

	for _, r := range runs {
		v := pick(r)
		sum += v
		out.Min = math.Min(out.Min, v)
		out.Max = math.Max(out.Max, v)
	}

	out.Mean = sum / float64(len(runs))

	return out
}

// rankByTension sorts most-tense first (#299 Q1): among parties whose deaths
// stay under the ceiling, the LOWEST close-call is the most exciting — it
// means the party keeps nearly dying without dying.
//
// Parties over the ceiling sort last regardless of their close-call, because
// up there a low close-call means they are dying rather than surviving
// narrowly, and ranking them as "most tense" would invert the metric's
// meaning.
func rankByTension(rows []CompositionStats) {
	slices.SortFunc(rows, func(a, b CompositionStats) int {
		aOver, bOver := a.DeathsPer100.Mean > deathsCeiling, b.DeathsPer100.Mean > deathsCeiling
		if aOver != bOver {
			if aOver {
				return 1
			}

			return -1
		}

		if c := cmp.Compare(a.CloseCall.Mean, b.CloseCall.Mean); c != 0 {
			return c
		}

		// Stable tie-break so a table never reorders between identical runs.
		return cmp.Compare(a.Label, b.Label)
	})
}

// verdictFor answers the question the sweep exists for (#299 Q2): did anything
// actually separate from the field?
//
// "Separate" is measured against the field's OWN noise — the widest per-seed
// close-call range in the table — because a gap smaller than the spread it is
// drawn from is not a finding. Saying "nothing dominates" is the expected and
// perfectly good result, and it is stated plainly rather than left for the
// reader to infer from numbers that look different.
func verdictFor(rows []CompositionStats) string {
	const needTwoToCompare = 2
	if len(rows) < needTwoToCompare {
		return "not enough compositions to compare"
	}

	widest := 0.0
	for _, r := range rows {
		widest = math.Max(widest, r.CloseCall.width())
	}

	gap := rows[1].CloseCall.Mean - rows[0].CloseCall.Mean
	if widest > 0 && gap > widest*dominanceMargin {
		return "STANDOUT: " + rows[0].Label + " separates from the field by more than the per-seed spread — worth a look"
	}

	return "no composition dominates: the gap between the top two (" +
		strconv.FormatFloat(gap, 'f', 4, 64) +
		") is within the field's own per-seed spread (" +
		strconv.FormatFloat(widest, 'f', 4, 64) + ")"
}
