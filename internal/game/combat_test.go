package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// entityOfSnap returns the entity with id in snap, and whether it was found —
// combat tests use this to check both presence (death/removal, respawn) and
// HP/hex after a resolve.
func entityOfSnap(snap protocol.TurnEvent, id int64) (protocol.Entity, bool) {
	for _, e := range snap.Entities {
		if e.ID == id {
			return e, true
		}
	}

	return protocol.Entity{}, false
}

// TestMeleeDealsDamageAttackerStays: an entity-targeted attack intent at an
// adjacent monster resolves as a melee swing — the monster takes damage and
// the attacker's own hex does not change.
func TestMeleeDealsDamageAttackerStays(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	if err := w.SubmitIntent(entityAttackIntent(me.EntityID, me.Token, monsterID)); err != nil {
		t.Fatalf("SubmitIntent(melee): %v", err)
	}

	snap := step(t, w)

	monster, ok := entityOfSnap(snap, monsterID)
	if !ok {
		t.Fatalf("monster %d missing from snapshot after a single melee attack", monsterID)
	}

	if got, want := monster.HP, protocol.MonsterMaxHP-game.ItemDamageForTest("iron-sword"); got != want {
		t.Errorf("monster HP = %d, want %d", got, want)
	}

	if got, want := hexOfSnap(snap, me.EntityID), me.Hex; got != want {
		t.Errorf("attacker hex = %v, want unchanged %v (a melee attack does not move the attacker)", got, want)
	}

	if got, want := monster.Hex, monsterHex; got != want {
		t.Errorf("monster hex = %v, want unchanged %v", got, want)
	}
}

// TestMeleeKillRemovesMonster: repeated melee attacks drain the monster's HP
// to 0; once dead it is gone from the next snapshot. Attack intents are
// one-shot, so the attacker resubmits before every turn that should land a
// swing.
func TestMeleeKillRemovesMonster(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(2)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	// MonsterMaxHP=10, a Fighter's sword melee attack = iron-sword damage 4: three melee attacks kill it.
	if err := w.SubmitIntent(entityAttackIntent(me.EntityID, me.Token, monsterID)); err != nil {
		t.Fatalf("SubmitIntent(melee): %v", err)
	}

	firstHit := step(t, w)

	monster, ok := entityOfSnap(firstHit, monsterID)
	if !ok {
		t.Fatalf("monster %d should survive the first hit", monsterID)
	}

	if got, want := monster.HP, protocol.MonsterMaxHP-game.ItemDamageForTest("iron-sword"); got != want {
		t.Fatalf("monster HP after first hit = %d, want %d", got, want)
	}

	if err := w.SubmitIntent(entityAttackIntent(me.EntityID, me.Token, monsterID)); err != nil {
		t.Fatalf("SubmitIntent(melee): %v", err)
	}

	secondHit := step(t, w)

	monster, ok = entityOfSnap(secondHit, monsterID)
	if !ok {
		t.Fatalf("monster %d should survive the second hit", monsterID)
	}

	if got, want := monster.HP, protocol.MonsterMaxHP-2*game.ItemDamageForTest("iron-sword"); got != want {
		t.Fatalf("monster HP after second hit = %d, want %d", got, want)
	}

	if err := w.SubmitIntent(entityAttackIntent(me.EntityID, me.Token, monsterID)); err != nil {
		t.Fatalf("SubmitIntent(melee): %v", err)
	}

	thirdHit := step(t, w)

	if _, ok := entityOfSnap(thirdHit, monsterID); ok {
		t.Fatalf("monster %d should have been removed once its HP reached 0", monsterID)
	}
}

// TestMeleeHitsRetreatingDefender (#104, attacks-before-moves): the defender
// vacates the melee target hex during this same turn's MOVE phase, but the
// attack phase has already resolved against pre-move positions — the melee
// attack lands anyway. The defender takes the hit, then completes its
// retreat; the attacker stays put (an entity-targeted attack intent never
// moves the attacker). This replaces TestMeleeRetreatDodgesDamage: the
// retreat-dodge (an automatic miss on vacation) is removed by design —
// retreat now trades hits for distance.
//
// The retreating entity here is the monster; its path is set directly via
// SetPathForTest and resolved with ResolveCombatOnlyForTest (skips
// thinkMonstersLocked), because the real AI never voluntarily retreats a
// monster away from a player. The player's half is an explicit
// entity-targeted attack intent (#116) — the monster's half keeps
// SetPathForTest, since this test is about the combat machinery
// (resolveEntityTargetedLocked/attackLocked/movePhaseLocked ordering),
// independent of which AI drives the monster.
func TestMeleeHitsRetreatingDefender(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(3)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	var (
		escapeHex protocol.Hex
		found     bool
	)

	for _, n := range game.HexNeighbors(monsterHex) {
		if n != me.Hex && isWalkable(w, n) {
			escapeHex = n
			found = true

			break
		}
	}

	if !found {
		t.Skip("no free walkable escape hex around the monster on this map")
	}

	if err := w.SubmitIntent(entityAttackIntent(me.EntityID, me.Token, monsterID)); err != nil {
		t.Fatalf("SubmitIntent(melee): %v", err)
	}

	w.SetPathForTest(monsterID, []protocol.Hex{escapeHex})
	w.ResolveCombatOnlyForTest()

	snap := w.Snapshot()

	monster, ok := entityOfSnap(snap, monsterID)
	if !ok {
		t.Fatalf("monster %d should survive one sword hit", monsterID)
	}

	if got, want := monster.HP, protocol.MonsterMaxHP-game.ItemDamageForTest("iron-sword"); got != want {
		t.Errorf("monster HP = %d, want %d (the melee attack lands against the pre-move position)", got, want)
	}

	if got, want := monster.Hex, escapeHex; got != want {
		t.Errorf("monster hex = %v, want %v (the retreat itself still lands, after the hit)", got, want)
	}

	if got, want := hexOfSnap(snap, me.EntityID), me.Hex; got != want {
		t.Errorf("attacker hex = %v, want unchanged %v (a melee attack never moves the attacker)", got, want)
	}
}

// TestMeleeMutualKill: a player and monster strike each other on the same
// turn, each with exactly enough damage to kill the other. Both attacks must
// accumulate against pre-attack HP and apply together — the monster is
// removed and the player, rather than vanishing, respawns at full HP.
//
// The monster's half of the mutual melee attack is driven via SetPathForTest
// + ResolveCombatOnlyForTest rather than thinkMonstersLocked, to pin the
// exact turn the melee attack lands independent of the AI's own
// targeting/pathfinding. This test targets the combat resolution algorithm's
// simultaneity, not the AI.
func TestMeleeMutualKill(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(4)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	// One hit each is lethal in both directions.
	w.SetHPForTest(monsterID, game.ItemDamageForTest("iron-sword"))
	w.SetHPForTest(me.EntityID, game.MonsterDamageForTest("wolf"))

	if err := w.SubmitIntent(entityAttackIntent(me.EntityID, me.Token, monsterID)); err != nil {
		t.Fatalf("SubmitIntent(melee): %v", err)
	}

	w.SetPathForTest(monsterID, []protocol.Hex{me.Hex})
	w.ResolveCombatOnlyForTest()

	snap := w.Snapshot()

	if _, ok := entityOfSnap(snap, monsterID); ok {
		t.Errorf("monster %d should have been removed by the mutual kill", monsterID)
	}

	player, ok := entityOfSnap(snap, me.EntityID)
	if !ok {
		t.Fatalf("player %d should respawn, not vanish", me.EntityID)
	}

	if got, want := player.HP, protocol.FighterMaxHP; got != want {
		t.Errorf("respawned player HP = %d, want %d (full)", got, want)
	}

	if got, want := player.ID, me.EntityID; got != want {
		t.Errorf("respawned entity id = %d, want %d (same id)", got, want)
	}

	if !isWalkable(w, player.Hex) {
		t.Errorf("respawned player hex %v is not walkable", player.Hex)
	}
}

// TestMeleePlayerDeathRespawns: a lethal melee attack against a player
// removes it from play only momentarily — resolveDeathsLocked immediately
// respawns it at full HP, on a walkable hex, keeping the same id (so the
// client, still holding the same token, stays joined).
func TestMeleePlayerDeathRespawns(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(5)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	w.SetHPForTest(me.EntityID, game.MonsterDamageForTest("wolf")) // exactly lethal
	w.SetPathForTest(monsterID, []protocol.Hex{me.Hex})
	w.ResolveCombatOnlyForTest()

	snap := w.Snapshot()

	player, ok := entityOfSnap(snap, me.EntityID)
	if !ok {
		t.Fatalf("player %d should respawn, not vanish", me.EntityID)
	}

	if got, want := player.HP, protocol.FighterMaxHP; got != want {
		t.Errorf("respawned player HP = %d, want %d (full)", got, want)
	}

	if got, want := player.ID, me.EntityID; got != want {
		t.Errorf("respawned entity id = %d, want %d (same id)", got, want)
	}

	if !isWalkable(w, player.Hex) {
		t.Errorf("respawned player hex %v is not walkable", player.Hex)
	}

	monster, ok := entityOfSnap(snap, monsterID)
	if !ok {
		t.Fatalf("monster %d (the attacker) should still be alive", monsterID)
	}

	if got, want := monster.HP, protocol.MonsterMaxHP; got != want {
		t.Errorf("monster HP = %d, want unchanged %d (it was not attacked back)", got, want)
	}
}

// TestMeleeRandomVictimOnStackedHexIsReproducible: a monster striking a
// same-faction stack of two players must damage exactly one of them, and
// under a pinned seed the choice must be reproducible — the same seed and
// the same board must always pick the same victim.
//
// The victim choice is compared between two independently-built worlds
// (same seed, same construction order) rather than against a hardcoded id:
// attackLocked's occupant list is built from a map (w.entities) ranged once
// per resolve, whose iteration order Go does not guarantee — the production
// code sorts occupants by id before the seeded pick specifically so that
// incidental map order cannot influence the outcome. Comparing two runs
// against each other (rather than pinning a literal id) verifies that
// determinism without also encoding an incidental implementation detail
// (which id happens to win) into the test.
func TestMeleeRandomVictimOnStackedHexIsReproducible(t *testing.T) {
	t.Parallel()

	const seed = 6

	run := func() (int64, int64) {
		w := newWorld()
		w.SetSeedForTest(seed)

		stackHex := protocol.Hex{Q: 0, R: 0}
		if !isWalkable(w, stackHex) {
			t.Skip("origin hex is not walkable on this map")
		}

		idA, _ := w.PlaceEntityForTest(stackHex)
		idB, _ := w.PlaceEntityForTest(stackHex)

		monsterHex := walkableNeighbor(t, w, stackHex)
		monsterID := w.PlaceMonsterForTest(monsterHex)

		w.SetPathForTest(monsterID, []protocol.Hex{stackHex})
		w.ResolveCombatOnlyForTest()

		snap := w.Snapshot()

		a, aok := entityOfSnap(snap, idA)
		b, bok := entityOfSnap(snap, idB)

		if !aok || !bok {
			t.Fatalf("both stacked players should survive a single hit")
		}

		switch {
		case a.HP < protocol.FighterMaxHP && b.HP == protocol.FighterMaxHP:
			return idA, idB
		case b.HP < protocol.FighterMaxHP && a.HP == protocol.FighterMaxHP:
			return idB, idA
		default:
			t.Fatalf("expected exactly one stacked player damaged; got HPs %d and %d", a.HP, b.HP)

			return 0, 0
		}
	}

	damaged1, healthy1 := run()
	damaged2, healthy2 := run()

	if got, want := damaged2, damaged1; got != want {
		t.Errorf("victim selection not reproducible for same seed: run 1 damaged %d, run 2 damaged %d", want, got)
	}

	if got, want := healthy2, healthy1; got != want {
		t.Errorf("healthy id changed between runs for the same seed: first run %d, second run %d", want, got)
	}
}

// TestMonsterAIAttacksAdjacentPlayer: with no player intent, a monster
// already adjacent to the sole player now strikes it (milestone 6.3 Task
// 3) instead of holding position (6.2's behaviour) — thinkMonstersLocked
// steps onto the player's hex, and the move phase converts that into an
// attack. The player takes the monster's claws damage (wolf's, here) and
// the monster stays put.
func TestMonsterAIAttacksAdjacentPlayer(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(6)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	snap := step(t, w)

	player, ok := entityOfSnap(snap, me.EntityID)
	if !ok {
		t.Fatalf("player %d missing from snapshot after a single monster attack", me.EntityID)
	}

	if got, want := player.HP, protocol.FighterMaxHP-game.MonsterDamageForTest("wolf"); got != want {
		t.Errorf("player HP = %d, want %d", got, want)
	}

	if got, want := player.Hex, me.Hex; got != want {
		t.Errorf("player hex = %v, want unchanged %v (a melee attack does not move the defender)", got, want)
	}

	if got, want := hexOfSnap(snap, monsterID), monsterHex; got != want {
		t.Errorf("monster hex = %v, want unchanged %v (a melee attack does not move the attacker)", got, want)
	}
}
