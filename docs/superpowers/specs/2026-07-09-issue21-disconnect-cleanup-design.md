# Issue #21 — Disconnect Cleanup: Design

*Status: DRAFT for review (2026-07-09). The real product fix behind issue #21
(the e2e-accumulation flake) and the "Entities never leave the world" placeholder:
remove a player's entity when its event stream is gone. Decides part of the open
§9 "offline-character policy". Must not break the M5 reconnect model. Branch off
`main`.*

## Goal

A player's entity should **leave the world when the player is gone** — not linger
forever. Today the SSE stream is anonymous and nothing removes entities, so every
join accumulates a permanent ghost (the root of the #21 e2e flake and a real
product bug: close your tab and your character stands there forever).

## The tension (why not just remove on disconnect)

The M5 reconnect model: the browser's `EventSource` auto-reconnects on any network
blip, keeping the same identity (token in localStorage) and entity id. If we
removed the entity the instant the stream closed, a 1-second blip would delete the
character (losing XP/class/position), and the reconnected client's intents would
fail. So removal needs a **grace period**: remove only after the stream has been
gone long enough that the player is really gone.

## Design

### 1. Identity on the SSE stream
The client connects to **`/api/events?token=<token>`** (EventSource can't set
headers, so a query param). The server reads the token and looks up the entity via
the existing `byToken` map — that stream now belongs to that entity.
*(The token is a bearer secret already sent in POST bodies; in the URL it's
slightly more exposed via logs — acceptable for a 15-friend game, flagged.)*

### 2. Presence tracking (stream count per entity)
Each player `entity` gains a live **stream count** and a `disconnectedAt` time:
- **Stream open** (handler entry, valid token): `world.StreamOpenedLocked(token)` →
  `streams++`.
- **Stream close** (handler return — including `r.Context().Done()`):
  `world.StreamClosedLocked(token)` → `streams--`; if it hits 0, stamp
  `disconnectedAt = now`.
- A freshly **joined** entity starts `streams=0, disconnectedAt=joinTime` — it has
  one grace period to open its stream (normally milliseconds).
- Multiple tabs → `streams > 1`; the entity stays while any stream is open.

### 3. Grace-period sweep
In the control loop (`Run`/`pollTick`, already running each `bubblePoll`), sweep:
remove any **player** entity with `streams == 0` AND `now - disconnectedAt >
DisconnectGrace`. Removal = delete from `entities` + `byToken`, then
`recomputeBubblesLocked` (in case it was mid-fight). The entity vanishes from the
next snapshot; other clients' entity layers despawn it.

### 4. Reconnect keeps the character
A blip → `EventSource` reopens `/api/events?token=X` within the grace →
`StreamOpenedLocked` → `streams++` → the sweep skips it. Only a stream gone for
longer than `DisconnectGrace` (the player really left) is removed.

### 5. Client: reconnect + re-join-if-gone
- `connectEvents` opens `/api/events?token=<token>` (from the stored identity; a
  brand-new client with no token yet connects without one and just watches until
  it joins — or joins first, then connects; keep join-before-stream).
- If the client returns after longer than the grace, its character was removed, so
  its stored token no longer maps to an entity. Minimal handling: if the client's
  own entity id is **absent from turn bundles** for a short spell, **re-join** with
  the stored token (an unknown token mints a fresh entity — existing behaviour) and
  adopt the new identity. This keeps a long-gone player playable (as a new
  character) instead of a dead client.

### 6. Config
`DisconnectGrace` (duration, env `DISCONNECT_GRACE`, default **20s**), threaded
into `NewWorld` like `combatPatience`/`bubblePoll`; shrinkable in tests/e2e.

## §9 policy decision (please confirm)
This decides the open "offline-character policy" as: **offline characters are
removed after a disconnect grace period** — there are no persistent standing-there
avatars. (The alternative — persist offline characters as idle bodies — is a
different game feel and is NOT what #21 wants.) Flagging because §9 lists it as
open.

## Out of scope / follow-ups (decided 2026-07-09 — future work)
- **Character persistence across reconnect** (future slice): a returning player
  should get their **old character back** (XP, class, level), not a fresh one.
  This slice's interim behaviour is the opposite — a player removed after the
  grace re-joins as a **new** character (unknown token → fresh entity). The future
  work archives the character by identity/token on removal and restores it on
  reconnect. Don't design anything here that blocks a later character store keyed
  by token. Tracked in the `character-persistence-reconnect` note.
- **Bed / home spawn point** (future slice): each player has a **bed** (home
  spawn); reconnecting (and dying) returns them to their bed rather than a
  spiral-from-origin spawn. Bed mechanics are TBD (place/claim a bed, one per
  player, maybe tied to a location/quest). Tracked in the `bed-home-spawn` note.
- **Revert the per-spec e2e servers**: with disconnect cleanup + a short e2e
  `DISCONNECT_GRACE`, a shared server would no longer accumulate, so the 6b.2
  per-spec-server workaround could be simplified back. Leave it as-is here (it
  works); note it as a possible follow-up (would also re-validate this fix under a
  shared server).
- **Character deletion UX, reconnect-with-full-state replay** — not this change.

## Tests
- **Unit (`internal/game`)**: `StreamOpened`/`StreamClosed` bookkeeping (count,
  `disconnectedAt`); the sweep removes a player whose streams==0 for >grace and
  keeps one within grace; a reconnect (open after close, within grace) is kept;
  multiple streams keep it; a monster (no token) is never swept; removing a
  bubble member recomputes correctly. Use the injectable clock + a tiny grace.
- **Integration (`test/integration`)**: open an SSE stream with a token, close it,
  advance past a shrunk grace → the entity is gone from the bundle; open, close,
  reopen within grace → still present. A second client sees a disconnected
  player's entity disappear.
- **e2e**: keep passing (per-spec servers stay). Optionally a spec that closes a
  page and a sibling sees the entity count drop — but the shared-server accounting
  is fiddly; the integration test is the load-bearing proof.

## Risks
- **Grace vs reconnect**: `DisconnectGrace` must exceed the `EventSource`
  reconnect interval + the liveness watchdog window, or a normal blip could delete
  a character. Default 20s is comfortably above both (watchdog is
  `max(3s, 4×intervalMs)`).
- **Concurrency**: `StreamOpened/Closed` and the sweep all hold `w.mu`; the
  handler calls them (open on entry, close via `defer`) around the existing
  ctx.Done loop. No new goroutine — the sweep rides the existing `pollTick`.
- **Join-before-stream window**: a joined entity with no stream yet must not be
  swept before its stream opens — the join-time `disconnectedAt` + the grace cover
  it (stream opens in ms).
- **Removing a mid-combat entity**: the sweep runs in `pollTick` alongside
  resolution; remove then `recomputeBubblesLocked`, consistent with the
  capture-then-recompute ordering (do it at a safe point in the pass).
