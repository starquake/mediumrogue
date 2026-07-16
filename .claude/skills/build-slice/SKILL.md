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

## Finish

- Last task is always docs: `FEATURES.md` (values from `internal/protocol` /
  `content.go`, never memory), `design-decisions.md` for decided direction,
  plus a stale-claim sweep (`grep -rn "<topic>" docs/`).
- `make e2e` if the client changed at all.
- `gh pr ready <n>`, watch CI, then STOP. **Do not merge** — the
  `ready to merge` label is the maintainer's; the `merge-pr` skill handles
  the landing when they add it.
