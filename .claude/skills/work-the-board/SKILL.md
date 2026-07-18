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
2. **Classify + act**, cheapest-and-most-reversible first, then merges, then the
   one build. For each item, **hand off to the skill that owns that step** rather
   than improvising:

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
| PR carrying `ready to merge` | **merge it** (label + green CI + rebase-if-behind + squash) | `merge-pr` |

3. **Refresh each ticket's Next-steps reminder** (post if missing, edit in place
   if stale) so its state + available actions are always current — see below.
4. **The build cap: one build per pass.** Cheap advancement, replies, and merges
   are unlimited; but do only the **single** highest-priority build (an approved
   slice, else a bug fix), then leave the rest labelled for the next pass. This
   bounds cost and blast radius so the maintainer sees each build before the next
   starts.
5. **Looks-driven** design steps still get their mockup first (`mockup` skill) —
   never build UI a pass hasn't previewed for the maintainer.

## The Next-steps reminder — keep it fresh on every ticket

Every open ticket carries **one** auto-maintained comment, headed
`> 🤖 **Next steps**`, that states where the ticket is and what actions are
available — so the maintainer *and* any commenter can move it without knowing the
workflow. **One comment per ticket, edited in place** (find it by its header and
update it — never post a second). A pass refreshes it wherever it no longer
matches the ticket's state, and — the general convention (CLAUDE.md) — it is
refreshed whenever *any* skill flips a `needs:*` label.

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

## Loop form (later)

"work the board every `<interval>`" / "…in a loop" runs this same pass on a
schedule — same contract, same one-build cap. Locally that's `/loop`;
unattended/away it's a `/schedule` cloud agent (headless: `gh`/GitHub works,
interactive-auth MCP does not). "stop the board" ends it.
