# Medium Rogue — implemented features reference

*Everything that actually exists in the game as of 2026-07-11 (main through
milestone 10a — persistence & identity).
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
  player in it locks in an intent, or after a 30 s patience timeout). A
  bubble-turn also never resolves sooner than `TURN_INTERVAL` after its own
  previous resolution (item 5, playtest batch 2) — a floor against a solo
  player spam-resolving faster than the world's own cadence; a straggler'd
  multi-player bubble is unaffected by it in practice (real lock-ins rarely
  land inside one interval). The rest of the world keeps ticking. Bubbles
  form/merge/dissolve as connected components; **only players extend a
  bubble's reach**. Walking into a bubble's radius joins the fight —
  reinforcement is a core mechanic; fleeing beyond the radius escapes it.

### Movement
- Flat-top hex grid, axial coordinates, grid-locked. **Click-to-move**
  (server BFS pathfinding, one hex per turn, re-validated each turn) or
  **QWE/ASD** keys for single steps. Up to **5 friendly entities stack** per
  hex (a full party moves as one blob; count badge rendered).
- **Player name labels** (item 8, playtest batch 2): a small always-on name
  tag above every PLAYER dot (not monsters — they get hover info instead,
  item 13), styled like the count badge and moving with the dot's tween.
  Party-color-tinted for a partymate, a brighter shade of my own dot's blue
  for mine, neutral near-white for anyone else.
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
- **Entity-targeted single-target ranged attacks** (item 7, playtest batch
  2): a bow shot names its victim by **entity id** (`IntentRequest.
  targetEntityId`), not a hex — clicking a hostile in range sends its id.
  An AoE cast (mage) stays **ground-targeted** (a hex — the blast radius
  makes that the natural target). Validated at submit (entity exists+alive,
  hostile, in range) AND re-validated at resolution against the victim's
  **post-move hex**: hits if still in range from the shooter's own post-move
  hex, else fizzles — the shot tracks a sidestepping or retreating target
  instead of trusting a stale hex.
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
  remaining HP — derived client-side by diffing bundles), **committed-action
  indicator** (item 6, playtest batch 2 — a solid step marker for a queued
  move, a persistent crosshair for a queued attack, a small hourglass on my
  own hex for a wait; shown while I've locked in this bubble-turn and it's
  still waiting on the rest of the bubble, cleared on the next turn bundle;
  `window.game.committedAction`), kill summaries and
  player deaths announced in chat, naming the slain **kind(s)**. Two or more
  players in the bubble at award time: nameless ("a wolf was slain (+20 XP to
  everyone in the fight)", "a wolf and a troll were slain (+80 XP …)", "2
  ghouls were slain (+70 XP …)") — no kill credit exists. **Exactly one**
  player in the bubble (item 3, playtest batch 2): named, active voice
  ("NAME slew a wolf (+20 XP)", mixed kinds group the same way — "NAME slew
  a wolf and a troll (+80 XP)"). Player deaths: "NAME died".

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

### Gear (milestone 6b.4, loot model updated 6c)
- **Items are content data** (registry in `internal/game/content.go`): 5
  class defaults + 8 drop items, including three designer-authored cards
  (Ancient Dwarven Mattock: +3 damage in a dwarf's hands; Staff of the War
  Mage: ×2 damage vs targets below 6 HP — a deliberate flat-threshold
  execute; Wyrmslayer Greatsword: ×1.5 damage vs dragons specifically —
  the first `targetKind`-conditioned card).
- **Drops are monster-side** (milestone 6c): each monster **kind** owns its
  own chance-to-drop and its own weighted item table (`monsterDef.drops`) —
  items no longer carry a drop weight at all. A slain monster rolls ITS
  kind's chance (10–100%, see the Monsters table below); a hit picks from
  ITS kind's table. Items land on the death hex, render as map markers, and
  are **picked up by walking over them** (announced in chat). Inventory is
  unbounded; own many, equip one per slot (close / ranged).
- **Equip / unequip toggle** (item 2, playtest batch 2): free & instant out
  of combat; **your whole turn inside a bubble** (a later move/attack
  replaces a queued swap; bubble dissolve applies it). An equip intent
  naming an item **already in its slot unequips it** instead of re-equipping
  (slot → 0: fists fallback for close, no ranged weapon at all for ranged)
  — the same free-outside/turn-inside rules apply to the toggle-off
  direction. Gear panel lists items with stats, rule text, equipped state;
  the "equipped" button is an active toggle (not disabled), amber on hover.

### Monsters (kinds & difficulty rings — milestone 6c)
- **Five kinds**, content data in `internal/game/content.go` (`monsterDefs`),
  each with its own stats, aggro radius, XP award, and loot table:

  | Kind | Ring(s) | HP | Dmg | XP | Aggro | Drop chance |
  |---|---|---|---|---|---|---|
  | Rat | 0–1 | 4 | 1 | 8 | 7 | 10% |
  | Wolf | 1 | 10 | 3 | 20 | 10 | 30% |
  | Ghoul | 1–2 | 16 | 4 | 35 | 8 | 35% |
  | Troll | 2 | 30 | 6 | 60 | 8 | 50% |
  | Dragon | 2 (capped at 1 per world) | 60 | 9 | 150 | 12 | 100% (incl. the Wyrmslayer Greatsword) |

  Wolf carries forward the pre-6c flat numbers exactly. Each kind renders
  with a distinct on-map dot color and one-letter glyph (`entities.ts`'s
  `KIND_STYLE`); an unrecognized kind falls back to the old flat monster
  red with no glyph. A kill announces the kind by name (see Combat above).
- **Difficulty rings**: the map bands into 3 concentric rings by hex
  distance from the origin (`RingCount`) — at the default `WORLD_RADIUS=24`
  that's ring 0 = 0–7 (home), ring 1 = 8–15, ring 2 = 16–24 (frontier).
  `SpawnMonsters` distributes placements across rings weighted by each
  ring's walkable area and picks a kind uniformly among the kinds
  registered for the chosen ring. **Sanctuary**: no hostile spawns within
  `SanctuaryRadius=5` of the origin (the seed of a future trade hub) —
  falls back (like every other spawn guard here) if a tiny map has no hex
  outside it.
- Spawned at startup (`MONSTER_COUNT`, default 0), seeded ring/kind
  placement. AI: hunt the nearest aggroed player one step per turn; bubble
  monsters chase their bubble's players unconditionally; world monsters
  keep hunting even while every player is bubbled (walk-in reinforcement
  pressure). **Aggro range is per-kind** (table above), overriding the
  shared default (`MonsterAggroRadius=10`, itself pipeline-hooked per
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
  character.
- **Character link** (milestone 10a — settles plan §9's identity question as
  "name + secret link"): `<origin>/#t=<token>`. A **"copy character link"**
  button appears in the HUD once joined; clicking it writes the link to the
  clipboard with a "copied!" flash. Opening the link on any browser/device
  imports the token (`net/session.ts`'s `importIdentityFromFragment`, called
  before anything else in the client runs), rejoins the **same character**,
  and skips the start screen — an imported token is always a "returning
  player." The fragment is stripped from the address bar via
  `history.replaceState` immediately: a URL hash is never sent in an HTTP
  request, so the token never reaches the server via the link itself, and
  nothing echoes it into chat. A **dead link** (the server no longer knows
  the token — state lost with persistence off, or a rejected snapshot) is
  refused rather than silently minting a default character: the client
  clears the dead identity and falls back to the start screen. **Trust
  note**: the token is a shoulder-surfable bearer secret, like the stored
  one already was — acceptable for the 15-friend trust model (the VPS is
  the trust boundary).
- **Disconnect archive** (milestone 10a): a player absent past the
  `DISCONNECT_GRACE` (default 20s) is **archived** — identity, XP, and gear
  saved — instead of deleted; rejoining with the same token **restores** the
  character at a fresh guarded spawn hex with full (level-scaled) HP,
  progression intact. Party membership and any personal quest do **not**
  survive a sweep — they dissolve / return to the board, exactly as before
  (session-scoped social state, not progression).

### World
- **Procedural generation**: seeded value-noise biomes (elevation+moisture →
  grass/forest/water), rock rim, forced origin clearing, spawns restricted
  to the origin-connected walkable region. Fixed default seed → identical
  world every restart. Camera follows the player.

### World persistence (milestone 10a, default OFF)
- **Periodic + shutdown JSON snapshot** behind `SNAPSHOT_PATH` (default `""`
  = disabled — every test and a casual `go run` stay hermetic; a deployment
  opts in). When enabled: the snapshot loads at startup, before the control
  loop starts; a background saver writes it every `SNAPSHOT_INTERVAL`
  (default 60s); a final write happens after the HTTP drain on graceful
  shutdown (SIGINT/SIGTERM), after the periodic saver has been joined — an
  in-flight periodic write can never land over the shutdown snapshot. Writes
  are atomic and durable (temp file, fsynced, then `os.Rename` in the same
  directory): a process crash or power loss mid-write leaves the previous
  snapshot intact at the live path instead of a corrupt one.
- **What persists**: every entity — players **and** monsters (a restart must
  not respawn a healed, repositioned monster population mid-expedition) —
  ground items, the quest board, the disconnect archive, and the turn/id
  counters (so SSE ids and entity/item instance ids stay monotonic and
  collision-free across a restart). The map itself is **never** persisted —
  it regenerates deterministically from `WORLD_SEED`/`WORLD_RADIUS`.
- **What stays transient**: queued move paths, a pending ranged-attack
  target, a queued equip, and combat-bubble membership (bubbles are never
  persisted — recomputed from positions on the first tick after load). Every
  restored player comes back marked disconnected **as of the load time**
  (not its pre-shutdown value), so the removal-grace clock restarts cleanly
  at load instead of sweeping every restored player instantly.
- **Fresh-on-mismatch**: a snapshot whose version, `WORLD_SEED`, or
  `WORLD_RADIUS` doesn't match the running configuration is rejected —
  logged loudly, **moved aside to `<path>.rejected-<unix-ts>`** (so the
  fresh world's periodic saver can't overwrite the only copy — a config typo
  never erases everyone's characters; fix the config and `mv` it back), and
  the server starts fresh. No migrations pre-launch (the wire's
  no-backward-compatibility rule applies to disk exactly as it does to the
  protocol); a save or load error always logs and continues, never crashes
  the game loop.
- **Archive growth is unbounded** (deliberate): tokens that never return
  accumulate in the disconnect archive and thus in the snapshot forever.
  Fine at 15-friends scale (a few hundred bytes per character); revisit with
  the planned SQLite-for-state upgrade.

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
  prerequisite). Events live: `deal-damage`, `take-damage`, `earn-xp`,
  `aggro-range` (per-kind aggro radius folds through it since 6c).
  Conditions: `chance`, `targetHPBelowPct`, `targetHPBelowFlat`,
  `targetHPFull`, `allyInBubble`, `targetAdjacent`, `attackerSpecies`,
  `targetKind` (victim is a monster of a specific registered kind — 6c,
  validated against the monster registry). Effects: `add`, `mulPct`. Fold
  order: all adds → all multipliers → event clamp (damage ≥1, XP ≥0).
  Sources: species cards + acting/equipped item cards. Content validated at
  process start (fail-loud). Every damage and XP number in the game flows
  through it.
- **Determinism**: per-resolution PCG rng seeded (worldSeed, turn); map
  iteration sorted before any rng draw; spawn randomness on separate
  fixed streams. Fully reproducible turns.
- **Testing surface**: unit tests beside code; `test/integration` drives the
  real handler tree over real HTTP/SSE; Playwright e2e drives the real
  embedded-client binary (27 specs). The client exposes **`window.game`**
  (positions incl. `monsterKind`, hp, inventory, combatMoves, damage events,
  tapHex, sendChat, identityLink…) as the always-in-sync test/debug surface.
- **Dev loop**: `make dev` (watchexec auto-restart) + `make client-dev`
  (Vite HMR proxying /api); `make check` full gate (lint, protocol drift,
  typecheck, tests, build); `make e2e`.
- **Combat event log** (`internal/game`, structured `slog`, the milestone-12
  analytics seed): every resolution path emits `slog.Info("combat", "event",
  ...)` — `move`, `attack` (attacker, victim, weapon defID, base, dealt),
  `fizzle` (reasons: `out_of_range`, `unequipped`, `bump_target_vacated`),
  `death`, `xp_award`, `pickup` — filterable on the `"combat"` msg key or the
  `event` attribute. `World.SetLogger` installs the sink (defaults to
  `slog.Default()`, mirrors `SetAnnounce`); `cmd/rogue/app` wires the
  process logger in.

---

## 3. Configuration (environment variables)

| Var | Default | Meaning |
|---|---|---|
| `LISTEN_ADDR` | `:8080` | HTTP listen address |
| `TURN_INTERVAL` | `5s` | world-turn period (tests shrink it) |
| `HEARTBEAT_INTERVAL` | `15s` | SSE keep-alive cadence |
| `MONSTER_COUNT` | `0` | monsters spawned at startup |
| `COMBAT_PATIENCE` | `30s` | bubble AFK fallback before auto-resolve |
| `BUBBLE_POLL` | `100ms` | control-loop poll (must be < TURN_INTERVAL) |
| `DISCONNECT_GRACE` | `20s` | despawn delay for disconnected players |
| `WORLD_SEED` | `0xC0FFEE` | procgen seed (decimal or 0x hex) |
| `WORLD_RADIUS` | `24` | world hex radius (~1,801 tiles) |
| `SNAPSHOT_PATH` | `""` (disabled) | world-snapshot file path; empty disables persistence entirely |
| `SNAPSHOT_INTERVAL` | `60s` | periodic snapshot-save cadence while persistence is enabled |

## 4. Game-rule constants (`internal/protocol`, compiled into both sides)

| Constant | Value | |
|---|---|---|
| `TurnSeconds` / `InputWindowSeconds` / `PlaybackSeconds` | 5 / 3 / 2 | turn anatomy |
| `CombatRadius` | 6 | bubble trigger distance |
| `StackCap` | 5 | max friendly entities per hex |
| `MaxNameLen` / `MaxChatLen` | 24 / 500 | input caps (runes) |
| `FighterMaxHP` / `RogueMaxHP` / `MageMaxHP` | 30 / 16 / 14 | level-1 HP |
| `HPPerLevel` / `DamagePerLevel` | 4 / 1 | per-level growth (**flat-curve retune pending**) |
| `XPPerLevel` / `QuestKillRewardPerTarget` | 100 / 20 | leveling & flat per-target kill-quest reward |
| `MonsterMaxHP` / `FistsDamage` | 10 / 1 | pre-6c monster baseline (wolf's HP) & unarmed profile |
| `HumanXPBonusPercent` / `ElfCritChancePercent` / `ElfCritMultiplier` / `DwarfDamageReduction` | 50 / 20 / 2 / 1 | species knobs |
| `RegenPerTurn` | 1 | out-of-combat HP per world turn |
| `MonsterAggroRadius` | 10 | default world-monster notice distance (> CombatRadius, compile-guarded); per-kind `aggroRadius` overrides it |
| `RingCount` | 3 | difficulty rings worldgen bands the map into |
| `SanctuaryRadius` | 5 | no hostile spawn within this many hexes of the origin |
| `DragonCount` | 1 | max dragons `SpawnMonsters` places per world |

*Monster stats (HP/damage/XP/aggro/loot) and item stats (damage/range/AoE
and rule cards) are content data in `internal/game/content.go`, not
constants — the wire carries display stats. `MonsterXP`,
`MonsterAttackDamage`, and `DropChancePercent` were retired in 6c: those
numbers are now per-kind (`monsterDef.xp`/`damage`/`dropChance`); wolf's
values are unchanged (20 / 3 / 30%).*

## 5. Decided but not yet built

Recorded in `roguelike-mp-plan.md` §0/§8/§9 and issue #36: flat-curve
retune, skills as level-up perks (First Aid, Make Camp), downed state &
revive, recovery layers beyond regen (potions, rests, sanctuary trade
hub — the 6c sanctuary zone is only the monster-free ground, not the hub
itself), continuous spawning with density-tracks-players, monster-kind
passives (the `rules` seam on `monsterDef` ships empty), ring UI
indicators, terrain-blocked LOS, path-preview breadcrumb, bed/home spawns
(reconnect/respawn still uses a guarded random spawn, not a bed — milestone
10a persisted characters and the world, but bed spawns stay future), admin
console & analytics log, SQLite-for-state (the milestone 10a JSON snapshot
is the decided interim store).
