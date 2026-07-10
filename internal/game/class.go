package game

import "github.com/starquake/mediumrogue/internal/protocol"

// baseMaxHP returns a class's level-1 max HP. An empty or unknown class falls
// back to RogueMaxHP (the squishy baseline); a joined player's class is always
// valid (enforced by Join, see validClass), so this fallback only guards
// non-player entities and test fixtures.
func baseMaxHP(class string) int {
	switch class {
	case protocol.ClassFighter:
		return protocol.FighterMaxHP
	case protocol.ClassRogue:
		return protocol.RogueMaxHP
	case protocol.ClassMage:
		return protocol.MageMaxHP
	default:
		return protocol.RogueMaxHP
	}
}

// maxHPFor is the single source of truth for a class's max HP at a given level:
// the class base plus HPPerLevel for each level above 1. Used for spawn/respawn
// HP, level-up scaling, and the wire.
func maxHPFor(class string, level int) int {
	return baseMaxHP(class) + protocol.HPPerLevel*(level-1)
}

// validClass reports whether class is one of the three playable classes.
// Class is required at Join time for a new entity — there is no default.
func validClass(class string) bool {
	switch class {
	case protocol.ClassFighter, protocol.ClassRogue, protocol.ClassMage:
		return true
	default:
		return false
	}
}
