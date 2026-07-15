# Attack Resolution Order & Glancing Blows: Design

*Status: approved 2026-07-15 (brainstorm session with the maintainer).
Confirms issue #104, amends #69/#91. Design docs updated in the same PR;
implementation follows a separate plan after spec review (standard
milestone-slice pause).*

## Goal

Fix the "my whole 4-second turn was wasted" sting without giving up what makes
WeGo chases tense. The trigger was playtest frustration: attacks whiff when
the target steps away on the same tick (the retreat-dodge), and the
decided-but-unbuilt binary `evasion%` (#69/#91) would have reintroduced the
same sting at a bounded rate. Three decisions, one session:

1. **Attacks resolve before moves** (confirms #104).
2. **One action per 4-second turn, reaffirmed** (move XOR attack).
3. **Glancing blows replace binary evasion** (amends #69/#91): the
   defender-side chance stat halves a hit, never negates it.

## Decision 1 — Attacks resolve before moves (#104 confirmed)

**Rule:** within a turn, all attacks resolve first, simultaneously, against
**pre-move positions**; then all moves resolve. Committing to an attack this
tick always produces damage (a valid target at commit time cannot vacate out
of the hit).

**What it replaces:** the current order (moves, then attacks against
post-move positions) makes stepping away an automatic dodge — celebrated in
`game-identity.md` as "stepping away genuinely dodges the swing", but in play
it reads as a whiffed turn for the attacker, and it hit the mage hardest:
her ground-targeted AoE blast lands on a hex the target already left. The
bow got a partial fix (entity-targeted shots, playtest batch 2 item 7); this
finishes the job for every attack type by fixing the resolution order itself.

**Fleeing survives — the story changes.** Retreat is no longer a free dodge;
it is *trading hits for distance*. Because of the one-action economy
(decision 2), an adjacent chaser must choose each turn: bump you (you gain a
hex) or chase you (no damage). Escape stays a real tactic; it just costs HP
now. `game-identity.md`'s retreat-dodge line is rewritten to this story.

**Mutual kills survive unchanged** — attacks are still simultaneous within
the attack phase (shared damage map), so two combatants going for each other
still take each other down; that drama is untouched.

**Consequences for existing rules:**

- Bump-to-attack no longer checks the post-move board: a move intent whose
  next step is a hostile-held hex converts to an attack against that
  occupant's pre-move position and always lands. The `bump_target_vacated`
  fizzle reason disappears.
- Entity-targeted ranged (bow) validates range against pre-move positions of
  both shooter and victim; the post-move re-aim logic goes away. Ground-
  targeted AoE (mage) stays hex-targeted and now hits whoever is on the
  target hexes *before* moves resolve.
- Death timing: an entity killed in the attack phase does not get its move.
  (Today the reverse holds — a mover escapes the killing blow. This is the
  point of the change.)

## Decision 2 — One action per turn, reaffirmed

Move XOR attack per 4-second turn stays. Examined and rejected: **full
move+attack every turn**, because with uniform 1-hex/turn speed it breaks
both chase dynamics at once —

- *Fleeing melee becomes impossible*: an equal-speed chaser moves **and**
  hits every turn; you are struck every turn until someone dies.
- *Kiting becomes free*: a ranged attacker steps back and shoots every turn
  while an equal-speed melee enemy never closes — zero-risk kills.

Patching those degeneracies needs speed stats, engagement/rooting rules, or
zone-of-control — new machinery drifting toward the TTRPG turn structure
`game-identity.md` rejects — plus a rebalance of every HP/damage number and
double the input per 2-second window. One-action is what keeps both chase
dilemmas alive: every attack costs the attacker their step, on both sides of
a chase.

**Noted future option (not committed):** *melee move+bump* — a move path
ending in an enemy walks the remaining steps and strikes in the same turn.
The surgical anti-kiting patch if closing distance ever feels bad in
playtests; it helps melee close without making fleeing impossible.

## Decision 3 — Glancing blows replace binary evasion (amends #69/#91)

**Rule:** the defender-side chance stat is **`glance%`** — an X% chance that
an incoming hit is **halved** (`mulPct 50`), never fully negated. The Rogue
gets it as the class passive; **proposed 20%** (a tunable `internal/protocol`
constant, higher than the old 10% evasion proposal because a glance is weaker
per proc than a full evade). Attacker-side `crit%` is untouched.

**Why glancing over binary evasion:**

- **No whiffed turns.** A binary miss wastes the attacker's whole 4-second
  turn — the exact sting decision 1 just removed, and the feel concern
  already raised on #69 ("small HP pools mean a binary miss wastes your
  whole turn"). A glance always shows damage happening; defense softens
  instead of nullifies.
- **Zero engine work.** Binary evasion needed a new pre-damage
  `evasion-check` event (a fully-evaded hit deals 0, which the `take-damage`
  fold cannot express — it floors landed hits at 1). A glance is an ordinary
  chance-conditioned `take-damage` card with `mulPct 50` — the exact mirror
  of how crit already ships (`chance` + `mulPct 200` on `deal-damage`, see
  `internal/game/rules.go` and the elf card in `content.go`). Issue #91
  shrinks from an engine slice to a content entry plus one protocol constant.
- **Still decoupled ARPG grammar.** Defence (`glance%`) and offence
  (`crit%`) remain independent percentage stats folded at their own events —
  no coupled to-hit roll appears anywhere. The identity guardrail is
  unchanged; only the vocabulary moves (`evasion%` → `glance%`).

**AoE carve-out dropped (supersedes decision Q6's first half).** The old rule
"AoE always hits — evasion applies to targeted attacks only" existed so
binary evasion could not make anything unhittable and the mage kept an
anti-evasion niche. A glance cannot make anything unhittable — worst case
every hit lands at half — so the carve-out buys nothing and costs a new
condition kind (the pipeline would need an attack-type condition to express
it). **Glance applies to all incoming damage, including AoE.** Q6's second
half (flat reduction as the anti-mage answer) stands.

**Interaction with the damage floor:** the `take-damage` fold still floors
landed hits at 1, so a glanced 1-damage hit stays 1. Fine — glance is
protection against real hits, not chip damage.

## Determinism note (for the implementation plan)

Both changes touch turn-RNG consumption order: the glance roll adds a draw,
and reordering attack/move phases may reorder existing draws (victim-pick,
tie-breaks). Existing seeded test expectations will shift — **re-derive the
expected values, never weaken the assertions**, per the standing determinism
rule. Sort any map-derived slice before drawing, as always.

## Scope

- **This PR:** this spec + design-doc alignment (`design.md`,
  `design-decisions.md`, `game-identity.md`, `content-authoring.md`, the
  `FEATURES.md` decided-but-not-built entry) + issue updates (#104
  confirmed, #91 amended to glance, #69 amendment note).
- **Not this PR:** any code. The resolution-order flip
  (`internal/game/world.go` — `resolveCombatLocked`, `moveAndBumpLocked`,
  `resolveRangedLocked`) and the Rogue glance card land via a separate
  implementation plan after this spec is reviewed. `FEATURES.md`'s
  description of *implemented* behavior (moves-then-attacks, retreat-dodge)
  stays accurate until that PR changes the behavior, and updates in that
  same PR.
