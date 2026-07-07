# Multiplayer Roguelike — Project Plan

**Genre target:** Shared-world roguelike with **simultaneous turns** (WeGo) — procgen, tile-based; death stings (XP setback) but is not permanent
**Simulation model:** One world turn every **5 seconds** (3 s input → instant resolution → ~2 s playback); near hostiles the clock **stops locally** and turns become action-gated (combat time bubbles)
**Visual style:** Hex-tile-based, Caves of Qud–inspired mood, with a post-processing filter
**Stack:** Go server (single binary) + TypeScript/PixiJS browser client
**Transport:** HTTP POST (intents up) + SSE (turn bundles & chat down)
**Distribution:** A URL in the group chat — no installs, no builds per platform

---

## How the Game Plays — a plain-language summary

*For someone who knows games but doesn't care about the tech. The gist of every decision in this document, described as the game you'd actually experience.*

**Getting in.** There's nothing to install. You get a link in the group chat, open it in your browser, claim your character — picking one of three classes (**rogue, fighter, or mage**) and one of three species (**human** learns faster, **elf** lands critical hits more often, **dwarf** shrugs off part of every hit) — and you're standing in the world. When the game gets updated, you just refresh — everyone is always on the same version.

**The world.** A shared fantasy world built on hexagon tiles, with deliberately simple, chunky retro graphics under a CRT-style filter — think old-school roguelike charm rather than modern polish. All ~15 of us are in the same world at the same time, each with our own character.

**How time works — the heartbeat.** The world moves in shared beats: every 5 seconds, one "turn" happens for everyone at once. During the first 3 seconds you can choose an action; then everything everyone chose plays out together in a short animation. In practice you barely notice the rhythm while exploring: you click somewhere on the map and your character walks there on their own, beat by beat, while you chat. You never have to be quick — if you do nothing, you simply stand still. Nobody has an advantage for having fast reflexes or a better internet connection; the game is closer to a board game where everyone moves their piece simultaneously than to an action game.

**When danger appears — time stops (for you).** The moment you and a monster spot each other within 6 tiles, the clock freezes *locally* — for you, the monster, and anyone standing nearby. The fight now plays like a proper turn-based tactics game: you can stare at the battlefield and think as long as you like. The turn advances when everyone in the fight has locked in their choice (with a patience limit of about a minute, so one distracted player can't stall the fight forever).

Here's the fun part: **the rest of the world keeps running at normal speed.** Your friends elsewhere on the map see your fight frozen mid-swing, marked as "in combat" — and they can walk over and *step into it*. Entering the fight area means joining the fight; there's no invite screen or loading transition. Yelling "help, three ghouls, north bridge!" in chat and watching the cavalry arrive is an intended core experience. Escaping works the same way in reverse: break line of sight or get far enough away, and you slip back into normal time.

**Fighting.** Combat is classic roguelike at heart: walk into an enemy to hit them. The three classes fight differently: a **fighter** deals steady melee damage and is tough enough to hold the front; a **mage** is fragile but casts area magic from the back, hitting groups of enemies at once; a **rogue** hits hardest against single targets but can't take a beating, and switches weapons by distance on their own — dagger against something adjacent, bow against something far away. Within each class there's variety to find: weapon types that trade speed against damage against reach, and different kinds of magic. Within a turn, all movement happens first, then all attacks land. Two consequences you'll feel immediately: stepping *away* from an enemy genuinely dodges the swing aimed at you (so retreating is a real tactic, not just delaying death), and two combatants who go for each other can absolutely take each other down on the same turn — those mutual-kill moments are meant to be dramatic, not glitchy.

**Traveling together.** Up to 5 players can stand on the same tile, so a party moves as one stack — one blob on the map heading somewhere with a shared destination. When something attacks the stack, it hits a random member, so a group soaks danger together. And 15 players *can't* all fit on one tile, which quietly encourages the group to split into a few parties instead of one unstoppable death ball.

**Quests.** Two flavors. **Player quests** are yours: you pick them, you pursue them. **Party quests** are shared: someone takes one, pitches it in chat ("who's coming?"), and invites others in. Groups aren't assigned — they form around whoever proposes something interesting, and dissolve just as naturally.

**Progression and death.** Your character earns **XP levels and gear** through play. XP comes from slain enemies, and it's shared generously: the moment an enemy falls, everyone standing in that fight gets the same, full amount — nobody competes for last hits, and running over to help a friend's fight always pays off, kill by kill, from the moment you arrive. Dying is not the end — no roguelike permadeath here — but it has a real sting: **you fall back to the start of the XP level you were in**, losing the progress inside that level. Levels themselves are never taken away, so an evening's real progress survives a bad fight, but "I'm 80% to level 6" is exactly the thing you're gambling when you pick one more battle. Death makes fights tense without ever deleting your character.

**Why it's built this way, in one line each:**
- *Slow shared turns* → fair for everyone, chat-friendly, and lag simply doesn't matter.
- *Frozen local fights* → real tactical thinking without holding up the rest of the world.
- *Walk-in reinforcements* → fights become social events, not private instances.
- *Browser-only* → zero setup for fifteen people with fifteen different computers.
- *Simple hex graphics + filter* → charm over polish, and every hour goes into the game instead of art.

---

## 0. Context & Game Concept (why this project exists)

The real goal: a fun game for our own group of **~15 people** — a kind of **mini-MMO** built with AI assistance doing the heavy lifting.

- **Graphics stay very simple and hex-tile-based** — deliberately, since graphics skills are limited. Aesthetic effort goes into the shader/filter pass, not art assets.
- **A walkable shared world with quests, in two types.** **Player quests** are personal: each player picks their own. **Party quests** are shared: a party works on them together, and players can invite others to join their quest. Parties form organically around the quest someone pitches in chat — no assigned teams.
- **The whole game runs on simultaneous 5-second turns.** Everyone chooses an action in the same 3-second window; the server resolves everything at once; everyone watches the outcome together. It plays like a board game night, not a twitch game.
- **The group may split into ~3 parties** that each tackle quests, rather than 15 people in one blob.
- **Three classes: rogue, fighter, mage.** Enough identity to make parties feel composed ("we need a mage for this") without ballooning the design. Each has a hard weapon identity:
  - **Rogue** — dagger *or* bow, chosen automatically by distance: adjacent target → dagger, distant target → bow. **High single-target damage, squishy.** The flexible mid-liner.
  - **Fighter** — melee weapons only. **Medium damage, tanky** — the one who can afford to stand in front and take hits.
  - **Mage** — magic only. **AoE damage, squishy.** The back line, hitting areas instead of single targets.
  - Depth comes from **variety within each lane**: different weapon types for rogue/fighter (speed vs damage vs reach), different magic types for mage (damage, control, support) — that's what gear drops and progression feed into.
- **Three species: human, elf, dwarf** — one passive bonus each, freely combinable with class (9 combos):
  - **Human** — +% XP gain (levels faster)
  - **Elf** — +% critical-hit chance
  - **Dwarf** — % damage reduction
  - The percentages are config constants, balanced in playtests. Species choose a *style* (grow fast / spike hard / survive), class chooses a *job*.
- **Progression: XP levels and gear are earned through play.** Death is not permadeath, but it stings: **you fall back to the start of your current XP level** (levels and your character survive; in-level progress does not).

**Design implications to keep in mind:**
- ~15 concurrent players is the *actual* scale target — tiny by MMO standards. Don't over-engineer for scale; optimize for fun and for sessions where most of the group is online at once.
- The shared turn cadence is the great equalizer: mixed reflexes and skill levels don't matter, network latency is irrelevant (200 ms ping vs a 3000 ms window), and there's natural room to chat between turns.
- Quest/party systems are first-class features, not stretch goals: shared quest state, party membership, and proximity logic all need protocol support.
- AFK handling falls out of the model naturally: no input = "wait" (or continue a queued path). No turn ever blocks on a missing player.

## 1. Why Go + a Browser Client

The stack decision evolved (Rust/Bevy → Go/Ebitengine → this) as the design got clearer. The reasons that settled it:

- **The turn model removed every performance argument.** The server resolves ~15 players' intents once per 5 seconds — any language handles that. The original "no GC pauses, real-time entity ticking" rationale for Rust no longer applies.
- **Distribution is the real constraint** for a casual 15-friend group across Windows/macOS/Linux. A browser client turns distribution into *a URL*: no installers, no code-signing, no "which download do I click," and every update reaches everyone instantly.
- **AI-verifiability drove the client choice.** A TypeScript/PixiJS client can be driven end-to-end with Playwright: click hexes, press keys, then *query live game state directly* and read console errors — far stronger verification than screenshot-squinting at an opaque canvas (the Ebitengine/WASM weakness). Since AI does the heavy lifting here, a testable client is a faster, safer project.
- **HTML/CSS for the social surface.** Chat, quest log, party UI, and the turn timer — the parts this game actually lives on — are ordinary DOM elements floating over the canvas, not widgets hand-drawn in a game engine.
- **Go is familiar territory**, cross-compilation worries vanish (only the server needs building), and goroutines + `net/http` cover everything the server does.

## 2. High-Level Architecture

```
+----------------------+        HTTP POST /intent        +----------------------+
|  Browser client      |  ---------------------------->  |  Go server           |
|  - PixiJS hex render |                                 |  - Turn loop (5 s)   |
|  - HTML UI (chat,    |  <----------------------------  |  - Authoritative     |
|    quest log, timer) |     SSE stream: turn bundles,   |    resolution        |
|  - Playback anim     |     chat, world events          |  - RNG / procgen     |
|  - WebGL filter      |                                 |  - Serves the client |
+----------------------+                                 +----------------------+
```

- **Server is authoritative.** All world state, turn resolution, combat outcomes, and RNG/procgen live server-side.
- **Client is a renderer + intent sender.** During the input window it POSTs the player's chosen action (an *intent*); each turn it receives a turn bundle over SSE and plays it back. It never decides outcomes.
- The message flow is beautifully simple: one intent up per player per turn; one turn-result bundle down per turn. No streaming state, no prediction, no interpolation of live movement.
- **One deployable:** the Go binary embeds the built client (`go:embed`) and serves static files + SSE + intent endpoints from a single process.

## 3. Repository Layout

Go module at the repo root, per the official server-project layout
(https://go.dev/doc/modules/layout#server-project) — no `server/` wrapper dir:

```
medium-rogue/
├── go.mod                   # module github.com/starquake/medium-rogue
├── cmd/rogue/               # main + app: lifecycle, serves client, SSE, intents
├── internal/
│   ├── protocol/            # wire types & game constants (single source of truth)
│   ├── game/                # world clock; later: turn resolution, hex math, procgen
│   ├── hub/                 # coalescing pub/sub (tick = "fetch latest")
│   ├── server/              # routes, middleware, SSE handlers
│   ├── config/              # env-based config
│   └── web/                 # go:embed of the built client (dist/)
├── test/integration/        # real-HTTP tests against the real handler tree
├── tools/                   # pinned Go-built CLI tools (tygo) — own module
├── client/                  # TypeScript + PixiJS (Vite)
│   ├── src/
│   │   ├── protocol.gen.ts  # GENERATED from internal/protocol (tygo)
│   │   ├── net/             # EventSource + intent POSTs
│   │   ├── render/          # (later) hex map, entities, playback animation
│   │   └── ui/              # (later) HTML overlay: chat, quest log, turn timer
│   └── e2e/                 # Playwright tests driving the real binary
└── Makefile                 # build client → embed → build server
```

**Reuse from `starquake/topbanana` — DECIDED.** Topbanana (Go + SSE + browser-client quiz game) is the proven chassis; this repo adopts its patterns, adapted not copied verbatim:

- **Scaffolding:** the Makefile pattern (pinned self-downloading tools into `build/bin`, `make check` aggregate gate, `make dev` via watchexec, `.env` include, `tools/go.mod` dependabot trick), the strict `.golangci.yml`, CLAUDE.md/CONTRIBUTING/docs conventions, GitHub Actions CI + dependabot + PR template.
- **Server code:** `cmd/server/app` bootstrap (main → app.Run, graceful shutdown, background-task draining, `-check` smoke flag); the `internal/server` layer (per-surface `addXRoutes` split, middleware, security headers, same-origin check on unsafe methods); the **SSE hub + heartbeat pattern** from `leaderboard`/`livesession` (coalescing pub/sub, buffered-1 tick channels, heartbeat interval threaded in for fast tests) as the backbone of the turn-bundle stream; `internal/config` env-based config and the bounded-body JSON decode/respond helpers.
- **Frontend:** keep **Vite + TS + PixiJS** (typed protocol, Playwright-testable) — topbanana's esbuild/vanilla-JS approach is *not* adopted, but its `go:embed` assets/serving pattern is reused for the built bundle.
- **Testing/infra from day one:** the integration-test harness style (real server in-process, real HTTP/SSE), the Playwright e2e scaffold, the coverage gate (`.testcoverage.yml`), `distrobox.ini.example`, Dockerfile/docker-compose templates.
- **Not adopted:** SQLite/sqlc/goose (plan stays in-memory + JSON snapshots to start) and the full auth stack (passwords, email verification, CSRF forms) — identity here is name + secret link; topbanana's session-cookie manager can be borrowed later if needed.

**Protocol drift defense (two languages, one wire):**
- The Go `protocol` package is the single source of truth; **`tygo` generates the TypeScript types** in the build. Hand-editing `protocol.gen.ts` is forbidden.
- A **contract test** replays recorded turn bundles through both the Go encoder and the TS decoder in CI.
- Turn cadence constants (`TURN_SECONDS`, input/playback split) live in the protocol package and are exported to the client the same way.

## 4. Transport: SSE + POST (decided)

The wire carries one small intent up per ≤5 s and one turn bundle down per 5 s — not a realtime workload. That makes **Server-Sent Events + plain HTTP POST** the best fit:

| | Why it wins here |
|---|---|
| **Reconnection built in** | `EventSource` auto-reconnects and sends `Last-Event-ID`; the server replays missed turn bundles from that ID. Reconnect/resync — the hardest part of a persistent game — rides on a browser primitive. |
| **Debuggable with curl** | The turn stream can be watched in a terminal and intents POSTed by hand — ideal for AI-driven verification and for humans at 1 a.m. |
| **Just HTTP** | No upgrade handshake, proxy-friendly, trivial in Go (`http.Flusher`) and in the browser. |

- Considered and set aside: **WebSocket** (works, but buys nothing at this cadence and costs hand-rolled reconnect/heartbeat — the fallback if genuinely chatty bidirectional traffic ever appears), **gRPC** (browsers can't speak it natively; needs a proxy), **Connect-RPC** (respectable schema-first alternative; more build tooling than this project warrants).
- Messages are **plain JSON** — human-readable on the wire, matching the debuggability theme.
- Turn bundles get monotonically increasing event IDs = turn numbers; the server keeps a short replay buffer for reconnecting clients, and a full-state resync endpoint covers clients that fall too far behind.
- Serve behind HTTP/2 (Caddy) so SSE connections don't eat the browser's HTTP/1.1 per-domain connection limit.

## 5. World & Simulation Model — Simultaneous Turns (WeGo)

The core of the design. Every 5 seconds, one world turn:

```
|<---------- 3 s input window ---------->|<-- resolve -->|<------ ~2 s playback ------>|
 players choose/queue actions              server computes  clients animate the outcome
 (move, attack, interact, wait)            (microseconds)   everyone sees the same turn
```

1. **Input window (3 s):** each client sends at most one intent for its player. Queued click-to-move paths auto-submit the next step, so walking costs zero attention. No input = wait (or continue queued path).
2. **Resolution (instant):** the server applies all intents simultaneously under deterministic conflict rules, runs AI/NPC intents the same way, and produces a turn result: everything that happened this turn.
3. **Playback (~2 s):** clients animate the turn result — moves slide, attacks land, deaths resolve — then the next input window opens. The 2 s is presentation time, not computation time.

**Clock policy — DECIDED: local combat time bubbles.**
- Out of danger, world turns auto-advance on the 5 s cadence above.
- When a player and a hostile gain **mutual line of sight within `COMBAT_RADIUS` hexes** (**6** for now, config constant), a **local time bubble** forms around them: entities in the bubble stop auto-advancing, and their turns become **action-gated** — the bubble's turn resolves when every player in it has committed an intent, with a fallback timeout (~60 s) that auto-waits AFK players. NPCs commit instantly.
- **The rest of the world keeps its 5 s heartbeat — deliberately, so friends can keep moving and walk into the fight to help.** Crossing into a bubble's radius (or entering its LOS) pulls you into its time domain: walking in *is* joining the fight, no enrollment mechanic needed.
- **Cross-domain interactions: interaction absorbs.** Any attempt to interact with a bubble from outside — attacking into it, targeting an entity inside, healing a combatant — first pulls the actor into the bubble's time domain; the intent then resolves as a normal bubble turn. Nobody acts on a frozen fight from world-clock speed.
- **Same turn everywhere.** Intents, resolution, conflict rules, and playback are identical inside and outside bubbles; only the metronome differs. There is no combat screen and no separate ruleset — the server just runs one turn loop per time domain.
- **Bubble lifecycle:** form on trigger; **merge** when two bubbles come within trigger range or share an entity; **dissolve** when no player↔hostile pair remains in mutual LOS within the radius. Fleeing beyond the radius or breaking LOS is therefore a real, legible escape mechanic.
- **Trigger on awareness (mutual LOS), not raw distance** — a distance-only trigger turns the stopped clock into a monster radar that leaks hidden enemies through walls.
- **Known looseness (accepted for co-op):** entities just outside a bubble act at world speed while the fight deliberates — that asymmetry is exactly the "jump in to help" feature. If edge-loitering ever gets cheesy in playtests, widen the absorption rule (e.g. anyone in LOS of the bubble gets absorbed).

**Grid & movement — DECIDED:**
- **Flat-top hex grid, grid-locked movement,** axial coordinates. Each tile has N/S/NE/NW/SE/SW neighbors — six equidistant directions, no diagonal problem. Red Blob Games' hex guide is the reference for coordinates, distance, pathfinding, and line-of-sight.
- **Primary input: click-to-move.** Click a hex → client sends the target → server pathfinds and the entity walks the path over subsequent turns (re-validated each turn; interrupted when hostiles appear).
- **Keyboard: QWE / ASD** mapping onto the six hex directions — two rows, no
  modifier states. There is no wait key: standing still is simply not sending
  an intent.
  ```
   Q   W   E        Q=NW  W=N  E=NE
   A   S   D        A=SW  S=S  D=SE
  ```
- Show this layout in-game (onboarding overlay/help screen) so the group doesn't need explaining.
- **Travel pace mitigation:** consider allowing movement of **multiple hexes per turn when no hostiles are in sight** (e.g. up to 3), dropping to 1 hex when enemies are near. Exploration stays brisk; proximity to danger — not a mode switch — is what makes things tactical.

**Hex occupancy & stacking — DECIDED:**
- Hex capacity is a config constant (`STACK_CAP`), **set to 5 — a full party fits on one hex.** Friendly stacking is on: allies share hexes, so a party can move as one blob (and share a click-to-move destination). The cap doubles as a soft mechanical nudge toward the ~3-parties-of-5 split — 15 players *can't* pile onto one hex.
- **Hit distribution** when attacking a hex with multiple occupants: a **random member** takes the hit. (Future lever: AoE enemies that hit the whole stack, making stacking a tactical choice rather than always-correct.)
- **Stack rendering:** top entity's sprite plus a **count badge** (e.g. "×3").
- Conflicts now only occur on **overflow**: moves into a hex resolve until it's full (5), and excess movers fall under the conflict-resolution rules below. Hostiles never share a hex — moving onto an enemy-held hex is an attack (bump-to-attack), not an entry.

**Combat:**
- There is **no combat screen and no separate ruleset**. Combat happens on the same map with the same intents — an "attack X" intent resolves alongside everyone's moves. The only thing that changes near hostiles is the clock policy (time bubbles above).
- **Bump-to-attack:** moving onto a hostile-occupied hex resolves as an attack — the attacker stays on their own hex and the move intent becomes an attack intent. The classic roguelike melee idiom, no special casing in the turn loop.
- Group tactics emerge from the shared turn: allies coordinate in chat during the input window, then everything lands at once.
- **XP — DECIDED: you get XP when you are in the bubble, per kill as it happens.** Each enemy death immediately grants the same, full XP amount to **every player inside the time bubble at that moment** — presence is the only criterion, and there is **no battle-end payout**. Join mid-fight and you earn from the next kill onward; leave early and you keep what you earned while present. No damage-based split, no kill credit, no participation requirement — walking in to help literally pays. (Human's +XP% applies to each award; species is the only differentiator.)

**Conflict resolution — DECIDED: phased. All moves resolve first, then all attacks.**
- **Move phase:** all move intents resolve simultaneously. If more entities target a hex than it can hold (`STACK_CAP`, or a hostile-held hex), the overflow is settled by a **deterministic tie-break** (server-seeded RNG for the turn — reproducible, no favoritism); losers stay put.
- **Bump-to-attack sequencing:** a move onto a hostile-held hex checks the board *after* the move phase — if the hostile is still there, the move converts into an attack (resolved in the attack phase, mover stays put); if the hostile vacated, the move simply completes into the empty hex.
- **Attack phase:** all attacks resolve simultaneously against **post-move positions**. Consequences, embraced as features:
  - Moving away from a melee attacker means the attack finds nothing — **retreating genuinely dodges**, which makes flight a real tactic and pairs with bubble-escape.
  - Two entities attacking each other can **both die on the same turn** — mutual kills are allowed and dramatic; the turn playback should sell these moments.
- No initiative stat, no per-entity ordering — the phase structure plus one seeded tie-break is the entire rule set. Write it once in `internal/game`, property-test it hard (it's pure logic, ideal territory).

## 6. Rendering, UI & the "Filter" Look

- **Hex map & entities: PixiJS** on a WebGL canvas — sprites on a flat-top hex layout, coordinates straight from Red Blob's formulas. Simple tile spritesheets.
- **Playback animation is a first-class client system:** the turn result is a script the client performs over ~2 s (tween moves, flash attacks, fade deaths). Get this right early — it's the entire game feel.
- **Social UI is HTML/CSS over the canvas:** chat panel, quest log, party list, and the **turn timer** as ordinary DOM — visible from day one, testable as structured elements.
- **The turn timer has two states:** a countdown bar while the world auto-advances, and a "waiting for: Piet, Anna…" list inside a time bubble — gentle social pressure instead of a hard clock. Fights frozen in other time domains are visible to passersby (with an "in combat" marker), which is the invitation to walk over and help.
- **Post-processing filter** (CRT scanlines, desaturation/bloom à la Caves of Qud): a **PixiJS filter (WebGL fragment shader)** over the whole stage. Keep it a separate, swappable pass to experiment with looks (scanlines vs. bloom vs. flat retro palette) without touching game logic.
- **Testability is a design rule:** the client exposes its state for Playwright (e.g. `window.game` with position/turn/entities getters). Every feature lands with an e2e test that drives the real browser against a real server.

## 7. Deployment & Operations

- **One binary, one process:** Go server with the built client embedded (`go:embed`). Deploy to any small VPS; put **Caddy** in front for TLS + HTTP/2.
- CI: typecheck + unit tests (Go and TS) + protocol contract test + Playwright e2e against a real server build.
- Dev loop: `make dev` runs the Go server and Vite dev-server side by side (Vite proxies `/api` + SSE to Go) for hot reload.
- Since you're on Silverblue day-to-day, a `toolbox`/`distrobox` container with Go + Node covers the toolchain without polluting the immutable base image.
- Persistence can start as "in memory + periodic JSON snapshot to disk" — 15 players don't need a database on day one.

## 8. Suggested Milestones

1. **Skeleton:** Go server serving an embedded Vite-built page, bootstrapped from the topbanana chassis (Makefile, golangci, app bootstrap, server layer, SSE hub, CI); client opens an SSE stream and receives heartbeat events; `make dev` loop works; tygo generation + contract test wired into CI.
2. **Static hex world render:** client renders a hardcoded hex map from a server-sent bundle. No movement yet.
3. **The turn loop:** server runs the 5 s cadence (input window → resolve → broadcast); one client POSTs a move intent and sees its entity step on the next turn. **This is the heart of the game — everything after builds on it.**
4. **Playback & feel:** turn results animate over the ~2 s playback window; visible turn timer; click-to-move with queued paths; QWE/ASD keys. First Playwright e2e: click a hex, assert the entity arrives.
5. **Multiplayer:** two+ clients connected, intents resolving simultaneously, both watching the same playback; reconnect/resync via `Last-Event-ID` proven with a pulled-plug test. First test of conflict-resolution rules.
6. **Combat, time bubbles & death:** bump-to-attack, deterministic resolution order. Local clock-stop bubbles: form on mutual LOS, action-gated turns with timeout, join-by-walking-in, merge, dissolve on flee. Death → respawn with fall-back-to-level-start XP penalty.
6b. **Classes, species & progression:** rogue (dagger/bow by distance; high damage, squishy), fighter (melee only; medium damage, tank), mage (magic only; AoE, squishy); species passives (human +XP, elf +crit, dwarf +DR); ranged + AoE attack intents; XP from kills/quests; level-ups; first gear drops with weapon/magic variety.
7. **Procedural generation:** server generates the world procedurally rather than a fixed map.
8. **Quests, parties & chat:** player quests and party quests, quest invites ("join my quest"), party membership, quest log UI, in-game chat panel (chat is core to the input-window social loop).
9. **Shader filter pass:** the WebGL post-processing filter for the retro look.
10. **Polish & launch:** deploy to the VPS, send the URL to the group, playtest with everyone online.

## 9. Open Decisions to Settle Early

- [x] ~~Language/stack~~ → **Decided: Go server + TypeScript/PixiJS browser client** (see §1; the "learn Rust" goal was retired when the design stopped needing Rust)
- [x] ~~Transport~~ → **Decided: SSE down + HTTP POST up, JSON payloads** (see §4)
- [x] ~~Grid-locked movement vs. free positioning~~ → **Decided: flat-top hex grid, grid-locked; click-to-move primary, QWE/ASD keyboard (no wait key)** (see §5)
- [x] ~~Tick rate~~ → **Decided: simultaneous 5 s turns (3 s input / ~2 s playback).** Keep the cadence a server config constant — feel-test 4 s vs 6 s with the group.
- [x] ~~Snapshot vs. delta-based state replication~~ → **Mooted by the turn model:** one turn-result bundle per turn; replay buffer + full-resync endpoint for reconnects.
- [x] ~~Stacking~~ → **Decided: `STACK_CAP = 5` (a full party fits on one hex); random-member hit distribution; count-badge rendering** (see §5)
- [x] ~~Combat pacing~~ → **Decided: local combat time bubbles** — clock stops locally on mutual LOS within `COMBAT_RADIUS = 6`; action-gated turns; the surrounding world keeps ticking so friends can walk in and help (see §5)
- [x] ~~Conflict-resolution rules~~ → **Decided: phased — all moves, then all attacks; seeded-RNG tie-break on hex overflow; attacks resolve against post-move positions** (see §5)
- [x] ~~Cross-domain interactions~~ → **Decided: interaction absorbs** — any attempt to act on a bubble from outside pulls the actor into its time domain first (see §5)
- [ ] Bubble tuning: commit-timeout length (~60 s?), and whether edge-loitering needs a wider absorption rule
- [ ] Multi-hex movement out of danger — **likely mooted by time bubbles** (safe travel auto-advances and costs no attention); revisit only if travel still feels slow in playtests
- [x] ~~Quest structure~~ → **Decided: two types — player quests (personal, self-picked) and party quests (shared; players can invite others to join). Parties self-organize around party quests** (see §0)
- [ ] Party-quest membership rules: does a late joiner get full progress/rewards, what happens to the quest when members leave or die, and can one player run multiple quests at once?
- [x] ~~Permadeath vs. softened death~~ → **Decided: no permadeath. XP and gear are earned; on death you fall back to the start of your current XP level** (levels and character survive; in-level progress is lost)
- [x] ~~Classes~~ → **Decided: rogue (dagger/bow by distance; high damage, squishy), fighter (melee; medium damage, tank), mage (magic; AoE, squishy); variety within each weapon/magic lane** (see §0)
- [ ] What happens to a character when its player goes offline (despawn, safe-log, or vulnerable)
- [ ] Death details: where do you respawn, and does gear survive death, drop on the spot (corpse run), or something in between?
- [ ] Ranged combat rules: bow/magic range in hexes, line-of-sight requirements, and friendly fire into occupied stacks (random-member rule applies?)
- [x] ~~Species~~ → **Decided: human (+% XP gain), elf (+% crit chance), dwarf (% damage reduction); percentages are config constants for playtest balancing** (see §0)
- [x] ~~XP distribution~~ → **Decided: per kill, at the moment it happens — every player in the time bubble gets the same full amount; no damage split, no kill credit, no battle-end payout** (see §5)
- [ ] Weapon/magic variety design: which weapon types (speed/damage/reach) and magic types (damage, control, support) exist at launch
- [ ] Combat math: base damage formula, crit multiplier (elf's bonus implies crits exist for everyone), how damage reduction applies (flat % is simplest), AoE templates (blast/line/cone) and whether player AoE hits allies
- [ ] Does the world tick when nobody (or almost nobody) is online, or only during sessions?
- [ ] Auth/identity: how players claim their character (name + secret link is probably enough for 15 friends)

---

*This plan is a starting scaffold — expect it to evolve once the turn loop is running and you can feel where the real design friction is.*
