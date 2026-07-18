# Design decisions — the gear / skill / combat arc

The decisions-of-record for the gear, skill, progression, and combat-depth
arc. **Live work lives in GitHub issues + milestones** (below) — this doc keeps
the *decided direction and the cuts* so the reasoning survives even though the
issues don't spell it out.

Retired the old `design-roadmap.md` decision-menu on **2026-07-14** once every
row was decided; GitHub is the single tracker now. Combat identity guardrails
live in [`game-identity.md`](game-identity.md); the ARPG-vs-TTRPG reasoning in
[`design.md`](design.md).

## Where the work lives now (GitHub)

| Milestone | Issues | Status |
|---|---|---|
| **Combat depth** | #69 (glance/crit umbrella), #91 (glance% build), #104 (attacks-before-moves) | committed |
| **Gear** | #88 (noticeability gear), #90 (shields) | committed |
| **Damage types** | #92 (DT1) | deferred |
| **Skills** | #57, #61, #62 | deferred |
| **Progression** | #60 | deferred |
| **Test hardening** | #89 | committed |

**Already shipped** (no issue): gear keystone G1–G3 (weapon type-tags, generic
hand slots, dropped class gates — #55/#56, 2026-07-13); XP1–XP3 (quadratic
curve, front-loaded HP, cut `DamagePerLevel`); `crit%` weapons (elf passive +
Misericorde/Duelist's Saber); additive percentage fold (Q8); sanctuary-scatter
spawn (Q9 first half).

## Decided — the *how* (settled rules)

- **1H vs 2H** — one-handed is the *default*; `two-handed` is the only tag. (Q3)
- **Combat RNG** — yes, but only as **bounded, decoupled seeded chances**:
  `glance%` (defence) and `crit%` (offence), each a pipeline rule card, drawn
  from the per-scope seeded PCG. **No coupled to-hit roll, no `d20`.** Block /
  damage-reduction stay deterministic. (Q5; *amended 2026-07-15*: binary
  `evasion%` softened to `glance%` — an X% chance the incoming hit is
  **halved**, never fully negated, so an attack turn is never wasted on a
  total miss. Rogue gets it as the class passive, proposed 20%. See the
  2026-07-15 spec. (Shipped: `RogueGlanceChancePercent`.)
- ~~**AoE always hits** — evasion applies to *targeted* attacks only~~
  *(superseded 2026-07-15)*: the carve-out existed so binary evasion could
  not make anything unhittable and the mage kept an anti-evasion niche; a
  glance can't make anything unhittable (worst case every hit lands at
  half), so **`glance%` applies to all incoming damage, AoE included** — and
  the pipeline needs no attack-type condition to express an exemption. Q6's
  second half stands: flat reduction is the anti-mage answer. The D&D
  save-vs-level framing stays rejected. (Q6)
- **Attacks resolve before moves** — within a turn, all attacks land
  simultaneously against **pre-move positions**, then all moves resolve.
  Committing to an attack always produces damage; the retreat-dodge
  (stepping away auto-dodged the swing, worst for the mage's ground-targeted
  AoE) is removed. Fleeing survives as *trading hits for distance*: a
  one-action chaser that strikes you isn't gaining ground that turn. Mutual
  kills unchanged. (#104, decided 2026-07-15; shipped.)
- **One action per turn, reaffirmed** — move XOR attack per 4-second turn.
  Full move+attack was examined and rejected (2026-07-15): at uniform
  1-hex/turn speed it makes fleeing melee impossible (chaser moves *and*
  hits every turn) and kiting free (ranged steps back and shoots forever),
  and fixing that needs speed/engagement machinery drifting toward TTRPG
  structure. Noted future option, not committed: *melee move+strike* (finish
  the approach and strike in one turn) as a surgical anti-kiting patch.
- **No monster levels / no party-scaling** — difficulty stays kinds + distance
  rings, so progress stays *felt* (the wolf that nearly killed you at L1 dies
  fast at L5). Ceiling-raising later = **authored variants** (new rules, not
  bigger numbers) placed farther out. (Q7)
- **Percentage stacking** — percentages **add** within one event's fold (one
  truncation, order-independent); **stages compose across events** (deal-damage
  → take-damage → crit) so multipliers stay true at their own stage. (Q8,
  shipped)
- **Spawn** — first spawn = seeded scatter *within the sanctuary*; **bed
  thereafter** (beds are a future slice; the scatter half shipped). (Q9)
- **Level-up reward** (if skills are ever built) — **one bankable skill point**,
  spent anytime outside combat, never a modal mid-bubble pick. (Q4)
- **Skill model** (if built) — the **3-tree** model (Class / Adventure /
  Survival) governs; class-agnostic life skills are the Adventure/Survival
  trees; the Class tree carries class-identity-via-skills. (Q10)
- **Melee is an attack intent** (#116, 2026-07-15): one click = one swing
  (ranged parity); attacking never moves the player (no after-kill walk);
  walks stop adjacent to a hostile destination; move-conversion is
  monster-only. Keyboard steps route through the click path so the
  roguelike step-into-enemy idiom survives.
- **Monster home tile + leash** (#102, 2026-07-15; shipped 2026-07-16):
  every monster's spawn hex is its **home tile**; a WORLD-domain monster
  farther from home than its leash — default `MonsterLeashMultiplier=2` ×
  its own aggro radius, per-kind overridable like `aggroRadius` — drops the
  chase and paths home, **ignoring players until it arrives** (no re-aggro
  mid-return), with **no heal** on return. Bubble monsters are exempt (a
  fight is a fight); the returning flag survives a bubble and the return
  resumes when it dissolves. Arrival = the home hex, or adjacent to it while
  home is at `StackCap`; the flag clears on the arrival think pass, which
  re-runs the normal aggro check immediately. The leash is a monster↔home
  relation, so it uses the kind's **base** aggro radius — the per-player
  `aggro-range` noticeability fold deliberately doesn't apply. Leash state
  persists (snapshot v5).
- **Shields v1** (#90, 2026-07-15; shipped): off-hand only (no dual-shield,
  no shield-in-main); two tiers (−1 common / −2 rare) as flat `take-damage`
  cards on the existing pipeline — no new event kind, no chance roll;
  drop-only (no starting-kit change). The trade is dual-wield's second hit
  for the reduction; a two-handed weapon still locks the slot (equipping a
  shield evicts it, room-checked). Active block/evasion stays deferred to
  #69, shield skills to #57.

- **World hover highlight — world-only, terrain-only** (#135, 2026-07-17;
  shipped): out of combat, hovering the tile a click would act on lights it
  (pale ice = walk, parchment = own-hex wait; nothing on rock/water) — the walk
  click's equivalent of #101's attack ember. Two calls worth keeping. **Only
  out of combat**: in combat the blue/red reach tints + the #101 ember already
  answer "is this click live?", so a third layer there is redundant — the
  highlight is suppressed while `inCombat`. **Terrain-only walkability**: the
  client does *not* re-run reachability on hover; a walled-off walkable island
  lights and the click then fails gracefully server-side (`ErrNoPath`, the ring
  clears) — the same failure as today. Duplicating the server's pathfinding on
  every `pointermove` wasn't worth eliminating a rare, self-correcting false
  positive. Client-only, no wire change.

## Cut — won't build (and why)

- **Throwables** — G4 (stacking throwables), G5 (multi-mode melee+thrown), Q1
  (thrown weapons). Not a staple ARPG mechanic; no thrown content will ship.
  The `thrown-weapon` type was already deleted by the gear keystone. (2026-07-14)
- **Subclasses / hybrids** — SU1 cross-class skill access (#58). Far-future,
  downstream of the whole skill system; cut rather than kept as an indefinite
  vision. The decided direction (subclasses-not-classes, via a class-tree
  capstone) is preserved in #58's history if ever revisited. (2026-07-14)
- **Combat action economy** — ACT / ACT-B (block) / ACT-P (protect-ally) (#93).
  Combat stays **attack-only** plus the passive layer (glance/crit) and
  shields. This also cuts **active skills (SK5)** and the **skill-usage UI
  (UI2)** — so a future skill system would be **passive-only**. (2026-07-14)

## Deferred — backlog, not committed

Kept as open issues, not green-lit; revisit when nearer work clears.

- **Skill system** — rule-card skills + 3 trees, property passives, prerequisites
  (SK1/2/3/7), aura/ally effects (SK6). #57 / #61.
- **Skill-tree UI** — view trees, spend points (UI1). #62.
- **Damage-type system** — every attack/monster carries a type (DT1); unblocks
  fire gear and the parked Infernal Chain Mail card. #92.
- **Skill points on level-up** — XP4, ties #60 ↔ #61. #60.
- **Parked gear cards** — Apprentice's War Mage Robes (needs a cascade effect),
  Infernal Chain Mail (needs damage types).

## Ongoing guideline (not a discrete build)

- **Anti-rubberband gear** (XP5) — high-level gear trades raw stats for
  modifiers / set bonuses / specialisation, never flat power. A content rule to
  apply as gear is authored, not a slice to build.

## Engineering notes

- **`crit%` and `glance%` are both free content** *(rewritten 2026-07-15;
  this note used to say "`evasion%` is the one engine slice")*. `crit%`
  ships as the elf-crit pattern — a `deal-damage` card with a `chance`
  condition and `mulPct 200`, authored on a weapon — so it needed no engine
  work (Misericorde / Duelist's Saber already do it). Binary `evasion%`
  *would have been* the engine slice: a fully-evaded hit deals **0**, which
  the pipeline can't express (the `take-damage` fold floors every landed hit
  at 1, and `mulPct 0` still clamps to 1), so it needed a new pre-damage
  `evasion-check` event wired through the three agreeing sites. **That cost
  is exactly why evasion was softened to `glance%`** (halve, never negate):
  a glance is an ordinary chance-conditioned `take-damage` card with
  `mulPct 50` — the mirror of crit — so #91 shrank from an engine slice to a
  content entry plus one protocol constant. (A glanced 1-damage hit still
  floors at 1; glance is protection against real hits, not chip damage.)

- **Same-origin guard on POSTs is defense-in-depth, not the auth boundary**
  *(decided 2026-07-16, #97)*. Auth is a bearer token in the request body —
  there are no ambient credentials for a cross-site form to ride — so the
  guard rejects only requests that *positively declare* another origin
  (`Origin` host ≠ request `Host`, or a `cross-site`/`same-site`
  `Sec-Fetch-Site`); header-less requests (curl, the Go tests) pass. Three
  calls worth keeping: `same-site` is rejected alongside `cross-site`
  (nothing legitimate POSTs here from a sibling subdomain, so the stricter
  read is free); "same origin" is derived from the request's own `Host`,
  since the served origin is configured nowhere; and that derivation
  knowingly accepts two gaps — the scheme is not compared (behind a
  TLS-terminating proxy the server cannot know its public scheme, so this is
  a same-HOST check) and a DNS-rebinding page is self-consistent and passes.
  Both need a *configured* origin to close; revisit together if real
  identity/cookies ever land, which is also when the guard would stop being
  merely defense-in-depth.

- **No server-side input-window cutoff — late intents are next-turn**
  *(decided 2026-07-16, #99)*. Intent acceptance stays permissive by design:
  the world mutex already serializes `SubmitIntent` against every resolution
  pass (the control loop's, and the in-`SubmitIntent` bubble lock-in
  resolution), so an intent can never mutate an already-resolving turn — it
  blocks for the pass and queues for the next one. A hard rejection during
  playback would punish clients that send late (and the ~2 s input window is
  client pacing, not a wire contract) for zero integrity gain. Pinned by
  `internal/game/intent_window_test.go`; revisit only if a bubble ever needs
  a server-enforced lock-in *deadline* (a different feature than a cutoff).

- **A blocked walk detours, bounded by slack — and only for players**
  *(decided 2026-07-17, #96)*. A queued click-to-move path whose next hex was
  occupied used to wait there forever. Unattended auto-walk is a core
  relaxation feature ("click somewhere and your character walks there on their
  own, beat by beat, while you chat"), so a silent permanent stall was a
  direct hit on it. Four calls worth keeping:
  - **Only occupancy blocks, never terrain.** `w.terrain` is generated once
    and never mutates, so a step that was walkable at submit time still is.
    The issue was filed as "unwalkable/occupied"; the unwalkable half was
    unreachable. Revisit only if destructible/dynamic terrain ever lands.
  - **Players only.** A monster's wait-on-block is load-bearing — it is how a
    standing intent becomes next turn's melee attack — and monsters already
    re-path from a retained goal every turn, so a stale route is not
    something they can have. The bug was always player-shaped.
  - **The goal is the route's own end**, not a stored destination. That
    preserves #116's stop-adjacent-to-a-hostile trim for free and keeps a
    queued walk a pure transient — no entity field, no `snapshotVersion`
    bump. Persisting a walk goal across restart stays unbuilt for lack of
    demand.
  - **Slack, not always-detour** (`RepathDetourSlack = 4`). Blockers are
    **transient** — the monster in your way has usually moved on by next turn
    — so an unbounded detour would send you around the map to dodge something
    that a one-turn wait would have cleared. Refusing a detour (slack
    exceeded, or none exists) simply restores the old wait; there is
    deliberately **no give-up rule**, since the player can already cancel by
    clicking elsewhere.

  Two consequences worth knowing. The re-path's predicate **exempts the goal**
  (`Pathfind` returns nil when the target is unwalkable, which would refuse
  every detour toward an occupied goal), so the adopted route's first step is
  re-checked — otherwise a blocked goal one hex away would be walked straight
  into, breaking the very `StackCap`/hostile rules the block enforces. And the
  hostile-blocked case is in practice **bubble-only**: in the world domain any
  monster within `CombatRadius` forms a bubble, which hard-cancels a multi-hex
  route (#103) before a hostile could ever occupy its next step — so the
  world-domain trigger is the friendly-`StackCap` blob, and the hostile
  trigger is a multi-hex flee path queued inside a fight.

## Noticeability is gear, and gear can cost you something *(decided 2026-07-18, #88)*

The pipeline has carried an `aggro-range` event since 6c with no content
behind it. #88 gives it its first cards, and settles three things.

- **Noticeability is gear-only, not a species trait.** How far off a monster
  notices you is a *choice you make in the inventory* — swap the boots on
  before the sneak, swap them off before the brawl — not a number you're born
  with and can never change. Species passives stay what they are (a small
  permanent flavour bonus); a permanent stealth species would also quietly
  re-tier every monster's aggro table for one third of the roster.
- **The fold is multiplicative, over each kind's OWN radius.** Padded Boots
  read ×0.75 and Iron Plate Armor ×1.25, applied to `monsterDef.aggroRadius`
  — so a rat's 7 and a dragon's 12 each move by a quarter and keep their
  relative reach. A flat ±N would have flattened the per-kind table that 6c
  deliberately introduced, and would have zeroed the short-radius kinds
  outright. `applyRules` clamps the result **≥1**: today's cards are
  multiplicative and can't go negative, but a future negative-`add` card
  could otherwise fold a radius to 0 — a monster that never notices anyone,
  which is not a design anyone will ask for on purpose.
- **Tradeoff gear is a direction, not a one-off.** Iron Plate Armor is
  strictly better than Leather Armor at its job (take-damage −2 vs −1) and
  charges for it: you are noticed 25% sooner. Gear that is only ever better
  turns the inventory into a sorting exercise — the "best" item is
  computable, so there is no decision. A real cost makes equipping a
  judgement about the situation you're walking into. Expect later gear to
  follow this shape rather than the strict-upgrade ladder.
## Damage types: six flat types, opposition as convention *(decided 2026-07-16, built 2026-07-18, #92)*

The damage-type arc (DT1) is the single highest-leverage step toward ARPG
gear feel: it unlocks resist gear, elemental weapons, and monster identities
at once. Four things were settled.

- **Six types in three families**: Sharp/Blunt (physical), Fire/Ice
  (elemental), Holy/Chaos (metaphysical). Every attack carries exactly one,
  every weapon and monster kind must declare one, and content load panics if
  either is missing — an untyped weapon would silently dodge every resist
  card ever written and surface only as odd numbers mid-fight.
- **Resists are cards, not a subsystem.** A resistance or vulnerability is a
  `take-damage` card gated on one new condition, `damageType` — the single
  condition kind that serves every such card ever written. This clears the
  no-mechanic-wildfire gate by construction, and it keeps the ARPG rule: the
  check is **decoupled** (what type is landing?), never a coupled roll
  folding attacker and defender into one number.
- **Opposition is an authoring convention, not machinery.** Holy↔Chaos and
  Fire↔Ice pair only in the content: the Chaos-aligned ghoul is *written*
  with a Holy vulnerability, and nothing in the engine knows the two are
  related. Full authoring freedom now; promotable to a real axis later if
  content always ends up mirrored — but not before, since an axis is far
  harder to remove than to add.
- **Types must be felt on day one**, so the first content wave ships with the
  machinery rather than after it: one resist armor per family, plus a weapon
  for each type that had no representative (Ice had none at all; Holy had
  only the dragon-only Wyrmslayer, which made the type effectively
  unobtainable). Blunt deliberately gets **no** resist — a card answering both
  physical types would be strictly better than either elemental resist, since
  nearly every early monster is sharp or blunt.

## Terrain blocks spotting: rock hard-blocks, forest softens *(decided 2026-07-18, #95)*

Bubbles triggered on pure distance since the beginning; LOS was always the
target. Four decisions made it real.

- **Rock hard-blocks, forest softens, water is transparent.** `walkableLocked`
  is deliberately *not* the predicate — water stops you walking, not seeing.
  Forest doesn't block either; it *costs* `ForestSightCost = 2` hexes of
  effective range per hex on the line, so you see a long way over open grass
  and a short way into trees. A hard forest block would have made woodland
  fights impossible to start; a free pass would have made forest cosmetic.
- **Sight gates monster aggro too, not just bubbles.** Otherwise a monster
  charges you *through* a rock wall and the bubble snaps the moment it rounds
  the corner — technically consistent, obviously silly. It gates over the
  monster kind's **own** reach, not `CombatRadius`, or every long-range kind
  goes blind.
- **Losing sight dissolves an existing bubble.** Ducking behind a rock ends
  the fight — matching `design.md`'s "break line of sight *or* get far enough
  away". This needed no code: bubbles are rebuilt from connected components
  every tick, so an edge that stops passing simply doesn't re-form. Emergent
  rather than written, which is why it carries a test that names the decision.
- **Symmetric, and endpoints don't count.** One ray, cost summed, so there is
  never "it sees you but you don't see it"; and only what lies strictly
  between two entities counts, so adjacent entities always see each other and
  standing in forest never hides you from something already next to you.

**Consequence worth knowing:** effective spotting range over forest is now
much shorter than the raw aggro radius. That is the feature, but it moved six
existing tests, each re-derived by making its terrain explicit rather than by
weakening an assertion. Ranged **attacks** stay distance-only by design —
giving them LOS is its own slice.

## Open flags (doc vs implementation)

- **Bubble trigger — LOS vs distance** *(decided 2026-07-14, **shipped
  2026-07-18** — #95)*. Bubbles triggered on pure distance as a placeholder;
  mutual line of sight was always the design target. **This flag is now
  closed** — see the LOS entry above.
