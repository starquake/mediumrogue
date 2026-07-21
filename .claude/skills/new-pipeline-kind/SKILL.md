---
name: new-pipeline-kind
description: >
  Use when adding a NEW event / condition / effect KIND to the combat modifier
  pipeline — a new fold point (`evXxx`), a new card condition (`condXxx`), a new
  effect verb (`effXxx`). "add a crit-check event", "add a `weatherIs`
  condition", "make cards able to heal on a fold", "the pipeline needs an
  on-kill hook". This is the ENGINE-CAPABILITY step: you are teaching the
  vocabulary, not spending it. NOT when writing a card with existing vocabulary
  (that is `add-content`); a WHOLE new mechanic may be a `design-slice` that
  contains this as one task. Trigger even if the user doesn't say "skill".
---

You are extending the pipeline's **vocabulary** — the set of moments a card can
hook (events), the questions a card can ask (conditions), and the verbs a card
can apply (effects). A kind is **pure data**: a `condition` is `{kind string; n
int; s string}` and an `effect` is `{kind string; n int}` (`internal/game/rules.go`).
Never a Go closure — the pipeline must serialize to SQLite (§7), so a kind is a
string tag the switches interpret, never a function. If your idea can't be
expressed as a string tag plus ints read by a switch, it isn't a pipeline kind.

## The gate, before you add a kind at all

**A new kind needs at least TWO real consumers.** One rider means write the card
with existing vocabulary, or the kind waits until a second use exists. This is
the `add-content` "no mechanic wildfire" rule applied to the engine: every kind
you add is four places of forever-maintenance and a wider validator surface, so
it earns its keep only when the vocabulary genuinely can't say the thing. (`condDamageType`
cleared this by construction — every resist and vulnerability card uses it.)

If the kind is one piece of a brand-new **mechanic** with unexamined design
assumptions, it belongs inside a `design-slice`, not a bare add. Adding a card
that USES an existing kind is `add-content`. This skill is only the middle case:
the vocabulary is missing and the mechanic around it is already decided.

## The places that must agree (the core of this skill)

The pipeline splits **runtime evaluation** (`rules.go`) from **load-time
validation** (`items.go`), and they will silently diverge unless you update
both — plus the const that names the kind and the generated designer guide. The
cross-reference comment on `rules.go`'s `evDealDamage` const block marks the
first three; #156 added the guide as the fourth (see `guide.go`).

**Why this is load-bearing, not bureaucracy** — the two switches fail
*differently* on an unknown kind:

- `conditionHolds` (`rules.go`) has a `default: return false` — an unvalidated
  condition **fails closed** (never holds, silently).
- `applyRules`' effect switch (`rules.go`, `switch c.then.kind`) has **no
  default at all** — an unvalidated effect is **silently skipped** and the card
  no-ops forever.

So the classic bug is exactly: add the const + the fold/eval site, forget
`validateRuleCards`, ship a card using the new kind — it passes init, passes
tests that don't happen to exercise it, and does **nothing** in play instead of
panicking at process start. `validateRuleCards` is the thing that turns "silent
no-op forever" into "panic at load." Skipping it is the whole failure mode.

### Adding a CONDITION (`condXxx`)

1. **Const** — add `condXxx = "…"` to the condition const block in `rules.go`,
   with a doc comment stating which side it reads (attacker vs victim — get this
   wrong and it's silently backwards, see `condShieldEquipped`'s comment) and
   its parameter (`n` numeric, `s` string, or none).
2. **Runtime** — add a `case condXxx:` to `conditionHolds` in `rules.go` (or its
   split-out helper, e.g. `targetHPConditionHolds` / `equipmentConditionHolds`,
   kept small for the complexity linter). This is what actually evaluates it.
3. **Load-time** — add a `case condXxx:` to `validateRuleCondition` in
   `items.go`. If the kind carries an `s` parameter, **validate it against its
   registry here** (`condAttackerSpecies` → `validSpecies`, `condDamageType` →
   `validDamageType`, `condTargetKind` → `monsterDefByID`) so a typo'd parameter
   — which would silently never hold — panics at load, not never-fires in play.
4. **Guide** — add a `condXxx: {…}` line to `guideDescriptions` and list it in
   `guideConditions` in `guide.go`; if it takes an `s`, add a real sample value
   to `sampleConditionFor` so `validateGuideVocabulary` exercises the same
   lookup content does.

### Adding an EFFECT (`effXxx`)

Const block + `applyRules`' `switch c.then.kind` (`rules.go`) + `validateRuleCards`'
effect switch (`items.go`) + `guideEffects`/`guideDescriptions` (`guide.go`).
Remember the fold phases: **all `add`s sum first, then all `mulPct` deltas sum
and apply once** (additive percent, one truncation — `applyRulesTraced`). A new
effect must state where in that order it folds and whether it participates in the
event clamp.

### Adding an EVENT (`evXxx`)

An event has no `conditionHolds` entry — instead:

1. **Const** — add `evXxx` to the event const block in `rules.go`.
2. **Fold site** — an event that nothing calls `applyRules(evXxx, …)` for is
   dead. Add the real fold in `world.go` (mirror how `rollDamageLocked` folds
   `evDealDamage`/`evTakeDamage`, `aggroRadiusForLocked` folds `evAggroRange`).
   Build the `ruleCtx` the event's conditions will read, and thread **rng only
   if a `chance` card is legal here** (see Determinism).
3. **Clamp** — if the folded value has a floor/ceiling, add a `case evXxx:` to
   `applyRulesTraced`'s event switch (take-damage floors at 1, earn-xp at 0).
4. **Load-time** — add `evXxx` to `validateRuleCards`' event switch (`items.go`).
5. **Guide** — `guideEvents` + `guideDescriptions` (`guide.go`).

## Fail-loud at init is the test bar

Content is validated by `mustValidateContent()` at package `init()`
(`content.go`) — a bad kind must **panic at process start, never mid-combat**.
Your kind isn't done until an *invalid* use of it panics at load. The proof is a
white-box panic-recover test in `package game` (the validators are unexported):

- **Rejects a bad use** — feed `validateRuleCards` / `validateRuleCondition` a
  card that names your kind wrong (unknown parameter, chance-on-earn-xp, …),
  assert it panics, and assert the panic **message mentions the kind or the bad
  value** (pattern: `TestValidateSkillDefsPanicsOn…` in `actives_test.go`,
  `recover()` + `strings.Contains` on the message).
- **Accepts a good use** — feed the validator a valid card and assert it does
  NOT panic (pattern: `TestValidateRuleCardsAcceptsDualWielding` in
  `dualwield_test.go`).
- **Evaluates correctly** — a direct test of the runtime side (`dualWieldingHolds`
  in `dualwield_test.go` calls the helper straight), then a **live-pipeline**
  test proving a real card of this kind reaches the real combat site
  (`meleeDamageTaken` / `damageDealtToKind` patterns from `add-content`). The
  fold is already tested; what you're proving is the card gets there.

The guide's fourth place is checked automatically: `TestGuideDocumentsEveryVocabularyKindInUse`
(`guide_test.go`) fails if any registered card uses a kind absent from
`guideDescriptions`, and `vocabFor` panics at init if a term is listed but
undescribed — so the guide can't quietly fall behind, but you must author the
description in the same change.

## Determinism (only if the condition consumes rng)

`condChance` reads `ctx.rng.IntN` — a chance condition **consumes the turn rng**,
so introducing one (or an event fold that threads rng) reorders the stream and
shifts every seeded expectation downstream. **Re-derive the pins, never weaken
the assertion**, and document the re-derivation in a comment. A kind with no
`chance` moves no rng — if a seeded test moves anyway, that's a bug to
investigate. When your fold draws from a map-derived slice, **sort it before
drawing**. Note the live guard: `earn-xp` folds run with a bare `ruleCtx{}` (no
rng), so `validateRuleCondition` **panics at load on a chance condition on an
earn-xp card** — if your new event folds without rng, add the same guard;
otherwise `conditionHolds` nil-derefs `ctx.rng` the first time it rolls, mid-combat.

## Paperwork (same PR)

- **`docs/FEATURES.md`** — the pipeline-vocabulary section, if the kind is
  player- or designer-facing. Values from `internal/protocol` / `content.go`,
  never memory.
- **`docs/content-authoring.md`** — the designer guide's hand-written half, if
  the new vocabulary changes what a designer can write (the derivable half
  regenerates from `guideDescriptions`).
- **`got, want` tests** throughout (`.claude/rules/go-style.md`).
- **Stale-claim sweep** — `grep -rn "<kind>" docs/ internal/` for comments that
  say the thing you just added doesn't exist yet; the `rules.go` const-block
  cross-reference comment has been missed twice.
- Full `set -o pipefail && make check` per commit (check the **exit code**, not
  grepped output); `make e2e` only if the client changed.

## The boundary, restated

- **New vocabulary** (a kind the switches don't know) → this skill.
- **A card using existing vocabulary** → `add-content`.
- **A new mechanic with unexamined design assumptions** → `design-slice`, which
  may contain this skill as one build task.
