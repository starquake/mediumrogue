package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// Monster-kind ids, named the same way world_test.go's other black-box test
// helpers name registry ids — the game package's own idKind* consts
// (monsters.go) aren't visible here (package game_test).
const (
	kindRat    = "rat"
	kindWolf   = "wolf"
	kindGhoul  = "ghoul"
	kindTroll  = "troll"
	kindDragon = "dragon"
)

// TestPerKindClawsDamage: two different monster kinds' claws deal exactly
// their own def's damage, not a shared flat value — proves closeDefFor's
// monster branch reads the per-kind profile (monsters.go's buildMonsterIndex),
// not a single global claws def.
func TestPerKindClawsDamage(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{kindRat, kindTroll} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			w := newWorld()

			center := protocol.Hex{Q: 0, R: 0}
			if !isWalkable(w, center) {
				t.Skip("origin is not walkable on this map")
			}

			monsterHex := walkableNeighbor(t, w, center)

			pid, _ := w.PlaceEntityForTest(center) // level-1 Fighter, FighterMaxHP
			monsterID := w.PlaceMonsterKindForTest(monsterHex, kind)

			// Monster strikes the player; ResolveCombatOnlyForTest skips the AI
			// think phase, so the pinned path is not overwritten by targeting
			// (mirrors melee_damage_test.go's TestMonsterMeleeDamageUnchanged).
			w.SetPathForTest(monsterID, []protocol.Hex{center})
			w.ResolveCombatOnlyForTest()

			player, ok := entityOfSnap(w.Snapshot(), pid)
			if !ok {
				t.Fatalf("player %d missing after a %s melee attack", pid, kind)
			}

			dealt := protocol.FighterMaxHP - player.HP
			if got, want := dealt, game.MonsterDamageForTest(kind); got != want {
				t.Errorf("%s claws dealt %d, want %d (its own damage)", kind, got, want)
			}
		})
	}
}

// TestPerKindKillXP: a troll's kill award is its OWN xp (60), not wolf's
// (20) — proves the kill-XP fold sums the slain kinds' xp values, not a
// flat constant.
func TestPerKindKillXP(t *testing.T) {
	t.Parallel()

	w := newWorld()

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesDwarf)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	trollHex := walkableNeighbor(t, w, me.Hex)
	trollID := w.PlaceMonsterKindForTest(trollHex, kindTroll)
	w.SetHPForTest(trollID, game.ItemDamageForTest("iron-sword")) // one melee attack is lethal

	step(t, w) // forms the bubble

	if err := w.SubmitIntent(entityAttackIntent(me.EntityID, me.Token, trollID)); err != nil {
		t.Fatalf("SubmitIntent(melee): %v", err)
	}

	snap := step(t, w)

	if _, ok := entityOfSnap(snap, trollID); ok {
		t.Fatalf("troll %d should have died to the melee attack", trollID)
	}

	if got, want := w.XPForTest(me.EntityID), game.MonsterXPForTest(kindTroll); got != want {
		t.Errorf("killer XP = %d, want %d (troll's own xp, dwarf has no XP passive)", got, want)
	}
}

// TestDragonAlwaysDropsFromItsOwnTable: dragon's dropChance is 100 —
// pickDropFrom over dragon's own table (content.go's monsterDefs), run over
// a fixed seed range, always draws SOMETHING, and always a def from
// DRAGON's own table (never wolf's — proves per-kind loot authority, not a
// shared dropTable). The dropChance roll itself (dropLootLocked) is pinned
// end to end for wolf already (drops_test.go's killDropSeed/killMissSeed);
// this proves dragon's own table and 100% chance are wired correctly
// without depending on combat-adjacency timing.
func TestDragonAlwaysDropsFromItsOwnTable(t *testing.T) {
	t.Parallel()

	if got, want := game.MonsterDropChanceForTest(kindDragon), 100; got != want {
		t.Fatalf("dragon dropChance = %d, want %d", got, want)
	}

	dragonDrops := make(map[string]bool)
	for _, id := range game.DropTableIDsForTest(kindDragon) {
		dragonDrops[id] = true
	}

	for seed := range uint64(20) {
		id := game.PickDropForTest(kindDragon, seed)
		if id == "" {
			t.Fatalf("PickDropForTest(dragon, %d) = \"\" (empty draw)", seed)
		}

		if !dragonDrops[id] {
			t.Errorf("PickDropForTest(dragon, %d) = %q, want a def from dragon's own table %v", seed, id, dragonDrops)
		}
	}
}

// TestAggroRadiusPerKindOverride (#36 + 6c): a troll's aggroRadius (8) is
// strictly LESS than the shared default protocol.MonsterAggroRadius (10) —
// a troll at distance 9 must stand still even though 9 is within the
// default, proving thinkMonstersLocked reads the kind's own override, not
// the flat default.
func TestAggroRadiusPerKindOverride(t *testing.T) {
	t.Parallel()

	w := newWorld()

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	trollAggro := game.MonsterAggroRadiusForTest(kindTroll)
	if got, want := trollAggro, protocol.MonsterAggroRadius; got >= want {
		t.Fatalf("setup: troll aggroRadius = %d, want strictly less than the default %d", got, want)
	}

	beyondTrollButWithinDefault := walkableHexAtDistance(t, w, me.Hex, trollAggro+1, protocol.MonsterAggroRadius)
	trollID := w.PlaceMonsterKindForTest(beyondTrollButWithinDefault, kindTroll)

	snap := step(t, w)

	if got, want := hexOfSnap(snap, trollID), beyondTrollButWithinDefault; got != want {
		t.Errorf("troll hex = %v, want unchanged %v (beyond its own aggroRadius, should stand still "+
			"even though it's within the shared default)", got, want)
	}
}

// TestAggroRadiusPerKindOverrideHunts: the same troll, placed within ITS
// OWN aggro radius, hunts normally — the override shrinks the radius, it
// doesn't disable aggro.
func TestAggroRadiusPerKindOverrideHunts(t *testing.T) {
	t.Parallel()

	w := newWorld()
	// Pinned so the player's random (#36) spawn hex is reproducible: an
	// unseeded spawn occasionally lands somewhere the shortest path's FIRST
	// step doesn't reduce raw hex-distance (a lake forcing a detour), which
	// would make "it closed in by at least one hex on turn one" flaky
	// rather than false — same latent risk walkableHexAtDistance's other
	// callers carry; pinning here keeps this specific assertion reliable.
	w.SetSeedForTest(4)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	trollAggro := game.MonsterAggroRadiusForTest(kindTroll)
	atTrollAggro := walkableHexAtDistance(t, w, me.Hex, trollAggro, trollAggro)
	clearSightLine(t, w, me.Hex, atTrollAggro) // this test varies the KIND's radius, not terrain (#95)
	trollID := w.PlaceMonsterKindForTest(atTrollAggro, kindTroll)

	snap := step(t, w)

	beforeDist := game.HexDistance(atTrollAggro, me.Hex)
	afterDist := game.HexDistance(hexOfSnap(snap, trollID), me.Hex)

	if afterDist >= beforeDist {
		t.Errorf("troll distance to player went %d -> %d, want it to close in (within its own aggro radius)",
			beforeDist, afterDist)
	}
}

// TestKillSummaryNamesKinds pins killSummary's exact announce text over the
// scenarios the spec calls out: a single kill, several of the same kind,
// two different kinds, and three different kinds — plus a non-adjacent
// repeat (a wolf, a troll, then a second wolf) to prove grouping does not
// depend on same-kind entries being consecutive in the input.
func TestKillSummaryNamesKinds(t *testing.T) {
	t.Parallel()

	wolfXP := game.MonsterXPForTest(kindWolf)
	ghoulXP := game.MonsterXPForTest(kindGhoul)
	trollXP := game.MonsterXPForTest(kindTroll)
	dragonXP := game.MonsterXPForTest(kindDragon)

	cases := []struct {
		name  string
		kinds []string
		want  string
	}{
		{
			"single wolf", []string{kindWolf},
			"a wolf was slain (+20 XP to everyone in the fight)",
		},
		{
			"two ghouls", []string{kindGhoul, kindGhoul},
			"2 ghouls were slain (+70 XP to everyone in the fight)",
		},
		{
			"wolf and troll", []string{kindWolf, kindTroll},
			"a wolf and a troll were slain (+80 XP to everyone in the fight)",
		},
		{
			"wolf, troll, and dragon", []string{kindWolf, kindTroll, kindDragon},
			"a wolf, a troll and a dragon were slain (+230 XP to everyone in the fight)",
		},
		{
			"non-adjacent repeat", []string{kindWolf, kindTroll, kindWolf},
			"2 wolves and a troll were slain (+100 XP to everyone in the fight)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got, want := game.KillSummaryForTest(tc.kinds...), tc.want; got != want {
				t.Errorf("killSummary(%v) = %q, want %q", tc.kinds, got, want)
			}
		})
	}

	// Zero kills: unreachable from the bubble path (it gates on len(slain) >
	// 0), but the exported-for-test helper must not panic — empty line back.
	if got, want := game.KillSummaryForTest(), ""; got != want {
		t.Errorf("killSummary(nothing slain) = %q, want %q", got, want)
	}

	// Sanity-check the XP arithmetic quoted above against the live registry,
	// so this test fails loudly (not silently) if the launch numbers change.
	if got, want := wolfXP+trollXP+dragonXP, 230; got != want {
		t.Fatalf("wolf+troll+dragon xp = %d, want %d (update the case table above)", got, want)
	}

	if got, want := 2*wolfXP+trollXP, 100; got != want {
		t.Fatalf("2*wolf+troll xp = %d, want %d (update the case table above)", got, want)
	}

	if got, want := 2*ghoulXP, 70; got != want {
		t.Fatalf("2*ghoul xp = %d, want %d (update the case table above)", got, want)
	}
}

// TestKillSoloSummaryNamesKiller pins killSoloSummary's exact announce text
// (playtest item 3: a solo killer is named, active voice, no "everyone in
// the fight" wording) for a single kind and a mixed-kind kill in the same
// bubble-turn (e.g. a mage's AoE catching two different kinds at once).
func TestKillSoloSummaryNamesKiller(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		kinds []string
		want  string
	}{
		{"single wolf", []string{kindWolf}, "hero slew a wolf (+20 XP)"},
		{"mixed kinds", []string{kindWolf, kindTroll}, "hero slew a wolf and a troll (+80 XP)"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got, want := game.KillSoloSummaryForTest("hero", tc.kinds...), tc.want; got != want {
				t.Errorf("killSoloSummary(%v) = %q, want %q", tc.kinds, got, want)
			}
		})
	}
}

// TestWyrmslayerDamageMultiplierVsDragon: the Wyrmslayer Greatsword's
// condTargetKind rule spikes damage ×1.5 vs a dragon specifically — a
// fighter wielding it deals its flat 9 damage (gear keystone rebalance, the
// §4 "1H ≈ ½ 2H" anchor) to a wolf, but 13 (9*150/100, truncated) to a
// dragon.
func TestWyrmslayerDamageMultiplierVsDragon(t *testing.T) {
	t.Parallel()

	dealtTo := func(t *testing.T, kind string) int {
		t.Helper()

		w := newWorld()

		center := protocol.Hex{Q: 0, R: 0}
		if !isWalkable(w, center) {
			t.Skip("origin is not walkable on this map")
		}

		pid, token := w.PlaceEntityForTest(center)

		instID := w.GrantItemForTest(pid, "wyrmslayer-greatsword")
		if err := w.SubmitIntent(equipIntent(pid, token, instID)); err != nil {
			t.Fatalf("equip wyrmslayer: %v", err)
		}

		monsterHex := walkableNeighbor(t, w, center)
		monsterID := w.PlaceMonsterKindForTest(monsterHex, kind)

		// Melee-attack the monster via an entity-targeted attack intent; the
		// monster has no path set, so it does not retaliate — isolating the
		// attacker's damage (mirrors melee_damage_test.go's
		// TestMeleeDamageUsesClassCloseWeapon).
		if err := w.SubmitIntent(entityAttackIntent(pid, token, monsterID)); err != nil {
			t.Fatalf("SubmitIntent(melee): %v", err)
		}

		w.ResolveCombatOnlyForTest()

		monster, ok := entityOfSnap(w.Snapshot(), monsterID)
		if !ok {
			t.Fatalf("%s missing after a single melee attack", kind)
		}

		return game.MonsterMaxHPForTest(kind) - monster.HP
	}

	// re-derived: gear keystone rebalance (damage 4 -> 9).
	if got, want := dealtTo(t, kindWolf), 9; got != want {
		t.Errorf("wyrmslayer dealt %d to a wolf, want %d (no multiplier)", got, want)
	}

	// re-derived: gear keystone rebalance (9 * 150 / 100 = 13, truncated).
	if got, want := dealtTo(t, kindDragon), 13; got != want {
		t.Errorf("wyrmslayer dealt %d to a dragon, want %d (9 * 150%%)", got, want)
	}
}

// TestSnapshotMonsterNameAndKind: a monster's wire Entity carries its
// kind's display name (Name) and registry id (MonsterKind) — previously
// Name was always empty for a monster and MonsterKind did not exist. A
// player's MonsterKind stays empty (no field collision with its own Name).
func TestSnapshotMonsterNameAndKind(t *testing.T) {
	t.Parallel()

	w := newWorld()

	me := joinNamed(t, w, "hero")

	trollHex := walkableNeighbor(t, w, me.Hex)
	trollID := w.PlaceMonsterKindForTest(trollHex, kindTroll)

	snap := w.Snapshot()

	troll, ok := entityOfSnap(snap, trollID)
	if !ok {
		t.Fatalf("troll %d missing from snapshot", trollID)
	}

	if got, want := troll.Name, "Troll"; got != want {
		t.Errorf("troll Name = %q, want %q", got, want)
	}

	if got, want := troll.MonsterKind, kindTroll; got != want {
		t.Errorf("troll MonsterKind = %q, want %q", got, want)
	}

	player, ok := entityOfSnap(snap, me.EntityID)
	if !ok {
		t.Fatalf("player %d missing from snapshot", me.EntityID)
	}

	if got, want := player.Name, "hero"; got != want {
		t.Errorf("player Name = %q, want %q", got, want)
	}

	if got, want := player.MonsterKind, ""; got != want {
		t.Errorf("player MonsterKind = %q, want %q", got, want)
	}
}
