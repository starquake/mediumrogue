import { Container, Graphics } from "pixi.js";

import type { Hex } from "../protocol.gen";
import { hexCorners, hexToPixel, HEX_SIZE } from "./hex";

// Ember orange — hostile/attack family (red-side of the palette, like the
// melee tint and attack flash) but hot enough to read as "this exact tile
// gets hit", distinct from MoveRangeLayer's muted washes.
const ATTACK_TILE_COLOR = 0xff8a3d;

/**
 * The attack-target highlight (#101): the exact tile(s) a click would hit.
 * Two states, drawn from the same tile math (main.ts's attackTilesFor):
 *
 * - hover: while the pointer rests on a tile a click would attack, every
 *   tile that attack would hit lights up — the one target tile for a melee
 *   swing or a bow shot, the full blast disc for a ground-targeted AoE
 *   (weapon aoeRadius > 0, e.g. the mage's Ember Focus).
 * - committed: after the click, the same tiles stay lit (stronger) until the
 *   turn resolves — the on-map half of the committed/pending indicator the
 *   clock-gated-actions convention requires, alongside FeedbackLayer's
 *   crosshair on the target itself.
 *
 * Distinct from MoveRangeLayer's per-turn tints: that layer answers "where
 * can I act", this one answers "what will THIS action hit". Insets the hex
 * outline further (HEX_SIZE - 5) so the two never read as one wash where
 * they overlap. Empty outside combat — no click attacks out there.
 */
export class AttackHighlightLayer {
  readonly container = new Container();
  private readonly gfx = new Graphics();

  constructor() {
    this.container.addChild(this.gfx);
  }

  update(hover: Hex[], committed: Hex[]): void {
    this.gfx.clear();

    for (const h of hover) {
      this.tile(h, 0.18, 0.7, 2);
    }

    for (const h of committed) {
      this.tile(h, 0.32, 0.95, 2.5);
    }
  }

  private tile(h: Hex, fillAlpha: number, strokeAlpha: number, strokeWidth: number): void {
    const corners = hexCorners(hexToPixel(h), HEX_SIZE - 5);
    this.gfx
      .poly(corners)
      .fill({ color: ATTACK_TILE_COLOR, alpha: fillAlpha })
      .stroke({ width: strokeWidth, color: ATTACK_TILE_COLOR, alpha: strokeAlpha });
  }
}
