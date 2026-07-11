# Milestone 10a — Persistence & Identity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Characters survive absence (sweep archives, token rejoin restores), the world survives restarts (versioned JSON snapshot), and identity is a copyable character link.

**Architecture:** An in-world `archive` map bridged by the sweep and Join; a `snapshot.go` marshal/restore pair over the persisted field set with a hard version/seed/radius gate; app-level load-at-boot + periodic/shutdown saver behind `SNAPSHOT_PATH`/`SNAPSHOT_INTERVAL`; client-side `#t=` token import + a copy-link HUD button.

**Spec:** `docs/superpowers/specs/2026-07-11-m10a-persistence-identity-design.md` — read it first; its decisions (what persists, what stays transient, fresh-on-mismatch) are binding.

## Global Constraints

- `make check` green every commit; FEATURES.md/STATUS/plan-doc updates ride this same PR (CLAUDE.md convention).
- Snapshot content = exactly the spec's field set; every transient zeroed on restore is named in a test.
- No new deps — encoding/json, os.Rename atomicity, existing app background-task draining.
- Default-off (`SNAPSHOT_PATH=""`) keeps every existing test hermetic; only the new tests opt in (t.TempDir()).
- Go style `.claude/rules/go-style.md`; commits per task.

---

### Task 1: Character archive — sweep archives, Join restores
**Files:** `internal/game/world.go` (+`archive` field, sweep, Join), `internal/game/archive_test.go`.
**Produces:** `characterRecord{name, class, species string; xp int; items []itemInstance; closeSlot, rangedSlot int64}`; `World.archive map[string]characterRecord`; Join order live→archived→new.
- [ ] TDD: sweep→archive→restore round-trip (XP/gear/equipped identical, new hex from guarded spawn, full level-scaled HP, archive entry consumed); unknown token unaffected; archived restore emits no start-screen-relevant change server-side (it's just Join). Commit `feat(game): disconnect sweep archives characters; token rejoin restores them`.

### Task 2: Snapshot marshal/restore
**Files:** Create `internal/game/snapshot.go`, `internal/game/snapshot_test.go`.
**Produces:** `snapshotVersion` const; `(w *World) MarshalState() ([]byte, error)`; `(w *World) RestoreState(data []byte) error` (fresh world only; version/seed/radius mismatch → typed error). Persisted set and zeroed transients exactly per spec §2 (entities incl. monsters, groundItems, quests, archive, turn/nextID/nextBubbleID; restored players `disconnectedAt = now`, streams 0).
- [ ] TDD: round-trip equality over the persisted set (build a world with players+gear+ground items+taken quest, marshal, restore into a fresh world, compare snapshots); mismatch gates; transients zeroed; restored-unclaimed players sweep after grace (grace from LOAD time — pin the spec's risk); turn continues monotonic. Commit `feat(game): versioned world snapshot — marshal/restore with fresh-on-mismatch`.

### Task 3: App + config wiring
**Files:** `internal/config/config.go` (+`SnapshotPath string`, `SnapshotInterval time.Duration` default 60s, validation: interval positive), `cmd/rogue/app/app.go` (load before Run; saver goroutine on the interval; final save in graceful shutdown via the existing background-task pattern — read how the app drains tasks and mirror it), config tests + an app-level test if the package has the pattern.
- [ ] TDD config; wire app; save errors log-and-continue (never crash the loop); atomic write (tmp+rename, same dir). Commit `feat(app): snapshot load-at-boot and periodic/shutdown saves behind SNAPSHOT_PATH`.

### Task 4: Integration — restart survival over HTTP
**Files:** `test/integration/persistence_test.go` (mirror the harness bootstrap; it constructs the world directly, so drive MarshalState/RestoreState + config the way the app does — check how the harness builds servers and add a snapshot-enabled variant).
- [ ] Test: server A: join, kill for XP (one-hit harness idioms), pick up an item, snapshot; server B from the file: token rejoin over HTTP restores XP/gear/name/class; monsters/ground items/quests match; a second fresh-token join still works. Stable across repetition. Commit `test(integration): the world survives a restart; characters survive a sweep`.

### Task 5: Client — character link import/copy + e2e + docs
**Files:** `client/src/net/session.ts` (fragment import before loadIdentity; strip via history.replaceState), `client/src/main.ts` + `client/index.html` (copy-link button in the HUD, hidden until joined; clipboard + "copied" flash; `window.game.identityLink` for tests), `client/e2e/identity.spec.ts` (new), docs: FEATURES.md (env vars table, persistence + identity sections), STATUS.md, plan §7 note ("snapshot implemented") + §9 identity item → decided.
- [ ] e2e: join in context A, read `window.game.identityLink`, open context B at that URL → same character, no start screen, fragment stripped. Full `make check` + `make e2e`. Commit `feat(client): character link — copy from the HUD, import via #t= fragment; docs`.

---

## Self-Review
- Spec coverage: archive (T1), snapshot+gates (T2), app/env (T3), restart-over-HTTP (T4), link+e2e+docs (T5). ✔
- Type consistency: `characterRecord` defined T1, serialized T2 (snapshot embeds the archive map) — T2 owns its JSON shape; entity JSON shape is snapshot-private (tags in snapshot.go's own DTOs, NOT on the unexported entity struct — keep wire/protocol and disk decoupled). ✔ (binding note for the implementer)
- Placeholders: harness specifics delegated to named files per repo convention. ✔
