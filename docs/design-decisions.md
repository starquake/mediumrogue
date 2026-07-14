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
| **Combat depth** | #69 (evasion/crit umbrella), #91 (evasion% build) | committed |
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
  `evasion%` (defence) and `crit%` (offence), each a pipeline rule card, drawn
  from the per-scope seeded PCG. **No coupled to-hit roll, no `d20`.** Block /
  damage-reduction stay deterministic. (Q5)
- **AoE always hits** — evasion applies to *targeted* attacks only; area damage
  is undodgeable (`take-damage` reduction cards still apply). Evasion beats
  melee/arrows; reduction is the anti-mage answer. The D&D save-vs-level
  framing stays rejected. (Q6)
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

## Cut — won't build (and why)

- **Throwables** — G4 (stacking throwables), G5 (multi-mode melee+thrown), Q1
  (thrown weapons). Not a staple ARPG mechanic; no thrown content will ship.
  The `thrown-weapon` type was already deleted by the gear keystone. (2026-07-14)
- **Subclasses / hybrids** — SU1 cross-class skill access (#58). Far-future,
  downstream of the whole skill system; cut rather than kept as an indefinite
  vision. The decided direction (subclasses-not-classes, via a class-tree
  capstone) is preserved in #58's history if ever revisited. (2026-07-14)
- **Combat action economy** — ACT / ACT-B (block) / ACT-P (protect-ally) (#93).
  Combat stays **attack-only** plus the passive layer (evasion/crit) and
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

- **`crit%` is free content; `evasion%` is the one engine slice.** `crit%`
  ships as the elf-crit pattern — a `deal-damage` card with a `chance`
  condition and `mulPct 200`, authored on a weapon — so it needed no engine
  work (Misericorde / Duelist's Saber already do it). `evasion%` (#91) is the
  real work: a fully-evaded hit deals **0**, which today's pipeline can't
  express (the `take-damage` fold floors every landed hit at 1, and `mulPct 0`
  still clamps to 1), so it needs a **new pre-damage `evasion-check` event**
  wired through the three agreeing sites (`rules.go` const + `conditionHolds`/
  `applyRules`; `items.go` `validateRuleCards`). That split is why crit shipped
  as content while evasion is the committed combat-depth slice.

## Open flags (doc vs implementation)

- **Bubble trigger — LOS vs distance** *(decided 2026-07-14)*. Bubbles trigger
  on **pure distance** (`≤ CombatRadius`) today; the plan keeps **mutual
  line-of-sight** as the design target (terrain blocks spotting), now tracked as
  a planned future addition in **#95**. Not a contradiction — distance-only is
  shipped, LOS is future.
