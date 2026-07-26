# Audio assets (#298)

**Sources:** five of Kenney's CC0 packs. RPG Audio is the bulk; the other four
contribute one or two files each, only for events RPG Audio has no sound for
(see below).

| Pack | Used for |
|---|---|
| [RPG Audio](https://kenney.nl/assets/rpg-audio) | combat, movement, inventory, panels |
| [Impact Sounds](https://kenney.nl/assets/impact-sounds) | death |
| [UI Audio](https://kenney.nl/assets/ui-audio) | UI clicks |
| [Sci-Fi Sounds](https://kenney.nl/assets/sci-fi-sounds) | the thrown flask's burst |
| [Music Jingles](https://kenney.nl/assets/music-jingles) | the level-up fanfare |

**Licence:** CC0 1.0 Universal (public domain) for all of them. Each pack's own
licence file ships verbatim beside these files (`License.txt` is RPG Audio's;
the rest are `License-<pack>.txt`). No attribution is required; the packs ask
only that credit "would be nice".

## Why only 27 files out of five whole packs

The client is embedded into the Go binary (`//go:embed all:dist`), so every
file here inflates both the binary and a player's first download. RPG Audio
alone is ~711 KB against a 676 KB client bundle — shipping it whole would have
more than doubled it, and the five packs together are several megabytes.

This is a curated subset (~400 KB): one sound per game event, plus variants
only where repetition would grate. It grew from ~225 KB when the five silent
events were filled in — six new files, four of them one-offs from a
supplementary pack.

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
| `cloth3.ogg`, `cloth4.ogg` | a **ranged** hit — a bowstring/whoosh (see below) |
| `impactSoft_heavy_000/001.ogg` | a **death** — the killing blow (`HitView.Fatal`) |
| `explosionCrunch_000.ogg` | a thrown **flask** bursting |
| `jingles_STEEL00.ogg` | **level-up** |
| `click1.ogg`, `click2.ogg` | a HUD button |

## The gaps, and how they were filled

The first cut shipped RPG Audio only (maintainer's call on #298), which left
five events silent: death, level-up, bow/ranged, magic, and UI beeps. All five
now have a sound. Three of the picks are honest approximations rather than the
real thing, and it is worth knowing which:

- **Ranged** uses RPG Audio's `cloth3`/`cloth4` — a cloth whoosh. **There is no
  bowstring in any of these packs.** This is the closest sound available, and
  it stays inside a pack already shipped rather than adding a fifth for one
  file.
- **Magic** is the thrown flask's burst, `explosionCrunch_000` from the *Sci-Fi*
  pack. An explosion is generic enough to read as alchemy; the rest of that
  pack (lasers, engines) is not, and none of it is used. `thrusterFire` was the
  obvious fire candidate and was rejected: 5.0s long and 196 KB, longer than a
  whole turn.
- **Level-up** is `jingles_STEEL00`, chosen for its length (0.93s) and family.
  **These jingles are numbered, not named**, so the pick could not be made from
  the filename.

The remaining silences are deliberate: there is still no sound for drinking,
learning a skill, completing a quest, or a rejected intent.

## What is NOT here

`Preview.ogg` from each pack: they are demo reels that play every sound in
sequence — RPG Audio's alone is 313 KB. They are not game assets.

Unused files from the three supplementary packs are not committed either; only
the handful named in the table above.

## Reproducing this set

Download the pack from the URL above and copy exactly the files listed in the
table. The pack is versioned by URL hash, so a future re-download may differ;
these files are committed rather than fetched at build time so a build is never
at the mercy of a third-party host.
