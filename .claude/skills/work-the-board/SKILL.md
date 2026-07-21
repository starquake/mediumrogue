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
comments and labels. A pass reads the board, does every **work-label** step it
can, executes the actions the maintainer has already authorised (a "go"
signal, a `ready to merge` label), and **stops at every gate that is theirs**.
The `needs:*` labels are the state machine; this skill just drives it.

**Two kinds of label, and the wording tells you which:**

- **Gate labels** — `needs: your input`, `needs: your sign-off`. Work *stops*.
  The maintainer decides; you may ask, never answer.
- **Work labels** — `needs: spec`, `needs: plan`, `needs: build`. Work
  *proceeds*. The label is the instruction; acting on it needs no further
  permission.

The test is in the label itself: **if it says "your", it is a gate.** No colour
legend, no memorised list — a label you have never seen before still sorts
correctly.

## The autonomy contract — do not cross

- **Never decide design direction.** Open questions are surfaced TO the
  maintainer (`needs: your input`), never answered by you.
- **Never grant `ready to merge`, and never merge without it.** The label is the
  maintainer's approval — you may *act* on it (merge), never *create* it.
  **Re-read the label at the moment you merge, from the API, not from a
  notification.** A Monitor event is a *pointer that something happened*, never
  a fact about the current state: it fires on the change and cannot see the
  retraction. (2026-07-19: `ready to merge` was added to #184 and removed 85
  seconds later when the maintainer noticed a red build. The monitor reported
  the add; merging on that event alone would have landed a PR whose approval
  had been explicitly withdrawn.) Same for CI: a green result seen before a
  push is not a green result after it.
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
   **Labels are NOT enough — read the comments too.** For every item at a
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

   **Read the ticket before ANSWERING it, too — not just before triaging it.**
   The rule above is about reading *their* comments; this one is about reading
   *your own*. When the maintainer asks a question in chat that belongs to an
   open ticket ("#175, what do popular RPGs use?"), fetch that ticket's comments
   before answering, and answer from what is already there. **A context break
   makes this mandatory rather than optional**: a compaction summary carries
   decisions, not what was already published to GitHub, so "I don't remember
   posting that" is no evidence that nothing was posted. (2026-07-19: a full
   genre-research comment — table, sources, its own answer block — had been on
   #175 for twenty minutes; the whole thing was re-derived and posted again as
   new, then a third comment presented an idea the first had already named. The
   thread asked the maintainer to close one ticket twice with two competing
   answer blocks. They caught it with "check the comment".)

   When duplication has already happened, **own it in a NEW comment** naming
   which earlier one is the better version — never edit or delete the
   redundant one. The append-only rule is not just for state changes: the
   thread is the record, including the noise in it.

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
   directions is `needs: your input`, not a silent backlog entry; a **bug** is
   `needs: build`, since bugs have no design gate) and a Next-steps reminder.
   A filed-and-unlabelled issue is invisible.

   **Filing is not done when `gh issue create` returns — do the label and the
   reminder in the SAME action.** Not "later in the pass": filing usually
   happens at the tail of something bigger, where the new ticket feels like a
   by-product rather than work, and that is exactly when the follow-up step
   gets dropped. (2026-07-19: #181 was filed with a strong body — repro
   command, evidence it was pre-existing, an explicit "not root-caused" — and
   no label, no reminder. The maintainer found it: *"#181 does not have
   instructions on how to continue."* This rule already existed and still
   didn't hold, which is why it is now part of the create rather than a step
   after it.)

   A ticket with evidence but no next step is close to not having been filed.
   Audit with `gh issue list --json number,labels` filtered to those carrying
   no `needs:` label — a pass that files anything should end by running it.

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
  spec/plan and hands back to a gate — nothing needed from you.*
- `needs: build` — approved. *Next: a pass builds a draft PR; then add
  `ready to merge` once you've reviewed it.*
- no `needs:` label — tailor: **blocked** (name the blocker — "blocked by #NN"),
  a **reference/record** (no action), or **un-triaged** (*Next: say "work the
  board" to triage it into a spec + questions*).
- `hold` — skipped; do not touch it or its reminder.
- a **PR** — `ready to merge` present → *Next: a pass merges it*; else it's a
  draft / awaiting CI / awaiting your `ready to merge`.

## Blue work is not a menu — pick it up

**A work label gets worked, not reported.** `needs: spec`, `needs: plan`,
`needs: build`, and any **bug** are already authorised: the label IS the
instruction. Listing one back to the maintainer as "available" turns a state
machine into a suggestion box, and makes them the scheduler for work they
already assigned.

This fails most often **outside a loop**, answering messages one at a time:
each reply feels complete on its own, and authorised work sits in the queue
because no reply happened to be about it. (2026-07-19: #181 sat labelled
`needs: build` across several exchanges until the maintainer asked *"why are
you not picking up #181 automatically?"* — there was no reason. Nothing
blocked it.)

So: **end every exchange by checking whether anything is in your court, and if
it is, do it** rather than closing with a status table. The one-build-per-pass
cap still applies inside a loop; it is not a licence to defer the build.
Genuinely nothing authorised is a fine answer — "everything is at your gate"
is a complete pass. Work you were handed and left is not.

**Drafting a spec or mockup is safe autonomous work — never defer it as
"needs supervision."** The one-build-per-pass cap and the "hold risky/large
builds for the maintainer's eyes" judgement are about *building code*. They do
NOT apply to advancing a `needs: spec` ticket: drafting the spec (and the
mockup for a looks-driven one) ends at `needs: your input` — the maintainer's
gate — so it is reversible and commits nothing. The mockup-first rule means
*get a mockup approved before building the UI*; it does not mean *wait for the
maintainer before drafting the mockup*. (2026-07-20: three `needs: spec`
tickets sat undrafted through two quiet loop passes because "looks-driven,
needs approval before building" was misread as "can't advance without them";
the maintainer asked *"why aren't you working on the needs:spec ones?"* — there
was no reason.) When a loop has no authorised build to do, drafting a queued
spec+mockup is the highest-value thing still in your court.

## Reporting

End the pass with a short summary: **what moved** (and to what state), **what you
built or merged**, and — most important — **what's now waiting on the
maintainer** (the `needs: your input` / `needs: your sign-off` gate queue). In a
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

### Prefer a Monitor over polling — it is faster AND free

A timed tick costs tokens every time it fires, including the many times it
finds nothing. A **Monitor** inverts that: the poll loop runs in the shell,
outside the agent's context, and only a printed line wakes anyone. Quiet
minutes cost **nothing**, and reaction time drops from tens of minutes to
about one.

Arm one at the start of a loop session (persistent, so it lives as long as
the session), watching the only two things a pass acts on unprompted — a new
maintainer comment, and a PR newly carrying `ready to merge`:

```bash
since=$(date -u +%Y-%m-%dT%H:%M:%SZ)
prev=$(gh pr list --state open --label "ready to merge" --json number -q '.[].number' 2>/dev/null | sort)
while true; do
  sleep 60
  now=$(date -u +%Y-%m-%dT%H:%M:%SZ)

  # Human comments anywhere in the repo; ours carry the bot header and are skipped.
  gh api "repos/<owner>/<repo>/issues/comments?since=$since&per_page=30" \
    --jq '.[] | select((.body | startswith("> 🤖")) | not)
          | "COMMENT on #\(.issue_url | split("/") | last): \(.body | gsub("\n"; " ") | .[0:120])"' 2>/dev/null || true

  # DIFFED against last cycle, or a labelled PR re-fires every minute and the
  # monitor gets auto-stopped for noise.
  cur=$(gh pr list --state open --label "ready to merge" --json number -q '.[].number' 2>/dev/null | sort)
  # grep keeps ONLY real numbers: when the labelled set empties, `echo ""`
  # feeds comm a blank line that it reports as new, emitting a bogus
  # "READY TO MERGE: PR #" with no number.
  comm -13 <(echo "$prev") <(echo "$cur") 2>/dev/null | grep -E '^[0-9]+$' | sed 's/^/READY TO MERGE: PR #/' || true
  prev=$cur

  since=$now
done
```

Two things that make it behave:

- **Both signals are mandatory — a label-only monitor is a silent trap.** The
  watch covers *two* things: new maintainer comments AND `ready to merge`. Ship
  it with only the label poll and every maintainer comment on a gated issue
  goes unseen — the loop looks healthy while an answer sits unread. (2026-07-21:
  a monitor armed with only the `ready to merge` poll dropped @starquake's
  per-IP answer on #199; it sat unanswered until they asked in chat why nothing
  had moved. If you hand-roll the monitor, keep the comment poll in it; never
  trim it to "just the labels".)
- **Diff the label set.** Emitting the current set every cycle spams a
  notification per minute for any PR that sits unmerged, and monitors that
  flood get stopped automatically.
- **`|| true` on every remote call.** One failed request must not kill a
  session-length watch.
- **Filter the diff to real values.** An empty set becomes a blank line, and
  a blank line looks "new" to `comm` — which fired a phantom
  "READY TO MERGE: PR #" the first time a merge emptied the label set
  (2026-07-19). Anything derived from a diff of two lists wants a shape check
  before it becomes a notification.

**Rate limits are not the constraint.** Authenticated GitHub REST allows
5,000 requests/hour; this loop uses 2/minute — **120/hour, ~2.4% of quota**.
If a faster poll were ever wanted, conditional requests (`If-None-Match`)
return 304 and do not count against the primary limit. GitHub webhooks would
be true push, but need a public receiver — real infrastructure to shave 60
seconds off something already free.

With a Monitor armed, the scheduled tick becomes **insurance only** — set it
to ~an hour, so a dead monitor is noticed but a healthy one costs nothing.

### Pacing when there is no Monitor: back off when quiet, speed up when active

A fixed interval is wrong in both directions — too slow while work is in
flight, too fast when the board is parked on the maintainer. In dynamic mode
(`/loop` with no interval), pick the next delay from **what this pass actually
found**:

| This pass… | Next delay |
|---|---|
| merged, built, or is watching CI it just pushed | **~5 min** — something is in flight and the next step is yours |
| folded in answers, replied, advanced a ticket | **~10 min** — the maintainer is at the keyboard; more may land |
| found new maintainer comments but nothing to do yet | **~15 min** |
| found nothing new (1st quiet pass) | **~20 min** |
| found nothing new (2nd) | **~40 min** |
| found nothing new (3rd or later) | **60 min** (the ceiling) |

**Any activity resets to the top of the table** — one answered question means
they're back, and the next pass should be prompt. The runtime clamps to
[60, 3600] seconds, so 60 min is the practical ceiling.

**After three consecutive quiet passes, say so and offer to stop.** A loop
whose every pass re-reads the same labels is burning turns; the queue being
long is information *for them*, not a reason to keep spinning. They can
restart it in one word.
