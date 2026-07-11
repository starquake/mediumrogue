# Designing Gear, Classes & Species — a guide for non-developers

*This document explains how mediumrogue's content system is meant to work
(the "rule-based" or modifier-pipeline design), and what that means for
someone who wants to invent gear, classes, species, and combat mechanics
without writing code. Game-design background lives in
`roguelike-mp-plan.md`; the engineering note behind this is the
`combat-modifier-pipeline` decision (plan §8, milestone 6b).*

> **Status:** the pipeline is **live** (milestone 6b.4). Events implemented:
> `deal-damage`, `take-damage`, `earn-XP` — the three species bonuses and
> gear's rule-carrying items all run through it today
> (`internal/game/rules.go`). `attack-roll`, `on-kill`, and `aggro-range` are
> still future (§2's event table below flags each as not-yet-implemented);
> a design written in that vocabulary now still lands as a Tier-2 vocabulary
> addition, not a rewrite. Everything below describes the system as it
> actually runs.

---

## 1. The big idea: content is cards, not code

In a naive implementation, every piece of content is code. "Dwarves take
less damage" means a programmer finds every place damage is dealt and adds
*"…unless the target is a dwarf."* Ten items later the combat code is a
thicket of exceptions, and adding an eleventh means a developer carefully
editing all of it.

The rule-based pattern flips this. The combat engine stays **generic and
dumb** — it knows how to swing weapons, move entities, and count hit points,
but it knows nothing about dwarves or frost daggers. All the flavor lives in
**rules** attached to content: small, self-contained statements of the form

> **When** *[a specific moment in combat]*,
> **if** *[some condition holds]*,
> **then** *[change a number or trigger something]*.

A species is a name plus one or two rules. A gear item is a name, some base
stats, plus zero or more rules. At each key moment ("event"), the engine
gathers every rule that applies — from your species, your class, each piece
of equipped gear, any active buffs — and runs the number through all of
them, like a factory line of small machines each making one adjustment.

**Concretely:** dwarf damage reduction stops being a special case inside the
combat code and becomes a card that reads:

> **Dwarf toughness** — *when* taking damage, reduce the damage by 2
> (never below 1).

The engine doesn't know what a dwarf is. It just sees "this entity carries a
take-damage rule" and applies it. A "Stone Amulet" item with the exact same
rule needs **zero new engine work** — it's just another card.

**Why this matters for you:** it means designing content *is* the design
work. If an idea can be written as when/if/then cards using the vocabulary
in this document, it can usually go into the game as data — no programmer
required per item. Your job as designer is to fill in cards; the
programmer's job is to keep the vocabulary of moments, conditions, and
effects rich enough.

---

## 2. The combat model your rules plug into

You need a feel for how a fight actually runs, because rules hook into its
moments. The short version (full detail: plan §5):

- The world moves in **shared 5-second turns**; near enemies, time freezes
  locally into a **combat bubble** where turns wait for everyone's choice.
  Same rules everywhere — only the clock differs.
- Within a turn, **all movement resolves first, then all attacks land** —
  simultaneously, against post-move positions. Two built-in consequences:
  stepping away from a melee attacker genuinely dodges the hit, and two
  combatants can kill each other on the same turn.
- **Melee** is bump-to-attack (walk into an enemy). **Ranged** (bow, magic)
  reaches 4 hexes, needs no line of sight, and never hits allies. Mage magic
  is an **area hit** (the target hex plus its ring of neighbors).
- Up to 5 allies can stand on one hex; a single-target hit on a stack picks
  a **random member**.
- **XP is paid per kill, instantly, in full, to every player inside the
  bubble** at that moment. Death costs you the progress inside your current
  level, never the level itself.

### The events (the "when" of every rule)

These are the moments the engine will expose. Every rule must name one:

| Event | Live? | The moment… | Example rules that hook here |
|---|---|---|---|
| **deal-damage** | yes | …your hit's damage number is computed | weapon enchantments, **"bonus vs undead"** (shipped as **Wyrmslayer Greatsword**, ×1.5 vs dragons — see `targetKind` below), damage buffs, **elf's crit** (a chance-conditioned ×2 — see the note below) |
| **take-damage** | yes | …an incoming hit's damage is applied to you | dwarf toughness, armor, shields, vulnerabilities |
| **earn-XP** | yes | …an XP award is computed for you | human fast-learner, an XP-boosting trinket |
| **aggro-range** | yes | …a WORLD-domain monster's notice radius is computed for a player | per-kind base radius (monster kinds, milestone 6c) folded through the player's own noticeability cards; no card uses this yet — future sneaky/loud gear |
| **attack-roll** | not yet | …an attack is being aimed: hit chance, crit chance, crit size | "lucky" weapons, accuracy debuffs, a miss chance |
| **on-kill** | not yet | …you (or your bubble) just killed something | lifesteal ("heal 2 on kill"), kill-triggered buffs |

`attack-roll` and `on-kill` are documented here so designs can be written
against them now, but nothing calls them yet — a card naming one would sit
unused until the event ships. **Elf's crit ended up modeled as a
`deal-damage` effect** (a chance-conditioned ×2 multiplier), not a separate
`attack-roll` roll — fewer moving parts, same result, and it's the pattern
to reach for first: only add a real `attack-roll` event once something needs
an actual to-hit/miss check (a genuine `attack-roll` candidate: the sketch
species idea *"Halfling — hard to hit"* in §6 below).

This list will grow (likely candidates: `aggro-range` for the backlogged
"monster aggro radius via the pipeline" idea, *turn-start / turn-end* for
poison and regeneration, *on-move*, *on-death*, *on-level-up*). Growing it
is engine work — see §7 on what's cheap vs. expensive.

---

## 3. The vocabulary of a rule

Every rule is three fill-in-the-blank slots.

**WHEN — one event** from the table above.

**IF — conditions (optional, combinable).** Things a rule can check:

- *About the actors:* attacker/target species, class, level, **monster kind**
  (shipped as `targetKind` — the target is a monster of a specific
  registered kind, e.g. "dragon"; validated against the monster registry at
  load), current HP ("below half health"), party membership.
- *About the situation:* distance to target, target adjacent or not, number
  of enemies in the bubble, allies on your hex, melee vs. ranged hit.
- *Chance:* "30% of the time" — the dice-roll condition (elf crits work
  this way).

**THEN — one effect.** The starter set:

- **Add / subtract** a flat amount (+2 damage, −1 damage taken).
- **Multiply** by a percentage (+25% XP, ×2 crit damage).
- **Clamp** (never below 1, never above the cap).
- Later, once supported: **trigger** something (heal, apply a status
  effect, push the target a hex).

A worked example, written exactly as you'd hand it to us:

> **Item — Butcher's Cleaver** (fighter melee weapon)
> Base: damage 4 (vs. sword's 5), melee.
> Rule: *when* dealing damage, *if* the target is below half health,
> *then* +3 damage.
> Intent: a finisher weapon — worse opener, brutal closer.

Notice the **Intent** line. Numbers get rebalanced in playtests; the intent
is what we preserve. Always write it.

---

## 4. Designing gear

Gear is the system this pipeline is being built for. The decided frame:

- Every character has two weapon slots: **close combat** and **ranged**.
  You can own several items but only the equipped one per slot counts.
  No melee weapon equipped → unarmed punch (minimal damage).
- Classes have hard weapon lanes (§5) — gear creates **variety within a
  lane**, it never breaks the lane. There will be many fighter melee
  weapons; there will never be a fighter bow.
- The planned trade-off axes for weapons: **speed vs. damage vs. reach**;
  for magic: **damage vs. control vs. support**.

**A gear card:**

```
Name:        (evocative — names carry half the flavor)
Slot:        close / ranged   (later maybe: armor, trinket)
For class:   fighter / rogue / mage / any
Base stats:  damage, range in hexes (0 = melee), area radius (0 = single target)
Rules:       0 or more when/if/then rules
Drops from:  what kind of monster/place should yield this?
             (real since milestone 6c: loot authority lives on the MONSTER,
             not the item — each monster kind owns its own weighted drop
             table (`monsterDef.drops`) and its own drop chance, so this
             line on a gear card is now a literal transcription instruction:
             "add {defID, weight} to kind X's table", not aspirational
             flavor text)
Intent:      the one-line reason this item exists
```

Base-stats-only items are completely fine — the plain "speed vs damage vs
reach" spread is the bread and butter; rule-carrying items are the spice.

More examples of the range of what cards can express:

> **Longspear** (fighter) — damage 4, range 1 hex (attack without being
> adjacent). No rules. Intent: reach as a trade-off; poke from behind a
> dwarf friend.

> **Hunting Bow of the Pack** (rogue, ranged) — damage 3, range 4.
> Rule: *when* dealing damage, *if* at least one ally shares the bubble,
> *then* +2 damage. Intent: a party-play bow — weaker solo.

> **Ember Staff** (mage, ranged) — damage 2, range 4, area radius 1.
> Rule: *when* dealing damage, *if* the target is adjacent to you, *then*
> double damage. Intent: a risky brawler-mage staff; rewards standing
> dangerously close.

---

## 5. Designing classes

Classes are the most protected part of the design. The three launch classes
and their identities are **decided** (plan §0):

| | Weapons | Damage | Toughness | Role |
|---|---|---|---|---|
| **Fighter** | melee only | medium | tanky | holds the front |
| **Rogue** | dagger + bow, auto-picked by distance | high single-target | squishy | flexible mid-line |
| **Mage** | magic only (area hits) | area damage | squishy | back line vs. groups |

In rule terms, a class is: **allowed weapon lanes + base max HP + default
starting weapons + (later) class ability rules**. Per-level growth (+HP,
+damage) currently applies evenly to everyone.

What you can design here:

- **Depth within a class** — mostly via its gear pool (§4) and, later,
  **class ability rules**, e.g. *fighter:* "when taking damage, if 2+
  enemies are adjacent, −1 damage taken (shield wall)"; *rogue:* "when
  attack-rolling, if the target is at full HP, +crit chance (ambush)."
- **Class-distinct level growth** — e.g. mage damage scales faster than
  mage HP. Cheap to do, big balance lever.
- **A fourth class** is possible but expensive everywhere (balance, sprites,
  party math for 15 players) — a proposal needs a role the trio can't cover,
  described in the same terms as the table above.

---

## 6. Designing species

Species are deliberately light: **one passive style, freely combinable with
any class** — species picks a *style*, class picks a *job*. The decided
three, written as rule cards:

> **Human — fast learner:** *when* earning XP, +X%.
> **Elf — deadly precision:** *when* attack-rolling, X% chance the hit
> crits for double damage.
> **Dwarf — stone-tough:** *when* taking damage, −X (never below 1).

(The X's are tuning knobs, set in playtests.)

A good new-species proposal is **one or two passive rules that create a
playstyle** and stay fun across all classes. Sketches of the shape:

> **Halfling — hard to hit:** when attack-rolled *against*, X% chance the
> attack misses outright. (Needs a "miss" concept at attack-roll — small
> engine addition, then reusable for smoke bombs, blur spells…)

> **Orc — bloodlust:** when dealing damage, if below half HP, +X damage.
> A high-risk style that gets scarier as it gets hurt.

The test for any species idea: is it a *passive* (it should never require
pressing a button), is it *felt* often enough to matter, and does it stay
interesting on all three classes?

---

## 7. Designing combat mechanics — and what things cost

You can absolutely propose mechanics, not just content. The honest guide is
**what a proposal costs to build**:

**Tier 1 — free (pure data).** New gear/species/class-tweak cards using
existing events, conditions, and effects; any retuning of numbers. This is
the everyday designer loop: write card → it appears in game → playtest.

**Tier 2 — cheap (one small engine addition, then reusable forever).** A
new *condition* ("target hasn't moved this turn"), a new *effect verb*
("heal", "push one hex"), or a new *event* ("turn-start"). A developer adds
it once; every future card can use it. **This is how the vocabulary grows —
and your proposals drive which words get added.** If several of your card
ideas keep needing the same missing word, that's the signal to build it.

**Tier 3 — real projects (new systems).** Status effects that persist
across turns (poison, stun, slow — needs duration/stacking bookkeeping),
consumables, armor slots, an active-abilities system (breaks the "one intent
per turn: move or attack" simplicity), summons, terrain effects. Worth
proposing! But these get scheduled as milestones, not slipped in as cards.

A few engine truths to design *with*, not against:

- **Simultaneous phased turns** (moves, then attacks) is the bedrock.
  Retreat-dodging and mutual kills are features. Nothing may depend on
  within-turn ordering like "who acted first."
- **Determinism:** all randomness is a server dice roll. "X% chance" is
  fine; "player skill-shot timing" is not — there are no reflexes in this
  game, ever.
- **No friendly fire** (current rule), stack cap 5, random-member hits on
  stacks, XP-by-presence-in-bubble — see plan §5 if a design touches these.
- **Every number is a knob.** Never fight over 3 vs 4 damage on paper;
  fight for the *intent*, we tune the number live.

---

## 8. The design-card template (copy me)

One card per idea. A shared doc or spreadsheet of these is the ideal
handoff format — they translate almost 1:1 into the game's data: this is now
literally the shape of `internal/game/content.go`'s `itemDefs`/`*Cards`
registry, which every gear card and species passive feeds today.

```
### <Name>
Type:       gear / species / class tweak / mechanic
Slot/lane:  (gear: close|ranged + which class; else: n/a)
Base stats: (gear: damage / range / area; species & mechanics: n/a)
Rules:
  - WHEN <attack-roll | deal-damage | take-damage | earn-XP | on-kill>
    IF   <condition(s), or "always">
    THEN <add/subtract N | +N% | chance-based …>
Intent:     one sentence — the feeling or decision this creates
Fantasy:    optional — name/flavor/description text
Questions:  anything you're unsure the engine can do (→ tier 2/3 check)
```

**Rules of thumb for good cards:**

1. **One idea per card.** An item with four rules is usually four item ideas.
2. **Design decisions, not power.** The best cards make a player *choose
   something* (stand close? save it for the boss? bring a party?). Flat
   "+2 damage, no strings" is filler — allowed, but it's the spice cards
   that make loot exciting.
3. **Downsides are content too.** "Worse base, great situationally" (the
   Cleaver, the Ember Staff) is the most reliable fun-item shape.
4. **Think in drops.** Every gear card should answer: where does a player
   find this, and why do they grin when they do?
5. **When in doubt, write it anyway** and put the doubt in *Questions* —
   "can a rule see whether the target moved?" is exactly the feedback that
   shapes what we build next.

---

*Questions or proposals: drop cards in the group chat or a shared doc; they
get reviewed against this vocabulary and either land as data (tier 1),
queue a vocabulary addition (tier 2), or become a milestone discussion
(tier 3).*
