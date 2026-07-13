# Fast-Lane Batch 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land the six decided fast-lane slices — quadratic XP curve, front-loaded HP curve, cut `DamagePerLevel`, additive percentage fold, sanctuary-scatter spawn, first `crit%` weapons.

**Architecture:** Six independent changes to the existing Go game core (`internal/protocol` constants, `internal/game` formulas/content) plus one client XP-bar formula. No new packages, no wire-shape changes, no snapshot-version bump (level/maxHP are derived from stored XP and recomputed on load).

**Tech Stack:** Go (module root), tygo-generated TS protocol (`make protocol`), Vitest/Playwright untouched, `make check` as the gate.

**Spec:** `docs/superpowers/specs/2026-07-13-fast-lane-batch-design.md` — read it first; its tables are the acceptance values.

## Global Constraints

- **Determinism:** all randomness through per-scope seeded PCG; **sort any map-derived slice before drawing**. When a change shifts a folded value or rng consumption, **re-derive** pinned expected values in tests — never weaken an assertion. Document re-derived seeds in test comments.
- **Drop-table pinning:** new drop entries are **appended last** to a monster's `drops` slice so existing cumulative-weight positions survive (`drops_test.go` pins `killDropSeed`/`killMissSeed`).
- **Protocol:** game-rule constants live in `internal/protocol/protocol.go`; after every constant change run `make protocol` and commit the regenerated `client/src/protocol.gen.ts` in the same commit. Never hand-edit the generated file.
- **FEATURES.md** (`docs/FEATURES.md`): §4 constants table and mechanics text must match `internal/protocol` exactly — updated in Task 7, same PR.
- **Pre-launch, no backward compat:** existing characters' levels drop under the new curve; no migration code.
- Go may not be on PATH; the Makefile falls back to `/usr/local/go/bin/go`. Run gates via `make test` / `make check`, not bare `go test`.
- Branch: `feat/fast-lane-batch` (exists, carries the spec). Everything lands via one PR.

## File Map

| File | Changes |
|---|---|
| `internal/protocol/protocol.go` | `XPPerLevel`→`XPCurveBase` (T1); `HPPerLevel`→`HPGainBase`+`HPGainMin` (T2); delete `DamagePerLevel` (T3) |
| `internal/game/world.go` | `levelFor`, `levelFloorXP`, `isqrt` (T1); `itemDamage` call sites (T3); `spawnHexLocked` radius (T5) |
| `internal/game/class.go` | `maxHPFor` + `levelHPBonus` (T2) |
| `internal/game/snapshot.go` | recompute maxHP on restore (T2) |
| `internal/game/items.go` | `itemDamage` signature (T3); two id constants (T6) |
| `internal/game/rules.go` | additive fold in `applyRules` (T4) |
| `internal/game/content.go` | two new items + drop entries (T6) |
| `client/src/main.ts` | XP-bar threshold math (T1) |
| `docs/FEATURES.md`, `docs/design-roadmap.md`, `docs/STATUS.md` | sync (T7) |

---

### Task 1: Quadratic XP curve

**Files:**
- Modify: `internal/protocol/protocol.go:203-204`
- Modify: `internal/game/world.go:1914-1915` (`levelFor`), `:1929-1930` (`levelFloorXP`)
- Modify: `client/src/main.ts:69,1110-1113`
- Test: `internal/game/world_test.go` (or the file holding existing `levelFor` tests — find with `grep -rn "levelFor" internal/game/*_test.go`)

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces: `levelFor(xp int) int`, `xpFloorFor(level int) int`, `isqrt(n int) int`, `protocol.XPCurveBase = 100`. Tasks 2/3 call `levelFor` unchanged in signature.

- [ ] **Step 1: Write the failing tests**

Add to the game package test file that already covers leveling (create `internal/game/level_test.go` if none is dedicated):

```go
func TestLevelForQuadraticCurve(t *testing.T) {
	t.Parallel()

	cases := []struct {
		xp   int
		want int
	}{
		{0, 1}, {99, 1}, {100, 2}, {250, 2}, {399, 2},
		{400, 3}, {899, 3}, {900, 4}, {1600, 5}, {3600, 7}, {8100, 10},
	}
	for _, c := range cases {
		if got, want := levelFor(c.xp), c.want; got != want {
			t.Errorf("levelFor(%d) = %d, want %d", c.xp, got, want)
		}
	}
}

func TestXPFloorForInvertsLevelFor(t *testing.T) {
	t.Parallel()

	for level := 1; level <= 20; level++ {
		floor := xpFloorFor(level)

		if got, want := levelFor(floor), level; got != want {
			t.Errorf("levelFor(xpFloorFor(%d)) = %d, want %d", level, got, want)
		}

		if level > 1 {
			if got, want := levelFor(floor-1), level-1; got != want {
				t.Errorf("levelFor(xpFloorFor(%d)-1) = %d, want %d", level, got, want)
			}
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `make test`
Expected: FAIL — `undefined: xpFloorFor`, and `TestLevelForQuadraticCurve` failing on `{400, 3}` etc. (old flat curve gives 5).

- [ ] **Step 3: Change the protocol constant**

In `internal/protocol/protocol.go` replace:

```go
	// XPPerLevel is the XP needed to advance one level.
	XPPerLevel = 100
```

with:

```go
	// XPCurveBase scales the quadratic XP curve: the total XP required to
	// REACH level L is XPCurveBase * (L-1)^2 (#60, roadmap XP1). Gaps grow
	// linearly: 100, 300, 500, ...
	XPCurveBase = 100
```

- [ ] **Step 4: Reimplement levelFor / levelFloorXP**

In `internal/game/world.go` replace the two functions:

```go
// levelFor returns the 1-based level for a cumulative XP total: the largest
// L with XPCurveBase*(L-1)^2 <= xp. Integer math only — float sqrt
// mis-rounds near perfect squares.
func levelFor(xp int) int { return 1 + isqrt(xp/protocol.XPCurveBase) }

// xpFloorFor returns the cumulative XP at which the given level starts.
func xpFloorFor(level int) int {
	return protocol.XPCurveBase * (level - 1) * (level - 1)
}

// levelFloorXP returns the XP at the start of xp's current level (the
// death floor: dying costs progress inside the level, never the level).
func levelFloorXP(xp int) int { return xpFloorFor(levelFor(xp)) }

// isqrt returns the integer square root: the largest s with s*s <= n.
func isqrt(n int) int {
	if n <= 0 {
		return 0
	}

	s := int(math.Sqrt(float64(n)))
	for s > 0 && s*s > n {
		s--
	}

	for (s+1)*(s+1) <= n {
		s++
	}

	return s
}
```

Add `"math"` to world.go's imports if absent. Grep for any other `protocol.XPPerLevel` use: `grep -rn "XPPerLevel" internal/ client/src --include="*.go" --include="*.ts"` — every Go hit must be converted to curve math (not just renamed); expected hits are `levelFor`/`levelFloorXP` (now done) and possibly tests.

- [ ] **Step 5: Regenerate the protocol and fix the client XP bar**

Run: `make protocol`

In `client/src/main.ts`: line 69 import `XPPerLevel` → `XPCurveBase`; replace lines 1110-1113:

```ts
        const xpFloor = XPCurveBase * (mine.level - 1) * (mine.level - 1);
        const xpNext = XPCurveBase * mine.level * mine.level;
        // Position readout (item 9, playtest batch 2): live per bundle, so
        // it never drifts from the server-authoritative hex even mid-tween.
        statsEl.textContent = `Lv ${mine.level} · ${mine.xp - xpFloor}/${xpNext - xpFloor} XP · (${mine.hex.q}, ${mine.hex.r})`;
```

- [ ] **Step 6: Re-derive broken pins, run the full gate**

Run: `make test` then `make check`. Existing tests that pinned flat-curve levels (6b.1 XP tests, quest payout levels, death-floor assertions) now fail with *different level numbers* — re-derive each expected value from the new table (100/400/900/…), never widen an assertion. Comment each re-derived value with `// re-derived for XPCurveBase quadratic curve (fast-lane T1)`.
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/protocol/protocol.go internal/game client/src
git commit -m "feat(xp): quadratic XP curve — total to reach L is 100*(L-1)^2 (#60, XP1)"
```

### Task 2: Front-loaded HP curve + restore-time recompute

**Files:**
- Modify: `internal/protocol/protocol.go:229-230`
- Modify: `internal/game/class.go:22-27`
- Modify: `internal/game/snapshot.go` (`RestoreState`, after entities are restored)
- Test: `internal/game/class_test.go`, `internal/game/snapshot_test.go`

**Interfaces:**
- Consumes: `levelFor` (Task 1; signature unchanged, works standalone too).
- Produces: `levelHPBonus(level int) int`, `maxHPFor(class string, level int) int` (signature unchanged), `protocol.HPGainBase = 8`, `protocol.HPGainMin = 1`. Uses the existing `syncMaxHPLocked(e *entity)` (world.go:1922) at restore.

- [ ] **Step 1: Write the failing tests**

```go
func TestLevelHPBonusFrontLoaded(t *testing.T) {
	t.Parallel()

	cases := []struct {
		level int
		want  int
	}{
		{1, 0}, {2, 8}, {3, 15}, {5, 26}, {9, 36}, {10, 37}, {12, 39},
	}
	for _, c := range cases {
		if got, want := levelHPBonus(c.level), c.want; got != want {
			t.Errorf("levelHPBonus(%d) = %d, want %d", c.level, got, want)
		}
	}
}

func TestMaxHPForUsesCurve(t *testing.T) {
	t.Parallel()

	// Fighter base 30: spec table pins L5 = 56, L10 = 67.
	if got, want := maxHPFor(protocol.ClassFighter, 5), 56; got != want {
		t.Errorf("maxHPFor(fighter, 5) = %d, want %d", got, want)
	}

	if got, want := maxHPFor(protocol.ClassFighter, 10), 67; got != want {
		t.Errorf("maxHPFor(fighter, 10) = %d, want %d", got, want)
	}
}
```

Snapshot recompute test (in `snapshot_test.go`, following its existing craft-a-snapshot style — see `TestInventoryLoopOverHTTP`'s crafted-snapshot pattern in `test/integration/inventory_test.go` and the unit tests already in `snapshot_test.go`):

```go
func TestRestoreRecomputesDerivedHP(t *testing.T) {
	t.Parallel()

	// A stored entity whose maxHP/hp were written under the OLD curves must
	// come back recalibrated: maxHP = maxHPFor(class, levelFor(xp)), hp
	// clamped. Craft: fighter, xp 400 (level 3 under the new curve),
	// stored maxHP 66 / hp 66 (stale flat-curve values).
	// Expected after restore: maxHP 45 (30 + 8 + 7), hp 45.
	w := newTestWorldForSnapshot(t) // reuse the file's existing helper; if
	// none exists, marshal a live world, edit the DTO's HP/MaxHP, restore.
	// ... craft, restore ...
	// if got, want := e.maxHP, 45; got != want { t.Errorf(...) }
	// if got, want := e.hp, 45; got != want { t.Errorf(...) }
}
```

(The implementer writes the craft using the file's real helpers — the assertion values above are the binding part: fighter, xp 400 → maxHP 45, hp clamped to 45.)

- [ ] **Step 2: Run to verify failure**

Run: `make test`
Expected: FAIL — `undefined: levelHPBonus`; maxHPFor table mismatches.

- [ ] **Step 3: Constants + curve**

In `internal/protocol/protocol.go` replace:

```go
	// HPPerLevel is the additional max HP gained per level above 1.
	HPPerLevel = 4
```

with:

```go
	// HPGainBase/HPGainMin shape the front-loaded HP curve (#60, roadmap
	// XP2): the max-HP gain when advancing FROM level n is
	// max(HPGainMin, HPGainBase-(n-1)) — 8,7,6,...,1 then +1 forever.
	HPGainBase = 8
	HPGainMin  = 1
```

In `internal/game/class.go` replace `maxHPFor`:

```go
// maxHPFor is the single source of truth for a class's max HP at a given
// level: the class base plus the front-loaded curve bonus (levelHPBonus).
// Used for spawn/respawn HP, level-up scaling, and the wire.
func maxHPFor(class string, level int) int {
	return baseMaxHP(class) + levelHPBonus(level)
}

// levelHPBonus is the cumulative max HP gained above level 1 under the
// front-loaded curve: the gain when advancing from level n is
// max(HPGainMin, HPGainBase-(n-1)). Loop, not closed form — levels are
// small and the loop reads as the rule.
func levelHPBonus(level int) int {
	bonus := 0

	for n := 1; n < level; n++ {
		gain := protocol.HPGainBase - (n - 1)
		if gain < protocol.HPGainMin {
			gain = protocol.HPGainMin
		}

		bonus += gain
	}

	return bonus
}
```

Run `make protocol` (constants changed). Grep `HPPerLevel` across Go+TS; convert any remaining use.

- [ ] **Step 4: Restore-time recompute**

In `internal/game/snapshot.go`, in `RestoreState` after the entity maps are rebuilt (after the `entitiesFromDTO` call), add:

```go
	// Level and maxHP are DERIVED from stored XP + class; recompute on load
	// so curve changes (XP1/XP2) apply to existing characters without a
	// snapshot-version bump — shape is unchanged, only derivable values.
	for _, e := range w.entities {
		if e.class != "" { // players only; a monster's maxHP is its kind's
			syncMaxHPLocked(e)
		}
	}
```

(`syncMaxHPLocked` already exists at world.go:1922 and clamps hp — do not reimplement it.)

- [ ] **Step 5: Run gate, re-derive**

Run: `make test` — re-derive any pinned maxHP values in world/class/integration tests from the spec's table (fighter 30/38/45/51/56/…; rogue base 16; mage base 14), comment `// re-derived for front-loaded HP curve (fast-lane T2)`. Then `make check`.
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/protocol/protocol.go internal/game client/src
git commit -m "feat(hp): front-loaded HP curve, gains 8..1 then +1; recompute derived HP on snapshot restore (#60, XP2)"
```

### Task 3: Cut DamagePerLevel

**Files:**
- Modify: `internal/protocol/protocol.go:231-232` (delete)
- Modify: `internal/game/items.go:429-433`
- Modify: `internal/game/world.go:1628,1705,1751` (call sites)
- Test: `internal/game/items_test.go` (or wherever `itemDamage` is covered)

**Interfaces:**
- Consumes: nothing.
- Produces: `itemDamage(def *itemDef) int` — **signature change**, level parameter removed. Task 6's crit test uses this.

- [ ] **Step 1: Write the failing test**

```go
func TestItemDamageIsLevelFree(t *testing.T) {
	t.Parallel()

	def := mustItemDef(t, idIronSword) // use the file's existing def-lookup helper; else itemDefByID
	if got, want := itemDamage(def), def.damage; got != want {
		t.Errorf("itemDamage = %d, want base %d", got, want)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `make test`
Expected: FAIL — compile error (itemDamage takes two args) — that IS the failure signal here.

- [ ] **Step 3: Implement**

Delete from `internal/protocol/protocol.go`:

```go
	// DamagePerLevel is the additional damage gained per level above 1.
	DamagePerLevel = 1
```

Also update the stale comment at protocol.go:213-215 that references DamagePerLevel/level scaling. In `internal/game/items.go`:

```go
// itemDamage is the single source of truth for an item's damage: the def's
// base — levels do not scale damage (#60, roadmap XP3: no raw-stat scaling;
// levels give HP and, later, skill points). Used by both the melee and
// ranged combat paths.
func itemDamage(def *itemDef) int {
	return def.damage
}
```

Update the three call sites in world.go (1628, 1705, 1751): `itemDamage(weapon, levelFor(a.attacker.xp))` → `itemDamage(weapon)` etc. If a call site's `levelFor` fetch becomes unused, delete it. Run `make protocol`.

- [ ] **Step 4: Add the regression + run gate**

```go
func TestAttackDamageDoesNotScaleWithLevel(t *testing.T) {
	t.Parallel()
	// Two identical fighters, one at level 1 (xp 0) and one at level 5
	// (xp 1600), bump-attack identical victims with identical seeds:
	// the damage dealt must be equal. Reuse the package's existing
	// combat-test scaffolding (see the 6b tests around rollDamageLocked).
}
```

Write it against the file's real combat scaffolding; the binding assertion is *equal damage across attacker levels*. Run `make test`; re-derive pinned combat damage numbers that included the level bonus (comment `// re-derived: DamagePerLevel cut (fast-lane T3)`). `make check`.
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/protocol/protocol.go internal/game client/src
git commit -m "feat(combat): cut DamagePerLevel — item damage is base + cards only (#60, XP3)"
```

### Task 4: Additive percentage fold

**Files:**
- Modify: `internal/game/rules.go:192-195` (the multiplier loop in `applyRules`)
- Test: `internal/game/rules_test.go`

**Interfaces:**
- Consumes: nothing. Produces: no signature change — behavior only.

- [ ] **Step 1: Write the failing tests**

```go
func TestApplyRulesMulPctAddsNotCompounds(t *testing.T) {
	t.Parallel()

	cards := []ruleCard{
		{event: evDealDamage, then: effect{kind: effMulPct, n: 110}},
		{event: evDealDamage, then: effect{kind: effMulPct, n: 110}},
	}

	// Two +10% cards on base 100: additive = 120, compounding would be 121.
	if got, want := applyRules(evDealDamage, 100, cards, ruleCtx{}), 120; got != want {
		t.Errorf("two +10%% cards on 100 = %d, want %d (add, not compound)", got, want)
	}
}

func TestApplyRulesMulPctOrderIndependent(t *testing.T) {
	t.Parallel()

	a := []ruleCard{
		{event: evDealDamage, then: effect{kind: effMulPct, n: 150}},
		{event: evDealDamage, then: effect{kind: effMulPct, n: 200}},
	}
	b := []ruleCard{a[1], a[0]}

	if got, want := applyRules(evDealDamage, 3, a, ruleCtx{}), applyRules(evDealDamage, 3, b, ruleCtx{}); got != want {
		t.Errorf("fold is order-dependent: %d vs %d", got, want)
	}

	// +50% and +100% on base 3: 3 * 250 / 100 = 7 (single truncation).
	if got, want := applyRules(evDealDamage, 3, a, ruleCtx{}), 7; got != want {
		t.Errorf("stacked mults on 3 = %d, want %d", got, want)
	}
}

func TestApplyRulesMulPctNegativeDeltaAndFloor(t *testing.T) {
	t.Parallel()

	// -50% and +20% = -30%: base 10 -> 7.
	mixed := []ruleCard{
		{event: evDealDamage, then: effect{kind: effMulPct, n: 50}},
		{event: evDealDamage, then: effect{kind: effMulPct, n: 120}},
	}
	if got, want := applyRules(evDealDamage, 10, mixed, ruleCtx{}), 7; got != want {
		t.Errorf("mixed deltas on 10 = %d, want %d", got, want)
	}

	// Sum of deltas <= -100% floors at 0 (deal-damage has no 1-floor).
	kill := []ruleCard{
		{event: evDealDamage, then: effect{kind: effMulPct, n: 0}},
	}
	if got, want := applyRules(evDealDamage, 10, kill, ruleCtx{}), 0; got != want {
		t.Errorf("-100%% on 10 = %d, want %d", got, want)
	}
}
```

(Match the file's existing literal style for `ruleCard`/`ruleCtx` — see `TestApplyRulesFoldOrder` at rules_test.go:20.)

- [ ] **Step 2: Run to verify failure**

Run: `make test`
Expected: `TestApplyRulesMulPctAddsNotCompounds` FAILS with 121 (compounding); the order test may pass by luck — that's fine.

- [ ] **Step 3: Implement the additive fold**

In `internal/game/rules.go` replace the multiplier application in `applyRules`:

```go
	v := base + add

	// Percentages ADD within one event's fold (#61 principle 14, roadmap
	// Q8): sum the deltas and apply once — a single integer truncation,
	// trivially order-independent. Stages still compose across events
	// (deal-damage -> take-damage -> future crit-check), so cross-stage
	// effects remain true multipliers.
	if len(muls) > 0 {
		delta := 0
		for _, m := range muls {
			delta += m - percentBase
		}

		v = v * (percentBase + delta) / percentBase
		if v < 0 {
			v = 0
		}
	}
```

- [ ] **Step 4: Re-derive the stacked-mult pins, run gate**

`TestApplyRulesFoldOrder` (rules_test.go:20) pinned compounding — re-derive its expectation under additive (comment `// re-derived: additive fold (fast-lane T4, #61 p14)`). Any combat/integration pin that stacked two multipliers (elf crit + Wyrmslayer: ×3 → ×2.5) re-derives likewise. Single-multiplier pins must NOT change — if one does, the implementation is wrong. Run `make test` then `make check`.
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/game/rules.go internal/game/rules_test.go
git commit -m "feat(rules): percentages add within an event fold, single truncation (#61 p14, Q8)"
```

### Task 5: Sanctuary-scatter spawn

**Files:**
- Modify: `internal/game/world.go:2474-2541` (`spawnHexLocked` + its doc comment)
- Test: `internal/game/spawn_test.go` (or wherever spawnHexLocked is covered — `grep -rn "spawnHexLocked" internal/game/*_test.go`)

**Interfaces:**
- Consumes: nothing. Produces: no signature change — the candidate area widens from `clearingRadius` (2) to `protocol.SanctuaryRadius` (5).

**Spec note (deviation, flagged for review):** the spec says respawn/rejoin "keep their current anchors". Their current anchor *is* `spawnHexLocked`; this task widens its radius, so respawns also scatter across the sanctuary. That matches Q9's model (sanctuary is the interim "home" until beds land) and avoids a join-vs-respawn code split nothing needs yet. If the reviewer wants join-only, split at the call sites — but prefer this.

- [ ] **Step 1: Write the failing test**

```go
func TestSpawnScattersAcrossSanctuary(t *testing.T) {
	t.Parallel()

	// A default-gen world with no monsters near the origin: spawn hexes must
	// (a) stay within SanctuaryRadius of the origin, and (b) actually use
	// the widened area — with the seeded stream, some spawn among the first
	// dozen joins must land at distance > clearingRadius (2). Determinism:
	// the same seed yields the same spawn sequence.
	w := newWorld(t) // the file's existing world-fixture helper
	origin := protocol.Hex{Q: 0, R: 0}
	sawBeyondClearing := false

	var first []protocol.Hex

	for i := 0; i < 12; i++ {
		h, err := w.spawnHexLockedForTest() // or lock + call, matching file style
		if err != nil {
			t.Fatalf("spawn %d: %v", i, err)
		}

		if got, want := HexDistance(origin, h), protocol.SanctuaryRadius; got > want {
			t.Errorf("spawn %d at distance %d, want <= %d", i, got, want)
		}

		if HexDistance(origin, h) > 2 {
			sawBeyondClearing = true
		}

		first = append(first, h)
	}

	if !sawBeyondClearing {
		t.Error("no spawn landed beyond the old clearingRadius — scatter not widened")
	}

	_ = first // determinism: rebuild the same-seed world, repeat, assert equal sequence
}
```

(Adapt fixture/locking to the file's real helpers; the three binding assertions are: within `SanctuaryRadius`, some beyond distance 2, same-seed reproducibility.)

- [ ] **Step 2: Run to verify failure**

Run: `make test`
Expected: FAIL on "no spawn landed beyond the old clearingRadius".

- [ ] **Step 3: Widen the candidate area**

In `internal/game/world.go` `spawnHexLocked` (line 2504):

```go
		if HexDistance(origin, h) > protocol.SanctuaryRadius || w.occupancyLocked(h) >= protocol.StackCap {
```

Rename the three candidate slices `clearingSafe/clearingUnoccupied/clearingAny` → `sanctuarySafe/sanctuaryUnoccupied/sanctuaryAny`, and update the function's doc comment (world.go:2474-2497) to say the search area is the sanctuary (`protocol.SanctuaryRadius`), seeded scatter per Q9 — first spawn scatters across the sanctuary; beds are the future per-player anchor. The guard tiers, sort, seeded pick, and spiral fallback are already exactly what Q9 needs — do not touch them.

- [ ] **Step 4: Run gate, re-derive**

Run: `make test` — any test pinning exact spawn hexes re-derives (comment `// re-derived: sanctuary scatter (fast-lane T5, Q9)`). e2e joins players — positions aren't pinned there, but run `make e2e` locally once to confirm. Then `make check`.
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/game
git commit -m "feat(spawn): scatter joins across the sanctuary, not just the origin clearing (Q9, #36)"
```

### Task 6: Crit weapons — Misericorde & Duelist's Saber

**Files:**
- Modify: `internal/game/items.go` (~:452 id block — two new constants)
- Modify: `internal/game/content.go` (two `itemDefs` entries; wolf + ghoul `drops`)
- Test: `internal/game/content_test.go` + a seeded crit test in the combat test file

**Interfaces:**
- Consumes: `itemDamage(def)` (Task 3 signature). Produces: content only — ids `idMisericorde = "misericorde"`, `idDuelistsSaber = "duelists-saber"`.

- [ ] **Step 1: Write the failing test**

```go
func TestCritWeaponsRegistered(t *testing.T) {
	t.Parallel()

	cases := []struct {
		id     string
		damage int
		chance int
	}{
		{idMisericorde, 6, 15},
		{idDuelistsSaber, 5, 10},
	}
	for _, c := range cases {
		def := itemDefByID(c.id) // the registry-lookup helper content_test.go already uses
		if def == nil {
			t.Fatalf("%s not registered", c.id)
		}

		if got, want := def.damage, c.damage; got != want {
			t.Errorf("%s damage = %d, want %d", c.id, got, want)
		}

		if got, want := len(def.rules), 1; got != want {
			t.Fatalf("%s rules = %d, want %d", c.id, got, want)
		}

		card := def.rules[0]
		if got, want := card.when[0].n, c.chance; got != want {
			t.Errorf("%s crit chance = %d, want %d", c.id, got, want)
		}

		if got, want := card.then.n, 200; got != want {
			t.Errorf("%s crit multiplier = %d, want %d (x2)", c.id, got, want)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `make test`
Expected: FAIL — `undefined: idMisericorde`.

- [ ] **Step 3: Add ids + content**

In `internal/game/items.go`, in the drop-item id const block (~:452):

```go
	idMisericorde   = "misericorde"
	idDuelistsSaber = "duelists-saber"
```

In `internal/game/content.go`, append to `itemDefs` after the existing weapon drops (card shape copied from `elfCards`, content.go:14-17):

```go
	{
		id: idMisericorde, name: "Misericorde", itemType: protocol.ItemTypeMeleeWeapon,
		wearableBy: []string{protocol.ClassRogue},
		damage:     6, desc: "15% chance to strike true for double damage",
		flavor:     "A blade thin enough to find the gap between any two plates.",
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condChance, n: 15}},
				then: effect{kind: effMulPct, n: 200}},
		},
	},
	{
		id: idDuelistsSaber, name: "Duelist's Saber", itemType: protocol.ItemTypeMeleeWeapon,
		wearableBy: []string{protocol.ClassFighter},
		damage:     5, desc: "10% chance to land a perfect riposte for double",
		flavor:     "Its balance rewards patience; its edge rewards timing.",
		rules: []ruleCard{
			{event: evDealDamage, when: []condition{{kind: condChance, n: 10}},
				then: effect{kind: effMulPct, n: 200}},
		},
	},
```

Drop tables — **append LAST** (Global Constraints; the healing-potion comment in the wolf table explains why):

- wolf `drops` (content.go ~:227): append `{defID: idDuelistsSaber, weight: 4},` after the healing-potion line.
- ghoul `drops` (content.go ~:247): append `{defID: idMisericorde, weight: 4},` at the end.

- [ ] **Step 4: Seeded crit proc test**

In the combat test file that already drives seeded attacks (grep `condChance` uses in tests — the elf-crit test is the template):

```go
func TestMisericordeCritProcsSeeded(t *testing.T) {
	// Equip a (non-elf) rogue with the Misericorde against a fixed victim.
	// Find, by scanning turn seeds, one seed where the 15% chance procs
	// (damage 12 before take-damage cards) and one where it doesn't
	// (damage 6) — pin BOTH, with the scan documented in a comment, the
	// same way the elf-crit seeded test pins its seeds.
}
```

Binding assertions: proc deals exactly `12`, non-proc exactly `6` (base, no other cards in play; species human would add XP cards only — use human, or dwarf-victim-free setup per the template test).

- [ ] **Step 5: Run gate**

Run: `make test` — `mustValidateContent` runs at init, so a malformed card fails every test loudly. Drop-table pins (`drops_test.go`) must NOT change (entries appended last); if they break, the append position is wrong — fix placement, don't re-derive. Then `make check`.
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/game
git commit -m "feat(content): Misericorde (rogue, 15% x2) and Duelist's Saber (fighter, 10% x2) — first crit% weapons (#69, Q5)"
```

### Task 7: Docs sync + PR

**Files:**
- Modify: `docs/FEATURES.md` (§4 constants table; XP/level/HP mechanics text; gear list gains both weapons; "decided but not yet built" prunes what just shipped)
- Modify: `docs/design-roadmap.md` (Decision cells XP1/XP2/XP3 → `✅ done`, Q8/Q9 rows note "shipped", DF-crit note in §8)
- Modify: `docs/STATUS.md` (session entry: what landed, the re-derived-seed list, the level-drop deploy note)

**Interfaces:** consumes the final constant names/values from Tasks 1-3 and the item stats from Task 6 — copy from `internal/protocol`/`content.go`, never from memory.

- [ ] **Step 1: Update the three docs**

FEATURES §4: `XPPerLevel 100` row → `XPCurveBase 100 (total XP to reach L = 100·(L−1)²)`; `HPPerLevel 4` → `HPGainBase 8 / HPGainMin 1 (gain from level n = max(1, 8−(n−1)))`; delete the `DamagePerLevel` row. Mechanics text: XP/level section gets the curve + death floor; combat section notes damage is level-free and percentages add per fold; movement/spawn section notes sanctuary scatter; gear tables add both crit weapons (stats verbatim from content.go). STATUS: prepend a dated session block listing the six landed items + every re-derived seed/test. Roadmap: flip the Decision cells.

- [ ] **Step 2: Full gate**

Run: `make check`
Expected: PASS (lint, protocol drift, typecheck, tests, build).

- [ ] **Step 3: Commit + PR**

```bash
git add docs
git commit -m "docs: FEATURES/roadmap/STATUS sync for fast-lane batch 1"
git push -u origin feat/fast-lane-batch
gh pr create --base main --title "feat: fast-lane batch 1 — XP/HP curves, level-free damage, additive fold, sanctuary scatter, crit weapons" --body-file <(echo "Implements docs/superpowers/specs/2026-07-13-fast-lane-batch-design.md — six independent slices, one commit each. Spec + plan in-branch. Roadmap: XP1 XP2 XP3 Q8 Q9 DF-crit.")
```

Do **not** merge — the `ready to merge` label is the maintainer's signal.

---

## Self-review notes

- Spec coverage: §1→T1, §2→T2, §3→T3, §4→T4, §5→T5 (with one flagged deviation: respawn also scatters — see T5's spec note), §6→T6, shared constraints→Global Constraints + T7. Client XP bar → T1 step 5. No gaps.
- Type consistency: `levelFor(int) int` unchanged (T1, used T2); `itemDamage(*itemDef) int` new signature defined T3, consumed T6; `syncMaxHPLocked` reused not redefined (T2); ids `idMisericorde`/`idDuelistsSaber` defined T6 and used only there.
- Two tests (T2 snapshot craft, T3 level-regression, T6 seeded crit) bind their *assertion values* but delegate scaffolding to the file's existing helpers — deliberate: the helpers exist and inventing their signatures here would be wrong; the binding values are complete.
