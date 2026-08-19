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

**The handoff state.** Every issue carries exactly one board state naming
the next action, so the maintainer can see at a glance what's waiting on them.
This skill moves it at each pause: `.claude/scripts/board.sh state <n> "<new>"`
replaces whatever was there, so there is nothing to clear. The gate states
(`Your input`, `Your sign-off`) are the maintainer's court; the
work states (`Spec`, `Plan`) are yours. `ready to merge` is a
PR-level gate you never set — see `merge-pr`.

## Step 1 — The issue

- If the slice has no issue yet, create one **from the template's sections** —
  `.github/ISSUE_TEMPLATE/design-slice.md` — plus the 🤖 attribution header.

  **Read that file and fill it in; do not write a design issue from memory.**
  `gh issue create --body/--body-file` **bypasses templates entirely** (they
  are a web-UI and interactive-CLI affordance; `--template` is starting text
  for an interactive editor and does not combine with `--body`). So nothing
  fails when the structure goes missing — which is how #441 happened: five
  design issues filed as hand-rolled prose, none containing the `Decisions`
  section, none carrying the template's own instruction to delete answered
  questions.

  The practical shape: read the template, fill its sections, write the result
  to a file, and pass `--body-file`. If an issue exists but free-form,
  restructure its body into those sections (it's your own issue to edit;
  otherwise propose the edit).
- **The BODY is the living spec; the COMMENTS are the history.** Opposite
  editing rules, and confusing them is a real failure mode (#441):
  - The **body** is always current — rewrite it freely, and when a question is
    answered MOVE it into `Decisions` and DELETE it from `Open questions`. A
    body that still asks a settled question is the bug.
  - The **comments** are append-only — never edit one in place, post a new
    `> 🤖 **Next steps**` each time the state changes.
  - An answer that arrives in chat is **written into the body**, not merely
    acknowledged in a comment. "Recorded on the ticket" means the reader of the
    ticket sees it as decided, not as still-open with a reply buried below.
- Read the codebase before writing: name real symbols (files, functions,
  consts). A spec that says `slotForType` beats one that says "the slot
  logic".
- **Label**: while you're writing the spec, the issue is in your court — set
  `Spec` (removing whatever it was) so the maintainer knows it's
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
- **Open questions get a copy-pastable answer block.** Any question left FOR
  the maintainer goes in the issue's *Open questions* section, and that
  section ends with a fenced block they can copy, fill in, and paste back as
  a comment — one line per question, the options spelled out inline, and
  **every line carrying a marked recommendation, `(rec)`** (#396):

  ~~~
  ```
  Q1 partymates: exempt from culling (rec) / separate roster field / accept shrinking
  Q2 fog visual: none for now (rec) / want a mockup
  ```
  ~~~

  The marker belongs **in the block**, not in a paragraph under it. A bare
  menu makes the maintainer reconstruct reasoning you already did, which is
  the slowest kind of question to answer — #365 put its leanings in prose
  below the block, covered one line of eight, and drew *"why are there no
  recommendations in #365 code block?"*. Where you genuinely have no view,
  mark that on the line (`discuss (rec)`) rather than leaving it blank: "I
  have no recommendation" is real information, and it is different from having
  forgotten to give one.

  **Two routes to an answer** (#399). The issue is always the RECORD; the
  INTERFACE is the maintainer's choice, and you **offer both** whenever a
  ticket carries more than two or three open questions — a long block is what
  made #365 read as *"I'm kinda lost"*.

  - **On GitHub** — the block is posted, they fill it in as a comment.
    Nothing further is required of you.
  - **In chat** — you ask the questions conversationally, **3–4 at a time**,
    each with its recommendation and what it costs. Then you **write the
    answers back to the issue**, and that step is not optional.

  The write-back is a NEW comment, posted **before you act on the answers** so
  the record survives a session that ends unexpectedly, carrying: a
  **decisions table** (one row per question); the **date**, and that it was
  answered in conversation, so a reader knows why it is not a filled-in block;
  a **verbatim quote** of anything whose wording carries reasoning the table
  flattens; and what became **moot**, so unanswered lines do not read as
  forgotten. #365's is the worked example — and it shows the payoff, since
  answering conversationally surfaced that the whole ticket was obsolete,
  which two board passes of the block had not.

  Ways this rule gets broken (#391, 2026-08-06; #399, 2026-08-09):

  - **Answers that never reach the ticket.** Asking and answering in chat is
    fine — that is the route above. What breaks the rule is leaving them
    there: an answer only in a transcript cannot be read later, by anyone
    else, or after the session ends (#385 was grilled entirely in chat and had
    to be reconstructed onto the ticket afterwards).
  - **A question in the prose that is missing from the block.** The block is
    the thing that gets answered, so anything not in it is effectively not
    asked. Re-read the prose against the block before posting and confirm
    every question has a line (#355 called its cooldown number "worth an
    opinion too" and then left it out of the block, so it stayed unanswered
    through two passes).

  When either happens, fix it by posting a NEW comment carrying the complete
  block — never by editing the old one, which breaks the thread's history.

  Prose questions get prose answers, which are slower to write and easy to
  answer only partly; a fillable block gets every question answered in one
  pass. Mirror the same block in the `> 🤖 **Next steps**` comment, so the
  maintainer can answer from the thread without opening the body. Recommending
  is not deciding — a `(rec)` makes "yes, do that" a valid reply, and gives
  them something to push against rather than into space.
- **Determinism & seeded tests**: state whether rng is consumed or
  reordered, and which pinned seeds/tables can move.
- **Mockup**: if the slice's value is how it looks, produce the mockup NOW —
  use the `mockup` skill — and embed the screenshot in the issue's Mockup
  section. Screenshot approval is part of the spec OK.
- **Label — hand off**: once the spec is posted and its open Decisions are
  questions FOR the maintainer, set `Your input` (removing
  `Spec`). This is the first pause: you cannot settle direction
  yourself, so the ball is theirs until they answer.

## Step 3 — Settle, then Plan (bottom half)

Only fill the Plan once the Decisions are settled. When you resume to write it,
the issue is back in your court — set `Plan` (removing
`Your input`). Tasks in landing order, each one green commit
(`set -o pipefail && make check 2>&1 | tail -15`); failing tests first where
practical; isolate the seeded-surface task (drop tables, rng changes) so pin
movement has exactly one cause; the last task is always docs (`FEATURES.md`,
`design-decisions.md` if direction was decided) in the same PR.

## Step 4 — STOP

Set `Your sign-off` (removing `Plan`) — the completed plan now
awaits the maintainer's explicit OK to build. Tell the maintainer the issue is
ready for design review and **end there**. The build belongs to the
`build-slice` skill, and it starts only when the maintainer says go.
