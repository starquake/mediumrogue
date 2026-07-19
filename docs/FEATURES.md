# Medium Rogue — implemented features reference

*Everything that actually exists in the game as of 2026-07-13 (main through
the inventory system (PR #51), the three-environment deployment, the
fast-lane batch — quadratic XP curve, front-loaded HP curve, level-free
damage, the additive percentage fold, sanctuary-scatter spawn/respawn, and
the first two crit%-weapons — and the gear keystone (#55/#56): weapon tags
replace class-shaped weapon types, hand slots replace per-class weapon
slots, class equip gates are dropped, weapons are rebalanced, the game's
first two-handed weapon ships, and combat resolves every fitting held
weapon as its own hit).
Design rationale lives in `design.md`; the content-design
vocabulary in `content-authoring.md`.
This file is the what-is-real summary: mechanics, systems, knobs.*

---

## 1. Game mechanics (what players experience)

### Time: WeGo turns & combat bubbles
- One shared **world turn every 4 s** (2 s input window, ~2 s playback).
  No input = stand still; queued click-to-move paths auto-advance. Latency
  and reflexes are irrelevant by design. The input window is client pacing,
  not a server deadline: an intent that arrives while a turn is resolving
  is still accepted and simply applies to the **next** turn (#99).
- **Combat time bubbles**: when a player and a hostile come within 6 hexes
  **and can see each other** (#95 — see line of sight below),
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
  reinforcement is a core mechanic; fleeing beyond the radius **or breaking
  line of sight** escapes it.
- **Line of sight (#95)** — terrain decides who can spot whom, so ducking
  behind a rock is a real way to avoid or end a fight:

  | Terrain | Effect on sight |
  |---|---|
  | **Rock** | hard-blocks — a single rock hex on the line ends the ray |
  | **Forest** | **softens**: each forest hex on the line costs `ForestSightCost` (2) hexes of effective range |
  | **Water** | unwalkable but **transparent** — you can see across a lake |
  | **Grass** | open |

  Only what lies **strictly between** counts, so adjacent entities always see
  each other and standing in forest never hides you from something already
  next to you. The check is **symmetric** — never "it sees you but you don't
  see it". Against `CombatRadius = 6` that reads: 6 hexes over open grass,
  ~4 through one belt of trees, ~2 through two.

  It gates **two** things: bubble formation (and dissolution — losing sight
  ends a fight, since bubbles are rebuilt from scratch every tick) and
  **monster aggro**, over that monster kind's own reach rather than
  `CombatRadius`. Aggro-range gear (#88) and sight are **independent gates**:
  the gear fold decides how far a monster could notice you, sight decides
  whether terrain lets it. The **leash is deliberately exempt** — a monster
  walking home ignores players entirely, sight or no sight. Ranged attacks
  remain distance-only (no LOS) by design; changing that is its own slice.
  The "In combat — waiting for: …" panel names the stragglers by **display
  name** (item 3, playtest batch 3 — was raw entity ids), mapped client-side
  from the bundle's entities with a `#id` fallback for an unknown id.

### Movement
- Flat-top hex grid, axial coordinates, grid-locked. **Click-to-move**
  (server BFS pathfinding, one hex per turn, re-validated each turn) or
  **QWE/ASD** keys for single steps. Up to **5 friendly entities stack** per
  hex (a full party moves as one blob; count badge rendered).
- **A blocked walk detours instead of stalling** (#96): when a queued path's
  next hex is closed — hostile-held, or same-faction at `StackCap` — a
  **player** re-routes around it and still advances that turn. Only occupancy
  ever blocks a step: terrain is generated once and never mutates, so a hex
  that was walkable when you clicked still is. The re-route aims at the
  route's own last hex, so a walk already trimmed to stop adjacent to a
  hostile keeps that endpoint. Two guards: a detour more than
  `RepathDetourSlack` (**4**) hexes longer than the route it replaces is
  refused — blockers are transient, and waiting a turn beats hiking around a
  chokepoint — and a re-route whose own first step is blocked is refused too,
  so a full or hostile-held hex is never walked into. Refused either way =
  the old behavior: wait, path retained, no give-up. **Monsters never
  detour** — their wait is how a standing intent becomes next turn's melee
  attack, and they already re-path from a retained goal every turn.
  In practice the hostile-blocked case is a **bubble** scenario (a multi-hex
  flee path queued inside a fight): out in the world any monster within
  `CombatRadius` forms a bubble, which hard-cancels a multi-hex route (#103)
  before a hostile could stand on its next step.
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
  **strong red** (adjacent hostile = melee attack (an attack intent)); the
  equipped ranged weapon's full reach is washed **light red** (click shoots
  when a hostile is there; anywhere for AoE); clicking your own hex
  waits/cancels. Reach is a BFS with `COMBAT_MOVE_RANGE = 1` (client),
  structured for future run/jump.
- **Attack-target highlights** (#101): hovering a tile a click would attack
  lights up — in **ember orange**, its own layer above the reach tints — the
  tile(s) that attack would actually hit: the single victim tile for a melee
  swing or a bow shot, the **full blast disc** (every walkable hex within the
  weapon's `aoeRadius`) for a ground-targeted AoE like the mage's Ember Focus.
  Clicking keeps the same tiles lit, stronger, **until the turn resolves** —
  the on-map half of the committed/pending indicator, alongside the crosshair.
  Mirrors the click routing exactly (an adjacent hostile is a melee swing
  unless an AoE weapon reaches; otherwise every held weapon whose own range
  covers the target fires), and answers "what will THIS action hit" where the
  reach tints answer "where can I act". A preview only — the server re-checks
  on resolution. `window.game.hoverAttackTiles` / `.committedAttackTiles` /
  `.hoverTile(q, r)`.
- **World hover highlight** (#135): **out of combat**, hovering a hex a click
  would act on lights up **that single tile** — **pale ice** for "walk here",
  **parchment** (the committed-wait colour) for your own hex, whose click is a
  wait/cancel; **rock and water light nothing**. It's the same "if a click does
  something, hovering shows it" affordance the #101 ember gives attacks, for the
  walk click that had no hover feedback before. **World-only**: in combat the
  blue/red reach tints and the #101 ember already answer it, so the highlight is
  suppressed there. Walkability is **terrain-only** (no reachability BFS) — a
  walled-off walkable island still lights, and the click then fails gracefully
  server-side like any unreachable target (accepted false positive). Drawn under
  the attack layer; `window.game.hoverMoveTile` (`{ hex, kind: "walk" | "wait" }`
  or null) / `.hoverTile(q, r)`.
- **Entering a combat bubble hard-cancels a queued auto-walk** (#103): the
  server clears a remaining **multi-hex** route on the world→bubble
  transition, and the client drops the walk goal and its destination ring on
  the same bundle. What survives: a path queued *inside* a bubble (fleeing),
  a path carried across a bubble merge, and a **single remaining step** —
  that's a deliberate adjacent move. Every
  in-combat move is otherwise a fresh, deliberate intent; after the fight,
  click the destination again.

### Combat
- **No separate combat screen** — same map, same intents. **Melee**: click
  (or key-step into) an adjacent enemy to swing — an entity-targeted attack
  intent (#116), one click per swing, and attacking never moves you;
  monsters still fight by moving into you (the classic roguelike
  bump-to-attack is now the monsters' rule); ranged **attack intent** (bow
  single-target, mage AoE radius 1), range 4 hexes, distance-only (no LOS —
  bubbles and aggro use LOS since #95, attacks deliberately don't),
  **no friendly fire**.
- **Entity-targeted single-target ranged attacks** (item 7, playtest batch
  2): a bow shot names its victim by **entity id** (`IntentRequest.
  targetEntityId`), not a hex — clicking a hostile in range sends its id.
  Melee shares this entity-targeted intent at distance 1 (#116). An AoE cast
  (mage) stays **ground-targeted** (a hex — the blast radius makes that the
  natural target).
  Validated at submit (entity exists+alive, hostile, in range); resolution
  (#104) runs against **pre-move positions**, so a committed shot always
  lands — the `out_of_range` fizzle survives only as a defensive guard
  (nothing moves between submit and the attack phase).
- **Phased resolution** (#104, attacks-before-moves): all attacks resolve
  simultaneously against **pre-move positions** (shared damage map — mutual
  kills are possible and intended; a stacked hex takes hits on a **random
  member**), then all moves resolve (seeded-RNG tie-break on hex overflow;
  an entity killed in the attack phase does not get its move). Committing
  to an attack always lands it; retreat means **trading hits for distance**
  — a one-action chaser that strikes you isn't gaining ground that turn.
- Class weapon routing on click: a rogue **melee-attacks with the dagger when
  adjacent**, shoots the bow at range (weapon-by-distance identity); a mage
  **blasts even adjacent** targets (staff bonk exists but its ranged magic is
  its real weapon); fighters are melee-only.
- **Feedback**: instant destination ring on walk clicks, one-shot flash on
  attack clicks — **including melee attack clicks** (#113: a melee attack
  always lands as a committed attack since #104, so clicking an adjacent hostile
  shows the attack flash + crosshair, never the walk ring/marker; the
  crosshair mouse cursor covers melee tiles too; `window.game.lastAttackFlash`
  records the most recent flash target for e2e), **pending item-action feedback** (equip/unequip/drink/drop) — an
  on-map ⇄ **swap glyph** on my own hex (drawn above the entity layer, with a dark
  rim) plus a small **amber spinner** on the pending button (the item stays
  named, not a bare "…"), shown from the click until the turn bundle applies the change and cleared
  then; **combat-agnostic** — the pending set drives it, not the clock (out of
  combat it clears on the next tick, in a bubble it persists until the bubble turn
  resolves; same mechanism either way; `window.game.pendingItems`,
  `FeedbackLayer.setItemAction`); a separate **pickup glyph** (a down-into-backpack
  arrow, not the ⇄ — a pickup isn't a gear swap; drawn above the entity layer on my
  hex) shows from a ground-item take until the next bundle resolves it, plus a
  spinner on that row's "take" button in the pickup modal
  (`window.game.pickupPending`, `FeedbackLayer.setPickup`), Diablo-style **floating damage numbers** (white over hostiles, red over players; killing blows shown as
  remaining HP — derived client-side by diffing bundles), **crit and glance
  moments** (#114 — a crit rises bigger, **gold**, with a "!" and a one-shot
  burst ring on the victim; a glance rises smaller, **pale steel** and italic;
  styled from the bundle's per-hit `Hits` view, since an HP delta alone can't
  tell a crit from a big hit. The delta stays the authoritative number — crit
  styling wins when a victim took both kinds in one bundle. Purely cosmetic;
  `window.game.hits`, `window.game.damage[].crit/.glance`), **committed-action
  indicator** (item 6, playtest batch 2 — a solid step marker for a queued
  move, a persistent crosshair for a queued attack, **ranged or melee** (#113),
  a small hourglass on my
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
| **Rogue** | Dagger (4) + Shortbow (4, rng 4) | 16 | high single-target, squishy; glance% passive — 20% chance an incoming hit is halved |
| **Mage** | Oak Wand (2 bonk) + Ember Focus (3, rng 4, AoE 1) | 14 | area damage, back line |

Default kits are granted into the hand slots (main-hand/off-hand) at join
time via the same placement path a player's own equip intent uses — not a
class-shaped weapon-slot special case (gear keystone, #55/#56).

| Species | Passive (pipeline rule card) |
|---|---|
| **Human** | +1 skill point per level (#124 — was +50% XP until then; see below) |
| **Elf** | 20% chance any hit crits ×2 |
| **Dwarf** | −1 damage taken per hit (floor 1) |

### Skills (#124)

Three trees — **Class** (per class), **Adventure** and **Survival** (shared) —
built on the same pure-data rule cards as gear and species passives. A skill
is content, not machinery: learning one adds its cards to your folds, and the
pipeline cannot tell them apart from a sword's.

| Skill | Tree | Card |
|---|---|---|
| Combat Training | Class | `deal-damage` ×1.10 with a **melee-tagged** weapon |
| Weak Spot | Class | `deal-damage` +4 vs a full-health target (requires Combat Training) |
| Shield Wall | Class | while a shield is in the off-hand, **15% chance** an incoming hit only glances |
| Crusher | Class | `deal-damage` ×1.10 on **blunt** hits (#57) |
| Kindler | Class | `deal-damage` ×1.10 on **fire** hits (#57) |
| Twin Fangs | Class | `deal-damage` ×1.10 while **dual-wielding** (#57) |
| Wand Chorus | Class | `deal-damage` ×1.15 on fire hits while dual-wielding (requires Twin Fangs) (#57) |
| Scouting | Adventure | `aggro-range` ×0.8 — renders as `−20% Aggro Range` |
| Survivalist | Survival | `take-damage` ×0.9 — the tree's root (#57) |
| Hardy | Survival | `take-damage` ×0.85 below 40% HP (requires Survivalist) (#57) |

- **The Survival tree is defensive/attrition** (settled #57, 2026-07-19). It
  shipped empty in v1, which meant a player could spend points into a tree with
  nothing in it; `TestSurvivalTreeIsNotEmpty` now fails loudly if any tree is
  emptied again.
- **Damage-type skills stack by SUMMING, never compounding.** Combat Training
  plus Crusher on a blunt melee weapon is +20%, not ×1.21 — percentages sum
  within one fold and apply once. Pinned by
  `TestCrusherAndCombatTrainingSUMRatherThanCompound`, because "stacking" is
  exactly where a reader assumes multiplication.
- **`dualWielding`** (#57) gates on the ATTACKER holding a weapon in **both**
  hands. A two-handed weapon is **not** dual-wielding — it occupies both slots
  but is one weapon, so the condition counts weapons rather than filled slots.

- **Active skills** (#161): a skill is **passive** (rule cards) or **active**
  (a trigger + cooldown) — never both, rejected at content load. An active is
  the turn's action, exactly like a move: not a bonus action, and it displaces
  a queued move or attack.
  - **Blink** — Survival tree, 3 hexes, **3-turn cooldown**. The destination
    needs range, walkability **and line of sight**: it does *not* pass through
    walls, deliberately unlike the classic ARPG blink, so cover stays real.
  - **Cooldowns count TURNS, whichever clock is ticking.** A bubble turn is
    slower in wall-clock than a world turn, and that dilation is the bubble's
    point — a turn-denominated cooldown rides it instead of fighting it. A
    seconds-denominated one would run *through* bullet time and break it.
  - Cooldowns **persist** (`snapshotVersion` 7), so a server restart is not a
    free reset.
  - Wire: `IntentUseSkill` with a skill id + target hex. Rejections are 422 —
    not learned, not an active, on cooldown, out of range, not walkable, no
    line of sight.
  - **No client UI yet**: no button, no keybind. Reachable only by POSTing the
    intent directly until #161's client half lands (blocked on a palette
    decision; the action bar is #185).
- **Points**: `SkillPointsPerLevel = 3` per level, `HumanBonusSkillPoints = 1`
  extra for Humans. The grant works off a persisted high-water mark, not a
  level-up event (the engine has none — level is derived from XP), so dying
  and re-earning a level never re-pays.
- **Cost**: `SkillPointCost = 3` per skill, uniform across passives and
  actives. Raised from 1 together with the per-level grant (2→3) so the change
  stayed a *pacing* change: at 2/level a 3-point cost would have turned the
  Human +1 into a third of a skill every level instead of a rounding
  difference, handing Humans their first skill a level earlier than everyone
  else. Both moved, so every species still affords one skill per level and the
  Human perk stays a spare point rather than a head start.
- **Learning is out-of-combat only.** Unlike the five inventory actions it is
  *not* queued as a bubble turn — it's rejected in combat (422). Learning is a
  between-fights decision.
- **Near-sighted (#61's proposal, settled in #124)**: you see what you have
  learned and what you can learn **next** — never the whole tree, never a
  locked capstone. This is enforced **server-side**: a locked skill is not
  hidden by the client, it is never sent. Stumbling onto a capstone is the
  intended experience; planning a build from a rendered graph is the thing
  designed out.
- **Own-only on the wire**: your skills and unspent points reach your own
  client and nobody else's (`SnapshotFor` renders a bundle per viewer).
- **No respec in v1.** Points are permanent once spent.
- Prerequisites are **same-tree only** — one tree's progression may never gate
  another's (#61 principle 5), enforced at content load.
- Panel: `k` (not `s` — that's south), default closed, toggles independently
  of the character panel.

### Item and skill text (#171)

Every mechanical line a player reads is **rendered from the rule cards**, never
authored beside them. Authored text is **flavor only, and carries no numbers**
— a load-time check rejects a digit in flavor, because a hand-written "blocks
2 damage" is exactly how prose and mechanics drift apart once the card changes.

The vocabulary:

| Shape | Renders as |
|---|---|
| defensive card (`take-damage`) | **resistance** — `+50% Chaos Resistance`, `+20% Damage Resistance` |
| offensive card (`deal-damage`) | **damage** — `+10% Melee Damage`, `×2 Damage vs Adjacent` |
| utility card (`earn-xp`, `aggro-range`) | names its own subject — `+5% XP`, `−20% Aggro Range` |
| base stats (not cards) | `Damage 4`, `Range 4`, `AoE 1`, `+5 HP`, `Stacks to 5` |

Resistance carries its own direction, so nothing is inferred from which slot
an item occupies, and there is no double negative to decode. Percentages show
as a **delta** (`+50%`, not `×1.5`) because percentages *add* within a fold —
the number shown is the number that stacks.

**A drawback is flagged and styled apart**, because sign alone cannot say:
`+25% Aggro Range` is a cost, `+5% XP` is not. Iron Plate Armor carries one of
each — `+20% Damage Resistance · +25% Aggro Range` — which is what the flag
exists for.

**Item nature is enforced at load**: a `deal-damage` card belongs on a weapon,
a `take-damage` card on worn kit, and a mixed item panics at process start.
Utility cards are exempt. The nature lives on the item's *type*, not its slot,
because the off-hand takes both a shield and a dual-wielded weapon.

### Progression, XP & death
- XP from kills: **every player in the bubble gets the full amount per kill
  as it happens** — no split, no kill credit, no battle-end payout. Quest
  completions pay all current holders in full. **Quadratic curve** (fast-lane
  batch, #60 XP1): total XP to **reach** level L is `XPCurveBase * (L-1)^2` —
  fast early levels (100, 300, 500 XP for L2/3/4 — gaps grow linearly),
  steep later. **Front-loaded HP curve** (#60 XP2): the max-HP gain when
  advancing from level n is `max(HPGainMin, HPGainBase-(n-1))` — 8, 7, 6, …
  falling to a floor of 1 XP per level forever. **Damage no longer scales
  with level at all** (#60 XP3, `DamagePerLevel` cut): a weapon's damage is
  its content-data base plus any rule-card modifiers, full stop — levels
  give HP only — **plus skill points** since #124 (`SkillPointsPerLevel = 2`,
  and a Human banks `HumanBonusSkillPoints = 1` more).
- **Death**: XP falls to the start of the current level (levels never
  lost — the "level start" floor is level-aware under the quadratic curve
  too), respawn at full HP with the **same identity and all gear** (gear
  always survives death — decided). Respawn location scattered across the
  **sanctuary** (any walkable, capacity-available hex within
  `SanctuaryRadius`, guarded against landing on/adjacent to a living
  monster — same `spawnHexLocked` tiers as a fresh join, Q9); the camera
  **cuts** to the respawn instead of panning.
- **Passive regen**: +1 HP per world turn while out of combat (never in a
  bubble, never above max). Removes death-as-the-only-heal.
- **HUD stats line** (item 9, playtest batch 2; XP portion reworked for the
  quadratic curve, fast-lane batch): `Lv L · (xp into this level)/(XP needed
  this level) XP · (q, r)` — my entity's hex, live per turn bundle.
- **Client liveness on the HUD** (#170): the stats line ends with the last
  turn the client **received**, and — only when the client has fallen behind
  — a `⚠ stuck` marker. Two counters make that visible: `turnReceived` is
  stamped on the FIRST line of the turn handler, `turnApplied` on its LAST,
  so a bundle that throws mid-apply advances one and not the other. A gap
  greater than 1 turn shows the marker. (#167 froze the client exactly this
  way while `turn` — assigned early in the handler — kept counting up, so
  the HUD read healthy over a dead map.)
- **Client error banner** (#170): `window.addEventListener("error")` and
  `"unhandledrejection"` put a red banner across the top —
  *the client hit an error and stopped updating — reload the page (…)* —
  carrying the message. An uncaught client exception is now visible in the
  UI instead of only in a console the player never opens, and the text is
  what they paste into a bug report.

### Gear & inventory (milestone 6b.4, loot 6c, inventory system: slots/backpack/drop/pickup/drink; gear keystone #55/#56: weapon tags, hand slots, gates dropped, rebalance + first 2H)
- **9-type item taxonomy** (`internal/protocol`'s `ItemType*` consts): one
  `weapon` type — carrying **tags** (`WeaponTagMelee` / `WeaponTagRanged` /
  `WeaponTagMagic`, which attacks fire it — a weapon needs ≥1) plus a
  `twoHanded` bool — replaces the old five class-shaped weapon types
  (`melee-weapon/thrown-weapon/ranged-weapon/staff/wand`); a `consumable`;
  a `shield` (#90 — occupies the off-hand; never fires as a hit, its −N is
  a `take-damage` rule card); and six armor/jewelry types that each map 1:1
  to a slot: `helmet`, `chest`, `gloves`, `boots`, `ring`, `amulet`. Only a
  weapon def may set `damage`/`rangeHex`/`aoeRadius` — a combat stat on any
  other type panics at load (`validateItemCombatStats`).
- **Eight equip slots** (`Slot*` consts): `main-hand` and `off-hand` — chosen
  at equip time, not fixed per class — plus the six armor slots above. A
  weapon's landing hand is `weaponTargetSlot`: two-handed, or an empty
  main-hand → main-hand; else an empty off-hand → off-hand; else main-hand
  (a swap, evicting the current occupant back to the backpack, rejected if
  the backpack has no room). Empty hands fall back to unarmed fists
  (`FistsDamage`). A **shield equips into the off-hand only** (#90):
  equipping one evicts a two-handed main-hand weapon to the backpack
  (room-checked first — rejected politely if full); equipping a two-hander
  evicts the shield the same way; a one-hander swaps **main** and leaves the
  shield in place. A consumable has no slot (backpack-only). Backpack stays
  **exactly 4 entries**: a gear instance or a consumable stack (identical
  consumables merge, up to 5; stacks never split) per entry.
- **Class equip gates dropped (#55/#56)** — any class may equip any item.
  `wearableBy` and the `ErrWrongClass` rejection are gone entirely; class
  identity now comes from starting kits (below) and, later, skills — not
  equip-time restrictions.
- **Two-handed weapons**: `TwoHanded=true` occupies main-hand **and** locks
  off-hand — equipping one evicts whatever the off-hand held back to the
  backpack (rejected if full); the off-hand hex greys out while it's locked.
  The Wyrmslayer Greatsword was the game's first two-handed weapon; the two
  magic staves are also two-handed since the keystone amendment.
- **Dual-wield / per-hit combat (#55 task 2)** — every fitting **held**
  weapon fires as its own hit, not just one "best" weapon: a melee attack
  resolves every melee-tagged held weapon against the same picked victim; a ranged
  attack resolves every ranged/magic-tagged held weapon that still reaches
  the target. Two single-target ranged weapons dual-wielded into a stacked
  hex **share one stack-victim pick** (mirroring melee's one-victim-then-
  every-weapon rule) rather than splitting the stack across independent rng
  draws — each AoE (magic) weapon still hits every hostile within its own
  `aoeRadius` independently, no shared pick needed. A single-weapon attacker
  (the common case, and every class's starting kit) is unaffected — this is
  byte-identical to the old single-hit resolution when exactly one weapon
  fires. **Wands are one-handed, staves are two-handed at doubled damage**
  (keystone amendment, 2026-07-13) — Ember Focus is now the game's only
  one-handed magic weapon, so dual-AoE stacking via dual-wielding two staves
  is no longer possible (a two-handed weapon locks the off-hand).
- **Weapons — content data** (`internal/game/content.go`'s `itemDefs`, 15
  registry weapons; rebalanced to the keystone's "1H ≈ ½ 2H" pass —
  several 1H damages moved down, the 2H anchor moved up). **Naming
  convention** (keystone amendment, 2026-07-13): **wands are one-handed,
  staves are two-handed at doubled damage** — the mage's starter Oak Staff
  was renamed **Oak Wand** to match (id/name only, stats unchanged), and
  Ember Staff / Staff of the War Mage both doubled from 3 to 6 damage and
  now occupy both hands:

  | Weapon | Tags | 2H | Dmg | Rng | AoE | Effect | Source |
  |---|---|:---:|---:|---:|---:|---:|---|---|
  | Iron Sword | melee | | 4 | – | – | — | fighter default |
  | Dagger | melee | | 4 | – | – | — | rogue default |
  | Shortbow | ranged | | 4 | 4 | – | — | rogue default |
  | Oak Wand | melee | | 2 | – | – | — | mage default |
  | Ember Focus | magic | | 3 | 4 | 1 | — | mage default |
  | Butcher's Cleaver | melee | | 3 | – | – | +3 dmg vs targets below half HP | starter drop (rat) |
  | Iron Warhammer | melee | | 5 | – | – | flat upgrade over Iron Sword — rare | starter drop |
  | Venom Fang | melee | | 3 | – | – | +4 dmg vs targets at full HP | starter drop |
  | Pack Bow | ranged | | 3 | 4 | – | +3 dmg while an ally shares the bubble | starter drop |
  | Ember Staff | magic | ✅ | 6 | 4 | 1 | ×2 dmg vs adjacent targets | starter drop |
  | Ancient Dwarven Mattock | melee | | 4 | – | – | +3 dmg in a dwarf's hands | designer batch |
  | Staff of the War Mage | magic | ✅ | 6 | 4 | 1 | ×2 dmg vs targets below 6 HP (flat) | designer batch |
  | Wyrmslayer Greatsword | melee | ✅ | 9 | – | – | ×1.5 dmg vs dragons | dragon drop (100%, weight 2) |
  | Misericorde | melee | | 4 | – | – | 15% chance to deal ×2 | ghoul drop |
  | Duelist's Saber | melee | | 4 | – | – | 10% chance to deal ×2 | wolf drop |

  `Rng`/`AoE` "–" = 0 (adjacent-only / single-target). Misericorde and
  Duelist's Saber are the first item-side **crit%-weapons** (fast-lane
  batch, #69 Q5) — the elf-crit `deal-damage`+`chance` card pattern applied
  to gear instead of a species passive; both are now equippable by any
  class (gates dropped) though the "rogue"/"fighter" naming is a flavor
  holdover from before #56.
- **Shields (#90, S4 of #55)** — the trade: a shield holds your off-hand
  (~half of dual-wield's melee output) in exchange for a flat `take-damage
  −N` on **every** hit, floor 1 (`applyRules`' event-level clamp); the −N
  stacks additively with Leather Armor's −1 and the dwarf passive's −1
  inside the same take-damage fold. Pure rule-card content — no new
  pipeline event, no `chance` roll (rng untouched). Drop-only (no class
  starts with one); richer defence (active block/evasion) is deferred to
  #69, shield skills to #57:

  | Item | Type | Card | Source |
  |---|---|---|---|
  | Wooden Buckler | shield | take-damage ×0.9 | rat (w1) / wolf (w4) drop |
  | Iron Kite Shield | shield | take-damage ×0.8 | troll (w4) / dragon (w1) drop |

- **Noticeability gear (#88)** — the first content to use the pipeline's
  `aggro-range` event: gear that changes how far off a world monster notices
  you. The fold is **multiplicative**, applied to each monster **kind's own**
  radius (so a rat's 7 and a dragon's 12 both move by a quarter rather than
  flattening to one number), and clamped **≥1** by `applyRules` — a monster
  can always eventually notice you. Noticeability is **gear-only** by design:
  a choice you make in the inventory, not a species you're born into.

  | Item | Type | Card(s) | Reach vs a wolf (10) | Source |
  |---|---|---|---|---|
  | Padded Boots | boots | aggro-range ×0.75 | 7 | rat (w1) / wolf (w4) drop |
  | Iron Plate Armor | chest | take-damage ×0.8, aggro-range ×1.25 | 12 | troll (w4) / dragon (w1) drop |

  Iron Plate Armor is the game's **first tradeoff item**: strictly better
  mitigation than Leather Armor (×0.8 vs ×0.9) bought with a real cost — you are
  noticed sooner. Gear that is only ever better makes the inventory a sorting
  exercise; a cost makes it a decision.
- **Damage types (#92, DT1)** — every attack carries exactly **one** of six
  types, and resistances and vulnerabilities are ordinary `take-damage` rule
  cards gated on it (`damageType`). There is no resist subsystem: one
  condition kind serves every resist and vulnerability card ever written.
  Every weapon and every monster kind must carry a type, enforced at content
  load — an untyped weapon would silently dodge every resist card.

  | Family | Types | Carried by |
  |---|---|---|
  | Physical | **Sharp** | swords, daggers, bows, cleaver, venom fang, misericorde, saber; rat + wolf claws |
  | Physical | **Blunt** | fists, oak wand, warhammer, mattock; troll claws |
  | Elemental | **Fire** | ember focus, ember staff, war-mage staff; dragon claws |
  | Elemental | **Ice** | Frostbrand |
  | Metaphysical | **Holy** | Wyrmslayer Greatsword, Consecrated Mace |
  | Metaphysical | **Chaos** | ghoul claws |

  The families, and the Holy↔Chaos / Fire↔Ice **oppositions, are an
  authoring convention — not machinery**. All six types are mechanically
  flat; "a Chaos ghoul fears Holy" is a vulnerability card someone wrote,
  and the engine does not know the pair exists. Promotable to a real axis
  later only if content always ends up mirrored.

  **Monster vulnerabilities**: ghoul takes **+50% from Holy**, troll **+50%
  from Fire** ("trolls fear fire" — the identity the arc was pitched on).

  **Resist gear** (each halves exactly one type — situational where flat
  mitigation is always-on):

  | Item | Type | Card | Source |
  |---|---|---|---|
  | Infernal Chain Mail | chest | fire ×0.5 | dragon (w2) drop |
  | Warded Gambeson | chest | sharp ×0.5 | wolf (w3) drop |
  | Pilgrim's Mantle | chest | chaos ×0.5 | ghoul (w3) drop |

  Blunt deliberately has **no** resist: one card answering both physical
  types would be strictly better than either elemental resist, since nearly
  every early monster is sharp or blunt. New weapons: **Frostbrand** (ice,
  damage 4, troll w3) and **Consecrated Mace** (holy, damage 4, ghoul w3) —
  both sit at the shipped 1H anchor so the *type* is the point, not a stat
  upgrade riding along. A weapon's type shows as a **Type** line in the stat
  tooltip (character panel and pickup modal alike).
- **Non-weapon items**: Leather Armor (chest: take-damage ×0.9, floor 1), Iron
  Plate Armor (chest: take-damage ×0.8 + aggro-range ×1.25), Padded Boots
  (boots: aggro-range ×0.75), the three resist armors above (chest: one type
  halved each), Headband of Learning (helmet: earn-XP ×1.05), Healing Potion
  (consumable: drink +5 HP, stacks to 5), and the two shields above
  (off-hand: take-damage ×0.9/×0.8).
- **Drops are monster-side** (milestone 6c): each monster **kind** owns its
  chance-to-drop and its weighted table (`monsterDef.drops`); a slain monster
  rolls its own chance (10–100%) and picks from its own table (potions ride
  the rat/wolf tables at low weight; the Wooden Buckler rides rat w1 / wolf
  w4, the Iron Kite Shield troll w4 / dragon w1, the Padded Boots rat w1 /
  wolf w4 and the Iron Plate Armor troll w4 / dragon w1; the damage-type wave
  (#92) puts the Warded Gambeson on the wolf, the Frostbrand on the troll,
  Infernal Chain Mail on the dragon (the one kind whose claws are fire), and
  the Pilgrim's Mantle and Consecrated Mace on the ghoul — which **ends** the
  ghoul table's long-untouched streak, by design: the ghoul is where a player
  first *wants* a damage type). Items land
  on the death hex and render as map markers.
- **Five inventory actions, one rule** — free & instant out of combat, **your
  whole turn inside a bubble** (a later move/attack supersedes a queued
  action; bubble dissolve applies it):
  - **equip** — moves a backpack item into its slot (`weaponTargetSlot` for a
    weapon, its type for armor/jewelry), swapping any displaced occupant
    back into the vacated entry (a two-handed weapon may evict the off-hand
    too — see above). Naming an already-equipped item **unequips** it
    (toggle).
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
    ×3 · consumable"); **hovering a row reveals the item's details** (#139) in
    the **same stat tooltip the inventory uses** (`gear/StatTooltip`, extracted
    so the character panel and the pickup modal share one component) — name,
    damage / range / AoE, the rendered **stat lines** and `flavor`. Alongside the candidate,
    the tooltip **also shows a block for each equipped item it would be weighed
    against** — for a weapon that's **both hands** (you can dual-wield two 1H
    weapons; a 2H weapon replaces both), each labelled by slot — so you read the
    raw stats side by side. `GroundItemView` carries the same detail fields as
    `ItemView`; `window.game.pickupModal.rows` exposes `damage`/`rangeHex`/
    `aoeRadius`.
  - **drink** — a consumable: applies its heal (clamped to max HP) and
    decrements the stack; an emptied stack frees its entry.
- **Keybindings** — `C` or `I` toggles the character panel (either key,
  interchangeably), `Esc` closes it (a genuine no-op while already closed,
  never a toggle); all three share the movement keys' typing-focus guard
  (`client/src/input/keys.ts`), so typing "c"/"i"/Escape into chat or the
  join-name field never touches the panel.
- **Client** — a toggleable **paper-doll** panel (`C`/`I` keys + a HUD button
  + an in-panel × whose tooltip lists all three; default closed since it is
  large): the eight named hexes (Helmet, Amulet, Gloves, Ring, Main Hand,
  Chest, Off-Hand, Boots) on the approved ARPG mockup's Vitruvian layout —
  the off-hand hex **greys out** with a "two-handed grip" ghost label and is
  unclickable while a two-handed weapon occupies main-hand — plus a 4-cell
  backpack with stack counts and per-item drop buttons. Walking onto a hex
  with ground items opens a **pickup modal** — one row per item (name +
  type), an individual **take** button, inline backpack-full feedback on a
  rejected row (row stays pickable), and "Close — leave the rest" (reopens
  on hex re-entry). Monster loot and player drops behave identically.
- **Layout: 1920×1080 is the minimum supported viewport** (#105). The
  character panel anchors below the HUD's **measured** bottom edge
  (`--hud-bottom`, kept current by a ResizeObserver in `main.ts`) — the HUD's
  height varies (combat panel swaps in for the timer, copy-link appears after
  join), and a hardcoded offset used to let the grown in-combat HUD run
  underneath the open panel. It still fully covers the quest board by design
  (an inventory screen, not a peek), reserves the same bottom chat-zone
  allowance as `#left-panels` (taller content scrolls inside the panel), and
  the worst case (in combat, panel open, chat populated) is pinned by
  `client/e2e/layout.spec.ts` at exactly 1920×1080.
- **Hover stat tooltip** — hovering an equipped hex or a backpack cell shows a
  floating tooltip: the item's `damage`/`range`/`AoE` and its effect line,
  and — when a **different** item fills the slot the hovered item occupies
  **or would occupy** (`targetSlotFor`, the client's mirror of
  `weaponTargetSlot` — so a backpack weapon compares against the hand it
  would actually land in) — the delta vs that item (green `+N` / red `-N`),
  so a pickup can be weighed before equipping. Stat-less gear shows "No
  combat stats". Below the stats, an item's authored **flavor/lore** renders
  as dim italic (the `ItemView.Flavor` field, seeded from the gear cards'
  `Fantasy:` text — e.g. the Wyrmslayer's dragon Werdmullerix); flavor is
  cosmetic, never gameplay-affecting.

### Monsters (kinds & difficulty rings — milestone 6c)
- **Six kinds**, content data in `internal/game/content.go` (`monsterDefs`),
  each with its own stats, aggro radius, XP award, and loot table. A kind
  **names its weapon** in the item registry (#179) rather than carrying a copy
  of one, so reach, damage and damage type all come from a real item:

  | Kind | Ring(s) | HP | Weapon | Dmg | Reach | XP | Aggro | Drop chance |
  |---|---|---|---|---|---|---|---|---|
  | Rat | 0–1 | 4 | Claws | 1 | melee | 8 | 7 | 10% |
  | Wolf | 1 | 10 | Fangs | 3 | melee | 20 | 10 | 30% |
  | Ghoul | 1–2 | 16 | Talons | 4 | melee | 35 | 8 | 35% |
  | Kin Archer | 1–2 | 12 | Hunter's Bow | 3 | **3 hexes** | 30 | 8 | 30% |
  | Troll | 2 | 30 | Maul | 6 | melee | 60 | 8 | 50% |
  | Dragon | 2 (capped at 1 per world) | 60 | Dragon Jaws | 9 | melee | 150 | 12 | 100% (incl. the Wyrmslayer Greatsword) |

  **The Kin Archer is the first kind that attacks without closing** (#179). It
  shoots from up to 3 hexes — under the player Shortbow's 4, so player gear
  still out-ranges it — and needs line of sight to fire, the same raycast the
  aggro check uses. It **shoots at point-blank rather than backing off**: every
  entity moves one hex per turn, so a kiting monster could never be caught,
  which is a softlock rather than a difficulty knob.

  Monster natural weapons (`Claws`, `Fangs`, `Talons`, `Maul`, `Dragon Jaws`,
  `Hunter's Bow`) are ordinary registry weapons carrying `monsterOnly` — a
  load-time validator panics if one is ever reachable through a drop table or
  a class default. They carry **no rule cards**: a kind's own cards
  (a troll's fire vulnerability) belong to the kind, not to a weapon other
  kinds may share.

  Wolf carries forward the pre-6c flat numbers exactly. Each kind renders
  with a distinct on-map dot color (`entities.ts`'s `KIND_STYLE`) plus a
  **game-icons.net glyph** drawn dark on the dot (rat, wolf-head,
  shambling-zombie, troll, dragon-head, bowman for the Kin Archer —
  `GLYPH_ICON_SVG`, keyed by kind id; the source filename must equal that id);
  an unrecognized kind falls back to the flat monster red with no glyph.
  Players carry the same treatment — a class glyph (crossed-swords/hood/
  pointy-hat) on their relationship-colored dot. Icons are vendored inline SVG
  (Pixi v8 `Graphics.svg()`, no asset pipeline), licensed **CC BY 3.0**
  (credited on the start screen). A kill announces the kind by name (see
  Combat above).
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
- **Home tile + leash** (#102): every monster remembers its spawn hex as its
  **home tile**. A WORLD-domain monster that strays farther from home than
  its **leash radius** — `MonsterLeashMultiplier=2` × its own aggro radius
  by default, per-kind overridable via `monsterDef.leashRadius` (no kind
  overrides it at launch) — drops the chase and paths back home,
  **ignoring players until it arrives** (no re-aggro mid-return; walking
  within `CombatRadius` of a player still forms a bubble, though — bubbles
  are positional). **No heal** on return — a long pull leaves it wounded.
  On arrival (its home hex, or adjacent to it while the home hex is at
  `StackCap`) the flag clears and the same think pass re-runs the normal
  aggro check. Monsters inside a combat bubble ignore the leash entirely —
  a fight is a fight; the flag survives a bubble, so an interrupted return
  resumes if the bubble dissolves. Leash trips are logged as `combat`
  events (`event=leash`). Home + returning state persist in the snapshot
  (v5).

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
  batch 3 — worldId added at snapshot version 2; version 3 added the
  slot-keyed equipped map + backpack/stacks of the inventory system;
  **version 4** (the gear keystone, #55/#56) re-keys equipped weapon slots
  from class-shaped names to the hand slots `main-hand`/`off-hand` and
  collapses the five weapon item-types into one `weapon` type + tags/
  twoHanded; **version 5** (#102) adds each monster's home tile + returning
  flag — leash state is multi-turn behavior, not a per-turn transient;
  a restored world keeps its identity, see the world-reset signal
  below). The map itself is **never** persisted —
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
  (HTTP/SSE, security headers, same-origin guard on POSTs — see Wire) →
  `internal/game` (world
  under one mutex; per-domain turn loops). Coalescing hub: a tick means
  "fetch latest state", never a delta.
- **Wire**: POST `/api/join`, `/api/intent`
  (move/attack/equip/unequip/drop/pickup/drink), `/api/chat`;
  GET `/api/map` (once), `/api/events` (SSE: full-snapshot turn bundles with
  turn-number ids, chat events, named heartbeats). Reconnect =
  resync-to-latest (`Last-Event-ID` as watermark only). JSON everywhere.
  **Same-origin guard** (#97): every POST carrying a cross-origin `Origin`
  or a cross-/same-site `Sec-Fetch-Site` header is rejected with 403 ("same
  origin" is derived from the request's own `Host` — no configured origin).
  Requests with neither header (curl, the Go tests, some same-origin
  fetches) pass: this is defense-in-depth, the auth boundary stays the
  bearer-token-in-body.
  **Input-window semantics** (#99): intent acceptance is not clock-gated —
  the world mutex serializes submission against resolution, so an intent
  arriving while a turn resolves is accepted, never affects the resolving
  turn, and applies to the next one. The client's 2 s input window is
  pacing, not a server cutoff.
- **Per-hit combat moments on the bundle** (#114): `TurnEvent.Hits` is a
  `HitView[]` — `turn`, `attackerId`, `victimId`, `amount`, `crit`, `glance`
  — the crit/glance facts an HP delta alone can't express. Recorded in
  `rollDamageLocked` (the one choke point every damage number flows through)
  from the rule-pipeline fold's own trace: a **chance**-conditioned `mulPct`
  firing is the moment — a boost in the `deal-damage` fold is a crit, a
  reduction in the `take-damage` fold a glance (a *deterministic* multiplier
  is never a moment). Bundles retain hits for `hitRetentionTurns` (4)
  resolutions because SSE ticks **coalesce** — a slow client skips bundles
  and dedupes on `HitView.Turn`. Cosmetic only: `amount` is the damage
  already reflected in HP, never applied twice; records are transient (like
  `entity.path`), so the snapshot shape — and `snapshotVersion` — is
  unchanged.
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
  `aggro-range` (per-kind aggro radius folds through it since 6c; live
  content since #88 — the noticeability gear above; clamped ≥1).
  Conditions: `chance`, `targetHPBelowPct`, `targetHPBelowFlat`,
  `targetHPFull`, `allyInBubble`, `targetAdjacent`, `attackerSpecies`,
  `targetKind` (victim is a monster of a specific registered kind — 6c,
  validated against the monster registry), `damageType` (the damage type
  of the hit being folded — #92, renamed from `incomingType` in #155,
  validated against the six types; works both ways: on a take-damage card it
  is the type landing on you (every resist and vulnerability), on a
  deal-damage card it is the type of the weapon you are swinging, which is
  how weapon-flavoured passives like "+10% blunt damage" are expressed). Effects: `add`, `mulPct`. Fold
  order: all adds → **percentages add within the fold** (every `mulPct`
  card's delta from 100% sums into one combined percentage, applied with a
  single truncation — #61 principle 14, roadmap Q8, fast-lane batch) → event
  clamp (damage ≥1, XP ≥0); stages still compose across separate events
  (e.g. deal-damage → take-damage), each a true multiplier at its own stage.
  Sources: species cards + acting/equipped item cards — weapon **damage
  itself carries no level scaling** (content-data base + cards only, #60
  XP3). Content validated at process start (fail-loud). Every damage and XP
  number in the game flows through it. The fold is also **traceable**
  (`applyRulesTraced`, #114): it reports which **chance**-conditioned `mulPct`
  cards fired, which is how crit/glance reach the wire (see §2's per-hit
  combat moments). `applyRules` is a thin wrapper over it; tracing is
  observational — same card order, same rng draws, no arithmetic change.
- **Determinism**: per-resolution PCG rng seeded (worldSeed, turn); map
  iteration sorted before any rng draw; spawn randomness on separate
  fixed streams. Fully reproducible turns.
- **Testing surface**: unit tests beside code; `test/integration` drives the
  real handler tree over real HTTP/SSE; Playwright e2e drives the real
  embedded-client binary (46 e2e tests across 28 spec files). The client exposes **`window.game`**
  (positions incl. `monsterKind`, hp, inventory, equipped, backpack,
  panelOpen, pickupModal, combatMoves, damage events, tapHex, hexToScreen,
  sendChat, identityLink, turnReceived, turnApplied, clientError…) as the
  always-in-sync test/debug surface.
  **`client-alive.spec.ts` (#170) is the liveness guard**: it drives an
  unequip + re-equip and asserts `turnApplied` keeps advancing across it,
  deliberately NOT `turn` — `turn` is assigned early in the handler and kept
  advancing right through #167's freeze, so a guard watching it would have
  passed while the game was dead.
  `hexToScreen(q, r)` returns a hex's live viewport coordinates — the inverse
  of the canvas pointerdown mapping — so a spec can drive a REAL
  `page.mouse.click` (and so exercise overlay `pointer-events` hit-testing)
  rather than only `tapHex`'s synthetic path.
- **Designer content guide** (#156): `docs/content-guide/README.md` is
  **generated** — `make guide` renders it from the live registries via
  `cmd/contentguide`, so its vocabulary tables, calibration anchors and
  item/monster numbers cannot drift from the game. Stat lines come from
  `statlines.go`, the same path that fills a tooltip, so the guide and the
  client can never disagree. Only the prose (the coupling tell, the drift
  cases, the checklist) is authored, in `docs/content-guide/guide.md.tmpl`.
  **`make guide-check` runs inside `make check`**: a change to a number the
  guide cites fails the gate until the guide is regenerated — the FEATURES.md
  "values come from the code, never memory" rule made mechanical.
- **Dev loop**: `make dev` (watchexec auto-restart) + `make client-dev`
  (Vite HMR proxying /api); `make check` full gate (lint, protocol drift,
  typecheck, tests, build); `make e2e`.
- **Combat event log** (item 1, playtest batch 2 — `internal/game`,
  structured `slog`, the milestone-12 analytics seed): every resolution
  path emits `slog.Info("combat", "event",
  ...)` — `move`, `attack` (attacker, victim, weapon defID, base, dealt),
  `fizzle` (reasons: `out_of_range`, `unequipped`, `target_gone`,
  `pending_item_action`), `death`, `xp_award`, `pickup` (item defID, count),
  `drop` (item defID, count, hex), `drink` (item defID, resulting hp),
  `leash` (#102 — a monster trips its leash and heads home: id, kind,
  from, home) —
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
| `HPGainBase` / `HPGainMin` | 8 / 1 | front-loaded HP curve: gain advancing FROM level n = `max(HPGainMin, HPGainBase-(n-1))` — 8,7,6,…,1 then +1 forever (#60 XP2) |
| `XPCurveBase` / `QuestKillRewardPerTarget` | 100 / 20 | quadratic XP curve: total XP to **reach** level L = `XPCurveBase*(L-1)^2` (#60 XP1) & flat per-target kill-quest reward |
| `MonsterMaxHP` / `FistsDamage` | 10 / 1 | pre-6c monster baseline (wolf's HP) & unarmed profile |
| `ElfCritChancePercent` / `ElfCritMultiplier` / `DwarfDamageReduction` | 20 / 2 / 1 | species knobs (`HumanXPBonusPercent` retired in #124 — the Human perk is skill points now) |
| `SkillPointsPerLevel` / `HumanBonusSkillPoints` | 2 / 1 | skill points banked per level, and the Human's extra (#124) |
| `RogueGlanceChancePercent` / `GlanceDamagePercent` | 20 / 50 | Rogue class passive: chance an incoming hit is halved (never negated; floor 1 still applies) |
| `RegenPerTurn` | 1 | out-of-combat HP per world turn |
| `ForestSightCost` | 2 | hexes of effective sight range one forest hex between two entities costs (#95); rock hard-blocks, water is transparent |
| `MonsterAggroRadius` | 10 | default world-monster notice distance (> CombatRadius, compile-guarded); per-kind `aggroRadius` overrides it |
| `MonsterLeashMultiplier` | 2 | default leash radius = this × the kind's aggro radius (#102); per-kind `leashRadius` overrides the derived value |
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

Recorded in `design.md` §0/§8/§9, `design-decisions.md` (Q1–Q11
all decided 2026-07-13), and issue #36: the **rest of the skill trees**
(#124 shipped the system with four v1 skills; First Aid & Make Camp still
seed the Survival/Adventure trees), downed
state & revive, further recovery layers (rests, the sanctuary
**trade hub** — the 6c sanctuary zone is only the monster-free ground, not
the hub itself; healing potions + the backpack-cap layer now ship with the
inventory system), wand↔staff interactions, item destruction/durability, backpack
upgrades, trading, continuous spawning with density-tracks-players,
monster-kind passives (the `rules` seam on `monsterDef` ships empty), ring
UI indicators, path-preview breadcrumb, bed/home spawns
(model decided — see `design-decisions.md` (Q9): sanctuary-scatter first spawn and
respawn shipped; the future step is last-visited bed with Home fallback —
milestone 10a persisted characters and the world, but the bed slice stays
future), admin console & analytics log, SQLite-for-state (the milestone 10a
JSON snapshot is the decided interim store).

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
