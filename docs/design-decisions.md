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
- **Shields v1** (#90, 2026-07-15; shipped): off-hand only (no dual-shield,
  no shield-in-main); two tiers (−1 common / −2 rare) as flat `take-damage`
  cards on the existing pipeline — no new event kind, no chance roll;
  drop-only (no starting-kit change). The trade is dual-wield's second hit
  for the reduction; a two-handed weapon still locks the slot (equipping a
  shield evicts it, room-checked). Active block/evasion stays deferred to
  #69, shield skills to #57.

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

## Open flags (doc vs implementation)

- **Bubble trigger — LOS vs distance** *(decided 2026-07-14)*. Bubbles trigger
  on **pure distance** (`≤ CombatRadius`) today; the plan keeps **mutual
  line-of-sight** as the design target (terrain blocks spotting), now tracked as
  a planned future addition in **#95**. Not a contradiction — distance-only is
  shipped, LOS is future.
