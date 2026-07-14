# Combat model — why mediumrogue is ARPG, not TTRPG

*Two short design notes that settled the combat-resolution model. They were
written during the #55 / #56 / #69 design discussion (July 2026) and are the
reasoning behind the "combat resolution is ARPG stat-checks, not TTRPG rolls"
decision recorded in issue #69 and `design-decisions.md`. Presentation
copies (PDF) were shared in that discussion; this is the canonical text.*

---

## Note 1 — Were we mixing TTRPG and ARPG?

**Short answer: yes — and it's worth being precise about *where*, because it
clears the path.**

**The engine is already ARPG.** The modifier pipeline (gear as pure-data rule
cards folding %/flat modifiers), "defence is a *rule*, not an Armor-Rating
stat," damage-reduction, deterministic resolution — that is Diablo/PoE
lineage. The first-gear review even *rejected* an "Armor Rating 9" stat
precisely because it is a TTRPG stat with nothing behind it.

**The combat-*resolution* discussion drifted TTRPG.** That entered with a
`d20` proposal — pure D&D — and got carried forward rather than flagged. The
bits that crept in, and their native-ARPG equivalents:

| TTRPG thing we picked up | ARPG equivalent |
|---|---|
| `d20 + accuracy vs Armor Rating` — one *coupled* roll | *Decoupled* stats: `evasion%` (defence) + `crit%` (offence) |
| "percent hit = 80% + attacker bonus − defender penalty" | …is *still* the coupled attack-roll, just in % clothing |
| Crit on a *die face* (nat-20, elf 19–20) | Crit *chance %* (elf = +crit%) |
| "Armor Rating / AC" = harder to *hit* | `evasion%` (avoid) *or* damage-reduction (mitigate) |
| "meets it beats it," clamp-as-nat-1 / nat-20 | just a % floor / ceiling |

**The tell is coupling.** TTRPG folds attacker accuracy *and* defender armour
into a **single** to-hit roll. ARPG keeps them as **separate** gear stats —
the weapon rolls its own `crit%`, the armour its own `evasion%`,
independently. Everything decided before the pivot (baseline hit chance,
"−%to-hit" defence, accuracy modifiers) was the *coupled* model wearing
percentages.

**So ARPG is the coherent choice — not just taste.** It matches the engine
already built. A `d20`/AC path would graft a TTRPG resolution layer onto an
ARPG stat system, and they would keep fighting — the *armor-rating-as-AC vs
armour-as-a-rule-card* seam is exactly where it grinds. Pulling it back to
`evasion%` / `crit%` gear stats removes the friction: offence lives on
weapons, defence on armour, each an independent percentage the engine already
knows how to fold.

---

## Note 2 — What if we moved to TTRPG?

**Two separable questions hide inside "move to TTRPG" — and they have
opposite answers.** "TTRPG" is really two things: the resolution *math*
(`d20 + bonus vs AC`, a coupled roll) and the turn *structure* (initiative,
sequential turns, an action economy, reactions). **The math is portable. The
structure is not.**

### Would the pipeline be replaced? — No.

The modifier pipeline is a *stat-stacking engine* — it sits *below* the
TTRPG/ARPG line. In D&D, "attack bonus" and "AC" are literally stacks of
modifiers (proficiency + ability + item) — exactly what the pipeline folds.
TTRPG would lean on it *more*, not less. What changes is only what sits on
top: a couple of new folded values and an attack-roll event that reads them.
Same shape as the ARPG extension (`evasion%` / `crit%`). Either way the
pipeline is **extended, never replaced.**

### Would it work with our time-based turns? — Split.

- **The math — yes.** A `d20`-vs-AC comparison resolves fine inside a
  4-second WeGo turn or a combat bubble; a roll is just a value comparison at
  resolution time. But adopting *only* the math is precisely the TTRPG/ARPG
  mix diagnosed in Note 1 — it buys nothing and reintroduces the coupling
  grind.
- **The structure — no.** Real TTRPG wants *initiative-based, sequential*
  turns, an action economy (action / bonus / move), reactions and opportunity
  attacks. Those assume "it's my turn, now yours" — fundamentally at odds with
  WeGo's *simultaneous* 4-second window where everyone acts at once.

**The killer is the co-op.** WeGo exists *because* of the ~15-player group —
everyone submits in the same window, nobody waits. TTRPG initiative makes 15
players + monsters wait through each other's turns each round — the exact
thing WeGo was built to avoid. The turn structure doesn't just clash with the
clock; it clashes with the *reason the clock works that way.*

### What a real move would change

| System | Under full TTRPG |
|---|---|
| Turn model | WeGo-simultaneous → *initiative-sequential* — the core pillar changes |
| Action economy | New: action / bonus-action / movement per turn (today it's one intent per turn) |
| Resolution | `d20 + attackBonus vs AC` — pipeline-fed, plus crit on a die face |
| Reactions | Opportunity attacks, readied actions, saves — all need sequential turns |
| Multiplayer | 15 players waiting per round — undermines the very premise WeGo serves |

There is a hybrid — keep WeGo on the overworld, and drop combat into a
TTRPG-style initiative mini-battle inside the combat bubble — but it is a
whole second combat mode, a jarring real-time→turn-based mode switch, and the
15-player wait reappears inside the bubble.

### Verdict

The pipeline is safe either way — the real incompatibility was never the
pipeline, it's **WeGo vs initiative**. ARPG stat-checks (`evasion%` /
`crit%`) fit WeGo *natively*: simultaneous, no turn order, one seeded draw at
resolution. TTRPG's genuine value — tactical initiative, reactions — needs
the sequential structure the game deliberately doesn't have. Cherry-pick its
math without its structure and you get the grind from Note 1; adopt its
structure and it's a different game.
