# Milestone 6c — Monster Kinds & Difficulty Rings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Monster variety as content data with per-kind stats/XP/loot, placed in distance-based difficulty rings; ships the `targetKind` condition and the Wyrmslayer Greatsword.

**Architecture:** A `monsterDef` registry in content.go mirroring the item registry (validated at init); combat reads per-kind numbers through the def instead of flat constants; loot authority moves from item `dropWeight` to monster-side tables; worldgen ring bands drive spawn placement. Wire adds `Entity.MonsterKind`.

**Tech Stack:** Go (module root), tygo protocol regen, SolidJS/Pixi client, Playwright e2e.

**Spec:** `docs/superpowers/specs/2026-07-10-m6c-monster-kinds-rings-design.md` — the kind table, ring math, and loot-model rationale live there; read it first. **Prerequisite: the playtest-ready batch PR is merged** (this plan consumes its aggro machinery and spawn guards).

## Global Constraints

- `make check` green at every commit; `make protocol` after ANY protocol change; generated TS never hand-edited.
- Go style `.claude/rules/go-style.md`; content validated at init, fail-loud (`mustValidateContent` idiom).
- Determinism: rng only via passed `*mrand.Rand`; sorted iteration before rng; seeded-test expectations re-derived, never weakened. `wolf` inherits today's exact monster numbers (10 HP / 3 dmg / 20 XP / 30% drop / starter-set table) so most seeded expectations survive.
- The decided loot model: tables live on MONSTERS (`monsterDef.drops`), authored item-side cards get transcribed there; item `dropWeight` is deleted.
- Commit per task, conventional messages.

---

### Task 1: Monster registry — defs, validation, wolf as the current monster

**Files:** Create `internal/game/monsters.go` (types + helpers + validation), extend `internal/game/content.go` (the 5-kind launch table from the spec), extend `mustValidateContent`. Test: `internal/game/monsters_test.go` (whitebox, like items_test.go).

**Interfaces (produces):** `type monsterDef struct{ id, name, glyph string; maxHP, damage, xp, aggroRadius, dropChance int; drops []drop; rings []int; rules []ruleCard }`; `type drop struct{ defID string; weight int }`; `monsterDefs []*monsterDef`, `monsterDefByID map[string]*monsterDef`; entity field `monsterKind string` (set at spawn; empty for players); `func kindOf(e *entity) *monsterDef` (nil for players).

- [ ] Failing tests: registry pins (5 kinds, wolf = 10/3/20/30% + the exact current starter-drop table), validation panics (dup id, drop referencing unknown item, ring with no kind, aggroRadius between 1 and CombatRadius-1, kind rules with unknown event), `kindOf` player→nil.
- [ ] Implement; wire `SpawnMonsters`/`SpawnMonsterAt`/`PlaceMonsterForTest` to take/default a kind (`wolf`) and set `monsterKind` + `maxHP` from the def. Nothing else reads the registry yet — behavior identical, all existing tests must pass unchanged.
- [ ] `make check`; commit `feat(game): monster-kind registry; wolf carries the current numbers`.

### Task 2: Combat reads the kind — damage, XP, loot, announces, targetKind

**Files:** Modify `internal/game/world.go` (claws/XP/drops/announce), `internal/game/rules.go` + `items.go` (condition `condTargetKind`, validation), `internal/game/content.go` (Wyrmslayer item; delete item `dropWeight` fields; per-kind tables already authored in Task 1), `internal/protocol/protocol.go` (delete `MonsterXP`, `MonsterAttackDamage`, `DropChancePercent` — grep the repo for fallout). Tests: extend `monsters_test.go`, `rules_test.go`, `drops_test.go`, `combat_log_test.go`.

**Interfaces:** `monsterClawsDef` becomes per-kind (`closeDefFor` monster branch returns a claws profile built from `kindOf(e).damage`); `resolveDeathsLocked` returns `[]*monsterDef` (slain kinds, id-sorted) instead of `int`; kill award = sum of slain kinds' `xp` through the unchanged earn-XP fold; `dropLootLocked(rng, kind, at)` rolls `kind.dropChance` then weighted-picks from `kind.drops`; `killSummary([]*monsterDef)` names kinds ("a wolf was slain…", "a wolf and a troll were slain (+80 XP…)"); new condition `condTargetKind` (holds when victim's `monsterKind == c.s`), validated against `monsterDefByID`.

- [ ] TDD per behavior; the Wyrmslayer pin test mirrors `TestFirstGearCardsPinned` (dmg 4, ×1.5 via `condTargetKind:"dragon"`, present only in dragon's table).
- [ ] Re-derive seeded drop/XP expectations (wolf's numbers match, so hunt only genuinely shifted ones; document re-derivations in test comments as before).
- [ ] `make check`; commit `feat(game): per-kind combat — damage, XP, monster-side loot, kind announces, targetKind`.

### Task 3: Rings — worldgen bands, spawn distribution, sanctuary zone

**Files:** Modify `internal/game/worldgen.go` + `world.go` spawn paths; `internal/protocol/protocol.go` (+`RingCount = 3`, `SanctuaryRadius = 5`, `DragonCount = 1`). Test: `internal/game/rings_test.go`.

**Interfaces:** `func ringOf(h protocol.Hex, worldRadius int) int` (bands at radius fractions; tiny radii collapse to ring 0); `SpawnMonsters(n)` distributes across rings weighted by ring tile-area, uniform kind pick among the ring's kinds (seeded), dragon capped at `DragonCount`; no hostile spawn within `SanctuaryRadius` of origin; all placement behind the playtest-batch guards.

- [ ] Failing tests: ring math at radius 24 and radius 4; sanctuary zone empty; kind-per-ring placement over a seeded spawn (fixed-seed expectations, worldgen_test.go style); dragon cap.
- [ ] Implement; `make check`; commit `feat(game): difficulty rings — banded spawn placement, sanctuary zone, dragon cap`.

### Task 4: Wire + client — MonsterKind, per-kind looks, window.game

**Files:** `internal/protocol/protocol.go` (`Entity.MonsterKind`, monsters send `Name` = kind display name) + regen; `internal/game/world.go` Snapshot; client `render/entities.ts` (color map keyed by kind + glyph letter from a small `KIND_STYLE` table with a fallback to today's red), `main.ts` (`window.game.positions` entries gain `monsterKind`), e2e spec asserting two kinds render distinct (`monsters.spec.ts` or new `kinds.spec.ts` — the monsters e2e server config may need a second kind seeded; check `playwright.config.ts` env plumbing).

- [ ] Wire + snapshot + contract-test extension; client rendering; `window.game` sync; e2e.
- [ ] `make check` + `make e2e`; commit `feat(client): monster kinds on the wire and on the map`.

### Task 5: Integration tests, docs, gate

**Files:** `test/integration/kinds_test.go` (kill a seeded specific kind over HTTP → its XP lands, its announce text observed — mirror `gear_test.go`'s harness); docs: STATUS.md session note, plan §8 (add the 6c line as landed), `docs/rule-based-content-design.md` (§4 "Drops from:" note → point at monster-side tables as now real; add `targetKind` to the live-conditions list).

- [ ] Integration test green under repetition (no flake); docs updated; full `make check` + `make e2e`; commit `test(integration)+docs: monster kinds end to end; 6c recorded`.

---

## Self-Review

- Spec coverage: registry (T1), per-kind combat/loot/announce + targetKind + Wyrmslayer (T2), rings/sanctuary/dragon cap (T3), wire/client/legibility (T4), HTTP + docs (T5). Aggro-per-kind: consumed via `monsterDef.aggroRadius` overriding the batch's constant — implemented where the batch put the radius check (T1 sets the field, T2/T3 don't touch it; ADD to T1 step: thread the override into the batch's aggro helper). ✔ (noted inline)
- Type consistency: `resolveDeathsLocked` return-type change ripples to both resolve callers and `killSummary` — T2 owns all three. `drop` struct named once (T1), consumed by T2. ✔
- Placeholders: none; per-behavior test detail delegated to named existing idioms (items_test.go, worldgen_test.go, gear_test.go) per the repo's established patterns. ✔
