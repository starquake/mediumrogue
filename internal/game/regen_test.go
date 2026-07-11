package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// hpOfSnap scans a turn snapshot for an entity's current HP.
func hpOfSnap(t *testing.T, snap protocol.TurnEvent, id int64) int {
	t.Helper()

	for _, e := range snap.Entities {
		if e.ID == id {
			return e.HP
		}
	}

	t.Fatalf("entity %d not in snapshot", id)

	return -1
}

// TestRegenTicksOutOfCombatOnAWorldTurn is the wiring check: a real
// ResolveTurnForTest step (which drives resolveWorldTurnLocked, the actual
// production call site) heals a hurt, unbubbled solo player by exactly
// protocol.RegenPerTurn.
func TestRegenTicksOutOfCombatOnAWorldTurn(t *testing.T) {
	t.Parallel()

	w := newWorld()

	resp, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	hurt := protocol.FighterMaxHP - 5
	w.SetHPForTest(resp.EntityID, hurt)

	snap := step(t, w)

	if got, want := hpOfSnap(t, snap, resp.EntityID), hurt+protocol.RegenPerTurn; got != want {
		t.Errorf("hp after one world turn = %d, want %d (regen of %d)", got, want, protocol.RegenPerTurn)
	}
}

// TestRegenNoOverhealAtFullHP: a player already at max HP does not regen past it.
func TestRegenNoOverhealAtFullHP(t *testing.T) {
	t.Parallel()

	w := newWorld()

	id, _ := w.PlaceEntityForTest(protocol.Hex{Q: 1, R: 0})
	w.SetHPForTest(id, protocol.FighterMaxHP)

	w.RegenTickForTest()

	if got, want := entityHPForTest(t, w, id), protocol.FighterMaxHP; got != want {
		t.Errorf("hp = %d, want unchanged max %d", got, want)
	}
}

// TestRegenNoneInBubble: a player pinned into a (fake) combat bubble does not
// regen even though it's hurt and alive — being in a fight means no regen.
func TestRegenNoneInBubble(t *testing.T) {
	t.Parallel()

	w := newWorld()

	id, _ := w.PlaceEntityForTest(protocol.Hex{Q: 1, R: 0})
	hurt := protocol.FighterMaxHP - 5
	w.SetHPForTest(id, hurt)
	w.SetBubbleIDForTest(id, 1) // any non-zero bubble id takes it out of the world domain

	w.RegenTickForTest()

	if got, want := entityHPForTest(t, w, id), hurt; got != want {
		t.Errorf("hp = %d, want unchanged %d (bubbled players don't regen)", got, want)
	}
}

// TestRegenNoneForMonsters: monsters never regen, even hurt and out of combat.
func TestRegenNoneForMonsters(t *testing.T) {
	t.Parallel()

	w := newWorld()

	id := w.PlaceMonsterForTest(protocol.Hex{Q: 1, R: 0})
	hurt := protocol.MonsterMaxHP - 3
	w.SetHPForTest(id, hurt)

	w.RegenTickForTest()

	if got, want := entityHPForTest(t, w, id), hurt; got != want {
		t.Errorf("monster hp = %d, want unchanged %d (monsters don't regen)", got, want)
	}
}

// TestRegenNoneForDeadEntity: an entity at hp <= 0 does not regen (it's dead,
// not "hurt" — resolveDeathsLocked's respawn is the only path back to HP, and
// that lives outside regenPlayersLocked entirely, exercised via
// RegenTickForTest here so the respawn machinery never masks this assertion).
func TestRegenNoneForDeadEntity(t *testing.T) {
	t.Parallel()

	w := newWorld()

	id, _ := w.PlaceEntityForTest(protocol.Hex{Q: 1, R: 0})
	w.SetHPForTest(id, 0)

	w.RegenTickForTest()

	if got, want := entityHPForTest(t, w, id), 0; got != want {
		t.Errorf("dead entity hp = %d, want unchanged %d", got, want)
	}
}

// entityHPForTest scans the snapshot for an entity's current HP, failing the
// test if the entity is missing.
func entityHPForTest(t *testing.T, w *game.World, id int64) int {
	t.Helper()

	return hpOfSnap(t, w.Snapshot(), id)
}
