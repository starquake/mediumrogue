# Milestone 7 — Procedural Generation: Design

*Status: DRAFT (2026-07-09). The final milestone-6-era world task: replace the
static hand-shaped map with a seeded procedural generator. Branch off `main`.
Builds on the static-map foundation (milestone 2) and the terrain/walkability
rules already in use by combat, pathfinding, and spawning.*

## Goal

Replace `StaticMap()` — a hand-shaped radius-12 hexagon with a rock rim, one
hand-placed lake, and per-tile hash-scattered forest — with a **seeded
procedural generator** that produces a **larger world of coherent biomes**
(grass plains, forest clumps, water bodies, rock mountains). Same terrain set,
same wire shape; the world is generated once at boot from a configurable seed.

Decided forks (user, 2026-07-09):
1. **Bigger fixed-radius map** (one `MapResponse` sent whole, as today) — not a
   chunked/streamed world.
2. **`WORLD_SEED` with a stable default** — every restart regenerates the SAME
   world (so planned beds/home spawn stay put, and tests are reproducible) —
   not random-per-boot.
3. **Noise-based coherent biomes** with a guaranteed-walkable spawn region — not
   the current incoherent per-tile hash.

## The generator

New file `internal/game/worldgen.go` (replacing `worldmap.go`'s `StaticMap`):

```go
// GenerateMap builds a deterministic procedural world of the given hex radius
// from seed. Same (seed, radius) → byte-identical map on every call.
func GenerateMap(seed uint64, radius int) protocol.MapResponse
```

**Two deterministic scalar fields**, sampled per hex:
- **elevation** — shapes water (low) → land → mountains (high).
- **moisture** — within land, separates **forest** (moist) from **grass** (dry).

Both come from a compact, self-contained **value-noise** function (no external
deps): a seeded integer hash over an integer lattice (reusing the existing
murmur-style mixing in `hexHash`, now seeded by `WORLD_SEED`), with smoothstep
interpolation between lattice points, summed over **2 octaves** for organic
shape. Coordinates are the hex's axial `(q, r)` scaled down so a "feature"
spans several hexes (coherent regions, not per-tile noise).

**Terrain assignment** (thresholds are tunable constants):
- Edge rim (`HexDistance(origin,h) == radius`) → `rock` (unchanged; keeps the
  world bounded and impassable at the edge).
- elevation below `waterLevel` → `water`.
- elevation above `mountainLevel` → `rock`.
- otherwise: moisture above `forestLevel` → `forest`, else `grass`.

**Spawn guarantee** (so no player is ever stranded on an island or in water):
1. Force a small **grass clearing** at the origin (radius ~2) — the spawn area
   is always walkable regardless of the noise there.
2. Compute the **connected walkable component containing the origin**
   (flood-fill over `grass`/`forest` via the existing hex neighbours). The
   spawner (today's origin-spiral in `world.go`) already starts at origin and
   picks nearby walkable tiles; restrict its candidates to this component so a
   spawn can never land across water from everyone else. A `spawnableLocked`
   helper (or a precomputed reachable set on the world) enforces it.

Determinism: `GenerateMap` uses only the seed and the lattice hash — no
wall-clock, no `math/rand` global. Determinism discipline matches the rest of
the codebase (seeded, reproducible).

## Config & wiring

- `internal/config`: `WORLD_SEED` (uint64, default a fixed constant e.g.
  `0xC0FFEE`) and `WORLD_RADIUS` (int, default **24** ≈ 1,801 tiles), parsed
  like the existing knobs (`TURN_INTERVAL`, `DISCONNECT_GRACE`) with env
  overrides. `cmd/rogue/app` threads them into world construction.
- `internal/game`: the `MapRadius` const becomes a world field (`w.radius`);
  the world holds its generated `protocol.MapResponse` (built once in
  `NewWorld`) and serves it from `Map()`. Combat/pathfinding/spawn read the
  world's map and radius, not a package const.

## Wire (`internal/protocol`)

**No protocol change.** `MapResponse{Radius int, Tiles []Tile}` and
`Tile{Hex, Terrain}` already carry everything; only `Radius` and the tile
contents differ at runtime. No tygo regen needed. (If a field is genuinely
required, flag it — but the intent is zero wire churn.)

## Client (`client/`)

The client already renders whatever `MapResponse` it receives, so a bigger map
"just renders." The one real gap: the `world` container is anchored so the hex
origin sits at **screen centre** and never pans, so on a radius-24 world a
player walking to the rim scrolls off-screen.

- **Camera-follow:** pan the `world` container each turn so **my entity stays
  centred** (offset `world.position` by `-hexToPixel(myHex)` plus the
  screen-centre term already there). Smooth is nice-to-have; snapping per turn
  is acceptable for this slice (movement already animates). Keep the existing
  resize handler working.
- `window.game` stays in sync (`tiles`, `me.hex` already exposed); no new
  fields strictly required, but expose enough for e2e to assert the camera
  centres on the player if convenient.

## Decisions (reasoned; flag if you disagree)

1. **Radius 24** default (~1,801 tiles) — ~4× today, ~120 tiles/player for 15
   players; sent whole as one `MapResponse` (well within a JSON bundle, gzipped
   small). Tunable via `WORLD_RADIUS`.
2. **Value noise, 2 octaves, 2 fields** (elevation + moisture) — simplest thing
   that yields coherent lakes/forests/mountains with no external deps. Simplex
   is overkill here.
3. **Rock rim kept** — bounds the world and matches existing edge assumptions.
4. **Grass clearing at origin + origin-component spawn restriction** — cheap,
   robust guarantee against stranded spawns without a full "largest region"
   analysis.
5. **Seed default is a fixed constant** — stable world across restarts; ops can
   set `WORLD_SEED` to reroll.

## Out of scope (later)

- **Chunked / streamed / boundless world** (its own milestone).
- **Rivers, roads, structures, towns, named regions.**
- **Terrain-blocked line-of-sight** (already deferred from 6b.2).
- **Biome-driven encounters / monster spawns / resources.**
- **Map persistence to disk** (regen-from-seed is the persistence for now).
- **Minimap, zoom controls** (camera-follow only this slice).

## Tests

- **Unit (`internal/game`)**: same `(seed, radius)` → byte-identical map
  (determinism); a different seed → a different map; all four terrains present;
  the rim is entirely `rock`; the **origin tile is walkable** and its
  flood-fill walkable component is **large** (e.g. ≥ half the interior) so
  spawns aren't stranded; tile count matches `3r(r+1)+1`. Pin seeds.
- **Integration (`test/integration`)**: two servers started with the **same
  `WORLD_SEED`** serve **identical** `/api/map`; a **different** seed serves a
  **different** map. Default `startServer` still yields a walkable world where
  the existing chase/spawn tests pass.
- **e2e (`client/e2e`)**: the generated world renders (tile count > 0, terrains
  varied); a player can walk on it; the camera keeps my entity roughly centred
  as I move across several turns.

## Risks

- **Determinism**: `GenerateMap` must draw only from the seed+lattice, never a
  global RNG or clock — else tests flake and restarts diverge. Same discipline
  as the seeded combat turns.
- **Stranded spawns**: noise could isolate the origin; the grass clearing +
  origin-component restriction is the guard. The connectivity unit test must be
  strict (fail if the origin region is small).
- **Bigger map performance**: ~1,801 tiles is fine on the wire and in Pixi, but
  the client must not rebuild the whole map layer every turn (build once, only
  pan). Camera-follow touches only `world.position`.
- **`MapRadius` const → field**: several call sites (tests, spawn, bubble)
  reference the radius; thread the world's radius through them without changing
  behaviour for the default.
