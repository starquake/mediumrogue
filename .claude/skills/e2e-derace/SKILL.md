---
name: e2e-derace
description: >
  Use whenever an e2e test flakes, a CI e2e job goes red on a
  monster/timing/combat spec, a Playwright `locator.click` or `expect.poll`
  times out, or a spec passes locally but fails in CI ("local-green /
  CI-red"). Encodes this repo's flaky-e2e playbook: reproduce at the CAUSE
  (never a re-run, never a raised timeout), the `<Index>`-not-`<For>` remount
  trap, polling `window.game` before DOM asserts, robust monster engagement
  via the shared `chaseIntoCombat`/`progressTracker` helpers, and the
  shared-world single-instance caveat that makes `--repeat-each` the WRONG
  repro tool for some specs. Trigger on any flaky-e2e request even if the
  user doesn't say "skill".
---

You are closing an e2e flake at its cause. A re-run **unblocks a PR; it never
ends a flake**, and raising a timeout is not a fix — it is the flake with a
longer fuse (CLAUDE.md). A test change lands or the flake is not closed. If
you genuinely cannot reproduce, **say so plainly** — an unverified theory is a
lead, not a diagnosis.

## Distrust the ticket

The stated hypothesis is where to start looking, not what to conclude. #117
was filed as "test-fixture death" and was really the test's own un-awaited
straggler tap. So: **reproduce first, then root-cause, then fix, then prove
with the same command.** Reproduce before you believe any theory, including
your own.

A flake can be a **product bug wearing a test-flake costume** — file it
separately, don't bury it in a test tweak. #130: stale attack targets returned
500 instead of 422; that surfaced *through* an e2e but the fix was server-side.
If the "flake" is the product misbehaving, the deliverable is a product PR and
a ticket, not a `test.slow()`.

## Reproduce — and pick the RIGHT repro tool (this is the subtle part)

There are two worlds an e2e spec can run against, and they take different
repro commands. **Get this wrong and you burn hours chasing artifacts CI never
sees** (learned #259/#260).

- **Fresh-world specs** (each project gets its own server on its own port):
  reproduce with `--repeat-each=6 --workers=9`. Parallel + repeated is exactly
  the pressure that shakes out races.
- **Shared-world specs** (`ranged`, `monsters`, anything that engages the
  persistent world): that world **does not respawn monsters**
  (`internal/game/quest.go` — "monsters don't respawn"), **lingers closed
  players** for `DISCONNECT_GRACE`, and **caps at `protocol.MaxPlayers` (30)**,
  refusing the overflow with a **503 `ErrWorldAtCapacity`**. Pile
  `--repeat-each` onto these and you manufacture **accumulation artifacts** —
  503 "world at capacity" cascades, monster drift/depletion, bubble-lock —
  that **CI's single-instance-per-test run NEVER hits**. Here `--repeat-each`
  is the WRONG tool: run **many INDEPENDENT invocations** of the spec instead
  (loop `make e2e` / the single-project run), each a clean world, and count
  clean rounds.

The one-line test for which world you're in: does the spec's Playwright
project spin up its own server, or lean on the shared one? Ports are
`BASE_PORT = 8123` + project index (`client/playwright.config.ts`,
`portFor`) — a distinct port per project.

For Go-side races: `go test -run TestX -count=N -race`.

## The four traps that are almost always the cause

### 1. `<Index>`, never `<For>`, for any list from a turn bundle

The client rebuilds its whole state from a full snapshot **every turn**,
minting fresh object references. `<For>` keys by reference → it remounts every
row every bundle → an in-flight click lands on a DOM node that just got
replaced → `locator.click` times out. `<Index>` keys by position → stable DOM.
This is the **default trap** and it has recurred several times: inventory,
backpack, quest rows, modal rows — any per-turn list. If a click times out on
a list that updates each turn, check this before anything else.

### 2. Poll `window.game` state before you assert DOM — never `sleep`

`window.game` is the test-and-debug surface (every stateful client feature
adds a synced field). Gate every DOM assertion on a **game-state** condition,
not wall-clock:

- Poll `window.game.turn` incrementing, or a `window.game` flag flipping
  (`inCombat`, a panel-open field), before asserting the DOM it drives.
- **Open a default-closed panel and CONFIRM it's open** before clicking its
  contents — a click into a not-yet-open panel is a guaranteed timeout.
- Budget on **game progress (turn advances), not the clock.** A turn-metered
  journey uses `test.slow()` for headroom; it does not sprinkle `sleep`.

A `sleep` is a bet that the machine is fast enough today. CI is slower and
headless, so **local-green / CI-red is normal** — it means the bet lost, not
that the code changed.

### 3. Robust monster engagement — the big one

The naive "find the NEAREST monster, step greedily toward it" pattern
deadlocks in four distinct ways, each a real captured failure:

- **Unreachable target.** Spawn placement checks *walkability*, not
  *connectivity* — a monster can sit in a terrain pocket where `Pathfind`
  returns nil. Nearest ≠ reachable, forever.
- **Local-minimum oscillation.** Greedy stepping ping-pongs on an
  equal-distance tie.
- **The leash treadmill.** A monster returning to its leash home walks away at
  the same 1-hex/turn speed you chase — you move every turn and never close.
  A *position*-based stuck check can't see this: the player is walking
  healthily the whole time (#181: 20s of motion, gap pinned at 10).
- **In-range but UNSEEN.** After ranged attacks became LOS-gated, a target can
  be inside bow range yet behind terrain — submit-time refuses it.

**Fix: reuse the shared helpers, don't re-roll engagement.**
`client/e2e/helpers.ts` exports **`chaseIntoCombat(page)`** (drives the player
until the client reports a combat bubble) and **`progressTracker(window)`**
(rotates off a target whose *best distance ever achieved* hasn't improved
within `window` polls — distance-stall, not position-stall, which is what
catches the treadmill). Together they rotate to the next-nearest monster you
can both **reach AND see**. Born of #181/#247; the whole family — #181, #234,
#205, #259 — was this. `monsters.spec.ts` and `attack-highlight.spec.ts` are
the worked callers. If you're writing a fresh chase loop, you're reinventing
these — import them instead.

Other helpers you'll want rather than re-derive: `gotoReady`, `seedIdentity`,
`hexDist`, `pickDistance2Destination`, `dumpState` (all `helpers.ts`).

### 4. Un-awaited stragglers

A tap/action fired without `await` outlives the assertion and lands in the
next turn's world — the #117 shape. Await every action a spec issues before it
asserts.

## Parallel-agent hazards

If another agent may be running e2e concurrently (cross-ref `build-slice`):
Playwright servers bind fixed ports from `8123` up. **Isolate your port range;
never `pkill` the shared server** — you'll kill a sibling's run. A stray green
that turns red on the next run is often a port collision, not a code race.

## Prove it, then leave evidence

- Re-run the **appropriate** repro (independent invocations for shared-world
  specs; `--repeat-each=6 --workers=9` for fresh-world) clean across multiple
  rounds. **State the numbers** ("18/18 independent runs green", "6/6 under
  repeat-each=6"). A fix without a repro count is a hope.
- `set -o pipefail && make e2e` exits **0** — gate on the exit code, never on
  grepped output.
- **Log recurrences on the flake's own issue** with the run link, so the next
  person inherits evidence instead of folklore. A recurrence on someone else's
  flake is a comment on their ticket, not a silent re-run.

## Later candidates (light cross-refs)

Engagement helpers and de-race discipline also show up in `build-slice`
(CI-after-push) and `add-content` (seeded determinism). This skill owns the
e2e-specific reproduction and the shared-world caveat; those own their own
gates.
