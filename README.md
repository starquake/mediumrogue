# Medium Rogue - An experiment in agentic engineering, disguised as a co-op hexcrawl for fifteen friends.

A shared hex world that moves in simultaneous 4-second turns — until you meet
a monster, and time freezes locally into a proper turn-based fight your
friends can walk into. Runs in the browser; distribution is a URL.

**Very early.** It runs, and the systems below are real, but it is not a game
you can sit down and play yet — expect gaps, placeholder numbers, and things
that change without warning.

## Stack

Go server (authoritative simulation, SSE turn stream, single binary with the
client embedded) + TypeScript/PixiJS client (Vite). Protocol types are
generated Go → TS by tygo. No third-party Go modules.

## Documentation

| | |
|---|---|
| [docs/FEATURES.md](docs/FEATURES.md) | Everything that actually exists — mechanics, systems, constants, env vars. Doubles as the player manual. Start here to find out what the game *does*. |
| [docs/design.md](docs/design.md) | The full design: every decided rule and every open question, with the reasoning. |
| [docs/game-identity.md](docs/game-identity.md) | What this is and isn't. One page, written because feature requests drift toward whichever genre label is nearest. |
| [docs/design-decisions.md](docs/design-decisions.md) | Decisions of record — the direction taken, and the things deliberately cut. |
| [docs/content-authoring.md](docs/content-authoring.md) | How to invent gear, monsters and skills without writing code. Aimed at a non-programmer. |
| [docs/mockups/](docs/mockups/) | UI mockups, approved before the real thing was built. |

Live work is in GitHub issues and milestones, not in a roadmap file.

## The agentic engineering part

Built with [Claude Code](https://claude.com/claude-code) against a written
workflow rather than ad-hoc prompting:

| | |
|---|---|
| [CLAUDE.md](CLAUDE.md) | The always-loaded rules: architecture, conventions, domain patterns, how work lands. |
| [.claude/skills/](.claude/skills/) | Encoded procedures — designing a slice, building it, adding content, reviewing style, driving the issue board. |
| [.claude/rules/](.claude/rules/) | Go style specifics, including the linter traps worth knowing. |

Design happens in the issue, the maintainer approves it there, and only then
does code get written. Every mechanic is a pure-data rule card, so content is
a table entry rather than a code change.

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

## License

**Code: [MIT](LICENSE)** — © 2026 Jan Visser.

**Third-party assets keep their own terms.** The entity glyphs are from
[game-icons.net](https://game-icons.net) and are licensed **CC BY 3.0**, not
MIT — per-icon authors are credited in
[`client/tools/glyph-icons/README.md`](client/tools/glyph-icons/README.md) and
in-app on the start screen. If you reuse them, carry the attribution.

Dependencies are permissive throughout: the server has **no third-party Go
modules at all** (standard library only), and the client is PixiJS + SolidJS,
both MIT.
