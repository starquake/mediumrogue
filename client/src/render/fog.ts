import { Container, Graphics } from "pixi.js";

import { InterestRadius } from "../protocol.gen";
import { HEX_SIZE } from "./hex";

// fog.ts (#289, variant A "soft vignette"): the visible edge of the interest
// radius. The server stops sending live entity data past InterestRadius, so the
// ground out there is terrain the client already holds from /api/map with
// nothing moving on it — this is what makes that legible instead of reading as
// pop-in.
//
// It sits directly above the map layer and BELOW everything else, so only the
// GROUND fades. Entities keep full contrast: a monster at 19 hexes is exactly
// the one about to matter, and dimming it would be the opposite of helpful.
//
// The boundary is a HEXAGON, not a circle or an ellipse, because that is what
// a constant hex distance actually is. An earlier ellipse through the north and
// east vertices looked close but left the east/west corners of the true
// boundary OUTSIDE the fog — so in-range monsters standing there appeared over
// unfogged black, which reads as a rendering fault rather than as fog.
//
// Drawn as concentric rings rather than one big gradient quad, and that is a
// performance decision. A screen-covering sprite costs a full-viewport alpha
// blend every frame even though its middle is entirely transparent; under CI's
// software renderer that measured ~6 s on a 14 s e2e spec. Rings rasterise only
// the annulus that actually has alpha, so at the default zoom — where the
// boundary sits at the screen edge — almost nothing is drawn.

// FADE_HEXES is how far inside the boundary the fade begins — the last hexes
// wash out rather than ending on a line, which is what makes it variant A
// rather than a drawn rim.
//
// The band is FADE_HEXES + 1 hexes wide in total, because FADE_END carries it
// one hex past the boundary: at 2 that is a 3-hex ramp. It started at 5 (a
// 6-hex ramp), which read as a soft gradient across a third of the visible
// world rather than as an edge.
const FADE_HEXES = 2;

// BANDS is how many constant-alpha rings approximate the ramp: enough that the
// steps are invisible against the hex grid, few enough to stay cheap.
const BANDS = 24;

// FADE_END is where the fade reaches full opacity: one hex PAST the interest
// radius, not on it. Landing it exactly on the boundary blacks out the
// outermost ring of hexes the server still sends, so a monster standing there —
// in range, drawn above the fog — appears to float on void. Finishing a hex
// later keeps some ground under every entity the client can be told about, and
// full opacity then begins where there is never anything to draw.
const FADE_END = (InterestRadius + 1) / InterestRadius;

// SKIRT is how far past the boundary the opaque fill reaches, as a multiple of
// the boundary. Beyond the fade the fog is fully opaque, and it has to
// out-reach the widest viewport anyone can pull up at ZOOM_MIN — otherwise
// undimmed ground would show past its edge.
const SKIRT = 3;

// FOG_COLOR matches the app background (#0f0a0c, main.ts), so "fully fogged"
// reads as the void the map sits on rather than a grey film over it.
const FOG_COLOR = 0x0f0a0c;

// CIRCUMRADIUS is the pixel distance to any corner of the hex-distance-
// InterestRadius ring. Under hex.ts's flat-top layout (x = 1.5·s·q,
// y = s·(√3/2·q + √3·r)) the corner (q=R, r=0) lands at (1.5·s·R, √3/2·s·R) and
// (q=0, r=R) at (0, √3·s·R) — both √3·s·R from the centre. All six corners are
// equidistant, so the boundary is a REGULAR hexagon and its corners can be
// generated from an angle rather than tabulated.
const CIRCUMRADIUS = Math.sqrt(3) * HEX_SIZE * InterestRadius;

// CORNERS is how many corners a hexagon has. Named so the geometry below reads
// as "walk the boundary" rather than as a bare 6.
const CORNERS = 6;

/** corner returns hexagon corner i, on a boundary scaled by `scale`. */
function corner(i: number, scale: number): [number, number] {
  // 30° puts points north and south with flat faces east and west, matching
  // how a constant hex distance actually falls on this grid.
  const angle = (Math.PI / 180) * (30 + (360 / CORNERS) * i);

  return [Math.cos(angle) * CIRCUMRADIUS * scale, Math.sin(angle) * CIRCUMRADIUS * scale];
}

export interface FogLayer {
  container: Container;
  /** Re-centres the fade on the player's live pixel position. */
  update(x: number, y: number): void;
}

/**
 * band fills the region between two scaled copies of the boundary hexagon.
 *
 * Built as six quads, one per hexagon edge, rather than as a polygon with a
 * hole: adjacent quads share their end edges, so the annulus tiles exactly with
 * no seams, and it needs no even-odd fill rule to punch the middle out.
 */
function band(g: Graphics, inner: number, outer: number, alpha: number): void {
  for (let i = 0; i < CORNERS; i++) {
    const [aix, aiy] = corner(i, inner);
    const [bix, biy] = corner(i + 1, inner);
    const [aox, aoy] = corner(i, outer);
    const [box, boy] = corner(i + 1, outer);

    g.poly([aix, aiy, bix, biy, box, boy, aox, aoy]).fill({ color: FOG_COLOR, alpha });
  }
}

/**
 * createFogLayer builds the interest-radius vignette.
 *
 * World-space on purpose, not screen-space: the graphics live in the same
 * `world` container as the map, so `world.scale` already applies the camera
 * zoom and the z-order puts them under the entities for free. All the layer
 * does per frame is follow the player.
 */
export function createFogLayer(): FogLayer {
  const container = new Container();
  const g = new Graphics();

  const fadeStart = (InterestRadius - FADE_HEXES) / InterestRadius;
  const span = FADE_END - fadeStart;

  for (let i = 0; i < BANDS; i++) {
    band(g, fadeStart + (span * i) / BANDS, fadeStart + (span * (i + 1)) / BANDS, (i + 1) / BANDS);
  }

  // Past the fade there is nothing the server would ever send.
  band(g, FADE_END, SKIRT, 1);

  container.addChild(g);

  return {
    container,
    update(x: number, y: number): void {
      container.position.set(x, y);
    },
  };
}
