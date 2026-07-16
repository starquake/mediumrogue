# mediumrogue — game design & rationale

**Genre target:** Shared-world roguelike with **simultaneous turns** (WeGo) — procgen, tile-based; death stings (XP setback) but is not permanent
**Simulation model:** One world turn every **4 seconds** (2 s input → instant resolution → ~2 s playback); near hostiles the clock **stops locally** and turns become action-gated (combat time bubbles)
**Visual style:** Hex-tile-based, Caves of Qud–inspired mood, with a post-processing filter
**Stack:** Go server (single binary) + TypeScript/PixiJS browser client
**Transport:** HTTP POST (intents up) + SSE (turn bundles & chat down)
**Distribution:** A URL in the group chat — no installs, no builds per platform

---


## 0. Context & Game Concept (why this project exists)

The real goal: a fun game for our own group of **~15 people** — a kind of **mini-MMO** built with AI assistance doing the heavy lifting.

- **Graphics stay very simple and hex-tile-based** — deliberately, since graphics skills are limited. Aesthetic effort goes into the shader/filter pass, not art assets.
- **A walkable shared world with quests, in two types.** **Player quests** are personal: each player picks their own. **Party quests** are shared: a party works on them together, and players can invite others to join their quest. Parties form organically around the quest someone pitches in chat — no assigned teams.
- **The whole game runs on simultaneous 4-second turns.** Everyone chooses an action in the same 2-second window; the server resolves everything at once; everyone watches the outcome together. It plays like a board game night, not a twitch game.
- **The group may split into ~3 parties** that each tackle quests, rather than 15 people in one blob.
- **Three classes: rogue, fighter, mage.** Enough identity to make parties feel composed ("we need a mage for this") without ballooning the design. Each has a hard weapon identity:
  - **Rogue** — dagger *or* bow, chosen automatically by distance: adjacent target → dagger, distant target → bow. **High single-target damage, squishy.** The flexible mid-liner.
  - **Fighter** — melee weapons only. **Medium damage, tanky** — the one who can afford to stand in front and take hits.
  - **Mage** — magic only. **AoE damage, squishy.** The back line, hitting areas instead of single targets.
  - Depth comes from **variety within each lane**: different weapon types for rogue/fighter (speed vs damage vs reach), different magic types for mage (damage, control, support) — that's what gear drops and progression feed into.
  - **Decided FUTURE direction (playtest feedback batch 2, 2026-07-11) — per-class weapon-slot semantics, own slice, not this batch:** fighter's melee lane gains a **THROWN** option alongside pure melee (thrown range strictly **< rogue's bow range**; designed so it can hit future **flying** enemies melee can't reach). Mage's two slots become **WAND + STAFF** (both ranged-casting — the staff may *also* melee-bonk in a pinch, the wand never; **no dual staff/wand**) — designed for a future wand↔staff interplay (e.g. a staff's sustained cast vs a wand's quick snap). Rogue's dagger/bow-by-distance identity is **unchanged**. Not implemented this batch — recorded here so item 7's entity-targeted ranged work and any future weapon-slot rework build toward the same target shape.
  - **LANDED (inventory system, 2026-07-12): the class-shaped weapon slots above are now the real storage model, and gear became a full 12-type taxonomy.** Every item has a `type` — `melee-weapon, thrown-weapon, ranged-weapon, staff, wand, consumable, head, body, hands, ring, amulet, feet` — and the equip **slot is derived from the type** (consumables have none; they stack in the backpack). A character has **8 equip slots** (six universal body slots + the two class-shaped weapon slots — fighter melee+thrown, rogue melee+ranged, mage staff+wand, exactly as decided above; the fighter's thrown slot ships **empty** pending thrown content, so a fighter still has no ranged attack) plus a **4-entry backpack** (gear or a consumable stack ≤5). **Wearability moved to the ITEM** (a weapon names its classes; armor/jewelry default to "any", may list several — Leather Armor is fighter-or-rogue) while **characters stay strictly single-class**. Actions: equip/unequip/drop/**pickup** (auto-pickup replaced by an explicit intent + a per-hex client modal; server gates merge→free-entry→reject) /**drink** — all free outside a bubble, your whole turn inside. Snapshot bumped to **v3**. See `docs/FEATURES.md` (the inventory-slots spec lives in git history).
  - **SUPERSEDED (gear keystone #55/#56, 2026-07-13; throwables scrapped 2026-07-14):** the class-shaped weapon slots above are gone. A weapon now carries a *set* of **tags** (`melee`/`ranged`/`magic`) + a `twoHanded` flag and equips into **generic `main-hand`/`off-hand` slots** — any class equips any weapon (the `wearableBy`/class gates are deleted; class identity moves to future skills). The `thrown-weapon`, `staff`, and `wand` item-types were collapsed into one `weapon` type. The fighter's **THROWN** future-direction (the bullet above) is **scrapped** — throwing isn't a staple ARPG mechanic and no thrown content will ship (Q1 in `docs/design-decisions.md`, which takes G4/G5 with it). Snapshot bumped to **v4**. See `docs/FEATURES.md` (Gear & inventory) and `docs/content-authoring.md` §4.
- **Three species: human, elf, dwarf** — one passive bonus each, freely combinable with class (9 combos):
  - **Human** — +% XP gain (levels faster)
  - **Elf** — +% critical-hit chance
  - **Dwarf** — flat damage reduction (−1 per hit, floor 1)
  - The percentages are config constants, balanced in playtests. Species choose a *style* (grow fast / spike hard / survive), class chooses a *job*.
- **Progression: XP levels and gear are earned through play.** Death is not permadeath, but it stings: **you fall back to the start of your current XP level** (levels and your character survive; in-level progress does not).
  - **Decided (2026-07-10): progression is horizontal — the toolbox, not the number.** Per-level stat gains stay deliberately shallow (a level-40 character has roughly *twice* a level-4 character's HP, not ten times — retune `HPPerLevel`/`DamagePerLevel` accordingly); real power growth is the **toolbox** found through play: gear and (later) abilities as pipeline rule cards. The emblem case: the lava monster that nearly killed your level-4 party falls easily at level 40 *because you found an ice sword along the way*, not because your numbers inflated. Rewards tactical choice over stat growth, and keeps difficulty zones permanently meaningful — soft-gated by tools, not hard-gated by level. See the scaling-options correspondence doc (2026-07-10).
  - **Decided (2026-07-10, structure superseded 2026-07-13 — see below): the level-up reward is a SKILL choice.** Skills are the third arm of the toolbox — gear you *find*, skills you *choose* — and they are **never leveled by use** — grind-by-repetition is vertical progression through the back door and rewards tedium over play. Active skill use follows the established action grammar: your whole turn inside a combat bubble, quick/free outside. *Structure amendment (2026-07-13, roadmap Q4/Q10 + #60/#61):* each level-up grants **one bankable skill point** (spent anytime outside combat — no modal pick that could land mid-bubble), spent in **three trees — Class / Adventure / Survival** (#61's principles govern). The original "class-agnostic life skills in bounded tiers I/II/III" become the **Adventure/Survival trees** (class-agnostic, as before); the **Class tree** is class-specific — it's how #56's "class identity via skills, not gear gates" is delivered. The two founding skills (designed by the group's content designer):
    - **First Aid** — *explicitly non-magical* healing: bandages, not spells (the mage's future support-magic lane stays a distinct class identity — in-combat, ally-targeted, magic; First Aid is the humble anyone-can-learn version). Consumes a cheap **bandage item** (toolbox stays the fuel; the merchant gets something to sell); the skill tier sets how much each bandage heals, higher tiers allow bandaging adjacent allies and — at the top tier — **reviving a downed player** (see the downed-state entry in §9).
    - **Make Camp** — creates a temporary **camp** in the field (a non-hostile world object, same seam as chests/merchants): a portable slice of the long-rest "home effect", strength by tier — I: faster rest regen nearby; II: the full long-rest effect; III: adds a timed **"well-rested" buff** (the modifier pipeline's first buff card; needs the duration system, a known tier-3 item). Decentralizes the sanctuary, makes deep-ring expeditions viable, and creates a real non-combat party role — camping in the dragon ring is a *decision*.

**Design implications to keep in mind:**
- ~15 concurrent players is the *actual* scale target — tiny by MMO standards. Don't over-engineer for scale; optimize for fun and for sessions where most of the group is online at once.
- The shared turn cadence is the great equalizer: mixed reflexes and skill levels don't matter, network latency is irrelevant (200 ms ping vs a 2000 ms window), and there's natural room to chat between turns.
- Quest/party systems are first-class features, not stretch goals: shared quest state, party membership, and proximity logic all need protocol support.
- AFK handling falls out of the model naturally: no input = "wait" (or continue a queued path). No turn ever blocks on a missing player.

## 1. Why Go + a Browser Client

The stack decision evolved (Rust/Bevy → Go/Ebitengine → this) as the design got clearer. The reasons that settled it:

- **The turn model removed every performance argument.** The server resolves ~15 players' intents once per 4 seconds — any language handles that. The original "no GC pauses, real-time entity ticking" rationale for Rust no longer applies.
- **Distribution is the real constraint** for a casual 15-friend group across Windows/macOS/Linux. A browser client turns distribution into *a URL*: no installers, no code-signing, no "which download do I click," and every update reaches everyone instantly.
- **AI-verifiability drove the client choice.** A TypeScript/PixiJS client can be driven end-to-end with Playwright: click hexes, press keys, then *query live game state directly* and read console errors — far stronger verification than screenshot-squinting at an opaque canvas (the Ebitengine/WASM weakness). Since AI does the heavy lifting here, a testable client is a faster, safer project.
- **HTML/CSS for the social surface.** Chat, quest log, party UI, and the turn timer — the parts this game actually lives on — are ordinary DOM elements floating over the canvas, not widgets hand-drawn in a game engine.
- **Go is familiar territory**, cross-compilation worries vanish (only the server needs building), and goroutines + `net/http` cover everything the server does.

## 2. High-Level Architecture

```
+----------------------+        HTTP POST /intent        +----------------------+
|  Browser client      |  ---------------------------->  |  Go server           |
|  - PixiJS hex render |                                 |  - Turn loop (4 s)   |
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
mediumrogue/
├── go.mod                   # module github.com/starquake/mediumrogue
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
- **Server code:** `cmd/server/app` bootstrap (main → app.Run, graceful shutdown, background-task draining, `-check` smoke flag); the `internal/server` layer (per-surface `addXRoutes` split, middleware, security headers; a same-origin/CSRF check on unsafe methods is still open — see STATUS's debt list); the **SSE hub + heartbeat pattern** from `leaderboard`/`livesession` (coalescing pub/sub, buffered-1 tick channels, heartbeat interval threaded in for fast tests) as the backbone of the turn-bundle stream; `internal/config` env-based config and the bounded-body JSON decode/respond helpers.
- **Frontend:** keep **Vite + TS + PixiJS** (typed protocol, Playwright-testable) — topbanana's esbuild/vanilla-JS approach is *not* adopted, but its `go:embed` assets/serving pattern is reused for the built bundle.
- **Testing/infra from day one:** the integration-test harness style (real server in-process, real HTTP/SSE), the Playwright e2e scaffold, the coverage gate (`.testcoverage.yml`), `distrobox.ini.example`, Dockerfile/docker-compose templates.
- **Not adopted:** SQLite/sqlc/goose (plan stays in-memory + JSON snapshots to start; SQLite is the planned *later* store for runtime state — see the persistence upgrade path in §7) and the full auth stack (passwords, email verification, CSRF forms) — identity here is name + secret link; topbanana's session-cookie manager can be borrowed later if needed.

**Protocol drift defense (two languages, one wire):**
- The Go `protocol` package is the single source of truth; **`tygo` generates the TypeScript types** in the build. Hand-editing `protocol.gen.ts` is forbidden.
- A **contract test** replays recorded turn bundles through both the Go encoder and the TS decoder in CI.
- Turn cadence constants (`TURN_SECONDS`, input/playback split) live in the protocol package and are exported to the client the same way.

## 4. Transport: SSE + POST (decided)

The wire carries one small intent up per ≤4 s and one turn bundle down per 4 s — not a realtime workload. That makes **Server-Sent Events + plain HTTP POST** the best fit:

| | Why it wins here |
|---|---|
| **Reconnection built in** | `EventSource` auto-reconnects and sends `Last-Event-ID`; the server replays missed turn bundles from that ID. Reconnect/resync — the hardest part of a persistent game — rides on a browser primitive. |
| **Debuggable with curl** | The turn stream can be watched in a terminal and intents POSTed by hand — ideal for AI-driven verification and for humans at 1 a.m. |
| **Just HTTP** | No upgrade handshake, proxy-friendly, trivial in Go (`http.Flusher`) and in the browser. |

- Considered and set aside: **WebSocket** (works, but buys nothing at this cadence and costs hand-rolled reconnect/heartbeat — the fallback if genuinely chatty bidirectional traffic ever appears), **gRPC** (browsers can't speak it natively; needs a proxy), **Connect-RPC** (respectable schema-first alternative; more build tooling than this project warrants).
- Messages are **plain JSON** — human-readable on the wire, matching the debuggability theme.
- Turn bundles get monotonically increasing event IDs = turn numbers; the server keeps a short replay buffer for reconnecting clients, and a full-state resync endpoint covers clients that fall too far behind.
- Serve behind HTTP/2 (Caddy) so SSE connections don't eat the browser's HTTP/1.1 per-domain connection limit.

**Milestone 5 update:** with full-snapshot turn bundles and a coalescing hub, a reconnecting client only needs the current snapshot, so the replay buffer and separate resync endpoint above were **not** built — reconnect is resync-to-latest, and `Last-Event-ID` is honoured only as a watermark to avoid re-painting an already-seen turn.

## 5. World & Simulation Model — Simultaneous Turns (WeGo)

The core of the design. Every 4 seconds, one world turn:

```
|<------ 2 s input window ------>|<-- resolve -->|<------ ~2 s playback ------>|
 players choose/queue actions      server computes  clients animate the outcome
 (move, attack, interact, wait)    (microseconds)   everyone sees the same turn
```

1. **Input window (2 s):** each client sends at most one intent for its player. Queued click-to-move paths auto-submit the next step, so walking costs zero attention. No input = wait (or continue queued path). **(Playtest feedback batch 3, item 1: lowered from 3 s — playtest 2026-07-11 found 3 s felt slow.)**
2. **Resolution (instant):** the server applies all intents simultaneously under deterministic conflict rules, runs AI/NPC intents the same way, and produces a turn result: everything that happened this turn.
3. **Playback (~2 s):** clients animate the turn result — moves slide, attacks land, deaths resolve — then the next input window opens. The 2 s is presentation time, not computation time.

**Clock policy — DECIDED: local combat time bubbles.**
- Out of danger, world turns auto-advance on the 4 s cadence above.
- When a player and a hostile gain **mutual line of sight within `COMBAT_RADIUS` hexes** (**6** for now, config constant), a **local time bubble** forms around them: entities in the bubble stop auto-advancing, and their turns become **action-gated** — the bubble's turn resolves when every player in it has committed an intent, with a fallback timeout (**30 s, decided post-playtest — item 4, playtest feedback batch 2; was ~60 s**) that auto-waits AFK players. NPCs commit instantly. **Turn floor (item 5, same batch):** a bubble-turn never resolves sooner than `TURN_INTERVAL` after its own previous resolution, even with every player locked in — closes a solo-player action-spam exploit (resolving faster than the world's own cadence) without slowing a genuine multi-player fight, where lock-ins rarely land inside one interval anyway.
- **The rest of the world keeps its 4 s heartbeat — deliberately, so friends can keep moving and walk into the fight to help.** Crossing into a bubble's radius (or entering its LOS) pulls you into its time domain: walking in *is* joining the fight, no enrollment mechanic needed.
- **Cross-domain interactions: interaction absorbs.** Any attempt to interact with a bubble from outside — attacking into it, targeting an entity inside, healing a combatant — first pulls the actor into the bubble's time domain; the intent then resolves as a normal bubble turn. Nobody acts on a frozen fight from world-clock speed.
- **Same turn everywhere.** Intents, resolution, conflict rules, and playback are identical inside and outside bubbles; only the metronome differs. There is no combat screen and no separate ruleset — the server just runs one turn loop per time domain.
- **Bubble lifecycle:** form on trigger; **merge** when two bubbles come within trigger range or share an entity; **dissolve** when no player↔hostile pair remains in mutual LOS within the radius. Fleeing beyond the radius or breaking LOS is therefore a real, legible escape mechanic.
- **Trigger on awareness (mutual LOS), not raw distance** — a distance-only trigger turns the stopped clock into a monster radar that leaks hidden enemies through walls. *(Implementation status: bubbles currently trigger on pure distance ≤ `COMBAT_RADIUS`; terrain-blocked LOS spotting is the planned target — see #95.)*
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
- Conflicts now only occur on **overflow**: moves into a hex resolve until it's full (5), and excess movers fall under the conflict-resolution rules below. Hostiles never share a hex — moving onto an enemy-held hex is an attack, not an entry.

**Combat:**
- There is **no combat screen and no separate ruleset**. Combat happens on the same map with the same intents — an "attack X" intent resolves alongside everyone's moves. The only thing that changes near hostiles is the clock policy (time bubbles above).
- **Melee:** walking into a hostile-occupied hex resolves as an attack — the attacker stays on their own hex and the move intent becomes an attack intent (the classic roguelike bump-to-attack), no special casing in the turn loop. **This conversion is monster-only** (decided 2026-07-15, #116): a player's melee is instead an entity-targeted attack intent sent directly by clicking (or key-stepping into) an adjacent enemy — the player keeps the bump *feel* via that click/key-step routing, but attacking never moves the player, and a queued walk stops adjacent to a hostile-held destination rather than converting.
- Group tactics emerge from the shared turn: allies coordinate in chat during the input window, then everything lands at once.
- **XP — DECIDED: you get XP when you are in the bubble, per kill as it happens.** Each enemy death immediately grants the same, full XP amount to **every player inside the time bubble at that moment** — presence is the only criterion, and there is **no battle-end payout**. Join mid-fight and you earn from the next kill onward; leave early and you keep what you earned while present. No damage-based split, no kill credit, no participation requirement — walking in to help literally pays. (Human's +XP% applies to each award; species is the only differentiator.)

**Conflict resolution — DECIDED: phased. All attacks resolve first, then all moves.** *(Amended 2026-07-15, #104 — the original decision was moves-then-attacks; the flip and its reasoning live in #104 and the PR that landed it.)*
- **Attack phase:** all attacks resolve simultaneously against **pre-move positions**. Consequences, embraced as features:
  - Committing to an attack **always lands it** — a target can no longer step out of a swing on the same tick, so an attack turn is never wasted (the old retreat-dodge read as whiffing, worst for the mage's ground-targeted AoE).
  - Two entities attacking each other can **both die on the same turn** — mutual kills are allowed and dramatic; the turn playback should sell these moments.
  - Retreat still works, as **trading hits for distance**: one action per turn means a chaser that stops to strike you isn't gaining ground that turn — flight stays a real tactic and pairs with bubble-escape, it just costs HP instead of being a free dodge.
- **Melee sequencing (monster movers — #116):** a MONSTER move intent whose next step is a hostile-held hex converts into an attack against that occupant (resolved in the attack phase, mover stays put). No post-move recheck — the target's pre-move position is what's hit, so the melee attack cannot fizzle by vacation. A player's melee never reaches this phase — it arrives as an entity-targeted attack intent (see the Melee bullet above); a player move onto a hostile-held hex simply blocks.
- **Move phase:** all move intents resolve simultaneously. If more entities target a hex than it can hold (`STACK_CAP`, or a hostile-held hex), the overflow is settled by a **deterministic tie-break** (server-seeded RNG for the turn — reproducible, no favoritism); losers stay put. An entity killed in the attack phase does not get its move.
- No initiative stat, no per-entity ordering — the phase structure plus one seeded tie-break is the entire rule set. Write it once in `internal/game`, property-test it hard (it's pure logic, ideal territory).

## 6. Rendering, UI & the "Filter" Look

- **Hex map & entities: PixiJS** on a WebGL canvas — sprites on a flat-top hex layout, coordinates straight from Red Blob's formulas. Simple tile spritesheets.
- **Playback animation is a first-class client system:** the turn result is a script the client performs over ~2 s (tween moves, flash attacks, fade deaths). Get this right early — it's the entire game feel.
- **Social UI is HTML/CSS over the canvas:** chat panel, quest log, party list, and the **turn timer** as ordinary DOM — visible from day one, testable as structured elements.
- **The turn timer has two states:** a countdown bar while the world auto-advances, and a "waiting for: Piet, Anna…" list inside a time bubble — gentle social pressure instead of a hard clock. Fights frozen in other time domains are visible to passersby (with an "in combat" marker), which is the invitation to walk over and help.
- **Selected-path preview (planned):** render **my own** chosen route on the map — the **destination/goal** marker plus **every hex along the path** the player will walk to reach it (a highlighted breadcrumb trail). This is **local-only**: a player sees just *their* path, never other players' or enemies' routes. The server already pathfinds click-to-move, so the client has (or can be given) the ordered hex list to draw; render it under the entities layer and clear it as the player advances / on arrival / on re-route. Fits naturally as a polish pass on the milestone-4 click-to-move-with-queued-paths work. (Do NOT broadcast other entities' paths — the "only me" scoping is the point.)
- **Post-processing filter** (CRT scanlines, desaturation/bloom à la Caves of Qud): a **PixiJS filter (WebGL fragment shader)** over the whole stage. Keep it a separate, swappable pass to experiment with looks (scanlines vs. bloom vs. flat retro palette) without touching game logic.
- **Testability is a design rule:** the client exposes its state for Playwright (e.g. `window.game` with position/turn/entities getters). Every feature lands with an e2e test that drives the real browser against a real server.

## 7. Deployment & Operations

- **One binary, one process:** Go server with the built client embedded (`go:embed`). Deploy to any small VPS; put **Caddy** in front for TLS + HTTP/2.
- CI: typecheck + unit tests (Go and TS) + protocol contract test + Playwright e2e against a real server build.
- Dev loop: `make dev` runs the Go server and Vite dev-server side by side (Vite proxies `/api` + SSE to Go) for hot reload.
- Since you're on Silverblue day-to-day, a `toolbox`/`distrobox` container with Go + Node covers the toolchain without polluting the immutable base image.
- **Persistence** starts as "in memory + periodic JSON snapshot to disk" — 15 players don't need a database on day one. **Implemented (milestone 10a, 2026-07-11):** `World.MarshalState`/`RestoreState` persist every entity (players and monsters), ground items, the quest board, the disconnect archive, and the turn/id counters, behind `SNAPSHOT_PATH` (default `""` = disabled) with a periodic saver (`SNAPSHOT_INTERVAL`, default 60s) and a final write on graceful shutdown; a version/seed/radius mismatch logs and starts fresh rather than migrating. The disconnect sweep now **archives** a player's identity/XP/gear instead of deleting it, and a rejoin with the same token restores it. See `docs/FEATURES.md`. **Upgrade path (decided 2026-07-10): SQLite for runtime *state*; the repo for *content*.**
  - **State → SQLite, later.** When persistence needs outgrow the JSON snapshot — character persistence across reconnects (the `character-persistence-reconnect` note), gear inventories once the gear slice lands, bed/home spawns, milestone-11 tuning overrides surviving restarts — move runtime state to SQLite: single file, no server process, transactional; borrow the sqlc/goose patterns from topbanana that §3 deliberately set aside for later. The milestone-12 analytics log may also land in SQLite *if* it needs ad-hoc querying; otherwise JSONL stays fine (and stays LLM-readable).
  - **Content → the repo, never the DB.** Gear/species/rule definitions (the "cards" of `content-authoring.md`) stay version-controlled with the code — Go data literals or an embedded JSON/YAML file. Two reasons: cards reference the engine's rule *vocabulary* (event names, effect verbs) and must version-lock with the binary (same philosophy as the version-locked client), and PR review of a card **is** the content-review workflow (diffs, history, atomic versioning). A definitions table in SQLite would add migrations and a DB↔binary sync problem while giving up all of that.
  - **Design prerequisite (build into the modifier pipeline from day one):** rule cards are **pure serializable data** — a kind plus parameters (e.g. `{event: take-damage, effect: subtract, amount: 2, floor: 1}`), turned into modifier functions by a small factory, never Go closures. Then definitions are data-addressable (persisted characters reference equipped gear by item ID) and the storage choice stays permanently open: literals, embedded files, or SQLite rows all feed the same factory.

## 8. Suggested Milestones

1. **Skeleton:** Go server serving an embedded Vite-built page, bootstrapped from the topbanana chassis (Makefile, golangci, app bootstrap, server layer, SSE hub, CI); client opens an SSE stream and receives heartbeat events; `make dev` loop works; tygo generation + contract test wired into CI.
2. **Static hex world render:** client renders a hardcoded hex map from a server-sent bundle. No movement yet.
3. **The turn loop:** server runs the world-turn cadence (input window → resolve → broadcast; 4 s since playtest batch 3, was 5 s when this milestone was built); one client POSTs a move intent and sees its entity step on the next turn. **This is the heart of the game — everything after builds on it.**
4. **Playback & feel:** turn results animate over the ~2 s playback window; visible turn timer; click-to-move with queued paths; QWE/ASD keys. First Playwright e2e: click a hex, assert the entity arrives.
5. **Multiplayer:** two+ clients connected, intents resolving simultaneously, both watching the same playback; reconnect/resync via `Last-Event-ID` proven with a pulled-plug test. First test of conflict-resolution rules.
6. **Combat, time bubbles & death:** melee attacks, deterministic resolution order. Local clock-stop bubbles: form on mutual LOS, action-gated turns with timeout, join-by-walking-in, merge, dissolve on flee. Death → respawn with fall-back-to-level-start XP penalty.
6b. **Classes, species & progression:** rogue (dagger/bow by distance; high damage, squishy), fighter (melee only; medium damage, tank), mage (magic only; AoE, squishy); species passives (human +XP, elf +crit, dwarf +DR); ranged + AoE attack intents; XP from kills/quests; level-ups; first gear drops with weapon/magic variety. (6b.4 landed — pipeline + gear/drops/equip; see spec)
    - **Planned architecture — a scalable combat modifier/rule system.** Species passives are implemented as simple per-trait helpers for now, but classes, species, **gear**, buffs, status effects, and abilities are all "modify a combat value at a combat event" (attack/crit, deal-damage, take-damage, earn-XP, on-kill). Build a **modifier/effect pipeline** — traits/gear/effects register modifiers that transform these values — **with the gear slice** (the second modifier source, where it earns its keep); migrate the species passives onto it as the first rule-set. Deferring it now is deliberate (YAGNI until gear defines the requirements); the current per-trait helpers are the seam. See the `combat-modifier-pipeline` note.
6c. **Monster kinds & difficulty rings:** **(landed)** — replaces the single anonymous monster with a registry of monster kinds (`internal/game/content.go`'s `monsterDefs`, exactly like items): rat/wolf/ghoul/troll/dragon, each with its own HP, damage, XP award, aggro radius, and its own weighted loot table (loot authority moved fully monster-side — item `dropWeight` and the global drop table/chance are retired). Placed by distance-based **difficulty rings** worldgen bands the map into (`RingCount=3`), weighted by ring area, with a monster-free **sanctuary** zone around the origin (`SanctuaryRadius=5`, the seed of the §9 recovery-layers trade hub) and a per-world dragon cap (`DragonCount=1`). Ships the **`targetKind`** pipeline condition and its proof, the **Wyrmslayer Greatsword** (×1.5 damage vs dragons specifically) — the first designer gear card that needed monster kinds to exist. Client: per-kind dot color + glyph (`entities.ts`'s `KIND_STYLE`); kill announces name the slain kind(s) ("a wolf was slain…", "a wolf and a troll were slain…"). wolf carries the pre-6c flat numbers forward unchanged.
7. **Procedural generation:** server generates the world procedurally rather than a fixed map. **(landed)** — seeded value-noise biomes (elevation + moisture) on a radius-24 world, rock rim, forced origin clearing, spawns restricted to the origin-connected walkable region; `WORLD_SEED`/`WORLD_RADIUS` knobs (fixed default seed → same world each restart); client camera follows the player. No wire change. Deferred: chunked/streamed world, rivers/roads/structures, terrain-blocked LOS.
8. **Quests, parties & chat:** player quests and party quests, quest invites ("join my quest"), party membership, quest log UI, in-game chat panel (chat is core to the input-window social loop) **(8.1 landed)** — global ephemeral chat over SSE (a dedicated non-coalescing broker, ephemeral/no-history by design), name-at-join, a `/`-command registry (`/here`), and the DOM social UI's first piece: a SolidJS `<ChatPanel>`. **(8.2 landed)** — party membership via chat commands (`/invite <name>`, `/accept`, `/leave`), `Entity.PartyID`, a SolidJS `<RosterPanel>`, and on-map partymate coloring; a party is ≥2 members, dissolves below that, persists across death, and is purged by the disconnect sweep. **(8.3 landed — milestone 8 complete)** — a seeded 6-quest board (kill + reach quests), `/quest <id>`/`/abandon`, one-slot rules *(since amended by §9 item 14: multiple concurrent personal quests, party join no longer abandons them, `/abandon <id>`)*, full-party payout on completion (human +XP% applies), a `SetAnnounce` chat hook, and a SolidJS `<QuestPanel>`. Chat history is still open — see §9. The DOM social UI (chat, quest log, party roster, floating over the Pixi canvas) is built with **SolidJS** (a small, CSP-safe, native-TSX reactive-DOM library; see the §9 UI-toolkit decision).
9. **Shader filter pass:** the WebGL post-processing filter for the retro look. **(dropped for now, 2026-07-10)** — a CRT pass (scanlines/vignette/desat) was built (PR #32) and rejected on looks; closed unmerged. May return later as a different look — when it does, **preview screenshots with the user before building**, and mind the strict-driver `highp` uniform-precision pitfall (see the `visual-features-need-preview` memory).
10. **Polish & launch:** deploy to the VPS, send the URL to the group, playtest with everyone online. **10a — persistence & identity (landed, 2026-07-11):** the launch gates — characters survive absence (disconnect sweep archives instead of deletes; token rejoin restores identity/XP/gear at a fresh spawn) and the world survives restarts (versioned JSON snapshot behind `SNAPSHOT_PATH`, default off). Identity is a copyable character link (`#t=<token>`), settling §9's auth/identity question. See `docs/FEATURES.md`. Deployment itself (VPS/Caddy) remains open.

**Tooling for tuning & balance (late / ongoing — build once the game is playable enough to tune, likely around or after launch):**

11. **Live admin / difficulty console:** an **admin-only** control surface to adjust game difficulty and parameters **while the game is running**, without a restart — so difficulty can be experimented with against real players and dialed in to "challenging but fair." Levers: monster count / strength / spawn rate, XP rates, combat constants (damage, radii, patience), world/encounter density, etc. Most game-rule numbers currently live as compile-time `internal/protocol` constants and boot-time `internal/config` env knobs; this milestone needs a **runtime-mutable override layer** (a live-tunable settings store the simulation reads each turn) exposed through an authenticated admin screen (a separate SolidJS page or a gated panel). Changes take effect on the next turn; ideally shows current values and lets you reset to defaults. Auth-gated (do **not** expose tuning to normal players).

12. **Combat & movement analytics log:** an append-only event log of combat and movement carrying enough context to **analyze difficulty and gear quality after the fact** — e.g. per event: turn, actors (id/name/class/species/level), action (move/attack/crit/kill/death), damage dealt/taken, weapon/gear used, HP before/after, bubble/encounter id, outcome. **Dual-audience by requirement:** it must be **human-readable** *and* **machine-parseable**, so both a person skimming it and an automated agent (an LLM) can parse it and form judgements about how hard the game is and how good the gear is. Do it the **idiomatic** way: structured Go logging via `slog` with a dedicated event category/attribute (or a dedicated append-only **JSONL** event stream) so it is **filterable** — normal server logs vs the analytics event stream separable by a key/level. Feeds directly into milestone 11's difficulty tuning (and future automated balance passes). Pairs with the `combat-modifier-pipeline` / gear work, which is where "gear quality" becomes a meaningful, loggable dimension.

## 9. Open Decisions to Settle Early

- [x] ~~Language/stack~~ → **Decided: Go server + TypeScript/PixiJS browser client** (see §1; the "learn Rust" goal was retired when the design stopped needing Rust)
- [x] ~~Transport~~ → **Decided: SSE down + HTTP POST up, JSON payloads** (see §4)
- [x] ~~Grid-locked movement vs. free positioning~~ → **Decided: flat-top hex grid, grid-locked; click-to-move primary, QWE/ASD keyboard (no wait key)** (see §5)
- [x] ~~Tick rate~~ → **Decided: simultaneous 4 s turns (2 s input / ~2 s playback).** Keep the cadence a server config constant. **Feel-tested (playtest feedback batch 3, item 1, 2026-07-11):** the original 5 s/3 s split (`TurnSeconds`/`InputWindowSeconds`) felt slow in play — lowered to 4 s/2 s; `PlaybackSeconds` unchanged at 2 s.
- [x] ~~Snapshot vs. delta-based state replication~~ → **Mooted by the turn model:** one turn-result bundle per turn.
- [x] ~~Reconnect/resync~~ → **Decided (milestone 5): resync-to-latest** — full-snapshot turn bundles + a coalescing hub mean a reconnecting client just needs the current snapshot; `Last-Event-ID` is honoured only as a watermark, no replay buffer or separate resync endpoint (see §4). **Extended (item 4, playtest feedback batch 3): a world-reset signal** — every turn bundle carries a `worldId` (random, minted at world creation, persisted in the snapshot so a restored world keeps its identity); a client seeing a different `worldId` mid-session reloads outright, so a restart-without-snapshot reads as a legible reset instead of a silently inconsistent reconnect.
- [x] ~~Stacking~~ → **Decided: `STACK_CAP = 5` (a full party fits on one hex); random-member hit distribution; count-badge rendering** (see §5)
- [x] ~~Combat pacing~~ → **Decided: local combat time bubbles** — clock stops locally on mutual LOS within `COMBAT_RADIUS = 6`; action-gated turns; the surrounding world keeps ticking so friends can walk in and help (see §5)
- [x] ~~Conflict-resolution rules~~ → **Decided: phased — all attacks resolve first, against pre-move positions, then all moves; seeded-RNG tie-break on hex overflow** (see §5; amended 2026-07-15, #104 — originally moves-then-attacks with attacks resolving against post-move positions)
- [x] ~~Cross-domain interactions~~ → **Decided: interaction absorbs** — any attempt to act on a bubble from outside pulls the actor into its time domain first (see §5)
- [x] ~~Bubble tuning: commit-timeout length~~ → **Decided (playtest feedback batch 2, item 4): 30 s** (`COMBAT_PATIENCE` default; was ~60 s — read as dead air waiting on a straggler). Whether edge-loitering needs a wider absorption rule is still open.
- [ ] Multi-hex movement out of danger — **likely mooted by time bubbles** (safe travel auto-advances and costs no attention); revisit only if travel still feels slow in playtests
- [x] ~~Quest structure~~ → **Decided: two types — player quests (personal, self-picked) and party quests (shared; players can invite others to join). Parties self-organize around party quests** (see §0)
- [x] ~~Party-quest membership rules: does a late joiner get full progress/rewards, what happens to the quest when members leave or die, and can one player run multiple quests at once?~~ → **Decided (8.3): full pay-at-completion** — a quest pays its full reward XP to *every current holder* the moment it completes, so a late joiner who's on the party when it completes gets the full reward (no partial-progress bookkeeping per member). ~~One quest slot per player/party (no running multiple at once); joining a party abandons a personal quest (auto-abandon on `/accept`)~~ **AMENDED (item 14, playtest feedback batch 2): a player may hold MULTIPLE personal quests concurrently, progressing and paying independently; a party still holds at most one quest at a time; joining a party no longer abandons personal quests** — they keep progressing alongside whatever the party itself takes. `/abandon` now takes an explicit `<id>` (no longer unambiguous). Unchanged: if a party **dissolves below 2 or the disconnect sweep removes a holder**, its quest returns to the board with progress reset (not kept for whoever's left) — and the disconnect sweep now returns *every* personal quest a swept player held, not just one.
- [x] ~~Permadeath vs. softened death~~ → **Decided: no permadeath. XP and gear are earned; on death you fall back to the start of your current XP level** (levels and character survive; in-level progress is lost)
- [x] ~~Classes~~ → **Decided: rogue (dagger/bow by distance; high damage, squishy), fighter (melee; medium damage, tank), mage (magic; AoE, squishy); variety within each weapon/magic lane** (see §0)
- [x] ~~What happens to a character when its player goes offline~~ → **Decided (issue #21): despawn after a grace.** A player's entity is removed from the live world once its event stream has been gone for `DISCONNECT_GRACE` (default 20s); a reconnect within the grace keeps it. So no vulnerable offline body stands around. Open follow-ups: **persist the character data** so a returning player gets their *old* character back (not a fresh one), and a **bed / home spawn** to return to — see the `character-persistence-reconnect` and `bed-home-spawn` notes.
- [x] ~~Does gear survive death?~~ → **Decided (2026-07-10): equipped gear ALWAYS survives death.** Under toolbox progression (§0) your gear *is* your power — dropping it on death would make dying deep a death spiral by construction (die → toolbox lies deep in danger → return weaker → die again). If corpse-run tension is ever wanted, at most *unequipped inventory* may drop — never the equipped toolbox. Where you respawn is still open (bed/home-spawn note; issue #36's random-spawn and camera-snap items).
- [x] ~~Scaling with a variable player count (people pop in and out)~~ → **Decided (2026-07-10), layered:** (1) **world density tracks online players** — a monsters-per-player spawn target once continuous spawning lands; (2) **spatial difficulty rings** as the difficulty story — monster kinds get stronger with distance from origin, players self-select by where they hunt (needs monster kinds; pairs with per-kind loot tables). Danger must be **legible**: distinct looks per kind, zone borders that announce themselves, no ambush-by-ignorance. (3) **Bubble-size scaling held in reserve** (HP-only, at formation, via a pipeline card conditioned on bubble player count) only if parties still trivialize their ring in playtests. **Rejected:** reward-side scaling (undermines XP-by-presence) and level-based monster scaling (rubber-banding kills felt progression; moot under the flat curve anyway). See the scaling-options correspondence doc (2026-07-10). **Monster kinds + difficulty rings + per-kind loot shipped in milestone 6c** (see §8); world-density-tracks-players and bubble-size scaling remain open, gated on continuous spawning.
- [x] ~~Ranged combat rules~~ → **Decided (6b.2):** bow & magic **range = 4 hexes**; **distance-only, no line-of-sight** requirement (terrain-blocked LOS deferred); **no friendly fire** (ranged hits opposing faction only) — a bow into a mixed stack hits a random *hostile* member (the 6.3 random-member rule); mage magic is **AoE** (radius 1) hitting all hostiles in the area. Ranged resolves inside a combat bubble, simultaneous with melee. **Amended (item 7, playtest feedback batch 2):** a single-target shot (bow) now targets the victim's **entity id**, not a hex — the AoE cast stays ground-targeted (hex) since its blast radius makes a hex the natural target. Resolution re-aims at the entity-targeted victim's post-move hex, hitting if it is still in range from the shooter's own post-move hex and fizzling otherwise — this is the retreat-dodge rule (§5) finally applied honestly to ranged, since a hex-pinned target used to let a sidestepping victim dodge a shot it should have still eaten, and let a fleeing one still eat a shot it should have dodged. **Amended again (2026-07-15, #104):** with attacks now resolving before moves, the post-move re-aim goes away — every attack (bump, bow, AoE) resolves against pre-move positions and always lands; entity-targeting remains as the aiming UX for single-target shots.
- [x] ~~Species~~ → **Decided: human (+% XP gain), elf (+% crit chance), dwarf (flat −1 damage reduction); the numbers are protocol constants for playtest balancing** (see §0)
- [x] ~~XP distribution~~ → **Decided: per kill, at the moment it happens — every player in the time bubble gets the same full amount; no damage split, no kill credit, no battle-end payout** (see §5)
- [ ] Weapon/magic variety design: which weapon types (speed/damage/reach) and magic types (damage, control, support) exist at launch
- [ ] Combat math — mostly decided since: damage reduction is **flat** (dwarf/Leather Armor `take-damage −N`, shipped); player AoE **never hits allies** (no friendly fire, shipped); the general crit model is **`crit%` ×2, decoupled ARPG** (#69, `design-decisions.md`). Still open: weapon-variety base-damage spread and AoE templates beyond blast (line/cone)
- [x] ~~HP/resource recovery~~ → **Direction decided (2026-07-10), layered** (today there is NO healing at all — death is literally the only way to refill HP, an inverted incentive to fix first):
  1. **Passive out-of-combat regen** (first slice, small): X HP per world turn while not in a bubble, tuned slow — kills the death-as-heal exploit without devaluing everything below. Knob.
  2. **Potions as consumable items** (drops → inventory → drink): drinking follows the equip rule — **free outside a bubble, your whole turn inside** — so spending a combat turn drinking instead of swinging is a real choice (never auto-drink; the choice IS the mechanic). UI offers two spends because "efficient" differs by context: **top-up** (waste-optimal — missing 8 HP, drink one 5-bottle) out of combat vs **heal-me** (turn-optimal — biggest bottle first, spill the excess) in combat, each previewing waste.
  3. **Short rest**: `/rest` channels regen at a multiplied rate while stationary and visible; instantly interrupted by damage or a bubble forming. Deliberately social downtime (resting together while chatting is the campfire moment). NO per-day rest limits — a persistent world has no "day", and rationed rests make idle waiting the resource.
  4. **Long rest = a place, not a timer**: at your **bed/camp/sanctuary** (bed-home-spawn note; pairs with the origin sanctuary + difficulty rings). What happens there: **full HP recovery**; **full reset of spendable resources** (mana/ability charges — none exist yet, but this is their designated reset point when abilities land); and it doubles as the **safe-zone activity hub** — **player-to-player trading** and a future **merchant NPC** live here, which keeps trade out of combat by construction and gives the sanctuary a reason to be visited beyond safety. (Merchant NPCs ride the same non-hostile-entity seam as treasure chests.)
  5. **Healing as toolbox** (with the spell-variety / on-kill slice): lifesteal gear cards and the mage's *support* magic lane (ally-targeted heal) — where healing creates party composition depth rather than just existing.
  All amounts/rates are knobs for the milestone-11 admin console; the milestone-12 analytics log should record heals by source so the layers can be tuned against real fights.
- [x] ~~Downed state & revive~~ → **Direction decided (2026-07-10, proposed in the group).** Reaching 0 HP **downs** a player in place instead of instantly killing them: incapacitated on their hex, visibly marked, on a bleed-out timer. **Revived in time → back up (partial HP, NO death penalty)**; timer expires → real death as today (respawn + XP-to-level-floor). Two revive routes, deliberately redundant so a group never *requires* one class: **First Aid at top tier** (non-magical, adjacent, the bandage skill's crowning tier) and a **mage support spell literally called Revive** (the support-magic lane's headline). Why this matters socially — the proposer's argument, which is exactly the game's philosophy: without it, a death mid-expedition teleports you across the map away from your party, and *either the group waits (boring) or it doesn't (feels bad)*; revive keeps the party together and makes rescue a heroic action instead of a walk. Design details still open: bleed-out length (knob), whether a downed player is still a bubble member (they must NOT gate the bubble's lock-ins — a downed player can't act), whether monsters attack downed players (lean: they prefer standing threats; an execute-happy monster kind can be a deliberate horror later), and whether a downed player can crawl. Revive interacts with nothing retroactively — until the downed state ships, 0 HP keeps meaning instant respawn.
- [ ] Does the world tick when nobody (or almost nobody) is online, or only during sessions?
- [x] ~~Auth/identity: how players claim their character~~ → **Decided & implemented (milestone 10a, 2026-07-11): name + secret link.** A player's identity is their bearer token; the client can copy it as a **character link** (`<origin>/#t=<token>`, a HUD button once joined). Opening the link on any browser/device imports the token (stored to localStorage, fragment stripped via `history.replaceState` before anything else runs — never sent to the server, never lands in chat) and rejoins the SAME character, skipping the start screen. Shoulder-surfable like any bearer secret — acceptable for the 15-friend trust model (the VPS disk/network is the trust boundary, not the token). No password, no email, no CSRF forms — see §3's "not adopted" note.
- [x] ~~Social-UI toolkit~~ → **Decided: SolidJS** (for the milestone-8 social UI). The DOM social UI (chat, quest log, party roster) floats over the Pixi canvas — text/input UI stays **DOM, not canvas** (you can't type into WebGL, and DOM gives text layout, scrolling, selection, a11y, and Playwright interaction for free). Solid is a small, native-TSX reactive-DOM library.
  - **Why Solid:** it's **native TSX**, so the existing `tsc --noEmit` gate type-checks the whole component *including the template* (props, elements, handlers) — the deciding "checkable in one tool" criterion. Its **fine-grained signals** give the nicest reactivity ergonomics, with real-DOM output (Playwright-friendly, light-DOM), a tiny runtime (~7 KB), and it stays CSP-safe (compile-time, no `new Function`). Adds only a small JSX toolchain (`vite-plugin-solid`) and **no second type-checker**.
  - **Preact was the runner-up** — more mature/adopted (~22M npm downloads/wk vs Solid's ~2.6M) and equally `tsc`-checkable — but we chose Solid for the reactivity DX; the maturity/adoption gap is acceptable for a ~15-friend game.
  - **Constraints kept:** CSP `no unsafe-eval` (cheap XSS defence-in-depth for chat/quest text; Solid is compile-time so it complies); tiny footprint (single `go:embed` bundle); **light-DOM** rendering (global CRT theme + Playwright selectors); **incremental** adoption beside the hand-written HUD.
  - **Rejected:** Lit (tagged-template `html` literals opaque to plain `tsc` without the separate `lit-analyzer`); Svelte (separate `svelte-check` + Svelte-5 "runes" churn); Alpine / Vue-runtime-templates (CSP `new Function`); SvelteKit (full-stack meta-framework; the Go binary is already the server — at most `adapter-static`, i.e. plain Svelte + Vite).

---

*This plan is a starting scaffold — expect it to evolve once the turn loop is running and you can feel where the real design friction is.*

## Appendix — combat model: why ARPG, not TTRPG


*Two short design notes that settled the combat-resolution model. They were
written during the #55 / #56 / #69 design discussion (July 2026) and are the
reasoning behind the "combat resolution is ARPG stat-checks, not TTRPG rolls"
decision recorded in issue #69 and `design-decisions.md`. Presentation
copies (PDF) were shared in that discussion; this is the canonical text.*

---

### Note 1 — Were we mixing TTRPG and ARPG?

**Short answer: yes — and it's worth being precise about *where*, because it
clears the path.**

**The engine is already ARPG.** The modifier pipeline (gear as pure-data rule
cards folding %/flat modifiers), "defence is a *rule*, not an Armor-Rating
stat," damage-reduction, deterministic resolution — that is Diablo/PoE
lineage. The first-gear review even *rejected* an "Armor Rating 9" stat
precisely because it is a TTRPG stat with nothing behind it.

**The combat-*resolution* discussion drifted TTRPG.** That entered with a
`d20` proposal — pure D&D — and got carried forward rather than flagged. The
bits that crept in, and their native-ARPG equivalents:

| TTRPG thing we picked up | ARPG equivalent |
|---|---|
| `d20 + accuracy vs Armor Rating` — one *coupled* roll | *Decoupled* stats: `glance%` (defence) + `crit%` (offence) |
| "percent hit = 80% + attacker bonus − defender penalty" | …is *still* the coupled attack-roll, just in % clothing |
| Crit on a *die face* (nat-20, elf 19–20) | Crit *chance %* (elf = +crit%) |
| "Armor Rating / AC" = harder to *hit* | `glance%` (blunt) *or* damage-reduction (mitigate) |
| "meets it beats it," clamp-as-nat-1 / nat-20 | just a % floor / ceiling |

*(2026-07-15: the defence chance was softened from binary `evasion%` to
`glance%` — a chance the incoming hit is halved, never fully negated — so an
attack turn is never wasted on a total miss. Still defender-side, still
decoupled; every argument in this appendix carries over unchanged — the
full reasoning lives in #69/#91 and the PR that landed the change.)*

**The tell is coupling.** TTRPG folds attacker accuracy *and* defender armour
into a **single** to-hit roll. ARPG keeps them as **separate** gear stats —
the weapon rolls its own `crit%`, the armour its own `glance%`,
independently. Everything decided before the pivot (baseline hit chance,
"−%to-hit" defence, accuracy modifiers) was the *coupled* model wearing
percentages.

**So ARPG is the coherent choice — not just taste.** It matches the engine
already built. A `d20`/AC path would graft a TTRPG resolution layer onto an
ARPG stat system, and they would keep fighting — the *armor-rating-as-AC vs
armour-as-a-rule-card* seam is exactly where it grinds. Pulling it back to
`glance%` / `crit%` gear stats removes the friction: offence lives on
weapons, defence on armour, each an independent percentage the engine already
knows how to fold.

---

### Note 2 — What if we moved to TTRPG?

**Two separable questions hide inside "move to TTRPG" — and they have
opposite answers.** "TTRPG" is really two things: the resolution *math*
(`d20 + bonus vs AC`, a coupled roll) and the turn *structure* (initiative,
sequential turns, an action economy, reactions). **The math is portable. The
structure is not.**

### Would the pipeline be replaced? — No.

The modifier pipeline is a *stat-stacking engine* — it sits *below* the
TTRPG/ARPG line. In D&D, "attack bonus" and "AC" are literally stacks of
modifiers (proficiency + ability + item) — exactly what the pipeline folds.
TTRPG would lean on it *more*, not less. What changes is only what sits on
top: a couple of new folded values and an attack-roll event that reads them.
Same shape as the ARPG extension (`glance%` / `crit%`). Either way the
pipeline is **extended, never replaced.**

### Would it work with our time-based turns? — Split.

- **The math — yes.** A `d20`-vs-AC comparison resolves fine inside a
  4-second WeGo turn or a combat bubble; a roll is just a value comparison at
  resolution time. But adopting *only* the math is precisely the TTRPG/ARPG
  mix diagnosed in Note 1 — it buys nothing and reintroduces the coupling
  grind.
- **The structure — no.** Real TTRPG wants *initiative-based, sequential*
  turns, an action economy (action / bonus / move), reactions and opportunity
  attacks. Those assume "it's my turn, now yours" — fundamentally at odds with
  WeGo's *simultaneous* 4-second window where everyone acts at once.

**The killer is the co-op.** WeGo exists *because* of the ~15-player group —
everyone submits in the same window, nobody waits. TTRPG initiative makes 15
players + monsters wait through each other's turns each round — the exact
thing WeGo was built to avoid. The turn structure doesn't just clash with the
clock; it clashes with the *reason the clock works that way.*

### What a real move would change

| System | Under full TTRPG |
|---|---|
| Turn model | WeGo-simultaneous → *initiative-sequential* — the core pillar changes |
| Action economy | New: action / bonus-action / movement per turn (today it's one intent per turn) |
| Resolution | `d20 + attackBonus vs AC` — pipeline-fed, plus crit on a die face |
| Reactions | Opportunity attacks, readied actions, saves — all need sequential turns |
| Multiplayer | 15 players waiting per round — undermines the very premise WeGo serves |

There is a hybrid — keep WeGo on the overworld, and drop combat into a
TTRPG-style initiative mini-battle inside the combat bubble — but it is a
whole second combat mode, a jarring real-time→turn-based mode switch, and the
15-player wait reappears inside the bubble.

### Verdict

The pipeline is safe either way — the real incompatibility was never the
pipeline, it's **WeGo vs initiative**. ARPG stat-checks (`glance%` /
`crit%`) fit WeGo *natively*: simultaneous, no turn order, one seeded draw at
resolution. TTRPG's genuine value — tactical initiative, reactions — needs
the sequential structure the game deliberately doesn't have. Cherry-pick its
math without its structure and you get the grind from Note 1; adopt its
structure and it's a different game.
