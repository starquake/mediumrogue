// Post-processing looks. The whole retro aesthetic is one swappable filter
// pass over the Pixi stage (design §6): experiment with looks here without
// touching game logic. The DOM social UI floats above the canvas and is
// deliberately NOT filtered.
import { defaultFilterVert, Filter, GlProgram } from "pixi.js";
import type { Application } from "pixi.js";

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

// Fragment form verified against the installed Pixi v8 package (see
// client/src/render/filter.ts history / task-1 report): no #version header or
// precision qualifiers (GlProgram injects "300 es" + default precision for
// us — see node_modules/pixi.js/lib/rendering/renderers/gl/shader/GlProgram.d.ts),
// plain `in`/`out` varyings, and `uTexture`/`uInputSize` are the exact names
// the filter system's global uniform group provides — mirrored from
// node_modules/pixi.js/lib/filters/defaults/displacement/displacement.frag.mjs,
// the one built-in filter whose fragment shader also reads uInputSize
// directly (xy = input texture size in texels, zw = 1/size — see
// FilterSystem.js _updateFilterUniforms).
const crtFrag = /* glsl */ `
  in vec2 vTextureCoord;
  out vec4 finalColor;

  uniform sampler2D uTexture;
  // highp is REQUIRED: the default filter vertex stage reads uInputSize at
  // highp (the GLSL ES vertex default), and a fragment redeclaration at the
  // injected default (mediump) is a LINK error on strict drivers (real
  // NVIDIA GL rejects the program and the whole stage renders blank; CI's
  // SwiftShader permits the mismatch, hiding it). Mirrors the explicit
  // qualifier in pixi.js's own displacement.frag.
  uniform highp vec4 uInputSize;
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

// buildCRT constructs a fresh CRT filter. Construction shape (glProgram via
// GlProgram.from + a plain-object `resources` group) mirrors the documented
// custom-filter example in node_modules/pixi.js/lib/filters/Filter.d.ts and
// the built-in AlphaFilter/NoiseFilter (defaultFilterVert + GlProgram.from +
// a single named uniform-group resource).
function buildCRT(): Filter {
  return new Filter({
    glProgram: GlProgram.from({ vertex: defaultFilterVert, fragment: crtFrag, name: "crt-filter" }),
    resources: {
      crtUniforms: {
        uScanlineDepth: { value: CRT.scanlineDepth, type: "f32" },
        uScanlinePeriod: { value: CRT.scanlinePeriod, type: "f32" },
        uVignetteStrength: { value: CRT.vignetteStrength, type: "f32" },
        uDesaturate: { value: CRT.desaturate, type: "f32" },
        uTint: { value: CRT.tint, type: "vec3<f32>" },
      },
    },
  });
}

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
