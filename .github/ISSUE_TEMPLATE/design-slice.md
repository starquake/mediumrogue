---
name: Design slice (spec + plan)
about: A new mechanic or feature — designed in this issue before it is built
labels: enhancement
---

<!-- The workflow (CLAUDE.md, "How work lands"): fill the SPEC, settle its
     decisions with the maintainer, then fill the PLAN; the maintainer's OK on
     the completed plan is the go-ahead to build. The implementation PR
     references this issue. Shipped decisions graduate to
     docs/design-decisions.md and docs/FEATURES.md in the implementation PR —
     this issue is the design record until then, and its history afterward.

     THIS BODY IS THE LIVING SPEC. THE COMMENTS ARE THE HISTORY.

     They are edited by opposite rules, and mixing them up is a real failure
     mode (#441): a finalised spec was posted as a comment while the body went
     on listing seven questions it had just answered, so anyone opening the
     ticket read the wrong document.

       - BODY: always current. Rewrite it freely. When a question is answered
         it MOVES INTO Decisions and is DELETED from Open questions — the body
         must never keep asking something that has been settled.
       - COMMENTS: append-only. Never edit one in place; a new `> 🤖 **Next
         steps**` comment each time the state changes, so the thread reads in
         order. Answers arriving in chat get written back into the BODY, not
         just acknowledged in a comment.

     Filing from the CLI? `gh issue create --body/--body-file` BYPASSES
     templates entirely — they are a web-UI and interactive affordance. So
     read this file and fill in its sections by hand, then pass --body-file.
     Nothing errors when the structure goes missing; #441 is what that looks
     like. -->

## Spec

### Goal

<!-- One paragraph: what ships, and the one-line reason it exists. -->

### Decisions

<!-- Numbered, settled with the maintainer, each with its why. Check every
     combat-adjacent idea against docs/game-identity.md — ARPG (decoupled
     percentage stat-checks), never TTRPG (coupled rolls, to-hit, saves).
     And no mechanic wildfire: never a new mechanic (pipeline kind, stat,
     subsystem) for a single item — a mechanic earns its place by serving
     multiple items, else use the existing card vocabulary. -->

### Open questions

<!-- Anything still FOR the maintainer. State the options and, where there is
     a defensible default, which one you'd take and why — recommending is not
     deciding. End the section with a fenced block they can copy, fill in and
     paste back as a comment, one line per question:

     ```
     Q1 <topic>: [ option a / option b / option c ]
     Q2 <topic>: [ option a / option b ]
     ```

     Mirror the same block in the `> 🤖 **Next steps**` comment so it can be
     answered from the thread.

     DELETE each question from this section the moment it is answered, moving
     it into Decisions above with its answer. Delete the whole section once
     everything is settled. A body that still asks a settled question is the
     #441 bug. -->

### Design

<!-- Server / client / content / wire, at whatever depth the slice needs.
     Name the real symbols (files, functions, consts) — the plan builds on
     them. Content is registry data + rule cards, never code at a combat
     site. -->

### Mockup (visual/looks-driven slices only)

<!-- Anything whose value is how it LOOKS gets a mockup approved here
     before the real UI is built: build an HTML mockup, screenshot it,
     commit the image under docs/mockups/ (dated filename, on the work
     branch), and embed it inline via the github.com /raw/ route:
     ![mockup](https://github.com/starquake/mediumrogue/raw/<branch>/docs/mockups/<file>.png)

     Exactly this URL form. It was originally required because the repo was
     private (only github.com routes carry the viewer's session; verified
     2026-07-16 on PR #120). The repo went PUBLIC on 2026-07-28 so both forms
     now resolve — the rule stays anyway, because one documented form means no
     per-embed decision, and churning these URLs is how the embeds broke last
     time.

     THE EMBED MUST BE REPOINTED TO /raw/main/ BY THE PR THAT MERGES IT (#393).
     A /raw/<branch>/ embed dies when the branch is deleted on merge —
     retroactively, in a comment nobody is looking at. Not a commit SHA
     either: PRs are squash-merged, so the original commit is not in main's
     history. Every feat/* embed tested was a 404.

     The maintainer's approval of the screenshot is part of the spec OK. -->

### Determinism & seeded tests

<!-- Does anything consume rng, or reorder its consumption? Which pinned
     seeds or weighted tables can move? Moved pins are re-derived, never
     weakened (drop rows: append LAST). -->

### Out of scope

<!-- Deferred pieces, each with the issue that tracks it. -->

## Plan

<!-- Tasks in landing order; each ends green (`set -o pipefail && make check`)
     and is one commit on the implementation PR. Failing tests first where
     practical. Keep the seeded-surface task (drop tables, rng changes)
     isolated so pin movement has one cause. -->

- [ ] Task 1 —
- [ ] Task 2 —
- [ ] Docs: `FEATURES.md` (+ `design-decisions.md` if a direction was decided)
      updated in the same PR
