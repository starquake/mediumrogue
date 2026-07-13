# Medium Rogue — implemented features reference

*Everything that actually exists in the game as of 2026-07-12 (main through
the inventory system (PR #51) and the three-environment deployment).
Design rationale lives in `roguelike-mp-plan.md`; current-session state in
`STATUS.md`; the content-design vocabulary in `rule-based-content-design.md`.
This file is the what-is-real summary: mechanics, systems, knobs.*

---

## 1. Game mechanics (what players experience)

### Time: WeGo turns & combat bubbles
- One shared **world turn every 4 s** (2 s input window, ~2 s playback).
  No input = stand still; queued click-to-move paths auto-advance. Latency
  and reflexes are irrelevant by design.
- **Combat time bubbles**: when a player and a hostile come within 6 hexes,
  a local bubble freezes — its turns are **action-gated** (advance when every
  player in it locks in an intent, or after a 30 s patience timeout — lowered
  from 60s, item 4, playtest batch 2). A
  bubble-turn also never resolves sooner than `TURN_INTERVAL` after its own
  previous resolution (item 5, playtest batch 2) — a floor against a solo
  player spam-resolving faster than the world's own cadence; a straggler'd
  multi-player bubble is unaffected by it in practice (real lock-ins rarely
  land inside one interval). The rest of the world keeps ticking. Bubbles
  form/merge/dissolve as connected components; **only players extend a
  bubble's reach**. Walking into a bubble's radius joins the fight —
  reinforcement is a core mechanic; fleeing beyond the radius escapes it.
  The "In combat — waiting for: …" panel names the stragglers by **display
  name** (item 3, playtest batch 3 — was raw entity ids), mapped client-side
  from the bundle's entities with a `#id` fallback for an unknown id.

### Movement
- Flat-top hex grid, axial coordinates, grid-locked. **Click-to-move**
  (server BFS pathfinding, one hex per turn, re-validated each turn) or
  **QWE/ASD** keys for single steps. Up to **5 friendly entities stack** per
  hex (a full party moves as one blob; count badge rendered).
- **Movement keys are ignored while typing** (item 10, playtest batch 2, bug
  fix): a focused input/textarea/contenteditable (chat, in particular — w/a/
  s/d are ordinary letters too) or the start screen being visible suppresses
  the QWE/ASD handler.
- **SPACE = wait** (item 11, playtest batch 2): the same own-hex move a
  click already waits/cancels with — clears any queued path, and inside a
  bubble it locks in this turn's action like any other move intent.
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
- **HUD stats line** (item 9, playtest batch 2): `Lv L · xp/XPPerLevel XP ·
  (q, r)` — my entity's hex, live per turn bundle.

### Gear & inventory (milestone 6b.4, loot 6c, inventory system: slots/backpack/drop/pickup/drink)
- **12-type item taxonomy** (the item's `type` decides everything): five
  weapon types `melee-weapon / thrown-weapon / ranged-weapon / staff / wand`,
  a `consumable`, and six body types `head / body / hands / ring / amulet /
  feet`. The equip **slot is derived from the type** — each gear type fits
  exactly one slot; a consumable has no slot (it lives in the backpack).
- **8 equip slots** — the six body slots plus **two class-shaped weapon
  slots**: fighter = melee + thrown · rogue = melee + ranged · mage = staff +
  wand. A staff can melee-bonk; a wand never melees. Empty melee-ish slot →
  unarmed fists. **No thrown content exists yet**, so a fighter has no ranged
  attack (its thrown slot ships empty). Plus a **backpack of exactly 4
  entries**: a gear instance or a **consumable stack** per entry (identical
  consumables merge, up to 5; stacks never split).
- **Wearability is on the ITEM, classes stay single**: a weapon names exactly
  which classes may wield it; armor/jewelry may name several (Leather Armor:
  fighter or rogue) or default to **any**. Characters never multi-class.
- **Items are content data** (`internal/game/content.go`): 5 class defaults +
  designer drops (Ancient Dwarven Mattock, Staff of the War Mage, Wyrmslayer
  Greatsword — the `targetKind` card) + **starter armor/consumable** (Leather
  Armor: take-damage −1, floor 1; Headband of Learning: earn-XP ×1.05;
  Healing Potion: drink +5 HP, stacks to 5).
- **Drops are monster-side** (milestone 6c): each monster **kind** owns its
  chance-to-drop and its weighted table (`monsterDef.drops`); a slain monster
  rolls its own chance (10–100%) and picks from its own table (potions ride
  the rat/wolf tables at low weight). Items land on the death hex and render
  as map markers.
- **Five inventory actions, one rule** — free & instant out of combat, **your
  whole turn inside a bubble** (a later move/attack supersedes a queued
  action; bubble dissolve applies it):
  - **equip** — moves a backpack item into its slot, swapping any displaced
    occupant back into the vacated entry. Naming an already-equipped item
    **unequips** it (toggle).
  - **unequip** — equipped item → a free backpack entry (rejected if full).
  - **drop** — an owned item lands on the player's own hex as a single ground
    stack: a consumable stack drops **whole** (one ground stack carrying its
    count — it is not split), gear is count 1.
  - **pickup** — an explicit intent (walk-over auto-pickup is **gone**), for
    one whole ground stack: the server gives its units a home in priority
    order — **top up a matching stack (to the cap) → free backpack entry**;
    a partial fit takes what fits and **leaves the remainder** on the ground
    as a smaller stack; nothing fits → **reject** with a clear error the
    client surfaces ("backpack full — drop something first"). Items never
    auto-equip. The client modal shows a stack as one row ("Healing Potion
    ×3 · consumable").
  - **drink** — a consumable: applies its heal (clamped to max HP) and
    decrements the stack; an emptied stack frees its entry.
- **Client** — a toggleable **paper-doll** panel (the `i` key, sharing the
  movement keys' typing-focus guard, + a HUD button + an in-panel ×; default
  closed since it is large): the 8 hex slots on a Vitruvian layout with the
  two weapon slots labelled per class, a 4-cell backpack with stack counts and
  per-item drop buttons. Walking onto a hex with ground items opens a **pickup
  modal** — one row per item (name + type), an individual **take** button,
  inline backpack-full feedback on a rejected row (row stays pickable), and
  "Close — leave the rest" (reopens on hex re-entry). Monster loot and player
  drops behave identically.
- **Hover stat tooltip** — hovering an equipped hex or a backpack cell shows a
  floating tooltip: the item's `damage`/`range`/`AoE` and its effect line, and
  — when a **different** item fills that slot — the delta vs the equipped item
  (green `+N` / red `-N`), so a pickup can be weighed before equipping.
  Stat-less gear shows "No combat stats". Below the stats, an item's authored
  **flavor/lore** renders as dim italic (the `ItemView.Flavor` field, seeded
  from the gear cards' `Fantasy:` text — e.g. the Wyrmslayer's dragon
  Werdmullerix); flavor is cosmetic, never gameplay-affecting.

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
- **Enemy hover tooltip** (item 13, playtest batch 2): hovering a monster's
  hex shows a small DOM tooltip near the cursor — kind display name + "HP
  cur/max". Client-side only (positions/hp/maxHp already ride every turn
  bundle); `pointer-events: none` throughout, so it never blocks a click.
  **HP is distance-gated** (item 6, playtest batch 3): the HP line only
  shows when the hovered monster is within `CombatRadius` of my own
  entity — name only beyond that (scouting doesn't read exact health
  through the fog of distance).
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
- **Seeded 6-quest board** (3 kill, 3 reach), `/quest <id>` / `/abandon <id>`.
  **Multiple personal quests** (item 14, playtest batch 2 — amends 8.3's
  one-slot rule): a player may hold **several personal quests
  concurrently**, progressing and paying out independently; **a party still
  holds at most one quest at a time**. **Joining a party no longer abandons
  personal quests** (also amends 8.3) — they keep progressing alongside
  whatever the party itself takes. `/abandon` now names the quest
  explicitly (`<id>`), since "the" active quest is no longer unambiguous.
  Dissolution still returns the PARTY's quest to the board. Kill quests tick
  via bubble presence, once per distinct quest (a solo player's several
  concurrent kill quests all tick from the same kill); countdown feedback in
  panel and chat.
- **Quest goal marker** (item 12, playtest batch 2): a pulsing gold diamond
  on EACH of my active "reach" quests' goal hexes — above the ground-loot
  layer, below entities. Kill quests get no marker (no single hex to point
  at); a marker clears when its quest completes or is abandoned.
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
- **Multi-tab safety** (item 2, playtest batch 3 — the "players swapped
  identities" fix): a reclaim/rejoin always sends the tab's **own known
  token**, never a re-read of localStorage (which two tabs on one origin
  share — the root cause: one tab's rejoin could silently pick up another
  tab's freshly-written token and start controlling that character). A
  `storage`-event listener reloads any tab whose stored identity is
  overwritten by another tab with a different token — split-brain becomes an
  obvious, consistent reload. A rejoin whose token the server no longer
  knows at all reloads to the start screen instead of silently minting a
  fresh level-1 character in the old one's place.

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
  ground items, the quest board, the disconnect archive, the turn/id
  counters (so SSE ids and entity/item instance ids stay monotonic and
  collision-free across a restart), and the **worldId** (item 4, playtest
  batch 3 — worldId added at snapshot version 2; **version 3** adds the
  slot-keyed equipped map + backpack/stacks of the inventory system; a
  restored world keeps its identity, see the world-reset signal below). The
  map itself is **never** persisted —
  it regenerates deterministically from `WORLD_SEED`/`WORLD_RADIUS`.
- **What stays transient**: queued move paths, a pending ranged-attack
  target, a queued inventory action, and combat-bubble membership (bubbles
  are never persisted — recomputed from positions on the first tick after
  load). Every
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
  (HTTP/SSE, security headers; no same-origin/CSRF check yet — tracked as
  debt in STATUS) → `internal/game` (world
  under one mutex; per-domain turn loops). Coalescing hub: a tick means
  "fetch latest state", never a delta.
- **Wire**: POST `/api/join`, `/api/intent`
  (move/attack/equip/unequip/drop/pickup/drink), `/api/chat`;
  GET `/api/map` (once), `/api/events` (SSE: full-snapshot turn bundles with
  turn-number ids, chat events, named heartbeats). Reconnect =
  resync-to-latest (`Last-Event-ID` as watermark only). JSON everywhere.
- **World-reset signal** (item 4, playtest batch 3): every turn bundle
  carries `worldId` — a random hex string minted once at world creation and
  **persisted in the snapshot** (a restored world keeps its predecessor's
  id: it IS the same world). The client remembers the first `worldId` it
  sees per session and does a full page reload if a later bundle's differs —
  a genuine world reset (restart with no matching snapshot), never an
  ordinary reconnect. The reload re-runs the reclaim-or-fail join path,
  which falls back to the start screen if the stored token is truly dead.
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
  embedded-client binary (35 e2e tests across 21 spec files). The client exposes **`window.game`**
  (positions incl. `monsterKind`, hp, inventory, equipped, backpack,
  panelOpen, pickupModal, combatMoves, damage events, tapHex, sendChat,
  identityLink…) as the always-in-sync test/debug surface.
- **Dev loop**: `make dev` (watchexec auto-restart) + `make client-dev`
  (Vite HMR proxying /api); `make check` full gate (lint, protocol drift,
  typecheck, tests, build); `make e2e`.
- **Combat event log** (item 1, playtest batch 2 — `internal/game`,
  structured `slog`, the milestone-12 analytics seed): every resolution
  path emits `slog.Info("combat", "event",
  ...)` — `move`, `attack` (attacker, victim, weapon defID, base, dealt),
  `fizzle` (reasons: `out_of_range`, `unequipped`, `bump_target_vacated`,
  `pending_item_action`), `death`, `xp_award`, `pickup` (item defID, count),
  `drop` (item defID, count, hex), `drink` (item defID, resulting hp) —
  filterable on the `"combat"` msg key or the
  `event` attribute. `World.SetLogger` installs the sink (defaults to
  `slog.Default()`, mirrors `SetAnnounce`); `cmd/rogue/app` wires the
  process logger in.
- **Identity audit log** (item 7, playtest batch 3 — same filterable
  convention, msg key `"identity"`): every identity lifecycle decision
  emits `slog.Info("identity", "event", ...)` — `join-new` (id, name,
  class), `join-reclaim` (live token), `join-restore` (archived token),
  `join-rejected` (reason: `invalid_name`/`invalid_class`/
  `invalid_species`/`no_spawn_hex`), `sweep-archive` (id, name), and
  `snapshot-restore` (player count, archive count, worldId). Token-bearing
  events carry a `token_prefix` of the first **8 chars only** — never the
  full bearer secret (a full token in a log file would be a
  character-theft vector). Purpose: a future cross-machine "players
  swapped" report gets diagnosed from the server log's join/sweep/restore
  sequence instead of hypothesized.

---

## 3. Configuration (environment variables)

| Var | Default | Meaning |
|---|---|---|
| `LISTEN_ADDR` | `:8080` | HTTP listen address |
| `TURN_INTERVAL` | `4s` | world-turn period (tests shrink it) |
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
| `TurnSeconds` / `InputWindowSeconds` / `PlaybackSeconds` | 4 / 2 / 2 | turn anatomy |
| `CombatRadius` | 6 | bubble trigger distance |
| `StackCap` | 5 | max friendly entities per hex |
| `BackpackSize` / `ItemStackCap` | 4 / 5 | backpack entries · max identical consumables per stack |
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

Recorded in `roguelike-mp-plan.md` §0/§8/§9, `design-roadmap.md` (Q1–Q11
all decided 2026-07-13), and issue #36: flat-curve retune, the **3-tree
skill system** (Class/Adventure/Survival; level-up = one bankable skill
point; First Aid & Make Camp seed the Survival/Adventure trees), the
**decoupled `evasion%`/`crit%` combat chances** (#69 — crit is pure content
via the elf-crit pattern; evasion needs the new pre-damage `evasion-check`
event; AoE always hits), the **additive percentage fold** (#61 principle
14), **sanctuary-scatter first spawn** then bed spawns, downed state &
revive, further recovery layers (rests, the sanctuary **trade hub** — the
6c sanctuary zone is only the monster-free ground, not the hub itself;
healing potions + the backpack-cap layer now ship with the inventory
system), thrown-weapon content (the fighter's thrown slot ships empty) &
wand↔staff interactions, item destruction/durability, backpack upgrades,
trading, continuous spawning with density-tracks-players, monster-kind
passives (the `rules` seam on `monsterDef` ships empty), ring UI
indicators, terrain-blocked LOS, path-preview breadcrumb, bed/home spawns
(model decided — see design-roadmap Q9: sanctuary-scatter first spawn, then
last-visited bed with Home fallback; reconnect/respawn still uses a guarded
random spawn today — milestone 10a persisted characters and the world, but
the bed slice stays future), admin
console & analytics log, SQLite-for-state (the milestone 10a JSON snapshot
is the decided interim store).

## Deployment

Three environments run from one binary image, on one VPS, behind SWAG. See
`deployments/README.md` for the operator setup checklist.

| Environment  | Domain                                    | Trigger                     | Image           |
| ------------ | ----------------------------------------- | --------------------------- | --------------- |
| production   | `mediumrogue.bananajuice.net`             | push a `v*.*.*` tag         | promoted semver |
| staging      | `mediumrogue-staging.bananajuice.net`     | CI green on `main`          | `:edge`         |
| development  | `mediumrogue-development.bananajuice.net` | `deploy:dev` label on a PR  | `pr-<n>`        |

- **Pipeline:** `ci.yml` builds one image per green `main` commit
  (`sha-<commit>` + `:edge`), cosign-signs it, and (on a `v*` tag) `promote`
  retags it to semver. `deploy.yml` resolves the tag to its digest, verifies
  the signature (staging/production; development skips it), and runs
  `docker compose up -d` over SSH.
- **State:** each environment keeps its own JSON world snapshot on its own
  named volume (`SNAPSHOT_PATH=/data/world.json`); the three worlds are
  independent.
- **No secrets:** the deploy `.env` carries only `IMAGE_NAME`/`IMAGE_DIGEST`.
