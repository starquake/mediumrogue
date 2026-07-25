# Audio assets (#298)

**Source:** Kenney's *RPG Audio* pack — <https://kenney.nl/assets/rpg-audio>
**Licence:** CC0 1.0 Universal (public domain). `License.txt` beside these files
is the pack's own, shipped verbatim. No attribution is required; the pack asks
only that credit "would be nice".

## Why only 18 of the pack's 51 files

The client is embedded into the Go binary (`//go:embed all:dist`), so every
file here inflates both the binary and a player's first download. The whole
pack is ~711 KB against a 676 KB client bundle — it would have more than
doubled it. This is a curated subset (~225 KB), one sound per game event plus
variants only where repetition would grate.

**`Preview.ogg` is deliberately absent**: it is a 313 KB demo reel, 44% of the
pack's weight, and plays every sound in sequence. It is not a game asset.

## What each file is for

| files | event |
|---|---|
| `knifeSlice.ogg`, `knifeSlice2.ogg` | a melee hit lands |
| `chop.ogg` | a **crit** — heavier than a normal hit |
| `cloth1.ogg`, `cloth2.ogg` | a **glance** — soft, the hit half-deflected |
| `footstep00..04.ogg` | the viewer's own move (five, so walking is not a metronome) |
| `handleCoins.ogg`, `handleSmallLeather.ogg` | picking an item up |
| `dropLeather.ogg` | dropping an item |
| `clothBelt.ogg`, `drawKnife1.ogg`, `metalClick.ogg` | equipping / unequipping |
| `bookOpen.ogg`, `bookClose.ogg` | a panel opens / closes |

## Known gaps

RPG Audio has **no death sting, no level-up fanfare, no bow/ranged, no magic
and no UI beeps** (maintainer's call on #298: RPG Audio only). Those events are
silent rather than being given a wrong sound. Filling them means adding
Kenney's *UI Audio* / *Impact Sounds* (also CC0) — a content change, not a
redesign.

## Reproducing this set

Download the pack from the URL above and copy exactly the files listed in the
table. The pack is versioned by URL hash, so a future re-download may differ;
these files are committed rather than fetched at build time so a build is never
at the mercy of a third-party host.
