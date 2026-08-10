package game //nolint:testpackage // white-box: reads the unexported item registry; see rules_test.go.

import (
	"strings"
	"testing"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// content_regen_test.go (#397): the evRegen fold needs a REGISTERED item on an
// entity, because equippedRuleCards resolves an instance through the registry —
// a state rules_test.go cannot reach with hand-built cards.

// wearing returns a player entity with defID equipped in slot.
func wearing(t *testing.T, slot, defID string) *entity {
	t.Helper()

	if _, ok := itemDefByID[defID]; !ok {
		t.Fatalf("item %q is not registered", defID)
	}

	return &entity{
		kind:     protocol.EntityPlayer,
		species:  protocol.SpeciesHuman,
		equipped: map[string]itemInstance{slot: {id: 1, defID: defID}},
	}
}

// TestMendersLocketRaisesRecovery is the end-to-end pin for #397: a registered
// item's card reaches the regen fold and changes what a player recovers.
//
// It asserts the DELTA against the base rate rather than a literal, so a
// future retune of protocol.RegenPerTurn moves both sides together instead of
// failing a test that was never about the base.
func TestMendersLocketRaisesRecovery(t *testing.T) {
	t.Parallel()

	bare := &entity{kind: protocol.EntityPlayer, species: protocol.SpeciesHuman}
	locket := wearing(t, protocol.SlotAmulet, idMendersLocket)

	if got, want := regenForLocked(bare), protocol.RegenPerTurn; got != want {
		t.Fatalf("ungeared regen = %d, want %d", got, want)
	}

	if got, want := regenForLocked(locket), protocol.RegenPerTurn+1; got != want {
		t.Errorf("Mender's Locket regen = %d, want %d", got, want)
	}
}

// TestRegenPercentageCardIsInertAtTheCurrentBase pins the reason the Locket
// uses effAdd rather than effMulPct, so nobody "fixes" it back.
//
// protocol.RegenPerTurn is 1 and the fold truncates, so every percentage below
// +100% floors straight back to 1: a "+25% recovery" card would validate,
// load, render in the designer guide, and do NOTHING. This test fails the day
// the base rate rises, which is exactly when percentage cards become
// expressible and this comment stops being true.
func TestRegenPercentageCardIsInertAtTheCurrentBase(t *testing.T) {
	t.Parallel()

	if protocol.RegenPerTurn != 1 {
		t.Skipf("base rate is now %d — percentages may be expressible; revisit the Locket's effAdd",
			protocol.RegenPerTurn)
	}

	quarter := []ruleCard{{event: evRegen, then: effect{kind: effMulPct, n: percentBase + 25}}}
	if got, want := applyRules(evRegen, protocol.RegenPerTurn, quarter, ruleCtx{}), protocol.RegenPerTurn; got != want {
		t.Errorf("+25%% regen card = %d, want %d (truncation makes it inert)", got, want)
	}

	double := []ruleCard{{event: evRegen, then: effect{kind: effMulPct, n: percentBase + 100}}}
	if got, want := applyRules(evRegen, protocol.RegenPerTurn, double, ruleCtx{}), 2; got != want {
		t.Errorf("+100%% regen card = %d, want %d", got, want)
	}
}

// TestMendersLocketStatLineNamesRecovery: a new EVENT that can appear on gear
// needs a fifth place updated beyond CLAUDE.md's documented four — the stat-line
// renderer (statlines.go). Without it, subjectText falls through to its
// "Damage" default and the Locket's +1 renders as "+1 Damage": not merely
// vague, but naming a stat the item does not touch.
//
// This is the same fall-through that shipped for evEndOfTurn in #271, where a
// fire flask's DoT rider read as "−3 Damage". Pinned here so the third event
// to land on gear does not repeat it.
func TestMendersLocketStatLineNamesRecovery(t *testing.T) {
	t.Parallel()

	def, ok := itemDefByID[idMendersLocket]
	if !ok {
		t.Fatalf("item %q is not registered", idMendersLocket)
	}

	lines := statLinesFor(def)
	if len(lines) == 0 {
		t.Fatal("Mender's Locket renders no stat line")
	}

	texts := make([]string, 0, len(lines))
	for _, l := range lines {
		texts = append(texts, l.text)
	}

	joined := strings.Join(texts, " ")

	if got, want := joined, "HP per turn"; !strings.Contains(got, want) {
		t.Errorf("stat line = %q, should contain %q", got, want)
	}

	if got, want := joined, "Damage"; strings.Contains(got, want) {
		t.Errorf("stat line = %q, must not name %q — the item does not touch it", got, want)
	}

	// More recovery is a BONUS, not a drawback — isDrawback must agree with
	// the noun, or the client renders the line in the wrong colour.
	for _, l := range lines {
		if l.drawback {
			t.Errorf("stat line %q is marked a drawback; +1 recovery is a bonus", l.text)
		}
	}
}
