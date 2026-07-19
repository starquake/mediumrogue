package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// ranged_monster_test.go (#179): the Kin Archer, the first monster that
// attacks without closing.
//
// This is the payoff of monster kinds NAMING a registry weapon instead of
// copying one. The old shorthand carried damage and damageType but never
// rangeHex, so every monster's attack profile was melee by construction — a
// ranged monster wasn't unimplemented, it was unrepresentable.

const kindArcher = "kin-archer"

// TestArcherShootsWithoutClosing: the archer damages a player from 3 hexes and
// does NOT move — the two halves that together mean "it used its range".
// Asserting only the damage would pass if it closed and bit; asserting only
// the position would pass if it stood still doing nothing.
func TestArcherShootsWithoutClosing(t *testing.T) {
	t.Parallel()

	w := newWorld()
	origin := protocol.Hex{Q: 0, R: 0}

	if !isWalkable(w, origin) {
		t.Skip("origin is not walkable on this map")
	}

	playerID, _ := w.PlaceEntityForTest(origin)

	// Three hexes: inside the archer's bow (reach 3), outside melee.
	at := walkableHexAtDistance(t, w, origin, 3, 3)
	clearSightLine(t, w, at, origin) // hold terrain constant; #95 sight is tested elsewhere

	archerID := w.PlaceMonsterKindForTest(at, kindArcher)

	full := entityHP(t, w.Snapshot(), playerID)

	snap := step(t, w)

	if got := entityHP(t, snap, playerID); got >= full {
		t.Errorf("player HP = %d, want less than %d — the archer should have shot", got, full)
	}

	// It shot from where it stood: no step toward the player.
	if got, want := entityHexIn(t, snap, archerID), at; got != want {
		t.Errorf("archer moved to %v, want it to hold %v and shoot", got, want)
	}
}

// TestArcherShootsAtPointBlankToo pins the maintainer's explicit call
// (2026-07-19): a ranged monster does NOT back off when a player closes.
//
// Backing off is unbeatable here rather than merely hard — every entity moves
// exactly one hex per turn, so a retreating archer could never be caught, and
// a melee player would eat an arrow every turn forever. That is a softlock,
// not a difficulty knob. Revisit only if #98 (multi-hex travel) lands.
func TestArcherShootsAtPointBlankToo(t *testing.T) {
	t.Parallel()

	w := newWorld()
	origin := protocol.Hex{Q: 0, R: 0}

	if !isWalkable(w, origin) {
		t.Skip("origin is not walkable on this map")
	}

	playerID, _ := w.PlaceEntityForTest(origin)

	adj := walkableHexAtDistance(t, w, origin, 1, 1)
	clearSightLine(t, w, adj, origin)

	archerID := w.PlaceMonsterKindForTest(adj, kindArcher)

	full := entityHP(t, w.Snapshot(), playerID)

	snap := step(t, w)

	if got := entityHP(t, snap, playerID); got >= full {
		t.Errorf("player HP = %d, want less than %d — the archer should still attack at range 1", got, full)
	}

	if got, want := entityHexIn(t, snap, archerID), adj; got != want {
		t.Errorf("archer at %v, want it to hold %v — it must not back off", got, want)
	}
}

// entityHexIn returns an entity's hex from a snapshot.
func entityHexIn(t *testing.T, snap protocol.TurnEvent, id int64) protocol.Hex {
	t.Helper()

	for _, e := range snap.Entities {
		if e.ID == id {
			return e.Hex
		}
	}

	t.Fatalf("entity %d missing from snapshot", id)

	return protocol.Hex{}
}
