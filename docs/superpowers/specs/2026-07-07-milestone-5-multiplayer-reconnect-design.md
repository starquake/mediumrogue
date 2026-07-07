# Milestone 5 — Multiplayer & Reconnect: Design

*Status: approved 2026-07-07. See `docs/roguelike-mp-plan.md` §8 milestone 5
and `docs/STATUS.md` for surrounding context.*

## Goal

Prove the game is genuinely multiplayer and survivable across a dropped
connection: two+ clients in one world resolving intents simultaneously and
watching the same turns, a client that loses its stream reconnecting and
resyncing cleanly, and a first test locking the observable conflict-resolution
behaviour (friendly stacking + `StackCap`). Almost all of this already works by
construction; milestone 5 is a small server hardening plus the verification the
architecture has been promising since milestone 1.

## Two decisions this design rests on

1. **Reconnect model: resync-to-latest (YAGNI).** No delta replay buffer, no
   separate resync endpoint. Turn bundles are full entity snapshots and the hub
   coalesces ("a tick means fetch the latest, never a delta"), so a reconnecting
   client only ever needs the *current* snapshot — which the SSE handler already
   sends on any fresh connection. This **supersedes** the early replay-buffer /
   resync-endpoint language in plan §4; that was written for a delta-based world
   that no longer exists.
2. **Conflict resolution: test current behaviour only.** Add tests for the
   *observable*, milestone-6-stable outcomes (two friendlies converging on one
   hex both stack; moves respect `StackCap`). Do **not** lock the exact overflow
   tie-break winner and do **not** implement the decided phased/seeded
   resolution — that stays milestone 6.

## Why so little new code

The snapshot + coalescing architecture already delivers most of milestone 5:

- The server broadcasts full-snapshot bundles to *every* SSE subscriber, and
  `resolveTurn` applies *all* queued intents in one turn — so two clients moving
  simultaneously already resolve correctly and already see each other.
- `EventSource` already auto-reconnects (and sends `Last-Event-ID`), and
  `handleEvents` already writes the current snapshot on any fresh connection
  (`lastSent` starts at `-1`) — so reconnect already resyncs to latest.

The remaining work: honour `Last-Event-ID` so a reconnect doesn't redundantly
re-paint a turn the client already has, expose entity positions so the
multi-client test can observe cross-client movement, and prove the whole thing
with tests.

## Server change — honour `Last-Event-ID` (`internal/server/events.go`)

One focused change to `handleEvents`: seed the `lastSent` watermark from the
`Last-Event-ID` request header instead of always `-1`.

- Parse `r.Header.Get("Last-Event-ID")` as an `int64`; on empty or unparseable
  input, default to `-1` (a helper, e.g. `parseLastEventID(r) int64`).
- Use that value as the initial `lastSent` before the immediate
  `writeTurn(...)` call. Everything else in the handler is unchanged.

Resulting behaviour (all falls out of the existing `writeTurn` "skip if
`turn == lastSent`" check):

| Client | `lastSent` seed | Immediate send |
|---|---|---|
| Fresh (no header) | `-1` | current snapshot (unchanged) |
| Reconnect, no turn since drop (`current == k`) | `k` | skipped — waits for the next tick (client already has `k`) |
| Reconnect after N turns (`current > k`) | `k` | current snapshot immediately (missed intermediates skipped — correct for full snapshots) |
| Reconnect after a server restart (`current < k`, e.g. turn reset to 0) | `k` | current snapshot (differs from `k`, so it sends — the client gets the fresh world) |

No replay buffer, no new endpoint, no client change for this (the browser's
`EventSource` sends `Last-Event-ID` automatically on reconnect).

## Client change — expose entity positions (`client/src/main.ts`)

The multi-client test needs client A to observe client B's *position*, but
`window.game` today exposes only `me` and an entity *count*. Add one
non-breaking field to `GameDebug` and populate it from each turn bundle:

- `positions: { id: number; hex: Hex }[]` — every entity in the latest bundle.

Keep the existing `entities` count field unchanged so current tests are
untouched. No other client changes: `EventSource` handles `Last-Event-ID`
transparently, multiplayer rendering already distinguishes self (blue) from
others (gold), and the connection-status HUD already exists.

## Tests

### Unit — `internal/game` (conflict resolution, observable outcomes)

A small `export_test.go` bridge, `PlaceEntityForTest(hex protocol.Hex) int64`,
places an entity at a chosen hex so setups are deterministic rather than
depending on spawn geometry (same style as the existing `ResolveTurnForTest`).

- **Friendly stacking:** two entities on hexes adjacent to a common hex both
  submit that hex as their destination; one `resolveTurn` → both land on it
  (stack of 2). Proves simultaneous resolution + friendly stacking.
- **`StackCap` overflow:** five entities already on hex X; a sixth, adjacent,
  submits X → after resolve the sixth is **blocked** (stays put) and X holds
  exactly `StackCap`. Asserts the stable invariant (`occupancy ≤ StackCap`, cap
  reachable) — **not** the specific tie-break winner (that changes at M6).

### Integration — `test/integration` (real HTTP, precise server behaviour)

- **`Last-Event-ID` resume:** open `/api/events`, read frames, note the last id
  `k`; disconnect; advance turns (POST an intent and let turns tick); reconnect
  **with a `Last-Event-ID: k` header** → assert the first frame received has an
  id `> k` (the server honoured the watermark and did not re-send `k`).
- **Fresh connection:** no `Last-Event-ID` header → the client receives the
  current turn immediately (locks the unchanged fresh-connect path).
- **Simultaneous resolution over the wire:** two joins, both POST an intent, and
  one SSE stream shows *both* entities at their new positions on the same
  bundle.

### e2e — `client/e2e` (real browser, the headline proof)

- **Multi-client visibility:** two browser contexts (A, B) each join as distinct
  entities (separate `localStorage`); both report `entities === 2`; A moves and
  B's `window.game.positions` reflects A's entity at its new hex (and vice
  versa) — two clients watching the same world.
- **Reconnect (pulled-plug):** `context.setOffline(true)` → `window.game.connected`
  flips to `false` (HUD shows "reconnecting…"); `context.setOffline(false)` →
  `connected` returns to `true`, the turn counter **resumes advancing** past its
  pre-offline value, and `me.hex` stays consistent. Proves `EventSource`
  reconnect + resync-to-latest end to end.

## Docs (part of the milestone)

- `docs/roguelike-mp-plan.md`: annotate §4 that the replay-buffer / resync-
  endpoint language is superseded by resync-to-latest (full snapshots +
  coalescing); tick the related §9 open decision.
- `docs/STATUS.md`: mark milestone 5 done; remove the "`Last-Event-ID` replay
  not implemented" known-placeholder; set the next milestone to 6 (combat, time
  bubbles, phased resolution, death). Set `Last updated`.

## Out of scope (deferred deliberately)

- Phased/seeded conflict resolution and bump-to-attack — milestone 6.
- Combat, time bubbles, and death — milestone 6.
- Any delta / replay-buffer machinery — mooted by resync-to-latest.
- Server-side input-window enforcement — acceptance stays permissive.
- Presence/roster UI beyond the existing per-entity rendering (self vs. others,
  stack count badges) — not required to prove multiplayer.

## Risks & mitigations

- **`Last-Event-ID` parsing:** a malformed header must never break the stream —
  default to `-1` (fresh-connect behaviour) on any parse failure, so a bad value
  degrades to "send current," never to an error.
- **Reconnect e2e flakiness:** `context.setOffline` toggling can race the
  `EventSource` retry timer; assert with `expect.poll` on `window.game.connected`
  and on the turn counter advancing (Node-side polling, not in-page timers), per
  the milestone-4 lesson about headless-CI timer throttling.
- **Multi-client identity bleed:** the two contexts must not share `localStorage`
  (they wouldn't, as separate Playwright contexts) — assert `A.me.id !== B.me.id`
  as a guard so a regression that collapses them fails loudly.
