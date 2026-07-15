package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// Seeds pinned so the wolf's melee attack on the Rogue does / does not proc
// the glance card's chance condition — found by scanning seeds during
// implementation (the drops_test.go killDropSeed/killMissSeed pattern). If
// a change reorders rng consumption these move: re-derive by re-scanning,
// never by weakening the halved/full assertions.
const (
	glanceProcSeed = 1 // verified: wolf melee attack (base 3) resolves dealt=1 (glanced)
	glanceMissSeed = 2 // verified: wolf melee attack (base 3) resolves dealt=3 (full hit)
)

// glanceScenario drives one wolf melee attack into a stationary Rogue under seed
// and returns the player's HP loss: the wolf is placed adjacent, given the
// player's hex as its path, and resolved without AI
// (ResolveCombatOnlyForTest), so the take-damage fold — where the glance
// card rolls — is the only chance draw in the turn.
func glanceScenario(t *testing.T, seed int64) int {
	t.Helper()

	w := newWorld()
	w.SetSeedForTest(seed)

	me, err := w.Join("", "tester", protocol.ClassRogue, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	w.SetPathForTest(monsterID, []protocol.Hex{me.Hex})
	w.ResolveCombatOnlyForTest()

	player, ok := entityOfSnap(w.Snapshot(), me.EntityID)
	if !ok {
		t.Fatalf("player %d missing from snapshot", me.EntityID)
	}

	return protocol.RogueMaxHP - player.HP
}

// TestRogueGlanceHalvesDamage (#91): the Rogue's class passive gives a
// RogueGlanceChancePercent chance that an incoming hit is halved
// (GlanceDamagePercent), never negated. A wolf melee attack (base 3) lands 3 full
// or 3*50/100 = 1 glanced.
func TestRogueGlanceHalvesDamage(t *testing.T) {
	t.Parallel()

	full := game.MonsterDamageForTest("wolf")
	halved := full * protocol.GlanceDamagePercent / 100

	if got, want := glanceScenario(t, glanceMissSeed), full; got != want {
		t.Errorf("HP loss under glanceMissSeed = %d, want %d (full hit)", got, want)
	}

	if got, want := glanceScenario(t, glanceProcSeed), halved; got != want {
		t.Errorf("HP loss under glanceProcSeed = %d, want %d (glanced: halved)", got, want)
	}
}

// TestNonRogueTakesFullDamage: the glance card is class-gated — a Fighter
// victim's take-damage fold carries no chance card, so the same wolf melee attack
// always lands in full, on any seed.
func TestNonRogueTakesFullDamage(t *testing.T) {
	t.Parallel()

	for seed := int64(1); seed <= 5; seed++ {
		w := newWorld()
		w.SetSeedForTest(seed)

		me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
		if err != nil {
			t.Fatalf("Join: %v", err)
		}

		monsterHex := walkableNeighbor(t, w, me.Hex)
		monsterID := w.PlaceMonsterForTest(monsterHex)

		w.SetPathForTest(monsterID, []protocol.Hex{me.Hex})
		w.ResolveCombatOnlyForTest()

		player, ok := entityOfSnap(w.Snapshot(), me.EntityID)
		if !ok {
			t.Fatalf("player %d missing from snapshot (seed %d)", me.EntityID, seed)
		}

		if got, want := protocol.FighterMaxHP-player.HP, game.MonsterDamageForTest("wolf"); got != want {
			t.Errorf("seed %d: fighter HP loss = %d, want %d (no glance for non-rogues)", seed, got, want)
		}
	}
}
