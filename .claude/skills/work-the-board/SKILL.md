---
name: work-the-board
description: >
  Use when the maintainer says "work the board" — do ONE triage-and-advance pass
  over the repo's open issues and pull requests (plus recently-closed, for late
  comments), moving each to its next step in the issue workflow: reply to
  comments, draft/refine specs and plans, build slices the maintainer has already
  approved, and merge PRs that carry `ready to merge`. It stops at every human
  gate. "work the board every 30m" / "…in a loop" runs the same pass on a
  schedule (see Loop form). Trigger on the phrase even if the maintainer doesn't
  say "skill". "stop the board" ends the loop.
---

You run the issue tracker forward so the maintainer can drive everything through
comments and labels. A pass reads the board, does every **blue (your-court)**
step it can, executes the actions the maintainer has already authorised (a "go"
signal, a `ready to merge` label), and **stops at every gate that is theirs**.
The `needs:*` labels are the state machine; this skill just drives it.

## The autonomy contract — do not cross

- **Never decide design direction.** Open questions are surfaced TO the
  maintainer (`needs: your input`), never answered by you.
- **Never grant `ready to merge`, and never merge without it.** The label is the
  maintainer's approval — you may *act* on it (merge), never *create* it.
- **Build only what's authorised:** a plan the maintainer OK'd (`needs: build`,
  or a go-signal on a `needs: your sign-off` issue), or a **bug** (no design gate
  — straight to a PR). Never build a `needs: your input` / `needs: your sign-off`
  slice.
- **Skip anything labelled `hold`** entirely — no comment, no build, no merge.
- Every comment you post carries the 🤖 attribution header (it goes through the
  maintainer's account; see CLAUDE.md).
- **At most ONE build per pass** (see the cap).

## The pass

1. **Enumerate.** `gh issue list --state open`, `gh pr list --state open`, and a
   recently-closed sweep for comments that landed after close. Drop anything
   carrying `hold`.
   **Labels are NOT enough — read the comments too.** For every item at an amber
   gate, fetch its comments (`gh api repos/:owner/:repo/issues/<n>/comments`) and
   look for the maintainer's answer or go-signal *after* your last comment. A
   go-signal (`go` / `build` / `approved` / "Build!") arrives as a **comment**
   and does **not** change the label — so a label-only pass reports "waiting on
   you" when they already answered. (This exact miss happened 2026-07-18: "Build!"
   sat on #88 and #92 through a whole pass.)

   **Scope that read by YOUR LAST COMMENT on that ticket — never by a wall-clock
   window.** `select(.created_at > "<some time>")` feels equivalent and isn't:
   an answer written before the cutoff is invisible, and the cutoff is always a
   guess about when you last looked. Find your own most recent comment on the
   ticket and read everything after it; if you have never commented, read the
   lot. (Second miss, same day: an answer on #155 sat 40 minutes outside a
   19:20 cutoff, so a pass asked the maintainer a question they had already
   answered — twice.)

   **A label REMOVED without a comment is a signal, not noise.** The maintainer
   answering in prose and then clearing the `needs:*` label is a normal way to
   say "done, over to you" — so a label that vanished since your last pass means
   go and re-read the thread, not "they tidied up".
2. **Classify + act** in this order — **merges always before builds**. Anything
   built before a pending merge lands on a stale base and has to rebase, so a
   `ready to merge` PR is the first real action of a pass, not the last:

   1. cheap advancement (replies, labels, specs, plans, reminders)
   2. **every PR carrying `ready to merge`** — merge them all, then re-pull
   3. **then** the pass's one build, branched off the freshly-merged `main`

   When two open PRs touch the same files (registries, drop tables, protocol),
   say so in the second one's body — whoever merges second owns the rebase, and
   they should know before review, not after.

   For each item, **hand off to the skill that owns that step** rather than
   improvising:

| State | Action | Owner |
|---|---|---|
| unanswered comment (issue/PR) | reply — factual auto-post, substantive draft for OK | `issue-comment-replies` |
| new issue, no `needs:` label | triage: mechanic → draft spec + questions → `needs: your input`; tweak → PR | `design-slice` |
| **bug** (new, or a reproduced report) | reproduce → root-cause → open a **green draft PR** fix | debug → PR |
| `needs: spec` | draft the spec + its open questions → `needs: your input` | `design-slice` |
| `needs: your input` | **stop** — UNLESS a new maintainer comment answers the questions → fold in → `needs: plan` → plan → `needs: your sign-off` | `design-slice` |
| `needs: plan` | write the plan → `needs: your sign-off` | `design-slice` |
| `needs: your sign-off` | **stop** — UNLESS the maintainer signalled **go** (a `go`/`build`/`approved` comment, OR they flipped it to `needs: build`) → build | `build-slice` |
| `needs: build` | build the approved slice → **green draft PR** (never ready, never merge) | `build-slice` |
| PR with new maintainer comments | address them, re-push | rework |
| PR carrying `ready to merge` | **merge it** (label + green CI + rebase-if-behind + squash), then **close the milestone if that was its last open issue** | `merge-pr` |
| **latent breakage you trip over while sweeping** (a dangling reference, a stale doc claim, an asset something embeds but that never reached `main`) | fix it → **green draft PR** | debug → PR |

3. **Post a Next-steps reminder on every ticket whose state you just moved** —
   a new comment each time, never an edit of the previous one, so the thread
   reads chronologically. Unchanged state → no comment. See below.
4. **The build cap: at most one build per pass — and ZERO is a valid pass.**
   Cheap advancement, replies, and merges are unlimited; but do only the
   **single** highest-priority build (an approved slice, else a bug fix, else a
   latent breakage), then leave the rest labelled for the next pass. This bounds
   cost and blast radius so the maintainer sees each build before the next
   starts. **If nothing is authorised, build nothing and say so** — never
   manufacture a design slice, a refactor, or a "while I'm here" change to fill
   the slot. An empty pass that reports "everything is at your gate" is doing its
   job.
5. **Looks-driven** design steps still get their mockup first (`mockup` skill) —
   never build UI a pass hasn't previewed for the maintainer.
6. **A ticket a pass FILES gets the same treatment as one it finds**: a
   `needs:*` label naming whose court it lands in (a finding with open
   directions is `needs: your input`, not a silent backlog entry) and a
   Next-steps reminder. A filed-and-unlabelled issue is invisible.

## The Next-steps reminder — append one whenever the state moves

Every open ticket carries an auto-maintained comment, headed
`> 🤖 **Next steps**`, that states where the ticket is and what actions are
available — so the maintainer *and* any commenter can move it without knowing the
workflow.

**Post a NEW comment each time the state changes — never edit the previous one
in place.** The thread is the ticket's history: editing rewrites it, so a
reader who saw the old state can't tell what changed or when. Appending keeps
the issue readable top-to-bottom, in order. The older reminders simply stand as
the record of where the ticket has been.

**Write it as a reply, not a dashboard.** The ticket is a back-and-forth: they
comment, you comment back, and the thread reads in order. So a reminder
answers *what just happened* — "folded your answers in, plan's in the body,
one thing I didn't decide for you" — rather than re-stating the whole ticket
from scratch. Assume the reader has the comment above it.

A reminder that no longer matches the ticket is **history, not an error** — it
was true when posted. Supersede it with a new one; never edit or delete it to
make the past tidy.

Post one whenever the state actually moves (a `needs:*` flip, a plan landing, a
PR opening or merging) — not on every pass. If nothing changed since the last
reminder, say nothing: an unchanged state re-posted is noise, and the previous
comment is still accurate.

Content by state (state line + a "Next:" line naming the action and who does it):

- `needs: your input` — waiting on the maintainer. *Next: answer the open
  questions in a comment; the next pass folds them in and writes the plan.*
  **Always include a copy-paste answer block**: a fenced code block listing each
  open question as one line with its shorthand options (`Q3 scope = world-only |
  also-in-combat`), headed `# keep your pick, delete the rest`, plus a free-text
  `notes =` line. The maintainer answers by pasting one filled-in block — no
  prose required, and a pass can read the answers unambiguously. Free-form
  questions get a labelled blank instead of options.
- `needs: your sign-off` — settled, waiting on the maintainer's OK. *Next:
  comment `go` / `build` / `approved` (or flip the label to `needs: build`); the
  next pass builds it into a draft PR.*
- `needs: spec` / `needs: plan` — Claude's court. *Next: a pass drafts the
  spec/plan and hands to the amber gate — nothing needed from you.*
- `needs: build` — approved. *Next: a pass builds a draft PR; then add
  `ready to merge` once you've reviewed it.*
- no `needs:` label — tailor: **blocked** (name the blocker — "blocked by #NN"),
  a **reference/record** (no action), or **un-triaged** (*Next: say "work the
  board" to triage it into a spec + questions*).
- `hold` — skipped; do not touch it or its reminder.
- a **PR** — `ready to merge` present → *Next: a pass merges it*; else it's a
  draft / awaiting CI / awaiting your `ready to merge`.

## Reporting

End the pass with a short summary: **what moved** (and to what state), **what you
built or merged**, and — most important — **what's now waiting on the
maintainer** (the amber `needs: your input` / `needs: your sign-off` queue). In a
loop this becomes a push notification ONLY when something needs them.

Report what a pass *chose not to do* as plainly as what it did: a build it
declined for lack of authorisation, a decision it refused to make on their
behalf, a flake it could not reproduce. Silence on those reads as "nothing
happened", which is a different and false claim.

## Loop form

"work the board every `<interval>`" / "…in a loop" runs this same pass on a
schedule — same contract, same one-build cap. Locally that's `/loop`;
unattended/away it's a `/schedule` cloud agent (headless: `gh`/GitHub works,
interactive-auth MCP does not). "stop the board" ends it.

**When the whole board is at the maintainer's gate, say so and offer to pause.**
A loop whose every pass finds nothing authorised is burning turns to re-read the
same labels. Stretch the interval or stop and let them restart it — the queue
being long is information *for them*, not a reason to keep spinning.
