# Project Status — resume here

*Last updated: 2026-07-09, after milestone-6b.2 classes (fighter/rogue/mage, ranged + AoE). Update this file at the end of
every working session (milestone landed, decisions made, next step).*

## What this project is

A multiplayer roguelike for a ~15-friend group. Shared hex world on
simultaneous 5-second turns (WeGo); near hostiles the clock stops *locally*
(combat time bubbles) so fights are deliberate and friends can walk in to
help. Browser client, distribution is a URL.

Read in this order:

1. **`docs/roguelike-mp-plan.md`** — the design document. Every game rule
   that has been decided (turn anatomy, hexes, stacking, phased resolution,
   classes/species, XP, quests) and every open question, plus a
   plain-language summary at the top. Design truth lives there, not here.
2. **`CLAUDE.md`** — architecture map, commands, conventions, maintenance
   reminders.
3. This file — where work stopped and what comes next.

## State: milestones 1–5 done, verified, committed

| Commit | Milestone |
|---|---|
| `d15ff13` | 1 — Skeleton: Go server, SSE turn stream, embedded Vite/TS client, CI, tooling |
| `e1e23fd` | 2 — Static hex world (radius-12, rock rim, lake, forest) rendered via PixiJS |
| `e3e4bcb` | 3 — The turn loop: join + tokens, move intents, per-turn resolution, moving entities |
| `milestone-4-playback-feel` (branch, not yet merged) | 4 — Playback & feel: `intervalMs` on turn bundles, server-side BFS path queues, per-entity playback tweens, click-to-move + unified keyboard, visible turn timer |
| `milestone-5-multiplayer-reconnect` (branch) | 5 — Multiplayer & reconnect: `Last-Event-ID` honoured as a resync watermark (resync-to-latest, no replay buffer) + SSE header-flush fix, simultaneous-resolution integration tests, first conflict-resolution tests (friendly stacking, `STACK_CAP` overflow) with a `PlaceEntityForTest` bridge, `window.game.positions`, client SSE liveness watchdog with reconnect |

What works right now (all covered by tests):

- `make server` → world ticks every `TURN_INTERVAL` (default 5 s); SSE stream
  `/api/events` broadcasts full entity snapshots with turn-number ids and an
  `intervalMs` field so the client can derive phase timing without a
  separate `windowEndsAt` field.
- Browser client: renders the map, joins (identity in localStorage, survives
  reload), moves with QWE/ASD (Q/W/E = NW/N/NE, A/S/D = SW/S/SE) or by
  clicking a hex (click-to-move) — both submit a destination intent that the
  server resolves via BFS pathfinding into a per-entity path queue, walking
  one hex per turn. Entities glide between hexes over the playback window
  instead of snapping (per-entity playback tween). A DOM turn-timer bar shows
  the playback/input phase clock live. `window.game` exposes `intervalMs`,
  `phase`, `phaseRemainingMs`, `destination`, `tapHex`, and `positions` (all
  entity ids + hexes, for multi-client test assertions) for tests.
- `POST /api/join` (token reclaim), `POST /api/intent` (202/401/422),
  `GET /api/map`, `GET /healthz`.
- Multiple clients share one world with simultaneous per-turn resolution
  (covered by an integration test posting intents from two clients and
  reading one shared turn bundle back).
- Reconnect/resync: the server honours `Last-Event-ID` only as a
  watermark — a resuming client is coalesced straight to the latest turn
  bundle (resync-to-latest), no replay buffer or separate resync endpoint
  (see plan §4, §9). An SSE header-flush fix ensures the stream opens
  promptly on reconnect.
- Client SSE liveness watchdog: if no data arrives within
  `max(3s, 4×intervalMs)`, the client reports disconnected and reconnects —
  covered by multi-client and reconnect e2e specs.
- Monsters: `MONSTER_COUNT` (default 0) spawns that many monsters at startup
  with seeded, reproducible placement; each turn a monster hunts the nearest
  player and walks toward it, stopping adjacent (never entering the player's
  hex — no combat yet). Entities carry `Kind` (`player`/`monster`) and
  `HP`/`MaxHP` on the wire. The client colours monsters distinctly from
  players and exposes `window.game.monsters` for tests.

## Milestone 6 — decomposed into slices (too large for one spec)

Combat needs hostiles (all entities are players today) and time bubbles are a
whole subsystem, so M6 is being built as a sequence of independently-shippable
slices, each its own spec → plan → PR:

- **6.0 heartbeat warmup — DONE**: named always-on `event: heartbeat`
  + client watchdog resets on it, so the liveness watchdog survives a frozen
  combat clock (see the resolved placeholder above). Closed the milestone-5 debt.
- **6.1 phased resolution — DONE** (this PR): the move phase now resolves all
  moves simultaneously with a per-turn seeded-RNG tie-break (a PCG seeded from the world seed and the turn)
  on `STACK_CAP` overflow, replacing the ascending-entity-ID placeholder.
  Reproducible + no id favoritism (tests pin the seed). The *attack phase*
  (bump-to-attack, post-move-position attacks) is still pending in 6.3.
- **6.2 monsters & HP — DONE**: a hostile entity kind, seeded spawning
  (`MONSTER_COUNT`), HP/MaxHP on the wire, minimal hunt-nearest-player AI
  (stops adjacent, no combat yet), client rendering + `window.game.monsters`.
- **6.3 combat & death — DONE**: bump-to-attack (walk onto a hostile to fight),
  the simultaneous attack phase against post-move positions (retreat dodges,
  mutual kills), damage (`PlayerAttackDamage=5`/`MonsterAttackDamage=3`),
  monster death (removed) and player death (respawn full HP, **same id/token**,
  **no XP penalty yet — that's 6b**). Monster AI now attacks when adjacent.
  Client draws HP bars over damaged entities; `window.game.hp` exposed.
- **6.4 time bubbles — DONE**: local combat time domains. A combat **bubble**
  forms when a player and monster are within `CombatRadius=6` (distance-based) —
  computed as connected components with an opposing pair, which yields
  form/grow/**merge**/**dissolve**/**walk-in reinforce**/**escape** from one
  rule. **Only players extend a bubble's reach**: a component edge needs a
  player endpoint (monster↔monster edges are dropped), so reinforcing players
  chain the frozen area outward while an enemy walking in joins the fight
  without enlarging it. A bubble **freezes** and advances on its own **action-gated** clock
  (all its players lock in an intent, or `COMBAT_PATIENCE` (default 60s) elapses)
  while the world keeps ticking every `TURN_INTERVAL` around it. Wire:
  `Entity.InCombat` + `TurnEvent.Bubbles` (`waitingForIds`, `patienceRemainingMs`).
  Client: an in-combat marker + a "waiting for… · Ns" combat panel;
  `window.game.inCombat`/`bubble`. **Milestone 6 complete.**

## Milestone 6b — classes/species + XP (decomposed like M6)

- **6b.1 XP & leveling — DONE** (this PR): players earn **shared XP** on a kill
  (every player in the fight/bubble gets the full `MonsterXP`, no last-hit
  competition), **level is derived** from XP (`1 + xp/XPPerLevel`), and death
  **floors XP to the current level's start** (keep the level, lose within-level
  progress) — resolving the 6.3 "no XP penalty yet" debt. Wire: `Entity.XP`/
  `Level`; client: a level/XP stats HUD + `window.game.xp`/`level`. A level
  grants **no mechanical bonus yet** — that arrives with classes/species.
- **6b.2 classes — DONE** (this PR): fighter/rogue/mage with distinct combat.
  Per-class HP (fighter 30 tanky, rogue 16, mage 14) + weapon damage, both
  **scaling with level** (levels now matter). **Class-default equipped weapons**
  (rogue dagger+bow, fighter sword, mage staff) — melee **bump** uses the close
  weapon; **ranged attack intents** (`kind:"attack"`) add rogue **bow**
  (single-target) and mage **AoE magic** (`no friendly fire`); fighter has no
  ranged. Class chosen at join (client picker, default fighter); `Entity.Class`
  + `window.game.class`. Ranged rules: `BowRange`/`MageRange=4`, AoE radius 1,
  distance-only (no terrain LOS). **Gear inventory/equip/drops deferred** (see
  the `gear-equipment-system` note — 6b.2 uses class defaults + unarmed fallback).
- **6b.3 species — NEXT**: human (learns faster → XP multiplier), elf (crits), dwarf (soak).

After that (§8): 7 = procgen, 8 = quests/parties/chat, 9 = shader filter, 10 = deploy.

## Known placeholders / debt (all deliberate)

- **No gear/inventory yet**: classes (6b.2) use **class-default equipped weapons**
  (rogue dagger+bow, fighter sword, mage staff) with an unarmed (fists) fallback;
  there's no inventory, equip/swap, or loot drops — that's a later gear slice
  (see the `gear-equipment-system` note). No **species** passives yet (6b.3). No
  **terrain-blocked LOS** for ranged (distance-only). Killed monsters are removed
  and **do not respawn** (fixed pool depletes; continuous spawning is later).
  `protocol.PlayerAttackDamage` is now an orphaned constant (melee uses class
  weapons) — remove opportunistically.
- **`spawnHexLocked` is faction-blind**: `Join` and player respawn pick the
  nearest free walkable hex without avoiding monster-occupied hexes, so a
  player can spawn co-located with a monster (opposing co-occupancy). Inert
  (only *movers* bump-attack, so co-located entities just sit until one moves,
  then resolve normally) but technically violates the §5 "hostiles never share
  a hex" invariant — add a faction-aware spawn guard when it matters. **6.4
  note:** with time-bubble domain scoping, a joiner/respawn near an active
  bubble is also invisible to that bubble's scoped resolution for one pass
  (self-heals at the pass-end recompute) — the domain split now leans on the
  post-recompute separation invariant, so fix this when continuous spawning lands.
- **Monsters don't extend bubble reach (6.4, deliberate)**: bubble-graph edges
  require a player endpoint, so a wandering monster within `CombatRadius` of a
  *bubble monster* but far from every bubble player stays world-domain. Harmless:
  two same-faction monsters can momentarily co-locate across the world/bubble
  boundary, but monsters don't fight monsters, and player↔monster domain scoping
  is unaffected (a monster adjacent to a bubble player is still always linked in
  via a player↔monster edge).
- **Terrain-blocked line-of-sight not implemented (6.4)**: combat bubbles form
  by pure hex **distance** (`≤ CombatRadius`), not mutual line-of-sight — rock
  doesn't block "spotting" yet. Deferred follow-up (adds a hex raycast).
- **E2e on shared stateful servers is timing-flaky**: both `multiplayer.spec.ts`
  (M5 reconnect via SSE `route.abort()`) and the `combat.spec.ts` damage test
  occasionally time out under parallel-worker contention — the shared Playwright
  servers accumulate every spec's players (no disconnect cleanup, below), so
  monsters can chase a lingering player and starve a chase, or reconnect timing
  drifts. Not milestone-specific; the real fix is per-test isolation / disconnect
  cleanup. Harden separately (re-run on a spurious CI red for now).
- **No server-side input-window enforcement**: intent acceptance stays
  permissive (an intent is accepted whenever it arrives, regardless of the
  client-visible timer phase); revisit once combat time bubbles (milestone 6)
  need a hard cutoff.
- **No re-pathing around a route blocked mid-walk**: if a queued path's next
  step becomes unwalkable/occupied, the entity just waits at its current hex
  rather than recomputing a detour.
- **No multi-hex-per-turn travel**: destination intents always walk exactly
  one hex per turn, even out of danger — deliberate for now, revisit for
  combat/flee mechanics (milestone 6).
- **No same-origin/CSRF guard on POSTs**: acceptable while auth is
  bearer-token-in-body (no ambient credentials). Revisit with real identity.
- **Entities never leave the world**: no disconnect handling — every join
  without a token mints a new entity forever (offline-character policy is an
  open decision in plan §9). **E2e consequence:** a shared Playwright server
  accumulates every spec's entities for the whole run — so under CI's worker
  parallelism accumulated players can fill a hex to `StackCap` and block an
  unrelated movement spec's walk (reproduced under `--workers=12`), and a
  monster-server spec can wedge a combat bubble (unstuck only by the 60s
  `COMBAT_PATIENCE` AFK fallback). Fix: `playwright.config.ts` gives **every spec
  its own single-consumer server** (a project + webServer per spec file, DRY over
  a `specs` list; `MONSTER_COUNT` set only where needed), so cross-spec state
  sharing is structurally impossible. Add a new e2e spec to that `specs` list.
  Tracked as **issue #21**; the real product fix is disconnect cleanup.
- **No explicit wait input**: standing still = not sending an intent. An
  explicit wait intent may become useful inside combat time bubbles
  (milestone 6) — decide then.
- **No combat-bubble "waiting for: …" timer state**: the turn timer shows
  playback/input phases only; the milestone-6 combat time bubble will need a
  distinct "paused, waiting on nearby players" state.
- **Reconnect/resync model is resync-to-latest, not replay**: with
  full-snapshot turn bundles and a coalescing hub, `Last-Event-ID` is honoured
  only as a watermark to avoid re-painting an already-seen turn — a
  reconnecting client is simply coalesced to the current snapshot. There is
  no replay buffer and no separate resync endpoint (deliberately; see plan
  §4, §9).
- **Mid-session SSE drop isn't e2e-reproducible in the sandbox**: Playwright's
  `setOffline` doesn't sever an already-open stream, so the reconnect e2e
  instead blocks `/api/events` with `route.abort()` and later restores it to
  simulate a drop/reconnect cycle.
- **Watchdog now resets on named heartbeats** (m6 warmup, done): the server
  emits a named `event: heartbeat` frame (no id) on a fixed always-on
  `HeartbeatInterval`, and the client's liveness watchdog re-arms on it as well
  as on turns. So a frozen world clock (no turns) still feeds the watchdog. The
  full "watchdog survives a frozen clock" scenario is only end-to-end testable
  once combat time bubbles (6.4) can actually freeze it; this warmup wired the
  mechanism and proved the client observes heartbeats (`window.game.heartbeats`,
  `client/e2e/heartbeat.spec.ts`).
- **nolint audit reminder** lives in CLAUDE.md (6 suppressions as of m3).

## Environment & gotchas (this repo, this machine)

- **Go is NOT on PATH**: use `export PATH=$PATH:/usr/local/go/bin` (the
  Makefile already falls back to `/usr/local/go/bin/go`).
- **Bash tool cwd drifts** between calls in long sessions — `cd` to the repo
  root (or use absolute paths) before `make`/`git`, and remember
  `make ... | tail` masks failures unless `set -o pipefail` is set first.
  This bit us once: a "passing" check that had actually failed.
- Playwright Chromium is installed (`npx playwright install chromium` done).
- `make check` = lint + protocol drift + TS check + tests + build. The
  protocol gate diffs `client/src/protocol.gen.ts` against git — after
  changing `internal/protocol`, run `make protocol` and stage the result.
- E2E (`make e2e`) runs the *real* binary with `TURN_INTERVAL=250ms` on port
  8123 (see `client/playwright.config.ts`).
- The topbanana repo (pattern source: hub, Makefile, server layering) can be
  re-cloned from `starquake/topbanana` if needed for reference.

## Working agreements observed so far

- Every feature lands with tests at the right layer (unit / integration /
  e2e) and `window.game` stays in sync for Playwright.
- Verify with `make check` + `make e2e` **before** committing; screenshot
  visual changes (headless Playwright + Read the PNG).
- Commit messages explain the *why* and note placeholders explicitly.
- Game-rule constants go in `internal/protocol` (shared with the client);
  timing knobs are env vars so tests can shrink them.
