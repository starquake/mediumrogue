---
name: Design slice (spec + plan)
about: A new mechanic or feature — designed in this issue before it is built
labels: enhancement
---

<!-- The workflow (CLAUDE.md, "How work lands"): fill the SPEC and settle its
     decisions in this issue's comments; then fill the PLAN; the maintainer's
     OK on the completed plan is the go-ahead to build. The implementation PR
     references this issue. Shipped decisions graduate to
     docs/design-decisions.md and docs/FEATURES.md in the implementation PR —
     this issue is the design record until then, and its history afterward. -->

## Spec

### Goal

<!-- One paragraph: what ships, and the one-line reason it exists. -->

### Decisions

<!-- Numbered, settled with the maintainer, each with its why. Check every
     combat-adjacent idea against docs/game-identity.md — ARPG (decoupled
     percentage stat-checks), never TTRPG (coupled rolls, to-hit, saves). -->

### Design

<!-- Server / client / content / wire, at whatever depth the slice needs.
     Name the real symbols (files, functions, consts) — the plan builds on
     them. Content is registry data + rule cards, never code at a combat
     site. -->

### Mockup (visual/looks-driven slices only)

<!-- Anything whose value is how it LOOKS gets a mockup approved here
     before the real UI is built: build an HTML mockup, screenshot it,
     commit the image under docs/mockups/ (dated filename, on the work
     branch), and LINK the blob page in this section:
     [mockup](https://github.com/starquake/mediumrogue/blob/<branch>/docs/mockups/<file>.png)
     (A plain link, not an inline ![image] embed: the repo is private, so
     GitHub's anonymous image proxy cannot render raw URLs inline — the
     blob page renders fine for logged-in collaborators.) The maintainer's
     approval of the screenshot is part of the spec OK. -->

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
