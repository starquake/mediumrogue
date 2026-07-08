# Milestone 6 warmup — Heartbeat as an observable SSE event: Design

*Status: approved 2026-07-08. First slice of the milestone-6 decomposition
(warmup before 6.1 phased resolution). Closes the milestone-5 carry-forward:
the client's liveness watchdog must survive a frozen world clock.*

## Goal

Promote the SSE keep-alive from an invisible comment frame to a **named event**
the client can observe, and give it a **guaranteed cadence** independent of
turns — so when combat time bubbles (milestone 6.4) freeze the world clock and
turn frames stop, the client's liveness watchdog is still fed and does not
false-trip a reconnect on a perfectly healthy connection.

## Background

- The server keeps SSE connections warm with a comment frame (`: hb\n\n`) every
  `HeartbeatInterval` on an otherwise-idle stream (`internal/server/events.go`).
- `EventSource` **hides comment frames from JavaScript**, so the client can't
  use them as a liveness signal. Its watchdog (added milestone 5,
  `client/src/net/events.ts`) therefore resets only on **turn** frames.
- Once a combat time bubble freezes the local clock, a healthy connection
  legitimately stops delivering turns — and the turn-only watchdog would declare
  it dead and reconnect in a loop. This warmup removes that latent bug ahead of
  6.4.

## Changes

### Protocol (`internal/protocol/protocol.go`)
Add `EventHeartbeat = "heartbeat"` alongside `EventTurn`, so both sides agree on
the SSE event name.

### Server (`internal/server/events.go`)
- Replace the comment write with a **named event**: `event: heartbeat\ndata: {}\n\n`.
  - **No `id:`** — a heartbeat is not a turn and must not advance the
    `Last-Event-ID` resume watermark. (An SSE frame with no `id:` leaves the
    last-event-id unchanged.)
  - `data: {}` is a minimal non-empty payload (SSE does not dispatch an event
    whose data buffer is empty).
- Make the heartbeat **always-on**: drop the `heartbeat.Reset(...)` call that
  currently restarts the heartbeat ticker on every turn. The heartbeat then
  fires on a fixed `HeartbeatInterval` cadence regardless of turns — a
  *guaranteed* liveness pulse, which is the whole point for a frozen clock. The
  minor extra traffic during active play (one small frame per interval) is
  negligible.

### Client (`client/src/net/events.ts`)
- Add a `heartbeat` event listener inside `connect()` that treats a heartbeat as
  proof of liveness: report connected and re-arm the watchdog (same as a turn
  frame, minus the turn payload).
- Add an optional `onHeartbeat` callback to `EventsCallbacks` so the app layer
  can observe heartbeats (for the debug surface / tests).

### Client (`client/src/main.ts`)
- Add `heartbeats: number` to `GameDebug` (initialised `0`), incremented via the
  `onHeartbeat` callback — the observable proof that the client receives named
  heartbeats.

### e2e config (`client/playwright.config.ts`)
- Add `HEARTBEAT_INTERVAL: "500ms"` to the webServer env so heartbeats fire
  several times within a short test (default is 15s — never seen in a fast e2e).

## Tests

- **Integration** — rewrite `TestEventsHeartbeat` (frozen turn clock, fast
  heartbeat): read raw SSE lines and assert a frame with `event: heartbeat`
  arrives, and that **no `id:` line** precedes it (it must not advance the
  resume watermark). This replaces the old "a `:` comment arrives" assertion.
- **e2e** — add a test asserting `window.game.heartbeats` increments above zero
  (the client genuinely receives named heartbeats) while the turn counter also
  advances and `connected` stays true.

## Out of scope

- The full **"watchdog survives a frozen clock"** behaviour is only end-to-end
  testable once a bubble can actually freeze the clock (milestone 6.4). This
  warmup wires the mechanism and proves the client *observes* heartbeats; the
  freeze-survival test lands with 6.4.
- No change to the watchdog window sizing (`max(3s, 4×intervalMs)`) — the
  guaranteed heartbeat (`HeartbeatInterval` < window in every configuration:
  15s < 20s in prod, 500ms < 3s in e2e) keeps it fed.

## Risks

- **`readFrames` interleaving:** with always-on heartbeats, a test using both a
  real (short) `HeartbeatInterval` and the `readFrames` helper could capture a
  heartbeat frame where it expects a turn. Every current `readFrames` caller
  freezes the heartbeat (`HeartbeatInterval = time.Hour`), so none is affected;
  `TestEventsHeartbeat` reads raw lines, not `readFrames`. Left as-is (YAGNI);
  if a future test needs both, teach `readFrames` to skip `heartbeat` frames.
