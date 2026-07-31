---
name: board
description: >
  Short alias for the board loop. Use when the maintainer types "/board",
  "board", "board loop", or "run the board" — it starts `work-the-board`'s
  LOOP form (a triage-and-advance pass on a schedule, with the persistent
  monitor armed), so they never have to spell out "work the board every 60m".
  "/board once" runs a single pass instead; "/board stop" (or "stop the
  board") ends it. Trigger on the bare word even without "skill".
---

This is an **alias**, not a second workflow. Everything about how a pass
behaves — the gates it stops at, the one-build cap, `hold`, the Next-steps
comment — lives in `work-the-board`. Read that skill and follow it; this file
only decides *which form* to run and *with what defaults*, so the maintainer
can type one word instead of a sentence.

## What each form means

| They type | You run |
|---|---|
| `/board`, `board`, `run the board` | **Loop form, 60-minute interval, monitor armed** |
| `/board 30m`, `board every 30m` | Loop form at that interval, monitor armed |
| `/board once`, `work the board` | **One pass**, no monitor — it would outlive nothing |
| `/board stop`, `stop the board` | End the loop **and** `TaskStop` the monitor |

**60 minutes is the default on purpose.** The monitor is what makes the loop
feel instant — a maintainer comment or a new `ready to merge` wakes it in about
a minute — so the timed tick is only a backstop for things no event announces
(CI that never reported, a stalled deploy). Ticking more often just spends
tokens to discover nothing changed.

## Starting a loop session

1. **Arm the monitor first**, persistent, exactly as `work-the-board`'s
   "Prefer a Monitor over polling" section specifies — **all three** signals:
   comments, `ready to merge`, *and* board **Status** moves, each diffed
   against the previous cycle. Do not hand-roll a trimmed version. A
   label-only watch silently drops maintainer answers (#199, 2026-07-21), and
   a watch without the Status poll misses a card moved to `Build` — which is
   an authorisation exactly as a `go` comment is (#313, 2026-07-31). Use the
   targeted GraphQL query from that section, **not** `board.sh list`: the
   latter costs 102 points a call and exceeds the GraphQL budget at a 60s
   interval.
2. **Check it is not already armed** — `TaskList` first, and skip if a monitor
   for this is running. Two monitors mean two notifications per event.
3. **Run the pass**, then schedule the next tick.

## Stopping

`/board stop` must do **both**: end the schedule and `TaskStop` the monitor.
Stopping the loop alone leaves a persistent watch running for the rest of the
session, still emitting events with nothing driving them.
