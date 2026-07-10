# Milestone 9 — Shader Filter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development (recommended) or superpowers:executing-plans. Steps use `- [ ]` checkboxes.

**Goal:** The CRT-style post-processing filter (scanlines + vignette + desaturation/tint) as a swappable PixiJS filter pass over the stage, on by default, toggleable + persisted, exposed to Playwright.

**Architecture:** One new client module `client/src/render/filter.ts` (looks registry `"crt" | "none"`, `applyFilter(app, name)`, the GLSL, tunable uniform defaults); `main.ts` applies the persisted choice at startup and wires a HUD toggle + `window.game.{filter,setFilter}`. Client-only — no Go, no wire change.

**Tech Stack:** TypeScript + PixiJS v8 (`Filter` + `GlProgram`, CSP-safe `unsafe-eval` build), Playwright.

## Global Constraints

- **Client-only**: no Go/protocol changes. The strict CSP holds (WebGL shader compilation is not eval).
- **The DOM UI is NOT filtered** — the filter goes on the Pixi stage only; chat/roster/quest/HUD stay crisp.
- **Verify the Pixi v8 filter API against the installed package** (`client/node_modules/pixi.js` `.d.ts`), not from memory — construct `Filter` the way v8 wants (GlProgram.from with Pixi's default filter vertex source; a `uniforms`/resources group for the tunables).
- **Default on** (`"crt"`), persisted in `localStorage` key `mediumrogue.filter`; `"none"` disables entirely.
- **No pixel assertions** in e2e (GPU-variant); assert the contract (`window.game.filter`, toggle, persistence) and that the full suite runs green WITH the filter active.
- Style: `import type` (verbatimModuleSyntax); match `main.ts` idioms; text-node DOM only. `make check` + `make e2e` green. One PR off `main` (branch `m9-shader-filter`, already created).

---

### Task 1: Filter module + wiring + toggle

**Files:**
- Create: `client/src/render/filter.ts`
- Modify: `client/src/main.ts` (startup apply, HUD toggle wiring, `window.game.filter`/`setFilter` in `GameDebug` + initializer)
- Modify: `client/index.html` (a `#filter-toggle` button in the HUD bar + style)

**Interfaces (Produces):** `type FilterName = "crt" | "none"`; `applyFilter(app: Application, name: FilterName): void`; `currentFilter(): FilterName`; `loadFilterChoice(): FilterName`; `saveFilterChoice(name: FilterName): void`; `window.game.filter: string`; `window.game.setFilter(name: string): void`.

- [ ] **Step 1: Inspect the installed Pixi v8 filter API**

Run: `grep -rn "class Filter\|GlProgram\|defaultFilterVert\|from(" client/node_modules/pixi.js/lib/filters/Filter.d.ts client/node_modules/pixi.js/lib/rendering/renderers/gl/shader/GlProgram.d.ts 2>/dev/null | head -20` (adjust paths as found — also check `pixi.js/lib/filters/defaults` for a default filter vertex export). Confirm the v8 construction shape before writing code. Pixi v8's canonical custom-filter form is:

```ts
import { Filter, GlProgram } from "pixi.js";

const filter = new Filter({
  glProgram: GlProgram.from({ vertex: defaultFilterVert, fragment: crtFrag, name: "crt-filter" }),
  resources: {
    crtUniforms: {
      uScanlineDepth: { value: 0.12, type: "f32" },
      // …
    },
  },
});
```

(v8 exports a default filter vertex shader — find its real export name, e.g. `import { defaultFilterVert } from "pixi.js"` — verify.)

- [ ] **Step 2: Write `client/src/render/filter.ts`**

```ts
// Post-processing looks. The whole retro aesthetic is one swappable filter
// pass over the Pixi stage (design §6): experiment with looks here without
// touching game logic. The DOM social UI floats above the canvas and is
// deliberately NOT filtered.
import { Application, Filter, GlProgram } from "pixi.js";

export type FilterName = "crt" | "none";

const STORAGE_KEY = "mediumrogue.filter";

// CRT tuning — all uniforms, dial the look here.
const CRT = {
  scanlineDepth: 0.12, // how dark the dark lines get (0..1)
  scanlinePeriod: 3.0, // screen pixels per scanline cycle
  vignetteStrength: 0.25, // corner darkening (0..1)
  desaturate: 0.2, // mix toward luminance (0..1)
  tint: [1.0, 1.04, 0.96], // faint phosphor bias (r,g,b multipliers)
};

const crtFrag = /* glsl */ `
  in vec2 vTextureCoord;
  out vec4 finalColor;

  uniform sampler2D uTexture;
  uniform vec4 uInputSize;
  uniform float uScanlineDepth;
  uniform float uScanlinePeriod;
  uniform float uVignetteStrength;
  uniform float uDesaturate;
  uniform vec3 uTint;

  void main(void) {
    vec4 color = texture(uTexture, vTextureCoord);

    // Scanlines in screen space.
    float y = vTextureCoord.y * uInputSize.y;
    float scan = 1.0 - uScanlineDepth * (0.5 + 0.5 * sin(6.2831853 * y / uScanlinePeriod));
    color.rgb *= scan;

    // Vignette.
    vec2 centered = vTextureCoord - 0.5;
    float vig = 1.0 - uVignetteStrength * dot(centered, centered) * 4.0;
    color.rgb *= clamp(vig, 0.0, 1.0);

    // Desaturate toward luminance, then phosphor tint.
    float luma = dot(color.rgb, vec3(0.299, 0.587, 0.114));
    color.rgb = mix(color.rgb, vec3(luma), uDesaturate) * uTint;

    finalColor = color;
  }
`;

// buildCRT constructs a fresh CRT filter (verify constructor shape against the
// installed Pixi v8 — see the plan's Step 1; adapt vertex-source import and
// the resources group to what the .d.ts actually exports).
function buildCRT(): Filter { /* per Step 1 findings */ }

let active: FilterName = "crt";

export function currentFilter(): FilterName {
  return active;
}

export function loadFilterChoice(): FilterName {
  return localStorage.getItem(STORAGE_KEY) === "none" ? "none" : "crt";
}

export function saveFilterChoice(name: FilterName): void {
  localStorage.setItem(STORAGE_KEY, name);
}

/** Apply a look to the stage ("none" removes post-processing entirely). */
export function applyFilter(app: Application, name: FilterName): void {
  active = name;
  app.stage.filters = name === "crt" ? [buildCRT()] : [];
}
```

Fill `buildCRT` with the verified v8 construction (uniform names must match the GLSL; `uTexture`/`uInputSize` are Pixi-provided filter built-ins in v8 — confirm their exact names in the installed types/default shaders; if v8's built-in is e.g. `uInputSize` vs `inputSize`, follow the package). If v8 requires a specific GLSL version header/`in/out` form for filters, copy the form used by a built-in v8 filter's fragment source (find one under `node_modules/pixi.js/lib/filters/defaults/…` and mirror it exactly).

- [ ] **Step 3: Wire `main.ts`**

- After `app.init(...)` + canvas append: `applyFilter(app, loadFilterChoice());`
- `GameDebug`: add `filter: string` (doc: the active post-processing look) and `setFilter(name: string): void`. Initializer: `filter: "crt"`, `setFilter: () => {}` (replaced after app exists). After the app exists:

```ts
  const syncFilterUI = (): void => {
    window.game.filter = currentFilter();
    filterToggleEl.textContent = `filter: ${currentFilter()}`;
  };

  window.game.setFilter = (name: string): void => {
    const look: FilterName = name === "none" ? "none" : "crt";
    applyFilter(app, look);
    saveFilterChoice(look);
    syncFilterUI();
  };

  filterToggleEl.addEventListener("click", () => {
    window.game.setFilter(currentFilter() === "crt" ? "none" : "crt");
  });
  syncFilterUI();
```

(`const filterToggleEl = mustGet("filter-toggle");` with the other elements; import the filter module with `import type { FilterName }` for the type.)

- [ ] **Step 4: index.html** — add to the HUD bar: `<button type="button" id="filter-toggle">filter: crt</button>`, styled like the HUD text (small, unobtrusive, pointer-events fine — it's inside the HUD which already takes events).

- [ ] **Step 5: Verify**

Run: `cd client && npm run check && npm run build`, then from repo root `PATH=$PATH:/usr/local/go/bin make check`, then a REAL browser smoke: `make e2e 2>&1 | tail -3` — the ENTIRE existing suite now runs with the filter on by default; it must be green (this is the headless-WebGL/SwiftShader proof). If a spec fails on shader compile, fix the GLSL form (Step 2 note) — do not turn the filter off by default to pass.

- [ ] **Step 6: Commit**

```bash
git add client/
git commit -m "client: CRT post-processing filter (swappable pass, HUD toggle, persisted)"
```

---

### Task 2: e2e + docs + gate

**Files:**
- Create: `client/e2e/filter.spec.ts`; modify `client/playwright.config.ts` (add `{ name: "filter" }`)
- Modify: `docs/STATUS.md`, `docs/roguelike-mp-plan.md`

- [ ] **Step 1: e2e** — `client/e2e/filter.spec.ts` (single browser, mirror `turn.spec.ts` setup):
1. Fresh client → `waitForFunction(window.game.filter === "crt")`; `#filter-toggle` text contains "crt".
2. Click `#filter-toggle` → `window.game.filter === "none"`, button text updates; **reload** → still `"none"` (localStorage persisted).
3. `window.game.setFilter("crt")` → back on; a walk still works with the filter active (one `tapHex` step + arrival, the walk.spec pattern — proves input/render unaffected).

- [ ] **Step 2: Gate** — `PATH=$PATH:/usr/local/go/bin make check && make e2e` **twice**, all specs both runs.

- [ ] **Step 3: Docs** — STATUS: milestone 9 DONE (CRT filter — scanlines/vignette/desaturate+tint as a swappable `filter.ts` pass, default on, HUD toggle + localStorage, DOM UI unfiltered, `window.game.filter`/`setFilter`; bloom/flat-palette/flicker deferred as registry entries) → next per §8 is **10 polish & launch**; set `Last updated`. Plan doc §8.9: "(landed)" annotation.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "docs+e2e: milestone 9 shader filter landed; next is polish & launch"
```

---

## Self-Review

- **Spec coverage:** registry+applyFilter+CRT GLSL+uniform tuning block → Task 1; default-on + persistence + HUD toggle + window.game → Task 1; DOM-unfiltered (stage-only) → Task 1 by construction; e2e contract + suite-runs-filtered + docs → Task 2. ✔
- **Placeholder scan:** `buildCRT` body is deliberately "per Step 1 findings" WITH the exact verification procedure and fallback instruction (mirror a built-in v8 filter's fragment form) — the one place where copying from the installed package beats plan-authored code, stated as a concrete procedure, not a TBD. Everything else is complete code. ✔
- **Type consistency:** `FilterName`, `applyFilter(app, name)`, `currentFilter()`, `loadFilterChoice/saveFilterChoice`, `window.game.{filter,setFilter}` consistent across tasks; uniform names match between GLSL and the resources group (called out). ✔
- **Risks flagged:** v8 API verified from the package; SwiftShader proof = the whole suite running filtered; no pixel assertions. ✔
