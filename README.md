# mediumrogue

A multiplayer roguelike for our group: a shared hex world that moves in
simultaneous 4-second turns — until you meet a monster, and time freezes
locally into a proper turn-based fight your friends can walk into. Runs in
the browser; distribution is a URL.

The design lives in [docs/design.md](docs/design.md).

## Stack

Go server (authoritative simulation, SSE turn stream, single binary with the
client embedded) + TypeScript/PixiJS client (Vite). Protocol types are
generated Go → TS by tygo.

## Develop

```sh
make dev          # terminal 1: Go server with auto-restart (:8080)
make client-dev   # terminal 2: Vite dev server with HMR (:5173, proxies /api)
```

## Verify

```sh
make check        # lint + protocol drift + typecheck + tests + build
make e2e          # Playwright against the real binary (once: cd client && npx playwright install chromium)
```

## Run the real thing

```sh
make build        # client bundle + embedded server binary
./build/bin/rogue # serves everything on :8080
```
