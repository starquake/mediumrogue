// Package game (white-box): exercises unexported rule-pipeline internals
// (ruleCard, applyRules, speciesCards) directly — there is no exported
// surface for these, by design (see rules.go).
//
//nolint:testpackage // see file doc above
package game

import (
	mrand "math/rand/v2"
	"slices"
	"testing"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// testRNG returns a deterministic rng for condition tests.
//
//nolint:gosec // deterministic test RNG, not security-sensitive.
func testRNG(seed uint64) *mrand.Rand { return mrand.New(mrand.NewPCG(seed, 0)) }

func TestApplyRulesFoldOrder(t *testing.T) {
	t.Parallel()

	// adds sum first, then multipliers, then the event clamp: (10+3-1)*200/100 = 24.
	// re-derived: additive fold (fast-lane T4, #61 p14) — only one mulPct
	// card here, so summed-delta and compounding agree (single truncation
	// either way); the value is unchanged by design (see
	// TestApplyRulesMulPctAddsNotCompounds for the two-card case that
	// differs: 121 compounding vs 120 additive).
	cards := []ruleCard{
		{event: evDealDamage, then: effect{kind: effAdd, n: 3}},
		{event: evDealDamage, then: effect{kind: effMulPct, n: 200}},
		{event: evDealDamage, then: effect{kind: effAdd, n: -1}},
		{event: evTakeDamage, then: effect{kind: effAdd, n: 100}}, // wrong event: ignored
	}
	if got, want := applyRules(evDealDamage, 10, cards, ruleCtx{}), 24; got != want {
		t.Errorf("applyRules = %d, want %d", got, want)
	}
}

func TestApplyRulesTakeDamageFloorsAtOne(t *testing.T) {
	t.Parallel()

	cards := []ruleCard{{event: evTakeDamage, then: effect{kind: effAdd, n: -10}}}
	if got, want := applyRules(evTakeDamage, 2, cards, ruleCtx{}), 1; got != want {
		t.Errorf("applyRules take-damage = %d, want floor %d", got, want)
	}
}

// TestApplyRulesAggroRangeFloorsAtOne (#88): the aggro-range fold carries the
// same ≥1 floor take-damage does. Every noticeability card shipped today is
// multiplicative and cannot go negative, but a future negative-`add` card
// could fold a radius to 0 or below — a monster that never notices anyone,
// discovered mid-playtest. One clamp now, no surprise later.
func TestApplyRulesAggroRangeFloorsAtOne(t *testing.T) {
	t.Parallel()

	cards := []ruleCard{{event: evAggroRange, then: effect{kind: effAdd, n: -999}}}
	if got, want := applyRules(evAggroRange, 10, cards, ruleCtx{}), 1; got != want {
		t.Errorf("applyRules aggro-range = %d, want floor %d", got, want)
	}
}

func TestSpeciesCardsMatchOldPassives(t *testing.T) {
	t.Parallel()

	// Dwarf: flat reduction, floored at 1 — the old applyDR numbers.
	dwarf := speciesCards(protocol.SpeciesDwarf)
	if got, want := applyRules(evTakeDamage, 5, dwarf, ruleCtx{}), 5-protocol.DwarfDamageReduction; got != want {
		t.Errorf("dwarf take-damage(5) = %d, want %d", got, want)
	}

	if got, want := applyRules(evTakeDamage, 1, dwarf, ruleCtx{}), 1; got != want {
		t.Errorf("dwarf take-damage(1) = %d, want %d", got, want)
	}

	// Human: the old award*(100+bonus)/100. wolf.xp (20) stands in for an
	// arbitrary representative kill-XP base — any base proves the fold.
	human := speciesCards(protocol.SpeciesHuman)
	wolfXP := monsterDefByID[idKindWolf].xp

	if got, want := applyRules(evEarnXP, wolfXP, human, ruleCtx{}),
		wolfXP*(percentBase+protocol.HumanXPBonusPercent)/percentBase; got != want {
		t.Errorf("human earn-xp = %d, want %d", got, want)
	}

	// Elf: with a chance card, both branches must be reachable across seeds.
	elf := speciesCards(protocol.SpeciesElf)
	crit, plain := false, false

	for seed := range uint64(100) {
		got := applyRules(evDealDamage, 4, elf, ruleCtx{rng: testRNG(seed)})
		switch got {
		case 4:
			plain = true
		case 4 * protocol.ElfCritMultiplier:
			crit = true
		default:
			t.Fatalf("elf deal-damage = %d, want 4 or %d", got, 4*protocol.ElfCritMultiplier)
		}
	}

	if !crit || !plain {
		t.Errorf("elf chance card: crit seen %v, plain seen %v, want both", crit, plain)
	}
}

func TestApplyRulesConditions(t *testing.T) {
	t.Parallel()

	halfDead := &entity{hp: 4, maxHP: 10}
	fresh := &entity{hp: 10, maxHP: 10}
	card := func(c condition) []ruleCard {
		return []ruleCard{{event: evDealDamage, when: []condition{c}, then: effect{kind: effAdd, n: 3}}}
	}

	belowPct := card(condition{kind: condTargetHPBelowPct, n: 50})
	if got, want := applyRules(evDealDamage, 1, belowPct, ruleCtx{victim: halfDead}), 4; got != want {
		t.Errorf("targetHPBelowPct(50) vs 4/10 = %d, want %d", got, want)
	}

	if got, want := applyRules(evDealDamage, 1, belowPct, ruleCtx{victim: fresh}), 1; got != want {
		t.Errorf("targetHPBelowPct(50) vs 10/10 = %d, want %d", got, want)
	}

	full := card(condition{kind: condTargetHPFull})
	if got, want := applyRules(evDealDamage, 1, full, ruleCtx{victim: fresh}), 4; got != want {
		t.Errorf("targetHPFull vs 10/10 = %d, want %d", got, want)
	}

	adj := ruleCtx{attacker: &entity{hex: protocol.Hex{Q: 0, R: 0}}, victim: &entity{hex: protocol.Hex{Q: 1, R: 0}}}

	adjacent := card(condition{kind: condTargetAdjacent})
	if got, want := applyRules(evDealDamage, 1, adjacent, adj), 4; got != want {
		t.Errorf("targetAdjacent at distance 1 = %d, want %d", got, want)
	}

	ally := card(condition{kind: condAllyInBubble})
	if got, want := applyRules(evDealDamage, 1, ally, ruleCtx{allyInBubble: true}), 4; got != want {
		t.Errorf("allyInBubble = %d, want %d", got, want)
	}
}

// TestApplyRulesTargetHPBelowFlat: the flat-threshold execute condition (the
// Staff of the War Mage's rule) compares against ABSOLUTE hp, deliberately
// not scaling with maxHP — a mop-up tool stays a mop-up tool against big
// monsters (designer decision, first-gear review v2).
func TestApplyRulesTargetHPBelowFlat(t *testing.T) {
	t.Parallel()

	card := []ruleCard{{
		event: evDealDamage,
		when:  []condition{{kind: condTargetHPBelowFlat, n: 6}},
		then:  effect{kind: effMulPct, n: 200},
	}}

	// hp 5 < 6: holds regardless of a huge maxHP.
	weak := &entity{hp: 5, maxHP: 200}
	if got, want := applyRules(evDealDamage, 3, card, ruleCtx{victim: weak}), 6; got != want {
		t.Errorf("applyRules vs hp 5 = %d, want %d", got, want)
	}

	// hp 6 is NOT below 6: boundary excluded.
	boundary := &entity{hp: 6, maxHP: 10}
	if got, want := applyRules(evDealDamage, 3, card, ruleCtx{victim: boundary}), 3; got != want {
		t.Errorf("applyRules vs hp 6 = %d, want %d", got, want)
	}

	// nil victim: fails closed.
	if got, want := applyRules(evDealDamage, 3, card, ruleCtx{}), 3; got != want {
		t.Errorf("applyRules vs nil victim = %d, want %d", got, want)
	}
}

// TestApplyRulesAggroRange (#36): proves the evAggroRange event works through
// the generic pipeline exactly like any other event — a rule card gated on
// the evaluated player's own species (ctx.attacker, per aggroRadiusForLocked's
// convention in world.go) shrinks or grows how far away a monster notices
// them. No content defines such a card yet (this is the whitebox
// pipeline-level proof the hook works, ahead of any gear using it).
func TestApplyRulesAggroRange(t *testing.T) {
	t.Parallel()

	// A hypothetical "sneaky" card: elves are 3 hexes less noticeable.
	sneaky := []ruleCard{{
		event: evAggroRange,
		when:  []condition{{kind: condAttackerSpecies, s: protocol.SpeciesElf}},
		then:  effect{kind: effAdd, n: -3},
	}}

	elf := &entity{species: protocol.SpeciesElf}
	if got, want := applyRules(evAggroRange, protocol.MonsterAggroRadius, sneaky, ruleCtx{attacker: elf}),
		protocol.MonsterAggroRadius-3; got != want {
		t.Errorf("aggro radius for elf with sneaky card = %d, want %d", got, want)
	}

	dwarf := &entity{species: protocol.SpeciesDwarf}
	if got, want := applyRules(evAggroRange, protocol.MonsterAggroRadius, sneaky, ruleCtx{attacker: dwarf}),
		protocol.MonsterAggroRadius; got != want {
		t.Errorf("aggro radius for dwarf (card does not apply) = %d, want unchanged %d", got, want)
	}

	// No cards at all: the default radius passes through unchanged.
	if got, want := applyRules(evAggroRange, protocol.MonsterAggroRadius, nil, ruleCtx{attacker: elf}),
		protocol.MonsterAggroRadius; got != want {
		t.Errorf("aggro radius with no cards = %d, want unchanged default %d", got, want)
	}
}

// TestApplyRulesAttackerSpecies: the species-gated condition (the Ancient
// Dwarven Mattock's rule) reads the ATTACKER's species — gear that is
// usable by a whole class but sings in one species' hands.
func TestApplyRulesAttackerSpecies(t *testing.T) {
	t.Parallel()

	card := []ruleCard{{
		event: evDealDamage,
		when:  []condition{{kind: condAttackerSpecies, s: protocol.SpeciesDwarf}},
		then:  effect{kind: effAdd, n: 3},
	}}

	dwarf := &entity{species: protocol.SpeciesDwarf}
	if got, want := applyRules(evDealDamage, 4, card, ruleCtx{attacker: dwarf}), 7; got != want {
		t.Errorf("applyRules dwarf attacker = %d, want %d", got, want)
	}

	elf := &entity{species: protocol.SpeciesElf}
	if got, want := applyRules(evDealDamage, 4, card, ruleCtx{attacker: elf}), 4; got != want {
		t.Errorf("applyRules elf attacker = %d, want %d", got, want)
	}

	// nil attacker (defensive): fails closed.
	if got, want := applyRules(evDealDamage, 4, card, ruleCtx{}), 4; got != want {
		t.Errorf("applyRules nil attacker = %d, want %d", got, want)
	}
}

// TestApplyRulesTargetKind: the Wyrmslayer Greatsword's condition (milestone
// 6c) — gates on the VICTIM being a monster of a specific registered kind.
func TestApplyRulesTargetKind(t *testing.T) {
	t.Parallel()

	card := []ruleCard{{
		event: evDealDamage,
		when:  []condition{{kind: condTargetKind, s: idKindDragon}},
		then:  effect{kind: effMulPct, n: 150},
	}}

	dragon := &entity{kind: protocol.EntityMonster, monsterKind: idKindDragon}
	if got, want := applyRules(evDealDamage, 4, card, ruleCtx{victim: dragon}), 6; got != want {
		t.Errorf("applyRules vs dragon = %d, want %d", got, want)
	}

	wolf := &entity{kind: protocol.EntityMonster, monsterKind: idKindWolf}
	if got, want := applyRules(evDealDamage, 4, card, ruleCtx{victim: wolf}), 4; got != want {
		t.Errorf("applyRules vs wolf (wrong kind) = %d, want %d", got, want)
	}

	player := &entity{kind: protocol.EntityPlayer}
	if got, want := applyRules(evDealDamage, 4, card, ruleCtx{victim: player}), 4; got != want {
		t.Errorf("applyRules vs a player victim = %d, want %d", got, want)
	}

	// nil victim (defensive): fails closed.
	if got, want := applyRules(evDealDamage, 4, card, ruleCtx{}), 4; got != want {
		t.Errorf("applyRules nil victim = %d, want %d", got, want)
	}
}

func TestApplyRulesMulPctAddsNotCompounds(t *testing.T) {
	t.Parallel()

	cards := []ruleCard{
		{event: evDealDamage, then: effect{kind: effMulPct, n: 110}},
		{event: evDealDamage, then: effect{kind: effMulPct, n: 110}},
	}

	// Two +10% cards on base 100: additive = 120, compounding would be 121.
	if got, want := applyRules(evDealDamage, 100, cards, ruleCtx{}), 120; got != want {
		t.Errorf("two +10%% cards on 100 = %d, want %d (add, not compound)", got, want)
	}
}

func TestApplyRulesMulPctOrderIndependent(t *testing.T) {
	t.Parallel()

	a := []ruleCard{
		{event: evDealDamage, then: effect{kind: effMulPct, n: 150}},
		{event: evDealDamage, then: effect{kind: effMulPct, n: 200}},
	}
	b := []ruleCard{a[1], a[0]}

	if got, want := applyRules(evDealDamage, 3, a, ruleCtx{}), applyRules(evDealDamage, 3, b, ruleCtx{}); got != want {
		t.Errorf("fold is order-dependent: %d vs %d", got, want)
	}

	// +50% and +100% on base 3: 3 * 250 / 100 = 7 (single truncation).
	if got, want := applyRules(evDealDamage, 3, a, ruleCtx{}), 7; got != want {
		t.Errorf("stacked mults on 3 = %d, want %d", got, want)
	}
}

func TestApplyRulesMulPctNegativeDeltaAndFloor(t *testing.T) {
	t.Parallel()

	// -50% and +20% = -30%: base 10 -> 7.
	mixed := []ruleCard{
		{event: evDealDamage, then: effect{kind: effMulPct, n: 50}},
		{event: evDealDamage, then: effect{kind: effMulPct, n: 120}},
	}
	if got, want := applyRules(evDealDamage, 10, mixed, ruleCtx{}), 7; got != want {
		t.Errorf("mixed deltas on 10 = %d, want %d", got, want)
	}

	// Sum of deltas <= -100% floors at 0 (deal-damage has no 1-floor).
	kill := []ruleCard{
		{event: evDealDamage, then: effect{kind: effMulPct, n: 0}},
	}
	if got, want := applyRules(evDealDamage, 10, kill, ruleCtx{}), 0; got != want {
		t.Errorf("-100%% on 10 = %d, want %d", got, want)
	}
}

func TestApplyRulesMulPctStackedDeltasClampAtZero(t *testing.T) {
	t.Parallel()

	// Two n:40 cards: deltas (40-100)+(40-100) = -120, so the pre-clamp
	// product 10*(100-120)/100 = -2 goes NEGATIVE — the max(...,0) clamp's
	// protective branch, which the single n:0 case never reaches.
	cards := []ruleCard{
		{event: evDealDamage, then: effect{kind: effMulPct, n: 40}},
		{event: evDealDamage, then: effect{kind: effMulPct, n: 40}},
	}

	if got, want := applyRules(evDealDamage, 10, cards, ruleCtx{}), 0; got != want {
		t.Errorf("stacked deltas below -100%% on 10 = %d, want %d", got, want)
	}
}

// TestApplyRulesTracedChanceMultipliers (#114): the traced fold reports
// which CHANCE-conditioned multiplier cards fired — a boost (>100%: a crit
// when the fold is deal-damage) and a reduction (<100%: a glance when the
// fold is take-damage) — and stays silent for deterministic multipliers,
// chance cards that do not fire, and chance-conditioned adds. Chance 100/0
// makes the outcomes seed-independent; the traced value must always equal
// the untraced fold's (applyRules is a thin wrapper over the traced fold).
func TestApplyRulesTracedChanceMultipliers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		card       ruleCard
		wantBoost  bool
		wantReduce bool
	}{
		{
			name: "chance boost fires",
			card: ruleCard{event: evDealDamage, when: []condition{{kind: condChance, n: 100}},
				then: effect{kind: effMulPct, n: 200}},
			wantBoost: true,
		},
		{
			name: "chance reduction fires",
			card: ruleCard{event: evDealDamage, when: []condition{{kind: condChance, n: 100}},
				then: effect{kind: effMulPct, n: 50}},
			wantReduce: true,
		},
		{
			name: "chance card that does not fire",
			card: ruleCard{event: evDealDamage, when: []condition{{kind: condChance, n: 0}},
				then: effect{kind: effMulPct, n: 200}},
		},
		{
			name: "deterministic multiplier is not a moment",
			card: ruleCard{event: evDealDamage, then: effect{kind: effMulPct, n: 200}},
		},
		{
			name: "chance-conditioned add is not a moment",
			card: ruleCard{event: evDealDamage, when: []condition{{kind: condChance, n: 100}},
				then: effect{kind: effAdd, n: 3}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := ruleCtx{rng: testRNG(1)}
			v, trace := applyRulesTraced(evDealDamage, 10, []ruleCard{tt.card}, ctx)

			if got, want := trace.boostFired, tt.wantBoost; got != want {
				t.Errorf("boostFired = %v, want %v", got, want)
			}

			if got, want := trace.reduceFired, tt.wantReduce; got != want {
				t.Errorf("reduceFired = %v, want %v", got, want)
			}

			if got, want := v, applyRules(evDealDamage, 10, []ruleCard{tt.card}, ruleCtx{rng: testRNG(1)}); got != want {
				t.Errorf("traced value = %d, want the untraced fold's %d", got, want)
			}
		})
	}
}

// TestAggroRadiusForNoticeabilityGear (#88): the two noticeability items fold
// over a monster kind's OWN radius, white-box through the same function the
// AI's notice check calls. Wolf's protocol.MonsterAggroRadius (10) is the
// base: Padded Boots (×0.75) read 7, Iron Plate Armor (×1.25) read 12. The
// fold is multiplicative precisely so a rat's short radius and a dragon's
// long one both move by a quarter instead of being flattened to one number.
func TestAggroRadiusForNoticeabilityGear(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		slot  string
		defID string
		want  int
	}{
		{name: "bare", want: protocol.MonsterAggroRadius},
		{name: "padded boots", slot: protocol.ItemTypeBoots, defID: idPaddedBoots, want: 7},
		{name: "iron plate armor", slot: protocol.ItemTypeChest, defID: idIronPlateArmor, want: 12},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &entity{species: protocol.SpeciesHuman, equipped: map[string]itemInstance{}}
			if tt.defID != "" {
				p.equipped[tt.slot] = itemInstance{id: 1, defID: tt.defID}
			}

			got := aggroRadiusForLocked(testRNG(1), protocol.MonsterAggroRadius, p)
			if want := tt.want; got != want {
				t.Errorf("aggroRadiusForLocked = %d, want %d", got, want)
			}
		})
	}
}

// TestCondDamageTypeHolds (#92): the condition every resistance and
// vulnerability card uses reads the type of the attack in flight — and holds
// for nothing when no attack is in flight (an earn-XP or aggro-range fold,
// whose ctx carries no type).
func TestCondDamageTypeHolds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		incoming string
		card     string
		want     bool
	}{
		{name: "same type", incoming: protocol.DamageTypeFire, card: protocol.DamageTypeFire, want: true},
		{name: "different type", incoming: protocol.DamageTypeIce, card: protocol.DamageTypeFire},
		{name: "no attack in flight", card: protocol.DamageTypeFire},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := ruleCtx{damageType: tt.incoming}
			if got, want := conditionHolds(condition{kind: condDamageType, s: tt.card}, ctx), tt.want; got != want {
				t.Errorf("condDamageType holds = %v, want %v", got, want)
			}
		})
	}
}

// TestFireResistCardHalvesOnlyFire (#92): a resist card is an ordinary
// take-damage rule card gated on the incoming type — it folds against a fire
// hit and is inert against every other type. Decoupled: it asks what type is
// landing, never who beats whom.
func TestFireResistCardHalvesOnlyFire(t *testing.T) {
	t.Parallel()

	resist := []ruleCard{{
		event: evTakeDamage,
		when:  []condition{{kind: condDamageType, s: protocol.DamageTypeFire}},
		then:  effect{kind: effMulPct, n: 50},
	}}

	if got, want := applyRules(evTakeDamage, 10, resist, ruleCtx{damageType: protocol.DamageTypeFire}), 5; got != want {
		t.Errorf("fire hit through fire resist = %d, want %d", got, want)
	}

	sharp := ruleCtx{damageType: protocol.DamageTypeSharp}
	if got, want := applyRules(evTakeDamage, 10, resist, sharp), 10; got != want {
		t.Errorf("sharp hit through fire resist = %d, want %d (inert)", got, want)
	}
}

// TestPercentMitigationStacksWithoutFlattening (#154) is the regression guard
// for the reason mitigation went percentage at all. Flat −N reductions summed
// straight into applyRules' ≥1 clamp: a dwarf in Iron Plate with an Iron Kite
// Shield took −5, which flattened EVERY monster up to a troll (6 damage) to
// the floor of 1, and made each extra piece of armour worth less than the
// last. Percentages scale with the hit instead, so a big hit stays big.
//
// The dwarf's flat −1 deliberately survives (@starquake, #154: "dwarf passive
// changes too = no") — a species trait is the one place a small always-on
// flat effect is defensible, and it is folded in the ADD phase before the
// percentages, exactly as the pipeline documents.
func TestPercentMitigationStacksWithoutFlattening(t *testing.T) {
	t.Parallel()

	cards := slices.Concat(
		speciesCards(protocol.SpeciesDwarf),
		itemDefByID[idIronPlateArmor].rules,
		itemDefByID[idIronKiteShield].rules,
	)

	// (raw − 1) × (1 − 0.20 − 0.20), truncated, floored at 1 — percent deltas
	// ADD within one fold (#61 principle 14), never compound.
	tests := []struct {
		kind string
		want int
	}{
		{kind: idKindRat, want: 1},    // 1 → floor
		{kind: idKindWolf, want: 1},   // (3−1)×0.6 = 1
		{kind: idKindGhoul, want: 1},  // (4−1)×0.6 = 1
		{kind: idKindTroll, want: 3},  // (6−1)×0.6 = 3 — was 1 under flat −5
		{kind: idKindDragon, want: 4}, // (9−1)×0.6 = 4
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			t.Parallel()

			raw := monsterDefByID[tt.kind].damage
			if got, want := applyRules(evTakeDamage, raw, cards, ruleCtx{}), tt.want; got != want {
				t.Errorf("%s hit of %d through dwarf+plate+kite = %d, want %d", tt.kind, raw, got, want)
			}
		})
	}

	// The point, stated as an assertion rather than a comment: a troll still
	// hurts more than a wolf through the same armour. Under flat mitigation
	// both landed for exactly 1.
	wolf := applyRules(evTakeDamage, monsterDefByID[idKindWolf].damage, cards, ruleCtx{})
	troll := applyRules(evTakeDamage, monsterDefByID[idKindTroll].damage, cards, ruleCtx{})

	if troll <= wolf {
		t.Errorf("troll hit %d is not greater than wolf hit %d — mitigation is flattening again", troll, wolf)
	}
}
