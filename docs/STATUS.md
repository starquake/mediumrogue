# Project Status — resume here

*Last updated: 2026-07-07, after milestone 3. Update this file at the end of
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

## State: milestones 1–3 done, verified, committed

| Commit | Milestone |
|---|---|
| `d15ff13` | 1 — Skeleton: Go server, SSE turn stream, embedded Vite/TS client, CI, tooling |
| `e1e23fd` | 2 — Static hex world (radius-12, rock rim, lake, forest) rendered via PixiJS |
| `e3e4bcb` | 3 — The turn loop: join + tokens, move intents, per-turn resolution, moving entities |

What works right now (all covered by tests):

- `make server` → world ticks every `TURN_INTERVAL` (default 5 s); SSE stream
  `/api/events` broadcasts full entity snapshots with turn-number ids.
- Browser client: renders the map, joins (identity in localStorage, survives
  reload), moves with QWE/ASD+X (S = wait, currently no-op), sees itself and
  others move on turn bundles. `window.game` exposes state for tests.
- `POST /api/join` (token reclaim), `POST /api/intent` (202/401/422),
  `GET /api/map`, `GET /healthz`.

## Next: milestone 4 — playback & feel

From plan §8: turn results animate over the ~2 s playback window (tween
moves instead of snapping); **visible turn timer** (countdown bar — needs the
server to expose input-window timing, e.g. a `windowEndsAt`/server-time field
in the turn bundle); **click-to-move** with queued paths (client sends target
hex, server pathfinds — needs A* or BFS in `internal/game`, a path queue per
entity that feeds one step per turn, re-validated per tick); first Playwright
test asserting click → multi-turn walk → arrival.

After that (§8): 5 = multiplayer polish + reconnect/`Last-Event-ID` replay
proof, 6 = combat + time bubbles + phased resolution, 6b = classes/species,
7 = procgen, 8 = quests/parties/chat, 9 = shader filter, 10 = deploy.

## Known placeholders / debt (all deliberate)

- **Move resolution order**: ascending entity ID with per-move occupancy
  check (`internal/game/world.go` resolveTurn). Milestone 6 replaces it with
  the decided phased resolution (all moves simultaneously, seeded-RNG
  tie-break on `STACK_CAP` overflow).
- **`Last-Event-ID` replay not implemented**: SSE ids are turn numbers and
  ready for it; the server keeps no replay buffer yet (milestone 5, with a
  full-resync answer for clients too far behind).
- **No same-origin/CSRF guard on POSTs**: acceptable while auth is
  bearer-token-in-body (no ambient credentials). Revisit with real identity.
- **Entities never leave the world**: no disconnect handling — every join
  without a token mints a new entity forever (offline-character policy is an
  open decision in plan §9).
- **S key sends nothing**: explicit wait intents become meaningful with
  combat time bubbles.
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
