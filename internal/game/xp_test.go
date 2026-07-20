package game_test

import (
	"strings"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestFreshPlayerHasZeroXPLevelOne: a newly joined player carries XP 0 and the
// derived Level 1 on the wire.
func TestFreshPlayerHasZeroXPLevelOne(t *testing.T) {
	t.Parallel()

	w := newWorld()

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	player, ok := entityOfSnap(w.Snapshot(), me.EntityID)
	if !ok {
		t.Fatalf("player %d missing from snapshot", me.EntityID)
	}

	if got, want := player.XP, 0; got != want {
		t.Errorf("fresh player XP = %d, want %d", got, want)
	}

	if got, want := player.Level, 1; got != want {
		t.Errorf("fresh player Level = %d, want %d", got, want)
	}
}

// TestKillGrantsXP: a player who strikes a one-hit monster to death is awarded
// the slain kind's full XP (wolf's here — the default spawn kind); the
// derived Level reflects the new total. Joins as a Dwarf so the base award
// is asserted without the Human XP bonus (the bonus has its own test);
// Dwarf adds no crit RNG and no XP modifier.
func TestKillGrantsXP(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(10)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesDwarf)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)
	w.SetHPForTest(monsterID, game.ItemDamageForTest("iron-sword")) // one melee attack is lethal

	// Kill XP is only earned inside a combat bubble (a real fight). One world
	// resolution with the monster adjacent forms that bubble around the idle
	// player; the kill then lands inside it via the lock-in immediate resolution.
	step(t, w)

	if err := w.SubmitIntent(entityAttackIntent(me.EntityID, me.Token, monsterID)); err != nil {
		t.Fatalf("SubmitIntent(melee): %v", err)
	}

	snap := w.Snapshot()

	if _, ok := entityOfSnap(snap, monsterID); ok {
		t.Fatalf("monster %d should have been killed by the melee attack", monsterID)
	}

	player, ok := entityOfSnap(snap, me.EntityID)
	if !ok {
		t.Fatalf("killer %d missing from snapshot", me.EntityID)
	}

	if got, want := player.XP, game.MonsterXPForTest("wolf"); got != want {
		t.Errorf("killer XP = %d, want %d (wolf's full kill XP)", got, want)
	}

	// 20 XP is still level 1 (XPCurveBase=100, level-2 floor=100); the
	// level-up crossing is exercised separately in
	// TestKillCrossingLevelBoundaryLevelsUp.
	if got, want := player.Level, 1; got != want {
		t.Errorf("killer Level = %d, want %d", got, want)
	}
}

// TestSharedXPIsFullNotSplit: two players in one fight both kill a single
// monster; each is awarded the kind's FULL kill XP, not a divided share —
// helping always pays, with no last-hit competition.
func TestSharedXPIsFullNotSplit(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(11)

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	ns := game.HexNeighbors(center)

	idA, tokA := w.PlaceEntityForTest(ns[0])
	idB, tokB := w.PlaceEntityForTest(ns[1])

	monsterID := w.PlaceMonsterForTest(center)
	w.SetHPForTest(monsterID, game.ItemDamageForTest("iron-sword")) // dies to a single hit

	// Kill XP is only earned inside a combat bubble (a real fight). One world
	// resolution forms the bubble around the two idle players and the monster; the
	// monster is not attacked this turn, so it survives to be killed in the bubble.
	step(t, w)

	// Both players melee-attack the monster on the same bubble-turn. The monster
	// deals only 3 damage to one player per turn, so both attackers survive to be
	// paid the full award.
	if err := w.SubmitIntent(entityAttackIntent(idA, tokA, monsterID)); err != nil {
		t.Fatalf("SubmitIntent(melee, A): %v", err)
	}

	if err := w.SubmitIntent(entityAttackIntent(idB, tokB, monsterID)); err != nil {
		t.Fatalf("SubmitIntent(melee, B): %v", err)
	}

	step(t, w)

	if _, ok := entityOfSnap(w.Snapshot(), monsterID); ok {
		t.Fatalf("monster %d should have died to the shared melee attacks", monsterID)
	}

	if got, want := w.XPForTest(idA), game.MonsterXPForTest("wolf"); got != want {
		t.Errorf("player A XP = %d, want the full %d (not split)", got, want)
	}

	if got, want := w.XPForTest(idB), game.MonsterXPForTest("wolf"); got != want {
		t.Errorf("player B XP = %d, want the full %d (not split)", got, want)
	}
}

// TestTwoKillsInOneFightGrantTwoMonsterXP: a lone player who fells two
// monsters in the same bubble is paid the kind's XP per kill — 2× wolf's
// kill XP cumulative, not a single flat award. One player lands one attack
// per turn, so the two kills fall on consecutive bubble-turns; the
// assertion is the cumulative total. A regression to one fixed award, to no
// bubble award at all, or to a world-domain award would all miss it.
func TestTwoKillsInOneFightGrantTwoMonsterXP(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(15)

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	ns := game.HexNeighbors(center)

	pid, tok := w.PlaceEntityForTest(center)

	monsterA := w.PlaceMonsterForTest(ns[0])
	monsterB := w.PlaceMonsterForTest(ns[1])
	w.SetHPForTest(monsterA, 1) // each dies to a single melee attack
	w.SetHPForTest(monsterB, 1)

	// One world resolution with both monsters adjacent forms the combat bubble
	// (kill XP is only earned inside a real fight). The player stays idle this
	// turn, so neither monster dies here and nothing is credited in the world
	// domain — proving the two later awards come from the bubble path.
	step(t, w)

	// Melee-attack monster A, then monster B — one attack, one kill per
	// bubble-turn. Attack intents are one-shot, so each kill needs its own
	// SubmitIntent.
	if err := w.SubmitIntent(entityAttackIntent(pid, tok, monsterA)); err != nil {
		t.Fatalf("SubmitIntent(melee, A): %v", err)
	}

	step(t, w)

	if _, ok := entityOfSnap(w.Snapshot(), monsterA); ok {
		t.Fatalf("monster A %d should have died to the first melee attack", monsterA)
	}

	if err := w.SubmitIntent(entityAttackIntent(pid, tok, monsterB)); err != nil {
		t.Fatalf("SubmitIntent(melee, B): %v", err)
	}

	step(t, w)

	if _, ok := entityOfSnap(w.Snapshot(), monsterB); ok {
		t.Fatalf("monster B %d should have died to the second melee attack", monsterB)
	}

	if got, want := w.XPForTest(pid), 2*game.MonsterXPForTest("wolf"); got != want {
		t.Errorf("player XP after two kills = %d, want %d (wolf's kill XP per kill)", got, want)
	}
}

// TestKillCrossingLevelBoundaryLevelsUp: a player one kill short of the next
// level crosses the level-2 floor (XPCurveBase, since XPCurveBase*(2-1)^2 ==
// XPCurveBase) on the kill and their derived Level increments. Joins as a
// Dwarf so the boundary math uses wolf's base kill XP (no Human bonus).
func TestKillCrossingLevelBoundaryLevelsUp(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(12)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesDwarf)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	// One wolf kill's XP below the level-2 boundary: still level 1 before the kill.
	w.SetXPForTest(me.EntityID, protocol.XPCurveBase-game.MonsterXPForTest("wolf"))

	before, ok := entityOfSnap(w.Snapshot(), me.EntityID)
	if !ok {
		t.Fatalf("player %d missing before the kill", me.EntityID)
	}

	if got, want := before.Level, 1; got != want {
		t.Fatalf("pre-kill Level = %d, want %d", got, want)
	}

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)
	w.SetHPForTest(monsterID, game.ItemDamageForTest("iron-sword"))

	// Kill XP is only earned inside a combat bubble; form it with one world
	// resolution (player idle, monster survives), then land the kill inside it.
	step(t, w)

	if err := w.SubmitIntent(entityAttackIntent(me.EntityID, me.Token, monsterID)); err != nil {
		t.Fatalf("SubmitIntent(melee): %v", err)
	}

	snap := w.Snapshot()

	player, ok := entityOfSnap(snap, me.EntityID)
	if !ok {
		t.Fatalf("player %d missing after the kill", me.EntityID)
	}

	if got, want := player.XP, protocol.XPCurveBase; got != want {
		t.Errorf("post-kill XP = %d, want %d (exactly the boundary)", got, want)
	}

	if got, want := player.Level, 2; got != want {
		t.Errorf("post-kill Level = %d, want %d (leveled up)", got, want)
	}
}

// TestDeathFloorsXPKeepsLevel: a dying player falls back to the start of the
// level they were in — keeping the level, losing only within-level progress.
func TestDeathFloorsXPKeepsLevel(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(13)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	// Mid level 2: 150 XP → level 2, floor 100.
	w.SetXPForTest(me.EntityID, 150)

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	// The monster strikes the player dead; the player has no intent, so no
	// monster dies this turn (pure death-floor, no kill award).
	w.SetHPForTest(me.EntityID, game.MonsterDamageForTest("wolf")) // exactly lethal
	w.SetPathForTest(monsterID, []protocol.Hex{me.Hex})
	w.ResolveCombatOnlyForTest()

	snap := w.Snapshot()

	player, ok := entityOfSnap(snap, me.EntityID)
	if !ok {
		t.Fatalf("player %d should respawn, not vanish", me.EntityID)
	}

	if got, want := player.XP, 100; got != want {
		t.Errorf("respawned XP = %d, want %d (floored to the level-2 start)", got, want)
	}

	if got, want := player.Level, 2; got != want {
		t.Errorf("respawned Level = %d, want %d (unchanged by death)", got, want)
	}
}

// TestPlayerDyingSameTurnAsMonsterGetsNoKillXP (#194): when a player dies on
// the very turn a monster also dies (a mutual kill), the dead player earns no
// kill XP. Seeding the player at 190 XP makes it observable: a correct floor
// lands at 100 (level-2 start); the bug (award-then-respawn) reaches ~200.
//
// Driven through ResolveTurnForTest — the REAL resolveBubbleTurnLocked award
// path. The previous version used ResolveCombatOnlyForTest, which never runs
// the award loop at all, so it passed vacuously while the live path was broken
// (that was a false pin; #194).
func TestPlayerDyingSameTurnAsMonsterGetsNoKillXP(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(14)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.SetXPForTest(me.EntityID, 190) // level 2, floor 100

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	// Step once to form the bubble (both at full HP — the world-domain step is
	// not lethal), so the mutual-kill step below runs through the bubble's
	// award path rather than forming a bubble mid-step.
	w.SetHPForTest(me.EntityID, 100)
	w.SetHPForTest(monsterID, 100)
	w.ResolveTurnForTest()

	if _, ok := entityOfSnap(w.Snapshot(), monsterID); !ok {
		t.Fatalf("monster gone before the mutual-kill turn — setup issue")
	}

	// Now arrange the mutual kill: one hit each is lethal in both directions.
	w.SetHPForTest(monsterID, game.ItemDamageForTest("iron-sword"))
	w.SetHPForTest(me.EntityID, game.MonsterDamageForTest("wolf"))
	w.SetXPForTest(me.EntityID, 190)

	if err := w.SubmitIntent(entityAttackIntent(me.EntityID, me.Token, monsterID)); err != nil {
		t.Fatalf("SubmitIntent(melee): %v", err)
	}

	w.ResolveTurnForTest()

	snap := w.Snapshot()

	if _, ok := entityOfSnap(snap, monsterID); ok {
		t.Fatalf("monster %d should have died in the mutual kill", monsterID)
	}

	player, ok := entityOfSnap(snap, me.EntityID)
	if !ok {
		t.Fatalf("player %d should respawn, not vanish", me.EntityID)
	}

	if got, want := player.XP, 100; got != want {
		t.Errorf("respawned XP = %d, want %d (no kill award; a same-turn death earns nothing)", got, want)
	}
}

// TestLevelUpIsAnnounced (#202): crossing a level posts a party-visible line
// naming the points earned. The initial 0→1 grant on join is silent (not a
// level-up), and a nameless test-bridge player is skipped like death.
func TestLevelUpIsAnnounced(t *testing.T) {
	t.Parallel()

	w := newWorld()

	var announced []string

	w.SetAnnounce(func(sender, text string) {
		if sender == protocol.SystemSender {
			announced = append(announced, text)
		}
	})

	me, err := w.Join("", "climber", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	// First XP event settles the level-1 grant silently.
	w.SetXPForTest(me.EntityID, 10)
	w.GrantSkillPointsForTest(me.EntityID)

	if len(announced) != 0 {
		t.Fatalf("initial grant announced %v, want silence", announced)
	}

	// Cross into level 2 → one announce naming the points (Human: 3+1=4/level).
	w.SetXPForTest(me.EntityID, 2*protocol.XPCurveBase+5)
	w.GrantSkillPointsForTest(me.EntityID)

	if len(announced) != 1 {
		t.Fatalf("level-up announces = %d (%v), want 1", len(announced), announced)
	}

	if got := announced[0]; !strings.Contains(got, "level 2") || !strings.Contains(got, "skill points") {
		t.Errorf("announce = %q, want it to name the level and points", got)
	}
}
