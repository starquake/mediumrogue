# Project Status — resume here

*Last updated: 2026-07-12 (later session) — **deployment is live and the
inventory system is merged**; the project's next frontier is a **design
backlog**, not code in flight.*

*__Deployment (plan §10 launch infra) — LIVE.__ Three environments run from one
image on the VPS (`zoot`, behind SWAG) via a GitHub Actions pipeline
(`.github/workflows/{ci,deploy}.yml`, `deployments/`): **production**
`mediumrogue.bananajuice.net` (a `v*` tag → `promote` → cosign-verified,
digest-pinned deploy; first release **`v0.0.1`**, marked pre-release),
**staging** `mediumrogue-staging.bananajuice.net` (auto on every push to
`main`), **development** `mediumrogue-development.bananajuice.net` (a PR labeled
`deploy:dev`; that PR's branch must be current with `main` or the label fires
nothing). Build-once → cosign-sign → promote → deploy-by-digest over SSH; each
env has its own world volume. Two bugs the static checks couldn't catch
surfaced on the first real run and are fixed: SWAG's `proxy.conf` already sets
`proxy_read_timeout`/buffering (env confs add only `proxy_buffering off`), and
each env's SSH deploy user must be in the `docker` group. Operator runbook:
`deployments/README.md`.*

*__Inventory system — MERGED (PR #51).__ The six-task slots/backpack/pickup
system (full task breakdown in the block below) landed, plus client polish:
snappier **move animation** (a fixed 200 ms tween + cubic ease-in-out,
replacing the linear whole-playback drift); paper-doll **fixes** (removed the
silhouette blob under the amulet; hex borders now trace all six edges, not
left/right only — a `clip-path` clips an inset box-shadow on the diagonals); a
hover **stat tooltip** (damage/range/AoE + the effect line, compared vs the
item equipped in that slot — green +N / red -N); and **item flavor/lore text** —
a new `flavor` field on items/`ItemView`, seeded from the gear cards' `Fantasy:`
lines (Wyrmslayer's dragon Werdmullerix, etc.), shown as dim italic in the
tooltip.*

*__Design backlog — the next arc.__ Eight design issues (#55–#62) form one
interlinked arc: gear evolution (#55 weapon type-properties, #56 drop class
restrictions) -> skills (#57, #61 three-tree principles + skill-card format) ->
XP & progression (#60) -> subclasses (#58) -> skill UI (#62). Engine-grounded
feedback is posted on every thread. The build-order dependency map is
**`docs/design-roadmap.md`** — start there when picking up the design work.
Separately, three cleanup issues remain: #27 (flaky reconnect e2e), #31, #36.*

*__Next (nothing is on fire).__ Options, rough priority: (a) start the first
design slice — the gear type-properties refactor (#55/#56) is the keystone —
via spec -> plan -> review; (b) land the cheap wins that don't need the whole
arc (XP curve + front-loaded HP are formulas; cut `DamagePerLevel`; stacking
throwables); (c) cut a production release (tag `v0.1.0`) to put the inventory
system in front of the group; (d) plan §8/§11-12 tooling (admin console,
analytics). Small open PR **#63** records the AI-comment attribution convention
— awaiting the `ready to merge` label.*

*Last updated: 2026-07-12, after the **inventory system** (slots, backpack,
drop & pickup) on branch `feat/inventory-slots` (PR open, not yet merged;
built on top of batch 3, which is now merged to main). The widest
entity-model change since 6b.4, in 6 tasks. **Taxonomy & storage (task 1):**
gear became a **12-type taxonomy** (`melee-weapon/thrown-weapon/
ranged-weapon/staff/wand/consumable/head/body/hands/ring/amulet/feet`), the
slot **derived from the type**; per-entity storage moved from a flat items
list + close/ranged ids to a **slot-keyed `equipped` map** (8 slots: 6 body +
2 class-shaped weapon slots — fighter melee+thrown, rogue melee+ranged, mage
staff+wand) **+ a 4-entry `backpack`** with consumable stacks (≤5, merge
never split). Wearability moved onto the ITEM (`wearableBy`; weapons name
their classes, armor/jewelry default "any", may list several) — characters
stay strictly single-class. The combat seam (`closeDefFor`/`rangedDefFor`)
re-derives through the class shape; fighter's thrown slot ships empty (no
ranged, unchanged). **Actions (task 2):** equip/unequip/drop/pickup/drink
intents, one free-outside/turn-inside rule (generalized `pendingItemAction`);
**auto-pickup removed** — pickup is an explicit intent, server gates
merge→free-entry→**reject** with the exact "backpack full — drop something
first". **Content (task 3):** Leather Armor (take-damage −1, fighter-or-rogue
— first multi-class wearability card), Headband of Learning (earn-XP ×1.05,
gear XP cards now fold at awards), Healing Potion (drink +5, stacks 5; rides
rat/wolf tables low-weight); content guide vocabulary updated. **Persistence
(task 4):** snapshot **v3** (v2 = batch-3's WorldID; v3 adds equipped/
backpack/stacks), version-gate rejects v1 AND v2; full HTTP inventory loop +
unit round-trip. **Client (task 5):** a toggleable **paper-doll** panel (the
`i` key + HUD button + in-panel ×; default closed — it's large) transcribing
the approved Vitruvian mockup, a 4-cell backpack with stack counts + drop
buttons, and a per-hex **pickup modal** (rows name+type, per-row take, inline
backpack-full feedback, "Close — leave the rest"); `window.game.{equipped,
backpack,panelOpen,pickupModal}` synced; `inventory.spec.ts` replaces
`gear.spec`. **Deviation:** backpack-full-rejection / stack-render / drink are
integration-covered (the monster-free e2e server can't produce a full
backpack or a consumable from class defaults). `make check` + `make e2e` (35
specs) green. **Next**: merge this PR; then plan §8 tooling (11 admin console,
12 analytics) or deployment.*

*Previously (2026-07-11, later session), after "playtest feedback batch
3" — 6 items on branch `feat/playtest-batch-3` (**now merged**, PR #50),
on top of batch 2. **Item 1** turn cadence lowered 5/3/2 → **4/2/2**
(`TurnSeconds`/`InputWindowSeconds`; playback unchanged) — the plan §9
"feel-test the cadence" decision landing (3 s input felt slow in play).
**Item 2 (BUG, root-caused)**: the live "players swapped identities" report
was a **client-side cross-tab race** — `net/session.ts`'s join() re-read
the token to reclaim from localStorage (shared by every tab on one origin)
instead of the caller's own known token, so one tab's periodic rejoin could
silently adopt another tab's freshly-written token and start controlling
that character. Fixed by splitting `join()` (new character, token always
empty) from `reclaim(identity)` (never re-reads storage; reclaim-or-fail —
an unknown token 422s and reloads to the start screen instead of minting a
stranger), plus a `storage`-event listener that reloads any tab whose
stored identity another tab overwrites. Server-side Join was verified
correct and its identity invariants pinned (2 unit tests, 1 HTTP
integration test, 1 new Playwright multi-tab spec,
`identity-multitab.spec.ts`). **Item 3** the combat panel's "waiting for"
now shows display names (was entity ids). **Item 4** world-reset signal:
`TurnEvent.WorldID`, minted at world creation, persisted in the snapshot
(version 1→2; a restored world keeps its id) — the client reloads when a
bundle's worldId differs from its session's first. **Item 5 (visual,
screenshot-verified)**: the HUD/quest-panel overlap was two independent
`position:fixed` boxes with a hardcoded `top:8rem` guess — now one shared
`#left-column` flex column (panels start where the HUD actually ends),
capped above the chat panel's zone with internal scroll; quest/gear panels
widened 20→23rem. **Item 6** the monster hover tooltip's HP line is now
gated by `CombatRadius` (name-only beyond it). **Environment note:**
running `make check` with the default shared Go/golangci caches while
OTHER worktree agents run concurrently produced bogus stale-path lint
errors — isolate `GOCACHE`/`GOLANGCI_LINT_CACHE` to a private dir per
worktree when that happens. **Next**: merge the batch-2 and batch-3 PRs,
then resume plan §8's post-launch tooling or deployment.*

*Previously (2026-07-11), after "playtest feedback batch 2" — 13
user-requested items on branch `feat/playtest-batch-2` (PR open, not yet
merged), landed on top of milestone 10a. Server: **item 1** structured
`slog` combat event log (`event=move/attack/fizzle/death/xp_award/pickup`,
`World.SetLogger`) — the milestone-12 analytics seed; **item 2** equip
intent is now an unequip TOGGLE (naming an already-equipped item clears the
slot instead of no-op-swapping); **item 3** a bubble kill announce names a
SOLO killer ("NAME slew a wolf (+20 XP)"), nameless wording unchanged for
2+ players; **item 4** `COMBAT_PATIENCE` default lowered 60s→30s; **item 5**
a bubble-turn floor (`bubble.lastResolvedAt` + `TURN_INTERVAL`) blocks
solo-player turn-spam; **item 7** single-target ranged attacks are now
ENTITY-targeted (`IntentRequest.TargetEntityID`), re-aiming at the victim's
post-move hex at resolution (mage AoE stays hex-targeted); **item 14**
(DECISION CHANGE, amends 8.3) a player may hold **multiple personal quests
concurrently**; joining a party no longer abandons them; a party still
holds at most one quest; `/abandon` now takes `<id>`. Client: **item 6**
committed-action indicator (move/attack/wait glyphs, `window.game.
committedAction`) while waiting on a bubble turn; **item 8** always-on
player name labels; **item 9** HUD `(q, r)` position readout; **item 10**
fix — typing in chat no longer moves the character; **item 11** SPACE =
wait; **item 12** pulsing gold quest-goal marker(s) for reach quests
(plural since item 14); **item 13** enemy hover tooltip (kind + HP). See
`docs/FEATURES.md` (kept in sync same-PR) for the full current picture and
plan §5/§9 for the amended decisions. **Next**: merge the PR, then resume
plan §8's post-launch tooling (11 = admin/difficulty console, 12 = the
analytics log this batch's item 1 seeds) or deployment.

*Previously (2026-07-11), milestone 10a (persistence & identity)
landed on top of milestone 6c (monster kinds & difficulty rings).
**10a, this session — the launch gates:** the disconnect sweep now
**archives** a player's identity/XP/gear (`World.archive`, keyed by token)
instead of deleting it; `Join` tries live token → **archived token
restores** (fresh spawn hex, full level-scaled HP, gear/XP intact) → unknown
token → new character. A versioned JSON **world snapshot**
(`World.MarshalState`/`RestoreState`, `internal/game/snapshot.go`, disk
shape fully decoupled from the wire) persists every entity (players AND
monsters), ground items, the quest board, and the archive, behind
`SNAPSHOT_PATH` (default `""` = disabled, so every existing test and a
casual `go run` stay hermetic) with a periodic saver
(`SNAPSHOT_INTERVAL`, default 60s) and a final write on graceful shutdown —
all wired in `cmd/rogue/app`. A version/seed/radius mismatch logs and starts
fresh, never a migration. Identity is now a copyable **character link**
(`<origin>/#t=<token>`, a HUD "copy character link" button once joined;
`net/session.ts`'s `importIdentityFromFragment` imports and strips the
fragment before anything else runs), settling plan §9's identity question.
See spec: `docs/superpowers/specs/2026-07-11-m10a-persistence-identity-design.md`.
**Landed before that:** milestone 6c (monster kinds & difficulty rings —
registry of 5 kinds, per-kind loot/aggro, difficulty rings, the Wyrmslayer
Greatsword), the combat-feel batch, the first designer gear batch, four
plan-doc decision batches, the playtest-ready batch, and `docs/FEATURES.md`
(the what-is-real reference — keep it in sync alongside this file per
CLAUDE.md's same-PR convention). **Requested next:** bed/home spawns stay
future (plan §9). Backlog: issue #36, monster-kind passives (the `rules`
seam on `monsterDef` ships empty), ring UI indicators. Milestone 9 (CRT
shader) remains dropped.

- **Deployment (landed, 2026-07-12):** three environments — production
  (`mediumrogue.bananajuice.net`, `v*` tag), staging
  (`mediumrogue-staging.bananajuice.net`, main), development
  (`mediumrogue-development.bananajuice.net`, `deploy:dev` PR label) — via
  `.github/workflows/{ci,deploy}.yml`, `deployments/app/*`, and
  `deployments/swag/*`. Image is built once per green main commit, cosign-
  signed, promoted on tag, deployed by digest over SSH behind SWAG. Copies
  topbanana's pipeline minus all secrets. **Operator still owns** DNS CNAMEs,
  GitHub Environments + SSH secrets, and placing the SWAG confs — see
  `deployments/README.md`. First real end-to-end test happens after that
  manual setup.

Update this file at the end of every working session (milestone landed, decisions made, next step).*

## What this project is

A multiplayer roguelike for a ~15-friend group. Shared hex world on
simultaneous 4-second turns (WeGo); near hostiles the clock stops *locally*
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

- `make server` → world ticks every `TURN_INTERVAL` (default 4 s); SSE stream
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
  `resolveBowLocked`, `resolveAoELocked`, and the kill-XP award); `on-kill`
  is documented in the content guide but not implemented — no card needs it
  yet; `aggro-range` shipped later in 6c. (The once-planned `attack-roll`
  to-hit event was later **dropped** — combat is fully ARPG/decoupled, so
  offence/defence are `crit%`/`evasion%` chances, not a coupled roll; see #69.) The three species passives
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
  the `on-kill` pipeline event (`aggro-range` shipped in 6c; the once-planned
  `attack-roll` was later dropped for the ARPG `evasion%`/`crit%` model — #69);
  armor/trinket slots; an inventory cap; item despawn; drop-on-death (corpse
  runs); per-monster loot tables (**shipped in 6c** — see below); the
  milestone-12 per-modifier analytics trace.

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

## Milestone 6c — monster kinds & difficulty rings

- **6c — DONE** (this PR): replaces the single anonymous monster
  (`internal/game/content.go`'s old flat `protocol.MonsterMaxHP`/
  `MonsterAttackDamage`/`MonsterXP`/`DropChancePercent`) with a **registry
  of 5 monster kinds** (`monsterDefs`, mirroring `itemDefs`): rat, wolf,
  ghoul, troll, dragon — each with its own `maxHP`, `damage`, `xp`,
  `aggroRadius` (overrides the shared `MonsterAggroRadius` default; 0 = use
  it), `dropChance`, and its **own weighted loot table**. **wolf carries the
  exact pre-6c flat numbers forward** (10 HP, 3 damage, 20 XP, aggro 10,
  30% drop, the original starter-drop table in its original order/weights),
  so existing balance and nearly every seeded test survived unchanged.
  Validated at process init (`validateMonsterDefs`, the same
  `mustValidateContent` idiom as items): unique ids, drops reference
  registered items, every ring covered by ≥1 kind, `aggroRadius` is 0 or
  strictly > `CombatRadius`.
  **Loot moved fully monster-side**: `itemDef.dropWeight` and the global
  `dropTable`/`protocol.DropChancePercent` are deleted; `dropLootLocked`
  rolls the slain kind's own `dropChance` and draws from its own table
  (`pickDropFrom`).
  **Combat reads the kind**: `closeDefFor`'s monster branch returns the
  kind's own claws profile (built once per kind at init); kill XP sums the
  slain kinds' `xp` (not a flat per-kill constant); `killSummary` names the
  slain kind(s) in the chat/combat log ("a wolf was slain (+20 XP to
  everyone in the fight)", "a wolf and a troll were slain (+80 XP…)", "2
  ghouls were slain (+70 XP…)" — grouping handles non-adjacent repeats).
  New **`targetKind`** pipeline condition (victim is a monster of a
  specific registered kind id), validated against the registry. Ships the
  **Wyrmslayer Greatsword** (fighter, damage 4, ×1.5 vs dragons via
  `targetKind:"dragon"`, dragon-only drop) — the first designer gear card
  that needed monster kinds to exist.
  **Difficulty rings**: worldgen bands the map into `RingCount=3` distance
  bands from the origin (`ringOf`) — at the default `WORLD_RADIUS=24` that's
  ring 0 = 0–7 (home), ring 1 = 8–15, ring 2 = 16–24 (frontier); small radii
  degrade gracefully. `SpawnMonsters` distributes placements across rings
  weighted by each ring's walkable-candidate count (an area proxy, naturally
  terrain-aware) and picks a kind uniformly among the kinds registered for
  the chosen ring, capping dragon at `DragonCount=1` per world.
  **Sanctuary**: no hostile spawn within `SanctuaryRadius=5` of the
  origin — the seed of the future trade-hub recovery layer (plan §9) —
  folded into the existing player-proximity spawn guard's safe/unguarded
  fallback (a fully-sanctuary tiny map still places monsters via the same
  fallback tier that already handled a fully player-guarded tiny map).
  Wire: `Entity.MonsterKind` (the registry id, empty for players); a
  monster's `Name` is now its kind's display name ("Wolf", "Dragon", …)
  instead of always empty. Client: `entities.ts`'s `KIND_STYLE` gives each
  kind a distinct dot color + one-letter glyph (falling back to the old
  flat monster red with no glyph for an unrecognized kind — forward
  compatible); `window.game.positions` entries gain `monsterKind`.
  Covered by unit tests (registry validation, per-kind damage/XP/loot/aggro,
  `targetKind` + the Wyrmslayer pin, kill-summary text variants, ring math
  at real and tiny radii, sanctuary/dragon-cap/seeded-reproducibility),
  a `protocol.gen.ts` contract test, an HTTP integration test (kill a seeded
  **troll** specifically → its own XP + its own kind-naming announce, both
  over real SSE), and `client/e2e/kinds.spec.ts` (a 30-monster server proves
  ≥2 distinct kinds reach the client and render).
  **Deviations from the plan** (both noted inline in their commits): rat's
  `aggroRadius` is `CombatRadius+1` (7), not the spec table's flat 6 (6
  violates the aggro-radius invariant this file shares with
  `protocol.MonsterAggroRadius`); the per-kind aggro-radius wiring landed in
  the Task 2 commit (per-kind combat) rather than Task 1 (registry), to keep
  it grouped with the rest of "combat reads the kind."
  **Deferred**: monster-kind passives (the `rules` seam on `monsterDef`
  ships empty — zero cost until a card uses it), ring UI indicators,
  continuous spawning with density-tracks-players (this slice + the
  playtest-batch spawn guards are its prerequisite), terrain-blocked LOS,
  boss mechanics beyond stats, per-kind movement speeds, the sanctuary hub
  itself (only its monster-free zone).

## Milestone 10a — persistence & identity

- **10a — DONE** (this PR): the two launch gates from plan §8's "10. Polish
  & launch" — characters survive absence, the world survives restarts — plus
  identity as a copyable link, settling plan §9's identity question.
  **Character archive** (`internal/game/world.go`): a new
  `characterRecord{name, class, species, xp, items, closeSlot, rangedSlot}`
  and `World.archive map[string]characterRecord`. `sweepDisconnectedLocked`
  captures a swept player's record before deleting the entity, instead of
  discarding it (the old behavior — see the now-corrected "Disconnect
  cleanup" bullet below). `Join`'s order is now live token → reclaim
  (unchanged) → **archived token → restore** (new entity from the record, a
  fresh guarded spawn hex, `hp = maxHPFor(class, levelFor(xp))`, archive
  entry consumed) → unknown token → new character (unchanged). Party
  membership and personal quests are deliberately **not** archived — they
  dissolve/return to the board on sweep exactly as before (session-scoped
  social state, not progression).
  **World snapshot** (`internal/game/snapshot.go`, new): `snapshotVersion`
  gates the on-disk shape; `World.MarshalState()`/`RestoreState(data)`
  round-trip the persisted field set — every entity (players AND monsters —
  a restart must not respawn a healed, repositioned monster population),
  ground items, the quest board, the archive, and the turn/nextID/
  nextBubbleID/nextPartyID counters. Every JSON tag lives on a DTO in
  `snapshot.go`, never on the unexported `entity`/`quest`/`characterRecord`
  structs — disk and wire stay fully decoupled. A version/worldSeed/
  worldRadius mismatch returns an error; the caller logs and keeps the fresh
  world already under construction (no migrations pre-launch). Transient
  fields (path, attackTarget, pendingEquip, bubbleID, streams) are left at
  their zero value on every restored entity; a restored player's
  `disconnectedAt` is stamped to the **load time**, not its pre-shutdown
  value — pinned by `TestSnapshotRestoredPlayerGraceStartsAtLoad`, the
  spec's called-out risk (the grace must restart at load, or every restore
  sweeps instantly).
  **App wiring** (`cmd/rogue/app`): `Config` gains `SnapshotPath`
  (`SNAPSHOT_PATH`, default `""` = disabled) and `SnapshotInterval`
  (`SNAPSHOT_INTERVAL`, default 60s, must be positive). `loadSnapshot` runs
  before `world.Run` starts the control loop and skips the fresh-world
  `SpawnMonsters` call when a restore actually happened; `runSnapshotSaver`
  ticks on the interval; one final `saveSnapshot` runs after the HTTP drain
  on graceful shutdown. Writes are atomic (temp file + `os.Rename` in the
  same directory); any save/load failure logs and continues — never crashes
  the game loop.
  **Client** (`net/session.ts`, `main.ts`, `index.html`): a "copy character
  link" HUD button (hidden until joined) writes `<origin>/#t=<token>` to the
  clipboard with a "copied!" flash (`window.game.identityLink`). On load,
  `importIdentityFromFragment` — called as the very first statement in
  `main.ts`, before any other client logic — imports a `#t=<token>`
  fragment's identity to localStorage and strips it via
  `history.replaceState`: the token is never sent to the server (a URL hash
  never rides an HTTP request) and never lands in chat. An imported token is
  always treated as a returning player, so the start screen never shows for
  it.
  Covered by unit tests (`archive_test.go`: sweep→archive→restore round-trip,
  unknown-token unaffected, party/quest not archived; `snapshot_test.go`:
  full round-trip, transients zeroed, the load-time-grace risk, turn
  monotonicity, version/seed/radius/garbage-data mismatch gates;
  `config_test.go`, `app_test.go`: defaults, overrides, atomic
  save/load, the periodic saver ticking and stopping), an HTTP integration
  test (`test/integration/persistence_test.go`:
  `TestWorldSurvivesRestartCharacterSurvivesSweep` — two independent server
  instances sharing one snapshot file, a token rejoin over HTTP restoring
  XP/gear/name/class via the live-reclaim path, monsters/ground items/quest
  board matching, stable across 20+ repetitions), and
  `client/e2e/identity.spec.ts` (a second, genuinely blank browser context
  opening the copied link ends up as the same character, start screen
  skipped, fragment stripped; the copy button's visibility/label/flash
  cycle).
  **Deviations from the plan/design** (noted inline in their commits): (1)
  `entity.partyID` and a new `World.nextPartyID` counter are persisted
  (kept, not zeroed) even though the design's top-level field-list prose
  didn't itemize `nextPartyID` separately from `nextID`/`nextBubbleID` — persisting
  `partyID` without a collision-safe counter to go with it would have been a
  worse bug than the omission; (2) the app-level saver/loader tests
  (`cmd/rogue/app/export_test.go` + `app_test.go`) are new — the plan's
  "if the package has the pattern" caveat didn't apply (no prior app-level
  tests existed), so the `internal/game`-style export-wrapper pattern was
  introduced for this package too, to satisfy the `testpackage` linter while
  still reaching the unexported `loadSnapshot`/`saveSnapshot`/
  `runSnapshotSaver`.
  **Deferred**: bed/home spawns (plan §9 — restored/respawned characters
  still use the existing guarded random spawn, not a bed), SQLite-for-state
  (the decided later upgrade), snapshot migrations, multi-world, admin
  endpoints for state, encrypting the snapshot (the VPS disk is the trust
  boundary), rate-limiting join. Deployment itself (VPS/Caddy) is ops, not
  this slice, and remains open.

## Known placeholders / debt (all deliberate)

- ~~No gear/inventory yet~~ **since shipped**: gear/loot drops (6b.4), species
  passives (6b.3), the full inventory system (slots + backpack, PR #51). Still
  true from this era: no **terrain-blocked LOS** for ranged (distance-only),
  and killed monsters **do not respawn** (fixed pool depletes; continuous
  spawning is later). `protocol.PlayerAttackDamage` was an orphaned constant
  here — since removed (melee uses class weapons).
- ~~`spawnHexLocked` is faction-blind~~ **since guarded** (the #36 fix):
  `spawnHexLocked` now prefers hexes not occupied by — or within
  `CombatRadius` of — a living monster, falling back through tiers to the
  old faction-blind spiral only as a last resort (`world.go`'s
  `tooCloseToMonsterLocked`/`occupiedByMonsterLocked`). **6.4
  note (still relevant):** with time-bubble domain scoping, a joiner/respawn near an active
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
- **Disconnect cleanup (issue #21, DONE; superseded by milestone 10a's
  archive)**: a player's entity is **removed from the live world after its
  event stream has been gone for `DISCONNECT_GRACE`** (env, default 20s). The
  SSE stream is identified by `/api/events?token=<token>`; the world tracks a per-
  player live stream count and sweeps players with no stream past the grace (in
  the `pollTick` control loop). A reconnect (stream reopens with the same token)
  within the grace **keeps** the character; the client also re-joins if its entity
  was swept during a long absence. **Update (10a): the sweep no longer deletes
  the character — it archives identity/XP/gear (`World.archive`), and a
  rejoin with the same token restores it** (fresh spawn hex, full HP,
  progression intact), settling §9's offline-character policy for real:
  pop-in/pop-out play is no longer destructive. Party membership and any
  personal quest still do NOT survive a sweep (session-scoped social state).
  See the milestone 10a section above.
- **E2e per-spec servers (now redundant, kept for safety)**: `playwright.config.ts`
  still gives **every spec its own single-consumer server** (a project + webServer
  per spec file, DRY over a `specs` list; `MONSTER_COUNT` only where needed) — the
  6b.2 mitigation for cross-spec entity accumulation. With disconnect cleanup that
  accumulation is fixed at the root, so this could be simplified back to a shared
  server (with a short `DISCONNECT_GRACE`) as a follow-up. Add a new e2e spec to
  the `specs` list for now.
- ~~No explicit wait input~~ **since shipped**: SPACE = an explicit wait
  intent (see FEATURES §Movement; `wait_test.go`).
- ~~No combat-bubble "waiting for: …" timer state~~ **since shipped**:
  `BubbleView.WaitingForIDs` + `PatienceRemainingMs` on the wire and the
  "In combat — waiting for: …" panel in the client (see FEATURES §Time).
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
