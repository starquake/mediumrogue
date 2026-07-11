# Milestone 10a — Persistence & Identity: Design

*The launch gates. Today a server restart wipes the world, and the
disconnect sweep DELETES a player's character — leave for an hour, lose
your gear and levels. Both must be fixed before the URL goes to the group.
Implements plan §7's decided baseline (in-memory + periodic JSON snapshot;
SQLite is the decided LATER upgrade for state) and the
`character-persistence-reconnect` note. Deployment itself (VPS/Caddy) is
ops, not this slice. Bed/home spawns stay future.*

## Goal

1. **Characters survive absence**: the disconnect sweep archives a player's
   character (identity, XP, gear) instead of deleting it; rejoining with the
   same token restores it — fresh spawn hex, full HP, everything else as
   left. Pop-in/pop-out play stops being destructive.
2. **The world survives restarts**: periodic + on-shutdown JSON snapshot,
   loaded at startup. Deploys and crashes no longer reset everyone.
3. **Identity is portable**: a player can copy a **character link**
   (URL with their secret token) to move browsers/devices. Settles plan
   §9's open identity question as "name + secret link".

## 1. Character archive (in `internal/game`)

- New world map `archive map[string]characterRecord` (key = token):
  `{name, class, species string; xp int; items []itemInstance; closeSlot,
  rangedSlot int64}`. Everything else (hex, hp, paths, bubbles) is
  transient by design — a restored character is *as if freshly joined* but
  with its progression and gear.
- `sweepDisconnectedLocked` moves the record into the archive before
  deleting the entity (players with a token only, as today).
- `Join` order: live token → reclaim (unchanged); **archived token →
  restore**: new entity with the archived record, spawn via the existing
  guarded random spawn, `hp = maxHPFor(class, level)`; archive entry
  removed (it lives in exactly one place). Unknown token → new character
  (unchanged; name/class/species required).
- Party/quest state is NOT archived: parties dissolve and personal quests
  return to the board on sweep (existing behavior, unchanged — quests and
  parties are session-scoped social state; progression is what persists).

## 2. World snapshot (new `internal/game/snapshot.go` + app wiring)

- **Format**: one JSON file, atomic write (tmp + rename). Top-level
  `{version, worldSeed, worldRadius, turn, nextID, nextBubbleID, entities,
  groundItems, quests, archive}`. `snapshotVersion` is a const bumped on
  ANY breaking state-shape change; **a mismatched version, seed, or radius
  logs and starts fresh** (no migrations pre-launch — the
  no-backward-compatibility rule applies to disk exactly as to the wire).
- **What persists**: all entities (players AND monsters — a restart must
  not respawn a healed, repositioned monster population mid-expedition),
  with transient fields zeroed on restore (paths, attackTarget,
  pendingEquip, bubbleID, streams; every player restores as disconnected
  with `disconnectedAt = load time`, so unclaimed entities sweep into the
  archive after the normal grace). Ground items, the quest board (holders
  reference persisted entity ids), the archive, and the id/turn counters
  (turn persists so SSE ids stay monotonic across restarts). Bubbles are
  never persisted — recomputed from positions on the first tick.
- **API**: `World.MarshalState() ([]byte, error)` and
  `RestoreState([]byte) error` under the world lock; restore only into a
  fresh, not-yet-running world (app startup), erroring on version/seed/
  radius mismatch (caller logs + continues fresh).
- **App wiring** (`cmd/rogue/app`): config gains `SNAPSHOT_PATH` (default
  **empty = disabled** — tests and casual `go run` stay hermetic; the
  deployment sets it) and `SNAPSHOT_INTERVAL` (default `60s`). When
  enabled: load at startup before `world.Run`; a background saver goroutine
  writes every interval; one final write during graceful shutdown (join the
  existing background-task draining). Save errors log and continue — a
  full disk must not kill the game loop.

## 3. Character link (client)

- On join, the client can surface the identity as a **character link**:
  `<origin>/#t=<token>`. A small "copy character link" button in the HUD
  (DOM, next to the status line; hidden until joined). Clipboard write via
  `navigator.clipboard`, with a visible "copied" flash.
- On load, a `#t=<token>` fragment **imports** the identity: store to
  localStorage, strip the fragment (`history.replaceState`), proceed with
  the normal join — the server restores/reclaims that token's character.
  The fragment never reaches the server (hashes aren't sent in requests)
  and never lands in chat.
- The start screen is unaffected (an imported token is a "returning
  player" and skips it).

## Out of scope

SQLite (decided later), bed/home spawns, snapshot migrations, multi-world,
admin endpoints for state, encrypting the snapshot (the VPS disk is the
trust boundary for 15 friends), rate-limiting join.

## Tests

- **Unit**: archive round-trip (sweep → archive → Join restores XP/gear/
  identity, fresh hex + full HP, archive entry consumed); snapshot
  marshal/restore round-trip equality on the persisted field set; version/
  seed/radius mismatch → error; transient fields zeroed; restored players
  sweep after grace if unclaimed; turn monotonicity across restore.
- **Integration**: boot server A (harness) with a snapshot path, join +
  earn XP + pick up an item, shut down gracefully, boot server B on the
  same file: token rejoin over HTTP restores the character; world state
  (monsters, ground items, quests) matches the shutdown snapshot.
- **e2e**: join, read `window.game` identity link, open a SECOND browser
  context on that link → same character (name/class/XP) without the start
  screen; fragment stripped from the URL bar.
- FEATURES.md same-PR convention: new env vars, the persistence section,
  identity story.

## Risks

- Snapshot writes hold the world lock during marshal — 15 players and
  ~2k tiles is small (the map itself is NOT persisted; it regenerates from
  the seed), but marshal-under-lock should copy-then-encode if it measures
  slow.
- Restored-as-disconnected interacts with the sweep: the grace must start
  at LOAD time, not the pre-restart `disconnectedAt`, or every restore
  sweeps instantly. Pinned by a test.
- Token in a URL is a shoulder-surfable secret — acceptable for the trust
  model (15 friends), noted in FEATURES.md.
