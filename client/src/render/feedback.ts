import { Container, Graphics, type Ticker } from "pixi.js";

import type { Hex } from "../protocol.gen";
import { hexToPixel, HEX_SIZE } from "./hex";

const DESTINATION_COLOR = 0x8fd0ff; // matches my entity dot
const ATTACK_COLOR = 0xd6544f; // matches the monster/hostile red
const WAIT_COLOR = 0xe8e4d0; // neutral parchment, distinct from either faction color
const PULSE_PERIOD_MS = 900;
const FLASH_DURATION_MS = 450;

/**
 * What I committed to THIS bubble-turn while inside a combat bubble (item 6,
 * playtest batch 2): the intent I already submitted, still shown while it
 * waits on the rest of the bubble to lock in. target is always a hex — a
 * hex-targeted attack (AoE) or the entity-targeted victim's hex at click
 * time (item 7); "wait" always targets my own hex. Cleared on the next turn
 * bundle (main.ts), whether it resolved or a later intent replaced it.
 */
export type CommittedActionKind = "move" | "attack" | "wait";
export interface CommittedAction {
  kind: CommittedActionKind;
  target: Hex;
}

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
  private readonly committedGfx = new Graphics();
  private dest: Hex | null = null;
  private pulseMs = 0;
  private flashHex: Hex | null = null;
  private flashMs = 0;

  constructor(ticker: Ticker) {
    this.container.addChild(this.destGfx);
    this.container.addChild(this.flashGfx);
    this.container.addChild(this.committedGfx);
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

  /**
   * Plant (or clear) the committed-action indicator (item 6): what I chose
   * this bubble-turn, shown until it resolves — a solid step marker for a
   * move, a persistent crosshair ring for an attack, a small hourglass on my
   * own hex for a wait. Static (no pulse/animation), distinct from the
   * pulsing destination ring and the reachable-tile tint, so it reads as
   * "already decided" rather than "still choosing." Drawn once here, not
   * per-tick, since nothing about it changes while it's showing.
   */
  setCommitted(action: CommittedAction | null): void {
    this.committedGfx.clear();

    if (action === null) {
      return;
    }

    const { x, y } = hexToPixel(action.target);

    switch (action.kind) {
      case "move":
        this.committedGfx
          .circle(x, y, HEX_SIZE * 0.28)
          .fill({ color: DESTINATION_COLOR, alpha: 0.95 })
          .circle(x, y, HEX_SIZE * 0.42)
          .stroke({ width: 2, color: DESTINATION_COLOR, alpha: 0.8 });
        break;
      case "attack": {
        const r = HEX_SIZE * 0.4;
        this.committedGfx
          .circle(x, y, r)
          .stroke({ width: 2.5, color: ATTACK_COLOR, alpha: 0.9 })
          .moveTo(x - r * 1.3, y)
          .lineTo(x + r * 1.3, y)
          .moveTo(x, y - r * 1.3)
          .lineTo(x, y + r * 1.3)
          .stroke({ width: 2, color: ATTACK_COLOR, alpha: 0.9 });
        break;
      }
      case "wait": {
        const w = HEX_SIZE * 0.22;
        const h = HEX_SIZE * 0.32;
        this.committedGfx
          .poly([x - w, y - h, x + w, y - h, x, y, x - w, y - h])
          .fill({ color: WAIT_COLOR, alpha: 0.9 })
          .poly([x - w, y + h, x + w, y + h, x, y, x - w, y + h])
          .fill({ color: WAIT_COLOR, alpha: 0.9 })
          .rect(x - w, y - h, 2 * w, 0.06 * HEX_SIZE)
          .fill({ color: WAIT_COLOR, alpha: 0.9 })
          .rect(x - w, y + h - 0.06 * HEX_SIZE, 2 * w, 0.06 * HEX_SIZE)
          .fill({ color: WAIT_COLOR, alpha: 0.9 });
        break;
      }
    }
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
