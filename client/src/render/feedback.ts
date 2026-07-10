import { Container, Graphics, type Ticker } from "pixi.js";

import type { Hex } from "../protocol.gen";
import { hexToPixel, HEX_SIZE } from "./hex";

const DESTINATION_COLOR = 0x8fd0ff; // matches my entity dot
const ATTACK_COLOR = 0xd6544f; // matches the monster/hostile red
const PULSE_PERIOD_MS = 900;
const FLASH_DURATION_MS = 450;

/**
 * Instant click acknowledgement, drawn the moment an intent is SUBMITTED —
 * deliberately ahead of the server's answer, which only arrives with the next
 * turn bundle. A walk click plants a pulsing ring on the destination hex (the
 * first slice of the planned path preview); a ranged-attack click fires a
 * brief expanding flash on the target hex. Both are local-only cosmetics: the
 * authoritative outcome still comes from turn bundles, and main.ts clears the
 * destination ring on arrival, rejection, or re-route.
 */
export class FeedbackLayer {
  readonly container = new Container();
  private readonly destGfx = new Graphics();
  private readonly flashGfx = new Graphics();
  private dest: Hex | null = null;
  private pulseMs = 0;
  private flashHex: Hex | null = null;
  private flashMs = 0;

  constructor(ticker: Ticker) {
    this.container.addChild(this.destGfx);
    this.container.addChild(this.flashGfx);
    ticker.add(this.tick);
  }

  /** Plant (or move) the destination ring. Pass null to clear it. */
  setDestination(hex: Hex | null): void {
    this.dest = hex;
    if (hex === null) {
      this.destGfx.clear();
    } else {
      this.pulseMs = 0;
    }
  }

  /** One-shot expanding ring on a ranged-attack target hex. */
  flashAttack(hex: Hex): void {
    this.flashHex = hex;
    this.flashMs = 0;
  }

  private tick = (ticker: Ticker): void => {
    if (this.dest !== null) {
      this.pulseMs = (this.pulseMs + ticker.deltaMS) % PULSE_PERIOD_MS;
      // Ring breathes between 0.55 and 0.75 of a hex, fading as it grows.
      const f = this.pulseMs / PULSE_PERIOD_MS;
      const { x, y } = hexToPixel(this.dest);
      this.destGfx
        .clear()
        .circle(x, y, HEX_SIZE * (0.55 + 0.2 * f))
        .stroke({ width: 2, color: DESTINATION_COLOR, alpha: 1 - 0.6 * f })
        .circle(x, y, HEX_SIZE * 0.12)
        .fill({ color: DESTINATION_COLOR, alpha: 0.9 });
    }

    if (this.flashHex !== null) {
      this.flashMs += ticker.deltaMS;
      if (this.flashMs >= FLASH_DURATION_MS) {
        this.flashHex = null;
        this.flashGfx.clear();
      } else {
        const f = this.flashMs / FLASH_DURATION_MS;
        const { x, y } = hexToPixel(this.flashHex);
        this.flashGfx
          .clear()
          .circle(x, y, HEX_SIZE * (0.3 + 0.6 * f))
          .stroke({ width: 3, color: ATTACK_COLOR, alpha: 1 - f });
      }
    }
  };
}
