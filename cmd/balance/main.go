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
	mode := flag.String("mode", "matrix", "matrix | deltas | sim")
	seed := flag.Uint64("seed", 1, "base seed — same seed, same report, to the digit")
	duels := flag.Int("duels", defaultDuels, "duels per matchup cell")
	levels := flag.String("levels", "1,3,5", "comma-separated player levels")
	jsonPath := flag.String("json", "", "write the full report as JSON to this path")

	flag.Parse()

	lv, err := parseLevels(*levels)
	if err != nil {
		fmt.Fprintln(os.Stderr, "balance:", err)
		os.Exit(exitUsage)
	}

	switch *mode {
	case "matrix":
		report := game.RunDuelMatrix(game.MatrixConfig{BaseSeed: *seed, Duels: *duels, Levels: lv})
		printMatrix(report)
		writeJSON(*jsonPath, report)
	case "deltas":
		report := game.RunDeltas(game.DeltaConfig{BaseSeed: *seed, Duels: *duels, Levels: lv})
		printDeltas(report)
		writeJSON(*jsonPath, report)
	case "sim":
		report := game.RunPartySim(game.PartySimConfig{BaseSeed: *seed})
		printSim(report)
		writeJSON(*jsonPath, report)
	default:
		fmt.Fprintf(os.Stderr, "balance: unknown mode %q (matrix | deltas | sim)\n", *mode)
		os.Exit(exitUsage)
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

func writeJSON(path string, v any) {
	if path == "" {
		return
	}

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "balance: marshal:", err)
		os.Exit(1)
	}

	if err := os.WriteFile(path, append(data, '\n'), reportPerm); err != nil {
		fmt.Fprintln(os.Stderr, "balance: write:", err)
		os.Exit(1)
	}

	outln("\nJSON report:", path)
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
