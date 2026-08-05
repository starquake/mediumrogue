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
comments and the board. A pass reads the board, does every **work-state** step it
can, executes the actions the maintainer has already authorised (a "go"
signal, a `ready to merge` label), and **stops at every gate that is theirs**.
The Status field is the state machine; this skill just drives it. Read it with
`.claude/scripts/board.sh list "<state>"` and move it with
`.claude/scripts/board.sh state <n> "<state>"`.

**Two kinds of state, and the wording tells you which:**

- **Gate states** — `Your input`, `Your sign-off`. Work *stops*.
  The maintainer decides; you may ask, never answer.
- **Work states** — `Spec`, `Plan`, `Build`. Work
  *proceeds*. The state is the instruction; acting on it needs no further
  permission.

The test is in the state name itself: **if it says "your", it is a gate.** No
colour legend, no memorised list — a state you have never seen before still
sorts correctly.

**Do not re-ask permission on a work state — not even a big or scary one.**
Size, architectural weight, or serialization/design risk does NOT downgrade a
`Build` (or `Spec`/`Plan`) to a gate. You start it *now*.
If the build carries load-bearing decisions, you make them and **surface them in
the draft PR for review** — the PR review plus the `ready to merge` label IS the
sign-off. Converting a work state back into a "should I kick this off, or hold?"
question re-invents a gate the maintainer already opened, and stalls their own
authorised work. (2026-07-21: held #271's `Build` mechanics slice and
asked "kick it off now, or park it?" — the maintainer: *"why don't you pick up
the needs build now?"* The rule was already here; the build being large and
architectural is exactly when the pull to re-ask is strongest and must be
resisted.)

**But a work state is not a promise that the ticket is workable.** The rule
above is about not re-asking for *permission*; it is not licence to invent
content the ticket does not contain. When a ticket in a work state lacks the
information to do the work — an empty body, a symptom with no reproduction, a
title and nothing else — the honest step is back to `Your input` with
**specific** questions, having first done every bit of investigation that does
not need the answer.

The distinction is what you are asking for. "Shall I start?" is re-inventing a
gate. "I looked, here is what I found, and here are the two facts only you have"
is the work. Do the investigation first either way: a question that arrives with
findings attached is worth answering, one that arrives empty is just the ticket
handed back.

(2026-07-31: #315 sat in `Spec` with an EMPTY body — the title was the whole
report. Reading the client found a genuine asymmetry (a move clears a committed
attack, an attack never clears the destination ring), but three attempts to
reproduce the reported behaviour failed, each for a different reason. Writing a
spec would have meant inventing the bug's shape; fixing on the reading alone
would have meant guessing which of two similar markers the maintainer saw. It
went back with the finding, the failed attempts, and two questions.)

**Do not ship a fix on a reading-only diagnosis when the reproduction fails.**
This is the flake rule generalised past flakes: reading the code tells you what
CAN happen, not what DID. If the repro will not come, say so plainly, publish
the mechanism you found, and let the maintainer confirm the shape before code
changes. A confident fix for the wrong mechanism closes the ticket and leaves
the bug.

## The autonomy contract — do not cross

- **Never decide design direction.** Open questions are surfaced TO the
  maintainer (`Your input`), never answered by you.
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
- **Build only what's authorised:** a plan the maintainer OK'd (`Build`,
  or a go-signal on a `Your sign-off` issue), or a **bug** (no design gate
  — straight to a PR). Never build a `Your input` / `Your sign-off`
  slice.
- **Skip anything labelled `hold`** entirely — no comment, no build, no merge.
- Every comment you post carries the 🤖 attribution header (it goes through the
  maintainer's account; see CLAUDE.md).
- **At most ONE build per pass** (see the cap).

## The pass

**Standing authorised work goes first.** Before anything else, read **every**
work lane — `Build`, then `Plan`, then `Spec` — and work from there. All three
are work states, all three are yours, and none of them needs further
permission. A spec waiting to be written is standing work exactly as a build
is, and it is the easiest to leave sitting precisely because nothing about it
feels urgent.

Work does not earn priority by being *new*: a ticket filed five minutes ago, a
request that just arrived, a failure you happened to notice are all louder than
a card that has been parked since yesterday, and loudness is not precedence.

The exceptions are real but must be *named as exceptions* rather than assumed:
a red `main`, a PR carrying `ready to merge`, and a direct maintainer request in
chat all pre-empt. Everything else queues behind the lane.

(2026-07-31: #318 and #333 sat in `Build`, and #315 in `Spec` — authorised,
ungated, nobody's court but Claude's — for an entire session, while three
reactive builds landed ahead of them. Every one was justifiable alone; the pattern was not. The maintainer
had to ask why the lane was untouched. The monitor could not help: it fires on
*transitions*, so a card that moved to `Build` an hour ago is silent forever
after — see the standing-queue heartbeat in the Monitor section.)

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
   answering in prose and then clearing the board state is a normal way to
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
| new issue, no `needs:` label | triage: mechanic → draft spec + questions → `Your input`; tweak → PR | `design-slice` |
| **bug** (new, or a reproduced report) | reproduce → root-cause → open a **green draft PR** fix | debug → PR |
| `Spec` | draft the spec + its open questions → `Your input` | `design-slice` |
| `Your input` | **stop** — UNLESS a new maintainer comment answers the questions → fold in → `Plan` → plan → `Your sign-off` | `design-slice` |
| `Plan` | write the plan → `Your sign-off` | `design-slice` |
| `Your sign-off` | **stop** — UNLESS the maintainer signalled **go** (a `go`/`build`/`approved` comment, OR they flipped it to `Build`) → build | `build-slice` |
| `Build` | build the approved slice → **green draft PR** (never ready, never merge) | `build-slice` |
| `Your review` | **stop** — the build is done and its PR is open; the maintainer's `ready to merge` is the only thing that moves it. Do keep the PR mergeable: CI green, rebased if behind. | `merge-pr` |
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
   board state naming whose court it lands in (a finding with open
   directions is `Your input`, not a silent backlog entry; a **bug** is
   `Build`, since bugs have no design gate) and a Next-steps reminder.
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

## Filing a ticket: say which route it takes

Every ticket you file states its **routing, with the reason**, and carries the
matching label — `needs: spec` or `needs: build`. Both already exist.

```
Routing: straight to Build — a tweak, no unexamined assumption.
Routing: Spec first — the reward economy is undecided, and it changes the shape.
```

You filed the ticket and did the investigation, so you have already made this
call; leaving it in your head means the maintainer re-derives it from a title on
a board card. **The label is what makes it visible without opening anything** —
the board renders titles, not bodies.

**The reason is not decoration.** A bare verdict cannot be disagreed with, and
disagreement is expected: the maintainer dragging the card somewhere else *is*
the override, exactly as with any other routing call.

Which is which follows CLAUDE.md's existing rule — *is there a design decision
with unexamined assumptions?*

- **`needs: build`** — a bug, or a tweak (a tuning number, a config default,
  copy, an order). Straight to a PR.
- **`needs: spec`** — a new mechanic or feature, where the assumptions want
  questioning before anything is written.

**Do not reach for a new Status option to carry this.** A position in the flow
is a Status; a property of the ticket is a label — the same reason `hold` and
`ready to merge` are labels. Adding a Status option means touching the
single-select, and reordering one replaces every option and clears every item's
value (2026-07-28). (Raised 2026-08-05: *"do you have a better idea for me to
know if the next step is spec or build?"* — the first instinct was a `Triage`
lane, and a label is strictly better: no column, no wipe risk, and one drag
instead of two.)

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

**Posting an answer block IS a `Your input` move — set the label in the
same action.** The reminder reflects the label, but the reverse also holds:
the moment you write a copy-paste answer block asking the maintainer to decide,
the ticket is in their court, so `Your input` goes on **in the same
step** — even if it was previously unlabelled backlog. Don't let the ticket's
prior state carry: your question is what moved it, and a prior "open-ended
backlog, no label" framing does not survive your posting a concrete decision.
The label and the answer block are one action, never two. (2026-07-21: an
answer block went onto #122 — an "open-ended backlog" ticket — while it stayed
unlabelled; the reminder said "your call" but no label said so, so it read as
belonging to nobody.)

Content by state (state line + a "Next:" line naming the action and who does it):

- `Your input` — waiting on the maintainer. *Next: answer the open
  questions in a comment; the next pass folds them in and writes the plan.*
  **Always include a copy-paste answer block**: a fenced code block listing each
  open question as one line with its shorthand options (`Q3 scope = world-only |
  also-in-combat`), headed `# keep your pick, delete the rest`, plus a free-text
  `notes =` line. The maintainer answers by pasting one filled-in block — no
  prose required, and a pass can read the answers unambiguously. Free-form
  questions get a labelled blank instead of options.
- `Your sign-off` — settled, waiting on the maintainer's OK. *Next:
  comment `go` / `build` / `approved` (or flip the label to `Build`); the
  next pass builds it into a draft PR.*
- `Spec` / `Plan` — Claude's court. *Next: a pass drafts the
  spec/plan and hands back to a gate — nothing needed from you.*
- `Build` — approved. *Next: a pass builds a draft PR; then add
  `ready to merge` once you've reviewed it.*
- no `needs:` label — tailor: **blocked** (name the blocker — "blocked by #NN"),
  a **reference/record** (no action), or **un-triaged** (*Next: say "work the
  board" to triage it into a spec + questions*).
- `hold` — skipped; do not touch it or its reminder.
- a **PR** — `ready to merge` present → *Next: a pass merges it*; else it's a
  draft / awaiting CI / awaiting your `ready to merge`.

## Blue work is not a menu — pick it up

**A work label gets worked, not reported.** `Spec`, `Plan`,
`Build`, and any **bug** are already authorised: the label IS the
instruction. Listing one back to the maintainer as "available" turns a state
machine into a suggestion box, and makes them the scheduler for work they
already assigned.

This fails most often **outside a loop**, answering messages one at a time:
each reply feels complete on its own, and authorised work sits in the queue
because no reply happened to be about it. (2026-07-19: #181 sat labelled
`Build` across several exchanges until the maintainer asked *"why are
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
NOT apply to advancing a `Spec` ticket: drafting the spec (and the
mockup for a looks-driven one) ends at `Your input` — the maintainer's
gate — so it is reversible and commits nothing. The mockup-first rule means
*get a mockup approved before building the UI*; it does not mean *wait for the
maintainer before drafting the mockup*. (2026-07-20: three `Spec`
tickets sat undrafted through two quiet loop passes because "looks-driven,
needs approval before building" was misread as "can't advance without them";
the maintainer asked *"why aren't you working on the needs:spec ones?"* — there
was no reason.) When a loop has no authorised build to do, drafting a queued
spec+mockup is the highest-value thing still in your court.

## Reporting

End the pass with a short summary: **what moved** (and to what state), **what you
built or merged**, and — most important — **what's now waiting on the
maintainer** (the `Your input` / `Your sign-off` gate queue). In a
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

Arm one at the start of a loop session (persistent, so it lives as long as the
session). The list of what it watches is not a matter of taste — it is derived
from **what the pass acts on**, and every omission has cost a real miss:

| signal | why the pass cares |
|---|---|
| issue/PR conversation comments | the maintainer's answers and go-signals |
| **PR review comments** | inline diff feedback — a *different* endpoint |
| `ready to merge` | the only merge authorisation |
| `hold` added / lifted | an override that stops or releases work |
| board **Status** transitions | a move to `Build` authorises a build |
| **`main` going red** | pre-empts everything else in the pass |
| an open PR going red | after its build stopped watching CI |
| **standing work lanes** (level-triggered) | authorised work that is merely *waiting* |

```bash
# pipefail is LOAD-BEARING: every snapshot below ends in `| sort`, and a
# pipeline's status is the LAST command's. Without this a failed `gh` returns 0
# with no output, and every guard in this script silently passes (#359).
set -o pipefail

since=$(date -u +%Y-%m-%dT%H:%M:%SZ)
SELF="${BOARD_SELF_SET_FILE:-${TMPDIR:-/tmp}/mediumrogue-board-selfset}"
GQ='{ user(login:"<owner>"){ projectV2(number:<n>){ items(first:100){ nodes{
  content{ ... on Issue { number } }
  fieldValueByName(name:"Status"){ ... on ProjectV2ItemFieldSingleSelectValue { name } }
}}}}}'
# NO `|| true` on a snapshot. A failed call must FAIL, so the caller can skip
# the diff — see "a failed poll is not an empty board" below.
snap_board(){ gh api graphql -f query="$GQ" --jq '.data.user.projectV2.items.nodes[]
  | select(.content.number != null) | "\(.content.number)|\(.fieldValueByName.name // "none")"' 2>/dev/null | sort -n; }
snap_label(){ gh pr list --state open --label "$1" --json number -q '.[].number' 2>/dev/null | sort; }
snap_hold(){ gh issue list --state open --label hold --json number -q '.[].number' 2>/dev/null | sort; }
# statusCheckRollup in ONE call: which open PRs are red, rather than per-PR polling.
snap_prci(){ gh pr list --state open --json number,statusCheckRollup \
  -q '.[] | select([.statusCheckRollup[]?|select(.conclusion=="FAILURE")]|length > 0) | .number' 2>/dev/null | sort || true; }
snap_main(){ gh run list --branch main --workflow CI --limit 1 \
  --json conclusion -q '.[0].conclusion // "none"' 2>/dev/null || echo none; }

# Guard the INITIALISATION too: a failed first call leaves prev empty, and the
# first successful cycle then floods however well the loop is guarded.
until prev_s=$(snap_board) && [ -n "$prev_s" ]; do sleep 30; done
prev_l=$(snap_label "ready to merge"); prev_h=$(snap_hold)
prev_ci=$(snap_prci); prev_main=$(snap_main); ticks=0
while true; do
  sleep 60
  now=$(date -u +%Y-%m-%dT%H:%M:%SZ)

  # 1. Human comments — issue AND pull-request CONVERSATION comments.
  gh api "repos/<owner>/<repo>/issues/comments?since=$since&per_page=30" \
    --jq '.[] | select((.body | startswith("> 🤖")) | not)
          | "COMMENT on #\(.issue_url | split("/") | last): \(.body | gsub("\n"; " ") | .[0:120])"' 2>/dev/null || true

  # 2. PR REVIEW comments live on a DIFFERENT endpoint — inline diff feedback is
  #    invisible to the poll above, however many comments it returns.
  gh api "repos/<owner>/<repo>/pulls/comments?since=$since&per_page=30" \
    --jq '.[] | select((.body | startswith("> 🤖")) | not)
          | "REVIEW COMMENT on \(.pull_request_url | split("/") | last) \(.path):\(.line // 0): \(.body | gsub("\n"; " ") | .[0:100])"' 2>/dev/null || true

  # 3. `ready to merge`, diffed — emitting the whole set every cycle would spam.
  # `[ -n "$cur" ] || [ -z "$prev_l" ]` is the SOFT-failure guard: gh can return
  # HTTP 200 with an errors array and no data, so jq prints nothing and the exit
  # code is 0 — which no exit-status check catches. A set that was non-empty and
  # is now empty is an outage until proven otherwise.
  if cur=$(snap_label "ready to merge") && { [ -n "$cur" ] || [ -z "$prev_l" ]; }; then
    comm -13 <(echo "$prev_l") <(echo "$cur") 2>/dev/null | grep -E '^[0-9]+$' | sed 's/^/READY TO MERGE: PR #/' || true
    prev_l=$cur
  fi

  # 4. `hold` is an override that STOPS work — both directions matter.
  if cur=$(snap_hold) && { [ -n "$cur" ] || [ -z "$prev_h" ]; }; then
    comm -13 <(echo "$prev_h") <(echo "$cur") 2>/dev/null | grep -E '^[0-9]+$' | sed 's/^/HOLD ADDED: #/' || true
    comm -23 <(echo "$prev_h") <(echo "$cur") 2>/dev/null | grep -E '^[0-9]+$' | sed 's/^/HOLD LIFTED: #/' || true
    prev_h=$cur
  fi

  # 5. main going red. The pass treats this as pre-empting everything, so not
  #    watching it means the maintainer is the detector — which is what happened.
  cur=$(snap_main)
  [ "$cur" != "$prev_main" ] && [ "$cur" = "failure" ] && echo "MAIN IS RED: CI failed on main"
  [ "$cur" != "$prev_main" ] && [ "$prev_main" = "failure" ] && [ "$cur" = "success" ] && echo "main is green again"
  prev_main=$cur

  # 6. An open PR going red AFTER its build watched CI to completion.
  if cur=$(snap_prci) && { [ -n "$cur" ] || [ -z "$prev_ci" ]; }; then
    comm -13 <(echo "$prev_ci") <(echo "$cur") 2>/dev/null | grep -E '^[0-9]+$' | sed 's/^/PR CI RED: #/' || true
    prev_ci=$cur
  fi

  # 7. Board Status, every transition, old -> new. Moves WE made are skipped:
  #    board.sh records them, and each entry is consumed once so a later genuine
  #    move to the same state still fires.
  # Skip the whole board block if the query failed: an empty snapshot would
  # read as "every card vanished", then as "every card is new" on recovery.
  if ! cur_s=$(snap_board) || { [ -z "$cur_s" ] && [ -n "$prev_s" ]; }; then
    since=$now
    continue
  fi

  printf '%s\n' "$cur_s" | while IFS='|' read -r n st; do
    [ -z "$n" ] && continue
    was=$(printf '%s\n' "$prev_s" | awk -F'|' -v k="$n" '$1==k{print $2}')
    [ "$was" = "$st" ] && continue
    if [ -f "$SELF" ] && grep -qxF "$n|$st" "$SELF"; then
      # `|| true`: grep -v exits 1 when it removes the only line, and `&& mv`
      # would then silently never run, suppressing that state forever.
      grep -vxF "$n|$st" "$SELF" > "$SELF.tmp" || true
      mv "$SELF.tmp" "$SELF"
      continue
    fi
    if [ -z "$was" ]; then echo "BOARD: #$n added as '$st'"
    else echo "BOARD: #$n moved $was -> $st"; fi
  done
  prev_s=$cur_s

  # 8. LEVEL-TRIGGERED heartbeat. Everything above fires on a CHANGE and is
  #    blind to work that is merely waiting. Every ~30 cycles, name what stands
  #    in the work lanes; silent when they are empty.
  ticks=$((ticks + 1))
  if [ $((ticks % 30)) -eq 0 ]; then
    waiting=""
    for lane in Build Plan Spec; do
      ids=$(printf '%s\n' "$cur_s" | awk -F'|' -v L="$lane" '$2==L{printf "#%s ", $1}')
      [ -n "$ids" ] && waiting="$waiting$lane: $ids"
    done
    [ -n "$waiting" ] && echo "STANDING work, outranks anything newly arrived — $waiting"
  fi

  since=$now
done
```

Deriving the list from the pass is the whole discipline. Three separate misses
came from watching a subset — a comment poll omitted (#199), Status omitted
(#313), the standing lanes and then `Spec` alone omitted (#347) — and each time
the monitor looked perfectly healthy while something sat unread. The cost of an
extra poll is one REST call a minute; the cost of a missing one is silence that
is indistinguishable from calm.

Things that make it behave:

- **All three signals are mandatory — a partial monitor is a silent trap.**
  Ship it with only the label poll and every maintainer comment on a gated
  issue goes unseen; ship it without the Status poll and a card moved to
  `Build` never wakes anything. Both have happened. (2026-07-21: a monitor
  armed with only the `ready to merge` poll dropped @starquake's per-IP answer
  on #199; it sat unanswered until they asked in chat why nothing had moved.
  2026-07-31: #313 was moved to `Build` and the loop did not react, because the
  monitor watched comments and labels only — the maintainer had to ask whether
  status changes were monitored at all.) The test is simple: **if the pass acts
  on a signal, the monitor watches it.** A move to `Build` is an authorisation
  exactly as a `go` comment is.
- **Watch every Status transition, not just the ones into `Build`.** A move
  *out* of a work state is a stop signal, and a move into a gate is the
  maintainer taking something back. Reporting `old -> new` costs nothing and
  makes the direction readable.
- **Report EVERY work lane, not just `Build`.** Watching one lane reproduces
  the original bug one lane over — which is exactly what happened: the first
  version of this heartbeat greped `Build` alone, while #315 sat in `Spec`,
  unworked, through the very session that prompted the fix. The maintainer
  caught it with "do you also check the other lanes like Spec?" (2026-07-31).
- **Report the standing queue, not only changes.** Every other signal here is
  edge-triggered, which means none of them can see work that is simply
  *waiting*: a card moved to `Build` an hour ago will never produce another
  event. Without a level-triggered heartbeat a lane holding two authorised
  builds is indistinguishable from an empty one, and reactive work wins by
  default forever (#347, 2026-07-31). Every ~30 cycles is deliberate — often
  enough that a parked lane resurfaces within the hour, rare enough that it is
  a heartbeat rather than a nag, and silent when the lanes are empty.
- **Never report your OWN board writes.** A pass sets several states, and each
  one otherwise wakes the agent a minute later to announce a change it just
  made — turning the cheapest signal into the noisiest. `board.sh state`
  appends `<issue>|<state>` to `$BOARD_SELF_SET_FILE` and the monitor consumes
  one matching entry per transition. **Consume, do not just match**: leaving
  the entry would swallow a genuine later move to the same state, which is the
  failure that actually matters. (Seen immediately, 2026-07-31: the first event
  the three-signal monitor produced was `#344 added as 'Your review'` — its own
  write, thirty seconds old.)
- **Diff the label set.** Emitting the current set every cycle spams a
  notification per minute for any PR that sits unmerged, and monitors that
  flood get stopped automatically.
- **`set -o pipefail`, or none of the other guards work.** Every snapshot ends
  in `| sort`, and a pipeline's exit status is the LAST command's — `sort`
  succeeds on empty input, so a failed `gh` returns **0 with no output** and
  every `if cur=$(snap); then` passes. (#359, 2026-08-02: the guard added by
  #347 could never fire, and the monitor emitted "added as …" for all 24 board
  items at once. The original verification used a stub that returned 1
  directly — that tested the guard's *shape* and never the real function's
  failure mode. **A mock that fails differently from the real thing proves
  nothing.**)
- **Guard the initialisation, and treat sudden emptiness as failure.**
  `prev=$(snap)` at startup has no guard at all, so a failed first call starts
  you from a lie. And `gh api graphql` can return HTTP 200 with an `errors`
  array and no `data` — jq prints nothing, exit code 0, which *no* exit-status
  check catches. A set that was non-empty and is now empty is an outage until
  proven otherwise.
- **A failed poll is not an empty board.** `|| true` keeps one bad request from
  killing a session-length watch — the right goal, but applied to a *snapshot*
  it converts failure into a false **empty set**, and the diff then reports
  every item as new the moment the API recovers. Verified 2026-08-01, when a
  transient outage surfaced through the watch: with `prev` holding three PRs and
  a failed call yielding "", the down-cycle is silent and the **recovery cycle
  emits `READY TO MERGE` for all three** — none of which carried the label.
  So: keep `|| true` on anything whose output is *emitted*, and drop it from
  anything whose output is *compared*, guarding each diff with `if cur=$(snap);
  then … fi` so a failed cycle leaves `prev` untouched and says nothing.
- **Filter the diff to real values.** An empty set becomes a blank line, and
  a blank line looks "new" to `comm` — which fired a phantom
  "READY TO MERGE: PR #" the first time a merge emptied the label set
  (2026-07-19). Anything derived from a diff of two lists wants a shape check
  before it becomes a notification.

**Rate limits: free on REST, a real trap on GraphQL.** The two polls have
separate budgets and only one of them is comfortable.

- **REST** (comments, PR labels) — 5,000 requests/hour, and this loop uses
  2/minute: **120/hour, ~2.4% of quota**. Not a constraint. If a faster poll
  were ever wanted, conditional requests (`If-None-Match`) return 304 and do
  not count against the primary limit.
- **GraphQL** (the board) — a separate 5,000 **points**/hour budget, and the
  obvious way to read the board blows it:

  | call | cost | at 60s polling |
  |---|---|---|
  | `gh project item-list --limit 200` (what `board.sh list` uses) | **102** | 6,120/hr — **over budget** |
  | the targeted `projectV2 { items { content{number} fieldValueByName } }` above | **1** | 60/hr |

  Measured 2026-07-31. **Use the targeted query in the monitor** — `board.sh`
  is built for one-shot reads in a pass, not for a loop. This is not
  theoretical: a `gh project item-list` sweep already emptied the GraphQL
  budget once (83 → 0 of 5,000), after which every board call failed while the
  REST polls carried on looking healthy.

GitHub webhooks would be true push, but need a public receiver — real
infrastructure to shave 60 seconds off something already nearly free.

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
