# Melee as an Attack Intent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Player melee becomes an entity-targeted attack intent (one click = one swing, attacking never moves you); move-conversion becomes monster-only; walks stop adjacent to a hostile destination (#116).

**Architecture:** Additive first, flip second: Task 1 teaches `queueAttackLocked`/`resolveEntityTargetedLocked` the adjacent-melee branch while the old conversion still works; Task 2 migrates every player-melee test onto attack intents while both paths are live (suite stays green at every commit); Task 3 flips the rules (conversion scoped to monsters, player moves block, pathfind trims); Task 4 catches the client up (intent kind, keyboard routing, destination-clear mirror); Task 5 aligns docs. All on the existing draft PR #118 branch (`docs/melee-attack-intent-spec`) — spec, plan, and build merge together (maintainer's workflow decision, 2026-07-15).

**Tech Stack:** Go server (`internal/game`), TS client (`client/src`), Playwright e2e. No protocol shape changes, no new constants, no snapshot version bump.

**Spec:** `docs/superpowers/specs/2026-07-15-melee-attack-intent-design.md` (approved on the draft PR).

## Global Constraints

- Determinism: seeded expectations that shift are **re-derived, never weakened** (protocol below). Sort map-derived slices before rng.
- Suite green at every task commit: `set -o pipefail && make check 2>&1 | tail -15` (go may be at `/usr/local/go/bin/go`).
- One draft PR: commit and push to `docs/melee-attack-intent-spec`; do NOT open new PRs or merge; PR #118 is marked ready only in Task 5.
- Go tests use the `got, want` inline style (`.claude/rules/go-style.md`); e2e de-raced by cause, verified with `--repeat-each=3 --workers=9`.
- Melee exclusivity: at distance 1 an entity-targeted attack fires ONLY `meleeDefsFor` (never ranged too). Mage ground-targeted AoE untouched.
- Vocabulary: melee, never "bump" (except the increment sense and the two definitional glosses).

## Re-derivation protocol

Player melee moves from the conversion pass (with its stacked-hex rng victim pick) to the entity-targeted pass (id-sorted, no pick). When a seeded test fails after migration: confirm the new value is legal arithmetic (base ± cards, ×2 crit, glance half, ≥1 floor; real drop ids), update the pin (or re-hunt seeds for seed-hunting tests like `drops_test.go`), and comment `re-derived: #116 melee intent moved the rng stream`.

---

### Task 1: Server — adjacent melee on the attack intent (additive)

**Files:**
- Modify: `internal/game/world.go` (`queueAttackLocked` ~933-974, `resolveEntityTargetedLocked` ~1803+)
- Test: new `internal/game/melee_intent_test.go`

**Interfaces:**
- Consumes: `meleeDefsFor(e)` (items.go:415, fists/claws fallback — never empty), `itemDamage`, `rollDamageLocked`, `HexDistance`, `opposing`, sentinels `ErrAttackTargetNotFound`/`ErrAttackTargetNotHostile`/`ErrOutOfRange`/`ErrNoRangedWeapon`, test bridges `PlaceMonsterForTest`, `SetClassForTest`, `entityAttackIntent` (game_test, entity_targeted_ranged_test.go:16), `entityOfSnap`, `walkableNeighbor`, `step`, `newWorld`.
- Produces: an entity-targeted attack intent that is VALID for any adjacent hostile (no ranged weapon required) and resolves as a melee swing. Old conversion path untouched in this task.

- [ ] **Step 1: Write the failing tests** (`internal/game/melee_intent_test.go`, package `game_test`):

```go
package game_test

import (
	"errors"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestMeleeIntentDealsDamageAttackerStays (#116): an entity-targeted attack
// intent against an ADJACENT hostile is a melee swing — valid even for a
// fighter, who holds no ranged weapon — and the attacker's hex never
// changes (attacking is not a move).
func TestMeleeIntentDealsDamageAttackerStays(t *testing.T) {
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
		t.Fatalf("SubmitIntent(adjacent melee attack): %v", err)
	}

	snap := step(t, w)

	monster, ok := entityOfSnap(snap, monsterID)
	if !ok {
		t.Fatalf("monster %d should survive one sword hit", monsterID)
	}

	if got, want := monster.HP, protocol.MonsterMaxHP-game.ItemDamageForTest("iron-sword"); got != want {
		t.Errorf("monster HP = %d, want %d (the melee swing lands)", got, want)
	}

	if got, want := hexOfSnap(snap, me.EntityID), me.Hex; got != want {
		t.Errorf("attacker hex = %v, want unchanged %v (attacking never moves you)", got, want)
	}
}

// TestMeleeIntentOneClickOneSwing (#116): an attack intent is one-shot —
// with no re-submission, the second turn deals no further damage.
func TestMeleeIntentOneClickOneSwing(t *testing.T) {
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
		t.Fatalf("SubmitIntent: %v", err)
	}

	step(t, w) // swing lands

	afterSecond := step(t, w) // nothing re-submitted — no second swing

	monster, ok := entityOfSnap(afterSecond, monsterID)
	if !ok {
		t.Fatalf("monster %d should still be alive", monsterID)
	}

	if got, want := monster.HP, protocol.MonsterMaxHP-game.ItemDamageForTest("iron-sword"); got != want {
		t.Errorf("monster HP after idle turn = %d, want %d (one click = one swing)", got, want)
	}
}

// TestMeleeIntentNonAdjacentNoRangedRejected (#116): a fighter (melee only)
// naming a hostile TWO hexes away is rejected with ErrOutOfRange at submit —
// melee reach is exactly 1, and no ranged weapon covers the gap. (This case
// used to reject as ErrNoRangedWeapon; the melee branch folds it into the
// unified reach check.)
func TestMeleeIntentNonAdjacentNoRangedRejected(t *testing.T) {
	t.Parallel()

	w := newWorld()

	rogueID, token := w.PlaceEntityForTest(protocol.Hex{Q: 0, R: 0})
	w.SetClassForTest(rogueID, protocol.ClassFighter)
	monsterID := w.PlaceMonsterForTest(protocol.Hex{Q: 2, R: 0})

	got := w.SubmitIntent(entityAttackIntent(rogueID, token, monsterID))
	if want := game.ErrOutOfRange; !errors.Is(got, want) {
		t.Errorf("err = %v, want %v", got, want)
	}
}

// TestMeleeIntentHitsNamedVictimOnStack (#116): melee on a stacked hex hits
// exactly the NAMED victim, like a bow shot — the conversion path's seeded
// stack pick does not apply to attack intents.
func TestMeleeIntentHitsNamedVictimOnStack(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	monsterHex := walkableNeighbor(t, w, me.Hex)
	firstID := w.PlaceMonsterForTest(monsterHex)
	secondID := w.PlaceMonsterForTest(monsterHex) // stacks on the same hex

	if err := w.SubmitIntent(entityAttackIntent(me.EntityID, me.Token, secondID)); err != nil {
		t.Fatalf("SubmitIntent: %v", err)
	}

	snap := step(t, w)

	if got, want := entityHP(t, snap, secondID), protocol.MonsterMaxHP-game.ItemDamageForTest("iron-sword"); got != want {
		t.Errorf("named victim HP = %d, want %d", got, want)
	}

	if got, want := entityHP(t, snap, firstID), protocol.MonsterMaxHP; got != want {
		t.Errorf("bystander HP = %d, want %d (untouched)", got, want)
	}
}
```

(`entityHP`/`hexOfSnap` already exist in the game_test package. If `PlaceMonsterForTest` refuses stacking, check its body — it places directly; two placements on one hex are fine for monsters, StackCap governs moves.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/game/ -run 'TestMeleeIntent' -v`
Expected: FAIL — `ErrNoRangedWeapon` on submit for the fighter cases (the pre-gate rejects melee-only attackers).

- [ ] **Step 3: Implement**

a) `queueAttackLocked` — remove the top-level `rangedDefFor` pre-gate; entity branch gets the unified reach check; ground branch keeps the ranged requirement:

```go
func (w *World) queueAttackLocked(e *entity, target protocol.Hex, targetEntityID int64) error {
	if targetEntityID != 0 {
		victim, ok := w.entities[targetEntityID]
		if !ok || victim.hp <= 0 {
			return ErrAttackTargetNotFound
		}

		if !opposing(e, victim) {
			return ErrAttackTargetNotHostile
		}

		// Reach (#116): an ADJACENT victim is always attackable — melee, and
		// every entity is melee-armed (meleeDefsFor falls back to fists/claws).
		// Beyond that, at least one held ranged/magic weapon must reach
		// (dual-wield: any, not best). A melee-only attacker naming a distant
		// victim therefore rejects as out-of-range, not as weaponless.
		dist := HexDistance(e.hex, victim.hex)
		if dist != 1 && len(rangedDefsFor(e, dist)) == 0 {
			return ErrOutOfRange
		}

		e.attackTargetEntity = targetEntityID
		e.attackTarget = nil
		e.path = nil
		e.pending = pendingItemAction{}

		return nil
	}

	// Ground-targeted (hex) attacks stay ranged-only: there is no hex-targeted
	// melee, so the old weapon gate lives on in this branch.
	if rangedDefFor(e) == nil {
		return ErrNoRangedWeapon
	}

	if len(rangedDefsFor(e, HexDistance(e.hex, target))) == 0 {
		return ErrOutOfRange
	}

	t := target
	e.attackTargetEntity = 0
	e.attackTarget = &t
	e.path = nil
	e.pending = pendingItemAction{}

	return nil
}
```

(Keep the existing doc comment, amended: entity-targeted reach is "adjacent = melee (#116) OR any reaching ranged weapon"; note the ErrNoRangedWeapon → ErrOutOfRange change for entity-targeted melee-only attackers.)

b) `resolveEntityTargetedLocked` — melee branch after the `target_gone` guard, before the ranged def lookup:

```go
	// #116: an adjacent victim means this intent is a MELEE swing, exclusively
	// — the weapon-by-distance identity (a rogue swings the dagger adjacent,
	// shoots the bow at range), so ranged/magic defs never also fire at
	// distance 1. Every held melee weapon lands its own hit on the named
	// victim (dual-wield parity with the monster conversion path in
	// attackLocked), each through the full pipeline. Positions are pre-move
	// (#104) and nothing moves between submit validation and this phase, so
	// adjacency here matches adjacency at submit.
	if HexDistance(attacker.hex, victim.hex) == 1 {
		for _, weapon := range meleeDefsFor(attacker) {
			base := itemDamage(weapon)
			dealt := w.rollDamageLocked(rng, attacker, victim, weapon, base)
			damage[victim.id] += dealt

			w.logger.Info(combatLogMsg, "event", combatEventAttack, "attacker", attacker.id, "victim", victim.id,
				"weapon", weapon.id, "base", base, "dealt", dealt)
		}

		return
	}

	// Also update this function's unequipped-fizzle guard: resolveRangedLocked
	// fizzles when rangedDefFor(e) == nil BEFORE delegating here — that guard
	// must move below the entity-targeted delegation or be made melee-aware,
	// otherwise a fighter's melee intent fizzles as "unequipped". Check
	// resolveRangedLocked (~1694): the `rangedDefFor(e) == nil` continue must
	// NOT swallow an entity-targeted intent whose victim is adjacent.
```

c) In `resolveRangedLocked`, reorder: resolve `targetEntityID != 0` intents by delegating FIRST (the melee branch handles weaponless-ranged attackers); keep the `rangedDefFor(e) == nil` unequipped fizzle only for ground-targeted intents. Update both doc comments.

- [ ] **Step 4: Run the new tests — verify pass**

Run: `go test ./internal/game/ -run 'TestMeleeIntent' -v`
Expected: PASS (all four).

- [ ] **Step 5: Full gate + commit**

Run: `set -o pipefail && make check 2>&1 | tail -15` — expected green (this task is additive; conversion still works, no existing expectations move).

```bash
git add -A && git commit -m "feat(game): adjacent melee on the entity-targeted attack intent (#116)

Additive: queueAttackLocked accepts an adjacent hostile with no ranged
weapon (unified reach check; melee-only distant target now ErrOutOfRange);
resolveEntityTargetedLocked resolves distance-1 intents as an exclusive
melee swing (every held melee weapon, dual-wield parity). The move-
conversion path is untouched until the test migration lands."
git push
```

---

### Task 2: Migrate player-melee tests to attack intents (both paths live)

**Files:**
- Modify (unit, game_test): `internal/game/combat_test.go`, `melee_damage_test.go`, `species_test.go`, `starter_content_test.go`, `kinds_combat_test.go`, `xp_test.go`, `drops_test.go`, `quest_test.go`, `glance_test.go` (only if it drives PLAYER melee — its scenario is monster-side, likely untouched), `combat_slog_test.go`, `combat_log_test.go`, `inventory_actions_test.go`, `items_test.go`, `unequip_test.go`, `level_test.go`, `monster_test.go`, `bubble_test.go`, `combat_gating_test.go`, `snapshot_test.go` — wherever a PLAYER kills/hits via `submitOK(w, me, monsterHex)` or a move-path onto a monster.
- Modify (integration): `test/integration/combat_test.go`, `class_test.go`, `species_test.go`, `xp_test.go`, `gear_test.go`, `quest_kill_test.go`, `persistence_test.go`, `inventory_test.go` — wherever `postIntent(t, ts, me, monsterHex)` is the player's melee.

**Interfaces:**
- Consumes: Task 1's melee intent; `entityAttackIntent` (game_test); `postJSON` (integration).
- Produces: a `postEntityAttackIntent(t, ts, me, targetEntityID)` integration helper (mirror of `postPickupIntent`, `Kind: protocol.IntentAttack, TargetEntityID: id`, expect 202); every player melee in tests submitted as an attack intent, re-submitted before each turn that should swing (intents are one-shot — a 3-hit kill is three submit+step rounds).

- [ ] **Step 1: The migration pattern** — for each site:

```go
// BEFORE (standing conversion — one submit, N steps):
if !submitOK(w, me, monsterHex) { t.Fatalf(...) }
first := step(t, w)
second := step(t, w) // kept swinging

// AFTER (one-shot intents — submit before every swinging turn):
if err := w.SubmitIntent(entityAttackIntent(me.EntityID, me.Token, monsterID)); err != nil {
	t.Fatalf("SubmitIntent(melee): %v", err)
}
first := step(t, w)
if err := w.SubmitIntent(entityAttackIntent(me.EntityID, me.Token, monsterID)); err != nil {
	t.Fatalf("SubmitIntent(melee): %v", err)
}
second := step(t, w)
```

Integration mirror: replace melee-purposed `postIntent(t, ts, me, monsterHex)` with `postEntityAttackIntent(t, ts, me, monsterID)` per turn (monster ids come from the same turn bundles the tests already decode). `gear_test.go`'s fight-through policy: the "adjacent monster → bump it" branch changes to `postEntityAttackIntent` at the adjacent monster's id (extend `nearestMonster`-style helper to return the entity, or add `adjacentMonsterID(bundle, myHex)`).

Update each migrated test's doc comment where it narrates the conversion ("the move intent becomes the attack", "path retained keeps landing") to the intent model. Tests that drive MONSTER melee (SetPathForTest onto a player — glance_test's scenario, TestMonsterAIAttacksAdjacentPlayer, combat_order_test's killed-mover test, `TestMeleeHitsRetreatingDefender`'s PLAYER half is a player attack → migrate the player's submit, keep the monster's SetPathForTest) keep the conversion; that's the point of the split.

- [ ] **Step 2: Migrate, then run per-package and re-derive**

Run: `go test ./internal/game/ -count=1` then `make test-integration`. Apply the re-derivation protocol to every seeded failure (expected: `drops_test.go` seed pins — the stack-pick draw disappears and the resolution pass moves; crit-weapon and elf-crit pins in `melee_damage_test.go`/`species_test.go`/`starter_content_test.go`).

- [ ] **Step 3: Full gate + commit**

`set -o pipefail && make check 2>&1 | tail -15` → green.

```bash
git add -A && git commit -m "test: migrate player melee to attack intents (#116)

Player-side melee in unit + integration tests now submits entity-targeted
attack intents (one submit per swinging turn); monster-side conversion
tests keep SetPathForTest. New postEntityAttackIntent integration helper.
Seeded pins re-derived where the rng stream moved."
git push
```

---

### Task 3: Flip the rules — conversion monster-only, walks stop adjacent

**Files:**
- Modify: `internal/game/world.go` (`collectMeleeAttacksLocked`, `queueMoveLocked` ~885-901), `internal/game/bubble.go` (the single-step carve-out comment)
- Test: extend `internal/game/melee_intent_test.go`

**Interfaces:**
- Consumes: `occupiedByMonsterLocked(h)` (world.go ~2181) for the trim (queueMoveLocked is player-only — dispatched intents; monster AI sets paths directly).
- Produces: player movers never convert; a player path ending on a hostile hex is trimmed at submit.

- [ ] **Step 1: Write the failing tests** (append to `melee_intent_test.go`):

```go
// TestPlayerMoveOntoMonsterBlocks (#116): a player MOVE intent whose next
// step is hostile-held no longer converts to a swing — the mover waits
// (blocked by movePhaseLocked), nobody takes damage, nobody moves.
// NOTE: submit the move via SetPathForTest, not SubmitIntent — Task 3's
// queueMoveLocked trim would turn an adjacent monster-hex click into an
// empty path; this test pins the RESOLUTION rule for a path that ends
// hostile-held anyway (e.g. the monster stepped into a stale route).
func TestPlayerMoveOntoMonsterBlocks(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	monsterHex := walkableNeighbor(t, w, me.Hex)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	w.SetPathForTest(me.EntityID, []protocol.Hex{monsterHex})
	w.ResolveCombatOnlyForTest()

	snap := w.Snapshot()

	if got, want := entityHP(t, snap, monsterID), protocol.MonsterMaxHP; got != want {
		t.Errorf("monster HP = %d, want %d (a blocked move deals no damage)", got, want)
	}

	if got, want := hexOfSnap(snap, me.EntityID), me.Hex; got != want {
		t.Errorf("player hex = %v, want unchanged %v (blocked, not moved)", got, want)
	}
}

// TestWalkToMonsterHexStopsAdjacent (#116): clicking a distant monster's hex
// is a WALK whose path is trimmed at submit — the player ends on an adjacent
// hex and never swings on its own.
func TestWalkToMonsterHexStopsAdjacent(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	me, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	near := walkableNeighbor(t, w, me.Hex)
	monsterHex := walkableNeighbor(t, w, near) // two steps out
	if monsterHex == me.Hex {
		t.Skip("cramped map corner — no two-step line from spawn")
	}

	monsterID := w.PlaceMonsterForTest(monsterHex)

	if !submitOK(w, me, monsterHex) {
		t.Fatalf("SubmitIntent(walk to monster hex) failed")
	}

	var last protocol.TurnEvent
	for range 4 { // more turns than the path needs
		last = step(t, w)
	}

	if got, want := entityHP(t, last, monsterID), protocol.MonsterMaxHP; got != want {
		t.Errorf("monster HP = %d, want %d (a walk never swings)", got, want)
	}

	if got := hexOfSnap(last, me.EntityID); got == monsterHex {
		t.Errorf("player hex = %v — walked ONTO the monster hex; the trim should stop adjacent", got)
	}

	if got, want := game.HexDistance(hexOfSnap(last, me.EntityID), monsterHex), 1; got != want {
		t.Errorf("player distance to monster hex = %d, want %d (stopped adjacent)", got, want)
	}
}
```

(Caveat for the second test: the monster AI moves too — resolve with `step`, which runs `thinkMonstersLocked`, so the wolf may aggro and approach; distance-1 assertion is against the CLICKED hex, which the wolf may have left. If the executor finds this racy, pin it with `ResolveCombatOnlyForTest` loops instead of `step` — no AI — and note it in the test comment.)

- [ ] **Step 2: Run to verify failure** — the block test fails (conversion still swings for players); the trim test fails (player walks onto the hex after the kill… or swings). `go test ./internal/game/ -run 'TestPlayerMoveOntoMonsterBlocks|TestWalkToMonsterHexStopsAdjacent' -v`

- [ ] **Step 3: Implement**

a) `collectMeleeAttacksLocked`: skip non-monsters —

```go
	for _, m := range members {
		if m.kind != protocol.EntityMonster || len(m.path) == 0 {
			continue
		}
		...
```

with the doc comment updated: "#116: move-conversion is a MONSTER rule — the AI attacks by pathing onto players. A PLAYER mover whose next step is hostile-held no longer converts; movePhaseLocked's hasOpposing check blocks it (waits, path retained), and player melee arrives as an entity-targeted attack intent instead (resolveEntityTargetedLocked)."

b) `queueMoveLocked` trim (after the `path == nil` check):

```go
	// #116: a walk that ends on a hostile-held hex stops adjacent — attacking
	// is an explicit attack intent, never a move. Board read at submit time:
	// a monster that wanders mid-walk just leaves the route one hex short,
	// like any stale path. Trimming to empty is a valid no-op move (already
	// adjacent — the client routes that click as an attack anyway).
	if w.occupiedByMonsterLocked(path[len(path)-1]) {
		path = path[:len(path)-1]
	}
```

(Only players reach queueMoveLocked — intents are player-dispatched; the AI writes paths directly. Note that in the comment.)

c) `internal/game/bubble.go` single-step carve-out comment: drop the "standing melee intent" justification — a surviving single step is now just a deliberate adjacent move.

- [ ] **Step 4: Verify green + full gate**

`go test ./internal/game/ -run 'TestMelee|TestPlayerMove|TestWalkToMonster' -v` → PASS; then `set -o pipefail && make check 2>&1 | tail -15` → green (Task 2 already moved every player-melee test off conversion; a failure here means a straggler — migrate it the same way, don't revert the flip).

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat(game): move-conversion is monster-only; walks stop adjacent (#116)

A player move onto a hostile-held hex blocks instead of swinging;
queueMoveLocked trims a monster-held destination so distant clicks stop
adjacent. Attacking never moves the player."
git push
```

---

### Task 4: Client — honest intents, keyboard melee, destination mirror

**Files:**
- Modify: `client/src/main.ts` (`meleeAt`, `bindMovementKeys`'s `onStep` ~1458-1464, the arrival-clear in the turn handler ~1218-1226)
- Test: `client/e2e/melee-feedback.spec.ts` (comment updates), run the combat/e2e surface

**Interfaces:**
- Consumes: `submitIntent(identity, target, IntentAttack, targetEntityId)` (the ranged call shape in `attackAt`), `hostileIdAt(target)`, `clickTarget`.
- Produces: melee clicks and key-steps submit `IntentAttack`; a hostile-destination walk's ring clears on adjacency.

- [ ] **Step 1: `meleeAt` submits the attack intent**

In `meleeAt`, replace the submit line:

```ts
    const targetEntityId = hostileIdAt(target) ?? 0;

    return submitIntent(identity, target, IntentAttack, targetEntityId).then(() => undefined);
```

and update its doc comment: mechanically an entity-targeted ATTACK intent now (#116) — one click = one swing, parity with ranged; the server rejects a stale/empty target (`targetEntityId 0` would be a ground shot a melee-only class can't make — guard: if `hostileIdAt` returns null, fall back to `walkTo(target)`; the melee tile routing means a hostile is there in practice, but a bundle race is possible). Show the guard in code:

```ts
  const meleeAt = (target: Hex): Promise<void> => {
    const targetEntityId = hostileIdAt(target);
    if (targetEntityId === null) {
      return walkTo(target); // the hostile left this bundle — treat as a step
    }

    feedbackLayer.flashAttack(target);
    window.game.lastAttackFlash = target;

    const committed: CommittedAction = { kind: "attack", target };
    window.game.committedAction = committed;
    feedbackLayer.setCommitted(committed);

    return submitIntent(identity, target, IntentAttack, targetEntityId).then(() => undefined);
  };
```

- [ ] **Step 2: keyboard steps route through `clickTarget`**

```ts
    onStep: (dir): void => {
      const from = window.game.me?.hex;
      if (from === undefined) {
        return;
      }
      // #116: through clickTarget, not walkTo — stepping into an adjacent
      // hostile is a melee attack (the roguelike idiom survives; only the
      // wire changed), and key-steps get the same in-combat reach filter
      // clicks have.
      void clickTarget(neighbor(from, dir));
    },
```

(`clickTarget` is declared before `bindMovementKeys` — verify ordering compiles; it does, both live in the same setup scope.)

- [ ] **Step 3: destination mirror for trimmed walks**

In the turn handler's arrival-clear (~1218), widen the condition: clear also when the destination hex currently holds a hostile and `mine.hex` is adjacent to it —

```ts
        // Arrived at the destination — or, for a hostile-held destination,
        // arrived ADJACENT to it (#116: the server trims such walks one hex
        // short; without this mirror the ring would linger forever).
        const dest = window.game.destination;
        if (dest !== null) {
          const arrived = mine.hex.q === dest.q && mine.hex.r === dest.r;
          const hostileHeld = window.game.positions.some(
            (p) => p.kind === EntityMonster && p.hex.q === dest.q && p.hex.r === dest.r,
          );
          const adjacent = hexDistance(mine.hex, dest) === 1;
          if (arrived || (hostileHeld && adjacent)) {
            window.game.destination = null;
            feedbackLayer.setDestination(null);
          }
        }
```

(Reuse the client's existing hex-distance helper — grep `const dist`/`hexDistance` in main.ts; if only local copies exist inside evaluate blocks, add a small module-level `hexDistance` next to `neighbor`.)

- [ ] **Step 4: e2e**

- `melee-feedback.spec.ts`: assertions unchanged; update the comment about "replaces the standing melee intent" (intents are one-shot now) and keep the own-hex cancel tap (harmless, still prevents any queued walk residue).
- `combat.spec.ts` chase: unchanged code — `tapHex` on the monster hex now routes an attack intent per poll (the poll re-taps every 300ms, so kills still land); verify, don't rewrite.
- Run: `set -o pipefail && make check 2>&1 | tail -5 && make e2e 2>&1 | tail -5` → 40/40. Then de-race the two combat-adjacent specs: `cd client && npx playwright test e2e/melee-feedback.spec.ts e2e/combat.spec.ts --repeat-each=3 --workers=9` → all green (autowalk excluded: known #117 flake).

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat(client): melee clicks and key-steps submit attack intents (#116)

meleeAt sends IntentAttack + targetEntityId (walkTo fallback on a stale
bundle); keyboard steps route through clickTarget so stepping into an
enemy still attacks; the destination ring clears on adjacency to a
hostile-held destination (mirrors the server's walk trim)."
git push
```

---

### Task 5: Docs, final verification, PR ready

**Files:**
- Modify: `docs/FEATURES.md`, `docs/design.md`, `docs/content-authoring.md`, `docs/design-decisions.md`

- [ ] **Step 1: FEATURES.md**
  - Combat §Melee bullet: "**Melee**: click (or key-step into) an adjacent enemy to swing — an entity-targeted attack intent (#116), one click per swing, and attacking never moves you; monsters still fight by moving into you (the classic roguelike bump-to-attack is now the monsters' rule)."
  - Movement §in-combat bullet: strong red tile = "melee attack (an attack intent)"; the #103 carve-out sentence: replace "in particular the standing melee intent, which keeps attacking turn after turn" with "a deliberate adjacent move".
  - Entity-targeted ranged bullet: note the intent is shared by melee at distance 1.
  - Class routing bullet: rogue "melee-attacks with the dagger when adjacent" stays true — no change needed beyond reading it once.
- [ ] **Step 2: design.md** §5 melee bullet: the player/monster split ("walking into a hostile converts for MONSTERS; a player's melee is an entity-targeted attack intent — decided 2026-07-15, #116; the player keeps the bump *feel* via click/key-step routing"). Keep the definitional gloss.
- [ ] **Step 3: content-authoring.md** §2: "**Melee** is an adjacent attack (click or step into an enemy; #116 — one click, one swing)"; keep the events table untouched.
- [ ] **Step 4: design-decisions.md**: add a dated decided entry — "**Melee is an attack intent** (#116, 2026-07-15): one click = one swing (ranged parity); attacking never moves the player (no after-kill walk); walks stop adjacent to a hostile destination; move-conversion is monster-only. Keyboard steps route through the click path so the roguelike step-into-enemy idiom survives."
- [ ] **Step 5: sweep + full gates**

Run: `grep -rn 'standing melee\|keeps attacking turn after turn\|becomes an attack intent' docs/ client/ internal/ | grep -v superpowers` — no stale assertions of the old model outside historical notes. Then `set -o pipefail && make check 2>&1 | tail -5 && make e2e 2>&1 | tail -5` → green.

- [ ] **Step 6: Commit, push, mark the draft PR ready**

```bash
git add -A && git commit -m "docs: melee attack-intent model shipped — FEATURES + design docs (#116)"
git push
gh pr ready 118
```

Watch CI to completion (`gh pr checks 118 --watch`). Do NOT merge — the `ready to merge` label is the maintainer's.

---

## Self-review notes (spec coverage)

- Spec §Server validation → Task 1a. §Resolution/exclusivity/stack behavior → Task 1b + stack test. §Conversion monster-only + blocked player move → Task 3a + block test. §Walk trim → Task 3b + trim test. §Client (intent kind, keyboard, no window.game changes) → Task 4 (plus the destination-mirror the spec's trim implies). §One-click-one-swing → Task 1 test. §Docs consequences → Task 5. §Determinism → re-derivation protocol + Task 2. §Test migration sketch → Task 2 owns it.
- Draft-PR workflow (maintainer decision): everything lands on PR #118's branch; ready at Task 5, merge stays label-gated.
