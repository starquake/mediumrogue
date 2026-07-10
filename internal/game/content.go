package game

import "github.com/starquake/mediumrogue/internal/protocol"

// content.go is the content registry: the pipeline's rule cards land here.
// Species passives are the first rule set (numbers stay protocol constants:
// they're tuning knobs).
//
//nolint:gochecknoglobals // fixed rule-card tables, effectively const.
var (
	humanCards = []ruleCard{
		{event: evEarnXP, then: effect{kind: effMulPct, n: percentBase + protocol.HumanXPBonusPercent}},
	}
	elfCards = []ruleCard{
		{event: evDealDamage, when: []condition{{kind: condChance, n: protocol.ElfCritChancePercent}},
			then: effect{kind: effMulPct, n: percentBase * protocol.ElfCritMultiplier}},
	}
	dwarfCards = []ruleCard{
		{event: evTakeDamage, then: effect{kind: effAdd, n: -protocol.DwarfDamageReduction}},
	}
)

// speciesCards returns a species' passive rule cards (nil for monsters'
// empty species).
func speciesCards(species string) []ruleCard {
	switch species {
	case protocol.SpeciesHuman:
		return humanCards
	case protocol.SpeciesElf:
		return elfCards
	case protocol.SpeciesDwarf:
		return dwarfCards
	default:
		return nil
	}
}
