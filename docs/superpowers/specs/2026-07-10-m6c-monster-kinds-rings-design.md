# Milestone 6c — Monster Kinds & Difficulty Rings: Design

*The slice that unlocks the most decided direction at once: monster variety
as content data (like items), per-kind loot tables (the dragon-drops
model), spatial difficulty rings (scaling decision, plan §9), legible
danger (toolbox-progression guardrail), and the `targetKind` condition the
first designer card (Wyrmslayer Greatsword) is waiting on — which ships in
this slice as its proof. Assumes the playtest-ready batch (regen, aggro
range, spawn guards) is merged first; builds on its aggro machinery.*

## Goal

Replace the single anonymous monster with a **registry of monster kinds**
— content data in `internal/game/content.go`, exactly like items — placed
in **distance-based difficulty rings** by worldgen, each with its own
stats, XP award, aggro radius, and **own loot table**. Danger becomes
legible (distinct look + name per kind) and permanent (rings are soft-gated
by the player's toolbox, per the flat-curve philosophy).

Player-visible: the world near the origin has a few weak scavengers; the
frontier has things that kill you; a dragon exists, guards the far ring,
and drops the loot that makes fighting it worthwhile. The chat log says
"a wolf was slain", not "a monster".

## Monster kinds as content (`internal/game/content.go`)

```go
type monsterDef struct {
    id, name string  // "wolf", "Wolf" — name is the wire/announce display
    glyph    string  // one letter for the client dot ("w"); color client-side by id
    maxHP    int
    damage   int     // claws profile — replaces the flat MonsterAttackDamage
    xp       int     // per-kill award — replaces the flat protocol.MonsterXP
    aggroRadius int  // overrides protocol.MonsterAggroRadius (0 = use default)
    dropChance  int  // percent, replaces the global DropChancePercent (0 = never)
    drops    []drop  // OWN weighted table: []{itemDefID, weight} — the
                     // monster-side loot model decided in the dragon discussion
    rings    []int   // which difficulty rings this kind spawns in (see below)
    rules    []ruleCard // future: kind passives (armored, regenerating); empty at launch
}
```

Registry + `monsterDefByID` + validation at init (unique ids, drops
reference registered items, rings valid, every ring has ≥1 kind), same
`mustValidateContent` idiom as items. `validateMaxReach`-style rule: every
kind's aggroRadius ≥ CombatRadius or 0.

**Launch set** (numbers are first-draft knobs; damage/HP tuned against
current player stats — fighter 30 HP, sword 4):

| Kind | Ring | HP | Dmg | XP | Aggro | Drops (chance) |
|---|---|---|---|---|---|---|
| `rat` | 0–1 | 4 | 1 | 8 | 6 | 10%: bandage-fodder tier (none yet → butchers-cleaver w1) |
| `wolf` | 1 | 10 | 3 | 20 | 10 | 30%: current starter drop set |
| `ghoul` | 1–2 | 16 | 4 | 35 | 8 | 35%: starter set + venom-fang weighted up |
| `troll` | 2 | 30 | 6 | 60 | 8 | 50%: warhammer/pack-bow/war-mage-staff weighted up |
| `dragon` | 2 (rare) | 60 | 9 | 150 | 12 | 100%: wyrmslayer-greatsword w2 + the rare pool |

`wolf` inherits today's monster numbers (10 HP, 3 dmg, 20 XP, 30% drop) so
existing balance and most seeded tests carry over with a rename.

**Wyrmslayer Greatsword ships in this slice** (the first designer card's
full intent, previously blocked on kinds existing): fighter close, dmg 4
(base adjusted per the review — not 5), `deal-damage` ×1.5 when the target
is a dragon — via the new **`targetKind` condition** (`s` = monster def
id). Dragon-only drop.

## Difficulty rings (worldgen + spawn placement)

- Ring = a band of hex distance from the origin: with `WORLD_RADIUS` 24,
  ring 0 = 0–7 (home), ring 1 = 8–15, ring 2 = 16–24 (frontier). Boundaries
  = fractions of the radius (works at test sizes; tiny maps collapse to
  ring 0 and spawn the ring-0 kinds only).
- **Ring 0 spawns nothing hostile inside distance 5 of the origin** — the
  sanctuary seed (future trade hub, plan §9 recovery entry).
- `SpawnMonsters(n)` distributes across rings (weighted by ring area),
  picking uniformly among the kinds allowed in each ring; `dragon` is
  gated to at most `DragonCount` (default 1) per world. All placement
  respects the playtest-batch spawn guards.
- Legibility at the border: rings are invisible lines, but the *kinds* are
  the signal — you see a troll's glyph/color from `aggroRadius + margin`
  away before it notices you (aggro ≤ CombatRadius would break this;
  validation enforces aggro ≥ CombatRadius so bubbles still form on
  approach). No UI ring indicator this slice; revisit if playtests show
  people blundering.

## Wire & client

- `Entity` gains `MonsterKind string` (def id; empty for players) and
  monsters now send `Name` = the kind's display name (players keep their
  chosen names — no field collision, monsters' Name was empty until now).
- Client: per-kind dot color (map keyed by MonsterKind, fallback to the
  current red) + the def's glyph letter rendered like class glyphs; HP bar
  unchanged. `window.game.positions` entries gain `monsterKind`.
- Announce texts use the kind name: "a wolf was slain (+20 XP …)",
  "2 ghouls were slain (+70 XP …)" — mixed-kind turns sum XP and list
  kinds ("a wolf and a troll were slain (+80 XP …)").
- `make protocol` + contract test extension.

## Combat integration

- `monsterClawsDef` damage → per-kind `damage` (claws profile per kind).
- Kill XP: `killed * MonsterXP` → sum of the slain kinds' `xp` values
  (collect slain defs in `resolveDeathsLocked`, return `[]*monsterDef`
  instead of a count; the human/earn-XP pipeline fold is unchanged).
- Drops: `dropLootLocked` rolls the SLAIN KIND's `dropChance` and picks
  from ITS table (global `dropTable`/`DropChancePercent` retire; items'
  `dropWeight` field becomes per-kind table weights — item defs lose
  `dropWeight`, tables live monster-side, per the decided loot model).
- New pipeline condition **`targetKind`** (victim is a monster of def id
  `s`); validated against the monster registry. `attackerKind` NOT added
  (nothing needs it; monsters don't equip).
- Kind `rules` hooks exist but ship empty (validation accepts them) — the
  seam for armored/regenerating kinds later, zero cost now.

## Out of scope (recorded)

Continuous spawning + density-tracks-players (needs this slice + spawn
guards; next), monster kind passives (seam ships empty), ring UI
indicators, terrain-blocked LOS, boss mechanics beyond stats, per-kind
movement speeds, the sanctuary hub itself (only its monster-free zone).

## Tests

Unit: registry validation (incl. every-ring-covered, aggro ≥ CombatRadius,
drops reference real items); ring math at real + tiny radii; per-kind XP
sums and loot rolls (seeded, per-kind tables); targetKind condition +
Wyrmslayer card pin; sanctuary zone monster-free. Integration: kill a
specific kind over HTTP → its XP and its announce text. e2e: two kinds
render with distinct colors/glyphs (`window.game.positions[].monsterKind`);
existing combat specs updated where they assume the old flat XP/monster.
Every existing seeded test that kills "the monster" gets its expectations
re-derived against `wolf` (same numbers — most should hold).

## Risks

- Widest test-expectation ripple since 6b.4 (MonsterXP is baked into many
  assertions) — mitigated by wolf inheriting the exact current numbers.
- Loot-table migration moves authority from items to monsters — the item
  `dropWeight` deletion must sweep tests and the content guide's §4 note.
- Ring placement vs small test maps: every ring-dependent test must use
  the real radius or pin ring-0 behavior explicitly.
