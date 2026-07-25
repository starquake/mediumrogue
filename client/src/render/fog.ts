import { Container, Sprite, Texture } from "pixi.js";

import { InterestRadius } from "../protocol.gen";
import { HEX_SIZE } from "./hex";

// fog.ts (#289, variant A "soft vignette"): the visible edge of the interest
// radius. The server stops sending live entity data past InterestRadius, so
// the ground out there is terrain the client already holds from /api/map with
// nothing moving on it — this is what makes that legible instead of reading as
// pop-in.
//
// It sits directly above the map layer and BELOW everything else, so only the
// GROUND fades. Entities keep full contrast: a monster at 19 hexes is exactly
// the one that is about to matter, and dimming it would be the opposite of
// helpful.

// FADE_HEXES is how far inside the boundary the fade begins — the last few
// hexes wash out rather than ending on a line, which is what makes it variant
// A rather than a drawn rim.
const FADE_HEXES = 5;

// SPAN sizes the sprite relative to the boundary. The gradient is opaque from
// the boundary outward, so the sprite has to reach past the widest viewport
// anyone can pull up at ZOOM_MIN — otherwise the ground beyond its edge would
// sit there undimmed at full brightness.
const SPAN = 3;

// FOG_RGB matches the app background (#0b0f0b, main.ts), so "fully fogged"
// reads as the void the map sits on rather than as a grey film over it.
const FOG_RGB = "11, 15, 11";

// TEXTURE_PX is the gradient's own resolution. It is stretched across
// thousands of world units, but a smooth radial ramp survives that — this only
// needs enough steps to avoid banding.
const TEXTURE_PX = 512;

// A hex ring maps to an ELLIPSE in pixels, not a circle: with flat-top hexes
// the east–west extent is 1.5 × HEX_SIZE × radius while north–south is
// √3 × HEX_SIZE × radius. Scaling the circular gradient by both keeps the fade
// on the boundary in every direction instead of biting early north and south.
const EAST_WEST = 1.5 * HEX_SIZE * InterestRadius;
const NORTH_SOUTH = Math.sqrt(3) * HEX_SIZE * InterestRadius;

/** A radial ramp: clear in the middle, opaque from the boundary outward. */
function gradientTexture(): Texture {
  const canvas = document.createElement("canvas");
  canvas.width = TEXTURE_PX;
  canvas.height = TEXTURE_PX;

  const ctx = canvas.getContext("2d");
  if (ctx === null) {
    // No 2D context means no fog rather than a crash — the game is still
    // entirely playable without the vignette.
    return Texture.EMPTY;
  }

  const half = TEXTURE_PX / 2;
  const gradient = ctx.createRadialGradient(half, half, 0, half, half, half);

  // Texture-space fractions: the boundary sits at 1/SPAN of the sprite's own
  // radius, and the fade starts FADE_HEXES inside it.
  const boundary = 1 / SPAN;
  const fadeStart = ((InterestRadius - FADE_HEXES) / InterestRadius) * boundary;

  gradient.addColorStop(0, `rgba(${FOG_RGB}, 0)`);
  gradient.addColorStop(fadeStart, `rgba(${FOG_RGB}, 0)`);
  gradient.addColorStop(boundary, `rgba(${FOG_RGB}, 1)`);
  gradient.addColorStop(1, `rgba(${FOG_RGB}, 1)`);

  ctx.fillStyle = gradient;
  ctx.fillRect(0, 0, TEXTURE_PX, TEXTURE_PX);

  return Texture.from(canvas);
}

export interface FogLayer {
  container: Container;
  /** Re-centres the fade on the player's live pixel position. */
  update(x: number, y: number): void;
}

/**
 * createFogLayer builds the interest-radius vignette.
 *
 * World-space on purpose, not screen-space: the sprite lives inside the same
 * `world` container as the map, so `world.scale` already applies the camera
 * zoom to it and the z-order puts it under the entities for free. All it has
 * to do per frame is follow the player.
 */
export function createFogLayer(): FogLayer {
  const container = new Container();
  const sprite = new Sprite(gradientTexture());

  sprite.anchor.set(0.5);
  sprite.width = 2 * SPAN * EAST_WEST;
  sprite.height = 2 * SPAN * NORTH_SOUTH;

  container.addChild(sprite);

  return {
    container,
    update(x: number, y: number): void {
      sprite.position.set(x, y);
    },
  };
}
