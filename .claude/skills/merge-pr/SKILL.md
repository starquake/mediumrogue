---
name: merge-pr
description: >
  Use whenever the user wants to merge/land a pull request — "merge #82",
  "land this PR", "check the label and merge", "merge the ready PRs", "squash
  and merge", "can you merge that". Runs this repo's merge workflow: verify the
  `ready to merge` label (the review-approval gate — NEVER merge without it),
  confirm CI is green, rebase onto main if the branch is behind, then
  squash-merge — EXCEPT dependabot PRs, which are merged by posting a
  `@dependabot squash and merge` comment (dependabot does the merge itself, not
  `gh pr merge`). Trigger this for any merge/land request even if the user
  doesn't say "skill".
---

You land pull requests following this repo's rules. The one that matters most:
**a PR is only mergeable when it carries the `ready to merge` label.** That
label *is* the review approval — GitHub won't let the author approve their own
PR, so the maintainer adds the label to signal "go." Without it you stop and
surface, never merge. Everything else below is about doing the merge cleanly
once that gate is met.

## Step 1 — The label gate (check immediately before merging)

```bash
gh pr view <n> --json labels,mergeable,mergeStateStatus,isCrossRepository \
  --jq '{labels: [.labels[].name], mergeable, state: .mergeStateStatus}'
```

- **No `ready to merge` label → STOP.** Tell the user it's missing and that
  adding it is the maintainer's signal; don't merge. (You can't add it
  yourself — that would defeat its purpose.)
- Re-check the label *right before* the merge command. A force-push (from a
  rebase, below) can drop it, so a label you saw earlier may be gone.

## Step 2 — CI must be green

```bash
gh pr checks <n>
```

Every required check must be **pass** — no `fail`, `pending`, or `in_progress`
on a blocking check. `deploy-*` jobs that show `skipping` are fine (they don't
run on PRs). If CI is still running, wait or tell the user; never merge red or
pending.

## Step 3 — Rebase if the branch is behind main

If `mergeStateStatus` is `BEHIND`/`DIRTY` or `mergeable` is `CONFLICTING`, the
branch needs to be brought up to date. **How depends on who owns the branch:**

- **A normal (human/agent) PR** — rebase locally:
  ```bash
  git fetch origin --quiet
  git checkout <branch> && git rebase origin/main
  # resolve any conflicts, then:
  make check                    # confirm the combined result is green
  git push --force-with-lease
  ```
  A force-push **re-triggers CI and can drop the `ready to merge` label** — so
  after it, go back to Step 1 (re-verify the label) and Step 2 (wait for CI)
  before merging.

- **A dependabot PR** — do NOT rebase it by hand. Post a comment and let
  dependabot rebase + re-run CI:
  ```bash
  gh pr comment <n> --body "@dependabot rebase"
  ```
  Then wait for it to update and CI to go green.

## Step 4 — Merge

Detect dependabot: the PR author is `dependabot[bot]`, or the head branch starts
with `dependabot/`.

```bash
gh pr view <n> --json author,headRefName --jq '{author: .author.login, head: .headRefName}'
```

- **Normal PR** — squash-merge and delete the branch (matches this repo's
  squash-merge history):
  ```bash
  gh pr merge <n> --squash --delete-branch
  ```

- **Dependabot PR** — dependabot merges itself via a comment. **Do not use
  `gh pr merge`** on it:
  ```bash
  gh pr comment <n> --body "@dependabot squash and merge"
  ```
  Dependabot merges when CI is green, then closes the PR and deletes its branch.

After a normal merge, update local main so follow-on work branches from the
latest:
```bash
git checkout main && git fetch origin --quiet && git pull --ff-only origin main
```

## Merging several at once ("check the labels and merge")

Check each PR's label + CI, then merge the ready ones **one at a time** —
merging one advances `main`, so re-check the next before merging it. Watch for
**merge-train conflicts**: two PRs that touch the same file (classically two
dependabot bumps both editing `package-lock.json`, or two feature branches
editing the same source) can't both stay clean — after the first merges, the
second needs a rebase (Step 3) before it will merge. Surface that rather than
force it.

## Guardrails

- **Never merge without `ready to merge`.** If the user says "merge it" but the
  label is absent, surface it and let them add it (or explicitly override).
- **Never merge failing or pending CI.**
- **Dependabot merges via `@dependabot squash and merge`, not `gh pr merge`** —
  mixing the two fights dependabot's own automation.
- Prefer `--force-with-lease` over `--force` when pushing a rebase.
- Merges are outward-facing and hard to undo — when the label/CI picture is
  ambiguous, ask rather than guess.
