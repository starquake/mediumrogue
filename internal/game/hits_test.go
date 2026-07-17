package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// Seeds pinned so the elf Fighter's melee swing on the wolf does / does not
// proc the elf-crit card's chance condition — found by scanning seeds during
// implementation (the glance_test.go glanceProcSeed/glanceMissSeed pattern).
// If a change reorders rng consumption these move: re-derive by re-scanning,
// never by weakening the crit/amount assertions.
const (
	critProcSeed = 4 // verified: elf sword swing (base 4) resolves dealt=8 (crit)
	critMissSeed = 1 // verified: elf sword swing (base 4) resolves dealt=4 (plain hit)
)

// hitOn returns the single Hits entry for victim in snap, failing the test
// when there is not exactly one — every scenario here lands exactly one hit
// on its victim per resolution.
func hitOn(t *testing.T, snap protocol.TurnEvent, victim int64) protocol.HitView {
	t.Helper()

	var (
		found protocol.HitView
		n     int
	)

	for _, h := range snap.Hits {
		if h.VictimID == victim {
			found = h
			n++
		}
	}

	if n != 1 {
		t.Fatalf("hits on victim %d = %d, want exactly 1 (all hits: %v)", victim, n, snap.Hits)
	}

	return found
}

// critScenario drives one elf Fighter melee swing (entity-targeted, adjacent
// — the #116 melee path) into a wolf under seed and returns the recorded hit
// plus the wolf's HP loss, so the crit flag can be checked against the real
// damage that landed.
func critScenario(t *testing.T, seed int64) (protocol.HitView, int) {
	t.Helper()

	w := newWorld()
	w.SetSeedForTest(seed)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesElf)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)
	w.SetAttackTargetEntityForTest(me.EntityID, monsterID)
	w.ResolveCombatOnlyForTest()

	snap := w.Snapshot()
	hit := hitOn(t, snap, monsterID)

	monster, ok := entityOfSnap(snap, monsterID)
	if !ok {
		t.Fatalf("monster %d missing from snapshot", monsterID)
	}

	return hit, game.MonsterMaxHPForTest("wolf") - monster.HP
}

// TestHitViewCritMoment (#114): the turn bundle's Hits view flags a hit whose
// deal-damage fold fired a chance-conditioned boost (the elf-crit card) as a
// crit — and never flags a plain hit — while Amount mirrors the exact damage
// the HP delta shows, and Turn stamps the resolution's own bundle turn.
func TestHitViewCritMoment(t *testing.T) {
	t.Parallel()

	plain, plainLoss := critScenario(t, critMissSeed)
	if plain.Crit || plain.Glance {
		t.Errorf("plain hit flags = crit %v glance %v, want both flags clear", plain.Crit, plain.Glance)
	}

	if got, want := plain.Amount, plainLoss; got != want {
		t.Errorf("plain hit Amount = %d, want the HP loss %d", got, want)
	}

	crit, critLoss := critScenario(t, critProcSeed)
	if !crit.Crit || crit.Glance {
		t.Errorf("crit hit flags = crit %v glance %v, want true false", crit.Crit, crit.Glance)
	}

	if got, want := crit.Amount, critLoss; got != want {
		t.Errorf("crit hit Amount = %d, want the HP loss %d", got, want)
	}

	if got, want := crit.Amount, plain.Amount*protocol.ElfCritMultiplier; got != want {
		t.Errorf("crit Amount = %d, want %d (plain ×%d)", got, want, protocol.ElfCritMultiplier)
	}
}

// glanceRun is one glanceHitScenario result: the recorded hit plus the world
// and the wolf's id, so a caller can keep stepping the same world (the
// retention test) without the helper handing back values nobody reads.
type glanceRun struct {
	world   *game.World
	hit     protocol.HitView
	monster int64
}

// glanceHitScenario mirrors glance_test.go's glanceScenario — one wolf melee
// attack into a stationary Rogue under seed — but returns the recorded
// HitView alongside the world (see glanceRun).
func glanceHitScenario(t *testing.T, seed int64) glanceRun {
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

	return glanceRun{world: w, hit: hitOn(t, w.Snapshot(), me.EntityID), monster: monsterID}
}

// TestHitViewGlanceMoment (#114): a hit whose take-damage fold fired the
// Rogue's chance-conditioned glance card is flagged glance (halved Amount);
// the same attack under a missing seed is flagged neither.
func TestHitViewGlanceMoment(t *testing.T) {
	t.Parallel()

	full := game.MonsterDamageForTest("wolf")

	glanced := glanceHitScenario(t, glanceProcSeed).hit
	if !glanced.Glance || glanced.Crit {
		t.Errorf("glanced hit flags = crit %v glance %v, want crit false, glance true", glanced.Crit, glanced.Glance)
	}

	if got, want := glanced.Amount, full*protocol.GlanceDamagePercent/100; got != want {
		t.Errorf("glanced Amount = %d, want %d (halved)", got, want)
	}

	plain := glanceHitScenario(t, glanceMissSeed).hit
	if plain.Glance || plain.Crit {
		t.Errorf("plain hit flags = crit %v glance %v, want both flags clear", plain.Crit, plain.Glance)
	}

	if got, want := plain.Amount, full; got != want {
		t.Errorf("plain Amount = %d, want %d (full)", got, want)
	}
}

// TestHitViewStampsBundleTurn (#114): a hit's Turn equals the Turn of the
// bundle its resolution produces — the number the client dedupes on across
// coalesced SSE frames.
func TestHitViewStampsBundleTurn(t *testing.T) {
	t.Parallel()

	run := glanceHitScenario(t, glanceProcSeed)

	if got, want := run.hit.Turn, run.world.Snapshot().Turn; got != want {
		t.Errorf("hit Turn = %d, want the bundle's %d", got, want)
	}
}

// TestHitsPrunedAfterRetention (#114): a hit rides bundles for
// hitRetentionTurns resolutions (coalescing slack), then is pruned — the
// Hits view never grows without bound.
func TestHitsPrunedAfterRetention(t *testing.T) {
	t.Parallel()

	run := glanceHitScenario(t, glanceProcSeed)
	w := run.world

	// Quiet turns: clear the wolf's path so no further hits land.
	w.SetPathForTest(run.monster, nil)

	for i := 1; i < game.HitRetentionTurnsForTest; i++ {
		w.ResolveCombatOnlyForTest()

		if got := len(w.Snapshot().Hits); got == 0 {
			t.Fatalf("Hits empty after %d quiet turns, want retention for %d",
				i, game.HitRetentionTurnsForTest-1)
		}
	}

	w.ResolveCombatOnlyForTest()

	if got, want := len(w.Snapshot().Hits), 0; got != want {
		t.Errorf("Hits after %d quiet turns = %d entries, want %d (pruned)",
			game.HitRetentionTurnsForTest, got, want)
	}
}
