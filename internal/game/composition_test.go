package game //nolint:testpackage // white-box: unexported classAt/validatePartySimConfig (see actives_test.go).

import (
	"errors"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// composition_test.go (#299): the party-composition axis.

// TestPartyCompositionsCounts pins the multiset counts. Three classes over N
// slots is C(N+2,2) — and the whole point of multisets is that this is far
// smaller than the 3^N permutations, because turn order is not a class
// property and running both orders would measure the same party twice.
func TestPartyCompositionsCounts(t *testing.T) {
	t.Parallel()

	for size, want := range map[int]int{1: 3, 2: 6, 3: 10, 4: 15, 5: 21} {
		if got := len(PartyCompositions(size)); got != want {
			t.Errorf("PartyCompositions(%d) = %d compositions, want %d", size, got, want)
		}
	}

	if got := PartyCompositions(0); got != nil {
		t.Errorf("PartyCompositions(0) = %v, want nil", got)
	}
}

// TestPartyCompositionsHaveNoPermutationDuplicates: the property the counts
// only imply. Two rows that are the same multiset in a different order would
// double a composition's weight in any ranking built on the table.
func TestPartyCompositionsHaveNoPermutationDuplicates(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool)

	for _, c := range PartyCompositions(5) {
		key := strings.Join(slices.Sorted(slices.Values(c)), ",")
		if seen[key] {
			t.Errorf("composition %v duplicates an earlier permutation", c)
		}

		seen[key] = true

		if len(c) != 5 {
			t.Errorf("composition %v has %d members, want 5", c, len(c))
		}
	}
}

// TestClassAtKeepsTheRoundRobinDefault is the pin that protects every existing
// report. A nil composition must reproduce the historical
// `classes[i%len(classes)]` EXACTLY — composition changes which classes join,
// which changes rng consumption, so any drift here silently moves numbers the
// guardrail tests are pinned to.
func TestClassAtKeepsTheRoundRobinDefault(t *testing.T) {
	t.Parallel()

	want := []string{
		protocol.ClassFighter, protocol.ClassRogue, protocol.ClassMage,
		protocol.ClassFighter, protocol.ClassRogue, protocol.ClassMage,
	}

	for i, w := range want {
		if got := classAt(nil, i); got != w {
			t.Errorf("classAt(nil, %d) = %q, want %q", i, got, w)
		}
	}
}

// TestClassAtUsesTheComposition: the other half.
func TestClassAtUsesTheComposition(t *testing.T) {
	t.Parallel()

	comp := []string{protocol.ClassMage, protocol.ClassMage, protocol.ClassFighter}

	for i, w := range comp {
		if got := classAt(comp, i); got != w {
			t.Errorf("classAt(comp, %d) = %q, want %q", i, got, w)
		}
	}
}

// TestValidatePartySimConfigRejectsAnUnknownClass: a typo'd class must fail
// loudly. Falling back to round-robin would report numbers for the DEFAULT
// party under the label of the one that was asked for — a wrong answer that
// looks like a right one.
func TestValidatePartySimConfigRejectsAnUnknownClass(t *testing.T) {
	t.Parallel()

	err := validatePartySimConfig(PartySimConfig{Composition: []string{protocol.ClassFighter, "warlock"}})
	if !errors.Is(err, ErrUnknownSimClass) {
		t.Errorf("err = %v, want %v", err, ErrUnknownSimClass)
	}

	if err := validatePartySimConfig(PartySimConfig{}); err != nil {
		t.Errorf("nil composition rejected: %v", err)
	}
}

// --- ranking and verdict (#299 tasks 4-5, 7) -------------------------------

func statsFor(label string, deaths, closeCall, ccMin, ccMax float64) CompositionStats {
	return CompositionStats{
		Label:        label,
		DeathsPer100: MetricSpread{Mean: deaths, Min: deaths, Max: deaths},
		CloseCall:    MetricSpread{Mean: closeCall, Min: ccMin, Max: ccMax},
	}
}

// TestRankByTensionPutsSqueakersFirst: lower close-call = nearer death without
// dying = more tension, which is the maintainer's chosen definition of "best"
// (#299 Q1).
func TestRankByTensionPutsSqueakersFirst(t *testing.T) {
	t.Parallel()

	rows := []CompositionStats{
		statsFor("3F", 1.0, 0.80, 0.7, 0.9),
		statsFor("1F/1R/1M", 1.0, 0.30, 0.2, 0.4),
		statsFor("3M", 1.0, 0.55, 0.5, 0.6),
	}

	rankByTension(rows)

	if got, want := rows[0].Label, "1F/1R/1M"; got != want {
		t.Errorf("most tense = %q, want %q", got, want)
	}
}

// TestRankByTensionSinksTheDying is the constraint half, and the reason the
// ceiling exists at all: a party that is being slaughtered ALSO has a very low
// close-call, so without the ceiling the ranking would crown the worst party
// in the table as the most exciting one.
func TestRankByTensionSinksTheDying(t *testing.T) {
	t.Parallel()

	rows := []CompositionStats{
		statsFor("survivors", 1.0, 0.40, 0.3, 0.5),
		statsFor("bloodbath", deathsCeiling+5, 0.05, 0.0, 0.1), // lowest close-call in the table
	}

	rankByTension(rows)

	if got, want := rows[0].Label, "survivors"; got != want {
		t.Errorf("top = %q, want %q — a dying party is not a tense one", got, want)
	}
}

// TestVerdictSaysNothingDominatesWhenTheGapIsNoise: the expected outcome, and
// the one the tool must state plainly rather than leave to be inferred from
// numbers that merely look different.
func TestVerdictSaysNothingDominatesWhenTheGapIsNoise(t *testing.T) {
	t.Parallel()

	// Gap between the top two is 0.01; the field's own per-seed spread is 0.20.
	rows := []CompositionStats{
		statsFor("a", 1.0, 0.40, 0.30, 0.50),
		statsFor("b", 1.0, 0.41, 0.31, 0.51),
	}

	if got := verdictFor(rows); !strings.Contains(got, "no composition dominates") {
		t.Errorf("verdict = %q, want it to report no dominance", got)
	}
}

// TestVerdictFlagsAGenuineStandout: the other branch, so the check is not
// vacuously always-negative.
func TestVerdictFlagsAGenuineStandout(t *testing.T) {
	t.Parallel()

	// Gap of 0.50 against a per-seed spread of 0.02.
	rows := []CompositionStats{
		statsFor("runaway", 1.0, 0.10, 0.09, 0.11),
		statsFor("rest", 1.0, 0.60, 0.59, 0.61),
	}

	got := verdictFor(rows)
	if !strings.Contains(got, "STANDOUT") || !strings.Contains(got, "runaway") {
		t.Errorf("verdict = %q, want a standout naming runaway", got)
	}
}

// TestCompositionLabelIsOrderIndependent: two equal multisets must render
// identically, or the same party appears twice in a table under two names.
func TestCompositionLabelIsOrderIndependent(t *testing.T) {
	t.Parallel()

	a := compositionLabel([]string{protocol.ClassMage, protocol.ClassFighter, protocol.ClassFighter})
	b := compositionLabel([]string{protocol.ClassFighter, protocol.ClassMage, protocol.ClassFighter})

	if a != b {
		t.Errorf("labels differ by order: %q vs %q", a, b)
	}

	if got, want := a, "2F/1M"; got != want {
		t.Errorf("label = %q, want %q", got, want)
	}
}

// TestSpreadOfCarriesTheRange: the mean alone is what would let a ranking
// invent precision it does not have (#299 decision 3).
func TestSpreadOfCarriesTheRange(t *testing.T) {
	t.Parallel()

	runs := []SizeStats{{CloseCall: 0.2}, {CloseCall: 0.6}, {CloseCall: 0.4}}
	got := spreadOf(runs, func(r SizeStats) float64 { return r.CloseCall })

	if got.Min != 0.2 || got.Max != 0.6 {
		t.Errorf("range = %v..%v, want 0.2..0.6", got.Min, got.Max)
	}

	if math.Abs(got.Mean-0.4) > 1e-9 {
		t.Errorf("mean = %v, want 0.4", got.Mean)
	}
}
