package game

import "github.com/starquake/mediumrogue/internal/protocol"

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
