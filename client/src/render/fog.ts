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
// Drawn as concentric RINGS rather than one big gradient quad, and that is a
// performance decision, not a stylistic one. The obvious implementation — a
// screen-covering sprite with a radial gradient — costs a full-viewport alpha
// blend every frame even though its middle is entirely transparent. Measured
// under CI's software renderer that was ~6 s on a 14 s e2e spec, pushing it
// against its 30 s timeout. Rings rasterise only the annulus that actually has
// alpha, so at the default zoom — where the boundary sits at the screen edge —
// almost nothing is drawn at all.

// FADE_HEXES is how far inside the boundary the fade begins — the last few
// hexes wash out rather than ending on a line, which is what makes it variant A
// rather than a drawn rim.
const FADE_HEXES = 5;

// BANDS is how many constant-alpha rings approximate the ramp: enough that the
// steps are invisible against the hex grid, few enough to stay cheap.
const BANDS = 24;

// SKIRT is how far past the boundary the opaque fill reaches, as a multiple of
// the boundary radius. Beyond the boundary the fog is fully opaque, and it has
// to out-reach the widest viewport anyone can pull up at ZOOM_MIN — otherwise
// undimmed ground would show past its edge.
const SKIRT = 3;

// FOG_COLOR matches the app background (#0b0f0b, main.ts), so "fully fogged"
// reads as the void the map sits on rather than a grey film over it.
const FOG_COLOR = 0x0b0f0b;

// A hex ring maps to an ELLIPSE in pixels, not a circle: with flat-top hexes
// the east–west extent is 1.5 × HEX_SIZE × radius while north–south is
// √3 × HEX_SIZE × radius. The geometry is built at the true east–west size, so
// the curves tessellate smoothly, and the container is stretched on Y by the
// ratio — drawing a unit circle and scaling it up would leave visible polygon
// edges.
const EAST_WEST = 1.5 * HEX_SIZE * InterestRadius;
const NORTH_SOUTH = Math.sqrt(3) * HEX_SIZE * InterestRadius;

export interface FogLayer {
  container: Container;
  /** Re-centres the fade on the player's live pixel position. */
  update(x: number, y: number): void;
}

/** ring strokes one constant-alpha annulus between radii r0 and r1. */
function ring(g: Graphics, r0: number, r1: number, alpha: number): void {
  const width = r1 - r0;

  g.ellipse(0, 0, r0 + width / 2, r0 + width / 2).stroke({ width, color: FOG_COLOR, alpha });
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

  const fadeStart = ((InterestRadius - FADE_HEXES) / InterestRadius) * EAST_WEST;

  for (let i = 0; i < BANDS; i++) {
    const r0 = fadeStart + ((EAST_WEST - fadeStart) * i) / BANDS;
    const r1 = fadeStart + ((EAST_WEST - fadeStart) * (i + 1)) / BANDS;

    ring(g, r0, r1, (i + 1) / BANDS);
  }

  // Everything past the boundary is simply gone.
  ring(g, EAST_WEST, EAST_WEST * SKIRT, 1);

  container.addChild(g);
  container.scale.set(1, NORTH_SOUTH / EAST_WEST);

  return {
    container,
    update(x: number, y: number): void {
      // The container carries the Y stretch, so the centre has to be divided
      // back out of it to land on the player's true pixel.
      container.position.set(x, (y * EAST_WEST) / NORTH_SOUTH);
    },
  };
}
