package game

import "github.com/starquake/mediumrogue/internal/protocol"

// weapon is an equipped weapon's combat profile. rangeHex 0 means the weapon is
// melee/adjacent; aoeRadius 0 means it is single-target. In milestone 6b.2 the
// two slots (close + ranged) are filled from class defaults (closeWeapon /
// rangedWeapon); the gear system that fills them from an inventory is a later
// slice.
type weapon struct {
	damage    int // base (level-1) damage
	rangeHex  int // 0 = melee/adjacent
	aoeRadius int // 0 = single target
}

// closeWeapon returns a class's default close (bump) weapon. An empty or unknown
// class falls back to fists — the unarmed default that guards a non-player
// entity (monsters have no class) since a joined player's class is always one
// of the three valid ones (enforced by Join, see validClass).
func closeWeapon(class string) weapon {
	switch class {
	case protocol.ClassFighter:
		return weapon{damage: protocol.SwordDamage}
	case protocol.ClassRogue:
		return weapon{damage: protocol.DaggerDamage}
	case protocol.ClassMage:
		return weapon{damage: protocol.StaffBonkDamage}
	default:
		return weapon{damage: protocol.FistsDamage}
	}
}

// rangedWeapon returns a class's default ranged weapon and whether it has one.
// Fighter (and any other class) has no ranged attack, reported by the false
// second return.
func rangedWeapon(class string) (weapon, bool) {
	switch class {
	case protocol.ClassRogue:
		return weapon{damage: protocol.BowDamage, rangeHex: protocol.BowRange}, true
	case protocol.ClassMage:
		return weapon{
			damage:    protocol.StaffMagicDamage,
			rangeHex:  protocol.MageRange,
			aoeRadius: protocol.MageAoERadius,
		}, true
	default:
		return weapon{}, false
	}
}

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

// weaponDamage is the single source of truth for a weapon's damage at a given
// level: the weapon base plus DamagePerLevel for each level above 1. Used by the
// melee and ranged combat paths (Tasks 3/4).
func weaponDamage(w weapon, level int) int {
	return w.damage + protocol.DamagePerLevel*(level-1)
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
