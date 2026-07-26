package game //nolint:testpackage // white-box: unexported classAt/validatePartySimConfig (see actives_test.go).

import (
	"errors"
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
