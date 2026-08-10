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

  It gates **three** things: bubble formation (and dissolution — losing sight
  ends a fight, since bubbles are rebuilt from scratch every tick),
  **monster aggro**, over that monster kind's own reach rather than
  `CombatRadius`, and **ranged attacks** (#195 — a shot needs the same clear
  ray a bubble does, so a wall shields a target instead of leaving it a
  through-wall farming loophole). Aggro-range gear (#88) and sight are
  **independent gates**: the gear fold decides how far a monster could notice
  you, sight decides whether terrain lets it. The **leash is deliberately
  exempt** — a monster walking home ignores players entirely, sight or no
  sight. A ranged attack whose target is behind a wall is rejected at submit
  with `ErrNoLineOfSight` (422); melee (adjacent) is exempt — endpoints are
  never occluded.
  The "In combat — waiting for: …" panel names the stragglers by **display
  name** (item 3, playtest batch 3 — was raw entity ids), mapped client-side
  from the bundle's entities with a `#id` fallback for an unknown id.

### Movement
- Flat-top hex grid, axial coordinates, grid-locked. **Click-to-move**
  (server BFS pathfinding, one hex per turn, re-validated each turn). Movement
  is **click/tap only** — the old QWE/ASD single-step keys were dropped by the
  survey-camera experiment (#273/#274); the camera follows the player and the
  mouse-wheel zooms (see the Camera bullet below). WASD is unbound.
  Up to **5 friendly entities stack** per hex (a full party moves as one blob;
  count badge rendered).
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
- **Keyboard controls are ignored while typing** (item 10, playtest batch 2,
  bug fix): a focused input/textarea/contenteditable (chat, in particular —
  w/a/s/d/c are ordinary letters too) or the start screen being visible
  suppresses every control key (wait and the panel/skills/help keys).
- **SPACE = wait** (item 11, playtest batch 2): the same own-hex move a
  click already waits/cancels with — clears any queued path, and inside a
  bubble it locks in this turn's action like any other move intent.
- **Follow camera + wheel zoom** (#273/#274, Grim-Dawn/Diablo style): the
  camera **re-centres on the player every frame** (the player is always
  screen-centred) and adds **mouse-wheel smooth zoom** — there is **no manual
  panning and no recenter key**. The zoom scales the whole scene toward a
  wheel-driven `targetZoom` eased frame-rate-independently (`1 - e^(-rate·dt)`),
  clamped to **[0.5, 2.5]** (`ZOOM_MIN`/`ZOOM_MAX`) and applied around the
  followed player; wheel scroll over the canvas zooms instead of scrolling the
  page. `window.game` exposes `camera` (the world container's live screen
  offset, which follows the player and folds in zoom) and `zoom` for tests;
  screen↔world transforms multiply/divide by `zoom` (no pan term).
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
- **A redirecting action cancels a queued auto-walk, accepted or not** (#343):
  aiming a skill, an attack, a throw or a recall means "do this instead", so
  the standing route is dropped at submit — *before* validation. Previously
  only the accept paths cleared it, so a refused action (out of range, on
  cooldown, target gone) told you it had failed and then walked you the old way
  anyway. **Incidental intents deliberately do NOT cancel a walk**: equipping,
  dropping, picking up or drinking while travelling is normal and leaves the
  route intact.
- **Entering a combat bubble hard-cancels a queued auto-walk** (#103): the
  server clears the remaining route — down to and including its **last hex**
  (#117: a single-step exemption, a leftover from the pre-#116 melee-bump
  design, used to walk the route's final hex under the fight) — on the
  world→bubble transition, and the client drops the walk goal and its
  destination ring on the same bundle. What survives: a path queued *inside*
  a bubble (fleeing) and a path carried across a bubble merge. Every
  in-combat move is otherwise a fresh, deliberate intent; after the fight,
  click the destination again.

### Combat
- **No separate combat screen** — same map, same intents. **Melee**: click
  (or key-step into) an adjacent enemy to swing — an entity-targeted attack
  intent (#116), one click per swing, and attacking never moves you;
  monsters still fight by moving into you (the classic roguelike
  bump-to-attack is now the monsters' rule); ranged **attack intent** (bow
  single-target, mage AoE radius 1), range 4 hexes, **line-of-sight-gated**
  (#195 — a shot needs the same clear ray bubbles and aggro use since #95;
  through a wall it rejects at submit with `ErrNoLineOfSight`),
  **no friendly fire**.
- **Entity-targeted single-target ranged attacks** (item 7, playtest batch
  2): a bow shot names its victim by **entity id** (`IntentRequest.
  targetEntityId`), not a hex — clicking a hostile in range sends its id.
  Melee shares this entity-targeted intent at distance 1 (#116). An AoE cast
  (mage) stays **ground-targeted** (a hex — the blast radius makes that the
  natural target).
  Validated at submit (entity exists+alive, hostile, in range, and — for a
  ranged shot — line of sight, #195); resolution (#104) runs against
  **pre-move positions**, so a committed shot always lands — the
  `out_of_range` fizzle survives only as a defensive guard (nothing moves
  between submit and the attack phase). A shot also fizzles `out_of_domain`
  (#195) if its named victim is being resolved in a **different domain** —
  entity-targeted resolution fetches the victim by id, so it is domain-guarded
  against byHex rather than reaching across a bubble boundary.
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
  still waiting on the rest of the bubble. Cleared at the resolving bundle's
  **playback end** (#138 — the hit animates across the playback window, so
  clearing at bundle arrival cut the indicator early), with a **turn-bundle
  backstop** (#252): any bundle newer than the resolving one force-clears,
  so a starved animation ticker (throttled tab) or bubble turns outpacing
  playback can never leave it lit — a hard bound of one bundle.
  `window.game.committedAction`, plus `window.game.bundles` (the per-bundle
  clock: +1 per processed bundle, immune to the turn-number jumps hub
  coalescing produces — the clock e2e contracts count in)), kill summaries and
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

Plus five **actives**, which carry a trigger and a cooldown instead of cards —
Evade, Second Wind, Bulwark, Expose and Ember Nova; see the table below.

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

- **Active skills** (#161, #300): a skill is **passive** (rule cards) or
  **active** (a trigger + cooldown) — never both, rejected at content load. An
  active is the turn's action, exactly like a move: not a bonus action, and it
  displaces a queued move or attack.

  An active carries a **behaviour kind** (#300) naming what it does. Every kind
  routes to machinery that already exists for potions and ranged AoE, so a
  new active is a registry row rather than a branch at resolution.

| Kind | Aim | What it does | Reuses |
|---|---|---|---|
| `reposition` | hex | moves the caster to the target hex | the Evade teleport |
| `self-effect` | *none* | applies a timed effect to the caster | `applyTimedEffectLocked` (a drink) |
| `target-effect` | entity | applies a timed effect to a hostile | the same, plus a shot's target gates |
| `area-damage` | hex | damages every hostile in a radius | `resolveAoELocked` |

| Active | Tree (requires) | Cooldown | Effect |
|---|---|---|---|
| **Evade** | Survival (Survivalist) | 3 turns | teleport up to **3 hexes** |
| **Second Wind** | Survival (Survivalist) | 6 turns | Regeneration **+3 HP/turn for 3 turns** on self |
| **Bulwark** | Survival (Hardy) | 6 turns | Ward **×0.75 damage taken for 4 turns** on self |
| **Expose** | Class (Weak Spot) | 5 turns | Vulnerable **×1.20 damage taken for 3 turns** on one hostile at **4 hexes** |
| **Ember Nova** | Class (Kindler) | 5 turns | **5 fire** in a **1-hex blast** at **4 hexes**, plus Burning **−2 HP/turn for 2 turns** |

  - **A evade buys distance, it does not break contact by range** (#161):
    `CombatRadius` is 6 and Evade reaches 3, so evading away from an adjacent
    monster ends at distance 4 — still in the bubble. Range alone never clears
    one. **Landing behind cover does**, since a bubble needs line of sight as
    well as distance (#95): hop past a rock and the fight drops at distance 4.
  - **Evade's destination** needs range, walkability **and room** — and
    nothing about the ray between. It **does** pass through walls (#313,
    2026-07-31, reversing #322), like the classic ARPG evade, and it respects
    hex occupancy exactly like
    an ordinary mover (#196) — a hex held by a monster or already at `StackCap`
    friendlies is refused, so a evade is never a teleport onto (or through)
    someone. The occupancy check runs at both submit (surfaced 422) and
    resolution (a hex that fills between the two — e.g. another evade the same
    turn — drops the later lander, no move, no cooldown). Walkability and
    occupancy are **reposition's rules, not every hex-aimed active's**: a blast
    is aimed at occupied hexes on purpose, because that is where the monsters
    are.
  - **Expose is hostile-only**, gated exactly as a ranged shot is (hostility,
    reach, line of sight). Its Vulnerable row is marked *harmful*, so an
    **Antivenom cleanses it** — inherited from the effect registry, not written
    for the skill. Bulwark's Ward is beneficial and survives a cleanse.
  - **Ember Nova cannot hit an ally.** It routes through the same AoE path a
    a ranged AoE does, which damages *every opposing-faction entity in radius*
    — friendly fire is absent by construction, not by a check. Its reach obeys
    `rangeHex + aoeRadius ≤ CombatRadius`, the same invariant items obey, so a
    blast can never kill outside its own bubble (where no kill-XP is awarded).
  - **A blast resolves in the ATTACK phase**, every other kind in the move
    phase. It needs the turn's rng and shared damage map, and it must land
    against **pre-move** positions like every other hit — resolving it with the
    evades would let a monster dodge by walking away this turn while another
    AoE at the same hex still connected.
  - **A freshly applied effect takes hold NEXT turn**, whatever applied it.
    Actives resolve after the attack phase, so an effect applied where it
    resolves would silently lose the turn it was cast on — a "4-turn" ward
    would protect for 3. Self-casts and blast riders therefore queue on the
    same buffer a weapon's on-hit rider uses.
  - **Cooldowns count TURNS, whichever clock is ticking.** A bubble turn is
    slower in wall-clock than a world turn, and that dilation is the bubble's
    point — a turn-denominated cooldown rides it instead of fighting it. A
    seconds-denominated one would run *through* bullet time and break it.
  - Cooldowns **persist** (`snapshotVersion` 7), so a server restart is not a
    free reset.
  - Wire: `IntentUseSkill` with a skill id, plus whichever of `target` /
    `targetEntityId` the skill's aim uses (a self-cast uses neither).
    `SkillView.aim` is one of `SkillAimSelf` / `SkillAimHex` /
    `SkillAimEntity`, so the client never infers the flow from `rangeHex`.
    Rejections are 422 — not learned, not an active, on cooldown, out of
    range, not walkable, no line of sight, hex occupied, target not found,
    target not hostile.
  - **Descriptions are rendered, never authored** (#300). An active carries no
    rule cards, so `statViewsFor` produced nothing and its panel entry was a
    flavor line with no mechanics — "Here, then not." told a player neither the
    range nor the cooldown. `activeStatLines` renders the descriptor instead
    ("Teleport Range 3", "Cooldown 3 turns", "+3 HP per turn for 3 turns, on
    yourself"), reading the same `activeDef` resolution reads, because an
    authored line restating the descriptor is a drift surface and
    `validateFlavorHasNoStats` already forbids numbers in flavor. A debuff's
    line is deliberately NOT flagged a drawback: that flag asks "does this
    worsen its holder", and for Expose the holder is the enemy.
  - **Client** (#185, #300, #304): the action bar shows four slots, keys
    **1–4** or a click. Each slot's face is the skill's **game-icons glyph**
    (the same source the entity dots use) — "Second Wind" never fitted at
    0.6rem, so the name moved to a hover tooltip carrying the stat lines the
    server already derives. A cooling slot draws a **translucent radial
    sweep** with the turn count on top: the radial says "soon", the number
    says "exactly N", and translucent because an opaque sweep blacks out the
    glyph and reads as a broken slot rather than a cooling one.
    - **Four slots, five actives — and that is a choice, not an overflow.**
      **Right-click a slot** to pick which active lives there, or `— none —`
      to empty it. An unslotted skill is shown as such in the menu rather
      than being invisible. The assignment is **client-local**
      (`localStorage`, like the mute setting): a UI preference, not world
      state, so no wire field and no snapshot bump — and it does not follow
      you to another browser.
    - Learning a new active **fills an empty slot, never displaces an
      assigned one**; a stored slot naming a skill this character has not
      learned is emptied, because `localStorage` is per browser and outlives
      the character it was set for. A **self-cast fires on the press**; a hex- or
    entity-aimed one arms, and the next map click is its target. An
    entity-aimed active clicked onto bare ground **stays armed** rather than
    spending the arm on a mis-click.
  - **Arming shows the reach** (`render/range.ts`'s `SkillRangeLayer`): while an
    active is armed, every tile it can be aimed at is tinted in the action
    bar's own arming green — press Evade and you see where it reaches instead
    of guessing and eating a 422. Hex-aimed skills light walkable ground in
    range; entity-aimed ones light the hostiles. Its own layer rather than a
    fourth set on `MoveRangeLayer`, which is empty outside combat by design
    while an active works anywhere.
    - The set is a deliberate **superset**: it models neither line of sight nor
      occupancy, so a Evade onto a monster-held hex still lights up and is
      refused on submit. That is the same fidelity the ranged attack wash has,
      and it is why the wire carries `aim` but not the behaviour kind — the
      client would need the kind ONLY to decide whether an occupied hex is
      legal, and at preview fidelity it does not have to decide.
    - `window.game.skillTiles` mirrors it for tests.
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
| lifesteal card (`deal-damage` + `lifesteal`) | its own affix — `+25% Lifesteal` (always a benefit, never a drawback) |
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

**Item nature is enforced at load**: a `deal-damage` card belongs on a weapon
**or on jewelry** (ring/amulet — the ARPG "affix on a ring", #271), a
`take-damage` card on worn kit, and a mixed item panics at process start.
Utility cards are exempt. The nature lives on the item's *type*, not its slot,
because the off-hand takes both a shield and a dual-wielded weapon. The jewelry
exemption is **narrow and deliberate**: armor and shields stay defence-only, so
their sign convention (a `−N% Damage` on a chestplate can only mean damage
*taken*) still holds. A crit% ring is attacker-side percentage, not a coupled
roll, so it is ARPG-legal on jewelry.

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
  give HP only — **plus skill points** since #124 (`SkillPointsPerLevel = 3`,
  and a Human banks `HumanBonusSkillPoints = 1` more).
- **Death**: XP falls to the start of the current level (levels never
  lost — the "level start" floor is level-aware under the quadratic curve
  too), respawn at full HP with the **same identity and all gear** (gear
  always survives death — decided). Respawn location scattered across the
  **sanctuary** (any walkable, capacity-available hex within
  `SanctuaryRadius`, guarded against landing on/adjacent to a living
  monster — same `spawnHexLocked` tiers as a fresh join, Q9); the camera
  **follows** the player, so it re-centres on the respawn automatically.
- **The health and energy draughts** (#322): always available, on `R` and `E`.
  **No item, no stack, no inventory slot** — a cooldown is the entire price,
  which is why the intent carries no item id. Each restores
  `protocol.PotionRestorePercent` (40%) of that pool's **maximum** — a share
  rather than a flat amount, so it keeps its value as pools grow — and cools
  for `protocol.PotionCooldownTurns` (5) turns.
  - **The two cooldowns are independent**: draining energy in a fight must not
    also cost you the heal.
  - **Legal inside a combat bubble, and immediate rather than queued as the
    turn's action** — a panic response that costs your turn is not one.
  - Refused while cooling with a **422** (`ErrPotionOnCooldown`). Persisted in
    the snapshot (**version 12**), because a restart handing back a free heal
    would be a real exploit.
- **The action bar is always visible** (#322 slice 3): it used to appear only
  once you had learned an active, which left a fresh character's globes and
  evade ball framing nothing and made the whole cluster jump the moment the
  first skill was learned. An empty slot is also the affordance you right-click
  to fill, so hiding the bar hid the way to populate it.
- **The globes** (#322): health left of the action bar, energy right, from the
  approved mockup (`docs/mockups/2026-07-28-health-evade-energy-globes.png`).
  Each shows `current/max` over its liquid, carries its draught's cooldown in
  its **top inner corner** — so a cooling draught never hides the amount it is
  refilling — and its key hint centred underneath, so the three readouts
  separate by height. A tick rather than a number when ready, so the shape does
  not jump.
- **Energy** (#322): the action currency every active spends. The pool is the
  **inverse of the HP curve** — the squishy classes live on their skills, so
  they carry more: `FighterMaxEnergy` 60 / `RogueMaxEnergy` 80 /
  `MageMaxEnergy` 100 at level 1, `+EnergyPerLevel` (5) a level, clamped rather
  than refilled on level-up.
  - **Costs are registry data** on each `activeDef`, priced off its cooldown and
    blast: Ember Nova 30, Expose 20, Second Wind 25, Bulwark 25. **Evade costs
    nothing** — an escape you cannot afford is not an escape.
  - **Spent where the cooldown is stamped**, so a charge can never drift from
    the cooldown it pairs with, and an active whose caster died before it
    resolved is never billed. Too little energy is a **422**
    (`ErrNotEnoughEnergy`), like any other well-formed request the world
    refuses.
  - **`EnergyRegenPerTurn` (5) comes back every turn, in a bubble as well as
    out of one** — and since #322 decision 11 **HP now does too**, which is a
    real change to fight arithmetic: bubbles previously had no healing at all.
    Monsters get none of it (only the Hydra heals, via its bite card), so the
    asymmetry favours players deliberately; `RegenPerTurn` stays at 1 so the
    trickle cannot outpace a monster hitting for 3–9.
  - **Own-only on the wire** (`Entity.Energy`/`MaxEnergy`): another player's
    remaining energy is build information and nothing on screen needs it.
    Persisted in the snapshot (**version 11**); `MaxEnergy` is derived from
    class and level on load, the way `MaxHP` is.
- **Evade** (#322): a **mechanic everyone has**, not a learnable skill — no
  point to spend, no row in the skills panel. `Space` **arms** it and the next map click spends it, repositioning you up to
  `protocol.EvadeRangeHex` hexes on a `protocol.EvadeCooldownTurns`-turn
  cooldown. Pressing `Space` again cancels, as does Escape.
  - **Nothing in the ray stops it — not woods, not walls.** Evade gates on its
    destination alone (#313, 2026-07-31). Ordinary line of sight spends
    `ForestSightCost` against a skill's own range, which refused every evade of
    2+ hexes in forest; #322 exempted woods but kept walls, and #313 dropped
    the ray check entirely. A teleport that cannot cross a wall reads as a walk
    with extra steps. The **destination** still gates: rock underfoot is not
    walkable, so you may jump past a wall but never onto one.
  - **Arming paints the reachable tiles** — everything within
    `protocol.EvadeRangeHex`, on the same overlay an armed active uses. That is
    the point of arming: the limit is visible **before** committing, rather
    than learned from a refusal. Nothing is painted until it is armed, because
    an always-on overlay is wallpaper.
  - **Its cooldown rides its own wire field**, `Entity.EvadeReadyIn` (own-only,
    like `Skills`): a universal mechanic carries no `SkillView`, so the HUD ball
    would otherwise have nothing to render.
  - **`.` waits** — the roguelike convention. It took over from `Space`, which
    evade now owns: the panic button gets the key a hand already rests on.
  - Formerly the **Blink** skill; renamed with its numbers unchanged.
- **Passive regen**: +1 HP per world turn while out of combat (never in a
  bubble, never above max). Removes death-as-the-only-heal.
- **HUD stats line** (item 9, playtest batch 2; XP portion reworked for the
  quadratic curve, fast-lane batch): `Lv L · (xp into this level)/(XP needed
  this level) XP · (q, r)` — my entity's hex, live per turn bundle.
- **The start screen's buttons are bound the moment it is revealed** (#317):
  its listeners attach when the screen is shown, not when the code later awaits
  them — the awaits do not run until the engine and the whole map have loaded,
  which used to leave the buttons painted, hover-responsive and **dead** for
  that whole window, dropping any click that landed in it with no feedback.
  `window.game.startBoundAtTiles` records how much map was on stage at bind
  time (0 = bound first) so the ordering is testable without racing it.
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
  | Ember Brand | melee | | 4 | – | – | — (Fire — off the mage's exclusive list) | troll (w3) / dragon (w1) drop |
  | Ironhead Greatmaul | melee | ✅ | 9 | – | – | — (players' first heavy 2H blunt) | skeleton (w3) / troll (w3) drop |
  | Longbow | ranged | | 3 | 6 | – | reach-for-damage: +2 range over the Shortbow for −1 damage | wolf (w3) / kin archer (w4) drop |
  | Vampiric Blade | melee | | 4 | – | – | +25% Lifesteal (heals the wielder for 25% of the damage it deals) | wraith drop (w2) |

  `Rng`/`AoE` "–" = 0 (adjacent-only / single-target). Misericorde and
  Duelist's Saber are the first item-side **crit%-weapons** (fast-lane
  batch, #69 Q5) — the elf-crit `deal-damage`+`chance` card pattern applied
  to gear instead of a species passive; both are now equippable by any
  class (gates dropped) though the "rogue"/"fighter" naming is a flavor
  holdover from before #56. The **Vampiric Blade** (#271) is the first
  **lifesteal** weapon: its `deal-damage`+`lifesteal` card heals the wielder
  for a % of the damage the blade deals (per-weapon — only its own hit
  leeches, not a whole dual-wield turn), clamped to max HP, applied with the
  turn's damage so a mutual kill still kills.
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
  | Physical | **Sharp** | swords, daggers, bows (incl. longbow), cleaver, venom fang, misericorde, saber; rat + wolf + goblin claws |
  | Physical | **Blunt** | fists, oak wand, warhammer, mattock, ironhead greatmaul; troll + skeleton claws |
  | Elemental | **Fire** | ember focus, ember staff, war-mage staff, ember brand; dragon claws |
  | Elemental | **Ice** | Frostbrand; frost wisp claws |
  | Metaphysical | **Holy** | Wyrmslayer Greatsword, Consecrated Mace |
  | Metaphysical | **Chaos** | ghoul + wraith claws |

  The families, and the Holy↔Chaos / Fire↔Ice **oppositions, are an
  authoring convention — not machinery**. All six types are mechanically
  flat; "a Chaos ghoul fears Holy" is a vulnerability card someone wrote,
  and the engine does not know the pair exists. Promotable to a real axis
  later only if content always ends up mirrored.

  **Monster vulnerabilities**: ghoul and wraith take **+50% from Holy**, troll
  and frost wisp **+50% from Fire** ("trolls fear fire" — the identity the arc
  was pitched on; the frost wisp is its ice mirror). **Monster resistances**
  (#266, the board's first): skeleton halves **Sharp**, wraith halves **Sharp
  and Blunt** — two physical-resist cards on one monster make it harder by
  design (the "don't stack a both-physical resist" caution is about player
  *armor*, not enemies).

  **Resist gear** (each halves exactly one type — situational where flat
  mitigation is always-on):

  | Item | Type | Card | Source |
  |---|---|---|---|
  | Infernal Chain Mail | chest | fire ×0.5 | dragon (w2) drop |
  | Warded Gambeson | chest | sharp ×0.5 | wolf (w3) drop |
  | Pilgrim's Mantle | chest | chaos ×0.5 | ghoul (w3) drop |
  | Ironbound Gauntlets | gloves | blunt ×0.5 | skeleton (w2) / troll (w3) drop |
  | Frostward Charm | amulet | ice ×0.5 | frost wisp (w3) / wolf (w1) drop |

  No card answers **both** physical types at once: one that halved Sharp and
  Blunt together would be strictly better than either elemental resist, since
  nearly every early monster is sharp or blunt. Each of the six types now has
  exactly one **single**-type resist (#267 filled Blunt with the Ironbound
  Gauntlets and Ice with the Frostward Charm, the first gloves- and
  amulet-slot content) — situational where flat mitigation is always-on. New
  weapons: **Frostbrand** (ice, damage 4, troll w3) and **Consecrated Mace**
  (holy, damage 4, ghoul w3) —
  both sit at the shipped 1H anchor so the *type* is the point, not a stat
  upgrade riding along. A weapon's type shows as a **Type** line in the stat
  tooltip (character panel and pickup modal alike).

  **Offensive jewelry** (#271): the **Ring of Precision** (ring: `10% chance
  ×2 Damage`) is the first jewelry to carry an **offensive** (`deal-damage`)
  card — the ARPG "affix on a ring". Its crit% applies to **every** attack the
  wearer lands (main-hand, off-hand, and ranged — the attacker's equipped
  jewelry `deal-damage` cards fold into every hit's roll), the point of a crit
  *ring* over a single crit *weapon*. Drops off the ghoul (w2). The jewelry
  offensive exemption is narrow (see the *Item nature* note above): armor and
  shields stay defence-only.
- **Non-weapon items**: Leather Armor (chest: take-damage ×0.9, floor 1), Iron
  Plate Armor (chest: take-damage ×0.8 + aggro-range ×1.25), Padded Boots
  (boots: aggro-range ×0.75), the three resist chest armors above (one type
  halved each), the Ironbound Gauntlets (gloves: blunt ×0.5) and Frostward
  Charm (amulet: ice ×0.5), Headband of Learning (helmet: earn-XP ×1.05), the
  heal-ladder consumables (Minor Salve +3, Healing Potion +5, Greater Draught
  +10, Full Restorative to full — each drink clamped to max HP, stacks to 5),
  the timed-effect consumables (#271, slice 2 — **Draught of Fury** +25%
  deal-damage for 4 turns, **Warding Tonic** +25% damage resistance for 4
  turns, **Antivenom** cures harmful effects — see **Timed / lingering
  effects** below), and the two shields above (off-hand: take-damage
  ×0.9/×0.8).
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
  first *wants* a damage type). The content expansion (#269) routes the new
  loot: each new kind's own table signals its counter (Skeleton → Ironhead
  Greatmaul; Frost Wisp → Frostward Charm + Frostbrand; Wraith → Consecrated
  Mace + Pilgrim's Mantle; Goblin → cleaver + salve), and the new gear also
  rides existing kinds so it stays reachable — Ember Brand on troll (w3) /
  dragon (w1), Ironhead Greatmaul on skeleton (w3) / troll (w3), Longbow on
  wolf (w3) / kin archer (w4), Ironbound Gauntlets on skeleton (w2) / troll
  (w3), Frostward Charm on frost wisp (w3) / wolf (w1); the recovery ladder
  spreads across the tiers (Minor Salve on rat/wolf/goblin/frost wisp, Greater
  Draught on wraith/troll, the very-rare Full Restorative on the dragon). The
  timed-effect content (#271, slice 2) routes the same way — the **Antivenom**
  rides the **Serpent** (the poison monster drops its own cure), and the buff
  potions ride the **Hydra**'s own new table (Draught of Fury w3, Warding Tonic
  w3, Greater Draught w1); both tables are new, so no existing pinned drop seed
  moves. The offensive-gear slice (#271) routes its two items on the same
  append-LAST rule, on kinds no seeded drop test pins: the **Ring of Precision**
  on the ghoul (w2 — its assassin/precision tier already carries the Misericorde)
  and the **Vampiric Blade** on the wraith (w2 — a life-draining elite). The wolf
  additions were the only ones to move a pinned drop seed, re-derived in
  `drops_test.go`. Items land on the death hex and render as map markers. The
  #271 slice 5's two **targeted consumables** (the Flask of Alchemist's Fire
  and the Scroll of Recall) were removed in #352 and are on no drop table
  because they no longer exist.
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
- **Keybindings** — `I` or `C` toggles the character panel, `Esc` closes it (a genuine
  no-op while already closed, never a toggle); both share the control keys'
  typing-focus guard (`client/src/input/keys.ts`), so typing "i"/Escape into
  chat or the join-name field never touches the panel. (`C` is a second toggle
  key alongside `I` — #273 briefly gave it to camera-recenter, but #274's pure
  follow camera has no recenter, so `C` is a panel alias again.)
- **Client** — a toggleable **paper-doll** panel (`I` key + a HUD button
  + an in-panel × whose tooltip lists the ways to close it; default closed since
  it is large): the eight named hexes (Helmet, Amulet, Gloves, Ring, Main Hand,
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

### Monsters (kinds & difficulty rings — milestone 6c, expanded #266, #271)
- **Fourteen kinds**, content data in `internal/game/content.go` (`monsterDefs`),
  each with its own stats, aggro radius, XP award, and loot table. A kind
  **names its weapon** in the item registry (#179) rather than carrying a copy
  of one, so reach, damage and damage type all come from a real item:

  | Kind | Ring(s) | HP | Weapon | Dmg | Reach | XP | Aggro | Drop chance |
  |---|---|---|---|---|---|---|---|---|
  | Rat | 0–1 | 4 | Claws | 1 | melee | 8 | 7 | 10% |
  | Goblin | 0–1 | 6 | Rusty Shiv | 2 | melee | 12 | 7 | 15% |
  | Serpent | 1 | 8 | Venom Sting | 2 | melee | 16 | 8 | 30% |
  | Wolf | 1 | 10 | Fangs | 3 | melee | 20 | 10 | 30% |
  | Ghoul | 1–2 | 16 | Talons | 4 | melee | 35 | 8 | 35% |
  | Kin Archer | 1–2 | 12 | Hunter's Bow | 3 | **3 hexes** | 30 | 8 | 30% |
  | Skeleton | 1–2 | 14 | Bone Club | 3 | melee | 30 | 8 | 35% |
  | Frost Wisp | 1–2 | 14 | Frost Touch | 4 | melee | 32 | 8 | 35% |
  | Hydra | 2 | 24 | Hydra Fangs | 4 | melee | 55 | 8 | 45% |
  | Troll | 2 | 30 | Maul | 6 | melee | 60 | 8 | 50% |
  | Wraith | 2 | 26 | Talons | 4 | melee | 70 | 8 | 45% |
  | Dragon | 2 (capped at 1 per world) | 60 | Dragon Jaws | 9 | melee | 150 | 12 | 100% (incl. the Wyrmslayer Greatsword) |
  | Risen | 2 | 4 | Claws | 1 | melee | 5 | 7 | 5% |
  | Necromancer | 2 | 24 | Bone Club | 3 | melee | 65 | 8 | 45% |

  **The expansion kinds (#266)** add the board's first *resistances* — before
  them every monster card was a vulnerability, so a player only had to avoid
  the wrong armor, never bring the right weapon. The **Skeleton** halves Sharp
  ("bring blunt"), the **Wraith** halves both Sharp and Blunt and takes +50%
  from Holy (the first enemy that *demands* elemental or Holy damage), the
  **Frost Wisp** is the only Ice attacker and takes +50% from Fire (the ice
  mirror of "trolls fear fire"), and the **Goblin** is a second home-ring face
  (a weak sharp trash mob, no cards).

  **The Serpent is the first kind whose attack applies a lingering effect**
  (#271, slice 1): its bite (`Venom Sting`, monsterOnly) poisons the victim — a
  small HP drain each end-of-turn for a few turns, refreshed on every hit (see
  the **Timed / lingering effects** entry). It drops the **Bloodrage Cleaver**
  (the timed-buff proof weapon) and its own **Antivenom** cure, so one
  encounter teaches both the DoT and the counter to it.

  **The Hydra regenerates as it fights** (#271, slice 2): its bite
  (`Hydra Fangs`, monsterOnly) self-applies a **regen** effect — a flat +3 HP
  each end-of-turn for a few turns, refreshed on every bite — so drawn-out, low
  damage never finishes it and bursting it down does. This is a fixed regen, not
  damage-proportional lifesteal (a later slice's new pipeline kind). It is the
  live proof of the `end-of-turn` heal direction the foundation only exercised
  in a white-box test, and its own table carries the buff potions (Draught of
  Fury, Warding Tonic) plus a Greater Draught.

  **The Necromancer is the first SUMMONER** (#271): while in combat (inside a
  bubble) it raises weak **Risen** adds on nearby free hexes, via an
  **end-of-turn spawn hook** (`summon.go`'s `tickSummonsLocked`, run at the same
  turn-resolution point as the timed-effect tick). The behavior is **pure data**
  — a `summonSpec` on the kind (`{minionKind, everyTurns, maxLiving, count}`),
  not a combat-site edit, mirroring the on-hit rider seam. It is **bounded two
  ways so it can never runaway-spawn**: a per-summoner **living-minion cap**
  (`maxLiving` = 3) and a **cooldown** (`everyTurns` = 3 in-combat turns per
  window). A fresh Necromancer starts on a full cooldown, so the first add only
  appears after a **wind-up** window. Each add lands on a **free adjacent hex**
  chosen through the same walkability + occupancy rule an ordinary mover obeys
  (never onto a blocked, player-occupied, or `StackCap`-full hex — the #196
  lesson); the only randomness (which free hex) rides the per-turn seeded PCG.
  The cap counts **living** minions, so killing an add frees room for the next
  window — a steady-state pressure, not a one-time burst. Its own melee (a Bone
  Club, the skeleton's weapon) is modest — the swarm is the threat. The Risen is
  also a plain wild frontier trash mob, so the kind isn't summon-only. A
  summoner's cooldown and a minion's parentage are **persisted**
  (`snapshotVersion` 10) so a restart mid-fight can't hand a free summon or let
  the adds escape the cap.

  **The Kin Archer is the first kind that attacks without closing** (#179). It
  shoots from up to 3 hexes — under the player Shortbow's 4, so player gear
  still out-ranges it — and needs line of sight to fire, the same raycast the
  aggro check uses. It **shoots at point-blank rather than backing off**: every
  entity moves one hex per turn, so a kiting monster could never be caught,
  which is a softlock rather than a difficulty knob.

  Monster natural weapons (`Claws`, `Fangs`, `Talons`, `Maul`, `Dragon Jaws`,
  `Hunter's Bow`, `Venom Sting`, `Hydra Fangs`) are ordinary registry weapons
  carrying `monsterOnly` — a load-time validator panics if one is ever reachable
  through a drop table or a class default. They carry **no rule cards**: a
  kind's own cards (a troll's fire vulnerability) belong to the kind, not to a
  weapon other kinds may share. Two carry an **on-hit timed effect** instead
  (#271): the Serpent's `Venom Sting` poisons its victim, the Hydra's
  `Hydra Fangs` regenerates its wielder.

  Wolf carries forward the pre-6c flat numbers exactly. Each kind renders
  with a distinct on-map dot color (`entities.ts`'s `KIND_STYLE`) plus a
  **game-icons.net glyph** drawn dark on the dot (rat, wolf-head,
  shambling-zombie, troll, dragon-head, bowman for the Kin Archer, and #266's
  goblin-head, skeleton, frozen-orb for the Frost Wisp, spectre for the
  Wraith, plus #271's cobra for the Serpent, hydra for the Hydra, skull-mask
  for the Necromancer, and half-dead for the Risen — `GLYPH_ICON_SVG`, keyed by
  kind id; the source filename need not
  match the id, but the `ICONS` map key in `gen-glyph-icons.mjs` must);
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
  through the fog of distance). Content is re-resolved on each turn bundle
  as well as on cursor movement (#205), so a monster that moves off/onto the
  hovered hex, takes damage, or dies under a **stationary** cursor updates
  (or clears) the tooltip immediately instead of lingering stale until the
  next mouse move.
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
- **Parties** via chat commands (`/invite <name>`, `/accept`, `/decline`,
  `/leave`): ≥2 members, dissolve below that, survive death, swept on
  disconnect. Partymates colored on-map; roster panel.
- **The invite is a prompt, not a sentence to read and type at** (#385). A
  pending invite rides the turn bundle as `TurnEvent.pendingInvite`
  (`PartyInviteView`: `inviterId`, `inviterName`, `members`), **own-only** like
  the roster — present for the person being asked and absent from everyone
  else's bundle, including the inviter's. The client draws a **passive** panel
  in the roster's slot with **Accept (Y) / Decline (N)**: it never takes focus
  or covers the board, because an invite can arrive mid-fight, and an **accent
  border** marks it as the one panel you did not open. It shows the inviter's
  **current party** when they have one (you are joining a group, so the group
  is named) and **warns when accepting would drop you out of your own** — the
  leave always happened, it was just invisible. One quiet **sound cue on
  arrival**, which is what makes a passive panel safe to miss-proof.
  - **No expiry.** Invites do not lapse; a real `/decline` makes a TTL
    unnecessary. (A turn-counted TTL would also have lapsed *fastest* for the
    player mid-fight — the turn counter ticks for every combat bubble — i.e.
    exactly the player least able to answer.)
  - **`/decline` is a real answer**, cleared server-side, and it tells the
    **inviter alone** (see directed chat below). No cooldown on re-inviting.
  - **No range limit**: `/invite` still resolves the nearest player of that
    name anywhere in the world.
  - Unchanged and deliberate: being invited **while dead** is allowed, and a
    second invite **overwrites** a pending one (last-wins).
  - `/accept` and `/decline` still work as typed commands — headless clients
    and the playtest bots use them. The **bot reads `pendingInvite`** rather
    than pattern-matching the chat sentence.
- **Global chat** over SSE (ephemeral, no history), `/here` shares position;
  system announces (quests, pickups, kills, deaths) make it the de facto
  combat log.
- **Directed chat lines** (#385): `ChatMessage.recipient` addresses one entity
  (`0` = the global channel, which is every line but the party decline).
  Enforced **server-side** — the SSE handler never writes the frame to a stream
  whose viewer is not the recipient, so it is not a flag the client is trusted
  to honour, and a token-less watcher matches no recipient and sees the global
  channel only.

### Joining & identity
- **Start screen — shown on every load** (#303). A **new** player gets the
  creation form (name, class card, species card, Enter — keyboard operable).
  A **returning** player gets their character card (name, class, species,
  level) with **Continue** as the default and prominent action: Enter or a
  click. Identity (token) persists in localStorage.
  - It used to be skipped entirely for returning players, which meant the
    people who play most never saw it — including its asset attribution.
    Showing it every load is what makes that credit reachable.
  - The card's name and level are **cached client-side** and refreshed each
    turn bundle: the screen renders *before* joining, and the stored identity
    carries only token/class/species. So the card is a preview — play on
    another browser and the level here is stale until you continue, at which
    point the server's value replaces it.
  - **The card is verified before it is offered** (#311). Because it renders
    from cache alone, a world that was reset out from under the browser used
    to be discovered only by clicking Continue: the reclaim came back 422, the
    panel silently became the creation form, and the player had spent a click
    learning nothing. The client now POSTs its stored token to
    **`/api/token-check`** first and shows the creation form directly — with a
    **"The world has reset"** notice, matching the mid-session reset card's
    wording — when the answer is "unknown". Both halves of the screen stay
    hidden for that one round trip, so the card never appears and then
    vanishes.
    - A token is "known" while it is **reclaimable** — live *or* archived.
    - A probe that cannot be answered (offline, 5xx, 2s timeout) is **not**
      read as "gone": the returning card shows exactly as before, and the
      reclaim-rejection fallback (which now also shows the notice) stays the
      backstop. That fallback is still genuinely reachable — the world can
      reset in the gap between a "known" answer and the reclaim.
    - Character-link arrivals (`#t=`) skip the start screen and the probe
      alike; their dead-link handling is unchanged.
- **Start over** (#303): abandons the current character for a fresh one,
  behind a confirmation. **The confirm shows the old character's identity
  link with a copy button, and that is the point** — the archive keeps the
  character indefinitely (the disconnect sweep archives it, and archive
  entries are deleted only on reclaim, never expired), so it stays reclaimable
  *if you kept the link*. The link IS the token; clearing the identity without
  copying it is what would make "recoverable" true on paper and false in
  practice. No server change was needed for any of this.
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
  (session-scoped social state, not progression). The grace clock is
  refreshed by presence **and** by activity: an open event stream keeps a
  player out of the sweep, and an accepted intent (a still-playing client
  whose SSE was reaped by a proxy idle timeout) likewise pushes the grace
  forward, so an actively-clicking player is never swept mid-session.
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
  world every restart. The camera follows the player (see Movement).

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
  (move/attack/equip/unequip/drop/pickup/drink/learn-skill/use-skill), `/api/chat`,
  `/api/token-check` (`TokenCheckRequest`/`TokenCheckResponse` — read-only
  "would this token still reclaim a character?", the start screen's pre-flight;
  a POST body, never a URL parameter, because the token is a bearer secret);
  GET `/api/map` (once), `/api/events` (SSE: per-viewer snapshot turn bundles
  with turn-number ids, chat events, named heartbeats — complete for what the
  viewer can see, never a delta; see **Interest radius** below). Reconnect =
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
- **Interest radius / fog of war** (#289, `protocol.InterestRadius = 20`):
  a turn bundle carries only what lies within 20 hexes of the viewer —
  entities (plus the viewer's own row unconditionally), ground items, bubbles
  with a visible member, and hits with a visible participant. Quests are NOT
  culled: they belong to a holder, not a hex. Each bundle is still a
  **complete** snapshot of what is relevant to that viewer, never a delta, so
  the hub's coalescing contract and resync-to-latest are unchanged.
  Applied in `World.SnapshotFor`, which was already per-viewer.
  **The invariant**: the radius must stay strictly greater than the LARGEST
  per-kind aggro radius — not the `MonsterAggroRadius = 10` default, since
  kinds override it and the Dragon sits at 12 (`content.go`). Otherwise a
  monster hunts from outside what the player can see and appears already
  aggressive. `TestInterestRadiusExceedsEveryAggroRadius` asserts it against
  every kind, so a new high-aggro monster fails the build.
  **Why 20**: it is about the largest radius a player can SEE. At `HEX_SIZE`
  32 the ring reaches 960 px east–west — exactly the edge of a 1920×1080
  viewport at the default zoom — and is nearly complete at `ZOOM_MIN` 0.5.
  At 30 it sat off-screen at every zoom, so the fog-of-war edge would have
  been invisible to everyone.
  **Party roster** (`TurnEvent.Party`): id + name for every member of the
  viewer's own party, complete regardless of distance. Partymates are culled
  from `entities` like anything else, so the client reads its roster panel
  from this field — deriving it from `entities` would make the panel shrink
  as a party spread out.
  **Pending party invite** (`TurnEvent.PendingInvite`, #385): the invite
  waiting on this viewer's answer — inviter id and name, plus the inviter's
  current party `members` (empty when they are solo, since accepting is what
  creates the party) — or absent when none is. Own-only for the same reason
  the roster is: who has asked you to join them is your business. At most one
  is pending per player (a second invite overwrites the first), which is why
  it is a nullable object rather than a list. It clears when the invite is
  accepted **or declined**, and never expires. Before this field the invite
  existed only as the chat announcement, so answering it meant reading a
  sentence and typing `/accept` — still supported, and still what headless
  clients use.
  **The visible edge** (`client/src/render/fog.ts`): a soft vignette (variant
  A) starting 2 hexes inside the boundary and reaching full opacity one hex
  PAST it — so nothing the server still sends is left without ground under it.
  Added to `world` LAST, above every world-space layer, so entities dim with
  the terrain: a monster near the limit is meant to be harder to make out, not
  a crisp sprite floating on fog. The player sits at the centre where the fade
  is zero, and DOM chrome (HUD, panels, chat) is outside `world` entirely.
  The boundary is a **hexagon** — that is what a constant hex distance is —
  built from a circumradius of `√3 × HEX_SIZE × radius` and drawn as
  concentric rings rather than one gradient quad: a screen-covering quad costs
  a full-viewport alpha blend every frame even where it is transparent, which
  measured ~6 s on a 14 s e2e spec under CI's software renderer.
- **The turn clock is the bar, at top centre** (#314): the WeGo turn timer sits
  fixed at the top centre of the viewport, not in the viewer's left-hand HUD —
  it is the one thing every player on the beat sees identically. Its fill is
  **white** during the input window and `--dim` during playback, so the two
  phases read apart at a glance. **There is no turn number in the UI**: the bar
  is the whole signal. `window.game.turn` still carries it for tests and
  debugging, and the `· stuck at N` freeze warning (#170) moved onto the status
  line, which is the other "is this client healthy" readout.
- **UI palette — bordeaux** (#314): the chrome is tinted wine-red rather than
  the original green. Five variables in `client/index.html` carry it: `--bg`
  (`#0f0a0c`), `--fg` (`#e7c9d1`), `--dim` (`#7d5560`), `--accent` (`#a32741`)
  and `--on-accent` (`#fbeef1`). The last exists because the old lime accent was
  light enough to carry near-black label text and bordeaux is not — anything
  sitting ON the accent takes `--on-accent`, never `--bg`.
  - **The world keeps its own colours.** Terrain (grass, forest, water, rock),
    entity dots, HP bars and the combat ring are *semantic*, not chrome: green
    means healthy, red means monster, gold means committed. Only the canvas
    values that exist to match the page background moved with it (the damage
    number's outline, item and map outlines, the fog colour).
  - Danger and warning colours (the error banner, the Start-over confirm, the
    reset notice) are deliberately left on their own red: they must read as
    *alarming* against chrome that is now itself red.
  - Two canvas greens also stay, for the same reason: the targeting overlay
    codes move-tiles blue (matching your own dot) and melee-tiles red (matching
    hostiles), so the **skill-range tile** and the **party dot** are the third
    distinct signal. Retinting either to bordeaux would collide with the
    hostile red at exactly the moment a player is aiming.
- **Typography** (#314): **Cinzel** for display strings, **Literata** for
  everything else — the project's first deliberate type choice, replacing the
  `"Courier New", monospace` default nobody ever picked. Both are OFL, both
  self-hosted from `client/public/fonts/` as **latin-subset WOFF2 at one weight**
  (14 KB + 20 KB); see that directory's `README.md` for sources and the size
  rationale. Self-hosting is mandatory, not stylistic: the CSP sets
  `default-src 'self'` with no `font-src`, so a CDN link is blocked and the text
  silently falls back.
  - **Two variables, one stack each**: `--font` (body) and `--font-display`
    (titles) in `client/index.html`. The display face is spent on the start
    screen wordmark, the HUD heading, panel titles and the controls overlay —
    nothing that must be read at small sizes.
  - **The body face is proportional.** Monospace was a property of the Courier
    default, never a requirement. It cost two things that are restored
    explicitly: numeric readouts get
    `font-variant-numeric: tabular-nums lining-nums` so HUD, combat, tooltip,
    inventory, roster and quest numbers stay in a column and a value going
    118 → 119 cannot reflow its row. Chat and the start screen are prose and
    keep natural figures.
  - **Canvas text names the face directly** — PixiJS does not inherit CSS, so
    the damage numbers (`render/damage.ts`) and the name label / stack badge
    (`render/entities.ts`) carry the family themselves, and must move with
    `--font`. **`main.ts` awaits `document.fonts.ready` before building the
    stage**: Pixi rasterises `Text` to a texture at construction and never
    restyles it, so a name label built before the font arrived would keep the
    fallback for as long as that entity is on screen. The await is bounded —
    `fonts.ready` resolves whether or not the font loaded, so a missing file
    degrades to the fallback instead of hanging the client.
  - **Credited in both surfaces** (start screen and controls overlay) even
    though the OFL requires no attribution — the same courtesy this repo
    already extends to its CC0 audio.
- **Sound effects** (#298, `client/src/audio/sound.ts`): combat, movement and
  inventory audio via **Howler**, effects only — no music or ambient. Assets
  are 26 CC0 clips from five Kenney packs in `client/public/audio/` (~368 KB;
  every pack's `Preview.ogg` demo reel is deliberately excluded), copied by
  Vite into `dist/` and embedded with the rest of the bundle.
  - **Events**: hit / crit / glance / **ranged** / **death** from
    `TurnEvent.Hits` (#114), plus **level-up**,
    equip, drop, pickup, panel open/close, a **HUD button click**, and the
    viewer's OWN footstep. Footsteps are own-entity only — one per moving
    entity would be a stampede at fifteen players.
  - **Death is on the wire** (`HitView.Fatal`), not derived. An entity missing
    from a bundle either died or walked past the interest radius (#289), and
    from outside those are identical — a client guessing would play a death
    sound at whoever wandered off. The server stamps the victim's LAST hit of
    the turn, because damage is applied as a summed map and no single blow
    owns a shared kill. A death by DoT lands no hit, so it is still silent.
  - **Ranged IS derived**: attacker and victim more than one hex apart is the
    definition of a shot, since every entity is melee-armed at adjacency. Crit
    and glance keep their own sounds — they describe the damage roll, not the
    delivery.
  - **Priority is play order**, because the budget is first-come-first-served.
    Level-up goes first (you level by killing, so it competes with a full
    combat turn and is the rarest cue in the game), then deaths, then ordinary
    hits. A fatal hit plays the death INSTEAD of a sword swing — one sound per
    hit.
  - **Still silent by design**: drinking, learning a skill, completing a quest,
    and rejected intents. Three of the filled events use approximations rather
    than the real thing (there is no bowstring in any Kenney pack) — see
    `client/public/audio/README.md`, which names each one.
  - **The unlock**: browsers block audio until a user gesture, and a returning
    player never makes one (`isNewPlayer` skips the start screen when a stored
    identity matches). A one-shot pointer/key listener releases it, or sound
    would silently never work for the people who play most.
  - **Budget**: at most **4 sounds per turn**, refilled once per bundle before
    anything plays, so a six-way bubble resolution is a fight and not a wall
    of noise.
  - **Falloff**: volume drops linearly with hex distance to a floor, never to
    silence — everything in a bundle is within `InterestRadius` (#289), so a
    hit the server bothered to send is a hit you can hear. Your own actions
    play at full.
  - **Variants are chosen at RANDOM** — a deliberate exception to this
    codebase's hash-everything determinism, because a hashed pick would give a
    player walking in a straight line the same footstep every time. Nothing
    about audio is snapshotted, replayed or asserted on.
  - **Attribution**: the CC0 packs ask only that credit "would be nice", and it
    is given in two places — the start panel (beside the glyph credit) and the
    **controls overlay**. Both, because `isNewPlayer` skips the start screen
    for anyone with a stored identity, so a credit living only there would be
    invisible to the people who play most. The overlay is `pointer-events:
    none`, so its links set `auto` on themselves; `credits.spec.ts` hit-tests
    that with `elementFromPoint` rather than reading the CSS.
  - **Toggle**: a `sound: on/off` HUD button, persisted in `localStorage`.
    Like every HUD button it sets `pointer-events: auto` on itself — `#hud` is
    click-through so the canvas works beneath it, and a button without that
    rule renders perfectly and is silently unclickable.
  - `window.game.audio` (played count, last sound, unlocked, muted) is the
    test surface: an e2e cannot hear. Unit tests assert the asset wiring in
    BOTH directions: every filename in `SOURCES` exists on disk (a typo 404s
    silently, leaving one event mute forever while `play()` still reports
    success), and every shipped file is named by `SOURCES` (every clip is
    preloaded, so a dead one costs binary size, first-download bytes and a
    preload fetch for a sound nothing can trigger).
- **Terrain atmosphere** (#296, `client/src/render/map.ts` + `terrain.ts`,
  `grain.ts`, `scatter.ts`): the ground is drawn to read as land rather than as
  a quilt of flat cells. Client-render-only — no server, protocol, or gameplay
  involvement, and every variation is hashed from a hex's own `q,r`, so it is
  identical on every client and never animates.
  - **Per-hex noise**: fill brightness varies ±15% per hex.
  - **Outlines only where terrain changes**: an interior hex draws no outline
    at all; seams between terrains (and the map rim) are stroked as one path.
    `EDGE_DIRECTIONS` (`hex.ts`) maps each `hexCorners` edge to the neighbour it
    faces, pinned geometrically by `hex.test.ts`.
  - **Grain**: a procedural tiling texture per land terrain — speckle on grass,
    mottle on forest, fracture on rock — sharing ONE fill matrix so it tiles in
    world space. Per-hex tiling would print a repeat inside every cell, i.e.
    the grid this removes. Generated at runtime, so `TERRAIN_COLORS` stays the
    single source of truth and nothing extra enters the bundle.
  - **Water is shaded by DEPTH, not textured**: each water hex takes its colour
    from its hex distance to the nearest shore (`waterDepths`, a multi-source
    BFS capped at `MAX_WATER_DEPTH`), `#244c73` shallow → `#162e45` deep —
    a band whose midpoint is the old flat `#1d3d5c`, so depth reads without
    lakes popping out of the muted palette. **The map edge counts as a shore**,
    or a water-rimmed world would render its boundary as the deepest ocean.
  - **Scatter motifs**: forest tufts, rock flecks, and water glints that
    cluster in the shallows. Grass is deliberately bare. Their own Graphics, so
    the heaviest pass stays separately measurable and cuttable.
  - **Cost**, measured at `WORLD_RADIUS=120` (43,561 tiles): map build 84 →
    213 ms, one-time at startup (`window.game.mapBuildMs`). Frame time went the
    other way — **116.6 → 66.6 ms, ~1.75× faster** — because no longer stroking
    six edges per hex (~261,000 segments) outweighs everything added.
- **Response compression** (#288, `internal/server/compress.go`): responses
  are gzipped for clients that send `Accept-Encoding: gzip`, decided by a
  media-type allowlist (`application/json`, `text/event-stream`, and the
  embedded bundle's `text/html`/`text/css`/`text/javascript`/`image/svg+xml`).
  `Vary: Accept-Encoding` is set on every response, compressed or not;
  `Content-Length` is dropped when compressing; `gzip;q=0` is honoured as a
  refusal. There is deliberately **no minimum-size threshold** — buffering to
  decide would risk stalling the SSE stream, which is the whole hazard here.
  Measured **on the deployed staging instance** at `WORLD_RADIUS=120` /
  `MONSTER_COUNT=1000`, i.e. at the middleware's actual
  `gzip.DefaultCompression` (level 6): `/api/map` 1,885,659 → 123,850 B
  (**15.2×**); one turn bundle 230,519 → ~11,801 B (**19.5×**), i.e.
  461 → ~23.6 kbit/s per client at a 4 s cadence. (The 16×/22× first quoted on
  #288 came from `gzip -9` and from compressing a bundle in isolation; the
  live stream also carries heartbeat frames and a per-frame flush boundary.
  These numbers are the deployed ones.)
  **The flush rule**: the wrapper implements `http.Flusher` and its `Flush`
  calls `gz.Flush()` (a sync marker) *before* the underlying flush. Without
  that, turn bundles sit in the compressor's window and the game freezes with
  the connection still open —
  `TestCompressResponsesFlushIsReadableImmediately` (unit, reads the bytes
  mid-handler) and `TestTurnFramesArriveOnAGzippedStream` (integration, real
  socket + real decoder) both fail if it is removed.
  Behind SWAG this rides on the existing `proxy_buffering off` for
  `/api/events`; nginx passes an already-coded body through untouched.
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
  content since #88 — the noticeability gear above; clamped ≥1),
  `end-of-turn` (#271 — folds an entity's active **timed effects** into a
  per-turn HP delta, base 0, no rng; see Timed effects below).
  Conditions: `chance`, `targetHPBelowPct`, `targetHPBelowFlat`,
  `targetHPFull`, `allyInBubble`, `targetAdjacent`, `attackerSpecies`,
  `targetKind` (victim is a monster of a specific registered kind — 6c,
  validated against the monster registry), `damageType` (the damage type
  of the hit being folded — #92, renamed from `incomingType` in #155,
  validated against the six types; works both ways: on a take-damage card it
  is the type landing on you (every resist and vulnerability), on a
  deal-damage card it is the type of the weapon you are swinging, which is
  how weapon-flavoured passives like "+10% blunt damage" are expressed). Effects:
  `add`, `mulPct`, `lifesteal` (`deal-damage` only — a rider that heals the
  attacker for N% of the damage a hit deals, read from the fold's trace and
  applied at `rollDamageLocked`; touches neither `add` nor `mulPct`, so it moves
  no damage number and consumes no rng — #271). Fold
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
- **Timed / lingering effects** (`internal/game/effects.go`, #271, slices 1–2):
  each entity carries a list of **active timed effects** — pure data
  `{effectDefId, magnitude, turnsRemaining}`, never a closure, **persisted in
  the snapshot** (`snapshotVersion` 9). An effect is a rule card that is active
  for N turns, folded by the same pipeline: a **buff** folds at
  `deal-damage`/`take-damage`/…; a **DoT/regen** folds at the `end-of-turn`
  event, where the end-of-turn tick (`tickEffectsLocked`, run once per turn
  resolution) applies the per-turn HP delta (a DoT drains — can be lethal,
  reaped by the same death pass; a regen heals — capped at max HP) and
  advances/expires every effect's counter. Deterministic and **rng-free**, so no
  seeded pin moves. **Stacking**: a re-applied same-def effect **refreshes** its
  timer and magnitude (never stacks N copies) — an ARPG bounded modifier, not a
  TTRPG status (no save, no roll; see design-decisions.md). The **effect defs**
  (`effectDefs`, content) are `poison` (harmful DoT), `burning` (harmful DoT),
  `vulnerable` (harmful take-damage multiplier ABOVE 1, #300), `regen` (heal),
  `frenzy` (deal-damage buff) and `ward` (take-damage/resist buff). The three
  **harmful** ones are what the cleanse path keys on — a flag on the row, never
  a list at the cleanse site.
  - **Four application triggers**, all pure-data riders (no combat-site special
    case): a weapon's **on-hit rider** (`itemDef.onHit`) applies an effect when
    a melee hit lands — collected at `rollDamageLocked`, applied *after* the tick
    so a fresh effect first bites next turn; a consumable's **drink riders**
    (`itemDef.appliesEffect` / `cleansesHarmful`) apply an effect (or clear
    effects) *now*, on drink (`drinkItemLocked`) — a Warding Tonic must turn
    aside the incoming blow the turn it is drunk, and a drink is already the
    player's whole turn in a bubble; an area-damage active's **rider**
    (`activeDef.effect`, #300) applies an effect to every blast victim — routed
    through the same buffered on-hit path (the synthesized weapon's
    `onHit`), so a blast DoT (the **Burning** effect, `−3/turn` for 3 turns)
    also first bites next turn and is cleansed by an Antivenom like any harmful
    effect (its stat line reads "−3 HP per turn"; it said "−3 Damage" until
    #300 — an end-of-turn HP delta is not damage dealt, and that wording read
    backwards on both signs, a regen as bonus damage and a DoT as damage
    reduction); and an **active skill** (`activeDef.effect`, #300) applies one to
    the caster (Second Wind, Bulwark), to a named hostile (Expose), or to every
    victim of its blast (Ember Nova) — buffered like the others, so the number
    on the content row is the number of turns the player actually feels.
  - **Cleanse is harmful-only** (`clearHarmfulEffectsLocked`): an Antivenom
    strips every effect whose def is `harmful` (the poison) and leaves your own
    buffs intact — curing the poison must not also strip the buff you drank.
  - Proof content: **on-hit** — the **Serpent** (poison bite → victim DoT), the
    **Bloodrage Cleaver** (self-buff-on-hit → `+15%` deal-damage for 2 turns),
    and slice 2's **Hydra** (regen bite → self-heal `+3` HP/turn for 3 turns —
    the live `end-of-turn` heal consumer); **on-drink** — the **Draught of Fury**
    (`+25%` deal-damage for 4 turns), the **Warding Tonic** (`+25%` resistance
    for 4 turns), and the **Antivenom** (cleanse). Cleared on a player respawn.
- **Determinism**: per-resolution PCG rng seeded (worldSeed, turn); map
  iteration sorted before any rng draw; spawn randomness on separate
  fixed streams. Fully reproducible turns.
- **Testing surface**: unit tests beside code; `test/integration` drives the
  real handler tree over real HTTP/SSE; Playwright e2e drives the real
  embedded-client binary (59 e2e tests across 31 spec files). The client exposes **`window.game`**
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
- **Balance measurement** (#283 — `make balance`, `cmd/balance`,
  `internal/game/balance*.go`): seeded headless measurement through the
  REAL resolution code (never a re-derived formula — the statlines.go
  "derived, never authored" rule applied to balance). Three modes:
  `-mode matrix` (default) duels every class × monster kind × level
  (`-levels`, default 1,3,5; `-duels` per cell, default 200) — win rate,
  mean turns, winner HP margin, observed DPS both sides, projected TTK
  both directions, and their ratio as a per-cell **threat** number (1.0 =
  even race, >1 = the monster kills faster); `-mode deltas` re-runs the
  grid with one item (or passive skill) swapped over the class-default kit
  → mean threat delta per item = a mechanical power score (actives
  excluded — a bot trigger policy would be measured, not the skill);
  `-mode sim` runs scripted bot parties of 1/2/3/5/10/15 through real
  worldgen + monster AI for ~200 turns × seeds — deaths/100 player-turns,
  per-fight HP low-water (the close-call proxy), combat fraction, XP pace,
  XP spread. Tables to stdout, `-json` for the machine-readable report
  (`balance-report.json` under `make balance`) — the analytics milestone's
  first artifact; player deaths are consumed from the structured combat
  log (the analytics-seed pattern's first machine consumer). Duels start
  adjacent, so reach/kiting advantages show up in the sim, not the matrix.
  **Report-first**: only four coarse pinned-seed guardrail tests block
  `make check` (rat win rate, starter-kind threat, a global threat
  ceiling, solo-carries-risk + parties-not-deadlier) — they move only when
  content moves.
  - **`-mode composition`** (#299) sweeps the class MIX, which `-mode sim`
    cannot: every party it builds is the same round-robin, so composition is
    neither an input nor an output there. Enumerates class **multisets**
    (turn order is not a class property, so 21 parties of 5 rather than 243),
    runs each at 20 seeds, and reports every metric with its **range**, not
    just its mean — at 3 seeds the per-seed variance is wide enough that a
    ranking would be inventing its winner.
    - **Ranked by tension** (maintainer's call): lowest close-call *among
      parties whose deaths stay under a ceiling*. The ceiling is the whole
      trick — a party being slaughtered also has a very low close-call, so
      without it the ranking crowns the worst party in the table as the most
      exciting one.
    - **It checks that nothing dominates; it does not crown a winner.** If one
      mix wins clearly, players copy it and the class choice stops being a
      choice — so a flat field is the good result, and the verdict line says
      so in words rather than leaving it to be inferred. "Separation" is
      measured against the field's own per-seed spread: a gap smaller than the
      noise it came from is not a finding.
    - Parties **1–5 only**: by 10 deaths fall to ~0.3 per 100 turns and by 15
      to ~0.09, so nothing dies and composition has nothing left to move.
    - **Not in `make balance`** — ~1100 world runs against the default mode's
      18 turns seconds into minutes, so it stays an opt-in mode.
    - A composition run is **not comparable with a `sim` run**: different
      classes join, so rng diverges. `PartySimConfig.Composition` defaults to
      nil and reproduces the old round-robin byte-identically, which is what
      keeps the four guardrails pinned.
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
- **Server hardening** (#199 — DoS/resource-exhaustion bounds, all in the
  HTTP layer so a throttled request never reaches the world):
  - **Player cap**: `protocol.MaxPlayers` (64) — a brand-new join past it is
    `503`; reclaims/restores are exempt (a returning player is not a new
    seat). Sized with headroom for the target deployment: a shared-network
    house of ~32 players (room to grow), all potentially from one address.
  - **Join rate limit**: new-character joins drain a **global** token bucket
    (burst `MaxPlayers`, refilling one slot per `JOIN_MIN_INTERVAL`) — a
    whole friend group or a post-restart mass reconnect (up to a full
    `MaxPlayers`-strong wave) bursts in at once, sustained entity-minting is
    throttled with `429` + `Retry-After`. Returning tokens (live or archived)
    bypass the bucket.
  - **Chat rate limit**: one line per `CHAT_MIN_INTERVAL` per token (plain
    lines and `/commands` alike; a rejected input — empty, too long — spends
    no budget); over-rate lines get `429` + `Retry-After`, which the client
    shows as a local system line.
  - **SSE stream cap**: at most `SSE_MAX_STREAMS` (256) concurrent
    `/api/events` streams **globally** (each open stream pays a per-tick
    snapshot under the world lock); over-cap connects get an immediate `503` +
    `Retry-After: 5` — an EventSource treats it as a failed connect and
    retries — instead of silently degrading turn resolution. Sized above the
    full `MaxPlayers` seat count for reconnect churn: during the disconnect
    grace a reconnecting player can transiently hold an old and a new stream
    at once.
  - **Per-IP SSE cap** (opt-in, `TRUST_PROXY_IP` + `PER_IP_SSE_STREAMS`,
    both **off by default**): an *optional fairness* layer on top of the
    global cap so one client IP can't hog every global slot — at most
    `PER_IP_SSE_STREAMS` concurrent `/api/events` streams per client IP,
    over-cap rejected with the **same** `503` + `Retry-After: 5` as the global
    cap (identical "stream-cap-full" semantics, scoped per IP; an EventSource
    reacts to both the same way). It ships **fully disabled**:
    `PER_IP_SSE_STREAMS` defaults to **0** (no per-IP limit), so even turning
    on `TRUST_PROXY_IP` alone never caps a shared-IP house — because per-IP
    fairness is *meaningless, even harmful* when legitimate players share one
    IP (the target deployment: ~32 players from one shared-network address).
    An operator enables it **only** for a deployment whose players have
    distinct IPs, by setting an explicit `PER_IP_SSE_STREAMS=<n>` **and**
    `TRUST_PROXY_IP=true`. When on, the client IP is the **last**
    `X-Forwarded-For` entry — the one the sole trusted proxy (SWAG, nginx
    `$proxy_add_x_forwarded_for`) appends for the peer that connected to it;
    earlier entries are client-supplied and spoofable. If the header is
    absent it falls back to `RemoteAddr` (one shared bucket — a stricter cap,
    still safe). With `TRUST_PROXY_IP` off, `X-Forwarded-For` is never read at
    all (behind a proxy `RemoteAddr` is the shared proxy IP, so a per-IP cap
    on it would be one bucket for everyone). **Security warning:** enable
    `TRUST_PROXY_IP` **only** where the app port is reachable *exclusively*
    via the trusted proxy — if the port is also directly reachable, any client
    can spoof `X-Forwarded-For` and dodge the cap.
  - **Body bounds**: JSON POST bodies are capped at 64 KiB
    (`http.MaxBytesReader`) and must arrive within a 10s per-request read
    deadline (a trickle defence that leaves the long-lived SSE GET
    untouched); request headers get the server-wide 10s
    `ReadHeaderTimeout`. A body **over** the cap is `413` (a size verdict,
    not a `400` "malformed JSON" that would misdirect the client to its
    encoding); a body carrying **more than one JSON value** (a valid prefix
    plus trailing garbage) is rejected `400` rather than silently accepted.
  - The rate/cap knobs are env vars where **`0` disables the limit**
    (the tests' and e2e harness's switch), threaded like `TURN_INTERVAL`;
    `TRUST_PROXY_IP` is a bool, default off.
  - The rejections use the standard JSON error body; `429`/`503` here are
    wire-layer verdicts, not game sentinels.

---

## 3. Configuration (environment variables)

| Var | Default | Meaning |
|---|---|---|
| `LISTEN_ADDR` | `:8080` | HTTP listen address |
| `TURN_INTERVAL` | `4s` | world-turn period (tests shrink it) |
| `HEARTBEAT_INTERVAL` | `15s` | SSE keep-alive cadence |
| `MONSTER_COUNT` | `0` | monsters spawned at startup |
| `STARTER_CONSUMABLES` | `""` (none) | comma-separated consumable ids granted into every **new** player's backpack at join; each id must be a registered consumable or the world fails loud at startup. Empty in production. Added for #271's two consumables, which were removed in #352 — the knob is content-agnostic and kept for future starter kits |
| `COMBAT_PATIENCE` | `30s` | bubble AFK fallback before auto-resolve |
| `BUBBLE_POLL` | `100ms` | control-loop poll (must be < TURN_INTERVAL) |
| `DISCONNECT_GRACE` | `20s` | despawn delay for disconnected players |
| `WORLD_SEED` | `0xC0FFEE` | procgen seed (decimal or 0x hex) |
| `WORLD_RADIUS` | `24` | world hex radius (~1,801 tiles) |
| `SNAPSHOT_PATH` | `""` (disabled) | world-snapshot file path; empty disables persistence entirely |
| `SNAPSHOT_INTERVAL` | `60s` | periodic snapshot-save cadence while persistence is enabled |
| `CHAT_MIN_INTERVAL` | `1s` | per-player minimum gap between chat lines (#199); `0` disables |
| `JOIN_MIN_INTERVAL` | `1s` | refill rate of the global new-character join bucket, burst `MaxPlayers` (#199); `0` disables |
| `SSE_MAX_STREAMS` | `256` | global cap on concurrent SSE event streams (#199); `0` disables |
| `TRUST_PROXY_IP` | `false` | trust `X-Forwarded-For` to enforce the per-IP SSE cap (#199); enable **only** where the app port is reachable exclusively via the proxy — otherwise the header is spoofable |
| `PER_IP_SSE_STREAMS` | `0` (disabled) | per-IP concurrent SSE stream cap, applied only when `TRUST_PROXY_IP` is on (#199); off by default because it's harmful when players share one IP — set an explicit `<n>` only for distinct-IP deployments |

## 4. Game-rule constants (`internal/protocol`, compiled into both sides)

| Constant | Value | |
|---|---|---|
| `TurnSeconds` / `InputWindowSeconds` / `PlaybackSeconds` | 4 / 2 / 2 | turn anatomy |
| `CombatRadius` | 6 | bubble trigger distance |
| `StackCap` | 5 | max friendly entities per hex |
| `BackpackSize` / `ItemStackCap` | 4 / 5 | backpack entries · max identical consumables per stack |
| `MaxNameLen` / `MaxChatLen` | 24 / 500 | input caps (runes) |
| `MaxPlayers` | 64 | player cap (#199): new joins past it are 503; reclaims/restores exempt — sized for a shared-network house of ~32 with headroom |
| `FighterMaxHP` / `RogueMaxHP` / `MageMaxHP` | 30 / 16 / 14 | level-1 HP |
| `HPGainBase` / `HPGainMin` | 8 / 1 | front-loaded HP curve: gain advancing FROM level n = `max(HPGainMin, HPGainBase-(n-1))` — 8,7,6,…,1 then +1 forever (#60 XP2) |
| `XPCurveBase` / `QuestKillRewardPerTarget` | 100 / 20 | quadratic XP curve: total XP to **reach** level L = `XPCurveBase*(L-1)^2` (#60 XP1) & flat per-target kill-quest reward |
| `MonsterMaxHP` / `FistsDamage` | 10 / 1 | pre-6c monster baseline (wolf's HP) & unarmed profile |
| `ElfCritChancePercent` / `ElfCritMultiplier` / `DwarfDamageReduction` | 20 / 2 / 1 | species knobs (`HumanXPBonusPercent` retired in #124 — the Human perk is skill points now) |
| `SkillPointsPerLevel` / `HumanBonusSkillPoints` | 3 / 1 | skill points banked per level, and the Human's extra (#124) |
| `SkillPointCost` | 3 | skill points to learn one skill (#57) |
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
