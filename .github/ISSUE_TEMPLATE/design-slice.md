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
     percentage stat-checks), never TTRPG (coupled rolls, to-hit, saves).
     And no mechanic wildfire: never a new mechanic (pipeline kind, stat,
     subsystem) for a single item — a mechanic earns its place by serving
     multiple items, else use the existing card vocabulary. -->

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
     (Exactly this URL form — the repo is private, and only github.com
     routes carry the viewer's session; a raw.githubusercontent.com URL
     renders as a broken icon. Verified 2026-07-16 on PR #120.) The
     maintainer's approval of the screenshot is part of the spec OK. -->

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
