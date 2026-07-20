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
  **Reopened and shipped**: the *passive* skill system shipped as #124, and
  **active skills shipped 2026-07-19 as a category** (#161), with Blink as
  their first content — server side; the client half is still pending. Worth knowing
  the original cut was a **scope** decision taken during a roadmap
  walk-through, not an identity one — `game-identity.md` explicitly says an
  action economy of the "your one action can be something other than attack"
  kind FITS. Reactions (block-when-attacked) stay out regardless: those are
  the TTRPG tell.

## Deferred — backlog, not committed

Kept as open issues, not green-lit; revisit when nearer work clears.

- ~~**Skill system**~~ — **shipped 2026-07-18 (#124)**: 3 trees, rule-card
  passives, prerequisites, points per level, near-sighted UI. Aura/ally
  effects (SK6) and the rest of #57's content backlog remain deferred.
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
  strictly better than Leather Armor at its job (take-damage ×0.8 vs ×0.9 —
  −2 vs −1 when it shipped, converted to percentages in #154) and
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
weakening an assertion. Ranged **attacks** stayed distance-only at first —
giving them LOS was deferred as its own slice, which #195 then actioned (see
below).

## Mitigation is a percentage, not a subtraction *(decided 2026-07-18, #154)*

Every damage reduction in the game was a flat `−N`: dwarf −1, Leather Armor
−1, Iron Plate −2, Wooden Buckler −1, Iron Kite Shield −2. Measured against
the live registry, a dwarf in plate with a kite shield (−5) took **1** from
everything up to and including a troll — the `≥1` clamp was doing all the
work, and each extra piece of armour was worth less than the last. That is
the TTRPG armour shape (armour subtracts); the project's grammar is ARPG,
where mitigation scales with the hit.

- **All gear mitigation converts to `mulPct`**: leather ×0.9, plate ×0.8,
  buckler ×0.9, kite ×0.8. Percent deltas **add** within one fold (#61
  principle 14), so plate + kite is 40% off rather than a compounding ×0.64 —
  predictable stacking with no cliff.
- **The dwarf passive stays flat −1** (@starquake's call). A species trait is
  the one place a small always-on subtraction is defensible: it's the
  "shrugs off a bit of everything" identity, it can't be stacked with itself,
  and it folds in the ADD phase before the percentages, which the pipeline
  already documents.
- **The `≥1` clamp stays** — it is the floor that keeps a landed hit
  meaningful, not a mitigation mechanism.

The felt result: a troll now lands for 3 through full armour where it used to
land for 1, and the *shape* of a defensive build is legible again — stacking
two 20% pieces is visibly better than one, at every monster tier.

## The skill system is content, not machinery *(built 2026-07-18, #124 — closes #123)*

Three trees (Class / Adventure / Survival) of pure-data rule cards, four v1
skills, points banked per level, and a near-sighted panel. What the slice
actually decided:

- **A skill is a rule card, so the pipeline cannot tell it from a sword.**
  Learning one appends its cards to the same folds gear and species passives
  already use — appended LAST, which is contractual: a chance-conditioned
  skill consumes rng, so any other position would shift every pinned seed in
  the repo.
- **Near-sighted, enforced server-side.** You see what you know and what you
  can learn next. A locked skill is not hidden by the client — it never
  reaches it (`skillViewsLocked`). The point is stumbling onto a capstone
  rather than planning toward one seen from the start, and enforcing it at
  the wire means no future client bug can leak the tree.
- **Own-only bundles.** The original spec said skills would be own-only
  "like `Items`" — but `Items` is sent for *every* entity, so there was no
  precedent to copy. `SnapshotFor(viewerToken)` renders a bundle per viewer
  instead; at ~15 players that is affordable, and the hub's coalescing
  contract is untouched.
- **The point bank rides a high-water mark, not a level-up event** — the
  engine has none, since level is derived from XP. Without `pointsGrantedLevel`
  three things silently double-pay: a repeated grant, re-earning XP lost to
  death, and reloading a snapshot.
- **Learning is out-of-combat only**, and deliberately NOT queued like the
  five inventory actions. It costs no bubble turn and needs no pending-action
  plumbing: it is a between-fights decision, not a turn's action.
- **The Human perk becomes +1 skill point per level**, retiring the +50% XP
  card — which is #123's complaint answered. XP bought "reach the same HP
  slightly sooner" because levels grant HP only, so it read strong and played
  invisible. It is a species check rather than a rule card, because a
  per-level *bank* grant is not a fold over a combat value.
- **Scoping by weapon TAG, not damage type** (Combat Training). The
  damage-type version needed no new vocabulary at all, but a tag is how a
  weapon is *used* — which is what "+10% with melee weapons" means.
- **Shield Wall is a `glance%` bump, not flat mitigation** — consistent with
  #154, after which a flat `−N` would have been the only subtractive card
  left besides the dwarf passive.

## Stat lines are rendered, and flavor never carries a number *(decided 2026-07-19, #171)*

Item and skill text used to be authored prose that restated the rule card
sitting beside it — "take half damage from chaos" next to a `take-damage ×0.5`
card. Two things were wrong with that: it reads like a tooltip from a
different genre, and it is a drift surface, because nothing stops the number
and the sentence disagreeing after a retune.

- **Mechanical text is derived from the cards.** `statLinesFor` renders them;
  nobody writes them. The tooltip and the mechanic cannot disagree because
  they are the same data.
- **Authored text is flavor only, and contains no digits** — enforced at load.
  Crude on purpose: a flavor line that genuinely wants a number can be
  reworded, which is cheaper than a rule no one can check.
- **Defensive stats read as RESISTANCE, offensive ones as DAMAGE.** The first
  draft used the slot to disambiguate ("−50% Chaos Damage" on armour meaning
  damage taken), which works but asks the reader to notice which slot an item
  occupies. Resistance carries its own direction and removes a double
  negative: `+50% Chaos Resistance` is plainly good, where `−50% Chaos Damage`
  needs a beat of thought. A future cursed item falls out for free as
  `−50% Fire Resistance`. Revised mid-build, before anything merged.
- **Sign does not mean good**, so drawbacks are flagged separately and styled
  apart. `+25% Aggro Range` is a cost; `+5% XP` is not. Detection is an
  explicit per-event table rather than a sign test, because "is this good?"
  depends on the event.
- **Item nature is enforced at content load** — offensive cards on weapons,
  defensive on worn kit — since the terse phrasing only stays unambiguous
  while items point one way. The nature belongs to the item's *type*, not its
  slot: the off-hand accepts both a shield and a dual-wielded weapon.
- **Base stats stay fields, not cards** *(settled 2026-07-19, #175)*. Damage,
  reach and heal are the pipeline's *input*; a card transforms a value, so
  something must supply the first one. The argument for converting them is
  uniformity; the argument against is that the base stops being checkable —
  "a weapon has damage" is a load-time invariant, "has a card that happens to
  be the base" is not, and a content bug becomes a fold you must simulate
  rather than a field you can read. The precedent agrees: Path of Exile,
  Diablo II–IV and WoW all keep base stats as fields on the base item and put
  the *variation* in data, even where the whole engine is a modifier fold.
  Everything-is-an-effect works for card-battlers (nothing persists) and
  component/ECS works for Caves of Qud (the query layer IS the engine);
  neither is a cheap bolt-on here.
  **What actually makes this safe is that there is exactly ONE base layer** —
  players, bare fists and monsters all reach the combat path as an `*itemDef`
  read through `itemDamage`; `monsterDef.damage` is authoring shorthand that
  `buildMonsterIndex` compiles into a real claws def at init. The day that
  stops being true, base-as-fields becomes the wrong call, so it is pinned by
  `TestOneBaseLayer_EveryDamageSourceIsAnItemDef`.
- **Offensive cards are LOCAL to the firing weapon; defensive cards are GLOBAL
  to the wearer** *(#175 — long implicit, written down 2026-07-19)*.
  `rollDamageLocked` collects the two sides differently and always has: the
  attacker side folds `species + THIS weapon's rules + skills`, so the sword in
  your other hand contributes nothing to this hit; the victim side folds
  `species + class + EVERY equipped slot + skills`, because a hit lands on the
  whole person, not on whichever slot is swinging. This is Path of Exile's
  local-vs-global modifier distinction, arrived at independently. PoE needs a
  scope flag on each mod because a ring there *may* carry a damage modifier;
  we forbid it instead — #171's `validateItemNature` allows offensive cards
  only on weapons — so a `deal-damage` card on a ring is a panic at process
  start rather than a card that silently never fires. Making it unrepresentable
  beats annotating it. The day gear modifies *other gear* (a ring that boosts
  sword damage specifically), we need PoE's flag for real; nothing wants that.

## Client visibility: a frozen client must say so (2026-07-19, #170)

#167 taught the failure mode: an uncaught exception inside the turn handler
stopped every layer from updating while SSE stayed healthy, so the HUD read
"connected" over a frozen map, the server log looked entirely normal, and the
only evidence was a browser console the player had no reason to open. Three
decisions came out of it.

- **An uncaught client error is a UI event, not a console event.** The window
  `error` / `unhandledrejection` handlers raise a banner naming the message.
  The player sees that something broke and gets text worth pasting into a
  report — the alternative is a silent map and a shrug.
- **Liveness is measured by what was APPLIED, not what arrived.** `turnApplied`
  is stamped on the turn handler's last line; `turnReceived` on its first. A
  bundle that throws halfway advances one and not the other, and the HUD's
  `⚠ stuck` marker keys off the gap. The existing `turn` field could not do
  this job: it is assigned early in the handler and counted up happily right
  through #167's freeze.
- **The regression guard watches the same number the HUD does.** The naive
  test — "assert the turn counter advances after an inventory action" — would
  have passed straight through the bug it exists to catch. `client-alive.spec.ts`
  asserts on `turnApplied` instead, and was verified by injecting a throw
  before that assignment and confirming both its cases go red.

## The designer guide is generated, not written (2026-07-19, #156)

A content guide was written for the designer on 2026-07-18 and handed over as a
PDF. It went stale **twice within a day** — the `damageType` rename, then
#154's percentage mitigation. A document that cites live numbers and cannot
know when they move is worse than no document, because a designer trusts it.

- **The data half is generated** from the registries (`GuideData()`,
  `cmd/contentguide`); the **prose half stays authored**. The split is
  argument-versus-data: regenerating the coupling tell or the drift cases would
  lose the reasoning that makes them persuasive.
- **Stat lines come from `statlines.go`, never re-derived.** A second renderer
  would be free to disagree with the tooltips players actually see — the guide
  would be internally consistent and still describe a game nobody is playing.
  Pinned by `TestGuideStatsComeFromStatlines`.
- **Markdown, not HTML or PDF** (maintainer's call): shareable as a file,
  rendered by GitHub, and diffable in review. A committed binary PDF is none
  of those.
- **`make guide-check` is in `make check`.** Staleness is what killed the last
  version, so the rule is enforced rather than remembered: move a number the
  guide cites and the gate fails until it is regenerated.
- The vocabulary descriptions make `guideDescriptions` a **fourth** place that
  must agree about the card grammar. Accepted deliberately, with two checks
  instead of trust: every documented kind is fed to the real validator at init
  (a rename panics at process start), and every kind content actually uses must
  be documented.

## Monster kinds name a weapon, not a copy of one (2026-07-19, #179)

`monsterDef` carried `damage` + `damageType` + `rules`, and the index
synthesised a claws `itemDef` from them at init. What the shorthand never
carried was `rangeHex` — so every monster was melee **by construction**, and a
ranged monster was not unimplemented but unrepresentable.

- **A kind names a registry weapon** (`weapon: idFangs`). The alternative —
  adding `rangeHex`/`aoeRadius` to the shorthand — is smaller, and means every
  future weapon property must be added in two places that agree. Referencing
  removes the class of bug instead of paying it down once.
- **Monster weapons share the player registry** (maintainer's call), with a
  `monsterOnly` flag validated at load. Sharing is what makes the base layer
  whole; the validator is the price, because nothing structural otherwise stops
  a one-line drop row handing a player Dragon Jaws.
- **Kind cards and weapon cards are now separate.** `monsterDef.rules` had been
  doing double duty — the claws' cards *and* the monster's whole-person
  defensive cards — because the synthesised claws was the only vehicle. On a
  *shared* weapon that breaks: a troll's fire vulnerability would ride on a
  maul any other kind could hold. This split should have existed anyway.
- **Determinism cost: none.** Natural weapons carry no cards, so a monster's
  cards arrive in exactly their pre-#179 fold positions and every pinned seed
  survived untouched. Adding a card to a natural weapon *would* shift them.
- **Ranged monsters shoot at point-blank; they never back off.** Kiting is not
  merely strong here, it is unbeatable: everything moves one hex per turn, so
  a retreating monster can never be caught and a melee player eats a hit every
  turn forever. Revisit only if #98 (multi-hex travel) lands. Genre-wise this
  also matches ARPG convention — monster kiting is rare and disliked, because
  kiting is the *player's* verb.

## The Survival tree means defence, and skills sum (2026-07-19, #57)

The v1 skill batch (#124) filled Class and Adventure and left **Survival
empty** — three trees are principle 1 of #61, so a player could spend points
into a tree with nothing in it. Skills 2 fixes that as its spine rather than
shipping a fourth Class skill.

- **Survival = defensive / attrition** (maintainer's call). #61 pins Adventure
  as "map, surroundings, loot, fog of war" and never defined Survival; it does
  now. Its root is deliberately dull — a flat percentage floor is what makes a
  tree enterable.
- **Mitigation is a percentage, never flat `−N`** — the same rule #154 settled
  for gear. Subtractive mitigation stacks into the ≥1 clamp and stops meaning
  anything.
- **Overlapping damage-type skills SUM.** Combat Training + Crusher on a blunt
  melee weapon is +20%, not ×1.21. This was flagged to the maintainer before
  the build rather than discovered after: the Class tree's first column is
  becoming "stack damage percentages", which is the least interesting shape a
  tree can take. Accepted for this batch, worth watching at the next.
- **`dualWielding` shipped with two riders, not one.** The no-mechanic-wildfire
  gate wants ≥2 real users for a new condition, and this ticket was originally
  filed *because* the mage dual-wand focus was a lone rider. Twin Fangs and
  Wand Chorus land together, so the gate was satisfied rather than waived.
- **A two-handed weapon is not dual-wielding.** It fills both hand slots but is
  one weapon, so the condition counts weapons (`heldWeapons`) rather than
  occupied slots — the reading a skimmer would most plausibly get wrong.

## Active skills are a category, and cooldowns count turns (2026-07-19, #161)

The 2026-07-14 action-economy prune cut active skills, so everything in #124 is
passive: cards folded onto a value at an event. Teleport reopened the question,
and the maintainer's call was to build the **category** rather than a hardcoded
teleport — more expensive once, no rewrite when the second active arrives.

- **A skill is passive or active, never both.** An active carries a trigger and
  a cooldown instead of cards; the mixed shape, a zero cooldown, and a range
  outside `1..CombatRadius` all panic at content load.
- **It is the turn's action.** Not a bonus action — it displaces a queued move
  or attack, which is why it fits an economy that allows exactly one action per
  turn and does not touch WeGo simultaneity.
- **Cooldowns count TURNS, whichever clock ticks.** The maintainer's framing
  settled this: *"the entire world slows down… a bit like bullet time."*
  Measured in turns there is no asymmetry — 3 turns is 3 turns everywhere — and
  the wall-clock difference *is* the bubble's conceit. A seconds-denominated
  cooldown would run through bullet time and break the effect.
- **Blink does NOT pass through walls**, the opposite of the genre's usual
  blink. Escaping still requires somewhere you can see, so a rock wall stays
  cover rather than a suggestion.
- **A queued active is dropped, not deferred**, if its caster attacked or died
  that turn: a stale trigger firing a turn late would teleport someone who has
  since chosen something else.
- Cooldowns persist (`snapshotVersion` 7) — a restart must not be a free reset.

## Open flags (doc vs implementation)

- **Bubble trigger — LOS vs distance** *(decided 2026-07-14, **shipped
  2026-07-18** — #95)*. Bubbles triggered on pure distance as a placeholder;
  mutual line of sight was always the design target. **This flag is now
  closed** — see the LOS entry above.

## Ranged attacks are line-of-sight-gated *(decided 2026-07-20, #195)*

The #95 slice made bubble membership and monster aggro require mutual line of
sight, but left ranged **attacks** distance-only as a deliberate deferral (its
own slice). A code review (2026-07-19) found the consequence had become a bug:
`queueAttackLocked`'s own invariant — *"anything a ranged attack can reach is
already in the shooter's bubble"* — went false the day #95 shipped, and nobody
added the guard the comment asked for.

- **Symptom (a) — through-wall farming.** A shot fired at a monster behind a
  rock forms no bubble (sight-blocked), so it resolves in the WORLD domain
  where the monster never aggros back (its raycast is blocked too). Free kills,
  loot drops, zero risk.
- **Symptom (b) — cross-domain snipe.** `resolveEntityTargetedLocked` fetched
  its victim from `w.entities` by id, not from the resolving domain's `byHex`,
  so a shooter at world cadence could damage a monster frozen in *someone
  else's* bubble — a corpse that bubble's own death loop never processes.

**Fix — two guards, at the layer where each fact is authoritative.** Terrain
is static and attack resolution runs against pre-move positions, so sight is
knowable at submit: a ranged shot (distance > 1) now rejects at submit with
`ErrNoLineOfSight` when terrain blocks the ray — the same `seesLocked`
predicate bubbles use. Melee (adjacent) is exempt: endpoints are never
occluded. Domain membership, by contrast, is a resolution-time property, so
`resolveEntityTargetedLocked` fizzles `out_of_domain` when the named victim is
absent from `byHex`. This actions the deferred "LOS on attacks" slice as the
bug fix it turned out to be, restoring the invariant literally: reach ⊆
see-able ⊆ same-bubble.
