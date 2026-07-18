---
name: add-content
description: >
  Use whenever a weapon, armor, shield, consumable, monster kind, or rule card
  is being ADDED or retuned — "add a fire sword", "make trolls fear fire",
  "the designer sent new cards", "give the ghoul a drop", "buff the dagger".
  Encodes where content lives (registry tables, never combat sites), the rule
  card vocabulary and its three-places-must-agree rule, the ARPG gate, the
  drop-table and seeded-pin protocol, and the paperwork (tests + FEATURES.md
  in the same PR). Trigger on any add-content request even if the user doesn't
  say "skill".
---

You add game content as **data in a registry**, never as code at a combat
site. If a change needs a new engine capability, that is a design slice
(`design-slice`), not this skill — stop and say so.

## The gates, before anything is written

1. **ARPG, not TTRPG** (`docs/game-identity.md`). The tell is **coupling**:
   any mechanism folding attacker + defender stats into one roll is TTRPG
   even when it wears percentages. Translate it (`5% miss` → `5% glance`;
   crit-on-a-die-face → `crit%`; save-vs-level AoE → AoE-always-hits) or push
   back — in both cases explaining *why*. A collaborator who designs in D&D
   vocabulary is not wrong about the *intent*; only the mechanism.
2. **No mechanic wildfire**: a NEW event/condition/effect kind needs at least
   **two** real consumers. One rider means the card is written with existing
   vocabulary or the kind waits. (#92's `damageType` cleared this by
   construction — every resist and vulnerability card ever written uses it.)
3. **A new kind is a design slice, not this skill.** Adding a card that uses
   existing vocabulary is content. Adding vocabulary is engine work.

## Where content lives

| What | Where |
|---|---|
| item def | `internal/game/content.go` (`itemDefs`) |
| item id const | `internal/game/items.go` — a typo becomes a compile error |
| monster kind | `internal/game/content.go` (`monsterDefs`); claws are built from the def by `buildMonsterIndex` |
| drop rows | the kind's own `drops` table (monster-side since 6c — items carry no weight) |
| shared numbers | `internal/protocol` **only** if both sides compile against them |

Rule cards are **pure data** — a struct of string kinds and ints, never a Go
closure (the SQLite-serialization prerequisite). Registries are validated at
package init (`mustValidateContent`), so a content bug **panics at process
start**, never mid-combat. Adding content is a table entry; if you find
yourself editing a combat site, stop.

## Required fields (each enforced by a load-time panic)

- Every **weapon**: `tags` (which attacks fire it) **and** `damageType` (one
  of the six, #92). Non-weapons must carry neither.
- Every **monster kind**: `damageType` — its claws are a weapon like any
  other. Also `rings`, and an `aggroRadius` that is 0 or `> CombatRadius`.
- Combat stats (`damage`/`rangeHex`/`aoeRadius`) are weapon-only; `heal` is
  consumable-only.

## Drop tables — the seeded surface

- **Append new rows LAST**, always, with a comment saying so and why. Every
  earlier entry then keeps its cumulative-weight position, and the pinned
  `killDropSeed`/`killMissSeed` (`drops_test.go`) usually survive untouched.
- **When a pin does move, re-derive it — never weaken the assertion.** The
  usual mover is `TestWolfCarriesTodaysExactNumbers`, which hand-lists the
  wolf table: extend it with the new last row.
- `TestPickDropCoversWolfsWholeTable` reads the **live** table, so a new wolf
  row is automatically covered — no edit needed.
- Rebasing two content branches that both appended? Resolve **base-branch
  rows first, then yours** — that *is* the appended-LAST protocol, and it
  keeps both sets of pins valid (done for #88 ↔ #92 on 2026-07-18).

## Determinism

- All randomness goes through the per-scope seeded PCG; **sort any
  map-derived slice before drawing** from rng.
- A `chance` condition **consumes rng**, so adding one reorders the stream and
  shifts seeded expectations downstream — re-derive them.
- A card with no `chance` moves no rng at all: if a seeded test moves anyway,
  that is a bug to investigate, not a pin to re-derive.
- Migrating/renaming content keeps its numbers **byte-identical** so pinned
  seeds survive.

## Testing content

Prove the card through the **live pipeline**, not a white-box fold, wherever
a live path exists — the fold is already tested; what you are proving is that
the card reaches the real combat site.

- Damage taken: the `meleeDamageTaken` pattern (`starter_content_test.go`).
- Damage dealt: the `damageDealtToKind` pattern (`damage_types_test.go`).
- Noticeability: `aggroRadiusForLocked` + a live wolf-notices-or-doesn't pair
  (`monster_test.go`).
- **Add the negative test.** A vulnerability test passes just as happily
  against a card that forgot its condition — so also assert the card is
  *inert* where it should be (a sharp resist does nothing against fire).
- `got, want` style throughout (`.claude/rules/go-style.md`).

## Traps that have actually bitten

- **A scripted edit that silently doesn't match** leaves the content missing
  while everything still compiles and passes. After any scripted edit, `grep`
  the file for what you added before running tests (#92: two vulnerability
  cards never landed; the tests caught it, but only by luck of being written
  first).
- **The fighter's starting kit still swings.** A one-handed weapon granted in
  a test goes to the *off*-hand and both hands attack — a damage assertion
  then measures the sum. Two-handers replace both. Compare two victims with
  the same setup rather than pinning an absolute number.
- **`make check` compares the regenerated `protocol.gen.ts` against the
  index**, so after `make protocol` you must `git add` it or the gate reports
  "stale" on a file you just regenerated.
- **Percent folds ADD within one event** (`+50%` and `+50%` is `+100%`, not
  `×2.25`), then one truncation. Stages compose across events.
- Content-load panics fire at **package init**, so a bad card breaks every
  test in the package at once — read the panic, not the test list.

## The paperwork (same PR, always)

- `docs/FEATURES.md`: the item/kind tables, drop sources, and any vocabulary
  change. **Values come from `internal/protocol` / `content.go`, never from
  memory.**
- `docs/design-decisions.md`: only if the change decides a *direction*
  (why blunt has no resist; why noticeability is gear-only).
- `docs/content-authoring.md`: if the designer-facing vocabulary grew.
- **Stale-claim sweep**: `grep -rn "<topic>" docs/ internal/` for comments
  that say the thing you just added doesn't exist yet. Doc comments in Go
  files count — two separate sweeps have missed `rules.go`'s own const-block
  comment.
- Full `make check` per commit; `make e2e` if the client changed.
