---
name: build-slice
description: >
  Use whenever the user gives the go-ahead to build an approved design slice —
  "let's build #NN", "go ahead and implement it", "execute the plan",
  "continue/resume building #NN" (mid-slice pickup), or "let's work on #NN"
  when #NN already has a maintainer-approved plan in the issue. Executes the
  plan task-by-task on ONE implementation PR: branch, failing tests first,
  full make check green per commit, CI watched after every push, docs in the
  same PR, ready at the end — merge stays label-gated. Trigger for any
  build-the-approved-slice request even if the user doesn't say "skill".
---

You execute an approved plan from a design issue, task by task, on one
implementation PR. Precondition: the issue's Plan section exists and the
maintainer has OK'd it. If either is missing, stop and route to the
`design-slice` skill instead — never invent a plan mid-build.

## Setup

- Branch from up-to-date `main` (`git checkout main && git pull --ff-only`),
  named for the slice. Open the PR early (draft) referencing the issue —
  `Closes #NN` if the slice completes it.
- **Label**: the maintainer's OK just moved the issue into your court — set
  `needs: build` on it (removing `needs: your sign-off`:
  `gh issue edit <n> --add-label "needs: build" --remove-label
  "needs: your sign-off"`), so it reads as in-progress, not awaiting them.
- Re-verify the plan against the current tree before the first commit: named
  symbols still exist, no interim merge moved the ground. Surface drift;
  don't silently improvise.
- **Resuming mid-slice**: the issue's ticked checkboxes plus the branch's
  commits are the progress record — read both, confirm they agree, and pick
  up at the first unticked task. Never redo a ticked task from scratch.

## The task loop (repeat per plan task)

1. Check the task's box context: what it consumes and produces.
2. **Failing tests first** where the plan says so — run them, confirm they
   fail for the right reason (missing feature, not a typo).
3. Implement. Follow the domain patterns (CLAUDE.md): content as registry
   data + rule cards; determinism rules (sort map-derived slices before rng;
   re-derive moved seeded pins, never weaken; drop rows appended LAST);
   `got, want` test style (`.claude/rules/go-style.md`).
4. Full gate: `cd` to the repo root, then
   `set -o pipefail && make check 2>&1 | tail -15` (go may live at
   `/usr/local/go/bin/go`). A seeded failure in a task that shouldn't touch
   rng is a bug — investigate, don't re-derive.
5. One commit per task, message referencing the issue; push; tick the task's
   checkbox in the issue's Plan.
6. **Watch CI to completion** (`gh pr checks <n> --watch`) — local-green is
   not mergeable. A flake (e.g. #117's autowalk timeout) on an unrelated
   diff: confirm it's known, re-run the job, note the recurrence.

## The PR body: REVIEW is the bottleneck, not throughput

The maintainer reviews every PR alone (2026-07-19: *"1. reviewing"*). So a PR
body is a **guide to reviewing**, never a defence of the work. Long bodies make
review more expensive — the reader has to scan everything to find the parts
that actually need judgement.

Structure, always:

1. **`## Where to look`** — the **two or three** places you made a judgement
   call the maintainer might disagree with, each naming its file. A number you
   picked, a deliberate overlap, a reading that could plausibly be wrong. If
   there are none, say so in one line.
2. A `---`, then **`*Mechanically verified — skip unless curious.*`** and ONE
   compressed paragraph: gate green, determinism, e2e, docs. These are claims
   already verified by running them; prose restating them adds nothing a
   reviewer must read.

**Do not** narrate the design reasoning in the PR body. It belongs in
`design-decisions.md`, written once — a PR body copy is drift with extra steps
and dies with the PR.

**Code comments follow the same rule** (maintainer's call, same day): keep the
*why* — why this side of the fold, why this order is contractual, why a
counter-intuitive reading is correct — but **not** whole explanations of game
mechanics or design intent. "Category axis rejected because the taxonomy has
none" is a comment; three paragraphs on why weapon categories are an MMO move
is `design-decisions.md`.

**Keep mechanical churn in its own commit** (regenerated files, lint fixes,
doc regeneration) so the reviewer can skip that commit in the diff instead of
scanning past it.

## Finish

- Last task is always docs: `FEATURES.md` (values from `internal/protocol` /
  `content.go`, never memory), `design-decisions.md` for decided direction,
  plus a stale-claim sweep (`grep -rn "<topic>" docs/`).
- `make e2e` if the client changed at all.
- `gh pr ready <n>`, watch CI, then STOP. **Do not merge** — the
  `ready to merge` label is the maintainer's; the `merge-pr` skill handles
  the landing when they add it.
- **Label**: the build is done and the ball is now on the PR's
  `ready to merge` gate, so clear the issue's `needs: build`
  (`gh issue edit <n> --remove-label "needs: build"`) — don't move it to a
  `needs:*` maintainer label, because `ready to merge` on the PR is already
  that signal and setting both double-counts. On merge the `Closes #NN` PR
  auto-closes the issue.
