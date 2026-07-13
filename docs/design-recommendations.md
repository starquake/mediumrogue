# Design recommendations — after the fully-ARPG combat decision

*Written 2026-07-12 as an overnight synthesis across every open issue
(#31, #36, #55–#62, #69), the design docs, the roadmap, and the built code.
It is a **morning-review document**: recommendations and an engineering read,
not decisions. Gameplay calls stay with the maintainer/designer. Anything I
was unsure about is flagged **⚠ FLAG** rather than acted on.*

Companion to the doc-sync changes in this same PR (see
[§5](#5-what-this-pr-changes)). The decision itself is captured on **#69** and
in [`combat-model-notes.md`](combat-model-notes.md).

---

## 1. What the ARPG decision settles

Combat resolution is **ARPG stat-checks, not TTRPG rolls**. Offence and
defence are **decoupled percentage gear stats**, each a rule card the existing
modifier pipeline folds:

- **`crit%`** (offence, on weapons) — a chance to deal ×2 damage.
- **`evasion%`** (defence, on light armour) — a chance to avoid a hit entirely
  (0 damage). Heavy armour keeps flat damage-reduction (the light-vs-heavy
  split).

No coupled to-hit roll, no accuracy-vs-Armor-Rating, no `d20`. This retires
the once-planned `attack-roll` event. Determinism holds — the chances draw
from the per-scope seeded PCG.

## 2. The #69 build reality (the important finding)

Reading the engine (`internal/game/rules.go`, `world.go`) surfaced a split
that changes how #69 should be sliced:

- **`crit%` is essentially free — Tier 1 (pure content).** It is exactly how
  **elf crit** already ships: a `deal-damage` card with a `chance` condition
  and an `effMulPct 200`. A per-weapon `crit%` is just that pattern authored
  on a weapon (or a new species/skill). **No engine work is required** to give
  weapons a crit chance today. A dedicated `crit-check` event would only be a
  cleanliness convenience, not a prerequisite.

- **`evasion%` is the real work — Tier 2 (one engine addition).** A fully
  evaded hit deals **0**, and today's pipeline *cannot express that*: the
  `take-damage` fold floors every landed hit at 1, and `effMulPct 0` still
  clamps to 1. So evasion needs a **new pre-damage `evasion-check` event**
  (defender-side, before `deal-damage`/`take-damage`), wired through the
  "three places that must agree" (`rules.go` const block + `conditionHolds`/
  `applyRules`; `items.go` `validateRuleCards`/`validateRuleCondition`).

**Recommendation:** split #69's roadmap rows accordingly —
- **`crit%` (DF-crit)** can ship *now*, as a fast-lane content win, the moment
  weapons want it. Cheapest possible.
- **`evasion%` (DF2)** is the genuine engine slice — one new event, then
  reusable forever (smoke bombs, blur spells, "hard to hit" species).
- **DF1** (light-vs-heavy identity) then becomes: heavy = today's
  `take-damage −N` (done), light = an `evasion%` card once DF2 lands.

⚠ **FLAG for the designer:** pick the **evasion cap** (a ceiling so no target
is un-hittable — @NGB1024 asked for this) and the **crit multiplier** (×2 is
assumed for v1). Both are pure tuning knobs; naming them unblocks the slice.

## 3. Recommended build order

My engineering read of the dependency graph (the *what-first*, not the
*whether* — Decision columns in `design-roadmap.md` are yours to set):

| Order | Work | Roadmap rows | Why here |
|:--:|---|---|---|
| 1 | **Fast-lane wins** — quadratic XP curve, front-loaded HP, cut `DamagePerLevel`, stacking throwables, **`crit%` weapons** | XP1, XP2, XP3, G4, (DF-crit) | Independent, cheap, satisfying; `crit%` is free content (§2). Momentum with zero architectural commitment. |
| 2 | **Gear foundation** — weapon type-tags, generic hand slots, drop class gates | G1, G2, G3 (#55/#56) | **The keystone.** Unblocks property-skills, shields, and gives `crit%`/`evasion%` a natural home. |
| 3 | **Evasion** — the `evasion-check` event + light-armour cards | DF2, DF1 (#69) | The one combat-engine addition; adds the light-armour playstyle. Best after gear has stat homes. |
| 4 | **Damage types** | DT1 | Unblocks fire gear/skills and the parked Infernal Chain Mail / War Mage Robes cards. |
| 5 | **Combat action economy** | ACT (#69 cluster) | Highest-leverage unlock — one system gives active skills (SK5), combat-heal (#61), block/guard, protect-ally. |
| 6 | **Skill system** | SK1–SK7 (#57/#61) | The big arc; needs gear (G1), damage types (DT1), and ACT under it. |
| 7 | **Subclasses, skill UI** | SU1–2 (#58), UI1–2 (#62) | Last — they sit on top of the settled skill model. |

**If you want the single highest-leverage engine investment:** it's **ACT
(combat action economy)** — block, combat-heal, protect-ally, and active
skills are all one system deep. But it is an `L`, so it belongs after the
cheap wins and the gear keystone.

## 4. Issue hygiene (GitHub-side — not in this PR) — ✅ all done

These were issue-body edits, so they couldn't ride a PR. All executed with
the maintainer's OK (2026-07-13):

- **#55** — the stale *"two future to-hit rolls"* phrase fixed in **both** the
  body and the decisions comment → *"two independent evasion/crit
  resolutions"*.
- **#57** — cosmetic only; verified clean, no action needed.
- **#31, #56, #62** — verified free of TTRPG-drift language; no changes.
- **#69** — rewritten to the ARPG model (title, body, decision comments),
  including the Q6 AoE-always-hits update.
- **After the Q2–Q11 decisions** (which post-date this doc's first draft),
  the affected descriptions were synced to the decided state: **#58** body
  rewritten (subclass model), **#60/#61** given appended "Decided so far"
  blocks (NGB's original text untouched), **#36**'s spawn checklist bullet
  updated to the Q9 decision. Every decision also has an AI-attributed
  comment on its ticket.

## 5. What this PR changes

Doc-sync so the repo stops teaching the retired coupled-roll model:

- **`docs/rule-based-content-design.md`** — the primary offender. Replaced the
  `attack-roll` event + "miss check" framing with the decoupled `crit-check` /
  `evasion-check` model; reframed the Elf and Halfling species examples
  (elf = `deal-damage` chance, halfling = `evasion%`); "dice roll" → seeded
  PCG; fixed the stale "5-second turns" → 4-second; updated the card template's
  event list.
- **`docs/STATUS.md`** — noted `attack-roll` was dropped (not "pending") in the
  6b.4 records; corrected the already-removed `protocol.PlayerAttackDamage`
  cleanup note.
- **`docs/FEATURES.md`** — header date/scope 2026-07-11/M10a →
  2026-07-12/inventory + deployment.
- **`docs/design-roadmap.md`** — combat section already carried the ARPG
  correction (this branch); fixed its pointer to reference the new in-repo
  notes.
- **`docs/combat-model-notes.md`** (new) — the two combat-model design notes
  (ARPG-vs-TTRPG / what-if-TTRPG), previously only on issue #69, now canonical
  in-repo so the roadmap pointer resolves.
- **`docs/design-recommendations.md`** (new) — this file.

**Added by the contradiction sweep (second pass):** README 5s→4s turn fix;
CLAUDE.md heartbeat-comments→named-events; content-design example numbers
re-pinned to shipped values (dwarf −1, Cleaver 3 vs sword 4, Ember Staff 3);
plan fixes (patience 30s, dwarf flat not %, §0 combat-math bullet updated to
decided state, §8 quest-slot bullet marked amended, same-origin check marked
*open* — it was claimed but never built, FEATURES now agrees); four stale
STATUS placeholders marked since-shipped (gear/inventory, spawn guard, SPACE
wait, bubble waiting-state); game-identity disambiguation (WeGo "combat
action economy" ≠ D&D multi-action turns; friend-trade/sanctuary hub in
scope, market-economy machinery the actual guardrail); roadmap **Q8–Q11**
capture the four unresolved conflicts (%-stacking add-vs-compound, spawn
fork on #36, plan-vs-roadmap skill-model governance, capstone-gate vs
principle 5).

No code, protocol, or content changes. No behaviour change.

## 6. Other staleness noticed (⚠ FLAG — not fixed here)

Surfaced during the sweep; left for your call rather than changed silently:

- **`roguelike-mp-plan.md` §5 asserts LOS-triggered bubbles** ("Trigger on
  awareness (mutual LOS), not raw distance"), but the implementation is
  **distance-only** (terrain-blocked LOS was deferred; STATUS/FEATURES both
  acknowledge it). The master plan's normative wording still reads as if LOS
  is live. Either build LOS or soften the plan's wording — a design call, so I
  left it.
- **`rule-based-content-design.md` §2** also said "5-second turns" — **fixed**
  here (it's a living reference doc). The identical phrase in the historical
  `m6.4` spec is a point-in-time record and was **left** as-is.
- **Historical specs/plans** (`m6b.4-gear-pipeline` design + plan) contain
  `attack-roll`/"to-hit" framing. They are dated milestone records, so I left
  them rather than rewriting history — flag if you'd prefer a note appended.

---

*This is a recommendation aid. The gameplay calls are the maintainer's and
designer's.*
