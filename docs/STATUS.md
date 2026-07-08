# Project Status — resume here

*Last updated: 2026-07-08, after milestone 5. Update this file at the end of
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

## Next: milestone 6 — combat, time bubbles, phased resolution & death

From plan §5, §8: bump-to-attack (moving onto a hostile-held hex resolves as
an attack); phased resolution — all moves resolve simultaneously with a
seeded-RNG tie-break on `STACK_CAP` overflow, then all attacks resolve
against post-move positions — replacing the milestone-3 ascending-entity-ID
placeholder; local combat time bubbles (clock stops locally on mutual LOS
within `COMBAT_RADIUS`, action-gated turns, rest of the world keeps ticking);
death → fall back to the start of the current XP level. Also fold in the
heartbeat-as-event watchdog upgrade noted below.

After that (§8): 6b = classes/species, 7 = procgen, 8 = quests/parties/chat,
9 = shader filter, 10 = deploy.

## Known placeholders / debt (all deliberate)

- **Move resolution order**: ascending entity ID with per-move occupancy
  check (`internal/game/world.go` resolveTurn). Milestone 6 replaces it with
  the decided phased resolution (all moves simultaneously, seeded-RNG
  tie-break on `STACK_CAP` overflow).
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
  open decision in plan §9).
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
- **Watchdog liveness is turn-based, not heartbeat-based**: the client's SSE
  liveness watchdog currently treats "no turn bundle within
  `max(3s, 4×intervalMs)`" as disconnected. Once milestone 6's combat time
  bubbles can legitimately freeze the world clock (no turn bundles for a
  while during a fight is expected, not a disconnect), this needs to key off
  a dedicated `event: heartbeat` frame instead of turn cadence — promote the
  existing heartbeat comment to a named SSE event.
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
