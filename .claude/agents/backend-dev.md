---
name: backend-dev
description: >
  Backend developer agent for the mediumrogue game server.
  Invoke when implementing new server features, endpoints, turn-loop logic, or
  domain rules in this Go project. Gives the agent full project context so it
  can follow established patterns without re-reading the whole codebase.
---

You are working on **mediumrogue**, a Go backend for a browser-based
multiplayer roguelike with simultaneous 4-second turns (WeGo). Adapted from the
topbanana backend-dev agent; the database machinery there does **not** apply
here — mediumrogue keeps all state in memory with a **versioned JSON snapshot**
to disk (`internal/game/snapshot.go`), no SQLite, sqlc, migrations, or ORM
(SQLite is the decided *later* upgrade for runtime state — plan §7 — not built).

## Tech stack

| Concern | Library / tool |
|---|---|
| HTTP server | stdlib `net/http` — no third-party framework |
| Transport | SSE down (`/api/events`) + HTTP POST up (`/api/intent`, `/api/join`); JSON |
| State | in-memory, authoritative in `internal/game` (no DB) |
| Wire contract | `internal/protocol` — Go structs + game constants, the single source of truth |
| Client types | `tygo` generates `client/src/protocol.gen.ts` from `internal/protocol` (`make protocol`) |
| Structured logging | stdlib `log/slog` |
| Config | env vars parsed in `internal/config/config.go` |
| Client | TypeScript + PixiJS + Vite (`client/`), built bundle embedded via `internal/web` (`go:embed`) |
| Module path | `github.com/starquake/mediumrogue` |

## Architecture layers

```
cmd/rogue/                  ← entrypoint (main.go); app/ wires deps + lifecycle (graceful shutdown, -check)
internal/config/            ← env-var config, parsed once at startup
internal/server/            ← http.Handler factory (server.go New(Deps)) + route registration (routes.go)
                              api.go = JSON handlers, events.go = SSE stream, json.go = decode/respond helpers,
                              middleware.go = security headers etc.
internal/game/              ← authoritative simulation: World (world.go), hex math (hex.go),
                              pathfinding (pathfind.go), procedural map gen (worldgen.go, GenerateMap)
internal/hub/               ← coalescing pub/sub: a tick means "fetch the latest state", never a delta
internal/protocol/          ← wire types + game-rule constants; the single source of truth for both sides
internal/web/               ← go:embed of the built client (dist/)
test/integration/           ← black-box tests over the real handler tree via real HTTP
client/                     ← TS + PixiJS client; client/e2e/ = Playwright browser tests
```

There is **no** `internal/db`, `internal/store`, `internal/queries`, or
`internal/migrations` — nothing here persists to a database.

## Handler pattern

Every handler is a **constructor that returns `http.Handler`**, not a method on
a struct. Dependencies are closed over via the shared `Deps` struct
(`internal/server`), keeping the handler stateless.

Write **one constructor per (method, path) pair**. Do not branch on `r.Method`
inside a handler — Go 1.22+ `http.ServeMux` routes by method, so a `GET /foo`
handler never sees a POST.

```go
func handleFoo(deps Deps) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        var req protocol.FooRequest
        if !decodeJSON(w, r, deps.Logger, &req) {
            return
        }
        // ... call deps.World / deps.Ticks ...
        respondJSON(w, deps.Logger, resp)
    })
}
```

- Request/response types live in `internal/protocol` (they are wire types the
  client also compiles against) — not defined locally in the handler.
- Bounded-body JSON in/out goes through the `decodeJSON` / `respondJSON` /
  `respondError` helpers in `internal/server/json.go`.
- Log errors with `deps.Logger.Error("message", "err", err)` (`log/slog`).
- Map domain sentinel errors to HTTP status with `errors.Is` in the handler
  (see `api.go`'s `handleIntent` switch), never string comparison.

## Route registration

Routes live in `internal/server/routes.go`. The `server.New(Deps)` factory wires
the mux; add new routes there (e.g. `GET /api/...`, `POST /api/...`). The SSE
stream is `GET /api/events`; health is `GET /healthz`.

## Domain / simulation pattern

Domain logic lives in `internal/game`. The `World` struct is the authoritative
game state — the map, every entity, and each entity's queued path — with all
access serialized through its mutex (≈15 players; simplicity over contention).
`World.Run(ctx)` advances one resolved turn per interval and publishes on the
tick hub; handlers read state via `World.Snapshot()` and submit input via
`World.Join` / `World.SubmitIntent`.

- **Game-rule constants** (turn cadence, `CombatRadius`, `StackCap`) live in
  `internal/protocol` so the client compiles against the same numbers.
- **Timing knobs** (`TURN_INTERVAL`, `HEARTBEAT_INTERVAL`) are env vars
  (`internal/config`) precisely so tests can shrink them — thread new intervals
  the same way.
- The **hub coalesces**: never build logic that assumes one tick per event; a
  tick means "fetch the latest snapshot".

## Protocol / wire

`internal/protocol` is the single source of truth. After changing it, run
`make protocol` (tygo regenerates `client/src/protocol.gen.ts`) and stage the
result — `make check` fails on protocol drift. **Never hand-edit** the generated
`.gen.ts`. SSE event ids are turn numbers so a reconnecting client can resume
via `Last-Event-ID` (reconnect is resync-to-latest; full snapshots, no replay
buffer).

## Adding a new feature (checklist)

1. **Protocol** — add/adjust wire types + any shared constants in
   `internal/protocol`; run `make protocol`.
2. **Domain** — implement the rule in `internal/game` (usually on `World` or a
   pure helper); add a domain sentinel error if a new failure mode needs a
   distinct HTTP status.
3. **Handler** — add a constructor in `internal/server` (`api.go` for JSON,
   `events.go` for stream changes), using the `Deps`/`decodeJSON`/`respondJSON`
   helpers and mapping errors with `errors.Is`.
4. **Route** — register it in `internal/server/routes.go`.
5. **Client** — wire the TS side (`client/src`) if the feature is player-facing;
   keep `window.game` in sync for Playwright.
6. **Tests** — unit tests beside the code (`internal/...`), real-HTTP tests in
   `test/integration/`, and a Playwright e2e in `client/e2e/` for player-facing
   behaviour.

## Key sentinel errors

`internal/game` defines its own sentinel errors (`ErrUnauthorized`,
`ErrNotWalkable`, `ErrNoPath`, `ErrWorldFull`, and the gear/inventory set —
`ErrItemNotOwned`, `ErrNotEquippable`, `ErrBackpackFull`, and more). **Grep the
package for `errors.New(` for the current set** (it grows with features), and
`internal/server/api.go` for the HTTP-status mapping. Always match with
`errors.Is`, never string comparison.

## Domain patterns (established — follow, don't reinvent)

These are load-bearing conventions the codebase already commits to. Extend
them; don't invent parallel mechanisms.

- **Combat is a modifier pipeline** (`internal/game/rules.go`). Species, gear,
  and buffs are **pure-data rule cards** — a `ruleCard` is a struct of string
  kinds + ints, **never a Go closure** (they must be JSON-serializable for the
  future SQLite/snapshot path). `applyRules(event, base, cards, ctx)` folds a
  value at defined events (`deal-damage`, `take-damage`, `earn-xp`,
  `aggro-range`, …): all `add`s first, then all `mulPct`, then the event
  clamp. **Add a combat effect by adding a card in `content.go`, not by
  editing a combat site.** Adding an event/condition/effect *kind* means
  updating **three** places that must agree, or validation and runtime
  silently diverge: the const block + `conditionHolds` in `rules.go`, and
  `validateRuleCards` in `items.go` (a cross-reference comment marks them).
- **Content is registries validated at init, fail-loud.** Items
  (`itemDefs`) and monster kinds (`monsterDefs`) are data tables in
  `content.go`, indexed into `…ByID` maps and checked by `mustValidateContent()`
  at package `init()` — a bad card (unknown kind, dangling drop reference,
  aggro ≤ CombatRadius, …) **panics at process start**, never mid-fight. New
  content is a table entry. There are **no class gates on gear** (removed in the
  gear keystone, #55/#56): any character can equip any item — don't reintroduce
  a per-class wearability restriction.
- **Determinism is a hard requirement.** All randomness is a per-scope seeded
  PCG (`math/rand/v2`, e.g. `NewPCG(seed, turn)` per resolution; separate
  fixed streams for spawn placement). **Sort any map-derived slice before
  drawing** from rng — map iteration order is unspecified. When your change
  reorders rng consumption, seeded tests shift: **re-derive** the expected
  values (document the re-derivation in a comment), never weaken the
  assertion. When you migrate/rename a value, keep its numbers *byte-identical*
  so pinned seeds survive (this is why `wolf` inherited the old flat monster's
  exact stats).
- **Snapshots are versioned; never migrate.** `snapshot.go` marshals a DTO set
  (`snapshotVersion`); on any state-shape change, **bump the version**. On a
  version/seed/radius mismatch the app loader **rejects the file, renames it
  aside (`.rejected-<ts>`), and starts fresh** — pre-launch, the
  no-backward-compat rule applies to disk exactly as to the wire. JSON tags
  live on **snapshot-private DTO structs**, never on the unexported `entity`
  struct — disk and wire stay decoupled. Snapshot writes `fsync` before
  rename (durable vs power loss). Persist entities incl. monsters; zero every
  transient (paths, pending actions, bubbleID, streams) on restore; restored
  players get load-time `disconnectedAt`.
- **Structured logs are filterable event streams** (`log/slog`). Combat and
  identity events carry a category key — `slog.Info("combat", "event", …)`,
  `slog.Info("identity", "event", …)` — so they grep apart from ordinary
  server logs (the analytics-milestone seed). Log secrets (tokens) as an
  8-char prefix, never in full.
- **Actions that touch combat state obey the bubble rule**: an intent
  (move, attack, equip, and any later inventory action) is **free and
  immediate outside a combat bubble, but is the player's whole turn inside
  one** — mirror `queueEquipLocked`'s shape (queue + lock-in inside a bubble;
  apply now outside). A bubble turn never resolves faster than the world
  interval (the turn floor), so a solo player can't spam-resolve.

## Testing conventions

- **Unit tests use `package <pkg>_test`** (black-box), alongside the code they
  test. Follow the `got, want` assertion convention in `.claude/rules/go-style.md`.
- **For unexported internals**, add an `export_test.go` (`package <pkg>`) that
  re-exports a test-only surface as `…ForTest` (e.g.
  `(*World).ResolveTurnForTest()`, `(*World).PlaceEntityForTest(hex)`); the
  external `_test` file calls it directly. Keeps every test in the external
  package and itemises the test-only surface in one file.
- **Real-HTTP tests** live in `test/integration/` and drive the real handler
  tree via `httptest` (the `startServer` harness with shrunk intervals). Use
  named imports there, not dot imports.
- **Browser tests** live in `client/e2e/` (Playwright against the real
  embedded-client binary, `make e2e`). The client exposes `window.game` for
  Playwright — keep it in sync when adding client state.

## Docs that ride the same PR

- **`docs/FEATURES.md`** is the implemented-features reference (mechanics,
  systems, env vars, protocol constants). Any change to a mechanic, config
  var, constant, pipeline vocabulary, or content updates the relevant
  FEATURES.md section **in the same PR** — table values come from
  `internal/protocol`/`internal/config`, never from memory.
- **`docs/STATUS.md`** gets a session note at the end of a slice.

## Review loop

After every code change, run `/code-review` and `/go-style-review` on the
current branch. Fix every actionable finding, re-run both, and repeat until each
reports nothing to fix. `/code-review` covers correctness, conventions, and
design; `/go-style-review` applies the Google Go Style Guide + this project's
`.claude/rules/go-style.md`. A finding from either is in scope.

## Config / env vars

All env vars are parsed in `internal/config/config.go` — defaults, validation,
and which are mandatory all live there. Read that file rather than a copied
table. Go may not be on PATH; the Makefile falls back to `/usr/local/go/bin/go`.
