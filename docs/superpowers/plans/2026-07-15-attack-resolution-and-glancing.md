# Attacks-Before-Moves + Rogue Glance% Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Flip combat resolution so attacks land before moves against pre-move positions (#104), and add the Rogue's `glance%` class passive — a 20% chance an incoming hit is halved (#91).

**Architecture:** `internal/game/world.go`'s `resolveCombatLocked` currently runs moves-then-attacks via `moveAndBumpLocked` (bumps deferred and re-checked post-move — the retreat-dodge). We split that into a pre-move bump scan + a pure move phase and call `attackLocked` between them. The glance is a pure-data rule card (`content.go`) folded victim-side by `rollDamageLocked` — the take-damage mirror of the existing elf-crit card — plus a new `classCards` seam beside `speciesCards`. No wire/state shape changes: no snapshot version bump, no new `window.game` fields.

**Tech Stack:** Go (module root), `internal/protocol` constants regenerated into `client/src/protocol.gen.ts` via tygo (`make protocol`).

**Spec:** `docs/superpowers/specs/2026-07-15-attack-resolution-and-glancing-design.md` (approved, merged in PR #108).

## Global Constraints

- **Determinism:** all randomness through the per-turn seeded PCG. When a seeded test's expectation shifts, **re-derive the expected value, never weaken the assertion** (no ranges, no tolerance). Use the re-derivation protocol below.
- **Everything lands via a PR** referencing #104 and #91; never push to main. Merge waits for the maintainer's `ready to merge` label.
- **FEATURES.md updates in the same PR** as the mechanic change (Task 4).
- Rule cards are **pure data** (struct kinds + ints, never closures).
- Game-rule numbers live in `internal/protocol` (`RogueGlanceChancePercent = 20`, `GlanceDamagePercent = 50` — exact values from the spec).
- Go tests use the `got, want` inline-declaration assertion style (`.claude/rules/go-style.md`).
- Go may not be on PATH; the Makefile falls back to `/usr/local/go/bin/go`. Run `set -o pipefail` before piping any command whose success you check.
- `make protocol` after any `internal/protocol` change; never hand-edit `client/src/protocol.gen.ts`.

## Re-derivation protocol (used by Tasks 1 and 2)

The order flip moves the mover-shuffle rng draws *after* the attack-phase draws, and the glance adds a draw on every take-damage fold with a Rogue victim — every downstream draw in a turn shifts. When `make test` fails on a seeded expectation:

1. Read the failing assertion and the actual value.
2. Confirm the delta is explainable by rng-stream reordering or the new glance draw — the actual value must still be a *legal* outcome (a real damage total: base ± flat cards, ×2 crit, halved glance with the ≥1 floor; a real drop id from that kind's table). If the value is impossible arithmetic, it's a bug in your change, not a re-derivation.
3. Update the pinned constant/expectation to the observed value. For seed-hunting tests (`drops_test.go`'s `killDropSeed`/`killMissSeed` pattern), hunt a new seed that produces the intended scenario instead of changing what the test means.
4. Add/extend the test comment: `re-derived: #104 attacks-before-moves reordered rng consumption` (or `#91 glance adds a take-damage draw`).

---

### Task 1: Flip resolution order — attacks before moves (#104)

**Files:**
- Modify: `internal/game/world.go` (`resolveCombatLocked` ~1364-1402, `pendingBump`/`pendingAttack` types ~1242-1251, `moveAndBumpLocked` ~1557-1622, doc comments on `attackLocked` ~1624, `resolveRangedLocked` ~1672, `resolveGroundTargetedLocked` ~1730, `resolveEntityTargetedLocked` ~1781)
- Test: `internal/game/combat_test.go` (`TestBumpRetreatDodgesDamage` ~116-185), `internal/game/entity_targeted_ranged_test.go` (~22-114), new `internal/game/combat_order_test.go`

**Interfaces:**
- Consumes: existing helpers `hasOpposing`, `opposingOccupants`, `removeEntity`, `attackLocked(rng, byHex, attacks)`, `resolveDeathsLocked`, test bridges `SetPathForTest`, `ResolveCombatOnlyForTest`, `PlaceMonsterForTest`, `SetHPForTest`.
- Produces: `collectBumpsLocked(byHex, members) ([]pendingAttack, map[int64]bool)` and `movePhaseLocked(rng, byHex, members, bumped)` — private to the package; `resolveCombatLocked`'s signature is unchanged.

- [ ] **Step 1: Rewrite the retreat test to assert the NEW rule**

In `internal/game/combat_test.go`, replace `TestBumpRetreatDodgesDamage` (lines ~116-185) entirely with:

```go
// TestBumpHitsRetreatingDefender (#104, attacks-before-moves): the defender
// vacates the bump-target hex during this same turn's MOVE phase, but the
// attack phase has already resolved against pre-move positions — the bump
// lands anyway. The defender takes the hit, then completes its retreat; the
// attacker stays put (a bump never moves the attacker, and its path is
// retained). This replaces TestBumpRetreatDodgesDamage: the retreat-dodge
// (an automatic miss on vacation) is removed by design — retreat now trades
// hits for distance.
//
// The retreating entity here is the monster; its path is set directly via
// SetPathForTest and resolved with ResolveCombatOnlyForTest (skips
// thinkMonstersLocked), because the real AI never voluntarily retreats a
// monster away from a player. This test is about the combat machinery
// (collectBumpsLocked/attackLocked/movePhaseLocked ordering), independent of
// which AI drives it.
func TestBumpHitsRetreatingDefender(t *testing.T) {
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
		t.Fatalf("monster %d should survive one sword hit", monsterID)
	}

	if got, want := monster.HP, protocol.MonsterMaxHP-game.ItemDamageForTest("iron-sword"); got != want {
		t.Errorf("monster HP = %d, want %d (the bump lands against the pre-move position)", got, want)
	}

	if got, want := monster.Hex, escapeHex; got != want {
		t.Errorf("monster hex = %v, want %v (the retreat itself still lands, after the hit)", got, want)
	}

	if got, want := hexOfSnap(snap, me.EntityID), me.Hex; got != want {
		t.Errorf("attacker hex = %v, want unchanged %v (a bump never moves the attacker)", got, want)
	}
}
```

- [ ] **Step 2: Add the killed-mover test**

Create `internal/game/combat_order_test.go`:

```go
package game_test

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestKilledEntityDoesNotMove (#104): an entity killed in the attack phase
// does not get its move — the spec's death-timing consequence. A 1-HP
// monster with a queued retreat path dies to the bump (resolved first) and
// is removed; no "move" combat event is ever logged for it.
func TestKilledEntityDoesNotMove(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(3)

	var buf bytes.Buffer

	w.SetLogger(slog.New(slog.NewJSONHandler(&buf, nil)))

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)
	w.SetHPForTest(monsterID, 1)

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

	if _, ok := entityOfSnap(w.Snapshot(), monsterID); ok {
		t.Fatalf("monster %d should be dead and removed", monsterID)
	}

	for _, ev := range eventsOfKind(slogEvents(t, &buf), "move") {
		if id, ok := ev["id"].(float64); ok && int64(id) == monsterID {
			t.Errorf("killed monster %d logged a move event %v; the dead never move", monsterID, ev)
		}
	}
}
```

(`slogEvents`/`eventsOfKind` already exist — they're used by `combat_slog_test.go` and `entity_targeted_ranged_test.go` in this same package.)

- [ ] **Step 3: Update the entity-targeted ranged tests to the new rule**

In `internal/game/entity_targeted_ranged_test.go`:

a) `TestEntityTargetedShotFollowsSidestepAndHits` (~22-59): behavior unchanged (still hits), but the mechanism reversed — update name and comment:

```go
// TestEntityTargetedShotHitsSidesteppingTarget (#104, attacks-before-moves):
// the shot resolves against PRE-MOVE positions, so a monster that sidesteps
// this same turn is hit where it stood; the sidestep itself then lands in
// the move phase. (Pre-#104 this test asserted the post-move re-aim; the
// assertions are identical — hit lands, sidestep lands — only the mechanism
// changed.)
func TestEntityTargetedShotHitsSidesteppingTarget(t *testing.T) {
```

(body unchanged.)

b) Replace `TestEntityTargetedShotFleeingBeyondRangeFizzles` (~61-114) entirely — the old behavior (fleeing beyond range dodges the shot) is the retreat-dodge, removed by design:

```go
// TestEntityTargetedShotHitsBeforeFlee (#104, attacks-before-moves): a
// monster in range at the attack phase is hit even when its move this same
// turn would have carried it beyond the shooter's range — attacks resolve
// against pre-move positions, so same-tick flight no longer dodges a
// committed shot. (This replaces TestEntityTargetedShotFleeingBeyondRangeFizzles,
// which asserted the pre-#104 post-move re-aim fizzle.)
func TestEntityTargetedShotHitsBeforeFlee(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	rogueHex := protocol.Hex{Q: 0, R: 0}
	monsterHex := protocol.Hex{Q: 4, R: 0} // distance 4 == shortbow range — in range pre-move
	fled := protocol.Hex{Q: 5, R: 0}       // distance 5 — would be out of range post-move

	rogueID, token := w.PlaceEntityForTest(rogueHex)
	w.SetClassForTest(rogueID, protocol.ClassRogue)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	if err := w.SubmitIntent(entityAttackIntent(rogueID, token, monsterID)); err != nil {
		t.Fatalf("SubmitIntent(entity-targeted attack): %v", err)
	}

	w.SetPathForTest(monsterID, []protocol.Hex{fled})

	w.ResolveCombatOnlyForTest()

	snap := w.Snapshot()

	if got, want := hexOfSnap(snap, monsterID), fled; got != want {
		t.Fatalf("monster hex = %v, want %v (the flee itself still lands, after the hit)", got, want)
	}

	wantHP := protocol.MonsterMaxHP - rangedDamage(t, protocol.ClassRogue)
	if got := entityHP(t, snap, monsterID); got != wantHP {
		t.Errorf("monster HP = %d, want %d (the shot resolves pre-move and hits)", got, wantHP)
	}
}
```

- [ ] **Step 4: Run the three test files to verify they fail**

Run: `cd /var/home/starquake/src/github.com/starquake/mediumrogue && go test ./internal/game/ -run 'TestBumpHitsRetreatingDefender|TestKilledEntityDoesNotMove|TestEntityTargetedShotHitsBeforeFlee|TestEntityTargetedShotHitsSidesteppingTarget' -v` (use `/usr/local/go/bin/go` if `go` is absent)

Expected: `TestBumpHitsRetreatingDefender`, `TestKilledEntityDoesNotMove`, and `TestEntityTargetedShotHitsBeforeFlee` FAIL (old order still dodges); `TestEntityTargetedShotHitsSidesteppingTarget` passes (behavior same either way).

- [ ] **Step 5: Implement the flip in `world.go`**

a) Replace the two pending types (~1242-1251) with one:

```go
// pendingAttack is a bump committed in the attack phase (#104,
// attacks-before-moves): a move intent whose next step was opposing-held on
// the PRE-MOVE board. The attacker stays put (path retained — a standing
// intent keeps attacking); target is the victim hex as it stood before any
// move this turn.
type pendingAttack struct {
	attacker *entity
	target   protocol.Hex
}
```

(Delete `pendingBump` entirely.)

b) Replace `moveAndBumpLocked` (~1557-1622) with two functions:

```go
// collectBumpsLocked scans the PRE-MOVE board for this turn's bump attacks
// (#104, attacks-before-moves): a mover whose next step is an opposing-held
// hex commits its turn to an attack on that hex and will not move (path
// retained). Consumes no rng — detection reads the static pre-move board in
// members' id-sorted order, so the returned attack order is deterministic
// without a draw. Returns the attacks and the committed bumpers' ids for
// movePhaseLocked to skip. The old retreat-dodge (a deferred bump
// re-checked post-move, completing as a move when the defender vacated —
// fizzle reason bump_target_vacated) is removed by design: a committed
// attack always lands. Callers hold w.mu.
func (w *World) collectBumpsLocked(
	byHex map[protocol.Hex][]*entity, members []*entity,
) ([]pendingAttack, map[int64]bool) {
	var attacks []pendingAttack

	bumped := make(map[int64]bool)

	for _, m := range members {
		if len(m.path) == 0 {
			continue
		}

		if hasOpposing(byHex[m.path[0]], m) {
			attacks = append(attacks, pendingAttack{m, m.path[0]})
			bumped[m.id] = true
		}
	}

	return attacks, bumped
}

// movePhaseLocked resolves the move phase, AFTER attacks (#104): movers
// advance one hex from their path in seeded-shuffled order — skipping
// entities that committed a bump this turn (bumped; a bump is the turn's
// whole action) and entities killed in the attack phase (hp <= 0 — the dead
// never move; deaths are removed later by resolveDeathsLocked). A
// destination that is opposing-held on the evolving board (including a
// hostile that arrived this same phase) blocks the mover — it waits, path
// retained, and next turn the standing intent becomes a bump. A
// same-faction destination at StackCap also waits, path retained. Callers
// hold w.mu.
func (w *World) movePhaseLocked(
	rng *mrand.Rand, byHex map[protocol.Hex][]*entity, members []*entity, bumped map[int64]bool,
) {
	movers := make([]*entity, 0, len(members))

	for _, e := range members {
		if len(e.path) > 0 && !bumped[e.id] && e.hp > 0 {
			movers = append(movers, e)
		}
	}

	slices.SortFunc(movers, func(a, b *entity) int { return int(a.id - b.id) })
	rng.Shuffle(len(movers), func(i, j int) { movers[i], movers[j] = movers[j], movers[i] })

	for _, m := range movers {
		next := m.path[0]
		occs := byHex[next]

		if hasOpposing(occs, m) || len(occs) >= protocol.StackCap {
			continue // blocked (hostile-held or full) → wait, path retained
		}

		from := m.hex
		byHex[m.hex] = removeEntity(byHex[m.hex], m)
		byHex[next] = append(byHex[next], m)
		m.hex = next
		m.path = m.path[1:]
		w.logger.Info(combatLogMsg, "event", combatEventMove, "id", m.id, "kind", m.kind, "from", from, "to", next)
	}
}
```

c) In `resolveCombatLocked` (~1386-1399), replace

```go
	attacks := w.moveAndBumpLocked(rng, byHex, members)
```
…through…
```go
	w.attackLocked(rng, byHex, attacks)
```

with:

```go
	// #104, attacks-before-moves: the attack phase resolves first, against
	// PRE-MOVE positions (byHex as built above), then movers advance. A
	// committed attack always lands; retreat trades hits for distance. Note
	// the rng-consumption order is contractual for determinism: bump victim
	// picks + damage folds draw first, the mover shuffle draws after.
	attacks, bumped := w.collectBumpsLocked(byHex, members)

	w.attackLocked(rng, byHex, attacks)

	w.movePhaseLocked(rng, byHex, members, bumped)
```

(Keep the existing walk-over-auto-pickup NOTE comment; it may sit above the new block.)

d) Update stale doc comments — they describe the post-move world:
- `attackLocked` (~1624-1630): change "the re-check ensured at least one" guard comment to "collectBumpsLocked ensured at least one on the pre-move board; a same-phase state change here would be a bug". Change "against post-move positions" phrasing to "against pre-move positions (#104)".
- `resolveRangedLocked` (~1672-1693) / `resolveGroundTargetedLocked` (~1730-1745) / `resolveEntityTargetedLocked` (~1781-1802): replace every "post-move" with "pre-move (#104 — attacks resolve before moves, so entity/hex positions here are as-submitted)". For `resolveEntityTargetedLocked` note the out_of_range fizzle is now defensive only (nothing moves between submit validation and attack resolution; reachable only via the `SetAttackTargetForTest` bridge or mid-turn unequip).
- `rollDamageLocked` needs no change in this task.

e) Verify nothing else references the deleted pieces:

Run: `grep -rn 'moveAndBumpLocked\|pendingBump\|bump_target_vacated' internal/ client/ test/`
Expected: no hits outside comments you just rewrote (fix any stragglers).

- [ ] **Step 6: Run the new tests — verify they pass**

Run: `go test ./internal/game/ -run 'TestBumpHitsRetreatingDefender|TestKilledEntityDoesNotMove|TestEntityTargetedShot' -v`
Expected: PASS (all four).

- [ ] **Step 7: Run the full unit suite and re-derive fallout**

Run: `set -o pipefail && make test 2>&1 | tail -40`

Expected: possible failures in seeded tests (`drops_test.go`'s pinned `killDropSeed`/`killMissSeed`, crit-weapon tests in `melee_damage_test.go`/`starter_content_test.go`, `species_test.go` elf-crit pins, `TestBumpRandomVictimOnStackedHexIsReproducible`) — the mover shuffle now draws *after* the attack rolls. Apply the re-derivation protocol (top of this plan) to each. Tests that consume no chance/pick rng in-fold (plain fighter-vs-wolf damage) keep their values.

Then: `set -o pipefail && make test-integration 2>&1 | tail -20`
Expected: PASS (integration asserts damage happened, not stream positions; re-derive the same way if one pins a seeded value).

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "feat(game): resolve attacks before moves (#104)

Attack phase now runs first, against pre-move positions: a committed
attack always lands. The retreat-dodge (deferred bump re-checked
post-move, fizzle bump_target_vacated) is removed by design; entities
killed in the attack phase do not get their move. Seeded expectations
re-derived where the rng stream reordered."
```

---

### Task 2: Rogue glance% class passive (#91)

**Files:**
- Modify: `internal/protocol/protocol.go` (after the per-species const block, ~line 260), `internal/game/content.go` (card table ~10-36), `internal/game/world.go` (`rollDamageLocked` ~1439-1448), `internal/game/items.go` (`mustValidateContent`)
- Generated: `client/src/protocol.gen.ts` (via `make protocol` — never by hand)
- Test: new `internal/game/glance_test.go`, `internal/game/content_test.go` (classCards unit test)

**Interfaces:**
- Consumes: `ruleCard`/`condition`/`effect` types and `condChance`/`effMulPct`/`evTakeDamage` kinds (`rules.go`), `speciesCards` pattern (`content.go`), `validateRuleCards(id string, cards []ruleCard)` (`items.go`/`monsters.go`).
- Produces: `protocol.RogueGlanceChancePercent = 20`, `protocol.GlanceDamagePercent = 50`, `classCards(class string) []ruleCard` (package-private, `content.go`).

- [ ] **Step 1: Write the failing tests**

Create `internal/game/glance_test.go`:

```go
package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// Seeds pinned so the wolf's bump on the Rogue does / does not proc the
// glance card's chance condition — found by scanning seeds during
// implementation (the drops_test.go killDropSeed/killMissSeed pattern). If
// a change reorders rng consumption these move: re-derive by re-scanning,
// never by weakening the halved/full assertions.
const (
	glanceProcSeed = 1 // REPLACE while implementing: seed where the glance procs
	glanceMissSeed = 2 // REPLACE while implementing: seed where it does not
)

// glanceScenario drives one wolf bump into a stationary Rogue under seed
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
// (GlanceDamagePercent), never negated. A wolf bump (base 3) lands 3 full
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
// victim's take-damage fold carries no chance card, so the same wolf bump
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
```

Add to `internal/game/content_test.go` (an in-package `package game` test file — check its package clause first; if it is `game_test`, export `classCards` via `export_test.go` as `ClassCardsForTest = classCards` and test through that):

```go
// TestClassCards: the Rogue carries exactly the glance passive; other
// classes (and monsters' empty class) carry none.
func TestClassCards(t *testing.T) {
	t.Parallel()

	if got, want := len(classCards(protocol.ClassRogue)), 1; got != want {
		t.Errorf("len(classCards(rogue)) = %d, want %d", got, want)
	}

	for _, class := range []string{protocol.ClassFighter, protocol.ClassMage, ""} {
		if got := classCards(class); got != nil {
			t.Errorf("classCards(%q) = %v, want nil", class, got)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/game/ -run 'TestRogueGlance|TestNonRogueTakesFullDamage|TestClassCards' -v`
Expected: FAIL — `protocol.RogueGlanceChancePercent`, `protocol.GlanceDamagePercent`, `classCards` undefined (compile error).

- [ ] **Step 3: Add the protocol constants**

In `internal/protocol/protocol.go`, directly after the per-species const block (`DwarfDamageReduction`, ~line 260):

```go
// Per-class passive bonuses (tunable). The Rogue's glance is the first
// class passive: the decoupled defender-side combat chance (#69/#91,
// amended 2026-07-15) — a glancing hit is HALVED, never fully negated (and
// the take-damage fold still floors every landed hit at 1).
const (
	// RogueGlanceChancePercent is the percent chance an incoming hit on a
	// Rogue only glances (GlanceDamagePercent applies).
	RogueGlanceChancePercent = 20
	// GlanceDamagePercent is a glancing hit's damage multiplier in percent
	// (50 = half damage), shared by any future glance-granting content.
	GlanceDamagePercent = 50
)
```

Then run: `make protocol` — regenerates `client/src/protocol.gen.ts`. Commit the regenerated file with this task; never edit it by hand.

- [ ] **Step 4: Add the card and the classCards seam**

In `internal/game/content.go`, extend the species-card var block (~10-21) with:

```go
	rogueGlanceCards = []ruleCard{
		{event: evTakeDamage, when: []condition{{kind: condChance, n: protocol.RogueGlanceChancePercent}},
			then: effect{kind: effMulPct, n: protocol.GlanceDamagePercent}},
	}
```

And after `speciesCards` (~36) add:

```go
// classCards returns a class's passive rule cards (nil for other classes
// and for monsters' empty class). The Rogue's glance% (#91) is the first
// class passive: the take-damage mirror of the elf-crit card — a
// chance-conditioned mulPct, pure content, no new pipeline event (the
// 2026-07-15 spec's point). Folded victim-side by rollDamageLocked, right
// after speciesCards.
func classCards(class string) []ruleCard {
	if class == protocol.ClassRogue {
		return rogueGlanceCards
	}

	return nil
}
```

(Use `rogueGlanceCards` consistently — the test in Step 1 calls `classCards`, not the var.)

- [ ] **Step 5: Fold it in `rollDamageLocked` and validate it at init**

In `internal/game/world.go` (~1445), change:

```go
	victimCards := slices.Concat(speciesCards(victim.species), victimGearCards(victim))
```
to:
```go
	// Species, then class, then gear: chance conditions consume the turn rng
	// in card order, so this concat order is contractual for determinism.
	victimCards := slices.Concat(speciesCards(victim.species), classCards(victim.class), victimGearCards(victim))
```

Also extend `rollDamageLocked`'s doc comment's "victim's take-damage cards (species + …)" phrasing to "(species + class + …)".

In `internal/game/items.go`, add to `mustValidateContent` (after `validateMonsterDefs(monsterDefs)`):

```go
	// Class passives ride the same card vocabulary as items/monsters —
	// validate them at init so a bad kind panics at process start, not
	// mid-combat (speciesCards predate this check and stay grandfathered;
	// extend this list when a card is added there).
	validateRuleCards("class:rogue", rogueGlanceCards)
```

- [ ] **Step 6: Pin the seeds and verify green**

Run: `go test ./internal/game/ -run 'TestRogueGlance' -v` — if `glanceProcSeed`/`glanceMissSeed` placeholders don't produce proc/miss, hunt them: temporarily loop seeds 1..50 in `glanceScenario` printing the HP loss (`go test -run TestRogueGlance -v` with a `t.Logf`), pick one seed per outcome, pin them with a comment naming the values, delete the loop.

Then run: `go test ./internal/game/ -run 'TestRogueGlance|TestNonRogueTakesFullDamage|TestClassCards' -v`
Expected: PASS.

- [ ] **Step 7: Full suite + re-derive Rogue-victim fallout**

Run: `set -o pipefail && make test 2>&1 | tail -30`
Expected: any seeded test with a Rogue *victim* now consumes one extra draw per take-damage fold — re-derive per protocol. (Tests with Rogue *attackers* are unaffected: the glance card's event is take-damage, and `applyRules` skips non-matching events before any rng draw.)

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "feat(game,protocol): Rogue glance% class passive (#91)

glance% = RogueGlanceChancePercent (20%) chance an incoming hit is
halved (GlanceDamagePercent 50), never negated — a chance-conditioned
take-damage card mirroring elf crit; no new pipeline event. First class
passive: new classCards seam beside speciesCards, folded victim-side in
rollDamageLocked, validated at init."
```

---

### Task 3: Documentation alignment (same PR — FEATURES.md rule)

**Files:**
- Modify: `docs/FEATURES.md`, `docs/design-decisions.md`, `docs/content-authoring.md`, `docs/design.md`

**Interfaces:** none (prose). Values quoted from `internal/protocol`, never memory: glance = `RogueGlanceChancePercent` 20% / `GlanceDamagePercent` 50; wolf damage 3.

- [ ] **Step 1: FEATURES.md — combat section (~lines 73-96)**

a) The "Entity-targeted single-target ranged attacks" bullet (~77-85): replace the re-validation sentence ("…AND re-validated at resolution against the victim's **post-move hex**: hits if still in range from the shooter's own post-move hex, else fizzles — the shot tracks a sidestepping or retreating target instead of trusting a stale hex.") with:

```markdown
  Validated at submit (entity exists+alive, hostile, in range); resolution
  (#104) runs against **pre-move positions**, so a committed shot always
  lands — the `out_of_range` fizzle survives only as a defensive guard
  (nothing moves between submit and the attack phase).
```

b) The "Phased resolution" bullet (~86-89): replace with:

```markdown
- **Phased resolution** (#104, attacks-before-moves): all attacks resolve
  simultaneously against **pre-move positions** (shared damage map — mutual
  kills are possible and intended; a stacked hex takes hits on a **random
  member**), then all moves resolve (seeded-RNG tie-break on hex overflow;
  an entity killed in the attack phase does not get its move). Committing
  to an attack always lands it; retreat means **trading hits for distance**
  — a one-action chaser that bumps you isn't gaining ground that turn.
```

c) The fizzle-reason list (~527): remove `bump_target_vacated` from the reasons enumeration.

- [ ] **Step 2: FEATURES.md — classes and constants**

a) Class table (~125): change the Rogue row's notes cell from `high single-target, squishy` to `high single-target, squishy; glance% passive — 20% chance an incoming hit is halved`.

b) Constants table (~574 area, wherever the species-passive constants rows sit — `grep -n 'ElfCritChancePercent' docs/FEATURES.md`): add a row:

```markdown
| `RogueGlanceChancePercent` / `GlanceDamagePercent` | 20 / 50 | Rogue class passive: chance an incoming hit is halved (never negated; floor 1 still applies) |
```

c) §5 "Decided but not yet built" (~592-601): delete the `glance% combat chance` entry and the `attacks-before-moves resolution flip` entry (both now built — the rest of that list stays).

- [ ] **Step 3: design-decisions.md — retire the open flag, mark decisions shipped**

a) Delete the **"Resolution order — attacks vs moves"** entry from "Open flags (doc vs implementation)" (added 2026-07-15; the gap it flagged is closed).

b) In the "Attacks resolve before moves" decided entry, change `(#104, decided 2026-07-15; not yet implemented.)` to `(#104, decided 2026-07-15; shipped.)`.

c) In the Q5 amendment, after "Rogue gets it as the class passive, proposed 20%." append " (Shipped: `RogueGlanceChancePercent`.)"

- [ ] **Step 4: content-authoring.md + design.md — drop the "not yet" caveats**

a) `docs/content-authoring.md` §2 combat-model bullet: replace the "*for now*: the reverse order … lands soon, after which …" phrasing with the shipped statement:

```markdown
- Within a turn, **all attacks land first — against pre-move positions —
  then all movement resolves** (#104): committing to an attack always lands
  it, and stepping away does not dodge. Two combatants can kill each other
  on the same turn.
```

b) `docs/content-authoring.md` §7 engine truths: change "(today moves-then-attacks; decided 2026-07-15, #104: attacks-then-moves, …)" to "(attacks-then-moves, #104: a committed attack always lands and retreat means trading hits for distance)".

c) `docs/design.md` §5 header amendment: delete the trailing sentence "Shipped behavior is still moves-first until #104's implementation lands." (the rest of the amendment note stays as history).

d) `docs/game-identity.md` (~57): change "`glance%` (the intended identity; not yet built — #91; softened from binary `evasion%` on 2026-07-15…)" to "`glance%` (shipped — #91; softened from binary `evasion%` on 2026-07-15: a glance halves a hit, never negates it)".

- [ ] **Step 5: Sweep for stragglers**

Run: `grep -rn 'bump_target_vacated\|post-move' docs/*.md internal/ client/src | grep -v superpowers | grep -v protocol.gen`
Expected: remaining "post-move" hits only in historical/amended notes (design.md §Amended notes) — none asserting current behavior. Fix any that do.

- [ ] **Step 6: Commit**

```bash
git add docs/
git commit -m "docs: FEATURES + design docs — attacks-before-moves and glance% are shipped (#104, #91)"
```

---

### Task 4: Full verification and PR

**Files:** none new.

- [ ] **Step 1: Full gate**

Run: `cd /var/home/starquake/src/github.com/starquake/mediumrogue && set -o pipefail && make check 2>&1 | tail -15`
Expected: lint, protocol-drift check, typecheck, tests, build all green. (Protocol drift is the check that catches a forgotten `make protocol`.)

- [ ] **Step 2: e2e**

Run: `set -o pipefail && make e2e 2>&1 | tail -20`
Expected: PASS. If `combat.spec.ts`/`ranged.spec.ts` flake, reproduce with `--repeat-each=3 --workers=9` before touching anything; fix causes (poll `window.game.turn`), never timeouts.

- [ ] **Step 3: Open the PR**

```bash
git push -u origin HEAD
gh pr create \
  --title "feat(game): attacks resolve before moves + Rogue glance% passive (#104, #91)" \
  --body "Implements docs/superpowers/specs/2026-07-15-attack-resolution-and-glancing-design.md (approved, PR #108).

- Attack phase resolves first, against pre-move positions — a committed attack always lands; retreat-dodge removed; killed entities don't move (#104).
- Rogue glance% class passive: 20% chance an incoming hit is halved, never negated — a chance-conditioned take-damage card, no new pipeline event; new classCards seam (#91).
- Seeded test expectations re-derived (rng stream reordered); FEATURES.md + design docs updated in-PR.

Closes #104. Closes #91."
```

Then watch CI to completion: `gh pr checks <n> --watch` — local-green is not mergeable. Do **not** merge; the `ready to merge` label is the maintainer's.

---

## Self-review notes (spec coverage)

- Spec Decision 1 (attacks-before-moves, bump always lands, fizzle removed, entity-targeted pre-move, death timing) → Task 1.
- Spec Decision 2 (one-action reaffirmed) → no code by design; docs already record it (PR #108).
- Spec Decision 3 (glance%, Rogue 20%, mulPct 50, all damage incl. AoE, floor-1 note) → Task 2. AoE-glance has no reachable in-game path today (only monsters attack players, and monsters only bump) — the `rollDamageLocked` seam covers every current and future damage path, so no AoE-specific test is possible or needed.
- Spec determinism note → re-derivation protocol + contractual-order comments.
- Spec scope ("FEATURES.md updates in that same PR") → Task 3.
