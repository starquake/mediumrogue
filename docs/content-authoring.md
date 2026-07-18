# Designing Gear, Classes & Species — a guide for non-developers

*This document explains how mediumrogue's content system is meant to work
(the "rule-based" or modifier-pipeline design), and what that means for
someone who wants to invent gear, classes, species, and combat mechanics
without writing code. Game-design background lives in
`design.md`; the engineering note behind this is the
`combat-modifier-pipeline` decision (plan §8, milestone 6b).*

> **Status:** the pipeline is **live** (milestone 6b.4). Events implemented:
> `deal-damage`, `take-damage`, `earn-XP`, and `aggro-range` (shipped in 6c)
> — the three species bonuses and gear's rule-carrying items all run through
> it today (`internal/game/rules.go`). `on-kill` is still future. The old
> coupled **`attack-roll`** (a single to-hit roll) has been **dropped**:
> combat is fully **ARPG**, so defence and offence are *decoupled* percentage
> chances — `glance%` (defender blunts a hit to half; softened from binary
> `evasion%` on 2026-07-15) and `crit%` (attacker crits) — never one to-hit
> roll or `d20` (see §2 and issue #69). Everything below describes the
> system as it actually runs.

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

> **Dwarf toughness** — *when* taking damage, reduce the damage by 1
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

- The world moves in **shared 4-second turns**; near enemies, time freezes
  locally into a **combat bubble** where turns wait for everyone's choice.
  Same rules everywhere — only the clock differs.
- Within a turn, **all attacks land first — against pre-move positions —
  then all movement resolves** (#104): committing to an attack always lands
  it, and stepping away does not dodge. Two combatants can kill each other
  on the same turn.
- **Melee** is an adjacent attack (click or step into an enemy; #116 — one
  click, one swing). **Ranged** (bow, magic)
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
| **aggro-range** | yes | …a WORLD-domain monster's notice radius is computed for a player | per-kind base radius (monster kinds, milestone 6c) folded through the player's own noticeability cards. Live content since #88: Padded Boots (×0.75), Iron Plate Armor (×1.25). Gear-only — no species card feeds it. The fold is clamped ≥1 |
| **crit-check** | not yet | …an attacker's chance to land a **critical hit** is computed | `crit%` weapon stats, elf precision (today a `deal-damage` chance card — see note) |
| **on-kill** | not yet | …you (or your bubble) just killed something | lifesteal ("heal 2 on kill"), kill-triggered buffs |

`crit-check` and `on-kill` are documented here so designs can be written
against them now, but nothing calls them yet. **Combat is ARPG and
decoupled** (issue #69): offence and defence are independent percentage
chances, never a single coupled to-hit roll. **Both chances are already
expressible today.** Crit is a `deal-damage` card with a `chance` condition
and a ×2 multiplier — exactly how **elf's crit** ships — so a dedicated
`crit-check` event is a convenience, not a requirement. The defender-side
chance is **`glance%`** (2026-07-15, replacing the old binary `evasion%`):
an X% chance an incoming hit is **halved**, never fully negated — the same
pattern on `take-damage` with a ×0.5 multiplier, and it applies to *all*
damage, AoE included. *(A planned `evasion-check` event used to sit in this
table; it was dropped with the softening — a fully-evaded 0-damage hit was
the one thing the pipeline couldn't express, and glance deliberately avoids
needing it. It also never wastes an attacker's whole 4-second turn on a
total miss.)*

This list will grow (likely candidates: *turn-start / turn-end* for poison
and regeneration, *on-move*, *on-death*, *on-level-up*). Growing it
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
- *Chance:* "30% of the time" — the `chance` condition, a seeded server-side
  roll (elf crits work this way).

**THEN — one effect.** The starter set:

- **Add / subtract** a flat amount (+2 damage, −1 damage taken).
- **Multiply** by a percentage (+25% XP, ×2 crit damage).
- **Clamp** (never below 1, never above the cap).
- Later, once supported: **trigger** something (heal, apply a status
  effect, push the target a hex).

A worked example, written exactly as you'd hand it to us:

> **Item — Butcher's Cleaver** (fighter melee weapon)
> Base: damage 3 (vs. sword's 4), melee.
> Rule: *when* dealing damage, *if* the target is below half health,
> *then* +3 damage.
> Intent: a finisher weapon — worse opener, brutal closer.

Notice the **Intent** line. Numbers get rebalanced in playtests; the intent
is what we preserve. Always write it.

---

## 4. Designing gear

Gear is the system this pipeline is being built for. The decided frame
(updated by the inventory-slots milestone):

- Every item has a **type**, and the type decides where it goes. The 9
  types (the gear keystone, #55/#56 — collapsed from the inventory-slots
  milestone's original 12; `shield` added by #90): one `weapon` type
  (carrying **tags** — `melee`/`ranged`/`magic`, which attacks fire it —
  plus a `two-handed?` flag), a `consumable`, a `shield` (fills the
  **off-hand** only — its whole effect is a `take-damage` rule card, e.g.
  add −1; it never carries damage/range of its own, and a two-handed weapon
  in main is evicted to the backpack when one equips), and six armor/jewelry
  types that each fill exactly one **equip slot** of the same name:
  `helmet, chest, gloves, boots, ring, amulet`. Consumables have no slot —
  they live in the backpack as stacks.
- A character has **8 equip slots**: the six armor slots above plus **two
  hand slots**, `main-hand` and `off-hand` — chosen at equip time, not fixed
  per class. A two-handed weapon occupies main-hand and locks off-hand
  (equipping one evicts whatever the off-hand held back to the backpack). No
  melee-tagged weapon equipped → unarmed punch (minimal damage). Every
  fitting held weapon fires as its own hit in combat — dual-wielding two 1H
  weapons means two hits, not a pick-one-best-weapon rule.
- Plus a **backpack of exactly 4 entries** — a gear item or a consumable
  stack (up to 5 identical consumables) per entry. Dropping things is a real
  decision; that's deliberate.
- **Any class can equip anything** (the gear keystone, #55/#56 — a decided
  reversal of the earlier "hard weapon lanes" design): there is no more
  per-weapon class list. A weapon's **tags** (`melee`/`ranged`/`magic` —
  which attacks fire it, ≥1 required) plus `twoHanded` are its only
  restriction, and tags are about *how the weapon fights*, not *who may
  carry it*. Variety still comes from gear (a fighter can carry a bow now,
  and will actually be good with it if it's in their hands), but **class
  identity moves to skills** (#56, roadmap Q10) — the class trees are where
  "a fighter feels like a fighter" gets built, not an equip-time gate.
  A character never has more than one class — multi-classing does not
  exist — but that's a class-selection rule, not a wearability rule.
- The planned trade-off axes for weapons: **speed vs. damage vs. reach**;
  for magic: **damage vs. control vs. support**.

**A gear card:**

```
Name:        (evocative — names carry half the flavor)
Type:        weapon (+ tags: melee / ranged / magic — pick ≥1; + two-handed?
             yes/no) / shield / helmet / chest / gloves / boots / ring /
             amulet / consumable
             (the slot follows from the type — a weapon lands in a hand
             chosen at equip time, not a fixed slot; a shield always fills
             the off-hand; don't specify it separately. No "wearable by"
             line — any class may equip any item; tags describe how a
             weapon fights, not who may carry it)
Base stats:  damage, range in hexes (0 = melee), area radius (0 = single target)
             (weapons only — armor/jewelry cards usually carry rules instead)
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

**A consumable card** is smaller — a consumable never equips and carries no
when/if/then rules; drinking it is an *action* (your whole turn in a fight,
free outside one), not a combat event:

```
Name:        (evocative)
Type:        consumable
Effect:      what drinking one does — today's vocabulary: heal N HP
             (clamped to your max; `heal` is a field on the item, not a
             pipeline rule)
Stacks:      up to 5 identical ones share a backpack entry; stacks never
             split, drinking uses one
Drops from:  same transcription rule as gear
Intent:      the one-line reason this item exists
```

Base-stats-only items are completely fine — the plain "speed vs damage vs
reach" spread is the bread and butter; rule-carrying items are the spice.
Armor and jewelry are usually the opposite: little or no base stats, one
good rule (Leather Armor: *when* taking damage, *then* −1, floor 1;
Headband of Learning: *when* earning XP, *then* ×1.05).

More examples of the range of what cards can express:

> **Longspear** (weapon — melee) — damage 4, range 1 hex (attack without
> being adjacent). No rules. Intent: reach as a trade-off; poke from behind
> a dwarf friend.

> **Hunting Bow of the Pack** (weapon — ranged) — damage 3, range 4.
> Rule: *when* dealing damage, *if* at least one ally shares the bubble,
> *then* +2 damage. Intent: a party-play bow — weaker solo.

> **Ember Staff** (weapon — magic) — damage 6, range 4, area radius 1, two-handed.
> Rule: *when* dealing damage, *if* the target is adjacent to you, *then*
> double damage. Intent: a risky brawler-mage staff; rewards standing
> dangerously close.

---

## 5. Designing classes

Classes are the most protected part of the design. The three launch classes
and their identities are **decided** (plan §0):

| | Starting kit | Damage | Toughness | Role |
|---|---|---|---|---|
| **Fighter** | Iron Sword | medium | tanky | holds the front |
| **Rogue** | Dagger + Shortbow | high single-target | squishy | flexible mid-line |
| **Mage** | Oak Wand + Ember Focus | area damage | squishy | back line vs. groups |

In rule terms, a class is: **base max HP + a starting kit + (future) its
Class skill tree (#61/Q10) — gear is NOT class-gated anymore (#56): anyone
can equip anything, and class identity comes from skills, not gear locks.**
Per-level growth (+HP, +damage) currently applies evenly to everyone.

What you can design here:

- **Depth within a class** — mostly via its gear pool (§4) and, later,
  **class ability rules**, e.g. *fighter:* "when taking damage, if 2+
  enemies are adjacent, −1 damage taken (shield wall)"; *rogue:* "when
  dealing damage, if the target is at full HP, +crit chance (ambush)."
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
> **Elf — deadly precision:** *when* dealing damage, X% chance the hit
> crits for double damage.
> **Dwarf — stone-tough:** *when* taking damage, −X (never below 1).

(The X's are tuning knobs, set in playtests.)

A good new-species proposal is **one or two passive rules that create a
playstyle** and stay fun across all classes. Sketches of the shape:

> **Halfling — hard to pin down:** *when* taking damage, X% chance the hit
> only **glances** (half damage). (This is the `glance%` mechanic — issue
> #69/#91; it's a plain chance-conditioned `take-damage` card, expressible
> today exactly like elf's crit, and reusable for light armour, smoke bombs,
> blur spells…)

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

- **Simultaneous phased turns** is the bedrock (attacks-then-moves, #104: a
  committed attack always lands and retreat means trading hits for distance
  instead of a free dodge). Mutual kills are a feature either way. Nothing
  may depend on within-turn ordering like "who acted first."
- **Determinism:** all randomness is a seeded server-side roll (a per-scope
  PCG keyed on world seed + turn). "X% chance" is fine; "player skill-shot
  timing" is not — there are no reflexes in this game, ever.
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
Type:       gear / consumable / species / class tweak / mechanic
Item type:  weapon (+ tags: melee / ranged / magic; two-handed: yes/no)
            or: helmet / chest / gloves / boots / ring / amulet / consumable
            (the slot follows from the type; weapons go to a hand)
Base stats: (weapons: damage / range / area; consumables: heal N;
             armor/jewelry, species & mechanics: usually n/a)
Rules:
  - WHEN <deal-damage | take-damage | earn-XP | crit-check | on-kill>
    IF   <condition(s), or "always">
    THEN <add/subtract N | +N% | chance-based …>
  (consumables carry no rules — their effect is the drink action itself)
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
