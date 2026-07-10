import { Container, Graphics } from "pixi.js";

import type { Hex } from "../protocol.gen";
import { hexCorners, hexToPixel, HEX_SIZE } from "./hex";

const MOVE_TILE_COLOR = 0x8fd0ff; // matches my dot / the destination ring
const BUMP_TILE_COLOR = 0xd6544f; // matches hostiles / the attack flash

/**
 * The tactical movement overlay: while my entity is in a combat bubble, the
 * hexes reachable this turn are tinted — blue for open moves, red for
 * bump-attacks (a hostile stands there; stepping in swings instead). Empty
 * outside combat: in WeGo exploration, click-anywhere pathing stays the
 * right interaction, so the overlay would only be noise. main.ts computes
 * the reachable sets (it owns walkability + occupancy); this layer only
 * draws them.
 */
export class MoveRangeLayer {
  readonly container = new Container();
  private readonly gfx = new Graphics();

  constructor() {
    this.container.addChild(this.gfx);
  }

  update(moves: Hex[], bumps: Hex[]): void {
    this.gfx.clear();

    for (const h of moves) {
      this.tile(h, MOVE_TILE_COLOR);
    }

    for (const h of bumps) {
      this.tile(h, BUMP_TILE_COLOR);
    }
  }

  private tile(h: Hex, color: number): void {
    const corners = hexCorners(hexToPixel(h), HEX_SIZE - 2);
    this.gfx
      .poly(corners)
      .fill({ color, alpha: 0.16 })
      .stroke({ width: 1.5, color, alpha: 0.6 });
  }
}
