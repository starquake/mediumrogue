package game

import (
	mrand "math/rand/v2"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// percentBase is the denominator for the species percent knobs
// (ElfCritChancePercent, HumanXPBonusPercent): a "percent out of 100".
const percentBase = 100

// validSpecies reports whether species is one of the three playable species.
// Species is required at Join time for a new entity — there is no default.
func validSpecies(species string) bool {
	switch species {
	case protocol.SpeciesHuman, protocol.SpeciesElf, protocol.SpeciesDwarf:
		return true
	default:
		return false
	}
}

// critMultiplier rolls one crit for the attacker: an elf crits with
// ElfCritChancePercent chance for ElfCritMultiplier× damage; everyone else 1.
// Consumes rng only for an elf (so non-elf combat is RNG-identical to before).
func critMultiplier(attacker *entity, rng *mrand.Rand) int {
	if attacker.species == protocol.SpeciesElf && rng.IntN(percentBase) < protocol.ElfCritChancePercent {
		return protocol.ElfCritMultiplier
	}

	return 1
}

// applyDR reduces a hit dealt to a dwarf by DwarfDamageReduction, floored at 1
// (a hit always lands for something). Others take full damage.
func applyDR(victim *entity, dmg int) int {
	if victim.species == protocol.SpeciesDwarf {
		if r := dmg - protocol.DwarfDamageReduction; r > 1 {
			return r
		}

		return 1
	}

	return dmg
}
