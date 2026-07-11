# Medium Rogue — implemented features reference

*Everything that actually exists in the game as of 2026-07-11 (main through PR #45).
Design rationale lives in `roguelike-mp-plan.md`; current-session state in
`STATUS.md`; the content-design vocabulary in `rule-based-content-design.md`.
This file is the what-is-real summary: mechanics, systems, knobs.*

---

## 1. Game mechanics (what players experience)

### Time: WeGo turns & combat bubbles
- One shared **world turn every 5 s** (3 s input window, ~2 s playback).
  No input = stand still; queued click-to-move paths auto-advance. Latency
  and reflexes are irrelevant by design.
- **Combat time bubbles**: when a player and a hostile come within 6 hexes,
  a local bubble freezes — its turns are **action-gated** (advance when every
  player in it locks in an intent, or after a 60 s patience timeout). The
  rest of the world keeps ticking. Bubbles form/merge/dissolve as connected
  components; **only players extend a bubble's reach**. Walking into a
  bubble's radius joins the fight — reinforcement is a core mechanic; fleeing
  beyond the radius escapes it.

### Movement
- Flat-top hex grid, axial coordinates, grid-locked. **Click-to-move**
  (server BFS pathfinding, one hex per turn, re-validated each turn) or
  **QWE/ASD** keys for single steps. Up to **5 friendly entities stack** per
  hex (a full party moves as one blob; count badge rendered).
- **In combat**: click-anywhere is replaced by tactical selection — only the
  tiles reachable this turn are clickable, tinted **blue** (open moves) /
  **strong red** (adjacent hostile = bump-attack); the equipped ranged
  weapon's full reach is washed **light red** (click shoots when a hostile is
  there; anywhere for AoE); clicking your own hex waits/cancels. Reach is a
  BFS with `COMBAT_MOVE_RANGE = 1` (client), structured for future run/jump.

### Combat
- **No separate combat screen** — same map, same intents. **Bump-to-attack**
  melee; ranged **attack intent** (bow single-target, mage AoE radius 1),
  range 4 hexes, distance-only (no LOS), **no friendly fire**.
- **Phased resolution**: all moves resolve simultaneously (seeded-RNG
  tie-break on hex overflow), then all attacks land against **post-move
  positions** — retreating genuinely dodges; mutual kills are possible and
  intended. A stacked hex takes hits on a **random member**.
- Class weapon routing on click: a rogue **bumps with the dagger when
  adjacent**, shoots the bow at range (weapon-by-distance identity); a mage
  **blasts even adjacent** targets (staff bonk exists but its ranged magic is
  its real weapon); fighters are melee-only.
- **Feedback**: instant destination ring on walk clicks, one-shot flash on
  attack clicks, pending "…" on equip buttons, Diablo-style **floating damage
  numbers** (white over hostiles, red over players; killing blows shown as
  remaining HP — derived client-side by diffing bundles), kill summaries and
  player deaths announced in chat ("a monster was slain (+20 XP to everyone
  in the fight)", "NAME died").

### Classes & species (chosen on the start screen)
| | Weapons (defaults) | HP | Role |
|---|---|---|---|
| **Fighter** | Iron Sword (4) | 30 | melee, tanky, holds the front |
| **Rogue** | Dagger (7) + Shortbow (6, rng 4) | 16 | high single-target, squishy |
| **Mage** | Oak Staff (2 bonk) + Ember Focus (4, rng 4, AoE 1) | 14 | area damage, back line |

| Species | Passive (pipeline rule card) |
|---|---|
| **Human** | +50% XP on every award |
| **Elf** | 20% chance any hit crits ×2 |
| **Dwarf** | −1 damage taken per hit (floor 1) |

### Progression, XP & death
- XP from kills: **every player in the bubble gets the full amount per kill
  as it happens** — no split, no kill credit, no battle-end payout. Quest
  completions pay all current holders in full. Flat curve: level =
  1 + XP/100; +4 max HP and +1 weapon damage per level (**retune pending**:
  toolbox-progression decision says these go much flatter).
- **Death**: XP falls to the start of the current level (levels never lost),
  respawn at full HP with the **same identity and all gear** (gear always
  survives death — decided). Respawn location randomized with guards; the
  camera **cuts** to the respawn instead of panning.
- **Passive regen**: +1 HP per world turn while out of combat (never in a
  bubble, never above max). Removes death-as-the-only-heal.

### Gear (milestone 6b.4)
- **Items are content data** (registry in `internal/game/content.go`): 5
  class defaults + 7 drop items, including two designer-authored cards
  (Ancient Dwarven Mattock: +3 damage in a dwarf's hands; Staff of the War
  Mage: ×2 damage vs targets below 6 HP — a deliberate flat-threshold
  execute).
- **Drops**: 30% chance per monster kill, weighted table; items land on the
  death hex, render as map markers, and are **picked up by walking over
  them** (announced in chat). Inventory is unbounded; own many, equip one
  per slot (close / ranged).
- **Equip**: free & instant out of combat; **your whole turn inside a
  bubble** (a later move/attack replaces a queued swap; bubble dissolve
  applies it). Gear panel lists items with stats, rule text, equipped state.

### Monsters
- Spawned at startup (`MONSTER_COUNT`, default 0), seeded placement. One
  kind today (10 HP, 3 damage, 20 XP) — the kinds/rings slice is specced.
  AI: hunt the nearest player one step per turn; bubble monsters chase their
  bubble's players; world monsters keep hunting even while every player is
  bubbled (walk-in reinforcement pressure). **Aggro range 10**: world
  monsters idle until a player is within 10 hexes (pipeline-hooked per
  player for future sneaky/loud gear). Spawn guards: players and monsters
  never spawn on/within 6 hexes of each other, with fallbacks for tiny maps.

### Quests, parties, chat
- **Seeded 6-quest board** (3 kill, 3 reach), `/quest <id>` / `/abandon`,
  one slot per player/party; joining a party abandons a personal quest;
  dissolution returns the quest to the board. Kill quests tick via bubble
  presence; countdown feedback in panel and chat.
- **Parties** via chat commands (`/invite <name>`, `/accept`, `/leave`):
  ≥2 members, dissolve below that, survive death, swept on disconnect.
  Partymates colored on-map; roster panel.
- **Global chat** over SSE (ephemeral, no history), `/here` shares position;
  system announces (quests, pickups, kills, deaths) make it the de facto
  combat log.

### Joining & identity
- **Character-creation start screen** (new players only): name, class card,
  species card, Enter — keyboard operable. Identity (token) persists in
  localStorage; **returning players skip the screen** and reclaim their
  character. Disconnected players despawn after a 20 s grace (a reconnect
  within it keeps the entity); a swept player rejoins as fresh. ⚠️ No
  server-side persistence yet: a server restart wipes the world (launch gate).

### World
- **Procedural generation**: seeded value-noise biomes (elevation+moisture →
  grass/forest/water), rock rim, forced origin clearing, spawns restricted
  to the origin-connected walkable region. Fixed default seed → identical
  world every restart. Camera follows the player.

---

## 2. Technical systems

- **Architecture**: single Go binary (authoritative simulation) embedding
  the built TS/PixiJS client via `go:embed`. `cmd/rogue` → `internal/server`
  (HTTP/SSE, security headers, same-origin checks) → `internal/game` (world
  under one mutex; per-domain turn loops). Coalescing hub: a tick means
  "fetch latest state", never a delta.
- **Wire**: POST `/api/join`, `/api/intent` (move/attack/equip), `/api/chat`;
  GET `/api/map` (once), `/api/events` (SSE: full-snapshot turn bundles with
  turn-number ids, chat events, named heartbeats). Reconnect =
  resync-to-latest (`Last-Event-ID` as watermark only). JSON everywhere.
- **Protocol single source of truth**: `internal/protocol` → tygo-generated
  `client/src/protocol.gen.ts` (never hand-edited; `make protocol`).
- **Modifier pipeline** (`internal/game/rules.go`): combat values fold
  through **rule cards** (pure serializable data — no closures; the SQLite
  prerequisite). Events live: `deal-damage`, `take-damage`, `earn-xp`
  (`aggro-range`). Conditions: `chance`, `targetHPBelowPct`,
  `targetHPBelowFlat`, `targetHPFull`, `allyInBubble`, `targetAdjacent`,
  `attackerSpecies`. Effects: `add`, `mulPct`. Fold order: all adds → all
  multipliers → event clamp (damage ≥1, XP ≥0). Sources: species cards +
  acting/equipped item cards. Content validated at process start
  (fail-loud). Every damage and XP number in the game flows through it.
- **Determinism**: per-resolution PCG rng seeded (worldSeed, turn); map
  iteration sorted before any rng draw; spawn randomness on separate
  fixed streams. Fully reproducible turns.
- **Testing surface**: unit tests beside code; `test/integration` drives the
  real handler tree over real HTTP/SSE; Playwright e2e drives the real
  embedded-client binary (24 specs). The client exposes **`window.game`**
  (positions, hp, inventory, combatMoves, damage events, tapHex, sendChat…)
  as the always-in-sync test/debug surface.
- **Dev loop**: `make dev` (watchexec auto-restart) + `make client-dev`
  (Vite HMR proxying /api); `make check` full gate (lint, protocol drift,
  typecheck, tests, build); `make e2e`.

---

## 3. Configuration (environment variables)

| Var | Default | Meaning |
|---|---|---|
| `LISTEN_ADDR` | `:8080` | HTTP listen address |
| `TURN_INTERVAL` | `5s` | world-turn period (tests shrink it) |
| `HEARTBEAT_INTERVAL` | `15s` | SSE keep-alive cadence |
| `MONSTER_COUNT` | `0` | monsters spawned at startup |
| `COMBAT_PATIENCE` | `60s` | bubble AFK fallback before auto-resolve |
| `BUBBLE_POLL` | `100ms` | control-loop poll (must be < TURN_INTERVAL) |
| `DISCONNECT_GRACE` | `20s` | despawn delay for disconnected players |
| `WORLD_SEED` | `0xC0FFEE` | procgen seed (decimal or 0x hex) |
| `WORLD_RADIUS` | `24` | world hex radius (~1,801 tiles) |

## 4. Game-rule constants (`internal/protocol`, compiled into both sides)

| Constant | Value | |
|---|---|---|
| `TurnSeconds` / `InputWindowSeconds` / `PlaybackSeconds` | 5 / 3 / 2 | turn anatomy |
| `CombatRadius` | 6 | bubble trigger distance |
| `StackCap` | 5 | max friendly entities per hex |
| `MaxNameLen` / `MaxChatLen` | 24 / 500 | input caps (runes) |
| `FighterMaxHP` / `RogueMaxHP` / `MageMaxHP` | 30 / 16 / 14 | level-1 HP |
| `HPPerLevel` / `DamagePerLevel` | 4 / 1 | per-level growth (**flat-curve retune pending**) |
| `XPPerLevel` / `MonsterXP` | 100 / 20 | leveling & kill award |
| `MonsterMaxHP` / `MonsterAttackDamage` / `FistsDamage` | 10 / 3 / 1 | monster & unarmed profiles |
| `HumanXPBonusPercent` / `ElfCritChancePercent` / `ElfCritMultiplier` / `DwarfDamageReduction` | 50 / 20 / 2 / 1 | species knobs |
| `DropChancePercent` | 30 | loot roll per monster kill |
| `RegenPerTurn` | 1 | out-of-combat HP per world turn |
| `MonsterAggroRadius` | 10 | world-monster notice distance (> CombatRadius, compile-guarded) |

*Item stats (damage/range/AoE and rule cards) are content data in
`internal/game/content.go`, not constants — the wire carries display stats.*

## 5. Decided but not yet built

Recorded in `roguelike-mp-plan.md` §0/§8/§9 and issue #36: monster kinds +
difficulty rings + per-kind loot (spec ready), flat-curve retune, skills as
level-up perks (First Aid, Make Camp), downed state & revive, recovery
layers beyond regen (potions, rests, sanctuary trade hub), continuous
spawning with density-tracks-players, path-preview breadcrumb, character
persistence + bed spawns, admin console & analytics log, SQLite-for-state.
