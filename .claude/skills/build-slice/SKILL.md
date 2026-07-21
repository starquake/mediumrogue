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

- Branch from up-to-date `main`, named for the slice. Open the PR early (draft)
  referencing the issue — `Closes #NN` if the slice completes it.
  **If you are running in an isolated worktree** (parallel/orchestrated build):
  `cd` to your worktree path and run every `git` command there — branch, commit,
  and push from inside it. Do **not** `git checkout` / `git checkout -b` in the
  shared repo root: that moves the shared checkout's HEAD onto your branch and
  can strand another agent's live work on it (2026-07-21: three agents ran
  `git checkout -b` against the shared root, hijacking it — one commit even
  landed on a sibling's branch). A worktree isolates the filesystem, not your
  `git` habits — point them at the worktree.
- **If you run `make e2e`, do not `pkill`/`fuser` the shared Playwright ports
  (8123+).** When builds run in parallel, another agent's e2e server lives on
  those ports; clearing the range kills its run (2026-07-21). If a port is busy,
  point your own webServer at an isolated range instead of freeing the shared
  one.
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

   **A test that can never run is worse than no test** — it reads as coverage
   on the dashboard while asserting nothing. After writing one, check it
   actually *ran*: a `test.skip` guard whose precondition the harness can never
   satisfy is the common shape. (2026-07-19: an e2e for "learning a skill
   updates the panel immediately" skipped every time, because the monster-free
   e2e server hands a fresh join zero skill points and has no grant hook. It
   was deleted and replaced with store unit tests, which is what
   `gear/store.test.ts` already exists to do.) When the state is unreachable
   end-to-end, drop to the layer that CAN reach it and say why in the file
   comment — don't keep the skipping test as a placeholder.
3. Implement. Follow the domain patterns (CLAUDE.md): content as registry
   data + rule cards; determinism rules (sort map-derived slices before rng;
   re-derive moved seeded pins, never weaken; drop rows appended LAST);
   `got, want` test style (`.claude/rules/go-style.md`).
4. Full gate: **check the EXIT CODE, never grep the output.**

   ```bash
   if set -o pipefail && make check > /tmp/check.log 2>&1; then
     echo "GATE PASS"
   else
     echo "GATE FAIL ($?)"; grep -E "^(---)? ?FAIL|Error" /tmp/check.log | head -5
   fi
   ```

   Grepping for failure patterns means inventing a regex that must match every
   way the toolchain can fail — and the one it misses reads as green.
   (2026-07-19: `make check 2>&1 | grep -iE "^FAIL|error:"` reported clean while
   four error sentinels were unmapped, which CI caught as a 500-instead-of-422
   bug. Switching to exit-code gating surfaced a SECOND failure in the same
   run — a snapshot fixture pinned to the old version — that the same regex had
   also been hiding.) A seeded failure in a task that shouldn't touch rng is a
   bug — investigate, don't re-derive.

   `go` may live at `/usr/local/go/bin/go`.
5. One commit per task, message referencing the issue; push; tick the task's
   checkbox in the issue's Plan.
6. **Watch CI to completion, and read EVERY job.** `gh pr checks <n> --watch`
   — never piped through `tail`/`head`. A pipe truncates the failing line out
   of view *and* swallows the command's non-zero exit, so a red build reads as
   green twice over (2026-07-19: `| tail -4` hid a failing `test` job; the
   maintainer found it). When the run is ambiguous, enumerate explicitly:

   ```bash
   gh api "repos/:owner/:repo/actions/runs?head_sha=$(git rev-parse HEAD)" \
     --jq '[.workflow_runs[]|select(.name=="CI")]|.[0].jobs_url' \
     | xargs -I{} gh api {} --jq '.jobs[] | "\(.name) \(.conclusion)"'
   ```

   Also confirm the run you are reading belongs to **the commit you just
   pushed** — `gh pr checks` will happily show the previous run's results for a
   few seconds after a push. Local-green is not mergeable. A flake (e.g. #117's autowalk timeout) on an unrelated
   diff: confirm it's known, re-run the job, note the recurrence.

   **Watch CI *synchronously, within this turn* — never background the watch and
   end.** If you are a subagent, arming a background watcher and stopping does
   NOT get you woken when it finishes: unlike the main session, a subagent is not
   re-invoked by its own background task, so the slice silently stalls with CI
   unread and the Next-steps comment unposted (2026-07-21: five build agents each
   said "the watcher will notify me" and ended mid-flight, every one needing a
   nudge to finish). Poll in a foreground loop until every job is terminal, then
   finish the slice in the same turn:

   ```bash
   while gh pr checks <n> --json bucket -q '.[].bucket' | grep -q pending; do sleep 30; done
   gh pr checks <n> --json name,bucket -q '.[] | "\(.bucket)\t\(.name)"'  # read every job
   ```

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

**The same rule governs COMMENTS, and it is easier to break there.** When the
maintainer asks a direct question — *"are you going to fix it?"*, *"can linting
prevent this?"* — the answer goes in the **first line**, and any reasoning goes
below it. (2026-07-19: a crash report got a reply that opened with "fixed in
#186" and then spent 400 words on TypeScript soundness. Nine minutes later:
*"Sooooo are you going to fix it?"* The answer was present and unfindable.) A
comment that has to be read to the end to learn whether the thing is done has
failed, however correct its content.

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
- **A NEW e2e test that drives movement, combat, or the turn clock gets a load
  run before you mark it ready** — `cd client && npx playwright test <spec>
  --repeat-each=6 --workers=9`. CI runs each test once, on a fast idle runner, so
  a timing/AI race hides there and only surfaces later — under load, or on a
  *different* PR's slower run — where it reads as someone else's flake. The de-race
  rule (CLAUDE.md) is how you fix a flake once it bites; this is how you keep the
  one you just wrote from biting at all (2026-07-21: #205's tooltip-clears test
  and an attack-highlight case passed their own CI, then failed a docs-only PR
  and stalled two others — both were unreachable-nearest-monster races a load run
  reproduces immediately). If it can't survive the load run, fix it now, not after.
- `gh pr ready <n>`, watch CI, then STOP. **Do not merge** — the
  `ready to merge` label is the maintainer's; the `merge-pr` skill handles
  the landing when they add it.
- **Label**: the build is done and the ball is now on the PR's
  `ready to merge` gate, so clear the issue's `needs: build`
  (`gh issue edit <n> --remove-label "needs: build"`) — don't move it to a
  `needs:*` maintainer label, because `ready to merge` on the PR is already
  that signal and setting both double-counts. On merge the `Closes #NN` PR
  auto-closes the issue.
