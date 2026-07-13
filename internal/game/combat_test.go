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

// TestBumpDealsDamageAttackerStays: a player moving onto an adjacent
// monster's hex is a bump-to-attack, not a move — the monster takes damage
// and the attacker's own hex does not change.
func TestBumpDealsDamageAttackerStays(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	if !submitOK(w, me, monsterHex) {
		t.Fatalf("SubmitIntent onto the monster's hex failed")
	}

	snap := step(t, w)

	monster, ok := entityOfSnap(snap, monsterID)
	if !ok {
		t.Fatalf("monster %d missing from snapshot after a single bump", monsterID)
	}

	if got, want := monster.HP, protocol.MonsterMaxHP-game.ItemDamageForTest("iron-sword"); got != want {
		t.Errorf("monster HP = %d, want %d", got, want)
	}

	if got, want := hexOfSnap(snap, me.EntityID), me.Hex; got != want {
		t.Errorf("attacker hex = %v, want unchanged %v (a bump does not move the attacker)", got, want)
	}

	if got, want := monster.Hex, monsterHex; got != want {
		t.Errorf("monster hex = %v, want unchanged %v", got, want)
	}
}

// TestBumpKillRemovesMonster: repeated bumps drain the monster's HP to 0;
// once dead it is gone from the next snapshot. The bump defers the
// attacker's path (never consumed on a bump), so the same standing intent
// keeps landing turn after turn without resubmitting.
func TestBumpKillRemovesMonster(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(2)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	if !submitOK(w, me, monsterHex) {
		t.Fatalf("SubmitIntent onto the monster's hex failed")
	}

	// MonsterMaxHP=10, a Fighter's sword bump = iron-sword damage 4: three bumps kill it.
	firstHit := step(t, w)

	monster, ok := entityOfSnap(firstHit, monsterID)
	if !ok {
		t.Fatalf("monster %d should survive the first hit", monsterID)
	}

	if got, want := monster.HP, protocol.MonsterMaxHP-game.ItemDamageForTest("iron-sword"); got != want {
		t.Fatalf("monster HP after first hit = %d, want %d", got, want)
	}

	secondHit := step(t, w)

	monster, ok = entityOfSnap(secondHit, monsterID)
	if !ok {
		t.Fatalf("monster %d should survive the second hit", monsterID)
	}

	if got, want := monster.HP, protocol.MonsterMaxHP-2*game.ItemDamageForTest("iron-sword"); got != want {
		t.Fatalf("monster HP after second hit = %d, want %d", got, want)
	}

	thirdHit := step(t, w)

	if _, ok := entityOfSnap(thirdHit, monsterID); ok {
		t.Fatalf("monster %d should have been removed once its HP reached 0", monsterID)
	}
}

// TestBumpRetreatDodgesDamage: the defender vacates the bump-target hex
// during the very same move phase (a retreat), so the deferred bump's
// post-move re-check finds the hex empty and completes as an ordinary move
// instead of an attack — no damage, and the attacker advances into the
// vacated hex.
//
// The retreating entity here is the monster; its path is set directly via
// SetPathForTest and resolved with ResolveCombatOnlyForTest (skips
// thinkMonstersLocked), because the real AI never voluntarily retreats a
// monster away from a player — it only ever holds or advances. This test is
// about the combat *machinery* (moveAndBumpLocked's bump/re-check logic),
// independent of which AI actually drives it.
func TestBumpRetreatDodgesDamage(t *testing.T) {
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

	if !submitOK(w, me, monsterHex) {
		t.Fatalf("SubmitIntent onto the monster's hex failed")
	}

	w.SetPathForTest(monsterID, []protocol.Hex{escapeHex})
	w.ResolveCombatOnlyForTest()

	snap := w.Snapshot()

	monster, ok := entityOfSnap(snap, monsterID)
	if !ok {
		t.Fatalf("monster %d should survive an undamaged retreat", monsterID)
	}

	if got, want := monster.HP, protocol.MonsterMaxHP; got != want {
		t.Errorf("monster HP = %d, want %d (no damage: the bump found the hex vacated)", got, want)
	}

	if got, want := monster.Hex, escapeHex; got != want {
		t.Errorf("monster hex = %v, want %v (retreated)", got, want)
	}

	if got, want := hexOfSnap(snap, me.EntityID), monsterHex; got != want {
		t.Errorf("attacker hex = %v, want %v (advanced into the vacated hex)", got, want)
	}
}

// TestBumpMutualKill: a player and monster bump each other on the same turn,
// each with exactly enough damage to kill the other. Both attacks must
// accumulate against pre-attack HP and apply together — the monster is
// removed and the player, rather than vanishing, respawns at full HP.
//
// The monster's half of the mutual bump is driven via SetPathForTest +
// ResolveCombatOnlyForTest rather than thinkMonstersLocked, to pin the exact
// turn the bump lands independent of the AI's own targeting/pathfinding.
// This test targets the combat resolution algorithm's simultaneity, not the
// AI.
func TestBumpMutualKill(t *testing.T) {
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

	if !submitOK(w, me, monsterHex) {
		t.Fatalf("SubmitIntent onto the monster's hex failed")
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

// TestBumpPlayerDeathRespawns: a lethal bump against a player removes it from
// play only momentarily — resolveDeathsLocked immediately respawns it at
// full HP, on a walkable hex, keeping the same id (so the client, still
// holding the same token, stays joined).
func TestBumpPlayerDeathRespawns(t *testing.T) {
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

// TestBumpRandomVictimOnStackedHexIsReproducible: a monster bumping a
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
func TestBumpRandomVictimOnStackedHexIsReproducible(t *testing.T) {
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
// already adjacent to the sole player now bumps into it (milestone 6.3 Task
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
		t.Errorf("player hex = %v, want unchanged %v (a bump does not move the defender)", got, want)
	}

	if got, want := hexOfSnap(snap, monsterID), monsterHex; got != want {
		t.Errorf("monster hex = %v, want unchanged %v (a bump does not move the attacker)", got, want)
	}
}
