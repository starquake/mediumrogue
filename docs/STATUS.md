# Project Status — resume here

*Last updated: 2026-07-10 (evening), after a full design-and-build day on
top of the morning's 6b.4 merge. **Landed since 6b.4** (PRs #38–#42):
combat-feel batch (instant click feedback, Diablo-style floating damage
numbers, tactical move-range overlay with ranged-reach marking and
restricted in-bubble clicks, deaths/kills in the chat log); the **first
designer gear batch** (Ancient Dwarven Mattock, Staff of the War Mage —
each adding a pipeline condition: `attackerSpecies`, `targetHPBelowFlat`);
and four decision batches in the plan doc: **toolbox progression** (flat
level curve — retune `HPPerLevel`/`DamagePerLevel` pending; power = the
gear/skill toolbox), **gear always survives death**, **scaling** (density
tracks online count + spatial difficulty rings; bubble scaling in reserve),
**recovery layers** (passive regen → potions → rests → sanctuary hub with
trade), and **skills as the level-up reward** (First Aid, Make Camp;
bounded tiers, never use-leveled) with the **downed/revive** direction.
**In flight:** the playtest-ready batch (passive regen, monster aggro
range, spawn guards + random spawns, respawn camera cut — PR pending) and
the monster-kinds + difficulty-rings slice (spec/plan awaiting review).
**Requested next:** a character-creation start screen (species/class —
mockup first per the `visual-features-need-preview` rule). Then plan §8's
**10 polish & launch**: identity + persistence across restarts are the real
launch gates (§7 JSON snapshot). Backlog: issue #36. Milestone 9 (CRT
shader) remains dropped. Update this file at the end of every working
session (milestone landed, decisions made, next step).*

## What this project is

A multiplayer roguelike for a ~15-friend group. Shared hex world on
simultaneous 5-second turns (WeGo); near hostiles the clock stops *locally*
(combat time bubbles) so fights are deliberate and friends can walk in to
help. Browser client, distribution is a URL.

Read in this order:

1. **`docs/roguelike-mp-plan.md`** — the design document. Every game rule
   that has been decided (turn anatomy, hexes, stacking, phased resolution,
   classes/species, XP, quests) and every open question, plus a
   plain-language summary at the top. Design truth lives there, not here.
2. **`CLAUDE.md`** — architecture map, commands, conventions, maintenance
   reminders.
3. This file — where work stopped and what comes next.

## State: milestones 1–5 done, verified, committed

| Commit | Milestone |
|---|---|
| `d15ff13` | 1 — Skeleton: Go server, SSE turn stream, embedded Vite/TS client, CI, tooling |
| `e1e23fd` | 2 — Static hex world (radius-12, rock rim, lake, forest) rendered via PixiJS |
| `e3e4bcb` | 3 — The turn loop: join + tokens, move intents, per-turn resolution, moving entities |
| `milestone-4-playback-feel` (branch, not yet merged) | 4 — Playback & feel: `intervalMs` on turn bundles, server-side BFS path queues, per-entity playback tweens, click-to-move + unified keyboard, visible turn timer |
| `milestone-5-multiplayer-reconnect` (branch) | 5 — Multiplayer & reconnect: `Last-Event-ID` honoured as a resync watermark (resync-to-latest, no replay buffer) + SSE header-flush fix, simultaneous-resolution integration tests, first conflict-resolution tests (friendly stacking, `STACK_CAP` overflow) with a `PlaceEntityForTest` bridge, `window.game.positions`, client SSE liveness watchdog with reconnect |

What works right now (all covered by tests):

- `make server` → world ticks every `TURN_INTERVAL` (default 5 s); SSE stream
  `/api/events` broadcasts full entity snapshots with turn-number ids and an
  `intervalMs` field so the client can derive phase timing without a
  separate `windowEndsAt` field.
- Browser client: renders the map, joins (identity in localStorage, survives
  reload), moves with QWE/ASD (Q/W/E = NW/N/NE, A/S/D = SW/S/SE) or by
  clicking a hex (click-to-move) — both submit a destination intent that the
  server resolves via BFS pathfinding into a per-entity path queue, walking
  one hex per turn. Entities glide between hexes over the playback window
  instead of snapping (per-entity playback tween). A DOM turn-timer bar shows
  the playback/input phase clock live. `window.game` exposes `intervalMs`,
  `phase`, `phaseRemainingMs`, `destination`, `tapHex`, and `positions` (all
  entity ids + hexes, for multi-client test assertions) for tests.
- `POST /api/join` (token reclaim), `POST /api/intent` (202/401/422),
  `GET /api/map`, `GET /healthz`.
- Multiple clients share one world with simultaneous per-turn resolution
  (covered by an integration test posting intents from two clients and
  reading one shared turn bundle back).
- Reconnect/resync: the server honours `Last-Event-ID` only as a
  watermark — a resuming client is coalesced straight to the latest turn
  bundle (resync-to-latest), no replay buffer or separate resync endpoint
  (see plan §4, §9). An SSE header-flush fix ensures the stream opens
  promptly on reconnect.
- Client SSE liveness watchdog: if no data arrives within
  `max(3s, 4×intervalMs)`, the client reports disconnected and reconnects —
  covered by multi-client and reconnect e2e specs.
- Monsters: `MONSTER_COUNT` (default 0) spawns that many monsters at startup
  with seeded, reproducible placement; each turn a monster hunts the nearest
  player and walks toward it, stopping adjacent (never entering the player's
  hex — no combat yet). Entities carry `Kind` (`player`/`monster`) and
  `HP`/`MaxHP` on the wire. The client colours monsters distinctly from
  players and exposes `window.game.monsters` for tests.

## Milestone 6 — decomposed into slices (too large for one spec)

Combat needs hostiles (all entities are players today) and time bubbles are a
whole subsystem, so M6 is being built as a sequence of independently-shippable
slices, each its own spec → plan → PR:

- **6.0 heartbeat warmup — DONE**: named always-on `event: heartbeat`
  + client watchdog resets on it, so the liveness watchdog survives a frozen
  combat clock (see the resolved placeholder above). Closed the milestone-5 debt.
- **6.1 phased resolution — DONE** (this PR): the move phase now resolves all
  moves simultaneously with a per-turn seeded-RNG tie-break (a PCG seeded from the world seed and the turn)
  on `STACK_CAP` overflow, replacing the ascending-entity-ID placeholder.
  Reproducible + no id favoritism (tests pin the seed). The *attack phase*
  (bump-to-attack, post-move-position attacks) is still pending in 6.3.
- **6.2 monsters & HP — DONE**: a hostile entity kind, seeded spawning
  (`MONSTER_COUNT`), HP/MaxHP on the wire, minimal hunt-nearest-player AI
  (stops adjacent, no combat yet), client rendering + `window.game.monsters`.
- **6.3 combat & death — DONE**: bump-to-attack (walk onto a hostile to fight),
  the simultaneous attack phase against post-move positions (retreat dodges,
  mutual kills), damage (`PlayerAttackDamage=5`/`MonsterAttackDamage=3`),
  monster death (removed) and player death (respawn full HP, **same id/token**,
  **no XP penalty yet — that's 6b**). Monster AI now attacks when adjacent.
  Client draws HP bars over damaged entities; `window.game.hp` exposed.
- **6.4 time bubbles — DONE**: local combat time domains. A combat **bubble**
  forms when a player and monster are within `CombatRadius=6` (distance-based) —
  computed as connected components with an opposing pair, which yields
  form/grow/**merge**/**dissolve**/**walk-in reinforce**/**escape** from one
  rule. **Only players extend a bubble's reach**: a component edge needs a
  player endpoint (monster↔monster edges are dropped), so reinforcing players
  chain the frozen area outward while an enemy walking in joins the fight
  without enlarging it. A bubble **freezes** and advances on its own **action-gated** clock
  (all its players lock in an intent, or `COMBAT_PATIENCE` (default 60s) elapses)
  while the world keeps ticking every `TURN_INTERVAL` around it. Wire:
  `Entity.InCombat` + `TurnEvent.Bubbles` (`waitingForIds`, `patienceRemainingMs`).
  Client: an in-combat marker + a "waiting for… · Ns" combat panel;
  `window.game.inCombat`/`bubble`. **Milestone 6 complete.**

## Milestone 6b — classes/species + XP (decomposed like M6)

- **6b.1 XP & leveling — DONE** (this PR): players earn **shared XP** on a kill
  (every player in the fight/bubble gets the full `MonsterXP`, no last-hit
  competition), **level is derived** from XP (`1 + xp/XPPerLevel`), and death
  **floors XP to the current level's start** (keep the level, lose within-level
  progress) — resolving the 6.3 "no XP penalty yet" debt. Wire: `Entity.XP`/
  `Level`; client: a level/XP stats HUD + `window.game.xp`/`level`. A level
  grants **no mechanical bonus yet** — that arrives with classes/species.
- **6b.2 classes — DONE** (this PR): fighter/rogue/mage with distinct combat.
  Per-class HP (fighter 30 tanky, rogue 16, mage 14) + weapon damage, both
  **scaling with level** (levels now matter). **Class-default equipped weapons**
  (rogue dagger+bow, fighter sword, mage staff) — melee **bump** uses the close
  weapon; **ranged attack intents** (`kind:"attack"`) add rogue **bow**
  (single-target) and mage **AoE magic** (`no friendly fire`); fighter has no
  ranged. Class chosen at join (client picker, default fighter); `Entity.Class`
  + `window.game.class`. Ranged rules: `BowRange`/`MageRange=4`, AoE radius 1,
  distance-only (no terrain LOS). **Gear inventory/equip/drops deferred** (see
  the `gear-equipment-system` note — 6b.2 uses class defaults + unarmed fallback).
- **6b.3 species — DONE** (this PR): three species passives chosen at join
  alongside class — **human** +50% XP, **elf** 20% crit for 2× damage, **dwarf**
  −1 damage-reduction per hit (floored ≥1). Elf crit uses the seeded per-turn RNG
  (deterministic; no draws for non-elves) at all three damage sites (bump, bow,
  AoE — a crit AoE crits its whole splash); human XP bonus at the kill award.
  Wire: `Entity.Species` + `JoinRequest.Species` (required); client species picker
  beside the class picker; `window.game.species`. All numbers are tunable
  `internal/protocol` constants. Passives are per-trait helpers for now — a
  scalable **combat modifier/rule pipeline** is planned with the gear slice (see
  §8 / the `combat-modifier-pipeline` note). **Milestone 6b complete.**
- **7 procgen — DONE** (this PR): the static hand-shaped radius-12 map is replaced
  by a **seeded procedural generator** (`GenerateMap(seed, radius)` in
  `internal/game/worldgen.go`). Terrain comes from two deterministic **value-noise**
  fields (elevation → water/land/mountain, moisture → forest/grass; no external
  deps), on a larger **radius-24** world (~1,801 tiles), with a **rock rim** and a
  **forced grass clearing at the origin**. Spawns are restricted to the origin's
  **connected walkable region** (`reachableWalkable` BFS) so a player is never
  stranded on an island/in water. `WORLD_SEED` (default `0xC0FFEE`) + `WORLD_RADIUS`
  (default 24) are env knobs (threaded like `TURN_INTERVAL`); a fixed seed
  regenerates the **same world every restart**. No protocol change — reuses
  `MapResponse`. Client: the camera now **follows the player** (`world` container
  pans to keep my entity centred; `window.game.camera`). Tunable constants:
  `noiseScale`, `waterLevel` (0.30), `mountainLevel` (0.78), `forestLevel`,
  `clearingRadius`. **Milestone 7 complete → milestone 6 era done.**

## Milestone 8 — quests, parties & chat (decomposed like M6)

- **8.1 chat — DONE** (this PR): global ephemeral chat over SSE. A dedicated
  **non-coalescing** fan-out broker (`internal/chat.Broker`, distinct from
  `internal/hub`'s coalescing-tick model — every chat message is delivered,
  best-effort per slow-subscriber buffer) publishes to `POST /api/chat` and
  fans out as a no-`id:` `event: chat` SSE frame (`protocol.ChatMessage
  {Seq,Sender,Text}`) — no-id is deliberate: chat must never advance
  `Last-Event-ID`/turn resync. A player now picks a **display name at join**
  (free-text field, default `"traveler"`, required on `Entity`/`JoinRequest`);
  `SenderFor(token)` resolves a chat sender's authoritative name + position
  server-side so commands can't be spoofed by the client. A `/`-command
  registry (`internal/chat.RunCommand`) parses `"/verb args…"`; the first
  command is **`/here`** (broadcasts the sender's live position, 📍 + `(q,
  r)`); an unknown/empty verb is a 422 whose message the client surfaces as a
  **readable local system line** (e.g. `unknown command: /badcmd`), not a raw
  JSON error blob. Client: the first **SolidJS** component,
  `<ChatPanel>` (`client/src/chat/ChatPanel.tsx`, mounted into `#chat-root` —
  a click-through overlay div so the underlying Pixi canvas keeps receiving
  map clicks; only `#chat-panel` itself re-enables `pointer-events`), backed
  by a small reactive store (`client/src/chat/store.ts`, capped to the last
  200 lines client-side — **not** a history buffer). `window.game.{name,
  chat, sendChat}` exposed for tests. **Ephemeral by design**: no server-side
  history, no replay on reconnect/join — a client only sees chat sent while
  its stream is live. Party/local channels and persisted history are later
  (see the plan §8 chat note). Covered by broker/command unit tests, a chat
  integration test (POST → both SSE subscribers receive the frame), and
  `client/e2e/chat.spec.ts` (two-client delivery, `/here`, the readable
  unknown-command line, and a pointer-events-overlay regression guard).

- **8.2 parties — DONE** (this PR): party membership via chat commands, no new
  UI beyond a roster panel. `Entity.PartyID` (0 = solo, minted per-party via
  `nextPartyID`) rides the existing turn bundle — no separate party stream.
  Three commands added to the `/`-registry (`internal/game/party.go`):
  **`/invite <name>`** records a pending invite to the **nearest** player
  named `<name>` **excluding the sender** (`nearestPlayerByNameLocked`, ties
  broken by lowest entity id) and broadcasts a chat announcement telling the
  target to `/accept`; **`/accept`** joins the accepter into the inviter's
  party (minting a new party id if the inviter was solo); **`/leave`** removes
  the caller from their party. A party is **≥ 2 members**: `leavePartyLocked`
  clears every remaining member's party id too if a leave/accept-elsewhere
  drops it below 2 — so a party never lingers at size 1. Party membership
  **persists across death** (only `/leave` or the disconnect sweep clears it)
  and the disconnect sweep purges both a swept player's party membership and
  any pending invites naming them (`world.go`). All three commands 422 with a
  readable message on failure (not joined, no such player, invite yourself,
  no/expired pending invite, already in that party, not in a party). Client:
  a second SolidJS component, `<RosterPanel>` (`client/src/party/RosterPanel.tsx`,
  mounted into `#roster-root`), renders `#roster-panel` with one
  `.roster-member` line per name — hidden entirely (via `<Show>`) while solo.
  Partymates render in a distinct on-map color (`PARTY_COLOR` in
  `client/src/render/entities.ts`, keyed off `partyId` matching mine, self
  excluded). `window.game.{party,partyId}` exposed for tests. **Deferred**:
  shared movement/waypoints, party perks/buffs, and a party leader role — see
  the plan §8/§9 notes. Covered by unit tests (invite/accept/leave, dissolve
  at <2, nearest-match determinism, disconnect-sweep purge), a chat-command
  integration test, and `client/e2e/parties.spec.ts` (two-client invite →
  accept → roster (2 members, matching non-zero party id) → leave → roster
  gone).

- **8.3 quests — DONE** (this PR): a seeded 6-quest board,
  `internal/game/quest.go` — 3 **kill** quests (slay 2–4 monsters, reward
  `N * MonsterXP`) and 3 **reach** quests (stand on a goal hex ≥8 hexes from
  spawn, reward a flat `questReachRewardXP`), generated deterministically from
  `WORLD_SEED` (sorted candidate lists before the seeded pick, so board order
  never depends on map iteration order) with a fallback for tiny test worlds
  that can't find a hex 8 away. Two new `/`-registry verbs: **`/quest <id>`**
  takes an available quest (solo or for your whole party) and **`/abandon`**
  drops your current one. **One quest slot** per player/party: taking a new
  quest 422s while you hold one; joining a party (`/accept`) auto-abandons a
  personal quest first; a party's quest returns to the board (progress reset)
  when the party dissolves below 2 or the disconnect sweep removes a holder.
  Kill quests tick **once per quest per turn** via existing combat-bubble
  presence (no separate kill-tracking pass); reach quests are checked after
  movement resolution. **Completion pays every current holder in full** — the
  human `+XP%` passive applies per holder, `syncMaxHPLocked` runs, and a new
  `World.SetAnnounce(fn)` hook (installed once at server wiring; called from
  inside the world lock, safe because the underlying chat publish is
  non-blocking) broadcasts a "Quest complete: … — NAMES gain N XP" system
  chat line (also used for the "quest returned to the board" line on
  dissolve/sweep). Client: a third SolidJS component, `<QuestPanel>`
  (`client/src/quest/QuestPanel.tsx`, mounted into `#quest-root`), renders
  `#quest-mine` (my active quest, objective + reward XP) and `#quest-board`
  (remaining available quests) from a small store (`client/src/quest/store.ts`)
  refreshed every turn bundle (`TurnEvent.Quests`, full-snapshot, no separate
  stream). `window.game.{quest,quests}` exposed for tests; the XP jump on
  completion is visible both in `window.game.xp` and the `#stats` HUD text.
  **Deferred**: the board depletes (completed quests stay completed) —
  repeatable quests arrive later alongside continuous monster spawning.
  Covered by unit tests (board determinism, take/abandon/one-slot,
  join-abandons-personal, dissolve/sweep-returns-to-board, kill tick
  once-per-turn, reach completion, full-party payout with human bonus), a
  chat-command integration test, and `client/e2e/quests.spec.ts` (dwarf join →
  take the closest reach quest → walk to its goal → completion → exact XP
  jump + `#stats` change + "Quest complete" chat line).

**Milestone 8 (quests, parties & chat) is complete.** Next per plan §8 is
**9 = shader filter**, then 10 = deploy; then late tooling **11 = live
admin/difficulty console** and **12 = combat/move analytics log** (see plan
§8.11–12).

**Handoff note (2026-07-10):** 8.1 (PR #26), 8.2 (PR #28), and 8.3 (this PR)
are landed; milestone 8 is done. The plan §9 party-quest-membership open
decision is now settled (full pay-at-completion to every current holder;
joining a party abandons a personal quest; one quest slot per
player/party; dissolve/sweep returns a quest to the board with progress
reset) — see plan §9. Also recorded but not built: the two tooling
milestones above, and a **selected-path preview** render item (plan §6 —
show my own route: goal + every hex, local-only).

## Milestone 6b.4 — gear, drops & the modifier pipeline (post-milestone-8 follow-up)

- **6b.4 — DONE** (this PR): the combat modifier pipeline (`internal/game/rules.go`)
  replaces the per-trait combat branches with a pure fold over **rule cards** —
  small `{event, when, then}` data literals, never closures (a §7 SQLite
  persistence prerequisite). Three events implemented this slice: `deal-damage`,
  `take-damage`, `earn-XP` (`applyRules`, called from `attackLocked`,
  `resolveBowLocked`, `resolveAoELocked`, and the kill-XP award); `attack-roll`,
  `on-kill`, and `aggro-range` are documented in the content guide but not
  implemented — no card needs them yet. The three species passives
  (human/elf/dwarf) were migrated onto the pipeline unchanged (`content.go`'s
  `humanCards`/`elfCards`/`dwarfCards`), reproducing the old hardcoded numbers
  exactly (pinned by `rules_test.go`).
  **Items are now real content**: `internal/game/content.go`'s `itemDefs`
  registry holds the 5 class defaults (iron sword, dagger, shortbow, oak
  staff, ember focus — the "live balance" numbers carried forward from the
  old protocol weapon constants) plus 5 starter drops — 4 with their own rule
  cards (Butcher's Cleaver, Venom Fang, Pack Bow, Ember Staff) and one flat
  upgrade with no rule card (Iron Warhammer) — validated at package init
  (`mustValidateContent`). Every character has a **close** and a
  **ranged** slot; class defaults are granted pre-equipped at join
  (`grantDefaultsLocked`); a slain monster has a `DropChancePercent=30` chance
  to drop a weighted-random item onto its death hex; walking onto a drop's hex
  picks it up automatically (`pickupLocked`). **Equip is an intent**
  (`kind:"equip"`, `IntentRequest.ItemID`): outside a combat bubble it applies
  immediately (202, no turn consumed); inside one it's the player's committed
  action for that turn, gated by the same readiness check as move/attack.
  Gear survives death (only `pendingEquip` is cleared, never `items`).
  Wire: `Entity.Items []ItemView` (id/defId/name/slot/damage/rangeHex/
  aoeRadius/desc/equipped) and `TurnEvent.GroundItems []GroundItemView`
  (id/hex/defId/name) ride every turn bundle.
  Client: a fourth SolidJS component, `<GearPanel>`
  (`client/src/gear/GearPanel.tsx`, mounted into `#gear-root`), lists my
  inventory with an "equip"/"worn" button per row (disabled once equipped);
  a new `render/items.ts` `GroundItemLayer` draws a small diamond marker per
  dropped item under the entity layer, redrawn wholesale each turn from
  `TurnEvent.GroundItems`. `main.ts` no longer mirrors weapon range/AoE as
  client-side literals — the click-vs-move ranged-attack UX hint now reads
  the equipped ranged item's `rangeHex`/`aoeRadius` straight off my entity's
  `items` each bundle. `window.game.{inventory,groundItems}` exposed for
  tests. Covered by unit tests (pipeline fold order, condition rng
  determinism, species-cards-reproduce-old-numbers, registry validation,
  weighted drop pick, pickup determinism, equip validation, gear-survives-death),
  integration tests over real HTTP (`TestEquipOverHTTP`, `TestEquipValidation`,
  `TestDropPickupLoop` — a pre-seeded monster ring farmed until a drop lands,
  since the drop roll's RNG isn't reachable/pinnable from this package — see
  `gear_test.go`'s determinism note), and `client/e2e/gear.spec.ts` (inventory
  render + panel presence + an equipped item's button rendering disabled; the
  full kill→drop→walk loop has no e2e monster-spawn hook, so it stays
  integration-only).
  **Deferred** (tracked on issue #36): buffs/status effects and durations;
  the `attack-roll`/`on-kill`/`aggro-range` pipeline events; armor/trinket
  slots; an inventory cap; item despawn; drop-on-death (corpse runs);
  per-monster loot tables (one global table today); the milestone-12
  per-modifier analytics trace.

**Known bug (tracked on #36):** `World.SpawnMonsterAt`, called mid-run after
players have already joined and bubbled, can stall — its occupancy check
(`byHex` for the WORLD domain) doesn't see entities already inside a combat
bubble, so a spawn can path a monster onto a hex it thinks is empty but
isn't. `test/integration/gear_test.go`'s `startGearServerWithMonsterRing`
works around it by placing every monster **before** `world.Run` starts and
before any player joins (the same "startup only" contract `SpawnMonsterAt`
already documents) — the real fix (making the occupancy check bubble-aware,
or documenting/enforcing startup-only via the type system) is deferred to
the #36 backlog rather than blocking this slice.

## Known placeholders / debt (all deliberate)

- **No gear/inventory yet**: classes (6b.2) use **class-default equipped weapons**
  (rogue dagger+bow, fighter sword, mage staff) with an unarmed (fists) fallback;
  there's no inventory, equip/swap, or loot drops — that's a later gear slice
  (see the `gear-equipment-system` note). No **species** passives yet (6b.3). No
  **terrain-blocked LOS** for ranged (distance-only). Killed monsters are removed
  and **do not respawn** (fixed pool depletes; continuous spawning is later).
  `protocol.PlayerAttackDamage` is now an orphaned constant (melee uses class
  weapons) — remove opportunistically.
- **`spawnHexLocked` is faction-blind**: `Join` and player respawn pick the
  nearest free walkable hex without avoiding monster-occupied hexes, so a
  player can spawn co-located with a monster (opposing co-occupancy). Inert
  (only *movers* bump-attack, so co-located entities just sit until one moves,
  then resolve normally) but technically violates the §5 "hostiles never share
  a hex" invariant — add a faction-aware spawn guard when it matters. **6.4
  note:** with time-bubble domain scoping, a joiner/respawn near an active
  bubble is also invisible to that bubble's scoped resolution for one pass
  (self-heals at the pass-end recompute) — the domain split now leans on the
  post-recompute separation invariant, so fix this when continuous spawning lands.
- **Monsters don't extend bubble reach (6.4, deliberate)**: bubble-graph edges
  require a player endpoint, so a wandering monster within `CombatRadius` of a
  *bubble monster* but far from every bubble player stays world-domain. Harmless:
  two same-faction monsters can momentarily co-locate across the world/bubble
  boundary, but monsters don't fight monsters, and player↔monster domain scoping
  is unaffected (a monster adjacent to a bubble player is still always linked in
  via a player↔monster edge).
- **Terrain-blocked line-of-sight not implemented (6.4)**: combat bubbles form
  by pure hex **distance** (`≤ CombatRadius`), not mutual line-of-sight — rock
  doesn't block "spotting" yet. Deferred follow-up (adds a hex raycast).
- **E2e on shared stateful servers is timing-flaky** (ticketed **#27**): both
  `multiplayer.spec.ts` (M5 reconnect via SSE `route.abort()`) and the
  `combat.spec.ts` damage test occasionally time out under parallel-worker
  contention — the shared Playwright servers accumulate every spec's players (no
  disconnect cleanup, below), so monsters can chase a lingering player and starve
  a chase, or reconnect timing drifts. One `make e2e` run during the 8.2 build hit
  the `multiplayer.spec` flake (2nd/3rd runs were clean). Not milestone-specific;
  the real fix is per-test isolation / disconnect cleanup. Harden separately
  (re-run on a spurious CI red for now).
- **No server-side input-window enforcement**: intent acceptance stays
  permissive (an intent is accepted whenever it arrives, regardless of the
  client-visible timer phase); revisit once combat time bubbles (milestone 6)
  need a hard cutoff.
- **No re-pathing around a route blocked mid-walk**: if a queued path's next
  step becomes unwalkable/occupied, the entity just waits at its current hex
  rather than recomputing a detour.
- **No multi-hex-per-turn travel**: destination intents always walk exactly
  one hex per turn, even out of danger — deliberate for now, revisit for
  combat/flee mechanics (milestone 6).
- **No same-origin/CSRF guard on POSTs**: acceptable while auth is
  bearer-token-in-body (no ambient credentials). Revisit with real identity.
- **Disconnect cleanup (issue #21, DONE)**: a player's entity is **removed after
  its event stream has been gone for `DISCONNECT_GRACE`** (env, default 20s). The
  SSE stream is identified by `/api/events?token=<token>`; the world tracks a per-
  player live stream count and sweeps players with no stream past the grace (in
  the `pollTick` control loop). A reconnect (stream reopens with the same token)
  within the grace **keeps** the character; the client also re-joins if its entity
  was swept during a long absence. This **decides §9's offline-character policy**:
  offline characters are removed from the live world after a grace (their *data*
  isn't persisted yet — see the deferred `character-persistence-reconnect` and
  `bed-home-spawn` notes: a reconnect currently mints a NEW character).
- **E2e per-spec servers (now redundant, kept for safety)**: `playwright.config.ts`
  still gives **every spec its own single-consumer server** (a project + webServer
  per spec file, DRY over a `specs` list; `MONSTER_COUNT` only where needed) — the
  6b.2 mitigation for cross-spec entity accumulation. With disconnect cleanup that
  accumulation is fixed at the root, so this could be simplified back to a shared
  server (with a short `DISCONNECT_GRACE`) as a follow-up. Add a new e2e spec to
  the `specs` list for now.
- **No explicit wait input**: standing still = not sending an intent. An
  explicit wait intent may become useful inside combat time bubbles
  (milestone 6) — decide then.
- **No combat-bubble "waiting for: …" timer state**: the turn timer shows
  playback/input phases only; the milestone-6 combat time bubble will need a
  distinct "paused, waiting on nearby players" state.
- **Reconnect/resync model is resync-to-latest, not replay**: with
  full-snapshot turn bundles and a coalescing hub, `Last-Event-ID` is honoured
  only as a watermark to avoid re-painting an already-seen turn — a
  reconnecting client is simply coalesced to the current snapshot. There is
  no replay buffer and no separate resync endpoint (deliberately; see plan
  §4, §9).
- **Mid-session SSE drop isn't e2e-reproducible in the sandbox**: Playwright's
  `setOffline` doesn't sever an already-open stream, so the reconnect e2e
  instead blocks `/api/events` with `route.abort()` and later restores it to
  simulate a drop/reconnect cycle.
- **Watchdog now resets on named heartbeats** (m6 warmup, done): the server
  emits a named `event: heartbeat` frame (no id) on a fixed always-on
  `HeartbeatInterval`, and the client's liveness watchdog re-arms on it as well
  as on turns. So a frozen world clock (no turns) still feeds the watchdog. The
  full "watchdog survives a frozen clock" scenario is only end-to-end testable
  once combat time bubbles (6.4) can actually freeze it; this warmup wired the
  mechanism and proved the client observes heartbeats (`window.game.heartbeats`,
  `client/e2e/heartbeat.spec.ts`).
- **nolint audit reminder** lives in CLAUDE.md (6 suppressions as of m3).

## Environment & gotchas (this repo, this machine)

- **Go is NOT on PATH**: use `export PATH=$PATH:/usr/local/go/bin` (the
  Makefile already falls back to `/usr/local/go/bin/go`).
- **Bash tool cwd drifts** between calls in long sessions — `cd` to the repo
  root (or use absolute paths) before `make`/`git`, and remember
  `make ... | tail` masks failures unless `set -o pipefail` is set first.
  This bit us once: a "passing" check that had actually failed.
- Playwright Chromium is installed (`npx playwright install chromium` done).
- `make check` = lint + protocol drift + TS check + tests + build. The
  protocol gate diffs `client/src/protocol.gen.ts` against git — after
  changing `internal/protocol`, run `make protocol` and stage the result.
- E2E (`make e2e`) runs the *real* binary with `TURN_INTERVAL=250ms` on port
  8123 (see `client/playwright.config.ts`).
- The topbanana repo (pattern source: hub, Makefile, server layering) can be
  re-cloned from `starquake/topbanana` if needed for reference.

## Working agreements observed so far

- Every feature lands with tests at the right layer (unit / integration /
  e2e) and `window.game` stays in sync for Playwright.
- Verify with `make check` + `make e2e` **before** committing; screenshot
  visual changes (headless Playwright + Read the PNG).
- Commit messages explain the *why* and note placeholders explicitly.
- Game-rule constants go in `internal/protocol` (shared with the client);
  timing knobs are env vars so tests can shrink them.
