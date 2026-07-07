# Milestone 4 — Playback & Feel: Design

*Status: approved 2026-07-07. Scope: all four M4 features in one plan
(playback tweening, turn timer, click-to-move with server pathfinding, first
Playwright e2e). See `docs/roguelike-mp-plan.md` §8 milestone 4 and
`docs/STATUS.md` for surrounding context.*

## Goal

Turn the bare turn loop (milestone 3) into something that *feels* like the
game: entities glide between hexes instead of snapping, a visible countdown
communicates the turn rhythm, and the primary input becomes click-to-move —
click a hex and your entity walks there on its own over successive turns. Land
the first Playwright end-to-end test proving click → multi-turn walk → arrival.

## The turn-cycle timing model (conceptual core)

The server remains a plain `time.Ticker`; it does **not** enforce the
3s-input / 2s-playback split. That split is a **client-side presentation
convention**, hard-re-synced on every turn-bundle arrival:

- Each `TurnEvent` carries `intervalMs` — the runtime `TURN_INTERVAL` in
  milliseconds. The client cannot derive this from compile-time constants
  because `TURN_INTERVAL` is env-configurable (5s in production, 250ms in the
  e2e suite) while `TurnSeconds`/`InputWindowSeconds`/`PlaybackSeconds` are
  fixed constants.
- On each bundle arrival the client restarts a local **phase clock** of length
  `intervalMs`, split by the compile-time ratio:
  - **playback phase** = `intervalMs × PlaybackSeconds / TurnSeconds` (first
    ~2/5) — entities tween to their new positions.
  - **input phase** = the remainder (~3/5) — the countdown bar drains.
- Because each bundle resets the clock, drift cannot accumulate and there is no
  client/server clock-skew to estimate. A bundle arriving slightly early or
  late just re-syncs the phase clock; the only artifact is a small, self-
  correcting jump in the bar.

**Intent acceptance stays permissive.** The server accepts intents right up to
resolution; the latest submission in a window wins (unchanged from milestone
3). The input-window bar is a UI cue, not a hard gate. Server-side window
enforcement (rejecting late intents) is explicitly deferred.

## Protocol changes (`internal/protocol`)

- `TurnEvent`: add field `IntervalMs int64` (`json:"intervalMs"`) — the runtime
  turn interval in milliseconds.
- `IntentRequest.Target` is **redefined from "adjacent step" to "destination"**:
  any walkable hex, not just a neighbor. The wire shape is unchanged (still a
  single `Hex`); only the meaning widens and the doc comment is rewritten.
- New error sentinel semantics: an unreachable destination is a client error
  (see server section). No new wire type — it maps to the existing
  `ErrorResponse` / `422`.
- After editing, run `make protocol` to regenerate `client/src/protocol.gen.ts`
  and stage the result (the `make check` protocol-drift gate diffs it).

## Server changes (`internal/game`)

### Pathfinding — new `internal/game/pathfind.go`

- Breadth-first search over walkable hexes (grass/forest), uniform step cost,
  deterministic neighbor order (the existing `HexNeighbors` order). BFS is
  sufficient — every walkable hex costs 1 — and simpler than A*.
- Signature (sketch): `func (w *World) pathLocked(from, to protocol.Hex) []protocol.Hex`
  returning the ordered list of steps **excluding** `from` (so a 1-hex move is
  a single-element slice), or `nil` if `to` is unwalkable or unreachable.
  Callers hold `w.mu`.
- Bounded by the map (radius `MapRadius`); the search space is small
  (~hundreds of hexes) — no need for optimization.

### Entity path queue

- `entity` gains `path []protocol.Hex` — the remaining route. This replaces the
  `intents map[int64]protocol.Hex` field's role; remove `intents` from `World`.
- `SubmitIntent(req)` (now destination semantics):
  1. Authn as today (entity exists + token matches → else `ErrUnauthorized`).
  2. Reject if `req.Target` is not walkable → `ErrNotWalkable`.
  3. Compute `path := w.pathLocked(e.hex, req.Target)`; if `nil` (unreachable)
     → new sentinel `ErrNoPath`.
  4. Store `e.path = path`, replacing any prior route (latest submission wins,
     consistent with milestone-3 "latest intent wins").
  - Destination == current hex is a no-op success (empty path).
- API mapping (`internal/server/api.go`): `ErrNoPath` joins `ErrNotWalkable` in
  the `422` branch.

### Turn resolution

- `resolveTurn`: iterate entities in ascending-ID order (unchanged placeholder
  ordering — milestone 6 replaces it with phased resolution). For each entity
  with a non-empty `path`:
  - Peek the next step. If `occupancyLocked(step) < StackCap`, move the entity
    there and pop the step. Otherwise leave the entity in place and **keep** the
    step (it waits this turn, retries next turn).
  - When the path empties, the entity idles.
- Re-validation each turn falls out of the occupancy check. Re-pathing around a
  route that became blocked mid-walk is **out of scope**: a blocked step simply
  waits.

### Snapshot

- `Snapshot` sets `IntervalMs: w.interval.Milliseconds()` (the `World` already
  holds `interval`). Entities' wire shape is unchanged (id + hex).

## Client changes (`client/src`)

### Hex picking — `render/hex.ts`

- Add `pixelToHex(point: Point): Hex` — the inverse of `hexToPixel` with proper
  hex rounding (fractional axial → cube round → axial), per Red Blob Games.
  This stays pure rendering/picking math; **no pathfinding on the client** (the
  server owns game-rule hex math, including reachability).

### Playback tween — `render/entities.ts`

- Refactor `EntityLayer` from wholesale-redraw to **per-entity sprites keyed by
  entity ID** (the existing code comment already anticipates this).
- On each turn bundle, for each entity tween from its currently-rendered hex to
  the new hex over the **playback-phase duration**. Moves are always ≤1 hex per
  turn, so every tween is a clean single-hop.
- New entities (first seen) appear at their position without a tween. No
  despawn yet (entities never leave in M4), so removal handling is a no-op
  guard.
- Preserve stack rendering: top sprite + `×N` count badge for shared hexes.
- The tween is driven by the Pixi ticker (or the phase clock); it must finish
  within the playback phase so entities are settled before the next bundle.

### Click-to-move + unified keyboard — `main.ts`, `input/`

- Pointer handler on the canvas: canvas coords → world coords (undo the
  `world` container centering translation) → `pixelToHex` → submit as the
  destination intent.
- **Keyboard unifies onto the same path**: a movement key computes the adjacent
  neighbor (existing `neighbor(from, dir)`) and submits it as a 1-hex
  destination. Resulting behavior is identical to milestone 3 (one step per
  press), but there is now a single "submit a destination" code path.
- Submitting a destination records it as the client's pending `destination`
  (for the timer/HUD and testing); it clears when `me.hex` equals it.

### Turn timer — `index.html` overlay + `ui/timer.ts`

- A DOM overlay bar (not canvas), consistent with the plan's "social UI is
  HTML/CSS over the canvas" rule and its testability.
- Driven by the phase clock: show a "resolving…" state during the playback
  phase, then a draining bar during the input phase. Single-state only — the
  combat-bubble "waiting for: Piet, Anna…" state is milestone 6.
- Reset on each bundle arrival.

### Testability surface — `window.game`

Extend `GameDebug` (keep it in sync — a design rule):

- `intervalMs: number` — from the latest bundle.
- `phase: "playback" | "input"` and `phaseRemainingMs: number` — current phase
  clock state.
- `destination: Hex | null` — the hex the client last asked to path to; null
  once reached (or never set).
- `tapHex(q: number, r: number): void` — debug hook that runs the **exact**
  click code path (submit that hex as a destination). Lets the e2e drive moves
  deterministically without pixel-fragile canvas clicking; the real pointer
  handler wires actual clicks for humans.

## Testing

### Unit — `internal/game`

- Pathfind BFS: straight line; routing around a water/rock obstacle;
  unreachable target (walled off) → nil; destination == start → empty path;
  destination unwalkable → nil.
- `resolveTurn`: an entity with a multi-step path advances exactly one hex per
  turn until arrival, then idles; an occupancy-blocked next step causes a wait
  (entity stays, path retained) and resumes when the hex frees.
- `SubmitIntent`: destination replaces a prior in-progress path; unwalkable →
  `ErrNotWalkable`; unreachable → `ErrNoPath`; bad token → `ErrUnauthorized`.

### Integration — `test/integration`

- Join, POST a far but reachable destination, then read the SSE stream and
  assert the entity's hex advances one step per bundle and arrives at the
  destination.
- Assert `intervalMs` is present and positive in turn bundles.
- POST an unreachable/unwalkable destination → `422`.

### e2e — `client/e2e` (Playwright against the real embedded binary)

- Load the page; wait for join. `window.game.tapHex(q, r)` a reachable hex;
  assert `window.game.me.hex` reaches it over several turns and
  `window.game.destination` clears on arrival.
- Assert the timer bar element exists in the DOM and its width animates across
  a turn (observe `phase`/`phaseRemainingMs` transitioning).
- Runs at `TURN_INTERVAL=250ms` (existing `client/playwright.config.ts`).

## Out of scope (deferred deliberately)

- Multi-hex-per-turn travel out of danger (plan §5 "maybe"; likely mooted by
  time bubbles) — stays one hex/turn.
- Server-side input-window enforcement — acceptance stays permissive.
- Re-pathing around a route blocked mid-walk — a blocked step just waits.
- The combat-bubble "waiting for: X" timer state — milestone 6.
- Camera follow / panning — the world stays centered on origin.
- Move-resolution order — stays the ascending-ID placeholder; milestone 6
  lands the decided phased resolution (all moves simultaneous, seeded tie-break
  on overflow).

## Risks & mitigations

- **Tween not finishing before the next bundle** (e.g. very short intervals):
  clamp tween duration to the playback phase and snap to the authoritative hex
  on the next bundle regardless — the server snapshot is always the source of
  truth, so a dropped/short tween degrades to a snap, never a desync.
- **`pixelToHex` rounding errors** at hex borders: use the standard cube-round
  algorithm; unit-test round-trips (`pixelToHex(hexToPixel(h)) == h`) for a
  sample of hexes.
- **Path queue vs. reconnect**: paths live server-side on the entity, so a
  client reconnect (fresh EventSource) resumes watching an in-progress walk
  with no special handling — a benefit of server-side pathing.
