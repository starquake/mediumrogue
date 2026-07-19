package game //nolint:testpackage // white-box: renders unexported cards; see rules_test.go's file doc.

import (
	"strings"
	"testing"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestStatLinesForShippedContent renders every item in the LIVE registry and
// pins the result (#171). Pinning real content rather than synthetic cards is
// the point: these strings are what a player reads, so a change to a card's
// number or a renderer's phrasing should show up here as a diff to approve,
// not slip out silently.
func TestStatLinesForShippedContent(t *testing.T) {
	t.Parallel()

	// Repeated base-stat lines, named so the expectations below read as a
	// table of what CHANGES per item rather than a wall of duplicate strings.
	const (
		dmg3, dmg4, dmg6, dmg9 = "Damage 3", "Damage 4", "Damage 6", "Damage 9"
		rng4                   = "Range 4"
		aoe1                   = "AoE 1"
	)

	want := map[string][]string{
		// Weapons — the number is damage DEALT; the slot says so (Q1).
		idIronSword:             {dmg4},
		idButchersCleaver:       {dmg3, "+3 Damage vs Below 50% HP"},
		idVenomFang:             {dmg3, "+4 Damage vs Full HP"},
		idPackBow:               {dmg3, rng4, "+3 Damage with an Ally"},
		idEmberStaff:            {dmg6, rng4, aoe1, "×2 Damage vs Adjacent"},
		idAncientDwarvenMattock: {dmg4, "+3 Damage (Dwarf)"},
		idWarMageStaff:          {dmg6, rng4, aoe1, "×2 Damage vs Below 6 HP"},
		idWyrmslayerGreatsword:  {dmg9, "+50% Damage vs Dragons"},
		idMisericorde:           {dmg4, "15% chance ×2 Damage"},
		idFrostbrand:            {dmg4},
		// Wearables — the number is damage TAKEN, again by slot.
		idLeatherArmor:      {"−10% Damage"},
		idIronKiteShield:    {"−20% Damage"},
		idPilgrimsMantle:    {"−50% Chaos Damage"},
		idInfernalChainMail: {"−50% Fire Damage"},
		idWardedGambeson:    {"−50% Sharp Damage"},
		// Utility stats name their own subject and are exempt from the slot
		// rule (Q5) — "+5% XP" is good, "+25% Aggro Range" is not.
		idHeadbandOfLearning: {"+5% XP"},
		idPaddedBoots:        {"−25% Aggro Range"},
		idIronPlateArmor:     {"−20% Damage", "+25% Aggro Range"},
		// A consumable's heal is not a card at all (#175).
		idHealingPotion: {"+5 HP", "Stacks to 5"},
	}

	for id, wantLines := range want {
		t.Run(id, func(t *testing.T) {
			t.Parallel()

			def, ok := itemDefByID[id]
			if !ok {
				t.Fatalf("%s is not registered", id)
			}

			got := make([]string, 0, len(wantLines))
			for _, l := range statLinesFor(def) {
				got = append(got, l.text)
			}

			if strings.Join(got, " · ") != strings.Join(wantLines, " · ") {
				t.Errorf("stat lines =\n  %q\nwant\n  %q", got, wantLines)
			}
		})
	}
}

// TestDrawbackDetection (#171 Q6): sign alone cannot say whether a stat is
// good — "+25% Aggro Range" is bad and "+5% XP" is good — so the renderer
// decides per EVENT. Iron Plate Armor is the shipped case that proves it: one
// benefit and one drawback on the same item.
func TestDrawbackDetection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		card ruleCard
		want bool
	}{
		{name: "less damage taken is good", card: ruleCard{event: evTakeDamage,
			then: effect{kind: effMulPct, n: percentBase - 20}}},
		{name: "MORE damage taken is a drawback", card: ruleCard{event: evTakeDamage,
			then: effect{kind: effMulPct, n: percentBase + 20}}, want: true},
		{name: "more damage dealt is good", card: ruleCard{event: evDealDamage,
			then: effect{kind: effAdd, n: 3}}},
		{name: "LESS damage dealt is a drawback", card: ruleCard{event: evDealDamage,
			then: effect{kind: effAdd, n: -3}}, want: true},
		{name: "more XP is good", card: ruleCard{event: evEarnXP,
			then: effect{kind: effMulPct, n: percentBase + 5}}},
		{name: "less XP is a drawback", card: ruleCard{event: evEarnXP,
			then: effect{kind: effMulPct, n: percentBase - 5}}, want: true},
		{name: "smaller aggro range is good", card: ruleCard{event: evAggroRange,
			then: effect{kind: effMulPct, n: percentBase - 25}}},
		{name: "BIGGER aggro range is a drawback", card: ruleCard{event: evAggroRange,
			then: effect{kind: effMulPct, n: percentBase + 25}}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := isDrawback(tt.card); got != tt.want {
				t.Errorf("isDrawback = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestIronPlateCarriesABenefitAndADrawback pins the tradeoff item end to end:
// the whole reason drawbacks need their own flag is that this item exists.
func TestIronPlateCarriesABenefitAndADrawback(t *testing.T) {
	t.Parallel()

	lines := statLinesFor(itemDefByID[idIronPlateArmor])
	if len(lines) != 2 {
		t.Fatalf("iron plate rendered %d lines, want 2", len(lines))
	}

	if lines[0].drawback {
		t.Errorf("%q marked as a drawback, want benefit", lines[0].text)
	}

	if !lines[1].drawback {
		t.Errorf("%q not marked as a drawback — being noticed sooner IS the cost", lines[1].text)
	}
}

// TestSkillStatLines (#171): skills render through the same path as gear —
// the pipeline cannot tell their cards apart, and neither should the tooltip.
func TestSkillStatLines(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		skillCombatTraining: "+10% Melee Damage",
		skillWeakSpot:       "+4 Damage vs Full HP",
		skillScouting:       "−20% Aggro Range",
		skillShieldWall:     "15% chance −50% Damage with a Shield",
	}

	for id, wantLine := range want {
		t.Run(id, func(t *testing.T) {
			t.Parallel()

			def, ok := skillDefByID[id]
			if !ok {
				t.Fatalf("%s is not registered", id)
			}

			lines := statLinesFor(&itemDef{rules: def.rules})
			if len(lines) != 1 {
				t.Fatalf("%s rendered %d lines, want 1: %+v", id, len(lines), lines)
			}

			if got := lines[0].text; got != wantLine {
				t.Errorf("%s = %q, want %q", id, got, wantLine)
			}
		})
	}
}

// TestEveryShippedCardRenders is the completeness guard: every rule card in
// the registry produces a non-empty line with a real subject. A card whose
// condition the renderer does not know would otherwise render as a bare
// amount ("+3 ") and nobody would notice until it shipped.
func TestEveryShippedCardRenders(t *testing.T) {
	t.Parallel()

	check := func(owner string, cards []ruleCard) {
		for _, c := range cards {
			line := cardStatLine(c)
			if strings.TrimSpace(line.text) == "" {
				t.Errorf("%s: card %+v rendered an empty line", owner, c)
			}

			if strings.Contains(line.text, "  ") {
				t.Errorf("%s: card %+v rendered %q with a doubled space (missing fragment)", owner, c, line.text)
			}
		}
	}

	for _, def := range itemDefs {
		check("item:"+def.id, def.rules)
	}

	for _, def := range skillDefs {
		check("skill:"+def.id, def.rules)
	}

	for _, def := range monsterDefs {
		check("monster:"+def.id, def.rules)
	}

	check("species:human", speciesCards(protocol.SpeciesHuman))
	check("species:elf", speciesCards(protocol.SpeciesElf))
	check("species:dwarf", speciesCards(protocol.SpeciesDwarf))
	check("class:rogue", classCards(protocol.ClassRogue))
}
