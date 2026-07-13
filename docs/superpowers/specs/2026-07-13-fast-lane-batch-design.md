# Fast-lane batch 1 — design

*Six small, independent slices that turn July's design decisions into game
reality: the XP curve, the front-loaded HP curve, cutting `DamagePerLevel`,
the additive percentage fold, the sanctuary-scatter first spawn, and the
first `crit%` weapons. Decisions behind each: `docs/design-roadmap.md`
(XP1, XP2, XP3, Q8, Q9, DF-crit / Q5) and issues #60, #61, #69, #36.*

**One PR, six tasks, one commit each.** Every item is independently testable
and revertable; none depends on another (the crit weapons benefit from the
additive fold landing first, but do not require it).

## Shared constraints (bind every task)

- **Determinism:** all randomness through the existing per-scope seeded
  streams; **sort any map-derived slice before drawing**. Where a change
  shifts rng consumption or a folded value, **re-derive** pinned expected
  values — never weaken an assertion.
- **Drop-table pinning:** new drop entries are **appended last** so existing
  cumulative-weight positions (and `killDropSeed`/`killMissSeed` in
  `drops_test.go`) survive — same convention the healing potion used.
- **FEATURES.md** (constants table §4, mechanics text, gear list) updates in
  the same PR. `docs/design-roadmap.md` Decision cells for XP1/XP2/XP3/DF-crit
  flip to `✅ done` when merged.
- **Protocol constants:** game-rule numbers live in `internal/protocol`;
  after changing them run `make protocol` (tygo) — `client/src/protocol.gen.ts`
  is generated, never hand-edited.
- **Pre-launch, no backward compat** (established rule): existing characters'
  *levels drop* under the new XP curve (e.g. 500 XP: old L6 → new L3). No
  migration; announce in the group when it deploys.

---

## 1. XP curve — cumulative quadratic (XP1, #60)

**Rule:** total XP required to *reach* level L is `XPCurveBase·(L−1)²`,
with `XPCurveBase = 100`.

| level | total XP | gap from previous |
|---|---|---|
| 2 | 100 | 100 |
| 3 | 400 | 300 |
| 4 | 900 | 500 |
| 5 | 1600 | 700 |
| 7 | 3600 | 1100 |
| 10 | 8100 | 1700 |

**Code:**
- `internal/protocol/protocol.go`: `XPPerLevel = 100` → `XPCurveBase = 100`
  (comment: "total XP to reach level L is XPCurveBase·(L−1)²").
- `internal/game/world.go:1915` `levelFor(xp)` → `1 + isqrt(xp/XPCurveBase)`
  where `isqrt` is an integer square root (add a small helper; no float —
  float sqrt mis-rounds near perfect squares).
- **Death floor** (world.go ~188: "on death XP falls to the current level's
  start"): the floor becomes `XPCurveBase·(level−1)²`. Add
  `xpFloorFor(level int) int` next to `levelFor` and use it at the death
  site; today's code computes the floor from the flat constant.
- **Client** (`client/src/main.ts:1110-1113`): `xp % XPPerLevel` breaks under
  a curve. Compute `floor = XPCurveBase·(level−1)²`,
  `next = XPCurveBase·level²`, show `(xp−floor)/(next−floor)`.

**Tests:** table-driven `levelFor` thresholds (0→1, 99→1, 100→2, 399→2,
400→3, 8100→10); `xpFloorFor` inverse property (`levelFor(xpFloorFor(L)) ==
L`); death-floor integration assertion re-derived.

## 2. Front-loaded HP curve (XP2, #60)

**Rule:** the max-HP gain when advancing *from* level n is
`max(HPGainMin, HPGainBase − (n−1))`, with `HPGainBase = 8`, `HPGainMin = 1`
→ gains 8, 7, 6, 5, 4, 3, 2, 1, 1, … (+1 forever).

| level | fighter (base 30) | rogue (base 16) | mage (base 14) |
|---|---|---|---|
| 1 | 30 | 16 | 14 |
| 3 | 45 | 31 | 29 |
| 5 | 56 | 42 | 40 |
| 10 | 67 | 53 | 51 |

(Old flat curve reached fighter 66 at L10 — same destination, chunky start.)

**Code:**
- `internal/protocol/protocol.go`: `HPPerLevel = 4` → `HPGainBase = 8`,
  `HPGainMin = 1`.
- `internal/game/class.go:25` `maxHPFor(class, level)` → base +
  `levelHPBonus(level)`, a closed-form helper: for `level−1 ≤ 8` the
  triangular sum, then `+1` per level beyond (write it as a loop or closed
  form — table-test it either way).
- **Snapshot load:** `maxHP` (and level) are *derived* from stored XP + class,
  so the loader recomputes `maxHP = maxHPFor(class, levelFor(xp))` and clamps
  `hp = min(hp, maxHP)` after decode. **No `snapshotVersion` bump** — the
  shape is unchanged; only derivable values shift. (This also cleanly absorbs
  the XP-curve level drop from task 1.)

**Tests:** `levelHPBonus` table (L1:0, L2:8, L5:26, L9:36, L10:37, L12:39);
snapshot-load recompute (craft a snapshot with stale maxHP/hp, assert both
corrected).

## 3. Cut `DamagePerLevel` (XP3, #60)

**Rule:** a level no longer adds weapon damage. Damage comes from the item
(base + rule cards) — levels give HP (task 2) and, later, skill points.

**Code:** delete `protocol.DamagePerLevel`; `internal/game/items.go:431`
`itemDamage(def, level)` → drop the level parameter entirely (touch the three
call sites world.go:1628/1705/1751); `make protocol`. Monster damage
(`monsterDef.damage`) is already level-free — unaffected.

**Tests:** re-derive any pinned damage numbers that assumed level scaling;
add one regression: a level-5 attacker's sword hit equals a level-1's.

## 4. Additive percentage fold (Q8, #61 principle 14)

**Rule:** within one event's fold, `mulPct` percentages **add**: collect
deltas `(m−100)` across matching cards, apply once —
`v = v·(100+Σdeltas)/100`, clamped to ≥0 before the event-level clamp.
Across events (deal-damage → take-damage) nothing changes — stages still
compose.

**Code:** `internal/game/rules.go` `applyRules` (:173-208): replace the
sequential `for _, m := range muls { v = v*m/100 }` with delta summation.
Comment the why (principle 14; one truncation; order-independent).

**Behavior shift (re-derive, don't weaken):** entities with a *single*
multiplier are byte-identical. Stacked-mult combos change — the known one is
elf crit + Wyrmslayer vs dragon: ×2·×1.5 = ×3 → +100%+50% = ×2.5. Update
`rules_test.go` fold-order/stacking expectations accordingly.

**Tests:** two +10% cards → exactly +20% (not ×1.21); order-independence
(same cards, both orders, equal result); negative delta (−50% + +20% =
−30%); floor at 0 for Σ ≤ −100; single-mult unchanged vs old fold.

## 5. Sanctuary-scatter first spawn (Q9, #36)

**Rule:** a *new* character's spawn search anchors at a seeded-random
walkable, origin-connected hex within `SanctuaryRadius` (5) of the origin —
instead of always anchoring at the origin. The existing guard tiers
(not monster-occupied, not within `CombatRadius` of a living monster,
stack-cap, spiral fallback) run unchanged from that anchor. Respawn-on-death
and token-rejoin keep their current anchors (bed spawns are a future slice).

**Code:** `internal/game/world.go` `spawnHexLocked` callers for **Join**
only: pick the anchor via the existing world-gen/spawn seeded stream
(`spawnStream`, world.go ~2118) — collect candidate hexes (walkable,
origin-connected, within `SanctuaryRadius`), **sort** them (map-derived!),
draw one, pass as the spiral anchor. No protocol change.

**Tests:** same seed → same anchor sequence (determinism); anchor always
within the sanctuary and walkable; guard tiers still veto monster-adjacent
anchors (place a monster inside the sanctuary in the test world);
two joins don't stack beyond `StackCap`.

## 6. First `crit%` weapons (Q5/DF-crit, #69)

Two new **drop-only** items (existing weapons untouched — no live-balance
shift, no pinned-test churn). Cards use the shipped elf-crit pattern:
`WHEN deal-damage IF chance N THEN mulPct 200`.

| | Misericorde | Duelist's Saber |
|---|---|---|
| type / class | melee-weapon, rogue | melee-weapon, fighter |
| damage | 6 | 5 |
| rule | 15% chance → ×2 | 10% chance → ×2 |
| expected damage | 6.9 (dagger: flat 7) | 5.5 (warhammer: flat 6) |
| desc | "15% chance to strike true for double damage" | "10% chance to land a perfect riposte for double" |
| flavor | "A blade thin enough to find the gap between any two plates." | "Its balance rewards patience; its edge rewards timing." |
| intent | the rogue's crit identity as loot — spike drama, not an upgrade | trade the warhammer's flat crown for moments |

**Placement:** append `{misericorde, weight 4}` to the **ghoul** table and
`{saber, weight 4}` to the **wolf** table — **appended last** (see shared
constraints). `mustValidateContent` covers them at init.

**Tests:** content-validation passes; a seeded combat test pinning one crit
proc and one non-proc for each item (re-derive seeds); expected-damage
sanity is a design note, not an assertion.

---

## Out of scope

Beds (Q9 second half), the `evasion-check` event (DF2 — needs the new
pre-damage event), skill trees/points (SK*/XP4), continuous monster
spawning, any change to existing weapons or species numbers.

## Success criteria

`make check` green; all re-derived seeds documented in their test comments;
FEATURES.md tables match `internal/protocol` exactly; a fresh world plays:
fast early levels, chunky early HP, flat damage, scattered sanctuary spawns,
and a lucky Misericorde drop that crits.
