// Command balance runs the balance-measurement harness (#283) and prints a
// human table plus (optionally) a machine-readable JSON report — the first
// artifact of the difficulty-tuning/analytics milestone. Report-first by
// design: this tool measures, the guardrail tests in internal/game assert
// only coarse extremes, and tuning decisions stay with the maintainer.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/starquake/mediumrogue/internal/game"
)

const (
	defaultDuels = 200
	exitUsage    = 2
	reportPerm   = 0o644
)

var errBadLevel = errors.New("bad level")

func main() {
	mode := flag.String("mode", "matrix", "matrix | deltas | sim | composition")
	seed := flag.Uint64("seed", 1, "base seed — same seed, same report, to the digit")
	duels := flag.Int("duels", defaultDuels, "duels per matchup cell")
	levels := flag.String("levels", "1,3,5", "comma-separated player levels")
	jsonPath := flag.String("json", "", "write the full report as JSON to this path")
	// composition mode (#299). Parties 1-5 only: by 10 almost nothing dies, so
	// there is no signal left for composition to move (the issue's own table).
	party := flag.Int("party", 0, "composition mode: one party size, or 0 to sweep 1-5")
	seeds := flag.Int("seeds", 0, "composition mode: seeds per composition (default 20)")

	flag.Parse()

	lv, err := parseLevels(*levels)
	if err != nil {
		fmt.Fprintln(os.Stderr, "balance:", err)
		os.Exit(exitUsage)
	}

	var writeErr error

	switch *mode {
	case "matrix":
		report := game.RunDuelMatrix(game.MatrixConfig{BaseSeed: *seed, Duels: *duels, Levels: lv})
		printMatrix(report)

		writeErr = writeJSON(*jsonPath, report)
	case "deltas":
		report := game.RunDeltas(game.DeltaConfig{BaseSeed: *seed, Duels: *duels, Levels: lv})
		printDeltas(report)

		writeErr = writeJSON(*jsonPath, report)
	case "sim":
		report := game.RunPartySim(game.PartySimConfig{BaseSeed: *seed})
		printSim(report)

		writeErr = writeJSON(*jsonPath, report)
	case "composition":
		reports := runCompositions(*seed, *party, *seeds)
		for _, r := range reports {
			printComposition(r)
		}

		writeErr = writeJSON(*jsonPath, reports)
	default:
		fmt.Fprintf(os.Stderr, "balance: unknown mode %q (matrix | deltas | sim | composition)\n", *mode)
		os.Exit(exitUsage)
	}

	// The report already printed to stdout; a failed JSON write still fails the
	// run, because -json is how CI captures it.
	if writeErr != nil {
		fmt.Fprintln(os.Stderr, "balance:", writeErr)
		os.Exit(1)
	}
}

func printDeltas(r game.DeltaReport) {
	outln("item/passive                   type      class    L   threat-delta  (negative = safer)")

	for _, d := range r.Rows {
		outf("%-30s %-9s %-8s %-3d %+.3f\n", d.ID, d.Kind, d.Class, d.Level, d.ThreatDelta)
	}
}

func parseLevels(s string) ([]int, error) {
	var out []int

	for part := range strings.SplitSeq(s, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 1 {
			return nil, fmt.Errorf("%w: %q", errBadLevel, part)
		}

		out = append(out, n)
	}

	return out, nil
}

// writeJSON returns its error rather than exiting, so the decision to stop the
// process stays in main where it is visible.
func writeJSON(path string, v any) error {
	if path == "" {
		return nil
	}

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	if err := os.WriteFile(path, append(data, '\n'), reportPerm); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	outln("\nJSON report:", path)

	return nil
}

func printMatrix(r game.MatrixReport) {
	outln("class    kind            L   win    draw  turns  hp%   dpsP   dpsM   threat")

	for _, c := range r.Cells {
		outf("%-8s %-15s %-3d %3d/%-3d %-5d %-6.1f %-5.2f %-6.2f %-6.2f %.2f\n",
			c.Class, c.Kind, c.Level, c.PlayerWins, c.Duels, c.Draws,
			c.MeanTurns, c.WinnerHPFrac, c.DPSPlayer, c.DPSMonster, c.Threat)
	}
}

func outln(args ...any) {
	_, _ = fmt.Fprintln(os.Stdout, args...)
}

func outf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stdout, format, args...)
}

func printSim(r game.PartySimReport) {
	outln("party  deaths/100t  close-call  combat%  xp/turn  spread")

	for _, s := range r.Sizes {
		outf("%-6d %-12.2f %-11.2f %-8.2f %-8.2f %.2f\n",
			s.Players, s.DeathsPer100, s.CloseCall, s.CombatFrac, s.XPPerTurn, s.Spread)
	}
}

// compositionSizes is the range the sweep defaults to (#299 Q4). Deliberately
// not 10 and 15: the issue's own measurements show deaths falling to 0.32 and
// 0.09 per 100 turns up there, so nothing dies and composition has nothing
// left to move. It is also the range a real group actually feels its choices.
//
//nolint:gochecknoglobals // fixed default, effectively const.
var compositionSizes = []int{1, 2, 3, 4, 5}

// runCompositions sweeps one party size, or all of 1-5.
func runCompositions(seed uint64, party, seeds int) []game.CompositionReport {
	sizes := compositionSizes
	if party > 0 {
		sizes = []int{party}
	}

	out := make([]game.CompositionReport, 0, len(sizes))
	for _, n := range sizes {
		out = append(out, game.RunCompositionSweep(game.PartySimConfig{BaseSeed: seed, Seeds: seeds}, n))
	}

	return out
}

// printComposition prints one size's field, most-tense first, with the range
// beside every mean — a bare mean is what would let the reader believe a
// ranking is sharper than its seeds support.
func printComposition(r game.CompositionReport) {
	outf("\n== party of %d — %d compositions, ranked by tension ==\n", r.Players, len(r.Rows))
	outln("mix       deaths/100t (min-max)   close-call (min-max)    xp/turn  combat%")

	for _, c := range r.Rows {
		outf("%-9s %-6.2f (%.2f-%.2f)%s %-6.3f (%.3f-%.3f)%s %-8.2f %.2f\n",
			c.Label,
			c.DeathsPer100.Mean, c.DeathsPer100.Min, c.DeathsPer100.Max, pad(c.DeathsPer100.Max),
			c.CloseCall.Mean, c.CloseCall.Min, c.CloseCall.Max, pad(c.CloseCall.Max),
			c.XPPerTurn.Mean, c.CombatFrac.Mean)
	}

	outf("\n%s\n", r.Verdict)
}

// singleDigitCeiling is where a printed number gains a character and the
// range column would otherwise jog left by one.
const singleDigitCeiling = 10

// pad keeps the range column aligned when a number loses a digit.
func pad(largest float64) string {
	if largest < singleDigitCeiling {
		return " "
	}

	return ""
}
