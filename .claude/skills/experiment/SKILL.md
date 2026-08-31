---
name: experiment
description: >
  Use when something must be TRIED before it can be specified — "let's see if X
  works", "build it so we can deploy to dev", "I want to walk it first". Runs
  the experiment lifecycle: state the success criterion BEFORE building, ship to
  development via deploy:dev, then graduate it or kill it and record why.
  Trigger even if the maintainer doesn't say "skill".
---

You run an experiment: code built to answer a question, not to ship.

The distinction that matters is not code quality. It is that a normal slice
starts from a decision and an experiment starts from a question. Everything
below follows from that.

## The rule this exists for

**An experiment without a stated success criterion cannot succeed.** It can only
produce a result that gets rationalised afterwards, in whichever direction the
person looking at it already leaned.

So the criterion is written down **before the build**, on the ticket, and it
says what would make the answer NO. If nothing could, this is not an experiment
— it is a slice with a hedge in front of it, and `design-slice` owns it.

(2026-08-31, #458: a graph-first worldgen mockup produced convincing pictures
and a detour factor of ×1.00–×1.16 against the existing world's ×1.04 — no
improvement at all on the property the whole idea was for. Without a number
stated in advance, the pictures would have carried it.)

## What the criterion may be

Not necessarily a metric. **"I walked it and it feels right" is a legitimate
criterion**, and often the honest one — but it has to be named in advance as
the thing that decides, so the measurement is not quietly promoted over the
judgement when they disagree.

Where both exist, say which wins. In this repo the maintainer's verdict from
playing usually should: measurements have twice pointed the wrong way (#458's
mean-degree metric did not measure branching at all; a "no monsters near the
origin" test passed while joining still started a fight, #460).

## Running it

1. **State the criterion on the design ticket.** It stays in a work state; the
   experiment does not close it.
2. **Branch `exp/<issue>-<slug>`.** Say in the PR body, in the first line, that
   it is an experiment and what question it answers.
3. **Ship it to development** — the `deploy:dev` label, per `release-promote`.
   Development is a throwaway sandbox with its own volume and world, and its
   deploy job is **label-only with no CI dependency**, so a red branch still
   deploys. Staging keeps running `main`, which makes it the control.
4. **Keep the branch honest anyway.** Lint clean, and the code covered by its
   own tests. An experiment that is embarrassing to read cannot be graduated,
   only rewritten — and rewriting loses the commit history that records why each
   piece is the way it is. Red tests are acceptable *when they are the old
   assumptions being violated*, and each one must be named in the PR body.
5. **Fix defects that would corrupt the verdict, before asking for one.** A
   world with 3× too much mud and no water answers a question about mud and
   water, not about the thing under test. Measure the properties the experiment
   is NOT about, and fix what has drifted.

## Graduating a success

**Clean the experiment branch in place; do not rebuild it clean.** The commit
history is the record of what was learned — which bugs were hit, which
assumptions failed, which numbers were measured. A fresh branch looks tidier and
is worth less.

The graduation checklist is the difference between "answers the question" and
"is the product":

- [ ] **Remove any scaffolding that made the suite lie.** A test-only escape
      hatch that points the existing tests at the OLD behaviour means shipping
      something the suite never exercises. Deleting it is what surfaces the real
      work, so delete it first.
- [ ] **Adapt the tests it breaks**, one category at a time, and say which
      category each failure is in: an assumption that is now genuinely wrong, a
      pinned value to **re-derive** (never weaken), or a defect the experiment
      exposed.
- [ ] **Delete the thing it replaces.** Two implementations behind a flag is a
      permanent question about which one is real.
- [ ] Docs: `FEATURES.md`, and `design-decisions.md` for the direction and the
      measurements that justified it.
- [ ] `snapshotVersion` if world state or its shape changed.
- [ ] **File a build ticket for the graduation and close THAT**, not the design
      ticket. One issue is one deliverable; the design ticket stays the record.

Then the PR goes from draft to ready and merges like any other — the
`ready to merge` label is still the maintainer's.

## Killing a failure — this is the half that gets skipped

A failed experiment that is only deleted will be proposed again, and someone
will redo the same measurements to reach the same answer.

- [ ] **Write the finding into `docs/design-decisions.md`**: what was tried,
      what was measured, and why it was rejected. The numbers matter more than
      the prose — they are what stops the re-proposal.
- [ ] Close the design ticket with that reasoning, or return it to `Your input`
      if a different approach is still open.
- [ ] Delete the branch, and remove the `deploy:dev` label so development stops
      running it.

This mirrors what the repo already does elsewhere: rejected linters stay
commented in `.golangci.yml` carrying their reason, and rejected mockup options
stay in the design ticket. A rejection with its reasoning is cheap to keep and
expensive to rediscover.

## What an experiment is NOT

- **Not a way around the design gate.** If the question is "what should this
  be?", that is `design-slice`. An experiment answers "does this work?", which
  needs a candidate answer to exist first.
- **Not a place to skip the gate on merging.** A graduated experiment is an
  ordinary PR needing `ready to merge`.
- **Not permanent.** An `exp/*` branch that has been deployed to development for
  weeks without a verdict is a decision nobody has made. Ask for one.
