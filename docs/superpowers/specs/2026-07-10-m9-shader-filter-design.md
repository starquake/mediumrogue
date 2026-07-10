# Milestone 9 — Shader Filter Pass: Design

*Status: DRAFT (2026-07-10). The retro-look post-processing filter (§6, §8.9).
Client-only; branches off `main` in parallel with the open 8.3 PR (orthogonal).*

## Goal

The game's signature look: a **CRT-style post-processing filter** (scanlines,
gentle vignette, slight desaturation toward a moody Caves-of-Qud-ish tone) as a
**PixiJS WebGL fragment-shader filter over the whole stage** — deliberately a
**separate, swappable pass** so looks can be experimented with without touching
game logic. (All decided in the design doc; no new forks.)

## Architecture (`client/src/render/filter.ts`)

- A **looks registry**: `FilterName = "crt" | "none"`. `"crt"` is the default;
  `"none"` disables post-processing entirely (also the accessibility/perf
  escape hatch). Adding a future look (bloom, flat palette) = adding a registry
  entry — game code never changes.
- **`applyFilter(app: Application, name: FilterName)`** sets
  `app.stage.filters` (a fresh `Filter` for the look, or `null` for none) and
  records the active name. **`currentFilter()`** returns it.
- **CRT shader** (Pixi v8 `Filter` + `GlProgram.from({vertex, fragment})`,
  using Pixi's default filter vertex shader): per-fragment —
  1. sample the scene;
  2. **scanlines**: darken alternating lines (`sin` on screen-space y; subtle,
     ~12% depth);
  3. **vignette**: radial darkening toward corners (~25% at the edge);
  4. **desaturate + tint**: mix toward luminance (~20%) with a faint
     green-amber bias for the retro phosphor mood.
  Tuning values are **uniforms** with named defaults in one place (a `CRT`
  params object) so the look can be dialed without touching GLSL.
- **CSP**: WebGL shader compilation is not JS eval — the strict CSP
  (`pixi.js/unsafe-eval` build) is unaffected.
- **DOM stays crisp**: the filter applies to the Pixi stage only; the social
  UI (chat/roster/quest panels, HUD) is DOM floating above the canvas and is
  deliberately NOT filtered (per §6 — the readable social surface).

## Wiring (`client/src/main.ts`, `client/index.html`)

- Apply the persisted look at startup (after `app.init`):
  `applyFilter(app, loadFilterChoice())` — **persisted in `localStorage`**
  (`mediumrogue.filter`), defaulting to `"crt"`.
- A small **HUD toggle button** (`#filter-toggle`, DOM, in the existing HUD
  bar): click cycles crt → none → crt, applies + persists. Labeled with the
  active look (e.g. `filter: crt`).
- **`window.game.filter: string`** (active look) and
  **`window.game.setFilter(name): void`** (apply + persist) for Playwright.

## Decisions (reasoned)

1. **Two looks this slice** (`crt`, `none`) — the doc asks for a swappable
   pass to *experiment*; the registry is the experiment seam, and `none` is
   the control. Bloom/flat-palette variants are follow-up registry entries.
2. **No time-based flicker/noise** this slice — needs a ticker-driven uniform
   and risks headache-y motion; static scanlines+vignette read as CRT already.
3. **localStorage persistence** — same mechanism as identity; a player who
   turns it off stays off across reloads.
4. **No pixel-value e2e assertions** — GPU output differs across
   machines/headless; e2e asserts the observable contract (default on,
   toggle/persist works, `window.game.filter`) and that gameplay specs still
   pass with the filter active (it's on by default in every existing e2e).

## Out of scope (later)

- Bloom / flat-retro-palette looks; time-based flicker/noise/curvature;
  per-look intensity sliders; the retina/high-DPI interaction (feel-pass
  memory item — the filter samples at backing-store resolution and will be
  revisited there).

## Tests

- **e2e (`client/e2e/filter.spec.ts`)**: fresh client → `window.game.filter
  === "crt"` and the toggle button shows it; `setFilter("none")` (and a real
  button click) flips `window.game.filter` + persists across a reload
  (localStorage); gameplay unaffected — a walk still works with the filter on
  (implicitly: every existing spec now runs filtered).
- **`npm run check`** type-checks the new module; `make check` + `make e2e`
  (all specs, twice) green.

## Risks

- **Pixi v8 filter API** (`Filter` + `GlProgram` + default filter vertex
  source) must match the installed Pixi version — verify against
  `node_modules/pixi.js` types, not memory.
- **Headless WebGL**: Playwright's Chromium uses SwiftShader — the filter must
  not crash it (it's a simple fragment shader; the e2e suite running green IS
  the test).
- **Perf**: one full-screen pass; trivial for this scene. If it ever matters,
  `none` is the escape hatch.
