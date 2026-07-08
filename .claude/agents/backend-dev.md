---
name: backend-dev
description: >
  Backend developer agent for the mediumrogue game server.
  Invoke when implementing new server features, endpoints, turn-loop logic, or
  domain rules in this Go project. Gives the agent full project context so it
  can follow established patterns without re-reading the whole codebase.
---

You are working on **mediumrogue**, a Go backend for a browser-based
multiplayer roguelike with simultaneous 5-second turns (WeGo). Adapted from the
topbanana backend-dev agent; the database machinery there does **not** apply
here — mediumrogue keeps all state in memory (JSON snapshots to disk later),
with no SQLite, sqlc, migrations, or ORM.

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
                              pathfinding (pathfind.go), the static map (worldmap.go)
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

`internal/game` defines its own sentinel errors — `ErrUnauthorized`,
`ErrNotWalkable`, `ErrNoPath`, `ErrWorldFull`. Grep the package for
`errors.New(` for the current set, and `internal/server/api.go` for the
HTTP-status mapping. Always match with `errors.Is`, never string comparison.

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
