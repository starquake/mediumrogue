---
name: design-slice
description: >
  Use whenever the user wants to start designing a new mechanic or feature —
  "let's design X", "start a slice for #NN", "spec out shields", "let's work
  on #NN" when #NN is a new mechanic with no approved plan yet. Runs the
  design-first workflow: the spec and plan are written IN the GitHub issue
  (design-slice template), decisions are settled with the maintainer there,
  and building only starts after the maintainer's explicit OK. Trigger for
  any design-a-new-mechanic request even if the user doesn't say "skill".
---

You design a milestone slice **in its GitHub issue** — the issue is the
single design record (CLAUDE.md, "How work lands"). No spec/plan docs are
committed to the repo. The one rule that matters most: **never auto-proceed
from plan to implementation** — the maintainer's OK on the issue is the
build signal, and it must be explicit.

**The handoff label.** Every issue carries at most one `needs:*` label naming
the next action, so the maintainer can see at a glance what's waiting on them.
This skill flips it at each pause. Whenever you set one, remove whatever
`needs:*` label was there before (`gh issue edit <n> --add-label "<new>"
--remove-label "<old>"`) — exactly one at a time. The gate labels
(`needs: your input`, `needs: your sign-off`) are the maintainer's court; the
work labels (`needs: spec`, `needs: plan`) are yours. `ready to merge` is a
PR-level gate you never set — see `merge-pr`.

## Step 1 — The issue

- If the slice has no issue yet, create one from the design-slice template
  (`.github/ISSUE_TEMPLATE/design-slice.md`) with the 🤖 attribution header.
  If an issue exists but free-form, restructure its body into the template's
  sections (it's your own issue to edit; otherwise propose the edit).
- Read the codebase before writing: name real symbols (files, functions,
  consts). A spec that says `slotForType` beats one that says "the slot
  logic".
- **Label**: while you're writing the spec, the issue is in your court — set
  `needs: spec` (removing any other `needs:*`) so the maintainer knows it's
  yours, not waiting on them.

## Step 2 — The Spec (top half of the template)

- **Goal**: what ships + the one-line reason.
- **Decisions**: numbered, each with its *why*. Anything unsettled is a
  question TO the maintainer, asked in the issue or in chat — do not decide
  design direction yourself.
- **The TTRPG gate** (`docs/game-identity.md`): any combat-adjacent proposal
  in D&D idiom gets translated to the ARPG equivalent (5% miss → 5% glance;
  crit-on-die-face → crit%) or pushed back — always explaining why. The tell
  is coupling: attacker + defender stats folded into one roll.
- **The no-mechanic-wildfire gate**: never introduce a mechanic (a new
  pipeline event/condition/effect kind, a new stat, a new subsystem) for a
  single item. A mechanic must be reusable by multiple items/content pieces
  to earn its place — if only one weapon would ever use it, either express
  the item with the existing card vocabulary or push back on the design.
  (Precedent: shields added zero new kinds — two items rode the existing
  take-damage fold.)
- **Record both gate outcomes IN the issue** — a one-line audit note per
  gate ("TTRPG gate: clean — every card is a single-side fold"; "wildfire
  gate: the new condition kind serves N cards"), even when nothing was
  flagged. A silent check is indistinguishable from a skipped one.
- **Determinism & seeded tests**: state whether rng is consumed or
  reordered, and which pinned seeds/tables can move.
- **Mockup**: if the slice's value is how it looks, produce the mockup NOW —
  use the `mockup` skill — and embed the screenshot in the issue's Mockup
  section. Screenshot approval is part of the spec OK.
- **Label — hand off**: once the spec is posted and its open Decisions are
  questions FOR the maintainer, set `needs: your input` (removing
  `needs: spec`). This is the first pause: you cannot settle direction
  yourself, so the ball is theirs until they answer.

## Step 3 — Settle, then Plan (bottom half)

Only fill the Plan once the Decisions are settled. When you resume to write it,
the issue is back in your court — set `needs: plan` (removing
`needs: your input`). Tasks in landing order, each one green commit
(`set -o pipefail && make check 2>&1 | tail -15`); failing tests first where
practical; isolate the seeded-surface task (drop tables, rng changes) so pin
movement has exactly one cause; the last task is always docs (`FEATURES.md`,
`design-decisions.md` if direction was decided) in the same PR.

## Step 4 — STOP

Set `needs: your sign-off` (removing `needs: plan`) — the completed plan now
awaits the maintainer's explicit OK to build. Tell the maintainer the issue is
ready for design review and **end there**. The build belongs to the
`build-slice` skill, and it starts only when the maintainer says go.
