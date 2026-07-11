import { Container, Graphics, type Ticker } from "pixi.js";

import type { Hex } from "../protocol.gen";
import { hexToPixel, HEX_SIZE } from "./hex";

const GOAL_COLOR = 0xf2c14e; // gold — distinct from any faction/item color on the map
const PULSE_PERIOD_MS = 1400;

/**
 * The quest-goal marker (item 12, playtest batch 2): a gold diamond/flag on
 * MY active "reach" quest's goal hex, pulsing subtly so it reads as a live
 * objective rather than map furniture. Kill quests get no marker (there is
 * no single hex to point at). Added to the world container ABOVE the ground
 * layer, BELOW entities — a player standing on the goal hex still reads as
 * a player, with the marker as backdrop, not an occluding icon.
 */
export class QuestMarkerLayer {
  readonly container = new Container();
  private readonly gfx = new Graphics();
  private goal: Hex | null = null;
  private pulseMs = 0;

  constructor(ticker: Ticker) {
    this.container.addChild(this.gfx);
    ticker.add(this.tick);
  }

  /** Set (or clear, with null) the goal hex to mark. */
  setGoal(hex: Hex | null): void {
    if (this.goal !== null && hex !== null && this.goal.q === hex.q && this.goal.r === hex.r) {
      return; // unchanged — don't reset the pulse phase every turn bundle
    }

    this.goal = hex;
    this.pulseMs = 0;
    if (hex === null) {
      this.gfx.clear();
    }
  }

  private tick = (ticker: Ticker): void => {
    if (this.goal === null) {
      return;
    }

    this.pulseMs = (this.pulseMs + ticker.deltaMS) % PULSE_PERIOD_MS;
    const f = this.pulseMs / PULSE_PERIOD_MS;
    // A gentle breathe: scale 0.85 -> 1.15 and back, via a triangle wave.
    const scale = 0.85 + 0.3 * (f < 0.5 ? f * 2 : 2 - f * 2);
    const size = HEX_SIZE * 0.5 * scale;
    const { x, y } = hexToPixel(this.goal);

    this.gfx
      .clear()
      .poly([x, y - size, x + size * 0.65, y, x, y + size, x - size * 0.65, y])
      .fill({ color: GOAL_COLOR, alpha: 0.85 })
      .poly([x, y - size, x + size * 0.65, y, x, y + size, x - size * 0.65, y])
      .stroke({ width: 2, color: 0x0b0f0b });
  };
}
