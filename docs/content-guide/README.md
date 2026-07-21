# mediumrogue — content design guide

> **Generated from the live registries.** Every number below is what the game
> actually runs. Regenerate with `make guide`; never hand-edit this file.
>
> The prose is authored, the tables are generated. If a number looks wrong,
> the game changed and this was regenerated — trust the table. If the
> *reasoning* looks wrong, argue with it.

## The one tell: coupling

This is an **ARPG**, never a TTRPG. The distinction is not vocabulary — it is
whether a mechanic folds the attacker's numbers and the defender's numbers
into *one* roll.

- **TTRPG:** attacker rolls, the defender's Armour Class sets the target, one
  roll decides hit-or-miss. Attack bonus and AC are coupled.
- **ARPG:** two *independent* percentage checks. `crit%` is the attacker's
  alone, `glance%` the defender's alone. Neither reads the other side's stats.
  Damage is then a fold, not a comparison.

**A proposal wearing percentages can still be TTRPG.** "5% chance to miss,
modified by the target's evasion" is a coupled roll in percentage clothing.
Ask only: *does one side's number change the other side's outcome inside a
single check?* If yes, translate it.

## The four drift cases — and their translations

| Arrives as | Why it doesn't fit | Build this instead |
|---|---|---|
| A miss chance / to-hit roll | Couples both sides; a whiff is a null turn in a 4-second WeGo loop. | A defender-side `glance%`. A glance **halves** a hit — it never negates it. |
| Flat `−N` damage reduction (armour) | Subtractive mitigation stacks into the ≥1 clamp and stops meaning anything. | A percentage on `take-damage`. (The dwarf passive's −1 is the one deliberate exception — a species trait that can't stack with itself.) |
| A stance, mode, or "block" | A mode economy this game doesn't have — one action per turn — and hit-negation was rejected when `evasion%` became `glance%`. | A `glance%` bump, conditioned on the situation the stance was meant to describe. |
| "Works with swords / axes / polearms" | Weapon categories don't exist. Weapons carry *tags* (how a weapon is used) and a *damage type* (what it deals). | `weaponTagged` for how it's swung, or `damageType` for what lands. |

## How a card works

Every species trait, item property and skill is a **rule card**: pure data,
never code. A card says *at this event*, *when these conditions hold*, *do this
to the value*.

Fold order is fixed: within one event, **every `add` sums first**, then **every
percentage sums and applies once**. Percentages never compound pairwise — two
+50% cards are +100%, not ×2.25.

### Events — the moments a value folds

| Event | Parameter | Meaning |
|---|---|---|
| `deal-damage` | — | A hit's damage, attacker side. Cards here belong on weapons. |
| `take-damage` | — | A hit's damage, victim side — resistances and vulnerabilities. Cards here belong on worn kit. |
| `earn-xp` | — | An XP award, before it lands. Folds without rng, so chance conditions are rejected. |
| `aggro-range` | — | How far a monster notices you from. Clamped to ≥1, so noticeability can never reach zero. |

### Conditions — when a card fires

| Condition | Parameter | Meaning |
|---|---|---|
| `chance` | n = percent | Fires n% of the time. This is where crit and glance live — a percentage, never a roll against the other side. |
| `damageType` | s = damage type | The type of the hit being folded: what is LANDING on you in a take-damage fold, what you are SWINGING in a deal-damage one. |
| `weaponTagged` | s = weapon tag | The weapon being swung carries that tag. A tag is how a weapon is USED; a damage type is what it DEALS. |
| `targetKind` | s = monster kind | The victim is a monster of that registered kind. Never holds against a player. |
| `attackerSpecies` | s = species | Who SWINGS is of that species — gear a class can use but that sings in one species' hands. |
| `shieldEquipped` | — | The DEFENDER holds a shield in its off-hand. Defender-side is a requirement, not a convention. |
| `dualWielding` | — | The ATTACKER holds a weapon in both hands. A two-handed weapon is NOT dual-wielding — it fills both slots but is one weapon. |
| `targetAdjacent` | — | The victim is in an adjacent hex. |
| `allyInBubble` | — | Another friendly is in this combat bubble. |
| `targetHPFull` | — | The victim is at full HP — opener flavour. |
| `targetHPBelowPct` | n = percent of maxHP | The victim is below n% of its own max HP. Scales with the target. |
| `targetHPBelowFlat` | n = hit points | The victim is below n absolute HP. Deliberately does NOT scale: a mop-up rule stays a mop-up rule against a boss. |

### Effects — what a card does

| Effect | Parameter | Meaning |
|---|---|---|
| `add` | n (may be negative) | Adds n. Every add in a fold sums first, before any percentage applies. |
| `mulPct` | n = percent (200 = double) | Scales by n%. Percentages sum within one fold and apply once — never compounding pairwise. |

### Damage types and weapon tags

Damage types: `blunt`, `sharp`, `fire`, `ice`, `holy`, `chaos`.

Weapon tags: `melee`, `ranged`, `magic`.

## Which cards may go on what

Enforced at load — a violation panics the server at start, not mid-combat.
**Offensive cards** (`deal-damage`) belong on weapons; **defensive cards**
(`take-damage`) belong on worn kit. That is what lets a stat line stay terse:
on a weapon a number means damage dealt, on armour it means damage taken, so
no line needs a "Taken" suffix to disambiguate.

It follows that stat text is **never written by hand** — it is rendered from
the card. Flavour text carries no numbers at all: a hand-written line
restating its own card is a drift surface, and the load-time validator rejects
any digit in flavour.

## Calibration — pitch new numbers against these

| Anchor | Value | What it means |
|---|---|---|
| Fists damage | `1` | An unarmed player's hit — the floor every weapon is measured against. |
| Glance damage | `50` | What a glance leaves of a hit, as a percent. A glance HALVES; it never negates. |
| Combat radius | `6` | A combat bubble's reach in hexes. A weapon's range + AoE can never exceed it. |
| Default aggro radius | `10` | How far a monster notices from unless its kind overrides it. Always greater than the combat radius. |
| Leash multiplier | `2` | How far past its aggro radius a monster chases before walking home. |
| Item stack cap | `5` | Consumables per backpack stack. |
| Forest sight cost | `2` | What one forest hex costs a line of sight. |
| Percent base | `100` | 100. A mulPct of 200 doubles; of 50 halves. |

## Every item today

| Item | Type | Damage type | Tags | Damage | Range | AoE | Heal | Stat lines |
|---|---|---|---|---|---|---|---|---|
| **Ancient Dwarven Mattock**<br>`ancient-dwarven-mattock` | weapon | blunt | melee | 4 | melee | — | — | Damage 4<br>+3 Damage (Dwarf)<br> |
| **Butcher's Cleaver**<br>`butchers-cleaver` | weapon | sharp | melee | 3 | melee | — | — | Damage 3<br>+3 Damage vs Below 50% HP<br> |
| **Consecrated Mace**<br>`consecrated-mace` | weapon | holy | melee | 4 | melee | — | — | Damage 4<br> |
| **Dagger**<br>`dagger` | weapon | sharp | melee | 4 | melee | — | — | Damage 4<br> |
| **Duelist's Saber**<br>`duelists-saber` | weapon | sharp | melee | 4 | melee | — | — | Damage 4<br>10% chance ×2 Damage<br> |
| **Ember Brand**<br>`ember-brand` | weapon | fire | melee | 4 | melee | — | — | Damage 4<br> |
| **Ember Focus**<br>`ember-focus` | weapon | fire | magic | 3 | 4 | 1 | — | Damage 3<br>Range 4<br>AoE 1<br> |
| **Ember Staff**<br>`ember-staff` | weapon<br>two-handed | fire | magic | 6 | 4 | 1 | — | Damage 6<br>Range 4<br>AoE 1<br>×2 Damage vs Adjacent<br> |
| **Frostbrand**<br>`frostbrand` | weapon | ice | melee | 4 | melee | — | — | Damage 4<br> |
| **Frostward Charm**<br>`frostward-charm` | amulet | — | — | — | melee | — | — | +50% Ice Resistance<br> |
| **Headband of Learning**<br>`headband-of-learning` | helmet | — | — | — | melee | — | — | +5% XP<br> |
| **Healing Potion**<br>`healing-potion` | consumable | — | — | — | melee | — | 5 | +5 HP<br>Stacks to 5<br> |
| **Infernal Chain Mail**<br>`infernal-chain-mail` | chest | — | — | — | melee | — | — | +50% Fire Resistance<br> |
| **Iron Kite Shield**<br>`iron-kite-shield` | shield | — | — | — | melee | — | — | +20% Damage Resistance<br> |
| **Iron Plate Armor**<br>`iron-plate-armor` | chest | — | — | — | melee | — | — | +20% Damage Resistance<br>⚠ +25% Aggro Range<br> |
| **Iron Sword**<br>`iron-sword` | weapon | sharp | melee | 4 | melee | — | — | Damage 4<br> |
| **Iron Warhammer**<br>`iron-warhammer` | weapon | blunt | melee | 5 | melee | — | — | Damage 5<br> |
| **Ironbound Gauntlets**<br>`ironbound-gauntlets` | gloves | — | — | — | melee | — | — | +50% Blunt Resistance<br> |
| **Ironhead Greatmaul**<br>`ironhead-greatmaul` | weapon<br>two-handed | blunt | melee | 9 | melee | — | — | Damage 9<br> |
| **Leather Armor**<br>`leather-armor` | chest | — | — | — | melee | — | — | +10% Damage Resistance<br> |
| **Longbow**<br>`longbow` | weapon | sharp | ranged | 3 | 6 | — | — | Damage 3<br>Range 6<br> |
| **Misericorde**<br>`misericorde` | weapon | sharp | melee | 4 | melee | — | — | Damage 4<br>15% chance ×2 Damage<br> |
| **Oak Wand**<br>`oak-wand` | weapon | blunt | melee | 2 | melee | — | — | Damage 2<br> |
| **Pack Bow**<br>`pack-bow` | weapon | sharp | ranged | 3 | 4 | — | — | Damage 3<br>Range 4<br>+3 Damage with an Ally<br> |
| **Padded Boots**<br>`padded-boots` | boots | — | — | — | melee | — | — | −25% Aggro Range<br> |
| **Pilgrim's Mantle**<br>`pilgrims-mantle` | chest | — | — | — | melee | — | — | +50% Chaos Resistance<br> |
| **Shortbow**<br>`shortbow` | weapon | sharp | ranged | 4 | 4 | — | — | Damage 4<br>Range 4<br> |
| **Venom Fang**<br>`venom-fang` | weapon | sharp | melee | 3 | melee | — | — | Damage 3<br>+4 Damage vs Full HP<br> |
| **Staff of the War Mage**<br>`war-mage-staff` | weapon<br>two-handed | fire | magic | 6 | 4 | 1 | — | Damage 6<br>Range 4<br>AoE 1<br>×2 Damage vs Below 6 HP<br> |
| **Warded Gambeson**<br>`warded-gambeson` | chest | — | — | — | melee | — | — | +50% Sharp Resistance<br> |
| **Wooden Buckler**<br>`wooden-buckler` | shield | — | — | — | melee | — | — | +10% Damage Resistance<br> |
| **Wyrmslayer Greatsword**<br>`wyrmslayer-greatsword` | weapon<br>two-handed | holy | melee | 9 | melee | — | — | Damage 9<br>+50% Damage vs Dragons<br> |

⚠ marks a **drawback** — a stat that makes its holder worse. Sign alone can't
say: `+25% Aggro Range` is a cost, `+5% XP` is not.

## Every monster kind today

A kind **names** its weapon in the item registry rather than carrying a copy of
one, so damage, reach and damage type below are the weapon's own numbers. The
Cards column is the kind's *own* cards — its identity — separate from anything
its weapon carries.

| Kind | HP | Weapon | Damage | Reach | Damage type | XP | Aggro | Drop % | Cards |
|---|---|---|---|---|---|---|---|---|---|
| **Dragon**<br>`dragon` | 60 | Dragon Jaws | 9 | melee | fire | 150 | 12 | 100 | — |
| **Ghoul**<br>`ghoul` | 16 | Talons | 4 | melee | chaos | 35 | 8 | 35 | ⚠ −50% Holy Resistance<br> |
| **Kin Archer**<br>`kin-archer` | 12 | Hunter's Bow | 3 | 3 hexes | sharp | 30 | 8 | 30 | — |
| **Rat**<br>`rat` | 4 | Claws | 1 | melee | sharp | 8 | 7 | 10 | — |
| **Troll**<br>`troll` | 30 | Maul | 6 | melee | blunt | 60 | 8 | 50 | ⚠ −50% Fire Resistance<br> |
| **Wolf**<br>`wolf` | 10 | Fangs | 3 | melee | sharp | 20 | 10 | 30 | — |

## Before you send a proposal

1. Does any single check read *both* sides' numbers? If yes, it's a coupled
   roll — translate it.
2. Is mitigation a percentage rather than a flat `−N`?
3. Does a defensive proc *halve* rather than cancel?
4. Is it expressible as event + conditions + effect using only the tables
   above? A card needing a brand-new condition is fine — but a new condition
   wants **two** real users before it earns its place.
5. Are the numbers pitched against the calibration anchors, not against
   another game's?
6. Is the flavour text free of digits, with the mechanics left to the
   generated stat line?
