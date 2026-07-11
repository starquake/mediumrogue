import { Container, Graphics, type Ticker } from "pixi.js";

import type { Hex } from "../protocol.gen";
import { hexToPixel, HEX_SIZE } from "./hex";

const GOAL_COLOR = 0xf2c14e; // gold — distinct from any faction/item color on the map
const PULSE_PERIOD_MS = 1400;

interface Marker {
  gfx: Graphics;
  hex: Hex;
  pulseMs: number;
}

/**
 * The quest-goal marker (item 12, playtest batch 2): a gold diamond/flag on
 * each of MY active "reach" quests' goal hexes — item 14 lets me hold
 * several concurrently, so this is keyed by quest id and can show more than
 * one at once — pulsing subtly so it reads as a live objective rather than
 * map furniture. Kill quests get no marker (there is no single hex to point
 * at). Added to the world container ABOVE the ground layer, BELOW entities
 * — a player standing on a goal hex still reads as a player, with the
 * marker as backdrop, not an occluding icon.
 */
export class QuestMarkerLayer {
  readonly container = new Container();
  private markers = new Map<number, Marker>();

  constructor(ticker: Ticker) {
    ticker.add(this.tick);
  }

  /** Set the full set of active goals, keyed by quest id. Replaces the previous set wholesale. */
  setGoals(goals: { id: number; hex: Hex }[]): void {
    const present = new Set<number>();

    for (const g of goals) {
      present.add(g.id);

      const existing = this.markers.get(g.id);
      if (existing !== undefined && existing.hex.q === g.hex.q && existing.hex.r === g.hex.r) {
        continue; // unchanged — don't reset the pulse phase every turn bundle
      }

      if (existing !== undefined) {
        existing.hex = g.hex;
        existing.pulseMs = 0;

        continue;
      }

      const gfx = new Graphics();
      this.container.addChild(gfx);
      this.markers.set(g.id, { gfx, hex: g.hex, pulseMs: 0 });
    }

    for (const [id, marker] of this.markers) {
      if (!present.has(id)) {
        marker.gfx.destroy();
        this.markers.delete(id);
      }
    }
  }

  private tick = (ticker: Ticker): void => {
    for (const marker of this.markers.values()) {
      marker.pulseMs = (marker.pulseMs + ticker.deltaMS) % PULSE_PERIOD_MS;
      const f = marker.pulseMs / PULSE_PERIOD_MS;
      // A gentle breathe: scale 0.85 -> 1.15 and back, via a triangle wave.
      const scale = 0.85 + 0.3 * (f < 0.5 ? f * 2 : 2 - f * 2);
      const size = HEX_SIZE * 0.5 * scale;
      const { x, y } = hexToPixel(marker.hex);

      marker.gfx
        .clear()
        .poly([x, y - size, x + size * 0.65, y, x, y + size, x - size * 0.65, y])
        .fill({ color: GOAL_COLOR, alpha: 0.85 })
        .poly([x, y - size, x + size * 0.65, y, x, y + size, x - size * 0.65, y])
        .stroke({ width: 2, color: 0x0b0f0b });
    }
  };
}
