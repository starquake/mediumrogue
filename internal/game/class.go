package game

import "github.com/starquake/mediumrogue/internal/protocol"

// The stat line a non-player entity or a test fixture gets when its class is
// empty or unrecognised. Spelled out rather than reusing the rogue constants
// so retuning the rogue does not silently retune every fixture: it is the
// rogue's value TODAY because the rogue is the middle line, not by definition.
const (
	fallbackMaxHP     = protocol.RogueMaxHP
	fallbackMaxEnergy = protocol.RogueMaxEnergy
)

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
		return fallbackMaxHP
	}
}

// baseMaxEnergy returns a class's level-1 energy pool (#322). Unknown classes
// fall back to the rogue's middle value for the same reason baseMaxHP does:
// only non-player entities and fixtures can reach it.
func baseMaxEnergy(class string) int {
	switch class {
	case protocol.ClassFighter:
		return protocol.FighterMaxEnergy
	case protocol.ClassRogue:
		return protocol.RogueMaxEnergy
	case protocol.ClassMage:
		return protocol.MageMaxEnergy
	default:
		return fallbackMaxEnergy
	}
}

// maxEnergyFor is the single source of truth for a class's energy pool at a
// given level: the class base plus a flat per-level gain.
func maxEnergyFor(class string, level int) int {
	return baseMaxEnergy(class) + protocol.EnergyPerLevel*(level-1)
}

// maxHPFor is the single source of truth for a class's max HP at a given
// level: the class base plus the front-loaded curve bonus (levelHPBonus).
// Used for spawn/respawn HP, level-up scaling, and the wire.
func maxHPFor(class string, level int) int {
	return baseMaxHP(class) + levelHPBonus(level)
}

// levelHPBonus is the cumulative max HP gained above level 1 under the
// front-loaded curve: the gain when advancing from level n is
// max(HPGainMin, HPGainBase-(n-1)). Loop, not closed form — levels are
// small and the loop reads as the rule.
func levelHPBonus(level int) int {
	bonus := 0

	for n := 1; n < level; n++ {
		bonus += max(protocol.HPGainBase-(n-1), protocol.HPGainMin)
	}

	return bonus
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
